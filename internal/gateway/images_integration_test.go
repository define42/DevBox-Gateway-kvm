//go:build integration

package gateway

import (
	"bytes"
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
	dir := t.TempDir()
	raw, compressed := filepath.Join(dir, "source.raw"), filepath.Join(dir, "compressed.qcow2")
	data := bytes.Repeat([]byte("compressed desktop disk\n"), 65536)
	if err := os.WriteFile(raw, data, 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.CommandContext(t.Context(), qemu, "convert", "-f", "raw", "-O", "qcow2", "-c", raw, compressed)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create compressed fixture: %v: %s", err, output)
	}
	file, err := os.Open(compressed)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	m := newTestManager(t, "graceful")
	image, err := m.ImportImage(t.Context(), "compressed.qcow2", file)
	if err != nil {
		t.Fatal(err)
	}
	opened, err := disk.OpenReadOnly(filepath.Join(m.imageDirectory(image.ID), "disk.qcow2"))
	if err != nil {
		t.Fatalf("normalized image unsupported by native disk driver: %v", err)
	}
	defer opened.Close()
	got := make([]byte, len(data))
	if _, err := opened.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, data) {
		t.Fatal("compressed image normalization changed guest data")
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
		})
	}
}
