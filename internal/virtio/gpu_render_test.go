package virtio

import (
	"bytes"
	"fmt"
	"image"
	"testing"
)

type gpuTestFramebuffer struct{ *framebuffer }

func (d gpuTestFramebuffer) Close() error {
	d.shutdown()

	return nil
}

func TestGPUResourcePixelFormats(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		format uint32
		pixel  [4]byte
		alpha  byte
	}{
		{"B8G8R8A8", gpuFormatB8G8R8A8, [4]byte{31, 23, 17, 127}, 127},
		{"B8G8R8X8", gpuFormatB8G8R8X8, [4]byte{31, 23, 17, 127}, 255},
		{"A8R8G8B8", gpuFormatA8R8G8B8, [4]byte{127, 17, 23, 31}, 127},
		{"X8R8G8B8", gpuFormatX8R8G8B8, [4]byte{127, 17, 23, 31}, 255},
		{"R8G8B8A8", gpuFormatR8G8B8A8, [4]byte{17, 23, 31, 127}, 127},
		{"X8B8G8R8", gpuFormatX8B8G8R8, [4]byte{127, 31, 23, 17}, 255},
		{"A8B8G8R8", gpuFormatA8B8G8R8, [4]byte{127, 31, 23, 17}, 127},
		{"R8G8B8X8", gpuFormatR8G8B8X8, [4]byte{17, 23, 31, 127}, 255},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			res := &gpuResource{width: 2, height: 1, format: tc.format, data: bytes.Repeat(tc.pixel[:], 2)}
			for _, cursor := range []bool{false, true} {
				alpha := tc.alpha
				if cursor {
					alpha = 127
				}
				want := bytes.Repeat([]byte{17, 23, 31, alpha}, 2)
				img := image.NewRGBA(image.Rect(0, 0, 2, 1))
				img = resourceImageInto(res, cursor, img)
				if !bytes.Equal(img.Pix, want) {
					t.Fatalf("cursor=%v: pixels %v, want %v", cursor, img.Pix, want)
				}
				clear(img.Pix)
				resourceImageRectInto(res, cursor, img, image.Rect(1, 0, 2, 1), image.Point{})
				clear(want[:4])
				if !bytes.Equal(img.Pix, want) {
					t.Fatalf("cursor=%v: partial conversion %v, want %v", cursor, img.Pix, want)
				}
			}
		})
	}
}

func TestGPUResourceBGRViewport(t *testing.T) {
	t.Parallel()
	for _, format := range []uint32{gpuFormatB8G8R8X8, gpuFormatB8G8R8A8} {
		for _, cursor := range []bool{false, true} {
			for _, region := range []struct {
				name string
				rect image.Rectangle
			}{
				{name: "full", rect: image.Rect(7, 2, 72, 8)},
				{name: "partial", rect: image.Rect(8, 3, 25, 6)},
				{name: "clipped", rect: image.Rect(-4, -3, 100, 20)},
				{name: "outside", rect: image.Rect(0, 0, 3, 1)},
			} {
				name := fmt.Sprintf("format=%d/cursor=%t/%s", format, cursor, region.name)
				t.Run(name, func(t *testing.T) {
					t.Parallel()
					res := &gpuResource{width: 72, height: 9, format: format, data: make([]byte, 72*9*4)}
					for i := range res.data {
						res.data[i] = byte(i*43 + i/7)
					}
					before := bytes.Clone(res.data)
					parent := image.NewRGBA(image.Rect(0, 0, 76, 12))
					for i := range parent.Pix {
						parent.Pix[i] = 0xa5
					}
					want := bytes.Clone(parent.Pix)
					img := parent.SubImage(image.Rect(2, 3, 67, 9)).(*image.RGBA)
					origin := image.Pt(5, -1)
					for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
						for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
							source := image.Pt(x, y).Add(origin)
							if !source.In(region.rect) {
								continue
							}
							s, d := (source.Y*int(res.width)+source.X)*4, parent.PixOffset(x, y)
							want[d], want[d+1], want[d+2] = res.data[s+2], res.data[s+1], res.data[s]
							want[d+3] = res.data[s+3]
							if format == gpuFormatB8G8R8X8 && !cursor {
								want[d+3] = 255
							}
						}
					}
					resourceImageRectInto(res, cursor, img, region.rect, origin)
					if !bytes.Equal(parent.Pix, want) {
						t.Fatal("conversion changed pixels or padding outside the clipped viewport")
					}
					if !bytes.Equal(res.data, before) {
						t.Fatal("conversion changed the resource")
					}
				})
			}
		}
	}
}

func TestGPUFlushPreservesInFlightFrames(t *testing.T) {
	t.Parallel()
	d := gpuTestFramebuffer{newFramebuffer()}
	t.Cleanup(func() { _ = d.Close() })
	g := &GPU{display: d, scanout: [gpuNumScanouts]uint32{1}}
	res := &gpuResource{width: 8, height: 8, format: gpuFormatB8G8R8X8, data: bytes.Repeat([]byte{7, 13, 29, 0}, 64)}
	g.cursor.image = image.NewRGBA(image.Rect(0, 0, 64, 64))
	copy(g.cursor.image.Pix, []byte{0, 255, 0, 255})
	g.cursor.x, g.cursor.y = 2, 2
	g.flush(res)
	old, _, _ := d.changedFrame(0, false)
	want := bytes.Clone(old.pix)

	// Transfer without flush and move the cursor: the desktop must still
	// contain the previously presented pixels, without a cursor trail.
	copy(res.data, bytes.Repeat([]byte{31, 37, 41, 0}, 64))
	g.cursor.x = 3
	g.present()
	moved, _, _ := d.changedFrame(old.seq, false)
	if !bytes.Equal(moved.pix[(2*8+2)*4:][:4], []byte{29, 13, 7, 255}) {
		t.Fatal("cursor movement published an unflushed transfer or left a trail")
	}
	g.flush(res)
	latest, _, _ := d.changedFrame(moved.seq, false)
	if !bytes.Equal(latest.pix[:4], []byte{41, 37, 31, 255}) {
		t.Fatal("flushed pixels did not reach the display")
	}
	if !bytes.Equal(old.pix, want) {
		t.Fatal("reused GPU storage changed a frame still being sent to a client")
	}

	// A scanout size change must replace both internal image buffers.
	res.width, res.height = 4, 4
	res.data = res.data[:4*4*4]
	g.flush(res)
	resized, _, _ := d.changedFrame(latest.seq, false)
	if resized.width != 4 || resized.height != 4 || len(resized.pix) != 4*4*4 {
		t.Fatalf("incorrect resized frame dimensions: %+v", resized)
	}
	if !bytes.Equal(old.pix, want) {
		t.Fatal("scanout resize changed an in-flight frame")
	}
}

func TestGPUTransferFullWidthPartialHeight(t *testing.T) {
	t.Parallel()
	const width, height = 4, 4
	mem := bytes.Repeat([]byte{13, 29, 43, 255}, width*height)
	g := NewGPU(11, nil, mem, nil)
	for _, rows := range []uint32{0, 1, 2, height} {
		res := &gpuResource{
			width: width, height: height,
			data:    make([]byte, len(mem)),
			backing: []gpuMemEntry{{addr: 0, length: uint32(len(mem))}},
		}
		g.transferToHost2D(res, 0, 0, width, rows, 0)
		want := make([]byte, len(mem))
		copy(want[:rows*width*4], mem)
		if !bytes.Equal(res.data, want) {
			t.Fatalf("transfer of %d rows copied pixels outside its rectangle", rows)
		}
	}
}
