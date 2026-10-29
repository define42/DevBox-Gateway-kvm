//go:build integration

package virtio

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"slices"
	"testing"
	"time"
)

const (
	rdpInputClientUser    = 1001
	rdpInputGlobalChannel = 1003
	rdpInputShareID       = 0x103ea
)

type rdpInputSinkEvent struct {
	down   bool
	keysym uint32
}

type rdpInputSink struct {
	events chan rdpInputSinkEvent
}

func (s *rdpInputSink) KeyEvent(down bool, keysym uint32) {
	s.events <- rdpInputSinkEvent{down: down, keysym: keysym}
}

func (*rdpInputSink) PointerEvent(uint8, uint16, uint16) {}

func TestRDPDisplayInputOverflowClosesConnectionAndReleasesKeys(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		fastPath bool
	}{
		{name: "fast-path", fastPath: true},
		{name: "slow-path"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			testRDPDisplayInputOverflow(t, test.fastPath)
		})
	}
}

func testRDPDisplayInputOverflow(t *testing.T, fastPath bool) {
	t.Helper()

	const committedKeys = 255
	display := newRDPTestDisplay(t)
	input := &rdpInputSink{events: make(chan rdpInputSinkEvent, 2*committedKeys+2)}
	display.SetInput(input)

	conn := dialRDPTestDisplay(t, display)
	response := negotiateRDPTestDisplay(t, conn, 1)
	if response[11] != 2 || binary.LittleEndian.Uint32(response[15:]) != 1 {
		t.Fatalf("TLS negotiation response: got %x", response)
	}
	secured := tls.Client(conn, rdpTestClientTLS(t, display))
	if err := secured.Handshake(); err != nil {
		t.Fatal(err)
	}
	activateRDPInputTestSession(t, secured)

	for index := range committedKeys {
		writeRDPInputUnicode(t, secured, fastPath, uint16(0x100+index))
	}
	// The first event would reach the held-key cap and the second would cross it.
	// The entire packet must be rejected without dispatching or retaining either.
	writeRDPInputUnicode(t, secured, fastPath, 0x1ff, 0x200)

	waitRDPClientCount(t, display, 0)
	assertRDPInputConnectionClosed(t, secured)
	if got := len(input.events); got != 2*committedKeys {
		t.Fatalf("guest input events = %d, want %d committed downs and releases", got, 2*committedKeys)
	}
	for index := range committedKeys {
		assertRDPInputKeyEvent(t, <-input.events, true, uint32(0x01000100+index))
	}
	for index := range committedKeys {
		assertRDPInputKeyEvent(t, <-input.events, false, uint32(0x01000100+index))
	}
}

func writeRDPInputUnicode(t *testing.T, conn net.Conn, fastPath bool, codes ...uint16) {
	t.Helper()
	if fastPath {
		writeRDPTest(t, conn, rdpInputUnicodePacket(codes...))

		return
	}
	payload := make([]byte, 4+12*len(codes))
	rdpInputPut16(payload, 0, len(codes))
	for index, code := range codes {
		event := payload[4+12*index:]
		rdpInputPut16(event, 4, 5) // TS_UNICODE_KEYBOARD_EVENT
		rdpInputPut16(event, 8, int(code))
	}
	writeRDPInputGlobal(t, conn, rdpInputShareData(28, payload))
}

func assertRDPInputKeyEvent(t *testing.T, got rdpInputSinkEvent, down bool, keysym uint32) {
	t.Helper()
	if got.down != down || got.keysym != keysym {
		t.Fatalf("input event = %+v, want key down=%v keysym=%#x", got, down, keysym)
	}
}

func assertRDPInputConnectionClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var data [4096]byte
	for {
		if _, err := conn.Read(data[:]); err != nil {
			var netErr net.Error
			if errors.As(err, &netErr) && netErr.Timeout() {
				t.Fatalf("connection did not close: %v", err)
			}

			return
		}
	}
}

func activateRDPInputTestSession(t *testing.T, conn net.Conn) {
	t.Helper()
	joinRDPInputTestChannels(t, conn)
	confirmRDPInputTestActive(t, conn)
}

func joinRDPInputTestChannels(t *testing.T, conn net.Conn, channels ...string) {
	t.Helper()
	joinRDPInputTestChannelsWithVersion(t, conn, 0x80004, channels...)
}

func joinRDPInputTestChannelsWithVersion(t *testing.T, conn net.Conn, version uint32, channels ...string) []byte {
	t.Helper()

	return joinRDPInputTestChannelsWithInitial(t, conn, rdpInputConnectInitialVersion(version, channels...), channels...)
}

func joinRDPInputTestChannelsWithInitial(t *testing.T, conn net.Conn, initial []byte, channels ...string) []byte {
	t.Helper()
	writeRDPInputMCS(t, conn, initial)
	if response := readRDPInputMCS(t, conn); len(response) < 2 || response[0] != 0x7f || response[1] != 0x66 {
		t.Fatalf("expected MCS Connect Response, got %x", response)
	}
	writeRDPInputMCS(t, conn, []byte{4, 1, 0, 1, 0})
	writeRDPInputMCS(t, conn, []byte{0x28})
	if response := readRDPInputMCS(t, conn); !bytes.Equal(response, []byte{0x2e, 0, 0, 0}) {
		t.Fatalf("expected Attach User Confirm, got %x", response)
	}
	joined := []uint16{rdpInputClientUser, rdpInputGlobalChannel}
	for index := range channels {
		joined = append(joined, uint16(1004+index))
	}
	for _, channel := range joined {
		join := []byte{0x38, 0, 0, byte(channel >> 8), byte(channel)}
		writeRDPInputMCS(t, conn, join)
		if response := readRDPInputMCS(t, conn); len(response) != 8 || response[0] != 0x3e || response[1] != 0 {
			t.Fatalf("expected Channel Join Confirm, got %x", response)
		}
	}
	info := make([]byte, 32)
	info[0], info[8] = 0x40, 0x10
	writeRDPInputGlobal(t, conn, info)
	license := readRDPInputGlobal(t, conn)
	if len(license) != 20 || license[0] != 0x80 || license[4] != 0xff || license[8] != 7 {
		t.Fatalf("expected valid-client license, got %x", license)
	}
	demand := readRDPInputGlobal(t, conn)
	if len(demand) < 6 || binary.LittleEndian.Uint16(demand[2:])&15 != 1 {
		t.Fatalf("expected Demand Active, got %x", demand)
	}

	return demand
}

func confirmRDPInputTestActive(t *testing.T, conn net.Conn) {
	t.Helper()
	confirmRDPInputTestActiveWithInput(t, conn, 0)
}

func confirmRDPInputTestActiveWithInput(t *testing.T, conn net.Conn, inputFlags uint16) {
	t.Helper()
	confirm := make([]byte, 16+6+4+28)
	rdpInputPut16(confirm, 0, len(confirm))
	rdpInputPut16(confirm, 2, 0x13)
	rdpInputPut16(confirm, 4, rdpInputClientUser)
	binary.LittleEndian.PutUint32(confirm[6:10], rdpInputShareID)
	rdpInputPut16(confirm, 12, 6)
	rdpInputPut16(confirm, 14, 32)
	rdpInputPut16(confirm, 22, 1)
	rdpInputPut16(confirm, 26, 2)
	rdpInputPut16(confirm, 28, 28)
	rdpInputPut16(confirm, 30, 24)
	if inputFlags != 0 {
		input := make([]byte, 88)
		rdpInputPut16(input, 0, 13)
		rdpInputPut16(input, 2, len(input))
		rdpInputPut16(input, 4, int(inputFlags))
		confirm = slices.Concat(confirm, input)
		rdpInputPut16(confirm, 0, len(confirm))
		rdpInputPut16(confirm, 14, 32+len(input))
		rdpInputPut16(confirm, 22, 2)
	}
	writeRDPInputGlobal(t, conn, confirm)
	for _, expected := range []byte{0x1f, 0x14} {
		response := readRDPInputGlobal(t, conn)
		if len(response) < 18 || response[14] != expected {
			t.Fatalf("expected activation data %#x, got %x", expected, response)
		}
	}
	writeRDPInputGlobal(t, conn, rdpInputShareData(0x1f, []byte{1, 0, 0xea, 3}))
	writeRDPInputGlobal(t, conn, rdpInputShareData(0x14, []byte{4, 0, 0, 0, 0, 0, 0, 0}))
	writeRDPInputGlobal(t, conn, rdpInputShareData(0x14, []byte{1, 0, 0, 0, 0, 0, 0, 0}))
	grant := readRDPInputGlobal(t, conn)
	if len(grant) != 26 || grant[14] != 0x14 || grant[18] != 2 {
		t.Fatalf("expected Granted Control, got %x", grant)
	}
	writeRDPInputGlobal(t, conn, rdpInputShareData(0x27, []byte{0, 0, 0, 0, 3, 0, 0x32, 0}))
	if font := readRDPInputGlobal(t, conn); len(font) != 26 || font[14] != 0x28 {
		t.Fatalf("expected Font Map, got %x", font)
	}
}

func rdpInputUnicodePacket(codes ...uint16) []byte {
	data := []byte{byte(len(codes) << 2)}
	for _, code := range codes {
		data = append(data, 4<<5, byte(code), byte(code>>8))
	}

	return append([]byte{data[0], byte(len(data) + 1)}, data[1:]...)
}

func rdpInputConnectInitialVersion(version uint32, channels ...string) []byte {
	return rdpInputConnectInitialWithGraphics(version, false, channels...)
}

func rdpInputConnectInitialWithGraphics(version uint32, graphics bool, channels ...string) []byte {
	coreSize := 132
	if graphics {
		coreSize = 146
	}
	core := make([]byte, coreSize)
	rdpInputPut16(core, 0, 0xc001)
	rdpInputPut16(core, 2, len(core))
	binary.LittleEndian.PutUint32(core[4:8], version)
	rdpInputPut16(core, 8, 800)
	rdpInputPut16(core, 10, 600)
	if graphics {
		rdpInputPut16(core, 144, 0x100) // RNS_UD_CS_SUPPORT_DYNVC_GFX_PROTOCOL
	}
	if len(channels) != 0 {
		network := make([]byte, 8+12*len(channels))
		rdpInputPut16(network, 0, 0xc003)
		rdpInputPut16(network, 2, len(network))
		binary.LittleEndian.PutUint32(network[4:], uint32(len(channels)))
		for index, channel := range channels {
			copy(network[8+12*index:16+12*index], channel)
		}
		core = slices.Concat(core, network)
	}
	gcc := rdpInputAppendPERLength([]byte{0, 5, 0, 0x14, 0x7c, 0, 1}, len(core)+14)
	gcc = append(gcc, 0, 8, 0, 16, 0, 1, 0xc0, 0, 'D', 'u', 'c', 'a')
	gcc = append(rdpInputAppendPERLength(gcc, len(core)), core...)
	body := []byte{4, 1, 1, 4, 1, 1, 1, 1, 0xff, 0x30, 0, 0x30, 0, 0x30, 0}
	body = append(body, rdpInputBER(4, gcc)...)

	return append([]byte{0x7f}, rdpInputBER(0x65, body)...)
}

func writeRDPInputMCS(t *testing.T, conn net.Conn, p []byte) {
	t.Helper()
	payload := append([]byte{2, 0xf0, 0x80}, p...)
	packet := make([]byte, 4+len(payload))
	packet[0] = 3
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	copy(packet[4:], payload)
	writeRDPTest(t, conn, packet)
}

func readRDPInputMCS(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	var header [4]byte
	if _, err := io.ReadFull(conn, header[:]); err != nil {
		t.Fatal(err)
	}
	size := int(binary.BigEndian.Uint16(header[2:]))
	if header[0] != 3 || header[1] != 0 || size < 7 {
		t.Fatalf("invalid TPKT header: %x", header)
	}
	payload := make([]byte, size-4)
	if _, err := io.ReadFull(conn, payload); err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(payload, []byte{2, 0xf0, 0x80}) {
		t.Fatalf("invalid X.224 data: %x", payload)
	}

	return payload[3:]
}

func writeRDPInputGlobal(t *testing.T, conn net.Conn, data []byte) {
	t.Helper()
	p := rdpInputAppendPERLength([]byte{0x64, 0, 0, 3, 0xeb, 0x70}, len(data))
	writeRDPInputMCS(t, conn, append(p, data...))
}

func readRDPInputGlobal(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	p := readRDPInputMCS(t, conn)
	if len(p) < 7 || p[0] != 0x68 || p[3] != 3 || p[4] != 0xeb {
		t.Fatalf("expected global-channel indication, got %x", p)
	}

	return rdpInputTakePER(t, p[6:])
}

func rdpInputShareData(kind byte, payload []byte) []byte {
	p := make([]byte, 18+len(payload))
	rdpInputPut16(p, 0, len(p))
	rdpInputPut16(p, 2, 0x17)
	rdpInputPut16(p, 4, rdpInputClientUser)
	binary.LittleEndian.PutUint32(p[6:10], rdpInputShareID)
	p[11], p[14] = 1, kind
	rdpInputPut16(p, 12, len(p)-6)
	copy(p[18:], payload)

	return p
}

func rdpInputTakePER(t *testing.T, p []byte) []byte {
	t.Helper()
	if len(p) == 0 {
		t.Fatal("empty PER length")
	}
	size, offset := int(p[0]), 1
	if size&0x80 != 0 {
		if len(p) < 2 {
			t.Fatal("truncated PER length")
		}
		size, offset = (size&0x7f)<<8|int(p[1]), 2
	}
	if size != len(p)-offset {
		t.Fatalf("PER payload length = %d, want %d", size, len(p)-offset)
	}

	return p[offset:]
}

func rdpInputAppendPERLength(p []byte, size int) []byte {
	if size < 128 {
		return append(p, byte(size))
	}

	return append(p, byte(size>>8)|0x80, byte(size))
}

func rdpInputBER(tag byte, body []byte) []byte {
	p := []byte{tag}
	switch {
	case len(body) < 128:
		p = append(p, byte(len(body)))
	case len(body) < 256:
		p = append(p, 0x81, byte(len(body)))
	default:
		p = append(p, 0x82, byte(len(body)>>8), byte(len(body)))
	}

	return append(p, body...)
}

func rdpInputPut16(data []byte, offset, value int) {
	binary.LittleEndian.PutUint16(data[offset:], uint16(value))
}
