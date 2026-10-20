package iodev

import "sync"

const (
	ps2DataPort   = uint64(0x60)
	ps2StatusPort = uint64(0x64)

	ps2QueueCapacity = 1024
	// Leave room for deferred mouse state plus a complete command reply.
	ps2InputQueueLimit = ps2QueueCapacity - 16

	ps2ACK     = byte(0xfa)
	ps2BATPass = byte(0xaa)
	ps2Resend  = byte(0xfe)

	ps2StatusOutputFull = byte(1 << 0)
	ps2StatusSystemFlag = byte(1 << 2)
	ps2StatusAuxData    = byte(1 << 5)

	ps2CmdKeyboardIRQ = byte(1 << 0)
	ps2CmdMouseIRQ    = byte(1 << 1)
	ps2CmdSystemFlag  = byte(1 << 2)
	ps2CmdKeyboardOff = byte(1 << 4)
	ps2CmdMouseOff    = byte(1 << 5)
	ps2CmdTranslate   = byte(1 << 6)
)

type ps2Pending struct {
	data byte
	aux  bool
}

type ps2Expect int

const (
	ps2ExpectNone ps2Expect = iota
	ps2ExpectControllerConfig
	ps2ExpectKeyboardParam
	ps2ExpectKeyboardOutput
	ps2ExpectMouseOutput
	ps2ExpectMouseCommand
)

// PS2Controller is a small 8042-compatible keyboard/mouse controller.
type PS2Controller struct {
	mu sync.Mutex

	commandByte byte
	queue       [ps2QueueCapacity]ps2Pending
	queueLen    int
	expect      ps2Expect

	// Every accepted make reserves its complete break sequence. Other traffic
	// cannot consume that space, even when the guest stops reading input.
	keysDown        [256]bool
	keyReleaseBytes int
	// Motion can be dropped under pressure; the latest button state must still
	// be delivered. This fixed slot is flushed as soon as a whole packet fits.
	mousePending    [4]byte
	mousePendingLen int

	keyboardEnabled bool
	keyboardScan    bool
	mouseEnabled    bool
	mouseReporting  bool
	mouseParam      bool
	mouseParamCmd   byte
	mouseID         byte
	mouseSamples    [3]byte

	mouseButtons uint8
	mouseX       uint16
	mouseY       uint16
	mouseInit    bool

	injectKeyboardIRQ func() error
	injectMouseIRQ    func() error
}

// NewPS2Controller returns an i8042 controller with keyboard and aux ports.
func NewPS2Controller(injectKeyboardIRQ, injectMouseIRQ func() error) *PS2Controller {
	return &PS2Controller{
		commandByte:       ps2CmdKeyboardIRQ | ps2CmdMouseIRQ | ps2CmdSystemFlag | ps2CmdTranslate,
		keyboardEnabled:   true,
		keyboardScan:      true,
		mouseEnabled:      true,
		injectKeyboardIRQ: injectKeyboardIRQ,
		injectMouseIRQ:    injectMouseIRQ,
	}
}

func (p *PS2Controller) Read(port uint64, data []byte) error {
	if len(data) != 1 {
		return errDataLenInvalid
	}

	var (
		injectNext bool
		nextAux    bool
	)

	p.mu.Lock()

	switch port {
	case ps2DataPort:
		data[0], injectNext, nextAux = p.popLocked()
	case ps2StatusPort:
		data[0] = ps2StatusSystemFlag
		if p.queueLen > 0 {
			data[0] |= ps2StatusOutputFull
			if p.queue[0].aux {
				data[0] |= ps2StatusAuxData
			}
		}
	default:
		data[0] = 0
	}

	p.mu.Unlock()

	if injectNext {
		p.injectIRQ(nextAux)
	}

	return nil
}

func (p *PS2Controller) Write(port uint64, data []byte) error {
	if len(data) != 1 {
		return errDataLenInvalid
	}

	switch port {
	case ps2DataPort:
		p.writeData(data[0])
	case ps2StatusPort:
		p.writeCommand(data[0])
	default:
	}

	return nil
}

func (p *PS2Controller) IOPort() uint64 {
	return ps2DataPort
}

func (p *PS2Controller) Size() uint64 {
	return 0x5
}

// KeyEvent queues an XT Set 1 make/break sequence from an X11 keysym.
func (p *PS2Controller) KeyEvent(down bool, keysym uint32) {
	makeSeq, ok := ps2ScanCode(keysym)
	if !ok {
		return
	}

	seq := ps2KeySequence(makeSeq, down)
	key := int(makeSeq.code)
	if makeSeq.extended {
		key |= 0x80
	}
	p.mu.Lock()
	if !p.keyboardEnabled || !p.keyboardScan {
		p.mu.Unlock()

		return
	}

	queueEmpty := p.queueLen == 0
	queued := false
	switch {
	case !down && p.keysDown[key]:
		// This space was reserved with the make. A deferred mouse packet may
		// consume ordinary admission capacity but cannot displace this break.
		p.keyReleaseBytes -= len(seq)
		p.keysDown[key] = false
		p.appendLocked(seq, false)
		queued = true
	case down && !p.keysDown[key]:
		p.keyReleaseBytes += len(seq)
		queued = p.enqueueLocked(seq, false, ps2InputQueueLimit)
		if queued {
			p.keysDown[key] = true
		} else {
			p.keyReleaseBytes -= len(seq)
		}
	default:
		queued = p.enqueueLocked(seq, false, ps2InputQueueLimit)
	}
	inject := queued && queueEmpty && p.irqEnabledLocked(false)
	p.mu.Unlock()
	if inject {
		p.injectIRQ(false)
	}
}

// PointerEvent converts an absolute pointer update into PS/2 mouse packets.
func (p *PS2Controller) PointerEvent(buttonMask uint8, x, y uint16) {
	p.mu.Lock()
	if !p.mouseEnabled || !p.mouseReporting {
		p.mouseX, p.mouseY = x, y
		p.mouseInit = true
		p.mu.Unlock()

		return
	}

	dx, dy := 0, 0
	if p.mouseInit {
		dx = int(x) - int(p.mouseX)
		dy = int(p.mouseY) - int(y)
	}

	p.mouseX, p.mouseY = x, y
	p.mouseInit = true

	buttons := ps2Buttons(buttonMask)
	oldButtons := p.mouseButtons
	p.mouseButtons = buttons
	queueEmpty := p.queueLen == 0
	p.enqueueMouseMotionLocked(buttons, oldButtons, dx, dy, int(p.mouseID), ps2WheelDelta(buttonMask))
	inject := queueEmpty && p.queueLen > 0 && p.irqEnabledLocked(true)
	p.mu.Unlock()

	if inject {
		p.injectIRQ(true)
	}
}

// RelativePointerEvent delivers captured mouse deltas without using desktop
// coordinates. Positive Y points down, opposite to the PS/2 packet convention.
// The next absolute event establishes a fresh baseline after capture ends.
func (p *PS2Controller) RelativePointerEvent(buttonMask uint8, dx, dy int16) {
	p.mu.Lock()
	p.mouseInit = false
	if !p.mouseEnabled || !p.mouseReporting {
		p.mu.Unlock()

		return
	}
	buttons := ps2Buttons(buttonMask)
	oldButtons := p.mouseButtons
	p.mouseButtons = buttons
	queueEmpty := p.queueLen == 0
	// Widen before negating so the minimum int16 Y delta remains positive.
	p.enqueueMouseMotionLocked(buttons, oldButtons, int(dx), -int(dy), int(p.mouseID), ps2WheelDelta(buttonMask))
	inject := queueEmpty && p.queueLen > 0 && p.irqEnabledLocked(true)
	p.mu.Unlock()
	if inject {
		p.injectIRQ(true)
	}
}

func (p *PS2Controller) writeCommand(cmd byte) {
	switch cmd {
	case 0x20: // Read controller command byte.
		p.mu.Lock()
		commandByte := p.commandByte
		p.mu.Unlock()
		p.enqueue([]byte{commandByte}, false)
	case 0x60: // Write controller command byte.
		p.mu.Lock()
		p.expect = ps2ExpectControllerConfig
		p.mu.Unlock()
	case 0xaa: // Controller self-test.
		p.enqueue([]byte{0x55}, false)
	case 0xab, 0xa9: // Test keyboard/aux port.
		p.enqueue([]byte{0x00}, false)
	case 0xad: // Disable keyboard port.
		p.mu.Lock()
		p.keyboardEnabled = false
		p.commandByte |= ps2CmdKeyboardOff
		p.mu.Unlock()
	case 0xae: // Enable keyboard port.
		p.mu.Lock()
		p.keyboardEnabled = true
		p.commandByte &^= ps2CmdKeyboardOff
		p.mu.Unlock()
	case 0xa7: // Disable aux port.
		p.mu.Lock()
		p.mouseEnabled = false
		p.commandByte |= ps2CmdMouseOff
		p.mu.Unlock()
	case 0xa8: // Enable aux port.
		p.mu.Lock()
		p.mouseEnabled = true
		p.commandByte &^= ps2CmdMouseOff
		p.mu.Unlock()
	case 0xd4: // Next data byte goes to aux device.
		p.mu.Lock()
		p.expect = ps2ExpectMouseCommand
		p.mu.Unlock()
	case 0xd2: // Next data byte appears in the keyboard output buffer.
		p.mu.Lock()
		p.expect = ps2ExpectKeyboardOutput
		p.mu.Unlock()
	case 0xd3: // Next data byte appears in the aux output buffer.
		p.mu.Lock()
		p.expect = ps2ExpectMouseOutput
		p.mu.Unlock()
	default:
	}
}

func (p *PS2Controller) writeData(v byte) {
	p.mu.Lock()
	expect := p.expect
	p.expect = ps2ExpectNone
	p.mu.Unlock()

	switch expect {
	case ps2ExpectControllerConfig:
		p.mu.Lock()
		p.commandByte = v
		p.keyboardEnabled = v&ps2CmdKeyboardOff == 0
		p.mouseEnabled = v&ps2CmdMouseOff == 0
		p.mu.Unlock()
	case ps2ExpectKeyboardParam:
		p.enqueue([]byte{ps2ACK}, false)
	case ps2ExpectKeyboardOutput:
		p.enqueue([]byte{v}, false)
	case ps2ExpectMouseOutput:
		p.enqueue([]byte{v}, true)
	case ps2ExpectMouseCommand:
		p.handleMouseCommand(v)
	default:
		p.handleKeyboardCommand(v)
	}
}

func (p *PS2Controller) handleKeyboardCommand(cmd byte) {
	switch cmd {
	case 0xff: // Reset.
		p.enqueue([]byte{ps2ACK, ps2BATPass}, false)
	case 0xf2: // Identify MF2 keyboard.
		p.enqueue([]byte{ps2ACK, 0xab, 0x83}, false)
	case 0xf3, 0xed, 0xf0: // Commands followed by one parameter byte.
		p.mu.Lock()
		p.expect = ps2ExpectKeyboardParam
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK}, false)
	case 0xf4: // Enable scanning.
		p.mu.Lock()
		p.keyboardScan = true
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK}, false)
	case 0xf5: // Disable scanning.
		p.mu.Lock()
		p.keyboardScan = false
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK}, false)
	case 0xee: // Echo.
		p.enqueue([]byte{0xee}, false)
	default:
		p.enqueue([]byte{ps2ACK}, false)
	}
}

func (p *PS2Controller) handleMouseCommand(cmd byte) {
	p.mu.Lock()
	reply := p.mouseReplyLocked(cmd)
	reset := !p.mouseParam && cmd == 0xff
	pending := p.mousePendingLen
	if reset {
		pending = 0
	}
	// Commit the deferred packet, reply and mode change together. If a guest
	// command flood leaves no room, ignore the whole command so it can retry;
	// never announce a new packet format before old-format bytes are queued.
	if pending+len(reply) > ps2QueueCapacity-p.queueLen-p.keyReleaseBytes {
		p.mu.Unlock()

		return
	}
	queueEmpty := p.queueLen == 0
	p.appendLocked(p.mousePending[:pending], true)
	p.mousePendingLen = 0
	if p.mouseParam {
		paramCmd := p.mouseParamCmd
		p.mouseParam = false
		p.mouseParamCmd = 0
		if paramCmd == 0xf3 {
			p.recordMouseSampleLocked(cmd)
		}
	} else {
		switch cmd {
		case 0xff:
			p.mouseReporting = false
			p.mouseID = 0
			p.mouseSamples = [3]byte{}
		case 0xf3, 0xe8:
			p.mouseParam = true
			p.mouseParamCmd = cmd
		case 0xf4:
			p.mouseReporting = true
		case 0xf5, 0xf6:
			p.mouseReporting = false
		default:
		}
	}
	p.appendLocked(reply, true)
	inject := queueEmpty && p.irqEnabledLocked(true)
	p.mu.Unlock()
	if inject {
		p.injectIRQ(true)
	}
}

func (p *PS2Controller) mouseReplyLocked(cmd byte) []byte {
	if p.mouseParam {
		return []byte{ps2ACK}
	}
	switch cmd {
	case 0xff: // Reset.
		return []byte{ps2ACK, ps2BATPass, 0x00}
	case 0xf2: // Get device ID.
		return []byte{ps2ACK, p.mouseID}
	case 0xe9: // Status request.
		return []byte{ps2ACK, 0x00, 0x02, 100}
	case 0xeb: // Read data.
		return append([]byte{ps2ACK}, ps2MousePacket(p.mouseButtons, 0, 0, int(p.mouseID), 0)...)
	case 0xf3, 0xe8, 0xf4, 0xf5, 0xe6, 0xe7, 0xea, 0xec, 0xee, 0xf0, 0xf6:
		return []byte{ps2ACK}
	default:
		return []byte{ps2Resend}
	}
}

func (p *PS2Controller) popLocked() (byte, bool, bool) {
	if p.queueLen == 0 {
		return 0, false, false
	}

	v := p.queue[0].data
	copy(p.queue[:p.queueLen-1], p.queue[1:p.queueLen])
	p.queueLen--
	p.queue[p.queueLen] = ps2Pending{}
	p.flushMouseLocked()

	if p.queueLen == 0 {
		return v, false, false
	}

	nextAux := p.queue[0].aux

	return v, p.irqEnabledLocked(nextAux), nextAux
}

func (p *PS2Controller) enqueue(data []byte, aux bool) {
	if len(data) == 0 {
		return
	}

	p.mu.Lock()
	queueEmpty := p.queueLen == 0
	queued := p.enqueueLocked(data, aux, ps2QueueCapacity)
	inject := queued && queueEmpty && p.irqEnabledLocked(aux)
	p.mu.Unlock()

	if inject {
		p.injectIRQ(aux)
	}
}

// enqueueLocked admits a whole packet, including space already promised to
// key releases and deferred mouse state. No partially read data is evicted.
func (p *PS2Controller) enqueueLocked(data []byte, aux bool, limit int) bool {
	if len(data) > limit-p.queueLen-p.keyReleaseBytes-p.mousePendingLen {
		return false
	}
	p.appendLocked(data, aux)

	return true
}

// appendLocked is called only after capacity is checked or reserved.
func (p *PS2Controller) appendLocked(data []byte, aux bool) {
	for _, b := range data {
		p.queue[p.queueLen] = ps2Pending{data: b, aux: aux}
		p.queueLen++
	}
}

func (p *PS2Controller) flushMouseLocked() {
	if p.mousePendingLen == 0 ||
		p.queueLen+p.keyReleaseBytes+p.mousePendingLen > ps2InputQueueLimit {
		return
	}
	p.appendLocked(p.mousePending[:p.mousePendingLen], true)
	p.mousePendingLen = 0
}

func (p *PS2Controller) irqEnabledLocked(aux bool) bool {
	if aux {
		return p.commandByte&ps2CmdMouseIRQ != 0
	}

	return p.commandByte&ps2CmdKeyboardIRQ != 0
}

func (p *PS2Controller) injectIRQ(aux bool) {
	if aux {
		if p.injectMouseIRQ != nil {
			_ = p.injectMouseIRQ()
		}

		return
	}

	if p.injectKeyboardIRQ != nil {
		_ = p.injectKeyboardIRQ()
	}
}

func (p *PS2Controller) recordMouseSampleLocked(sample byte) {
	p.mouseSamples[0] = p.mouseSamples[1]
	p.mouseSamples[1] = p.mouseSamples[2]
	p.mouseSamples[2] = sample

	switch p.mouseSamples {
	case [3]byte{200, 100, 80}:
		p.mouseID = 3
	case [3]byte{200, 200, 80}:
		p.mouseID = 4
	}
}

func (p *PS2Controller) enqueueMouseMotionLocked(buttons, oldButtons uint8, dx, dy int, mouseID, wheel int) {
	for dx != 0 || dy != 0 || wheel != 0 || buttons != oldButtons {
		chunkX := clampMouseDelta(dx)
		chunkY := clampMouseDelta(dy)
		chunkWheel := clampMouseWheel(wheel)
		// A newer packet supersedes any deferred button state. If it cannot
		// fit, retain only its zero-motion state and stop expanding movement.
		p.mousePendingLen = 0
		if !p.enqueueLocked(ps2MousePacket(buttons, chunkX, chunkY, mouseID, chunkWheel), true, ps2InputQueueLimit) {
			p.mousePendingLen = copy(p.mousePending[:], ps2MousePacket(buttons, 0, 0, mouseID, 0))

			return
		}

		dx -= chunkX
		dy -= chunkY
		wheel -= chunkWheel
		oldButtons = buttons
	}
}

func ps2MousePacket(buttons uint8, dx, dy int, mouseID, wheel int) []byte {
	flags := 0x08 | buttons&0x07
	if dx < 0 {
		flags |= 0x10
	}
	if dy < 0 {
		flags |= 0x20
	}

	packet := []byte{flags, byte(int8(dx)), byte(int8(dy))}
	if mouseID == 3 || mouseID == 4 {
		packet = append(packet, byte(int8(wheel)))
	}

	return packet
}

func ps2Buttons(mask uint8) uint8 {
	var buttons uint8

	if mask&0x01 != 0 {
		buttons |= 0x01 // left
	}
	if mask&0x04 != 0 {
		buttons |= 0x02 // right
	}
	if mask&0x02 != 0 {
		buttons |= 0x04 // middle
	}

	return buttons
}

func ps2WheelDelta(mask uint8) int {
	switch {
	case mask&0x08 != 0:
		return 1
	case mask&0x10 != 0:
		return -1
	default:
		return 0
	}
}

func clampMouseDelta(v int) int {
	if v > 127 {
		return 127
	}

	if v < -127 {
		return -127
	}

	return v
}

func clampMouseWheel(v int) int {
	if v > 7 {
		return 7
	}

	if v < -8 {
		return -8
	}

	return v
}
