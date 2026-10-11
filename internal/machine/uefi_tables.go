package machine

import (
	"encoding/binary"
	"errors"
	"fmt"

	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/pvh"
)

const (
	uefiACPITablesFile = "etc/acpi/tables"
	uefiACPIRSDPFile   = "etc/acpi/rsdp"
	uefiTableLoader    = "etc/table-loader"
	uefiPMBase         = 0xb000
	acpiHeaderSize     = 36
	qemuLoaderSize     = 128
)

var errUEFIACPI = errors.New("invalid UEFI ACPI configuration")

// buildUEFITables describes the devices implemented by GoKVM, rather than
// advertising QEMU devices that are not present. OVMF relocates these tables
// using its fw_cfg loader and installs them through EFI_ACPI_TABLE_PROTOCOL.
// The loader format is defined by OvmfPkg/Include/IndustryStandard/QemuLoader.h.
func buildUEFITables(cpuCount int, functions []pci.Function) (map[string][]byte, error) {
	if cpuCount < 1 || cpuCount > 255 {
		return nil, fmt.Errorf("%w: requires 1 to 255 vCPUs, got %d", errUEFIACPI, cpuCount)
	}
	routeCount := 0
	for _, function := range functions {
		if function.Slot < 0 || function.Slot > 31 || function.Function < 0 || function.Function > 7 {
			return nil, fmt.Errorf("%w: invalid PCI address %d.%d", errUEFIACPI, function.Slot, function.Function)
		}
		if function.Device == nil {
			return nil, fmt.Errorf("%w: missing device at %d.%d", errUEFIACPI, function.Slot, function.Function)
		}
		header := function.Device.GetDeviceHeader()
		if header.InterruptPin > 4 {
			return nil, fmt.Errorf("%w: invalid interrupt pin at %d.%d", errUEFIACPI, function.Slot, function.Function)
		}
		if header.InterruptPin != 0 && header.InterruptLine != 0xff {
			routeCount++
		}
	}
	if routeCount > 255 {
		return nil, fmt.Errorf("%w: too many interrupt routes: %d", errUEFIACPI, routeCount)
	}

	dsdt := acpiTable("DSDT", 2, uefiDSDT(cpuCount, functions))
	facs := make([]byte, 64)
	copy(facs, "FACS")
	binary.LittleEndian.PutUint32(facs[4:], uint32(len(facs)))
	facs[32] = 2
	fadt := uefiFADT()
	madt := uefiMADT(cpuCount)
	rsdt := acpiTable("RSDT", 1, make([]byte, 8))

	tables := make([]byte, 0, len(dsdt)+len(facs)+len(fadt)+len(madt)+len(rsdt)+64)
	appendTable := func(table []byte, alignment int) uint32 {
		padding := (-len(tables)) & (alignment - 1)
		tables = append(tables, make([]byte, padding)...)
		offset := uint32(len(tables))
		tables = append(tables, table...)

		return offset
	}
	dsdtOffset := appendTable(dsdt, 16)
	facsOffset := appendTable(facs, 64)
	fadtOffset := appendTable(fadt, 16)
	madtOffset := appendTable(madt, 16)
	rsdtOffset := appendTable(rsdt, 16)

	// Relative pointers are converted to guest addresses by the firmware.
	binary.LittleEndian.PutUint32(tables[fadtOffset+36:], facsOffset)
	binary.LittleEndian.PutUint32(tables[fadtOffset+40:], dsdtOffset)
	binary.LittleEndian.PutUint64(tables[fadtOffset+132:], uint64(facsOffset))
	binary.LittleEndian.PutUint64(tables[fadtOffset+140:], uint64(dsdtOffset))
	binary.LittleEndian.PutUint32(tables[rsdtOffset+36:], fadtOffset)
	binary.LittleEndian.PutUint32(tables[rsdtOffset+40:], madtOffset)

	rsdp := make([]byte, 20)
	copy(rsdp, "RSD PTR ")
	copy(rsdp[9:], "GOKVM ")
	binary.LittleEndian.PutUint32(rsdp[16:], rsdtOffset)

	loader := make([]byte, 0, 12*qemuLoaderSize)
	loader = append(loader, uefiLoaderAllocate(uefiACPITablesFile, 64, 1)...)
	loader = append(loader, uefiLoaderAllocate(uefiACPIRSDPFile, 16, 2)...)
	for _, pointer := range []struct {
		offset uint32
		size   byte
	}{
		{offset: fadtOffset + 36, size: 4},
		{offset: fadtOffset + 40, size: 4},
		{offset: fadtOffset + 132, size: 8},
		{offset: fadtOffset + 140, size: 8},
		{offset: rsdtOffset + 36, size: 4},
		{offset: rsdtOffset + 40, size: 4},
	} {
		loader = append(loader, uefiLoaderPointer(uefiACPITablesFile, uefiACPITablesFile, pointer.offset, pointer.size)...)
	}
	loader = append(loader, uefiLoaderPointer(uefiACPIRSDPFile, uefiACPITablesFile, 16, 4)...)
	for _, table := range []struct {
		offset uint32
		size   int
	}{
		{offset: fadtOffset, size: len(fadt)},
		{offset: rsdtOffset, size: len(rsdt)},
	} {
		// ADD_CHECKSUM adds to the existing byte, so clear it before relocation.
		tables[table.offset+9] = 0
		loader = append(loader, uefiLoaderChecksum(
			uefiACPITablesFile,
			table.offset+9,
			table.offset,
			uint32(table.size),
		)...)
	}
	loader = append(loader, uefiLoaderChecksum(uefiACPIRSDPFile, 8, 0, uint32(len(rsdp)))...)

	return map[string][]byte{
		uefiACPITablesFile: tables,
		uefiACPIRSDPFile:   rsdp,
		uefiTableLoader:    loader,
	}, nil
}

func acpiTable(signature string, revision byte, body []byte) []byte {
	table := make([]byte, acpiHeaderSize+len(body))
	copy(table, signature)
	binary.LittleEndian.PutUint32(table[4:], uint32(len(table)))
	table[8] = revision
	copy(table[10:], "GOKVM ")
	copy(table[16:], "GOKVMPC ")
	binary.LittleEndian.PutUint32(table[24:], 1)
	copy(table[28:], "GOKV")
	binary.LittleEndian.PutUint32(table[32:], 1)
	copy(table[acpiHeaderSize:], body)
	var sum byte
	for _, value := range table {
		sum += value
	}
	table[9] = -sum

	return table
}

func uefiFADT() []byte {
	table := acpiTable("FACP", 3, make([]byte, 244-acpiHeaderSize))
	binary.LittleEndian.PutUint16(table[46:], 9) // SCI, shared with PCI INTx through GSI 9.
	binary.LittleEndian.PutUint32(table[48:], 0xb2)
	table[52], table[53] = 0xf1, 0xf0 // PIIX4 ACPI enable/disable commands.
	binary.LittleEndian.PutUint32(table[56:], uefiPMBase)
	binary.LittleEndian.PutUint32(table[64:], uefiPMBase+4)
	binary.LittleEndian.PutUint32(table[76:], uefiPMBase+8)
	table[88], table[89], table[91] = 4, 2, 4
	binary.LittleEndian.PutUint16(table[96:], 0xffff) // No C2/C3 states.
	binary.LittleEndian.PutUint16(table[98:], 0xffff)
	binary.LittleEndian.PutUint16(table[109:], 3) // Legacy devices and 8042 controller.
	// WBINVD, C1, fixed power button, no sleep button, 32-bit timer, reset register.
	binary.LittleEndian.PutUint32(table[112:], 1|1<<2|1<<5|1<<8|1<<10)
	uefiGAS(table[116:], 0xcf9, 8, 1)
	table[128] = 6
	uefiGAS(table[148:], uefiPMBase, 32, 2)
	uefiGAS(table[172:], uefiPMBase+4, 16, 2)
	uefiGAS(table[208:], uefiPMBase+8, 32, 3)

	return table
}

func uefiGAS(data []byte, port uint64, bits, access byte) {
	data[0], data[1], data[3] = 1, bits, access // System I/O address space.
	binary.LittleEndian.PutUint64(data[4:], port)
}

func uefiMADT(cpuCount int) []byte {
	body := []byte{0, 0, 0, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(body, 0xfee00000)
	binary.LittleEndian.PutUint32(body[4:], 1) // Dual 8259 PIC present.
	for cpu := range cpuCount {
		body = append(body, 0, 8, byte(cpu), byte(cpu), 1, 0, 0, 0)
	}
	// One KVM I/O APIC, GSI base 0. Its ID follows all local APIC IDs.
	body = append(body, 1, 12, byte(cpuCount), 0, 0, 0, 0xc0, 0xfe, 0, 0, 0, 0)
	// The PC PIT IRQ0 is routed to IOAPIC input 2 by KVM.
	body = append(body, 2, 10, 0, 0, 2, 0, 0, 0, 0, 0)
	// Firmware and the OS use LINT1 for the local NMI input.
	body = append(body, 4, 6, 0xff, 0, 0, 1)

	return acpiTable("APIC", 1, body)
}

func uefiDSDT(cpuCount int, functions []pci.Function) []byte {
	routes := make([][]byte, 0, len(functions))
	for _, function := range functions {
		header := function.Device.GetDeviceHeader()
		if header.InterruptPin == 0 || header.InterruptLine == 0xff {
			continue
		}
		address := uint32(function.Slot)<<16 | uint32(function.Function)
		routes = append(routes, amlPackage([][]byte{
			amlInteger(uint64(address)),
			amlInteger(uint64(header.InterruptPin - 1)),
			{0}, // No interrupt-link device: the last value is a fixed GSI.
			amlInteger(uint64(header.InterruptLine)),
		}))
	}

	resources := make([]byte, 0, 64)
	resources = append(resources, amlWordResource(2, 0, 0, 255)...)
	resources = append(resources, amlWordResource(1, 3, 0, 0xcf7)...)
	resources = append(resources, amlWordResource(1, 3, 0xd00, 0xffff)...)
	resources = append(resources, amlMemoryResource(uint32(guestmem.MMIOStart), uint32(pvh.KVMTSSStart-1))...)
	resources = append(resources, 0x79, 0) // EndTag; resource checksum is optional.
	pciBody := []byte("PCI0")
	pciBody = append(pciBody, amlName("_HID", amlString("PNP0A03"))...)
	pciBody = append(pciBody, amlName("_UID", amlInteger(0))...)
	pciBody = append(pciBody, amlName("_BBN", amlInteger(0))...)
	pciBody = append(pciBody, amlName("_SEG", amlInteger(0))...)
	pciBody = append(pciBody, amlName("_CRS", amlBuffer(resources))...)
	pciBody = append(pciBody, amlName("_PRT", amlPackage(routes))...)

	systemBus := []byte("\\_SB_")
	systemBus = append(systemBus, amlObject([]byte{0x5b, 0x82}, pciBody)...)
	for cpu := range cpuCount {
		body := []byte(fmt.Sprintf("C%03X", cpu))
		body = append(body, amlName("_HID", amlString("ACPI0007"))...)
		body = append(body, amlName("_UID", amlInteger(uint64(cpu)))...)
		systemBus = append(systemBus, amlObject([]byte{0x5b, 0x82}, body)...)
	}
	// PIIX4 uses SLP_TYP=0 for S5, as does OVMF ResetShutdown.
	body := amlName("_S5_", amlPackage([][]byte{{0}, {0}, {0}, {0}}))

	return append(body, amlObject([]byte{0x10}, systemBus)...)
}

// The small AML helpers emit only the constants, packages and namespace objects
// needed by this fixed platform. PkgLength includes its own encoded byte count.
func amlObject(opcode, body []byte) []byte {
	size := len(body)
	width := 1
	for size+width >= 1<<(4+8*(width-1)) {
		if width == 1 && size+width < 64 {
			break
		}
		width++
	}
	length := size + width
	encoded := make([]byte, width)
	if width == 1 {
		encoded[0] = byte(length)
	} else {
		encoded[0] = byte((width-1)<<6) | byte(length&0x0f)
		for i := 1; i < width; i++ {
			encoded[i] = byte(length >> (4 + (i-1)*8))
		}
	}
	result := append([]byte{}, opcode...)
	result = append(result, encoded...)

	return append(result, body...)
}

func amlName(name string, value []byte) []byte {
	result := append([]byte{0x08}, name...)

	return append(result, value...)
}

func amlInteger(value uint64) []byte {
	if value < 2 {
		return []byte{byte(value)}
	}
	if value <= 0xff {
		return []byte{0x0a, byte(value)}
	}
	if value <= 0xffff {
		return binary.LittleEndian.AppendUint16([]byte{0x0b}, uint16(value))
	}
	if value <= 0xffffffff {
		return binary.LittleEndian.AppendUint32([]byte{0x0c}, uint32(value))
	}

	return binary.LittleEndian.AppendUint64([]byte{0x0e}, value)
}

func amlString(value string) []byte {
	return append(append([]byte{0x0d}, value...), 0)
}

func amlPackage(elements [][]byte) []byte {
	body := []byte{byte(len(elements))}
	for _, element := range elements {
		body = append(body, element...)
	}

	return amlObject([]byte{0x12}, body)
}

func amlBuffer(data []byte) []byte {
	body := append(amlInteger(uint64(len(data))), data...)

	return amlObject([]byte{0x11}, body)
}

func amlWordResource(kind, flags byte, minimum, maximum uint16) []byte {
	data := []byte{0x88, 13, 0, kind, 0x0c, flags, 0, 0}
	data = binary.LittleEndian.AppendUint16(data, minimum)
	data = binary.LittleEndian.AppendUint16(data, maximum)
	data = binary.LittleEndian.AppendUint16(data, 0)

	return binary.LittleEndian.AppendUint16(data, maximum-minimum+1)
}

func amlMemoryResource(minimum, maximum uint32) []byte {
	data := []byte{0x87, 23, 0, 0, 0x0c, 1, 0, 0, 0, 0}
	data = binary.LittleEndian.AppendUint32(data, minimum)
	data = binary.LittleEndian.AppendUint32(data, maximum)
	data = binary.LittleEndian.AppendUint32(data, 0)

	return binary.LittleEndian.AppendUint32(data, maximum-minimum+1)
}

func uefiLoaderAllocate(file string, alignment uint32, zone byte) []byte {
	command := make([]byte, qemuLoaderSize)
	binary.LittleEndian.PutUint32(command, 1)
	copy(command[4:60], file)
	binary.LittleEndian.PutUint32(command[60:], alignment)
	command[64] = zone

	return command
}

func uefiLoaderPointer(pointerFile, pointeeFile string, offset uint32, size byte) []byte {
	command := make([]byte, qemuLoaderSize)
	binary.LittleEndian.PutUint32(command, 2)
	copy(command[4:60], pointerFile)
	copy(command[60:116], pointeeFile)
	binary.LittleEndian.PutUint32(command[116:], offset)
	command[120] = size

	return command
}

func uefiLoaderChecksum(file string, result, start, size uint32) []byte {
	command := make([]byte, qemuLoaderSize)
	binary.LittleEndian.PutUint32(command, 3)
	copy(command[4:60], file)
	binary.LittleEndian.PutUint32(command[60:], result)
	binary.LittleEndian.PutUint32(command[64:], start)
	binary.LittleEndian.PutUint32(command[68:], size)

	return command
}
