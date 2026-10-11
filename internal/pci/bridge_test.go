package pci_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

func TestGetDeviceHeader(t *testing.T) {
	t.Parallel()

	br := pci.NewBridge()
	expected := uint16(0x1237)
	actual := br.GetDeviceHeader().DeviceID

	if actual != expected {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}

	if got := br.GetDeviceHeader().Subclass; got != 0x00 {
		t.Fatalf("subclass: got %#x, want host bridge", got)
	}

	if got, want := br.GetDeviceHeader().SubsystemVendorID, uint16(0x1af4); got != want {
		t.Fatalf("subsystem vendor: got %#x, want %#x", got, want)
	}

	if got, want := br.GetDeviceHeader().SubsystemID, uint16(0x1100); got != want {
		t.Fatalf("subsystem id: got %#x, want %#x", got, want)
	}
}

func TestIOHanders(t *testing.T) {
	t.Parallel()

	expected := pci.ErrIONotPermit
	br := pci.NewBridge()

	if actual := br.Read(0x0, []byte{}); !errors.Is(actual, expected) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}

	if actual := br.Write(0x0, []byte{}); !errors.Is(actual, expected) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}

func TestGetIORange(t *testing.T) {
	t.Parallel()

	expected := uint64(0)
	actual := pci.NewBridge().Size()

	if actual != expected {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}
}
