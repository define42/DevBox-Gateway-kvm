//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

// mcRefinementSourceLayout describes precisely the visible source span,
// including the two leading and three trailing filter taps.
func mcRefinementSourceLayout(kernel string, width, height, stride int) (offset, length int) {
	switch kernel {
	case "horizontal":
		return 2, (height-1)*stride + width + 5
	case "vertical":
		return 2 * stride, (height+4)*stride + width
	default:
		return 2*stride + 2, (height+4)*stride + width + 5
	}
}

func TestMcAVX2RefinementShapes(t *testing.T) {
	// Include the new refinement dimensions, odd and short row counts, and
	// adjacent unsupported dimensions that must retain the existing paths.
	shapes := [][2]int{
		{5, 1}, {5, 2}, {5, 4}, {5, 5}, {9, 8}, {9, 9}, {17, 16}, {17, 17},
		{4, 5}, {8, 9}, {16, 17}, {16, 16}, {3, 3}, {6, 5}, {7, 9},
		{10, 9}, {15, 17}, {18, 17},
	}
	rng := rand.New(rand.NewSource(0x52917))
	for _, kernel := range mcAVX2TestKernels {
		for _, shape := range shapes {
			width, height := shape[0], shape[1]
			if kernel.name == "center" && width > 17 {
				continue // The scalar center reference uses a fixed 17-wide scratch buffer.
			}
			t.Run(fmt.Sprintf("%s/%dx%d", kernel.name, width, height), func(t *testing.T) {
				stride, dstStride := width+9, width+3
				srcOff, srcLen := mcRefinementSourceLayout(kernel.name, width, height, stride)
				// Both views are unaligned and end at the last required sample.
				// Extra capacity and backing canaries expose writes beyond len.
				srcBacking := bytes.Repeat([]byte{0xAD}, srcLen+11)
				src := srcBacking[3 : 3+srcLen]
				dstLen := 1 + (height-1)*dstStride + width
				for pattern := 0; pattern < 7; pattern++ {
					for i := range src {
						switch pattern {
						case 0:
							src[i] = 0
						case 1:
							src[i] = 255
						case 2:
							src[i] = byte((i & 1) * 255)
						case 3:
							src[i] = byte(((i / stride) & 1) * 255)
						default:
							src[i] = byte(rng.Intn(256))
						}
					}
					before := bytes.Clone(srcBacking)
					want := bytes.Repeat([]byte{0xCD}, dstLen+8)
					got := bytes.Clone(want)
					kernel.c(src, srcOff, int32(stride), want[:dstLen], 1, int32(dstStride),
						int32(width), int32(height))
					kernel.avx2(src, srcOff, int32(stride), got[:dstLen], 1, int32(dstStride),
						int32(width), int32(height))
					if !bytes.Equal(got, want) {
						t.Fatalf("pattern %d: output or destination canary differs", pattern)
					}
					if !bytes.Equal(srcBacking, before) {
						t.Fatalf("pattern %d: source or source canary changed", pattern)
					}
				}
			})
		}
	}
}

func TestMcAVX2RefinementImpulses(t *testing.T) {
	for _, kernel := range mcAVX2TestKernels {
		for _, width := range []int{5, 9, 17} {
			t.Run(fmt.Sprintf("%s/width%d", kernel.name, width), func(t *testing.T) {
				height, stride := width, width+5
				srcOff, srcLen := mcRefinementSourceLayout(kernel.name, width, height, stride)
				src := make([]byte, srcLen)
				want, got := make([]byte, width*height), make([]byte, width*height)
				// Moving one impulse through every source/halo position catches
				// tap swaps, lane joins, and stale row registers independently.
				for impulse := range src {
					src[impulse] = 255
					kernel.c(src, srcOff, int32(stride), want, 0, int32(width), int32(width), int32(height))
					kernel.avx2(src, srcOff, int32(stride), got, 0, int32(width), int32(width), int32(height))
					if !bytes.Equal(got, want) {
						t.Fatalf("impulse at source index %d differs", impulse)
					}
					src[impulse] = 0
				}
			})
		}
	}
}

func TestMcAVX2RefinementOverlap(t *testing.T) {
	for _, kernel := range mcAVX2TestKernels {
		for _, width := range []int32{4, 5, 8, 9, 16, 17} {
			t.Run(fmt.Sprintf("%s/width%d", kernel.name, width), func(t *testing.T) {
				const stride, srcOff = 32, 3*32 + 3
				for _, delta := range []int{-stride + 1, -1, 0, 1, 4, 8, 16, stride} {
					want := make([]byte, stride*24)
					for i := range want {
						want[i] = byte(i*37 + 13)
					}
					got := bytes.Clone(want)
					// Existing SSE2 ordering is the contract for overlapping
					// horizontal/vertical input and output, not scalar ordering.
					kernel.sse2(want, srcOff, stride, want, srcOff+delta, stride, width, 17)
					kernel.avx2(got, srcOff, stride, got, srcOff+delta, stride, width, 17)
					if !bytes.Equal(got, want) {
						t.Fatalf("overlap delta %d differs", delta)
					}
				}
			})
		}
	}
}

func TestMcAVX2RefinementNegativeStrides(t *testing.T) {
	for _, kernel := range mcAVX2TestKernels {
		for _, width := range []int32{4, 5, 8, 9, 16, 17} {
			t.Run(fmt.Sprintf("%s/width%d", kernel.name, width), func(t *testing.T) {
				const height, stride = 9, 32
				src := make([]byte, stride*(height+5))
				for i := range src {
					src[i] = byte(i*29 + 37)
				}
				for _, reverseSrc := range []bool{false, true} {
					for _, reverseDst := range []bool{false, true} {
						srcOff, srcStride := 2*stride+2, int32(stride)
						dstOff, dstStride := 1, int32(stride)
						if reverseSrc {
							srcOff, srcStride = (height+2)*stride+2, -stride
						}
						if reverseDst {
							dstOff, dstStride = 1+(height-1)*stride, -stride
						}
						want := bytes.Repeat([]byte{0xDB}, stride*height+8)
						got := bytes.Clone(want)
						kernel.c(src, srcOff, srcStride, want, dstOff, dstStride, width, height)
						kernel.avx2(src, srcOff, srcStride, got, dstOff, dstStride, width, height)
						if !bytes.Equal(got, want) {
							t.Fatalf("reverse source=%v destination=%v differs", reverseSrc, reverseDst)
						}
					}
				}
			})
		}
	}
}

func TestMcAVX2RefinementShortVisibleBuffers(t *testing.T) {
	for _, kernel := range mcAVX2TestKernels {
		for _, width := range []int{4, 5, 8, 9, 16, 17} {
			for _, shortSource := range []bool{false, true} {
				t.Run(fmt.Sprintf("%s/width%d/source%v", kernel.name, width, shortSource), func(t *testing.T) {
					const height = 9
					stride := width + 5
					srcOff, srcLen := mcRefinementSourceLayout(kernel.name, width, height, stride)
					src, dst := make([]byte, srcLen+32), make([]byte, width*height+32)
					src, dst = src[:srcLen], dst[:width*height]
					if shortSource {
						src = src[:len(src)-1]
					} else {
						dst = dst[:len(dst)-1]
					}
					defer func() {
						if recover() == nil {
							t.Fatal("short visible buffer did not panic despite extra capacity")
						}
					}()
					kernel.avx2(src, srcOff, int32(stride), dst, 0, int32(width), int32(width), height)
				})
			}
		}
	}
}

func TestMcAVX2RefinementOverlappingRows(t *testing.T) {
	// Strides may be shorter than the block width. Those rows share storage,
	// but disjoint source and destination buffers still have scalar semantics.
	for _, kernel := range mcAVX2TestKernels {
		for _, width := range []int{5, 9, 17} {
			for _, stride := range []int{0, 1, width, width + 5} {
				for _, dstStride := range []int{0, 1, width - 1, width, width + 3} {
					t.Run(fmt.Sprintf("%s/width%d/src%d/dst%d", kernel.name, width, stride, dstStride), func(t *testing.T) {
						const height = 5
						srcOff, srcLen := mcRefinementSourceLayout(kernel.name, width, height, stride)
						src := make([]byte, srcLen)
						for i := range src {
							src[i] = byte(i*41 + 17)
						}
						want := bytes.Repeat([]byte{0xDA}, (height-1)*dstStride+width)
						got := bytes.Clone(want)
						kernel.c(src, srcOff, int32(stride), want, 0, int32(dstStride), int32(width), height)
						kernel.avx2(src, srcOff, int32(stride), got, 0, int32(dstStride), int32(width), height)
						if !bytes.Equal(got, want) {
							t.Fatal("overlapping rows differ")
						}
					})
				}
			}
		}
	}
}
