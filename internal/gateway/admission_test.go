package gateway

import (
	"context"
	"errors"
	"io"
	"net"
	"net/netip"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type admissionTestConn struct {
	remote net.Addr
	closed atomic.Bool
}

func (c *admissionTestConn) Read([]byte) (int, error)    { return 0, io.EOF }
func (c *admissionTestConn) Write(p []byte) (int, error) { return len(p), nil }
func (c *admissionTestConn) Close() error {
	c.closed.Store(true)

	return nil
}
func (c *admissionTestConn) LocalAddr() net.Addr              { return c.remote }
func (c *admissionTestConn) RemoteAddr() net.Addr             { return c.remote }
func (c *admissionTestConn) SetDeadline(time.Time) error      { return nil }
func (c *admissionTestConn) SetReadDeadline(time.Time) error  { return nil }
func (c *admissionTestConn) SetWriteDeadline(time.Time) error { return nil }

func admissionRaw(ip string) *admissionTestConn {
	return &admissionTestConn{remote: &net.TCPAddr{IP: net.ParseIP(ip), Port: 12345}}
}

func admissionTestServer(t *testing.T, configure func(*Config)) *Server {
	t.Helper()
	config := Config{MaxConnections: 2}
	if configure != nil {
		configure(&config)
	}
	if err := validateConnectionConfig(&config); err != nil {
		t.Fatal(err)
	}
	s := &Server{config: config, connections: make(map[*trackedConn]struct{})}
	t.Cleanup(func() {
		s.mu.Lock()
		all := make([]*trackedConn, 0, len(s.connections))
		for conn := range s.connections {
			all = append(all, conn)
		}
		s.mu.Unlock()
		for _, conn := range all {
			_ = conn.Close()
		}
	})

	return s
}

func mustAdmit(t *testing.T, s *Server, ip string) *trackedConn {
	t.Helper()
	conn := s.admitConnection(t.Context(), admissionRaw(ip))
	if conn == nil {
		t.Fatalf("connection from %s was rejected", ip)
	}

	return conn
}

func TestAdmissionSeparatePools(t *testing.T) {
	t.Parallel()
	s := admissionTestServer(t, func(c *Config) {
		c.MaxConnections, c.MaxHandshakes = 1, 2
	})
	authenticated := mustAdmit(t, s, "192.0.2.1")
	if !s.promoteConnection(authenticated) {
		t.Fatal("initial promotion failed")
	}
	if !s.promoteConnection(authenticated) {
		t.Fatal("authentication must be idempotent")
	}
	first := mustAdmit(t, s, "192.0.2.2")
	second := mustAdmit(t, s, "192.0.2.3")
	if s.admitConnection(t.Context(), admissionRaw("192.0.2.4")) != nil {
		t.Fatal("pending pool exceeded")
	}
	assertAdmissionProtocolCounts(t, s, 2, 1)
	if s.promoteConnection(first) {
		t.Fatal("authenticated pool exceeded")
	}
	if !s.promoteConnection(authenticated) {
		t.Fatal("full pending pool affected an authenticated connection")
	}
	_ = authenticated.Close()
	if !s.promoteConnection(first) {
		t.Fatal("authenticated slot not released on close")
	}
	_ = first.Close()
	_ = first.Close()
	_ = second.Close()
	assertAdmissionProtocolCounts(t, s, 0, 0)
}

func TestAdmissionPerSourceLimits(t *testing.T) {
	t.Parallel()
	s := admissionTestServer(t, func(c *Config) {
		c.MaxConnectionsPerIP, c.MaxHandshakesPerIP = 2, 1
	})
	first := mustAdmit(t, s, "192.0.2.1")
	if s.admitConnection(t.Context(), admissionRaw("::ffff:192.0.2.1")) != nil {
		t.Fatal("IPv4-mapped address bypassed pending source limit")
	}
	other := mustAdmit(t, s, "192.0.2.2")
	if !s.promoteConnection(first) {
		t.Fatal("promotion failed")
	}
	second := mustAdmit(t, s, "192.0.2.1")
	if !s.promoteConnection(second) {
		t.Fatal("second promotion failed")
	}
	if s.admitConnection(t.Context(), admissionRaw("192.0.2.1")) != nil {
		t.Fatal("combined pending/authenticated source limit exceeded")
	}
	_ = first.Close()
	third := mustAdmit(t, s, "192.0.2.1")
	_ = other.Close()
	_ = second.Close()
	_ = third.Close()
	assertAdmissionProtocolCounts(t, s, 0, 0)
}

func TestAdmissionRateLimitsPersistAcrossDisconnect(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := admissionTestServer(t, func(c *Config) {
			c.ConnectionRateLimit, c.ConnectionRateLimitPerIP = 2, 1
		})
		_ = mustAdmit(t, s, "192.0.2.1").Close()
		for range 10 {
			if s.admitConnection(t.Context(), admissionRaw("192.0.2.1")) != nil {
				t.Fatal("disconnect reset source rate allowance")
			}
		}
		// Rejected attempts from the abusive IP cannot spend this token.
		_ = mustAdmit(t, s, "192.0.2.2").Close()
		if s.admitConnection(t.Context(), admissionRaw("192.0.2.3")) != nil {
			t.Fatal("global burst exceeded")
		}
		time.Sleep(time.Second)
		_ = mustAdmit(t, s, "192.0.2.1").Close()
		_ = mustAdmit(t, s, "192.0.2.3").Close()
		assertAdmissionProtocolCounts(t, s, 0, 0)
	})
}

func TestAdmissionAbsoluteDeadlineCancelsWork(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := admissionTestServer(t, nil)
		pending := mustAdmit(t, s, "192.0.2.1")
		authenticated := mustAdmit(t, s, "192.0.2.2")
		if !s.promoteConnection(authenticated) {
			t.Fatal("promotion failed")
		}
		canceled := make(chan struct{})
		go func() { <-pending.ctx.Done(); close(canceled) }()
		time.Sleep(s.config.HandshakeTimeout)
		synctest.Wait()
		select {
		case <-canceled:
		default:
			t.Fatal("expiration did not cancel authentication context")
		}
		if !pending.Conn.(*admissionTestConn).closed.Load() {
			t.Fatal("expired socket not closed")
		}
		if authenticated.ctx.Err() != nil || authenticated.Conn.(*admissionTestConn).closed.Load() {
			t.Fatal("pending timeout closed authenticated connection")
		}
		if s.promoteConnection(pending) {
			t.Fatal("expired connection promoted")
		}
		assertAdmissionProtocolCounts(t, s, 0, 1)
		_ = authenticated.Close()
	})
}

func TestAdmissionConcurrentPromotionExpirationAndClose(t *testing.T) {
	t.Parallel()
	s := admissionTestServer(t, func(c *Config) {
		c.ConnectionRateLimit, c.ConnectionRateLimitPerIP = 100000, 100000
	})
	for range 100 {
		conn := mustAdmit(t, s, "192.0.2.1")
		var workers sync.WaitGroup
		workers.Go(func() { s.promoteConnection(conn) })
		workers.Go(func() { s.expireConnection(conn) })
		workers.Go(func() { _ = conn.Close() })
		workers.Wait()
		assertAdmissionProtocolCounts(t, s, 0, 0)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.connections) != 0 {
		t.Fatal("tracked connections leaked")
	}
	for _, peer := range s.admission.peers {
		if peer.total != 0 || peer.pending != 0 {
			t.Fatal("source counters leaked")
		}
	}
}

func TestAdmissionPeerStateBoundedAndReclaimed(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := admissionTestServer(t, nil)
		active := mustAdmit(t, s, "192.0.2.1")
		if !s.promoteConnection(active) {
			t.Fatal("promotion failed")
		}
		s.mu.Lock()
		for i := 1; i < maxAdmissionPeers; i++ {
			ip := netip.AddrFrom4([4]byte{10, 0, byte(i >> 8), byte(i)})
			s.admission.peers[ip] = &connectionPeer{lastSeen: time.Now()}
		}
		s.mu.Unlock()
		if s.admitConnection(t.Context(), admissionRaw("198.51.100.1")) != nil {
			t.Fatal("peer state table exceeded capacity")
		}
		time.Sleep(admissionPeerTTL)
		fresh := mustAdmit(t, s, "198.51.100.1")
		s.mu.Lock()
		count := len(s.admission.peers)
		s.mu.Unlock()
		if count != 2 {
			t.Fatalf("peer states = %d, want active source plus new source", count)
		}
		_ = active.Close()
		_ = fresh.Close()
	})
}

func TestAdmissionIPv6SourceGrouping(t *testing.T) {
	t.Parallel()
	s := admissionTestServer(t, func(c *Config) { c.MaxHandshakesPerIP = 1 })
	mustAdmit(t, s, "2001:db8:1:2::1")
	if s.admitConnection(t.Context(), admissionRaw("2001:db8:1:2::ffff")) != nil {
		t.Fatal("IPv6 rotation bypassed source limit")
	}
	mustAdmit(t, s, "2001:db8:1:3::1")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if s.admitConnection(ctx, admissionRaw("192.0.2.1")) != nil {
		t.Fatal("canceled server accepted connection")
	}
}

func TestConnectionAdmissionConfigValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		set  func(*Config)
	}{
		{"negative handshakes", func(c *Config) { c.MaxHandshakes = -1 }},
		{"excessive handshakes", func(c *Config) { c.MaxHandshakes = 4097 }},
		{"negative connections per IP", func(c *Config) { c.MaxConnectionsPerIP = -1 }},
		{"excessive connections per IP", func(c *Config) { c.MaxConnectionsPerIP = 4097 }},
		{"negative handshakes per IP", func(c *Config) { c.MaxHandshakesPerIP = -1 }},
		{"excessive handshakes per IP", func(c *Config) { c.MaxHandshakesPerIP = 4097 }},
		{"negative global rate", func(c *Config) { c.ConnectionRateLimit = -1 }},
		{"excessive global rate", func(c *Config) { c.ConnectionRateLimit = 100001 }},
		{"negative per-IP rate", func(c *Config) { c.ConnectionRateLimitPerIP = -1 }},
		{"excessive per-IP rate", func(c *Config) { c.ConnectionRateLimitPerIP = 100001 }},
		{"negative timeout", func(c *Config) { c.HandshakeTimeout = -time.Second }},
		{"short timeout", func(c *Config) { c.HandshakeTimeout = time.Millisecond }},
		{"long timeout", func(c *Config) { c.HandshakeTimeout = time.Minute + 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			var config Config
			test.set(&config)
			if err := validateConnectionConfig(&config); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid admission setting returned %v", err)
			}
		})
	}
}

func TestAdmissionHTTPContextPreservesValuesAndCancellation(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		s := admissionTestServer(t, nil)
		conn := mustAdmit(t, s, "192.0.2.1")
		key := struct{ name string }{"http-base-context-value"}
		base := context.WithValue(t.Context(), key, "preserved")
		ctx := connectionContext(base, conn)
		if got := ctx.Value(key); got != "preserved" {
			t.Fatalf("HTTP base context value = %v", got)
		}
		_ = conn.Close()
		synctest.Wait()
		if !errors.Is(ctx.Err(), context.Canceled) {
			t.Fatalf("connection closure did not cancel HTTP context: %v", ctx.Err())
		}
	})
}
