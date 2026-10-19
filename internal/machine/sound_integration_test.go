//go:build integration

package machine

import (
	"bytes"
	"encoding/binary"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

func TestUEFISoundRegistrationAndClose(t *testing.T) {
	t.Parallel()
	m := newLifecycleMachine(t, 1)
	if err := m.EnableUEFIPlatform(); err != nil {
		t.Fatal(err)
	}
	m.AddSound(nil)
	dev, offset, ok := m.pci.LookupMMIO(virtio.SoundMMIOBase)
	sound, isSound := dev.(*virtio.Sound)
	if !ok || !isSound || offset != 0 {
		t.Fatal("sound device is not mapped at its PCI BAR")
	}
	header := sound.GetDeviceHeader()
	if header.DeviceID != 0x1059 || header.InterruptPin != 1 || header.InterruptLine != virtioSoundIRQ {
		t.Fatalf("unexpected sound PCI identity or interrupt: %+v", header)
	}
	for _, function := range m.pci.Functions() {
		if function.Device == sound {
			route := amlPackage([][]byte{
				amlInteger(uint64(function.Slot)<<16 | uint64(function.Function)),
				{0},
				{0},
				amlInteger(virtioSoundIRQ),
			})
			if !bytes.Contains(uefiDSDT(1, m.pci.Functions()), route) {
				t.Fatal("UEFI ACPI tables omit the sound interrupt route")
			}
		}
	}
	line := m.pciIRQLines[virtioSoundIRQ]
	if line == nil {
		t.Fatal("sound device does not use UEFI level-triggered interrupts")
	}
	irqHigh := func() bool {
		line.mu.Lock()
		defer line.mu.Unlock()

		return line.high
	}

	// Publish a PCM_INFO request before handing its queue to the device worker.
	// Its interrupt publishes completion without racing reads of guest memory.
	request := m.mem[4096:4112]
	binary.LittleEndian.PutUint32(request, 0x0100)
	binary.LittleEndian.PutUint32(request[8:], 1)
	binary.LittleEndian.PutUint32(request[12:], 32)
	queue := &virtio.SplitQueue{
		Desc:  new([virtio.QueueSize]virtio.SplitDesc),
		Avail: &virtio.SplitAvail{Idx: 1}, Used: new(virtio.SplitUsed),
	}
	queue.Desc[0] = virtio.SplitDesc{Addr: 4096, Len: 16, Flags: 1, Next: 1}
	queue.Desc[1] = virtio.SplitDesc{Addr: 8192, Len: 36, Flags: 2}
	sound.QueueReady(0, queue)
	deadline := time.Now().Add(2 * time.Second)
	for !irqHigh() {
		if time.Now().After(deadline) {
			t.Fatal("sound worker did not complete the guest request")
		}
		time.Sleep(time.Millisecond)
	}
	response := m.mem[8192:8228]
	if binary.LittleEndian.Uint32(response) != 0x8000 || queue.Used.Idx != 1 {
		t.Fatalf("PCM_INFO did not complete successfully: %x", response)
	}
	if response[28] != 0 || response[29] != 2 || response[30] != 2 {
		t.Fatalf("sound device does not expose stereo playback: %x", response)
	}
	var status [1]byte
	sound.MMIO(0x1000, status[:], false) // Read-to-clear ISR capability.
	if status[0] != 1 || irqHigh() {
		t.Fatal("acknowledging sound completion did not lower its interrupt")
	}

	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("machine close did not stop and join the sound worker")
	}
}
