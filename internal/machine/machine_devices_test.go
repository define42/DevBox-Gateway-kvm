package machine

import (
	"bytes"
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

func TestMachinePeripheralRegistrationAndClose(t *testing.T) {
	t.Parallel()
	m := &Machine{pci: pci.New(), mem: make([]byte, 1<<20)}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := m.EnableUEFIPlatform(); err != nil {
		t.Fatal(err)
	}
	if err := m.EnableUEFIPlatform(); !errors.Is(err, pci.ErrDeviceOccupied) {
		t.Fatalf("duplicate chipset setup = %v", err)
	}
	// Keep configuration-change IRQs in memory; this fixture has no KVM VM.
	m.pciIRQLines = make(map[uint8]*sharedIRQLine)
	for _, irq := range []uint8{virtioGPUIRQ, virtioSoundIRQ, virtioInputKeyboardIRQ, virtioInputPointerIRQ} {
		m.pciIRQLines[irq] = &sharedIRQLine{setLevel: func(bool) error { return nil }}
	}
	display := &machineTestDisplay{}
	if err := m.AddGPUDisplay(display); err != nil {
		t.Fatal(err)
	}
	if display.resize == nil {
		t.Fatal("GPU did not install the display resize callback")
	}
	if err := display.resize(640, 480); err != nil {
		t.Fatalf("display resize = %v", err)
	}
	m.AddSound(nil)
	input := m.AddVirtioInput()
	input.KeyEvent(true, 'a')
	input.PointerEvent(1, 400, 300)
	m.AddReadOnlyCDROM(bytes.NewReader(make([]byte, 2048)), 2048)
	for _, test := range []struct {
		base     uint64
		deviceID uint16
	}{
		{virtio.GPUMMIOBase, 0x1050},
		{virtio.SoundMMIOBase, 0x1059},
		{virtio.InputKeyboardMMIOBase, 0x1052},
		{virtio.InputPointerMMIOBase, 0x1052},
	} {
		device, offset, ok := m.pci.LookupMMIO(test.base)
		if !ok || offset != 0 || device.GetDeviceHeader().DeviceID != test.deviceID {
			t.Fatalf("peripheral BAR %#x did not resolve to device %#x", test.base, test.deviceID)
		}
		irq := device.GetDeviceHeader().InterruptLine
		if m.pciIRQLines[irq] == nil {
			t.Fatalf("UEFI device %#x has no shared interrupt source", test.deviceID)
		}
	}
	if len(m.devices) != 2 || m.devices[0].IOPort() != 0x1f0 || m.devices[1].IOPort() != 0x3f6 {
		t.Fatal("read-only CD-ROM did not expose its command and control ports")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if display.closed != 1 {
		t.Fatalf("display close calls = %d, want 1", display.closed)
	}
	if err := display.resize(800, 600); err == nil {
		t.Fatal("closed GPU accepted a display resize")
	}
}
