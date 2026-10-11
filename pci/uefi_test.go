package pci_test

import (
	"errors"
	"testing"

	"github.com/define42/gokvm/pci"
)

func TestUEFIChipsetEnumeration(t *testing.T) {
	t.Parallel()

	p := pci.New(pci.NewBridge())
	if err := p.AddUEFIChipset(); err != nil {
		t.Fatal(err)
	}
	p.AddDevices(pci.NewIDEController())
	for _, tc := range []struct {
		name     string
		slot     int
		function int
		want     uint32
	}{
		{name: "host", slot: 0, want: 0x12378086},
		{name: "ISA", slot: 1, want: 0x70008086},
		{name: "missing function", slot: 1, function: 1, want: 0xffffffff},
		{name: "PM", slot: 1, function: 3, want: 0x71138086},
		{name: "next device", slot: 2, want: 0x70108086},
		{name: "missing slot", slot: 3, want: 0xffffffff},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			p := pci.New(pci.NewBridge())
			if err := p.AddUEFIChipset(); err != nil {
				t.Fatal(err)
			}
			p.AddDevices(pci.NewIDEController())
			if got := readPCIConfig(t, p, tc.slot, tc.function, 0); got != tc.want {
				t.Fatalf("identity = %#x, want %#x", got, tc.want)
			}
		})
	}
	if got := readPCIConfig(t, p, 1, 0, 0x0c); got&0x00800000 == 0 {
		t.Fatal("ISA function does not advertise multiple functions")
	}
	if got := readPCIConfig(t, p, 1, 3, 0x40); got != 1 {
		t.Fatalf("initial PMBA = %#x, want 1", got)
	}
	writePCIConfig(t, p, 1, 3, 0x40, 0xb001)
	writePCIConfig(t, p, 1, 3, 0x80, 1)
	if got := readPCIConfig(t, p, 1, 3, 0x40); got != 0xb001 {
		t.Fatalf("PMBA write did not persist: %#x", got)
	}
	if got := readPCIConfig(t, p, 1, 0, 0x40); got != 0 {
		t.Fatalf("PMBA write leaked into ISA configuration: %#x", got)
	}
	if got := readPCIConfig(t, p, 1, 3, 0x80); got != 1 {
		t.Fatalf("PM decode-enable write did not persist: %#x", got)
	}
	functions := p.Functions()
	if len(functions) != 4 || functions[2].Slot != 1 || functions[2].Function != 3 || functions[3].Slot != 2 {
		t.Fatalf("unexpected PCI addresses: %+v", functions)
	}
	functions[0].Slot = 31
	if p.Functions()[0].Slot != 0 {
		t.Fatal("function snapshot aliases bus registry")
	}
}

func TestUEFIChipsetRejectsOccupiedSlotAtomically(t *testing.T) {
	t.Parallel()

	for _, function := range []int{0, 3} {
		t.Run(string(rune('0'+function)), func(t *testing.T) {
			t.Parallel()
			p := pci.New(pci.NewBridge())
			if err := p.AddDeviceAt(1, function, pci.NewBridge()); err != nil {
				t.Fatal(err)
			}
			if err := p.AddUEFIChipset(); !errors.Is(err, pci.ErrDeviceOccupied) {
				t.Fatalf("got %v, want ErrDeviceOccupied", err)
			}
			if len(p.Devices()) != 2 {
				t.Fatal("failed registration changed device list")
			}
		})
	}
}

func readPCIConfig(t *testing.T, p *pci.PCI, slot, function, offset int) uint32 {
	t.Helper()
	address := uint32(0x80000000 | slot<<11 | function<<8 | offset)
	if err := p.PciConfAddrOut(0xcf8, pci.NumToBytes(address)); err != nil {
		t.Fatal(err)
	}
	var data [4]byte
	if err := p.PciConfDataIn(0xcfc, data[:]); err != nil {
		t.Fatal(err)
	}

	return uint32(pci.BytesToNum(data[:]))
}

func writePCIConfig(t *testing.T, p *pci.PCI, slot, function, offset int, value uint32) {
	t.Helper()
	address := uint32(0x80000000 | slot<<11 | function<<8 | offset)
	if err := p.PciConfAddrOut(0xcf8, pci.NumToBytes(address)); err != nil {
		t.Fatal(err)
	}
	if err := p.PciConfDataOut(0xcfc, pci.NumToBytes(value)); err != nil {
		t.Fatal(err)
	}
}
