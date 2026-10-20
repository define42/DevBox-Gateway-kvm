package virtio

import "sync"

// FirmwareInput keeps the firmware keyboard and mouse available until the guest
// activates its absolute tablet. Keyboard events always use the firmware device.
// Desktop positions use the tablet after its driver starts; captured relative
// motion uses the firmware mouse. Each event goes to exactly one device.
type FirmwareInput struct {
	mu            sync.Mutex
	fallback      DisplayInput
	tablet        *InputDevice
	absolute      bool
	relative      bool
	buttons       uint8
	x, y          uint16
	width, height int
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
				p.releaseFallbackPointerLocked()
			}
			p.absolute, p.relative = true, false
			p.x, p.y, p.buttons = x, y, buttons&0x07
			p.width, p.height = width, height
			p.tablet.PointerEventInBounds(buttons, x, y, width, height)

			return
		}
	}
	if p.absolute || p.relative {
		if relative, ok := p.fallback.(interface{ RebasePointer(uint16, uint16) }); ok {
			relative.RebasePointer(x, y)
		}
		p.absolute, p.relative = false, false
	}
	if p.fallback != nil {
		p.fallback.PointerEvent(buttons, x, y)
	}
	p.x, p.y, p.buttons = x, y, buttons&0x07
}

func (p *FirmwareInput) releaseFallbackPointerLocked() {
	if p.relative {
		if relative, ok := p.fallback.(RelativePointerInput); ok {
			relative.RelativePointerEvent(0, 0, 0)
		}

		return
	}
	p.fallback.PointerEvent(0, p.x, p.y)
}

// SupportsRelativePointer reports whether the firmware device can accept
// captured motion, so remote clients only negotiate it for usable sinks.
func (p *FirmwareInput) SupportsRelativePointer() bool {
	return supportsRelativePointer(p.fallback)
}

// RelativePointerEvent sends captured deltas to the firmware mouse. A held
// tablet button is released at its previous position before changing devices.
func (p *FirmwareInput) RelativePointerEvent(buttons uint8, dx, dy int16) {
	relative, ok := p.fallback.(RelativePointerInput)
	if !ok || !p.SupportsRelativePointer() {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.absolute && p.buttons != 0 && p.tablet != nil {
		p.tablet.writeMu.Lock()
		if p.tablet.pointerDriverReady() {
			p.tablet.PointerEventInBounds(0, p.x, p.y, p.width, p.height)
		}
		p.tablet.writeMu.Unlock()
	}
	p.absolute, p.relative = false, true
	p.buttons = buttons & 0x07
	relative.RelativePointerEvent(buttons, dx, dy)
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
