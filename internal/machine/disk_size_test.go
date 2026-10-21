package machine

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

func TestAddDiskWithSizePreservesQCOW2Capacity(t *testing.T) {
	t.Parallel()
	path := writeMachineQCOW2(t, 4096)
	m := newDiskTestMachine(t)
	if err := m.AddDiskWithSize(path, 8192); !errors.Is(err, disk.ErrSpecMismatch) {
		t.Fatalf("mismatched disk capacity returned %v", err)
	}
	if len(m.pci.Devices()) != 0 {
		t.Fatal("failed disk attachment registered a device")
	}
	if err := m.AddDiskWithSize(path, 4096); err != nil {
		t.Fatal(err)
	}
	devices := m.pci.Devices()
	if len(devices) != 1 {
		t.Fatalf("disk devices = %d, want 1", len(devices))
	}
	block, ok := devices[0].(*virtio.Blk)
	if !ok {
		t.Fatal("primary device is not virtio-blk")
	}
	var capacity [8]byte
	block.ReadDeviceConfig(0, capacity[:])
	if sectors := binary.LittleEndian.Uint64(capacity[:]); sectors != 8 {
		t.Fatalf("capacity = %d sectors, want the pinned virtual size of 8", sectors)
	}
}

func TestPrimaryDiskConstructorsRejectRaw(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "guest.raw")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	m := newDiskTestMachine(t)
	if err := m.AddDisk(path); err == nil {
		t.Fatal("unbounded primary constructor accepted raw")
	}
	if err := m.AddDiskWithSize(path, 4096); err == nil {
		t.Fatal("fixed-capacity primary constructor accepted raw")
	}
	if len(m.pci.Devices()) != 0 {
		t.Fatal("rejected raw disk registered a device")
	}
}

func newDiskTestMachine(t *testing.T) *Machine {
	t.Helper()
	m := &Machine{pci: pci.New(), mem: make([]byte, 4096)}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})

	return m
}

func writeMachineQCOW2(t *testing.T, size uint64) string {
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
