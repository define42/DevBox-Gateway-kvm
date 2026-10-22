//go:build amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
)

// Both levels use pre-existing entry points, permitting this benchmark file to
// run unchanged on the frozen baseline. Ordinary widths are unchanged controls.
func BenchmarkMcCenterPairs(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	for _, shape := range [][2]int{{5, 5}, {9, 9}, {17, 17}, {5, 9}, {9, 5}, {9, 17}, {17, 9}, {4, 4}, {8, 8}, {16, 16}} {
		width, height := shape[0], shape[1]
		b.Run(fmt.Sprintf("checked/%dx%d", width, height), func(b *testing.B) {
			const srcStride, dstStride = 48, 32
			src := make([]byte, srcStride*24)
			dst := make([]byte, dstStride*17)
			for i := range src {
				src[i] = byte(i*29 + 41)
			}
			b.ReportAllocs()
			for b.Loop() {
				McHorVer22_avx2(src, 3*srcStride+3, srcStride, dst, 0, dstStride, int32(width), int32(height))
			}
			mcSIMDBenchmarkSink = dst[0]
		})
		b.Run(fmt.Sprintf("raw/%dx%d", width, height), func(b *testing.B) {
			const srcStride, dstStride = 24, 32
			src := make([]int16, srcStride*17)
			dst := make([]byte, dstStride*17)
			for i := range src {
				src[i] = int16(i*41%13261 - 2550)
			}
			b.ReportAllocs()
			if width == 5 || width == 9 || width == 17 {
				for b.Loop() {
					mcHorRawRefineAVX2(&dst[0], dstStride, &src[0], srcStride, width, height)
				}
			} else if width == 4 {
				for b.Loop() {
					mcHorizontalRawToU8SSE2(dst, dstStride, src, srcStride, width, height)
				}
			} else {
				for b.Loop() {
					mcHorizontalRawToU8AVX2(dst, dstStride, src, srcStride, width, height)
				}
			}
			mcSIMDBenchmarkSink = dst[0]
		})
	}
}
