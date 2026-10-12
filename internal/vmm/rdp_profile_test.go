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
		address string
		profile string
	}{
		{name: "default without RDP"},
		{name: "default with RDP", address: "127.0.0.1:0"},
		{name: "office", address: "127.0.0.1:0", profile: rdp.ProfileOffice},
		{name: "smooth", address: "127.0.0.1:0", profile: rdp.ProfileSmooth},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			vm := New(Config{RDP: test.address, RDPProfile: test.profile})
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
		address string
		profile string
	}{
		{name: "unknown", address: "127.0.0.1:0", profile: "fast"},
		{name: "unknown without RDP", profile: "fast"},
		{name: "office without RDP", profile: rdp.ProfileOffice},
		{name: "smooth without RDP", profile: rdp.ProfileSmooth},
		{name: "wrong case", address: "127.0.0.1:0", profile: "Office"},
		{name: "whitespace", address: "127.0.0.1:0", profile: "office "},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			vm := New(Config{Dev: "/missing-kvm-device", RDP: test.address, RDPProfile: test.profile})
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
	// constructor, where it must fail before the invalid listen address.
	vm := New(Config{RDP: "invalid-address", RDPProfile: "unknown"})
	display, err := vm.display(nil)
	if display != nil {
		_ = display.Close()
	}
	if !errors.Is(err, rdp.ErrProfile) {
		t.Fatalf("RDP profile was not passed to the display constructor: %v", err)
	}
}
