package gateway

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"syscall"
	"testing"
)

func TestRDPDiagnosticLogZeroValueAndBound(t *testing.T) {
	t.Parallel()

	var log rdpDiagnosticLog
	empty := log.snapshot("alice", "", false)
	if empty == nil || len(empty) != 0 {
		t.Fatalf("zero-value snapshot = %#v; want non-nil empty slice", empty)
	}
	encoded, err := json.Marshal(empty)
	if err != nil || string(encoded) != "[]" {
		t.Fatalf("empty JSON = %q, %v; want []", encoded, err)
	}

	for index := 0; index < rdpDiagnosticEventLimit+8; index++ {
		attempt := log.begin()
		attempt.owner = "alice"
		attempt.stage("connection_request")
	}
	events := log.snapshot("alice", "", false)
	if len(events) != rdpDiagnosticEventLimit {
		t.Fatalf("bounded snapshot has %d events, want %d", len(events), rdpDiagnosticEventLimit)
	}
	if got, want := events[0].AttemptID, uint64(9); got != want {
		t.Fatalf("oldest retained attempt = %d, want %d", got, want)
	}
	if got, want := events[len(events)-1].AttemptID, uint64(rdpDiagnosticEventLimit+8); got != want {
		t.Fatalf("newest retained attempt = %d, want %d", got, want)
	}
}

func TestRDPDiagnosticFilteringAndMetadataCopies(t *testing.T) {
	t.Parallel()

	var log rdpDiagnosticLog
	requested, selected := uint32(11), uint32(1)
	alice := log.begin()
	alice.owner = "alice-secret-owner"
	alice.event.VMID = "vm-a"
	alice.event.RequestedProtocols = &requested
	alice.event.SelectedProtocol = &selected
	alice.event.TLSVersion = "TLS 1.3"
	alice.event.CipherSuite = "TLS_AES_128_GCM_SHA256"
	alice.stage("frontend_tls")

	// Stored metadata must not retain pointers supplied by the connection path.
	requested = 99
	selected = 77

	bob := log.begin()
	bob.owner = "bob"
	bob.event.VMID = "vm-b"
	bob.stage("backend_lookup")

	unknown := log.begin()
	unknown.event.VMID = "vm-a"
	unknown.stage("connection_request")

	events := log.snapshot("alice-secret-owner", "vm-a", false)
	if len(events) != 1 {
		t.Fatalf("owner/VM snapshot = %#v; want one Alice event", events)
	}
	if events[0].RequestedProtocols == nil || *events[0].RequestedProtocols != 11 ||
		events[0].SelectedProtocol == nil || *events[0].SelectedProtocol != 1 {
		t.Fatalf("stored metadata was mutated: %#v", events[0])
	}

	// Snapshot callers must not be able to mutate the buffer through pointers.
	*events[0].RequestedProtocols = 1234
	again := log.snapshot("alice-secret-owner", "vm-a", false)
	if *again[0].RequestedProtocols != 11 || again[0].RequestedProtocols == events[0].RequestedProtocols {
		t.Fatalf("snapshot metadata was not deep-copied: first=%p second=%p value=%d",
			events[0].RequestedProtocols, again[0].RequestedProtocols, *again[0].RequestedProtocols)
	}

	if got := log.snapshot("", "", false); len(got) != 0 {
		t.Fatalf("ordinary empty owner received %d records", len(got))
	}
	if got := log.snapshot("alice-secret-owner", "vm-b", false); len(got) != 0 {
		t.Fatalf("VM filter returned unrelated records: %#v", got)
	}
	if got := log.snapshot("ignored", "", true); len(got) != 3 {
		t.Fatalf("administrator snapshot has %d records, want 3", len(got))
	}

	encoded, err := json.Marshal(log.snapshot("ignored", "", true))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "alice-secret-owner") || strings.Contains(string(encoded), `"owner"`) {
		t.Fatalf("serialized diagnostics exposed an owner: %s", encoded)
	}
}

func TestRDPDiagnosticAttemptEvents(t *testing.T) {
	t.Parallel()

	var log rdpDiagnosticLog
	attempt := log.begin()
	attempt.owner = "alice"
	attempt.stage("credential_authentication")
	attempt.record("authenticated")
	attempt.stage("session_proxy")
	attempt.finish(io.EOF)

	events := log.snapshot("alice", "", false)
	if len(events) != 4 {
		t.Fatalf("attempt emitted %d events, want 4", len(events))
	}
	wantOutcomes := []string{"progress", "authenticated", "progress", "closed"}
	for index, want := range wantOutcomes {
		if events[index].Outcome != want {
			t.Errorf("event %d outcome = %q, want %q", index, events[index].Outcome, want)
		}
		if events[index].At.IsZero() {
			t.Errorf("event %d has zero timestamp", index)
		}
	}
	if events[3].Error != "" {
		t.Errorf("normal proxy EOF error = %q, want empty", events[3].Error)
	}

	unknown := log.begin()
	unknown.owner = "alice"
	unknown.stage("client supplied secret stage")
	unknown.record("client supplied secret outcome")
	unknown.finish(fmt.Errorf("client supplied secret error: %w", io.ErrNoProgress))
	events = log.snapshot("alice", "", false)
	for _, event := range events[len(events)-3:] {
		encoded, err := json.Marshal(event)
		if err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(encoded), "client supplied secret") || event.Stage != "unknown" {
			t.Fatalf("unknown diagnostic input was exposed: %s", encoded)
		}
	}
}

func TestRDPDiagnosticErrorRedaction(t *testing.T) {
	t.Parallel()

	timeout := diagnosticTimeoutError{"timeout-secret"}
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"canceled", fmt.Errorf("cancel-secret: %w", context.Canceled), "operation canceled"},
		{"deadline", fmt.Errorf("deadline-secret: %w", context.DeadlineExceeded), "operation timed out"},
		{"authentication", fmt.Errorf("password-secret: %w", ErrAuthentication), "authentication failed"},
		{"invalid", fmt.Errorf("wire-secret: %w", ErrInvalid), "invalid request or protocol data"},
		{"not found", fmt.Errorf("vm-secret: %w", ErrNotFound), "resource not found"},
		{"conflict", fmt.Errorf("state-secret: %w", ErrConflict), "resource conflict"},
		{"startup", fmt.Errorf("worker-secret: %w", ErrStartupFailed), "backend startup failed"},
		{"capacity", fmt.Errorf("capacity-secret: %w", ErrMemoryCapacity), "insufficient backend capacity"},
		{
			"certificate",
			fmt.Errorf("host-secret: %w", x509.HostnameError{Host: "private-host-secret"}),
			"TLS certificate validation failed",
		},
		{"certificate pointer", &x509.HostnameError{Host: "private-host-secret"}, "TLS certificate validation failed"},
		{"reset", fmt.Errorf("peer-secret: %w", syscall.ECONNRESET), "connection reset"},
		{"closed", fmt.Errorf("stream-secret: %w", net.ErrClosed), "connection closed"},
		{"eof", fmt.Errorf("stream-secret: %w", io.EOF), "connection closed"},
		{"timeout", timeout, "network timeout"},
		{"unknown", fmt.Errorf("unknown-secret: %w", io.ErrNoProgress), "internal connection error"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got := rdpDiagnosticError(test.err)
			if got != test.want {
				t.Fatalf("category = %q, want %q", got, test.want)
			}
			if strings.Contains(got, "secret") {
				t.Fatalf("category exposed private error text: %q", got)
			}
		})
	}

	var log rdpDiagnosticLog
	attempt := log.begin()
	attempt.owner = "alice"
	attempt.stage("frontend_tls")
	attempt.finish(fmt.Errorf("raw certificate bytes and password-secret: %w", io.ErrNoProgress))
	encoded, err := json.Marshal(log.snapshot("alice", "", false))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(encoded), "password-secret") || strings.Contains(string(encoded), "raw certificate") {
		t.Fatalf("event JSON exposed raw error text: %s", encoded)
	}
}

func TestRDPDiagnosticLogConcurrent(t *testing.T) {
	t.Parallel()

	var log rdpDiagnosticLog
	const attempts = 128
	var wait sync.WaitGroup
	ids := make(chan uint64, attempts)
	for index := 0; index < attempts; index++ {
		wait.Add(1)
		go func(index int) {
			defer wait.Done()
			attempt := log.begin()
			attempt.owner = "alice"
			attempt.event.VMID = fmt.Sprintf("vm-%d", index%4)
			ids <- attempt.event.AttemptID
			attempt.stage("connection_request")
			attempt.finish(nil)
		}(index)
	}
	wait.Wait()
	close(ids)

	seen := make(map[uint64]bool, attempts)
	for id := range ids {
		if id == 0 || id > attempts || seen[id] {
			t.Fatalf("invalid or duplicate attempt ID %d", id)
		}
		seen[id] = true
	}
	if len(seen) != attempts {
		t.Fatalf("allocated %d unique IDs, want %d", len(seen), attempts)
	}
	if got := log.snapshot("alice", "", false); len(got) != attempts*2 {
		t.Fatalf("concurrent log has %d events, want %d", len(got), attempts*2)
	}
}

type diagnosticTimeoutError struct{ text string }

func (e diagnosticTimeoutError) Error() string { return e.text }
func (diagnosticTimeoutError) Timeout() bool   { return true }
func (diagnosticTimeoutError) Temporary() bool { return true }
