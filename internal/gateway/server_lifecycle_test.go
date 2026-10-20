package gateway

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"path/filepath"
	"testing"
	"testing/synctest"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/cert"
)

func TestServerServeListenFailureClosesIdentity(t *testing.T) {
	t.Parallel()
	identity, err := cert.New(cert.Options{Hostname: "gateway.test", StateDir: filepath.Join(t.TempDir(), "tls")})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(identity.Close)
	s := &Server{config: Config{ListenAddr: "127.0.0.1:65536", TLSManager: identity}}
	if err := s.Serve(t.Context()); err == nil || errors.Is(err, ErrConflict) {
		t.Fatalf("invalid listen port = %v, want listen failure", err)
	}
	if err := identity.Start(t.Context(), nil); !errors.Is(err, cert.ErrInvalid) {
		t.Fatalf("listen failure retained certificate manager: %v", err)
	}
	if err := s.Serve(t.Context()); !errors.Is(err, ErrConflict) {
		t.Fatalf("second Serve = %v, want conflict", err)
	}
	if s.Addr() != nil {
		t.Fatal("failed listener was published")
	}
}

func TestServerIdentityStartupFailureClosesListener(t *testing.T) {
	t.Parallel()
	identity, err := cert.New(cert.Options{Hostname: "gateway.test", StateDir: filepath.Join(t.TempDir(), "tls")})
	if err != nil {
		t.Fatal(err)
	}
	identity.Close()
	s := &Server{config: Config{TLSManager: identity}}
	listener := lifecycleTestListener()
	if err := s.serve(t.Context(), listener); !errors.Is(err, cert.ErrInvalid) {
		t.Fatalf("closed identity startup = %v", err)
	}
	select {
	case <-listener.done:
	default:
		t.Fatal("identity startup failure left listener open")
	}
}

func TestServerMaintenanceAndShutdownRevokeSessions(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		s, _, expiringToken, _ := webFixture(t)
		liveToken, err := s.newSession("bob", "192.0.2.2", "live-secret")
		if err != nil {
			t.Fatal(err)
		}
		expiringKey, _ := tokenKey(expiringToken)
		liveKey, _ := tokenKey(liveToken)
		expiring := s.store.sessions[expiringKey]
		live := s.store.sessions[liveKey]
		expiring.expires = time.Now().Add(time.Second)
		expiredCtx, expiredCancel := context.WithCancel(t.Context())
		defer expiredCancel()
		liveCtx, liveCancel := context.WithCancel(t.Context())
		defer liveCancel()
		expiring.connections[&connectionLease{cancel: expiredCancel}] = struct{}{}
		live.connections[&connectionLease{cancel: liveCancel}] = struct{}{}
		if _, err := s.store.grant(expiringKey, "expired-desktop"); err != nil {
			t.Fatal(err)
		}
		if _, err := s.store.grant(liveKey, "live-desktop"); err != nil {
			t.Fatal(err)
		}
		s.store.attempts["192.0.2.3"] = loginWindow{start: time.Now(), count: 1}
		idleIP := netip.MustParseAddr("192.0.2.3")
		activeIP := netip.MustParseAddr("192.0.2.4")
		s.admission.peers = map[netip.Addr]*connectionPeer{
			idleIP:   {lastSeen: time.Now()},
			activeIP: {lastSeen: time.Now(), total: 1},
		}
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		listener := lifecycleTestListener()
		done := make(chan error, 1)
		go func() { done <- s.serve(ctx, listener) }()
		synctest.Wait()
		if s.Addr() != listener.Addr() {
			t.Fatal("bound listener address was not published")
		}
		time.Sleep(time.Minute)
		synctest.Wait()
		if expiredCtx.Err() == nil || expiring.passwordHash != "" {
			t.Fatal("maintenance did not revoke expired session credentials and leases")
		}
		if liveCtx.Err() != nil || live.passwordHash != "live-secret" {
			t.Fatal("maintenance revoked an unexpired session")
		}
		s.store.mu.Lock()
		sessions, grants, attempts := len(s.store.sessions), len(s.store.grants), len(s.store.attempts)
		s.store.mu.Unlock()
		if sessions != 1 || grants != 1 || attempts != 0 {
			t.Fatalf("maintenance retained sessions=%d grants=%d attempts=%d", sessions, grants, attempts)
		}
		s.mu.Lock()
		idle, active := s.admission.peers[idleIP], s.admission.peers[activeIP]
		s.mu.Unlock()
		if idle != nil || active == nil {
			t.Fatal("maintenance did not distinguish idle and active peer entries")
		}
		cancel()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if liveCtx.Err() == nil || live.passwordHash != "" {
			t.Fatal("shutdown retained session credentials or an active lease")
		}
		if len(s.store.sessions) != 0 || len(s.store.grants) != 0 || !s.draining {
			t.Fatal("shutdown did not revoke sessions and stop admitting requests")
		}
	})
}

func lifecycleTestListener() *channelListener {
	return &channelListener{
		addr:     &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1), Port: 8443},
		incoming: make(chan net.Conn),
		done:     make(chan struct{}),
	}
}
