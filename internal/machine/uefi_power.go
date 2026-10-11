package machine

import (
	"encoding/binary"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"github.com/define42/DevBox-Gateway-kvm/internal/iodev"
)

// ErrShutdownUnsupported indicates that no ACPI power button is exposed.
var ErrShutdownUnsupported = errors.New("graceful shutdown requires UEFI ACPI power management")

// RequestShutdown presses the virtual power button. The guest's ACPI policy
// decides how to respond; success does not mean shutdown has completed. Call
// after guest setup. Unlike Stop, this does not interrupt vCPU execution.
func (m *Machine) RequestShutdown() error {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if atomic.LoadUint32(&m.stopped) != 0 {
		return ErrMachineStopped
	}
	if m.uefiPower == nil {
		return ErrShutdownUnsupported
	}

	return m.uefiPower.pressPowerButton()
}

// uefiPower provides the PIIX4 PM registers used by OVMF and the guest FADT.
// The PM timer runs independently of vCPU execution; S5 requests stop all CPUs.
type uefiPower struct {
	mu         sync.Mutex
	status     uint16
	enable     uint16
	control    uint16
	timer      *iodev.ACPIPMTimer
	stop       func() error
	irq        func(bool) error
	sciHigh    bool
	sciPending bool // Retry failed shared-line updates even if the desired level changes back.
}

func (p *uefiPower) IOPort() uint64 { return 0xb000 }
func (p *uefiPower) Size() uint64   { return 0x40 }

func (p *uefiPower) Read(port uint64, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	clear(data)
	if port == 0xb008 && len(data) == 4 {
		return p.timer.Read(port, data)
	}
	var registers [6]byte
	binary.LittleEndian.PutUint16(registers[:], p.status)
	binary.LittleEndian.PutUint16(registers[2:], p.enable)
	binary.LittleEndian.PutUint16(registers[4:], p.control)
	for i := range data {
		if addr := port + uint64(i); addr >= 0xb000 && addr < 0xb006 {
			data[i] = registers[addr-0xb000]
		}
	}

	return nil
}

func (p *uefiPower) Write(port uint64, data []byte) error {
	p.mu.Lock()
	for i, b := range data {
		addr := port + uint64(i)
		switch {
		case addr >= 0xb000 && addr < 0xb002:
			p.status &^= uint16(b) << (8 * (addr - 0xb000)) // PM1_STS is write-one-to-clear.
		case addr >= 0xb002 && addr < 0xb004:
			shift := 8 * (addr - 0xb002)
			p.enable = p.enable&^(0xff<<shift) | uint16(b)<<shift
		case addr >= 0xb004 && addr < 0xb006:
			shift := 8 * (addr - 0xb004)
			p.control = p.control&^(0xff<<shift) | uint16(b)<<shift
		}
	}
	// OVMF ResetShutdown writes SLP_TYP=0 and SLP_EN, matching QEMU PIIX4.
	shutdown := p.control&(1<<13) != 0 && (p.control>>10)&7 == 0
	p.control &^= 1 << 13 // SLP_EN is a write-only trigger.
	irqErr := p.updateSCILocked()
	p.mu.Unlock()
	if shutdown {
		return errors.Join(irqErr, p.stop())
	}

	return irqErr
}

func (p *uefiPower) smi(_ uint64, data []byte) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if len(data) > 0 {
		switch data[0] {
		case 0xf1:
			p.control |= 1
		case 0xf0:
			p.control &^= 1
		}
	}

	return p.updateSCILocked()
}

func (p *uefiPower) pressPowerButton() error {
	p.mu.Lock()
	defer p.mu.Unlock()
	// ACPI 6.5 section 4.8.2.2.1.1: the status is sticky regardless of
	// PWRBTN_EN. The OS clears it by writing one to PM1_STS bit 8.
	p.status |= 1 << 8

	return p.updateSCILocked()
}

func (p *uefiPower) updateSCILocked() error {
	high := p.control&1 != 0 && p.status&p.enable&(1<<8) != 0
	if p.irq == nil || (high == p.sciHigh && !p.sciPending) {
		return nil
	}
	if err := p.irq(high); err != nil {
		p.sciPending = true

		return fmt.Errorf("ACPI SCI level %t: %w", high, err)
	}
	p.sciHigh = high
	p.sciPending = false

	return nil
}
