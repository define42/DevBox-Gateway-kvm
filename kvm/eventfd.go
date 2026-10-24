package kvm

import "unsafe"

// IOEventFD is struct kvm_ioeventfd. While assigned, a guest write to Addr
// signals FD inside KVM instead of returning KVM_EXIT_MMIO or KVM_EXIT_IO.
type IOEventFD struct {
	DataMatch uint64
	Addr      uint64
	// Len is the matched access width: 1, 2, 4 or 8 bytes. Zero matches any
	// width, requires CapIOEventFDAnyLength and lets x86 KVM complete the
	// write on its fast MMIO path without decoding the instruction.
	Len   uint32
	FD    int32
	Flags uint32
	_     [36]uint8
}

const (
	// IOEventFDDataMatch signals only writes whose value equals DataMatch.
	IOEventFDDataMatch = 1 << 0
	// IOEventFDPIO matches port I/O instead of MMIO.
	IOEventFDPIO = 1 << 1
	// IOEventFDDeassign removes an entry assigned with the same fields.
	IOEventFDDeassign = 1 << 2
)

// SetIOEventFD assigns an ioeventfd, or removes one with IOEventFDDeassign.
// Assigning a second eventfd at an occupied address fails with EEXIST.
func SetIOEventFD(vmFd uintptr, e *IOEventFD) error {
	_, err := Ioctl(vmFd,
		IIOW(kvmIOEventFD, unsafe.Sizeof(IOEventFD{})),
		uintptr(unsafe.Pointer(e)))

	return err
}

// IRQFD is struct kvm_irqfd. While assigned, signalling FD injects the
// interrupt routed to GSI without a vCPU exit or a userspace ioctl.
type IRQFD struct {
	FD         uint32
	GSI        uint32
	Flags      uint32
	ResampleFD uint32
	_          [16]uint8
}

const (
	// IRQFDDeassign unbinds FD from GSI.
	IRQFDDeassign = 1 << 0
	// IRQFDResample emulates a level-triggered line acknowledged via ResampleFD.
	IRQFDResample = 1 << 1
)

// SetIRQFD binds an eventfd to a GSI, or unbinds it with IRQFDDeassign. A GSI
// without a route accepts the binding; delivery starts once it is routed.
func SetIRQFD(vmFd uintptr, f *IRQFD) error {
	_, err := Ioctl(vmFd,
		IIOW(kvmIRQFD, unsafe.Sizeof(IRQFD{})),
		uintptr(unsafe.Pointer(f)))

	return err
}
