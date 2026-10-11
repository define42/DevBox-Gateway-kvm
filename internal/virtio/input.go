package virtio

import (
	"encoding/binary"
	"errors"
	"log"
	"sync"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

const (
	inputEventQueue  = 0
	inputStatusQueue = 1
	inputNumQueues   = 2

	// InputKeyboardMMIOBase and InputPointerMMIOBase are the guest-physical
	// bases of the two virtio-input memory BARs.
	InputKeyboardMMIOBase = 0xd003_0000
	InputPointerMMIOBase  = 0xd004_0000

	inputConfigLen = 136 // struct virtio_input_config
	inputUnionOff  = 8
	inputEventLen  = 8 // struct virtio_input_event

	virtioInputDeviceID = 18

	inputCfgUnset    = 0x00
	inputCfgIDName   = 0x01
	inputCfgIDSerial = 0x02
	inputCfgIDDevids = 0x03
	inputCfgPropBits = 0x10
	inputCfgEvBits   = 0x11
	inputCfgAbsInfo  = 0x12

	busVirtual = 0x06

	evSyn = 0x00
	evKey = 0x01
	evRel = 0x02
	evAbs = 0x03
	evRep = 0x14

	synReport = 0x00

	relWheel = 0x08
	relX     = 0x00
	relY     = 0x01

	absX = 0x00
	absY = 0x01

	inputAbsMax = 32767

	keyEsc        = 1
	key1          = 2
	key2          = 3
	key3          = 4
	key4          = 5
	key5          = 6
	key6          = 7
	key7          = 8
	key8          = 9
	key9          = 10
	key0          = 11
	keyMinus      = 12
	keyEqual      = 13
	keyBackspace  = 14
	keyTab        = 15
	keyQ          = 16
	keyW          = 17
	keyE          = 18
	keyR          = 19
	keyT          = 20
	keyY          = 21
	keyU          = 22
	keyI          = 23
	keyO          = 24
	keyP          = 25
	keyLeftBrace  = 26
	keyRightBrace = 27
	keyEnter      = 28
	keyLeftCtrl   = 29
	keyA          = 30
	keyS          = 31
	keyD          = 32
	keyF          = 33
	keyG          = 34
	keyH          = 35
	keyJ          = 36
	keyK          = 37
	keyL          = 38
	keySemicolon  = 39
	keyApostrophe = 40
	keyGrave      = 41
	keyLeftShift  = 42
	keyBackslash  = 43
	keyZ          = 44
	keyX          = 45
	keyC          = 46
	keyV          = 47
	keyB          = 48
	keyN          = 49
	keyM          = 50
	keyComma      = 51
	keyDot        = 52
	keySlash      = 53
	keyRightShift = 54
	keyLeftAlt    = 56
	keySpace      = 57
	keyCapsLock   = 58
	keyF1         = 59
	keyF2         = 60
	keyF3         = 61
	keyF4         = 62
	keyF5         = 63
	keyF6         = 64
	keyF7         = 65
	keyF8         = 66
	keyF9         = 67
	keyF10        = 68
	keyNumLock    = 69
	keyScrollLock = 70
	keyF11        = 87
	keyF12        = 88
	keyHome       = 102
	keyUp         = 103
	keyPageUp     = 104
	keyLeft       = 105
	keyRight      = 106
	keyEnd        = 107
	keyDown       = 108
	keyPageDown   = 109
	keyInsert     = 110
	keyDelete     = 111
	keyRightCtrl  = 97
	keyRightAlt   = 100
	keyLeftMeta   = 125
	keyRightMeta  = 126

	btnLeft   = 0x110
	btnRight  = 0x111
	btnMiddle = 0x112

	inputMaxPendingEvents = 4096
)

var errNoInputEvent = errors.New("no virtio-input event")

type inputKind int

const (
	inputKindKeyboard inputKind = iota
	inputKindPointer
	inputKindTablet
)

type inputEvent struct {
	typ   uint16
	code  uint16
	value int32
}

// InputDevice is a modern virtio-input keyboard, relative mouse, or absolute tablet.
var _ pci.CapsAndMMIO = (*InputDevice)(nil)

type InputDevice struct {
	*ModernTransport

	name     string
	kind     inputKind
	irq      uint8
	mmioBase uint32

	configSelect uint8
	configSubsel uint8

	VirtQueue    [inputNumQueues]*SplitQueue
	LastAvailIdx [inputNumQueues]uint16

	mu       sync.Mutex // Configuration selectors, queue mappings and pending events.
	closed   bool
	pending  []inputEvent
	buttons  uint8
	lastX    uint16
	lastY    uint16
	hasPoint bool

	kick      chan int
	done      chan struct{}
	closeOnce sync.Once
}

func (d *InputDevice) GetDeviceHeader() pci.DeviceHeader {
	return pci.DeviceHeader{
		// 0x1040 + virtio device id (18 = input). The 0x1052 id marks a
		// non-transitional device, so Linux uses the modern interface.
		DeviceID:            0x1040 + virtioInputDeviceID,
		VendorID:            0x1AF4,
		RevisionID:          1,
		ClassCode:           0x09, // Input device controller
		Subclass:            0x80, // Other input controller
		HeaderType:          0,
		SubsystemID:         0x1040 + virtioInputDeviceID,
		Command:             0x6,
		Status:              0x10,
		CapabilitiesPointer: capCommonAt,
		BAR: [6]uint32{
			d.mmioBase,
		},
		InterruptPin:  1,
		InterruptLine: d.irq,
	}
}

func (d *InputDevice) DeviceFeatures() uint64 { return 0 }

func (d *InputDevice) NumQueues() int { return inputNumQueues }

func (d *InputDevice) DeviceConfigLen() int { return inputConfigLen }

func (d *InputDevice) ReadDeviceConfig(offset uint64, data []byte) {
	cfg := d.configImage()
	zero(data)

	if offset >= uint64(len(cfg)) {
		return
	}

	end := offset + uint64(len(data))
	if end > uint64(len(cfg)) {
		end = uint64(len(cfg))
	}

	copy(data, cfg[offset:end])
}

func (d *InputDevice) WriteDeviceConfig(offset uint64, data []byte) {
	d.mu.Lock()
	defer d.mu.Unlock()

	for i, v := range data {
		switch offset + uint64(i) {
		case 0:
			d.configSelect = v
		case 1:
			d.configSubsel = v
		}
	}
}

func (d *InputDevice) QueueReady(idx int, q *SplitQueue) {
	if idx < 0 || idx >= inputNumQueues {
		return
	}
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()

		return
	}
	d.VirtQueue[idx] = q
	d.LastAvailIdx[idx] = 0
	d.mu.Unlock()

	if idx == inputEventQueue {
		_ = d.flushEvents()
	}
}

func (d *InputDevice) Notify(idx int) {
	select {
	case d.kick <- idx:
	default:
	}
}

func (d *InputDevice) IOThreadEntry() {
	log.Printf("virtio-input: %s IOThreadEntry started", d.name)

	ticker := time.NewTicker(1 * time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-d.done:
			log.Printf("virtio-input: %s IOThreadEntry received done signal", d.name)

			return
		case idx := <-d.kick:
			d.drain(idx)
		case <-ticker.C:
			d.drain(-1)
		}
	}
}

func (d *InputDevice) drain(idx int) {
	if idx == inputStatusQueue || idx < 0 {
		for d.drainStatusQueue() == nil {
		}
	}

	if idx == inputEventQueue || idx < 0 {
		for d.flushEvents() == nil {
		}
	}

	_ = d.ReinjectIfPending()
}

// KeyEvent satisfies VNCInput for keyboard devices.
func (d *InputDevice) KeyEvent(down bool, keysym uint32) {
	if d.kind != inputKindKeyboard {
		return
	}

	code, ok := inputKeyCode(keysym)
	if !ok {
		return
	}

	value := int32(0)
	if down {
		value = 1
	}

	d.enqueue(inputEvent{typ: evKey, code: code, value: value}, synEvent())
}

// PointerEvent satisfies VNCInput. Tablet callers should use PointerEventInBounds
// to supply the display dimensions; this legacy entry point assumes 1024x768.
func (d *InputDevice) PointerEvent(buttonMask uint8, x, y uint16) {
	if d.kind == inputKindTablet {
		d.PointerEventInBounds(buttonMask, x, y, 1024, 768)

		return
	}
	if d.kind != inputKindPointer {
		return
	}

	var events []inputEvent
	d.mu.Lock()
	if d.hasPoint {
		dx := int32(int(x) - int(d.lastX))
		dy := int32(int(y) - int(d.lastY))
		if dx != 0 {
			events = append(events, inputEvent{typ: evRel, code: relX, value: dx})
		}
		if dy != 0 {
			events = append(events, inputEvent{typ: evRel, code: relY, value: dy})
		}
	}

	d.lastX = x
	d.lastY = y
	d.hasPoint = true

	if d.buttons != buttonMask {
		old := d.buttons
		d.buttons = buttonMask
		events = append(events, buttonEvents(old, buttonMask)...)
		events = append(events, wheelEvents(old, buttonMask)...)
	}
	d.mu.Unlock()

	if len(events) == 0 {
		return
	}

	events = append(events, synEvent())
	d.enqueue(events...)
}

// PointerEventInBounds sends a pixel position in the current framebuffer to an
// absolute tablet. Relative mice keep their existing delta-based behavior.
func (d *InputDevice) PointerEventInBounds(buttonMask uint8, x, y uint16, width, height int) {
	if d.kind != inputKindTablet {
		d.PointerEvent(buttonMask, x, y)

		return
	}
	if width <= 0 || height <= 0 {
		return
	}

	// Complete movement before reporting button changes. Linux mousedev splits
	// large moves into PS/2 packets; a button in the same report would be pressed
	// on the first partial move, before the pointer reaches the intended target.
	events := []inputEvent{
		{typ: evAbs, code: absX, value: inputAbsolutePosition(x, width)},
		{typ: evAbs, code: absY, value: inputAbsolutePosition(y, height)},
		synEvent(),
	}
	d.mu.Lock()
	events = append(events, buttonEvents(d.buttons, buttonMask)...)
	events = append(events, wheelEvents(d.buttons, buttonMask)...)
	d.buttons = buttonMask
	if len(events) > 3 {
		events = append(events, synEvent())
	}
	d.enqueueLocked(events...)
	d.mu.Unlock()
	d.Notify(inputEventQueue)
}

func inputAbsolutePosition(position uint16, size int) int32 {
	// Map pixel centers rather than the outside edges. Linux mousedev scales an
	// absolute maximum to width (one past the last pixel); avoiding that value
	// prevents a one-pixel offset after reaching a screen edge in legacy Xvesa.
	return int32((2*min(int(position), size-1) + 1) * inputAbsMax / (2 * size))
}

func (d *InputDevice) Read(port uint64, bytes []byte) error { return nil }

func (d *InputDevice) Write(port uint64, bytes []byte) error { return nil }

func (d *InputDevice) IOPort() uint64 { return 0 }

func (d *InputDevice) Size() uint64 { return 0 }

// Reset abandons old guest queue mappings while excluding queue consumers.
func (d *InputDevice) Reset() {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.resetLocked()
}

func (d *InputDevice) resetLocked() {
	d.VirtQueue = [inputNumQueues]*SplitQueue{}
	d.LastAvailIdx = [inputNumQueues]uint16{}
	d.configSelect, d.configSubsel = 0, 0
	d.pending = nil
	d.buttons = 0
	d.lastX, d.lastY = 0, 0
	d.hasPoint = false
}

func (d *InputDevice) Close() error {
	d.closeOnce.Do(func() {
		d.mu.Lock()
		defer d.mu.Unlock()
		d.closed = true
		d.resetLocked()
		close(d.done)
	})

	return nil
}

func (d *InputDevice) enqueue(events ...inputEvent) {
	d.mu.Lock()
	d.enqueueLocked(events...)
	d.mu.Unlock()

	d.Notify(inputEventQueue)
}

func (d *InputDevice) enqueueLocked(events ...inputEvent) {
	if d.closed {
		return
	}
	if len(d.pending)+len(events) > inputMaxPendingEvents {
		drop := len(d.pending) + len(events) - inputMaxPendingEvents
		if drop > len(d.pending) {
			drop = len(d.pending)
		}
		d.pending = d.pending[drop:]
	}
	d.pending = append(d.pending, events...)
}

func (d *InputDevice) popPending() (inputEvent, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	return d.popPendingLocked()
}

func (d *InputDevice) popPendingLocked() (inputEvent, bool) {
	if len(d.pending) == 0 {
		return inputEvent{}, false
	}

	ev := d.pending[0]
	copy(d.pending, d.pending[1:])
	d.pending = d.pending[:len(d.pending)-1]

	return ev, true
}

func (d *InputDevice) flushEvents() error {
	d.mu.Lock()
	err := d.flushEventsLocked()
	d.mu.Unlock()
	if err != nil {
		return err
	}

	return d.Interrupt()
}

func (d *InputDevice) flushEventsLocked() error {
	q := d.VirtQueue[inputEventQueue]
	if q == nil {
		return ErrVQNotInit
	}

	if d.LastAvailIdx[inputEventQueue] == LoadU16(&q.Avail.Idx) {
		return errNoInputEvent
	}

	ev, ok := d.popPendingLocked()
	if !ok {
		return errNoInputEvent
	}

	head := q.Avail.Ring[d.LastAvailIdx[inputEventQueue]%QueueSize]
	usedLen, written := d.writeEvent(q, head, ev)
	if !written {
		d.pending = append([]inputEvent{ev}, d.pending...)

		return errNoInputEvent
	}

	uidx := LoadU16(&q.Used.Idx)
	q.Used.Ring[uidx%QueueSize].ID = uint32(head)
	q.Used.Ring[uidx%QueueSize].Len = usedLen
	StoreAddU16(&q.Used.Idx, 1)
	d.LastAvailIdx[inputEventQueue]++

	return nil
}

func (d *InputDevice) writeEvent(q *SplitQueue, head uint16, ev inputEvent) (uint32, bool) {
	var raw [inputEventLen]byte
	binary.LittleEndian.PutUint16(raw[0:], ev.typ)
	binary.LittleEndian.PutUint16(raw[2:], ev.code)
	binary.LittleEndian.PutUint32(raw[4:], uint32(ev.value))

	descID := head
	written := 0

	for {
		desc := q.Desc[descID]
		if desc.Flags&descFWrite != 0 {
			n := int(desc.Len)
			if n > len(raw)-written {
				n = len(raw) - written
			}

			end := desc.Addr + uint64(n)
			if n > 0 && guestmem.ValidRange(d.Mem, desc.Addr, uint64(n)) {
				copy(d.Mem[desc.Addr:end], raw[written:written+n])
				written += n
			}
		}

		if written == len(raw) {
			return inputEventLen, true
		}

		if desc.Flags&descFNext == 0 {
			break
		}

		descID = desc.Next
	}

	return uint32(written), false
}

func (d *InputDevice) drainStatusQueue() error {
	d.mu.Lock()
	err := d.drainStatusQueueLocked()
	d.mu.Unlock()
	if err != nil {
		return err
	}

	return d.Interrupt()
}

func (d *InputDevice) drainStatusQueueLocked() error {
	q := d.VirtQueue[inputStatusQueue]
	if q == nil {
		return ErrVQNotInit
	}

	if d.LastAvailIdx[inputStatusQueue] == LoadU16(&q.Avail.Idx) {
		return errNoInputEvent
	}

	for d.LastAvailIdx[inputStatusQueue] != LoadU16(&q.Avail.Idx) {
		head := q.Avail.Ring[d.LastAvailIdx[inputStatusQueue]%QueueSize]
		uidx := LoadU16(&q.Used.Idx)
		q.Used.Ring[uidx%QueueSize].ID = uint32(head)
		q.Used.Ring[uidx%QueueSize].Len = 0
		StoreAddU16(&q.Used.Idx, 1)
		d.LastAvailIdx[inputStatusQueue]++
	}

	return nil
}

func (d *InputDevice) configImage() [inputConfigLen]byte {
	d.mu.Lock()
	defer d.mu.Unlock()

	var cfg [inputConfigLen]byte
	cfg[0] = d.configSelect
	cfg[1] = d.configSubsel

	switch d.configSelect {
	case inputCfgIDName:
		cfg[2] = byte(copy(cfg[inputUnionOff:], d.name))
	case inputCfgIDSerial:
		cfg[2] = byte(copy(cfg[inputUnionOff:], "0"))
	case inputCfgIDDevids:
		cfg[2] = 8
		binary.LittleEndian.PutUint16(cfg[inputUnionOff:], busVirtual)
		binary.LittleEndian.PutUint16(cfg[inputUnionOff+2:], 0x1af4)
		switch d.kind {
		case inputKindKeyboard:
			binary.LittleEndian.PutUint16(cfg[inputUnionOff+4:], 0x0001)
		case inputKindTablet:
			binary.LittleEndian.PutUint16(cfg[inputUnionOff+4:], 0x0003)
		default:
			binary.LittleEndian.PutUint16(cfg[inputUnionOff+4:], 0x0002)
		}
		binary.LittleEndian.PutUint16(cfg[inputUnionOff+6:], 1)
	case inputCfgPropBits:
		// These pointers do not require touchscreen or touchpad properties.
	case inputCfgEvBits:
		bitmap := d.evBitmap(d.configSubsel)
		cfg[2] = inputBitmapSize(bitmap[:])
		copy(cfg[inputUnionOff:], bitmap[:])
	case inputCfgAbsInfo:
		if d.kind == inputKindTablet && (d.configSubsel == absX || d.configSubsel == absY) {
			cfg[2] = 20 // struct virtio_input_absinfo: min, max, fuzz, flat, resolution.
			binary.LittleEndian.PutUint32(cfg[inputUnionOff+4:], inputAbsMax)
		}
	case inputCfgUnset:
	default:
	}

	return cfg
}

func (d *InputDevice) evBitmap(subsel uint8) [128]byte {
	var bitmap [128]byte

	switch d.kind {
	case inputKindKeyboard:
		switch subsel {
		case evKey:
			for _, code := range supportedKeyboardKeys() {
				setInputBit(bitmap[:], int(code))
			}
		case evRep:
			setInputBit(bitmap[:], 0)
		}
	case inputKindPointer, inputKindTablet:
		switch subsel {
		case evKey:
			setInputBit(bitmap[:], btnLeft)
			setInputBit(bitmap[:], btnRight)
			setInputBit(bitmap[:], btnMiddle)
		case evRel:
			if d.kind == inputKindPointer {
				setInputBit(bitmap[:], relX)
				setInputBit(bitmap[:], relY)
			}
			setInputBit(bitmap[:], relWheel)
		case evAbs:
			if d.kind == inputKindTablet {
				setInputBit(bitmap[:], absX)
				setInputBit(bitmap[:], absY)
			}
		}
	}

	return bitmap
}

func NewInputKeyboard(irq uint8, inject func() error, mem []byte) *InputDevice {
	return newInputDevice("gokvm keyboard", inputKindKeyboard, irq, InputKeyboardMMIOBase, inject, mem)
}

func NewInputPointer(irq uint8, inject func() error, mem []byte) *InputDevice {
	return newInputDevice("gokvm mouse", inputKindPointer, irq, InputPointerMMIOBase, inject, mem)
}

// NewInputTablet creates an absolute pointer suitable for VNC and RDP input.
func NewInputTablet(irq uint8, inject func() error, mem []byte) *InputDevice {
	return newInputDevice("gokvm tablet", inputKindTablet, irq, InputPointerMMIOBase, inject, mem)
}

func newInputDevice(
	name string,
	kind inputKind,
	irq uint8,
	mmioBase uint32,
	inject func() error,
	mem []byte,
) *InputDevice {
	d := &InputDevice{
		name:     name,
		kind:     kind,
		irq:      irq,
		mmioBase: mmioBase,
		kick:     make(chan int, QueueSize),
		done:     make(chan struct{}),
	}

	d.ModernTransport = NewModernTransport(d, mem, inject)

	return d
}

// InputPair sends remote keyboard events to one virtio-input device and pointer
// events to another.
type InputPair struct {
	keyboard *InputDevice
	pointer  *InputDevice
}

func NewInputPair(keyboard, pointer *InputDevice) *InputPair {
	return &InputPair{keyboard: keyboard, pointer: pointer}
}

func (p *InputPair) KeyEvent(down bool, keysym uint32) {
	if p != nil && p.keyboard != nil {
		p.keyboard.KeyEvent(down, keysym)
	}
}

func (p *InputPair) PointerEvent(buttonMask uint8, x, y uint16) {
	if p != nil && p.pointer != nil {
		p.pointer.PointerEvent(buttonMask, x, y)
	}
}

// PointerEventInBounds preserves the framebuffer dimensions for absolute input.
func (p *InputPair) PointerEventInBounds(buttonMask uint8, x, y uint16, width, height int) {
	if p != nil && p.pointer != nil {
		p.pointer.PointerEventInBounds(buttonMask, x, y, width, height)
	}
}

func inputBitmapSize(bitmap []byte) byte {
	for i := len(bitmap) - 1; i >= 0; i-- {
		if bitmap[i] != 0 {
			return byte(i + 1)
		}
	}

	return 0
}

func setInputBit(bitmap []byte, bit int) {
	if bit < 0 || bit/8 >= len(bitmap) {
		return
	}

	bitmap[bit/8] |= 1 << (uint(bit) % 8)
}

func inputBitSet(bitmap []byte, bit int) bool {
	if bit < 0 || bit/8 >= len(bitmap) {
		return false
	}

	return bitmap[bit/8]&(1<<(uint(bit)%8)) != 0
}

func synEvent() inputEvent {
	return inputEvent{typ: evSyn, code: synReport}
}

func buttonEvents(oldMask, newMask uint8) []inputEvent {
	buttons := []struct {
		mask uint8
		code uint16
	}{
		{mask: 1 << 0, code: btnLeft},
		{mask: 1 << 1, code: btnMiddle},
		{mask: 1 << 2, code: btnRight},
	}

	events := make([]inputEvent, 0, len(buttons))
	for _, button := range buttons {
		if oldMask&button.mask == newMask&button.mask {
			continue
		}

		value := int32(0)
		if newMask&button.mask != 0 {
			value = 1
		}
		events = append(events, inputEvent{typ: evKey, code: button.code, value: value})
	}

	return events
}

func wheelEvents(oldMask, newMask uint8) []inputEvent {
	var events []inputEvent

	if oldMask&0x08 == 0 && newMask&0x08 != 0 {
		events = append(events, inputEvent{typ: evRel, code: relWheel, value: 1})
	}

	if oldMask&0x10 == 0 && newMask&0x10 != 0 {
		events = append(events, inputEvent{typ: evRel, code: relWheel, value: -1})
	}

	return events
}

func inputKeyCode(keysym uint32) (uint16, bool) {
	if keysym >= 'A' && keysym <= 'Z' {
		keysym += 'a' - 'A'
	}

	if keysym >= 0xffbe && keysym <= 0xffc9 {
		return inputFunctionKey(int(keysym - 0xffbe)), true
	}

	code, ok := map[uint32]uint16{
		0xff08: keyBackspace,
		0xff09: keyTab,
		0xff0d: keyEnter,
		0xff1b: keyEsc,
		0xffff: keyDelete,
		0xff50: keyHome,
		0xff51: keyLeft,
		0xff52: keyUp,
		0xff53: keyRight,
		0xff54: keyDown,
		0xff55: keyPageUp,
		0xff56: keyPageDown,
		0xff57: keyEnd,
		0xff63: keyInsert,
		0xffe1: keyLeftShift,
		0xffe2: keyRightShift,
		0xffe3: keyLeftCtrl,
		0xffe4: keyRightCtrl,
		0xffe9: keyLeftAlt,
		0xffea: keyRightAlt,
		0xffeb: keyLeftMeta,
		0xffec: keyRightMeta,
		0xffe5: keyCapsLock,
		0xff14: keyScrollLock,
		0xff7f: keyNumLock,
		'a':    keyA,
		'b':    keyB,
		'c':    keyC,
		'd':    keyD,
		'e':    keyE,
		'f':    keyF,
		'g':    keyG,
		'h':    keyH,
		'i':    keyI,
		'j':    keyJ,
		'k':    keyK,
		'l':    keyL,
		'm':    keyM,
		'n':    keyN,
		'o':    keyO,
		'p':    keyP,
		'q':    keyQ,
		'r':    keyR,
		's':    keyS,
		't':    keyT,
		'u':    keyU,
		'v':    keyV,
		'w':    keyW,
		'x':    keyX,
		'y':    keyY,
		'z':    keyZ,
		'1':    key1,
		'2':    key2,
		'3':    key3,
		'4':    key4,
		'5':    key5,
		'6':    key6,
		'7':    key7,
		'8':    key8,
		'9':    key9,
		'0':    key0,
		' ':    keySpace,
		'-':    keyMinus,
		'=':    keyEqual,
		'[':    keyLeftBrace,
		']':    keyRightBrace,
		'\\':   keyBackslash,
		';':    keySemicolon,
		'\'':   keyApostrophe,
		'`':    keyGrave,
		',':    keyComma,
		'.':    keyDot,
		'/':    keySlash,
		// VNC/RDP send modifier transitions separately. Shifted keysyms
		// still identify the same physical key on the US keyboard.
		'!': key1,
		'@': key2,
		'#': key3,
		'$': key4,
		'%': key5,
		'^': key6,
		'&': key7,
		'*': key8,
		'(': key9,
		')': key0,
		'_': keyMinus,
		'+': keyEqual,
		'{': keyLeftBrace,
		'}': keyRightBrace,
		'|': keyBackslash,
		':': keySemicolon,
		'"': keyApostrophe,
		'~': keyGrave,
		'<': keyComma,
		'>': keyDot,
		'?': keySlash,
	}[keysym]

	return code, ok
}

func inputFunctionKey(n int) uint16 {
	return []uint16{
		keyF1, keyF2, keyF3, keyF4, keyF5, keyF6,
		keyF7, keyF8, keyF9, keyF10, keyF11, keyF12,
	}[n]
}

func supportedKeyboardKeys() []uint16 {
	return []uint16{
		keyEsc, key1, key2, key3, key4, key5, key6, key7, key8, key9, key0,
		keyMinus, keyEqual, keyBackspace, keyTab,
		keyQ, keyW, keyE, keyR, keyT, keyY, keyU, keyI, keyO, keyP,
		keyLeftBrace, keyRightBrace, keyEnter, keyLeftCtrl,
		keyA, keyS, keyD, keyF, keyG, keyH, keyJ, keyK, keyL,
		keySemicolon, keyApostrophe, keyGrave, keyLeftShift, keyBackslash,
		keyZ, keyX, keyC, keyV, keyB, keyN, keyM, keyComma, keyDot,
		keySlash, keyRightShift, keyLeftAlt, keySpace, keyCapsLock,
		keyF1, keyF2, keyF3, keyF4, keyF5, keyF6, keyF7, keyF8, keyF9,
		keyF10, keyNumLock, keyScrollLock, keyF11, keyF12, keyHome, keyUp,
		keyPageUp, keyLeft, keyRight, keyEnd, keyDown, keyPageDown,
		keyInsert, keyDelete, keyRightCtrl, keyRightAlt, keyLeftMeta, keyRightMeta,
	}
}
