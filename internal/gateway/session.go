package gateway

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync"
	"time"
)

const (
	sessionCookie = "__Host-devbox-kvm"
	loginCookie   = "__Host-devbox-kvm-login"
	connectWindow = 2 * time.Minute
)

type browserSession struct {
	owner, login, clientIP, csrf, passwordHash string
	IsAdmin                                    bool
	expires                                    time.Time
	connections                                map[*connectionLease]struct{}
}

type connectionLease struct{ cancel context.CancelFunc }

type connectGrant struct {
	session [sha256.Size]byte
	vm      string
	expires time.Time
}

type loginWindow struct {
	start time.Time
	count int
}

type sessionStore struct {
	mu       sync.Mutex
	sessions map[[sha256.Size]byte]*browserSession
	grants   map[[sha256.Size]byte]connectGrant
	attempts map[string]loginWindow
}

func newSessionStore() sessionStore {
	return sessionStore{
		sessions: make(
			map[[sha256.Size]byte]*browserSession,
		),
		grants: make(
			map[[sha256.Size]byte]connectGrant,
		),
		attempts: make(
			map[string]loginWindow,
		),
	}
}

func randomToken() (string, error) {
	var b [32]byte
	if _, err := rand.Read(b[:]); err != nil {
		return "", err
	}

	return hex.EncodeToString(b[:]), nil
}

func tokenKey(raw string) ([sha256.Size]byte, bool) {
	if len(raw) != 64 {
		return [sha256.Size]byte{}, false
	}
	for _, c := range raw {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return [sha256.Size]byte{}, false
		}
	}

	return sha256.Sum256([]byte(raw)), true
}

func clientIP(address string) string {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return ""
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || ip.Zone() != "" {
		return ""
	}

	return ip.Unmap().String()
}

func (s *Server) newSession(owner, ip, hash string) (string, error) {
	return s.newIdentitySession(Identity{Username: owner}, ip, hash)
}

func (s *Server) newIdentitySession(identity Identity, ip, hash string) (string, error) {
	return s.newLoginSession(identity, identity.Username, ip, hash)
}

// newLoginSession retains the authenticated login name separately from the
// directory's canonical VM owner, which need not be a valid LDAP bind identifier.
func (s *Server) newLoginSession(identity Identity, login, ip, hash string) (string, error) {
	if login == "" || len(login) > 128 || strings.ContainsAny(login, "\r\n\x00") {
		return "", ErrInvalid
	}
	raw, err := randomToken()
	if err != nil {
		return "", err
	}
	csrf, err := randomToken()
	if err != nil {
		return "", err
	}
	key, _ := tokenKey(raw)
	s.store.mu.Lock()
	defer s.store.mu.Unlock()
	s.store.pruneLocked(time.Now())
	if len(s.store.sessions) >= s.config.MaxSessions {
		return "", fmt.Errorf("%w: too many active sessions", ErrConflict)
	}
	s.store.sessions[key] = &browserSession{
		owner:        identity.Username,
		login:        login,
		IsAdmin:      identity.IsAdmin,
		clientIP:     ip,
		passwordHash: hash,
		csrf:         csrf,
		expires: time.Now().Add(
			s.config.SessionTTL,
		),
		connections: make(
			map[*connectionLease]struct{},
		),
	}

	return raw, nil
}

func (s *sessionStore) session(raw, ip string) ([sha256.Size]byte, browserSession, bool) {
	key, ok := tokenKey(raw)
	if !ok || ip == "" {
		return key, browserSession{}, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	sess := s.sessions[key]
	if sess == nil || sess.clientIP != ip {
		return key, browserSession{}, false
	}
	if !time.Now().Before(sess.expires) {
		s.revokeLocked(key)

		return key, browserSession{}, false
	}
	copy := *sess
	copy.connections = nil

	return key, copy, true
}

func (s *sessionStore) revokeLocked(key [sha256.Size]byte) {
	if sess := s.sessions[key]; sess != nil {
		for lease := range sess.connections {
			lease.cancel()
		}
		sess.passwordHash = ""
		delete(s.sessions, key)
	}
	for verifier, grant := range s.grants {
		if grant.session == key {
			delete(s.grants, verifier)
		}
	}
}

func (s *sessionStore) revoke(key [sha256.Size]byte) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.revokeLocked(key)
}

func (s *sessionStore) pruneLocked(now time.Time) {
	for key, sess := range s.sessions {
		if !now.Before(sess.expires) {
			s.revokeLocked(key)
		}
	}
	for key, grant := range s.grants {
		if !now.Before(grant.expires) {
			delete(s.grants, key)
		}
	}
	for ip, window := range s.attempts {
		if now.Sub(window.start) >= time.Minute {
			delete(s.attempts, ip)
		}
	}
}

func (s *sessionStore) allowLogin(ip string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneLocked(now)
	window, exists := s.attempts[ip]
	if ip == "" || (!exists && len(s.attempts) >= 4096) {
		return false
	}
	if !exists {
		window.start = now
	}
	window.count++
	s.attempts[ip] = window

	return window.count <= 10
}

func (s *sessionStore) grant(key [sha256.Size]byte, vm string) (string, error) {
	raw, err := randomToken()
	if err != nil {
		return "", err
	}
	verifier, _ := tokenKey(raw)
	s.mu.Lock()
	defer s.mu.Unlock()
	now := time.Now()
	s.pruneLocked(now)
	sess := s.sessions[key]
	if sess == nil {
		return "", ErrAuthentication
	}
	count := 0
	for existing, grant := range s.grants {
		if grant.session != key {
			continue
		}
		if grant.vm == vm {
			delete(s.grants, existing)
		} else {
			count++
		}
	}
	if count >= 32 {
		return "", ErrConflict
	}
	expires := minTime(now.Add(connectWindow), sess.expires)
	s.grants[verifier] = connectGrant{session: key, vm: vm, expires: expires}

	return raw, nil
}

// pendingGrant registers session cancellation before backend lookup. A failed
// password check must not spend the token; claimGrant spends it after authentication.
func (s *sessionStore) pendingGrant(
	ctx context.Context, raw, ip string,
) (context.Context, string, string, func(), bool) {
	verifier, ok := tokenKey(raw)
	if !ok || ip == "" {
		return nil, "", "", nil, false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	grant, ok := s.grants[verifier]
	if !ok {
		return nil, "", "", nil, false
	}
	sess := s.sessions[grant.session]
	if sess == nil || sess.clientIP != ip {
		return nil, "", "", nil, false
	}
	connCtx, cancel := context.WithDeadline(ctx, sess.expires)
	lease := &connectionLease{cancel: cancel}
	sess.connections[lease] = struct{}{}
	release := func() {
		cancel()
		s.mu.Lock()
		delete(sess.connections, lease)
		s.mu.Unlock()
	}

	return connCtx, sess.owner, grant.vm, release, true
}

// claimGrant atomically consumes a still-valid grant after password validation.
// Concurrent connections can authenticate, but only one can enter the desktop.
func (s *sessionStore) claimGrant(raw, ip string) bool {
	verifier, ok := tokenKey(raw)
	if !ok || ip == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pruneLocked(time.Now())
	grant, ok := s.grants[verifier]
	if !ok {
		return false
	}
	sess := s.sessions[grant.session]
	if sess == nil || sess.clientIP != ip {
		return false
	}
	delete(s.grants, verifier)

	return true
}

func minTime(a, b time.Time) time.Time {
	if a.Before(b) {
		return a
	}

	return b
}
