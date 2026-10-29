package gateway

import (
	"context"
	"slices"
	"strings"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

// RDPWorkerDiagnosticBackend supplies only cached worker snapshots. Implementers
// must not wait for workers or collect guest output. Callers must authorize an
// administrator before invoking this cross-owner interface. Implementations are
// trusted to return at most 64 VMs with at most four validated, sanitized session
// events per VM; Manager enforces this at worker ingress and cache collection.
type RDPWorkerDiagnosticBackend interface {
	RDPWorkerDiagnostics(context.Context) (RDPWorkerDiagnosticReport, error)
}

type RDPWorkerDiagnosticReport struct {
	State     string                `json:"state"`
	Truncated bool                  `json:"truncated"`
	VMs       []RDPWorkerDiagnostic `json:"vms"`
}

// Deliberately exclude VM owner/name, private addresses and other inventory data.
type RDPWorkerDiagnostic struct {
	VMID      string                      `json:"vmId"`
	State     string                      `json:"state"`
	SampledAt *time.Time                  `json:"sampledAt,omitempty"`
	Sessions  []telemetry.RDPSessionEvent `json:"sessions"`
}

// RDPWorkerDiagnostics takes bounded, immutable launch views without acquiring
// VM lifecycle locks or doing process/file I/O. Old launch data must never be
// attributed to a replacement worker, even when a restart races this download.
func (m *Manager) RDPWorkerDiagnostics(ctx context.Context) (RDPWorkerDiagnosticReport, error) {
	report := RDPWorkerDiagnosticReport{State: "available", VMs: []RDPWorkerDiagnostic{}}
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return report, err
	}
	defer finish()
	views, truncated := m.rdpDiagnosticViews()
	if err := ctx.Err(); err != nil {
		return RDPWorkerDiagnosticReport{}, err
	}
	report.Truncated = truncated
	now := time.Now()
	for _, view := range views {
		if err := ctx.Err(); err != nil {
			return RDPWorkerDiagnosticReport{}, err
		}
		report.VMs = append(report.VMs, workerRDPDiagnostic(view, now))
	}

	return report, nil
}

func (m *Manager) rdpDiagnosticViews() ([]*managedVMView, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	views := make([]*managedVMView, 0, maxPerformanceReportVMs+1)
	count := 0
	for _, vm := range m.vms {
		view := vm.view.Load()
		if view.record.State == "deleted" {
			continue
		}
		count++
		index, _ := slices.BinarySearchFunc(views, view.record.ID, func(v *managedVMView, id string) int {
			return strings.Compare(v.record.ID, id)
		})
		if index >= maxPerformanceReportVMs {
			continue
		}
		views = slices.Insert(views, index, view)
		if len(views) > maxPerformanceReportVMs {
			views = views[:maxPerformanceReportVMs]
		}
	}

	return views, count > maxPerformanceReportVMs
}

func workerRDPDiagnostic(view *managedVMView, now time.Time) RDPWorkerDiagnostic {
	result := RDPWorkerDiagnostic{VMID: view.record.ID, State: "stopped", Sessions: []telemetry.RDPSessionEvent{}}
	if view.run != nil {
		result.State = "unavailable"
		if view.run.performance != nil {
			sample := view.run.performance.snapshot(now)
			result.State, result.SampledAt = sample.State, sample.SampledAt
			if sample.RDPSessions != nil {
				result.Sessions = sample.RDPSessions
			}
		}
		select {
		case <-view.run.done:
			return RDPWorkerDiagnostic{VMID: view.record.ID, State: "stopped", Sessions: []telemetry.RDPSessionEvent{}}
		default:
		}
	}
	if view.record.State == "starting" || view.record.State == "creating" {
		result.State = "starting"
	}

	return result
}
