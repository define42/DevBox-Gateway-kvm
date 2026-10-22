package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var hadamardChromaBenchmarkSink uint8

// Baseline-compatible production entry: DC quantization, AC quantize/scan,
// coefficient decisions, dequantization, and reconstructed DC for both planes.
func BenchmarkChromaCoefficientEncoding(b *testing.B) {
	for _, qp := range []uint8{20, 32} {
		for _, intra := range []bool{false, true} {
			for _, content := range []string{"Dense", "Motion", "Sparse", "Zero"} {
				b.Run(fmt.Sprintf("QP%d/intra=%t/%s", qp, intra, content), func(b *testing.B) {
					var input, residual [128]int16
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
					mb := SMB{uiMbType: common.MB_TYPE_16x16, uiChromaQp: qp, pNonZeroCount: nonzero[:]}
					if intra {
						mb.uiMbType = common.MB_TYPE_INTRA16x16
					}
					cache := SMbCache{pCoeffLevel: residual[:], pDct: &dct}
					b.ReportAllocs()
					for b.Loop() {
						residual = input
						mb.uiCbp = 0
						WelsEncRecUV(&functions, &mb, &cache, residual[:64], 1)
						WelsEncRecUV(&functions, &mb, &cache, residual[64:], 2)
					}
					hadamardChromaBenchmarkSink = mb.uiCbp
					b.ReportMetric(2, "planes/op")
				})
			}
		}
	}
}
