package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"slices"
	"strings"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

const maxPerformanceReportVMs = 64

// The diagnostic schema is an explicit allowlist, not a serialization of VM,
// Config or the process environment. In particular it excludes owner/name,
// image paths, error details, credentials and private connection addresses.
type performanceReport struct {
	Version               int                   `json:"version"`
	GeneratedAt           time.Time             `json:"generatedAt"`
	SampleIntervalSeconds float64               `json:"sampleIntervalSeconds"`
	Scope                 string                `json:"scope"`
	Truncated             bool                  `json:"truncated"`
	VMs                   []performanceReportVM `json:"vms"`
	Notes                 []string              `json:"notes"`
}

type performanceReportVM struct {
	ID          string        `json:"id"`
	State       string        `json:"state"`
	Performance VMPerformance `json:"performance"`
}

func (s *Server) perf(w http.ResponseWriter, r *http.Request) {
	_, session, ok := s.requireSession(w, r)
	if !ok {
		return
	}
	report, err := s.performanceReport(r.Context(), session.owner, r.URL.Query().Get("vm"))
	if err != nil {
		status, message := backendError(err)
		http.Error(w, message, status)

		return
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		http.Error(w, "Performance is temporarily unavailable.", http.StatusServiceUnavailable)

		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="devbox-performance.json"`)
	}
	_, _ = w.Write(data)
}

func (s *Server) performanceReport(ctx context.Context, owner, id string) (performanceReport, error) {
	report := performanceReport{
		Version: 1, Scope: "current-user", SampleIntervalSeconds: telemetry.Interval.Seconds(),
		VMs: []performanceReportVM{}, Notes: performanceReportNotes(),
	}
	vms, err := s.config.Backend.List(ctx, owner)
	if err != nil {
		return report, err
	}
	// Filter even when a backend accidentally returns a mixed-owner inventory.
	owned := make([]VM, 0)
	for _, vm := range vms {
		if vm.Owner == owner && (id == "" || vm.ID == id) {
			owned = append(owned, vm)
		}
	}
	if id != "" && len(owned) == 0 {
		return report, ErrNotFound
	}
	slices.SortFunc(owned, func(a, b VM) int { return strings.Compare(a.ID, b.ID) })
	if len(owned) > maxPerformanceReportVMs {
		report.Truncated = true
		owned = owned[:maxPerformanceReportVMs]
	}
	for _, vm := range owned {
		if err := ctx.Err(); err != nil {
			return report, err
		}
		performance, err := s.performanceForVM(ctx, owner, vm)
		// A desktop can be deleted between listing and the ownership-checked
		// snapshot. Omit it, but retain normal 404 semantics for explicit IDs.
		if errors.Is(err, ErrNotFound) && id == "" {
			continue
		}
		if err != nil {
			return report, err
		}
		report.VMs = append(report.VMs, performanceReportVM{ID: vm.ID, State: vm.State, Performance: performance})
	}
	report.GeneratedAt = time.Now().UTC()

	return report, nil
}

func performanceReportNotes() []string {
	return []string{
		"Read-only cached snapshots, not a benchmark. Reproduce the lag while connected over RDP, " +
			"then collect several reports at least 5 seconds apart.",
		"Only the signed-in user's VMs are included, including for administrators. " +
			"VM IDs and measurements remain in this report; review it before sharing.",
		"sampleIntervalSeconds at report level is the nominal export period. Per-VM sampleIntervalSeconds " +
			"is the actual interval used for rates. Null means unavailable, not zero.",
		"Use live samples for diagnosis. Starting/unavailable need time to collect; stale means exports " +
			"stopped arriving. stopped is not live. Lifetime counters reset when the VM worker restarts. " +
			"Inventory and telemetry are sampled separately, so their states may briefly differ.",
		"CPU percent is host-process CPU including guest execution and encoding: 100% is one host CPU. " +
			"Resident bytes is worker RSS, not guest application memory or guest free memory.",
		"requestedEncoder is configuration; encoder is the actual active RDP backend. " +
			"Software H.264 uses the bundled pure Go encoder. Empty means no active encoder.",
		"FPS and payload bytes/second aggregate server sends across viewers, not displayed FPS or link capacity. " +
			"Idle desktops legitimately send few frames. Payload bytes exclude transport overhead.",
		"network reports RDP bytes at the VM endpoint, including negotiation, TLS and all channels, " +
			"excluding TCP/IP overhead and guest networking. Sent is VM to client (download); received is client to VM " +
			"(upload). Mbps uses decimal megabits per second. Totals span reconnects until VM restart; " +
			"rates need two samples. A null network means unavailable or unsupported worker telemetry.",
		"encodeMs and writeMs are interval averages, not percentiles. Encode time excludes capture/conversion. " +
			"ACK is the slowest current viewer's smoothed send-to-ACK turnaround, " +
			"not pure network or input-to-display latency.",
		"pipeline separates virtio transfer/flush, framebuffer capture and colour conversion. Times are interval " +
			"service-time averages, not CPU time or percentiles. Stages have different operation counts; do not add " +
			"their means to estimate frame latency. A null pipeline means unsupported worker telemetry.",
		"pipeline flush includes resource pixel conversion and synchronous publication, not RDP encoding. " +
			"fullRefreshesPerSecond counts forced image initialization, not every full-screen damaged region. " +
			"convertMs includes bitmap comparison/packing when bitmap is active.",
		"pipeline ackBlockedMsPerSecond sums writer-observed pending-frame waits beyond normal pacing; " +
			"it can exceed 1000 with multiple viewers. outstandingAcks is the latest writer-observed aggregate; " +
			"both can lag ACK events during blocked transport writes. " +
			"Geometry is zero when unknown or more than one viewer is connected.",
		"This report does not measure guest disk latency, guest GPU usage, host contention or client decoding/display " +
			"delays. It cannot prove a root cause on its own. Optional administrator CPU profiling is separate " +
			"and never starts when this endpoint is read.",
		"Reports include at most 64 VMs in ID order. If truncated is true, request /perf?vm=<id> for another VM.",
	}
}
