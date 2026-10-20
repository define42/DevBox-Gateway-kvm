package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

type performanceTestBackend struct {
	*webBackend
	performance func(context.Context, string, string) (VMPerformance, error)
}

func (b *performanceTestBackend) Performance(ctx context.Context, owner, id string) (VMPerformance, error) {
	return b.performance(ctx, owner, id)
}

func performanceRequest(raw, id string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "https://gateway.test/vms/"+id+"/performance?owner=bob", nil)
	r.RemoteAddr = "127.0.0.1:45678"
	if raw != "" {
		r.AddCookie(&http.Cookie{
			Name: sessionCookie, Value: raw, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
		})
	}

	return r
}

func TestVMPerformanceEnforcesOwnership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, id       string
		anonymous      bool
		admin, revoked bool
		status         int
	}{
		{name: "owner", id: "mine", status: http.StatusOK},
		{name: "other owner", id: "foreign", status: http.StatusNotFound},
		{name: "missing", id: "absent", status: http.StatusNotFound},
		{name: "anonymous", id: "mine", anonymous: true, status: http.StatusSeeOther},
		{name: "revoked", id: "mine", revoked: true, status: http.StatusSeeOther},
		{name: "admin other owner", id: "foreign", admin: true, status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			if test.admin {
				var err error
				raw, err = s.newIdentitySession(Identity{Username: "alice", IsAdmin: true}, "127.0.0.1", "hash")
				if err != nil {
					t.Fatal(err)
				}
			}
			if test.revoked {
				key, _, _ := s.store.session(raw, "127.0.0.1")
				s.store.revoke(key)
			}
			called := false
			s.config.Backend = &performanceTestBackend{
				webBackend: base,
				performance: func(ctx context.Context, owner, id string) (VMPerformance, error) {
					called = true
					if owner != "alice" || id != "mine" || ctx.Err() != nil {
						t.Errorf("untrusted owner or context: %s, %s, %v", owner, id, ctx.Err())
					}

					return VMPerformance{
						State: "live", Encoder: "software", Connections: 1, FramesSent: 42,
						Network: &VMNetworkPerformance{SentBytes: 12000, ReceivedBytes: 3000},
					}, nil
				},
			}
			if test.anonymous {
				raw = ""
			}
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, performanceRequest(raw, test.id))
			if w.Code != test.status || called != (test.status == http.StatusOK) {
				t.Fatalf("status=%d called=%t, want %d", w.Code, called, test.status)
			}
			if w.Header().Get("Cache-Control") != "no-store" {
				t.Fatal("private response is cacheable")
			}
			if test.status == http.StatusOK {
				var got VMPerformance
				if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.FramesSent != 42 ||
					got.Network == nil || got.Network.SentBytes != 12000 || got.Network.ReceivedBytes != 3000 {
					t.Fatalf("performance response: %+v, %v", got, err)
				}
			}
		})
	}
}

func TestVMPerformanceOptionalBackendAndErrors(t *testing.T) {
	t.Parallel()
	s, base, raw, _ := webFixture(t)
	s.config.DesktopMemoryMiB = 4096
	w := httptest.NewRecorder()
	s.routes().ServeHTTP(w, performanceRequest(raw, "mine"))
	var got VMPerformance
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || w.Code != http.StatusOK ||
		got.State != "unavailable" || got.CPUPercent != nil || got.ResidentBytes != nil ||
		got.GuestMemoryBytes != 4<<30 || got.Network != nil {
		t.Fatalf("optional telemetry invented metrics: %+v, %v, status %d", got, err, w.Code)
	}
	base.vms[0].State = "creating"
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, performanceRequest(raw, "mine"))
	if err := json.Unmarshal(w.Body.Bytes(), &got); err != nil || got.State != "starting" || got.SampledAt != nil {
		t.Fatalf("optional backend provisioning state: %+v, %v", got, err)
	}
	s.config.Backend = &performanceTestBackend{
		webBackend:  base,
		performance: func(context.Context, string, string) (VMPerformance, error) { return VMPerformance{}, ErrNotFound },
	}
	w = httptest.NewRecorder()
	s.routes().ServeHTTP(w, performanceRequest(raw, "mine"))
	if w.Code != http.StatusNotFound {
		t.Fatalf("backend ownership revalidation failed: %d", w.Code)
	}
}
