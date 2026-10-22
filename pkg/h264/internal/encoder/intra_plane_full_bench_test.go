//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var planeFullPixelSink byte
var planeFullCostSink int32

// Uses the existing checked entries, so this file compiles unchanged on the
// baseline. Coefficient reduction and all reference validation are timed.
func BenchmarkIntraPlaneComplete(b *testing.B) {
	for _, size := range []int{8, 16} {
		for _, stride := range []int32{33, 1984} {
			for _, content := range []string{"Flat", "Smooth", "Noise"} {
				for _, withCost := range []bool{false, true} {
					b.Run(fmt.Sprintf("size%d/stride%d/%s/withSAD%t", size, stride, content, withCost), func(b *testing.B) {
						var functions SWelsFuncPtrList
						flags := common.WelsCPUFeatureDetect(nil)
						WelsInitIntraPredFuncs(&functions, flags)
						WelsInitSampleSadFunc(&functions, flags)
						predict := functions.pfGetLumaI16x16Pred[common.I16_PRED_P]
						cost := functions.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16]
						if size == 8 {
							predict = functions.pfGetChromaPred[common.C_PRED_P]
							cost = functions.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8]
						}
						ref := make([]byte, int(stride)*(size+2))
						encoded := make([]byte, 3+(size-1)*int(stride)+size)
						dst := make([]byte, 5+size*size)
						for i := range ref {
							ref[i] = 128
							if content == "Noise" {
								ref[i] = byte(i*137 + 11)
							}
							if content == "Smooth" {
								ref[i] = byte(96 + 2*(i%int(stride)-1) + 3*(i/int(stride)-1))
							}
						}
						for i := range encoded {
							encoded[i] = byte(i*71 + 17)
						}
						var result int32
						b.ReportAllocs()
						for b.Loop() {
							predict(dst, 5, ref, int(stride)+1, stride)
							if withCost {
								result = cost(dst, 5, int32(size), encoded, 3, stride)
							}
						}
						planeFullPixelSink, planeFullCostSink = dst[5], result
					})
				}
			}
		}
	}
}

// Complete luma-plus-chroma mode decisions exercise the new plane callbacks
// alongside unchanged DC/H/V predictors and sample-cost dispatch.
func BenchmarkIntraPlaneModeChain(b *testing.B) {
	for _, width := range []int32{64, 1920} {
		for _, content := range []string{"Flat", "Smooth", "Noise"} {
			for _, sad := range []bool{false, true} {
				b.Run(fmt.Sprintf("width%d/%s/SAD%t", width, content, sad), func(b *testing.B) {
					var functions SWelsFuncPtrList
					flags := common.WelsCPUFeatureDetect(nil)
					WelsInitIntraPredFuncs(&functions, flags)
					WelsInitSampleSadFunc(&functions, flags)
					if sad {
						SetFastCodingFunc(&functions)
					} else {
						SetNormalCodingFunc(&functions)
					}
					layer := SDqLayer{iEncStride: [3]int32{width, width / 2, width / 2}, iCsStride: [3]int32{width + 64, width/2 + 32, width/2 + 32}}
					cache := SMbCache{uiNeighborIntra: 7, pMemPredMb: make([]byte, 512)}
					for plane := 0; plane < 3; plane++ {
						size := 8
						if plane == 0 {
							size = 16
						}
						rs, es := int(layer.iCsStride[plane]), int(layer.iEncStride[plane])
						ref, encoded := make([]byte, rs*(size+2)), make([]byte, 3+(size-1)*es+size)
						for i := range ref {
							ref[i] = 128
							if content == "Noise" {
								ref[i] = byte(i*137 + plane*29 + 11)
							}
							if content == "Smooth" {
								ref[i] = byte(96 + 2*(i%rs-1) + 3*(i/rs-1))
							}
						}
						for y := 0; y < size; y++ {
							for x := 0; x < size; x++ {
								value := byte(128)
								if content == "Noise" {
									value = byte(x*71 + y*137 + plane*31 + 17)
								}
								if content == "Smooth" {
									value = byte(96 + 2*x + 3*y)
								}
								encoded[3+y*es+x] = value
							}
						}
						cache.SPicData.pCsMb[plane], cache.SPicData.pEncMb[plane] = ref, encoded
						cache.SPicData.iCsMbOff[plane], cache.SPicData.iEncMbOff[plane] = rs+1, 3
					}
					var luma, chroma int32
					b.ReportAllocs()
					for b.Loop() {
						luma = WelsMdI16x16(&functions, &layer, &cache, 26)
						chroma = WelsMdIntraChroma(&functions, &layer, &cache, 26)
					}
					planeFullCostSink = luma + chroma
				})
			}
		}
	}
}
