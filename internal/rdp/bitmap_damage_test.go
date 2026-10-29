package rdp

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"io"
	"math/rand/v2"
	"strconv"
	"testing"
)

func TestBitmapDamageMatchesFullConversion(t *testing.T) {
	t.Parallel()
	for _, depth := range []int{16, 24, 32} {
		for _, size := range []image.Point{image.Pt(143, 97), image.Pt(27, 35), image.Pt(199, 173)} {
			t.Run(strconv.Itoa(depth)+"/"+size.String(), func(t *testing.T) {
				t.Parallel()
				partial, err := NewBitmapEncoder(143, 97, depth)
				if err != nil {
					t.Fatal(err)
				}
				full, err := NewBitmapEncoder(143, 97, depth)
				if err != nil {
					t.Fatal(err)
				}
				parent := image.NewRGBA(image.Rect(-7, -9, size.X+3, size.Y+5))
				img := parent.SubImage(image.Rect(-3, -5, size.X-3, size.Y-5)).(*image.RGBA)
				random := rand.New(rand.NewPCG(53, 47)) //nolint:gosec // Deterministic test pixels.
				for index := range parent.Pix {
					parent.Pix[index] = byte(random.Uint32())
				}
				actual := image.NewRGBA(image.Rect(0, 0, 143, 97))
				want := image.NewRGBA(actual.Rect)
				for frame := range 30 {
					regions := make([]image.Rectangle, 0, 3)
					if frame > 0 {
						for range 3 {
							x := img.Rect.Min.X + random.IntN(img.Rect.Dx())
							y := img.Rect.Min.Y + random.IntN(img.Rect.Dy())
							region := image.Rect(x, y, x+1+random.IntN(9), y+1+random.IntN(7)).Intersect(img.Rect)
							for row := region.Min.Y; row < region.Max.Y; row++ {
								for col := region.Min.X; col < region.Max.X; col++ {
									img.SetRGBA(col, row, color.RGBA{byte(random.Uint32()), byte(random.Uint32()), byte(random.Uint32()), 255})
								}
							}
							regions = append(regions, region)
						}
					}
					if err := partial.WriteFrameDamage(img, regions, false, func(packet []byte) error {
						decodeTestBitmap(t, actual, packet, depth)

						return nil
					}); err != nil {
						t.Fatal(err)
					}
					if err := full.WriteFrame(img, false, func(packet []byte) error {
						decodeTestBitmap(t, want, packet, depth)

						return nil
					}); err != nil {
						t.Fatal(err)
					}
					if !bytes.Equal(actual.Pix, want.Pix) {
						t.Fatalf("frame %d partial bitmap differs from full conversion", frame)
					}
					if err := partial.WriteFrameDamage(img, regions, false, func([]byte) error {
						t.Fatal("unchanged damage emitted bitmap data")

						return nil
					}); err != nil {
						t.Fatal(err)
					}
				}
			})
		}
	}
}

func TestBitmapDamageRetainsFailedUpdates(t *testing.T) {
	t.Parallel()
	encoder, err := NewBitmapEncoder(128, 128, 32)
	if err != nil {
		t.Fatal(err)
	}
	img := image.NewRGBA(image.Rect(0, 0, 128, 128))
	decoded := image.NewRGBA(img.Rect)
	updates := 0
	failAfter := -1
	send := func(packet []byte) error {
		if updates == failAfter {
			return io.ErrClosedPipe
		}
		updates++
		decodeTestBitmap(t, decoded, packet, 32)

		return nil
	}
	if err := encoder.WriteFrameDamage(img, []image.Rectangle{}, false, send); err != nil || updates != 8 {
		t.Fatalf("initial empty damage: %d updates, %v", updates, err)
	}
	img.SetRGBA(1, 1, color.RGBA{R: 255, A: 255})
	img.SetRGBA(1, 63, color.RGBA{R: 255, G: 255, A: 255})
	img.SetRGBA(100, 1, color.RGBA{G: 255, A: 255})
	updates = 0
	// Fail the second strip of the first tile, after its first 63 rows were
	// delivered. A retry must resend both strips and all other dirty tiles.
	failAfter = 1
	regions := []image.Rectangle{image.Rect(1, 1, 2, 64), image.Rect(100, 1, 101, 2)}
	err = encoder.WriteFrameDamage(img, regions, false, send)
	if !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("send failure = %v", err)
	}
	img.SetRGBA(1, 100, color.RGBA{B: 255, A: 255})
	updates = 0
	failAfter = -1
	regions = []image.Rectangle{image.Rect(1, 100, 2, 101)}
	if err := encoder.WriteFrameDamage(img, regions, false, send); err != nil || updates != 6 {
		t.Fatalf("retry with different damage: %d updates, %v", updates, err)
	}
	for _, pixel := range []image.Point{image.Pt(1, 1), image.Pt(1, 63), image.Pt(100, 1), image.Pt(1, 100)} {
		if decoded.RGBAAt(pixel.X, pixel.Y) != img.RGBAAt(pixel.X, pixel.Y) {
			t.Fatalf("retry lost damage at %v", pixel)
		}
	}
	img.SetRGBA(100, 100, color.RGBA{R: 255, B: 255, A: 255})
	updates = 0
	if err := encoder.WriteFrameDamage(img, []image.Rectangle{}, false, send); err != nil || updates != 0 {
		t.Fatal("empty damage scanned unrelated source pixels")
	}
	if err := encoder.WriteFrameDamage(img, []image.Rectangle{}, true, send); err != nil || updates != 8 ||
		decoded.RGBAAt(100, 100) != img.RGBAAt(100, 100) {
		t.Fatal("forced refresh omitted changes outside supplied damage")
	}
	resized := image.NewRGBA(image.Rect(-3, 5, 30, 54))
	resized.SetRGBA(29, 53, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	updates = 0
	if err := encoder.WriteFrameDamage(resized, []image.Rectangle{}, false, send); err != nil || updates != 8 ||
		decoded.RGBAAt(127, 127) != resized.RGBAAt(29, 53) {
		t.Fatal("source resize omitted complete reconstruction")
	}
}

func TestBitmapDamageRejectsMalformedImage(t *testing.T) {
	t.Parallel()
	encoder, err := NewBitmapEncoder(32, 32, 24)
	if err != nil {
		t.Fatal(err)
	}
	img := &image.RGBA{Pix: make([]byte, 16), Stride: -1, Rect: image.Rect(0, 0, 2, 2)}
	if err := encoder.WriteFrameDamage(img, nil, false, func([]byte) error {
		t.Fatal("malformed image reached bitmap sender")

		return nil
	}); !errors.Is(err, ErrBitmapFormat) {
		t.Fatalf("malformed image error = %v", err)
	}
}

func BenchmarkBitmapDamage(b *testing.B) {
	for _, size := range []struct {
		name          string
		width, height int
	}{
		{"1080p", 1920, 1080},
		{"4K", 3840, 2160},
	} {
		for _, partial := range []bool{false, true} {
			name := size.name + "/Full"
			if partial {
				name = size.name + "/64x32"
			}
			b.Run(name, func(b *testing.B) {
				img := image.NewRGBA(image.Rect(0, 0, size.width, size.height))
				encoder, err := NewBitmapEncoder(size.width, size.height, 32)
				if err != nil {
					b.Fatal(err)
				}
				send := func([]byte) error { return nil }
				if err := encoder.WriteFrame(img, false, send); err != nil {
					b.Fatal(err)
				}
				var regions []image.Rectangle
				if partial {
					regions = []image.Rectangle{image.Rect(64, 64, 128, 96)}
				}
				b.ReportAllocs()
				for b.Loop() {
					img.Pix[img.PixOffset(64, 64)] ^= 255
					if err := encoder.WriteFrameDamage(img, regions, false, send); err != nil {
						b.Fatal(err)
					}
				}
			})
		}
	}
}
