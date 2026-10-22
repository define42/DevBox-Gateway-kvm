//go:build amd64

package common

import "testing"

var packedSadBenchmarkSink int32

// Existing checked entry points keep the harness identical on both trees.
func BenchmarkSampleSadPackedRows(b *testing.B) {
	for _, test := range []struct {
		name string
		fn   func([]byte, int, int32, []byte, int, int32) int32
	}{
		{"8x4", WelsSampleSad8x4_sse2},
		{"8x8", WelsSampleSad8x8_sse2},
		{"8x16", WelsSampleSad8x16_sse2},
		{"4x4", WelsSampleSad4x4_sse2},
		{"4x8", WelsSampleSad4x8_sse2},
		{"16x8Control", WelsSampleSad16x8_sse2},
		{"16x16Control", WelsSampleSad16x16_sse2},
	} {
		b.Run(test.name, func(b *testing.B) {
			var src, ref [32*16 + 7]byte
			for i := range src {
				src[i], ref[i] = byte(i*17+5), byte(i*29+3)
			}
			var sum int32
			b.ReportAllocs()
			for b.Loop() {
				sum = test.fn(src[:], 3, 31, ref[:], 7, 32)
			}
			packedSadBenchmarkSink = sum
		})
	}
}
