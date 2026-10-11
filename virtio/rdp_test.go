package virtio

import (
	"bytes"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"syscall"
	"testing"
	"time"

	"github.com/define42/gokvm/internal/rdp/avc"
)

var _ ConsoleDisplay = (*RDPDisplay)(nil)

func TestRDPDisplayH264Configuration(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"", "software"} {
		name := backend
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d, err := NewRDPDisplayWithConfig("127.0.0.1:0", RDPConfig{H264: true, H264Encoder: backend})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = d.Close() })
			if !d.h264 || d.h264Backend != avc.BackendSoftware {
				t.Fatalf("software H.264 was not enabled: enabled=%v backend=%q", d.h264, d.h264Backend)
			}
			if d.linearInterval != time.Second/avc.FrameRate {
				t.Fatalf("H.264 capture interval %v does not match encoder frame rate", d.linearInterval)
			}
		})
	}
}

func TestRDPEncoderConfigValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		options RDPConfig
	}{
		{name: "unknown", options: RDPConfig{H264: true, H264Encoder: "invalid"}},
		{name: "software-without-h264", options: RDPConfig{H264Encoder: "software"}},
		{name: "nvenc-without-h264", options: RDPConfig{H264Encoder: "nvenc"}},
		{name: "auto-without-h264", options: RDPConfig{H264Encoder: "auto"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := NewRDPDisplayWithConfig("invalid-address", tc.options); !errors.Is(err, errRDPEncoder) {
				t.Fatalf("invalid encoder options reached listener creation: %v", err)
			}
		})
	}
}

func TestRDPDisplayRequiresTLSSecurity(t *testing.T) {
	t.Parallel()

	d := newRDPTestDisplay(t)
	for _, requested := range []uint32{0, 2} {
		conn := dialRDPTestDisplay(t, d)
		response := negotiateRDPTestDisplay(t, conn, requested)
		if response[11] != 3 || binary.LittleEndian.Uint32(response[15:]) != 1 {
			t.Fatalf("protocols %#x: got negotiation response %x, want SSL_REQUIRED_BY_SERVER", requested, response)
		}
		assertRDPConnectionClosed(t, conn)
		waitRDPClientCount(t, d, 0)
	}
}

func TestRDPDisplayTLSMinimumVersionAndCertificate(t *testing.T) {
	t.Parallel()

	d := newRDPTestDisplay(t)
	conn := dialRDPTestDisplay(t, d)
	response := negotiateRDPTestDisplay(t, conn, 1)
	if response[11] != 2 || binary.LittleEndian.Uint32(response[15:]) != 1 {
		t.Fatalf("TLS negotiation response: got %x", response)
	}

	secured := tls.Client(conn, rdpTestClientTLS(t, d))
	if err := secured.Handshake(); err != nil {
		t.Fatalf("TLS handshake with pinned console certificate: %v", err)
	}
	state := secured.ConnectionState()
	if state.Version < tls.VersionTLS12 || len(state.VerifiedChains) == 0 {
		t.Fatalf("unverified or obsolete TLS connection: version %#x, chains %d", state.Version, len(state.VerifiedChains))
	}
	if err := secured.Close(); err != nil {
		t.Fatal(err)
	}
	waitRDPClientCount(t, d, 0)

	oldConn := dialRDPTestDisplay(t, d)
	negotiateRDPTestDisplay(t, oldConn, 1)
	oldTLS := rdpTestClientTLS(t, d)
	oldTLS.MinVersion = tls.VersionTLS10
	oldTLS.MaxVersion = tls.VersionTLS11
	if err := tls.Client(oldConn, oldTLS).Handshake(); err == nil {
		t.Fatal("server accepted obsolete TLS")
	}
}

func TestRDPDisplayCloseInterruptsStalledHandshakes(t *testing.T) {
	t.Parallel()

	// Cover a client that sends nothing, one stalled after RDP security
	// negotiation, and one that completes TLS but never sends MCS Connect Initial.
	for _, stage := range []string{"negotiation", "TLS", "MCS"} {
		t.Run(stage, func(t *testing.T) {
			t.Parallel()
			d := newRDPTestDisplay(t)
			conn := dialRDPTestDisplay(t, d)
			if stage != "negotiation" {
				negotiateRDPTestDisplay(t, conn, 1)
			}
			if stage == "MCS" {
				secured := tls.Client(conn, rdpTestClientTLS(t, d))
				if err := secured.Handshake(); err != nil {
					t.Fatal(err)
				}
				conn = secured
			}
			waitRDPClientCount(t, d, 1)

			closed := make(chan error, 1)
			go func() { closed <- d.Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatalf("Close: %v", err)
				}
			case <-time.After(2 * time.Second):
				t.Fatal("Close waited for stalled handshake")
			}
			assertRDPConnectionClosed(t, conn)
			waitRDPClientCount(t, d, 0)
		})
	}
}

func TestRDPDisplayMalformedClientsDoNotStopListener(t *testing.T) {
	t.Parallel()

	d := newRDPTestDisplay(t)
	for _, packet := range [][]byte{
		{3, 0, 0, 6},                          // TPKT shorter than its header and X.224 minimum.
		{3, 1, 0, 11},                         // Nonzero reserved TPKT byte.
		{3, 0, 0, 11, 6, 0xd0, 0, 0, 0, 0, 0}, // Confirmation instead of connection request.
		{4, 3, 0},                             // Fast-path input before negotiation.
	} {
		conn := dialRDPTestDisplay(t, d)
		writeRDPTest(t, conn, packet)
		assertRDPConnectionClosed(t, conn)
		waitRDPClientCount(t, d, 0)
	}

	// A client that sends malformed MCS over valid TLS also must not affect
	// subsequent clients or the shared console state.
	conn := dialRDPTestDisplay(t, d)
	negotiateRDPTestDisplay(t, conn, 1)
	secured := tls.Client(conn, rdpTestClientTLS(t, d))
	if err := secured.Handshake(); err != nil {
		t.Fatal(err)
	}
	writeRDPTest(t, secured, []byte{3, 0, 0, 7, 2, 0xf0, 0x80})
	assertRDPConnectionClosed(t, secured)
	waitRDPClientCount(t, d, 0)

	healthy := dialRDPTestDisplay(t, d)
	response := negotiateRDPTestDisplay(t, healthy, 1)
	if response[11] != 2 {
		t.Fatalf("listener did not accept a valid client after malformed clients: %x", response)
	}
	if err := tls.Client(healthy, rdpTestClientTLS(t, d)).Handshake(); err != nil {
		t.Fatalf("listener failed TLS after malformed clients: %v", err)
	}
}

func TestRDPDisplayRejectsSecondClientAndReusesSlot(t *testing.T) {
	t.Parallel()

	d := newRDPTestDisplay(t)
	first := dialRDPTestDisplay(t, d)
	negotiateRDPTestDisplay(t, first, 1)
	waitRDPClientCount(t, d, 1)

	excess := dialRDPTestDisplay(t, d)
	assertRDPConnectionClosed(t, excess)
	waitRDPClientCount(t, d, 1)
	// Rejecting another viewer must leave the admitted connection usable.
	secured := tls.Client(first, rdpTestClientTLS(t, d))
	if err := secured.Handshake(); err != nil {
		t.Fatalf("existing client disrupted by rejected connection: %v", err)
	}
	if err := secured.Close(); err != nil {
		t.Fatal(err)
	}
	waitRDPClientCount(t, d, 0)

	replacement := dialRDPTestDisplay(t, d)
	response := negotiateRDPTestDisplay(t, replacement, 1)
	if response[11] != 2 {
		t.Fatalf("client slot was not reusable: %x", response)
	}
	if err := tls.Client(replacement, rdpTestClientTLS(t, d)).Handshake(); err != nil {
		t.Fatalf("replacement client could not complete TLS: %v", err)
	}
}

func newRDPTestDisplay(t *testing.T) *RDPDisplay {
	t.Helper()
	d, err := NewRDPDisplay("127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	return d
}

func dialRDPTestDisplay(t *testing.T, d *RDPDisplay) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", d.Addr(), time.Second)
	if err != nil {
		if errors.Is(err, syscall.ENETUNREACH) {
			t.Skipf("loopback is unreachable in this network namespace: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}

	return conn
}

func negotiateRDPTestDisplay(t *testing.T, conn net.Conn, protocols uint32) []byte {
	t.Helper()
	request := []byte{3, 0, 0, 19, 14, 0xe0, 0, 0, 0, 0, 0, 1, 0, 8, 0, 0, 0, 0, 0}
	binary.LittleEndian.PutUint32(request[15:], protocols)
	writeRDPTest(t, conn, request)
	response := make([]byte, 19)
	if _, err := io.ReadFull(conn, response); err != nil {
		t.Fatal(err)
	}
	if response[0] != 3 || response[1] != 0 ||
		binary.BigEndian.Uint16(response[2:]) != 19 || response[5] != 0xd0 {
		t.Fatalf("invalid RDP negotiation response: %x", response)
	}

	return response
}

func rdpTestClientTLS(t *testing.T, d *RDPDisplay) *tls.Config {
	t.Helper()
	cert, err := x509.ParseCertificate(d.tls.Certificates[0].Certificate[0])
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	roots.AddCert(cert)

	return &tls.Config{RootCAs: roots, ServerName: "localhost", MinVersion: tls.VersionTLS12}
}

func assertRDPConnectionClosed(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	var b [1]byte
	if _, err := io.ReadFull(conn, b[:]); err == nil {
		t.Fatalf("connection remained open, received %x", b)
	} else {
		var netErr net.Error
		if errors.As(err, &netErr) && netErr.Timeout() {
			t.Fatalf("connection did not close: %v", err)
		}
	}
}

func waitRDPClientCount(t *testing.T, d *RDPDisplay, want int) {
	t.Helper()
	deadline := time.Now().Add(time.Second)
	for {
		d.connMu.Lock()
		got := 0
		if d.conn != nil {
			got = 1
		}
		d.connMu.Unlock()
		if got == want {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("connected clients: got %d, want %d", got, want)
		}
		time.Sleep(time.Millisecond)
	}
}

func writeRDPTest(t *testing.T, w io.Writer, data []byte) {
	t.Helper()
	if _, err := io.Copy(w, bytes.NewReader(data)); err != nil {
		t.Fatal(err)
	}
}
