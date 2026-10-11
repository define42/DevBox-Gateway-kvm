package virtio

import (
	"image"
	"image/color"
	"image/draw"
	"testing"
)

type legacyDisplaySink struct {
	frame  *image.RGBA
	frames int
}

func (d *legacyDisplaySink) Flush(_, _ int, img *image.RGBA) error {
	d.frame = image.NewRGBA(img.Bounds())
	draw.Draw(d.frame, img.Bounds(), img, img.Bounds().Min, draw.Src)
	d.frames++

	return nil
}

func (d *legacyDisplaySink) Close() error { return nil }

func TestMultiDisplaySeparatesCursorAndLegacyComposition(t *testing.T) {
	t.Parallel()
	remote, legacy := &gpuDamageSink{}, &legacyDisplaySink{}
	d := NewMultiDisplay(remote, legacy)
	background := color.RGBA{R: 13, G: 29, B: 47, A: 255}
	frame := image.NewRGBA(image.Rect(0, 0, 4, 4))
	draw.Draw(frame, frame.Bounds(), &image.Uniform{C: background}, image.Point{}, draw.Src)
	shape := image.NewRGBA(image.Rect(0, 0, 2, 2))
	green := color.RGBA{G: 255, A: 255}
	draw.Draw(shape, shape.Bounds(), &image.Uniform{C: green}, image.Point{}, draw.Src)
	cursor := DisplayCursor{Image: shape, X: -1, Y: -1, HotX: 1, HotY: 1}
	if err := d.SetCursor(cursor); err != nil {
		t.Fatal(err)
	}
	// Caller owns the shape storage once SetCursor returns.
	clear(shape.Pix)
	if err := d.Flush(4, 4, frame); err != nil {
		t.Fatal(err)
	}
	if remote.frame.RGBAAt(0, 0) != background || remote.cursor.Image.RGBAAt(0, 0) != green {
		t.Fatal("remote desktop includes a composited cursor or its cursor shape was borrowed")
	}
	if legacy.frame.RGBAAt(0, 0) != green || legacy.frame.RGBAAt(1, 1) != background {
		t.Fatal("legacy cursor clipping or shape snapshot is incorrect")
	}
	// Only the declared pixel changes; desktop storage is borrowed and must
	// not be retained, including during a later cursor-only update.
	red := color.RGBA{R: 255, A: 255}
	frame.SetRGBA(3, 3, red)
	if err := d.FlushDamage(4, 4, frame, []image.Rectangle{image.Rect(3, 3, 4, 4)}); err != nil {
		t.Fatal(err)
	}
	clear(frame.Pix)
	cursor.Image = remote.cursor.Image
	cursor.X, cursor.Y = 2, 2
	if err := d.SetCursor(cursor); err != nil {
		t.Fatal(err)
	}
	if remote.frames != 2 || remote.cursors != 2 {
		t.Fatalf("cursor move republished remote desktop: %d frames, %d cursors", remote.frames, remote.cursors)
	}
	if legacy.frame.RGBAAt(0, 0) != background || legacy.frame.RGBAAt(2, 2) != green {
		t.Fatal("legacy cursor move left a trail or used borrowed desktop memory")
	}
	cursor.Image = nil
	if err := d.SetCursor(cursor); err != nil {
		t.Fatal(err)
	}
	if remote.frames != 2 || remote.cursor.Image != nil ||
		legacy.frame.RGBAAt(2, 2) != background || legacy.frame.RGBAAt(3, 3) != red {
		t.Fatal("cursor hiding failed to preserve the partially updated desktop")
	}
	// Resizing must refresh the legacy backing image before cursor updates.
	frame = image.NewRGBA(image.Rect(0, 0, 6, 2))
	frame.SetRGBA(5, 1, red)
	if err := d.FlushDamage(6, 2, frame, []image.Rectangle{image.Rect(5, 1, 6, 2)}); err != nil {
		t.Fatal(err)
	}
	if legacy.frame.Bounds() != frame.Bounds() || legacy.frame.RGBAAt(5, 1) != red {
		t.Fatal("legacy desktop resize retained an old backing image")
	}
}

func TestMultiDisplayAvoidsLegacyDesktopCopyForCursorBackends(t *testing.T) {
	t.Parallel()
	d := NewMultiDisplay(&gpuDamageSink{}, &gpuDamageSink{})
	frame := image.NewRGBA(image.Rect(0, 0, 4, 4))
	if err := d.FlushDamage(4, 4, frame, nil); err != nil {
		t.Fatal(err)
	}
	if d.frame != nil || d.composite != nil {
		t.Fatal("cursor-capable backends allocated a legacy desktop copy")
	}
}

func TestMultiDisplayGraphicsResetReleasesCachedScanout(t *testing.T) {
	t.Parallel()
	first, second, legacy := &graphicsResetSink{}, &graphicsResetSink{}, &legacyDisplaySink{}
	d := NewMultiDisplay(first, legacy, NewMultiDisplay(second))
	frame := image.NewRGBA(image.Rect(0, 0, 4, 4))
	oldColor := color.RGBA{R: 255, A: 255}
	draw.Draw(frame, frame.Bounds(), &image.Uniform{C: oldColor}, image.Point{}, draw.Src)
	if err := d.Flush(4, 4, frame); err != nil {
		t.Fatal(err)
	}
	d.GraphicsReset()
	if first.resets != 1 || second.resets != 1 {
		t.Fatal("reset did not reach every capable child, including nested displays")
	}
	if first.frames != 1 || second.frames != 1 || legacy.frames != 1 || legacy.frame.RGBAAt(0, 0) != oldColor {
		t.Fatal("reset replaced a child's visible frame")
	}
	// A cursor-only update between drivers must not republish stale firmware
	// pixels and prevent a child display from presenting its serial fallback.
	if err := d.SetCursor(DisplayCursor{}); err != nil {
		t.Fatal(err)
	}
	if legacy.frames != 1 {
		t.Fatal("cursor update republished the released scanout")
	}
	newColor := color.RGBA{B: 255, A: 255}
	draw.Draw(frame, frame.Bounds(), &image.Uniform{C: newColor}, image.Point{}, draw.Src)
	if err := d.FlushDamage(4, 4, frame, []image.Rectangle{image.Rect(0, 0, 1, 1)}); err != nil {
		t.Fatal(err)
	}
	if first.frame.RGBAAt(3, 3) != newColor || second.frame.RGBAAt(3, 3) != newColor ||
		legacy.frame.RGBAAt(3, 3) != newColor {
		t.Fatal("the first new scanout retained pixels from the old graphics owner")
	}
}
