package ebda_test

import (
	"encoding/binary"
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/ebda"
)

func TestMPTableDescribesConfiguredCPUs(t *testing.T) {
	t.Parallel()
	for _, cpus := range []int{1, 2, 4, 64} {
		t.Run(fmt.Sprintf("CPUs%d", cpus), func(t *testing.T) {
			t.Parallel()
			e, err := ebda.New(cpus)
			if err != nil {
				t.Fatal(err)
			}
			data, err := e.Bytes()
			if err != nil {
				t.Fatal(err)
			}
			// Decode the physical table independently of its Go structures.
			// The floating pointer must remain in the first KiB searched by
			// Linux, and both Linux and PVH loaders copy these bytes verbatim.
			if len(data) < 0x40+44 {
				t.Fatalf("truncated EBDA: %d bytes", len(data))
			}
			floating := data[0x30:0x40]
			if string(floating[:4]) != "_MP_" || floating[8] != 1 || floating[9] != 4 {
				t.Fatalf("invalid MP floating pointer: %x", floating)
			}
			checkMPChecksum(t, floating)
			physical := binary.LittleEndian.Uint32(floating[4:8])
			if physical != 0x9fc40 {
				t.Fatalf("MP table physical address: %#x", physical)
			}
			table := data[int(physical)-0x9fc00:]
			if string(table[:4]) != "PCMP" || table[6] != 4 {
				t.Fatalf("invalid MP table header: %x", table[:44])
			}
			if got := string(table[8:16]); got != "DEVBOXVM" {
				t.Errorf("MP table OEM ID = %q, want DEVBOXVM", got)
			}
			for _, value := range table[16:34] {
				if value != 0 {
					t.Fatalf("OEM ID overwrote adjacent product/OEM fields: %x", table[16:34])
				}
			}
			length := int(binary.LittleEndian.Uint16(table[4:6]))
			if length != len(table) || length != 44+20*cpus {
				t.Fatalf("table length=%d, serialized=%d, want %d", length, len(table), 44+20*cpus)
			}
			checkMPChecksum(t, table)
			if count := int(binary.LittleEndian.Uint16(table[34:36])); count != cpus {
				t.Fatalf("MP entry count=%d, want %d", count, cpus)
			}
			if lapic := binary.LittleEndian.Uint32(table[36:40]); lapic != 0xfee00000 {
				t.Fatalf("local APIC address: %#x", lapic)
			}
			for offset, cpu := 44, 0; offset < len(table); offset, cpu = offset+20, cpu+1 {
				entry := table[offset : offset+20]
				flags := byte(1) // Every advertised CPU is present and enabled.
				if cpu == 0 {
					flags |= 2 // Exactly one bootstrap processor.
				}
				if entry[0] != 0 || int(entry[1]) != cpu || entry[2] != 0x14 || entry[3] != flags {
					t.Fatalf("invalid processor entry %d: %x", cpu, entry)
				}
			}
		})
	}
}

func checkMPChecksum(t *testing.T, data []byte) {
	t.Helper()
	var checksum byte
	for _, b := range data {
		checksum += b
	}
	if checksum != 0 {
		t.Fatalf("invalid MP checksum: %#x", checksum)
	}
}

func TestNewRejectsInvalidCPUCounts(t *testing.T) {
	t.Parallel()
	for _, cpus := range []int{-1, 0, 65} {
		if _, err := ebda.New(cpus); err == nil {
			t.Fatalf("accepted invalid CPU count %d", cpus)
		}
	}
}
