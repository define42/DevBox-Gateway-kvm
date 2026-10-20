package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"mime"
	"net/http"
	"net/http/httptest"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

type perfReportTestBackend struct {
	*webBackend
	listed, sampled []string
	list            func(context.Context, string) ([]VM, error)
	sample          func(context.Context, string, string) (VMPerformance, error)
}

func (b *perfReportTestBackend) List(ctx context.Context, owner string) ([]VM, error) {
	b.listed = append(b.listed, owner)
	if b.list != nil {
		return b.list(ctx, owner)
	}

	return b.webBackend.List(ctx, owner)
}

func (b *perfReportTestBackend) Performance(ctx context.Context, owner, id string) (VMPerformance, error) {
	b.sampled = append(b.sampled, owner+":"+id)
	if b.sample != nil {
		return b.sample(ctx, owner, id)
	}

	return VMPerformance{State: "live", FramesSent: 42}, nil
}

func TestPerfAuthenticationAndOwnership(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query        string
		anonymous, revoked bool
		expired            bool
		admin              bool
		status             int
	}{
		{name: "owner inventory", query: "?owner=bob", status: http.StatusOK},
		{name: "owner filter", query: "?vm=mine&owner=bob", status: http.StatusOK},
		{name: "foreign filter", query: "?vm=foreign", status: http.StatusNotFound},
		{name: "missing filter", query: "?vm=absent", status: http.StatusNotFound},
		{name: "anonymous", anonymous: true, status: http.StatusSeeOther},
		{name: "revoked", revoked: true, status: http.StatusSeeOther},
		{name: "expired", expired: true, status: http.StatusSeeOther},
		{name: "admin inventory", query: "?owner=bob", admin: true, status: http.StatusOK},
		{name: "admin foreign filter", query: "?vm=foreign", admin: true, status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			backend := &perfReportTestBackend{webBackend: base}
			s.config.Backend = backend
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
			if test.expired {
				key, _, _ := s.store.session(raw, "127.0.0.1")
				s.store.mu.Lock()
				s.store.sessions[key].expires = time.Now().Add(-time.Second)
				s.store.mu.Unlock()
			}
			if test.anonymous {
				raw = ""
			}
			w := webRequest(s, http.MethodGet, "/perf"+test.query, raw, nil)
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, test.status, w.Body.String())
			}
			checkPerfReadOnly(t, base, w)
			if test.status == http.StatusSeeOther {
				if w.Header().Get("Location") != "/login" || len(backend.listed) != 0 || len(backend.sampled) != 0 {
					t.Fatal("unauthenticated request accessed diagnostics or did not redirect to login")
				}

				return
			}
			if !slices.Equal(backend.listed, []string{"alice"}) {
				t.Fatalf("List owners = %v, want canonical session owner", backend.listed)
			}
			if test.status != http.StatusOK {
				if len(backend.sampled) != 0 {
					t.Fatalf("unowned VM reached Performance: %v", backend.sampled)
				}

				return
			}
			got := decodePerfReport(t, w)
			if len(got.VMs) != 1 || got.VMs[0].ID != "mine" ||
				!slices.Equal(backend.sampled, []string{"alice:mine"}) {
				t.Fatalf("unscoped diagnostics: %+v; calls %v", got.VMs, backend.sampled)
			}
		})
	}
}

func TestPerfSchemaPrivacyAndDownload(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query string
		download    bool
	}{
		{name: "inline"},
		{name: "attachment", query: "?vm=mine&download=1", download: true},
		{name: "explicit download only", query: "?download=true"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			base.vms[0].Name = "private-vm-name"
			base.vms[0].Error = "private-process-error"
			base.vms[0].ImageID = "private-image-id"
			base.vms[0].ImageName = "private-image-name"
			s.config.DesktopImage = "/private/image.qcow2"
			s.config.RequiredGroup = "private-access-group"
			s.config.KeyFile = "/private/tls-key.pem"
			sampledAt := time.Date(2026, time.October, 18, 12, 0, 0, 0, time.UTC)
			cpu, interval, fps, payloadRate := 251.5, 5.25, 9.5, 2048.5
			encode, write, ack := 4.5, 0.3, 27.5
			sentMbps, receivedMbps := 1.25, 0.003
			resident := uint64(2 << 30)
			want := VMPerformance{
				State: "live", SampledAt: &sampledAt, SampleIntervalSeconds: &interval,
				CPUPercent: &cpu, ResidentBytes: &resident, GuestMemoryBytes: 4 << 30, VCPUs: 2,
				RequestedEncoder: "auto", RDPProfile: "office", Connections: 2, Encoder: "software",
				FramesSent: 100, FramesPerSecond: &fps, PayloadBytes: 4096, PayloadBytesPerSecond: &payloadRate,
				EncodeMs: &encode, WriteMs: &write, ACKMs: &ack,
				Network: &VMNetworkPerformance{
					SentBytes: 12000, ReceivedBytes: 3000, SentMbps: &sentMbps, ReceivedMbps: &receivedMbps,
				},
				Pipeline: &VMPipelinePerformance{
					CaptureMs: &write, ConvertMs: &encode, OutstandingACKs: 2,
				},
			}
			s.config.Backend = &perfReportTestBackend{
				webBackend: base,
				sample:     func(context.Context, string, string) (VMPerformance, error) { return want, nil },
			}
			before := time.Now()
			w := webRequest(s, http.MethodGet, "/perf"+test.query, raw, nil)
			after := time.Now()
			got := decodePerfReport(t, w)
			if got.Version != 1 || got.Scope != "current-user" || got.SampleIntervalSeconds != 5 ||
				got.Truncated || got.GeneratedAt.Before(before) || got.GeneratedAt.After(after) || len(got.Notes) == 0 {
				t.Fatalf("invalid report envelope: %+v", got)
			}
			if len(got.VMs) != 1 || got.VMs[0].ID != "mine" || got.VMs[0].State != "running" ||
				!reflect.DeepEqual(got.VMs[0].Performance, want) {
				t.Fatalf("snapshot fields changed: %+v", got.VMs)
			}
			root := checkPerfJSONKeys(t, w.Body.Bytes(),
				"version", "generatedAt", "sampleIntervalSeconds", "scope", "truncated", "vms", "notes")
			var entries []json.RawMessage
			if err := json.Unmarshal(root["vms"], &entries); err != nil {
				t.Fatal(err)
			}
			entry := checkPerfJSONKeys(t, entries[0], "id", "state", "performance")
			performance := checkPerfJSONKeys(t, entry["performance"], "state", "sampledAt", "sampleIntervalSeconds",
				"cpuPercent", "residentBytes", "guestMemoryBytes", "vcpus", "requestedEncoder", "rdpProfile",
				"connections", "encoder", "framesSent", "framesPerSecond", "payloadBytes", "payloadBytesPerSecond",
				"encodeMs", "writeMs", "ackMs", "pipeline", "network")
			checkPerfJSONKeys(t, performance["network"], "sentBytes", "receivedBytes", "sentMbps", "receivedMbps")
			checkPerfJSONKeys(t, performance["pipeline"], "transferMs", "transferBytesPerSecond",
				"flushMs", "flushesPerSecond", "flushedPixelsPerSecond", "fullRefreshesPerSecond",
				"scanoutSwitchesPerSecond", "captureMs", "convertMs", "ackBlockedMsPerSecond",
				"outstandingAcks", "sourceWidth", "sourceHeight", "outputWidth", "outputHeight")
			for _, secret := range []string{"alice", "bob", "foreign", "secret-work", "private-", "/private/", raw} {
				if strings.Contains(w.Body.String(), secret) {
					t.Fatalf("diagnostics disclosed %q", secret)
				}
			}
			if disposition := w.Header().Get("Content-Disposition"); test.download {
				mediaType, params, err := mime.ParseMediaType(disposition)
				if err != nil || mediaType != "attachment" || params["filename"] != "devbox-performance.json" {
					t.Fatalf("download disposition = %q: %v", disposition, err)
				}
			} else if disposition != "" {
				t.Fatalf("unexpected attachment: %q", disposition)
			}
			checkPerfReadOnly(t, base, w)
		})
	}
}

func TestPerfInventoryLimitAndOrdering(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query string
		count, want int
		truncated   bool
	}{
		{name: "empty", count: 0, want: 0},
		{name: "at limit", count: 64, want: 64},
		{name: "over limit", count: 70, want: 64, truncated: true},
		{name: "select beyond limit", query: "?vm=vm-069", count: 70, want: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			base.vms = nil
			for index := test.count - 1; index >= 0; index-- {
				base.vms = append(base.vms, VM{ID: fmt.Sprintf("vm-%03d", index), Owner: "alice", State: "running"})
			}
			// Foreign entries must not consume the current user's report limit.
			for index := range 70 {
				base.vms = append(base.vms, VM{ID: fmt.Sprintf("foreign-%03d", index), Owner: "bob"})
			}
			backend := &perfReportTestBackend{webBackend: base}
			s.config.Backend = backend
			w := webRequest(s, http.MethodGet, "/perf"+test.query, raw, nil)
			got := decodePerfReport(t, w)
			if got.VMs == nil || len(got.VMs) != test.want || got.Truncated != test.truncated ||
				len(backend.sampled) != test.want {
				t.Fatalf("report count=%d truncated=%t, snapshots=%d", len(got.VMs), got.Truncated, len(backend.sampled))
			}
			for index, vm := range got.VMs {
				wantID := fmt.Sprintf("vm-%03d", index)
				if test.query != "" {
					wantID = "vm-069"
				}
				if vm.ID != wantID || backend.sampled[index] != "alice:"+wantID {
					t.Fatalf("entry %d = %s, call %s; want %s", index, vm.ID, backend.sampled[index], wantID)
				}
			}
			checkPerfReadOnly(t, base, w)
		})
	}
}

func TestPerfOptionalBackend(t *testing.T) {
	t.Parallel()
	for _, test := range []struct{ name, state, want string }{
		{"running", "running", "unavailable"},
		{"stopped", "stopped", "stopped"},
		{"starting", "starting", "starting"},
		{"creating", "creating", "starting"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			base.vms[0].State = test.state
			s.config.DesktopCPUs, s.config.DesktopMemoryMiB = 4, 8192
			w := webRequest(s, http.MethodGet, "/perf", raw, nil)
			got := decodePerfReport(t, w)
			if len(got.VMs) != 1 || got.VMs[0].State != test.state {
				t.Fatalf("fallback inventory: %+v", got.VMs)
			}
			want := VMPerformance{State: test.want, VCPUs: 4, GuestMemoryBytes: 8 << 30}
			if !reflect.DeepEqual(got.VMs[0].Performance, want) {
				t.Fatalf("fallback invented live measurements: %+v", got.VMs[0].Performance)
			}
			checkPerfReadOnly(t, base, w)
		})
	}
}

func TestPerfDisappearedVM(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, query string
		status      int
	}{
		{name: "omit disappeared", status: http.StatusOK},
		{name: "selected disappeared", query: "?vm=gone", status: http.StatusNotFound},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			base.vms = append(base.vms, VM{ID: "gone", Owner: "alice", State: "running"})
			s.config.Backend = &perfReportTestBackend{
				webBackend: base,
				sample: func(_ context.Context, _, id string) (VMPerformance, error) {
					if id == "gone" {
						return VMPerformance{}, fmt.Errorf("private lifecycle detail: %w", ErrNotFound)
					}

					return VMPerformance{State: "live"}, nil
				},
			}
			w := webRequest(s, http.MethodGet, "/perf"+test.query, raw, nil)
			if w.Code != test.status || strings.Contains(w.Body.String(), "private lifecycle") {
				t.Fatalf("disappeared VM status=%d, body=%s", w.Code, w.Body.String())
			}
			if test.status == http.StatusOK {
				got := decodePerfReport(t, w)
				if len(got.VMs) != 1 || got.VMs[0].ID != "mine" {
					t.Fatalf("disappeared VM was retained: %+v", got.VMs)
				}
			}
			checkPerfReadOnly(t, base, w)
		})
	}
}

func TestPerfErrorsDoNotWritePartialReports(t *testing.T) {
	t.Parallel()
	privateError := fmt.Errorf("private backend error /secret/token: %w", io.ErrUnexpectedEOF)
	for _, test := range []struct {
		name    string
		list    bool
		err     error
		invalid bool
		status  int
	}{
		{name: "list failure", list: true, err: privateError, status: http.StatusInternalServerError},
		{name: "list conflict", list: true, err: ErrConflict, status: http.StatusConflict},
		{name: "snapshot failure", err: privateError, status: http.StatusInternalServerError},
		{name: "snapshot conflict", err: ErrConflict, status: http.StatusConflict},
		{name: "snapshot canceled", err: context.Canceled, status: http.StatusRequestTimeout},
		{name: "invalid float", invalid: true, status: http.StatusServiceUnavailable},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			base.vms = []VM{
				{ID: "a-good", Owner: "alice", State: "running"},
				{ID: "z-error", Owner: "alice", State: "running"},
			}
			if test.list {
				base.listErr = test.err
			}
			backend := &perfReportTestBackend{
				webBackend: base,
				sample: func(_ context.Context, _, id string) (VMPerformance, error) {
					if id == "a-good" {
						return VMPerformance{State: "live", FramesSent: 12345}, nil
					}
					if test.invalid {
						invalid := math.NaN()

						return VMPerformance{State: "live", CPUPercent: &invalid}, nil
					}

					return VMPerformance{}, test.err
				},
			}
			s.config.Backend = backend
			w := webRequest(s, http.MethodGet, "/perf?download=1", raw, nil)
			if w.Code != test.status || json.Valid(w.Body.Bytes()) ||
				strings.Contains(w.Body.String(), "private") || strings.Contains(w.Body.String(), "a-good") ||
				strings.Contains(w.Body.String(), "framesSent") || w.Header().Get("Content-Disposition") != "" {
				t.Fatalf("error leaked details or a partial report: status %d, body %s", w.Code, w.Body.String())
			}
			if test.list && len(backend.sampled) != 0 {
				t.Fatalf("snapshots collected after List failed: %v", backend.sampled)
			}
			checkPerfReadOnly(t, base, w)
		})
	}
}

func TestPerfRequestContext(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		listOnly bool
	}{
		{name: "canceled list", listOnly: true},
		{name: "canceled snapshot"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			type requestKey struct{}
			ctx, cancel := context.WithCancel(context.WithValue(t.Context(), requestKey{}, "request-marker"))
			defer cancel()
			backend := &perfReportTestBackend{
				webBackend: base,
				list: func(listCtx context.Context, owner string) ([]VM, error) {
					if owner != "alice" || listCtx.Value(requestKey{}) != "request-marker" {
						t.Error("List lost the authenticated owner or request context values")
					}
					if test.listOnly {
						if !errors.Is(listCtx.Err(), context.Canceled) {
							t.Error("List did not observe the canceled request")
						}

						return nil, listCtx.Err()
					}

					return base.List(listCtx, owner)
				},
				sample: func(sampleCtx context.Context, owner, id string) (VMPerformance, error) {
					if owner != "alice" || id != "mine" || sampleCtx.Value(requestKey{}) != "request-marker" {
						t.Error("Performance lost ownership or request context values")
					}
					cancel()
					if !errors.Is(sampleCtx.Err(), context.Canceled) {
						t.Error("Performance did not observe request cancellation")
					}

					return VMPerformance{}, sampleCtx.Err()
				},
			}
			s.config.Backend = backend
			if test.listOnly {
				cancel()
			}
			r := httptest.NewRequestWithContext(ctx, http.MethodGet, "https://gateway.test/perf", nil)
			r.RemoteAddr = "127.0.0.1:45678"
			r.AddCookie(&http.Cookie{
				Name: sessionCookie, Value: raw, Secure: true, HttpOnly: true, SameSite: http.SameSiteStrictMode,
			})
			w := httptest.NewRecorder()
			s.routes().ServeHTTP(w, r)
			if w.Code != http.StatusRequestTimeout || len(backend.listed) != 1 ||
				(test.listOnly && len(backend.sampled) != 0) || (!test.listOnly && len(backend.sampled) != 1) {
				t.Fatalf("canceled request status=%d, list=%v, snapshots=%v", w.Code, backend.listed, backend.sampled)
			}
			checkPerfReadOnly(t, base, w)
		})
	}
}

func TestPerfHTTPMethods(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, method string
		status       int
	}{
		{"head", http.MethodHead, http.StatusOK},
		{"post", http.MethodPost, http.StatusMethodNotAllowed},
		{"delete without origin", http.MethodDelete, http.StatusForbidden},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, base, raw, _ := webFixture(t)
			backend := &perfReportTestBackend{webBackend: base}
			s.config.Backend = backend
			w := webRequest(s, test.method, "/perf", raw, nil)
			if w.Code != test.status {
				t.Fatalf("%s status=%d, want %d", test.method, w.Code, test.status)
			}
			if test.method == http.MethodHead {
				if w.Header().Get("Content-Type") != "application/json" {
					t.Fatal("HEAD omitted representation headers")
				}
			} else if len(backend.listed) != 0 || len(backend.sampled) != 0 {
				t.Fatal("rejected method accessed diagnostics")
			}
			checkPerfReadOnly(t, base, w)
		})
	}
}

func TestPerfDashboardDownloadLink(t *testing.T) {
	t.Parallel()
	s, base, raw, _ := webFixture(t)
	w := webRequest(s, http.MethodGet, "/", raw, nil)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `href="/perf?download=1"`) ||
		!strings.Contains(w.Body.String(), "Download diagnostics") {
		t.Fatalf("authenticated dashboard lacks diagnostics download link: status %d", w.Code)
	}
	checkPerfReadOnly(t, base, w)
}

func decodePerfReport(t *testing.T, w *httptest.ResponseRecorder) performanceReport {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("diagnostics status=%d type=%q: %s", w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	var result performanceReport
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatal(err)
	}

	return result
}

func checkPerfJSONKeys(t *testing.T, data []byte, keys ...string) map[string]json.RawMessage {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	if len(object) != len(keys) {
		t.Fatalf("JSON has unexpected fields: %s", data)
	}
	for _, key := range keys {
		if _, ok := object[key]; !ok {
			t.Fatalf("JSON lacks %q: %s", key, data)
		}
	}

	return object
}

func checkPerfReadOnly(t *testing.T, backend *webBackend, w *httptest.ResponseRecorder) {
	t.Helper()
	if len(backend.calls) != 0 {
		t.Fatalf("diagnostics performed VM actions: %v", backend.calls)
	}
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("private diagnostics lack cache or MIME protections")
	}
}
