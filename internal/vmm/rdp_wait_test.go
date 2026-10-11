package vmm

import (
	"errors"
	"testing"
)

func TestRDPWaitRequiresListenerBeforeMachineInit(t *testing.T) {
	t.Parallel()
	v := New(Config{Dev: "/nonexistent-kvm", RDPWait: true})
	if err := v.Init(); !errors.Is(err, errRDPWaitConfig) {
		t.Fatalf("RDP wait without listener reached machine initialization: %v", err)
	}
	if v.Machine != nil {
		t.Fatal("invalid configuration created a machine")
	}
}

func TestRDPWaitRejectsUninitializedDisplayBeforeBoot(t *testing.T) {
	t.Parallel()
	v := New(Config{RDP: "127.0.0.1:0", RDPWait: true})
	if err := v.BootContext(t.Context()); !errors.Is(err, errRDPWaitConfig) {
		t.Fatalf("RDP wait without an initialized display returned %v", err)
	}
}
