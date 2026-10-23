//go:build amd64

package avc

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"testing"

	"golang.org/x/sys/cpu"
)

type i420AVX2Format struct {
	name    string
	convert func(dstY0, dstY1, dstU, dstV, src0, src1 []byte) (bool, bool)
	bgrx    bool
}

// i420AVX2Formats names both source layouts accepted by the vector kernels.
func i420AVX2Formats() []i420AVX2Format {
	return []i420AVX2Format{
		{"RGBA", rgbaToI420AVX2, false},
		{"BGRX", bgrxToI420AVX2, true},
	}
}

// referenceRGB32I420Rows swaps BGRX input into RGBA order for the scalar reference.
func referenceRGB32I420Rows(src0, src1 []byte, width int, bgrx bool) (y0, y1, u, v []byte) {
	if bgrx {
		src0, src1 = swapRedBlue(src0), swapRedBlue(src1)
	}

	return referenceRGBAI420Rows(src0, src1, width)
}

func swapRedBlue(src []byte) []byte {
	swapped := bytes.Clone(src)
	for i := 0; i+3 < len(swapped); i += 4 {
		swapped[i], swapped[i+2] = swapped[i+2], swapped[i]
	}

	return swapped
}

// Every color is presented as a uniform 2x2 block, so each luma sample and each
// exact chroma average is compared against the scalar formulas.
func TestRGB32ToI420AVX2WideExhaustiveColors(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}
	if testing.Short() {
		t.Skip("exhaustive color test skipped in short mode")
	}

	const width = 512 // Blue varies along the row, one two-pixel block each.
	const colors = 1 << 24
	wantY, wantU, wantV := make([]byte, colors), make([]byte, colors), make([]byte, colors)
	for color := range colors {
		red, green, blue := color>>16, (color>>8)&0xff, color&0xff
		wantY[color] = byte((54*red + 183*green + 18*blue) >> 8)
		wantU[color] = byte(((-29*red - 99*green + 128*blue) >> 8) + 128)
		wantV[color] = byte(((128*red - 116*green - 12*blue) >> 8) + 128)
	}

	for _, format := range i420AVX2Formats() {
		t.Run(format.name, func(t *testing.T) {
			t.Parallel()
			red, blue := 0, 2
			if format.bgrx {
				red, blue = 2, 0
			}
			src := make([]byte, width*4)
			y0, y1 := make([]byte, width), make([]byte, width)
			u, v := make([]byte, width/2), make([]byte, width/2)
			for r := range 256 {
				for g := range 256 {
					for b := range 256 {
						for _, pixel := range []int{2 * b, 2*b + 1} {
							src[pixel*4+red], src[pixel*4+1], src[pixel*4+blue] = byte(r), byte(g), byte(b)
							src[pixel*4+3] = byte(pixel * 7) // Alpha or padding must not matter.
						}
					}
					if _, ok := format.convert(y0, y1, u, v, src, src); !ok {
						t.Fatal("valid input was rejected on an AVX2 CPU")
					}
					base := r<<16 | g<<8
					for b := range 256 {
						color := base | b
						if y0[2*b] != wantY[color] || y0[2*b+1] != wantY[color] ||
							y1[2*b] != wantY[color] || y1[2*b+1] != wantY[color] ||
							u[b] != wantU[color] || v[b] != wantV[color] {
							t.Fatalf("color %06x = Y %d,%d,%d,%d U %d V %d, want Y %d U %d V %d",
								color, y0[2*b], y0[2*b+1], y1[2*b], y1[2*b+1], u[b], v[b],
								wantY[color], wantU[color], wantV[color])
						}
					}
				}
			}
		})
	}
}

// Random 2x2 blocks check the floor of non-uniform channel sums for every wide
// span shape, including each overlapping-tail length.
func TestRGB32ToI420AVX2WideMatchesScalar(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}

	widths := []int{maxDimension}
	for width := 32; width <= 258; width += 2 {
		widths = append(widths, width)
	}
	extremes := []byte{0, 1, 2, 3, 127, 128, 252, 253, 254, 255}
	for _, format := range i420AVX2Formats() {
		t.Run(format.name, func(t *testing.T) {
			t.Parallel()
			for _, width := range widths {
				for _, extreme := range []bool{false, true} {
					random := rand.New(rand.NewPCG(uint64(width), 0x9e3779b97f4a7c15)) //nolint:gosec // Deterministic test data.
					src0, src1 := make([]byte, width*4), make([]byte, width*4)
					for _, row := range [][]byte{src0, src1} {
						for i := range row {
							if extreme {
								row[i] = extremes[random.IntN(len(extremes))]
							} else {
								row[i] = byte(random.Uint32())
							}
						}
					}
					y0, y1 := make([]byte, width), make([]byte, width)
					u, v := make([]byte, width/2), make([]byte, width/2)
					for _, plane := range [][]byte{y0, y1, u, v} {
						for i := range plane {
							plane[i] = byte(random.Uint32())
						}
					}
					wantY0, wantY1, wantU, wantV := referenceRGB32I420Rows(src0, src1, width, format.bgrx)
					wantChanged := !bytes.Equal(y0, wantY0) || !bytes.Equal(y1, wantY1) ||
						!bytes.Equal(u, wantU) || !bytes.Equal(v, wantV)

					changed, ok := format.convert(y0, y1, u, v, src0, src1)
					if !ok {
						t.Fatalf("width %d: valid input was rejected on an AVX2 CPU", width)
					}
					if changed != wantChanged {
						t.Fatalf("width %d: changed = %t, want %t", width, changed, wantChanged)
					}
					checkI420SIMDPlanes(t, y0, y1, u, v, wantY0, wantY1, wantU, wantV)
				}
			}
		})
	}
}

func TestRGB32ToI420AVX2WideChangedAndRepair(t *testing.T) {
	t.Parallel()
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 is unavailable")
	}

	// Two full blocks, then a final block overlapping the second by 18 pixels.
	const width = 78
	src0, src1 := make([]byte, width*4), make([]byte, width*4)
	for index := range src0 {
		src0[index] = byte(index*29 + 17)
		src1[index] = byte(index*43 + 91)
	}
	for _, format := range i420AVX2Formats() {
		t.Run(format.name, func(t *testing.T) {
			t.Parallel()
			wantY0, wantY1, wantU, wantV := referenceRGB32I420Rows(src0, src1, width, format.bgrx)
			planes := [][]byte{bytes.Clone(wantY0), bytes.Clone(wantY1), bytes.Clone(wantU), bytes.Clone(wantV)}
			if changed, ok := format.convert(planes[0], planes[1], planes[2], planes[3], src0, src1); !ok || changed {
				t.Fatalf("unchanged conversion = changed %t, ok %t", changed, ok)
			}

			for plane, length := range []int{width, width, width / 2, width / 2} {
				// First block, second block, overlapped tail and final sample.
				for _, index := range []int{0, 31 * length / width, 47 * length / width, length - 1} {
					t.Run(fmt.Sprintf("plane=%d/index=%d", plane, index), func(t *testing.T) {
						planes := [][]byte{bytes.Clone(wantY0), bytes.Clone(wantY1), bytes.Clone(wantU), bytes.Clone(wantV)}
						planes[plane][index] ^= 1
						changed, ok := format.convert(planes[0], planes[1], planes[2], planes[3], src0, src1)
						if !ok || !changed {
							t.Fatalf("mismatched destination = changed %t, ok %t", changed, ok)
						}
						checkI420SIMDPlanes(t, planes[0], planes[1], planes[2], planes[3], wantY0, wantY1, wantU, wantV)
					})
				}
			}
		})
	}
}
