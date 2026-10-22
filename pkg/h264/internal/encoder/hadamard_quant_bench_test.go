package encoder

import (
	"fmt"
	"testing"
)

var hadamardQuantBenchmarkSink int32

// On the baseline, provide a test-only hadamardQuant2x2 shim that forwards to
// WelsHadamardQuant2x2_c. Both versions then use this identical checked benchmark.
func BenchmarkHadamardQuant2x2(b *testing.B) {
	for _, qp := range []int{0, 26, 51} {
		for _, content := range []string{"Zero", "Dense", "Extremes"} {
			for _, checked := range []bool{false, true} {
				b.Run(fmt.Sprintf("QP%d/%s/checked=%t", qp, content, checked), func(b *testing.B) {
					var input, rs [64]int16
					for i := range input {
						switch content {
						case "Dense":
							input[i] = int16(i*71 - 2000)
						case "Extremes":
							input[i] = [...]int16{-32768, -32767, -1, 0, 1, 32766, 32767}[i%7]
						}
					}
					ff := int16(int32(g_kiQuantInterFF[qp][0]) << 1)
					mf := g_kiQuantMF[qp][0] >> 1
					var dct, block [4]int16
					b.ReportAllocs()
					if checked {
						for b.Loop() {
							rs = input
							hadamardQuantBenchmarkSink = hadamardQuant2x2(rs[:], ff, mf, dct[:], block[:])
						}
					} else {
						for b.Loop() {
							rs = input
							hadamardQuantBenchmarkSink = WelsHadamardQuant2x2_c(rs[:], ff, mf, dct[:], block[:])
						}
					}
				})
			}
		}
	}
}
