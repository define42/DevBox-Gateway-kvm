package avc

import (
	"bytes"
	"image"
	"math/rand/v2"
	"testing"
)

func TestI420ScaledRegionMatchesReference(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name                      string
		sourceWidth, sourceHeight int
		width, height             int
	}{
		{"upscale", 23, 19, 64, 48},
		{"downscale", 101, 79, 64, 48},
		{"vertical-only", 64, 19, 64, 48},
		{"horizontal-only", 23, 48, 64, 48},
		{"single-pixel", 1, 1, 64, 48},
		{"maximum-output-width", 91, 19, maxDimension, 48},
		{"wide-source", maxDimension*2 + 1, 19, 64, 48},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			// Include row padding, but no accessible padding after the final
			// source pixel. Rect.Min is deliberately unrelated to pixel offsets.
			stride := test.sourceWidth*4 + 13
			img := &image.RGBA{
				Pix:    make([]byte, (test.sourceHeight-1)*stride+test.sourceWidth*4),
				Stride: stride,
				Rect:   image.Rect(-11, 7, test.sourceWidth-11, test.sourceHeight+7),
			}
			random := rand.New(rand.NewPCG(53, 71)) //nolint:gosec // Deterministic conversion fixtures.
			for index := range img.Pix {
				img.Pix[index] = byte(random.Uint32())
			}
			sourceBefore := bytes.Clone(img.Pix)
			reference := referenceI420(test.width, test.height, img)
			for _, region := range []struct {
				name string
				rect image.Rectangle
			}{
				{"full", image.Rect(0, 0, test.width, test.height)},
				{"vector", image.Rect(2, 4, 10, 8)},
				{"vector-tail", image.Rect(2, 4, 16, 10)},
				{"bottom-right", image.Rect(test.width-10, test.height-2, test.width, test.height)},
				{"tiny", image.Rect(2, 4, 4, 6)},
				{"scalar-tail", image.Rect(2, 4, 8, 8)},
				{"empty", image.Rectangle{}},
			} {
				t.Run(region.name, func(t *testing.T) {
					t.Parallel()
					const prefix, suffix = 19, 37
					storage := bytes.Repeat([]byte{0x95}, prefix+len(reference)+suffix)
					got := storage[prefix : prefix+len(reference)]
					want := bytes.Clone(got)
					copyI420TestRegion(want, reference, test.width, test.height, region.rect)
					wantChanged := !bytes.Equal(got, want)
					changed := scaleI420Region(got, test.width, test.height, img, region.rect)
					if changed != wantChanged || !bytes.Equal(got, want) {
						t.Fatalf("region conversion mismatch: changed = %t, want %t", changed, wantChanged)
					}
					if scaleI420Region(got, test.width, test.height, img, region.rect) {
						t.Fatal("unchanged region reported a change")
					}
					if !bytes.Equal(storage[:prefix], bytes.Repeat([]byte{0x95}, prefix)) ||
						!bytes.Equal(storage[prefix+len(reference):], bytes.Repeat([]byte{0x95}, suffix)) {
						t.Fatal("conversion modified output guard bytes")
					}
					if !bytes.Equal(img.Pix, sourceBefore) {
						t.Fatal("conversion modified source pixels or row padding")
					}
				})
			}
			// Alpha must not enter the color arithmetic or change detector.
			got := bytes.Clone(reference)
			alphaImage := *img
			alphaImage.Pix = bytes.Clone(img.Pix)
			for y := range test.sourceHeight {
				for x := range test.sourceWidth {
					alphaImage.Pix[y*stride+x*4+3] ^= 0xff
				}
			}
			if scaleI420Region(got, test.width, test.height, &alphaImage, image.Rect(0, 0, test.width, test.height)) ||
				!bytes.Equal(got, reference) {
				t.Fatal("alpha-only source changes affected I420 output")
			}
		})
	}
}

// Copy reference pixels only within the requested output rectangle; preserving
// the sentinel elsewhere also checks that damage conversion leaves it untouched.
func copyI420TestRegion(dst, src []byte, width, height int, region image.Rectangle) {
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			dst[y*width+x] = src[y*width+x]
		}
	}
	for plane := range 2 {
		base := width*height + plane*width*height/4
		for y := region.Min.Y / 2; y < region.Max.Y/2; y++ {
			for x := region.Min.X / 2; x < region.Max.X/2; x++ {
				dst[base+y*width/2+x] = src[base+y*width/2+x]
			}
		}
	}
}
