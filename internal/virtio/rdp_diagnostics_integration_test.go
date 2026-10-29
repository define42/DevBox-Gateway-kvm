//go:build integration

package virtio

import (
	"bytes"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
	"net"
	"testing"
	"time"

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

func TestRDPBackendDynamicCapabilitiesWindowsCompatibility(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		response []byte
	}{
		{name: "version-1", response: []byte{0x50, 0, 1, 0}},
		{name: "version-2", response: []byte{0x50, 0, 2, 0}},
		{name: "windows-version-3-unused-bits", response: []byte{0x5c, 0, 3, 0}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := telemetry.New()
			display := newRDPTestDisplayWithConfig(t, RDPConfig{Telemetry: collector})
			input := &rdpInputSink{events: make(chan rdpInputSinkEvent, 2)}
			display.SetInput(input)
			conn := connectRDPDiagnosticDynamicSession(t, display)

			writeRDPDiagnosticDynamicMessage(t, conn, test.response)
			assertRDPDiagnosticDisplayCreate(t, conn)
			writeRDPDiagnosticDynamicMessage(t, conn, []byte{0x10, 2, 0, 0, 0, 0})
			caps := readRDPDiagnosticDynamicMessage(t, conn)
			if len(caps) != 22 || !bytes.Equal(caps[:2], []byte{0x30, 2}) ||
				binary.LittleEndian.Uint32(caps[2:]) != 5 || binary.LittleEndian.Uint32(caps[6:]) != 20 {
				t.Fatalf("display-control capabilities = %x", caps)
			}

			// Synchronize with the real reader by dispatching input after both
			// capability negotiation and channel creation have completed.
			writeRDPInputUnicode(t, conn, true, 0x100)
			select {
			case event := <-input.events:
				assertRDPInputKeyEvent(t, event, true, 0x01000100)
			case <-time.After(time.Second):
				t.Fatal("desktop input did not survive dynamic capability negotiation")
			}
			if events := collector.Snapshot().RDPSessions; len(events) != 0 {
				t.Fatalf("valid dynamic capabilities terminated the session: %+v", events)
			}
		})
	}
}

func TestRDPBackendDiagnosticsDynamicCapabilityReasons(t *testing.T) {
	t.Parallel()
	secret := []byte("private-dynamic-packet-marker")
	for _, test := range []struct {
		name       string
		response   []byte
		reason     string
		negotiated bool
	}{
		{name: "zero-version", response: []byte{0x50, 0, 0, 0}, reason: "dvc_caps_version"},
		{name: "unknown-version", response: []byte{0x50, 0, 4, 0}, reason: "dvc_caps_version"},
		{name: "channel-id-bits", response: []byte{0x51, 0, 1, 0}, reason: "dvc_caps_header"},
		{name: "nonzero-pad", response: []byte{0x50, 1, 1, 0}, reason: "dvc_caps_header"},
		{name: "truncated", response: []byte{0x50, 0, 1}, reason: "dvc_caps_length"},
		{
			name: "trailing-private-data", response: append([]byte{0x50, 0, 1, 0}, secret...),
			reason: "dvc_caps_length",
		},
		{name: "duplicate", response: []byte{0x50, 0, 1, 0}, reason: "dvc_caps_state", negotiated: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			collector := telemetry.New()
			display := newRDPTestDisplayWithConfig(t, RDPConfig{Telemetry: collector})
			conn := connectRDPDiagnosticDynamicSession(t, display)
			if test.negotiated {
				writeRDPDiagnosticDynamicMessage(t, conn, []byte{0x50, 0, 1, 0})
				assertRDPDiagnosticDisplayCreate(t, conn)
			}

			// Keep the client open until the server processes the malformed PDU;
			// a client-initiated close could otherwise mask the protocol failure.
			writeRDPDiagnosticDynamicMessage(t, conn, test.response)
			waitRDPClientCount(t, display, 0)
			snapshot := collector.Snapshot()
			if len(snapshot.RDPSessions) != 1 {
				t.Fatalf("terminal RDP events = %+v, want one", snapshot.RDPSessions)
			}
			event := snapshot.RDPSessions[0]
			if event.Stage != "dynamic_channels" || event.Outcome != "failed" ||
				event.Error != "protocol_or_internal_error" || event.Reason != test.reason {
				t.Fatalf("dynamic capability event = %+v, want reason %q", event, test.reason)
			}
			encoded, err := json.Marshal(snapshot)
			if err != nil {
				t.Fatal(err)
			}
			if bytes.Contains(encoded, secret) {
				t.Fatalf("backend telemetry exposed dynamic packet data: %s", encoded)
			}
		})
	}
}

func connectRDPDiagnosticDynamicSession(t *testing.T, display *RDPDisplay) net.Conn {
	t.Helper()
	conn := dialRDPTestDisplay(t, display)
	negotiateRDPTestDisplay(t, conn, 1)
	secured := tls.Client(conn, rdpTestClientTLS(t, display))
	if err := secured.Handshake(); err != nil {
		t.Fatal(err)
	}
	joinRDPInputTestChannels(t, secured, "drdynvc")
	confirmRDPInputTestActive(t, secured)
	if request := readRDPDiagnosticDynamicMessage(t, secured); !bytes.Equal(request, []byte{0x50, 0, 1, 0}) {
		t.Fatalf("server must offer version-1 dynamic capabilities, got %x", request)
	}

	return secured
}

func assertRDPDiagnosticDisplayCreate(t *testing.T, conn net.Conn) {
	t.Helper()
	want := append([]byte{0x10, 2}, "Microsoft::Windows::RDS::DisplayControl\x00"...)
	if create := readRDPDiagnosticDynamicMessage(t, conn); !bytes.Equal(create, want) {
		t.Fatalf("display-control create request = %x, want %x", create, want)
	}
}

func writeRDPDiagnosticDynamicMessage(t *testing.T, conn net.Conn, message []byte) {
	t.Helper()
	fragment := make([]byte, 8+len(message))
	binary.LittleEndian.PutUint32(fragment, uint32(len(message)))
	binary.LittleEndian.PutUint32(fragment[4:], 3) // CHANNEL_FLAG_FIRST | CHANNEL_FLAG_LAST
	copy(fragment[8:], message)
	packet := rdpInputAppendPERLength([]byte{0x64, 0, 0, 3, 0xec, 0x70}, len(fragment))
	writeRDPInputMCS(t, conn, append(packet, fragment...))
}

func readRDPDiagnosticDynamicMessage(t *testing.T, conn net.Conn) []byte {
	t.Helper()
	for {
		packet := readRDPInputMCS(t, conn)
		if len(packet) < 7 || packet[0] != 0x68 {
			t.Fatalf("invalid channel indication: %x", packet)
		}
		channel := binary.BigEndian.Uint16(packet[3:])
		if channel == rdpInputGlobalChannel {
			continue // The frame writer can interleave bitmap/pointer updates.
		}
		if channel != 1004 {
			t.Fatalf("unexpected output channel %d", channel)
		}
		fragment := rdpInputTakePER(t, packet[6:])
		if len(fragment) < 8 || binary.LittleEndian.Uint32(fragment) != uint32(len(fragment)-8) ||
			binary.LittleEndian.Uint32(fragment[4:]) != 3 {
			t.Fatalf("invalid unfragmented dynamic-channel message: %x", fragment)
		}

		return fragment[8:]
	}
}
