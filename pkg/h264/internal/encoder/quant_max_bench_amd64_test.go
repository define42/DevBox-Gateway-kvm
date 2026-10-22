//go:build amd64

package encoder

import (
	"fmt"
	"testing"
)

// This file also compiles against the SSE2-only baseline. Include input restore
// and the production bounds/dispatch wrapper, as used by the skip decision.
func BenchmarkQuantFour4x4MaxChecked(b *testing.B) {
	for _, qp := range []int{10, 26, 42} {
		for _, density := range []string{"zero", "sparse", "dense", "extremes"} {
			b.Run(fmt.Sprintf("QP%d/%s", qp, density), func(b *testing.B) {
				var input, dct [64]int16
				for i := range input {
					switch density {
					case "sparse":
						if i%11 == 0 {
							input[i] = int16(i*17 - 300)
						}
					case "dense":
						input[i] = int16(i*71 - 2000)
					case "extremes":
						input[i] = [...]int16{-32768, -32767, -1, 0, 1, 32766, 32767}[i%7]
					}
				}
				ff, mf := g_kiQuantInterFF[qp], g_kiQuantMF[qp]
				var maxima [4]int16
				b.ReportAllocs()
				b.SetBytes(128)
				for b.Loop() {
					dct = input
					WelsQuantFour4x4Max_sse2(dct[:], ff[:], mf[:], maxima[:])
				}
			})
		}
	}
}
