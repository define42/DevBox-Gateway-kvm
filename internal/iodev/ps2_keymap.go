package iodev

type ps2Scan struct {
	code     byte
	extended bool
}

func ps2ScanCode(keysym uint32) (ps2Scan, bool) {
	if scan, ok := ps2ASCIIKey(keysym); ok {
		return scan, true
	}

	if keysym >= 0xffbe && keysym <= 0xffc9 {
		return ps2FunctionKey(int(keysym - 0xffbe)), true
	}

	scan, ok := map[uint32]ps2Scan{
		0xff08: {code: 0x0e},                 // BackSpace
		0xff09: {code: 0x0f},                 // Tab
		0xff0d: {code: 0x1c},                 // Return
		0xff1b: {code: 0x01},                 // Escape
		0xffff: {code: 0x53, extended: true}, // Delete
		0xff50: {code: 0x47, extended: true}, // Home
		0xff51: {code: 0x4b, extended: true}, // Left
		0xff52: {code: 0x48, extended: true}, // Up
		0xff53: {code: 0x4d, extended: true}, // Right
		0xff54: {code: 0x50, extended: true}, // Down
		0xff55: {code: 0x49, extended: true}, // Page Up
		0xff56: {code: 0x51, extended: true}, // Page Down
		0xff57: {code: 0x4f, extended: true}, // End
		0xff63: {code: 0x52, extended: true}, // Insert
		0xffe1: {code: 0x2a},                 // Left Shift
		0xffe2: {code: 0x36},                 // Right Shift
		0xffe3: {code: 0x1d},                 // Left Ctrl
		0xffe4: {code: 0x1d, extended: true}, // Right Ctrl
		0xffe9: {code: 0x38},                 // Left Alt
		0xffea: {code: 0x38, extended: true}, // Right Alt
		0xffeb: {code: 0x5b, extended: true}, // Left Super
		0xffec: {code: 0x5c, extended: true}, // Right Super
		0xffe5: {code: 0x3a},                 // Caps Lock
		0xff14: {code: 0x46},                 // Scroll Lock
		0xff7f: {code: 0x45},                 // Num Lock
		0xff61: {code: 0x52, extended: true}, // Print
		0xff13: {code: 0x45},                 // Pause, best-effort
	}[keysym]

	return scan, ok
}

func ps2ASCIIKey(keysym uint32) (ps2Scan, bool) {
	if keysym >= 'A' && keysym <= 'Z' {
		keysym += 'a' - 'A'
	}

	scan, ok := map[uint32]byte{
		'a':  0x1e,
		'b':  0x30,
		'c':  0x2e,
		'd':  0x20,
		'e':  0x12,
		'f':  0x21,
		'g':  0x22,
		'h':  0x23,
		'i':  0x17,
		'j':  0x24,
		'k':  0x25,
		'l':  0x26,
		'm':  0x32,
		'n':  0x31,
		'o':  0x18,
		'p':  0x19,
		'q':  0x10,
		'r':  0x13,
		's':  0x1f,
		't':  0x14,
		'u':  0x16,
		'v':  0x2f,
		'w':  0x11,
		'x':  0x2d,
		'y':  0x15,
		'z':  0x2c,
		'1':  0x02,
		'2':  0x03,
		'3':  0x04,
		'4':  0x05,
		'5':  0x06,
		'6':  0x07,
		'7':  0x08,
		'8':  0x09,
		'9':  0x0a,
		'0':  0x0b,
		' ':  0x39,
		'-':  0x0c,
		'=':  0x0d,
		'[':  0x1a,
		']':  0x1b,
		'\\': 0x2b,
		';':  0x27,
		'\'': 0x28,
		'`':  0x29,
		',':  0x33,
		'.':  0x34,
		'/':  0x35,
	}[keysym]

	return ps2Scan{code: scan}, ok
}

func ps2FunctionKey(n int) ps2Scan {
	return []ps2Scan{
		{code: 0x3b},
		{code: 0x3c},
		{code: 0x3d},
		{code: 0x3e},
		{code: 0x3f},
		{code: 0x40},
		{code: 0x41},
		{code: 0x42},
		{code: 0x43},
		{code: 0x44},
		{code: 0x57},
		{code: 0x58},
	}[n]
}

func ps2KeySequence(makeSeq ps2Scan, down bool) []byte {
	if makeSeq.extended {
		if down {
			return []byte{0xe0, makeSeq.code}
		}

		return []byte{0xe0, makeSeq.code | 0x80}
	}

	if down {
		return []byte{makeSeq.code}
	}

	return []byte{makeSeq.code | 0x80}
}
