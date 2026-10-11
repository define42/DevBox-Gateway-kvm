package disk

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOpenRawImage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "disk.raw")
	want := []byte("raw image data")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}

	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()

	if got := img.Size(); got != int64(len(want)) {
		t.Fatalf("Size: got %d, want %d", got, len(want))
	}

	got := make([]byte, len(want))
	if _, err := img.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("ReadAt: got %q, want %q", got, want)
	}

	replacement := []byte("RAW")
	if _, err := img.WriteAt(replacement, 0); err != nil {
		t.Fatal(err)
	}

	if err := img.Sync(); err != nil {
		t.Fatal(err)
	}

	onDisk, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(onDisk[:len(replacement)], replacement) {
		t.Fatalf("WriteAt: got %q, want %q", onDisk[:len(replacement)], replacement)
	}
}

func TestOpenQCOW2CapacityAndUnallocatedRead(t *testing.T) {
	t.Parallel()

	path := createQCOW2(t, "8M")

	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()

	if got := img.Size(); got != 8*1024*1024 {
		t.Fatalf("Size: got %d, want %d", got, 8*1024*1024)
	}

	got := bytes.Repeat([]byte{0xa5}, 4096)
	if _, err := img.ReadAt(got, 1234); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, make([]byte, len(got))) {
		t.Fatal("unallocated qcow2 read did not return zeroes")
	}
}

func TestQCOW2WriteReadReopenAndQEMUCheck(t *testing.T) {
	t.Parallel()

	path := createQCOW2(t, "8M")

	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}

	qcow, ok := img.(*qcow2Image)
	if !ok {
		t.Fatal("Open did not detect qcow2 image")
	}

	clusterSize := int64(qcow.clusterSize)
	firstOff := int64(1234)
	firstData := []byte("hello qcow2")
	crossOff := clusterSize - 13
	crossData := bytes.Repeat([]byte{0x5a}, 64)
	partialOff := 2*clusterSize + 100
	partialData := []byte{0xde, 0xad, 0xbe, 0xef}

	mustWriteAt(t, img, firstData, firstOff)
	mustWriteAt(t, img, crossData, crossOff)
	mustWriteAt(t, img, partialData, partialOff)

	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	img, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}

	assertReadAt(t, img, firstData, firstOff)
	assertReadAt(t, img, crossData, crossOff)
	assertReadAt(t, img, partialData, partialOff)

	aroundPartial := bytes.Repeat([]byte{0xa5}, 128)
	if _, err := img.ReadAt(aroundPartial, partialOff-64); err != nil {
		t.Fatal(err)
	}

	wantAround := make([]byte, 128)
	copy(wantAround[64:], partialData)
	if !bytes.Equal(aroundPartial, wantAround) {
		t.Fatal("partial write did not preserve surrounding zeroes")
	}

	if err := img.Close(); err != nil {
		t.Fatal(err)
	}

	runCommand(t, "qemu-img", "check", path)

	rawPath := filepath.Join(t.TempDir(), "converted.raw")
	runCommand(t, "qemu-img", "convert", "-f", "qcow2", "-O", "raw", path, rawPath)

	raw, err := os.ReadFile(rawPath)
	if err != nil {
		t.Fatal(err)
	}

	assertBytesAt(t, raw, firstData, firstOff)
	assertBytesAt(t, raw, crossData, crossOff)
	assertBytesAt(t, raw, partialData, partialOff)
}

func TestQCOW2ZeroClusterRead(t *testing.T) {
	t.Parallel()

	requireCommand(t, "qemu-io")
	path := createQCOW2(t, "1M")

	runCommand(t, "qemu-io", "-f", "qcow2", "-c", "write -z 0 4k", path)

	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()

	got := bytes.Repeat([]byte{0xa5}, 4096)
	if _, err := img.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}

	if !bytes.Equal(got, make([]byte, len(got))) {
		t.Fatal("zero cluster read did not return zeroes")
	}
}

func TestOpenQCOW2RejectsBackingFile(t *testing.T) {
	t.Parallel()

	basePath := filepath.Join(t.TempDir(), "base.raw")
	if err := os.WriteFile(basePath, make([]byte, 1024*1024), 0o600); err != nil {
		t.Fatal(err)
	}

	overlayPath := filepath.Join(t.TempDir(), "overlay.qcow2")
	runCommand(t, "qemu-img", "create", "-f", "qcow2", "-F", "raw", "-b", basePath, overlayPath)

	img, err := Open(overlayPath)
	if err == nil {
		_ = img.Close()

		t.Fatal("Open succeeded, want backing-file error")
	}

	if !strings.Contains(err.Error(), "backing") {
		t.Fatalf("Open error: got %q, want backing-file error", err)
	}
}

func createQCOW2(t *testing.T, size string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "disk.qcow2")
	runCommand(t, "qemu-img", "create", "-f", "qcow2", path, size)

	return path
}

func runCommand(t *testing.T, name string, args ...string) {
	t.Helper()

	path := requireCommand(t, name)
	cmd := exec.Command(path, args...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("%s %s: %v\n%s", name, strings.Join(args, " "), err, out)
	}
}

func requireCommand(t *testing.T, name string) string {
	t.Helper()

	path, err := exec.LookPath(name)
	if err != nil {
		t.Skipf("%s is not installed", name)
	}

	return path
}

func mustWriteAt(t *testing.T, img Image, data []byte, off int64) {
	t.Helper()

	n, err := img.WriteAt(data, off)
	if err != nil || n != len(data) {
		t.Fatalf("WriteAt(%d): got (%d, %v), want (%d, nil)", off, n, err, len(data))
	}
}

func assertReadAt(t *testing.T, img Image, want []byte, off int64) {
	t.Helper()

	got := make([]byte, len(want))
	n, err := img.ReadAt(got, off)
	if err != nil || n != len(want) {
		t.Fatalf("ReadAt(%d): got (%d, %v), want (%d, nil)", off, n, err, len(want))
	}

	if !bytes.Equal(got, want) {
		t.Fatalf("ReadAt(%d): got %x, want %x", off, got, want)
	}
}

func assertBytesAt(t *testing.T, got []byte, want []byte, off int64) {
	t.Helper()

	if off < 0 || off+int64(len(want)) > int64(len(got)) {
		t.Fatalf("offset %d is outside buffer length %d", off, len(got))
	}

	if !bytes.Equal(got[off:off+int64(len(want))], want) {
		t.Fatalf("bytes at %d: got %x, want %x", off, got[off:off+int64(len(want))], want)
	}
}
