package main

import (
	"context"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
	"github.com/define42/DevBox-Gateway-kvm/internal/machine"
	"github.com/define42/DevBox-Gateway-kvm/internal/vmm"
)

func TestBootResetPreservesDiskSizeAndGuestData(t *testing.T) {
	t.Parallel()
	path := writeBootQCOW2(t)
	calls := 0
	conf := vmm.Config{Disk: path, UEFI: "firmware"}
	err := runBootWithGuest(t.Context(), conf, func(_ context.Context, conf vmm.Config) error {
		calls++
		if conf.DiskSizeBytes != 4096 {
			t.Fatalf("boot %d received disk capacity %d", calls, conf.DiskSizeBytes)
		}
		// Init validates the same pinned size on every recreated VMM. Guest
		// sector zero is QCOW2 data, independent of the container's header.
		if err := conf.ResolveDiskSize(); err != nil {
			return err
		}
		image, err := disk.OpenQCOW2WithSize(conf.Disk, conf.DiskSizeBytes)
		if err != nil {
			return err
		}
		if calls == 1 {
			_, writeErr := image.WriteAt([]byte("QFI\xfb"), 0)

			return errors.Join(machine.ErrUEFIReset, writeErr, image.Close())
		}
		var magic [4]byte
		_, readErr := image.ReadAt(magic[:], 0)
		if string(magic[:]) != "QFI\xfb" {
			t.Errorf("guest sector zero was not preserved: %x", magic)
		}

		return errors.Join(readErr, image.Close())
	})
	if err != nil {
		t.Fatal(err)
	}
	if calls != 2 {
		t.Fatalf("boot calls = %d, want initial boot and reset", calls)
	}
}

func TestBootResetRejectsChangedDiskCapacity(t *testing.T) {
	t.Parallel()
	path := writeBootQCOW2(t)
	calls := 0
	conf := vmm.Config{Disk: path, UEFI: "firmware"}
	err := runBootWithGuest(t.Context(), conf, func(_ context.Context, conf vmm.Config) error {
		calls++
		if conf.DiskSizeBytes != 4096 {
			t.Fatalf("boot %d replaced the pinned capacity with %d", calls, conf.DiskSizeBytes)
		}
		if err := conf.ResolveDiskSize(); err != nil {
			return err
		}
		if calls > 1 {
			t.Fatal("changed disk capacity passed reset validation")
		}
		// Simulate external container-header modification between boots.
		// This must not be accepted as a new standalone capacity on reset.
		file, err := os.OpenFile(path, os.O_WRONLY, 0)
		if err != nil {
			return err
		}
		var changedSize [8]byte
		binary.BigEndian.PutUint64(changedSize[:], 8192)
		_, writeErr := file.WriteAt(changedSize[:], 24)

		return errors.Join(machine.ErrUEFIReset, writeErr, file.Close())
	})
	if !errors.Is(err, disk.ErrSpecMismatch) {
		t.Fatalf("reset after capacity change returned %v", err)
	}
	if calls != 2 {
		t.Fatalf("boot calls = %d, want initial boot and rejected reset", calls)
	}
}

func TestBootRejectsDiskSizeMismatchBeforeStartingGuest(t *testing.T) {
	t.Parallel()
	path := writeBootQCOW2(t)
	conf := vmm.Config{Disk: path, DiskSizeBytes: 8192}
	err := runBootWithGuest(t.Context(), conf, func(context.Context, vmm.Config) error {
		t.Fatal("guest started despite a mismatched disk capacity")

		return nil
	})
	if !errors.Is(err, disk.ErrSpecMismatch) {
		t.Fatalf("boot error = %v, want capacity mismatch", err)
	}
}

func TestBootCLIPropagatesDiskSize(t *testing.T) {
	t.Parallel()
	path := writeBootQCOW2(t)
	missingKVM := filepath.Join(t.TempDir(), "missing-kvm")
	args := []string{
		"devbox-kvm", "boot", "-d", path, "-disk-size-bytes=4096", "-D", missingKVM,
	}
	err := run(t.Context(), args)
	var pathErr *os.PathError
	if !errors.As(err, &pathErr) || pathErr.Path != missingKVM || !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("valid QCOW2 disk did not reach KVM initialization: %v", err)
	}
	// Repeating the size flag overrides it in the normal CLI parser. The
	// resulting capacity check must run before the same missing KVM path.
	args = append(args, "-disk-size-bytes=8192")
	if err := run(t.Context(), args); !errors.Is(err, disk.ErrSpecMismatch) {
		t.Fatalf("CLI capacity mismatch returned %v", err)
	}
}

func writeBootQCOW2(t *testing.T) string {
	t.Helper()
	const clusterSize = 512
	data := make([]byte, 4*clusterSize)
	copy(data, "QFI\xfb")
	binary.BigEndian.PutUint32(data[4:8], 3)
	binary.BigEndian.PutUint32(data[20:24], 9)
	binary.BigEndian.PutUint64(data[24:32], 4096)
	binary.BigEndian.PutUint32(data[36:40], 16)
	binary.BigEndian.PutUint64(data[40:48], 3*clusterSize)
	binary.BigEndian.PutUint64(data[48:56], clusterSize)
	binary.BigEndian.PutUint32(data[56:60], 1)
	binary.BigEndian.PutUint32(data[96:100], 4)
	binary.BigEndian.PutUint32(data[100:104], 104)
	binary.BigEndian.PutUint64(data[clusterSize:], 2*clusterSize)
	for cluster := range 4 {
		binary.BigEndian.PutUint16(data[2*clusterSize+cluster*2:], 1)
	}
	path := filepath.Join(t.TempDir(), "guest.qcow2")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
