package gateway

import (
	"net/http"
	"strings"
	"testing"
)

func TestDashboardResourcesHaveRestrictedTypesAndPolicy(t *testing.T) {
	t.Parallel()
	s := &Server{config: Config{PublicAddress: "gateway.test:443"}}
	for _, tc := range []struct{ path, contentType string }{
		{"/static/theme.css", "text/css"},
		{"/static/dashboard.css", "text/css"},
		{"/static/gokvm.css", "text/css"},
		{"/static/dashboard.js", "text/javascript"},
		{"/static/vendor/bootstrap/5.3.2/bootstrap.min.css", "text/css"},
		{"/static/vendor/bootstrap-icons/1.11.3/fonts/bootstrap-icons.woff2", "font/woff2"},
	} {
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()
			result := webRequest(s, http.MethodGet, tc.path, "", nil)
			if result.Code != http.StatusOK || result.Body.Len() == 0 ||
				!strings.HasPrefix(result.Header().Get("Content-Type"), tc.contentType) {
				t.Fatalf("resource returned %d, %s", result.Code, result.Header().Get("Content-Type"))
			}
			policy := result.Header().Get("Content-Security-Policy")
			if strings.Contains(policy, "unsafe-") ||
				!strings.Contains(policy, "script-src 'self'") ||
				!strings.Contains(policy, "font-src 'self'") ||
				!strings.Contains(policy, "connect-src 'self'") {
				t.Fatalf("frontend policy permits unsafe scripts or blocks local resources: %s", policy)
			}
		})
	}
	for _, path := range []string{"/static/", "/static/login.html", "/static/README.md", "/static/missing.js"} {
		if result := webRequest(s, http.MethodGet, path, "", nil); result.Code != http.StatusNotFound {
			t.Errorf("unexpected resource exposed at %s: %d", path, result.Code)
		}
	}
}

func TestDashboardLatencyRequiresLiveSession(t *testing.T) {
	t.Parallel()
	s, _, session, _ := webFixture(t)
	unauthenticated := webRequest(s, http.MethodGet, "/health/latency", "", nil)
	if unauthenticated.Code != http.StatusSeeOther || unauthenticated.Header().Get("Location") != "/login" {
		t.Fatalf("unauthenticated latency response %d", unauthenticated.Code)
	}
	authenticated := webRequest(s, http.MethodGet, "/health/latency", session, nil)
	if authenticated.Code != http.StatusNoContent || authenticated.Body.Len() != 0 ||
		authenticated.Header().Get("Cache-Control") != "no-store" {
		t.Fatalf("authenticated latency response %d", authenticated.Code)
	}
	key, _, _ := s.store.session(session, "127.0.0.1")
	s.store.revoke(key)
	if result := webRequest(s, http.MethodGet, "/health/latency", session, nil); result.Code != http.StatusSeeOther {
		t.Fatalf("revoked session accepted: %d", result.Code)
	}
}
