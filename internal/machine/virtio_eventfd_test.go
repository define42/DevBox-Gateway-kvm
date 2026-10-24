package machine

import (
	"encoding/binary"
	"errors"
	"os"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
	"github.com/define42/DevBox-Gateway-kvm/kvm"
	"golang.org/x/sys/unix"
)

func newEventFDMachine(t *testing.T) *Machine {
	t.Helper()
	m, err := New("/dev/kvm", 1, MinMemSize)
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
		t.Skipf("KVM unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})

	return m
}

// eventFDTestDevice is a minimal modern virtio function recording guest kicks.
type eventFDTestDevice struct {
	*virtio.ModernTransport
	base   uint32
	queues int
	kicks  chan int
}

func newEventFDTestDevice(mem []byte, queues int, base uint32) *eventFDTestDevice {
	d := &eventFDTestDevice{base: base, queues: queues, kicks: make(chan int, 16)}
	d.ModernTransport = virtio.NewModernTransport(d, mem, func() error { return nil })

	return d
}

func (d *eventFDTestDevice) GetDeviceHeader() pci.DeviceHeader {
	return pci.DeviceHeader{
		VendorID: 0x1af4, DeviceID: 0x105a, RevisionID: 1, Status: 0x10, CapabilitiesPointer: 0x40,
		BAR: [6]uint32{d.base}, InterruptPin: 1, InterruptLine: 10,
	}
}

func (d *eventFDTestDevice) Read(uint64, []byte) error          { return nil }
func (d *eventFDTestDevice) Write(uint64, []byte) error         { return nil }
func (d *eventFDTestDevice) IOPort() uint64                     { return 0 }
func (d *eventFDTestDevice) Size() uint64                       { return 0 }
func (d *eventFDTestDevice) DeviceFeatures() uint64             { return 0 }
func (d *eventFDTestDevice) NumQueues() int                     { return d.queues }
func (d *eventFDTestDevice) DeviceConfigLen() int               { return 0 }
func (d *eventFDTestDevice) ReadDeviceConfig(uint64, []byte)    {}
func (d *eventFDTestDevice) WriteDeviceConfig(uint64, []byte)   {}
func (d *eventFDTestDevice) QueueReady(int, *virtio.SplitQueue) {}
func (d *eventFDTestDevice) Notify(idx int)                     { d.kicks <- idx }

func (d *eventFDTestDevice) expectKick(t *testing.T, queue int) {
	t.Helper()
	select {
	case got := <-d.kicks:
		if got != queue {
			t.Fatalf("kicked queue %d, want %d", got, queue)
		}
	case <-time.After(5 * time.Second):
		t.Fatalf("queue %d kick was not delivered", queue)
	}
}

func writeConfig(p *pci.PCI, slot, offset int, data []byte) {
	_ = p.PciConfAddrOut(0xcf8, pci.NumToBytes(uint32(0x80000000|slot<<11|offset&0xfc)))
	_ = p.PciConfDataOut(0xcfc+uint64(offset&3), data)
}

// guestMMIO performs the access the vCPU MMIO exit handler would.
func guestMMIO(t *testing.T, p *pci.PCI, addr uint64, data []byte) {
	t.Helper()
	dev, offset, ok := p.LookupMMIO(addr)
	if !ok {
		t.Fatalf("no device decodes %#x", addr)
	}
	dev.MMIO(offset, data, true)
}

// runDoorbellWrite runs 32-bit guest code that stores a 16-bit value to addr
// and then leaves through port 0x80, returning the first exit reason.
func runDoorbellWrite(t *testing.T, m *Machine, addr uint32) kvm.ExitType {
	t.Helper()
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	code := []byte{0x66, 0xb8, 0x01, 0x00, 0x66, 0xa3, 0, 0, 0, 0, 0xe6, 0x80, 0xf4}
	binary.LittleEndian.PutUint32(code[6:], addr)
	copy(m.mem[0x100000:], code)
	if err := m.SetupRegs(0x100000, 0, false); err != nil {
		t.Fatal(err)
	}
	if err := kvm.SetMPState(m.vcpuFds[0], &kvm.MPState{State: kvm.MPStateRunnable}); err != nil {
		t.Fatal(err)
	}
	if err := kvm.Run(m.vcpuFds[0]); err != nil {
		t.Fatal(err)
	}

	return kvm.ExitType(m.runs[0].ExitReason)
}

func TestVirtioDoorbellsCompleteInKernel(t *testing.T) {
	t.Parallel()
	m := newEventFDMachine(t)
	if m.doorbells == nil {
		t.Skip("KVM lacks ioeventfd")
	}
	dev := newEventFDTestDevice(m.mem, 2, 0xd0060000)
	m.connectVirtio(dev, 10)
	m.pci.AddDevices(dev)

	if exit := runDoorbellWrite(t, m, 0xd0060000+0x3004); exit != kvm.EXITIO {
		t.Fatalf("doorbell write exited with %s, want completion in KVM", exit)
	}
	dev.expectKick(t, 1)

	// Firmware relocates the BAR: the doorbells follow it.
	writeConfig(m.pci, 1, 0x10, pci.NumToBytes(uint32(0xd0070000)))
	if exit := runDoorbellWrite(t, m, 0xd0070000+0x3000); exit != kvm.EXITIO {
		t.Fatalf("relocated doorbell write exited with %s", exit)
	}
	dev.expectKick(t, 0)
	if exit := runDoorbellWrite(t, m, 0xd0060000+0x3004); exit != kvm.EXITMMIO {
		t.Fatalf("write to the vacated doorbell exited with %s, want an MMIO exit", exit)
	}
	select {
	case queue := <-dev.kicks:
		t.Fatalf("vacated doorbell kicked queue %d", queue)
	default:
	}
}

// doorbellBound reports whether an ioeventfd is assigned at addr.
func doorbellBound(t *testing.T, m *Machine, addr uint64) bool {
	t.Helper()
	fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(fd)
	probe := kvm.IOEventFD{Addr: addr, Len: m.doorbells.length, FD: int32(fd)}
	err = kvm.SetIOEventFD(m.vmFd, &probe)
	if errors.Is(err, syscall.EEXIST) {
		return true
	}
	if err != nil {
		t.Fatal(err)
	}
	probe.Flags = kvm.IOEventFDDeassign
	if err := kvm.SetIOEventFD(m.vmFd, &probe); err != nil {
		t.Fatal(err)
	}

	return false
}

func TestVirtioDoorbellsRebindFreedAddresses(t *testing.T) {
	t.Parallel()
	m := newEventFDMachine(t)
	if m.doorbells == nil {
		t.Skip("KVM lacks ioeventfd")
	}
	first := newEventFDTestDevice(m.mem, 1, 0xd0060000)
	second := newEventFDTestDevice(m.mem, 1, 0xd0070000)
	m.connectVirtio(first, 10)
	m.connectVirtio(second, 10)
	m.pci.AddDevices(first, second)
	if !doorbellBound(t, m, 0xd0063000) || !doorbellBound(t, m, 0xd0073000) {
		t.Fatal("initial doorbells are not bound")
	}

	// Enumeration briefly assigns the second BAR the first one's base.
	writeConfig(m.pci, 2, 0x10, pci.NumToBytes(uint32(0xd0060000)))
	if doorbellBound(t, m, 0xd0073000) {
		t.Fatal("moved doorbell stayed bound at its old address")
	}
	writeConfig(m.pci, 1, 0x10, pci.NumToBytes(uint32(0xd0080000)))
	if !doorbellBound(t, m, 0xd0083000) {
		t.Fatal("first device's doorbell did not follow its BAR")
	}
	// The address freed by the first device now belongs to the second.
	if exit := runDoorbellWrite(t, m, 0xd0063000); exit != kvm.EXITIO {
		t.Fatalf("doorbell freed by another device exited with %s", exit)
	}
	second.expectKick(t, 0)
}

func TestVirtioMSIXDeliversThroughIRQFD(t *testing.T) {
	t.Parallel()
	m := newEventFDMachine(t)
	if m.msiRoutes == nil {
		t.Skip("KVM lacks irqfd")
	}
	dev := newEventFDTestDevice(m.mem, 1, 0xd0060000)
	m.connectVirtio(dev, 10)
	m.pci.AddDevices(dev)
	var lapic kvm.LAPICState
	if err := kvm.GetLocalAPIC(m.vcpuFds[0], &lapic); err != nil {
		t.Fatal(err)
	}
	const svr, irr = 0xf0, 0x200
	binary.LittleEndian.PutUint32(lapic.Regs[svr:], binary.LittleEndian.Uint32(lapic.Regs[svr:])|0x1ff)
	if err := kvm.SetLocalAPIC(m.vcpuFds[0], &lapic); err != nil {
		t.Fatal(err)
	}

	// Program the device like Linux: enable MSI-X with the function masked,
	// write vector 1's message, map queue 0 to it, then unmask.
	const vector = 0x51
	writeConfig(m.pci, 1, 0x86, []byte{0x01, 0xc0})
	entry := uint64(0xd0060000 + 0x4000 + 16)
	guestMMIO(t, m.pci, entry, binary.LittleEndian.AppendUint32(nil, 0xfee00000))
	guestMMIO(t, m.pci, entry+4, make([]byte, 4))
	guestMMIO(t, m.pci, entry+8, binary.LittleEndian.AppendUint32(nil, vector))
	guestMMIO(t, m.pci, entry+12, make([]byte, 4))
	guestMMIO(t, m.pci, 0xd0060000+22, []byte{0, 0})
	guestMMIO(t, m.pci, 0xd0060000+26, []byte{1, 0})
	writeConfig(m.pci, 1, 0x86, []byte{0x01, 0x80})
	if err := dev.Interrupt(0); err != nil {
		t.Fatal(err)
	}
	for deadline := time.Now().Add(5 * time.Second); ; time.Sleep(time.Millisecond) {
		if err := kvm.GetLocalAPIC(m.vcpuFds[0], &lapic); err != nil {
			t.Fatal(err)
		}
		if binary.LittleEndian.Uint32(lapic.Regs[irr+vector/32*0x10:])&(1<<(vector%32)) != 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("queue interrupt did not reach the local APIC")
		}
	}
	// Legacy line interrupts still resolve through the replaced routing table.
	if err := kvm.IRQLineStatus(m.vmFd, 4, 0); err != nil {
		t.Fatal(err)
	}
}

func TestVirtioMSIXFallsBackWhenGSIsRunOut(t *testing.T) {
	t.Parallel()
	routing := &msiRouting{next: 30, limit: 30}
	if _, err := routing.newRouter(2); !errors.Is(err, errGSIExhausted) {
		t.Fatalf("router without GSIs = %v, want %v", err, errGSIExhausted)
	}
	m := &Machine{pci: pci.New(), msiRoutes: routing}
	dev := newEventFDTestDevice(make([]byte, 0x10000), 1, 0xd0060000)
	m.connectVirtio(dev, 10)
	if caps := dev.Capabilities(); len(caps) != 0x44 {
		t.Fatalf("device without vectors offers MSI-X: %d capability bytes", len(caps))
	}
	if err := routing.close(); err != nil {
		t.Fatal(err)
	}
	if err := routing.route(30, 0xfee00000, 0x30); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("route after close = %v", err)
	}
}
