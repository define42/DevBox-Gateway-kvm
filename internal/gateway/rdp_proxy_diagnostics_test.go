package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"slices"
	"strings"
	"syscall"
	"testing"
)

func TestRDPProxyDiagnosticFixedCategories(t *testing.T) {
	t.Parallel()

	for _, operation := range []string{"front_read", "front_write", "backend_read", "backend_write"} {
		wrapped := fmt.Errorf("outer private text: %w", &rdpProxyError{operation: operation, err: io.EOF})
		gotOperation, category := rdpProxyDiagnostic(wrapped)
		if gotOperation != operation || category != "peer_eof" {
			t.Errorf("diagnostic for %q = %q, %q", operation, gotOperation, category)
		}
	}

	tests := []struct {
		name     string
		err      error
		category string
	}{
		{name: "peer EOF", err: io.EOF, category: "peer_eof"},
		{name: "unexpected EOF", err: io.ErrUnexpectedEOF, category: "unexpected_eof"},
		{name: "network closed", err: net.ErrClosed, category: "connection_closed"},
		{name: "pipe closed", err: io.ErrClosedPipe, category: "connection_closed"},
		{name: "connection reset", err: syscall.ECONNRESET, category: "connection_reset"},
		{name: "connection aborted", err: syscall.ECONNABORTED, category: "connection_aborted"},
		{name: "broken pipe", err: syscall.EPIPE, category: "broken_pipe"},
		{name: "short write", err: io.ErrShortWrite, category: "short_write"},
		{name: "deadline", err: context.DeadlineExceeded, category: "timeout"},
		{name: "network timeout", err: rdpProxyTimeoutError{}, category: "timeout"},
		{name: "canceled", err: context.Canceled, category: "canceled"},
		{name: "other transport", err: io.ErrNoProgress, category: "transport_error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			private := fmt.Errorf("private endpoint and credential text: %w", test.err)
			wrapped := fmt.Errorf("outer private text: %w", &rdpProxyError{
				operation: "front_read",
				err:       private,
			})
			operation, category := rdpProxyDiagnostic(wrapped)
			if operation != "front_read" || category != test.category {
				t.Fatalf("diagnostic = %q, %q; want front_read, %q", operation, category, test.category)
			}
			if strings.Contains(category, "private") || strings.Contains(category, "credential") {
				t.Fatalf("diagnostic exposed raw error text: %q", category)
			}
		})
	}

	for _, err := range []error{
		io.EOF,
		&rdpProxyError{operation: "peer supplied operation", err: io.EOF},
	} {
		operation, category := rdpProxyDiagnostic(err)
		if operation != "" || category != "" {
			t.Errorf("invalid proxy error produced %q, %q", operation, category)
		}
	}
	if !rdpDiagnosticNormalClose(io.ErrClosedPipe) || rdpDiagnosticError(io.ErrClosedPipe) != "connection closed" {
		t.Fatal("closed pipe is not handled as an existing normal proxy closure")
	}
}

func TestRDPProxyDiagnosticTerminalOnlyAndRedacted(t *testing.T) {
	t.Parallel()

	var log rdpDiagnosticLog
	attempt := log.begin()
	attempt.owner = "alice"
	attempt.stage("session_proxy")
	attempt.record("authenticated")
	attempt.finish(&rdpProxyError{
		operation: "front_read",
		err:       fmt.Errorf("password and raw packet secret: %w", io.EOF),
	})

	events := log.snapshot("alice", "", false)
	if len(events) != 3 {
		t.Fatalf("proxy attempt emitted %d events, want 3", len(events))
	}
	for index, event := range events[:2] {
		if event.ProxyOperation != "" || event.ProxyError != "" {
			t.Errorf("nonterminal event %d retained proxy details: %#v", index, event)
		}
	}
	terminal := events[2]
	if terminal.Outcome != "closed" || terminal.Error != "" || terminal.ProxyOperation != "front_read" ||
		terminal.ProxyError != "peer_eof" {
		t.Fatalf("terminal proxy event = %#v", terminal)
	}
	encoded, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "password") || strings.Contains(string(encoded), "raw packet") {
		t.Fatalf("proxy diagnostic JSON exposed wrapped error text: %s", encoded)
	}

	attempt.event.ProxyOperation = "front_read"
	attempt.event.ProxyError = "peer_eof"
	attempt.stage("backend_lookup")
	attempt.event.ProxyOperation = "front_read"
	attempt.event.ProxyError = "peer_eof"
	attempt.record("progress")
	attempt.event.ProxyOperation = "front_read"
	attempt.event.ProxyError = "peer_eof"
	attempt.finish(&rdpProxyError{operation: "backend_write", err: io.ErrShortWrite})
	events = log.snapshot("alice", "", false)
	for _, event := range events[len(events)-3:] {
		if event.ProxyOperation != "" || event.ProxyError != "" {
			t.Fatalf("non-proxy event retained proxy details: %#v", event)
		}
	}
}

func TestRDPProxyDiagnosticHTTPScopesAndPrivacy(t *testing.T) {
	t.Parallel()

	s, backend, ownerSession, _ := webFixture(t)
	adminSession, err := s.newIdentitySession(
		Identity{Username: "administrator", IsAdmin: true}, "127.0.0.1", "hash",
	)
	if err != nil {
		t.Fatal(err)
	}
	alice := addRDPProxyDiagnosticAttempt(
		s, "alice", "mine", "front_read", io.EOF, "alice-private-proxy-error",
	)
	bob := addRDPProxyDiagnosticAttempt(
		s, "bob", "mine", "backend_write", io.ErrShortWrite, "bob-private-proxy-error",
	)
	unknown := addRDPProxyDiagnosticAttempt(
		s, "", "mine", "backend_read", syscall.ECONNRESET, "unknown-private-proxy-error",
	)

	ownerResponse := webRequest(s, http.MethodGet, "/perf", ownerSession, nil)
	ownerReport := decodePerfReport(t, ownerResponse)
	if got := terminalRDPProxyAttemptIDs(ownerReport.RDPLoginEvents); !slices.Equal(got, []uint64{alice}) {
		t.Fatalf("owner proxy events = %v, want [%d]", got, alice)
	}
	ownerTerminal := terminalRDPProxyEvent(t, ownerReport.RDPLoginEvents, alice)
	if ownerTerminal.ProxyOperation != "front_read" || ownerTerminal.ProxyError != "peer_eof" {
		t.Fatalf("owner proxy terminal event = %#v", ownerTerminal)
	}
	checkPerfReadOnly(t, backend, ownerResponse)

	adminResponse := webRequest(s, http.MethodGet, "/admin/diagnostics", adminSession, nil)
	adminReport := decodeAdminDiagnosticReport(t, adminResponse)
	got := terminalRDPProxyAttemptIDs(adminReport.RDPLoginEvents)
	want := []uint64{alice, bob, unknown}
	if !slices.Equal(got, want) {
		t.Fatalf("administrator proxy events = %v, want %v", got, want)
	}
	event := terminalRDPProxyEvent(t, adminReport.RDPLoginEvents, bob)
	if event.ProxyOperation != "backend_write" || event.ProxyError != "short_write" {
		t.Fatalf("administrator Bob proxy event = %#v", event)
	}
	event = terminalRDPProxyEvent(t, adminReport.RDPLoginEvents, unknown)
	if event.ProxyOperation != "backend_read" || event.ProxyError != "connection_reset" {
		t.Fatalf("administrator unattributed proxy event = %#v", event)
	}

	for _, response := range []string{ownerResponse.Body.String(), adminResponse.Body.String()} {
		for _, secret := range []string{
			"alice-private-proxy-error", "bob-private-proxy-error", "unknown-private-proxy-error",
		} {
			if strings.Contains(response, secret) {
				t.Fatalf("HTTP proxy diagnostics exposed %q", secret)
			}
		}
	}
	encoded, err := json.Marshal(ownerTerminal)
	if err != nil {
		t.Fatal(err)
	}
	checkDiagnosticJSONFields(t, encoded,
		[]string{"timestamp", "attemptId", "stage", "outcome", "message", "proxyOperation", "proxyError"},
		[]string{
			"timestamp", "attemptId", "stage", "outcome", "message", "error", "reason", "proxyOperation",
			"proxyError", "vmId", "requestedProtocols", "selectedProtocol", "tlsVersion", "cipherSuite",
		})
}

func addRDPProxyDiagnosticAttempt(
	s *Server, owner, vm, operation string, cause error, privateText string,
) uint64 {
	attempt := s.rdpDiagnostics.begin()
	attempt.owner = owner
	attempt.event.VMID = vm
	attempt.stage("session_proxy")
	attempt.finish(&rdpProxyError{operation: operation, err: fmt.Errorf("%s: %w", privateText, cause)})

	return attempt.event.AttemptID
}

func terminalRDPProxyAttemptIDs(events []rdpDiagnosticEvent) []uint64 {
	ids := make([]uint64, 0, len(events))
	for _, event := range events {
		if event.ProxyOperation != "" {
			ids = append(ids, event.AttemptID)
		}
	}

	return ids
}

func terminalRDPProxyEvent(t *testing.T, events []rdpDiagnosticEvent, attemptID uint64) rdpDiagnosticEvent {
	t.Helper()
	for _, event := range events {
		if event.AttemptID == attemptID && event.ProxyOperation != "" {
			return event
		}
	}
	t.Fatalf("missing terminal proxy event for attempt %d", attemptID)

	return rdpDiagnosticEvent{}
}

type rdpProxyTimeoutError struct{}

func (rdpProxyTimeoutError) Error() string { return "private timeout text" }

func (rdpProxyTimeoutError) Timeout() bool { return true }

func (rdpProxyTimeoutError) Temporary() bool { return true }
