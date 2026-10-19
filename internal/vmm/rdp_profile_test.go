package vmm

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

func TestRDPProfileConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		enabled bool
		profile string
	}{
		{name: "default without RDP"},
		{name: "default with RDP", enabled: true},
		{name: "office", enabled: true, profile: rdp.ProfileOffice},
		{name: "smooth", enabled: true, profile: rdp.ProfileSmooth},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Config{RDPProfile: test.profile}
			if test.enabled {
				config = withGatewayRDP(t, config)
			}
			vm := New(config)
			if err := vm.validateRDPPerformanceConfig(); err != nil {
				t.Fatalf("valid RDP profile rejected: %v", err)
			}
		})
	}
}

func TestRDPProfileValidationBeforeMachineInit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		enabled bool
		profile string
	}{
		{name: "unknown", enabled: true, profile: "fast"},
		{name: "unknown without RDP", profile: "fast"},
		{name: "office without RDP", profile: rdp.ProfileOffice},
		{name: "smooth without RDP", profile: rdp.ProfileSmooth},
		{name: "wrong case", enabled: true, profile: "Office"},
		{name: "whitespace", enabled: true, profile: "office "},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := Config{Dev: "/missing-kvm-device", RDPProfile: test.profile}
			if test.enabled {
				config = withGatewayRDP(t, config)
			}
			vm := New(config)
			if err := vm.Init(); !errors.Is(err, errRDPProfileConfig) {
				t.Fatalf("got %v, want profile configuration error before opening KVM", err)
			}
			if vm.Machine != nil {
				t.Fatal("invalid RDP profile created a machine")
			}
		})
	}
}

func TestRDPDisplayReceivesProfileConfiguration(t *testing.T) {
	t.Parallel()
	// Bypass VMM validation to prove that the profile reaches the display
	// constructor, where it must fail before serving the private listener.
	vm := New(withGatewayRDP(t, Config{RDPProfile: "unknown"}))
	display, err := vm.display(nil)
	if display != nil {
		_ = display.Close()
	}
	if !errors.Is(err, rdp.ErrProfile) {
		t.Fatalf("RDP profile was not passed to the display constructor: %v", err)
	}
}
