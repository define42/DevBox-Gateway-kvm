package main

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/buildinfo"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

func TestConfiguration(t *testing.T) {
	t.Parallel()
	args := []string{"-mode", "bitmap", "-state-dir", "private", "-output", "artifacts"}
	config, err := parseConfiguration(args)
	if err != nil || config.mode != "bitmap" || config.timeout != 150*time.Second {
		t.Fatalf("configuration=%+v error=%v", config, err)
	}
	for _, extra := range [][]string{
		{"-listen", "0.0.0.0:3389"},
		{"-mode", "auto"},
		{"-mode", ""},
		{"-state-dir", ""},
		{"-output", ""},
		{"-timeout", "1s"},
		{"-timeout", "11m"},
		{"extra"},
	} {
		if _, err := parseConfiguration(append(append([]string(nil), args...), extra...)); !errors.Is(err, errConfiguration) {
			t.Fatalf("accepted invalid arguments %v: %v", extra, err)
		}
	}
	config, err = parseConfiguration(append(args, "-mode", "h264", "-timeout", "2m"))
	if err != nil || config.mode != "h264" || config.timeout != 2*time.Minute {
		t.Fatalf("h264 configuration=%+v error=%v", config, err)
	}
}

func TestCertificateAndReadiness(t *testing.T) {
	t.Parallel()
	now := time.Now()
	serverTLS, der, err := newCertificate(now)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	if serverTLS.MinVersion != tls.VersionTLS12 || len(serverTLS.Certificates) != 1 ||
		serverTLS.Certificates[0].PrivateKey == nil || !bytes.Equal(serverTLS.Certificates[0].Certificate[0], der) {
		t.Fatal("unexpected TLS configuration")
	}
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	for _, name := range []string{"localhost", "127.0.0.1"} {
		if _, err := certificate.Verify(x509.VerifyOptions{Roots: roots, DNSName: name, CurrentTime: now}); err != nil {
			t.Fatalf("verify %s: %v", name, err)
		}
	}
	if err := certificate.VerifyHostname("example.invalid"); err == nil {
		t.Fatal("certificate unexpectedly valid for external host")
	}
	config := configuration{mode: "h264", stateDir: t.TempDir()}
	if err := publishReadiness(config, der, 12345); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(config.stateDir, "ready.json"))
	if err != nil {
		t.Fatal(err)
	}
	var ready readiness
	if err := json.Unmarshal(data, &ready); err != nil {
		t.Fatal(err)
	}
	if ready.Address != "127.0.0.1:12345" || ready.Port != 12345 || ready.Mode != "h264" {
		t.Fatalf("readiness=%+v", ready)
	}
	publicCertificate, err := os.ReadFile(filepath.Join(config.stateDir, "server.cer"))
	if err != nil || !bytes.Equal(publicCertificate, der) {
		t.Fatalf("public certificate differs: %v", err)
	}
	files, err := os.ReadDir(config.stateDir)
	if err != nil || len(files) != 2 {
		t.Fatalf("unexpected state files (private key must stay in memory): %v %v", files, err)
	}
	if err := publishReadiness(config, der, 54321); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrote existing readiness: %v", err)
	}
	if err := publishReadiness(config, der, 0); !errors.Is(err, errConfiguration) {
		t.Fatalf("accepted invalid port: %v", err)
	}
}

func TestAtomicJSONRefusesStaleReports(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "server.json")
	if err := writeJSON(path, map[string]int{"version": 1}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSON(path, map[string]int{"version": 2}); !errors.Is(err, os.ErrExist) {
		t.Fatalf("existing file replaced: %v", err)
	}
	data, err := os.ReadFile(path)
	if err != nil || !bytes.Contains(data, []byte(`"version": 1`)) {
		t.Fatalf("old report modified: %q %v", data, err)
	}
}

func TestF8InputTransitions(t *testing.T) {
	t.Parallel()
	state := &inputState{}
	decoder := rdp.InputDecoder{}
	for index, packet := range [][]byte{
		{4, 0, 0x42},          // F8 down.
		{4, 0, 0x42},          // Repeated down must not toggle again.
		{4, 1, 0x42},          // Up must not toggle.
		{4, 0, 0x41},          // F7 is unrelated.
		{8, 0, 0x42, 1, 0x42}, // A full F8 press in one packet.
	} {
		events, err := decoder.Decode(packet, true)
		if err != nil {
			t.Fatal(err)
		}
		state.apply(events)
		want := uint64(1)
		if index == 4 {
			want = 2
		}
		if got := state.transitions.Load(); got != want {
			t.Fatalf("packet %d: got %d transitions, want %d", index, got, want)
		}
	}
}

func TestControlPDUs(t *testing.T) {
	t.Parallel()
	state := &inputState{}
	state.control(nil)
	packet := make([]byte, 22)
	packet[2], packet[14] = 7, 33
	state.control(packet)
	if !state.refresh.Swap(false) {
		t.Fatal("refresh rectangle did not request a refresh")
	}
	packet[14], packet[18] = 35, 0
	state.control(packet)
	if !state.suppressed.Load() {
		t.Fatal("suppression ignored")
	}
	packet[18] = 1
	state.control(packet)
	if state.suppressed.Load() || !state.refresh.Load() {
		t.Fatal("restoration must clear suppression and request a refresh")
	}
}

func TestFixtureMatchesClientQuadrants(t *testing.T) {
	t.Parallel()
	colors := []color.RGBA{{220, 40, 40, 255}, {40, 200, 60, 255}, {40, 80, 220, 255}, {220, 190, 40, 255}}
	frame := image.NewRGBA(image.Rect(0, 0, fixtureWidth, fixtureHeight))
	for layout := range uint64(2) {
		for sequence := range uint64(3) {
			paintFrame(frame, layout, sequence)
			for quadrant := range 4 {
				x := fixtureWidth * (1 + 2*(quadrant%2)) / 4
				y := fixtureHeight * (1 + 2*(quadrant/2)) / 4
				want := colors[(uint64(quadrant)+2*layout)%4]
				for dy := -3; dy <= 3; dy++ {
					for dx := -3; dx <= 3; dx++ {
						if got := frame.RGBAAt(x+dx, y+dy); got != want {
							t.Fatalf("layout=%d quadrant=%d: got %v want %v", layout, quadrant, got, want)
						}
					}
				}
			}
			if got := frame.RGBAAt(0, 0).R; got != uint8(sequence%2)*255 {
				t.Fatal("heartbeat did not advance")
			}
		}
	}
}

func TestFixtureBitmapFraming(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{16, 24, 32} {
		t.Run(strconv.Itoa(depth), func(t *testing.T) {
			t.Parallel()
			encoder, err := rdp.NewBitmapEncoder(fixtureWidth, fixtureHeight, depth)
			if err != nil {
				t.Fatal(err)
			}
			frame := image.NewRGBA(image.Rect(0, 0, fixtureWidth, fixtureHeight))
			paintFrame(frame, 0, 0)
			tiles := 0
			validate := func(packet []byte) error {
				tiles++
				if len(packet) < 22 || binary.LittleEndian.Uint16(packet) != 1 ||
					binary.LittleEndian.Uint16(packet[2:]) != 1 ||
					int(binary.LittleEndian.Uint16(packet[16:])) != depth ||
					binary.LittleEndian.Uint16(packet[18:]) != 0 ||
					int(binary.LittleEndian.Uint16(packet[20:])) != len(packet)-22 {
					t.Fatal("invalid fixture bitmap framing")
				}

				return nil
			}
			if err := encoder.WriteFrame(frame, false, validate); err != nil || tiles != 192 {
				t.Fatalf("initial frame: %d tiles, %v", tiles, err)
			}
			paintFrame(frame, 0, 1)
			tiles = 0
			if err := encoder.WriteFrame(frame, false, validate); err != nil || tiles != 1 {
				t.Fatalf("heartbeat: %d tiles, %v", tiles, err)
			}
		})
	}
}

func TestFrameWriterSelectsOnlyRequestedEncoder(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"bitmap", "h264"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			writer, err := newFrameWriter(mode, &rdp.Session{BitsPerPixel: 32})
			if err != nil {
				t.Fatal(err)
			}
			defer writer.close()
			if mode == "bitmap" {
				if writer.name() != "bitmap" || writer.bitmap == nil || writer.graphics != nil {
					t.Fatal("bitmap mode selected H.264")
				}
			} else if writer.name() != "software" || writer.graphics == nil || writer.bitmap != nil {
				t.Fatal("H.264 mode has a bitmap fallback")
			}
		})
	}
}

func TestCompletionRequiresProtocolEvidence(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		mode, actual  string
		ready         bool
		frames, input uint64
		duration      time.Duration
		err           error
		completed     bool
	}{
		{"bitmap", "bitmap", "bitmap", false, 60, 1, minimumSession, io.EOF, true},
		{
			"wrapped_graceful_disconnect", "bitmap", "bitmap", false, 60, 1, minimumSession,
			fmt.Errorf("session_read: %w", io.EOF), true,
		},
		{"h264", "h264", "software", true, 60, 1, minimumSession, io.EOF, true},
		{"reset", "bitmap", "bitmap", false, 60, 1, minimumSession, syscall.ECONNRESET, true},
		{"early", "bitmap", "bitmap", false, 60, 1, minimumSession - time.Nanosecond, io.EOF, false},
		{"no_input", "bitmap", "bitmap", false, 60, 0, minimumSession, io.EOF, false},
		{"no_frames", "bitmap", "bitmap", false, 0, 1, minimumSession, io.EOF, false},
		{"wrong_encoder", "h264", "bitmap", false, 60, 1, minimumSession, io.EOF, false},
		{"graphics_not_ready", "h264", "software", false, 60, 1, minimumSession, io.EOF, false},
		{"protocol_failure", "bitmap", "bitmap", false, 60, 1, minimumSession, rdp.ErrInvalidInput, false},
		{"truncated_packet", "bitmap", "bitmap", false, 60, 1, minimumSession, io.ErrUnexpectedEOF, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			result := report{
				Mode: test.mode, Outcome: "failed", ActualEncoder: test.actual,
				GraphicsReady: test.ready, FramesSent: test.frames,
			}
			finishSession(&result, test.input, test.duration, test.err)
			if got := result.Outcome == "completed"; got != test.completed {
				t.Fatalf("result=%+v", result)
			}
			if result.Outcome == "passed" {
				t.Fatal("server must not claim client rendering has passed")
			}
		})
	}
}

func TestReportJSONContract(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"bitmap", "h264"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			actualEncoder := "bitmap"
			if mode == "h264" {
				actualEncoder = "software"
			}
			started := time.Date(2026, time.October, 29, 14, 0, 0, 0, time.UTC)
			result := report{
				Version: 1, Mode: mode, ActualEncoder: actualEncoder, Outcome: "failed",
				StartedAt: started, EndedAt: started.Add(35 * time.Second), SessionSeconds: 35,
				FramesSent: 70, InputTransitions: 1, GraphicsReady: mode == "h264",
				Application: buildinfo.Info{Version: "test-build", Commit: "test-commit", GoVersion: "go1.26.0"},
			}
			finishSession(&result, result.InputTransitions, 35*time.Second, io.EOF)
			path := filepath.Join(t.TempDir(), "server.json")
			if err := writeJSON(path, result); err != nil {
				t.Fatal(err)
			}
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]any
			if err := json.Unmarshal(data, &fields); err != nil {
				t.Fatal(err)
			}
			for key, want := range map[string]any{
				"version": float64(1), "mode": mode, "actualEncoder": actualEncoder, "outcome": "completed",
				"framesSent": float64(70), "inputTransitions": float64(1), "sessionSeconds": float64(35),
				"graphicsReady": mode == "h264", "stage": "session", "reason": "protocol_activity_and_peer_close_observed",
				"startedAt": "2026-10-29T14:00:00Z", "endedAt": "2026-10-29T14:00:35Z",
				"outstandingAcks": float64(0), "ackMs": float64(0),
			} {
				if got, present := fields[key]; !present || got != want {
					t.Errorf("serialized %s=%v, want %v", key, got, want)
				}
			}
			if _, wrongVersionField := fields["schema"]; wrongVersionField {
				t.Error("evidence uses version, not schema")
			}
			// `go test -run TestReportJSONContract -v` prints complete examples
			// for PowerShell/schema maintenance, without adding a runtime flag.
			t.Logf("complete successful server evidence (still requires client success):\n%s", data)
		})
	}
}

func TestTimeoutCannotComplete(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	result := report{Mode: "bitmap", ActualEncoder: "bitmap", Outcome: "failed", FramesSent: 60}
	finishOrTimeout(ctx, &result, 1, time.Minute, net.ErrClosed)
	if result.Outcome != "failed" || result.Reason != "session_timeout" {
		t.Fatalf("timeout incorrectly completed: %+v", result)
	}
}

func TestRunRejectsNonRDPConnection(t *testing.T) {
	t.Parallel()
	config := configuration{mode: "bitmap", stateDir: t.TempDir(), output: t.TempDir(), timeout: 8 * time.Second}
	results := make(chan report, 1)
	go func() { results <- run(config) }()
	ready := awaitReadiness(t, config.stateDir)
	host, _, err := net.SplitHostPort(ready.Address)
	if err != nil || host != "127.0.0.1" {
		t.Fatalf("non-loopback address published: %q %v", ready.Address, err)
	}
	conn, err := net.DialTimeout("tcp4", ready.Address, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte{0, 0, 0, 0}); err != nil {
		t.Fatal(err)
	}
	select {
	case result := <-results:
		if result.Outcome != "failed" || result.Reason != "handshake_failed" || result.FramesSent != 0 ||
			result.InputTransitions != 0 || result.EndedAt.Before(result.StartedAt) {
			t.Fatalf("unexpected report: %+v", result)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("invalid handshake did not finish promptly")
	}
	if second, err := net.DialTimeout("tcp4", ready.Address, time.Second); err == nil {
		_ = second.Close()
		t.Fatal("fixture accepted a second connection")
	}
}

func awaitReadiness(t *testing.T, stateDir string) readiness {
	t.Helper()
	timeout := time.NewTimer(5 * time.Second)
	defer timeout.Stop()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		data, err := os.ReadFile(filepath.Join(stateDir, "ready.json"))
		if err == nil {
			var ready readiness
			if err := json.Unmarshal(data, &ready); err != nil {
				t.Fatalf("readiness not published atomically: %v", err)
			}

			return ready
		}
		if !errors.Is(err, os.ErrNotExist) {
			t.Fatal(err)
		}
		select {
		case <-timeout.C:
			t.Fatal("fixture did not become ready")
		case <-ticker.C:
		}
	}
}
