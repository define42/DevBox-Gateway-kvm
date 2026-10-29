package gateway

import (
	"encoding/json"
	"fmt"
	"mime"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestAdminDiagnosticsAuthorizationAndDownload(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, path string
		identity   string
		status     int
		download   bool
		revoked    bool
		expired    bool
	}{
		{name: "anonymous", path: "/admin/diagnostics", status: http.StatusSeeOther},
		{name: "non-administrator", path: "/admin/diagnostics", identity: "user", status: http.StatusForbidden},
		{
			name: "revoked administrator", path: "/admin/diagnostics", identity: "admin",
			status: http.StatusSeeOther, revoked: true,
		},
		{
			name: "expired administrator", path: "/admin/diagnostics", identity: "admin",
			status: http.StatusSeeOther, expired: true,
		},
		{name: "administrator inline", path: "/admin/diagnostics", identity: "admin", status: http.StatusOK},
		{
			name: "explicit download value only", path: "/admin/diagnostics?download=true", identity: "admin",
			status: http.StatusOK,
		},
		{
			name: "administrator download", path: "/admin/diagnostics?download=1", identity: "admin",
			status: http.StatusOK, download: true,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			s, _, userSession, _ := webFixture(t)
			adminSession, err := s.newIdentitySession(
				Identity{Username: "administrator", IsAdmin: true}, "127.0.0.1", "hash",
			)
			if err != nil {
				t.Fatal(err)
			}
			if test.revoked || test.expired {
				key, _, ok := s.store.session(adminSession, "127.0.0.1")
				if !ok {
					t.Fatal("missing administrator session")
				}
				if test.revoked {
					s.store.revoke(key)
				} else {
					s.store.mu.Lock()
					s.store.sessions[key].expires = time.Now().Add(-time.Second)
					s.store.mu.Unlock()
				}
			}
			// Administrator diagnostics deliberately remain available when the
			// VM backend is unavailable.
			s.config.Backend = nil
			addHTTPDiagnosticEvent(s, "", "", "connection_request")
			raw := ""
			switch test.identity {
			case "user":
				raw = userSession
			case "admin":
				raw = adminSession
			}
			before := time.Now().UTC()
			w := webRequest(s, http.MethodGet, test.path, raw, nil)
			after := time.Now().UTC()
			if w.Code != test.status {
				t.Fatalf("status = %d, want %d: %s", w.Code, test.status, w.Body.String())
			}
			checkDiagnosticPrivateHeaders(t, w)
			if test.status == http.StatusSeeOther {
				if w.Header().Get("Location") != "/login" {
					t.Fatalf("anonymous redirect = %q, want /login", w.Header().Get("Location"))
				}

				return
			}
			if test.status == http.StatusForbidden {
				if strings.Contains(w.Body.String(), "connection_request") {
					t.Fatal("non-administrator received diagnostic events")
				}

				return
			}
			report := decodeAdminDiagnosticReport(t, w)
			if report.Version != 1 || report.Scope != "administrator" || len(report.Notes) == 0 ||
				report.Application.Version == "" || report.Application.GoVersion == "" ||
				report.GeneratedAt.Before(before) || report.GeneratedAt.After(after) ||
				len(report.RDPLoginEvents) != 1 {
				t.Fatalf("invalid administrator report: %+v", report)
			}
			disposition := w.Header().Get("Content-Disposition")
			if test.download {
				mediaType, params, err := mime.ParseMediaType(disposition)
				if err != nil || mediaType != "attachment" || params["filename"] != "devbox-diagnostics.json" {
					t.Fatalf("download disposition = %q: %v", disposition, err)
				}
			} else if disposition != "" {
				t.Fatalf("unexpected inline disposition %q", disposition)
			}
		})
	}
}

func TestHTTPDiagnosticsEventScopeAndVMFiltering(t *testing.T) {
	t.Parallel()
	s, backend, userSession, _ := webFixture(t)
	adminSession, err := s.newIdentitySession(
		Identity{Username: "alice", IsAdmin: true}, "127.0.0.1", "hash",
	)
	if err != nil {
		t.Fatal(err)
	}
	unknown := addHTTPDiagnosticEvent(s, "", "mine", "connection_request")
	aliceMine := addHTTPDiagnosticEvent(s, "alice", "mine", "frontend_tls")
	aliceOther := addHTTPDiagnosticEvent(s, "alice", "other", "backend_lookup")
	bobMine := addHTTPDiagnosticEvent(s, "bob", "mine", "credential_authentication")

	admin := decodeAdminDiagnosticReport(t,
		webRequest(s, http.MethodGet, "/admin/diagnostics?owner=alice&vm=mine", adminSession, nil))
	got := diagnosticAttemptIDs(admin.RDPLoginEvents)
	want := []uint64{unknown, aliceMine, aliceOther, bobMine}
	if !slices.Equal(got, want) {
		t.Fatalf("administrator event IDs = %v, want all %v", got, want)
	}

	// owner is not a supported selector. The signed-in identity remains the
	// authority even when a caller attempts to spoof another owner.
	owner := decodePerfReport(t, webRequest(s, http.MethodGet, "/perf?owner=bob", userSession, nil))
	if got, want := diagnosticAttemptIDs(owner.RDPLoginEvents), []uint64{aliceMine, aliceOther}; !slices.Equal(got, want) {
		t.Fatalf("owner event IDs = %v, want %v", got, want)
	}
	if len(owner.VMs) != 1 || owner.VMs[0].ID != "mine" {
		t.Fatalf("spoofed owner changed VM scope: %+v", owner.VMs)
	}
	adminOwner := decodePerfReport(t,
		webRequest(s, http.MethodGet, "/perf?owner=bob", adminSession, nil))
	if got := diagnosticAttemptIDs(adminOwner.RDPLoginEvents); !slices.Equal(got, []uint64{aliceMine, aliceOther}) {
		t.Fatalf("administrator /perf escaped owner scope: event IDs %v", got)
	}
	if len(adminOwner.VMs) != 1 || adminOwner.VMs[0].ID != "mine" {
		t.Fatalf("administrator /perf escaped owner VM scope: %+v", adminOwner.VMs)
	}

	selected := decodePerfReport(t,
		webRequest(s, http.MethodGet, "/perf?vm=mine&owner=bob", userSession, nil))
	if got := diagnosticAttemptIDs(selected.RDPLoginEvents); !slices.Equal(got, []uint64{aliceMine}) {
		t.Fatalf("VM-filtered event IDs = %v, want [%d]", got, aliceMine)
	}
	foreign := webRequest(s, http.MethodGet, "/perf?vm=foreign&owner=bob", userSession, nil)
	if foreign.Code != http.StatusNotFound {
		t.Fatalf("spoofed owner selected foreign VM: status=%d body=%s", foreign.Code, foreign.Body.String())
	}
	checkPerfReadOnly(t, backend, foreign)
}

func TestAdminDiagnosticsSchemaAndPrivacy(t *testing.T) {
	t.Parallel()
	s, backend, userSession, _ := webFixture(t)
	adminSession, err := s.newIdentitySession(
		Identity{Username: "administrator", IsAdmin: true}, "127.0.0.1", "hash",
	)
	if err != nil {
		t.Fatal(err)
	}
	requested, selected := uint32(11), uint32(1)
	attempt := s.rdpDiagnostics.begin()
	attempt.owner = "diagnostic-private-owner"
	attempt.event.VMID = "visible-vm-id"
	attempt.event.RequestedProtocols = &requested
	attempt.event.SelectedProtocol = &selected
	attempt.event.TLSVersion = "TLS 1.3"
	attempt.event.CipherSuite = "TLS_AES_128_GCM_SHA256"
	attempt.stage("frontend_tls")
	attempt.finish(fmt.Errorf(
		"diagnostic-raw-password routing-token-marker 203.0.113.9 @private-backend: %w",
		rdpClientInfoSmartCardPIN,
	))

	w := webRequest(s, http.MethodGet, "/admin/diagnostics", adminSession, nil)
	report := decodeAdminDiagnosticReport(t, w)
	if len(report.RDPLoginEvents) != 2 || report.RDPLoginEvents[1].Error != "authentication failed" ||
		report.RDPLoginEvents[1].Reason != "smart_card_pin_not_supported" ||
		report.RDPLoginEvents[1].RequestedProtocols == nil ||
		*report.RDPLoginEvents[1].RequestedProtocols != requested ||
		report.RDPLoginEvents[1].SelectedProtocol == nil ||
		*report.RDPLoginEvents[1].SelectedProtocol != selected {
		t.Fatalf("diagnostic event metadata changed: %+v", report.RDPLoginEvents)
	}
	root := checkPerfJSONKeys(t, w.Body.Bytes(),
		"version", "generatedAt", "scope", "application", "rdpLoginEvents", "notes")
	checkDiagnosticJSONFields(t, root["application"],
		[]string{"version", "goVersion"},
		[]string{"version", "commit", "buildDate", "goVersion", "modified"})
	var events []json.RawMessage
	if err := json.Unmarshal(root["rdpLoginEvents"], &events); err != nil {
		t.Fatal(err)
	}
	for _, event := range events {
		checkDiagnosticJSONFields(t, event,
			[]string{"timestamp", "attemptId", "stage", "outcome", "message"},
			[]string{
				"timestamp", "attemptId", "stage", "outcome", "message", "error", "reason", "vmId",
				"requestedProtocols", "selectedProtocol", "tlsVersion", "cipherSuite",
			})
	}
	for _, private := range []string{
		"diagnostic-private-owner", "diagnostic-raw-password", "routing-token-marker", "203.0.113.9",
		"@private-backend", userSession,
	} {
		if strings.Contains(w.Body.String(), private) {
			t.Fatalf("administrator diagnostics exposed %q", private)
		}
	}
	checkPerfReadOnly(t, backend, w)
}

func addHTTPDiagnosticEvent(s *Server, owner, vm, stage string) uint64 {
	attempt := s.rdpDiagnostics.begin()
	attempt.owner = owner
	attempt.event.VMID = vm
	attempt.stage(stage)

	return attempt.event.AttemptID
}

func diagnosticAttemptIDs(events []rdpDiagnosticEvent) []uint64 {
	ids := make([]uint64, len(events))
	for index, event := range events {
		ids[index] = event.AttemptID
	}

	return ids
}

func decodeAdminDiagnosticReport(t *testing.T, w *httptest.ResponseRecorder) adminDiagnosticReport {
	t.Helper()
	if w.Code != http.StatusOK || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("administrator diagnostics status=%d type=%q: %s",
			w.Code, w.Header().Get("Content-Type"), w.Body.String())
	}
	var report adminDiagnosticReport
	if err := json.Unmarshal(w.Body.Bytes(), &report); err != nil {
		t.Fatal(err)
	}

	return report
}

func checkDiagnosticJSONFields(t *testing.T, data []byte, required, allowed []string) {
	t.Helper()
	var object map[string]json.RawMessage
	if err := json.Unmarshal(data, &object); err != nil {
		t.Fatal(err)
	}
	for key := range object {
		if !slices.Contains(allowed, key) {
			t.Fatalf("diagnostic JSON has unexpected field %q: %s", key, data)
		}
	}
	for _, key := range required {
		if _, ok := object[key]; !ok {
			t.Fatalf("diagnostic JSON lacks %q: %s", key, data)
		}
	}
}

func checkDiagnosticPrivateHeaders(t *testing.T, w *httptest.ResponseRecorder) {
	t.Helper()
	if w.Header().Get("Cache-Control") != "no-store" || w.Header().Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("private diagnostics lack cache or MIME protections")
	}
}
