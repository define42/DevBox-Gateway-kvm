//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
)

func BenchmarkIDctFourPacked(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	for _, stride := range []int{8, 64, 2048} {
		for _, mode := range []string{"prior-paired", "packed", "checked"} {
			b.Run(fmt.Sprintf("stride%d/%s", stride, mode), func(b *testing.B) {
				var coeff [64]int16
				for i := range coeff {
					coeff[i] = int16(i*43 - 1300)
				}
				pred, rec := make([]byte, 7*stride+8), make([]byte, 7*stride+8)
				for i := range pred {
					pred[i] = byte(i)
				}
				b.ReportAllocs()
				switch mode {
				case "prior-paired":
					for b.Loop() {
						idctFourT4RecAVX2(rec, stride, pred, stride, coeff[:])
					}
				case "packed":
					for b.Loop() {
						idctFourT4RecPackedAVX2(&rec[0], stride, &pred[0], stride, &coeff)
					}
				case "checked":
					for b.Loop() {
						WelsIDctFourT4Rec_sse2(rec, 0, int32(stride), pred, 0, int32(stride), coeff[:])
					}
				}
			})
		}
	}
}
