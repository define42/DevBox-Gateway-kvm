//go:build integration

package gateway

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/define42/gokvm/internal/disk"
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
