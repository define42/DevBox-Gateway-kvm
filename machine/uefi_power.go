package machine

import (
	"encoding/binary"
	"sync"

	"github.com/define42/gokvm/iodev"
)

// uefiPower provides the PIIX4 PM registers used by OVMF and the guest FADT.
// The PM timer runs independently of vCPU execution; S5 requests stop all CPUs.
type uefiPower struct {
	mu      sync.Mutex
	status  uint16
	enable  uint16
	control uint16
	timer   *iodev.ACPIPMTimer
	stop    func() error
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
	p.mu.Unlock()
	if shutdown {
		return p.stop()
	}

	return nil
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

	return nil
}
