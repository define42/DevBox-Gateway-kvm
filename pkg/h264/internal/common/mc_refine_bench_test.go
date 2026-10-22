//go:build amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
)

// BenchmarkMcAVX2Refinement measures the extra row or column required while
// evaluating the neighboring half-pel candidates for 4-, 8- and 16-wide blocks.
func BenchmarkMcAVX2Refinement(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	const srcStride, dstStride = 48, 32
	src := make([]byte, srcStride*24)
	dst := make([]byte, dstStride*17)
	for i := range src {
		src[i] = byte(i*29 + 41)
	}
	for _, kernel := range mcAVX2TestKernels {
		shapes := [][2]int32{{5, 4}, {9, 8}, {17, 16}, {16, 16}}
		if kernel.name == "vertical" {
			shapes = [][2]int32{{4, 5}, {8, 9}, {16, 17}, {16, 16}}
		} else if kernel.name == "center" {
			shapes = [][2]int32{{5, 5}, {9, 9}, {17, 17}, {16, 16}}
		}
		for _, shape := range shapes {
			width, height := shape[0], shape[1]
			b.Run(fmt.Sprintf("%s/%dx%d", kernel.name, width, height), func(b *testing.B) {
				b.ReportAllocs()
				b.SetBytes(int64(width * height))
				for b.Loop() {
					kernel.avx2(src, 3*srcStride+3, srcStride, dst, 0, dstStride, width, height)
				}
				mcSIMDBenchmarkSink = dst[0]
			})
		}
	}
}
