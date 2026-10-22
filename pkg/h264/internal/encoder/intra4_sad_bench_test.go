package encoder

import (
	"fmt"
	"math"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var intra4SadBenchmarkSink int32

// Baseline-compatible: copy this file unchanged to frozen round9. One operation
// executes the complete existing WelsMdI4x4Fast entry, including predictor
// dispatch, directional modes, early exit, transforms and reconstruction.
// The common reset restores only the 16x16 reconstructed region and mode cache.
func BenchmarkIntra4SadCaller(b *testing.B) {
	for _, strides := range [][2]int32{{32, 32}, {1920, 1984}} {
		for _, tc := range []struct {
			name      string
			content   string
			qp        uint8
			neighbors uint8
			limit     int32
			control   string
		}{
			{"FlatFull", "Flat", 20, 15, math.MaxInt32, ""},
			{"SmoothFull", "Smooth", 20, 15, math.MaxInt32, ""},
			{"NoiseFull", "Noise", 20, 15, math.MaxInt32, ""},
			{"NoiseQP32Full", "Noise", 32, 15, math.MaxInt32, ""},
			{"LeftTopFull", "Smooth", 32, 7, math.MaxInt32, ""},
			{"TopOnlyFull", "Smooth", 20, 2, math.MaxInt32, ""},
			{"IsolatedFull", "Flat", 32, 0, math.MaxInt32, ""},
			{"FirstBlockExit", "Noise", 20, 15, 0, ""},
			{"IsolatedFirstExit", "Noise", 32, 0, 0, ""},
			{"WrappedCostControl", "Noise", 20, 15, math.MaxInt32, "wrapped"},
			{"SATDControl", "Noise", 20, 15, math.MaxInt32, "satd"},
		} {
			b.Run(fmt.Sprintf("%s/stride%d_%d", tc.name, strides[0], strides[1]), func(b *testing.B) {
				f := newIntra4SadCallerFixture(strides[0], strides[1], tc.qp, tc.content, tc.neighbors, 2)
				if tc.control == "wrapped" {
					cost := f.functions.sSampleDealingFuncs.pfSampleSad[BLOCK_4x4]
					f.functions.sSampleDealingFuncs.pfSampleSad[BLOCK_4x4] = func(a []byte, ao int, as int32, c []byte, co int, cs int32) int32 {
						return cost(a, ao, as, c, co, cs)
					}
				} else if tc.control == "satd" {
					f.functions.sSampleDealingFuncs.pfMdCost = &f.functions.sSampleDealingFuncs.pfSampleSatd
				}
				var result int32
				b.ReportAllocs()
				for b.Loop() {
					f.reset(tc.limit)
					result = WelsMdI4x4Fast(&f.ctx, &f.md, &f.mb, &f.cache)
				}
				intra4SadBenchmarkSink = result
			})
		}
	}
}

type intra4SadCallerFixture struct {
	functions              SWelsFuncPtrList
	ctx                    sWelsEncCtx
	layer                  SDqLayer
	strides                SStrideTables
	encOffsets, recOffsets [24]int32
	md                     SWelsMD
	mb                     SMB
	cache                  SMbCache
	coefficients           [384]int16
	dct                    SDCTCoeff
	nonzero                [24]int8
	modes                  [8]int8
	previous               [16]bool
	remaining              [16]int8
	scratch                [32]byte
	source, reconstructed  []byte
	initialPixels          [256]byte
	initialModes           [48]int8
}

func newIntra4SadCallerFixture(encStride, recStride int32, qp uint8, content string, neighbors uint8, predicted int8) *intra4SadCallerFixture {
	f := new(intra4SadCallerFixture)
	flags := common.WelsCPUFeatureDetect(nil)
	WelsInitEncodingFuncs(&f.functions, flags)
	WelsInitReconstructionFuncs(&f.functions, flags)
	WelsInitIntraPredFuncs(&f.functions, flags)
	WelsInitSampleSadFunc(&f.functions, flags)
	SetFastCodingFunc(&f.functions)
	f.layer.iEncStride[0], f.layer.iCsStride[0] = encStride, recStride
	WelsGetEncBlockStrideOffset(f.encOffsets[:], encStride, encStride/2)
	WelsGetEncBlockStrideOffset(f.recOffsets[:], recStride, recStride/2)
	f.strides.pStrideEncBlockOffset[0] = f.encOffsets[:]
	f.strides.pStrideDecBlockOffset[0][0], f.strides.pStrideDecBlockOffset[0][1] = f.recOffsets[:], f.recOffsets[:]
	f.ctx.pFuncList, f.ctx.pCurDqLayer, f.ctx.pStrideTab = &f.functions, &f.layer, &f.strides
	f.mb.uiMbType, f.mb.uiLumaQp = common.MB_TYPE_INTRA4x4, qp
	f.mb.pNonZeroCount, f.mb.pIntra4x4PredMode = f.nonzero[:], f.modes[:]
	f.cache.pCoeffLevel, f.cache.pDct, f.cache.pMemPredBlk4 = f.coefficients[:], &f.dct, f.scratch[:]
	f.cache.pPrevIntra4x4PredModeFlag, f.cache.pRemIntra4x4PredModeFlag = f.previous[:], f.remaining[:]
	f.cache.uiNeighborIntra = neighbors
	f.md.iLambda = g_kiQpCostTable[qp]
	f.source = make([]byte, 3+15*int(encStride)+16+7)
	f.reconstructed = make([]byte, 18*int(recStride)+24)
	f.cache.SPicData.pEncMb[0], f.cache.SPicData.pCsMb[0] = f.source, f.reconstructed
	f.cache.SPicData.iEncMbOff[0], f.cache.SPicData.iCsMbOff[0] = 3, int(recStride)+4
	for i := range f.source {
		f.source[i] = 0xa7
	}
	for i := range f.reconstructed {
		f.reconstructed[i] = 128
	}
	pixel := func(x, y int) byte {
		switch content {
		case "Smooth":
			return byte(96 + 2*x + 3*y)
		case "Horizontal":
			return byte(128 + y*5)
		case "Vertical":
			return byte(128 + x*7)
		case "Noise":
			return byte((x*71 + y*137 + 17) ^ (x * y * 11))
		case "Extreme":
			if (x+y)&1 == 0 {
				return 0
			}
			return 255
		default:
			return 128
		}
	}
	es, rs, ro := int(encStride), int(recStride), f.cache.SPicData.iCsMbOff[0]
	for x := -1; x < 24; x++ {
		f.reconstructed[ro-rs+x] = pixel(x, -1)
	}
	for y := 0; y < 16; y++ {
		f.reconstructed[ro+y*rs-1] = pixel(-1, y)
		for x := 0; x < 16; x++ {
			f.source[3+y*es+x] = pixel(x, y)
			f.initialPixels[y*16+x] = byte(x*13 + y*19 + 7)
		}
	}
	for i := range f.initialModes {
		f.initialModes[i] = predicted
	}
	for i := range f.coefficients {
		f.coefficients[i] = -1234
	}
	for i := range f.nonzero {
		f.nonzero[i] = 13
	}
	for i := range f.modes {
		f.modes[i] = -3
	}
	for i := range f.remaining {
		f.remaining[i] = -4
	}
	for i := range f.scratch {
		f.scratch[i] = byte(i*3 + 9)
	}
	f.reset(math.MaxInt32)
	return f
}

func (f *intra4SadCallerFixture) reset(limit int32) {
	f.md.iCostLuma, f.mb.uiCbp = limit, 0x30
	f.cache.iIntraPredMode = f.initialModes
	ro, stride := f.cache.SPicData.iCsMbOff[0], int(f.layer.iCsStride[0])
	for y := 0; y < 16; y++ {
		copy(f.reconstructed[ro+y*stride:ro+y*stride+16], f.initialPixels[y*16:y*16+16])
	}
}
