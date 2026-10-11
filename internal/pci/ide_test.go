package pci_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

func TestIDEControllerHeader(t *testing.T) {
	t.Parallel()

	hdr := pci.NewIDEController().GetDeviceHeader()
	if hdr.VendorID != 0x8086 || hdr.DeviceID != 0x7010 {
		t.Fatalf("id: got %#x:%#x, want 0x8086:0x7010", hdr.VendorID, hdr.DeviceID)
	}

	if hdr.ClassCode != 0x01 || hdr.Subclass != 0x01 || hdr.ProgIF != 0x00 {
		t.Fatalf("class: got %#x/%#x/%#x, want IDE compatibility mode", hdr.ClassCode, hdr.Subclass, hdr.ProgIF)
	}
}

func TestIDEControllerNoIORange(t *testing.T) {
	t.Parallel()

	dev := pci.NewIDEController()
	if got := dev.Size(); got != 0 {
		t.Fatalf("size: got %#x, want 0", got)
	}

	if err := dev.Read(0, nil); !errors.Is(err, pci.ErrIONotPermit) {
		t.Fatalf("read: got %v, want %v", err, pci.ErrIONotPermit)
	}
}
