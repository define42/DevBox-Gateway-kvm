package kvm_test

import (
	"encoding/binary"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/kvm"
	"golang.org/x/sys/unix"
)

func TestIRQRoutingEntryMatchesKernelLayout(t *testing.T) {
	t.Parallel()
	// struct kvm_irq_routing_entry is 48 bytes with its union at offset 16.
	var entry kvm.IRQRoutingEntry
	if size := unsafe.Sizeof(entry); size != 48 {
		t.Fatalf("routing entry size = %d, want 48", size)
	}
	if offset := unsafe.Offsetof(entry.U); offset != 16 {
		t.Fatalf("routing union offset = %d, want 16", offset)
	}
	data, err := (&kvm.IRQRouting{
		Nr:      1,
		Entries: []kvm.IRQRoutingEntry{kvm.NewMSIRoute(30, 0x1_fee0_1000, 0x4041)},
	}).Bytes()
	if err != nil {
		t.Fatal(err)
	}
	le := binary.LittleEndian
	if len(data) != 8+48 || le.Uint32(data[8:]) != 30 || le.Uint32(data[12:]) != kvm.IRQRouteMSI ||
		le.Uint32(data[24:]) != 0xfee01000 || le.Uint32(data[28:]) != 1 || le.Uint32(data[32:]) != 0x4041 {
		t.Fatalf("MSI route encoding = %x", data)
	}
}

func TestDefaultIRQRoutesMatchKVM(t *testing.T) {
	t.Parallel()
	routes := kvm.DefaultIRQRoutes()
	if len(routes) != 40 {
		t.Fatalf("default routes = %d, want 40", len(routes))
	}
	pins := map[[2]uint32]uint32{}
	for _, route := range routes {
		if route.Type != kvm.IRQRouteIRQChip {
			t.Fatalf("default route %+v is not an irqchip route", route)
		}
		pins[[2]uint32{route.GSI, route.U[0]}] = route.U[1]
	}
	for gsi := uint32(0); gsi < kvm.IOAPICPins; gsi++ {
		if pin, ok := pins[[2]uint32{gsi, kvm.IRQChipIOAPIC}]; !ok || pin != gsi {
			t.Fatalf("GSI %d does not reach IOAPIC pin %d", gsi, gsi)
		}
		pic := uint32(kvm.IRQChipPICMaster)
		if gsi >= 8 {
			pic = kvm.IRQChipPICSlave
		}
		if pin, ok := pins[[2]uint32{gsi, pic}]; ok != (gsi < 16) || (ok && pin != gsi%8) {
			t.Fatalf("GSI %d PIC route = %d/%v", gsi, pin, ok)
		}
	}
}

// newIRQChipVM returns a VM with in-kernel PIC, IOAPIC and LAPICs.
func newIRQChipVM(t *testing.T) uintptr {
	t.Helper()
	devKVM, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devKVM.Close() })
	vmFd, err := kvm.CreateVM(devKVM.Fd())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(int(vmFd)) })
	if err := kvm.CreateIRQChip(vmFd); err != nil {
		t.Fatal(err)
	}

	return vmFd
}

func newEventFD(t *testing.T) int {
	t.Helper()
	fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = unix.Close(fd) })

	return fd
}

func TestSetGSIRoutingKeepsLegacyLines(t *testing.T) {
	t.Parallel()
	vmFd := newIRQChipVM(t)
	routes := append(kvm.DefaultIRQRoutes(), kvm.NewMSIRoute(kvm.IOAPICPins, 0xfee00000, 0x30))
	if err := kvm.SetGSIRouting(vmFd, &kvm.IRQRouting{Nr: uint32(len(routes)), Entries: routes}); err != nil {
		t.Fatal(err)
	}
	// Line interrupts resolve through the replaced table.
	for _, level := range []uint32{1, 0} {
		if err := kvm.IRQLineStatus(vmFd, 4, level); err != nil {
			t.Fatalf("GSI 4 level %d after MSI routing: %v", level, err)
		}
	}
	short := &kvm.IRQRouting{Nr: 2, Entries: routes[:1]}
	if err := kvm.SetGSIRouting(vmFd, short); !errors.Is(err, syscall.EINVAL) {
		t.Fatalf("routing count beyond entries = %v, want EINVAL", err)
	}
}

func TestIRQFDDeliversRoutedMSI(t *testing.T) {
	t.Parallel()
	vmFd := newIRQChipVM(t)
	vcpuFd, err := kvm.CreateVCPU(vmFd, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(int(vcpuFd)) })
	// Fixed interrupts are accepted only by a software-enabled local APIC.
	var lapic kvm.LAPICState
	if err := kvm.GetLocalAPIC(vcpuFd, &lapic); err != nil {
		t.Fatal(err)
	}
	const svr, irr = 0xf0, 0x200
	binary.LittleEndian.PutUint32(lapic.Regs[svr:], binary.LittleEndian.Uint32(lapic.Regs[svr:])|0x1ff)
	if err := kvm.SetLocalAPIC(vcpuFd, &lapic); err != nil {
		t.Fatal(err)
	}

	const gsi, vector = kvm.IOAPICPins, 0x45
	routes := append(kvm.DefaultIRQRoutes(), kvm.NewMSIRoute(gsi, 0xfee00000, vector))
	if err := kvm.SetGSIRouting(vmFd, &kvm.IRQRouting{Nr: uint32(len(routes)), Entries: routes}); err != nil {
		t.Fatal(err)
	}
	fd := newEventFD(t)
	if err := kvm.SetIRQFD(vmFd, &kvm.IRQFD{FD: uint32(fd), GSI: gsi}); err != nil {
		t.Fatal(err)
	}
	var one [8]byte
	binary.NativeEndian.PutUint64(one[:], 1)
	if _, err := unix.Write(fd, one[:]); err != nil {
		t.Fatal(err)
	}
	// KVM may complete delivery from a workqueue rather than the eventfd wakeup.
	pending := func() bool {
		if err := kvm.GetLocalAPIC(vcpuFd, &lapic); err != nil {
			t.Fatal(err)
		}
		word := binary.LittleEndian.Uint32(lapic.Regs[irr+vector/32*0x10:])

		return word&(1<<(vector%32)) != 0
	}
	for deadline := time.Now().Add(5 * time.Second); !pending(); {
		if time.Now().After(deadline) {
			t.Fatal("irqfd did not deliver the routed MSI vector")
		}
		time.Sleep(time.Millisecond)
	}
	if err := kvm.SetIRQFD(vmFd, &kvm.IRQFD{FD: uint32(fd), GSI: gsi, Flags: kvm.IRQFDDeassign}); err != nil {
		t.Fatal(err)
	}
}

func TestIOEventFDAbsorbsGuestWrite(t *testing.T) {
	t.Parallel()
	devKVM, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = devKVM.Close() })
	vmFd, err := kvm.CreateVM(devKVM.Fd())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(int(vmFd)) })
	mem, err := syscall.Mmap(-1, 0, 0x1000, syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED|syscall.MAP_ANONYMOUS)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Munmap(mem) })
	// mov ax, 0x2000; mov ds, ax; mov [0], ax; hlt: a 16-bit write to the
	// unbacked guest-physical address 0x20000.
	copy(mem, []byte{0xb8, 0x00, 0x20, 0x8e, 0xd8, 0xa3, 0x00, 0x00, 0xf4})
	if err := kvm.SetUserMemoryRegion(vmFd, &kvm.UserspaceMemoryRegion{
		GuestPhysAddr: 0x1000, MemorySize: 0x1000, UserspaceAddr: uint64(uintptr(unsafe.Pointer(&mem[0]))),
	}); err != nil {
		t.Fatal(err)
	}
	vcpuFd, err := kvm.CreateVCPU(vmFd, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Close(int(vcpuFd)) })
	mmapSize, err := kvm.GetVCPUMMmapSize(devKVM.Fd())
	if err != nil {
		t.Fatal(err)
	}
	mapping, err := syscall.Mmap(int(vcpuFd), 0, int(mmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = syscall.Munmap(mapping) })
	run := (*kvm.RunData)(unsafe.Pointer(&mapping[0]))
	start := func() {
		t.Helper()
		sregs, err := kvm.GetSregs(vcpuFd)
		if err != nil {
			t.Fatal(err)
		}
		sregs.CS.Base, sregs.CS.Selector = 0, 0
		if err := kvm.SetSregs(vcpuFd, sregs); err != nil {
			t.Fatal(err)
		}
		if err := kvm.SetRegs(vcpuFd, &kvm.Regs{RIP: 0x1000, RFLAGS: 2}); err != nil {
			t.Fatal(err)
		}
	}

	var length uint32 = 2
	if supported, err := kvm.CheckExtension(devKVM.Fd(), kvm.CapIOEventFDAnyLength); err == nil && supported != 0 {
		length = 0
	}
	fd := newEventFD(t)
	doorbell := kvm.IOEventFD{Addr: 0x20000, Len: length, FD: int32(fd)}
	if err := kvm.SetIOEventFD(vmFd, &doorbell); err != nil {
		t.Fatal(err)
	}
	duplicate := doorbell
	duplicate.FD = int32(newEventFD(t))
	if err := kvm.SetIOEventFD(vmFd, &duplicate); !errors.Is(err, syscall.EEXIST) {
		t.Fatalf("second ioeventfd at one address = %v, want EEXIST", err)
	}

	start()
	if err := kvm.Run(vcpuFd); err != nil {
		t.Fatal(err)
	}
	if exit := kvm.ExitType(run.ExitReason); exit != kvm.EXITHLT {
		t.Fatalf("guest write with ioeventfd exited with %s, want HLT", exit)
	}
	var count [8]byte
	if _, err := unix.Read(fd, count[:]); err != nil || binary.NativeEndian.Uint64(count[:]) != 1 {
		t.Fatalf("ioeventfd count = %d (%v), want 1", binary.NativeEndian.Uint64(count[:]), err)
	}

	deassign := doorbell
	deassign.Flags = kvm.IOEventFDDeassign
	if err := kvm.SetIOEventFD(vmFd, &deassign); err != nil {
		t.Fatal(err)
	}
	if err := kvm.SetIOEventFD(vmFd, &deassign); !errors.Is(err, syscall.ENOENT) {
		t.Fatalf("repeated deassign = %v, want ENOENT", err)
	}
	start()
	if err := kvm.Run(vcpuFd); err != nil {
		t.Fatal(err)
	}
	if addr, _, write := run.MMIO(); kvm.ExitType(run.ExitReason) != kvm.EXITMMIO || addr != 0x20000 || !write {
		t.Fatalf("deassigned doorbell exit = %s at %#x", kvm.ExitType(run.ExitReason), addr)
	}
}
