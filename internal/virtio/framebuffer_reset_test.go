package virtio

import (
	"bytes"
	"image"
	"io"
	"testing"
)

func TestRDPDoesNotAcceptSerialOutput(t *testing.T) {
	t.Parallel()
	if _, ok := any((*RDPDisplay)(nil)).(io.Writer); ok {
		t.Fatal("RDP exposes a serial output writer")
	}
}

func TestRDPGraphicsReplacesSameSizeFallbackFrame(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	fallback := image.NewRGBA(image.Rect(0, 0, 32, 32))
	fallback.Pix[0] = 255
	var last []byte
	rendered := false
	if !d.refreshFallback([]byte{1}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA { return fallback }) {
		t.Fatal("legacy framebuffer did not start")
	}
	img := image.NewRGBA(fallback.Bounds())
	img.Pix[len(img.Pix)-4] = 255
	// Even a small update must replace fallback pixels across the whole image.
	if err := d.FlushDamage(32, 32, img, []image.Rectangle{image.Rect(0, 0, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.snapshot().pix, img.Pix) {
		t.Fatal("fallback pixels survived the graphics handoff")
	}
	d.GraphicsReset()
	for _, damage := range [][]image.Rectangle{{}, {image.Rect(-10, -10, -1, -1)}} {
		before := d.snapshot()
		if err := d.FlushDamage(16, 16, image.NewRGBA(image.Rect(0, 0, 16, 16)), damage); err != nil {
			t.Fatal(err)
		}
		if d.snapshot().seq != before.seq || d.graphicsActive {
			t.Fatal("empty damage reclaimed graphics ownership")
		}
	}
	img.Pix[4] = 255
	if err := d.FlushDamage(32, 32, img, []image.Rectangle{image.Rect(0, 0, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(d.snapshot().pix, img.Pix) {
		t.Fatal("first graphics frame after reset was not published in full")
	}
}

func TestFramebufferResetDoesNotReviveLegacyFallback(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	img := image.NewRGBA(image.Rect(0, 0, 1, 1))
	var last []byte
	rendered := false
	if !d.refreshFallback([]byte{1}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA { return img }) {
		t.Fatal("legacy fallback did not start")
	}
	before := d.snapshot()
	d.GraphicsReset()
	if got := d.snapshot(); got.seq != before.seq || !bytes.Equal(got.pix, before.pix) {
		t.Fatal("initial GPU reset displaced active legacy graphics")
	}
	if err := d.Flush(1, 1, img); err != nil {
		t.Fatal(err)
	}
	d.GraphicsReset()
	before = d.snapshot()
	if d.refreshFallback([]byte{2}, &last, &rendered, framebufferBlank, func([]byte) *image.RGBA {
		t.Error("stale legacy framebuffer rendered after GPU ownership")

		return img
	}) {
		t.Fatal("legacy fallback revived across GPU reset")
	}
	if d.snapshot().seq != before.seq {
		t.Fatal("legacy fallback overwrote the last graphics frame")
	}
}
