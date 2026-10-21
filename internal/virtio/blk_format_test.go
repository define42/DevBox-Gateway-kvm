package virtio_test

import (
	"encoding/binary"
	"path/filepath"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

func TestBlkRejectsRawBootDisk(t *testing.T) {
	t.Parallel()

	path, _ := blockReadFixture(t)
	for _, tc := range []struct {
		name string
		open func() (*virtio.Blk, error)
	}{
		{
			name: "qcow2",
			open: func() (*virtio.Blk, error) {
				return virtio.NewBlk(path, 10, &mockInjector{}, nil)
			},
		},
		{
			name: "qcow2_with_size",
			open: func() (*virtio.Blk, error) {
				return virtio.NewBlkWithSize(path, 4*virtio.SectorSize, 10, &mockInjector{}, nil)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			device, err := tc.open()
			if device != nil {
				t.Cleanup(func() { _ = device.Close() })
			}
			if err == nil {
				t.Fatal("QCOW2 constructor accepted a raw boot disk")
			}
		})
	}
}

func TestBlkWithSizeValidatesQCOW2Capacity(t *testing.T) {
	t.Parallel()

	rawPath, _ := blockReadFixture(t)
	path := filepath.Join(t.TempDir(), "disk.qcow2")
	runQEMUImg(t, "convert", "-f", "raw", "-O", "qcow2", rawPath, path)

	const expected = 4 * virtio.SectorSize
	device, err := virtio.NewBlkWithSize(path, expected, 10, &mockInjector{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	capacity := make([]byte, 8)
	device.ReadDeviceConfig(0, capacity)
	if got := binary.LittleEndian.Uint64(capacity); got != expected/virtio.SectorSize {
		t.Errorf("capacity = %d sectors, want %d", got, expected/virtio.SectorSize)
	}
	if err := device.Close(); err != nil {
		t.Fatal(err)
	}

	device, err = virtio.NewBlkWithSize(path, expected+virtio.SectorSize, 10, &mockInjector{}, nil)
	if device != nil {
		t.Cleanup(func() { _ = device.Close() })
	}
	if err == nil {
		t.Fatal("QCOW2 constructor accepted an unexpected virtual capacity")
	}
}
