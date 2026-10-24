package pci_test

import (
	"bytes"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

func configWrite(p *pci.PCI, slot int, offset int, data []byte) {
	_ = p.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000000|slot<<11|offset&0xfc)))
	_ = p.PciConfDataOut(0xcfc+uint64(offset&3), data)
}

func configRead(p *pci.PCI, slot int, offset int, size int) []byte {
	data := make([]byte, size)
	_ = p.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000000|slot<<11|offset&0xfc)))
	_ = p.PciConfDataIn(0xcfc+uint64(offset&3), data)

	return data
}

type barMove struct {
	dev  pci.CapsAndMMIO
	base uint64
}

func TestObserveMMIOBARsFollowsGuestPlacement(t *testing.T) {
	t.Parallel()

	first := &testMMIODevice{}
	p := pci.New(first)
	var moves []barMove
	p.ObserveMMIOBARs(func(dev pci.CapsAndMMIO, base uint64) {
		moves = append(moves, barMove{dev: dev, base: base})
		// The observer runs under the bus lock; it must only record.
	})
	second := &testMMIODevice{}
	p.AddDevices(pci.NewBridge(), second)
	want := []barMove{{first, 0xd0000000}, {second, 0xd0000000}}
	if !slices.Equal(moves, want) {
		t.Fatalf("initial placements = %+v, want %+v", moves, want)
	}

	moves = nil
	// Sizing must not look like a move, nor rewriting the current base.
	configWrite(p, 0, 0x10, pci.NumToBytes(uint32(0xffffffff)))
	if size := pci.BytesToNum(configRead(p, 0, 0x10, 4)); size != uint64(pci.SizeToBits(0x1000)) {
		t.Fatalf("BAR size probe = %#x", size)
	}
	configWrite(p, 0, 0x10, pci.NumToBytes(uint32(0xd0000000)))
	configWrite(p, 0, 0x04, []byte{0x06, 0x00})
	if len(moves) != 0 {
		t.Fatalf("probe, unchanged base or command write reported moves: %+v", moves)
	}
	configWrite(p, 0, 0x10, pci.NumToBytes(uint32(0xfebf0000)))
	configWrite(p, 2, 0x12, []byte{0xbe, 0xfe}) // Partial writes move as they land.
	want = []barMove{{first, 0xfebf0000}, {second, 0xfebe0000}}
	if !slices.Equal(moves, want) {
		t.Fatalf("relocations = %+v, want %+v", moves, want)
	}
	if bar := pci.BytesToNum(configRead(p, 2, 0x10, 4)); bar != 0xfebe0000 {
		t.Fatalf("partially written BAR reads %#x", bar)
	}
	if dev, offset, ok := p.LookupMMIO(0xfebe0010); !ok || dev != second || offset != 0x10 {
		t.Fatal("observer disagrees with MMIO decoding")
	}
}

// capabilityDevice owns a 12-byte capability at 0x40 whose second word is
// live state, like MSI-X Message Control.
type capabilityDevice struct {
	testMMIODevice
	control uint16
	writes  [][]byte
}

func (d *capabilityDevice) Capabilities() []byte {
	return []byte{0x11, 0x00, 0x03, 0x00, 0x00, 0x40, 0x00, 0x00, 0x00, 0x48, 0x00, 0x00}
}

func (d *capabilityDevice) ReadCapability(off int, data []byte) {
	for i := range data {
		switch off + i {
		case 0x42:
			data[i] |= byte(d.control)
		case 0x43:
			data[i] |= byte(d.control >> 8)
		}
	}
}

func (d *capabilityDevice) WriteCapability(off int, data []byte) bool {
	if off < 0x40 || off+len(data) > 0x4c {
		return false
	}
	d.writes = append(d.writes, append([]byte{byte(off)}, data...))
	for i, b := range data {
		if off+i == 0x43 {
			d.control = uint16(b&0xc0) << 8
		}
	}

	return true
}

func TestCapabilityHandlerOwnsLiveRegisters(t *testing.T) {
	t.Parallel()

	dev := &capabilityDevice{}
	p := pci.New(dev)
	configWrite(p, 0, 0x42, []byte{0xff, 0xff})
	if got := configRead(p, 0, 0x40, 4); !bytes.Equal(got, []byte{0x11, 0x00, 0x03, 0xc0}) {
		t.Fatalf("capability read = %x, want static bytes with live control", got)
	}
	if got := configRead(p, 0, 0x43, 1); got[0] != 0xc0 {
		t.Fatalf("byte read of live control = %#x", got[0])
	}
	configWrite(p, 0, 0x42, []byte{0x00, 0x80})
	if got := configRead(p, 0, 0x42, 2); !bytes.Equal(got, []byte{0x03, 0x80}) {
		t.Fatalf("owned write was retained by the bus: %x", got)
	}
	if len(dev.writes) != 2 || !bytes.Equal(dev.writes[0], []byte{0x42, 0xff, 0xff}) {
		t.Fatalf("handler writes = %x", dev.writes)
	}
	// Bytes the function does not own keep the generic writable behaviour.
	configWrite(p, 0, 0x50, []byte{0x12, 0x34})
	if got := configRead(p, 0, 0x50, 2); !bytes.Equal(got, []byte{0x12, 0x34}) {
		t.Fatalf("unowned capability bytes = %x, want written value", got)
	}
	if got := configRead(p, 0, 0x3c, 4); got[0] != 0 || len(dev.writes) != 2 {
		t.Fatalf("header read reached the capability handler: %x", got)
	}
}
