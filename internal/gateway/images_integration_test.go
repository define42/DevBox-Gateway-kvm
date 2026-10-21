//go:build integration

package gateway

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

func TestImageCatalogNormalizesCompressedQCOW2(t *testing.T) {
	t.Parallel()
	qemu, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img is needed for compressed QCOW2 integration coverage")
	}
	for _, tc := range []struct {
		name    string
		options string
	}{
		{name: "deflate v2", options: "compat=0.10,compression_type=zlib"},
		{name: "deflate v3", options: "compat=1.1,compression_type=zlib"},
		{name: "zstd", options: "compat=1.1,compression_type=zstd"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			raw, compressed := filepath.Join(dir, "source.raw"), filepath.Join(dir, "compressed.qcow2")
			data := bytes.Repeat([]byte("compressed desktop disk\n"), 65536)
			// Exercise compressed sectors, sparse clusters, and a partial final cluster.
			clear(data[256<<10 : 512<<10])
			data = append(data, bytes.Repeat([]byte{0x5a}, 512)...)
			if err := os.WriteFile(raw, data, 0o600); err != nil {
				t.Fatal(err)
			}
			runImageFixtureCommand(t, qemu, "convert", "-f", "raw", "-O", "qcow2", "-c", "-o", tc.options, raw, compressed)
			importQCOW2AndCompare(t, qemu, compressed, data)
		})
	}
}

func TestImageCatalogNormalizesQCOW2Layouts(t *testing.T) {
	t.Parallel()
	qemu, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img is needed for QCOW2 layout integration coverage")
	}
	for _, bits := range []int{1, 2, 4, 8, 32, 64} {
		t.Run(fmt.Sprintf("%d-bit refcounts", bits), func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			raw, source := filepath.Join(dir, "source.raw"), filepath.Join(dir, "source.qcow2")
			data := make([]byte, 2<<20+512)
			copy(data, bytes.Repeat([]byte{0xa5, 0x5a, 0xe7}, 32768))
			copy(data[len(data)-512:], bytes.Repeat([]byte{0x33}, 512))
			if err := os.WriteFile(raw, data, 0o600); err != nil {
				t.Fatal(err)
			}
			runImageFixtureCommand(t, qemu, "convert", "-f", "raw", "-O", "qcow2",
				"-o", fmt.Sprintf("compat=1.1,refcount_bits=%d", bits), raw, source)
			importQCOW2AndCompare(t, qemu, source, data)
		})
	}
	t.Run("persistent dirty bitmaps", func(t *testing.T) {
		t.Parallel()
		qemuIO, err := exec.LookPath("qemu-io")
		if err != nil {
			t.Skip("qemu-io is needed to populate persistent bitmap fixtures")
		}
		dir := t.TempDir()
		raw, source := filepath.Join(dir, "source.raw"), filepath.Join(dir, "bitmaps.qcow2")
		data := make([]byte, 1<<20)
		copy(data, bytes.Repeat([]byte{0xa5}, 512))
		copy(data[len(data)-512:], bytes.Repeat([]byte{0x5a}, 512))
		if err := os.WriteFile(raw, data, 0o600); err != nil {
			t.Fatal(err)
		}
		runImageFixtureCommand(t, qemu, "convert", "-f", "raw", "-O", "qcow2", raw, source)
		runImageFixtureCommand(t, qemu, "bitmap", "--add", "-g", "512", source, "incremental-a")
		runImageFixtureCommand(t, qemu, "bitmap", "--add", "-g", "4096", source, "incremental-second")
		// Persist dirty entries so validation visits bitmap tables and blocks,
		// as well as multiple variable-length directory entries and their padding.
		runImageFixtureCommand(t, qemuIO, "-f", "qcow2", "-c", "write -q -P 102 262144 512", source)
		copy(data[262144:], bytes.Repeat([]byte{102}, 512))
		importQCOW2AndCompare(t, qemu, source, data)
	})
	t.Run("extended L2 subcluster holes and zeros", func(t *testing.T) {
		t.Parallel()
		qemuIO, err := exec.LookPath("qemu-io")
		if err != nil {
			t.Skip("qemu-io is needed to generate subcluster fixtures")
		}
		const capacity = 2 << 20
		const cluster = 64 << 10
		const subcluster = cluster / 32
		source := filepath.Join(t.TempDir(), "extended.qcow2")
		runImageFixtureCommand(t, qemu, "create", "-f", "qcow2", "-o",
			"compat=1.1,cluster_size=65536,extended_l2=on", source, fmt.Sprint(capacity))
		data := make([]byte, capacity)
		for _, write := range []struct {
			offset  int
			length  int
			pattern byte
			zero    bool
		}{
			{offset: 0, length: subcluster, pattern: 0xa5},
			{offset: 30 * subcluster, length: 2 * subcluster, pattern: 0x5a},
			{offset: 2 * subcluster, length: subcluster, zero: true},
			{offset: cluster, length: cluster, pattern: 0x44},
			{offset: cluster + subcluster, length: subcluster, zero: true},
			{offset: capacity - subcluster, length: subcluster, pattern: 0xbb},
		} {
			command := fmt.Sprintf("write -q -P %d %d %d", write.pattern, write.offset, write.length)
			if write.zero {
				command = fmt.Sprintf("write -q -z %d %d", write.offset, write.length)
			}
			runImageFixtureCommand(t, qemuIO, "-f", "qcow2", "-c", command, source)
			for i := write.offset; i < write.offset+write.length; i++ {
				data[i] = write.pattern
			}
		}
		assertExtendedL2SubclusterFixture(t, source)
		importQCOW2AndCompare(t, qemu, source, data)
	})
}

// Verify the generated fixture contains all three subcluster states in one L2
// entry; otherwise a QEMU version could silently reduce this to ordinary reads.
func assertExtendedL2SubclusterFixture(t *testing.T, path string) {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var header [104]byte
	if _, err := file.ReadAt(header[:], 0); err != nil {
		t.Fatal(err)
	}
	l1Offset := binary.BigEndian.Uint64(header[40:48])
	var l1 [8]byte
	if _, err := file.ReadAt(l1[:], int64(l1Offset)); err != nil {
		t.Fatal(err)
	}
	l2Offset := binary.BigEndian.Uint64(l1[:]) & 0x00fffffffffffe00
	var entry [16]byte
	if _, err := file.ReadAt(entry[:], int64(l2Offset)); err != nil {
		t.Fatal(err)
	}
	bitmap := binary.BigEndian.Uint64(entry[8:])
	allocated, zero := uint32(bitmap), uint32(bitmap>>32)
	if allocated == 0 || zero == 0 || allocated|zero == ^uint32(0) || allocated&zero != 0 {
		t.Fatalf("fixture lacks distinct allocated, zero, and hole subclusters: allocated=%#x zero=%#x", allocated, zero)
	}
}

func importQCOW2AndCompare(t *testing.T, qemu, source string, want []byte) {
	t.Helper()
	runImageFixtureCommand(t, qemu, "check", "-f", "qcow2", source)
	file, err := os.Open(source)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	m := newTestManager(t, "graceful")
	image, err := m.ImportImage(t.Context(), "source.qcow2", file)
	if err != nil {
		t.Fatal(err)
	}
	normalized := filepath.Join(m.imageDirectory(image.ID), "disk.qcow2")
	opened, err := disk.OpenReadOnly(normalized)
	if err != nil {
		t.Fatalf("normalized image unsupported by native disk driver: %v", err)
	}
	defer opened.Close()
	if opened.Size() != int64(len(want)) || image.VirtualSizeBytes != int64(len(want)) {
		t.Fatalf("normalized capacity = %d (catalog %d), want %d", opened.Size(), image.VirtualSizeBytes, len(want))
	}
	got := make([]byte, len(want))
	if n, err := opened.ReadAt(got, 0); err != nil || n != len(got) {
		t.Fatalf("read normalized contents: %d bytes, %v", n, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("normalization changed guest-visible bytes")
	}
	if err := opened.Close(); err != nil {
		t.Fatal(err)
	}
	runImageFixtureCommand(t, qemu, "check", "-f", "qcow2", normalized)
	// Compare both interpretations against raw expected contents, independently
	// of the Go importer and live disk reader sharing an implementation mistake.
	expected := filepath.Join(t.TempDir(), "expected.raw")
	if err := os.WriteFile(expected, want, 0o600); err != nil {
		t.Fatal(err)
	}
	runImageFixtureCommand(t, qemu, "compare", "-f", "raw", "-F", "qcow2", expected, source)
	runImageFixtureCommand(t, qemu, "compare", "-f", "raw", "-F", "qcow2", expected, normalized)
}

func runImageFixtureCommand(t *testing.T, program string, args ...string) {
	t.Helper()
	//nolint:gosec // Test-owned fixture tools, fixed options and private paths, without a shell.
	cmd := exec.CommandContext(t.Context(), program, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s %v: %v: %s", filepath.Base(program), args, err, output)
	}
}

func TestImageCatalogImportedQCOW2SupportsFullCapacity(t *testing.T) {
	t.Parallel()
	qemu, err := exec.LookPath("qemu-img")
	if err != nil {
		t.Skip("qemu-img is needed for QCOW2 capacity integration coverage")
	}
	for _, test := range []struct {
		name   string
		compat string
	}{
		{name: "v2", compat: "0.10"},
		{name: "v3", compat: "1.1"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			const capacity = 16 << 20
			dir := t.TempDir()
			raw, source := filepath.Join(dir, "source.raw"), filepath.Join(dir, "source.qcow2")
			data := make([]byte, capacity)
			marker := []byte("imported source contents")
			copy(data, marker)
			copy(data[len(data)-len(marker):], marker)
			if err := os.WriteFile(raw, data, 0o600); err != nil {
				t.Fatal(err)
			}
			// Small clusters give this sparse image an initially small refcount table.
			// The native writer must be able to fill the entire advertised capacity.
			//nolint:gosec // Fixed test options and private fixture paths; no shell or caller input.
			cmd := exec.CommandContext(t.Context(), qemu, "convert", "-f", "raw", "-O", "qcow2",
				"-o", "compat="+test.compat+",cluster_size=512", raw, source)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("create small-cluster fixture: %v: %s", err, output)
			}
			file, err := os.Open(source)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			m := newTestManager(t, "graceful")
			image, err := m.ImportImage(t.Context(), "small-clusters.qcow2", file)
			if err != nil {
				t.Fatal(err)
			}
			if image.VirtualSizeBytes != capacity {
				t.Fatalf("imported capacity = %d, want %d", image.VirtualSizeBytes, capacity)
			}
			vm, err := m.CreateWithImage(t.Context(), "alice", "capacity", managerTestPasswordHash(), image.ID)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(m.directory(vm.ID), "disk.img")
			opened, err := disk.OpenQCOW2WithSize(path, capacity)
			if err != nil {
				t.Fatal(err)
			}
			defer opened.Close()
			got := make([]byte, len(data))
			if n, err := opened.ReadAt(got, 0); err != nil || n != len(got) {
				t.Fatalf("read imported contents: %d bytes, %v", n, err)
			}
			if !bytes.Equal(got, data) {
				t.Fatal("importing and cloning the image changed guest data")
			}
			runImageFixtureCommand(t, qemu, "compare", "-f", "raw", "-F", "qcow2", raw, path)
			for index := range data {
				data[index] = byte(index%251 + 1)
			}
			if n, err := opened.WriteAt(data, 0); err != nil || n != len(data) {
				t.Fatalf("write advertised capacity: %d of %d bytes, %v", n, len(data), err)
			}
			if err := opened.Sync(); err != nil {
				t.Fatal(err)
			}
			if err := opened.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := disk.OpenReadOnly(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			if n, err := reopened.ReadAt(got, 0); err != nil || n != len(got) {
				t.Fatalf("read full disk after reopening: %d bytes, %v", n, err)
			}
			if !bytes.Equal(got, data) {
				t.Fatal("full-capacity writes did not survive reopening")
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			cmd = exec.CommandContext(t.Context(), qemu, "check", "-f", "qcow2", path)
			if output, err := cmd.CombinedOutput(); err != nil {
				t.Fatalf("check fully allocated disk: %v: %s", err, output)
			}
			if err := os.WriteFile(raw, data, 0o600); err != nil {
				t.Fatal(err)
			}
			runImageFixtureCommand(t, qemu, "compare", "-f", "raw", "-F", "qcow2", raw, path)
		})
	}
}
