package virtio

import (
	"image"
	"sync"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
)

type framebufferInputRecorder struct {
	keyDown          bool
	keysym           uint32
	buttons          uint8
	position, bounds image.Point
	relative         image.Point
}

func (p *framebufferInputRecorder) KeyEvent(down bool, keysym uint32) {
	p.keyDown, p.keysym = down, keysym
}

func (p *framebufferInputRecorder) PointerEvent(buttons uint8, x, y uint16) {
	p.buttons, p.position = buttons, image.Pt(int(x), int(y))
}

func (p *framebufferInputRecorder) PointerEventInBounds(buttons uint8, x, y uint16, width, height int) {
	p.PointerEvent(buttons, x, y)
	p.bounds = image.Pt(width, height)
}

func (p *framebufferInputRecorder) RelativePointerEvent(_ uint8, dx, dy int16) {
	p.relative = image.Pt(int(dx), int(dy))
}

func TestFramebufferInputDuringCapture(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	const width, height = 128, 96
	if err := d.Flush(width, height, image.NewRGBA(image.Rect(0, 0, width, height))); err != nil {
		t.Fatal(err)
	}
	oldInput, input := &framebufferInputRecorder{}, &framebufferInputRecorder{}
	d.SetInput(oldInput)
	entered := make(chan struct{})
	release := make(chan struct{})
	captured := make(chan error, 1)
	go func() {
		var frame displayFrame
		_, err := d.readFrameChanges(&frame, false, func(*image.RGBA, bool, []image.Rectangle) error {
			close(entered)
			<-release

			return nil
		})
		captured <- err
	}()
	<-entered
	inputDone := make(chan struct{})
	defer func() {
		close(release)
		if err := <-captured; err != nil {
			t.Error(err)
		}
		<-inputDone
	}()
	go func() {
		defer close(inputDone)
		d.SetInput(input)
		sink, inputWidth, inputHeight := d.inputSnapshot()
		if sink != input || inputWidth != width || inputHeight != height || !supportsRelativePointer(sink) {
			t.Errorf("input metadata during capture: sink %T, size %dx%d", sink, inputWidth, inputHeight)
		}
		d.dispatchInput(&rdp.Session{Width: 64, Height: 48}, []rdp.InputEvent{
			{Kind: rdp.InputKey, Down: true, Key: 'a'},
			{Kind: rdp.InputPointer, X: 16, Y: 12, Buttons: 1},
			{Kind: rdp.InputRelativePointer, DX: -3, DY: 5},
		})
	}()
	// The timeout is only a failure guard: successful input completes while
	// capture still holds the pixel mutex, without any scheduling delay.
	select {
	case <-inputDone:
	case <-time.After(5 * time.Second):
		t.Fatal("input waited for the framebuffer conversion lock")
	}
	if !input.keyDown || input.keysym != 'a' || input.buttons != 1 ||
		input.position != image.Pt(32, 24) || input.bounds != image.Pt(width, height) ||
		input.relative != image.Pt(-3, 5) {
		t.Fatalf("input delivered during capture: %+v", input)
	}
	if *oldInput != (framebufferInputRecorder{}) {
		t.Fatalf("input reached the replaced sink: %+v", oldInput)
	}
}

func TestFramebufferInputGeometryPublication(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		publish func(*RDPDisplay, int, int) error
	}{
		{"full RGBA", func(d *RDPDisplay, width, height int) error {
			return d.Flush(width, height, image.NewRGBA(image.Rect(0, 0, width, height)))
		}},
		{"partial RGBA", func(d *RDPDisplay, width, height int) error {
			img := image.NewRGBA(image.Rect(0, 0, width, height))

			return d.FlushDamage(width, height, img, []image.Rectangle{image.Rect(1, 1, 2, 2)})
		}},
		{"partial BGRX", func(d *RDPDisplay, width, height int) error {
			return d.FlushBGRX(width, height, width*4, make([]byte, width*height*4),
				[]image.Rectangle{image.Rect(1, 1, 2, 2)})
		}},
		{"fallback", func(d *RDPDisplay, width, height int) error {
			var last []byte
			rendered := false
			if !d.refreshFallback([]byte{1}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA {
				return image.NewRGBA(image.Rect(0, 0, width, height))
			}) {
				return errInvalidFramebuffer
			}

			return nil
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			d := &RDPDisplay{framebuffer: newFramebuffer()}
			t.Cleanup(d.shutdown)
			sink, width, height := d.inputSnapshot()
			if sink != nil || width != displayDefaultWidth || height != displayDefaultHeight {
				t.Fatalf("initial input metadata: sink %T, size %dx%d", sink, width, height)
			}
			input := &framebufferInputRecorder{}
			d.SetInput(input)
			for _, size := range []image.Point{image.Pt(64, 48), image.Pt(80, 60)} {
				if err := test.publish(d, size.X, size.Y); err != nil {
					t.Fatal(err)
				}
				sink, width, height = d.inputSnapshot()
				frame := d.snapshot()
				if sink != input || image.Pt(width, height) != size || width != frame.width || height != frame.height {
					t.Fatalf("publication %v: input size %dx%d, frame size %dx%d, sink %T",
						size, width, height, frame.width, frame.height, sink)
				}
			}
			d.SetInput(nil)
			if sink, width, height = d.inputSnapshot(); sink != nil || width != 80 || height != 60 {
				t.Fatalf("detached input metadata: sink %T, size %dx%d", sink, width, height)
			}
		})
	}
}

func TestFramebufferInputMetadataConcurrentPublication(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	first, second := &framebufferInputRecorder{}, &framebufferInputRecorder{}
	d.SetInput(first)
	if err := d.Flush(64, 32, image.NewRGBA(image.Rect(0, 0, 64, 32))); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for generation := range 100 {
			width, height := 64, 32
			if generation%2 == 1 {
				width, height = 96, 48
			}
			if err := d.FlushBGRX(width, height, width*4, make([]byte, width*height*4), nil); err != nil {
				t.Error(err)

				return
			}
		}
	})
	workers.Go(func() {
		for range 100 {
			d.SetInput(first)
			d.SetInput(second)
		}
	})
	workers.Go(func() {
		for range 500 {
			input, width, height := d.inputSnapshot()
			if (input != first && input != second) ||
				(width != 64 || height != 32) && (width != 96 || height != 48) {
				t.Errorf("incoherent input metadata: sink %T, size %dx%d", input, width, height)

				return
			}
		}
	})
	workers.Wait()
}
