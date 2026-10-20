package gateway

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/tredoe/osutil/user/crypt/sha512_crypt"
)

type testAuthenticator func(context.Context, string, string) (string, error)

func (f testAuthenticator) Authenticate(ctx context.Context, user, pass string) (string, error) {
	return f(ctx, user, pass)
}

type webBackend struct {
	mu                  sync.Mutex
	vms                 []VM
	endpoint            Endpoint
	listErr             error
	calls               []string
	owner, passwordHash string
}

func (b *webBackend) List(context.Context, string) ([]VM, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	return append([]VM(nil), b.vms...), b.listErr
}

func (b *webBackend) Create(_ context.Context, owner, name, hash string) (VM, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.owner, b.passwordHash = owner, hash
	b.calls = append(b.calls, "create")
	vm := VM{ID: "new", Name: name, Owner: owner, State: "stopped"}
	b.vms = append(b.vms, vm)

	return vm, nil
}

func (b *webBackend) action(owner, id, action string) error {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.calls = append(b.calls, action+":"+owner+":"+id)

	return nil
}

func (b *webBackend) Start(_ context.Context, owner, id string) error {
	return b.action(owner, id, "start")
}

func (b *webBackend) Stop(_ context.Context, owner, id string) error {
	return b.action(owner, id, "stop")
}

func (b *webBackend) Delete(_ context.Context, owner, id string) error {
	return b.action(owner, id, "delete")
}

func (b *webBackend) RDP(_ context.Context, owner, id string) (Endpoint, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	owned := false
	for _, vm := range b.vms {
		if vm.ID == id && vm.Owner == owner {
			owned = true

			break
		}
	}
	if !owned {
		return Endpoint{}, ErrNotFound
	}
	b.calls = append(b.calls, "rdp:"+owner+":"+id)

	return b.endpoint, nil
}

func webFixture(t *testing.T) (*Server, *webBackend, string, browserSession) {
	t.Helper()
	backend := &webBackend{
		vms: []VM{
			{
				ID:    "mine",
				Name:  "my-work",
				Owner: "alice",
				State: "running",
			},
			{
				ID:    "foreign",
				Name:  "secret-work",
				Owner: "bob",
				State: "running",
			},
		},
	}
	s := &Server{
		config: Config{
			PublicAddress: "gateway.test:443",
			SessionTTL:    time.Hour,
			MaxSessions:   16,
			Backend:       backend,
			Authenticator: testAuthenticator(
				func(
					_ context.Context,
					u, p string,
				) (string, error) {
					if u == "alice" && p == "test-password" {
						return "alice", nil
					}

					return "", ErrAuthentication
				},
			),
		},
		store:     newSessionStore(),
		authSlots: make(chan struct{}, 8),
	}
	raw, err := s.newSession("alice", "127.0.0.1", "$6$rounds=100000$test$hash")
	if err != nil {
		t.Fatal(err)
	}
	_, sess, ok := s.store.session(raw, "127.0.0.1")
	if !ok {
		t.Fatal("missing session")
	}

	return s, backend, raw, sess
}

func webRequest(s *Server, method, path, raw string, form url.Values) *httptest.ResponseRecorder {
	r := httptest.NewRequest(method, "https://gateway.test"+path, strings.NewReader(form.Encode()))
	r.RemoteAddr = "127.0.0.1:45678"
	if raw != "" {
		r.AddCookie(
			&http.Cookie{
				Name:     sessionCookie,
				Value:    raw,
				Secure:   true,
				HttpOnly: true,
				SameSite: http.SameSiteStrictMode,
			},
		)
	}
	if method == http.MethodPost {
		r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		r.Header.Set("Origin", "https://gateway.test")
	}
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)

	return w
}

func TestDashboardRequiresSessionAndFiltersOwners(t *testing.T) {
	t.Parallel()
	s, _, raw, _ := webFixture(t)
	if w := webRequest(s, http.MethodGet, "/", "", nil); w.Code != 303 || w.Header().Get("Location") != "/login" {
		t.Fatalf("anonymous dashboard: %d %s", w.Code, w.Body.String())
	}
	w := webRequest(s, http.MethodGet, "/", raw, nil)
	if w.Code != 200 ||
		!strings.Contains(
			w.Body.String(),
			"my-work",
		) ||
		strings.Contains(
			w.Body.String(),
			"secret-work",
		) {
		t.Fatalf("owner filtering: %d %s", w.Code, w.Body.String())
	}
	for _, h := range []string{"Content-Security-Policy", "Strict-Transport-Security", "X-Content-Type-Options"} {
		if w.Header().Get(h) == "" {
			t.Errorf("missing %s", h)
		}
	}
}

func TestVMOperationsRequireCSRFAndOwnership(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"start", "stop", "delete", "rdp"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			s, b, raw, sess := webFixture(t)
			for _, id := range []string{"mine", "foreign"} {
				w := webRequest(s, http.MethodPost, "/vms/"+id+"/"+action, raw, url.Values{"csrf": {"bad"}})
				if w.Code != 403 {
					t.Fatalf("missing CSRF: %d", w.Code)
				}
			}
			w := webRequest(s, http.MethodPost, "/vms/foreign/"+action, raw, url.Values{"csrf": {sess.csrf}})
			if w.Code != 404 || len(b.calls) != 0 {
				t.Fatalf("foreign VM allowed: %d, calls=%v", w.Code, b.calls)
			}
			w = webRequest(s, http.MethodPost, "/vms/mine/"+action, raw, url.Values{"csrf": {sess.csrf}})
			want := 303
			if action == "rdp" {
				want = 200
			}
			if w.Code != want {
				t.Fatalf("own VM failed: %d %s", w.Code, w.Body.String())
			}
		})
	}
}

func TestOriginAndSessionIPCannotBeSpoofed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct{ name, origin, remote, host string }{
		{
			"cross-origin",
			"https://evil.test",
			"127.0.0.1:1",
			"gateway.test",
		},
		{
			"missing-origin",
			"",
			"127.0.0.1:1",
			"gateway.test",
		},
		{
			"opaque-origin",
			"null",
			"127.0.0.1:1",
			"gateway.test",
		},
		{
			"different-ip",
			"https://gateway.test",
			"127.0.0.2:1",
			"gateway.test",
		},
		{
			"wrong-host",
			"https://gateway.test",
			"127.0.0.1:1",
			"evil.test",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			s, b, raw, sess := webFixture(t)
			r := httptest.NewRequest(
				http.MethodPost,
				"https://"+tc.host+"/vms/mine/stop",
				strings.NewReader(
					url.Values{
						"csrf": {sess.csrf},
					}.Encode(),
				),
			)
			r.RemoteAddr = tc.remote
			r.Header.Set("Origin", tc.origin)
			r.Header.Set("X-Forwarded-For", "127.0.0.1")
			r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
			r.AddCookie(
				&http.Cookie{
					Name:     sessionCookie,
					Value:    raw,
					Secure:   true,
					HttpOnly: true,
					SameSite: http.SameSiteStrictMode,
				},
			)
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, r)
			if len(b.calls) != 0 || w.Code == 200 {
				t.Fatalf("request accepted: %d %v", w.Code, b.calls)
			}
		})
	}
}

func TestLoginHashesPasswordAndIssuesSecureSession(t *testing.T) {
	t.Parallel()
	s, b, _, _ := webFixture(t)
	s.config.Authenticator = testAuthenticator(func(_ context.Context, username, password string) (string, error) {
		if username == "alice.login" && password == "test-password" {
			return "alice", nil
		}

		return "", ErrAuthentication
	})
	page := webRequest(s, http.MethodGet, "/login", "", nil)
	var challenge *http.Cookie
	for _, c := range page.Result().Cookies() {
		if c.Name == "__Host-devbox-kvm-login" {
			challenge = c
		}
	}
	if challenge == nil {
		t.Fatal("no login CSRF cookie")
	}
	r := httptest.NewRequest(
		http.MethodPost,
		"https://gateway.test/login",
		strings.NewReader(
			url.Values{
				"csrf":     {challenge.Value},
				"username": {"alice.login"},
				"password": {"test-password"},
			}.Encode(),
		),
	)
	r.RemoteAddr = "127.0.0.1:4444"
	r.Header.Set("Origin", "https://gateway.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(challenge)
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 303 {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	var cookie *http.Cookie
	for _, c := range w.Result().Cookies() {
		if c.Name == "__Host-devbox-kvm" {
			cookie = c
		}
	}
	if cookie == nil ||
		!cookie.Secure ||
		!cookie.HttpOnly ||
		cookie.SameSite != http.SameSiteStrictMode ||
		cookie.Path != "/" {
		t.Fatalf("insecure cookie: %+v", cookie)
	}
	_, sess, ok := s.store.session(cookie.Value, "127.0.0.1")
	if !ok || sess.owner != "alice" || sess.login != "alice.login" {
		t.Fatal("authenticated session lost the LDAP login or canonical owner")
	}
	rdp := webRequest(s, http.MethodPost, "/vms/mine/rdp", cookie.Value, url.Values{"csrf": {sess.csrf}})
	if rdp.Code != http.StatusOK || !strings.Contains(rdp.Body.String(), "username:s:alice.login\r\n") {
		t.Fatal("download did not retain the authenticated LDAP login name")
	}
	if !strings.HasPrefix(
		sess.passwordHash,
		"$6$rounds=100000$",
	) ||
		sha512_crypt.New().Verify(
			sess.passwordHash,
			[]byte(
				"test-password",
			),
		) != nil {
		t.Fatal("guest password hash missing or incorrect")
	}
	created := webRequest(
		s,
		http.MethodPost,
		"/vms",
		cookie.Value,
		url.Values{
			"csrf":  {sess.csrf},
			"name":  {"new-work"},
			"owner": {"bob"},
		},
	)
	if created.Code != 303 || b.owner != "alice" || b.passwordHash != sess.passwordHash {
		t.Fatalf("incorrect create identity/hash: code=%d owner=%s", created.Code, b.owner)
	}
	if strings.Contains(w.Body.String(), "test-password") || r.PostForm.Get("password") != "" {
		t.Fatal("password retained in response or form")
	}
}

func TestLoginRejectsCSRFAndEmptyPassword(t *testing.T) {
	t.Parallel()
	s, backend, raw, session := webFixture(t)
	if w := webRequest(
		s,
		http.MethodPost,
		"/login",
		"",
		url.Values{
			"username": {"alice"},
			"password": {"test-password"},
		},
	); w.Code != 403 {
		t.Fatalf("login CSRF accepted: %d", w.Code)
	}
	challenge, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	r := httptest.NewRequest(
		http.MethodPost,
		"https://gateway.test/login",
		strings.NewReader(
			url.Values{
				"csrf":     {challenge},
				"username": {"alice"},
				"password": {""},
			}.Encode(),
		),
	)
	r.RemoteAddr = "127.0.0.1:1"
	r.Header.Set("Origin", "https://gateway.test")
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	r.AddCookie(&http.Cookie{
		Name: loginCookie, Value: challenge, Secure: true,
		HttpOnly: true, SameSite: http.SameSiteStrictMode,
	})
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, r)
	if w.Code != 401 {
		t.Fatalf("empty password accepted: %d", w.Code)
	}
	_, existing, ok := s.store.session(raw, "127.0.0.1")
	if len(backend.calls) != 0 || !ok || existing.csrf != session.csrf {
		t.Fatal("rejected login changed an existing session or called the backend")
	}
}

func TestDownloadedRDPEnablesPlaybackOnly(t *testing.T) {
	t.Parallel()
	s, _, raw, sess := webFixture(t)
	w := webRequest(s, http.MethodPost, "/vms/mine/rdp", raw, url.Values{"csrf": {sess.csrf}})
	if w.Code != http.StatusOK {
		t.Fatalf("download failed: %d", w.Code)
	}
	for _, setting := range []string{"audiomode:i:0", "audiocapturemode:i:0"} {
		if !strings.Contains(w.Body.String(), setting+"\r\n") {
			t.Errorf("RDP file is missing %q", setting)
		}
	}
}

func TestDownloadedTokenSingleUseIPBoundAndLogoutRevocable(t *testing.T) {
	t.Parallel()
	s, _, raw, sess := webFixture(t)
	w := webRequest(s, http.MethodPost, "/vms/mine/rdp", raw, url.Values{"csrf": {sess.csrf}})
	if w.Code != 200 {
		t.Fatalf("download failed: %d", w.Code)
	}
	_, rest, ok := strings.Cut(w.Body.String(), "loadbalanceinfo:s:")
	if !ok {
		t.Fatal("no routing token")
	}
	token, _, _ := strings.Cut(rest, "\r\n")
	if !strings.Contains(w.Body.String(), "username:s:alice\r\n") ||
		!strings.Contains(w.Body.String(), "prompt for credentials:i:1\r\n") ||
		!strings.Contains(w.Body.String(), "prompt for credentials on client:i:1\r\n") {
		t.Fatal("RDP file must contain the login name and request credentials")
	}
	if strings.Contains(
		w.Body.String(),
		"password",
	) ||
		!strings.Contains(
			w.Body.String(),
			"enablecredsspsupport:i:0",
		) ||
		!strings.Contains(
			w.Body.String(),
			"full address:s:gateway.test:443",
		) {
		t.Fatal("invalid RDP file")
	}
	if _, _, _, _, ok := s.store.pendingGrant(context.Background(), token, "127.0.0.2"); ok {
		t.Fatal("different IP consumed token")
	}
	ctx, owner, vm, release, ok := s.store.pendingGrant(context.Background(), token, "127.0.0.1")
	if !ok || owner != "alice" || vm != "mine" {
		t.Fatal("valid grant rejected")
	}
	defer release()
	if s.store.claimGrant(token, "127.0.0.2") {
		t.Fatal("different IP claimed token")
	}
	if !s.store.claimGrant(token, "127.0.0.1") {
		t.Fatal("valid token could not be claimed")
	}
	if _, _, _, _, ok := s.store.pendingGrant(context.Background(), token, "127.0.0.1"); ok {
		t.Fatal("token replay accepted")
	}
	key, _, _ := s.store.session(raw, "127.0.0.1")
	fresh, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	out := webRequest(s, http.MethodPost, "/logout", raw, url.Values{"csrf": {sess.csrf}})
	if out.Code != 303 {
		t.Fatalf("logout failed: %d", out.Code)
	}
	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("logout did not cancel active RDP")
	}
	if _, _, _, _, ok := s.store.pendingGrant(context.Background(), fresh, "127.0.0.1"); ok {
		t.Fatal("logout retained token")
	}
	if _, _, ok := s.store.session(raw, "127.0.0.1"); ok {
		t.Fatal("logout retained browser session")
	}
}

func TestSessionLimitsExpiryAndTokenWindow(t *testing.T) {
	t.Parallel()
	s, _, raw, _ := webFixture(t)
	s.config.MaxSessions = 1
	if _, err := s.newSession("bob", "127.0.0.2", "hash"); !errors.Is(err, ErrConflict) {
		t.Fatalf("session limit ignored: %v", err)
	}
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	verifier, _ := tokenKey(token)
	s.store.mu.Lock()
	grant := s.store.grants[verifier]
	remaining := time.Until(grant.expires)
	grant.expires = time.Now().Add(-time.Second)
	s.store.grants[verifier] = grant
	s.store.mu.Unlock()
	if remaining <= 119*time.Second || remaining > connectWindow {
		t.Fatalf("incorrect grant window: %s", remaining)
	}
	if _, _, _, _, ok := s.store.pendingGrant(context.Background(), token, "127.0.0.1"); ok {
		t.Fatal("expired token accepted")
	}
	s.store.mu.Lock()
	s.store.sessions[key].expires = time.Now().Add(-time.Second)
	s.store.mu.Unlock()
	if _, _, ok := s.store.session(raw, "127.0.0.1"); ok {
		t.Fatal("expired session accepted")
	}
	if _, err := s.newSession("bob", "127.0.0.2", "hash"); err != nil {
		t.Fatal("expired session did not release capacity")
	}
}

func testTLSIdentity(t *testing.T, name string) (tls.Certificate, []byte, string, string) {
	t.Helper()
	pub, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	cert := &x509.Certificate{
		SerialNumber: big.NewInt(
			1,
		),
		Subject: pkix.Name{
			CommonName: name,
		},
		DNSNames: []string{
			name,
		},
		NotBefore: time.Now().Add(
			-time.Hour,
		),
		NotAfter: time.Now().Add(
			time.Hour,
		),
		KeyUsage: x509.KeyUsageDigitalSignature | x509.KeyUsageCertSign,
		ExtKeyUsage: []x509.ExtKeyUsage{
			x509.ExtKeyUsageServerAuth,
		},
		BasicConstraintsValid: true,
		IsCA:                  true,
	}
	der, err := x509.CreateCertificate(rand.Reader, cert, cert, pub, key)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
	keyPEM := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
	dir := t.TempDir()
	certFile, keyFile := filepath.Join(dir, "cert.pem"), filepath.Join(dir, "key.pem")
	if err := os.WriteFile(certFile, certPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(keyFile, keyPEM, 0o600); err != nil {
		t.Fatal(err)
	}
	identity, err := tls.X509KeyPair(certPEM, keyPEM)
	if err != nil {
		t.Fatal(err)
	}

	return identity, certPEM, certFile, keyFile
}

// Normal browser form POSTs serialize Origin as null under no-referrer, so
// preserving same-origin referrers is required by the strict Origin guard.
func TestGatewayFormPagesPreserveBrowserOrigin(t *testing.T) {
	t.Parallel()
	for _, path := range []string{"/login", "/"} {
		t.Run(path, func(t *testing.T) {
			t.Parallel()
			s, _, session, _ := webFixture(t)
			if path == "/login" {
				session = ""
			}
			page := webRequest(s, http.MethodGet, path, session, nil)
			if page.Code != http.StatusOK {
				t.Fatalf("form page returned %d", page.Code)
			}
			if policy := page.Header().Get("Referrer-Policy"); policy != "same-origin" {
				t.Fatalf("form page policy %q must preserve same-origin POST Origin and suppress cross-origin referrers", policy)
			}
		})
	}
}
