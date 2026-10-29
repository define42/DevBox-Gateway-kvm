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
	Version        int                  `json:"version"`
	GeneratedAt    time.Time            `json:"generatedAt"`
	Scope          string               `json:"scope"`
	Application    buildinfo.Info       `json:"application"`
	RDPLoginEvents []rdpDiagnosticEvent `json:"rdpLoginEvents"`
	Notes          []string             `json:"notes"`
}

func (s *Server) adminDiagnostics(w http.ResponseWriter, r *http.Request) {
	if _, ok := s.requireAdmin(w, r); !ok {
		return
	}
	// Do not query the backend: failed VM lookups must not prevent collecting
	// the very negotiation failures this report is intended to diagnose.
	report := adminDiagnosticReport{
		Version: 1, GeneratedAt: time.Now().UTC(), Scope: "administrator",
		Application: buildinfo.Current(), RDPLoginEvents: s.rdpDiagnostics.snapshot("", "", true),
		Notes: []string{
			"application identifies the running devbox-kvm gateway binary. Development builds report devel; " +
				"commit and build date are included only when recorded in the binary.",
			"The rolling in-memory RDP log contains at most 512 recent events across all connections, " +
				"including failures before a user or VM is identified. It resets on gateway restart. " +
				"Reproduce the Microsoft Remote Desktop failure, then download this report immediately.",
			"Events use UTC timestamps and a per-process attemptId. A progress event identifies the next " +
				"operation; a failed event identifies the last stage reached and a sanitized error category.",
			"Authenticated means gateway password validation succeeded, not that a desktop was displayed. " +
				"session_proxy means the gateway is relaying subsequent activation/session traffic; it does not " +
				"inspect that traffic or diagnose guest desktop failures.",
			"No passwords, usernames, routing tokens, client IPs, private backend addresses, raw packets or " +
				"raw error text are recorded. VM IDs and negotiated protocol/TLS metadata remain; review before sharing.",
			"Owner-scoped VM performance measurements remain available through My DevBoxes / Download diagnostics.",
		},
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
