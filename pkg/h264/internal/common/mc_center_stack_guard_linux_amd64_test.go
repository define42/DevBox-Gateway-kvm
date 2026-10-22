//go:build linux && amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestMcCenterStackExactRowGuards(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, width := range []int{4, 5, 8, 9, 16, 17} {
		for _, height := range []int{1, 2, 5, 9, 16, 17} {
			for _, atEnd := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/end%v", width, height, atEnd), func(t *testing.T) {
					src, stride := centerPairsGuardedRows(t, height+5, width+5, atEnd, true)
					dst, dstStride := centerPairsGuardedRows(t, height, width, atEnd, false)
					srcOff := 2*stride + 2
					for _, raw := range []bool{false, true} {
						if raw {
							mcCenterRefineAVX2(&dst[0], dstStride, &src[2*stride], stride, width, height)
						} else {
							McHorVer22_avx2(src, srcOff, int32(stride), dst, 0, int32(dstStride), int32(width), int32(height))
						}
						for y := range height {
							for x := range width {
								if want := centerStackReference(src, srcOff, stride, x, y); dst[y*dstStride+x] != want {
									t.Fatalf("raw=%v pixel(%d,%d) differs", raw, x, y)
								}
							}
						}
					}
				})
			}
		}
	}
}

// Center-filter dispatch expansion must not send regular horizontal widths
// to the separate 5/9/17 refinement kernel. Exact per-row guards catch both
// widened source loads and destination stores, including its width17 default.
func TestMcCenterStackHorizontalDispatcherBoundary(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, width := range []int{4, 8, 16} {
		for _, height := range []int{1, 4, 16} {
			for _, atEnd := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/end%v", width, height, atEnd), func(t *testing.T) {
					src, stride := centerPairsGuardedRows(t, height, width+5, atEnd, true)
					dst, dstStride := centerPairsGuardedRows(t, height, width, atEnd, false)
					McHorVer20_avx2(src, 2, int32(stride), dst, 0, int32(dstStride), int32(width), int32(height))
					for y := range height {
						for x := range width {
							var sum int32
							for tap, coefficient := range [...]int32{1, -5, 20, 20, -5, 1} {
								sum += coefficient * int32(src[y*stride+x+tap])
							}
							want := byte(min(max((sum+16)>>5, 0), 255))
							if got := dst[y*dstStride+x]; got != want {
								t.Fatalf("pixel(%d,%d): got %d want %d", x, y, got, want)
							}
						}
					}
				})
			}
		}
	}
}
