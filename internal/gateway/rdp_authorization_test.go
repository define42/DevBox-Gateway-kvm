package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func TestRDPAuthorizationDoesNotDependOnInventory(t *testing.T) {
	t.Parallel()
	endpoint := startTestBackend(t, func(conn net.Conn) bool { return passwordBackendHandshake(conn, nil) })
	s, backend, cert, raw, _ := startGatewayForTest(t, endpoint)
	backend.mu.Lock()
	backend.listErr = ErrConflict
	backend.mu.Unlock()
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	front := dialFrontRDP(t, s, cert, token)
	if err := passwordClientHandshake(front, "alice", "rdp-password"); err != nil {
		t.Fatal(err)
	}
	if _, err := front.Write([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	var response [5]byte
	if _, err := io.ReadFull(front, response[:]); err != nil || string(response[:]) != "ready" {
		t.Fatalf("RDP depended on unavailable inventory: %q, %v", response, err)
	}
}

func TestRDPRequiresFreshCredentialsForTheGrantOwner(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, username, password, currentPassword, identity string
		revoked, allowed                                    bool
	}{
		{
			name: "valid password", username: "alice", password: "rdp-password",
			currentPassword: "rdp-password", identity: "alice", allowed: true,
		},
		{
			name: "login alias", username: "alice.login", password: "rdp-password",
			currentPassword: "rdp-password", identity: "alice", allowed: true,
		},
		{
			name: "another valid account", username: "bob", password: "rdp-password",
			currentPassword: "rdp-password", identity: "bob",
		},
		{
			name: "old password after directory change", username: "alice", password: "rdp-password",
			currentPassword: "new-password", identity: "alice",
		},
		{
			name: "current password after directory change", username: "alice", password: "new-password",
			currentPassword: "new-password", identity: "alice", allowed: true,
		},
		{
			name: "directory access revoked", username: "alice", password: "rdp-password",
			currentPassword: "rdp-password", identity: "alice", revoked: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var received atomic.Bool
			endpoint := startTestBackend(t, func(conn net.Conn) bool { return passwordBackendHandshake(conn, &received) })
			var attempts atomic.Int32
			auth := testAuthenticator(func(_ context.Context, username, password string) (string, error) {
				attempts.Add(1)
				if tc.revoked || username != tc.username || password != tc.currentPassword {
					return "", ErrAuthentication
				}

				return tc.identity, nil
			})
			// The fixture browser session predates this connection and has a stored guest password hash.
			s, _, cert, raw, _ := startGatewayWithAuthenticator(t, endpoint, auth)
			key, _, _ := s.store.session(raw, "127.0.0.1")
			token, err := s.store.grant(key, "mine")
			if err != nil {
				t.Fatal(err)
			}
			front := dialFrontRDP(t, s, cert, token)
			if err := passwordClientHandshake(front, tc.username, tc.password); err != nil {
				t.Fatal(err)
			}
			_, _ = front.Write([]byte("ready"))
			var reply [5]byte
			_, err = io.ReadFull(front, reply[:])
			if attempts.Load() != 1 {
				t.Fatalf("RDP authentication calls: got %d, want 1", attempts.Load())
			}
			if tc.allowed {
				if err != nil || string(reply[:]) != "ready" || !received.Load() {
					t.Fatalf("valid current credentials did not open the desktop: %v", err)
				}
				if s.store.claimGrant(token, "127.0.0.1") {
					t.Fatal("authenticated connection did not spend its routing token")
				}

				return
			}
			assertRDPConnectionClosed(t, err)
			if received.Load() {
				t.Fatal("invalid credentials reached the desktop")
			}
			ctx, _, _, release, ok := s.store.pendingGrant(context.Background(), token, "127.0.0.1")
			if !ok {
				t.Fatal("failed authentication spent the routing token")
			}
			release()
			if ctx.Err() == nil {
				t.Fatal("released unauthenticated connection retained its lease")
			}
		})
	}
}

func TestRDPAuthenticationSharesLoginLimits(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"rate limit", "concurrent authentication limit"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var received atomic.Bool
			endpoint := startTestBackend(t, func(conn net.Conn) bool { return passwordBackendHandshake(conn, &received) })
			var attempts atomic.Int32
			auth := testAuthenticator(func(context.Context, string, string) (string, error) {
				attempts.Add(1)

				return "alice", nil
			})
			s, _, cert, raw, _ := startGatewayWithAuthenticator(t, endpoint, auth)
			if name == "rate limit" {
				for range 10 {
					if !s.store.allowLogin("127.0.0.1") {
						t.Fatal("unexpected initial login rejection")
					}
				}
			} else {
				for range cap(s.authSlots) {
					s.authSlots <- struct{}{}
				}
			}
			key, _, _ := s.store.session(raw, "127.0.0.1")
			token, err := s.store.grant(key, "mine")
			if err != nil {
				t.Fatal(err)
			}
			front := dialFrontRDP(t, s, cert, token)
			if err := passwordClientHandshake(front, "alice", "rdp-password"); err != nil {
				t.Fatal(err)
			}
			_ = front.SetReadDeadline(time.Now().Add(time.Second))
			_, err = front.Read(make([]byte, 1))
			assertRDPConnectionClosed(t, err)
			if received.Load() || attempts.Load() != 0 {
				t.Fatal("RDP bypassed shared authentication limits")
			}
		})
	}
}

func TestLogoutCancelsRDPPasswordValidation(t *testing.T) {
	t.Parallel()
	var received atomic.Bool
	endpoint := startTestBackend(t, func(conn net.Conn) bool { return passwordBackendHandshake(conn, &received) })
	started := make(chan struct{})
	finished := make(chan struct{})
	auth := testAuthenticator(func(ctx context.Context, _, _ string) (string, error) {
		close(started)
		<-ctx.Done()
		close(finished)

		// An authenticator returning a result at cancellation must still fail closed.
		return "alice", nil
	})
	s, _, cert, raw, _ := startGatewayWithAuthenticator(t, endpoint, auth)
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	front := dialFrontRDP(t, s, cert, token)
	if err := passwordClientHandshake(front, "alice", "rdp-password"); err != nil {
		t.Fatal(err)
	}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("RDP did not start password validation")
	}
	s.store.revoke(key)
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("logout did not cancel password validation")
	}
	_, err = front.Read(make([]byte, 1))
	assertRDPConnectionClosed(t, err)
	if received.Load() {
		t.Fatal("revoked session reached the desktop after password validation")
	}
	if s.store.claimGrant(token, "127.0.0.1") {
		t.Fatal("logout retained the in-flight grant")
	}
}

func assertRDPConnectionClosed(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		t.Fatal("unauthenticated RDP connection remained open")
	}
	var networkError net.Error
	if errors.As(err, &networkError) && networkError.Timeout() {
		t.Fatal("RDP connection timed out instead of closing after authentication rejection")
	}
}
