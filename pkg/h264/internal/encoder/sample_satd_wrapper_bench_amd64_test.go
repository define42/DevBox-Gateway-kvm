//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// BenchmarkSampleSatdWrapperDispatch measures the actual encoder callbacks.
// The unchanged generic helper and 4x4 dispatch are code-layout/noise controls.
// This file can be copied unchanged to the baseline tree.
func BenchmarkSampleSatdWrapperDispatch(b *testing.B) {
	flags := common.WelsCPUFeatureDetect(nil)
	required := uint32(common.WELS_CPU_AVX2 | common.WELS_CPU_SSSE3 | common.WELS_CPU_SSE41)
	if flags&required != required {
		b.Skip("AVX2, SSSE3 and SSE4.1 required")
	}
	var funcs SWelsFuncPtrList
	WelsInitSampleSadFunc(&funcs, flags)
	for _, shape := range []struct {
		width, height, block int
	}{
		{8, 8, BLOCK_8x8}, {16, 8, BLOCK_16x8},
		{8, 16, BLOCK_8x16}, {16, 16, BLOCK_16x16},
		{4, 4, BLOCK_4x4},
	} {
		for _, magnitude := range []int{32, 1984} {
			for _, negative := range []bool{false, true} {
				if shape.width == 4 && negative {
					continue // The unchanged SSE4.1 4x4 entry rejects negative strides.
				}
				stride, offset := int32(magnitude), 7
				direction := "positive"
				if negative {
					stride = -stride
					offset += (shape.height - 1) * magnitude
					direction = "negative"
				}
				for _, generic := range []bool{false, true} {
					if shape.width == 4 && generic {
						continue
					}
					kind := "dispatch"
					fn := funcs.sSampleDealingFuncs.pfSampleSatd[shape.block]
					if shape.width == 4 {
						kind = "dispatchControl"
					} else if generic {
						kind = "genericControl"
						fn = func(a []byte, aOff int, aStride int32, ref []byte, refOff int, refStride int32) int32 {
							return sampleSatdBlockAVX2(a, aOff, aStride, ref, refOff, refStride, shape.width, shape.height)
						}
					}
					b.Run(fmt.Sprintf("%dx%d/stride%d/%s/%s", shape.width, shape.height, magnitude, direction, kind), func(b *testing.B) {
						span := 7 + (shape.height-1)*magnitude + shape.width
						a, ref := make([]byte, span), make([]byte, span)
						for i := range a {
							a[i], ref[i] = byte(i*17+5), byte(i*29+3)
						}
						var cost int32
						b.ReportAllocs()
						for b.Loop() {
							cost = fn(a, offset, stride, ref, offset, stride)
						}
						sampleSatdBenchmarkSink = cost
					})
				}
			}
		}
	}
}
