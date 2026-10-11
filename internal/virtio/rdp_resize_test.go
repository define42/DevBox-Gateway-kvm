package virtio

import (
	"image"
	"reflect"
	"testing"
	"time"

	"github.com/define42/gokvm/internal/rdp"
)

func TestRDPResizeControllerReconnect(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{}
	first := &rdp.Session{Width: 1280, Height: 720}
	second := &rdp.Session{Width: 800, Height: 600}
	third := &rdp.Session{Width: 1920, Height: 1080}
	var modes []rdp.DesktopSize
	d.addResizeViewer(first)
	// The GPU can attach after a client has completed negotiation.
	d.SetResizeHandler(func(width, height int) error {
		modes = append(modes, rdp.DesktopSize{Width: width, Height: height})

		return nil
	})
	d.resizeGuest(second)
	d.removeResizeViewer(second)
	d.removeResizeViewer(first)
	d.resizeGuest(first) // A departed viewer cannot reclaim control.
	d.addResizeViewer(second)
	d.removeResizeViewer(first)
	if d.viewer != second {
		t.Fatal("departed viewer removed the new resize controller")
	}
	d.removeResizeViewer(second)
	d.removeResizeViewer(second)
	d.addResizeViewer(third)
	d.resizeGuest(second)
	want := []rdp.DesktopSize{{Width: 1280, Height: 720}, {Width: 800, Height: 600}, {Width: 1920, Height: 1080}}
	if !reflect.DeepEqual(modes, want) {
		t.Fatalf("GPU modes: got %v, want %v", modes, want)
	}
}

func TestRDPResizeResetsEncoderHistory(t *testing.T) {
	t.Parallel()
	bitmap, err := rdp.NewBitmapEncoder(1024, 768, 32)
	if err != nil {
		t.Fatal(err)
	}
	writer := &rdpFrameWriter{
		session: &rdp.Session{Width: 1280, Height: 720, BitsPerPixel: 32},
		bitmap:  bitmap, width: 1024, height: 768, nextFrame: time.Now().Add(time.Hour),
	}
	if err := writer.updateSize(); err != nil {
		t.Fatal(err)
	}
	if writer.bitmap == bitmap || !writer.force || !writer.nextFrame.IsZero() ||
		writer.width != 1280 || writer.height != 720 {
		t.Fatalf("resize did not discard old encoder history: %+v", writer)
	}
	bitmap = writer.bitmap
	writer.force = false
	if err := writer.updateSize(); err != nil {
		t.Fatal(err)
	}
	if writer.bitmap != bitmap || writer.force {
		t.Fatal("unchanged geometry unnecessarily reset the encoder")
	}
}

type resizePointerInput struct {
	x, y    uint16
	buttons uint8
}

func (p *resizePointerInput) KeyEvent(bool, uint32) {}

func (p *resizePointerInput) PointerEvent(buttons uint8, x, y uint16) {
	p.buttons, p.x, p.y = buttons, x, y
}

func TestRDPResizePointerTracksFramebuffer(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	defer d.shutdown()
	input := &resizePointerInput{}
	d.SetInput(input)
	session := &rdp.Session{Width: 800, Height: 600}
	for _, size := range []image.Point{{X: 1024, Y: 768}, {X: 1280, Y: 720}, {X: 800, Y: 600}} {
		if err := d.Flush(size.X, size.Y, image.NewRGBA(image.Rect(0, 0, size.X, size.Y))); err != nil {
			t.Fatal(err)
		}
		d.dispatchInput(session, []rdp.InputEvent{{Kind: rdp.InputPointer, X: 400, Y: 300, Buttons: 1}})
		if int(input.x) != size.X/2 || int(input.y) != size.Y/2 || input.buttons != 1 {
			t.Fatalf("pointer at %v: got %+v", size, input)
		}
		d.dispatchInput(session, []rdp.InputEvent{{Kind: rdp.InputPointer, X: 65535, Y: 65535}})
		if int(input.x) >= size.X || int(input.y) >= size.Y {
			t.Fatalf("pointer escaped framebuffer %v: %+v", size, input)
		}
	}
}

func TestRDPMultiDisplayForwardsResizeHandler(t *testing.T) {
	t.Parallel()
	rdpDisplay := &RDPDisplay{}
	display := NewMultiDisplay(NewPNGDisplay("unused.png"), rdpDisplay)
	var width, height int
	display.SetResizeHandler(func(w, h int) error {
		width, height = w, h

		return nil
	})
	rdpDisplay.addResizeViewer(&rdp.Session{Width: 1280, Height: 720})
	if width != 1280 || height != 720 {
		t.Fatalf("combined displays did not attach resize handler: %dx%d", width, height)
	}
}

func TestRDPResizeAbsolutePointerTracksPixels(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	tablet := NewInputTablet(6, nil, nil)
	t.Cleanup(func() { _ = tablet.Close() })
	d.SetInput(NewInputPair(nil, tablet))
	for _, geometry := range []struct {
		guest, client image.Point
	}{
		{image.Pt(1024, 768), image.Pt(800, 600)},
		{image.Pt(1280, 720), image.Pt(800, 600)},
		{image.Pt(800, 600), image.Pt(1920, 1080)},
		{image.Pt(1920, 1080), image.Pt(1920, 1080)},
	} {
		if err := d.Flush(geometry.guest.X, geometry.guest.Y,
			image.NewRGBA(image.Rectangle{Max: geometry.guest})); err != nil {
			t.Fatal(err)
		}
		session := &rdp.Session{Width: geometry.client.X, Height: geometry.client.Y}
		for _, point := range []image.Point{
			image.Pt(0, 0), geometry.client.Sub(image.Pt(1, 1)),
			image.Pt(geometry.client.X/2, geometry.client.Y/2), image.Pt(65535, 65535),
		} {
			d.dispatchInput(session, []rdp.InputEvent{{Kind: rdp.InputPointer, X: uint16(point.X), Y: uint16(point.Y)}})
			x, xOK := tablet.popPending()
			y, yOK := tablet.popPending()
			syn, synOK := tablet.popPending()
			if !xOK || !yOK || !synOK || x.typ != evAbs || x.code != absX ||
				y.typ != evAbs || y.code != absY || syn != synEvent() {
				t.Fatalf("absolute pointer report: %+v %+v %+v", x, y, syn)
			}
			// Linux mousedev maps the advertised absolute range back to
			// pixels. These must be the same source pixels the RDP encoder
			// samples, regardless of the previous position or display size.
			got := image.Pt(int(x.value)*geometry.guest.X/inputAbsMax, int(y.value)*geometry.guest.Y/inputAbsMax)
			want := image.Pt(min(point.X, geometry.client.X-1)*geometry.guest.X/geometry.client.X,
				min(point.Y, geometry.client.Y-1)*geometry.guest.Y/geometry.client.Y)
			if got != want {
				t.Fatalf("geometry %+v, client %v: guest pointer %v, want %v", geometry, point, got, want)
			}
		}
	}
}
