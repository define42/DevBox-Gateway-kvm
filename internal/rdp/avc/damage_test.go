package avc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"
)

type damageCodec struct {
	pixels []byte
	force  bool
	calls  int
	fail   bool
}

func (c *damageCodec) encode(pixels []byte, force bool, _ int64) ([]byte, error) {
	c.calls++
	c.pixels = append(c.pixels[:0], pixels...)
	c.force = force
	if c.fail {
		c.fail = false

		return nil, ErrCodec
	}

	return []byte{1}, nil
}

func (c *damageCodec) close() {}

func TestEncoderDamageMatchesFullConversion(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                      string
		sourceWidth, sourceHeight int
		width, height             int
	}{
		{"native", 128, 96, 128, 96},
		{"upscale", 47, 35, 128, 96},
		{"downscale", 201, 133, 128, 96},
		{"odd-source", 17, 21, 32, 16},
		{"single-pixel", 1, 1, 16, 16},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			codec := &damageCodec{}
			encoder := &Encoder{
				codec: codec, frameRate: FrameRate, width: test.width, height: test.height,
				i420: make([]byte, test.width*test.height*3/2),
			}
			parent := image.NewRGBA(image.Rect(-7, -9, test.sourceWidth+3, test.sourceHeight+5))
			img := parent.SubImage(image.Rect(-3, -5, test.sourceWidth-3, test.sourceHeight-5)).(*image.RGBA)
			random := rand.New(rand.NewPCG(37, 19)) //nolint:gosec // Deterministic test pixels.
			for index := range parent.Pix {
				parent.Pix[index] = byte(random.Uint32())
			}
			for frame := range 40 {
				regions := make([]image.Rectangle, 0, 3)
				if frame > 0 {
					// Several changes model updates coalesced while waiting for an
					// ACK. Odd edges exercise adjacent 2x2 chroma dependencies.
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
				if _, err := encoder.EncodeDamage(img, regions, false); err != nil {
					t.Fatal(err)
				}
				if want := referenceI420(test.width, test.height, img); !bytes.Equal(codec.pixels, want) {
					t.Fatalf("frame %d partial conversion differs from full conversion", frame)
				}
				calls := codec.calls
				if encoded, err := encoder.EncodeDamage(img, regions, false); err != nil || encoded != nil || codec.calls != calls {
					t.Fatalf("unchanged damage encoded another frame: calls %d -> %d, data %v, err %v",
						calls, codec.calls, encoded, err)
				}
			}
		})
	}
}

func TestEncoderDamageRetryAndRefresh(t *testing.T) {
	t.Parallel()
	codec := &damageCodec{}
	encoder := &Encoder{codec: codec, frameRate: FrameRate, width: 32, height: 32, i420: make([]byte, 32*32*3/2)}
	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	if _, err := encoder.EncodeDamage(img, []image.Rectangle{}, false); err != nil || codec.calls != 1 {
		t.Fatalf("initial frame with empty damage: calls %d, err %v", codec.calls, err)
	}
	img.SetRGBA(1, 1, color.RGBA{R: 255})
	codec.fail = true
	if _, err := encoder.EncodeDamage(img, []image.Rectangle{image.Rect(1, 1, 2, 2)}, false); !errors.Is(err, ErrCodec) {
		t.Fatalf("encode failure = %v", err)
	}
	img.SetRGBA(29, 29, color.RGBA{G: 255})
	if _, err := encoder.EncodeDamage(img, []image.Rectangle{image.Rect(29, 29, 30, 30)}, false); err != nil {
		t.Fatal(err)
	}
	if !codec.force || !bytes.Equal(codec.pixels, referenceI420(32, 32, img)) {
		t.Fatal("retry lost earlier damage or failed to request an independent frame")
	}
	calls := codec.calls
	// An omitted change must not be scanned by an empty damage update; an
	// explicit refresh must reconstruct everything, including omitted changes.
	img.SetRGBA(15, 15, color.RGBA{B: 255})
	if data, err := encoder.EncodeDamage(img, []image.Rectangle{}, false); data != nil || err != nil ||
		codec.calls != calls {
		t.Fatal("empty damage read unrelated source pixels")
	}
	if _, err := encoder.EncodeDamage(img, []image.Rectangle{}, true); err != nil || !codec.force ||
		!bytes.Equal(codec.pixels, referenceI420(32, 32, img)) {
		t.Fatal("forced refresh did not reconstruct entire image")
	}
	// Replacing the source image must also rebuild the retained image even
	// when the first frame at the new geometry has no dirty rectangles.
	resized := image.NewRGBA(image.Rect(-2, 3, 17, 36))
	resized.SetRGBA(16, 35, color.RGBA{R: 255, G: 255, B: 255})
	if _, err := encoder.EncodeDamage(resized, []image.Rectangle{}, false); err != nil ||
		!bytes.Equal(codec.pixels, referenceI420(32, 32, resized)) {
		t.Fatal("source resize did not reconstruct entire image")
	}
}
