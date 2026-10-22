package encoder

import (
	"fmt"
	"testing"
)

var intraDCBenchmarkSink int16

// Copy this file to the baseline with the supplied test-only dispatcher shim.
// Both routes include their public-length checks and inverse input reset cost.
func BenchmarkIntraDCTransforms(b *testing.B) {
	for _, scalar := range []bool{false, true} {
		b.Run(fmt.Sprintf("Forward/scalar=%t", scalar), func(b *testing.B) {
			var source [241]int16
			for i := range source {
				source[i] = int16(i*1701 - 32768)
			}
			var output [16]int16
			b.ReportAllocs()
			if scalar {
				for b.Loop() {
					WelsHadamardT4Dc_c(output[:], source[:])
				}
			} else {
				for b.Loop() {
					hadamardT4Dc(output[:], source[:])
				}
			}
			intraDCBenchmarkSink = output[0]
		})
		for _, mf := range []uint16{10, 104, 832, 65535} {
			b.Run(fmt.Sprintf("Inverse/mf%d/scalar=%t", mf, scalar), func(b *testing.B) {
				input := [16]int16{-32768, 32767, -1, 1, 16384, -16385, 8191, -8192, 32766, -32767, 3, -5, 7, -11, 13, -17}
				var output [16]int16
				b.ReportAllocs()
				if scalar {
					for b.Loop() {
						output = input
						WelsDequantIHadamard4x4_c(output[:], mf)
					}
				} else {
					for b.Loop() {
						output = input
						dequantIHadamard4x4(output[:], mf)
					}
				}
				intraDCBenchmarkSink = output[0]
			})
		}
	}
}
