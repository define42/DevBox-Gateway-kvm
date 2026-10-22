//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// This benchmark only uses the pre-existing refinement API so its source can
// also measure the frozen implementation before validation was batched.
func BenchmarkQuarterRefineShapes(b *testing.B) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		b.Skip("AVX2 required")
	}
	for _, size := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}} {
		for _, wins := range []bool{false, true} {
			b.Run(fmt.Sprintf("%dx%d/wins=%v", size[0], size[1], wins), func(b *testing.B) {
				var funcs SWelsFuncPtrList
				WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2|common.WELS_CPU_SSSE3|common.WELS_CPU_SSE41|common.WELS_CPU_AVX2)
				common.InitMcFunc(&funcs.sMcFuncs, common.WELS_CPU_SSE2)
				funcs.sSampleDealingFuncs.pfMeCost = &funcs.sSampleDealingFuncs.pfSampleSatd
				enc, scratch := make([]byte, 1024), make([]byte, 2048)
				for i := range enc {
					enc[i] = byte(i*17 + 3)
				}
				me := SWelsME{pEncMb: enc, iEncMbOff: 3, uiBlockSize: uint8(size[2])}
				params := SQuarRefineParams{iStrideA: 29, iStrideB: 31}
				for i := range 4 {
					params.pSrcA[i], params.pSrcB[i] = make([]byte, 1024), make([]byte, 1024)
					for j := range params.pSrcA[i] {
						params.pSrcA[i][j], params.pSrcB[i][j] = byte(j*13+i), byte(j*31+i)
					}
				}
				refine := SMeRefinePointer{pQuarPixBest: scratch, pQuarPixTmp: scratch[640:]}
				initialCost := int32(0)
				if wins {
					initialCost = 1 << 30
				}
				b.ReportAllocs()
				for b.Loop() {
					params.iBestCost, params.iBestQuarPix = initialCost, ME_NO_BEST_QUAR_PIXEL
					MeRefineQuarPixel(&funcs, &me, &refine, size[0], size[1], &params, 32)
				}
				sampleSatdBenchmarkSink = params.iBestCost
			})
		}
	}
}
