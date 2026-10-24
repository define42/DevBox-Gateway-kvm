//nolint:dupl
package kvm

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"syscall"
	"unsafe"
)

// irqLevel defines an IRQ as Level? Not sure.
type irqLevel struct {
	IRQ   uint32
	Level uint32
}

// IRQLines sets the interrupt line for an IRQ.
func IRQLineStatus(vmFd uintptr, irq, level uint32) error {
	irqLev := irqLevel{
		IRQ:   irq,
		Level: level,
	}
	_, err := Ioctl(vmFd,
		IIOWR(kvmIRQLineStatus, unsafe.Sizeof(irqLevel{})),
		uintptr(unsafe.Pointer(&irqLev)))

	return err
}

// CreateIRQChip creates an IRQ device (chip) to which to attach interrupts?
func CreateIRQChip(vmFd uintptr) error {
	_, err := Ioctl(vmFd, IIO(kvmCreateIRQChip), 0)

	return err
}

// pitConfig defines properties of a programmable interrupt timer.
type pitConfig struct {
	Flags uint32
	_     [15]uint32
}

// CreatePIT2 creates the in-kernel PIT with the speaker port at 0x61.
// Guests use that port to control channel 2 and read its output during
// TSC calibration, even when no audible speaker is attached.
func CreatePIT2(vmFd uintptr) error {
	pit := pitConfig{
		Flags: 1, // KVM_PIT_SPEAKER_DUMMY
	}
	_, err := Ioctl(vmFd,
		IIOW(kvmCreatePIT2, unsafe.Sizeof(pitConfig{})),
		uintptr(unsafe.Pointer(&pit)))

	return err
}

type PITChannelState struct {
	Count         uint32
	LatchedCount  uint16
	CountLatched  uint8
	StatusLatched uint8
	Status        uint8
	ReadState     uint8
	WriteState    uint8
	WriteLatch    uint8
	RWMode        uint8
	Mode          uint8
	BCD           uint8
	Gate          uint8
	CountLoadTime int64
}

type PITState2 struct {
	Channels [3]PITChannelState
	Flags    uint32
	_        [9]uint32
}

// GetPIT2 retrieves the state of the in-kernel PIT model. Only valid after KVM_CREATE_PIT2.
func GetPIT2(vmFd uintptr, pstate *PITState2) error {
	_, err := Ioctl(vmFd,
		IIOR(kvmGetPIT2, unsafe.Sizeof(PITState2{})),
		uintptr(unsafe.Pointer(pstate)))

	return err
}

// SetPIT2 sets the state of the in-kernel PIT model. Only valid after KVM_CREATE_PIT2.
func SetPIT2(vmFd uintptr, pstate *PITState2) error {
	_, err := Ioctl(vmFd,
		IIOW(kvmSetPIT2, unsafe.Sizeof(PITState2{})),
		uintptr(unsafe.Pointer(pstate)))

	return err
}

type PICState struct {
	LastIRR                uint8 /* edge detection */
	IRR                    uint8 /* interrupt request register */
	IMR                    uint8 /* interrupt mask register */
	ISR                    uint8 /* interrupt service register */
	PriorityAdd            uint8 /* highest irq priority */
	IRQBase                uint8
	ReadRegSelect          uint8
	Poll                   uint8
	SpecialMask            uint8
	InitState              uint8
	AutoEOI                uint8
	RotateOnAutoEOI        uint8
	SpecialFullyNestedMode uint8
	Init4                  uint8 /* true if 4 byte init */
	ELCR                   uint8 /* PIIX edge/trigger selection */
	ELCRMask               uint8
}

type IRQChip struct {
	ChipID uint32
	_      uint32
	Chip   [512]byte
}

// GetIRQChip reads the state of a kernel interrupt controller created with
// KVM_CREATE_IRQCHIP into a buffer provided by the caller.
func GetIRQChip(vmFd uintptr, irqc *IRQChip) error {
	_, err := Ioctl(vmFd,
		IIOWR(kvmGetIRQChip, unsafe.Sizeof(IRQChip{})),
		uintptr(unsafe.Pointer(irqc)))

	return err
}

// SetIRQChip sets the state of a kernel interrupt controller created with
// KVM_CREATE_IRQCHIP from a buffer provided by the caller.
func SetIRQChip(vmFd uintptr, irqc *IRQChip) error {
	_, err := Ioctl(vmFd,
		IIOR(kvmSetIRQChip, unsafe.Sizeof(IRQChip{})), uintptr(unsafe.Pointer(irqc)))

	return err
}

const (
	// Routing entry types (kvm_irq_routing_entry.type).
	IRQRouteIRQChip = 1
	IRQRouteMSI     = 2

	// In-kernel interrupt controllers (kvm_irq_routing_irqchip.irqchip).
	IRQChipPICMaster = 0
	IRQChipPICSlave  = 1
	IRQChipIOAPIC    = 2

	// IOAPICPins is the number of KVM IOAPIC inputs, and so of legacy GSIs.
	IOAPICPins = 24
)

type IRQRoutingIRQChip struct {
	IRQChip uint32
	Pin     uint32
}

// IRQRoutingEntry is struct kvm_irq_routing_entry. U is the kernel's 32-byte
// union: irqchip and pin for IRQRouteIRQChip, or address_lo, address_hi and
// data for IRQRouteMSI. Use NewIRQChipRoute and NewMSIRoute to fill it.
type IRQRoutingEntry struct {
	GSI   uint32
	Type  uint32
	Flags uint32
	_     uint32
	U     [8]uint32
}

// NewIRQChipRoute routes gsi to pin of an in-kernel interrupt controller.
func NewIRQChipRoute(gsi, irqChip, pin uint32) IRQRoutingEntry {
	return IRQRoutingEntry{GSI: gsi, Type: IRQRouteIRQChip, U: [8]uint32{irqChip, pin}}
}

// NewMSIRoute routes gsi to the MSI message address/data.
func NewMSIRoute(gsi uint32, address uint64, data uint32) IRQRoutingEntry {
	return IRQRoutingEntry{
		GSI:  gsi,
		Type: IRQRouteMSI,
		U:    [8]uint32{uint32(address), uint32(address >> 32), data},
	}
}

// DefaultIRQRoutes returns the routing KVM_CREATE_IRQCHIP installs on x86:
// GSIs 0-15 drive the IOAPIC pin and the PIC pin of the same number, GSIs
// 16-23 only the IOAPIC. KVM_SET_GSI_ROUTING replaces the whole table, so a
// table adding MSI routes must keep these for legacy line interrupts.
func DefaultIRQRoutes() []IRQRoutingEntry {
	routes := make([]IRQRoutingEntry, 0, IOAPICPins+16)
	for gsi := uint32(0); gsi < IOAPICPins; gsi++ {
		routes = append(routes, NewIRQChipRoute(gsi, IRQChipIOAPIC, gsi))
		if gsi < 16 {
			pic := uint32(IRQChipPICMaster)
			if gsi >= 8 {
				pic = IRQChipPICSlave
			}
			routes = append(routes, NewIRQChipRoute(gsi, pic, gsi%8))
		}
	}

	return routes
}

type IRQRouting struct {
	Nr      uint32
	Flags   uint32
	Entries []IRQRoutingEntry
}

func (r *IRQRouting) Bytes() ([]byte, error) {
	var buf bytes.Buffer

	if err := binary.Write(&buf, binary.LittleEndian, r.Nr); err != nil {
		return nil, err
	}

	if err := binary.Write(&buf, binary.LittleEndian, r.Flags); err != nil {
		return nil, err
	}

	for _, entry := range r.Entries {
		if err := binary.Write(&buf, binary.LittleEndian, entry); err != nil {
			return nil, err
		}
	}

	return buf.Bytes(), nil
}

func NewIRQRouting(data []byte) (*IRQRouting, error) {
	r := IRQRouting{}

	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, data); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	if err := binary.Read(&buf, binary.LittleEndian, &r.Nr); err != nil {
		return nil, err
	}

	if err := binary.Read(&buf, binary.LittleEndian, &r.Flags); err != nil {
		return nil, err
	}

	r.Entries = make([]IRQRoutingEntry, r.Nr)

	if err := binary.Read(&buf, binary.LittleEndian, &r.Entries); err != nil && !errors.Is(err, io.EOF) {
		return nil, err
	}

	return &r, nil
}

// SetGSIRouting sets the GSI routing table entries, overwriting any previously set entries.
// Nr entries are passed to KVM, so Nr must not exceed len(Entries).
func SetGSIRouting(vmFd uintptr, irqR *IRQRouting) error {
	if int(irqR.Nr) > len(irqR.Entries) {
		return syscall.EINVAL
	}

	data, err := irqR.Bytes()
	if err != nil {
		return err
	}

	// The ioctl encodes sizeof(struct kvm_irq_routing): the nr and flags
	// header that precedes the flexible entry array.
	_, err = Ioctl(vmFd,
		IIOW(kvmSetGSIRouting, unsafe.Sizeof([2]uint32{})),
		uintptr(unsafe.Pointer(&data[0])))

	return err
}

// InjectInterrupt queues a hardware interrupt vector to be injected.
func InjectInterrupt(vcpuFd uintptr, intr uint32) error {
	_, err := Ioctl(vcpuFd,
		IIOW(kvmInterrupt, 4),
		uintptr(intr))

	return err
}

const LAPICRegSize = 0x400

type LAPICState struct {
	Regs [LAPICRegSize]byte
}

// GetLocalAPIC reads the Local APIC registers and copies them into the input argument.
func GetLocalAPIC(vcpuFd uintptr, lapic *LAPICState) error {
	_, err := Ioctl(vcpuFd,
		IIOR(kvmGetLAPIC, unsafe.Sizeof(LAPICState{})),
		uintptr(unsafe.Pointer(lapic)))

	return err
}

// SetLocalAPIC copies the input argument into the Local APIC registers.
func SetLocalAPIC(vcpuFd uintptr, lapic *LAPICState) error {
	_, err := Ioctl(vcpuFd,
		IIOW(kvmSetLAPIC, unsafe.Sizeof(LAPICState{})),
		uintptr(unsafe.Pointer(lapic)))

	return err
}

// ReinjectControl sets i8254 Inject mode.
func ReinjectControl(vmFd uintptr, mode uint8) error {
	tmp := struct {
		pitReinject uint8
		_           [31]byte
	}{
		pitReinject: mode,
	}
	_, err := Ioctl(vmFd,
		IIO(kvmReinjectControl), uintptr(unsafe.Pointer(&tmp)))

	return err
}
