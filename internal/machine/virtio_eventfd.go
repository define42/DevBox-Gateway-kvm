package machine

import (
	"cmp"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"slices"
	"sync"
	"syscall"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
	"github.com/define42/DevBox-Gateway-kvm/kvm"
	"golang.org/x/sys/unix"
)

// Virtio fast paths: KVM ioeventfds complete queue kicks inside the kernel,
// and irqfds bound to MSI routes deliver per-queue MSI-X interrupts without a
// vCPU exit, an ISR read or an INTx assert/deassert pair. Each is enabled when
// KVM supports it; otherwise devices keep the userspace MMIO and INTx paths.

var errGSIExhausted = errors.New("KVM GSI routes exhausted")

// virtioFunction is a modern virtio PCI function: its bus identity plus the
// methods promoted from its embedded virtio.ModernTransport.
type virtioFunction interface {
	pci.CapsAndMMIO
	SetIRQLine(setLevel func(bool) error)
	EnableMSIX(router virtio.MSIXRouter)
	MSIXVectors() int
	QueueDoorbells() []uint64
	NotifyQueue(idx int)
}

// initVirtioEventFDs enables the fast paths the host kernel supports. It runs
// once the in-kernel irqchip exists, before any device is attached.
func (m *Machine) initVirtioEventFDs() {
	routes, err := kvm.CheckExtension(m.kvmFd, kvm.CapIRQRouting)
	irqfd, irqfdErr := kvm.CheckExtension(m.kvmFd, kvm.CapIRQFD)
	if err == nil && irqfdErr == nil && irqfd != 0 && routes > kvm.IOAPICPins {
		m.msiRoutes = &msiRouting{vmFd: m.vmFd, next: kvm.IOAPICPins, limit: uint32(routes)}
	}
	if supported, err := kvm.CheckExtension(m.kvmFd, kvm.CapIOEventFD); err != nil || supported == 0 {
		return
	}
	// A zero-length ioeventfd matches any width and lets KVM complete the
	// write on its fast MMIO path; drivers ring virtio doorbells with 16 bits.
	length := uint32(2)
	if anyLength, err := kvm.CheckExtension(m.kvmFd, kvm.CapIOEventFDAnyLength); err == nil && anyLength != 0 {
		length = 0
	}
	m.doorbells = &doorbellRegistry{vmFd: m.vmFd, length: length}
	m.pci.ObserveMMIOBARs(m.doorbells.moved)
}

// connectVirtio wires a virtio function's interrupts and queue doorbells. Call
// it before starting the device's workers and before adding it to the bus.
func (m *Machine) connectVirtio(dev virtioFunction, irq uint8) {
	m.connectVirtioIRQ(dev, irq)
	if m.msiRoutes != nil {
		if router, err := m.msiRoutes.newRouter(dev.MSIXVectors()); err != nil {
			log.Printf("virtio %#04x: MSI-X unavailable, using INTx: %v", dev.GetDeviceHeader().DeviceID, err)
		} else {
			dev.EnableMSIX(router)
		}
	}
	if m.doorbells != nil {
		if err := m.doorbells.add(dev, &m.deviceWG); err != nil {
			log.Printf("virtio %#04x: ioeventfd unavailable, using MMIO exits: %v", dev.GetDeviceHeader().DeviceID, err)
		}
	}
}

// msiRouting owns the VM's GSI routing table: KVM's default line routes plus
// an MSI route for every MSI-X vector a guest has programmed. Each vector has
// a dedicated GSI and irqfd, assigned when its device is attached.
type msiRouting struct {
	mu     sync.Mutex
	vmFd   uintptr
	next   uint32 // Next unassigned GSI; lower GSIs are IOAPIC/PIC lines.
	limit  uint32
	routes []kvm.IRQRoutingEntry
	irqfds []*os.File
	closed bool
}

func (r *msiRouting) newRouter(vectors int) (*msixRouter, error) {
	router := &msixRouter{routing: r}
	binary.NativeEndian.PutUint64(router.signal[:], 1)
	for range vectors {
		gsi, file, err := r.allocate()
		if err != nil {
			return nil, err
		}
		router.gsis = append(router.gsis, gsi)
		router.irqfds = append(router.irqfds, file)
	}

	return router, nil
}

// allocate binds a fresh eventfd to an unused GSI. The GSI stays unrouted, and
// so inert, until the guest programs and unmasks its vector.
func (r *msiRouting) allocate() (uint32, *os.File, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.next >= r.limit {
		return 0, nil, errGSIExhausted
	}
	// Only device workers write the eventfd and KVM consumes it in its wakeup
	// callback. Keep it blocking so Go never registers it with the poller.
	fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC)
	if err != nil {
		return 0, nil, fmt.Errorf("create irqfd: %w", err)
	}
	if err := kvm.SetIRQFD(r.vmFd, &kvm.IRQFD{FD: uint32(fd), GSI: r.next}); err != nil {
		_ = unix.Close(fd)

		return 0, nil, fmt.Errorf("bind irqfd to GSI %d: %w", r.next, err)
	}
	file := os.NewFile(uintptr(fd), "virtio-msix-irqfd")
	r.irqfds = append(r.irqfds, file)
	r.next++

	return r.next - 1, file, nil
}

// route points gsi at an MSI message and replaces KVM's routing table, which
// KVM_SET_GSI_ROUTING only accepts as a whole.
func (r *msiRouting) route(gsi uint32, address uint64, data uint32) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return os.ErrClosed
	}
	entry := kvm.NewMSIRoute(gsi, address, data)
	if i, found := slices.BinarySearchFunc(r.routes, gsi, func(e kvm.IRQRoutingEntry, gsi uint32) int {
		return cmp.Compare(e.GSI, gsi)
	}); found {
		r.routes[i] = entry
	} else {
		r.routes = slices.Insert(r.routes, i, entry)
	}
	table := slices.Concat(kvm.DefaultIRQRoutes(), r.routes)

	return kvm.SetGSIRouting(r.vmFd, &kvm.IRQRouting{Nr: uint32(len(table)), Entries: table})
}

func (r *msiRouting) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	var errs []error
	for _, file := range r.irqfds {
		errs = append(errs, file.Close())
	}
	r.irqfds = nil

	return errors.Join(errs...)
}

// msixRouter implements virtio.MSIXRouter for one device: vector n is
// delivered by signalling the irqfd of gsis[n].
type msixRouter struct {
	routing *msiRouting
	gsis    []uint32
	irqfds  []*os.File
	signal  [8]byte // The eventfd increment, written by every Signal.
}

func (r *msixRouter) Route(vector int, address uint64, data uint32) error {
	return r.routing.route(r.gsis[vector], address, data)
}

func (r *msixRouter) Signal(vector int) error {
	_, err := r.irqfds[vector].Write(r.signal[:])

	return err
}

// doorbellRegistry binds a KVM ioeventfd to every queue doorbell and moves the
// bindings whenever the guest moves a device's memory BAR. A doorbell KVM
// cannot bind, for example while firmware briefly assigns two BARs the same
// base, keeps working through the userspace MMIO exit until a later move
// frees its address.
type doorbellRegistry struct {
	mu      sync.Mutex
	vmFd    uintptr
	length  uint32
	devices []*deviceDoorbells
	closed  bool
}

type deviceDoorbells struct {
	dev    pci.CapsAndMMIO
	base   uint64
	mapped bool
	queues []doorbell
}

type doorbell struct {
	offset uint64
	file   *os.File
	fd     int // Kept separately: File.Fd would make the descriptor blocking.
	addr   uint64
	bound  bool
}

// add creates one eventfd per queue and a worker forwarding its kicks to the
// device. The bus reports the device's BAR when it is registered.
func (r *doorbellRegistry) add(dev virtioFunction, workers *sync.WaitGroup) error {
	offsets := dev.QueueDoorbells()
	doorbells := &deviceDoorbells{dev: dev}
	for _, offset := range offsets {
		// Nonblocking, so forwarders wait in Go's poller, not on OS threads.
		fd, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
		if err != nil {
			for _, b := range doorbells.queues {
				_ = b.file.Close()
			}

			return fmt.Errorf("create ioeventfd: %w", err)
		}
		doorbells.queues = append(doorbells.queues, doorbell{
			offset: offset, fd: fd, file: os.NewFile(uintptr(fd), "virtio-doorbell"),
		})
	}
	r.mu.Lock()
	r.devices = append(r.devices, doorbells)
	r.mu.Unlock()
	for queue, b := range doorbells.queues {
		workers.Go(func() { forwardDoorbell(b.file, dev, queue) })
	}

	return nil
}

func forwardDoorbell(file *os.File, dev virtioFunction, queue int) {
	// One read collects every kick since the last, like a coalesced Notify.
	var count [8]byte
	for {
		if _, err := file.Read(count[:]); err != nil {
			return
		}
		dev.NotifyQueue(queue)
	}
}

// moved is the PCI bus's MMIO BAR observer. It runs with the bus locked.
func (r *doorbellRegistry) moved(dev pci.CapsAndMMIO, base uint64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.closed {
		return
	}
	for _, doorbells := range r.devices {
		if doorbells.dev == dev {
			doorbells.base, doorbells.mapped = base, true
			r.reconcileLocked()

			return
		}
	}
}

// reconcileLocked unbinds doorbells their device has moved away from before
// binding any doorbell at its current address, because the addresses freed
// may be the ones another device is waiting for.
func (r *doorbellRegistry) reconcileLocked() {
	for _, doorbells := range r.devices {
		for i := range doorbells.queues {
			b := &doorbells.queues[i]
			if b.bound && b.addr != doorbells.base+b.offset {
				if err := r.set(b, b.addr, kvm.IOEventFDDeassign); err != nil {
					log.Printf("virtio doorbell %#x: unbind ioeventfd: %v", b.addr, err)
				}
				b.bound = false
			}
		}
	}
	for _, doorbells := range r.devices {
		if !doorbells.mapped {
			continue
		}
		for i := range doorbells.queues {
			b := &doorbells.queues[i]
			if b.bound {
				continue
			}
			addr := doorbells.base + b.offset
			err := r.set(b, addr, 0)
			if err == nil {
				b.addr, b.bound = addr, true
			} else if !errors.Is(err, syscall.EEXIST) {
				log.Printf("virtio doorbell %#x: bind ioeventfd: %v", addr, err)
			}
		}
	}
}

func (r *doorbellRegistry) set(b *doorbell, addr uint64, flags uint32) error {
	return kvm.SetIOEventFD(r.vmFd, &kvm.IOEventFD{Addr: addr, Len: r.length, FD: int32(b.fd), Flags: flags})
}

// close releases the eventfds, which ends the forwarders. KVM drops its
// bindings with the VM; none can fire once the vCPUs have stopped.
func (r *doorbellRegistry) close() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.closed = true
	var errs []error
	for _, doorbells := range r.devices {
		for _, b := range doorbells.queues {
			errs = append(errs, b.file.Close())
		}
	}
	r.devices = nil

	return errors.Join(errs...)
}
