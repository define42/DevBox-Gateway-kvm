package gateway

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"testing"
	"time"
)

func startEchoBackend(t *testing.T) Endpoint {
	t.Helper()

	return startTestBackend(t, nil)
}

func startTestBackend(t *testing.T, beforeCopy func(net.Conn) bool) Endpoint {
	t.Helper()
	certificate, certPEM, _, _ := testTLSIdentity(t, "backend.test")
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		defer close(done)
		raw, err := ln.Accept()
		if err != nil {
			return
		}
		defer raw.Close()
		stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
		defer stop()
		_ = raw.SetDeadline(time.Now().Add(5 * time.Second))
		packet, err := readConnectionPacket(raw)
		if err != nil {
			return
		}
		token, protocols, err := parseConnectionRequest(packet)
		if err != nil || token != "" || protocols != protocolTLS {
			t.Error("backend received a forwarded token or invalid fresh negotiation")

			return
		}
		if err := writePacket(raw, connectionConfirm(2)); err != nil {
			return
		}
		secure := tls.Server(
			raw,
			&tls.Config{
				Certificates: []tls.Certificate{
					certificate,
				},
				MinVersion: tls.VersionTLS12,
			},
		)
		if err := secure.HandshakeContext(ctx); err != nil {
			return
		}
		_ = secure.SetDeadline(time.Time{})
		if beforeCopy != nil && !beforeCopy(secure) {
			return
		}
		_, _ = io.Copy(secure, secure)
	}()
	t.Cleanup(func() {
		cancel()
		_ = ln.Close()
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			t.Error("backend failed to stop")
		}
	})

	return Endpoint{Address: ln.Addr().String(), ServerName: "backend.test", CertificatePEM: certPEM}
}

func startGatewayForTest(t *testing.T, endpoint Endpoint) (*Server, *webBackend, []byte, string, browserSession) {
	t.Helper()

	return startGatewayWithAuthenticator(t, endpoint, testAuthenticator(
		func(_ context.Context, username, password string) (string, error) {
			if username == "alice" && password == "rdp-password" {
				return "alice", nil
			}

			return "", ErrAuthentication
		},
	))
}

func startGatewayWithAuthenticator(
	t *testing.T, endpoint Endpoint, authenticator Authenticator,
) (*Server, *webBackend, []byte, string, browserSession) {
	t.Helper()
	_, certPEM, certFile, keyFile := testTLSIdentity(t, "gateway.test")
	backend := &webBackend{vms: []VM{{ID: "mine", Name: "work", Owner: "alice", State: "running"}}, endpoint: endpoint}
	s, err := New(
		Config{
			ListenAddr:    "127.0.0.1:0",
			PublicAddress: "gateway.test:443",
			CertFile:      certFile,
			KeyFile:       keyFile,
			Backend:       backend,
			Authenticator: authenticator,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, ln) }()
	// The listener is already bound; Serve's first Accept receives this dial.
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("gateway failed to stop")
		}
	})
	raw, err := s.newSession("alice", "127.0.0.1", "$6$rounds=100000$test$hash")
	if err != nil {
		t.Fatal(err)
	}
	_, sess, ok := s.store.session(raw, "127.0.0.1")
	if !ok {
		t.Fatal("missing session")
	}

	return s, backend, certPEM, raw, sess
}

func dialFrontRDP(t *testing.T, s *Server, certPEM []byte, token string) *tls.Conn {
	t.Helper()
	raw, err := net.DialTimeout("tcp", s.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
	if err := writePacket(raw, connectionRequest(token)); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	reply, err := readConnectionPacket(raw)
	if err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if len(reply) != 19 || binary.LittleEndian.Uint32(reply[15:]) != protocolTLS {
		raw.Close()
		t.Fatal("front did not require TLS")
	}
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	secure := tls.Client(raw, &tls.Config{MinVersion: tls.VersionTLS12, RootCAs: roots, ServerName: "gateway.test"})
	if err := secure.HandshakeContext(context.Background()); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })

	return secure
}

func TestGatewayMultiplexesHTTPSAndRDPAndLogoutClosesProxy(t *testing.T) {
	t.Parallel()
	endpoint := startTestBackend(t, func(conn net.Conn) bool { return passwordBackendHandshake(conn, nil) })
	s, backend, certPEM, raw, sess := startGatewayForTest(t, endpoint)
	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(certPEM)
	transport := &http.Transport{
		TLSClientConfig: &tls.Config{
			RootCAs:    roots,
			ServerName: "gateway.test",
			MinVersion: tls.VersionTLS12,
		},
		DialContext: func(
			ctx context.Context,
			_, _ string,
		) (net.Conn, error) {
			var dialer net.Dialer

			return dialer.DialContext(ctx, "tcp", s.Addr().String())
		},
	}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{
		Transport: transport,
		Timeout:   3 * time.Second,
		CheckRedirect: func(
			*http.Request,
			[]*http.Request,
		) error {
			return http.ErrUseLastResponse
		},
	}
	request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://gateway.test/", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.AddCookie(
		&http.Cookie{
			Name:     sessionCookie,
			Value:    raw,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		},
	)
	response, err := client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(response.Body)
	response.Body.Close()
	if err != nil || response.StatusCode != http.StatusOK || !strings.Contains(string(data), "Available DevBoxes") {
		t.Fatalf("HTTPS dashboard failed: status=%d err=%v", response.StatusCode, err)
	}
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	// A disconnected TLS handshake must not spend the browser's grant.
	aborted, err := net.Dial("tcp", s.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = aborted.SetDeadline(time.Now().Add(time.Second))
	if err := writePacket(aborted, connectionRequest(token)); err != nil {
		t.Fatal(err)
	}
	if _, err := readConnectionPacket(aborted); err != nil {
		t.Fatal(err)
	}
	aborted.Close()
	front := dialFrontRDP(t, s, certPEM, token)
	if err := passwordClientHandshake(front, "alice", "rdp-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := front.Write([]byte("hello")); err != nil {
		t.Fatal(err)
	}
	echo := make([]byte, 5)
	if _, err := io.ReadFull(front, echo); err != nil || string(echo) != "hello" {
		t.Fatalf("RDP roundtrip failed: %q %v", echo, err)
	}
	replay := dialFrontRDP(t, s, certPEM, token)
	_ = replay.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := replay.Read(make([]byte, 1)); err == nil {
		t.Fatal("replayed token opened another connection")
	}
	replay.NetConn().Close()
	backend.mu.Lock()
	calls := append([]string(nil), backend.calls...)
	backend.mu.Unlock()
	if len(calls) != 1 || calls[0] != "rdp:alice:mine" {
		t.Fatalf("unexpected backend resolutions: %v", calls)
	}
	form := url.Values{"csrf": {sess.csrf}}
	logout, err := http.NewRequestWithContext(
		context.Background(),
		http.MethodPost,
		"https://gateway.test/logout",
		strings.NewReader(
			form.Encode(),
		),
	)
	if err != nil {
		t.Fatal(err)
	}
	logout.Header.Set("Origin", "https://gateway.test")
	logout.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	logout.AddCookie(
		&http.Cookie{
			Name:     sessionCookie,
			Value:    raw,
			Secure:   true,
			HttpOnly: true,
			SameSite: http.SameSiteStrictMode,
		},
	)
	response, err = client.Do(logout)
	if err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	if response.StatusCode != http.StatusSeeOther {
		t.Fatalf("logout: %d", response.StatusCode)
	}
	_ = front.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := front.Read(make([]byte, 1)); err == nil {
		t.Fatal("logout retained active RDP connection")
	}
}

func TestBackendTLSRequiresCertificateAndServerName(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"valid", "wrong name", "wrong certificate", "non-loopback"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			endpoint := startEchoBackend(t)
			switch name {
			case "wrong name":
				endpoint.ServerName = "attacker.test"
			case "wrong certificate":
				_, endpoint.CertificatePEM, _, _ = testTLSIdentity(t, "backend.test")
			case "non-loopback":
				endpoint.Address = "192.0.2.1:3389"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			defer cancel()
			conn, err := dialRDPBackend(ctx, endpoint)
			if name != "valid" {
				if err == nil {
					conn.NetConn().Close()
					t.Fatal("unverified backend accepted")
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer conn.NetConn().Close()
			if _, err := conn.Write([]byte("hello")); err != nil {
				t.Fatal(err)
			}
			echo := make([]byte, 5)
			if _, err := io.ReadFull(conn, echo); err != nil || string(echo) != "hello" {
				t.Fatalf("backend echo failed: %q %v", echo, err)
			}
		})
	}
}

func TestRDPConnectionParserRejectsMalformedFrames(t *testing.T) {
	t.Parallel()
	token, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	valid := connectionRequest(token)
	if got, protocols, err := parseConnectionRequest(valid); err != nil || got != token || protocols != protocolTLS {
		t.Fatalf("valid packet rejected: %s %d %v", got, protocols, err)
	}
	for _, tc := range []struct {
		name  string
		alter func([]byte) []byte
	}{
		{name: "truncated", alter: func(b []byte) []byte { return b[:10] }},
		{name: "TPKT length", alter: func(b []byte) []byte {
			b[3]--

			return b
		}},
		{name: "X224 length", alter: func(b []byte) []byte {
			b[4]--

			return b
		}},
		{name: "TPDU type", alter: func(b []byte) []byte {
			b[5] = 0xf0

			return b
		}},
		{name: "invalid token", alter: func(b []byte) []byte {
			b[11] = '!'

			return b
		}},
		{name: "negotiation length", alter: func(b []byte) []byte {
			b[len(b)-6] = 7

			return b
		}},
		{name: "trailing bytes", alter: func(b []byte) []byte {
			b = append(b, 0)
			b[3]++
			b[4]++

			return b
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, _, err := parseConnectionRequest(tc.alter(append([]byte(nil), valid...))); err == nil {
				t.Fatal("malformed connection accepted")
			}
		})
	}
}

func TestLogoutDuringRDPSetupCancelsAuthorization(t *testing.T) {
	t.Parallel()
	s, _, raw, _ := webFixture(t)
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	ctx, _, _, release, ok := s.store.pendingGrant(context.Background(), token, "127.0.0.1")
	if !ok {
		t.Fatal("consume failed")
	}
	defer release()
	s.store.revoke(key)
	if ctx.Err() == nil {
		t.Fatal("setup authorization survived logout")
	}
}
