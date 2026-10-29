package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

type workerDiagnosticTestBackend struct {
	*perfReportTestBackend
	calls  int
	report RDPWorkerDiagnosticReport
	err    error
}

func (b *workerDiagnosticTestBackend) RDPWorkerDiagnostics(context.Context) (RDPWorkerDiagnosticReport, error) {
	b.calls++

	return b.report, b.err
}

func TestAdminWorkerDiagnosticsAuthorization(t *testing.T) {
	t.Parallel()
	s, base, user, _ := webFixture(t)
	backend := &workerDiagnosticTestBackend{perfReportTestBackend: &perfReportTestBackend{webBackend: base}}
	s.config.Backend = backend
	for _, raw := range []string{"", user} {
		w := webRequest(s, http.MethodGet, "/admin/diagnostics", raw, nil)
		if w.Code == http.StatusOK || backend.calls != 0 {
			t.Fatal("non-administrator invoked cross-owner worker diagnostics")
		}
		checkDiagnosticPrivateHeaders(t, w)
	}
	admin, err := s.newIdentitySession(Identity{Username: "administrator", IsAdmin: true}, "127.0.0.1", "hash")
	if err != nil {
		t.Fatal(err)
	}
	backend.report = RDPWorkerDiagnosticReport{State: "available", VMs: []RDPWorkerDiagnostic{}}
	w := webRequest(s, http.MethodGet, "/admin/diagnostics?download=1", admin, nil)
	report := decodeAdminDiagnosticReport(t, w)
	if backend.calls != 1 || report.RDPWorkers.State != "available" {
		t.Fatalf("administrator failed to collect worker cache: %+v", report.RDPWorkers)
	}
	checkPerfReadOnly(t, base, w)
}

func TestAdminWorkerDiagnosticsUnavailableStillDownloads(t *testing.T) {
	t.Parallel()
	s, base, _, _ := webFixture(t)
	backend := &workerDiagnosticTestBackend{
		perfReportTestBackend: &perfReportTestBackend{webBackend: base},
		report:                RDPWorkerDiagnosticReport{VMs: []RDPWorkerDiagnostic{{VMID: "partial-secret"}}},
		err:                   fmt.Errorf("private-worker-password: %w", context.DeadlineExceeded),
	}
	s.config.Backend = backend
	admin, err := s.newIdentitySession(Identity{Username: "administrator", IsAdmin: true}, "127.0.0.1", "hash")
	if err != nil {
		t.Fatal(err)
	}
	addHTTPDiagnosticEvent(s, "", "", "connection_request")
	w := webRequest(s, http.MethodGet, "/admin/diagnostics", admin, nil)
	report := decodeAdminDiagnosticReport(t, w)
	if report.RDPWorkers.State != "unavailable" || report.RDPWorkers.VMs == nil ||
		len(report.RDPWorkers.VMs) != 0 || len(report.RDPLoginEvents) != 1 {
		t.Fatalf("worker failure broke gateway diagnostic download: %+v", report)
	}
	for _, secret := range []string{"private-worker-password", "partial-secret"} {
		if strings.Contains(w.Body.String(), secret) {
			t.Fatalf("worker failure exposed %q", secret)
		}
	}
	checkPerfReadOnly(t, base, w)
}

func TestHTTPWorkerDiagnosticsSchemaAndOwnerScope(t *testing.T) {
	t.Parallel()
	s, base, user, _ := webFixture(t)
	now := time.Now().UTC()
	event := performanceRDPSession(1, now)
	event.Stage, event.Outcome, event.Error = "dynamic_channels", "failed", "protocol_or_internal_error"
	event.Reason = "dvc_caps_version"
	backend := &workerDiagnosticTestBackend{
		perfReportTestBackend: &perfReportTestBackend{
			webBackend: base,
			sample: func(_ context.Context, owner, id string) (VMPerformance, error) {
				if owner != "alice" || id != "mine" {
					t.Fatalf("foreign performance requested: %s %s", owner, id)
				}

				return VMPerformance{State: "live", RDPSessions: []telemetry.RDPSessionEvent{event}}, nil
			},
		},
		report: RDPWorkerDiagnosticReport{State: "available", VMs: []RDPWorkerDiagnostic{
			{VMID: "foreign-admin-only-vm", State: "stale", SampledAt: &now, Sessions: []telemetry.RDPSessionEvent{event}},
		}},
	}
	s.config.Backend = backend
	admin, err := s.newIdentitySession(Identity{Username: "alice", IsAdmin: true}, "127.0.0.1", "hash")
	if err != nil {
		t.Fatal(err)
	}
	w := webRequest(s, http.MethodGet, "/admin/diagnostics", admin, nil)
	report := decodeAdminDiagnosticReport(t, w)
	if len(report.RDPWorkers.VMs) != 1 || len(report.RDPWorkers.VMs[0].Sessions) != 1 ||
		report.RDPWorkers.VMs[0].Sessions[0].Reason != event.Reason {
		t.Fatalf("worker diagnostics omitted: %+v", report.RDPWorkers)
	}
	checkWorkerDiagnosticJSON(t, w.Body.Bytes())
	for _, raw := range []string{user, admin} {
		ownerResponse := webRequest(s, http.MethodGet, "/perf?owner=bob", raw, nil)
		ownerReport := decodePerfReport(t, ownerResponse)
		if len(ownerReport.VMs) != 1 || ownerReport.VMs[0].ID != "mine" ||
			len(ownerReport.VMs[0].Performance.RDPSessions) != 1 ||
			ownerReport.VMs[0].Performance.RDPSessions[0].Reason != event.Reason || backend.calls != 1 {
			t.Fatalf("owner scope/worker history incorrect: %+v, cross-owner calls %d", ownerReport, backend.calls)
		}
		if strings.Contains(ownerResponse.Body.String(), "foreign-admin-only-vm") ||
			strings.Contains(ownerResponse.Body.String(), "\"rdpWorkers\"") {
			t.Fatal("owner performance leaked cross-owner diagnostics")
		}
		checkPerfReadOnly(t, base, ownerResponse)
	}
}

func checkWorkerDiagnosticJSON(t *testing.T, data []byte) {
	t.Helper()
	var root map[string]json.RawMessage
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	worker := checkPerfJSONKeys(t, root["rdpWorkers"], "state", "truncated", "vms")
	var vms []json.RawMessage
	if err := json.Unmarshal(worker["vms"], &vms); err != nil {
		t.Fatal(err)
	}
	vm := checkPerfJSONKeys(t, vms[0], "vmId", "state", "sampledAt", "sessions")
	var sessions []json.RawMessage
	if err := json.Unmarshal(vm["sessions"], &sessions); err != nil {
		t.Fatal(err)
	}
	checkPerfJSONKeys(t, sessions[0], "sequence", "timestamp", "stage", "outcome", "error", "reason", "durationMs")
}
