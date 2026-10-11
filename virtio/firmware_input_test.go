package virtio

import (
	"slices"
	"sync"
	"testing"
)

type firmwarePointerEvent struct {
	buttons uint8
	x, y    uint16
}

type firmwareKeyEvent struct {
	down bool
	key  uint32
}

type firmwareInputRecorder struct {
	points  []firmwarePointerEvent
	rebases []firmwarePointerEvent
	keys    []firmwareKeyEvent
}

func (r *firmwareInputRecorder) KeyEvent(down bool, key uint32) {
	r.keys = append(r.keys, firmwareKeyEvent{down: down, key: key})
}

func (r *firmwareInputRecorder) PointerEvent(buttons uint8, x, y uint16) {
	r.points = append(r.points, firmwarePointerEvent{buttons: buttons, x: x, y: y})
}

func (r *firmwareInputRecorder) RebasePointer(x, y uint16) {
	r.rebases = append(r.rebases, firmwarePointerEvent{x: x, y: y})
}

func firmwareTablet(t *testing.T) *InputDevice {
	t.Helper()
	tablet := NewInputTablet(6, func() error { return nil }, make([]byte, 0x10000))
	t.Cleanup(func() { _ = tablet.Close() })

	return tablet
}

func enableFirmwareTabletQueue(tablet *InputDevice) {
	writeCfg(tablet.ModernTransport, 22, inputEventQueue, 2)
	writeCfg(tablet.ModernTransport, 32, 0x1000, 4)
	writeCfg(tablet.ModernTransport, 40, 0x2000, 4)
	writeCfg(tablet.ModernTransport, 48, 0x3000, 4)
	writeCfg(tablet.ModernTransport, 28, 1, 2)
}

func takeTabletEvents(tablet *InputDevice) []inputEvent {
	var result []inputEvent
	for {
		event, ok := tablet.popPending()
		if !ok {
			return result
		}
		result = append(result, event)
	}
}

func TestFirmwareInputPreservesKeyboardAndHandsPointerToTablet(t *testing.T) {
	t.Parallel()
	legacy := &firmwareInputRecorder{}
	tablet := firmwareTablet(t)
	input := NewFirmwareInput(legacy, tablet)
	input.KeyEvent(true, 0xff1b)
	input.PointerEventInBounds(1, 100, 200, 800, 600)
	enableFirmwareTabletQueue(tablet)
	// Queue setup precedes DRIVER_OK: firmware keeps control during this gap.
	input.PointerEventInBounds(1, 110, 210, 800, 600)
	if len(legacy.points) != 2 || len(takeTabletEvents(tablet)) != 0 {
		t.Fatal("tablet stole input before DRIVER_OK")
	}
	writeCfg(tablet.ModernTransport, 20, 15, 1)
	input.PointerEventInBounds(1, 400, 100, 800, 600)
	input.KeyEvent(false, 0xff1b)
	if !slices.Equal(legacy.keys, []firmwareKeyEvent{{down: true, key: 0xff1b}, {key: 0xff1b}}) {
		t.Fatalf("keyboard did not remain on firmware device: %v", legacy.keys)
	}
	if len(legacy.points) != 3 || legacy.points[2] != (firmwarePointerEvent{x: 110, y: 210}) {
		t.Fatalf("firmware handoff must only release the old button at its old position: %v", legacy.points)
	}
	want := []inputEvent{
		{typ: evAbs, code: absX, value: 16403},
		{typ: evAbs, code: absY, value: 5488},
		synEvent(),
		{typ: evKey, code: btnLeft, value: 1},
		synEvent(),
	}
	if events := takeTabletEvents(tablet); !slices.Equal(events, want) {
		t.Fatalf("first tablet click did not reach the absolute target before pressing: %v", events)
	}
	// Resize and release: the same point has different normalized coordinates.
	input.PointerEventInBounds(0, 400, 100, 1600, 1200)
	want = []inputEvent{
		{typ: evAbs, code: absX, value: 8201},
		{typ: evAbs, code: absY, value: 2744},
		synEvent(),
		{typ: evKey, code: btnLeft},
		synEvent(),
	}
	if events := takeTabletEvents(tablet); !slices.Equal(events, want) {
		t.Fatalf("tablet lost framebuffer dimensions or button release: %v", events)
	}
	if len(legacy.points) != 3 {
		t.Fatal("desktop pointer was also forwarded to the relative mouse")
	}
}

func TestFirmwareInputRequiresUsableTabletDriver(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                string
		status              uint64
		queue, closed, want bool
		disable, invalid    bool
	}{
		{name: "uninitialized"},
		{name: "queue before driver", queue: true, status: 11},
		{name: "driver without queue", status: 15},
		{name: "disabled queue", queue: true, status: 15, disable: true},
		{name: "invalid queue mapping", status: 15, invalid: true},
		{name: "ready", queue: true, status: 15, want: true},
		{name: "failed", queue: true, status: 15 | 128},
		{name: "reset needed", queue: true, status: 15 | 64},
		{name: "closed", queue: true, status: 15, closed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tablet := firmwareTablet(t)
			legacy := &firmwareInputRecorder{}
			input := NewFirmwareInput(legacy, tablet)
			if tc.queue {
				enableFirmwareTabletQueue(tablet)
			}
			if tc.disable {
				writeCfg(tablet.ModernTransport, 28, 0, 2)
			}
			if tc.invalid {
				writeCfg(tablet.ModernTransport, 32, 0xffffffff, 4)
				writeCfg(tablet.ModernTransport, 28, 1, 2)
			}
			writeCfg(tablet.ModernTransport, 20, tc.status, 1)
			if tc.closed {
				_ = tablet.Close()
			}
			input.PointerEventInBounds(0, 0, 0, 1024, 768)
			if got := len(takeTabletEvents(tablet)) != 0; got != tc.want {
				t.Fatalf("tablet received input = %v, want %v", got, tc.want)
			}
			if got := len(legacy.points) == 0; got != tc.want {
				t.Fatalf("firmware received wrong input: %v", legacy.points)
			}
		})
	}
}

func TestFirmwareInputResetRebasesFallbackAndRestoresAbsoluteInput(t *testing.T) {
	t.Parallel()
	legacy := &firmwareInputRecorder{}
	tablet := firmwareTablet(t)
	input := NewFirmwareInput(legacy, tablet)
	input.PointerEventInBounds(1, 20, 30, 800, 600)
	enableFirmwareTabletQueue(tablet)
	writeCfg(tablet.ModernTransport, 20, 15, 1)
	input.PointerEventInBounds(1, 400, 100, 800, 600)
	writeCfg(tablet.ModernTransport, 20, 0, 1)
	input.PointerEventInBounds(1, 700, 500, 800, 600)
	if !slices.Equal(legacy.rebases, []firmwarePointerEvent{{x: 700, y: 500}}) {
		t.Fatalf("fallback replayed old relative history after reset: %v", legacy.rebases)
	}
	if len(takeTabletEvents(tablet)) != 0 {
		t.Fatal("reset left stale tablet events or accepted new events")
	}
	enableFirmwareTabletQueue(tablet)
	writeCfg(tablet.ModernTransport, 20, 15, 1)
	input.PointerEventInBounds(1, 400, 100, 800, 600)
	want := []inputEvent{
		{typ: evAbs, code: absX, value: 16403},
		{typ: evAbs, code: absY, value: 5488},
		synEvent(),
		{typ: evKey, code: btnLeft, value: 1},
		synEvent(),
	}
	if events := takeTabletEvents(tablet); !slices.Equal(events, want) {
		t.Fatalf("tablet failed to restore position/button state after reset: %v", events)
	}
	if last := legacy.points[len(legacy.points)-1]; last != (firmwarePointerEvent{x: 700, y: 500}) {
		t.Fatalf("firmware held button was not released during second handoff: %v", last)
	}
}

func TestFirmwareInputConcurrentReset(t *testing.T) {
	t.Parallel()
	tablet := firmwareTablet(t)
	input := NewFirmwareInput(&firmwareInputRecorder{}, tablet)
	var workers sync.WaitGroup
	workers.Go(func() {
		for range 250 {
			enableFirmwareTabletQueue(tablet)
			writeCfg(tablet.ModernTransport, 20, 15, 1)
			writeCfg(tablet.ModernTransport, 20, 0, 1)
		}
	})
	workers.Go(func() {
		for i := range 1000 {
			input.PointerEventInBounds(uint8(i%2), uint16(i%800), uint16(i%600), 800, 600)
		}
	})
	workers.Wait()
	writeCfg(tablet.ModernTransport, 20, 0, 1)
	if events := takeTabletEvents(tablet); len(events) != 0 {
		t.Fatalf("reset retained pointer events: %v", events)
	}
}
