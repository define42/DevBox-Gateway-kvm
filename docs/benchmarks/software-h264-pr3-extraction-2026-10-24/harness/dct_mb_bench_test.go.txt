package encoder

import (
	"fmt"
	"testing"
)

// This benchmark uses only pre-existing interfaces so the identical fixture
// can be copied into an unchanged checkout for before/after measurements.
// The scalar and wrapped controls verify batching does not bypass callbacks.
func BenchmarkWelsDctMb(b *testing.B) {
	for _, stride := range []int{16, 64, 2048} {
		b.Run(fmt.Sprintf("stride%d", stride), func(b *testing.B) {
			source, prediction := make([]byte, 3+15*stride+16), make([]byte, 5+256)
			for i := range source {
				source[i] = byte(i*37 + 13)
			}
			for i := range prediction {
				prediction[i] = byte(i*17 + 53)
			}
			wrapped := func(d []int16, src []byte, srcOff int, srcStride int32, pred []byte, predOff int, predStride int32) {
				WelsDctFourT4_sse2(d, src, srcOff, srcStride, pred, predOff, predStride)
			}
			for _, mode := range []struct {
				name      string
				transform PDctFunc
			}{{"default", WelsDctFourT4_sse2}, {"scalar", WelsDctFourT4_c}, {"wrapped", wrapped}} {
				b.Run(mode.name, func(b *testing.B) {
					var output [256]int16
					b.ReportAllocs()
					for b.Loop() {
						WelsDctMb(output[:], source, 3, int32(stride), prediction, 5, mode.transform)
					}
				})
			}
		})
	}
}
