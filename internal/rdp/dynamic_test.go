package rdp

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestDynamicCapabilitiesWindowsReplies(t *testing.T) {
	t.Parallel()
	for _, header := range []byte{0x50, 0x54, 0x58, 0x5c} {
		for _, version := range []byte{1, 2, 3} {
			t.Run(fmt.Sprintf("header_%02x_version_%d", header, version), func(t *testing.T) {
				t.Parallel()
				traffic := append(graphicsInputFixture([]byte{header, 0, version, 0}), 4, 4, 0, 0x1e)
				conn := &graphicsTestConn{incoming: bytes.NewReader(traffic)}
				s := &Session{
					Width: 1024, Height: 768, conn: conn,
					joined: map[uint16]bool{1004: true}, dynamic: &dynamicState{channel: 1004},
					graphics: &graphicsState{supported: true},
				}
				if _, err := s.BeginGraphics(); err != nil {
					t.Fatal(err)
				}
				if _, err := s.BeginDisplayControl(); err != nil {
					t.Fatal(err)
				}
				data, fast, err := s.ReadPacket()
				if err != nil || !fast || !bytes.Equal(data, []byte{4, 0, 0x1e}) {
					t.Fatalf("input after capability response = %x, %v, %v", data, fast, err)
				}
				// Higher client capabilities do not enable unsupported v2/v3 features.
				output := graphicsOutputFixture(t, conn.outgoing.Bytes())
				if len(output) != 3 || !bytes.Equal(output[0], []byte{0x50, 0, 1, 0}) ||
					!bytes.Equal(output[1], append([]byte{0x10, 1}, graphicsChannelName...)) ||
					!bytes.Equal(output[2], append([]byte{0x10, 2}, displayChannelName...)) {
					t.Fatalf("unexpected v1 negotiation messages: %x", output)
				}
				if s.dynamic.phase != 2 || s.dynamic.graphics.phase != 2 || s.dynamic.display.phase != 2 {
					t.Fatal("capabilities did not open both requested endpoints")
				}
				before := conn.outgoing.Len()
				if _, err := s.BeginGraphics(); err != nil {
					t.Fatal(err)
				}
				if _, err := s.BeginDisplayControl(); err != nil {
					t.Fatal(err)
				}
				if conn.outgoing.Len() != before {
					t.Fatal("repeated begin reopened dynamic channels")
				}
			})
		}
	}
}

func TestDynamicCapabilitiesRejectInvalidReplies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		phase  byte
		data   []byte
		reason string
	}{
		{"unsolicited", 0, []byte{0x5c, 0, 3, 0}, "dvc_caps_state"},
		{"duplicate", 2, []byte{0x5c, 0, 3, 0}, "dvc_caps_state"},
		{"truncated", 1, []byte{0x50}, "dvc_caps_length"},
		{"missing version", 1, []byte{0x50, 0}, "dvc_caps_length"},
		{"short version", 1, []byte{0x50, 0, 1}, "dvc_caps_length"},
		{"trailing data", 1, append([]byte{0x50, 0, 1, 0}, []byte("private-packet-marker")...), "dvc_caps_length"},
		{"cbId one", 1, []byte{0x51, 0, 1, 0}, "dvc_caps_header"},
		{"cbId two", 1, []byte{0x5e, 0, 2, 0}, "dvc_caps_header"},
		{"cbId three", 1, []byte{0x5f, 0, 3, 0}, "dvc_caps_header"},
		{"padding", 1, []byte{0x5c, 1, 3, 0}, "dvc_caps_header"},
		{"zero version", 1, []byte{0x50, 0, 0, 0}, "dvc_caps_version"},
		{"unknown version", 1, []byte{0x50, 0, 4, 0}, "dvc_caps_version"},
		{"high version byte", 1, []byte{0x50, 0, 1, 1}, "dvc_caps_version"},
		{"maximum version", 1, []byte{0x5c, 0, 0xff, 0xff}, "dvc_caps_version"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			conn := &graphicsTestConn{}
			s := &Session{conn: conn, dynamic: &dynamicState{
				channel: 1004, phase: test.phase, display: dynamicEndpoint{phase: 1},
			}}
			err := s.readDynamic(test.data)
			if err == nil {
				t.Fatal("invalid capabilities accepted")
			}
			wrapped := withDiagnosticStage(diagnosticStageDynamicChannels, err)
			var detail interface{ RDPDiagnosticReason() string }
			if !errors.As(wrapped, &detail) || detail.RDPDiagnosticReason() != test.reason {
				t.Fatalf("missing safe diagnostic reason %q: %v", test.reason, wrapped)
			}
			if !errors.Is(wrapped, err) || strings.Contains(wrapped.Error(), "private-packet-marker") {
				t.Fatal("diagnostics changed error identity or exposed packet contents")
			}
			if s.dynamic.phase != test.phase || s.dynamic.display.phase != 1 || conn.outgoing.Len() != 0 {
				t.Fatal("invalid capabilities changed state or opened a channel")
			}
		})
	}
}
