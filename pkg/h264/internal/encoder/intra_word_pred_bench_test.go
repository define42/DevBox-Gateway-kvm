package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"testing"
)

var i4WordCostSink int32

func BenchmarkI4WordPredictors(b *testing.B) {
	var funcs SWelsFuncPtrList
	WelsInitIntraPredFuncs(&funcs, 0)
	WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2)
	for _, tc := range []struct {
		name string
		mode int
	}{{"DC", common.I4_PRED_DC}, {"H", common.I4_PRED_H}, {"V", common.I4_PRED_V}} {
		for _, withSAD := range []bool{false, true} {
			name := tc.name
			if withSAD {
				name += "AndSAD"
			}
			b.Run(name, func(b *testing.B) {
				ref, enc, dst := make([]byte, 16384), make([]byte, 16384), make([]byte, 16)
				for i := range ref {
					ref[i] = byte(i*17 + 7)
					enc[i] = byte(i*19 + 11)
				}
				fn := funcs.pfGetLumaI4x4Pred[tc.mode]
				sad := funcs.sSampleDealingFuncs.pfSampleSad[BLOCK_4x4]
				var cost int32
				b.ReportAllocs()
				for b.Loop() {
					fn(dst, 0, ref, 2049, 2048)
					if withSAD {
						cost = sad(dst, 0, 4, enc, 0, 2048)
					}
				}
				i4WordCostSink = cost
			})
		}
	}
}
