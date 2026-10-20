package iodev_test

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/iodev"
)

func TestPS2ControllerCommandsAndPortEnablement(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	if dev.IOPort() != 0x60 || dev.Size() != 5 {
		t.Fatal("incorrect i8042 port layout")
	}
	for _, command := range []struct{ request, response byte }{
		{0x20, 0x47}, {0xaa, 0x55}, {0xab, 0}, {0xa9, 0},
	} {
		writePS2(t, dev, 0x64, command.request)
		assertPS2Reply(t, dev, false, []byte{command.response})
	}
	writePS2(t, dev, 0x64, 0x60)
	writePS2(t, dev, 0x60, 0x77) // Disable both ports, retaining IRQ configuration.
	writePS2(t, dev, 0x64, 0x20)
	assertPS2Reply(t, dev, false, []byte{0x77})
	dev.KeyEvent(true, 'a')
	dev.PointerEvent(1, 10, 10)
	assertPS2Reply(t, dev, false, nil)
	writePS2(t, dev, 0x64, 0xae)
	dev.KeyEvent(true, 'a')
	assertPS2Reply(t, dev, false, []byte{0x1e})
	writePS2(t, dev, 0x64, 0xad)
	dev.KeyEvent(false, 'a')
	assertPS2Reply(t, dev, false, nil)
	writePS2(t, dev, 0x64, 0xa8)
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.PointerEvent(1, 10, 10)
	assertPS2Reply(t, dev, true, []byte{9, 0, 0})
	writePS2(t, dev, 0x64, 0xa7)
	dev.PointerEvent(0, 20, 20)
	assertPS2Reply(t, dev, true, nil)
}

func TestPS2KeyboardCommandReplies(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		command byte
		want    []byte
	}{
		{name: "reset", command: 0xff, want: []byte{0xfa, 0xaa}},
		{name: "identify", command: 0xf2, want: []byte{0xfa, 0xab, 0x83}},
		{name: "echo", command: 0xee, want: []byte{0xee}},
		{name: "defaults", command: 0xf6, want: []byte{0xfa}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dev := iodev.NewPS2Controller(nil, nil)
			writePS2(t, dev, 0x60, test.command)
			assertPS2Reply(t, dev, false, test.want)
		})
	}
}

func TestPS2KeyboardParameterAndScanControl(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	for _, command := range []byte{0xf3, 0xed, 0xf0} {
		writePS2(t, dev, 0x60, command)
		assertPS2Reply(t, dev, false, []byte{0xfa})
		writePS2(t, dev, 0x60, 0xee) // Parameter, not an echo command.
		assertPS2Reply(t, dev, false, []byte{0xfa})
		writePS2(t, dev, 0x60, 0xee)
		assertPS2Reply(t, dev, false, []byte{0xee})
	}
	writePS2(t, dev, 0x60, 0xf5)
	assertPS2Reply(t, dev, false, []byte{0xfa})
	dev.KeyEvent(true, 'A')
	assertPS2Reply(t, dev, false, nil)
	writePS2(t, dev, 0x60, 0xf4)
	assertPS2Reply(t, dev, false, []byte{0xfa})
	dev.KeyEvent(true, 'A')
	dev.KeyEvent(false, 'A')
	dev.KeyEvent(true, 0x12345678) // Unmapped keysyms must not produce scan codes.
	assertPS2Reply(t, dev, false, []byte{0x1e, 0x9e})
	for index, code := range []byte{0x3b, 0x3c, 0x3d, 0x3e, 0x3f, 0x40, 0x41, 0x42, 0x43, 0x44, 0x57, 0x58} {
		dev.KeyEvent(true, 0xffbe+uint32(index))
		dev.KeyEvent(false, 0xffbe+uint32(index))
		assertPS2Reply(t, dev, false, []byte{code, code | 0x80})
	}
}

func TestPS2OutputBuffersHonorIRQMask(t *testing.T) {
	t.Parallel()
	keyboardIRQs, mouseIRQs := 0, 0
	dev := iodev.NewPS2Controller(
		func() error {
			keyboardIRQs++

			return nil
		},
		func() error {
			mouseIRQs++

			return nil
		},
	)
	writePS2(t, dev, 0x64, 0x60)
	writePS2(t, dev, 0x60, 4) // System flag, IRQs disabled, both ports enabled.
	writePS2(t, dev, 0x64, 0xd2)
	writePS2(t, dev, 0x60, 0x77)
	writePS2(t, dev, 0x64, 0xd3)
	writePS2(t, dev, 0x60, 0x88)
	if status := readPS2(t, dev, 0x64); status&0x21 != 1 {
		t.Fatalf("keyboard buffer status = %#x", status)
	}
	if value := readPS2(t, dev, 0x60); value != 0x77 {
		t.Fatalf("keyboard buffer byte = %#x", value)
	}
	assertPS2Reply(t, dev, true, []byte{0x88})
	if keyboardIRQs != 0 || mouseIRQs != 0 {
		t.Fatal("masked output buffers raised IRQs")
	}
	writePS2(t, dev, 0x64, 0x60)
	writePS2(t, dev, 0x60, 7)
	dev.KeyEvent(true, 'a')
	writePS2(t, dev, 0x64, 0xd3)
	writePS2(t, dev, 0x60, 0x99)
	if value := readPS2(t, dev, 0x60); value != 0x1e {
		t.Fatalf("unmasked keyboard byte = %#x", value)
	}
	assertPS2Reply(t, dev, true, []byte{0x99})
	if keyboardIRQs != 1 || mouseIRQs != 1 {
		t.Fatalf("mixed queue IRQs = keyboard %d mouse %d", keyboardIRQs, mouseIRQs)
	}
}

func TestPS2MouseStatusResetAndExplorer(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	writePS2Mouse(t, dev, 0xe8)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	writePS2Mouse(t, dev, 2)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	writePS2Mouse(t, dev, 0xe9)
	assertPS2Reply(t, dev, true, []byte{0xfa, 0, 2, 100})
	for _, sample := range []byte{200, 200, 80} {
		writePS2Mouse(t, dev, 0xf3)
		assertPS2Reply(t, dev, true, []byte{0xfa})
		writePS2Mouse(t, dev, sample)
		assertPS2Reply(t, dev, true, []byte{0xfa})
	}
	writePS2Mouse(t, dev, 0xf2)
	assertPS2Reply(t, dev, true, []byte{0xfa, 4})
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.PointerEvent(0x12, 500, 500) // Middle button and wheel down.
	assertPS2Reply(t, dev, true, []byte{0x0c, 0, 0, 0xff})
	dev.PointerEvent(2, 200, 800)
	assertPS2Reply(t, dev, true, []byte{
		0x3c, 0x81, 0x81, 0, 0x3c, 0x81, 0x81, 0, 0x3c, 0xd2, 0xd2, 0,
	})
	writePS2Mouse(t, dev, 0xeb)
	assertPS2Reply(t, dev, true, []byte{0xfa, 0x0c, 0, 0, 0})
	writePS2Mouse(t, dev, 0xf5)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.PointerEvent(0, 300, 300)
	assertPS2Reply(t, dev, true, nil)
	writePS2Mouse(t, dev, 0xff)
	assertPS2Reply(t, dev, true, []byte{0xfa, 0xaa, 0})
	writePS2Mouse(t, dev, 0xf2)
	assertPS2Reply(t, dev, true, []byte{0xfa, 0})
}

func TestPS2RejectsInvalidPortWidths(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	for _, width := range []int{0, 2, 4} {
		if err := dev.Read(0x60, make([]byte, width)); err == nil {
			t.Errorf("read width %d accepted", width)
		}
		if err := dev.Write(0x60, make([]byte, width)); err == nil {
			t.Errorf("write width %d accepted", width)
		}
	}
	writePS2(t, dev, 0x61, 0xff)
	if got := readPS2(t, dev, 0x61); got != 0 {
		t.Fatalf("unmapped port = %#x", got)
	}
	if got := readPS2(t, dev, 0x60); got != 0 {
		t.Fatalf("empty output buffer = %#x", got)
	}
}

func writePS2Mouse(t *testing.T, dev *iodev.PS2Controller, value byte) {
	t.Helper()
	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, value)
}

func assertPS2Reply(t *testing.T, dev *iodev.PS2Controller, aux bool, want []byte) {
	t.Helper()
	for index, value := range want {
		status := readPS2(t, dev, 0x64)
		if status&1 == 0 || (status&0x20 != 0) != aux {
			t.Fatalf("reply byte %d status = %#x, want full buffer and aux=%t", index, status, aux)
		}
		if got := readPS2(t, dev, 0x60); got != value {
			t.Fatalf("reply byte %d = %#x, want %#x", index, got, value)
		}
	}
	if status := readPS2(t, dev, 0x64); status&1 != 0 {
		t.Fatalf("unexpected trailing output: status %#x", status)
	}
}
