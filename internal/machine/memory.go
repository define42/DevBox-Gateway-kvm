package machine

import (
	"fmt"
	"syscall"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/internal/bootparam"
	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"github.com/define42/DevBox-Gateway-kvm/internal/pvh"
	"github.com/define42/DevBox-Gateway-kvm/kvm"
)

const guestMemoryAlignment = 2 << 20

// mapGuestMemory returns both the complete mmap allocation and the aligned RAM
// slice. Only the complete allocation may be passed to syscall.Munmap.
func mapGuestMemory(size int) (mapping, mem []byte, err error) {
	const padding = guestMemoryAlignment - 1
	const holeSize = guestmem.HighRAMStart - guestmem.MMIOStart
	if size <= 0 || uint64(size) > uint64(int(^uint(0)>>1)-padding)-holeSize {
		return nil, nil, syscall.EINVAL
	}
	// Keep guest physical addresses usable as slice indices. The hole only
	// reserves virtual address space: it is never registered with KVM or
	// accessed by loaders/devices, so no physical host RAM backs it.
	span := size
	if uint64(size) > guestmem.MMIOStart {
		span += int(holeSize)
	}

	mapping, err = syscall.Mmap(-1, 0, span+padding,
		syscall.PROT_READ|syscall.PROT_WRITE,
		syscall.MAP_PRIVATE|syscall.MAP_ANONYMOUS)
	if err != nil {
		return nil, nil, err
	}

	// KVM can use large pages when host and guest addresses have matching low
	// 21 bits. Guest RAM starts at physical address zero.
	offset := int(-uintptr(unsafe.Pointer(&mapping[0])) & padding)
	mem = mapping[offset : offset+span : offset+span]
	// Private anonymous RAM supports THP independently of the host's shmem
	// policy. Unsupported or unavailable huge pages safely fall back to 4 KiB.
	_ = syscall.Madvise(mem, syscall.MADV_HUGEPAGE)

	return mapping, mem, nil
}

func (m *Machine) ramSizes() (low, high uint64) {
	span := uint64(len(m.mem))
	low = min(span, guestmem.MMIOStart)
	if span > guestmem.HighRAMStart {
		high = span - guestmem.HighRAMStart
	}

	return low, high
}

func (m *Machine) memoryRegions() []kvm.UserspaceMemoryRegion {
	low, high := m.ramSizes()
	regions := []kvm.UserspaceMemoryRegion{{
		Slot: 0, GuestPhysAddr: 0, MemorySize: low,
		UserspaceAddr: uint64(uintptr(unsafe.Pointer(&m.mem[0]))),
	}}
	if high != 0 {
		regions = append(regions, kvm.UserspaceMemoryRegion{
			Slot: 1, GuestPhysAddr: guestmem.HighRAMStart, MemorySize: high,
			UserspaceAddr: uint64(uintptr(unsafe.Pointer(&m.mem[guestmem.HighRAMStart]))),
		})
	}

	return regions
}

func (m *Machine) registerMemory() error {
	for _, region := range m.memoryRegions() {
		if err := kvm.SetUserMemoryRegion(m.vmFd, &region); err != nil {
			return fmt.Errorf("register guest RAM at %#x: %w", region.GuestPhysAddr, err)
		}
	}

	return nil
}

// memoryMap describes RAM and reservations identically for Linux and PVH.
func (m *Machine) memoryMap() []bootparam.E820Entry {
	low, high := m.ramSizes()
	entries := []bootparam.E820Entry{
		{Addr: 0, Size: bootparam.EBDAStart, Type: bootparam.E820Ram},
		{Addr: bootparam.EBDAStart, Size: highMemBase - bootparam.EBDAStart, Type: bootparam.E820Reserved},
	}
	if m.vesaEnabled {
		entries = append(entries,
			bootparam.E820Entry{Addr: highMemBase, Size: vesaFramebufferBase - highMemBase, Type: bootparam.E820Ram},
			bootparam.E820Entry{Addr: vesaFramebufferBase, Size: vesaFramebufferReserveSize, Type: bootparam.E820Reserved},
			bootparam.E820Entry{Addr: vesaFramebufferEnd, Size: low - vesaFramebufferEnd, Type: bootparam.E820Ram},
		)
	} else {
		entries = append(entries, bootparam.E820Entry{Addr: highMemBase, Size: low - highMemBase, Type: bootparam.E820Ram})
	}
	// PCI BAR space must be a gap in E820, not an E820Reserved entry:
	// Linux searches gaps between all entry types when choosing its PCI window.
	// Reserve only the platform tail containing KVM's TSS/identity pages and
	// the APICs, keeping the whole MMIO hole absent from KVM RAM slots.
	entries = append(entries, bootparam.E820Entry{
		Addr: pvh.KVMTSSStart, Size: guestmem.HighRAMStart - pvh.KVMTSSStart, Type: bootparam.E820Reserved,
	})
	if high != 0 {
		entries = append(entries, bootparam.E820Entry{Addr: guestmem.HighRAMStart, Size: high, Type: bootparam.E820Ram})
	}

	return entries
}
