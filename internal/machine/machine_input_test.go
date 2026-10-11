package machine

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/iodev"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

func TestUEFIInputAttachesFirmwareControllerAndAbsoluteTablet(t *testing.T) {
	t.Parallel()
	m := &Machine{pci: pci.New(), mem: make([]byte, 0x10000)}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	input := m.AddUEFIInput()
	if _, ok := input.(*virtio.FirmwareInput); !ok {
		t.Fatalf("UEFI input has no firmware/tablet router: %T", input)
	}
	if len(m.devices) != 1 {
		t.Fatalf("UEFI has %d legacy devices, want one PS/2 controller", len(m.devices))
	}
	if _, ok := m.devices[0].(*iodev.PS2Controller); !ok {
		t.Fatalf("firmware controller is %T", m.devices[0])
	}
	devices := m.pci.Devices()
	if len(devices) != 1 {
		t.Fatalf("UEFI attached %d PCI devices, want only the tablet", len(devices))
	}
	tablet, ok := devices[0].(*virtio.InputDevice)
	if !ok || tablet.GetDeviceHeader().BAR[0] != virtio.InputPointerMMIOBase {
		t.Fatal("UEFI absolute pointer is missing or uses the keyboard's BAR")
	}
	// The guest-visible device name distinguishes the absolute tablet from a
	// relative virtio mouse or a redundant second keyboard.
	tablet.WriteDeviceConfig(0, []byte{1, 0})
	var config [136]byte
	tablet.ReadDeviceConfig(0, config[:])
	if string(config[8:8+int(config[2])]) != "gokvm tablet" {
		t.Fatal("UEFI pointer is not the absolute tablet")
	}
}
