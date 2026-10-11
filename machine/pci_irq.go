package machine

import (
	"sync"

	"github.com/define42/gokvm/kvm"
	"github.com/define42/gokvm/virtio"
)

// sharedIRQLine combines the independently asserted INTx signals of devices
// sharing one GSI. Acknowledging one device must not clear another's interrupt.
type sharedIRQLine struct {
	mu       sync.Mutex
	asserted int
	high     bool
	setLevel func(bool) error
}

func (line *sharedIRQLine) source() func(bool) error {
	var asserted bool

	return func(level bool) error {
		line.mu.Lock()
		defer line.mu.Unlock()
		if level != asserted {
			if level {
				line.asserted++
			} else {
				line.asserted--
			}
			asserted = level
		}
		high := line.asserted != 0
		if high == line.high {
			return nil
		}
		if err := line.setLevel(high); err != nil {
			return err
		}
		line.high = high

		return nil
	}
}

func (m *Machine) connectVirtioIRQ(transport *virtio.ModernTransport, irq uint8) {
	if !m.uefiIRQ {
		return
	}
	m.pciIRQMu.Lock()
	defer m.pciIRQMu.Unlock()
	if m.pciIRQLines == nil {
		m.pciIRQLines = make(map[uint8]*sharedIRQLine)
	}
	line := m.pciIRQLines[irq]
	if line == nil {
		line = &sharedIRQLine{setLevel: func(asserted bool) error {
			var level uint32
			if asserted {
				level = 1
			}

			return kvm.IRQLineStatus(m.vmFd, uint32(irq), level)
		}}
		m.pciIRQLines[irq] = line
	}
	transport.SetIRQLine(line.source())
}
