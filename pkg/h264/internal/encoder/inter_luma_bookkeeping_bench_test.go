package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var interLumaBookkeepingBenchmarkSink uint8

// The unchanged public entry and the same residual reset are timed in both
// trees. Inputs remain representative after every iteration's dequantization.
func BenchmarkInterLumaBookkeeping(b *testing.B) {
	for _, qp := range []uint8{20, 32} {
		for _, content := range []string{"Dense", "Motion", "Sparse", "Zero"} {
			b.Run(fmt.Sprintf("%s/QP%d", content, qp), func(b *testing.B) {
				var input, residual [384]int16
				for i := range input {
					value := int16((i*1701+13)%2049 - 1024)
					switch content {
					case "Motion":
						if i%7 >= 3 {
							value = 0
						}
					case "Sparse":
						if i%17 != 0 {
							value = 0
						}
						value /= 8
					case "Zero":
						value = 0
					}
					input[i] = value
				}
				var functions SWelsFuncPtrList
				flags := common.WelsCPUFeatureDetect(nil)
				WelsInitEncodingFuncs(&functions, flags)
				WelsInitReconstructionFuncs(&functions, flags)
				functions.pfSetMemZeroSize64 = WelsSetMemZero_c
				var dct SDCTCoeff
				var nonzero [24]int8
				mb := SMB{uiLumaQp: qp, pNonZeroCount: nonzero[:]}
				cache := SMbCache{pCoeffLevel: residual[:], pDct: &dct}
				b.ReportAllocs()
				for b.Loop() {
					residual = input
					mb.uiCbp = 0x30
					WelsEncInterY(&functions, &mb, &cache)
				}
				interLumaBookkeepingBenchmarkSink = mb.uiCbp
			})
		}
	}
}
