package common

import "testing"

func BenchmarkChromaInterpolation(b *testing.B) {
	for _, tc := range []struct {
		name string
		fn   PWelsMcFunc
	}{{"Scalar", McChroma_c}, {"SIMD", McChroma_sse2}} {
		b.Run(tc.name, func(b *testing.B) {
			var src, dst [10 * 64]byte
			for i := range src {
				src[i] = uint8(i * 19)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tc.fn(src[:], 0, 64, dst[:], 0, 64, 3, 5, 8, 8)
			}
		})
	}
}
