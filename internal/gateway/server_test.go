package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"testing"
	"time"
)

func TestServerRejectsMissingAuthenticationAndUnsafeAddresses(t *testing.T) {
	t.Parallel()
	_, _, cert, key := testTLSIdentity(t, "gateway.test")
	for _, tc := range []struct {
		name   string
		change func(*Config)
	}{
		{name: "missing auth", change: func(c *Config) { c.Authenticator = nil }},
		{name: "missing backend", change: func(c *Config) { c.Backend = nil }},
		{name: "missing certificate", change: func(c *Config) { c.CertFile = "" }},
		{name: "header injection", change: func(c *Config) { c.PublicAddress = "gateway.test\r\nx:443" }},
		{name: "URL as address", change: func(c *Config) { c.PublicAddress = "https://gateway.test:443" }},
		{name: "missing public port", change: func(c *Config) { c.PublicAddress = "gateway.test" }},
		{name: "short creation timeout", change: func(c *Config) { c.CreationTimeout = time.Second }},
		{name: "unbounded creation timeout", change: func(c *Config) { c.CreationTimeout = 25 * time.Hour }},
		{name: "unbounded sessions", change: func(c *Config) { c.MaxSessions = 10001 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c := Config{
				PublicAddress: "gateway.test:443",
				CertFile:      cert,
				KeyFile:       key,
				Authenticator: testAuthenticator(
					func(
						context.Context,
						string,
						string,
					) (string, error) {
						return "alice", nil
					},
				),
				Backend: &webBackend{},
			}
			tc.change(&c)
			if _, err := New(c); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid configuration accepted: %v", err)
			}
		})
	}
}

func TestServerCancellationClosesIncompleteConnections(t *testing.T) {
	t.Parallel()
	_, _, cert, key := testTLSIdentity(t, "gateway.test")
	s, err := New(
		Config{
			PublicAddress: "gateway.test:443",
			CertFile:      cert,
			KeyFile:       key,
			Authenticator: testAuthenticator(
				func(
					context.Context,
					string,
					string,
				) (string, error) {
					return "alice", nil
				},
			),
			Backend:        &webBackend{},
			MaxConnections: 2,
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
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- s.serve(ctx, ln) }()
	var clients []net.Conn
	defer func() {
		for _, c := range clients {
			_ = c.Close()
		}
	}()
	for i := 0; i < 2; i++ {
		c, err := net.DialTimeout("tcp", ln.Addr().String(), time.Second)
		if err != nil {
			t.Fatal(err)
		}
		clients = append(clients, c)
	}
	cancel()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown waited for slow client timeout")
	}
	for _, c := range clients {
		_ = c.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := c.Read(make([]byte, 1)); err == nil {
			t.Fatal("connection survived shutdown")
		}
	}
	s.mu.Lock()
	remaining := len(s.connections)
	s.mu.Unlock()
	if remaining != 0 {
		t.Fatalf("%d connections survived shutdown", remaining)
	}
}

func TestTokenCanBeConsumedOnlyOnceConcurrently(t *testing.T) {
	t.Parallel()
	s, _, raw, _ := webFixture(t)
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, _, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	accepted := make(chan bool, 16)
	for i := 0; i < 16; i++ {
		workers.Go(func() {
			_, _, _, release, ok := s.store.consume(context.Background(), token, "127.0.0.1")
			if ok {
				release()
			}
			accepted <- ok
		})
	}
	workers.Wait()
	close(accepted)
	count := 0
	for ok := range accepted {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("token accepted %d times", count)
	}
}

func TestLDAPCancellationClosesPendingStartTLS(t *testing.T) {
	t.Parallel()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	a, err := NewLDAPAuthenticator(LDAPConfig{URL: "ldap://" + ln.Addr().String(), BaseDN: "dc=example"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := a.Authenticate(ctx, "alice", "not-sent-before-tls"); result <- err }()
	raw, err := ln.Accept()
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(time.Second))
	buf := make([]byte, 4096)
	n, err := raw.Read(buf)
	if err != nil {
		t.Fatal(err)
	}
	if n == 0 {
		t.Fatal("no StartTLS request")
	}
	cancel()
	select {
	case err := <-result:
		if err == nil {
			t.Fatal("LDAP authenticated without StartTLS response")
		}
	case <-time.After(time.Second):
		t.Fatal("LDAP cancellation did not interrupt pending StartTLS")
	}
	if _, err := raw.Read(buf); !errors.Is(err, io.EOF) {
		t.Fatalf("LDAP transport remained open: %v", err)
	}
}

func TestRDPGrantReplacementAndBoundedCapacity(t *testing.T) {
	t.Parallel()

	s, _, raw, _ := webFixture(t)
	key, _, _ := s.store.session(raw, "127.0.0.1")
	old, _, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	fresh, _, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	if _, _, _, _, ok := s.store.consume(context.Background(), old, "127.0.0.1"); ok {
		t.Fatal("a new download did not revoke the previous file")
	}
	for i := range 31 {
		if _, _, err := s.store.grant(key, fmt.Sprintf("vm-%d", i)); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := s.store.grant(key, "over-limit"); !errors.Is(err, ErrConflict) {
		t.Fatalf("token capacity is not bounded: %v", err)
	}
	ctx, owner, vm, release, ok := s.store.consume(context.Background(), fresh, "127.0.0.1")
	if !ok || ctx.Err() != nil || owner != "alice" || vm != "mine" {
		t.Fatal("capacity checks invalidated an existing token")
	}
	release()
	if _, _, err := s.store.grant(key, "after-consume"); err != nil {
		t.Fatal("consumed token did not release capacity")
	}
}

func TestRDPRechecksOwnerAfterDownload(t *testing.T) {
	t.Parallel()

	s, backend, cert, raw, _ := startGatewayForTest(t, Endpoint{})
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, _, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	backend.mu.Lock()
	backend.vms[0].Owner = "bob"
	backend.mu.Unlock()
	front := dialFrontRDP(t, s, cert, token)
	_ = front.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := front.Read(make([]byte, 1)); err == nil {
		t.Fatal("a stale ownership grant opened a connection")
	}
	backend.mu.Lock()
	defer backend.mu.Unlock()
	if len(backend.calls) != 0 {
		t.Fatal("ownership check occurred after backend resolution")
	}
}
