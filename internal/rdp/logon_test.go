package rdp

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"testing"
)

func readLogonNotificationFixture(t *testing.T, conn net.Conn) {
	t.Helper()
	notify := readGlobalFixture(t, conn)
	// MS-RDPBCGR 2.2.10.1.1 and 2.2.10.1.1.3: a Share Data header,
	// INFOTYPE_LOGON_PLAINNOTIFY, and exactly 576 bytes of padding.
	if len(notify) != 18+4+576 || notify[14] != 0x26 ||
		binary.LittleEndian.Uint16(notify[2:4]) != 0x17 ||
		binary.LittleEndian.Uint32(notify[18:22]) != 2 {
		t.Fatalf("invalid Save Session Info plain notification: %x", notify)
	}
	if notify[15] != 0 || binary.LittleEndian.Uint16(notify[16:18]) != 0 {
		t.Fatal("logon notification must not be compressed")
	}
	if !bytes.Equal(notify[22:], make([]byte, 576)) {
		t.Fatal("plain logon notification padding contains nonzero data")
	}
}

func TestInitialActivationNotifiesLogonBeforeGraphics(t *testing.T) {
	t.Parallel()
	conn := &graphicsTestConn{}
	session := &Session{conn: conn}
	initial := &activation{session: session, controlled: true}
	fontList := shareDataFixture(0x27, []byte{0, 0, 0, 0, 1, 0, 0x32, 0})
	if done, err := initial.finalize(fontList); done || err != nil || conn.outgoing.Len() != 0 {
		t.Fatalf("nonfinal Font List completed logon: done=%v err=%v output=%x", done, err, conn.outgoing.Bytes())
	}
	fontList[22] = 3
	if done, err := initial.finalize(fontList); !done || err != nil {
		t.Fatalf("initial activation failed: done=%v err=%v", done, err)
	}
	if err := session.WriteDataPDU(2, []byte{1, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	// A subsequent display activation must send Font Map without pretending
	// the already-authorized user has logged on a second time.
	resize := &activation{session: session, controlled: true, reactivation: true}
	if done, err := resize.finalize(fontList); !done || err != nil {
		t.Fatalf("display reactivation failed: done=%v err=%v", done, err)
	}
	reader := bytes.NewReader(conn.outgoing.Bytes())
	wire := readerConn{Reader: reader}
	if font := readGlobalFixture(t, wire); len(font) != 26 || font[14] != 0x28 {
		t.Fatalf("expected initial Font Map before logon notification, got %x", font)
	}
	readLogonNotificationFixture(t, wire)
	if bitmap := readGlobalFixture(t, wire); len(bitmap) != 22 || bitmap[14] != 2 {
		t.Fatalf("expected graphics after one logon notification, got %x", bitmap)
	}
	if font := readGlobalFixture(t, wire); len(font) != 26 || font[14] != 0x28 {
		t.Fatalf("expected reactivation Font Map, got %x", font)
	}
	if reader.Len() != 0 {
		t.Fatal("display reactivation sent an extra logon notification")
	}
}

type logonWriteFailureConn struct {
	net.Conn
	writes, failAt int
	output         bytes.Buffer
}

func (c *logonWriteFailureConn) Write(data []byte) (int, error) {
	c.writes++
	if c.writes == c.failAt {
		return 0, io.ErrClosedPipe
	}

	return c.output.Write(data)
}

func TestActivationPropagatesLogonWriteFailure(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		failAt int
	}{{"Font Map", 1}, {"Save Session Info", 2}} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conn := &logonWriteFailureConn{failAt: test.failAt}
			state := activation{session: &Session{conn: conn}, controlled: true}
			fontList := shareDataFixture(0x27, []byte{0, 0, 0, 0, 3, 0, 0x32, 0})
			done, err := state.finalize(fontList)
			if done || !errors.Is(err, io.ErrClosedPipe) || conn.writes != test.failAt {
				t.Fatalf("write failure reported successful activation: done=%v err=%v writes=%d", done, err, conn.writes)
			}
			if test.failAt == 1 && conn.output.Len() != 0 {
				t.Fatal("sent logon notification despite failed Font Map")
			}
			if test.failAt == 2 {
				reader := bytes.NewReader(conn.output.Bytes())
				font := readGlobalFixture(t, readerConn{Reader: reader})
				if len(font) != 26 || font[14] != 0x28 || reader.Len() != 0 {
					t.Fatalf("failed logon notification corrupted preceding Font Map: %x", font)
				}
			}
		})
	}
}
