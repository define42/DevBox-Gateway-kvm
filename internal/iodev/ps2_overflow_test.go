package iodev_test

import (
	"bytes"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/iodev"
)

func TestPS2OverflowPreservesPartiallyReadExtendedKey(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	dev.KeyEvent(true, 0xff53) // Right arrow.
	if got := readPS2(t, dev, 0x60); got != 0xe0 {
		t.Fatalf("extended prefix = %#x, want 0xe0", got)
	}
	for range 2048 {
		dev.KeyEvent(true, 0xff53)
	}
	dev.KeyEvent(false, 0xff53)
	data := append([]byte{0xe0}, drainBoundedPS2(t, dev, false)...)
	if len(data)%2 != 0 || !bytes.HasSuffix(data, []byte{0xe0, 0xcd}) {
		t.Fatalf("incomplete extended make/break sequence: %x", data)
	}
	for offset := 0; offset < len(data)-2; offset += 2 {
		if data[offset] != 0xe0 || data[offset+1] != 0x4d {
			t.Fatalf("extended make at %d = %x", offset, data[offset:offset+2])
		}
	}
}

func TestPS2OverflowPreservesMouseButtonReleases(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		samples []byte
		size    int
	}{
		{name: "standard", size: 3},
		{name: "wheel", samples: []byte{200, 100, 80}, size: 4},
		{name: "explorer", samples: []byte{200, 200, 80}, size: 4},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			dev := iodev.NewPS2Controller(nil, nil)
			for _, sample := range tt.samples {
				writePS2Mouse(t, dev, 0xf3)
				assertPS2Reply(t, dev, true, []byte{0xfa})
				writePS2Mouse(t, dev, sample)
				assertPS2Reply(t, dev, true, []byte{0xfa})
			}
			writePS2Mouse(t, dev, 0xf4)
			assertPS2Reply(t, dev, true, []byte{0xfa})
			dev.PointerEvent(5, 0, 0) // Left and right buttons.
			if got := readPS2(t, dev, 0x60); got != 0x0b {
				t.Fatalf("initial button packet = %#x, want 0x0b", got)
			}
			for range 2048 {
				dev.PointerEvent(5, 65535, 0)
				dev.PointerEvent(5, 0, 0)
			}
			dev.PointerEvent(4, 0, 0) // Release left, retain right.
			data := append([]byte{0x0b}, drainBoundedPS2(t, dev, true)...)
			checkPS2MouseStream(t, data, tt.size, 2)
			for range 2048 {
				dev.PointerEvent(4, 65535, 0)
				dev.PointerEvent(4, 0, 0)
			}
			dev.PointerEvent(0, 0, 0)
			checkPS2MouseStream(t, drainBoundedPS2(t, dev, true), tt.size, 0)
		})
	}
}

func TestPS2OverflowKeepsMixedInputReleases(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.KeyEvent(true, 'a')
	dev.PointerEvent(1, 0, 0)
	for range 2048 {
		dev.PointerEvent(1, 65535, 0)
		dev.PointerEvent(1, 0, 0)
		writePS2(t, dev, 0x60, 0xee)
	}
	dev.KeyEvent(false, 'a')
	dev.PointerEvent(0, 0, 0)
	keyboard, mouse := drainMixedPS2(t, dev)
	if len(keyboard) < 2 || keyboard[0] != 0x1e || keyboard[len(keyboard)-1] != 0x9e {
		t.Fatalf("command flood lost keyboard release: %x", keyboard)
	}
	checkPS2MouseStream(t, mouse, 3, 0)
}

func TestPS2OverflowAdmitsOnlyCompleteCommandReplies(t *testing.T) {
	t.Parallel()
	irqs := &ps2IRQCounter{}
	dev := iodev.NewPS2Controller(
		func() error {
			irqs.keyboard.Add(1)

			return nil
		},
		func() error {
			irqs.mouse.Add(1)

			return nil
		},
	)
	for range 2048 {
		dev.KeyEvent(true, 'a')
	}
	// Fill the 16-byte command margin with complete 3+4+3+4+2-byte replies.
	for range 2 {
		writePS2(t, dev, 0x60, 0xf2)
		writePS2Mouse(t, dev, 0xe9)
	}
	writePS2(t, dev, 0x60, 0xff)
	writePS2(t, dev, 0x60, 0xf2) // Identification must now be dropped whole.
	dev.KeyEvent(false, 'a')     // Its reservation survives the command traffic.
	writePS2Mouse(t, dev, 0xe9)  // No partial reply may enter the full queue.
	keyboard, mouse := drainMixedPS2(t, dev)
	if !bytes.HasSuffix(keyboard, []byte{0xfa, 0xab, 0x83, 0xfa, 0xab, 0x83, 0xfa, 0xaa, 0x9e}) {
		t.Fatalf("missing or partial keyboard reply: %x", keyboard)
	}
	if !bytes.Equal(mouse, []byte{0xfa, 0x00, 0x02, 100, 0xfa, 0x00, 0x02, 100}) {
		t.Fatalf("missing or partial mouse reply: %x", mouse)
	}
	if irqs.keyboard.Load() != int64(len(keyboard)) || irqs.mouse.Load() != int64(len(mouse)) {
		t.Fatalf("IRQs keyboard=%d mouse=%d, bytes keyboard=%d mouse=%d",
			irqs.keyboard.Load(), irqs.mouse.Load(), len(keyboard), len(mouse))
	}
	writePS2Mouse(t, dev, 0xf2)
	assertPS2Reply(t, dev, true, []byte{0xfa, 0})
	if irqs.mouse.Load() != int64(len(mouse)+2) {
		t.Fatal("mouse IRQ delivery did not resume after draining the full queue")
	}
}

func TestPS2OverflowResetDiscardsDeferredMouseState(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	for _, sample := range []byte{200, 100, 80} {
		writePS2Mouse(t, dev, 0xf3)
		assertPS2Reply(t, dev, true, []byte{0xfa})
		writePS2Mouse(t, dev, sample)
		assertPS2Reply(t, dev, true, []byte{0xfa})
	}
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.PointerEvent(1, 0, 0)
	dev.PointerEvent(1, 65535, 0)
	writePS2Mouse(t, dev, 0xff)
	data := drainBoundedPS2(t, dev, true)
	if len(data)%4 != 3 || !bytes.HasSuffix(data, []byte{0xfa, 0xaa, 0}) {
		t.Fatalf("stale mouse state after reset reply: %x", data)
	}
}

func TestPS2OverflowMouseDisablePreservesReleaseAndRequiresReplySpace(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.PointerEvent(1, 0, 0)
	dev.PointerEvent(1, 65535, 0)
	dev.PointerEvent(0, 65535, 0)
	writePS2Mouse(t, dev, 0xf5)
	data := drainBoundedPS2(t, dev, true)
	if len(data)%3 != 1 || !bytes.HasSuffix(data, []byte{8, 0, 0, 0xfa}) {
		t.Fatalf("mouse disable lost pending release before ACK: %x", data)
	}
	dev.PointerEvent(1, 65535, 0)
	if status := readPS2(t, dev, 0x64); status&1 != 0 {
		t.Fatal("mouse still reporting after acknowledged disable")
	}
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	for range 2048 {
		writePS2(t, dev, 0x60, 0xee)
	}
	writePS2Mouse(t, dev, 0xf5) // No reply space: the command must not take effect.
	_ = drainBoundedPS2(t, dev, false)
	dev.PointerEvent(1, 65535, 0)
	assertPS2Reply(t, dev, true, []byte{9, 0, 0})
}

func TestPS2OverflowNegotiationFlushesDeferredPacketBeforeACK(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.PointerEvent(1, 0, 0)
	dev.PointerEvent(1, 65535, 0)
	for _, sample := range []byte{200, 100, 80} {
		writePS2Mouse(t, dev, 0xf3)
		writePS2Mouse(t, dev, sample)
	}
	data := drainBoundedPS2(t, dev, true)
	acks := 0
	var packets []byte
	for offset := 0; offset < len(data); {
		if data[offset] == 0xfa {
			acks++
			offset++

			continue
		}
		if offset+3 > len(data) {
			t.Fatalf("partial mouse packet during negotiation at byte %d", offset)
		}
		packets = append(packets, data[offset:offset+3]...)
		offset += 3
	}
	if acks != 6 || len(data) == 0 || data[len(data)-1] != 0xfa {
		t.Fatalf("old mouse packet must precede final negotiation ACK: %x", data)
	}
	checkPS2MouseStream(t, packets, 3, 1)
}

func checkPS2MouseStream(t *testing.T, data []byte, size int, buttons byte) {
	t.Helper()
	if len(data) < size || len(data)%size != 0 {
		t.Fatalf("mouse stream has incomplete %d-byte packets: %x", size, data)
	}
	for offset := 0; offset < len(data); offset += size {
		if data[offset]&8 == 0 {
			t.Fatalf("mouse packet lost framing at byte %d: %x", offset, data[offset:offset+size])
		}
	}
	want := make([]byte, size)
	want[0] = 8 | buttons
	if got := data[len(data)-size:]; !bytes.Equal(got, want) {
		t.Fatalf("latest button packet = %x, want %x", got, want)
	}
}

func drainMixedPS2(t *testing.T, dev *iodev.PS2Controller) (keyboard, mouse []byte) {
	t.Helper()
	for status := readPS2(t, dev, 0x64); status&1 != 0; status = readPS2(t, dev, 0x64) {
		if len(keyboard)+len(mouse) >= 1028 {
			t.Fatal("mixed PS/2 backlog exceeded its fixed bound")
		}
		value := readPS2(t, dev, 0x60)
		if status&0x20 != 0 {
			mouse = append(mouse, value)
		} else {
			keyboard = append(keyboard, value)
		}
	}

	return keyboard, mouse
}
