package virtio

import "sync"

// FirmwareInput keeps the firmware keyboard and mouse available until the guest
// activates its absolute tablet. Keyboard events always use the firmware device.
// Pointer events go to exactly one device, avoiding relative-mouse acceleration
// and coordinate drift once the guest has initialized the tablet driver.
type FirmwareInput struct {
	mu       sync.Mutex
	fallback DisplayInput
	tablet   *InputDevice
	absolute bool
	buttons  uint8
	x, y     uint16
}

// NewFirmwareInput combines a firmware-compatible input device with a tablet.
// A fallback implementing RebasePointer(uint16, uint16) can resume relative input
// after a tablet reset without replaying the desktop's intervening movement.
func NewFirmwareInput(fallback DisplayInput, tablet *InputDevice) *FirmwareInput {
	return &FirmwareInput{fallback: fallback, tablet: tablet}
}

// KeyEvent preserves firmware keyboard input before and after the tablet handoff.
func (p *FirmwareInput) KeyEvent(down bool, keysym uint32) {
	if p.fallback != nil {
		p.fallback.KeyEvent(down, keysym)
	}
}

// PointerEvent uses the initial display geometry for legacy callers.
func (p *FirmwareInput) PointerEvent(buttons uint8, x, y uint16) {
	p.PointerEventInBounds(buttons, x, y, displayDefaultWidth, displayDefaultHeight)
}

// PointerEventInBounds preserves the framebuffer geometry for absolute input.
func (p *FirmwareInput) PointerEventInBounds(buttons uint8, x, y uint16, width, height int) {
	if width <= 0 || height <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.tablet != nil {
		// Serialize readiness and delivery with queue setup/device reset. The
		// transport releases commonMu before calling into the input device.
		p.tablet.writeMu.Lock()
		defer p.tablet.writeMu.Unlock()
		if p.tablet.pointerDriverReady() {
			if !p.absolute && p.buttons != 0 && p.fallback != nil {
				// Finish any firmware drag at its previous position, without
				// also moving the relative pointer to this absolute target.
				p.fallback.PointerEvent(0, p.x, p.y)
				p.buttons = 0
			}
			p.absolute = true
			p.tablet.PointerEventInBounds(buttons, x, y, width, height)

			return
		}
	}
	if p.absolute {
		if relative, ok := p.fallback.(interface{ RebasePointer(uint16, uint16) }); ok {
			relative.RebasePointer(x, y)
		}
		p.absolute = false
	}
	if p.fallback != nil {
		p.fallback.PointerEvent(buttons, x, y)
	}
	p.x, p.y, p.buttons = x, y, buttons&0x07
}

// pointerDriverReady requires writeMu to remain locked through event delivery.
// An enabled queue alone is insufficient: drivers publish buffers before setting
// DRIVER_OK, and a failed device must return to firmware input until reinitialized.
func (d *InputDevice) pointerDriverReady() bool {
	const driverOK, needsReset, failed = 4, 64, 128
	d.commonMu.RLock()
	ready := d.deviceStatus&driverOK != 0 && d.deviceStatus&(needsReset|failed) == 0 &&
		d.queues[inputEventQueue].enable == 1
	d.commonMu.RUnlock()
	if !ready {
		return false
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	q := d.VirtQueue[inputEventQueue]

	return !d.closed && d.kind == inputKindTablet && q != nil && q.Desc != nil && q.Avail != nil && q.Used != nil
}
