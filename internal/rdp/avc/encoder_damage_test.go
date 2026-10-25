package avc

import (
	"bytes"
	"errors"
	"image"
	"image/color"
	"math/rand/v2"
	"testing"
)

type macroblockCodec struct {
	damageCodec
	hints []bool
}

func (c *macroblockCodec) encode(pixels []byte, force bool, timestamp int64) ([]byte, error) {
	c.hints = nil

	return c.damageCodec.encode(pixels, force, timestamp)
}

func (c *macroblockCodec) encodeDamage(
	pixels []byte, force bool, timestamp int64, unchanged []bool,
) ([]byte, error) {
	c.hints = append(c.hints[:0], unchanged...)

	return c.damageCodec.encode(pixels, force, timestamp)
}

func TestEncoderDamageHintsCoverEveryChangedSample(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		source        image.Rectangle
		width, height int
	}{
		{"native partial edge", image.Rect(0, 0, 66, 50), 66, 50},
		{"upscale offset", image.Rect(-7, 9, 24, 30), 98, 70},
		{"downscale offset", image.Rect(13, -5, 174, 112), 66, 50},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			codec := &macroblockCodec{}
			encoder := &Encoder{
				codec: codec, width: test.width, height: test.height,
				frameRate: FrameRate, i420: make([]byte, test.width*test.height*3/2),
			}
			img := image.NewRGBA(test.source)
			if _, err := encoder.Encode(img, false); err != nil || codec.hints != nil {
				t.Fatalf("initial frame used hints or failed: %v", err)
			}
			random := rand.New(rand.NewPCG(42, 19)) //nolint:gosec // Deterministic image fixture.
			for range 50 {
				previous := bytes.Clone(codec.pixels)
				calls := codec.calls
				regions := make([]image.Rectangle, 0, 3)
				for range 3 {
					x := img.Rect.Min.X + random.IntN(img.Rect.Dx())
					y := img.Rect.Min.Y + random.IntN(img.Rect.Dy())
					region := image.Rect(x, y, x+1+random.IntN(9), y+1+random.IntN(9)).Intersect(img.Rect)
					for row := region.Min.Y; row < region.Max.Y; row++ {
						for col := region.Min.X; col < region.Max.X; col++ {
							img.SetRGBA(col, row, color.RGBA{
								R: byte(random.Uint32()), G: byte(random.Uint32()),
								B: byte(random.Uint32()), A: 255,
							})
						}
					}
					regions = append(regions, region)
				}
				if _, err := encoder.EncodeDamage(img, regions, false); err != nil {
					t.Fatal(err)
				}
				if codec.calls != calls {
					assertChangedSamplesNotHinted(t, previous, codec.pixels, codec.hints, test.width, test.height)
				}
			}
		})
	}
}

func assertChangedSamplesNotHinted(t *testing.T, previous, current []byte, hints []bool, width, height int) {
	t.Helper()
	columns := (width + 15) / 16
	if len(hints) != columns*((height+15)/16) {
		t.Fatalf("mask length = %d", len(hints))
	}
	for _, plane := range []struct{ offset, width, height, scale int }{
		{0, width, height, 1},
		{width * height, width / 2, height / 2, 2},
		{width * height * 5 / 4, width / 2, height / 2, 2},
	} {
		for y := range plane.height {
			for x := range plane.width {
				index := plane.offset + y*plane.width + x
				if previous[index] != current[index] && hints[(y*plane.scale/16)*columns+x*plane.scale/16] {
					t.Fatalf("changed sample at plane %d (%d,%d) was marked unchanged", plane.offset, x, y)
				}
			}
		}
	}
}

func TestEncoderDamageHintsAreConservativeAndReused(t *testing.T) {
	t.Parallel()
	encoder := &Encoder{width: 66, height: 50}
	source := image.Rect(-7, 9, 59, 59)
	mask := encoder.unchangedMacroblocks(source, []image.Rectangle{image.Rect(8, 24, 10, 26)})
	// The edit straddles both macroblock boundaries at output coordinate 16.
	for i, unchanged := range mask {
		dirty := i == 0 || i == 1 || i == 5 || i == 6
		if unchanged == dirty {
			t.Fatalf("block %d unchanged = %v", i, unchanged)
		}
	}
	first := &mask[0]
	mask = encoder.unchangedMacroblocks(source, []image.Rectangle{image.Rect(58, 58, 59, 59)})
	if &mask[0] != first {
		t.Fatal("partial frames allocated a new mask")
	}
	for i, unchanged := range mask {
		if unchanged == (i == len(mask)-1) {
			t.Fatalf("edge block %d unchanged = %v", i, unchanged)
		}
	}
}

func TestEncoderDamageHintsResetForRefreshRetryAndGeometry(t *testing.T) {
	t.Parallel()
	codec := &macroblockCodec{}
	encoder := &Encoder{
		codec: codec, width: 64, height: 48,
		frameRate: FrameRate, i420: make([]byte, 64*48*3/2),
	}
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	if _, err := encoder.Encode(img, false); err != nil {
		t.Fatal(err)
	}
	img.SetRGBA(2, 2, color.RGBA{R: 255, A: 255})
	codec.fail = true
	if _, err := encoder.EncodeDamage(img, []image.Rectangle{image.Rect(2, 2, 3, 3)}, false); !errors.Is(err, ErrCodec) ||
		len(codec.hints) == 0 {
		t.Fatalf("partial failure did not exercise hints: %v", err)
	}
	img.SetRGBA(49, 33, color.RGBA{G: 255, A: 255})
	if _, err := encoder.EncodeDamage(img, []image.Rectangle{image.Rect(49, 33, 50, 34)}, false); err != nil ||
		codec.hints != nil || !codec.force || !bytes.Equal(codec.pixels, referenceI420(64, 48, img)) {
		t.Fatalf("retry reused unsafe hints or lost failed changes: %v", err)
	}
	if _, err := encoder.EncodeDamage(img, []image.Rectangle{}, true); err != nil || codec.hints != nil || !codec.force {
		t.Fatalf("refresh reused hints: %v", err)
	}
	img.SetRGBA(63, 47, color.RGBA{B: 255, A: 255})
	if _, err := encoder.Encode(img, false); err != nil || codec.hints != nil {
		t.Fatalf("full conversion reused hints: %v", err)
	}
	resized := image.NewRGBA(image.Rect(4, 7, 85, 76))
	if _, err := encoder.EncodeDamage(resized, []image.Rectangle{}, false); err != nil || codec.hints != nil ||
		!bytes.Equal(codec.pixels, referenceI420(64, 48, resized)) {
		t.Fatalf("geometry change reused hints: %v", err)
	}
	calls := codec.calls
	if data, err := encoder.EncodeDamage(resized, []image.Rectangle{}, false); err != nil || data != nil ||
		codec.calls != calls {
		t.Fatalf("unchanged frame encoded: %v", err)
	}
}
