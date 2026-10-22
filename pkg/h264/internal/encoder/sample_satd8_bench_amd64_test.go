//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// BenchmarkSampleSatd8Rows separates checked entry points from the private
// kernel. The 16-wide cases are controls for the shared four-row path.
func BenchmarkSampleSatd8Rows(b *testing.B) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		b.Skip("AVX2 required")
	}
	for _, size := range [][2]int{{8, 8}, {8, 16}, {16, 8}, {16, 16}} {
		for _, raw := range []bool{false, true} {
			kind := "checked"
			if raw {
				kind = "raw"
			}
			b.Run(fmt.Sprintf("plain/%dx%d/%s", size[0], size[1], kind), func(b *testing.B) {
				var src, ref [32*16 + 7]byte
				for i := range src {
					src[i], ref[i] = byte(i*17+5), byte(i*29+3)
				}
				var cost int
				b.ReportAllocs()
				if raw {
					for b.Loop() {
						cost = satdBlockAVX2(&src[3], 32, &ref[7], 32, size[0], size[1])
					}
				} else {
					for b.Loop() {
						cost = int(sampleSatdBlockAVX2(src[:], 3, 32, ref[:], 7, 32, size[0], size[1]))
					}
				}
				sampleSatdBenchmarkSink = int32(cost)
			})
		}
	}
	for _, size := range [][2]int32{{8, 4}, {8, 8}, {8, 12}, {8, 16}, {16, 8}, {16, 16}} {
		b.Run(fmt.Sprintf("average/%dx%d/checked", size[0], size[1]), func(b *testing.B) {
			var src, a, c [32*16 + 7]byte
			for i := range src {
				src[i], a[i], c[i] = byte(i*17+5), byte(i*29+3), byte(i*37+1)
			}
			var cost int32
			b.ReportAllocs()
			for b.Loop() {
				cost = sampleAvgSatdAVX2(src[:], 3, 31, a[:], 5, 32, c[:], 7, 29, size[0], size[1])
			}
			sampleSatdBenchmarkSink = cost
		})
	}
}
