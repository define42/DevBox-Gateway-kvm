package machine

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/define42/gokvm/internal/pci"
	"github.com/define42/gokvm/internal/virtio"
)

func TestMultipleDiskInitialBARs(t *testing.T) {
	t.Parallel()

	m := &Machine{pci: pci.New(), mem: make([]byte, 4096)}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	for index, name := range []string{"boot.raw", "seed.iso", "install.iso"} {
		path := filepath.Join(t.TempDir(), name)
		if err := os.WriteFile(path, make([]byte, (index+1)*virtio.SectorSize), 0o600); err != nil {
			t.Fatal(err)
		}
		var err error
		if index == 0 {
			err = m.AddDisk(path)
		} else {
			err = m.AddReadOnlyDisk(path)
		}
		if err != nil {
			t.Fatal(err)
		}
	}

	for index, dev := range m.pci.Devices() {
		block, ok := dev.(*virtio.Blk)
		if !ok {
			t.Fatalf("device %d is not a disk", index)
		}
		base := uint64(block.GetDeviceHeader().BAR[0])
		if index == 0 && base != virtio.BlkMMIOBase {
			t.Fatalf("primary disk BAR changed to %#x", base)
		}
		// No PCI config writes are made: direct boots using pci=realloc=off
		// must decode the correct device at both ends of every initial BAR.
		for _, offset := range []uint64{0, block.MMIOSize() - 1} {
			decoded, gotOffset, ok := m.pci.LookupMMIO(base + offset)
			if !ok || decoded != block || gotOffset != offset {
				t.Fatalf("disk %d BAR %#x aliases another device at offset %#x", index, base, offset)
			}
		}
		for _, reserved := range []uint64{
			virtio.NetMMIOBase, virtio.GPUMMIOBase, virtio.InputKeyboardMMIOBase,
			virtio.InputPointerMMIOBase, virtio.SoundMMIOBase,
		} {
			if base < reserved+0x10000 && reserved < base+block.MMIOSize() {
				t.Fatalf("disk %d BAR %#x overlaps reserved device %#x", index, base, reserved)
			}
		}
		var capacity [8]byte
		block.ReadDeviceConfig(0, capacity[:])
		if binary.LittleEndian.Uint64(capacity[:]) != uint64(index+1) {
			t.Fatalf("disk %d attachment order changed", index)
		}
		if readOnly := block.DeviceFeatures()&(1<<5) != 0; readOnly != (index != 0) {
			t.Fatalf("disk %d read-only feature = %v", index, readOnly)
		}
	}
}
