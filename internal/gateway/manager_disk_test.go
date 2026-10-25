package gateway

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

func TestOpenDiskVirtualSizeLimit(t *testing.T) {
	t.Parallel()

	qcow2 := catalogQCOW2("virtual size fixture")
	tests := []struct {
		name     string
		data     []byte
		limit    int64
		wantSize int64
		wantErr  error
	}{
		{name: "QCOW2 at boundary", data: qcow2, limit: 2 << 20, wantSize: 2 << 20},
		{
			name: "QCOW2 over boundary", data: qcow2, limit: (2 << 20) - 1,
			wantSize: 2 << 20, wantErr: ErrDiskTooLarge,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			path := filepath.Join(t.TempDir(), "disk.img")
			if err := os.WriteFile(path, test.data, 0o600); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			size, validateErr := validateOpenDisk(file, test.limit)
			closeErr := file.Close()
			if size != test.wantSize || !errors.Is(validateErr, test.wantErr) {
				t.Fatalf("validate size = %d, %v; want %d, %v", size, validateErr, test.wantSize, test.wantErr)
			}
			if closeErr != nil {
				t.Fatal(closeErr)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(after, before) {
				t.Fatal("capacity validation modified disk contents")
			}
			if test.wantErr != nil {
				assertDiskSizeError(t, validateErr, test.wantSize, test.limit)
			}
		})
	}
}

func assertDiskSizeError(t *testing.T, err error, actual, limit int64) {
	t.Helper()
	if !strings.Contains(err.Error(), strconv.FormatInt(actual, 10)) ||
		!strings.Contains(err.Error(), strconv.FormatInt(limit, 10)) {
		t.Fatalf("capacity error omits actual or limit: %v", err)
	}
}

func TestManagerDiskLimitConfig(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value int64
		want  int64
		valid bool
	}{
		{name: "default", want: DefaultMaxDiskBytes, valid: true},
		{name: "positive", value: 1, want: 1, valid: true},
		{name: "negative", value: -1},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			config := ManagerConfig{MaxDiskBytes: test.value}
			err := normalizeManagerLimits(&config)
			if test.valid {
				if err != nil || config.MaxDiskBytes != test.want {
					t.Fatalf("normalized limit = %d, %v; want %d", config.MaxDiskBytes, err, test.want)
				}

				return
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("negative disk limit error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestManagerRejectsOversizedDefaultDisk(t *testing.T) {
	t.Parallel()

	config := managerTestConfig(t)
	config.MaxDiskBytes = 4095
	before, err := os.ReadFile(config.BaseImage)
	if err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(config)
	if manager != nil {
		_ = manager.Close()
	}
	if !errors.Is(err, ErrDiskTooLarge) || !strings.Contains(err.Error(), "4096") ||
		!strings.Contains(err.Error(), "4095") {
		t.Fatalf("oversized default image error = %v", err)
	}
	after, readErr := os.ReadFile(config.BaseImage)
	if readErr != nil || !bytes.Equal(after, before) {
		t.Fatalf("default image validation modified data: %v", readErr)
	}
}

func TestManagerImportRejectsVirtualDiskOverLimit(t *testing.T) {
	t.Parallel()

	config := managerTestConfig(t)
	config.MaxDiskBytes = (2 << 20) - 1
	manager, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})

	_, err = manager.ImportImage(t.Context(), "oversized.qcow2", bytes.NewReader(catalogQCOW2("oversized")))
	if !errors.Is(err, ErrDiskTooLarge) {
		t.Fatalf("sparse oversized image import error = %v", err)
	}
	entries, readErr := os.ReadDir(filepath.Join(config.DataDir, "images"))
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("rejected import left catalog files: %v, %v", entries, readErr)
	}
	images, listErr := manager.Images(t.Context())
	if listErr != nil || len(images) != 1 || images[0].ID != DefaultImageID {
		t.Fatalf("rejected import was published: %+v, %v", images, listErr)
	}
}

func TestManagerCreationChecksPinnedAndClonedDisk(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		mutateClone bool
	}{
		{name: "stale catalog metadata"},
		{name: "source changes during clone", mutateClone: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			manager := newTestManager(t, "graceful")
			image, err := manager.ImportImage(
				t.Context(), "selected.qcow2", bytes.NewReader(catalogQCOW2("selected")),
			)
			if err != nil {
				t.Fatal(err)
			}
			report := configureCreationDiskMutation(t, manager, image, test.mutateClone)

			_, err = manager.CreateWithProgress(
				t.Context(), "alice", "oversized", managerTestPasswordHash(), image.ID, report,
			)
			wantErr := ErrDiskTooLarge
			if test.mutateClone {
				wantErr = disk.ErrSpecMismatch
			}
			if !errors.Is(err, wantErr) {
				t.Fatalf("creation error = %v, want %v", err, wantErr)
			}
			assertRejectedCreationClean(t, manager)
		})
	}
}

func configureCreationDiskMutation(
	t *testing.T, manager *Manager, image Image, mutateClone bool,
) func(CreateProgress) {
	t.Helper()
	if mutateClone {
		manager.config.MaxDiskBytes = 2 << 20

		return mutateCatalogDiskOnCopy(t, manager, image.ID, 3<<20)
	}

	manager.config.MaxDiskBytes = (2 << 20) - 1
	manager.imageMu.Lock()
	image.VirtualSizeBytes = 1
	manager.images[image.ID] = image
	manager.imageMu.Unlock()

	return nil
}

func mutateCatalogDiskOnCopy(t *testing.T, manager *Manager, imageID string, size uint64) func(CreateProgress) {
	t.Helper()
	var once sync.Once

	return func(progress CreateProgress) {
		if progress.Phase != "copying" || progress.CopiedBytes != 0 {
			return
		}
		once.Do(func() {
			path := filepath.Join(manager.imageDirectory(imageID), "disk.qcow2")
			file, err := os.OpenFile(path, os.O_WRONLY, 0)
			if err != nil {
				t.Fatal(err)
			}
			var encoded [8]byte
			binary.BigEndian.PutUint64(encoded[:], size)
			_, writeErr := file.WriteAt(encoded[:], 24)
			closeErr := file.Close()
			if err := errors.Join(writeErr, closeErr); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func assertRejectedCreationClean(t *testing.T, manager *Manager) {
	t.Helper()
	vms, listErr := manager.List(t.Context(), "alice")
	if listErr != nil || len(vms) != 0 {
		t.Fatalf("rejected creation remained visible: %+v, %v", vms, listErr)
	}
	entries, readErr := os.ReadDir(filepath.Join(manager.config.DataDir, "vms"))
	if readErr != nil || len(entries) != 0 {
		t.Fatalf("rejected creation published state: %v, %v", entries, readErr)
	}
}

func TestManagerLoadsOversizedCatalogButBlocksCreation(t *testing.T) {
	t.Parallel()

	config := managerTestConfig(t)
	config.MaxDiskBytes = 2 << 20
	manager, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	image, err := manager.ImportImage(t.Context(), "existing.qcow2", bytes.NewReader(catalogQCOW2("existing")))
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	config.MaxDiskBytes = 1 << 20
	reopened, err := NewManager(config)
	if err != nil {
		t.Fatalf("lower policy prevented catalog management: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	images, err := reopened.Images(t.Context())
	if err != nil || len(images) != 2 {
		t.Fatalf("oversized catalog did not load: %+v, %v", images, err)
	}
	if _, err := reopened.CreateWithImage(
		t.Context(), "alice", "blocked", managerTestPasswordHash(), image.ID,
	); !errors.Is(err, ErrDiskTooLarge) {
		t.Fatalf("oversized catalog image creation error = %v", err)
	}
	if err := reopened.DeleteImage(t.Context(), image.ID); err != nil {
		t.Fatalf("oversized catalog image could not be deleted: %v", err)
	}
}

func TestManagerLoadsOversizedVMButBlocksStart(t *testing.T) {
	t.Parallel()

	config := managerTestConfig(t)
	config.MaxDiskBytes = 4096
	manager, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	vm := createManagerVM(t, manager, "alice", "existing")
	diskPath := filepath.Join(manager.directory(vm.ID), "disk.img")
	before, err := os.ReadFile(diskPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}

	config.BaseImage = ""
	config.MaxDiskBytes = 4095
	reopened, err := NewManager(config)
	if err != nil {
		t.Fatalf("lower policy prevented VM management: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
	if _, err := reopened.GetAny(t.Context(), vm.ID); err != nil {
		t.Fatalf("oversized VM did not load: %v", err)
	}
	if err := reopened.Start(t.Context(), vm.Owner, vm.ID); !errors.Is(err, ErrDiskTooLarge) {
		t.Fatalf("oversized VM start error = %v", err)
	}
	reopened.mu.Lock()
	active := reopened.active
	reopened.mu.Unlock()
	if active != 0 {
		t.Fatalf("rejected start reserved %d active slots", active)
	}
	after, err := os.ReadFile(diskPath)
	if err != nil || !bytes.Equal(after, before) {
		t.Fatalf("start validation modified VM disk: %v", err)
	}
	if _, err := os.Stat(filepath.Join(reopened.directory(vm.ID), "helper.pid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("rejected start spawned a worker: %v", err)
	}
	if err := reopened.Delete(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatalf("oversized VM could not be deleted: %v", err)
	}
}
