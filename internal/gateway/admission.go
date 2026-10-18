package gateway

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"sync"
	"time"
)

const (
	maxAdmissionPeers = 4096
	admissionPeerTTL  = time.Minute
)

// All admission and tracked-connection state is protected by Server.mu.
type connectionAdmission struct {
	pending, authenticated int
	peers                  map[netip.Addr]*connectionPeer
	global                 connectionBucket
	nextPrune              time.Time
}

type connectionPeer struct {
	pending, total int
	lastSeen       time.Time
	bucket         connectionBucket
}

// Bursts are bounded to one second of the configured connection rate.
type connectionBucket struct {
	tokens float64
	last   time.Time
}

func (b *connectionBucket) available(now time.Time, rate int) bool {
	if b.last.IsZero() {
		b.tokens = float64(rate)
	} else {
		b.tokens = min(float64(rate), b.tokens+max(0, now.Sub(b.last).Seconds())*float64(rate))
	}
	b.last = now

	return b.tokens >= 1
}

func validateConnectionConfig(c *Config) error {
	for _, limit := range []struct {
		value         *int
		fallback, max int
	}{
		{&c.MaxHandshakes, 64, 4096},
		{&c.MaxConnectionsPerIP, 32, 4096},
		{&c.MaxHandshakesPerIP, 8, 4096},
		{&c.ConnectionRateLimit, 128, 100000},
		{&c.ConnectionRateLimitPerIP, 8, 100000},
	} {
		if *limit.value == 0 {
			*limit.value = limit.fallback
		}
		if *limit.value < 1 || *limit.value > limit.max {
			return fmt.Errorf("%w: invalid connection admission limit", ErrInvalid)
		}
	}
	if c.HandshakeTimeout == 0 {
		c.HandshakeTimeout = 10 * time.Second
	}
	if c.HandshakeTimeout < time.Second || c.HandshakeTimeout > time.Minute {
		return fmt.Errorf("%w: handshake timeout must be one second to one minute", ErrInvalid)
	}

	return nil
}

// admissionIP uses the socket peer, never a forwarding header. Group IPv6 /64s
// so rotating addresses on the same network cannot evade the source limits.
func admissionIP(addr net.Addr) (netip.Addr, bool) {
	peer, err := netip.ParseAddrPort(addr.String())
	if err != nil {
		return netip.Addr{}, false
	}
	ip := peer.Addr().Unmap().WithZone("")
	if ip.Is6() {
		ip = netip.PrefixFrom(ip, 64).Masked().Addr()
	}

	return ip, true
}

func (s *Server) pruneAdmissionLocked(now time.Time) {
	for ip, peer := range s.admission.peers {
		if peer.total == 0 && now.Sub(peer.lastSeen) >= admissionPeerTTL {
			delete(s.admission.peers, ip)
		}
	}
	s.admission.nextPrune = now.Add(admissionPeerTTL)
}

// admitConnection runs before buffers, TLS state, or a worker are allocated.
// Rejected sources do not spend the global allowance.
func (s *Server) admitConnection(ctx context.Context, raw net.Conn) *trackedConn {
	ip, ok := admissionIP(raw.RemoteAddr())
	if !ok {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining || ctx.Err() != nil || s.admission.pending >= s.config.MaxHandshakes {
		return nil
	}
	now := time.Now()
	if !now.Before(s.admission.nextPrune) {
		s.pruneAdmissionLocked(now)
	}
	peer := s.admission.peers[ip]
	if peer == nil {
		if len(s.admission.peers) >= maxAdmissionPeers {
			return nil
		}
		peer = &connectionPeer{}
	}
	if peer.total >= s.config.MaxConnectionsPerIP ||
		peer.pending >= s.config.MaxHandshakesPerIP ||
		!peer.bucket.available(now, s.config.ConnectionRateLimitPerIP) ||
		!s.admission.global.available(now, s.config.ConnectionRateLimit) {
		return nil
	}
	peer.bucket.tokens--
	s.admission.global.tokens--
	peer.total++
	peer.pending++
	peer.lastSeen = now
	if s.admission.peers == nil {
		s.admission.peers = make(map[netip.Addr]*connectionPeer)
	}
	s.admission.peers[ip] = peer
	s.admission.pending++
	connCtx, cancel := context.WithCancel(ctx)
	conn := &trackedConn{
		Conn: raw, server: s, ip: ip, ctx: connCtx, cancel: cancel,
		deadline: now.Add(s.config.HandshakeTimeout),
	}
	s.connections[conn] = struct{}{}
	conn.timer = time.AfterFunc(time.Until(conn.deadline), func() { s.expireConnection(conn) })

	return conn
}

func (s *Server) expireConnection(conn *trackedConn) {
	s.mu.Lock()
	_, exists := s.connections[conn]
	expire := exists && !conn.authenticated
	if expire {
		conn.expired = true
	}
	s.mu.Unlock()
	if expire {
		_ = conn.Close()
	}
}

// Promotion reserves authenticated capacity without borrowing from the pending
// pool. In particular, completing TLS is not sufficient to call this method.
func (s *Server) promoteConnection(conn *trackedConn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.connections[conn]; !exists || conn.expired || s.draining || conn.ctx.Err() != nil {
		return false
	}
	if conn.authenticated {
		return true
	}
	if !time.Now().Before(conn.deadline) || s.admission.authenticated >= s.config.MaxConnections {
		return false
	}
	conn.authenticated = true
	conn.timer.Stop()
	s.admission.pending--
	s.admission.authenticated++
	s.admission.peers[conn.ip].pending--

	return true
}

type trackedConn struct {
	net.Conn
	server        *Server
	ip            netip.Addr
	ctx           context.Context //nolint:containedctx // Its lifetime is deliberately tied to the tracked socket.
	cancel        context.CancelFunc
	once          sync.Once
	timer         *time.Timer
	deadline      time.Time
	authenticated bool
	expired       bool
}

func (c *trackedConn) Close() error {
	// Cancel backend/authentication work as well as socket I/O. Do neither
	// while holding the admission lock.
	c.cancel()
	err := c.Conn.Close()
	c.once.Do(func() {
		s := c.server
		s.mu.Lock()
		defer s.mu.Unlock()
		c.timer.Stop()
		delete(s.connections, c)
		peer := s.admission.peers[c.ip]
		peer.total--
		peer.lastSeen = time.Now()
		if c.authenticated {
			s.admission.authenticated--
		} else {
			s.admission.pending--
			peer.pending--
		}
	})

	return err
}

type connectionContextKey struct{}

func trackedConnection(conn net.Conn) *trackedConn {
	switch conn := conn.(type) {
	case *trackedConn:
		return conn
	case *tls.Conn:
		return trackedConnection(conn.NetConn())
	case *bufferedConn:
		return trackedConnection(conn.Conn)
	default:
		return nil
	}
}

func connectionContext(ctx context.Context, raw net.Conn) context.Context {
	if conn := trackedConnection(raw); conn != nil {
		// Preserve net/http's context values while canceling in-flight handlers
		// immediately on admission expiry or socket closure.
		ctx, cancel := context.WithCancel(ctx)
		context.AfterFunc(conn.ctx, cancel)

		return context.WithValue(ctx, connectionContextKey{}, conn)
	}

	return ctx
}

func (s *Server) promoteHTTPConnection(r *http.Request) bool {
	conn, ok := r.Context().Value(connectionContextKey{}).(*trackedConn)
	// Standalone HTTP handlers (including httptest) have no listener admission.
	return !ok || s.promoteConnection(conn)
}

func (s *Server) httpConnectionAuthenticated(r *http.Request) bool {
	conn, ok := r.Context().Value(connectionContextKey{}).(*trackedConn)
	if !ok {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	return conn.authenticated
}

func connectionBusy(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Connection", "close")
	r.Close = true
	http.Error(w, "Gateway connection capacity reached. Please try again shortly.", http.StatusServiceUnavailable)
}
