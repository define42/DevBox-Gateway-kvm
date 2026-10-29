package gateway

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func TestManagerRDPDiagnosticsBoundedSortedInventory(t *testing.T) {
	t.Parallel()
	m := &Manager{ctx: t.Context(), vms: make(map[string]*managedVM)}
	for i := 70; i >= 0; i-- {
		id := fmt.Sprintf("vm-%03d", i)
		state := "stopped"
		if i == 0 {
			state = "deleted"
		}
		m.vms[id] = newManagedVM(VM{ID: id, Owner: "private-owner", Name: "private-name", State: state})
	}
	report, err := m.RDPWorkerDiagnostics(t.Context())
	if err != nil || report.State != "available" || !report.Truncated || len(report.VMs) != maxPerformanceReportVMs {
		t.Fatalf("bounded diagnostics = %+v, %v", report, err)
	}
	for i, vm := range report.VMs {
		if vm.VMID != fmt.Sprintf("vm-%03d", i+1) || vm.State != "stopped" || vm.Sessions == nil || len(vm.Sessions) != 0 {
			t.Fatalf("diagnostic VM %d = %+v", i, vm)
		}
	}
}

func TestManagerRDPDiagnosticsCacheAndWorkerLifetime(t *testing.T) {
	t.Parallel()
	now := time.Now().UTC()
	p := &processPerformance{}
	sample := performanceTestSample()
	sample.RDPSessions = []telemetry.RDPSessionEvent{performanceRDPSession(1, now)}
	if !p.decode(performanceTestJSON(t, sample), now) {
		t.Fatal("sample rejected")
	}
	v := newManagedVM(VM{ID: "vm", State: "running"})
	v.run = &managedProcess{done: make(chan struct{}), performance: p}
	v.publish()
	m := &Manager{ctx: t.Context(), vms: map[string]*managedVM{"vm": v}}
	report, err := m.RDPWorkerDiagnostics(t.Context())
	if err != nil || len(report.VMs) != 1 || report.VMs[0].State != "live" ||
		report.VMs[0].SampledAt == nil || len(report.VMs[0].Sessions) != 1 {
		t.Fatalf("live diagnostics = %+v, %v", report, err)
	}
	report.VMs[0].Sessions[0].Stage = "mutated"
	report.VMs[0].SampledAt = nil
	stale := workerRDPDiagnostic(v.view.Load(), now.Add(performanceStaleAfter+time.Second))
	if stale.State != "stale" || len(stale.Sessions) != 1 ||
		stale.Sessions[0].Stage != "session_read" || stale.SampledAt == nil {
		t.Fatalf("stale/copied diagnostics = %+v", stale)
	}
	oldView := v.view.Load()
	close(v.run.done)
	v.run = &managedProcess{done: make(chan struct{}), performance: &processPerformance{}}
	v.publish()
	if got := workerRDPDiagnostic(oldView, now); got.State != "stopped" || len(got.Sessions) != 0 || got.SampledAt != nil {
		t.Fatalf("exited launch exposed stale data: %+v", got)
	}
	report, err = m.RDPWorkerDiagnostics(t.Context())
	if err != nil || report.VMs[0].State != "unavailable" || len(report.VMs[0].Sessions) != 0 {
		t.Fatalf("replacement inherited diagnostics: %+v, %v", report, err)
	}
}

func TestManagerRDPDiagnosticsLifecycleAndUnavailable(t *testing.T) {
	t.Parallel()
	for _, state := range []string{"creating", "starting", "running", "stopped"} {
		t.Run(state, func(t *testing.T) {
			t.Parallel()
			v := newManagedVM(VM{ID: "vm", State: state})
			want := "stopped"
			if state == "creating" || state == "starting" {
				want = "starting"
			}
			if state == "running" {
				v.run = &managedProcess{done: make(chan struct{})}
				v.publish()
				want = "unavailable"
			}
			got := workerRDPDiagnostic(v.view.Load(), time.Now())
			if got.State != want || got.Sessions == nil || len(got.Sessions) != 0 {
				t.Fatalf("diagnostics = %+v, want %s with empty sessions", got, want)
			}
		})
	}
}

func TestManagerRDPDiagnosticsCancellationAndEmpty(t *testing.T) {
	t.Parallel()
	m := &Manager{ctx: t.Context(), vms: make(map[string]*managedVM)}
	report, err := m.RDPWorkerDiagnostics(t.Context())
	if err != nil || report.VMs == nil || len(report.VMs) != 0 || report.Truncated || report.State != "available" {
		t.Fatalf("empty diagnostics = %+v, %v", report, err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := m.RDPWorkerDiagnostics(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled diagnostics error = %v", err)
	}
	m.closed = true
	if _, err := m.RDPWorkerDiagnostics(t.Context()); err == nil {
		t.Fatal("closed manager accepted diagnostics request")
	}
}
