package pci

import "fmt"

// AddUEFIChipset installs the southbridge functions at the addresses expected by
// OVMF's i440FX platform. Call it before adding peripherals at slot one or later.
// The machine supplies the corresponding legacy and ACPI PM I/O handlers.
func (p *PCI) AddUEFIChipset() error {
	isa := registerDevice(&uefiChipsetFunction{header: DeviceHeader{
		VendorID: 0x8086, DeviceID: 0x7000,
		ClassCode: 0x06, Subclass: 0x01, HeaderType: 0x80,
		Command: 0x0007,
	}})
	pm := registerDevice(&uefiChipsetFunction{header: DeviceHeader{
		VendorID: 0x8086, DeviceID: 0x7113, RevisionID: 0x03,
		ClassCode: 0x06, Subclass: 0x80,
		InterruptLine: 9, InterruptPin: 1,
	}})

	p.mu.Lock()
	defer p.mu.Unlock()
	for _, function := range []int{0, 3} {
		if _, exists := p.functions[8+function]; exists {
			return fmt.Errorf("install UEFI chipset: %w: 01.%x", ErrDeviceOccupied, function)
		}
	}
	p.registerAt(1, 0, isa)
	p.registerAt(1, 3, pm)
	if p.configOverride == nil {
		p.configOverride = map[int]map[int]byte{}
	}
	// PMBA bit zero marks the register as an I/O-space base. OVMF assigns
	// the address and enables decoding through PMREGMISC (offset 0x80).
	p.configOverride[p.functions[11]] = map[int]byte{0x40: 0x01}

	return nil
}

type uefiChipsetFunction struct {
	header DeviceHeader
}

func (d *uefiChipsetFunction) GetDeviceHeader() DeviceHeader { return d.header }

func (d *uefiChipsetFunction) Read(_ uint64, _ []byte) error { return ErrIONotPermit }

func (d *uefiChipsetFunction) Write(_ uint64, _ []byte) error { return ErrIONotPermit }

func (d *uefiChipsetFunction) IOPort() uint64 { return 0 }

func (d *uefiChipsetFunction) Size() uint64 { return 0 }
