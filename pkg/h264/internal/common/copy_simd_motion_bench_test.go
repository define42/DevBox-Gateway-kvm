//go:build amd64

package common

import (
	"fmt"
	"testing"
)

// Existing motion entry points keep the harness identical on both trees.
func BenchmarkIntegerMotionCopy(b *testing.B) {
	for _, mode := range []struct {
		name string
		fn   PWelsMcFunc
	}{
		{"LumaSSE2", McLuma_sse2},
		{"LumaAVX2", McLuma_avx2},
		{"ChromaSSE2", McChroma_sse2},
	} {
		for _, shape := range [][2]int32{{8, 8}, {8, 16}, {16, 8}, {16, 16}, {8, 4}, {4, 4}} {
			b.Run(fmt.Sprintf("%s/%dx%d", mode.name, shape[0], shape[1]), func(b *testing.B) {
				var src, dst [32*16 + 7]byte
				for i := range src {
					src[i] = byte(i*29 + 3)
				}
				b.ReportAllocs()
				for b.Loop() {
					mode.fn(src[:], 3, 32, dst[:], 5, 32, 0, 0, shape[0], shape[1])
				}
			})
		}
	}
}
