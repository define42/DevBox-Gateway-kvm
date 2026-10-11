package virtio

import (
	"bytes"
	"image"
	"image/color"
	"sync"
	"testing"
)

func TestFramebufferDamageCoalescesForIndependentReaders(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	defer d.shutdown()
	img := image.NewRGBA(image.Rect(0, 0, 256, 192))
	if err := d.FlushDamage(256, 192, img, nil); err != nil {
		t.Fatal(err)
	}
	var fast, slow vncFrame
	d.copyFrameChanges(&fast, false)
	d.copyFrameChanges(&slow, false)
	points := []image.Point{{X: 2, Y: 3}, {X: 180, Y: 75}, {X: 240, Y: 150}}
	for i, point := range points {
		img.SetRGBA(point.X, point.Y, color.RGBA{R: byte(i + 1), A: 255})
		if err := d.FlushDamage(256, 192, img, []image.Rectangle{{Min: point, Max: point.Add(image.Pt(1, 1))}}); err != nil {
			t.Fatal(err)
		}
		damage := d.copyFrameChanges(&fast, false)
		if len(damage) != 1 || damage[0].Dx()*damage[0].Dy() > framebufferTileSize*framebufferTileSize {
			t.Fatalf("one-pixel change copied too much: %v", damage)
		}
		if !bytes.Equal(fast.pix, img.Pix) {
			t.Fatal("fast reader missed an update")
		}
	}
	if len(d.copyFrameChanges(&slow, false)) == 0 || !bytes.Equal(slow.pix, img.Pix) {
		t.Fatal("slow reader lost changes while skipping frames")
	}
	if rects := d.copyFrameChanges(&slow, false); rects == nil || len(rects) != 0 {
		t.Fatalf("unchanged frame must return explicit empty damage: %v", rects)
	}
	before := d.seq
	if err := d.FlushDamage(256, 192, img, []image.Rectangle{}); err != nil {
		t.Fatal(err)
	}
	if d.seq != before {
		t.Fatal("empty damage published a frame")
	}
}

func TestFramebufferDamagePreservesPublishedSnapshots(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	defer d.shutdown()
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	if err := d.FlushDamage(128, 128, img, nil); err != nil {
		t.Fatal(err)
	}
	old, _, _ := d.changedFrame(0, true)
	img.SetRGBA(1, 1, color.RGBA{R: 100, A: 255})
	img.SetRGBA(100, 100, color.RGBA{R: 200, A: 255})
	if err := d.FlushDamage(128, 128, img, []image.Rectangle{image.Rect(0, 0, 2, 2)}); err != nil {
		t.Fatal(err)
	}
	if old.pix[img.PixOffset(1, 1)] != 0 {
		t.Fatal("a partial publication modified an existing immutable snapshot")
	}
	frame := d.snapshot()
	if frame.pix[img.PixOffset(1, 1)] != 100 || frame.pix[img.PixOffset(100, 100)] != 0 {
		t.Fatal("partial publication lost damage or exposed an unflushed area")
	}
	// Mutating the borrowed input after return cannot change the publication.
	clear(img.Pix)
	if d.snapshot().pix[img.PixOffset(1, 1)] != 100 {
		t.Fatal("display retained borrowed pixel storage")
	}
}

func TestFramebufferDamageResizeAndBoundedRegions(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	defer d.shutdown()
	var frame vncFrame
	for _, size := range []image.Point{{X: 256, Y: 128}, {X: 128, Y: 256}} {
		img := image.NewRGBA(image.Rectangle{Max: size})
		img.SetRGBA(size.X-1, size.Y-1, color.RGBA{B: 255, A: 255})
		if err := d.FlushDamage(size.X, size.Y, img, []image.Rectangle{image.Rect(0, 0, 1, 1)}); err != nil {
			t.Fatal(err)
		}
		rects := d.copyFrameChanges(&frame, false)
		if len(rects) != 1 || rects[0] != img.Bounds() || !bytes.Equal(frame.pix, img.Pix) {
			t.Fatal("geometry change did not force a complete snapshot")
		}
	}
	img := image.NewRGBA(image.Rect(0, 0, 2048, 2048))
	if err := d.FlushDamage(2048, 2048, img, nil); err != nil {
		t.Fatal(err)
	}
	d.copyFrameChanges(&frame, false)
	var scattered []image.Rectangle
	for y := 0; y < 2048; y += 128 {
		for x := 0; x < 2048; x += 128 {
			scattered = append(scattered, image.Rect(x, y, x+1, y+1))
		}
	}
	if err := d.FlushDamage(2048, 2048, img, scattered); err != nil {
		t.Fatal(err)
	}
	if rects := d.copyFrameChanges(&frame, false); len(rects) != 1 || rects[0] != img.Bounds() {
		t.Fatalf("fragmented damage did not fall back to a bounded full repaint: %v", rects)
	}
}

func TestFramebufferDamageConcurrentPublication(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	defer d.shutdown()
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	if err := d.FlushDamage(128, 128, img, nil); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for i := 0; i < 100; i++ {
			img.Pix[0] = byte(i)
			if err := d.FlushDamage(128, 128, img, []image.Rectangle{image.Rect(0, 0, 1, 1)}); err != nil {
				t.Error(err)
			}
		}
	})
	for range 2 {
		workers.Go(func() {
			var frame vncFrame
			for range 100 {
				d.copyFrameChanges(&frame, false)
			}
		})
	}
	workers.Wait()
	var final vncFrame
	d.copyFrameChanges(&final, false)
	if final.pix[0] != 99 {
		t.Fatal("latest publication was lost")
	}
}

func BenchmarkFramebufferDamage(b *testing.B) {
	for _, tc := range []struct {
		name string
		rect image.Rectangle
	}{
		{name: "Full1080p", rect: image.Rect(0, 0, 1920, 1080)},
		{name: "Small64x32", rect: image.Rect(100, 100, 164, 132)},
	} {
		b.Run(tc.name, func(b *testing.B) {
			d := &RDPDisplay{framebuffer: newFramebuffer()}
			defer d.shutdown()
			img := image.NewRGBA(image.Rect(0, 0, 1920, 1080))
			if err := d.FlushDamage(1920, 1080, img, nil); err != nil {
				b.Fatal(err)
			}
			var frame vncFrame
			d.copyFrameChanges(&frame, false)
			rects := []image.Rectangle{tc.rect}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				if err := d.FlushDamage(1920, 1080, img, rects); err != nil {
					b.Fatal(err)
				}
				d.copyFrameChanges(&frame, false)
			}
		})
	}
}
