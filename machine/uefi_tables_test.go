package machine

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"github.com/define42/gokvm/internal/guestmem"
	"github.com/define42/gokvm/pci"
	"github.com/define42/gokvm/pvh"
)

type uefiACPITestDevice struct {
	pci.Device
	header pci.DeviceHeader
}

func (d uefiACPITestDevice) GetDeviceHeader() pci.DeviceHeader { return d.header }

func TestUEFITablesFirmwareRelocation(t *testing.T) {
	t.Parallel()

	functions := []pci.Function{
		{Slot: 2, Function: 0, Device: uefiACPITestDevice{header: pci.DeviceHeader{InterruptPin: 1, InterruptLine: 10}}},
		{Slot: 5, Function: 3, Device: uefiACPITestDevice{header: pci.DeviceHeader{InterruptPin: 2, InterruptLine: 11}}},
	}
	files, err := buildUEFITables(4, functions)
	if err != nil {
		t.Fatal(err)
	}
	const tableAddress = uint64(0x3ff00000)
	bases := map[string]uint64{uefiACPITablesFile: tableAddress, uefiACPIRSDPFile: 0xf0000}
	loaded := applyUEFITestLoader(t, files, bases)
	rsdp := loaded[uefiACPIRSDPFile]
	assertACPIChecksum(t, rsdp)
	if string(rsdp[:8]) != "RSD PTR " {
		t.Fatalf("invalid RSDP signature %q", rsdp[:8])
	}
	tables := loaded[uefiACPITablesFile]
	rsdt := lookupUEFITestTable(t, tables, uint64(binary.LittleEndian.Uint32(rsdp[16:])), "RSDT")
	fadt := lookupUEFITestTable(t, tables, uint64(binary.LittleEndian.Uint32(rsdt[36:])), "FACP")
	madt := lookupUEFITestTable(t, tables, uint64(binary.LittleEndian.Uint32(rsdt[40:])), "APIC")
	dsdtAddr := uint64(binary.LittleEndian.Uint32(fadt[40:]))
	if got := binary.LittleEndian.Uint64(fadt[140:]); got != dsdtAddr {
		t.Fatalf("FADT DSDT pointers disagree: %#x / %#x", dsdtAddr, got)
	}
	dsdt := lookupUEFITestTable(t, tables, dsdtAddr, "DSDT")
	facsAddr := uint64(binary.LittleEndian.Uint32(fadt[36:]))
	if got := binary.LittleEndian.Uint64(fadt[132:]); got != facsAddr {
		t.Fatalf("FADT FACS pointers disagree: %#x / %#x", facsAddr, got)
	}
	facs := tables[facsAddr-tableAddress:]
	if string(facs[:4]) != "FACS" || binary.LittleEndian.Uint32(facs[4:]) != 64 || facsAddr%64 != 0 {
		t.Fatalf("invalid FACS at %#x", facsAddr)
	}
	for _, tc := range []struct {
		name   string
		offset int
		want   uint32
	}{
		{name: "SMI command", offset: 48, want: 0xb2},
		{name: "PM1 event", offset: 56, want: 0xb000},
		{name: "PM1 control", offset: 64, want: 0xb004},
		{name: "PM timer", offset: 76, want: 0xb008},
	} {
		if got := binary.LittleEndian.Uint32(fadt[tc.offset:]); got != tc.want {
			t.Errorf("%s address = %#x, want %#x", tc.name, got, tc.want)
		}
	}
	if fadt[52] != 0xf1 || fadt[53] != 0xf0 {
		t.Errorf("ACPI mode commands = %x, want f1f0", fadt[52:54])
	}
	if got := binary.LittleEndian.Uint16(fadt[46:]); got != 9 {
		t.Errorf("SCI = %d, want 9", got)
	}
	assertUEFITestMADT(t, madt)

	// These artifacts can be disassembled independently with ACPICA's iasl.
	for name, table := range map[string][]byte{"dsdt.aml": dsdt, "fadt.dat": fadt, "madt.dat": madt} {
		if err := os.WriteFile(filepath.Join(t.ArtifactDir(), name), table, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestUEFITablesRejectInvalidTopology(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		cpus      int
		functions []pci.Function
	}{
		{name: "no CPUs", cpus: 0},
		{name: "APIC ID overflow", cpus: 256},
		{name: "missing device", cpus: 1, functions: []pci.Function{{Slot: 1}}},
		{name: "invalid slot", cpus: 1, functions: []pci.Function{{Slot: 32, Device: pci.NewBridge()}}},
		{name: "invalid function", cpus: 1, functions: []pci.Function{{Slot: 1, Function: 8, Device: pci.NewBridge()}}},
		{
			name: "invalid pin", cpus: 1,
			functions: []pci.Function{{Device: uefiACPITestDevice{header: pci.DeviceHeader{InterruptPin: 5}}}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := buildUEFITables(tc.cpus, tc.functions); err == nil {
				t.Fatal("expected invalid topology error")
			}
		})
	}
}

func TestUEFITablesPCIWindowExcludesKVMReservedMemory(t *testing.T) {
	t.Parallel()

	dsdt := uefiDSDT(1, nil)
	// A DWORD address descriptor has a 23-byte body and resource type zero
	// for memory. Inspect its published limits independently of the encoder.
	offset := bytes.Index(dsdt, []byte{0x87, 23, 0, 0})
	if offset < 0 || len(dsdt)-offset < 26 {
		t.Fatal("missing complete PCI memory resource descriptor")
	}
	resource := dsdt[offset : offset+26]
	minimum := binary.LittleEndian.Uint32(resource[10:])
	maximum := binary.LittleEndian.Uint32(resource[14:])
	length := binary.LittleEndian.Uint32(resource[22:])
	if minimum != uint32(guestmem.MMIOStart) || maximum != uint32(pvh.KVMTSSStart-1) {
		t.Fatalf("PCI memory window = %#x..%#x, want %#x..%#x",
			minimum, maximum, guestmem.MMIOStart, pvh.KVMTSSStart-1)
	}
	if length != uint32(pvh.KVMTSSStart-guestmem.MMIOStart) {
		t.Fatalf("PCI memory window length = %#x, want %#x", length, pvh.KVMTSSStart-guestmem.MMIOStart)
	}
}

func TestAMLPkgLengthBoundaries(t *testing.T) {
	t.Parallel()

	for _, size := range []int{0, 14, 15, 62, 63, 64, 4093, 4094, 4095, 65535} {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()
			object := amlObject([]byte{0x11}, make([]byte, size))
			lead := object[1]
			following := int(lead >> 6)
			length := int(lead & 0x3f)
			if following > 0 {
				length = int(lead & 0xf)
				for i := range following {
					length |= int(object[2+i]) << (4 + 8*i)
				}
			}
			if length != len(object)-1 || length-following-1 != size {
				t.Fatalf("AML package length %d with %d extra bytes does not cover %d-byte body", length, following, size)
			}
		})
	}
}

func applyUEFITestLoader(t *testing.T, files map[string][]byte, bases map[string]uint64) map[string][]byte {
	t.Helper()
	loaded := map[string][]byte{}
	loader := files[uefiTableLoader]
	if len(loader)%128 != 0 {
		t.Fatal("loader size is not a multiple of 128 bytes")
	}
	fileName := func(field []byte) string { return string(bytes.TrimRight(field, "\x00")) }
	for offset := 0; offset < len(loader); offset += 128 {
		command := loader[offset : offset+128]
		name := fileName(command[4:60])
		switch binary.LittleEndian.Uint32(command) {
		case 1:
			data, ok := files[name]
			if !ok {
				t.Fatalf("allocation references missing file %q", name)
			}
			alignment := binary.LittleEndian.Uint32(command[60:])
			if alignment == 0 || alignment&(alignment-1) != 0 || alignment > 4096 {
				t.Fatalf("invalid alignment %d", alignment)
			}
			if bases[name]%uint64(alignment) != 0 {
				t.Fatalf("test allocation is not aligned for %q", name)
			}
			loaded[name] = slices.Clone(data)
		case 2:
			target := fileName(command[60:116])
			data, ok := loaded[name]
			if !ok || loaded[target] == nil {
				t.Fatal("relocation references unallocated file")
			}
			pointerOffset := int(binary.LittleEndian.Uint32(command[116:]))
			width := int(command[120])
			if pointerOffset+width > len(data) {
				t.Fatal("relocation outside file")
			}
			var relative uint64
			switch width {
			case 4:
				relative = uint64(binary.LittleEndian.Uint32(data[pointerOffset:]))
			case 8:
				relative = binary.LittleEndian.Uint64(data[pointerOffset:])
			default:
				t.Fatalf("unsupported test relocation width %d", width)
			}
			if relative >= uint64(len(loaded[target])) {
				t.Fatalf("relative pointer %#x exceeds target %q", relative, target)
			}
			absolute := bases[target] + relative
			if width == 4 {
				binary.LittleEndian.PutUint32(data[pointerOffset:], uint32(absolute))
			} else {
				binary.LittleEndian.PutUint64(data[pointerOffset:], absolute)
			}
		case 3:
			data := loaded[name]
			result := binary.LittleEndian.Uint32(command[60:])
			start := binary.LittleEndian.Uint32(command[64:])
			length := binary.LittleEndian.Uint32(command[68:])
			if result >= uint32(len(data)) || start+length > uint32(len(data)) {
				t.Fatal("checksum outside file")
			}
			var sum byte
			for _, value := range data[start : start+length] {
				sum += value
			}
			data[result] -= sum
		default:
			t.Fatalf("unexpected loader opcode at %d", offset)
		}
	}

	return loaded
}

func lookupUEFITestTable(t *testing.T, tables []byte, address uint64, signature string) []byte {
	t.Helper()
	const base = uint64(0x3ff00000)
	if address < base || address-base+36 > uint64(len(tables)) {
		t.Fatalf("%s pointer %#x outside blob", signature, address)
	}
	offset := address - base
	length := uint64(binary.LittleEndian.Uint32(tables[offset+4:]))
	if length < 36 || offset+length > uint64(len(tables)) {
		t.Fatalf("%s table length %#x outside blob", signature, length)
	}
	table := tables[offset : offset+length]
	if string(table[:4]) != signature {
		t.Fatalf("signature %q, want %q", table[:4], signature)
	}
	assertACPIChecksum(t, table)

	return table
}

func assertACPIChecksum(t *testing.T, data []byte) {
	t.Helper()
	var sum byte
	for _, value := range data {
		sum += value
	}
	if sum != 0 {
		t.Fatalf("%q checksum = %#x, want 0", data[:4], sum)
	}
}

func assertUEFITestMADT(t *testing.T, madt []byte) {
	t.Helper()
	if got := binary.LittleEndian.Uint32(madt[36:]); got != 0xfee00000 {
		t.Errorf("LAPIC address = %#x", got)
	}
	var cpus int
	var ioAPICs int
	var pitOverrides int
	for offset := 44; offset < len(madt); {
		length := int(madt[offset+1])
		if length < 2 || offset+length > len(madt) {
			t.Fatalf("invalid MADT record at %d", offset)
		}
		entry := madt[offset : offset+length]
		switch entry[0] {
		case 0:
			if entry[2] != byte(cpus) || entry[3] != byte(cpus) || binary.LittleEndian.Uint32(entry[4:]) != 1 {
				t.Fatalf("invalid enabled CPU record %x", entry)
			}
			cpus++
		case 1:
			if binary.LittleEndian.Uint32(entry[4:]) != 0xfec00000 || binary.LittleEndian.Uint32(entry[8:]) != 0 {
				t.Fatalf("invalid IOAPIC record %x", entry)
			}
			ioAPICs++
		case 2:
			if entry[3] == 0 && binary.LittleEndian.Uint32(entry[4:]) == 2 {
				pitOverrides++
			}
		}
		offset += length
	}
	if cpus != 4 || ioAPICs != 1 || pitOverrides != 1 {
		t.Fatalf("MADT topology: %d CPUs, %d IOAPICs, %d PIT overrides", cpus, ioAPICs, pitOverrides)
	}
}
