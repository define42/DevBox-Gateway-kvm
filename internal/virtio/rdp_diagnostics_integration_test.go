//go:build integration

package virtio

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func TestRDPBackendDiagnosticsRetainPostActivationFailure(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	display := newRDPTestDisplayWithConfig(t, RDPConfig{Telemetry: collector})
	conn := dialRDPTestDisplay(t, display)
	negotiateRDPTestDisplay(t, conn, 1)
	secured := tls.Client(conn, rdpTestClientTLS(t, display))
	if err := secured.Handshake(); err != nil {
		t.Fatal(err)
	}
	activateRDPInputTestSession(t, secured)

	secret := []byte("private-password-marker")
	writeRDPInputGlobal(t, secured, secret)
	waitRDPClientCount(t, display, 0)
	snapshot := collector.Snapshot()
	if len(snapshot.RDPSessions) != 1 {
		t.Fatalf("terminal RDP events = %+v, want one", snapshot.RDPSessions)
	}
	event := snapshot.RDPSessions[0]
	if event.Stage != rdpStageSessionRead || event.Outcome != "failed" ||
		event.Error != "protocol_or_internal_error" {
		t.Fatalf("post-activation event = %+v", event)
	}
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(encoded, secret) {
		t.Fatalf("backend telemetry exposed client packet data: %s", encoded)
	}
}
