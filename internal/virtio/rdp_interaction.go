package virtio

import (
	"sync"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

const rdpDragTimeout = 150 * time.Millisecond

// rdpInteraction belongs to one connection. Input wakes the writer when dragging
// changes its pacing; it never needs the framebuffer's pixel lock. The short
// timeout returns held-but-stationary pointers to normal pacing without polling.
type rdpInteraction struct {
	mu       sync.Mutex
	until    time.Time
	x, y     uint16
	absolute bool
	left     bool
	changed  chan struct{}
}

func newRDPInteraction() *rdpInteraction {
	return &rdpInteraction{changed: make(chan struct{}, 1)}
}

func (a *rdpInteraction) notification() <-chan struct{} {
	if a == nil {
		return nil
	}

	return a.changed
}

func (a *rdpInteraction) active() bool {
	if a == nil {
		return false
	}
	a.mu.Lock()
	defer a.mu.Unlock()

	return time.Now().Before(a.until)
}

func (a *rdpInteraction) observe(events []rdp.InputEvent) {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	wasActive := now.Before(a.until)
	for _, event := range events {
		var moved bool
		switch event.Kind {
		case rdp.InputPointer:
			moved = a.absolute && a.left && (event.X != a.x || event.Y != a.y)
			a.x, a.y, a.absolute = event.X, event.Y, true
		case rdp.InputRelativePointer:
			moved = event.DX != 0 || event.DY != 0
			a.absolute = false
		default:
			continue
		}
		a.left = event.Buttons&1 != 0
		if !a.left {
			a.until = time.Time{}
		} else if moved && event.Buttons&(8|16) == 0 {
			a.until = now.Add(rdpDragTimeout)
		}
	}
	if wasActive != now.Before(a.until) {
		select {
		case a.changed <- struct{}{}:
		default:
		}
	}
}
