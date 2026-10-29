package gateway

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"strings"
	"testing"
	"time"
)

func awaitRDPDiagnosticCompletion(t *testing.T, s *Server) []rdpDiagnosticEvent {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(time.Millisecond)
	defer tick.Stop()
	for {
		events := s.rdpDiagnostics.snapshot("", "", true)
		if len(events) > 0 {
			last := events[len(events)-1]
			if last.Outcome == "failed" || last.Outcome == "closed" {
				return events
			}
		}
		select {
		case <-deadline.C:
			t.Fatal("RDP diagnostic attempt did not finish")
		case <-tick.C:
		}
	}
}

func TestRDPDiagnosticMalformedNegotiation(t *testing.T) {
	t.Parallel()
	s := &Server{}
	server, client := net.Pipe()
	defer server.Close()
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(3 * time.Second))
	done := make(chan struct{})
	go func() {
		defer close(done)
		s.handleRDP(t.Context(), server)
	}()
	// A valid TPKT envelope with a missing RDP negotiation request fails before TLS.
	if _, err := client.Write([]byte{3, 0, 0, 11, 6, 0xe0, 0, 0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	events := awaitRDPDiagnosticCompletion(t, s)
	<-done
	last := events[len(events)-1]
	if last.Stage != "negotiation_request" || last.Outcome != "failed" ||
		last.Error != "invalid request or protocol data" {
		t.Fatalf("unexpected negotiation failure: %+v", last)
	}
	if len(s.rdpDiagnostics.snapshot("alice", "", false)) != 0 {
		t.Fatal("unattributed negotiation exposed to an ordinary user")
	}
}

func TestRDPDiagnosticTLSFailureBeforePassword(t *testing.T) {
	t.Parallel()
	s, _, _, _, _ := startGatewayForTest(t, Endpoint{}) //nolint:dogsled // No credentials or session needed before TLS.
	raw, err := net.DialTimeout("tcp", s.Addr().String(), time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	_ = raw.SetDeadline(time.Now().Add(3 * time.Second))
	const protocols = protocolTLS | 2 | 8
	if err := writePacket(raw, connectionRequestWithProtocols("", protocols)); err != nil {
		t.Fatal(err)
	}
	if _, err := readConnectionPacket(raw); err != nil {
		t.Fatal(err)
	}
	// Disconnect at precisely the point a client would start its TLS handshake.
	_ = raw.Close()
	events := awaitRDPDiagnosticCompletion(t, s)
	last := events[len(events)-1]
	if last.Stage != "frontend_tls" || last.Outcome != "failed" || last.Error != "connection closed" {
		t.Fatalf("unexpected TLS failure: %+v", last)
	}
	if last.RequestedProtocols == nil || *last.RequestedProtocols != protocols ||
		last.SelectedProtocol == nil || *last.SelectedProtocol != protocolTLS {
		t.Fatal("diagnostics lost RDP security protocol selection")
	}
	if last.VMID != "" || len(s.rdpDiagnostics.snapshot("alice", "", false)) != 0 {
		t.Fatal("pre-routing failure incorrectly attributed to a VM or user")
	}
}

func TestRDPDiagnosticMissingRoutingGrant(t *testing.T) {
	t.Parallel()
	s, _, cert, _, _ := startGatewayForTest(t, Endpoint{}) //nolint:dogsled // Deliberately omit the session/routing grant.
	front := dialFrontRDP(t, s, cert, "")
	defer front.NetConn().Close()
	events := awaitRDPDiagnosticCompletion(t, s)
	last := events[len(events)-1]
	if last.Stage != "routing_grant" || last.Outcome != "failed" || last.Error != "authentication failed" {
		t.Fatalf("unexpected routing failure: %+v", last)
	}
	if last.TLSVersion == "" || last.CipherSuite == "" {
		t.Fatal("successful TLS handshake metadata missing")
	}
}

func TestRDPDiagnosticPasswordAndSession(t *testing.T) {
	t.Parallel()
	for _, valid := range []bool{false, true} {
		name := "invalid-password"
		if valid {
			name = "authenticated-session"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			endpoint := startTestBackend(t, func(conn net.Conn) bool {
				return passwordBackendHandshake(conn, nil)
			})
			s, _, cert, raw, _ := startGatewayForTest(t, endpoint)
			key, _, _ := s.store.session(raw, "127.0.0.1")
			token, err := s.store.grant(key, "mine")
			if err != nil {
				t.Fatal(err)
			}
			password := "diagnostics-secret-incorrect-password"
			if valid {
				password = "rdp-password"
			}
			front := dialFrontRDP(t, s, cert, token)
			if err := passwordClientHandshake(front, "alice", password); err != nil {
				t.Fatal(err)
			}
			if valid {
				payload := []byte("diagnostics-secret-desktop-traffic")
				if err := writePacket(front, payload); err != nil {
					t.Fatal(err)
				}
				echo := make([]byte, len(payload))
				if _, err := io.ReadFull(front, echo); err != nil {
					t.Fatal(err)
				}
				if !bytes.Equal(echo, payload) {
					t.Fatal("diagnostics affected proxied traffic")
				}
				_ = front.NetConn().Close()
			}
			events := awaitRDPDiagnosticCompletion(t, s)
			last := events[len(events)-1]
			if valid {
				if last.Stage != "session_proxy" || last.Outcome != "closed" || last.Error != "" {
					t.Fatalf("unexpected session completion: %+v", last)
				}
			} else if last.Stage != "credential_authentication" || last.Outcome != "failed" ||
				last.Error != "authentication failed" {
				t.Fatalf("unexpected password failure: %+v", last)
			}
			authenticated := false
			for _, event := range events {
				if event.AttemptID != last.AttemptID || event.At.IsZero() || event.Stage == "unknown" {
					t.Fatalf("incomplete diagnostic event: %+v", event)
				}
				authenticated = authenticated || event.Outcome == "authenticated"
			}
			if authenticated != valid || last.VMID != "mine" {
				t.Fatal("diagnostics misreported authentication or VM association")
			}
			owned := s.rdpDiagnostics.snapshot("alice", "mine", false)
			if len(owned) == 0 || len(owned) >= len(events) || len(s.rdpDiagnostics.snapshot("bob", "", false)) != 0 {
				t.Fatal("RDP diagnostic ownership filtering failed")
			}
			encoded, err := json.Marshal(events)
			if err != nil {
				t.Fatal(err)
			}
			for _, secret := range []string{
				password, "alice", token, raw, endpoint.Address, "127.0.0.1", "diagnostics-secret-desktop-traffic",
			} {
				if strings.Contains(string(encoded), secret) {
					t.Fatal("RDP diagnostic events contain private connection data")
				}
			}
		})
	}
}
