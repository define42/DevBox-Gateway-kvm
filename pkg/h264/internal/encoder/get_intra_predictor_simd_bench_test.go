//go:build amd64

package encoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func BenchmarkIntraModeDecision(b *testing.B) {
	for _, variant := range []string{"ScalarPredictors", "SIMDPredictors", "SIMDFusedSAD"} {
		b.Run(variant, func(b *testing.B) {
			var funcs SWelsFuncPtrList
			WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2)
			if variant == "ScalarPredictors" {
				WelsInitIntraPredFuncs(&funcs, 0)
			} else {
				WelsInitIntraPredFuncs(&funcs, common.WELS_CPU_SSE2)
			}
			if variant != "SIMDFusedSAD" {
				funcs.sSampleDealingFuncs.pfIntra16x16Combined3Sad = nil
				funcs.sSampleDealingFuncs.pfIntra8x8Combined3Sad = nil
			}
			SetFastCodingFunc(&funcs)
			rng := rand.New(rand.NewSource(904))
			cache := SMbCache{uiNeighborIntra: 7, pMemPredMb: make([]byte, 512)}
			for plane := range cache.SPicData.pCsMb {
				cache.SPicData.pCsMb[plane] = make([]byte, 1024)
				cache.SPicData.pEncMb[plane] = make([]byte, 1024)
				_, _ = rng.Read(cache.SPicData.pCsMb[plane])
				_, _ = rng.Read(cache.SPicData.pEncMb[plane])
				cache.SPicData.iCsMbOff[plane] = 33
			}
			layer := SDqLayer{iCsStride: [3]int32{32, 32, 32}, iEncStride: [3]int32{32, 32, 32}}
			b.ReportAllocs()
			for b.Loop() {
				WelsMdI16x16(&funcs, &layer, &cache, 26)
				WelsMdIntraChroma(&funcs, &layer, &cache, 26)
			}
		})
	}
}
