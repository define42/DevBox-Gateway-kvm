package gateway

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

func TestManagerPreservesQCOW2CapacityAcrossReload(t *testing.T) {
	t.Parallel()

	for _, source := range []string{"default", "catalog"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			manager := newTestManager(t, "graceful")
			vm := createQCOW2CapacityVM(t, manager, source)
			want := int64(4096)
			if source == "catalog" {
				want = 2 << 20
			}
			if vm.DiskSizeBytes != want {
				t.Fatalf("created disk capacity = %d, want %d", vm.DiskSizeBytes, want)
			}
			image, err := disk.OpenQCOW2WithSize(filepath.Join(manager.directory(vm.ID), "disk.img"), want)
			if err != nil {
				t.Fatal(err)
			}
			// Guest-visible sector zero is data inside QCOW2, not its host header.
			_, writeErr := image.WriteAt([]byte("QFI\xfb"), 0)
			if err := errors.Join(writeErr, image.Close(), manager.Close()); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewManager(manager.config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			reopened.cgroups = testCgroupFactory{}
			reopened.readMemory, reopened.readResident = manager.readMemory, manager.readResident
			loaded, err := reopened.GetAny(t.Context(), vm.ID)
			if err != nil || loaded.DiskSizeBytes != want {
				t.Fatalf("loaded disk capacity = %d, %v; want %d", loaded.DiskSizeBytes, err, want)
			}
			if err := reopened.Start(t.Context(), vm.Owner, vm.ID); err != nil {
				t.Fatal(err)
			}
			args, err := os.ReadFile(filepath.Join(reopened.directory(vm.ID), "helper.disk-policy"))
			if err != nil || string(args) != strconv.FormatInt(want, 10) {
				t.Fatalf("worker disk capacity = %q, %v; want %d", args, err, want)
			}
		})
	}
}

func TestManagerRejectsChangedQCOW2Disk(t *testing.T) {
	t.Parallel()

	for _, change := range []string{"capacity", "raw replacement"} {
		t.Run(change, func(t *testing.T) {
			t.Parallel()
			manager := newTestManager(t, "graceful")
			vm := createQCOW2CapacityVM(t, manager, "catalog")
			path := filepath.Join(manager.directory(vm.ID), "disk.img")
			if change == "capacity" {
				var capacity [8]byte
				// Still below the global limit; the creation-time capacity must win.
				binary.BigEndian.PutUint64(capacity[:], 3<<20)
				writeDiskHeaderBytes(t, path, capacity[:], 24)
			} else if err := os.WriteFile(path, make([]byte, vm.DiskSizeBytes), 0o600); err != nil {
				t.Fatal(err)
			}
			err := manager.Start(t.Context(), vm.Owner, vm.ID)
			if !errors.Is(err, ErrDiskChanged) || (change == "capacity" && !errors.Is(err, disk.ErrSpecMismatch)) {
				t.Fatalf("changed disk start error = %v", err)
			}
			if _, err := os.Stat(filepath.Join(manager.directory(vm.ID), "helper.pid")); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("rejected start spawned a worker: %v", err)
			}
		})
	}
}

func TestManagerRejectsRawBaseImage(t *testing.T) {
	t.Parallel()
	config := managerTestConfig(t, "graceful")
	if err := os.WriteFile(config.BaseImage, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	manager, err := NewManager(config)
	if manager != nil {
		_ = manager.Close()
	}
	if err == nil {
		t.Fatal("raw base image was accepted as a QCOW2 template")
	}
}

func TestManagerRejectsRawTemplateAtCreation(t *testing.T) {
	t.Parallel()
	manager := newTestManager(t, "graceful")
	if err := os.WriteFile(manager.config.BaseImage, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := manager.Create(t.Context(), "alice", "raw", managerTestPasswordHash()); err == nil {
		t.Fatal("raw replacement template was accepted at creation")
	}
	assertRejectedCreationClean(t, manager)
}

func TestManagerRejectsInvalidSavedDiskCapacity(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name  string
		key   string
		value any
	}{
		{name: "null"},
		{name: "case-insensitive zero", key: "DISKSIZEBYTES", value: 0},
		{name: "zero", value: 0},
		{name: "negative", value: -1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manager := newTestManager(t, "graceful")
			vm := createManagerVM(t, manager, "alice", "saved")
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(manager.directory(vm.ID), "vm.json")
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var record map[string]any
			if err := json.Unmarshal(data, &record); err != nil {
				t.Fatal(err)
			}
			key := test.key
			if key == "" {
				key = "diskSizeBytes"
			}
			delete(record, "diskSizeBytes")
			record[key] = test.value
			data, err = json.Marshal(record)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewManager(manager.config)
			if reopened != nil {
				_ = reopened.Close()
			}
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid saved disk capacity load error = %v", err)
			}
		})
	}
}

func createQCOW2CapacityVM(t *testing.T, manager *Manager, source string) VM {
	t.Helper()
	if source == "default" {
		return createManagerVM(t, manager, "alice", "pinned")
	}
	image, err := manager.ImportImage(t.Context(), "pinned.qcow2", bytes.NewReader(catalogQCOW2("pinned")))
	if err != nil {
		t.Fatal(err)
	}
	vm, err := manager.CreateWithImage(t.Context(), "alice", "pinned", managerTestPasswordHash(), image.ID)
	if err != nil {
		t.Fatal(err)
	}

	return vm
}

func writeDiskHeaderBytes(t *testing.T, path string, data []byte, offset int64) {
	t.Helper()
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	_, writeErr := file.WriteAt(data, offset)
	if err := errors.Join(writeErr, file.Close()); err != nil {
		t.Fatal(err)
	}
}

func TestManagerMigratesLegacyDiskCapacity(t *testing.T) {
	t.Parallel()
	for _, source := range []string{"default", "catalog"} {
		t.Run(source, func(t *testing.T) {
			t.Parallel()
			manager := newTestManager(t, "graceful")
			vm := createQCOW2CapacityVM(t, manager, source)
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			metadata := removeSavedDiskCapacity(t, manager, vm)
			path := filepath.Join(manager.directory(vm.ID), "disk.img")
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			reopened, err := NewManager(manager.config)
			if err != nil {
				t.Fatalf("load legacy VM: %v", err)
			}
			t.Cleanup(func() { _ = reopened.Close() })
			loaded, err := reopened.GetAny(t.Context(), vm.ID)
			if err != nil || loaded.DiskSizeBytes != vm.DiskSizeBytes {
				t.Fatalf("migrated VM capacity = %d, %v; want %d", loaded.DiskSizeBytes, err, vm.DiskSizeBytes)
			}
			saved, err := os.ReadFile(metadata)
			if err != nil {
				t.Fatal(err)
			}
			var persisted VM
			if err := json.Unmarshal(saved, &persisted); err != nil {
				t.Fatal(err)
			}
			if persisted.DiskSizeBytes != vm.DiskSizeBytes || persisted.ID != vm.ID || persisted.Owner != vm.Owner {
				t.Fatalf("migration was not persisted correctly: %+v", persisted)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("migration changed disk contents: %v", err)
			}
			// Once persisted, the capacity must survive removal of the template.
			if source == "catalog" {
				if err := reopened.DeleteImage(t.Context(), vm.ImageID); err != nil {
					t.Fatal(err)
				}
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			config := manager.config
			config.BaseImage = ""
			again, err := NewManager(config)
			if err != nil {
				t.Fatalf("reload migrated VM without source template: %v", err)
			}
			defer again.Close()
			// The migrated capacity must still reject later disk-header changes.
			var changed [8]byte
			binary.BigEndian.PutUint64(changed[:], uint64(vm.DiskSizeBytes+512))
			writeDiskHeaderBytes(t, path, changed[:], 24)
			if err := again.Start(t.Context(), vm.Owner, vm.ID); !errors.Is(err, ErrDiskChanged) {
				t.Fatalf("migrated VM lost pinned capacity: %v", err)
			}
		})
	}
}

func TestManagerRejectsUnsafeLegacyDiskCapacity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		want error
	}{
		{name: "missing template", want: ErrInvalid},
		{name: "changed capacity", want: ErrDiskChanged},
		{name: "raw disk", want: ErrDiskChanged},
		{name: "malformed layout", want: ErrDiskChanged},
		{name: "disk symlink", want: syscall.ELOOP},
		{name: "public disk", want: ErrInvalid},
		{name: "metadata write", want: os.ErrPermission},
		{name: "missing disk", want: os.ErrNotExist},
		{name: "size limit", want: ErrDiskTooLarge},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			manager := newTestManager(t, "graceful")
			vm := createQCOW2CapacityVM(t, manager, "catalog")
			if test.name == "missing template" {
				if err := manager.DeleteImage(t.Context(), vm.ImageID); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.Close(); err != nil {
				t.Fatal(err)
			}
			metadata := removeSavedDiskCapacity(t, manager, vm)
			before, err := os.ReadFile(metadata)
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(manager.directory(vm.ID), "disk.img")
			config := manager.config
			switch test.name {
			case "changed capacity":
				var changed [8]byte
				binary.BigEndian.PutUint64(changed[:], uint64(vm.DiskSizeBytes+512))
				writeDiskHeaderBytes(t, path, changed[:], 24)
			case "raw disk":
				if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
					t.Fatal(err)
				}
			case "malformed layout":
				var offset [8]byte
				binary.BigEndian.PutUint64(offset[:], 1<<30)
				writeDiskHeaderBytes(t, path, offset[:], 40)
			case "disk symlink":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(config.BaseImage, path); err != nil {
					t.Fatal(err)
				}
			case "public disk":
				if err := os.Chmod(path, 0o644); err != nil {
					t.Fatal(err)
				}
			case "metadata write":
				dir := manager.directory(vm.ID)
				if err := os.Chmod(dir, 0o500); err != nil {
					t.Fatal(err)
				}
				t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
			case "missing disk":
				if err := os.Remove(path); err != nil {
					t.Fatal(err)
				}
			case "size limit":
				config.MaxDiskBytes = vm.DiskSizeBytes - 1
			}
			reopened, err := NewManager(config)
			if reopened != nil {
				_ = reopened.Close()
			}
			if !errors.Is(err, test.want) {
				t.Fatalf("legacy migration error = %v, want %v", err, test.want)
			}
			after, err := os.ReadFile(metadata)
			if err != nil || !bytes.Equal(before, after) {
				t.Fatalf("failed migration changed saved metadata: %v", err)
			}
		})
	}
}

func removeSavedDiskCapacity(t *testing.T, manager *Manager, vm VM) string {
	t.Helper()
	path := filepath.Join(manager.directory(vm.ID), "vm.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var record map[string]json.RawMessage
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatal(err)
	}
	delete(record, "diskSizeBytes")
	data, err = json.Marshal(record)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
