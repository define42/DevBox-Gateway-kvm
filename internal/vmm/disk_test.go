package vmm

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

func TestResolveDiskSizePinsQCOW2Capacity(t *testing.T) {
	t.Parallel()
	path := writeQCOW2Disk(t, 4096)
	conf := Config{Disk: path}
	if err := conf.ResolveDiskSize(); err != nil {
		t.Fatal(err)
	}
	if conf.DiskSizeBytes != 4096 {
		t.Fatalf("resolved capacity = %d", conf.DiskSizeBytes)
	}
	if err := conf.ResolveDiskSize(); err != nil {
		t.Fatalf("unchanged capacity failed validation: %v", err)
	}
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	var changedSize [8]byte
	binary.BigEndian.PutUint64(changedSize[:], 8192)
	if _, err := file.WriteAt(changedSize[:], 24); err != nil {
		_ = file.Close()
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := conf.ResolveDiskSize(); !errors.Is(err, disk.ErrSpecMismatch) {
		t.Fatalf("capacity change returned %v, want mismatch", err)
	}
	if conf.DiskSizeBytes != 4096 {
		t.Fatalf("changed metadata replaced the pinned capacity with %d", conf.DiskSizeBytes)
	}
}

func TestRawPrimaryDiskRejectedBeforeOpeningKVM(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "guest.raw")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, size := range []int64{0, 4096} {
		vm := New(Config{Dev: "/nonexistent-kvm", Disk: path, DiskSizeBytes: size})
		err := vm.Init()
		if err == nil || errors.Is(err, os.ErrNotExist) {
			t.Errorf("raw primary disk reached KVM: %v", err)
		}
		if vm.Machine != nil {
			t.Fatal("raw primary disk initialized a machine")
		}
	}
}

func TestInvalidDiskSizeFailsBeforeOpeningKVM(t *testing.T) {
	t.Parallel()
	path := writeQCOW2Disk(t, 4096)
	for _, conf := range []Config{
		{DiskSizeBytes: 4096},
		{Disk: path, DiskSizeBytes: -1},
		{Disk: path, DiskSizeBytes: 8192},
	} {
		conf.Dev = "/nonexistent-kvm"
		vm := New(conf)
		err := vm.Init()
		if !errors.Is(err, errDiskConfig) && !errors.Is(err, disk.ErrSpecMismatch) {
			t.Errorf("capacity=%d: got %v, want disk capacity error", conf.DiskSizeBytes, err)
		}
	}
}

func writeQCOW2Disk(t *testing.T, size uint64) string {
	t.Helper()
	const clusterSize = 512
	data := make([]byte, 4*clusterSize)
	copy(data, "QFI\xfb")
	binary.BigEndian.PutUint32(data[4:8], 3)
	binary.BigEndian.PutUint32(data[20:24], 9)
	binary.BigEndian.PutUint64(data[24:32], size)
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
