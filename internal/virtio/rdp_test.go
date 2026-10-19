package virtio

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"os/exec"
	"sync/atomic"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
)

var _ ConsoleDisplay = (*RDPDisplay)(nil)

func TestRDPDisplayUsesSuppliedAbstractUnixListener(t *testing.T) {
	t.Parallel()
	listener := newRDPTestListener(t)
	t.Cleanup(func() { _ = listener.Close() })
	addr := listener.Addr().String()

	// The address remains bound throughout construction. Rebinding it instead
	// of using the supplied listener must fail.
	options := authenticatedRDPTestConfig(listener)
	d, err := NewRDPDisplayWithConfig(options)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	if got := d.Addr(); got != addr {
		t.Fatalf("display address = %q, want supplied listener address %q", got, addr)
	}

	conn := dialRDPTestDisplay(t, d)
	response := negotiateRDPTestDisplay(t, conn, 1)
	if response[11] != 2 || binary.LittleEndian.Uint32(response[15:]) != 1 {
		t.Fatalf("TLS negotiation response: got %x", response)
	}
	secured := tls.Client(conn, rdpTestClientTLS(t, d))
	if err := secured.Handshake(); err != nil {
		t.Fatalf("TLS handshake on supplied listener: %v", err)
	}
	if err := d.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	assertRDPConnectionClosed(t, secured)
	if conn, err := listener.Accept(); !errors.Is(err, net.ErrClosed) {
		if conn != nil {
			_ = conn.Close()
		}
		t.Fatalf("listener after display shutdown: %v, want net.ErrClosed", err)
	}
}

func TestRDPDisplayInitializationFailurePreservesSuppliedListener(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		options RDPConfig
		wantErr error
	}{
		{name: "encoder", options: RDPConfig{H264: true, H264Encoder: "invalid"}, wantErr: errRDPEncoder},
		{name: "threads", options: RDPConfig{H264Threads: -1}, wantErr: errRDPThreads},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			listener := newRDPTestListener(t)
			t.Cleanup(func() { _ = listener.Close() })
			tc.options = mergeAuthenticatedRDPTestConfig(listener, tc.options)
			d, err := NewRDPDisplayWithConfig(tc.options)
			if d != nil {
				t.Cleanup(func() { _ = d.Close() })
			}
			if !errors.Is(err, tc.wantErr) {
				t.Fatalf("initialization error = %v, want %v", err, tc.wantErr)
			}
			if err := listener.Close(); err != nil {
				t.Fatalf("caller no longer owns listener after initialization failure: %v", err)
			}
		})
	}
}

func TestRDPDisplayH264Configuration(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"", "software"} {
		name := backend
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			d := newRDPTestDisplayWithConfig(t, RDPConfig{H264: true, H264Encoder: backend})
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
			if _, err := NewRDPDisplayWithConfig(tc.options); !errors.Is(err, errRDPEncoder) {
				t.Fatalf("invalid encoder options reached listener creation: %v", err)
			}
		})
	}
}

func TestRDPDisplayRequiresGatewayTransport(t *testing.T) {
	t.Parallel()

	tcpListener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = tcpListener.Close() })
	pathnameListener, err := net.ListenUnix("unix", &net.UnixAddr{Name: t.TempDir() + "/rdp.sock", Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = pathnameListener.Close() })

	for _, test := range []struct {
		name     string
		listener net.Listener
	}{
		{name: "missing"},
		{name: "typed nil Unix", listener: (*net.UnixListener)(nil)},
		{name: "TCP", listener: tcpListener},
		{name: "pathname Unix", listener: pathnameListener},
	} {
		options := authenticatedRDPTestConfig(test.listener)
		if _, err := NewRDPDisplayWithConfig(options); !errors.Is(err, errRDPListener) {
			t.Errorf("%s listener error = %v, want %v", test.name, err, errRDPListener)
		}
	}

	listener := newRDPTestListener(t)
	t.Cleanup(func() { _ = listener.Close() })
	missingIdentity := authenticatedRDPTestConfig(listener)
	missingIdentity.GatewayPID = 0
	if _, err := NewRDPDisplayWithConfig(missingIdentity); !errors.Is(err, errRDPGatewayIdentity) {
		t.Fatalf("missing identity error = %v, want %v", err, errRDPGatewayIdentity)
	}
	changedParent := authenticatedRDPTestConfig(listener)
	changedParent.getParentPID = func() int { return changedParent.GatewayPID + 1 }
	if _, err := NewRDPDisplayWithConfig(changedParent); !errors.Is(err, errRDPGatewayParent) {
		t.Fatalf("changed parent error = %v, want %v", err, errRDPGatewayParent)
	}
}

func TestRDPDisplayRetiresListenerWhenGatewayParentChanges(t *testing.T) {
	t.Parallel()
	listener := newRDPTestListener(t)
	options := authenticatedRDPTestConfig(listener)
	var parentPID atomic.Int64
	parentPID.Store(int64(options.GatewayPID))
	options.getParentPID = func() int { return int(parentPID.Load()) }
	display, err := NewRDPDisplayWithConfig(options)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = display.Close() })

	parentPID.Store(int64(options.GatewayPID + 1))
	conn, err := net.DialTimeout("unix", display.Addr(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	assertRDPConnectionClosed(t, conn)
	select {
	case <-display.done:
	case <-time.After(time.Second):
		t.Fatal("display remained active after its gateway parent changed")
	}
	if conn, err := net.DialTimeout("unix", display.Addr(), 50*time.Millisecond); err == nil {
		_ = conn.Close()
		t.Fatal("listener still accepted connections after its gateway parent changed")
	}
	if err := display.Close(); err != nil {
		t.Fatalf("closing retired display: %v", err)
	}
}

func TestRDPDisplayAuthorizesExpectedPeerIdentity(t *testing.T) {
	t.Parallel()
	server, client := newRDPTestUnixPair(t)
	defer server.Close()
	defer client.Close()

	pid := os.Getpid()
	uid := uint32(os.Geteuid())
	for _, test := range []struct {
		name      string
		pid       int32
		uid       uint32
		parentPID int
		wantErr   error
	}{
		{name: "expected", pid: int32(pid), uid: uid, parentPID: pid},
		{name: "wrong PID", pid: int32(pid + 1), uid: uid, parentPID: pid + 1, wantErr: errRDPPeerIdentity},
		{name: "wrong effective UID", pid: int32(pid), uid: uid + 1, parentPID: pid, wantErr: errRDPPeerIdentity},
		{name: "changed live parent", pid: int32(pid), uid: uid, parentPID: pid + 1, wantErr: errRDPGatewayParent},
	} {
		display := &RDPDisplay{
			gatewayPID: test.pid,
			gatewayUID: test.uid,
			getParentPID: func() int {
				return test.parentPID
			},
		}
		err := display.authorizeGateway(server)
		if !errors.Is(err, test.wantErr) {
			t.Errorf("%s authorization error = %v, want %v", test.name, err, test.wantErr)
		}
	}
}

func TestRDPDisplayAuthenticatesRealParentProcess(t *testing.T) {
	t.Parallel()

	listener := newRDPTestListener(t)
	address := listener.Addr().String()
	file, err := listener.File()
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	//nolint:gosec // Re-executes this test binary with a fixed helper-test selector.
	server := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRDPDisplayGatewayServerHelper$")
	server.Env = append(os.Environ(), "GOKVM_RDP_GATEWAY_SERVER_HELPER=1")
	server.ExtraFiles = []*os.File{file}
	stdin, err := server.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	stdout, err := server.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	var stderr bytes.Buffer
	server.Stderr = &stderr
	if err := server.Start(); err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	_ = listener.Close()
	waited := false
	t.Cleanup(func() {
		_ = stdin.Close()
		if !waited {
			_ = server.Wait()
		}
	})

	ready := make(chan string, 1)
	go func() {
		line, _ := bufio.NewReader(stdout).ReadString('\n')
		ready <- line
	}()
	select {
	case line := <-ready:
		if line != "ready\n" {
			t.Fatalf("gateway server helper did not become ready: %q (%s)", line, stderr.String())
		}
	case <-ctx.Done():
		t.Fatalf("gateway server helper readiness: %v (%s)", ctx.Err(), stderr.String())
	}

	//nolint:gosec // Re-executes this test binary with a fixed helper-test selector.
	rogue := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestRDPDisplayUnauthorizedPeerHelper$")
	rogue.Env = append(os.Environ(), "GOKVM_RDP_UNAUTHORIZED_PEER_HELPER="+address)
	if output, err := rogue.CombinedOutput(); err != nil {
		t.Fatalf("different-PID peer helper: %v\n%s", err, output)
	}

	conn, err := net.DialTimeout("unix", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	response := negotiateRDPTestDisplay(t, conn, 1)
	if response[11] != 2 {
		t.Fatalf("parent gateway was not admitted after rejecting a different PID: %x", response)
	}
	_ = conn.Close()
	if err := stdin.Close(); err != nil {
		t.Fatal(err)
	}
	if err := server.Wait(); err != nil {
		t.Fatalf("gateway server helper: %v (%s)", err, stderr.String())
	}
	waited = true
}

func TestRDPDisplayGatewayServerHelper(t *testing.T) {
	if os.Getenv("GOKVM_RDP_GATEWAY_SERVER_HELPER") != "1" {
		return
	}
	t.Parallel()

	file := os.NewFile(3, "inherited-rdp-listener")
	if file == nil {
		t.Fatal("missing inherited listener")
	}
	listener, err := net.FileListener(file)
	_ = file.Close()
	if err != nil {
		t.Fatal(err)
	}
	display, err := NewRDPDisplayWithConfig(RDPConfig{
		Listener: listener, GatewayPID: os.Getppid(), GatewayUID: uint32(os.Geteuid()),
	})
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	defer func() { _ = display.Close() }()
	if _, err := os.Stdout.WriteString("ready\n"); err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}
}

func TestRDPDisplayUnauthorizedPeerHelper(t *testing.T) {
	address := os.Getenv("GOKVM_RDP_UNAUTHORIZED_PEER_HELPER")
	if address == "" {
		return
	}
	t.Parallel()

	//nolint:gosec // The parent test supplies its freshly generated abstract socket address.
	conn, err := net.DialTimeout("unix", address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	assertRDPConnectionClosed(t, conn)
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

	return newRDPTestDisplayWithConfig(t, RDPConfig{})
}

func newRDPTestDisplayWithConfig(t *testing.T, options RDPConfig) *RDPDisplay {
	t.Helper()
	listener := newRDPTestListener(t)
	options = mergeAuthenticatedRDPTestConfig(listener, options)
	d, err := NewRDPDisplayWithConfig(options)
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })

	return d
}

func newRDPTestListener(t *testing.T) *net.UnixListener {
	t.Helper()
	listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: "@gokvm-test-" + rand.Text(), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}

	return listener
}

func authenticatedRDPTestConfig(listener net.Listener) RDPConfig {
	return mergeAuthenticatedRDPTestConfig(listener, RDPConfig{})
}

func mergeAuthenticatedRDPTestConfig(listener net.Listener, options RDPConfig) RDPConfig {
	options.Listener = listener
	options.GatewayPID = os.Getpid()
	options.GatewayUID = uint32(os.Geteuid())
	options.getParentPID = os.Getpid

	return options
}

func newRDPTestUnixPair(t *testing.T) (*net.UnixConn, *net.UnixConn) {
	t.Helper()
	listener := newRDPTestListener(t)
	defer listener.Close()
	accepted := make(chan *net.UnixConn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := listener.AcceptUnix()
		if err != nil {
			acceptErr <- err

			return
		}
		accepted <- conn
	}()
	client, err := net.DialUnix("unix", nil, listener.Addr().(*net.UnixAddr))
	if err != nil {
		t.Fatal(err)
	}
	select {
	case server := <-accepted:
		return server, client
	case err := <-acceptErr:
		_ = client.Close()
		t.Fatal(err)

		return nil, nil
	case <-time.After(time.Second):
		_ = client.Close()
		t.Fatal("accepting Unix test connection timed out")

		return nil, nil
	}
}

func dialRDPTestDisplay(t *testing.T, d *RDPDisplay) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("unix", d.Addr(), time.Second)
	if err != nil {
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
