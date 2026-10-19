package vmm

import (
	"errors"
	"strings"
	"testing"
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
			config: Config{RDPH264Threads: 2},
			want:   errRDPH264ThreadsConfig,
		},
		{
			name:   "invalid-encoder",
			config: Config{RDPH264: true, RDPH264Encoder: "invalid"},
			want:   errRDPH264EncoderConfig,
		},
		{
			name:   "encoder-without-h264",
			config: Config{RDPH264Encoder: "nvenc"},
			want:   errRDPH264EncoderConfig,
		},
		{
			name:   "software-without-h264",
			config: Config{RDPH264Encoder: "software"},
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
			v := New(withGatewayRDP(t, Config{RDPH264: true, RDPH264Encoder: backend}))
			if err := v.validateRDPPerformanceConfig(); err != nil {
				t.Fatalf("valid encoder configuration rejected: %v", err)
			}
		})
	}
}

func TestRDPDisplayReceivesEncoderConfiguration(t *testing.T) {
	t.Parallel()
	// Calling display directly bypasses VMM validation. An invalid backend must
	// still reach the RDP constructor and fail before it serves the listener.
	v := New(withGatewayRDP(t, Config{RDPH264: true, RDPH264Encoder: "invalid"}))
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
	v := New(gatewayRDPConfig(t))
	display, err := v.display(nil)
	if err != nil {
		t.Fatal(err)
	}
	if display == nil {
		t.Fatal("inherited listener did not create an RDP display")
	}
	t.Cleanup(func() { _ = display.Close() })
	if display != v.rdpDisplay {
		t.Fatal("configured RDP display was not selected as the machine display")
	}
}

func TestDisplayWithoutGatewayListener(t *testing.T) {
	t.Parallel()
	v := New(Config{})
	display, err := v.display(nil)
	if display != nil {
		_ = display.Close()
	}
	if err != nil || display != nil || v.rdpDisplay != nil {
		t.Fatalf("headless configuration created a display: %v, %v", display, err)
	}
}
