package iodev

import "sync"

const (
	ps2DataPort   = uint64(0x60)
	ps2StatusPort = uint64(0x64)

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
	queue       []ps2Pending
	expect      ps2Expect

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
		if len(p.queue) > 0 {
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

// KeyEvent queues an XT Set 1 make/break sequence from an RFB keysym.
func (p *PS2Controller) KeyEvent(down bool, keysym uint32) {
	makeSeq, ok := ps2ScanCode(keysym)
	if !ok {
		return
	}

	p.mu.Lock()
	enabled := p.keyboardEnabled && p.keyboardScan
	p.mu.Unlock()

	if !enabled {
		return
	}

	seq := ps2KeySequence(makeSeq, down)
	p.enqueue(seq, false)
}

// PointerEvent converts an absolute VNC pointer update into PS/2 mouse packets.
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
	mouseID := p.mouseID
	p.mu.Unlock()

	p.enqueueMouseMotion(buttons, oldButtons, dx, dy, int(mouseID), ps2WheelDelta(buttonMask))
}

func (p *PS2Controller) writeCommand(cmd byte) {
	switch cmd {
	case 0x20: // Read controller command byte.
		p.enqueue([]byte{p.commandByte}, false)
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
	if p.mouseParam {
		paramCmd := p.mouseParamCmd
		p.mouseParam = false
		p.mouseParamCmd = 0
		if paramCmd == 0xf3 {
			p.recordMouseSampleLocked(cmd)
		}
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK}, true)

		return
	}
	p.mu.Unlock()

	switch cmd {
	case 0xff: // Reset.
		p.mu.Lock()
		p.mouseReporting = false
		p.mouseID = 0
		p.mouseSamples = [3]byte{}
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK, ps2BATPass, 0x00}, true)
	case 0xf2: // Get device ID: standard PS/2 mouse.
		p.mu.Lock()
		id := p.mouseID
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK, id}, true)
	case 0xf3, 0xe8: // Set sample rate / resolution, then parameter byte.
		p.mu.Lock()
		p.mouseParam = true
		p.mouseParamCmd = cmd
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK}, true)
	case 0xf4: // Enable data reporting.
		p.mu.Lock()
		p.mouseReporting = true
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK}, true)
	case 0xf5: // Disable data reporting.
		p.mu.Lock()
		p.mouseReporting = false
		p.mu.Unlock()
		p.enqueue([]byte{ps2ACK}, true)
	case 0xe9: // Status request.
		p.enqueue([]byte{ps2ACK, 0x00, 0x02, 100}, true)
	case 0xe6, 0xe7, 0xea, 0xec, 0xee, 0xf0, 0xf6:
		if cmd == 0xf6 {
			p.mu.Lock()
			p.mouseReporting = false
			p.mu.Unlock()
		}
		p.enqueue([]byte{ps2ACK}, true)
	case 0xeb: // Read data.
		p.mu.Lock()
		buttons := p.mouseButtons
		p.mu.Unlock()
		p.mu.Lock()
		mouseID := int(p.mouseID)
		p.mu.Unlock()
		p.enqueue(append([]byte{ps2ACK}, ps2MousePacket(buttons, 0, 0, mouseID, 0)...), true)
	default:
		p.enqueue([]byte{ps2Resend}, true)
	}
}

func (p *PS2Controller) popLocked() (byte, bool, bool) {
	if len(p.queue) == 0 {
		return 0, false, false
	}

	v := p.queue[0].data
	copy(p.queue, p.queue[1:])
	p.queue = p.queue[:len(p.queue)-1]

	if len(p.queue) == 0 {
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
	queueEmpty := len(p.queue) == 0
	for _, b := range data {
		p.queue = append(p.queue, ps2Pending{data: b, aux: aux})
	}

	inject := queueEmpty && p.irqEnabledLocked(p.queue[0].aux)
	p.mu.Unlock()

	if inject {
		p.injectIRQ(aux)
	}
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

func (p *PS2Controller) enqueueMouseMotion(buttons, oldButtons uint8, dx, dy int, mouseID, wheel int) {
	for dx != 0 || dy != 0 || wheel != 0 || buttons != oldButtons {
		chunkX := clampMouseDelta(dx)
		chunkY := clampMouseDelta(dy)
		chunkWheel := clampMouseWheel(wheel)
		p.enqueue(ps2MousePacket(buttons, chunkX, chunkY, mouseID, chunkWheel), true)

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
