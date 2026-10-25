package avc

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"runtime"
	"testing"
)

func TestEncoderDamageHintsMatchExactI420(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name          string
		source        image.Rectangle
		width, height int
	}{
		{"native partial edges", image.Rect(-7, 9, 59, 59), 66, 50},
		{"upscale offset", image.Rect(-7, 9, 24, 30), 98, 70},
		{"downscale offset", image.Rect(13, -5, 174, 112), 66, 50},
	} {
		for _, bgrx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/BGRX=%t", test.name, bgrx), func(t *testing.T) {
				t.Parallel()
				codec := &macroblockCodec{}
				encoder := &Encoder{
					codec: codec, width: test.width, height: test.height,
					frameRate: FrameRate, threads: 1,
					i420: make([]byte, test.width*test.height*3/2),
				}
				img := newExactHintImage(test.source)
				encodeExactHintFrame(t, encoder, img, nil, bgrx)
				if codec.hints != nil {
					t.Fatal("initial frame used unchanged hints")
				}
				// A change in the first row must survive all later unchanged
				// rows and repeated conversions of the same conservative damage.
				first := image.Rect(2, 0, 6, 1).Add(img.Rect.Min)
				fillExactHintRegion(img, first, color.RGBA{R: 255, G: 32, B: 16, A: 255})
				regions := []image.Rectangle{img.Rect, img.Rect.Inset(3), img.Rect}
				assertExactDamageFrame(t, encoder, codec, img, regions, bgrx)

				calls := codec.calls
				previous := bytes.Clone(codec.pixels)
				for y := img.Rect.Min.Y; y < img.Rect.Max.Y; y++ {
					for x := img.Rect.Min.X; x < img.Rect.Max.X; x++ {
						img.Pix[img.PixOffset(x, y)+3] ^= 0xff
					}
				}
				if data := encodeExactHintFrame(t, encoder, img, regions, bgrx); len(data) != 0 ||
					codec.calls != calls || !bytes.Equal(codec.pixels, previous) {
					t.Fatal("alpha or ignored X-byte changes encoded a frame")
				}

				// Pick the last sampled source pixel, including downscaled images,
				// to exercise the partial macroblock at the bottom-right edge.
				x := img.Rect.Min.X + (test.width-1)*img.Rect.Dx()/test.width
				y := img.Rect.Min.Y + (test.height-1)*img.Rect.Dy()/test.height
				img.SetRGBA(x, y, color.RGBA{R: 8, G: 255, B: 64, A: 255})
				assertExactDamageFrame(t, encoder, codec, img, regions, bgrx)

				// A formerly changed edge block is unchanged on this frame. This
				// checks that exact hints describe each frame, not accumulated damage.
				fillExactHintRegion(img, first, color.RGBA{R: 128, G: 128, B: 128, A: 255})
				assertExactDamageFrame(t, encoder, codec, img, regions, bgrx)
			})
		}
	}
}

func TestEncoderDamageHintsIncludeChromaOnlyChanges(t *testing.T) {
	t.Parallel()
	for _, bgrx := range []bool{false, true} {
		t.Run(fmt.Sprintf("BGRX=%t", bgrx), func(t *testing.T) {
			t.Parallel()
			const width, height = 66, 50
			codec := &macroblockCodec{}
			encoder := &Encoder{
				codec: codec, width: width, height: height, frameRate: FrameRate,
				i420: make([]byte, width*height*3/2), threads: 1,
			}
			img := newExactHintImage(image.Rect(0, 0, width, height))
			encodeExactHintFrame(t, encoder, img, nil, bgrx)
			patch := image.Rect(16, 16, 18, 18)
			for _, value := range []color.RGBA{
				{R: 158, G: 128, B: 38, A: 255},
				{R: 128, G: 128, B: 128, A: 255},
			} {
				previous := bytes.Clone(codec.pixels)
				fillExactHintRegion(img, patch, value)
				assertExactDamageFrame(t, encoder, codec, img, []image.Rectangle{img.Rect}, bgrx)
				if !bytes.Equal(previous[:width*height], codec.pixels[:width*height]) ||
					bytes.Equal(previous[width*height:], codec.pixels[width*height:]) {
					t.Fatal("fixture must change chroma while preserving every luma sample")
				}
			}
		})
	}
}

//nolint:paralleltest // Temporarily changes the process-wide GOMAXPROCS limit.
func TestEncoderDamageHintsExactAcrossWorkers(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	const width, height = 1280, 720
	for _, threads := range []int{2, 3, 4} {
		for _, bgrx := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dworkers/BGRX=%t", threads, bgrx), func(t *testing.T) {
				codec := &macroblockCodec{}
				encoder := &Encoder{
					codec: codec, width: width, height: height, frameRate: FrameRate,
					i420: make([]byte, width*height*3/2), threads: threads,
				}
				img := newExactHintImage(image.Rect(0, 0, width, height))
				encodeExactHintFrame(t, encoder, img, nil, bgrx)
				large := image.Rect(2, 6, width-2, height-6)
				regions := []image.Rectangle{large, large.Inset(2), large}
				// One changed row in each macroblock row leaves equal rows on
				// either side of worker boundaries, even when a band splits a block.
				for y := large.Min.Y; y < large.Max.Y; y += 16 {
					img.SetRGBA(33, y, color.RGBA{R: 240, G: 12, B: 72, A: 255})
				}
				assertExactDamageFrame(t, encoder, codec, img, regions, bgrx)
				for y := large.Min.Y; y < large.Max.Y; y += 16 {
					img.SetRGBA(65, y, color.RGBA{R: 32, G: 240, B: 96, A: 255})
				}
				assertExactDamageFrame(t, encoder, codec, img, regions, bgrx)
			})
		}
	}
}

func newExactHintImage(bounds image.Rectangle) *image.RGBA {
	parent := image.NewRGBA(bounds.Inset(-3))
	img := parent.SubImage(bounds).(*image.RGBA)
	fillExactHintRegion(img, bounds, color.RGBA{R: 128, G: 128, B: 128, A: 255})

	return img
}

func fillExactHintRegion(img *image.RGBA, region image.Rectangle, value color.RGBA) {
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			img.SetRGBA(x, y, value)
		}
	}
}

func encodeExactHintFrame(
	t *testing.T, encoder *Encoder, img *image.RGBA, regions []image.Rectangle, bgrx bool,
) []byte {
	t.Helper()
	if !bgrx {
		data, err := encoder.EncodeDamage(img, regions, false)
		if err != nil {
			t.Fatal(err)
		}

		return data
	}
	var rebased []image.Rectangle
	if regions != nil {
		rebased = make([]image.Rectangle, len(regions))
		for i, region := range regions {
			rebased[i] = region.Sub(img.Rect.Min)
		}
	}
	if err := encoder.PrepareBGRXDamage(
		packedBGRXForPreparation(img), img.Stride, img.Rect.Dx(), img.Rect.Dy(), rebased, false,
	); err != nil {
		t.Fatal(err)
	}
	data, err := encoder.EncodePrepared()
	if err != nil {
		t.Fatal(err)
	}

	return data
}

func assertExactDamageFrame(
	t *testing.T, encoder *Encoder, codec *macroblockCodec, img *image.RGBA, regions []image.Rectangle, bgrx bool,
) {
	t.Helper()
	previous := bytes.Clone(codec.pixels)
	calls := codec.calls
	if data := encodeExactHintFrame(t, encoder, img, regions, bgrx); len(data) == 0 || codec.calls != calls+1 {
		t.Fatal("changed fixture did not encode exactly one frame")
	}
	if want := referenceI420(encoder.width, encoder.height, img); !bytes.Equal(codec.pixels, want) {
		t.Fatal("partial conversion differs from complete reference conversion")
	}
	assertExactI420Hints(t, previous, codec.pixels, codec.hints, encoder.width, encoder.height)
}

func assertExactI420Hints(t *testing.T, previous, current []byte, hints []bool, width, height int) {
	t.Helper()
	columns := (width + 15) / 16
	want := make([]bool, columns*((height+15)/16))
	for i := range want {
		want[i] = true
	}
	if len(previous) != width*height*3/2 || len(current) != len(previous) {
		t.Fatalf("unexpected I420 lengths: previous %d, current %d", len(previous), len(current))
	}
	for _, plane := range []struct{ offset, width, height, scale int }{
		{0, width, height, 1},
		{width * height, width / 2, height / 2, 2},
		{width * height * 5 / 4, width / 2, height / 2, 2},
	} {
		for y := range plane.height {
			for x := range plane.width {
				index := plane.offset + y*plane.width + x
				if previous[index] != current[index] {
					want[(y*plane.scale/16)*columns+x*plane.scale/16] = false
				}
			}
		}
	}
	if len(hints) != len(want) {
		t.Fatalf("unchanged hint count = %d, want %d", len(hints), len(want))
	}
	for i, unchanged := range want {
		if hints[i] != unchanged {
			t.Fatalf("macroblock (%d,%d) unchanged hint = %t, want %t from exact Y/U/V samples",
				i%columns, i/columns, hints[i], unchanged)
		}
	}
}
