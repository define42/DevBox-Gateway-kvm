package virtio

import "time"

// Idle devices retain a bounded watchdog for guest notifications that never
// arrive. Ordinary kicks wake immediately; outstanding interrupts retain each
// device's original recovery interval. This avoids millisecond polling of idle
// desktops without relying solely on guest notification behavior.
const deviceIdlePollInterval = 100 * time.Millisecond

type devicePoll struct {
	timer          *time.Timer
	activeInterval time.Duration
	delay          time.Duration
}

func newDevicePoll(activeInterval time.Duration) *devicePoll {
	return &devicePoll{
		timer:          time.NewTimer(deviceIdlePollInterval),
		activeInterval: activeInterval,
		delay:          deviceIdlePollInterval,
	}
}

// reset is called only by the device worker, after each wake and drain. A
// reusable timer avoids allocating on every kick or recovery attempt.
func (p *devicePoll) reset(active bool) {
	if active {
		p.delay = p.activeInterval
	} else {
		p.delay = min(p.delay*2, deviceIdlePollInterval)
	}
	p.timer.Reset(p.delay)
}

// interruptWake is only needed after backing off. While the timer is already
// fast, leave IRQ transitions coalesced in the buffered channel instead of
// scheduling the worker once more for every acknowledged receive packet.
func (p *devicePoll) interruptWake(t *ModernTransport) <-chan struct{} {
	if p.delay <= p.activeInterval {
		return nil
	}

	return t.irqWake
}
