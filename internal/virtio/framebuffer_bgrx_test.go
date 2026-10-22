package virtio

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"sync"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
)

func framebufferBGRX(img *image.RGBA, padding int) ([]byte, int) {
	stride := img.Rect.Dx()*4 + padding
	pix := make([]byte, stride*img.Rect.Dy())
	for y := 0; y < img.Rect.Dy(); y++ {
		for x := 0; x < img.Rect.Dx(); x++ {
			c := img.RGBAAt(img.Rect.Min.X+x, img.Rect.Min.Y+y)
			i := y*stride + x*4
			pix[i], pix[i+1], pix[i+2], pix[i+3] = c.B, c.G, c.R, byte(x+y)
		}
	}

	return pix, stride
}

func TestFramebufferBGRXPublicationAndFormatChanges(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:i+4], []byte{29, 13, 7, 255})
	}
	if err := d.FlushDamage(128, 128, img, nil); err != nil {
		t.Fatal(err)
	}
	old, _, _ := d.changedFrame(0, true)
	var bitmap displayFrame
	d.copyFrameChanges(&bitmap, true)
	for step, bgrx := range []bool{true, false, true} {
		point := image.Pt(step+1, step+2)
		img.SetRGBA(point.X, point.Y, color.RGBA{R: byte(90 + step), G: 31, B: 47, A: 255})
		want := bytes.Clone(img.Pix)
		// The input contains changes that have not been flushed.
		img.SetRGBA(127, 127, color.RGBA{R: 250, A: 255})
		rects := []image.Rectangle{{Min: point, Max: point.Add(image.Pt(1, 1))}}
		var err error
		if bgrx {
			pix, stride := framebufferBGRX(img, 17)
			err = d.FlushBGRX(128, 128, stride, pix, rects)
			clear(pix) // Borrowed input must not survive publication.
		} else {
			err = d.FlushDamage(128, 128, img, rects)
		}
		if err != nil {
			t.Fatal(err)
		}
		img.SetRGBA(127, 127, color.RGBA{R: 29, G: 13, B: 7, A: 255})
		if got := d.snapshot(); !bytes.Equal(got.pix, want) {
			t.Fatalf("step %d exposed an unflushed pixel or changed retained colours", step)
		}
		d.copyFrameChanges(&bitmap, false)
		if !bytes.Equal(bitmap.pix, want) {
			t.Fatalf("step %d bitmap reader lost pixels across a format change", step)
		}
		if old.pix[img.PixOffset(point.X, point.Y)] != 29 {
			t.Fatal("publication mutated an immutable RGBA snapshot")
		}
	}
	oldBGRX, _, _ := d.changedFrame(0, true)
	before := bytes.Clone(oldBGRX.pix)
	pix, stride := framebufferBGRX(img, 0)
	pix[0]++
	if err := d.FlushBGRX(128, 128, stride, pix, nil); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(oldBGRX.pix, before) {
		t.Fatal("publication mutated an immutable BGRX snapshot")
	}
	seq := d.seq
	if err := d.FlushBGRX(128, 128, stride, pix, []image.Rectangle{}); err != nil || d.seq != seq {
		t.Fatal("empty damage published pixels")
	}
}

func TestFramebufferBGRXValidation(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                        string
		width, height, stride, size int
	}{
		{name: "short stride", width: 16, height: 16, stride: 63, size: 1024},
		{name: "short buffer", width: 16, height: 16, stride: 64, size: 1023},
		{name: "negative stride", width: 16, height: 16, stride: -1, size: 1024},
		{name: "overflow", width: int(^uint(0) >> 1), height: 16, stride: 64, size: 1024},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			d := &RDPDisplay{framebuffer: newFramebuffer()}
			t.Cleanup(d.shutdown)
			err := d.FlushBGRX(tc.width, tc.height, tc.stride, make([]byte, tc.size), nil)
			if !errors.Is(err, errInvalidFramebuffer) {
				t.Fatalf("invalid publication: %v", err)
			}
			if d.seq != 0 || d.graphicsActive {
				t.Fatal("invalid publication changed display state")
			}
		})
	}
}

func TestFramebufferBorrowedCaptureFailureRetainsDamage(t *testing.T) {
	t.Parallel()
	d := newFramebuffer()
	t.Cleanup(d.shutdown)
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	if err := d.flush(128, 128, img); err != nil {
		t.Fatal(err)
	}
	var frame displayFrame
	errRejected := errInvalidFramebuffer
	if _, err := d.readFrameChanges(&frame, false, func(*image.RGBA, bool, []image.Rectangle) error {
		return errRejected
	}); !errors.Is(err, errRejected) {
		t.Fatal(err)
	}
	if frame.seq != 0 || frame.width != 0 {
		t.Fatal("failed capture advanced the reader")
	}
	if _, err := d.readFrameChanges(&frame, false, func(_ *image.RGBA, _ bool, rects []image.Rectangle) error {
		if len(rects) != 1 || rects[0] != img.Bounds() {
			t.Fatal("failed capture lost full damage")
		}

		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if len(frame.pix) != 0 || frame.seq != d.seq {
		t.Fatal("capture retained RGB pixels or lost metadata")
	}
}

func TestFramebufferDirectCaptureMatchesCopiedEncoding(t *testing.T) {
	t.Parallel()
	for _, size := range []image.Point{{X: 128, Y: 128}, {X: 64, Y: 96}} {
		t.Run(size.String(), func(t *testing.T) {
			t.Parallel()
			d := &RDPDisplay{framebuffer: newFramebuffer()}
			t.Cleanup(d.shutdown)
			type viewer struct {
				writer   rdpFrameWriter
				copied   displayFrame
				baseline *avc.Encoder
			}
			var viewers [2]viewer
			for i := range viewers {
				var err error
				viewers[i].writer.avc, err = avc.NewEncoder(size.X, size.Y)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(viewers[i].writer.close)
				viewers[i].baseline, err = avc.NewEncoder(size.X, size.Y)
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(viewers[i].baseline.Close)
			}
			for step := range 8 {
				width := 128
				if step >= 5 {
					width = 130
				}
				img := image.NewRGBA(image.Rect(0, 0, width, 128))
				for i := 0; i < len(img.Pix); i += 4 {
					copy(img.Pix[i:i+4], []byte{byte(i + step*31), byte(i/7 + step), byte(i/13 + step*17), 255})
				}
				rects := []image.Rectangle{image.Rect(1+step, 3, 13+step, 17), image.Rect(100, 100, 121, 119)}
				if step%2 == 0 {
					pix, stride := framebufferBGRX(img, 7)
					if err := d.FlushBGRX(width, 128, stride, pix, rects); err != nil {
						t.Fatal(err)
					}
					clear(pix)
				} else if err := d.FlushDamage(width, 128, img, rects); err != nil {
					t.Fatal(err)
				}
				for i := range viewers {
					if i == 1 && step%2 != 0 {
						continue
					} // Independently paced reader.
					v := &viewers[i]
					force := step == 4
					damage := d.copyFrameChanges(&v.copied, force)
					if _, err := v.writer.captureAVC(d.framebuffer, force); err != nil {
						t.Fatal(err)
					}
					if len(v.writer.frame.pix) != 0 {
						t.Fatal("video reader allocated a packed-pixel snapshot")
					}
					got, err := v.writer.avc.EncodePrepared()
					if err != nil {
						t.Fatal(err)
					}
					legacy := &image.RGBA{Pix: v.copied.pix, Stride: width * 4, Rect: img.Rect}
					want, err := v.baseline.EncodeDamage(legacy, damage, force)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(got, want) {
						t.Fatalf("step %d viewer %d changed the encoded stream", step, i)
					}
				}
			}
		})
	}
}

func TestFramebufferCaptureOwnsPixelsBeforeEncoding(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for i := 0; i < len(img.Pix); i += 4 {
		copy(img.Pix[i:i+4], []byte{31, 71, 137, 255})
	}
	pix, stride := framebufferBGRX(img, 0)
	if err := d.FlushBGRX(32, 32, stride, pix, nil); err != nil {
		t.Fatal(err)
	}
	encoder, err := avc.NewEncoder(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	writer := &rdpFrameWriter{avc: encoder}
	t.Cleanup(writer.close)
	if _, err := writer.captureAVC(d.framebuffer, true); err != nil {
		t.Fatal(err)
	}
	if !d.mu.TryLock() {
		t.Fatal("capture retained the publication lock")
	}
	d.mu.Unlock()
	clear(pix)
	if err := d.FlushBGRX(32, 32, stride, pix, nil); err != nil {
		t.Fatal(err)
	}
	got, err := encoder.EncodePrepared()
	if err != nil {
		t.Fatal(err)
	}
	baseline, err := avc.NewEncoder(32, 32)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(baseline.Close)
	want, err := baseline.Encode(img, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("new publication modified an already prepared frame")
	}
}

func TestFramebufferBGRXConcurrentCapture(t *testing.T) {
	t.Parallel()
	d := &RDPDisplay{framebuffer: newFramebuffer()}
	t.Cleanup(d.shutdown)
	const width, height = 128, 128
	pix := make([]byte, width*height*4)
	if err := d.FlushBGRX(width, height, width*4, pix, nil); err != nil {
		t.Fatal(err)
	}
	var workers sync.WaitGroup
	workers.Go(func() {
		for generation := byte(1); generation <= 40; generation++ {
			for i := range pix {
				pix[i] = generation
			}
			if err := d.FlushBGRX(width, height, width*4, pix, nil); err != nil {
				t.Error(err)

				return
			}
		}
	})
	for range 2 {
		workers.Go(func() {
			var frame displayFrame
			for range 40 {
				_, err := d.readFrameChanges(&frame, false, func(img *image.RGBA, bgrx bool, _ []image.Rectangle) error {
					if !bgrx {
						t.Error("lost source format")

						return errInvalidFramebuffer
					}
					for _, pixel := range img.Pix {
						if pixel != img.Pix[0] {
							t.Error("capture mixed publication generations")

							return errInvalidFramebuffer
						}
					}

					return nil
				})
				if err != nil {
					t.Error(err)

					return
				}
			}
		})
	}
	workers.Wait()
}
