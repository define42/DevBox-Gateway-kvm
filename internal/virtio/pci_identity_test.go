package virtio

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
)

func TestModernPCIIdentitiesSupportFirmwareDiscovery(t *testing.T) {
	t.Parallel()

	// OVMF's Virtio10Dxe applies the non-transitional PCI discovery rules.
	// A VGA-class 1050 device is deliberately left to its legacy VGA driver,
	// which is unsuitable for our virtio-only GPU.
	for _, tc := range []struct {
		name   string
		header pci.DeviceHeader
	}{
		{name: "block", header: (&Blk{}).GetDeviceHeader()},
		{name: "network", header: (&Net{}).GetDeviceHeader()},
		{name: "graphics", header: (&GPU{}).GetDeviceHeader()},
		{name: "input", header: (&InputDevice{}).GetDeviceHeader()},
		{name: "sound", header: (&Sound{}).GetDeviceHeader()},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			h := tc.header
			if h.VendorID != 0x1af4 || h.DeviceID < 0x1040 || h.DeviceID > 0x107f ||
				h.RevisionID < 1 || h.SubsystemID < 0x40 || h.Status&0x10 == 0 {
				t.Fatalf("firmware cannot discover modern virtio PCI device: %+v", h)
			}
			if h.DeviceID == 0x1050 && h.ClassCode == 3 && h.Subclass == 0 {
				t.Fatal("virtio-only GPU advertises unsupported legacy VGA compatibility")
			}
		})
	}
}
