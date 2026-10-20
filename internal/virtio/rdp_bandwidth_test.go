package virtio

import (
	"bytes"
	"crypto/tls"
	"errors"
	"io"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func TestRDPBandwidthPartialIO(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	transport := &bandwidthTestConn{
		reader: func(data []byte) (int, error) { return copy(data, "read"), io.EOF },
		writer: func([]byte) (int, error) { return 3, io.ErrClosedPipe },
	}
	conn := trackRDPBandwidth(transport, collector)
	data := make([]byte, 10)
	if count, err := conn.Read(data); count != 4 || !errors.Is(err, io.EOF) || string(data[:count]) != "read" {
		t.Fatalf("read result changed: n=%d err=%v data=%q", count, err, data)
	}
	if count, err := conn.Write([]byte("longer")); count != 3 || !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("write result changed: n=%d err=%v", count, err)
	}
	transport.reader = func([]byte) (int, error) { return 0, io.EOF }
	transport.writer = func([]byte) (int, error) { return 0, io.ErrClosedPipe }
	_, _ = conn.Read(data)
	_, _ = conn.Write(data)
	network := collector.Snapshot().Network
	if network.SentBytes != 3 || network.ReceivedBytes != 4 {
		t.Fatalf("partial I/O accounting: %+v", network)
	}
}

func TestRDPBandwidthPreservesTransport(t *testing.T) {
	t.Parallel()
	transport := &bandwidthTestConn{}
	if got := trackRDPBandwidth(transport, nil); got != transport {
		t.Fatal("disabled accounting wrapped the connection")
	}
	conn := trackRDPBandwidth(transport, telemetry.New())
	if conn.LocalAddr().String() != "local" || conn.RemoteAddr().String() != "remote" {
		t.Fatal("connection addresses changed")
	}
	deadline := time.Unix(100, 0)
	if err := conn.SetDeadline(deadline); err != nil || transport.deadline != deadline {
		t.Fatalf("deadline not forwarded: %v", err)
	}
	if err := conn.SetReadDeadline(deadline.Add(time.Second)); err != nil ||
		transport.readDeadline != deadline.Add(time.Second) {
		t.Fatalf("read deadline not forwarded: %v", err)
	}
	if err := conn.SetWriteDeadline(deadline.Add(2 * time.Second)); err != nil ||
		transport.writeDeadline != deadline.Add(2*time.Second) {
		t.Fatalf("write deadline not forwarded: %v", err)
	}
	if err := conn.Close(); !errors.Is(err, io.ErrClosedPipe) || !transport.closed {
		t.Fatalf("close not forwarded: %v", err)
	}
}

func TestRDPBandwidthCopyCannotBypassCounters(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	var written, received bytes.Buffer
	transport := &bandwidthTestConn{reader: strings.NewReader("received").Read, writer: written.Write}
	conn := trackRDPBandwidth(transport, collector)
	if _, err := io.Copy(conn, io.LimitReader(strings.NewReader("sent"), 4)); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(&received, conn); err != nil {
		t.Fatal(err)
	}
	network := collector.Snapshot().Network
	if written.String() != "sent" || received.String() != "received" ||
		network.SentBytes != 4 || network.ReceivedBytes != 8 {
		t.Fatalf(
			"io.Copy bypassed accounting: sent=%q received=%q counters=%+v",
			written.String(), received.String(), network,
		)
	}
}

func TestRDPBandwidthHandshakeAndReconnect(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	display := newRDPTestDisplayWithConfig(t, RDPConfig{Telemetry: collector})
	var previous telemetry.NetworkSnapshot
	for range 2 {
		conn := dialRDPTestDisplay(t, display)
		negotiateRDPTestDisplay(t, conn, 1)
		secured := tls.Client(conn, rdpTestClientTLS(t, display))
		if err := secured.Handshake(); err != nil {
			t.Fatal(err)
		}
		// Closing the underlying connection ends incomplete MCS negotiation,
		// ensuring all transport observations precede the snapshot below.
		if err := conn.Close(); err != nil {
			t.Fatal(err)
		}
		waitRDPClientCount(t, display, 0)
		snapshot := collector.Snapshot()
		if snapshot.Connections != 0 || snapshot.FramesSent != 0 || snapshot.PayloadBytes != 0 {
			t.Fatalf("incomplete handshake registered an activated session: %+v", snapshot)
		}
		if snapshot.Network.SentBytes <= previous.SentBytes+19 ||
			snapshot.Network.ReceivedBytes <= previous.ReceivedBytes+19 {
			t.Fatalf("TLS/negotiation traffic not counted across reconnect: old=%+v new=%+v", previous, snapshot.Network)
		}
		previous = *snapshot.Network
	}
}

// The extra fast paths would bypass Read/Write if the wrapper exposed them.
type bandwidthTestConn struct {
	reader                                func([]byte) (int, error)
	writer                                func([]byte) (int, error)
	deadline, readDeadline, writeDeadline time.Time
	closed                                bool
}

func (c *bandwidthTestConn) Read(data []byte) (int, error) { return c.reader(data) }

func (c *bandwidthTestConn) Write(data []byte) (int, error) { return c.writer(data) }

func (c *bandwidthTestConn) Close() error {
	c.closed = true

	return io.ErrClosedPipe
}

func (*bandwidthTestConn) LocalAddr() net.Addr { return &net.UnixAddr{Name: "local", Net: "unix"} }

func (*bandwidthTestConn) RemoteAddr() net.Addr { return &net.UnixAddr{Name: "remote", Net: "unix"} }

func (c *bandwidthTestConn) SetDeadline(value time.Time) error {
	c.deadline = value

	return nil
}

func (c *bandwidthTestConn) SetReadDeadline(value time.Time) error {
	c.readDeadline = value

	return nil
}

func (c *bandwidthTestConn) SetWriteDeadline(value time.Time) error {
	c.writeDeadline = value

	return nil
}

func (*bandwidthTestConn) ReadFrom(io.Reader) (int64, error) {
	return 0, errors.ErrUnsupported
}

func (*bandwidthTestConn) WriteTo(io.Writer) (int64, error) {
	return 0, errors.ErrUnsupported
}
