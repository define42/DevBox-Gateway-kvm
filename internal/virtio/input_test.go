package virtio

import (
	"encoding/binary"
	"errors"
	"slices"
	"sync"
	"testing"
)

func TestInputGetDeviceHeader(t *testing.T) {
	t.Parallel()

	v := NewInputKeyboard(5, func() error { return nil }, nil)
	hdr := v.GetDeviceHeader()

	if hdr.DeviceID != 0x1052 {
		t.Fatalf("DeviceID: got 0x%x, want 0x1052", hdr.DeviceID)
	}

	if hdr.SubsystemID != 0x1040+virtioInputDeviceID {
		t.Fatalf("SubsystemID: got %d, want %d", hdr.SubsystemID, 0x1040+virtioInputDeviceID)
	}

	if hdr.ClassCode != 0x09 || hdr.Subclass != 0x80 {
		t.Fatalf("class: got %#x/%#x, want 0x09/0x80", hdr.ClassCode, hdr.Subclass)
	}

	if hdr.Status&0x10 == 0 {
		t.Fatal("capabilities-list status bit not set")
	}

	if hdr.BAR[0] != InputKeyboardMMIOBase {
		t.Fatalf("BAR0: got 0x%x, want 0x%x", hdr.BAR[0], InputKeyboardMMIOBase)
	}
}

func TestInputKeyboardConfig(t *testing.T) {
	t.Parallel()

	v := NewInputKeyboard(5, func() error { return nil }, nil)

	cfg := readInputConfig(v, inputCfgIDName, 0)
	if string(cfg[inputUnionOff:inputUnionOff+cfg[2]]) != "gokvm keyboard" {
		t.Fatalf("name: got %q", cfg[inputUnionOff:inputUnionOff+cfg[2]])
	}

	cfg = readInputConfig(v, inputCfgIDDevids, 0)
	if cfg[2] != 8 {
		t.Fatalf("devids size: got %d, want 8", cfg[2])
	}

	if bus := binary.LittleEndian.Uint16(cfg[inputUnionOff:]); bus != busVirtual {
		t.Fatalf("bus: got %d, want %d", bus, busVirtual)
	}

	cfg = readInputConfig(v, inputCfgEvBits, evKey)
	if !inputBitSet(cfg[inputUnionOff:], keyA) {
		t.Fatal("keyboard EV_KEY bitmap does not include KEY_A")
	}

	if !inputBitSet(cfg[inputUnionOff:], keyEnter) {
		t.Fatal("keyboard EV_KEY bitmap does not include KEY_ENTER")
	}

	if inputBitSet(cfg[inputUnionOff:], btnLeft) {
		t.Fatal("keyboard EV_KEY bitmap unexpectedly includes BTN_LEFT")
	}
}

func TestInputKeyboardDeliversEvents(t *testing.T) {
	t.Parallel()

	var interrupts int
	mem := make([]byte, 0x1000)
	v := NewInputKeyboard(5, func() error {
		interrupts++

		return nil
	}, mem)

	q := newInputSplitQueue()
	queueInputBuffer(q, 0, 0x100)
	queueInputBuffer(q, 1, 0x108)
	v.QueueReady(inputEventQueue, q)

	v.KeyEvent(true, 'a')

	if err := v.flushEvents(); err != nil {
		t.Fatalf("flush key event: %v", err)
	}

	if err := v.flushEvents(); err != nil {
		t.Fatalf("flush sync event: %v", err)
	}

	assertInputEvent(t, mem[0x100:0x108], evKey, keyA, 1)
	assertInputEvent(t, mem[0x108:0x110], evSyn, synReport, 0)

	if got := LoadU16(&q.Used.Idx); got != 2 {
		t.Fatalf("used idx: got %d, want 2", got)
	}

	if interrupts != 2 {
		t.Fatalf("interrupts: got %d, want 2", interrupts)
	}
}

func TestInputKeyboardShiftedPunctuation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		keysym uint32
		code   uint16
	}{
		{"exclamation", '!', key1},
		{"at", '@', key2},
		{"hash", '#', key3},
		{"dollar", '$', key4},
		{"percent", '%', key5},
		{"caret", '^', key6},
		{"ampersand", '&', key7},
		{"asterisk", '*', key8},
		{"left parenthesis", '(', key9},
		{"right parenthesis", ')', key0},
		{"underscore", '_', keyMinus},
		{"plus", '+', keyEqual},
		{"left brace", '{', keyLeftBrace},
		{"right brace", '}', keyRightBrace},
		{"pipe", '|', keyBackslash},
		{"colon", ':', keySemicolon},
		{"quote", '"', keyApostrophe},
		{"tilde", '~', keyGrave},
		{"less than", '<', keyComma},
		{"greater than", '>', keyDot},
		{"question", '?', keySlash},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := make([]byte, 0x1000)
			v := NewInputKeyboard(5, func() error { return nil }, mem)
			q := newInputSplitQueue()
			for i := range uint16(9) {
				queueInputBuffer(q, i, 0x100+uint64(i)*inputEventLen)
			}
			v.QueueReady(inputEventQueue, q)
			v.KeyEvent(true, 0xffe1) // Shift_L, supplied by the remote client.
			v.KeyEvent(true, tc.keysym)
			v.KeyEvent(false, tc.keysym)
			v.KeyEvent(false, 0xffe1)
			for i, ev := range []inputEvent{
				{typ: evKey, code: keyLeftShift, value: 1},
				synEvent(),
				{typ: evKey, code: tc.code, value: 1},
				synEvent(),
				{typ: evKey, code: tc.code, value: 0},
				synEvent(),
				{typ: evKey, code: keyLeftShift, value: 0},
				synEvent(),
			} {
				if err := v.flushEvents(); err != nil {
					t.Fatalf("flush event %d: %v", i, err)
				}
				addr := 0x100 + i*inputEventLen
				assertInputEvent(t, mem[addr:addr+inputEventLen], ev.typ, ev.code, ev.value)
			}
			if err := v.flushEvents(); !errors.Is(err, errNoInputEvent) || q.Used.Idx != 8 {
				t.Fatalf("shifted punctuation produced extra events: used=%d, error=%v", q.Used.Idx, err)
			}
		})
	}
}

func TestInputKeyCodeFunctionKeys(t *testing.T) {
	t.Parallel()

	code, ok := inputKeyCode(0xffc8)
	if !ok || code != keyF11 {
		t.Fatalf("F11: got (%d, %v), want (%d, true)", code, ok, keyF11)
	}

	code, ok = inputKeyCode(0xffc9)
	if !ok || code != keyF12 {
		t.Fatalf("F12: got (%d, %v), want (%d, true)", code, ok, keyF12)
	}
}

func readInputConfig(v *InputDevice, sel, subsel uint8) [inputConfigLen]byte {
	v.WriteDeviceConfig(0, []byte{sel})
	v.WriteDeviceConfig(1, []byte{subsel})

	var cfg [inputConfigLen]byte
	v.ReadDeviceConfig(0, cfg[:])

	return cfg
}

func newInputSplitQueue() *SplitQueue {
	return &SplitQueue{
		Desc:  &[QueueSize]SplitDesc{},
		Avail: &SplitAvail{},
		Used:  &SplitUsed{},
	}
}

func queueInputBuffer(q *SplitQueue, descID uint16, addr uint64) {
	q.Desc[descID] = SplitDesc{Addr: addr, Len: inputEventLen, Flags: descFWrite}
	q.Avail.Ring[q.Avail.Idx%QueueSize] = descID
	q.Avail.Idx++
}

func assertInputEvent(t *testing.T, raw []byte, typ, code uint16, value int32) {
	t.Helper()

	if got := binary.LittleEndian.Uint16(raw[0:]); got != typ {
		t.Fatalf("event type: got %d, want %d", got, typ)
	}

	if got := binary.LittleEndian.Uint16(raw[2:]); got != code {
		t.Fatalf("event code: got %d, want %d", got, code)
	}

	if got := int32(binary.LittleEndian.Uint32(raw[4:])); got != value {
		t.Fatalf("event value: got %d, want %d", got, value)
	}
}

func TestInputTabletConfig(t *testing.T) {
	t.Parallel()

	v := NewInputTablet(6, func() error { return nil }, nil)
	cfg := readInputConfig(v, inputCfgIDName, 0)
	if got := string(cfg[inputUnionOff : inputUnionOff+cfg[2]]); got != "gokvm tablet" {
		t.Fatalf("tablet name = %q, want gokvm tablet", got)
	}
	cfg = readInputConfig(v, inputCfgIDDevids, 0)
	if cfg[2] != 8 || binary.LittleEndian.Uint16(cfg[inputUnionOff+4:]) != 0x0003 {
		t.Fatal("tablet must preserve its device identity")
	}
	cfg = readInputConfig(v, inputCfgPropBits, 0)
	if cfg[2] != 0 {
		t.Fatal("tablet must not advertise touchscreen or touchpad properties")
	}
	cfg = readInputConfig(v, inputCfgEvBits, evAbs)
	if !inputBitSet(cfg[inputUnionOff:], absX) || !inputBitSet(cfg[inputUnionOff:], absY) {
		t.Fatal("tablet must advertise both absolute axes")
	}
	cfg = readInputConfig(v, inputCfgEvBits, evRel)
	wheelOnly := [128]byte{relWheel / 8: 1 << (relWheel % 8)}
	if !slices.Equal(cfg[inputUnionOff:], wheelOnly[:]) {
		t.Fatal("tablet must advertise only a relative scroll wheel")
	}
	cfg = readInputConfig(v, inputCfgEvBits, evKey)
	for _, button := range []int{btnLeft, btnMiddle, btnRight} {
		if !inputBitSet(cfg[inputUnionOff:], button) {
			t.Fatalf("tablet button %d not advertised", button)
		}
	}
	for _, axis := range []uint8{absX, absY} {
		cfg = readInputConfig(v, inputCfgAbsInfo, axis)
		if cfg[2] != 20 || binary.LittleEndian.Uint32(cfg[inputUnionOff:]) != 0 ||
			binary.LittleEndian.Uint32(cfg[inputUnionOff+4:]) != inputAbsMax {
			t.Fatalf("invalid tablet axis %d limits: %v", axis, cfg)
		}
	}
	cfg = readInputConfig(v, inputCfgAbsInfo, 2)
	if cfg[2] != 0 {
		t.Fatal("tablet must not advertise unsupported absolute axes")
	}
}

func TestInputTabletPointerEventUsesDefaultBounds(t *testing.T) {
	t.Parallel()

	v := NewInputTablet(6, nil, nil)
	pair := NewInputPair(nil, v)
	pair.PointerEvent(1, 0, 0)
	want := []inputEvent{
		{typ: evAbs, code: absX, value: 15},
		{typ: evAbs, code: absY, value: 21},
		synEvent(),
		{typ: evKey, code: btnLeft, value: 1},
		synEvent(),
	}
	if !slices.Equal(v.pending, want) {
		t.Fatalf("default-bounds tablet reports %v, want %v", v.pending, want)
	}
}

func TestInputKeyboardIgnoresPointerEvents(t *testing.T) {
	t.Parallel()

	v := NewInputKeyboard(5, nil, nil)
	v.PointerEvent(1, 100, 200)
	v.PointerEventInBounds(1, 100, 200, 800, 600)
	if len(v.pending) != 0 {
		t.Fatalf("keyboard received pointer events: %v", v.pending)
	}
}

func TestInputTabletFirstClickDeliversPosition(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 0x1000)
	v := NewInputTablet(6, func() error { return nil }, mem)
	q := newInputSplitQueue()
	// Movement must be a complete report before the first button press.
	for i := 0; i < 5; i++ {
		queueInputBuffer(q, uint16(i), uint64(0x100+i*inputEventLen))
	}
	v.QueueReady(inputEventQueue, q)
	pair := NewInputPair(nil, v)
	pair.PointerEventInBounds(1, 400, 100, 800, 600)
	for v.LastAvailIdx[inputEventQueue] < 5 {
		if err := v.flushEvents(); err != nil {
			t.Fatalf("flush tablet event: %v", err)
		}
	}
	assertInputEvent(t, mem[0x100:0x108], evAbs, absX, 16403)
	assertInputEvent(t, mem[0x108:0x110], evAbs, absY, 5488)
	assertInputEvent(t, mem[0x110:0x118], evSyn, synReport, 0)
	assertInputEvent(t, mem[0x118:0x120], evKey, btnLeft, 1)
	assertInputEvent(t, mem[0x120:0x128], evSyn, synReport, 0)
}

func TestInputTabletPositionsMatchFramebuffer(t *testing.T) {
	t.Parallel()

	// Exercise every pixel, including both edges and reconnect-like jumps. The
	// legacy Linux mousedev path must reconstruct exactly the original pixel.
	for _, size := range []int{1, 600, 768, 800, 1024, 1920, 2560} {
		for position := range size {
			absolute := inputAbsolutePosition(uint16(position), size)
			// A nonzero top-left value also avoids the input core filtering a
			// first position as unchanged from its initial zero ABS state.
			if absolute == 0 {
				t.Fatalf("size %d position %d encoded as an unchanged initial axis", size, position)
			}
			pixel := int(absolute) * size / inputAbsMax
			if pixel != position {
				t.Fatalf("size %d position %d reconstructed as %d", size, position, pixel)
			}
		}
		if got := inputAbsolutePosition(65535, size); got != inputAbsolutePosition(uint16(size-1), size) {
			t.Fatalf("size %d out-of-range position was not clamped: %d", size, got)
		}
	}
}

func TestInputTabletButtonsWheelAndBounds(t *testing.T) {
	t.Parallel()

	v := NewInputTablet(6, func() error { return nil }, nil)
	v.PointerEventInBounds(0x0d, 200, 120, 800, 600)
	v.PointerEventInBounds(0, 200, 120, 800, 600)
	want := []inputEvent{
		{typ: evAbs, code: absX, value: 8212},
		{typ: evAbs, code: absY, value: 6580},
		synEvent(),
		{typ: evKey, code: btnLeft, value: 1},
		{typ: evKey, code: btnRight, value: 1},
		{typ: evRel, code: relWheel, value: 1},
		synEvent(),
		{typ: evAbs, code: absX, value: 8212},
		{typ: evAbs, code: absY, value: 6580},
		synEvent(),
		{typ: evKey, code: btnLeft},
		{typ: evKey, code: btnRight},
		synEvent(),
	}
	if !slices.Equal(v.pending, want) {
		t.Fatalf("tablet reports %v, want %v", v.pending, want)
	}
	before := len(v.pending)
	v.PointerEventInBounds(1, 0, 0, 0, 600)
	v.PointerEventInBounds(1, 0, 0, 800, -1)
	if len(v.pending) != before {
		t.Fatal("invalid framebuffer bounds generated input")
	}
}

func TestInputConcurrentConfigurationAndQueues(t *testing.T) {
	t.Parallel()

	v := NewInputKeyboard(5, func() error { return nil }, make([]byte, 0x1000))
	q := newInputSplitQueue()
	queueInputBuffer(q, 0, 0x100)
	queueInputBuffer(q, 1, 0x108)
	status := newInputSplitQueue()
	status.Avail.Idx = 1
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 1000 {
			selection := []byte{inputCfgIDName, 0}
			if i%2 != 0 {
				selection = []byte{inputCfgEvBits, evKey}
			}
			v.WriteDeviceConfig(0, selection)
		}
	})
	workers.Go(func() {
		var cfg [inputConfigLen]byte
		for range 1000 {
			v.ReadDeviceConfig(0, cfg[:])
			switch cfg[0] {
			case 0: // Reset/default configuration.
			case inputCfgIDName:
				if string(cfg[inputUnionOff:inputUnionOff+int(cfg[2])]) != "gokvm keyboard" {
					t.Error("input name/configuration selector snapshot was torn")

					return
				}
			case inputCfgEvBits:
				if cfg[1] != evKey || !inputBitSet(cfg[inputUnionOff:], keyA) {
					t.Error("input event bitmap/configuration selector snapshot was torn")

					return
				}
			default:
				t.Errorf("unexpected input selector %d", cfg[0])

				return
			}
		}
	})
	workers.Go(func() {
		for range 200 {
			v.QueueReady(inputEventQueue, q)
			v.QueueReady(inputStatusQueue, status)
			v.Reset()
		}
	})
	workers.Go(func() {
		for range 1000 {
			v.KeyEvent(true, 'a')
			_ = v.flushEvents()
			_ = v.drainStatusQueue()
		}
	})
	workers.Wait()
	v.Reset()
	if err := v.flushEvents(); !errors.Is(err, ErrVQNotInit) {
		t.Fatalf("reset retained event queue: %v", err)
	}
	if err := v.drainStatusQueue(); !errors.Is(err, ErrVQNotInit) {
		t.Fatalf("reset retained status queue: %v", err)
	}
	if _, ok := v.popPending(); ok {
		t.Fatal("reset retained pending input")
	}
}

func TestInputQueueReplacementStartsAtFirstDescriptor(t *testing.T) {
	t.Parallel()

	v := NewInputKeyboard(5, func() error { return nil }, make([]byte, 0x1000))
	for range 2 {
		q := newInputSplitQueue()
		queueInputBuffer(q, 0, 0x100)
		v.QueueReady(inputEventQueue, q)
		v.KeyEvent(true, 'a')
		// QueueReady may consume a pending SYN event from the previous queue.
		if LoadU16(&q.Used.Idx) == 0 {
			if err := v.flushEvents(); err != nil {
				t.Fatalf("replacement queue skipped first descriptor: %v", err)
			}
		}
		if LoadU16(&q.Used.Idx) != 1 {
			t.Fatal("replacement queue did not start at its first available descriptor")
		}
	}
}
