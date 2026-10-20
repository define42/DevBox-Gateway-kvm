package vmm

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/machine"
	"github.com/define42/DevBox-Gateway-kvm/internal/virgl"
)

func TestGPU3DUnavailableBeforeKVMInit(t *testing.T) {
	t.Parallel()
	if virgl.Available() {
		t.Skip("this build includes virgl support")
	}
	vm := New(Config{Dev: "/missing-kvm-device", GPU3D: true})
	if err := vm.Init(); !errors.Is(err, virgl.ErrUnavailable) {
		t.Fatalf("Init error = %v, want unavailable renderer before opening KVM", err)
	}
	if vm.Machine != nil {
		t.Fatal("unavailable renderer created a machine")
	}
}

func TestHeadlessGPU3DUnavailableIsNotIgnored(t *testing.T) {
	t.Parallel()
	if virgl.Available() {
		t.Skip("this build includes virgl support")
	}
	// Exercise display wiring independently of the early Init validation.
	// A headless boot must still construct the requested GPU and report failure.
	vm := New(Config{GPU3D: true})
	if err := vm.initDisplay(&machine.Machine{}); !errors.Is(err, virgl.ErrUnavailable) {
		t.Fatalf("headless display initialization error = %v, want unavailable renderer", err)
	}
}
