package iodev_test

import (
	"sync/atomic"
	"testing"

	"github.com/define42/gokvm/internal/iodev"
)

type ps2IRQCounter struct {
	keyboard atomic.Int64
	mouse    atomic.Int64
}

func TestPS2KeyboardEvents(t *testing.T) {
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

	dev.KeyEvent(true, 'a')
	dev.KeyEvent(false, 'a')
	dev.KeyEvent(true, 0xff53) // Right arrow.
	dev.KeyEvent(false, 0xff53)

	want := []byte{0x1e, 0x9e, 0xe0, 0x4d, 0xe0, 0xcd}
	for _, w := range want {
		if got := readPS2(t, dev, 0x60); got != w {
			t.Fatalf("keyboard byte: got %#x, want %#x", got, w)
		}
	}

	if got, wantIRQs := irqs.keyboard.Load(), int64(len(want)); got != wantIRQs {
		t.Fatalf("keyboard IRQs: got %d, want %d", got, wantIRQs)
	}

	if got := irqs.mouse.Load(); got != 0 {
		t.Fatalf("mouse IRQs: got %d, want 0", got)
	}
}

func TestPS2MouseEvents(t *testing.T) {
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

	// Send "enable data reporting" to the aux device via the controller.
	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, 0xf4)
	if got := readPS2(t, dev, 0x60); got != 0xfa {
		t.Fatalf("mouse enable ACK: got %#x, want 0xfa", got)
	}

	dev.PointerEvent(0x01, 10, 10) // left button down, no movement yet.
	dev.PointerEvent(0x05, 12, 7)  // left+right, x +2, y +3.

	want := []byte{
		0x09, 0x00, 0x00,
		0x0b, 0x02, 0x03,
	}
	for _, w := range want {
		if got := readPS2(t, dev, 0x60); got != w {
			t.Fatalf("mouse byte: got %#x, want %#x", got, w)
		}
	}

	if got, wantIRQs := irqs.mouse.Load(), int64(1+len(want)); got != wantIRQs {
		t.Fatalf("mouse IRQs: got %d, want %d", got, wantIRQs)
	}
}

func TestPS2AuxOutputBufferCommand(t *testing.T) {
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

	writePS2(t, dev, 0x64, 0xd3)
	writePS2(t, dev, 0x60, 0x5a)

	if got := readPS2(t, dev, 0x64); got&0x20 == 0 {
		t.Fatalf("status %#x does not mark aux data", got)
	}

	if got := readPS2(t, dev, 0x60); got != 0x5a {
		t.Fatalf("aux output byte: got %#x, want %#x", got, 0x5a)
	}

	if got := irqs.mouse.Load(); got != 1 {
		t.Fatalf("mouse IRQs: got %d, want 1", got)
	}

	if got := irqs.keyboard.Load(); got != 0 {
		t.Fatalf("keyboard IRQs: got %d, want 0", got)
	}
}

func TestPS2MouseRejectsUnknownCommands(t *testing.T) {
	t.Parallel()

	dev := iodev.NewPS2Controller(nil, nil)

	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, 0xe1)
	if got := readPS2(t, dev, 0x60); got != 0xfe {
		t.Fatalf("unknown mouse command response: got %#x, want %#x", got, 0xfe)
	}
}

func TestPS2MouseSetDefaultsDisablesReporting(t *testing.T) {
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

	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, 0xf4)
	_ = readPS2(t, dev, 0x60)

	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, 0xf6)
	_ = readPS2(t, dev, 0x60)

	dev.PointerEvent(0x01, 10, 10)
	if got := readPS2(t, dev, 0x64); got&0x01 != 0 {
		t.Fatalf("mouse data queued after set defaults: status %#x", got)
	}
}

func TestPS2MouseIntelliMouseNegotiation(t *testing.T) {
	t.Parallel()

	dev := iodev.NewPS2Controller(nil, nil)

	for _, sample := range []byte{200, 100, 80} {
		writePS2(t, dev, 0x64, 0xd4)
		writePS2(t, dev, 0x60, 0xf3)
		if got := readPS2(t, dev, 0x60); got != 0xfa {
			t.Fatalf("set sample ACK: got %#x, want %#x", got, 0xfa)
		}

		writePS2(t, dev, 0x64, 0xd4)
		writePS2(t, dev, 0x60, sample)
		if got := readPS2(t, dev, 0x60); got != 0xfa {
			t.Fatalf("sample value ACK: got %#x, want %#x", got, 0xfa)
		}
	}

	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, 0xf2)
	if got := readPS2(t, dev, 0x60); got != 0xfa {
		t.Fatalf("get ID ACK: got %#x, want %#x", got, 0xfa)
	}
	if got := readPS2(t, dev, 0x60); got != 0x03 {
		t.Fatalf("mouse ID: got %#x, want IntelliMouse ID %#x", got, 0x03)
	}

	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, 0xf4)
	_ = readPS2(t, dev, 0x60)

	dev.PointerEvent(0x08, 10, 10)

	want := []byte{0x08, 0x00, 0x00, 0x01}
	for _, w := range want {
		if got := readPS2(t, dev, 0x60); got != w {
			t.Fatalf("IntelliMouse packet byte: got %#x, want %#x", got, w)
		}
	}
}

func readPS2(t *testing.T, dev *iodev.PS2Controller, port uint64) byte {
	t.Helper()

	var b [1]byte
	if err := dev.Read(port, b[:]); err != nil {
		t.Fatal(err)
	}

	return b[0]
}

func writePS2(t *testing.T, dev *iodev.PS2Controller, port uint64, v byte) {
	t.Helper()

	if err := dev.Write(port, []byte{v}); err != nil {
		t.Fatal(err)
	}
}
