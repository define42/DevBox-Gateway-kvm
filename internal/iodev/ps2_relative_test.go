package iodev_test

import (
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/iodev"
)

func TestPS2RelativeMotionPreservesSignedDeltas(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		dx, dy int16
	}{
		{name: "small", dx: 7, dy: -11},
		{name: "split", dx: 300, dy: -500},
		{name: "negative", dx: -300, dy: 500},
		{name: "minimum", dx: -32768, dy: -32768},
		{name: "maximum", dx: 32767, dy: 32767},
		{name: "buttons only"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dev := iodev.NewPS2Controller(nil, nil)
			relative, ok := any(dev).(interface{ RelativePointerEvent(uint8, int16, int16) })
			if !ok {
				t.Fatal("PS/2 controller does not accept relative pointer input")
			}
			writePS2Mouse(t, dev, 0xf4)
			assertPS2Reply(t, dev, true, []byte{0xfa})
			relative.RelativePointerEvent(5, tc.dx, tc.dy)
			data := drainBoundedPS2(t, dev, true)
			if len(data) == 0 || len(data)%3 != 0 {
				t.Fatalf("invalid relative packet stream: %v", data)
			}
			var x, y int
			for i := 0; i < len(data); i += 3 {
				if data[i]&0x0f != 0x0b {
					t.Fatalf("relative motion lost held buttons: %02x", data[i])
				}
				x += int(int8(data[i+1]))
				y += int(int8(data[i+2]))
			}
			if x != int(tc.dx) || y != -int(tc.dy) {
				t.Fatalf("relative packet motion = %d,%d, want %d,%d", x, y, tc.dx, -int(tc.dy))
			}
		})
	}
}

func TestPS2RelativeButtonsWheelAndAbsoluteRebase(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	relative, ok := any(dev).(interface{ RelativePointerEvent(uint8, int16, int16) })
	if !ok {
		t.Fatal("PS/2 controller does not accept relative pointer input")
	}
	for _, sample := range []byte{200, 100, 80} {
		writePS2Mouse(t, dev, 0xf3)
		assertPS2Reply(t, dev, true, []byte{0xfa})
		writePS2Mouse(t, dev, sample)
		assertPS2Reply(t, dev, true, []byte{0xfa})
	}
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.PointerEvent(0, 600, 400)
	relative.RelativePointerEvent(2|8, 8, -4)
	relative.RelativePointerEvent(0, 0, 0)
	// Absolute input resumes at its current position, without replaying the
	// old desktop coordinate history from before captured relative input.
	dev.PointerEvent(0, 1000, 700)
	dev.PointerEvent(0, 1002, 699)
	want := []byte{0x0c, 8, 4, 1, 0x08, 0, 0, 0, 0x08, 2, 1, 0}
	if got := drainBoundedPS2(t, dev, true); !slices.Equal(got, want) {
		t.Fatalf("relative wheel/release/rebase packets = %v, want %v", got, want)
	}
}

func TestPS2RelativeMotionHonorsReporting(t *testing.T) {
	t.Parallel()
	dev := iodev.NewPS2Controller(nil, nil)
	dev.RelativePointerEvent(1, 5, 6)
	if got := drainBoundedPS2(t, dev, true); len(got) != 0 {
		t.Fatalf("disabled relative mouse emitted packets: %v", got)
	}
	writePS2Mouse(t, dev, 0xf4)
	assertPS2Reply(t, dev, true, []byte{0xfa})
	dev.RelativePointerEvent(1, 5, 6)
	want := []byte{0x29, 5, 250}
	if got := drainBoundedPS2(t, dev, true); !slices.Equal(got, want) {
		t.Fatalf("enabling relative reporting replayed old input: %v", got)
	}
}
