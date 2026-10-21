package encoder

import "testing"

func BenchmarkIDctReconstruction(b *testing.B) {
	for _, tc := range []struct {
		name string
		fn   PIDctFunc
	}{{"Scalar", WelsIDctFourT4Rec_c}, {"SIMD", WelsIDctFourT4Rec_sse2}} {
		b.Run(tc.name, func(b *testing.B) {
			var coeff [64]int16
			for i := range coeff {
				coeff[i] = int16(i*43 - 1300)
			}
			var pred, rec [8 * 64]byte
			for i := range pred {
				pred[i] = uint8(i)
			}
			b.ReportAllocs()
			b.ResetTimer()
			for b.Loop() {
				tc.fn(rec[:], 0, 64, pred[:], 0, 64, coeff[:])
			}
		})
	}
}
