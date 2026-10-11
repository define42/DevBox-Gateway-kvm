package virtio

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"sync"
	"testing"
	"time"
)

var _ ConsoleDisplay = (*VNCDisplay)(nil)

func TestFramebufferSerialWritesAreBatched(t *testing.T) {
	t.Parallel()

	d := newFramebuffer()
	d.serialInterval = time.Hour
	t.Cleanup(d.shutdown)
	payload := []byte("Starting...\rReady\x1b[K\nroot@slax:~# ")
	for i := range payload {
		if n, err := d.writeSerial(payload[i : i+1]); n != 1 || err != nil {
			t.Fatalf("write byte %d: got %d, %v", i, n, err)
		}
	}
	if frame := d.snapshot(); frame.seq != 1 {
		t.Fatalf("serial burst published %d frames before refresh, want 1", frame.seq)
	}
	if !d.refreshSerial() {
		t.Fatal("serial renderer stopped before graphics took over")
	}

	expected := newVNCTextConsole(vncTextCols, vncTextRows)
	expected.write([]byte("Ready\nroot@slax:~# "))
	frame := d.snapshot()
	if frame.seq != 2 || !bytes.Equal(frame.pix, expected.render().Pix) {
		t.Fatal("batched serial frame lost characters, ordering, or terminal escapes")
	}
	d.refreshSerial()
	if got := d.snapshot().seq; got != frame.seq {
		t.Fatal("idle serial console published another frame")
	}
}

func TestFramebufferSerialPromptIsEventuallyPublished(t *testing.T) {
	t.Parallel()

	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	if _, err := d.writeSerial([]byte("boot\n")); err != nil {
		t.Fatal(err)
	}
	_, _, published := d.changedFrame(1, false)
	if _, err := d.writeSerial([]byte("login: ")); err != nil {
		t.Fatal(err)
	}
	select {
	case <-published:
	case <-time.After(time.Second):
		t.Fatal("serial prompt without a newline was never rendered")
	}
	expected := newVNCTextConsole(vncTextCols, vncTextRows)
	expected.write([]byte("boot\nlogin: "))
	if !bytes.Equal(d.snapshot().pix, expected.render().Pix) {
		t.Fatal("published serial frame does not contain the complete prompt")
	}
}

func TestFramebufferPendingSerialCannotOverwriteTakeover(t *testing.T) {
	t.Parallel()

	for _, takeover := range []string{"gpu", "vga", "shutdown"} {
		t.Run(takeover, func(t *testing.T) {
			t.Parallel()
			d := newFramebuffer()
			d.serialInterval = time.Hour
			t.Cleanup(d.shutdown)
			for _, text := range []string{"initial", "pending"} {
				if _, err := d.writeSerial([]byte(text)); err != nil {
					t.Fatal(err)
				}
			}
			img := image.NewRGBA(image.Rect(0, 0, 1, 1))
			img.SetRGBA(0, 0, color.RGBA{R: 0xff, A: 0xff})
			switch takeover {
			case "gpu":
				if err := d.Flush(1, 1, img); err != nil {
					t.Fatal(err)
				}
			case "vga":
				var last []byte
				rendered := false
				d.refreshFallback([]byte{1}, &last, &rendered, framebufferBlank,
					func([]byte) *image.RGBA { return img })
			case "shutdown":
				d.shutdown()
			}
			before := d.snapshot()
			if d.refreshSerial() {
				t.Fatal("serial renderer continued after takeover")
			}
			after := d.snapshot()
			if before.seq != after.seq || !bytes.Equal(before.pix, after.pix) {
				t.Fatal("pending serial output replaced the final framebuffer")
			}
		})
	}
}

func TestFramebufferTabletUsesCurrentDimensions(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	tablet := NewInputTablet(6, nil, nil)
	t.Cleanup(func() { _ = tablet.Close() })
	d.SetInput(NewInputPair(nil, tablet))
	for _, tt := range []struct {
		size int
		want int32
	}{{4, 12287}, {8, 6143}} {
		if err := d.Flush(tt.size, tt.size, image.NewRGBA(image.Rect(0, 0, tt.size, tt.size))); err != nil {
			t.Fatal(err)
		}
		d.sendPointerEvent(0, 1, 1)
		for _, want := range []inputEvent{
			{typ: evAbs, code: absX, value: tt.want},
			{typ: evAbs, code: absY, value: tt.want},
			synEvent(),
		} {
			if got, ok := tablet.popPending(); !ok || got != want {
				t.Fatalf("size %d: tablet event = %+v, %v; want %+v", tt.size, got, ok, want)
			}
		}
	}
}

func TestFramebufferCopiesSubimage(t *testing.T) {
	t.Parallel()

	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	img := image.NewRGBA(image.Rect(4, 7, 8, 10))
	img.SetRGBA(5, 8, color.RGBA{R: 0x12, G: 0x34, B: 0x56, A: 0xff})
	img.SetRGBA(6, 9, color.RGBA{R: 0x78, G: 0x9a, B: 0xbc, A: 0xff})
	sub := img.SubImage(image.Rect(5, 8, 7, 10)).(*image.RGBA)
	if err := d.Flush(2, 2, sub); err != nil {
		t.Fatal(err)
	}

	clear(img.Pix)
	frame := d.snapshot()
	want := []byte{
		0x12, 0x34, 0x56, 0xff, 0, 0, 0, 0,
		0, 0, 0, 0, 0x78, 0x9a, 0xbc, 0xff,
	}
	if frame.width != 2 || frame.height != 2 || !bytes.Equal(frame.pix, want) {
		t.Fatalf("snapshot: got %+v, want a packed copy of the subimage", frame)
	}

	clear(frame.pix)
	if !bytes.Equal(d.snapshot().pix, want) {
		t.Fatal("snapshot pixels alias the stored framebuffer")
	}
}

func TestFramebufferRejectsInvalidImages(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		width  int
		height int
		img    *image.RGBA
	}{
		{name: "nil", width: 1, height: 1},
		{name: "bounds", width: 2, height: 1, img: image.NewRGBA(image.Rect(0, 0, 1, 1))},
		{name: "overflow", width: int(^uint(0) >> 1), height: 2, img: image.NewRGBA(image.Rect(0, 0, 1, 1))},
		{name: "short pixels", width: 1, height: 2, img: &image.RGBA{
			Pix: make([]byte, 4), Stride: 4, Rect: image.Rect(0, 0, 1, 2),
		}},
		{name: "short stride", width: 1, height: 1, img: &image.RGBA{
			Pix: make([]byte, 4), Stride: 0, Rect: image.Rect(0, 0, 1, 1),
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			d := newFramebuffer()
			t.Cleanup(d.shutdown)
			if err := d.Flush(tt.width, tt.height, tt.img); !errors.Is(err, errInvalidFramebuffer) {
				t.Fatalf("Flush: got %v, want invalid-framebuffer error", err)
			}
		})
	}
}

func TestFramebufferGPUFrameSupersedesFallback(t *testing.T) {
	t.Parallel()

	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	entered := make(chan struct{})
	proceed := make(chan struct{})
	fallbackDone := make(chan struct{})
	go func() {
		defer close(fallbackDone)
		var last []byte
		rendered := false
		d.refreshFallback([]byte{1}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA {
			close(entered)
			<-proceed

			return image.NewRGBA(image.Rect(0, 0, 1, 1))
		})
	}()
	<-entered

	gpuDone := make(chan error, 1)
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.SetRGBA(0, 0, color.RGBA{R: 0xff, A: 0xff})
	go func() { gpuDone <- d.Flush(1, 1, img) }()
	close(proceed)
	<-fallbackDone
	if err := <-gpuDone; err != nil {
		t.Fatal(err)
	}

	if _, err := d.writeSerial([]byte("later serial output")); err != nil {
		t.Fatal(err)
	}
	var last []byte
	rendered := false
	if d.refreshFallback([]byte{2}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA {
		t.Error("fallback rendered after the GPU took over")

		return img
	}) {
		t.Fatal("fallback remains enabled after the GPU took over")
	}

	if frame := d.snapshot(); !bytes.Equal(frame.pix, img.Pix) {
		t.Fatalf("GPU framebuffer was overwritten: %v", frame.pix)
	}
}

func TestFramebufferShutdownWakesWaiters(t *testing.T) {
	t.Parallel()

	d := newFramebuffer()
	finished := make(chan bool, 1)
	go func() {
		_, ok := d.frameForRequestUntil(true, 0, nil)
		finished <- ok
	}()

	d.shutdown()
	d.shutdown()
	select {
	case ok := <-finished:
		if ok {
			t.Fatal("frame waiter reported an update after shutdown")
		}
	case <-time.After(time.Second):
		t.Fatal("shutdown left a frame waiter blocked")
	}
}

func TestFramebufferConcurrentFallbackStartAndShutdown(t *testing.T) {
	t.Parallel()

	for range 100 {
		d := newFramebuffer()
		mem := []byte{1, 2, 3, 0}
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			d.StartLinearFramebufferFallback(mem, 0, 1, 1, 4)
		}()
		go func() {
			defer wg.Done()
			d.shutdown()
		}()
		wg.Wait()

		d.StartLinearFramebufferFallback(mem, 0, 1, 1, 4)
		before := d.snapshot().seq
		if _, err := d.writeSerial([]byte("after shutdown")); err != nil {
			t.Fatal(err)
		}
		if err := d.Flush(1, 1, image.NewRGBA(image.Rect(0, 0, 1, 1))); err != nil {
			t.Fatal(err)
		}
		if got := d.snapshot().seq; got != before {
			t.Fatalf("frame changed after shutdown: sequence %d -> %d", before, got)
		}
	}
}

func TestFramebufferPublicationNotificationAndOwnership(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	_, changed, first := d.changedFrame(0, false)
	if changed {
		t.Fatal("empty framebuffer reported a change")
	}
	_, _, second := d.changedFrame(0, false)
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	img.Pix[0] = 42
	if err := d.Flush(1, 1, img); err != nil {
		t.Fatal(err)
	}
	for _, notification := range []<-chan struct{}{first, second} {
		select {
		case <-notification:
		default:
			t.Fatal("publication did not notify every frame reader")
		}
	}
	frame, changed, next := d.changedFrame(0, false)
	if !changed || frame.seq != 1 || frame.pix[0] != 42 {
		t.Fatalf("published frame: %+v, changed %v", frame, changed)
	}
	select {
	case <-next:
		t.Fatal("next frame notification was already signaled")
	default:
	}
	img.Pix[0] = 73
	if err := d.Flush(1, 1, img); err != nil {
		t.Fatal(err)
	}
	if frame.pix[0] != 42 {
		t.Fatal("subsequent publication changed an in-flight frame")
	}
	select {
	case <-next:
	default:
		t.Fatal("next publication did not wake the frame reader")
	}
	latest, changed, _ := d.changedFrame(frame.seq, false)
	if !changed || latest.seq != 2 || latest.pix[0] != 73 {
		t.Fatalf("latest frame: %+v, changed %v", latest, changed)
	}
}

func TestFramebufferConcurrentPublicationNotifications(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	const frames = 1000
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		img := image.NewRGBA(image.Rect(0, 0, 1, 1))
		for sequence := range frames {
			img.Pix[0] = byte(sequence + 1)
			if err := d.Flush(1, 1, img); err != nil {
				t.Error(err)

				return
			}
		}
	}()
	t.Cleanup(func() { <-finished })
	timeout := time.NewTimer(2 * time.Second)
	defer timeout.Stop()
	var sequence uint64
	for sequence < frames {
		frame, changed, published := d.changedFrame(sequence, false)
		if changed {
			if frame.seq <= sequence || frame.pix[0] != byte(frame.seq) {
				t.Fatalf("torn frame: %+v after sequence %d", frame, sequence)
			}
			sequence = frame.seq

			continue
		}
		select {
		case <-published:
		case <-timeout.C:
			t.Fatalf("missed publication after sequence %d", sequence)
		}
	}
}

//nolint:paralleltest // AllocsPerRun forbids running inside a parallel test.
func TestFramebufferIdleFallbackDoesNotAllocate(t *testing.T) {
	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	mem := []byte{1, 2, 3, 0}
	var last []byte
	rendered := false
	render := func(data []byte) *image.RGBA {
		return renderLinearFramebuffer(data, 1, 1, 4)
	}
	d.refreshFallback(mem, &last, &rendered, framebufferBlank, render)
	allocations := testing.AllocsPerRun(100, func() {
		d.refreshFallback(mem, &last, &rendered, framebufferBlank, render)
	})
	if allocations != 0 {
		t.Fatalf("unchanged fallback allocates %.0f times per capture", allocations)
	}
	if frame := d.snapshot(); frame.seq != 1 {
		t.Fatalf("unchanged fallback was republished %d times", frame.seq)
	}
}

func BenchmarkFramebufferRead(b *testing.B) {
	d := newFramebuffer()
	b.Cleanup(d.shutdown)
	if err := d.Flush(1024, 768, image.NewRGBA(image.Rect(0, 0, 1024, 768))); err != nil {
		b.Fatal(err)
	}
	b.Run("copy", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			d.snapshot()
		}
	})
	b.Run("immutable", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			d.changedFrame(0, false)
		}
	})
}

func BenchmarkFramebufferSerialByte(b *testing.B) {
	d := newFramebuffer()
	b.Cleanup(d.shutdown)
	p := []byte{'x'}
	b.ReportAllocs()
	b.SetBytes(1)
	for b.Loop() {
		if _, err := d.writeSerial(p); err != nil {
			b.Fatal(err)
		}
	}
}
