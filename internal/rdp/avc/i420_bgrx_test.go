package avc

import (
	"bytes"
	"fmt"
	"image"
	"math/rand/v2"
	"runtime"
	"testing"
)

func TestBGRXI420RegionParity(t *testing.T) {
	t.Parallel()
	for _, fixture := range []struct {
		name                      string
		sourceWidth, sourceHeight int
		width, height             int
	}{
		{"native", 64, 48, 64, 48},
		{"upscale", 23, 19, 64, 48},
		{"downscale", 101, 79, 64, 48},
		{"vertical-scale", 64, 19, 64, 48},
		{"horizontal-scale", 23, 48, 64, 48},
		{"single-pixel", 1, 1, 64, 48},
		{"maximum-output-width", 91, 19, maxDimension, 48},
		{"wide-source", maxDimension*2 + 1, 19, 64, 48},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			t.Parallel()
			img, pix, storage := newBGRXI420TestSource(fixture.sourceWidth, fixture.sourceHeight)
			sourceBefore := bytes.Clone(storage)
			reference := referenceI420(fixture.width, fixture.height, img)
			for _, region := range []struct {
				name string
				rect image.Rectangle
			}{
				{"full", image.Rect(0, 0, fixture.width, fixture.height)},
				{"vector", image.Rect(2, 4, 10, 8)},
				{"vector-tail", image.Rect(2, 4, 16, 10)},
				{"bottom-right", image.Rect(fixture.width-10, fixture.height-2, fixture.width, fixture.height)},
				{"tiny", image.Rect(2, 4, 4, 6)},
				{"scalar-tail", image.Rect(2, 4, 8, 8)},
				{"empty", image.Rectangle{}},
			} {
				for _, scalar := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/scalar=%t", region.name, scalar), func(t *testing.T) {
						t.Parallel()
						const prefix, suffix = 19, 37
						destination := bytes.Repeat([]byte{0x95}, prefix+len(reference)+suffix)
						got := destination[prefix : prefix+len(reference) : prefix+len(reference)]
						want := bytes.Clone(got)
						copyI420TestRegion(want, reference, fixture.width, fixture.height, region.rect)
						wantChanged := !bytes.Equal(got, want)
						convert := scaleBGRXI420Region
						if scalar {
							convert = scaleBGRXI420RegionScalar
						}
						changed := convert(got, fixture.width, fixture.height, pix, img.Stride,
							fixture.sourceWidth, fixture.sourceHeight, region.rect)
						if changed != wantChanged || !bytes.Equal(got, want) {
							t.Fatalf("conversion changed = %t, want %t; pixels equal = %t",
								changed, wantChanged, bytes.Equal(got, want))
						}
						if convert(got, fixture.width, fixture.height, pix, img.Stride,
							fixture.sourceWidth, fixture.sourceHeight, region.rect) {
							t.Fatal("repeated conversion reported a change")
						}
						xOnly := bytes.Clone(pix)
						for y := range fixture.sourceHeight {
							for x := range fixture.sourceWidth {
								xOnly[y*img.Stride+x*4+3] ^= 0xff
							}
						}
						if convert(got, fixture.width, fixture.height, xOnly, img.Stride,
							fixture.sourceWidth, fixture.sourceHeight, region.rect) || !bytes.Equal(got, want) {
							t.Fatal("unused X-byte changes affected I420 output")
						}
						if !bytes.Equal(destination[:prefix], bytes.Repeat([]byte{0x95}, prefix)) ||
							!bytes.Equal(destination[prefix+len(reference):], bytes.Repeat([]byte{0x95}, suffix)) {
							t.Fatal("conversion changed destination guard bytes")
						}
						if !bytes.Equal(storage, sourceBefore) {
							t.Fatal("conversion changed source pixels, padding or guards")
						}
					})
				}
			}
		})
	}
}

//nolint:paralleltest // Temporarily changes the process-wide GOMAXPROCS limit.
func TestBGRXI420ParallelDamageParity(t *testing.T) {
	previousProcs := runtime.GOMAXPROCS(4)
	t.Cleanup(func() { runtime.GOMAXPROCS(previousProcs) })
	const width, height = 1024, 770
	for _, fixture := range []struct {
		name                      string
		sourceWidth, sourceHeight int
	}{
		{"native", width, height},
		{"upscale", 643, 479},
		{"downscale", 1537, 1135},
	} {
		for _, threads := range []int{1, 2, 3, 4} {
			t.Run(fmt.Sprintf("%s/%dworkers", fixture.name, threads), func(t *testing.T) {
				img, pix, _ := newBGRXI420TestSource(fixture.sourceWidth, fixture.sourceHeight)
				encoder := Encoder{
					width: width, height: height, i420: make([]byte, width*height*3/2),
					backend: BackendSoftware, threads: threads,
				}
				full := image.Rect(0, 0, width, height)
				want := referenceI420(width, height, img)
				if !encoder.convertBGRXI420Region(pix, img.Stride, fixture.sourceWidth, fixture.sourceHeight, full) ||
					!bytes.Equal(encoder.i420, want) ||
					encoder.convertBGRXI420Region(pix, img.Stride, fixture.sourceWidth, fixture.sourceHeight, full) {
					t.Fatal("initial full conversion or repeat differs")
				}

				// Change the last sampled pixel so only the final worker reports a
				// difference. Other bands must not hide that change notification.
				x := (width - 1) * fixture.sourceWidth / width
				y := (height - 1) * fixture.sourceHeight / height
				pos := y*img.Stride + x*4
				for channel := range 3 {
					img.Pix[pos+channel] ^= 0xff
					pix[pos+channel] ^= 0xff
				}
				want = referenceI420(width, height, img)
				if !encoder.convertBGRXI420Region(pix, img.Stride, fixture.sourceWidth, fixture.sourceHeight, full) ||
					!bytes.Equal(encoder.i420, want) {
					t.Fatal("change in final row band was lost")
				}

				// Modify all pixels but supply partial and overlapping damage. Only
				// converted output regions may change in the retained I420 image.
				for y := range fixture.sourceHeight {
					for x := range fixture.sourceWidth {
						for channel := range 3 {
							pos := y*img.Stride + x*4 + channel
							img.Pix[pos] ^= 0x55
							pix[pos] ^= 0x55
						}
					}
				}
				modified := referenceI420(width, height, img)
				for _, region := range []image.Rectangle{
					image.Rect(2, 8, 1020, 764),
					image.Rect(4, 6, 1022, 768),
					image.Rect(width-2, height-2, width, height),
				} {
					copyI420TestRegion(want, modified, width, height, region)
					wantChanged := !bytes.Equal(encoder.i420, want)
					if changed := encoder.convertBGRXI420Region(pix, img.Stride,
						fixture.sourceWidth, fixture.sourceHeight, region); changed != wantChanged ||
						!bytes.Equal(encoder.i420, want) {
						t.Fatal("incremental conversion differs from RGBA reference")
					}
					if encoder.convertBGRXI420Region(pix, img.Stride, fixture.sourceWidth, fixture.sourceHeight, region) {
						t.Fatal("repeated damage reported a change")
					}
				}
			})
		}
	}
}

func newBGRXI420TestSource(width, height int) (img *image.RGBA, pix, storage []byte) {
	stride := width*4 + 13
	img = &image.RGBA{
		Pix:    make([]byte, (height-1)*stride+width*4),
		Stride: stride,
		Rect:   image.Rect(-11, 7, width-11, height+7),
	}
	random := rand.New(rand.NewPCG(53, 71)) //nolint:gosec // Deterministic conversion fixture.
	for index := range img.Pix {
		img.Pix[index] = byte(random.Uint32())
	}
	const prefix, suffix = 17, 23
	storage = bytes.Repeat([]byte{0xa5}, prefix+len(img.Pix)+suffix)
	pix = storage[prefix : prefix+len(img.Pix) : prefix+len(img.Pix)]
	copy(pix, img.Pix)
	for y := range height {
		for x := range width {
			pos := y*stride + x*4
			pix[pos], pix[pos+2] = pix[pos+2], pix[pos]
		}
	}

	return img, pix, storage
}
