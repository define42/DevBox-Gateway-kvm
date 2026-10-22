//go:build amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
)

// This checked whole-call benchmark uses only round9 entry points, so the
// same file can be copied unchanged to the frozen baseline.
func BenchmarkMcCenterWholeCaller(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	for _, shape := range [][2]int{{5, 5}, {9, 9}, {17, 17}, {9, 17}, {17, 9}, {5, 9}, {9, 5}, {4, 4}, {8, 8}, {16, 16}} {
		for _, stride := range []int{48, 2048} {
			b.Run(fmt.Sprintf("%dx%d/stride%d", shape[0], shape[1], stride), func(b *testing.B) {
				width, height := shape[0], shape[1]
				off, length := mcRefinementSourceLayout("center", width, height, stride)
				src := make([]byte, length)
				dst := make([]byte, 32*height)
				for i := range src {
					src[i] = byte(i*37 + 11)
				}
				b.ReportAllocs()
				for b.Loop() {
					McHorVer22_avx2(src, off, int32(stride), dst, 0, 32, int32(width), int32(height))
				}
				mcSIMDBenchmarkSink = dst[0]
			})
		}
	}
}
