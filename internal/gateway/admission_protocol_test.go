package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

func newAdmissionProtocolServer(t *testing.T, configure func(*Config)) (*Server, string, *tls.Config) {
	t.Helper()
	_, certPEM, certFile, keyFile := testTLSIdentity(t, "gateway.test")
	config := Config{
		ListenAddr: "127.0.0.1:0", PublicAddress: "gateway.test:443",
		CertFile: certFile, KeyFile: keyFile, Backend: &webBackend{},
		Authenticator: testAuthenticator(func(_ context.Context, username, password string) (string, error) {
			if username == "alice" && password == "test-password" {
				return "alice", nil
			}

			return "", ErrAuthentication
		}),
	}
	if configure != nil {
		configure(&config)
	}
	server, err := New(config)
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("invalid test certificate")
	}

	return server, runTLSTestServer(t, server), &tls.Config{
		RootCAs: roots, ServerName: "gateway.test", MinVersion: tls.VersionTLS12,
	}
}

func dialAdmissionHTTPS(t *testing.T, address string, config *tls.Config) *tls.Conn {
	t.Helper()
	connection, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", address, config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = connection.Close() })
	if err := connection.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}

	return connection
}

func admissionHTTPResponse(t *testing.T, connection net.Conn, request string) *http.Response {
	t.Helper()
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(connection), nil)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		_ = response.Body.Close()
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}

	return response
}

func assertAdmissionProtocolCounts(t *testing.T, server *Server, pending, authenticated int) {
	t.Helper()
	server.mu.Lock()
	gotPending, gotAuthenticated := server.admission.pending, server.admission.authenticated
	server.mu.Unlock()
	if gotPending != pending || gotAuthenticated != authenticated {
		t.Fatalf("admitted pending/authenticated = %d/%d, want %d/%d",
			gotPending, gotAuthenticated, pending, authenticated)
	}
}

func TestAdmissionUnauthenticatedHTTPSExpires(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name       string
		incomplete bool
	}{
		{name: "incomplete headers", incomplete: true},
		{name: "login keepalive"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server, address, config := newAdmissionProtocolServer(t, func(config *Config) {
				config.HandshakeTimeout = time.Second
			})
			connection := dialAdmissionHTTPS(t, address, config)
			if test.incomplete {
				if _, err := io.WriteString(connection, "GET /login HTTP/1.1\r\nHost: gateway.test\r\n"); err != nil {
					t.Fatal(err)
				}
			} else {
				for range 2 {
					response := admissionHTTPResponse(t, connection,
						"GET /login HTTP/1.1\r\nHost: gateway.test\r\n\r\n")
					if response.StatusCode != http.StatusOK {
						t.Fatalf("login status = %d", response.StatusCode)
					}
				}
				assertAdmissionProtocolCounts(t, server, 1, 0)
			}
			// Neither net/http's header deadline nor a completed public request
			// may extend the admission deadline or count as authentication.
			if err := connection.SetReadDeadline(time.Now().Add(2 * time.Second)); err != nil {
				t.Fatal(err)
			}
			if _, err := connection.Read(make([]byte, 1)); err == nil {
				t.Fatal("unauthenticated connection remained open")
			} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
				t.Fatal("HTTP deadlines extended the absolute authentication deadline")
			}
		})
	}
}

func TestAdmissionHTTPSSeparatesAuthenticatedCapacity(t *testing.T) {
	t.Parallel()
	server, address, config := newAdmissionProtocolServer(t, func(config *Config) {
		config.MaxConnections = 1
		config.MaxHandshakes = 2
	})
	session, err := server.newSession("alice", "127.0.0.1", "test-hash")
	if err != nil {
		t.Fatal(err)
	}
	authenticated := dialAdmissionHTTPS(t, address, config)
	protected := fmt.Sprintf("GET /health/latency HTTP/1.1\r\nHost: gateway.test\r\nCookie: %s=%s\r\n\r\n",
		sessionCookie, session)
	if response := admissionHTTPResponse(t, authenticated, protected); response.StatusCode != http.StatusNoContent {
		t.Fatalf("authenticated status = %d", response.StatusCode)
	}
	assertAdmissionProtocolCounts(t, server, 0, 1)

	pending := dialAdmissionHTTPS(t, address, config)
	if response := admissionHTTPResponse(t, pending,
		"GET /login HTTP/1.1\r\nHost: gateway.test\r\n\r\n"); response.StatusCode != http.StatusOK {
		t.Fatalf("full authenticated pool blocked public login: %d", response.StatusCode)
	}
	assertAdmissionProtocolCounts(t, server, 1, 1)
	if response := admissionHTTPResponse(t, pending, protected); response.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("promotion into full authenticated pool returned %d, want 503", response.StatusCode)
	}
	if response := admissionHTTPResponse(t, authenticated, protected); response.StatusCode != http.StatusNoContent {
		t.Fatalf("pending connection disrupted authenticated connection: %d", response.StatusCode)
	}
}

func TestAdmissionHTTPSPromotesCookieBeforeReadingForm(t *testing.T) {
	t.Parallel()
	server, address, config := newAdmissionProtocolServer(t, nil)
	session, err := server.newSession("alice", "127.0.0.1", "test-hash")
	if err != nil {
		t.Fatal(err)
	}
	_, sessionData, ok := server.store.session(session, "127.0.0.1")
	if !ok {
		t.Fatal("missing test session")
	}
	body := url.Values{"csrf": {sessionData.csrf}, "name": {"work"}}.Encode()
	connection := dialAdmissionHTTPS(t, address, config)
	headers := fmt.Sprintf("POST /vms HTTP/1.1\r\nHost: gateway.test\r\nOrigin: https://gateway.test\r\n"+
		"Cookie: %s=%s\r\nContent-Type: application/x-www-form-urlencoded\r\n"+
		"Content-Length: %d\r\nExpect: 100-continue\r\n\r\n", sessionCookie, session, len(body))
	if response := admissionHTTPResponse(t, connection, headers); response.StatusCode != http.StatusContinue {
		t.Fatalf("waiting for request body returned %d, want 100 Continue", response.StatusCode)
	}
	assertAdmissionProtocolCounts(t, server, 0, 1)
	if response := admissionHTTPResponse(t, connection, body); response.StatusCode != http.StatusSeeOther {
		t.Fatalf("authenticated form returned %d", response.StatusCode)
	}
}

func TestAdmissionHTTPSLoginRequiresValidCredentials(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		password string
		status   int
	}{
		{name: "valid credentials", password: "test-password", status: http.StatusSeeOther},
		{name: "invalid credentials", password: "wrong-password", status: http.StatusUnauthorized},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server, address, config := newAdmissionProtocolServer(t, nil)
			connection := dialAdmissionHTTPS(t, address, config)
			response := admissionHTTPResponse(t, connection,
				"GET /login HTTP/1.1\r\nHost: gateway.test\r\n\r\n")
			csrf := ""
			for _, cookie := range response.Cookies() {
				if cookie.Name == loginCookie {
					csrf = cookie.Value
				}
			}
			if csrf == "" {
				t.Fatal("missing login CSRF cookie")
			}
			body := url.Values{"csrf": {csrf}, "username": {"alice"}, "password": {test.password}}.Encode()
			request := fmt.Sprintf("POST /login HTTP/1.1\r\nHost: gateway.test\r\nOrigin: https://gateway.test\r\n"+
				"Cookie: %s=%s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\n\r\n%s",
				loginCookie, csrf, len(body), body)
			if response := admissionHTTPResponse(t, connection, request); response.StatusCode != test.status {
				t.Fatalf("login status = %d, want %d", response.StatusCode, test.status)
			}
			if test.status == http.StatusSeeOther {
				assertAdmissionProtocolCounts(t, server, 0, 1)
			} else {
				assertAdmissionProtocolCounts(t, server, 1, 0)
			}
		})
	}
}

func TestAdmissionRDPRequiresCredentialsNotOnlyRoutingToken(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		password string
		allowed  bool
	}{
		{name: "valid credentials", password: "rdp-password", allowed: true},
		{name: "invalid credentials", password: "wrong-password"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			backendReady := make(chan struct{})
			endpoint := startTestBackend(t, func(connection net.Conn) bool {
				close(backendReady)

				return passwordBackendHandshake(connection, nil)
			})
			server, _, certPEM, session, _ := startGatewayForTest(t, endpoint)
			key, _, ok := server.store.session(session, "127.0.0.1")
			if !ok {
				t.Fatal("missing test session")
			}
			token, err := server.store.grant(key, "mine")
			if err != nil {
				t.Fatal(err)
			}
			connection := dialFrontRDP(t, server, certPEM, token)
			select {
			case <-backendReady:
			case <-time.After(2 * time.Second):
				t.Fatal("RDP backend was not reached")
			}
			assertAdmissionProtocolCounts(t, server, 1, 0)
			if err := passwordClientHandshake(connection, "alice", test.password); err != nil {
				t.Fatal(err)
			}
			if !test.allowed {
				if _, err := connection.Read(make([]byte, 1)); err == nil {
					t.Fatal("failed RDP credentials left the connection open")
				} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
					t.Fatal("failed RDP authentication did not close the connection")
				}
				server.mu.Lock()
				authenticated := server.admission.authenticated
				server.mu.Unlock()
				if authenticated != 0 {
					t.Fatal("failed RDP credentials consumed authenticated capacity")
				}

				return
			}
			if _, err := io.WriteString(connection, "admitted"); err != nil {
				t.Fatal(err)
			}
			echo := make([]byte, len("admitted"))
			if _, err := io.ReadFull(connection, echo); err != nil || string(echo) != "admitted" {
				t.Fatalf("authenticated RDP echo = %q, error = %v", echo, err)
			}
			assertAdmissionProtocolCounts(t, server, 0, 1)
		})
	}
}

func TestAdmissionTimeoutCancelsHTTPAuthentication(t *testing.T) {
	t.Parallel()
	started := make(chan struct{})
	canceled := make(chan struct{})
	_, address, config := newAdmissionProtocolServer(t, func(config *Config) {
		config.HandshakeTimeout = time.Second
		config.Authenticator = testAuthenticator(func(ctx context.Context, _, _ string) (string, error) {
			close(started)
			<-ctx.Done()
			close(canceled)

			return "", ctx.Err()
		})
	})
	csrf, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	body := url.Values{"csrf": {csrf}, "username": {"alice"}, "password": {"test-password"}}.Encode()
	connection := dialAdmissionHTTPS(t, address, config)
	request := fmt.Sprintf("POST /login HTTP/1.1\r\nHost: gateway.test\r\nOrigin: https://gateway.test\r\n"+
		"Cookie: %s=%s\r\nContent-Type: application/x-www-form-urlencoded\r\nContent-Length: %d\r\n\r\n%s",
		loginCookie, csrf, len(body), body)
	if _, err := io.WriteString(connection, request); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("HTTP authentication did not start")
	}
	select {
	case <-canceled:
	case <-time.After(2 * time.Second):
		t.Fatal("absolute authentication deadline did not cancel credential verification")
	}
}

func TestAdmissionHTTPSClosesConnectionAfterSessionRevocation(t *testing.T) {
	t.Parallel()
	server, address, config := newAdmissionProtocolServer(t, nil)
	session, err := server.newSession("alice", "127.0.0.1", "test-hash")
	if err != nil {
		t.Fatal(err)
	}
	connection := dialAdmissionHTTPS(t, address, config)
	protected := fmt.Sprintf("GET /health/latency HTTP/1.1\r\nHost: gateway.test\r\nCookie: %s=%s\r\n\r\n",
		sessionCookie, session)
	if response := admissionHTTPResponse(t, connection, protected); response.StatusCode != http.StatusNoContent {
		t.Fatalf("authenticated status = %d", response.StatusCode)
	}
	assertAdmissionProtocolCounts(t, server, 0, 1)
	key, _ := tokenKey(session)
	server.store.revoke(key)
	request := fmt.Sprintf("GET /login HTTP/1.1\r\nHost: gateway.test\r\nCookie: %s=%s\r\n\r\n",
		sessionCookie, session)
	response := admissionHTTPResponse(t, connection, request)
	if response.StatusCode != http.StatusOK || !response.Close {
		t.Fatalf("revoked session login response = %d, close = %t; want 200 with Connection: close",
			response.StatusCode, response.Close)
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); err == nil {
		t.Fatal("revoked session kept authenticated connection open")
	} else if timeout, ok := err.(net.Error); ok && timeout.Timeout() {
		t.Fatal("revoked session retained authenticated capacity after response")
	}
}

func TestAdmissionHTTPSRejectsUnusableSessionCookies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		ip      string
		invalid bool
		expired bool
		unknown bool
	}{
		{name: "invalid token", invalid: true},
		{name: "unknown token", unknown: true},
		{name: "expired session", ip: "127.0.0.1", expired: true},
		{name: "different source IP", ip: "127.0.0.2"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			server, address, config := newAdmissionProtocolServer(t, nil)
			var session string
			var err error
			switch {
			case test.invalid:
				session = "not-a-session-token"
			case test.unknown:
				session, err = randomToken()
			default:
				session, err = server.newSession("alice", test.ip, "test-hash")
				if err == nil && test.expired {
					key, _ := tokenKey(session)
					server.store.mu.Lock()
					server.store.sessions[key].expires = time.Now().Add(-time.Second)
					server.store.mu.Unlock()
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			connection := dialAdmissionHTTPS(t, address, config)
			request := fmt.Sprintf("GET /health/latency HTTP/1.1\r\nHost: gateway.test\r\nCookie: %s=%s\r\n\r\n",
				sessionCookie, session)
			if response := admissionHTTPResponse(t, connection, request); response.StatusCode != http.StatusSeeOther {
				t.Fatalf("unusable session status = %d, want redirect to login", response.StatusCode)
			}
			assertAdmissionProtocolCounts(t, server, 1, 0)
		})
	}
}
