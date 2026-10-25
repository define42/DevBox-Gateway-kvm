package vmm

import (
	"errors"
	"testing"
)

func TestRDPPerformanceConfigBeforeMachineInit(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		config Config
		want   error
	}{
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

func TestInitDisplayWithoutGatewayListener(t *testing.T) {
	t.Parallel()
	v := New(Config{})
	if err := v.initDisplay(nil); err != nil {
		t.Fatalf("headless display initialization: %v", err)
	}
	if v.rdpDisplay != nil {
		t.Fatal("headless initialization created an RDP display")
	}
}

func TestDisplayRequiresGatewayListener(t *testing.T) {
	t.Parallel()
	v := New(Config{})
	display, err := v.display(nil)
	if display != nil {
		_ = display.Close()
		t.Fatal("display created without an RDP listener")
	}
	if !errors.Is(err, errRDPListenerConfig) {
		t.Fatalf("display without an RDP listener: got %v, want %v", err, errRDPListenerConfig)
	}
}
