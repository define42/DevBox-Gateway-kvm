package rdp

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestGraphicsTimestamp(t *testing.T) {
	t.Parallel()
	stamp := time.Date(2026, time.October, 8, 18, 49, 30, 999_000_000, time.FixedZone("UTC+2", 2*60*60))
	if got, want := graphicsTimestamp(stamp), uint32(16<<22|49<<16|30<<10|999); got != want {
		t.Fatalf("START_FRAME timestamp = %#x, want %#x", got, want)
	}
	if got := graphicsTimestamp(time.Date(2026, time.October, 8, 0, 0, 0, 0, time.UTC)); got != 0 {
		t.Fatalf("midnight timestamp = %#x, want zero", got)
	}
}

func TestGraphicsChannelNegotiationAndAcknowledgments(t *testing.T) {
	t.Parallel()
	caps := graphicsCapsFixture(graphicsVersion81, 0x10)
	traffic := bytes.Join([][]byte{
		graphicsInputFixture([]byte{0x50, 0, 1, 0}),
		graphicsInputFixture([]byte{0x10, 1, 0, 0, 0, 0}),
		graphicsInputFixture(append([]byte{0x30, 1}, caps...)),
		{4, 4, 0, 0x1e}, // Fast-path keyboard input after negotiation.
	}, nil)
	conn := &graphicsTestConn{incoming: bytes.NewReader(traffic)}
	session := &Session{
		Width: 1024, Height: 768, conn: conn,
		joined:   map[uint16]bool{1004: true},
		graphics: &graphicsState{supported: true},
		dynamic:  &dynamicState{channel: 1004},
	}
	started, err := session.BeginGraphics()
	if err != nil || !started {
		t.Fatalf("begin graphics = %v, %v", started, err)
	}
	data, fast, err := session.ReadPacket()
	if err != nil || !fast || !bytes.Equal(data, []byte{4, 0, 0x1e}) {
		t.Fatalf("input after negotiation = %x, %v, %v", data, fast, err)
	}
	if !session.GraphicsReady() || !session.GraphicsCanSend() {
		t.Fatal("graphics not ready after capability exchange")
	}
	output := graphicsOutputFixture(t, conn.outgoing.Bytes())
	if len(output) != 3 || !bytes.Equal(output[0], []byte{0x50, 0, 1, 0}) ||
		!bytes.Equal(output[1], append([]byte{0x10, 1}, graphicsChannelName...)) {
		t.Fatalf("unexpected negotiation messages: %x", output)
	}
	if !bytes.Equal(output[2][:4], []byte{0x30, 1, 0xe0, 4}) {
		t.Fatalf("missing DVC / ZGFX framing: %x", output[2][:4])
	}
	checkGraphicsInitialization(t, output[2][4:])
	conn.outgoing.Reset()

	frame := []byte{0, 0, 0, 1, 0x65, 0xaa}
	for range graphicsMaxFrames {
		if sent, err := session.WriteAVC420(frame); err != nil || !sent {
			t.Fatalf("send frame = %v, %v", sent, err)
		}
	}
	if session.GraphicsCanSend() {
		t.Fatal("acknowledgment window did not limit queued frames")
	}
	if sent, err := session.WriteAVC420(frame); err != nil || sent {
		t.Fatalf("over-window frame = %v, %v", sent, err)
	}
	ack := make([]byte, 12)
	binary.LittleEndian.PutUint32(ack[4:], 99)
	if err := session.readGraphicsPDU(0x0d, ack); err != nil || session.GraphicsCanSend() {
		t.Fatalf("unrecognized acknowledgment changed the window: %v", err)
	}
	binary.LittleEndian.PutUint32(ack[4:], 1)
	if err := session.readGraphicsPDU(0x0d, ack); err != nil || !session.GraphicsCanSend() {
		t.Fatalf("frame acknowledgment did not reopen window: %v", err)
	}
	binary.LittleEndian.PutUint32(ack, ^uint32(0))
	if err := session.readGraphicsPDU(0x0d, ack); err != nil || !session.graphics.ackOff {
		t.Fatalf("suspend acknowledgments failed: %v", err)
	}
}

func TestGraphicsAcknowledgmentAdaptation(t *testing.T) {
	t.Parallel()
	const bytesPerFrame = 1000
	base := time.Second / 60
	now := time.Unix(100, 0)
	g := graphicsState{
		ready: true, baseInterval: base, averageBytes: bytesPerFrame,
	}

	// Three buffered frames slow 60 Hz pacing to 15 Hz immediately.
	g.adaptAcknowledgments(3*bytesPerFrame,
		graphicsPendingFrame{id: 1, sentAt: now.Add(-10 * time.Millisecond)}, now)
	slow := 4 * base
	if g.frameInterval != slow {
		t.Fatalf("queue slowdown = %v, want %v", g.frameInterval, slow)
	}

	// QUEUE_DEPTH_UNAVAILABLE cannot prove an empty queue. A fast ACK still
	// permits gradual recovery instead of snapping directly back to 60 Hz.
	g.adaptAcknowledgments(0,
		graphicsPendingFrame{id: 2, sentAt: now.Add(-10 * time.Millisecond)}, now)
	if g.frameInterval <= base || g.frameInterval >= slow {
		t.Fatalf("first recovery interval = %v, want between %v and %v", g.frameInterval, base, slow)
	}
	for id := uint32(3); id < 32; id++ {
		g.adaptAcknowledgments(0,
			graphicsPendingFrame{id: id, sentAt: now.Add(-10 * time.Millisecond)}, now)
	}
	if g.frameInterval != base {
		t.Fatalf("recovered interval = %v, want %v", g.frameInterval, base)
	}

	g.resetAcknowledgments()
	g.averageBytes = bytesPerFrame
	g.adaptAcknowledgments(0,
		graphicsPendingFrame{id: 32, sentAt: now.Add(-100 * time.Millisecond)}, now)
	if want := 50 * time.Millisecond; g.frameInterval != want {
		t.Fatalf("turnaround slowdown = %v, want %v", g.frameInterval, want)
	}

	g.resetAcknowledgments()
	g.averageBytes = bytesPerFrame
	g.adaptAcknowledgments(^uint32(0)-1,
		graphicsPendingFrame{id: 33, sentAt: now.Add(-2 * time.Second)}, now)
	if g.frameInterval != graphicsMaxFrameInterval {
		t.Fatalf("maximum slowdown = %v, want %v", g.frameInterval, graphicsMaxFrameInterval)
	}
}

func TestGraphicsAcknowledgmentIsolationAndReset(t *testing.T) {
	t.Parallel()
	base := time.Second / 60
	now := time.Now()
	g := &graphicsState{
		ready: true, baseInterval: base, averageBytes: 1000,
		frameInterval: 4 * base,
		inFlight: []graphicsPendingFrame{
			{id: 7, sentAt: now.Add(-10 * time.Millisecond)},
			{id: 8, sentAt: now.Add(-10 * time.Millisecond)},
		},
	}
	s := &Session{graphics: g}
	ack := make([]byte, 12)
	binary.LittleEndian.PutUint32(ack, 8000)
	binary.LittleEndian.PutUint32(ack[4:], 99)
	if err := s.readGraphicsPDU(0x0d, ack); err != nil {
		t.Fatal(err)
	}
	if g.frameInterval != 4*base || len(g.inFlight) != 2 {
		t.Fatal("unknown frame acknowledgment changed pacing or the send window")
	}

	qoe := make([]byte, 12)
	binary.LittleEndian.PutUint32(qoe, 7)
	binary.LittleEndian.PutUint32(qoe[4:], 1234)
	binary.LittleEndian.PutUint16(qoe[8:], 20)
	binary.LittleEndian.PutUint16(qoe[10:], 30)
	if err := s.readGraphicsPDU(0x16, qoe); err != nil {
		t.Fatal(err)
	}
	if g.frameInterval != 4*base || len(g.inFlight) != 2 {
		t.Fatal("informational QoE acknowledgment changed pacing")
	}

	binary.LittleEndian.PutUint32(ack[4:], 7)
	if err := s.readGraphicsPDU(0x0d, ack); err != nil {
		t.Fatal(err)
	}
	if want := 9 * base; g.frameInterval != want ||
		len(g.inFlight) != 1 || g.inFlight[0].id != 8 {
		t.Fatalf("recognized acknowledgment: interval=%v in-flight=%v, want %v and frame 8",
			g.frameInterval, g.inFlight, want)
	}

	binary.LittleEndian.PutUint32(ack, ^uint32(0))
	if err := s.readGraphicsPDU(0x0d, ack); err != nil {
		t.Fatal(err)
	}
	if !g.ackOff || len(g.inFlight) != 0 || g.averageBytes != 0 ||
		g.ackTurnaround != 0 || g.frameInterval != 0 {
		t.Fatal("suspended acknowledgments did not reset pacing state")
	}

	// The first ordinary ACK opts back in. Its untracked ID must not seed
	// adaptation from the previous acknowledgment generation.
	binary.LittleEndian.PutUint32(ack, 0)
	binary.LittleEndian.PutUint32(ack[4:], 8)
	if err := s.readGraphicsPDU(0x0d, ack); err != nil {
		t.Fatal(err)
	}
	if g.ackOff || g.frameInterval != 0 {
		t.Fatal("acknowledgment resume reused stale pacing state")
	}
	if got := s.GraphicsFrameInterval(base); got != base {
		t.Fatalf("post-resume interval = %v, want %v", got, base)
	}
}

func TestGraphicsStatsSnapshot(t *testing.T) {
	t.Parallel()
	if got := new(Session).GraphicsStats(); got != (GraphicsStats{}) {
		t.Fatalf("client without graphics has pacing statistics: %+v", got)
	}
	g := &graphicsState{
		frameInterval: 50 * time.Millisecond,
		ackTurnaround: 80 * time.Millisecond,
		inFlight:      []graphicsPendingFrame{{id: 1}, {id: 2}},
	}
	s := &Session{graphics: g}
	want := GraphicsStats{
		FrameInterval: 50 * time.Millisecond, ACKTurnaround: 80 * time.Millisecond, OutstandingACKs: 2,
	}
	if got := s.GraphicsStats(); got != want {
		t.Fatalf("pacing snapshot = %+v, want %+v", got, want)
	}
	if g.baseInterval != 0 || g.frameInterval != want.FrameInterval {
		t.Fatal("reading statistics changed pacing configuration")
	}
	g.resetAcknowledgments()
	if got := s.GraphicsStats(); got != (GraphicsStats{}) {
		t.Fatalf("snapshot retained statistics after acknowledgment reset: %+v", got)
	}
}

func TestGraphicsStatsConcurrentSnapshot(t *testing.T) {
	t.Parallel()
	g := &graphicsState{}
	s := &Session{graphics: g}
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := range 1000 {
			g.mu.Lock()
			g.frameInterval = time.Duration(i) * time.Microsecond
			g.ackTurnaround = 2 * g.frameInterval
			g.mu.Unlock()
		}
	})
	for range 1000 {
		got := s.GraphicsStats()
		if got.ACKTurnaround != 2*got.FrameInterval {
			t.Errorf("snapshot mixed two pacing updates: %+v", got)

			break
		}
	}
	workers.Wait()
}

func TestGraphicsChangesWakeWriter(t *testing.T) {
	t.Parallel()
	if new(Session).GraphicsChanged() != nil {
		t.Fatal("client without graphics has a notification channel")
	}
	s := &Session{
		Width: 1024, Height: 768, conn: &graphicsTestConn{},
		graphics: &graphicsState{}, dynamic: graphicsDynamicFixture(3),
	}
	changed := s.GraphicsChanged()
	if changed != s.GraphicsChanged() {
		t.Fatal("graphics notification channel changed between calls")
	}
	wantWake := func(want bool) {
		t.Helper()
		select {
		case <-changed:
			if !want {
				t.Fatal("unexpected graphics wake")
			}
		default:
			if want {
				t.Fatal("graphics writer was not notified")
			}
		}
	}
	wantWake(false)
	if err := s.readGraphics(graphicsCapsFixture(graphicsVersion81, 0x10)); err != nil {
		t.Fatal(err)
	}
	wantWake(true)
	for range graphicsMaxFrames {
		if sent, err := s.WriteAVC420([]byte{0, 0, 1, 0x65}); err != nil || !sent {
			t.Fatalf("send frame = %v, %v", sent, err)
		}
	}
	wantWake(false)
	ack := make([]byte, 12)
	binary.LittleEndian.PutUint32(ack[4:], 99)
	if err := s.readGraphicsPDU(0x0d, ack); err != nil {
		t.Fatal(err)
	}
	wantWake(false)
	for _, id := range []uint32{1, 2} {
		binary.LittleEndian.PutUint32(ack[4:], id)
		if err := s.readGraphicsPDU(0x0d, ack); err != nil {
			t.Fatal(err)
		}
	}
	wantWake(true)
	wantWake(false) // Multiple acknowledgments coalesce without blocking the reader.
	if !s.GraphicsCanSend() {
		t.Fatal("acknowledgments did not reopen the send window")
	}
	binary.LittleEndian.PutUint32(ack, ^uint32(0))
	if err := s.readGraphicsPDU(0x0d, ack); err != nil {
		t.Fatal(err)
	}
	wantWake(true)
	if err := s.readDynamic([]byte{0x40, graphicsChannelID}); err != nil {
		t.Fatal(err)
	}
	wantWake(true)
	if s.GraphicsReady() {
		t.Fatal("closed graphics channel still ready")
	}
}

func TestGraphicsGCCSettings(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		gfx      bool
		channels []string
		want     uint16
		wantErr  bool
	}{
		{"off", false, []string{"drdynvc"}, 1004, false},
		{"graphics", true, []string{"rdpdr", "drdynvc"}, 1005, false},
		{"no-dvc", true, []string{"cliprdr"}, 0, false},
		{"duplicate", true, []string{"drdynvc", "drdynvc"}, 0, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			settings, err := parseConnectInitialDetails(graphicsConnectFixture(tc.gfx, tc.channels))
			if (err != nil) != tc.wantErr {
				t.Fatalf("parse GCC: %v", err)
			}
			if err == nil && (settings.graphics != tc.gfx || settings.dynamicChannel != tc.want ||
				settings.channels != len(tc.channels)) {
				t.Fatalf("incorrect settings: %+v", settings)
			}
		})
	}
}

func TestGraphicsAdvertisesRefreshAndSuppressOutput(t *testing.T) {
	t.Parallel()
	session := &Session{Width: 1024, Height: 768}
	demand := session.demandActive()
	offset := 14 + int(binary.LittleEndian.Uint16(demand[10:]))
	count := int(binary.LittleEndian.Uint16(demand[offset:]))
	data := demand[offset+4:]
	for range count {
		size := int(binary.LittleEndian.Uint16(data[2:]))
		if binary.LittleEndian.Uint16(data) == 1 {
			if size != 24 || data[22] != 1 || data[23] != 1 {
				t.Fatal("general capability does not advertise refresh / suppress output")
			}

			return
		}
		data = data[size:]
	}
	t.Fatal("no general capability")
}

func checkGraphicsInitialization(t *testing.T, data []byte) {
	t.Helper()
	for _, command := range []uint16{0x13, 0x0e, 9, 0x0f} {
		if len(data) < 8 || binary.LittleEndian.Uint16(data) != command {
			t.Fatalf("expected graphics command %#x, got %x", command, data)
		}
		size := int(binary.LittleEndian.Uint32(data[4:]))
		if size < 8 || size > len(data) {
			t.Fatalf("invalid initialization PDU length %d", size)
		}
		if command == 0x0e && (size != 340 || binary.LittleEndian.Uint32(data[8:]) != 1024 ||
			binary.LittleEndian.Uint32(data[12:]) != 768 || binary.LittleEndian.Uint32(data[16:]) != 1 ||
			binary.LittleEndian.Uint32(data[28:]) != 1023 || binary.LittleEndian.Uint32(data[32:]) != 767) {
			t.Fatal("invalid reset graphics monitor layout")
		}
		if command == 9 && (size != 15 || binary.LittleEndian.Uint16(data[10:]) != 1024 ||
			binary.LittleEndian.Uint16(data[12:]) != 768 || data[14] != 0x20) {
			t.Fatal("invalid created surface")
		}
		data = data[size:]
	}
	if len(data) != 0 {
		t.Fatal("unexpected trailing initialization data")
	}
}

func TestGraphicsAVC420FrameEnvelope(t *testing.T) {
	t.Parallel()
	annexB := []byte{0, 0, 1, 0x67, 0x42, 0, 0, 1, 0x65, 1, 2, 3}
	data := avc420Frame(1024, 768, 42, 1234, annexB)
	if binary.LittleEndian.Uint16(data) != 0x0b || binary.LittleEndian.Uint32(data[4:]) != 16 ||
		binary.LittleEndian.Uint32(data[8:]) != 1234 || binary.LittleEndian.Uint32(data[12:]) != 42 {
		t.Fatal("invalid start frame")
	}
	data = data[16:]
	if binary.LittleEndian.Uint16(data) != 1 || int(binary.LittleEndian.Uint32(data[4:])) != 39+len(annexB) {
		t.Fatal("invalid WireToSurface1 header")
	}
	wire := data[8:]
	if binary.LittleEndian.Uint16(wire[2:]) != 0x0b || wire[4] != 0x20 ||
		binary.LittleEndian.Uint16(wire[9:]) != 1024 || binary.LittleEndian.Uint16(wire[11:]) != 768 ||
		int(binary.LittleEndian.Uint32(wire[13:])) != 14+len(annexB) ||
		binary.LittleEndian.Uint32(wire[17:]) != 1 ||
		binary.LittleEndian.Uint16(wire[25:]) != 1024 || binary.LittleEndian.Uint16(wire[27:]) != 768 ||
		!bytes.Equal(wire[31:31+len(annexB)], annexB) {
		t.Fatal("invalid AVC420 codec, region, or Annex B bitstream")
	}
	end := data[39+len(annexB):]
	if len(end) != 12 || binary.LittleEndian.Uint16(end) != 0x0c || binary.LittleEndian.Uint32(end[8:]) != 42 {
		t.Fatal("invalid end frame")
	}
}

func TestGraphicsCapabilitiesAndFallback(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		version uint32
		flags   uint32
		want    bool
	}{
		{"avc420", graphicsVersion81, 0x10, true},
		{"no-avc420", graphicsVersion81, 0, false},
		{"version8", 0x00080004, 0, false},
		{"avc444-client", 0x000a0002, 0, true},
		{"disabled", 0x000a0002, 0x20, false},
		{"unknown", 0x10000000, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			caps := graphicsCapsFixture(tc.version, tc.flags)
			version, _, err := selectGraphicsCapabilities(caps[8:])
			if err != nil || (version != 0) != tc.want {
				t.Fatalf("selected %#x, error %v", version, err)
			}
		})
	}
	for _, data := range [][]byte{nil, {0, 0}, {65, 0}, {1, 0, 1}, {1, 0, 5, 1, 8, 0, 255, 255, 255, 255}} {
		if _, _, err := selectGraphicsCapabilities(data); err == nil {
			t.Fatalf("accepted malformed capabilities %x", data)
		}
	}
	s := &Session{}
	if started, err := s.BeginGraphics(); err != nil || started || s.GraphicsReady() || s.GraphicsCanSend() {
		t.Fatal("client without GFX failed bitmap fallback")
	}
	conn := &graphicsTestConn{}
	s = &Session{conn: conn, graphics: &graphicsState{}, dynamic: graphicsDynamicFixture(3)}
	if err := s.readGraphics(graphicsCapsFixture(graphicsVersion81, 0)); err != nil || s.GraphicsReady() {
		t.Fatalf("client without AVC420 failed bitmap fallback: %v", err)
	}
	output := graphicsOutputFixture(t, conn.outgoing.Bytes())
	if len(output) != 1 || !bytes.Equal(output[0], []byte{0x40, 1}) {
		t.Fatalf("unsupported codec should close graphics channel: %x", output)
	}
}

func TestGraphicsFragmentsAndZGFX(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, 1, 1598, 1599, 65535, 65536, 140000} {
		data := make([]byte, size)
		for i := range data {
			data[i] = byte(i)
		}
		conn := &graphicsTestConn{}
		s := &Session{conn: conn, graphics: &graphicsState{}, dynamic: graphicsDynamicFixture(0)}
		if err := s.writeGraphics(data); err != nil {
			t.Fatal(err)
		}
		fragments := graphicsOutputFixture(t, conn.outgoing.Bytes())
		var buffer fragmentBuffer
		var assembled []byte
		for i, fragment := range fragments {
			var err error
			if len(fragment) > 1600 || fragment[1] != 1 {
				t.Fatalf("invalid DVC fragment size/channel at %d", i)
			}
			if i == 0 && len(fragments) > 1 {
				if fragment[0] != 0x28 {
					t.Fatal("missing DYNVC_DATA_FIRST")
				}
				assembled, err = buffer.first(binary.LittleEndian.Uint32(fragment[2:]), fragment[6:])
			} else {
				if fragment[0] != 0x30 {
					t.Fatal("missing DYNVC_DATA")
				}
				assembled, err = buffer.next(fragment[2:])
			}
			if err != nil {
				t.Fatal(err)
			}
		}
		if !bytes.Equal(decodeZGFXFixture(t, assembled), data) {
			t.Fatalf("fragmented payload mismatch at length %d", size)
		}
	}
}

func TestGraphicsRejectsInvalidFragments(t *testing.T) {
	t.Parallel()
	var buffer fragmentBuffer
	for _, total := range []uint32{0, graphicsMaxIncoming + 1, ^uint32(0)} {
		if _, err := buffer.first(total, []byte{1}); err == nil {
			t.Fatalf("accepted invalid total %d", total)
		}
	}
	if _, err := buffer.first(4, []byte{1, 2}); err != nil {
		t.Fatal(err)
	}
	if _, err := buffer.first(4, []byte{3}); err == nil {
		t.Fatal("accepted overlapping fragmented messages")
	}
	if _, err := buffer.next([]byte{3, 4, 5}); err == nil {
		t.Fatal("accepted fragment overflow")
	}
	if _, _, err := readDynamicInteger([]byte{0, 0, 0, 0}, 3); err == nil {
		t.Fatal("accepted reserved dynamic integer size")
	}
}

func TestGraphicsStaticReassembly(t *testing.T) {
	t.Parallel()
	var buffer fragmentBuffer
	fragment := func(flags uint32, payload string) []byte {
		data := make([]byte, 8+len(payload))
		binary.LittleEndian.PutUint32(data, 6)
		binary.LittleEndian.PutUint32(data[4:], flags)
		copy(data[8:], payload)

		return data
	}
	for i, data := range [][]byte{fragment(1, "ab"), fragment(0, "cd"), fragment(2, "ef")} {
		message, err := buffer.static(data)
		if err != nil || (message != nil) != (i == 2) {
			t.Fatalf("static fragment %d = %q, %v", i, message, err)
		}
		if i == 2 && string(message) != "abcdef" {
			t.Fatalf("static message = %q", message)
		}
	}
	for _, data := range [][]byte{
		fragment(0, "abcdef"), fragment(1, "abcdef"), fragment(3, "ab"), fragment(0x200003, "abcdef"),
	} {
		var invalid fragmentBuffer
		if _, err := invalid.static(data); err == nil {
			t.Fatalf("accepted invalid static channel packet %x", data)
		}
	}
}

func FuzzGraphicsChannel(f *testing.F) {
	f.Add([]byte{0x50, 0, 1, 0})
	f.Add([]byte{0x10, 1, 0, 0, 0, 0})
	f.Add(append([]byte{0x30, 1}, graphicsCapsFixture(graphicsVersion81, 0x10)...))
	f.Fuzz(func(t *testing.T, data []byte) {
		conn := &graphicsTestConn{}
		for phase := byte(1); phase <= 3; phase++ {
			s := &Session{
				Width: 1024, Height: 768, conn: conn, graphics: &graphicsState{}, dynamic: graphicsDynamicFixture(phase),
			}
			_ = s.readDynamic(data)
			_ = s.readGraphics(data)
		}
		var fragment fragmentBuffer
		_, _ = fragment.static(data)
		_, _, _ = selectGraphicsCapabilities(data)
	})
}

type graphicsTestConn struct {
	net.Conn
	incoming *bytes.Reader
	outgoing bytes.Buffer
}

func (c *graphicsTestConn) Read(data []byte) (int, error)  { return c.incoming.Read(data) }
func (c *graphicsTestConn) Write(data []byte) (int, error) { return c.outgoing.Write(data) }

func graphicsCapsFixture(version, flags uint32) []byte {
	payload := make([]byte, 14)
	binary.LittleEndian.PutUint16(payload, 1)
	binary.LittleEndian.PutUint32(payload[2:], version)
	binary.LittleEndian.PutUint32(payload[6:], 4)
	binary.LittleEndian.PutUint32(payload[10:], flags)

	return graphicsPDU(0x12, payload)
}

func graphicsConnectFixture(graphics bool, names []string) []byte {
	core := make([]byte, 146)
	put16(core, 0, 0xc001)
	put16(core, 2, len(core))
	if graphics {
		put16(core, 144, 0x100)
	}
	network := make([]byte, 8+12*len(names))
	put16(network, 0, 0xc003)
	put16(network, 2, len(network))
	binary.LittleEndian.PutUint32(network[4:], uint32(len(names)))
	for i, name := range names {
		copy(network[8+12*i:16+12*i], name)
	}
	blocks := append(append([]byte(nil), core...), network...)
	gcc := appendPERLength([]byte{0, 5, 0, 0x14, 0x7c, 0, 1}, len(blocks)+14)
	gcc = append(gcc, 0, 8, 0, 16, 0, 1, 0xc0, 0, 'D', 'u', 'c', 'a')
	gcc = append(appendPERLength(gcc, len(blocks)), blocks...)
	body := []byte{4, 1, 1, 4, 1, 1, 1, 1, 0xff, 0x30, 0, 0x30, 0, 0x30, 0}
	body = append(body, ber(4, gcc)...)

	return append([]byte{0x7f}, ber(0x65, body)...)
}

func graphicsInputFixture(data []byte) []byte {
	channel := make([]byte, 8+len(data))
	binary.LittleEndian.PutUint32(channel, uint32(len(data)))
	binary.LittleEndian.PutUint32(channel[4:], 3)
	copy(channel[8:], data)
	mcs := appendPERLength([]byte{0x64, 0, 0, 3, 0xec, 0x70}, len(channel))
	mcs = append(mcs, channel...)
	var wire bytes.Buffer
	_ = writeTPKT(&wire, append([]byte{2, 0xf0, 0x80}, mcs...))

	return wire.Bytes()
}

func graphicsOutputFixture(t *testing.T, data []byte) [][]byte {
	t.Helper()
	reader := bytes.NewReader(data)
	var messages [][]byte
	for reader.Len() > 0 {
		mcs, err := readMCS(reader)
		if err != nil {
			t.Fatal(err)
		}
		if len(mcs) < 7 || !bytes.Equal(mcs[:6], []byte{0x68, 0, 1, 3, 0xec, 0x70}) {
			t.Fatalf("invalid static channel MCS: %x", mcs)
		}
		payload, err := takePER(mcs[6:])
		if err != nil || len(payload) < 8 || int(binary.LittleEndian.Uint32(payload)) != len(payload)-8 ||
			binary.LittleEndian.Uint32(payload[4:]) != 3 {
			t.Fatalf("invalid static channel header: %x, %v", payload, err)
		}
		messages = append(messages, payload[8:])
	}

	return messages
}

func decodeZGFXFixture(t *testing.T, data []byte) []byte {
	t.Helper()
	if len(data) >= 2 && data[0] == 0xe0 && data[1] == 4 {
		if len(data)-2 > 65535 {
			t.Fatal("oversized single ZGFX segment")
		}

		return data[2:]
	}
	if len(data) < 7 || data[0] != 0xe1 {
		t.Fatalf("invalid multipart ZGFX header %x", data)
	}
	count, total := int(binary.LittleEndian.Uint16(data[1:])), int(binary.LittleEndian.Uint32(data[3:]))
	data = data[7:]
	var result []byte
	for range count {
		if len(data) < 5 {
			t.Fatal(io.ErrUnexpectedEOF)
		}
		size := int(binary.LittleEndian.Uint32(data))
		if size < 1 || size > 65536 || size > len(data)-4 || data[4] != 4 {
			t.Fatalf("invalid ZGFX segment size %d", size)
		}
		result = append(result, data[5:4+size]...)
		data = data[4+size:]
	}
	if len(data) != 0 || len(result) != total {
		t.Fatal("ZGFX segment lengths disagree")
	}

	return result
}

func graphicsDynamicFixture(phase byte) *dynamicState {
	managerPhase := byte(2)
	if phase == 1 {
		managerPhase = 1
	}

	return &dynamicState{channel: 1004, phase: managerPhase, graphics: dynamicEndpoint{phase: phase}}
}
