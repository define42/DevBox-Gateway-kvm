package machine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
	"unsafe"

	"github.com/define42/gokvm/internal/iodev"
	"github.com/define42/gokvm/internal/pvh"
	"github.com/define42/gokvm/kvm"
)

func TestGuestMemoryAlignment(t *testing.T) {
	t.Parallel()
	sizes := []int{
		1,
		4096,
		MinMemSize,
		guestMemoryAlignment,
		guestMemoryAlignment + 4096,
		3*guestMemoryAlignment + 17,
	}
	for _, size := range sizes {
		t.Run(fmt.Sprint(size), func(t *testing.T) {
			t.Parallel()

			mapping, mem, err := mapGuestMemory(size)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := syscall.Munmap(mapping); err != nil {
					t.Errorf("unmapping original allocation: %v", err)
				}
			})
			addr := uintptr(unsafe.Pointer(&mem[0]))
			base := uintptr(unsafe.Pointer(&mapping[0]))
			if addr%guestMemoryAlignment != 0 {
				t.Fatalf("guest RAM %#x is not 2 MiB aligned", addr)
			}
			if len(mem) != size || cap(mem) != size || addr < base || addr+uintptr(size) > base+uintptr(len(mapping)) {
				t.Fatal("aligned RAM does not preserve requested size within the allocation")
			}
			if mem[0] != 0 || mem[len(mem)-1] != 0 {
				t.Fatal("anonymous RAM was not zero initialized")
			}
			mem[0], mem[len(mem)-1] = 0x35, 0x7a
			if mapping[int(addr-base)+len(mem)-1] != 0x7a {
				t.Fatal("aligned RAM does not alias the original allocation")
			}
		})
	}
}

func TestGuestMemoryRejectsInvalidSize(t *testing.T) {
	t.Parallel()
	for _, size := range []int{-1, 0, int(^uint(0) >> 1)} {
		mapping, mem, err := mapGuestMemory(size)
		if !errors.Is(err, syscall.EINVAL) || mapping != nil || mem != nil {
			t.Fatalf("size %d: got allocation %d/%d and error %v", size, len(mapping), len(mem), err)
		}
	}
}

func TestGuestMemoryKVMCoherence(t *testing.T) {
	t.Parallel()

	dev, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		t.Skipf("KVM unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = dev.Close() })
	mapping, mem, err := mapGuestMemory(guestMemoryAlignment + 4096)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Munmap(mapping) })
	vm, err := kvm.CreateVM(dev.Fd())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(int(vm)) })
	if err := kvm.SetUserMemoryRegion(vm, &kvm.UserspaceMemoryRegion{
		MemorySize: uint64(len(mem)), UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
	}); err != nil {
		t.Fatal(err)
	}
	vcpu, err := kvm.CreateVCPU(vm, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(int(vcpu)) })
	runSize, err := kvm.GetVCPUMMmapSize(dev.Fd())
	if err != nil {
		t.Fatal(err)
	}
	runMapping, err := syscall.Mmap(int(vcpu), 0, int(runSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Munmap(runMapping) })
	sregs, err := kvm.GetSregs(vcpu)
	if err != nil {
		t.Fatal(err)
	}
	sregs.CS.Base, sregs.CS.Selector, sregs.DS.Base, sregs.DS.Selector = 0, 0, 0, 0
	if err := kvm.SetSregs(vcpu, sregs); err != nil {
		t.Fatal(err)
	}

	// Read a host-written byte, add seven, write it back through the guest
	// mapping, then halt. Private mmap must still be shared with the vCPU.
	copy(mem[0x1000:], []byte{0xa0, 0x00, 0x20, 0x04, 0x07, 0xa2, 0x01, 0x20, 0xf4})
	mem[0x2000] = 35
	if err := kvm.SetRegs(vcpu, &kvm.Regs{RIP: 0x1000, RFLAGS: 2}); err != nil {
		t.Fatal(err)
	}
	if err := kvm.Run(vcpu); err != nil {
		t.Fatal(err)
	}
	run := (*kvm.RunData)(unsafe.Pointer(&runMapping[0]))
	if kvm.ExitType(run.ExitReason) != kvm.EXITHLT || mem[0x2001] != 42 {
		t.Fatalf("guest/host coherence failed: exit %v, result %d", run.ExitReason, mem[0x2001])
	}
}

func TestGuestMemoryLargeLayout(t *testing.T) {
	t.Parallel()
	const size = 6 << 30
	m := bootTestMachine(t, size)
	low, high := m.ramSizes()
	if low != 0xd0000000 || high != size-low || uint64(len(m.mem)) != 1<<32+high {
		t.Fatalf("incorrect split: low %#x high %#x span %#x", low, high, len(m.mem))
	}
	regions := m.memoryRegions()
	if len(regions) != 2 || regions[0].GuestPhysAddr != 0 || regions[0].MemorySize != low ||
		regions[1].GuestPhysAddr != 1<<32 || regions[1].MemorySize != high {
		t.Fatalf("incorrect KVM RAM slots: %+v", regions)
	}
	if regions[1].UserspaceAddr-regions[0].UserspaceAddr != 1<<32 {
		t.Fatal("host mapping does not preserve guest physical address indexing")
	}
	var totalRAM uint64
	var hasPlatformReservation bool
	for _, r := range m.memoryMap() {
		if r.Type == 1 {
			totalRAM += r.Size
			if r.Addr < 1<<32 && r.Addr+r.Size > low {
				t.Fatalf("E820 marks MMIO hole as RAM: %+v", r)
			}
		} else if r.Addr == pvh.KVMTSSStart && r.Addr+r.Size == 1<<32 {
			hasPlatformReservation = true
		}
		if r.Addr < pvh.KVMTSSStart && r.Addr+r.Size > low {
			t.Fatalf("E820 covers the gap Linux needs for PCI allocation: %+v", r)
		}
	}
	if !hasPlatformReservation || totalRAM != size-(highMemBase-0x9fc00) {
		t.Fatalf("incorrect E820 accounting: platform reservation=%v, RAM=%#x", hasPlatformReservation, totalRAM)
	}
	cmos := iodev.NewCMOS(low, high)
	cmosLow := uint64(binary.LittleEndian.Uint16(cmos.Data[0x34:]))<<16 + 16<<20
	cmosHigh := (uint64(cmos.Data[0x5b]) | uint64(cmos.Data[0x5c])<<8 | uint64(cmos.Data[0x5d])<<16) << 16
	if cmosLow != low || cmosHigh != high {
		t.Fatalf("CMOS RAM sizes %#x/%#x disagree with KVM %#x/%#x", cmosLow, cmosHigh, low, high)
	}
	if _, err := m.ReadAt(make([]byte, 2), int64(low-1)); err == nil {
		t.Fatal("read crossing into hole succeeded")
	}
	if _, err := m.WriteAt([]byte{1}, int64(low)); err == nil {
		t.Fatal("write into hole succeeded")
	}
	if _, err := m.WriteAt([]byte{42}, 1<<32); err != nil {
		t.Fatal(err)
	}
	var result [1]byte
	if _, err := m.ReadAt(result[:], 1<<32); err != nil || result[0] != 42 {
		t.Fatalf("high RAM read: %v, %v", result, err)
	}
}

func TestGuestMemoryKVMHighRAMAndMMIO(t *testing.T) {
	t.Parallel()
	dev, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		t.Skipf("KVM unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	if err := dev.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := New("/dev/kvm", 1, 6<<30)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := m.SetupRegs(0x1000, 0, true); err != nil {
		t.Fatal(err)
	}
	// Map virtual 1 GiB to physical 4 GiB using an existing 2 MiB PDE.
	binary.LittleEndian.PutUint64(m.mem[pageTableBase+0x2000+512*8:], 1<<32|0x83)
	copy(m.mem[0x1000:], []byte{
		0xbb, 0, 0, 0, 0x40, // mov ebx, 0x40000000
		0xc6, 0x03, 0x5a, // mov byte [rbx], 0x5a
		0xbb, 0, 0, 0, 0xd0, // mov ebx, 0xd0000000
		0x8b, 0x03, // mov eax, [rbx] -- must exit for MMIO
		0xf4,
	})
	if err := kvm.Run(m.vcpuFds[0]); err != nil {
		t.Fatal(err)
	}
	if m.mem[1<<32] != 0x5a {
		t.Fatal("guest high RAM write was not visible to host")
	}
	if kvm.ExitType(m.runs[0].ExitReason) != kvm.EXITMMIO {
		t.Fatalf("PCI hole access exited with %v instead of MMIO", kvm.ExitType(m.runs[0].ExitReason))
	}
	addr, _, write := m.runs[0].MMIO()
	if addr != 0xd0000000 || write {
		t.Fatalf("wrong MMIO access: %#x, write %v", addr, write)
	}
}
