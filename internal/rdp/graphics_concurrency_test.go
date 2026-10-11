package rdp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestGraphicsInputDuringBlockedFrameWrite(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		ack        bool
		frameID    uint32
		queueDepth uint32
		pending    []uint32
	}{
		{name: "keyboard", pending: []uint32{1, 2}},
		{name: "previous_frame_ACK", ack: true, frameID: 1, pending: []uint32{2}},
		{name: "current_frame_ACK", ack: true, frameID: 2},
		{name: "suspend_ACK", ack: true, queueDepth: ^uint32(0)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var incoming []byte
			if tc.ack {
				ack := make([]byte, 12)
				binary.LittleEndian.PutUint32(ack, tc.queueDepth)
				binary.LittleEndian.PutUint32(ack[4:], tc.frameID)
				incoming = graphicsInputFixture(append([]byte{0x30, 1}, graphicsPDU(0x0d, ack)...))
			}
			incoming = append(incoming, 4, 4, 0, 0x1e) // Fast-path 'a' key down.
			session, conn, written := blockedGraphicsFrame(t, incoming, nil)
			input := readBlockedGraphicsInput(t, session, conn)
			select {
			case result := <-input:
				checkBlockedGraphicsInput(t, result)
			case <-time.After(time.Second):
				t.Fatal("available keyboard input stalled behind a graphics ACK during a blocked write")
			}
			select {
			case result := <-written:
				t.Fatalf("graphics write finished before transport release: %+v", result)
			default:
			}
			g := session.graphics
			g.mu.Lock()
			pending := append([]graphicsPendingFrame(nil), g.inFlight...)
			g.mu.Unlock()
			if len(pending) != len(tc.pending) {
				t.Fatalf("pending frames = %v, want %v", pending, tc.pending)
			}
			for i, frameID := range tc.pending {
				if pending[i].id != frameID {
					t.Fatalf("pending frames = %v, want %v", pending, tc.pending)
				}
			}
			conn.release()
			if result := <-written; !result.sent || result.err != nil {
				t.Fatalf("completed graphics write = %+v", result)
			}
			if len(g.inFlight) != len(tc.pending) {
				t.Fatal("write completion restored an acknowledged or suspended frame")
			}
			for _, frame := range g.inFlight {
				if frame.sentAt.IsZero() {
					t.Fatal("completed frame has no send timestamp")
				}
			}
			if tc.queueDepth == ^uint32(0) && (g.averageBytes != 0 || g.ackTurnaround != 0) {
				t.Fatal("write completion restored suspended acknowledgment statistics")
			}
		})
	}
}

func TestGraphicsInputDuringBlockedResize(t *testing.T) {
	t.Parallel()
	ack := make([]byte, 12)
	binary.LittleEndian.PutUint32(ack[4:], 1)
	incoming := graphicsInputFixture(append([]byte{0x30, 1}, graphicsPDU(0x0d, ack)...))
	incoming = append(incoming, 4, 4, 0, 0x1e)
	session, conn := newBlockedGraphicsSession(t, incoming, nil)
	resized := make(chan blockedGraphicsWriteResult, 1)
	conn.workers.Go(func() {
		changed, err := session.Resize(1280, 720)
		resized <- blockedGraphicsWriteResult{sent: changed, err: err}
	})
	select {
	case <-conn.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("graphics resize did not reach transport")
	}
	input := readBlockedGraphicsInput(t, session, conn)
	select {
	case result := <-input:
		checkBlockedGraphicsInput(t, result)
	case <-time.After(time.Second):
		t.Fatal("keyboard input stalled behind a graphics ACK during resize")
	}
	if width, height := session.Size(); width != 1024 || height != 768 {
		t.Fatal("resize committed geometry before the surface was sent")
	}
	conn.release()
	if result := <-resized; !result.sent || result.err != nil {
		t.Fatalf("completed graphics resize = %+v", result)
	}
	if width, height := session.Size(); width != 1280 || height != 720 {
		t.Fatal("resize did not commit geometry after sending the surface")
	}
	if len(session.graphics.inFlight) != 0 || session.graphics.frameID != 1 {
		t.Fatal("resize did not retire the old ACK window while preserving frame IDs")
	}
}

func TestGraphicsFailedWriteRetiresReservation(t *testing.T) {
	t.Parallel()
	session, conn, written := blockedGraphicsFrame(t, nil, io.ErrClosedPipe)
	conn.release()
	result := <-written
	if result.sent || !errors.Is(result.err, io.ErrClosedPipe) {
		t.Fatalf("partially failed write = %+v", result)
	}
	g := session.graphics
	if g.frameID != 2 || len(g.inFlight) != 1 || g.inFlight[0].id != 1 || g.averageBytes != 0 {
		t.Fatalf("failed write retained reservation or statistics: ID=%d pending=%v bytes=%d",
			g.frameID, g.inFlight, g.averageBytes)
	}
}

func TestGraphicsCloseDuringBlockedFrameWrite(t *testing.T) {
	t.Parallel()
	incoming := append(graphicsInputFixture([]byte{0x40, 1}), 4, 4, 0, 0x1e)
	session, conn, written := blockedGraphicsFrame(t, incoming, nil)
	changed := session.GraphicsChanged()
	input := readBlockedGraphicsInput(t, session, conn)
	select {
	case <-changed:
	case <-time.After(time.Second):
		t.Fatal("graphics channel close could not retire its ACK window during a write")
	}
	if session.GraphicsReady() {
		t.Fatal("closed graphics channel remained ready")
	}
	conn.release()
	if result := <-written; !result.sent || result.err != nil {
		t.Fatalf("in-progress frame write = %+v", result)
	}
	checkBlockedGraphicsInput(t, <-input)
	g := session.graphics
	if !g.closed || len(g.inFlight) != 0 || g.averageBytes != 0 {
		t.Fatal("write completion restored closed graphics state")
	}
	output := graphicsOutputFixture(t, conn.outgoing.Bytes())
	if len(output) < 2 || !bytes.Equal(output[len(output)-1], []byte{0x40, 1}) {
		t.Fatal("graphics close response did not follow the complete fragmented frame")
	}
	for _, message := range output[:len(output)-1] {
		if message[0]>>4 != 2 && message[0]>>4 != 3 {
			t.Fatalf("close response interrupted outgoing graphics fragments: %x", message[:2])
		}
	}
}

type blockedGraphicsWriteResult struct {
	sent bool
	err  error
}

type blockedGraphicsInputResult struct {
	events []InputEvent
	err    error
}

func blockedGraphicsFrame(t *testing.T, incoming []byte, writeErr error) (
	*Session, *blockedGraphicsConn, <-chan blockedGraphicsWriteResult,
) {
	t.Helper()
	session, conn := newBlockedGraphicsSession(t, incoming, writeErr)
	written := make(chan blockedGraphicsWriteResult, 1)
	conn.workers.Go(func() {
		frame := make([]byte, 64<<10)
		copy(frame, []byte{0, 0, 0, 1, 0x41})
		sent, err := session.WriteAVC420(frame)
		written <- blockedGraphicsWriteResult{sent: sent, err: err}
	})
	select {
	case <-conn.writeStarted:
	case <-time.After(time.Second):
		t.Fatal("graphics writer did not reach transport")
	}

	return session, conn, written
}

func newBlockedGraphicsSession(t *testing.T, incoming []byte, writeErr error) (*Session, *blockedGraphicsConn) {
	t.Helper()
	conn := &blockedGraphicsConn{
		incoming: bytes.NewReader(incoming), writeErr: writeErr,
		writeStarted: make(chan struct{}), releaseWrite: make(chan struct{}),
	}
	session := &Session{
		Width: 1024, Height: 768, conn: conn,
		joined: map[uint16]bool{1004: true},
		dynamic: &dynamicState{
			channel: 1004, phase: 2, graphics: dynamicEndpoint{phase: 3},
		},
		graphics: &graphicsState{
			supported: true, ready: true, frameID: 1,
			inFlight: []graphicsPendingFrame{{id: 1, sentAt: time.Now()}},
		},
	}
	t.Cleanup(func() {
		conn.release()
		conn.workers.Wait()
	})

	return session, conn
}

func readBlockedGraphicsInput(
	t *testing.T, session *Session, conn *blockedGraphicsConn,
) <-chan blockedGraphicsInputResult {
	t.Helper()
	input := make(chan blockedGraphicsInputResult, 1)
	conn.workers.Go(func() {
		data, fast, err := session.ReadPacket()
		var events []InputEvent
		if err == nil {
			var decoder InputDecoder
			events, err = decoder.Decode(data, fast)
		}
		input <- blockedGraphicsInputResult{events: events, err: err}
	})

	return input
}

func checkBlockedGraphicsInput(t *testing.T, result blockedGraphicsInputResult) {
	t.Helper()
	if result.err != nil || len(result.events) != 1 || result.events[0].Kind != InputKey ||
		!result.events[0].Down || result.events[0].Key != 'a' {
		t.Fatalf("keyboard result = %+v", result)
	}
}

type blockedGraphicsConn struct {
	net.Conn
	incoming     *bytes.Reader // Owned by the sole Session reader.
	outgoing     bytes.Buffer  // Writes are serialized by Session.writeMu.
	writeErr     error
	writeStarted chan struct{}
	writeOnce    sync.Once
	releaseWrite chan struct{}
	releaseOnce  sync.Once
	workers      sync.WaitGroup
}

func (c *blockedGraphicsConn) Read(data []byte) (int, error) {
	return c.incoming.Read(data)
}

func (c *blockedGraphicsConn) Write(data []byte) (int, error) {
	c.writeOnce.Do(func() { close(c.writeStarted) })
	<-c.releaseWrite
	if c.writeErr != nil {
		return len(data) / 2, c.writeErr
	}

	return c.outgoing.Write(data)
}

func (c *blockedGraphicsConn) release() {
	c.releaseOnce.Do(func() { close(c.releaseWrite) })
}
