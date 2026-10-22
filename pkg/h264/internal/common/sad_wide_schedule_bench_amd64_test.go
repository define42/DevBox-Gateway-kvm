//go:build amd64

package common

import (
	"fmt"
	"testing"
)

func BenchmarkSadWideScheduling(b *testing.B) {
	for _, size := range []struct {
		name string
		fn   func([]byte, int, int32, []byte, int, int32) int32
	}{{"16x16", WelsSampleSad16x16_sse2}, {"16x8", WelsSampleSad16x8_sse2}} {
		for _, stride := range []int32{16, 32, 2048} {
			for _, offset := range []int{0, 3} {
				b.Run(fmt.Sprintf("%s/stride%d/off%d", size.name, stride, offset), func(b *testing.B) {
					a, z := make([]byte, 16*stride+64), make([]byte, 16*stride+64)
					for i := range a {
						a[i] = byte(i*17 + 9)
						z[i] = byte(i*19 + 11)
					}
					var sum int32
					b.ReportAllocs()
					for b.Loop() {
						sum = size.fn(a, offset, stride, z, offset+1, stride)
					}
					packedSadBenchmarkSink = sum
				})
			}
		}
	}
}
