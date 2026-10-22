package encoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const skipDctQuantCallerStride = 64
const skipDctQuantCallerOrigin = 20*skipDctQuantCallerStride + 20

type skipDctQuantCallerState struct {
	ctx          sWelsEncCtx
	layer        SDqLayer
	functions    SWelsFuncPtrList
	alternate    SWelsFuncPtrList
	md           SWelsMD
	blocks       [4]SMB
	cache        SMbCache
	refPic       SPicture
	decPic       SPicture
	enc          [3][4096]byte
	ref          [3][4096]byte
	prediction   [416]byte
	coefficients [416]int16
	levels       SDCTCoeff
	alternateSad int32
}

// Production initialization keeps the fixture independent of SMB's field
// representation, and the timed function is the existing entry in both trees.
func newSkipDctQuantCallerState(scenario string, flags uint32, useSAD bool) *skipDctQuantCallerState {
	s := &skipDctQuantCallerState{}
	s.layer = SDqLayer{iMbWidth: 2, iMbHeight: 2, iEncStride: [3]int32{64, 64, 64},
		pRefPic: &s.refPic, pDecPic: &s.decPic, sMbDataP: s.blocks[:]}
	s.layer.sLayerInfo.pPpsP = &SWelsPPS{}
	s.layer.sSliceEncCtx.iMbNumInFrame = 4
	s.layer.sSliceEncCtx.pOverallMbMap = make([]uint16, 4)
	s.refPic.iLineSize = [3]int32{64, 64, 64}
	s.refPic.iPictureType = common.I_SLICE
	s.refPic.pMbSkipSad = make([]int32, 4)
	s.decPic.sMvList = make([]SMVUnitXY, 4)
	s.ctx = sWelsEncCtx{pCurDqLayer: &s.layer, pFuncList: &s.functions,
		pStrideTab: &SStrideTables{}, pMvUnitBlock4x4: make([]SMVUnitXY, 4*16),
		pRefIndexBlock4x4: make([]int8, 4*4), pSadCostMb: make([]int32, 4),
		pIntra4x4PredModeBlocks: make([]int8, 4*8), pNonZeroCountBlocks: make([]int8, 4*24)}
	s.ctx.pStrideTab.pMbIndexX[0] = []int16{0, 1, 0, 1}
	s.ctx.pStrideTab.pMbIndexY[0] = []int16{0, 0, 1, 1}
	s.ctx.pStrideTab.pStrideEncBlockOffset[0] = make([]int32, 24)
	InitMbInfo(&s.ctx, s.blocks[:], &s.layer, 0, 4)
	mb := &s.blocks[1]
	mb.uiLumaQp, mb.uiChromaQp, mb.uiCbp = 32, 32, 0x3f
	mb.sP16x16Mv = SMVUnitXY{17, -19}
	*mb.pSadCost = -37
	for i := range mb.sMv {
		mb.sMv[i] = SMVUnitXY{int16(i + 1), int16(-i - 1)}
	}
	for i := range mb.pRefIndex {
		mb.pRefIndex[i] = 2
	}
	s.md = SWelsMD{bMdUsingSad: useSAD, iCostLuma: -71, iCostChroma: -73,
		iCostSkipMb: -79, iSadPredSkip: 0}
	s.alternateSad = -101
	for plane := range 3 {
		for i := range s.enc[plane] {
			s.enc[plane][i], s.ref[plane][i] = 128, 128
		}
		s.cache.SPicData.pEncMb[plane] = s.enc[plane][:]
		s.cache.SPicData.pRefMb[plane] = s.ref[plane][:]
		s.cache.SPicData.iEncMbOff[plane] = skipDctQuantCallerOrigin
		s.cache.SPicData.iRefMbOff[plane] = skipDctQuantCallerOrigin
	}
	for i := range s.prediction {
		s.prediction[i] = 0xa5
	}
	for i := range s.coefficients {
		s.coefficients[i] = int16(i*71 + 13)
	}
	for i := range s.levels.iLumaBlock {
		s.levels.iLumaBlock[i] = int16(i*37 - 1701)
	}
	for i := range s.levels.iChromaBlock {
		s.levels.iChromaBlock[i] = int16(i*19 - 701)
	}
	s.cache.pSkipMb = s.prediction[16:400]
	s.cache.pCoeffLevel = s.coefficients[16:400]
	s.cache.pDct = &s.levels
	// Available zero-reference neighbors produce a zero skip predictor.
	for i := range s.cache.sMvComponents.iRefIndexCache {
		s.cache.sMvComponents.iRefIndexCache[i] = 0
	}
	if scenario != "Direct" {
		s.enc[0][skipDctQuantCallerOrigin] = 129 // one nonzero SAD, quantized away at QP 32
	}
	switch scenario {
	case "PredThreshold":
		s.md.iSadPredSkip = 2
	case "RefThreshold":
		s.refPic.iPictureType = common.P_SLICE
		s.refPic.pMbSkipSad[1] = 2
		s.cache.uiRefMbType = common.MB_TYPE_SKIP
	case "RejectLuma", "RejectU", "RejectV":
		plane, size := 0, 16
		if scenario == "RejectU" {
			plane, size = 1, 8
		} else if scenario == "RejectV" {
			plane, size = 2, 8
		}
		for y := range size {
			for x := range size {
				s.enc[plane][skipDctQuantCallerOrigin+y*skipDctQuantCallerStride+x] = 255
			}
		}
	}
	WelsInitEncodingFuncs(&s.functions, flags)
	WelsInitSampleSadFunc(&s.functions, flags)
	common.InitMcFunc(&s.functions.sMcFuncs, flags)
	s.functions.pfUpdateMbMv = UpdateMbMv_c
	return s
}

var skipDctQuantCallerBenchmarkSink bool

func BenchmarkSkipDctQuantCaller(b *testing.B) {
	flags := common.WelsCPUFeatureDetect(nil)
	for _, scenario := range []string{"Direct", "PredThreshold", "RefThreshold", "RejectLuma", "RejectU", "RejectV", "TransformSuccess"} {
		for _, useSAD := range []bool{true, false} {
			mode := "SAD"
			if !useSAD {
				mode = "SATD"
			}
			b.Run(scenario+"/"+mode, func(b *testing.B) {
				s := newSkipDctQuantCallerState(scenario, flags, useSAD)
				want := scenario != "RejectLuma" && scenario != "RejectU" && scenario != "RejectV"
				if got := WelsMdPSkipEnc(&s.ctx, &s.md, &s.blocks[1], &s.cache); got != want {
					b.Fatalf("fixture returned %v, want %v", got, want)
				}
				var got bool
				b.ReportAllocs()
				for b.Loop() {
					got = WelsMdPSkipEnc(&s.ctx, &s.md, &s.blocks[1], &s.cache)
				}
				skipDctQuantCallerBenchmarkSink = got
			})
		}
	}
}
