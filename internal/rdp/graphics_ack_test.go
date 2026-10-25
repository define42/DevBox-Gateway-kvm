package rdp

import (
	"encoding/binary"
	"image"
	"net"
	"testing"
	"testing/synctest"
	"time"
)

// Turnaround starts after the final transport write completes, for both
// damage frames and frames that repaint the complete desktop.
func TestGraphicsACKTurnaroundExcludesTransportWrite(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name      string
		fullFrame bool
	}{
		{name: "damage"},
		{name: "full_frame", fullFrame: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				conn := newACKBlockedTransport(t)
				session := newACKSession(conn)
				written := make(chan blockedGraphicsWriteResult, 1)
				conn.workers.Go(func() {
					sent, err := writeACKFrame(session, test.fullFrame)
					written <- blockedGraphicsWriteResult{sent: sent, err: err}
				})
				<-conn.writeStarted
				time.Sleep(250 * time.Millisecond)
				checkACKStats(t, session, 0, 1)
				conn.release()
				if result := <-written; !result.sent || result.err != nil {
					t.Fatalf("write frame = %+v", result)
				}
				time.Sleep(12 * time.Millisecond)
				receiveACK(t, session, 1, 0)
				checkACKStats(t, session, 12*time.Millisecond, 0)
				if got, want := session.GraphicsStats().FrameInterval, time.Second/30; got != want {
					t.Fatalf("pacing = %v, want %v; blocked write must not inflate ACK pacing", got, want)
				}
			})
		})
	}
}

func TestGraphicsACKTurnaroundSmoothingAndReset(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		session := newACKSession(&discardTransportConn{})
		sendACKFrame(t, session)
		time.Sleep(4 * time.Millisecond)
		receiveACK(t, session, 1, 0)
		checkACKStats(t, session, 4*time.Millisecond, 0)

		sendACKFrame(t, session)
		time.Sleep(2 * time.Millisecond)
		// Neither a duplicate nor an unknown ID measures the pending frame.
		receiveACK(t, session, 1, 0)
		receiveACK(t, session, 99, 0)
		checkACKStats(t, session, 4*time.Millisecond, 1)
		time.Sleep(18 * time.Millisecond)
		receiveACK(t, session, 2, 0)
		// A 20 ms sample changes the 4 ms average by one eighth of the gap.
		checkACKStats(t, session, 6*time.Millisecond, 0)

		sendACKFrame(t, session)
		time.Sleep(14 * time.Millisecond)
		receiveACK(t, session, 3, 0)
		checkACKStats(t, session, 7*time.Millisecond, 0)

		sendACKFrame(t, session)
		time.Sleep(time.Millisecond)
		receiveACK(t, session, 4, 0)
		checkACKStats(t, session, 6250*time.Microsecond, 0)

		// A new ACK generation must seed a fresh sample, not retain the EWMA.
		receiveACK(t, session, 4, ^uint32(0))
		checkACKStats(t, session, 0, 0)
		time.Sleep(time.Second)
		receiveACK(t, session, 4, 0)
		checkACKStats(t, session, 0, 0)
		sendACKFrame(t, session)
		time.Sleep(12 * time.Millisecond)
		receiveACK(t, session, 5, 0)
		checkACKStats(t, session, 12*time.Millisecond, 0)
	})
}

func TestGraphicsACKDuringWritePreservesTurnaround(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		session := newACKSession(&discardTransportConn{})
		sendACKFrame(t, session)
		time.Sleep(8 * time.Millisecond)
		receiveACK(t, session, 1, 0)

		// The client can ACK bytes already delivered while Write is returning.
		conn := newACKBlockedTransport(t)
		session.conn = conn
		written := make(chan blockedGraphicsWriteResult, 1)
		conn.workers.Go(func() {
			sent, err := writeACKFrame(session, true)
			written <- blockedGraphicsWriteResult{sent: sent, err: err}
		})
		<-conn.writeStarted
		time.Sleep(5 * time.Millisecond)
		receiveACK(t, session, 2, 0)
		checkACKStats(t, session, 8*time.Millisecond, 0)
		time.Sleep(200 * time.Millisecond)
		conn.release()
		if result := <-written; !result.sent || result.err != nil {
			t.Fatalf("write frame = %+v", result)
		}
		checkACKStats(t, session, 8*time.Millisecond, 0)

		// The early ACK neither reappears nor poisons the next completed sample.
		sendACKFrame(t, session)
		time.Sleep(16 * time.Millisecond)
		receiveACK(t, session, 3, 0)
		checkACKStats(t, session, 9*time.Millisecond, 0)
	})
}

func newACKSession(conn net.Conn) *Session {
	session := &Session{
		Width: 128, Height: 128, conn: conn,
		graphics: &graphicsState{ready: true}, dynamic: graphicsDynamicFixture(3),
	}
	session.GraphicsFrameInterval(time.Second / 30)

	return session
}

func newACKBlockedTransport(t *testing.T) *blockedGraphicsConn {
	t.Helper()
	conn := &blockedGraphicsConn{
		writeStarted: make(chan struct{}), releaseWrite: make(chan struct{}),
	}
	t.Cleanup(func() {
		conn.release()
		conn.workers.Wait()
	})

	return conn
}

func writeACKFrame(session *Session, fullFrame bool) (bool, error) {
	frame := []byte{0, 0, 0, 1, 0x41, 0xaa}
	regions := []image.Rectangle{image.Rect(0, 112, 128, 128)}
	if fullFrame {
		return session.WriteAVC420(frame)
	}

	return session.WriteAVC420Damage(frame, regions)
}

func sendACKFrame(t *testing.T, session *Session) {
	t.Helper()
	if sent, err := writeACKFrame(session, false); !sent || err != nil {
		t.Fatalf("write frame = %v, %v", sent, err)
	}
}

func receiveACK(t *testing.T, session *Session, frameID, queueDepth uint32) {
	t.Helper()
	var ack [12]byte
	binary.LittleEndian.PutUint32(ack[:], queueDepth)
	binary.LittleEndian.PutUint32(ack[4:], frameID)
	if err := session.readGraphicsPDU(0x0d, ack[:]); err != nil {
		t.Fatalf("receive ACK for frame %d: %v", frameID, err)
	}
}

func checkACKStats(t *testing.T, session *Session, turnaround time.Duration, outstanding uint32) {
	t.Helper()
	if got := session.GraphicsStats(); got.ACKTurnaround != turnaround || got.OutstandingACKs != outstanding {
		t.Fatalf("ACK stats = %+v, want turnaround %v, outstanding %d", got, turnaround, outstanding)
	}
}
