package gateway

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/buildinfo"
)

// Administrator diagnostics include unattributed attempts (for example, a TLS
// failure before a routing grant can be checked). This endpoint must never be
// exposed through the owner-scoped /perf report or an unauthenticated request.
type adminDiagnosticReport struct {
	Version        int                       `json:"version"`
	GeneratedAt    time.Time                 `json:"generatedAt"`
	Scope          string                    `json:"scope"`
	Application    buildinfo.Info            `json:"application"`
	RDPLoginEvents []rdpDiagnosticEvent      `json:"rdpLoginEvents"`
	RDPWorkers     RDPWorkerDiagnosticReport `json:"rdpWorkers"`
	Notes          []string                  `json:"notes"`
}

func (s *Server) adminDiagnostics(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	// The optional worker provider reads cached telemetry only. Missing or failed
	// worker snapshots must never prevent downloading the gateway login events.
	report := adminDiagnosticReport{
		Version: 1, GeneratedAt: time.Now().UTC(), Scope: "administrator",
		Application: buildinfo.Current(), RDPLoginEvents: s.rdpDiagnostics.snapshot("", "", true),
		RDPWorkers: RDPWorkerDiagnosticReport{State: "unsupported", VMs: []RDPWorkerDiagnostic{}},
		Notes: []string{
			"application identifies the running devbox-kvm gateway binary. Development builds report devel; " +
				"commit and build date are included only when recorded in the binary.",
			"The rolling in-memory RDP log contains at most 512 recent events across all connections, " +
				"including failures before a user or VM is identified. It resets on gateway restart. " +
				"Reproduce the Microsoft Remote Desktop failure, then collect a report after the worker heartbeat.",
			"Events use UTC timestamps and a per-process attemptId. A progress event identifies the next " +
				"operation; a failed event identifies the last stage reached and a sanitized error category.",
			"Client Info validation failures include a fixed reason code, such as empty_password or " +
				"unsupported_security_flags. These reasons describe rejection checks, not credential values. " +
				"A failure at client_info_validation happens before the password authenticator is called.",
			"Authenticated means gateway password validation succeeded, not that a desktop was displayed. " +
				"session_proxy means the gateway is relaying subsequent activation/session traffic; it does not " +
				"inspect that traffic. closed is a transport result, not proof of successful desktop activation.",
			"rdpWorkers contains cached VM-side terminal RDP session diagnostics for at most 64 VMs in ID order. " +
				"Each worker retains its last four ended sessions across idle samples and reconnects, until eviction " +
				"or worker restart. Wait at least five seconds after reproducing a failure before downloading. " +
				"A stale snapshot may not contain the latest failure; empty sessions can mean no recorded attempts " +
				"or an older worker without instrumentation.",
			"Worker session sequence numbers are local to that VM process, not gateway attemptIds. Correlate " +
				"using VM ID and UTC timestamps. stage is the last operation reached; transport closure does not " +
				"identify a malformed packet. error contains only a fixed category, never raw error text.",
			"Deploy the updated gateway and restart affected VM workers to enable new worker instrumentation. " +
				"Downloading diagnostics does not restart anything or collect worker console output.",
			"No passwords, usernames, routing tokens, client IPs, private backend addresses, raw packets or " +
				"raw error text are recorded. VM IDs and negotiated protocol/TLS metadata remain; review before sharing.",
			"Owner-scoped VM performance measurements remain available through My DevBoxes / Download diagnostics.",
		},
	}
	if backend, ok := s.config.Backend.(RDPWorkerDiagnosticBackend); ok {
		workers, err := backend.RDPWorkerDiagnostics(r.Context())
		if err != nil {
			report.RDPWorkers.State = "unavailable"
		} else {
			report.RDPWorkers = workers
		}
	}
	data, err := json.MarshalIndent(report, "", "  ")
	if err != nil {
		http.Error(w, "Diagnostics are temporarily unavailable.", http.StatusServiceUnavailable)

		return
	}
	w.Header().Set("Content-Type", "application/json")
	if r.URL.Query().Get("download") == "1" {
		w.Header().Set("Content-Disposition", `attachment; filename="devbox-diagnostics.json"`)
	}
	_, _ = w.Write(data)
}
