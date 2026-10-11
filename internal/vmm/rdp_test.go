package vmm

import (
	"errors"
	"net"
	"strings"
	"testing"
	"time"
)

func TestRDPPerformanceConfigBeforeMachineInit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		config Config
		want   error
	}{
		{name: "negative-threads", config: Config{RDPH264: true, RDPH264Threads: -1}, want: errRDPH264ThreadsConfig},
		{name: "excessive-threads", config: Config{RDPH264: true, RDPH264Threads: 17}, want: errRDPH264ThreadsConfig},
		{
			name:   "threads-without-h264",
			config: Config{RDP: "127.0.0.1:0", RDPH264Threads: 2},
			want:   errRDPH264ThreadsConfig,
		},
		{
			name:   "invalid-encoder",
			config: Config{RDPH264: true, RDPH264Encoder: "invalid"},
			want:   errRDPH264EncoderConfig,
		},
		{
			name:   "encoder-without-h264",
			config: Config{RDP: "127.0.0.1:0", RDPH264Encoder: "nvenc"},
			want:   errRDPH264EncoderConfig,
		},
		{
			name:   "software-without-h264",
			config: Config{RDP: "127.0.0.1:0", RDPH264Encoder: "software"},
			want:   errRDPH264EncoderConfig,
		},
		{name: "stats-without-rdp", config: Config{RDPStats: true}, want: errRDPStatsConfig},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.config.Dev = "/missing-kvm-device"
			v := New(tc.config)
			if err := v.Init(); !errors.Is(err, tc.want) {
				t.Fatalf("got %v, want configuration error %v before opening KVM", err, tc.want)
			}
			if v.Machine != nil {
				t.Fatal("invalid RDP configuration created a machine")
			}
		})
	}
}

func TestRDPEncoderConfiguration(t *testing.T) {
	t.Parallel()
	for _, backend := range []string{"", "software", "nvenc", "auto"} {
		name := backend
		if name == "" {
			name = "default"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := New(Config{RDP: "127.0.0.1:0", RDPH264: true, RDPH264Encoder: backend})
			if err := v.validateRDPPerformanceConfig(); err != nil {
				t.Fatalf("valid encoder configuration rejected: %v", err)
			}
		})
	}
}

func TestRDPDisplayReceivesEncoderConfiguration(t *testing.T) {
	t.Parallel()
	// Calling display directly bypasses VMM validation. An invalid backend must
	// still reach the RDP constructor and fail before it opens the listener.
	v := New(Config{RDP: "invalid-address", RDPH264: true, RDPH264Encoder: "invalid"})
	display, err := v.display(nil)
	if display != nil {
		_ = display.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "H.264 encoder") {
		t.Fatalf("encoder configuration was not passed to RDP: %v", err)
	}
}

func TestRDPDisplayConfiguration(t *testing.T) {
	t.Parallel()
	for _, both := range []bool{false, true} {
		name := "rdp"
		if both {
			name = "rdp-and-vnc"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			v := New(Config{RDP: "127.0.0.1:0"})
			want := 1
			if both {
				v.VNC, want = "127.0.0.1:0", 2
			}
			display, err := v.display(nil)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = display.Close() })
			if !v.hasRemoteDisplay() || len(v.consoles) != want || v.rdpDisplay == nil || v.serialOutput != nil {
				t.Fatalf("incomplete remote console configuration: %+v", v)
			}
		})
	}
}

func TestRDPFailureClosesVNCListener(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := listener.Addr().String()
	_ = listener.Close()
	v := New(Config{VNC: addr, RDP: "127.0.0.1:0", RDPCert: "missing.crt", RDPKey: "missing.key"})
	if _, err := v.display(nil); err == nil {
		t.Fatal("invalid certificate unexpectedly accepted")
	}
	if v.vncDisplay != nil || v.rdpDisplay != nil || len(v.consoles) != 0 {
		t.Fatal("partially constructed display retained")
	}
	rebound, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("VNC listener leaked after RDP failure: %v", err)
	}
	_ = rebound.Close()
}

func TestSerialMirrorDoesNotBlockRemoteInput(t *testing.T) {
	t.Parallel()
	serial := make(chan byte, 1)
	serial <- 'x'
	primary := &recordedKeyInput{keys: make(chan uint32, 1)}
	mirror := &serialMirrorInput{primary: primary, serial: serial}
	done := make(chan struct{})
	go func() {
		mirror.KeyEvent(true, 'a')
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		<-serial // Release a blocked writer before reporting the regression.
		t.Fatal("full serial buffer blocked remote input")
	}
	if got := <-primary.keys; got != 'a' {
		t.Fatalf("primary keyboard event: %d", got)
	}
	if got := <-serial; got != 'x' {
		t.Fatalf("buffered serial byte changed: %q", got)
	}
}

type recordedKeyInput struct{ keys chan uint32 }

func (r *recordedKeyInput) KeyEvent(_ bool, keysym uint32)    { r.keys <- keysym }
func (r *recordedKeyInput) PointerEvent(_ uint8, _, _ uint16) {}
