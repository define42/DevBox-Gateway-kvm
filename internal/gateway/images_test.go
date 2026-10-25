package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

func catalogQCOW2(marker string) []byte {
	const cluster = 65536
	data := make([]byte, 6*cluster)
	copy(data, "QFI\xfb")
	binary.BigEndian.PutUint32(data[4:8], 3)
	binary.BigEndian.PutUint32(data[20:24], 16)
	binary.BigEndian.PutUint64(data[24:32], 2<<20)
	binary.BigEndian.PutUint32(data[36:40], 1)
	binary.BigEndian.PutUint64(data[40:48], 3*cluster)
	binary.BigEndian.PutUint64(data[48:56], cluster)
	binary.BigEndian.PutUint32(data[56:60], 1)
	binary.BigEndian.PutUint32(data[96:100], 4)
	binary.BigEndian.PutUint32(data[100:104], 104)
	binary.BigEndian.PutUint64(data[cluster:cluster+8], 2*cluster)
	for index := range 6 {
		binary.BigEndian.PutUint16(data[2*cluster+index*2:], 1)
	}
	binary.BigEndian.PutUint64(data[3*cluster:3*cluster+8], uint64(4*cluster)|(uint64(1)<<63))
	binary.BigEndian.PutUint64(data[4*cluster:4*cluster+8], uint64(5*cluster)|(uint64(1)<<63))
	copy(data[5*cluster:], marker)

	return data
}

func TestImageCatalogWithoutDefaultImage(t *testing.T) {
	t.Parallel()
	config := managerTestConfig(t)
	config.BaseImage = ""
	m, err := NewManager(config)
	if err != nil {
		t.Fatalf("start without a base image: %v", err)
	}
	defer m.Close()
	images, err := m.Images(t.Context())
	if err != nil || len(images) != 0 {
		t.Fatalf("initial catalog = %+v, %v", images, err)
	}
	hash := seedHelperPasswordHash()
	if _, err := m.Create(t.Context(), "alice", "selected", hash); !errors.Is(err, ErrNotFound) {
		t.Fatalf("created VM without a template: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(config.DataDir, "vms"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("failed creation left VM state: %v, %v", entries, err)
	}
	image, err := m.ImportImage(t.Context(), "Rocky Desktop.qcow2", bytes.NewReader(catalogQCOW2("selected image")))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := m.CreateWithImage(t.Context(), "alice", "selected", hash, ""); !errors.Is(err, ErrNotFound) {
		t.Fatalf("creation without a default did not require an image selection: %v", err)
	}
	vm, err := m.CreateWithImage(t.Context(), "alice", "selected", hash, image.ID)
	if err != nil {
		t.Fatalf("create from the first uploaded image: %v", err)
	}
	assertSelectedImageData(t, m, vm)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	images, err = reopened.Images(t.Context())
	if err != nil || len(images) != 1 || images[0] != image || images[0].Default {
		t.Fatalf("uploaded catalog did not persist: %+v, %v", images, err)
	}
	if err := reopened.DeleteImage(t.Context(), image.ID); err != nil {
		t.Fatalf("delete the last template: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := NewManager(config)
	if err != nil {
		t.Fatalf("reopen an empty catalog with an existing VM: %v", err)
	}
	defer again.Close()
	images, err = again.Images(t.Context())
	if err != nil || len(images) != 0 {
		t.Fatalf("deleted template reappeared: %+v, %v", images, err)
	}
	saved, err := again.GetAny(t.Context(), vm.ID)
	if err != nil || saved.ImageID != image.ID || saved.ImageName != image.Name {
		t.Fatalf("existing VM lost its source image metadata: %+v, %v", saved, err)
	}
	assertSelectedImageData(t, again, saved)
}

func TestImageCatalogImportSelectionPersistenceAndDeletion(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	images, err := m.Images(t.Context())
	if err != nil || len(images) != 1 || images[0].ID != DefaultImageID || !images[0].Default {
		t.Fatalf("default catalog = %+v, %v", images, err)
	}
	image, err := m.ImportImage(t.Context(), "Rocky Desktop.qcow2", bytes.NewReader(catalogQCOW2("selected image")))
	if err != nil {
		t.Fatal(err)
	}
	if !validID(image.ID) || image.Default || image.VirtualSizeBytes != 2<<20 || image.SizeBytes != 6*65536 {
		t.Fatalf("imported image = %+v", image)
	}
	duplicate := bytes.NewReader(catalogQCOW2("duplicate"))
	if _, err := m.ImportImage(t.Context(), "rocky desktop.qcow2", duplicate); !errors.Is(err, ErrConflict) {
		t.Fatalf("case-insensitive duplicate image accepted: %v", err)
	}
	hash := "$6$testsalt$" + strings.Repeat("a", 85) + "."
	vm, err := m.CreateWithImage(t.Context(), "alice", "selected", hash, image.ID)
	if err != nil {
		t.Fatal(err)
	}
	if vm.ImageID != image.ID || vm.ImageName != image.Name {
		t.Fatalf("VM lost its selected image: %+v", vm)
	}
	assertSelectedImageData(t, m, vm)
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(m.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	images, err = reopened.Images(t.Context())
	if err != nil || len(images) != 2 || images[1] != image {
		t.Fatalf("catalog did not persist: %+v, %v", images, err)
	}
	if err := reopened.DeleteImage(t.Context(), DefaultImageID); !errors.Is(err, ErrConflict) {
		t.Fatalf("default image deletable: %v", err)
	}
	if err := reopened.DeleteImage(t.Context(), image.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := reopened.CreateWithImage(t.Context(), "bob", "removed", hash, image.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted image remained selectable: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reopened.directory(vm.ID), "disk.img")); err != nil {
		t.Fatalf("deleting template removed a VM's standalone disk: %v", err)
	}
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	again, err := NewManager(m.config)
	if err != nil {
		t.Fatal(err)
	}
	defer again.Close()
	saved, err := again.GetAny(t.Context(), vm.ID)
	if err != nil || saved.ImageID != image.ID || saved.ImageName != image.Name {
		t.Fatalf("existing VM lost deleted source image metadata: %+v, %v", saved, err)
	}
}

func assertSelectedImageData(t *testing.T, m *Manager, vm VM) {
	t.Helper()
	clone, err := disk.OpenReadOnly(filepath.Join(m.directory(vm.ID), "disk.img"))
	if err != nil {
		t.Fatal(err)
	}
	marker := make([]byte, len("selected image"))
	_, readErr := clone.ReadAt(marker, 0)
	if err := errors.Join(readErr, clone.Close()); err != nil || string(marker) != "selected image" {
		t.Fatalf("VM did not clone selected image: %q, %v", marker, err)
	}
}

func TestImageCatalogFailureCleansStaging(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name  string
		input io.Reader
		limit int64
		want  error
	}{
		{name: "empty", input: strings.NewReader(""), want: ErrInvalid},
		{name: "raw disk", input: strings.NewReader("raw image bytes"), want: ErrInvalid},
		{name: "too large", input: bytes.NewReader(catalogQCOW2("large")), limit: 100, want: ErrImageTooLarge},
		{name: "reader failure", input: imageFailedReader{}, want: io.ErrUnexpectedEOF},
	}
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t)
			config.MaxImageUploadBytes = test.limit
			m, err := NewManager(config)
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if _, err := m.ImportImage(t.Context(), "broken.qcow2", test.input); !errors.Is(err, test.want) {
				t.Fatalf("import error = %v, want %v", err, test.want)
			}
			entries, err := os.ReadDir(filepath.Join(m.config.DataDir, "images"))
			if err != nil || len(entries) != 0 {
				t.Fatalf("failed import left files: %v, %v", entries, err)
			}
			images, err := m.Images(t.Context())
			if err != nil || len(images) != 1 {
				t.Fatalf("failed import published an image: %+v, %v", images, err)
			}
		})
	}
}

type imageFailedReader struct{}

func (imageFailedReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestImageCatalogCancellationDuringUpload(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	input := &imageCancelReader{reader: bytes.NewReader(catalogQCOW2("cancelled upload")), cancel: cancel}
	if _, err := m.ImportImage(ctx, "cancelled.qcow2", input); !errors.Is(err, context.Canceled) {
		t.Fatalf("interrupted upload returned %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(m.config.DataDir, "images"))
	if err != nil || len(entries) != 0 {
		t.Fatalf("cancelled import left files: %v, %v", entries, err)
	}
}

type imageCancelReader struct {
	reader io.Reader
	cancel context.CancelFunc
}

func (r *imageCancelReader) Read(buffer []byte) (int, error) {
	n, err := r.reader.Read(buffer)
	r.cancel()

	return n, err
}

func TestImageCatalogCancellationAndInvalidNames(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	cancelled := bytes.NewReader(catalogQCOW2("cancelled"))
	if _, err := m.ImportImage(ctx, "cancelled.qcow2", cancelled); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled import = %v", err)
	}
	invalidNames := []string{"", "../outside.qcow2", "path\\file.qcow2", "line\nbreak", " name", strings.Repeat("x", 129)}
	for _, name := range invalidNames {
		input := bytes.NewReader(catalogQCOW2("bad name"))
		if _, err := m.ImportImage(t.Context(), name, input); !errors.Is(err, ErrInvalid) {
			t.Fatalf("invalid image name %q accepted: %v", name, err)
		}
	}
	if err := m.DeleteImage(t.Context(), "../../outside"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("invalid image ID accepted: %v", err)
	}
}

func TestImageCatalogRejectsUnsafeSavedImage(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{"image symlink", "metadata symlink", "directory permissions"} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			image, err := m.ImportImage(t.Context(), "saved.qcow2", bytes.NewReader(catalogQCOW2("saved")))
			if err != nil {
				t.Fatal(err)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			dir := m.imageDirectory(image.ID)
			switch kind {
			case "image symlink", "metadata symlink":
				name := "disk.qcow2"
				if kind == "metadata symlink" {
					name = "image.json"
				}
				path := filepath.Join(dir, name)
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(m.config.BaseImage, path); err != nil {
					t.Fatal(err)
				}
			case "directory permissions":
				if err := os.Chmod(dir, 0o755); err != nil { //nolint:gosec // Deliberately unsafe fixture.
					t.Fatal(err)
				}
			}
			if unsafe, err := NewManager(m.config); err == nil {
				_ = unsafe.Close()
				t.Fatal("unsafe image catalog accepted")
			}
		})
	}
}

func TestImageCatalogInterruptedUploadRecoveryAndLegacyVMs(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	vm := createManagerVM(t, m, "alice", "legacy")
	_ = createManagerVM(t, m, "bob", "other")
	all, err := m.ListAll(t.Context())
	if err != nil || len(all) != 2 {
		t.Fatalf("admin inventory = %+v, %v", all, err)
	}
	vm.ImageID, vm.ImageName = "", ""
	data, err := json.Marshal(vm)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(m.directory(vm.ID), "vm.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	staging := filepath.Join(m.config.DataDir, "images", ".importing-"+vm.ID)
	if err := os.Mkdir(staging, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(staging, "upload.qcow2"), []byte("partial"), 0o600); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(m.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("interrupted upload remained: %v", err)
	}
	loaded, err := reopened.GetAny(t.Context(), vm.ID)
	if err != nil || loaded.ImageID != DefaultImageID || loaded.ImageName != filepath.Base(m.config.BaseImage) {
		t.Fatalf("legacy VM did not inherit default source metadata: %+v, %v", loaded, err)
	}
}
