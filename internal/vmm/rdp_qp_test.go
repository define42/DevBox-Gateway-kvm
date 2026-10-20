package vmm

import (
	"errors"
	"strings"
	"testing"
)

func TestRDPH264QPBeforeMachineInit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		qp   int
		h264 bool
	}{
		{name: "negative", qp: -1, h264: true},
		{name: "excessive", qp: 52, h264: true},
		{name: "minimum without H264", qp: 0},
		{name: "default without H264", qp: 20},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			vm := New(Config{Dev: "/missing-kvm-device", RDPH264: test.h264, RDPH264QP: &test.qp})
			if err := vm.Init(); !errors.Is(err, errRDPH264QPConfig) {
				t.Fatalf("QP validation must precede KVM initialization: %v", err)
			}
			if vm.Machine != nil {
				t.Fatal("invalid QP configuration created a machine")
			}
		})
	}
}

func TestRDPH264QPConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		config Config
	}{
		{name: "headless default"},
		{name: "H264 default", config: Config{RDPH264: true}},
		{name: "minimum", config: Config{RDPH264: true, RDPH264QP: new(0)}},
		{name: "maximum", config: Config{RDPH264: true, RDPH264QP: new(51)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			vm := New(test.config)
			if err := vm.validateRDPPerformanceConfig(); err != nil {
				t.Fatalf("valid QP configuration rejected: %v", err)
			}
		})
	}
}

func TestRDPH264QPDoesNotRetainCallerPointer(t *testing.T) {
	t.Parallel()
	qp := 0
	vm := New(Config{RDPH264: true, RDPH264QP: &qp})
	qp = 52
	if err := vm.validateRDPPerformanceConfig(); err != nil {
		t.Fatalf("caller mutation changed the worker QP: %v", err)
	}
	if vm.RDPH264QP == nil || *vm.RDPH264QP != 0 {
		t.Fatalf("worker did not retain explicit QP 0: %v", vm.RDPH264QP)
	}
}

func TestRDPDisplayReceivesQPConfiguration(t *testing.T) {
	t.Parallel()
	// Bypass VMM validation to verify the selected value reaches the display.
	vm := New(withGatewayRDP(t, Config{RDPH264: true, RDPH264QP: new(52)}))
	display, err := vm.display(nil)
	if display != nil {
		_ = display.Close()
	}
	if err == nil || !strings.Contains(err.Error(), "H.264 QP") {
		t.Fatalf("QP configuration was not passed to the display constructor: %v", err)
	}
}
