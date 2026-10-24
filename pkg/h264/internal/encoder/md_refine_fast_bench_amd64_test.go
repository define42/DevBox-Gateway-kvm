//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Uses only baseline APIs and flags. Measure together with the existing full
// BenchmarkMeRefineFracPixelCaller matrix, including its narrow/no-motion cases.
func BenchmarkWholeFractionalControls(b *testing.B) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		b.Skip("AVX2 required")
	}
	for _, block := range []uint8{BLOCK_16x16, BLOCK_8x8} {
		for _, scenario := range []string{"pairOff", "quarterOff", "scalar", "customFilter"} {
			b.Run(fmt.Sprintf("block%d/%s", block, scenario), func(b *testing.B) {
				w := int32(16)
				if block == BLOCK_8x8 {
					w = 8
				}
				f := newFractionalCallerFixture(w, w, block, 32, true)
				switch scenario {
				case "pairOff":
					f.funcs.sSampleDealingFuncs.bSampleHalfSatdPair = false
				case "quarterOff":
					f.funcs.sSampleDealingFuncs.bSampleAvgSatdFour = false
				case "scalar":
					WelsInitSampleSadFunc(&f.funcs, 0)
					f.funcs.sSampleDealingFuncs.pfMeCost = &f.funcs.sSampleDealingFuncs.pfSampleSatd
					common.InitMcFunc(&f.funcs.sMcFuncs, 0)
				case "customFilter":
					original := f.funcs.sMcFuncs.PfLumaHalfpelHor
					f.funcs.sMcFuncs.PfLumaHalfpelHor = func(a []byte, ao int, as int32, b []byte, bo int, bs, w, h int32) {
						original(a, ao, as, b, bo, bs, w, h)
					}
				}
				b.ReportAllocs()
				for b.Loop() {
					f.run()
				}
				sampleSatdBenchmarkSink = int32(f.me.uiSatdCost)
			})
		}
	}
}
