package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var intraReconstructionBenchmarkSink uint8

// One operation reconstructs all sixteen I4 blocks with real function-table
// calls, quantization, scan/count bookkeeping, and either IDCT or prediction
// copying. Prediction and source remain stable across repeated operations.
func BenchmarkI4Reconstruction(b *testing.B) {
	for _, strides := range [][2]int32{{64, 64}, {1920, 1984}} {
		for _, qp := range []uint8{20, 32} {
			for _, content := range []string{"Zero", "Sparse", "Dense"} {
				b.Run(fmt.Sprintf("%s/QP%d/Stride%d_%d", content, qp, strides[0], strides[1]), func(b *testing.B) {
					f := newIntraReconstructionFixture(strides[0], strides[1], qp, content, common.WelsCPUFeatureDetect(nil))
					b.ReportAllocs()
					for b.Loop() {
						f.mb.uiCbp = 0x30
						for block := uint8(0); block < 16; block++ {
							WelsEncRecI4x4Y(&f.ctx, &f.mb, &f.cache, block)
						}
					}
					b.ReportMetric(16, "blocks/op")
					intraReconstructionBenchmarkSink = f.mb.uiCbp
				})
			}
		}
	}
}

type intraReconstructionFixture struct {
	functions              SWelsFuncPtrList
	ctx                    sWelsEncCtx
	layer                  SDqLayer
	strideTables           SStrideTables
	encOffsets, recOffsets [24]int32
	mb                     SMB
	cache                  SMbCache
	coefficients           [384]int16
	nonzero                [24]int8
	dct                    SDCTCoeff
	prediction             [16]byte
	source, reconstructed  []byte
}

func newIntraReconstructionFixture(encStride, recStride int32, qp uint8, content string, flags uint32) *intraReconstructionFixture {
	f := new(intraReconstructionFixture)
	WelsInitEncodingFuncs(&f.functions, flags)
	WelsInitReconstructionFuncs(&f.functions, flags)
	f.layer.iEncStride[0], f.layer.iCsStride[0] = encStride, recStride
	WelsGetEncBlockStrideOffset(f.encOffsets[:], encStride, encStride/2)
	WelsGetEncBlockStrideOffset(f.recOffsets[:], recStride, recStride/2)
	f.strideTables.pStrideEncBlockOffset[0] = f.encOffsets[:]
	f.strideTables.pStrideDecBlockOffset[0][0] = f.recOffsets[:]
	f.strideTables.pStrideDecBlockOffset[0][1] = f.recOffsets[:]
	f.ctx.pFuncList, f.ctx.pCurDqLayer, f.ctx.pStrideTab = &f.functions, &f.layer, &f.strideTables
	f.mb.uiMbType, f.mb.uiLumaQp, f.mb.uiCbp = common.MB_TYPE_INTRA4x4, qp, 0x30
	f.mb.pNonZeroCount = f.nonzero[:]
	f.cache.pCoeffLevel, f.cache.pDct, f.cache.pBestPredI4x4Blk4 = f.coefficients[:], &f.dct, f.prediction[:]
	f.source = make([]byte, 3+15*int(encStride)+16+7)
	f.reconstructed = make([]byte, 5+15*int(recStride)+16+7)
	f.cache.SPicData.pEncMb[0], f.cache.SPicData.pCsMb[0] = f.source, f.reconstructed
	f.cache.SPicData.iEncMbOff[0], f.cache.SPicData.iCsMbOff[0] = 3, 5
	for i := range f.prediction {
		f.prediction[i] = 128
	}
	for i := range f.source {
		f.source[i] = 128
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			value := byte(128)
			switch content {
			case "Sparse":
				if x%4 == 0 && y%4 == 0 && (x/4+y/4)%3 == 0 {
					value = 160
				}
			case "Dense":
				value = byte(x*19 + y*37 + 11)
			}
			f.source[3+y*int(encStride)+x] = value
		}
	}
	for i := range f.reconstructed {
		f.reconstructed[i] = 0xa5
	}
	for i := range f.coefficients {
		f.coefficients[i] = -1234
	}
	for i := range f.nonzero {
		f.nonzero[i] = 13
	}
	return f
}
