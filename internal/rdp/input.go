package rdp

import (
	"encoding/binary"
	"errors"
	"fmt"
	"maps"
	"slices"
	"unicode/utf16"
)

var ErrInvalidInput = errors.New("invalid RDP input")

// maxHeldKeys bounds per-connection state, including arbitrary Unicode keysyms.
const maxHeldKeys = 256

// InputKind identifies a keyboard, absolute pointer, or relative pointer update.
type InputKind uint8

const (
	InputKey InputKind = iota
	InputPointer
	InputRelativePointer
)

// InputEvent uses X11 keysyms and pointer button bits for the guest input sink.
// Bits 0, 1, and 2 are left, middle, and right;
// bits 3 and 4 are momentary wheel up and down events. X/Y are absolute pixel
// positions; DX/DY are signed relative movement, without desktop scaling.
type InputEvent struct {
	Kind    InputKind
	Down    bool
	Key     uint32
	X, Y    uint16
	DX, DY  int16
	Buttons uint8
}

// InputDecoder maintains the input state for one activated RDP connection.
// It is used by the connection's reader goroutine and is not concurrency safe.
// Lock-state synchronization is ignored: the guest handles Caps/Num/Scroll Lock
// through their ordinary key events.
type InputDecoder struct {
	// AllowRelativePointer must reflect the session's negotiated input capability.
	AllowRelativePointer bool

	held        map[uint32]struct{}
	buttons     uint8
	x, y        uint16
	relative    bool
	wheel       int
	pause       bool
	unicodeHigh [2]uint16
}

// Decode accepts either a complete uncompressed slow-path Share Data PDU, or
// the fast-path header followed by event data with transport length bytes
// already removed by ReadPacket. Invalid packets leave the decoder unchanged.
// At most 256 distinct keys may be held at any point in a packet. Callers must
// accept input only after connection activation and close on a decode error.
func (d *InputDecoder) Decode(data []byte, fastPath bool) ([]InputEvent, error) {
	// Parse scalar state and packet-bounded events without changing held keys.
	next := *d
	var events []InputEvent
	var err error
	if fastPath {
		events, err = next.fastPath(data)
	} else {
		events, err = next.slowPath(data)
	}
	if err != nil {
		return nil, err
	}
	if err := next.applyKeys(events); err != nil {
		return nil, err
	}
	*d = next

	return events, nil
}

// applyKeys validates ordered changes before committing them. The overlay only
// retains keys whose state differs from the original, so it holds at most
// 2*maxHeldKeys entries even when a packet presses and releases many unique keys.
func (d *InputDecoder) applyKeys(events []InputEvent) error {
	heldCount := len(d.held)
	if heldCount > maxHeldKeys {
		return fmt.Errorf("%w: too many held keys", ErrInvalidInput)
	}
	changes := make(map[uint32]bool)
	for _, event := range events {
		if event.Kind != InputKey {
			continue
		}
		_, original := d.held[event.Key]
		held, changed := changes[event.Key]
		if !changed {
			held = original
		}
		if held == event.Down {
			continue
		}
		if event.Down {
			if heldCount == maxHeldKeys {
				return fmt.Errorf("%w: too many held keys", ErrInvalidInput)
			}
			heldCount++
		} else {
			heldCount--
		}
		if event.Down == original {
			delete(changes, event.Key)
		} else {
			changes[event.Key] = event.Down
		}
	}
	// No failure can occur after this point. Remove released keys first so the
	// committed map never temporarily exceeds the cap during a key replacement.
	for key, down := range changes {
		if !down {
			delete(d.held, key)
		}
	}
	if heldCount != 0 && d.held == nil {
		d.held = make(map[uint32]struct{}, heldCount)
	}
	for key, down := range changes {
		if down {
			d.held[key] = struct{}{}
		}
	}

	return nil
}

func (d *InputDecoder) fastPath(data []byte) ([]InputEvent, error) {
	if len(data) < 1 || data[0]&0xc3 != 0 {
		return nil, fmt.Errorf("%w: invalid fast-path input header", ErrInvalidInput)
	}
	count := int(data[0] >> 2 & 15)
	data = data[1:]
	if count == 0 {
		if len(data) == 0 {
			return nil, fmt.Errorf("%w: missing fast-path input event count", ErrInvalidInput)
		}
		count, data = int(data[0]), data[1:]
	}
	var events []InputEvent
	for range count {
		if len(data) == 0 {
			return nil, fmt.Errorf("%w: missing fast-path input event", ErrInvalidInput)
		}
		header := data[0]
		code, flags := header>>5, header&31
		data = data[1:]
		lengths := [...]int{1, 6, 6, 0, 2, 6, 4}
		if int(code) >= len(lengths) {
			return nil, fmt.Errorf("%w: unknown fast-path input event %d", ErrInvalidInput, code)
		}
		size := lengths[code]
		if len(data) < size {
			return nil, fmt.Errorf("%w: truncated fast-path input event %d", ErrInvalidInput, code)
		}
		switch code {
		case 0: // TS_FP_KEYBOARD_EVENT
			events = d.scan(events, flags&1 == 0, flags&2 != 0, flags&4 != 0, uint16(data[0]))
		case 1: // TS_FP_POINTER_EVENT
			events = d.pointer(events, binary.LittleEndian.Uint16(data),
				binary.LittleEndian.Uint16(data[2:]), binary.LittleEndian.Uint16(data[4:]))
		case 4: // TS_FP_UNICODE_KEYBOARD_EVENT
			events = d.unicode(events, flags&1 == 0, binary.LittleEndian.Uint16(data))
		case 5: // TS_FP_RELPOINTER_EVENT
			if flags != 0 {
				return nil, fmt.Errorf("%w: invalid relative pointer event header", ErrInvalidInput)
			}
			var err error
			events, err = d.relativePointer(events, binary.LittleEndian.Uint16(data),
				int16(binary.LittleEndian.Uint16(data[2:])), int16(binary.LittleEndian.Uint16(data[4:])))
			if err != nil {
				return nil, err
			}
		case 2, 3, 6:
			// Extended mouse buttons, synchronize and QoE timestamps are consumed
			// without forwarding: they have no equivalent in the guest input sink.
		}
		data = data[size:]
	}
	if len(data) != 0 {
		return nil, fmt.Errorf("%w: trailing fast-path input data", ErrInvalidInput)
	}

	return events, nil
}

func (d *InputDecoder) slowPath(data []byte) ([]InputEvent, error) {
	if len(data) < 22 || int(binary.LittleEndian.Uint16(data)) != len(data) ||
		binary.LittleEndian.Uint16(data[2:])&15 != 7 || data[14] != 28 || data[15] != 0 {
		return nil, fmt.Errorf("%w: invalid slow-path input PDU", ErrInvalidInput)
	}
	count := int(binary.LittleEndian.Uint16(data[18:]))
	data = data[22:]
	if count != len(data)/12 || len(data)%12 != 0 {
		return nil, fmt.Errorf("%w: invalid slow-path input event count", ErrInvalidInput)
	}
	var events []InputEvent
	for range count {
		kind := binary.LittleEndian.Uint16(data[4:])
		flags := binary.LittleEndian.Uint16(data[6:])
		code := binary.LittleEndian.Uint16(data[8:])
		switch kind {
		case 0, 2, 0x8002: // Synchronize, unused, extended mouse buttons.
		case 4:
			events = d.scan(events, flags&0x8000 == 0, flags&0x0100 != 0, flags&0x0200 != 0, code)
		case 5:
			events = d.unicode(events, flags&0x8000 == 0, code)
		case 0x8001:
			events = d.pointer(events, flags, code, binary.LittleEndian.Uint16(data[10:]))
		case 0x8004:
			var err error
			events, err = d.relativePointer(events, flags, int16(code), int16(binary.LittleEndian.Uint16(data[10:])))
			if err != nil {
				return nil, err
			}
		default:

			return nil, fmt.Errorf("%w: unknown slow-path input event %#x", ErrInvalidInput, kind)
		}
		data = data[12:]
	}

	return events, nil
}

func (d *InputDecoder) key(events []InputEvent, down bool, key uint32) []InputEvent {
	if key == 0 {
		return events
	}

	return append(events, InputEvent{Kind: InputKey, Down: down, Key: key})
}

func (d *InputDecoder) scan(events []InputEvent, down, extended, extended1 bool, code uint16) []InputEvent {
	if extended1 {
		// E1 CTRL is part of the Pause sequence; never leave a guest CTRL held.
		if code == 0x1d && down {
			d.pause = true
		}

		return events
	}
	if !extended && code == 0x45 && d.pause {
		if !down {
			d.pause = false
		}

		return d.key(events, down, 0xff13)
	}
	var key uint32
	if extended {
		key = map[uint16]uint32{
			0x1c: 0xff0d, 0x1d: 0xffe4, 0x35: '/', 0x37: 0xff61, 0x38: 0xffea,
			0x47: 0xff50, 0x48: 0xff52, 0x49: 0xff55, 0x4b: 0xff51,
			0x4d: 0xff53, 0x4f: 0xff57, 0x50: 0xff54, 0x51: 0xff56,
			0x52: 0xff63, 0x53: 0xffff, 0x5b: 0xffeb, 0x5c: 0xffec, 0x5d: 0xff67,
		}[code]
	} else {
		key = scanKeysym(code)
	}

	return d.key(events, down, key)
}

// Scancodes identify physical keys; modifiers are forwarded separately rather
// than applying the client keyboard layout to printable keysyms.
func scanKeysym(code uint16) uint32 {
	keys := [...]uint32{
		0x01: 0xff1b, 0x02: '1', 0x03: '2', 0x04: '3', 0x05: '4', 0x06: '5',
		0x07: '6', 0x08: '7', 0x09: '8', 0x0a: '9', 0x0b: '0', 0x0c: '-', 0x0d: '=',
		0x0e: 0xff08, 0x0f: 0xff09,
		0x10: 'q', 0x11: 'w', 0x12: 'e', 0x13: 'r', 0x14: 't', 0x15: 'y',
		0x16: 'u', 0x17: 'i', 0x18: 'o', 0x19: 'p', 0x1a: '[', 0x1b: ']',
		0x1c: 0xff0d, 0x1d: 0xffe3,
		0x1e: 'a', 0x1f: 's', 0x20: 'd', 0x21: 'f', 0x22: 'g', 0x23: 'h',
		0x24: 'j', 0x25: 'k', 0x26: 'l', 0x27: ';', 0x28: '\'', 0x29: '`',
		0x2a: 0xffe1, 0x2b: '\\', 0x2c: 'z', 0x2d: 'x', 0x2e: 'c', 0x2f: 'v',
		0x30: 'b', 0x31: 'n', 0x32: 'm', 0x33: ',', 0x34: '.', 0x35: '/',
		0x36: 0xffe2, 0x37: 0xffaa, 0x38: 0xffe9, 0x39: ' ', 0x3a: 0xffe5,
		0x3b: 0xffbe, 0x3c: 0xffbf, 0x3d: 0xffc0, 0x3e: 0xffc1, 0x3f: 0xffc2,
		0x40: 0xffc3, 0x41: 0xffc4, 0x42: 0xffc5, 0x43: 0xffc6, 0x44: 0xffc7,
		0x45: 0xff7f, 0x46: 0xff14,
		0x47: 0xffb7, 0x48: 0xffb8, 0x49: 0xffb9, 0x4a: 0xffad,
		0x4b: 0xffb4, 0x4c: 0xffb5, 0x4d: 0xffb6, 0x4e: 0xffab,
		0x4f: 0xffb1, 0x50: 0xffb2, 0x51: 0xffb3, 0x52: 0xffb0, 0x53: 0xffae,
		0x56: '<', 0x57: 0xffc8, 0x58: 0xffc9,
	}
	if int(code) < len(keys) {
		return keys[code]
	}

	return 0
}

func (d *InputDecoder) unicode(events []InputEvent, down bool, code uint16) []InputEvent {
	index := 0
	if !down {
		index = 1
	}
	if code >= 0xd800 && code <= 0xdbff {
		d.unicodeHigh[index] = code

		return events
	}
	r := rune(code)
	if code >= 0xdc00 && code <= 0xdfff {
		if d.unicodeHigh[index] == 0 {
			return events // Ignore an unmatched low surrogate.
		}
		r = utf16.DecodeRune(rune(d.unicodeHigh[index]), r)
	}
	d.unicodeHigh[index] = 0
	key := uint32(r)
	switch key {
	case 0x08, 0x09, 0x0d, 0x1b:
		key |= 0xff00
	case 0x7f:
		key = 0xffff
	default:
		if key > 0xff {
			key |= 0x01000000
		}
	}

	return d.key(events, down, key)
}

func (d *InputDecoder) pointer(events []InputEvent, flags, x, y uint16) []InputEvent {
	if flags&0x0400 != 0 {
		return events // Horizontal scrolling is not supported by the guest sink.
	}
	if flags&0x0200 != 0 {
		// Wheel coordinates are reserved by RDP; use the last pointer position.
		delta := int(flags & 0x01ff)
		if delta&0x0100 != 0 {
			delta -= 0x0200
		}
		d.wheel += delta
		for d.wheel >= 120 || d.wheel <= -120 {
			button := uint8(8)
			if d.wheel < 0 {
				button = 16
				d.wheel += 120
			} else {
				d.wheel -= 120
			}
			press, release := d.pointerState(), d.pointerState()
			press.Buttons |= button
			events = append(events, press, release)
		}

		return events
	}
	d.x, d.y, d.relative = x, y, false
	d.pointerButtons(flags)

	return append(events, d.pointerState())
}

func (d *InputDecoder) pointerButtons(flags uint16) {
	for _, button := range [...]struct {
		flag uint16
		mask uint8
	}{{0x1000, 1}, {0x2000, 4}, {0x4000, 2}} {
		if flags&button.flag != 0 {
			if flags&0x8000 != 0 {
				d.buttons |= button.mask
			} else {
				d.buttons &^= button.mask
			}
		}
	}
}

func (d *InputDecoder) relativePointer(events []InputEvent, flags uint16, dx, dy int16) ([]InputEvent, error) {
	if !d.AllowRelativePointer {
		return nil, fmt.Errorf("%w: relative pointer input was not negotiated", ErrInvalidInput)
	}
	// MS-RDPBCGR 2.2.8.1.1.3.1.1.7: relative events allow movement and
	// standard/extended button flags, but no wheel or reserved flag bits.
	const buttons = 0x7003
	if flags&^uint16(0xf803) != 0 || flags&0x8000 != 0 && flags&buttons == 0 {
		return nil, fmt.Errorf("%w: invalid relative pointer flags %#x", ErrInvalidInput, flags)
	}
	d.relative = true
	d.pointerButtons(flags)
	event := d.pointerState()
	if flags&0x0800 != 0 {
		event.DX, event.DY = dx, dy
	}
	// Extended buttons have no guest equivalent, but their packets may also
	// contain movement or standard button changes.
	return append(events, event), nil
}

func (d *InputDecoder) pointerState() InputEvent {
	if d.relative {
		return InputEvent{Kind: InputRelativePointer, Buttons: d.buttons}
	}

	return InputEvent{Kind: InputPointer, X: d.x, Y: d.y, Buttons: d.buttons}
}

// ReleaseAll clears held keys and mouse buttons when a connection closes so a
// disconnect during a drag or modifier press does not leave guest input stuck.
func (d *InputDecoder) ReleaseAll() []InputEvent {
	var events []InputEvent
	for _, key := range slices.Sorted(maps.Keys(d.held)) {
		events = append(events, InputEvent{Kind: InputKey, Key: key})
	}
	if d.buttons != 0 {
		d.buttons = 0
		events = append(events, d.pointerState())
	}
	*d = InputDecoder{AllowRelativePointer: d.AllowRelativePointer}

	return events
}
