package rdp

import (
	"errors"
	"io"
	"net"
	"testing"
	"time"
)

var (
	errDiagnosticTestSecret   = errors.New("private protocol detail")
	errDiagnosticTestUnstaged = errors.New("unstaged")
)

func TestAcceptDiagnosticStageTracksEarlyActivationEOF(t *testing.T) {
	t.Parallel()
	serverTLS, clientTLS := protocolTestTLS(t)
	server, client := net.Pipe()
	t.Cleanup(func() {
		_ = server.Close()
		_ = client.Close()
	})
	if err := client.SetDeadline(time.Now().Add(5 * time.Second)); err != nil {
		t.Fatal(err)
	}
	stages := make(chan string, 16)
	result := make(chan error, 1)
	go func() {
		_, err := Accept(server, serverTLS, 1024, 768, WithDiagnosticStage(func(stage string) {
			stages <- stage
		}))
		result <- err
	}()

	secured := negotiateSessionFixture(t, client, clientTLS)
	writeMCSFixture(t, secured, connectInitialFixture())
	if response := readMCSFixture(t, secured); len(response) < 2 || response[0] != 0x7f || response[1] != 0x66 {
		t.Fatalf("expected MCS Connect Response, got %x", response)
	}
	if err := secured.Close(); err != nil && !errors.Is(err, io.ErrClosedPipe) {
		t.Fatal(err)
	}
	if err := <-result; err == nil {
		t.Fatal("activation survived client EOF")
	}
	close(stages)
	var got []string
	for stage := range stages {
		got = append(got, stage)
	}
	want := []string{
		diagnosticStageNegotiation,
		diagnosticStageTLS,
		diagnosticStageMCSConnect,
		diagnosticStageActivationErectDomain,
	}
	if len(got) != len(want) {
		t.Fatalf("diagnostic stages = %q, want %q", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("diagnostic stages = %q, want %q", got, want)
		}
	}
}

func TestDiagnosticStageErrorPreservesIdentity(t *testing.T) {
	t.Parallel()
	sentinel := errDiagnosticTestSecret
	err := withDiagnosticStage(diagnosticStageGraphics, sentinel)
	if !errors.Is(err, sentinel) {
		t.Fatal("diagnostic wrapper changed error identity")
	}
	stage, ok := DiagnosticStage(err)
	if !ok || stage != diagnosticStageGraphics {
		t.Fatalf("diagnostic stage = %q, %v", stage, ok)
	}
	got := withDiagnosticStage(diagnosticStageSessionRead, err)
	if stage, ok := DiagnosticStage(got); !ok || stage != diagnosticStageGraphics {
		t.Fatalf("outer cleanup stage replaced originating stage with %q", stage)
	}
	if stage, ok := DiagnosticStage(errDiagnosticTestUnstaged); ok || stage != "" {
		t.Fatalf("unstaged error reported as %q", stage)
	}
}
