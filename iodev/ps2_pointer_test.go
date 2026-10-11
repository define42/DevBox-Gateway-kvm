package iodev_test

import (
	"testing"

	"github.com/define42/gokvm/iodev"
)

func TestPS2RebasePointerDoesNotReplayTabletMovement(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	writePS2(t, dev, 0x64, 0xd4)
	writePS2(t, dev, 0x60, 0xf4)
	_ = readPS2(t, dev, 0x60)
	dev.PointerEvent(0, 10, 10)
	dev.RebasePointer(700, 500)
	if readPS2(t, dev, 0x64)&1 != 0 {
		t.Fatal("rebasing injected pointer motion")
	}
	dev.PointerEvent(1, 700, 500)
	dev.PointerEvent(0, 705, 496)
	for _, want := range []byte{0x09, 0, 0, 0x08, 5, 4} {
		if got := readPS2(t, dev, 0x60); got != want {
			t.Fatalf("relative motion after handoff = %02x, want %02x", got, want)
		}
	}
}
