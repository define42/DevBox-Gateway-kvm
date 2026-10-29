package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"net"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"
	"unicode/utf16"
)

func mcsPacket(body []byte) []byte {
	packet := append([]byte{3, 0, 0, 0, 2, 0xf0, 0x80}, body...)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))

	return packet
}

func clientInfoPacket(username, password string, unicode bool) []byte {
	return clientInfoFieldsPacket([5]string{"", username, password, "", ""}, unicode, nil)
}

func clientInfoFieldsPacket(fields [5]string, unicode bool, extra []byte) []byte {
	width := 1
	if unicode {
		width = 2
	}
	var header [22]byte
	info := header[:]
	info[0] = 0x40
	flags := uint32(0x8 | 0x00100000)
	if unicode {
		flags |= 0x10
	}
	binary.LittleEndian.PutUint32(info[8:12], flags)
	for i, field := range fields {
		var encoded []byte
		if unicode {
			for _, unit := range utf16.Encode([]rune(field)) {
				encoded = binary.LittleEndian.AppendUint16(encoded, unit)
			}
		} else {
			encoded = []byte(field)
		}
		binary.LittleEndian.PutUint16(info[12+2*i:14+2*i], uint16(len(encoded)))
		info = append(info, encoded...)
		info = append(info, make([]byte, width)...)
	}
	info = append(info, extra...)
	mcs := []byte{0x64, 0, 0, 3, 0xeb, 0x70}
	if len(info) >= 128 {
		mcs = append(mcs, byte(len(info)>>8)|0x80, byte(len(info)))
	} else {
		mcs = append(mcs, byte(len(info)))
	}

	return mcsPacket(append(mcs, info...))
}

func passwordClientHandshake(conn net.Conn, username, password string) error {
	return passwordClientHandshakePacket(conn, clientInfoPacket(username, password, true))
}

func passwordClientHandshakePacket(conn net.Conn, clientInfo []byte, channels ...uint16) error {
	g := rdpHandshake{remaining: 256 << 10}
	if err := writePacket(conn, mcsPacket([]byte{0x7f, 0x65, 0})); err != nil {
		return err
	}
	if _, _, err := g.read(conn); err != nil {
		return err
	}
	for _, body := range [][]byte{{4, 1, 0, 1, 0}, {0x28}} {
		if err := writePacket(conn, mcsPacket(body)); err != nil {
			return err
		}
	}
	if _, _, err := g.read(conn); err != nil {
		return err
	}
	if channels == nil {
		channels = []uint16{1001, 1003}
	}
	for _, channel := range channels {
		body := []byte{0x38, 0, 0, byte(channel >> 8), byte(channel)}
		if err := writePacket(conn, mcsPacket(body)); err != nil {
			return err
		}
		if _, _, err := g.read(conn); err != nil {
			return err
		}
	}

	return writePacket(conn, clientInfo)
}

func passwordBackendHandshake(conn net.Conn, received *atomic.Bool) bool {
	g := rdpHandshake{remaining: 256 << 10}
	if _, _, err := g.read(conn); err != nil {
		return false
	}
	if err := writePacket(conn, mcsPacket([]byte{0x7f, 0x66, 0})); err != nil {
		return false
	}
	for range 2 {
		if _, _, err := g.read(conn); err != nil {
			return false
		}
	}
	if err := writePacket(conn, mcsPacket([]byte{0x2e, 0, 0, 0})); err != nil {
		return false
	}
	for range 2 {
		_, body, err := g.read(conn)
		if err != nil || len(body) != 5 {
			return false
		}
		response := append([]byte{0x3e, 0}, body[1:]...)
		response = append(response, body[3:5]...)
		if err := writePacket(conn, mcsPacket(response)); err != nil {
			return false
		}
	}
	_, body, err := g.read(conn)
	if err != nil || len(body) < 7 || body[0] != 0x64 {
		return false
	}
	info, err := clientInfoData(body)
	if err != nil || binary.LittleEndian.Uint32(info[:4]) != 0x40 ||
		binary.LittleEndian.Uint16(info[16:18]) != 0 {
		return false
	}
	if received != nil {
		received.Store(true)
	}

	return true
}

func rdpCredentialTLSPair(t *testing.T) (*tls.Conn, *tls.Conn) {
	t.Helper()
	certificate, certificatePEM, _, _ := testTLSIdentity(t, "trace.test")
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certificatePEM) {
		t.Fatal("could not trust trace test certificate")
	}
	serverRaw, clientRaw := net.Pipe()
	server := tls.Server(serverRaw, &tls.Config{
		Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12,
	})
	client := tls.Client(clientRaw, &tls.Config{
		RootCAs: roots, ServerName: "trace.test", MinVersion: tls.VersionTLS12,
	})
	t.Cleanup(func() {
		_ = server.NetConn().Close()
		_ = client.NetConn().Close()
	})
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	serverDone := make(chan error, 1)
	go func() { serverDone <- server.HandshakeContext(ctx) }()
	clientErr := client.HandshakeContext(ctx)
	serverErr := <-serverDone
	if clientErr != nil || serverErr != nil {
		t.Fatalf("trace test TLS handshake: client=%v server=%v", clientErr, serverErr)
	}

	return server, client
}

func TestVerifyRDPClientTraceStages(t *testing.T) {
	t.Parallel()
	front, client := rdpCredentialTLSPair(t)
	backendServer, backend := rdpCredentialTLSPair(t)
	var stages []string
	verified := make(chan error, 1)
	go func() {
		verified <- verifyRDPClientWithTrace(t.Context(), front, backend,
			func(_ context.Context, credentials rdpCredentials) error {
				if credentials != (rdpCredentials{Username: "alice", Password: "rdp-password"}) {
					return ErrAuthentication
				}

				return nil
			}, func(stage string) { stages = append(stages, stage) })
	}()
	backendDone := make(chan bool, 1)
	go func() { backendDone <- passwordBackendHandshake(backendServer, nil) }()
	clientDone := make(chan error, 1)
	go func() { clientDone <- passwordClientHandshake(client, "alice", "rdp-password") }()
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-verified; err != nil {
		t.Fatal(err)
	}
	if !<-backendDone {
		t.Fatal("backend did not complete traced handshake")
	}
	want := []string{
		rdpStageClientMCSConnect,
		rdpStageBackendMCSConnect,
		rdpStageClientMCSResponse,
		rdpStageClientErectDomain,
		rdpStageClientAttachUser,
		rdpStageBackendAttachUser,
		rdpStageClientAttachConfirm,
		rdpStageClientChannelJoinOrInfo,
		rdpStageBackendChannelJoin,
		rdpStageClientChannelConfirm,
		rdpStageClientChannelJoinOrInfo,
		rdpStageBackendChannelJoin,
		rdpStageClientChannelConfirm,
		rdpStageClientChannelJoinOrInfo,
		rdpStageClientInfoValidation,
		rdpStageCredentialAuthentication,
		rdpStageBackendClientInfo,
	}
	if !slices.Equal(stages, want) {
		t.Fatalf("handshake stages = %q, want %q", stages, want)
	}
}

func TestVerifyRDPClientTraceStopsAtFailure(t *testing.T) {
	t.Parallel()
	front, client := rdpCredentialTLSPair(t)
	backendServer, backend := rdpCredentialTLSPair(t)
	var stages []string
	verified := make(chan error, 1)
	go func() {
		verified <- verifyRDPClientWithTrace(t.Context(), front, backend,
			func(context.Context, rdpCredentials) error { return nil },
			func(stage string) { stages = append(stages, stage) })
	}()
	clientDone := make(chan error, 1)
	go func() { clientDone <- writePacket(client, mcsPacket([]byte{0x7f, 0x65, 0})) }()
	backendDone := make(chan error, 1)
	go func() {
		gate := rdpHandshake{remaining: 256 << 10}
		if _, _, err := gate.read(backendServer); err != nil {
			backendDone <- err

			return
		}
		backendDone <- writePacket(backendServer, mcsPacket([]byte{0x7f, 0x65, 0}))
	}()
	if err := <-clientDone; err != nil {
		t.Fatal(err)
	}
	if err := <-backendDone; err != nil {
		t.Fatal(err)
	}
	if err := <-verified; !errors.Is(err, ErrAuthentication) {
		t.Fatalf("invalid backend response error = %v, want authentication failure", err)
	}
	want := []string{rdpStageClientMCSConnect, rdpStageBackendMCSConnect}
	if !slices.Equal(stages, want) {
		t.Fatalf("failed handshake stages = %q, want %q", stages, want)
	}
}

func TestAuthenticatedClientInfoRejectsFailedAuthentication(t *testing.T) {
	t.Parallel()
	var called bool
	packet, err := authenticatedClientInfo(t.Context(), clientInfoPacket("alice", "rdp-password", true)[7:],
		func(_ context.Context, credentials rdpCredentials) error {
			called = true
			if credentials != (rdpCredentials{Username: "alice", Password: "rdp-password"}) {
				t.Fatal("authenticator did not receive original credentials")
			}

			return ErrAuthentication
		})
	if !called || !errors.Is(err, ErrAuthentication) || len(packet) != 0 {
		t.Fatal("failed authentication produced a backend packet")
	}
}

func TestAuthenticatedClientInfoRejectsCancellation(t *testing.T) {
	t.Parallel()
	for _, duringAuthentication := range []bool{false, true} {
		t.Run(map[bool]string{false: "before", true: "during"}[duringAuthentication], func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			if !duringAuthentication {
				cancel()
			}
			var called bool
			packet, err := authenticatedClientInfo(ctx, clientInfoPacket("alice", "rdp-password", true)[7:],
				func(context.Context, rdpCredentials) error {
					called = true
					cancel()

					return nil
				})
			if called != duringAuthentication || !errors.Is(err, context.Canceled) || len(packet) != 0 {
				t.Fatal("canceled authentication produced a backend packet")
			}
		})
	}
}

func TestClientInfoCredentialsBoundsAndEncoding(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		password string
		unicode  bool
	}{
		{name: "ANSI", password: "rdp-password"},
		//nolint:gosec // Synthetic Unicode credential tests UTF-16 decoding; not a real password.
		{name: "Unicode", password: "p\u00e4ss\U0001f680", unicode: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mcs := clientInfoPacket("alice", tc.password, tc.unicode)[7:]
			credentials, _, err := parseClientInfo(bytes.Clone(mcs))
			if err != nil {
				t.Fatal(err)
			}
			if credentials != (rdpCredentials{Username: "alice", Password: tc.password}) {
				t.Fatal("credentials were not decoded exactly")
			}
			for i := range len(mcs) {
				if _, _, err := parseClientInfo(bytes.Clone(mcs[:i])); err == nil {
					t.Fatalf("accepted truncation at %d", i)
				}
			}
			for _, offset := range []int{0, 1, 3, 5, 6} {
				altered := bytes.Clone(mcs)
				altered[offset] ^= 1
				if _, _, err := parseClientInfo(altered); err == nil {
					t.Fatalf("accepted bad MCS byte %d", offset)
				}
			}
		})
	}
}

func TestClientInfoRejectsInvalidCredentials(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		username string
		password string
		unicode  bool
	}{
		{name: "empty username", password: "rdp-password", unicode: true},
		{name: "empty password", username: "alice", unicode: true},
		{name: "long username", username: strings.Repeat("u", 129), password: "p", unicode: true},
		{name: "long password", username: "alice", password: strings.Repeat("p", 1025), unicode: true},
		{name: "long UTF8 password", username: "alice", password: strings.Repeat("\u5bc6", 342), unicode: true},
		{name: "username NUL", username: "ali\x00ce", password: "p", unicode: true},
		{name: "password NUL", username: "alice", password: "p\x00ssword", unicode: true},
		{name: "ambiguous ANSI", username: "alice", password: "p\xe4ssword"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := parseClientInfo(clientInfoPacket(tc.username, tc.password, tc.unicode)[7:]); err == nil {
				t.Fatal("invalid credentials accepted")
			}
		})
	}
}

func TestClientInfoRejectsMalformedFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func([]byte)
	}{
		{name: "contradictory security PDU type", mutate: func(info []byte) { info[0] |= 0x80 }},
		{name: "smart card PIN", mutate: func(info []byte) { info[10] |= 0x4 }},
		{name: "odd UTF16 length", mutate: func(info []byte) { info[14]++ }},
		{name: "oversized field", mutate: func(info []byte) { info[15] = 0xff }},
		{name: "missing terminator", mutate: func(info []byte) { info[34] = 1 }},
		{name: "unpaired high surrogate", mutate: func(info []byte) { info[25] = 0xd8 }},
		{name: "unpaired low surrogate", mutate: func(info []byte) { info[25] = 0xdc }},
		{name: "trailing high surrogate", mutate: func(info []byte) { info[33] = 0xd8 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mcs := clientInfoPacket("alice", "rdp-password", true)[7:]
			info, err := clientInfoData(mcs)
			if err != nil {
				t.Fatal(err)
			}
			tc.mutate(info)
			if _, _, err := parseClientInfo(mcs); err == nil {
				t.Fatal("malformed Client Info accepted")
			}
		})
	}
}

func TestClientInfoStripsPasswordAndPreservesOtherFields(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		unicode  bool
		password string
		extra    []byte
	}{
		{name: "ANSI short", password: "rdp-password"},
		{name: "Unicode long", unicode: true, password: strings.Repeat("\u5bc6\U0001f680", 40)},
		{name: "extended info", unicode: true, password: "rdp-password", extra: bytes.Repeat([]byte{0x23}, 180)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fields := [5]string{"EXAMPLE", "alice", tc.password, "shell", "directory"}
			packet := clientInfoFieldsPacket(fields, tc.unicode, tc.extra)
			credentials, sanitized, err := parseClientInfo(packet[7:])
			if err != nil {
				t.Fatal(err)
			}
			if credentials != (rdpCredentials{Domain: "EXAMPLE", Username: "alice", Password: tc.password}) {
				t.Fatal("credentials changed")
			}
			fields[2] = ""
			want := clientInfoFieldsPacket(fields, tc.unicode, tc.extra)
			info, err := clientInfoData(want[7:])
			if err != nil {
				t.Fatal(err)
			}
			flags := binary.LittleEndian.Uint32(info[8:12]) &^ (0x8 | 0x00100000)
			binary.LittleEndian.PutUint32(info[8:12], flags)
			if !bytes.Equal(sanitized, want) {
				t.Fatal("password removal damaged packet or retained credential flags")
			}
			if _, _, err := parseClientInfo(sanitized[7:]); err == nil {
				t.Fatal("sanitized Client Info still contains usable credentials")
			}
		})
	}
}

func FuzzClientInfoCredentials(f *testing.F) {
	f.Add(clientInfoPacket("alice", "rdp-password", false)[7:])
	f.Add(clientInfoPacket("alice", "\u5bc6\u78bc\U0001f680", true)[7:])
	f.Add([]byte{0x64})
	f.Fuzz(func(t *testing.T, input []byte) {
		credentials, sanitized, err := parseClientInfo(bytes.Clone(input))
		if err != nil {
			return
		}
		if credentials.Username == "" || credentials.Password == "" {
			t.Fatal("accepted empty credentials")
		}
		if len(sanitized) < 7 || int(binary.BigEndian.Uint16(sanitized[2:4])) != len(sanitized) {
			t.Fatal("invalid sanitized TPKT length")
		}
		info, err := clientInfoData(sanitized[7:])
		if err != nil || binary.LittleEndian.Uint16(info[16:18]) != 0 {
			t.Fatal("invalid sanitized Client Info")
		}
	})
}

func TestRoutingTokenWithoutValidPasswordNeverReachesDesktop(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing", "wrong"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var received atomic.Bool
			endpoint := startTestBackend(t, func(conn net.Conn) bool { return passwordBackendHandshake(conn, &received) })
			s, _, cert, raw, _ := startGatewayForTest(t, endpoint)
			key, _, _ := s.store.session(raw, "127.0.0.1")
			token, err := s.store.grant(key, "mine")
			if err != nil {
				t.Fatal(err)
			}
			password := ""
			if name == "wrong" {
				password = "incorrect-password"
			}
			front := dialFrontRDP(t, s, cert, token)
			if err := passwordClientHandshake(front, "alice", password); err != nil {
				t.Fatal(err)
			}
			// Even a pipelined attempt to send desktop input must remain behind the gate.
			_, _ = front.Write([]byte("attempted desktop input"))
			_ = front.SetReadDeadline(time.Now().Add(time.Second))
			_, readErr := front.Read(make([]byte, 1))
			assertRDPConnectionClosed(t, readErr)
			if received.Load() {
				t.Fatal("backend received ClientInfo before password verification")
			}
		})
	}
}

func TestPasswordGateRejectsBackendDesktopBeforeAuthentication(t *testing.T) {
	t.Parallel()
	endpoint := startTestBackend(t, func(conn net.Conn) bool {
		g := rdpHandshake{remaining: 256 << 10}
		if _, _, err := g.read(conn); err != nil {
			return false
		}
		_ = writePacket(conn, mcsPacket([]byte{0x68, 0, 0, 3, 0xeb, 0x70, 4, 'd', 'e', 's', 'k'}))

		return false
	})
	s, _, cert, raw, _ := startGatewayForTest(t, endpoint)
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	front := dialFrontRDP(t, s, cert, token)
	if err := writePacket(front, mcsPacket([]byte{0x7f, 0x65, 0})); err != nil {
		t.Fatal(err)
	}
	_ = front.SetReadDeadline(time.Now().Add(time.Second))
	n, readErr := front.Read(make([]byte, 100))
	assertRDPConnectionClosed(t, readErr)
	if n != 0 {
		t.Fatal("pre-authentication desktop bytes escaped")
	}
}

func TestAuthenticationPacketReaderRejectsFastPathAndBoundsAllocation(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{
		{0, 0, 0, 8, 2, 0xf0, 0x80, 0},
		{3, 0, 0, 7, 2, 0xf0, 0x80},
		{3, 0, 0xff, 0xff},
		{3, 0, 0, 8, 2, 0xf0, 0, 0},
	} {
		g := rdpHandshake{remaining: 64}
		if _, _, err := g.read(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid pre-auth packet accepted")
		}
	}
	g := rdpHandshake{remaining: 16}
	p := mcsPacket([]byte{0x28})
	if _, _, err := g.read(bytes.NewReader(p)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.read(bytes.NewReader(p)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.read(bytes.NewReader(p)); err == nil {
		t.Fatal("handshake byte budget ignored")
	}
}
