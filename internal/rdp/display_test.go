package rdp

import (
	"bytes"
	"encoding/binary"
	"net"
	"sync"
	"testing"
	"time"
)

func TestDisplayControlNegotiationAndLatestRequest(t *testing.T) {
	t.Parallel()
	traffic := bytes.Join([][]byte{
		graphicsInputFixture([]byte{0x50, 0, 1, 0}),
		graphicsInputFixture([]byte{0x10, 2, 0, 0, 0, 0}),
		graphicsInputFixture(append([]byte{0x30, 2}, displayLayoutFixture(1280, 720)...)),
		graphicsInputFixture(append([]byte{0x30, 2}, displayLayoutFixture(1921, 1081)...)),
		{4, 4, 0, 0x1e},
	}, nil)
	conn := &graphicsTestConn{incoming: bytes.NewReader(traffic)}
	s := &Session{
		Width: 1024, Height: 768, conn: conn,
		joined: map[uint16]bool{1004: true}, dynamic: &dynamicState{channel: 1004},
	}
	requests := s.ResizeRequests()
	if requests != s.ResizeRequests() {
		t.Fatal("resize request channel changed")
	}
	if started, err := s.BeginDisplayControl(); err != nil || !started {
		t.Fatalf("begin display control: %v, %v", started, err)
	}
	data, fast, err := s.ReadPacket()
	if err != nil || !fast || !bytes.Equal(data, []byte{4, 0, 0x1e}) {
		t.Fatalf("input after display control negotiation: %x, %v, %v", data, fast, err)
	}
	select {
	case size := <-requests:
		if size != (DesktopSize{Width: 1920, Height: 1080}) {
			t.Fatalf("latest requested size = %+v", size)
		}
	default:
		t.Fatal("missing resize request")
	}
	select {
	case <-requests:
		t.Fatal("obsolete resize request was not coalesced")
	default:
	}
	output := graphicsOutputFixture(t, conn.outgoing.Bytes())
	if len(output) != 3 || !bytes.Equal(output[1], append([]byte{0x10, 2}, displayChannelName...)) {
		t.Fatalf("display control channel negotiation = %x", output)
	}
	caps := output[2]
	if len(caps) != 22 || !bytes.Equal(caps[:2], []byte{0x30, 2}) ||
		binary.LittleEndian.Uint32(caps[2:]) != 5 || binary.LittleEndian.Uint32(caps[6:]) != 20 ||
		binary.LittleEndian.Uint32(caps[10:]) != 1 ||
		binary.LittleEndian.Uint32(caps[14:])*binary.LittleEndian.Uint32(caps[18:]) != maxDesktopArea {
		t.Fatalf("invalid display control capabilities: %x", caps)
	}
	if width, height := s.Size(); width != 1024 || height != 768 {
		t.Fatal("parsing a request changed geometry before the writer applied it")
	}
}

func TestDynamicChannelsInterleaveFragments(t *testing.T) {
	t.Parallel()
	caps := graphicsCapsFixture(graphicsVersion81, 0x10)
	traffic := bytes.Join([][]byte{
		graphicsInputFixture([]byte{0x5c, 0, 3, 0}), // Windows-style capabilities.
		graphicsInputFixture([]byte{0x10, 1, 0, 0, 0, 0}),
		graphicsInputFixture([]byte{0x10, 2, 0, 0, 0, 0}),
		graphicsInputFixture(append([]byte{0x20, 1, byte(len(caps))}, caps[:10]...)),
		graphicsInputFixture(append([]byte{0x30, 2}, displayLayoutFixture(1600, 900)...)),
		graphicsInputFixture(append([]byte{0x30, 1}, caps[10:]...)),
		{4, 4, 0, 0x1e},
	}, nil)
	conn := &graphicsTestConn{incoming: bytes.NewReader(traffic)}
	s := &Session{
		Width: 1024, Height: 768, conn: conn,
		joined: map[uint16]bool{1004: true}, dynamic: &dynamicState{channel: 1004},
		graphics: &graphicsState{supported: true},
	}
	if _, err := s.BeginDisplayControl(); err != nil {
		t.Fatal(err)
	}
	if _, err := s.BeginGraphics(); err != nil {
		t.Fatal(err)
	}
	if _, _, err := s.ReadPacket(); err != nil {
		t.Fatal(err)
	}
	if !s.GraphicsReady() {
		t.Fatal("graphics failed when display control was interleaved")
	}
	select {
	case size := <-s.ResizeRequests():
		if size != (DesktopSize{Width: 1600, Height: 900}) {
			t.Fatalf("incorrect interleaved display request: %+v", size)
		}
	default:
		t.Fatal("display request lost during graphics fragment reassembly")
	}
	output := graphicsOutputFixture(t, conn.outgoing.Bytes())
	count := 0
	for _, data := range output {
		if data[0] == 0x50 {
			count++
		}
	}
	if count != 1 {
		t.Fatal("dynamic capabilities were negotiated separately for each logical channel")
	}
}

func TestDisplayRejectsInvalidGeometryWithoutDisconnect(t *testing.T) {
	t.Parallel()
	s := &Session{Width: 1024, Height: 768}
	requests := s.ResizeRequests()
	for _, size := range []DesktopSize{
		{Width: 0, Height: 768},
		{Width: 199, Height: 768},
		{Width: 4098, Height: 768},
		{Width: 1024, Height: 199},
		{Width: 4096, Height: 4096},
		{Width: 4097, Height: 768},
	} {
		s.readDisplayControl(displayLayoutFixture(size.Width, size.Height))
		if changed, err := s.Resize(size.Width, size.Height); err != nil || changed {
			t.Fatalf("invalid resize applied: %+v (%v)", size, err)
		}
	}
	valid := displayLayoutFixture(1920, 1080)
	for _, offset := range []int{0, 4, 8, 12, 16, 20, 24} {
		invalid := bytes.Clone(valid)
		binary.LittleEndian.PutUint32(invalid[offset:], ^uint32(0))
		s.readDisplayControl(invalid)
	}
	s.readDisplayControl(valid[:55])
	s.readDisplayControl(append(bytes.Clone(valid), 0))
	select {
	case got := <-requests:
		t.Fatalf("invalid layout queued a resize: %+v", got)
	default:
	}
	if !s.DisplayReady() {
		t.Fatal("invalid resize deactivated the desktop")
	}
}

func TestGraphicsResizeResetsSurfaceAndRetiresOldAcknowledgments(t *testing.T) {
	t.Parallel()
	conn := &graphicsTestConn{}
	s := &Session{
		Width: 1024, Height: 768, conn: conn, dynamic: graphicsDynamicFixture(3),
		graphics: &graphicsState{ready: true, frameID: 40, inFlight: []graphicsPendingFrame{{id: 39}, {id: 40}}},
	}
	changed := s.DisplayChanged()
	if changed != s.DisplayChanged() {
		t.Fatal("display change channel changed")
	}
	if resized, err := s.Resize(1920, 1080); err != nil || !resized {
		t.Fatalf("resize: %v, %v", resized, err)
	}
	if width, height := s.Size(); width != 1920 || height != 1080 {
		t.Fatalf("resized geometry = %dx%d", width, height)
	}
	if !s.DisplayReady() || !s.GraphicsCanSend() {
		t.Fatal("graphics resize left an old acknowledgment blocking the writer")
	}
	select {
	case <-changed:
	default:
		t.Fatal("resize did not wake writer")
	}
	output := graphicsOutputFixture(t, conn.outgoing.Bytes())
	data := decodeZGFXFixture(t, output[0][2:])
	for _, command := range []uint16{0x0a, 0x0e, 9, 0x0f} {
		if len(data) < 8 || binary.LittleEndian.Uint16(data) != command {
			t.Fatalf("resize command order, expected %#x: %x", command, data)
		}
		if command == 0x0e && (binary.LittleEndian.Uint32(data[8:]) != 1920 ||
			binary.LittleEndian.Uint32(data[12:]) != 1080) {
			t.Fatal("reset uses old geometry")
		}
		data = data[binary.LittleEndian.Uint32(data[4:]):]
	}
	if len(data) != 0 {
		t.Fatal("unexpected extra resize commands")
	}
	if sent, err := s.WriteAVC420([]byte{0, 0, 1, 0x65}); err != nil || !sent {
		t.Fatalf("new frame after resize: %v, %v", sent, err)
	}
	ack := make([]byte, 12)
	binary.LittleEndian.PutUint32(ack[4:], 40)
	if err := s.readGraphicsPDU(0x0d, ack); err != nil {
		t.Fatal(err)
	}
	if len(s.graphics.inFlight) != 1 || s.graphics.inFlight[0].id != 41 {
		t.Fatal("late acknowledgment retired a frame in the new geometry")
	}
	if resized, err := s.Resize(1920, 1080); err != nil || resized {
		t.Fatalf("duplicate geometry triggered a resize: %v, %v", resized, err)
	}
}

func TestBitmapResizeReactivatesOnExistingConnection(t *testing.T) {
	t.Parallel()
	server, client := net.Pipe()
	t.Cleanup(func() { _ = server.Close(); _ = client.Close() })
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	s := &Session{Width: 800, Height: 600, BitsPerPixel: 24, conn: server}
	changed := s.DisplayChanged()
	resized := make(chan error, 1)
	go func() {
		_, err := s.Resize(1280, 720)
		resized <- err
	}()
	deactivate := readGlobalFixture(t, client)
	if kind, err := shareType(deactivate); err != nil || kind != 6 {
		t.Fatalf("expected DeactivateAll, got %x, %v", deactivate, err)
	}
	demand := readGlobalFixture(t, client)
	if kind, err := shareType(demand); err != nil || kind != 1 {
		t.Fatalf("expected new DemandActive, got %x, %v", demand, err)
	}
	if err := <-resized; err != nil {
		t.Fatal(err)
	}
	if s.DisplayReady() {
		t.Fatal("bitmap rendering was not suspended during reactivation")
	}
	if accepted, err := s.Resize(1600, 900); err != nil || accepted {
		t.Fatal("overlapping reactivation was accepted")
	}
	<-changed // Resize-start notification.
	type readResult struct {
		data []byte
		fast bool
		err  error
	}
	read := make(chan readResult, 1)
	go func() {
		data, fast, err := s.ReadPacket()
		read <- readResult{data, fast, err}
	}()
	slowInput := shareDataFixture(28, []byte{1, 0, 0, 0, 0, 0, 0, 0, 4, 0, 0, 0, 0x1e, 0, 0, 0})
	writeGlobalFixture(t, client, slowInput)
	activateSessionFixture(t, client)
	result := <-read
	if result.err != nil || result.fast || !bytes.Equal(result.data, slowInput) {
		t.Fatalf("reactivation input = %x, %v, %v", result.data, result.fast, result.err)
	}
	fastInput, fast, err := s.ReadPacket()
	if err != nil || !fast || !bytes.Equal(fastInput, []byte{4, 0, 0x1e}) {
		t.Fatalf("queued fast input = %x, %v, %v", fastInput, fast, err)
	}
	if width, height := s.Size(); width != 1280 || height != 720 || !s.DisplayReady() {
		t.Fatalf("desktop after reactivation = %dx%d, ready=%v", width, height, s.DisplayReady())
	}
	select {
	case <-changed:
	default:
		t.Fatal("completed bitmap reactivation did not notify writer")
	}
}

func TestDisplaySizeSnapshotConcurrentResize(t *testing.T) {
	t.Parallel()
	s := &Session{
		Width: 1024, Height: 768, conn: &graphicsTestConn{},
		dynamic: graphicsDynamicFixture(3), graphics: &graphicsState{ready: true},
	}
	var readers sync.WaitGroup
	readers.Add(1)
	go func() {
		defer readers.Done()
		for range 500 {
			width, height := s.Size()
			if (width != 1024 || height != 768) && (width != 1920 || height != 1080) {
				t.Errorf("inconsistent geometry snapshot %dx%d", width, height)
			}
			_ = s.DisplayReady()
			_ = s.GraphicsCanSend()
		}
	}()
	for i := range 100 {
		width, height := 1024, 768
		if i%2 == 0 {
			width, height = 1920, 1080
		}
		if _, err := s.Resize(width, height); err != nil {
			t.Fatal(err)
		}
	}
	readers.Wait()
}

func FuzzDisplayControl(f *testing.F) {
	f.Add(displayLayoutFixture(1920, 1080))
	f.Add(displayLayoutFixture(0, 0))
	f.Fuzz(func(t *testing.T, data []byte) {
		s := &Session{}
		s.readDisplayControl(data)
		select {
		case size := <-s.ResizeRequests():
			if !validDesktopSize(size.Width, size.Height) {
				t.Fatalf("accepted invalid dimensions: %+v", size)
			}
		default:
		}
	})
}

func displayLayoutFixture(width, height int) []byte {
	data := make([]byte, 56)
	binary.LittleEndian.PutUint32(data, 2)
	binary.LittleEndian.PutUint32(data[4:], 56)
	binary.LittleEndian.PutUint32(data[8:], 40)
	binary.LittleEndian.PutUint32(data[12:], 1)
	binary.LittleEndian.PutUint32(data[16:], 1)
	binary.LittleEndian.PutUint32(data[28:], uint32(width))
	binary.LittleEndian.PutUint32(data[32:], uint32(height))
	binary.LittleEndian.PutUint32(data[48:], 100)
	binary.LittleEndian.PutUint32(data[52:], 100)

	return data
}
