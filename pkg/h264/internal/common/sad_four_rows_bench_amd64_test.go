//go:build amd64

package common

import (
	"fmt"
	"testing"
)

var sadFourRowsBenchmarkSink [4]int32

// The fixture and public checked entries also exist in the frozen baseline.
func BenchmarkSampleSadFourRows(b *testing.B) {
	for _, tc := range sadFourSIMDCases {
		for _, stride := range []int{32, 2048} {
			b.Run(fmt.Sprintf("%s/stride%d", tc.name, stride), func(b *testing.B) {
				src, ref := make([]byte, stride*(tc.height+2)), make([]byte, stride*(tc.height+3))
				for i := range src {
					src[i] = byte(i*29 + 17)
				}
				for i := range ref {
					ref[i] = byte(i*31 + 11)
				}
				var costs [4]int32
				b.ReportAllocs()
				for b.Loop() {
					tc.batched(src, 3, int32(stride), ref, stride+5, int32(stride), costs[:])
				}
				sadFourRowsBenchmarkSink = costs
			})
		}
	}
}
