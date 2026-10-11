// Port of codec/encoder/core/src/ratectl.cpp.
//
// Rate control of the encoder. The integer and floating point expressions
// follow the C code exactly (including the C arithmetic conversions and the
// float/double evaluation order), since the RC decisions determine the
// bitstream.

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"
)

var g_kiQpToQstepTable = [52]int32{
	63, 71, 79, 89, 100, 112, 126, 141, 159, 178,
	200, 224, 252, 283, 317, 356, 400, 449, 504, 566,
	635, 713, 800, 898, 1008, 1131, 1270, 1425, 1600, 1796,
	2016, 2263, 2540, 2851, 3200, 3592, 4032, 4525, 5080, 5702,
	6400, 7184, 8063, 9051, 10159, 11404, 12800, 14368, 16127, 18102,
	20319, 22807,
} //WELS_ROUND(INT_MULTIPLY*pow (2.0, (iQP - 4.0) / 6.0))

// rcDivRoundF32 is WELS_DIV_ROUND (x, y) with an int x and a float y: the C
// expression is evaluated in float.
func rcDivRoundF32(x int32, y float32) int32 {
	if y == 0 {
		return int32(float32(x) / (y + 1))
	}
	return int32((y/2 + float32(x)) / y)
}

// rcLogf is the C++ float overload std::log (float).
func rcLogf(x float32) float32 {
	return float32(math.Log(float64(x)))
}

// CMemoryAlign* pMA dropped.
func RcInitLayerMemory(pWelsSvcRc *SWelsSvcRc, kiMaxTl int32) {
	kiGomSize := int(pWelsSvcRc.iGomSize)
	if kiGomSize < 0 || kiMaxTl < 0 {
		return
	}
	pWelsSvcRc.pTemporalOverRc = make([]SRCTemporal, kiMaxTl)
	pWelsSvcRc.pGomComplexity = make([]float64, kiGomSize)
	pWelsSvcRc.pGomForegroundBlockNum = make([]int32, kiGomSize)
	pWelsSvcRc.pCurrentFrameGomSad = make([]int32, kiGomSize)
	pWelsSvcRc.pGomCost = make([]int32, kiGomSize)
}

func RcFreeLayerMemory(pWelsSvcRc *SWelsSvcRc) {
	if pWelsSvcRc != nil && pWelsSvcRc.pTemporalOverRc != nil {
		pWelsSvcRc.pTemporalOverRc = nil
		pWelsSvcRc.pGomComplexity = nil
		pWelsSvcRc.pGomForegroundBlockNum = nil
		pWelsSvcRc.pCurrentFrameGomSad = nil
		pWelsSvcRc.pGomCost = nil
	}
}

func RcConvertQp2QStep(iQP int32) int32 {
	return g_kiQpToQstepTable[iQP]
}

func RcConvertQStep2Qp(iQpStep int32) int32 {
	if iQpStep <= g_kiQpToQstepTable[0] { //Qp step too small, return qp=0
		return 0
	}
	// 6 * log (float) is float, / log (2.0) is double.
	fLog := float32(6) * rcLogf(float32(iQpStep)*1.0/INT_MULTIPLY)
	return common.WELS_ROUND(float64(fLog)/math.Log(2.0) + 4.0)
}

func RcInitSequenceParameter(pEncCtx *sWelsEncCtx) {
	var pWelsSvcRc *SWelsSvcRc
	var pDLayerParam *api.SSpatialLayerConfig

	var j int32
	var iMbWidth int32

	bMultiSliceMode := false
	iGomRowMode0, iGomRowMode1 := int32(1), int32(1)
	for j = 0; j < pEncCtx.pSvcParam.ISpatialLayerNum; j++ {
		pWelsSvcRc = &pEncCtx.pWelsSvcRc[j]
		pDLayerParam = &pEncCtx.pSvcParam.SSpatialLayers[j]
		iMbWidth = (pDLayerParam.IVideoWidth >> 4)
		pWelsSvcRc.iNumberMbFrame = iMbWidth * (pDLayerParam.IVideoHeight >> 4)

		pWelsSvcRc.iRcVaryPercentage = pEncCtx.pSvcParam.iBitsVaryPercentage // % -- for temp
		pWelsSvcRc.iRcVaryRatio = pWelsSvcRc.iRcVaryPercentage

		pWelsSvcRc.iBufferFullnessSkip = 0
		pWelsSvcRc.uiLastTimeStamp = 0
		pWelsSvcRc.iCost2BitsIntra = 1
		pWelsSvcRc.iAvgCost2Bits = 1
		pWelsSvcRc.iSkipBufferRatio = SKIP_RATIO
		pWelsSvcRc.iContinualSkipFrames = 0
		pWelsSvcRc.iQpRangeUpperInFrame = (QP_RANGE_UPPER_MODE1*MAX_BITS_VARY_PERCENTAGE - ((QP_RANGE_UPPER_MODE1 - QP_RANGE_MODE0) *
			pWelsSvcRc.iRcVaryRatio)) / MAX_BITS_VARY_PERCENTAGE
		pWelsSvcRc.iQpRangeLowerInFrame = (QP_RANGE_LOWER_MODE1*MAX_BITS_VARY_PERCENTAGE - ((QP_RANGE_LOWER_MODE1 - QP_RANGE_MODE0) *
			pWelsSvcRc.iRcVaryRatio)) / MAX_BITS_VARY_PERCENTAGE

		if iMbWidth <= MB_WIDTH_THRESHOLD_90P {
			pWelsSvcRc.iSkipQpValue = SKIP_QP_90P
			iGomRowMode0 = GOM_ROW_MODE0_90P
			iGomRowMode1 = GOM_ROW_MODE1_90P
		} else if iMbWidth <= MB_WIDTH_THRESHOLD_180P {
			pWelsSvcRc.iSkipQpValue = SKIP_QP_180P
			iGomRowMode0 = GOM_ROW_MODE0_180P
			iGomRowMode1 = GOM_ROW_MODE1_180P
		} else if iMbWidth <= MB_WIDTH_THRESHOLD_360P {
			pWelsSvcRc.iSkipQpValue = SKIP_QP_360P
			iGomRowMode0 = GOM_ROW_MODE0_360P
			iGomRowMode1 = GOM_ROW_MODE1_360P
		} else {
			pWelsSvcRc.iSkipQpValue = SKIP_QP_720P
			iGomRowMode0 = GOM_ROW_MODE0_720P
			iGomRowMode1 = GOM_ROW_MODE1_720P
		}
		iGomRowMode0 = iGomRowMode1 + ((iGomRowMode0 - iGomRowMode1) * pWelsSvcRc.iRcVaryRatio / MAX_BITS_VARY_PERCENTAGE)

		pWelsSvcRc.iNumberMbGom = iMbWidth * iGomRowMode0

		pWelsSvcRc.iMinQp = pEncCtx.pSvcParam.IMinQp

		pWelsSvcRc.iMaxQp = pEncCtx.pSvcParam.IMaxQp

		pWelsSvcRc.iFrameDeltaQpUpper = LAST_FRAME_QP_RANGE_UPPER_MODE1 - ((LAST_FRAME_QP_RANGE_UPPER_MODE1 -
			LAST_FRAME_QP_RANGE_UPPER_MODE0) * pWelsSvcRc.iRcVaryRatio / MAX_BITS_VARY_PERCENTAGE)
		pWelsSvcRc.iFrameDeltaQpLower = LAST_FRAME_QP_RANGE_LOWER_MODE1 - ((LAST_FRAME_QP_RANGE_LOWER_MODE1 -
			LAST_FRAME_QP_RANGE_LOWER_MODE0) * pWelsSvcRc.iRcVaryRatio / MAX_BITS_VARY_PERCENTAGE)

		pWelsSvcRc.iSkipFrameNum = 0
		pWelsSvcRc.iGomSize = (pWelsSvcRc.iNumberMbFrame + pWelsSvcRc.iNumberMbGom - 1) / pWelsSvcRc.iNumberMbGom
		pWelsSvcRc.bEnableGomQp = 1 // true

		RcInitLayerMemory(pWelsSvcRc, 1+int32(pEncCtx.pSvcParam.sDependencyLayers[j].iHighestTemporalId))

		bMultiSliceMode = ((api.SM_RASTER_SLICE == pDLayerParam.SSliceArgument.UiSliceMode) ||
			(api.SM_SIZELIMITED_SLICE == pDLayerParam.SSliceArgument.UiSliceMode))
		if bMultiSliceMode {
			pWelsSvcRc.iNumberMbGom = pWelsSvcRc.iNumberMbFrame
		}
	}
}

func RcInitTlWeight(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pTOverRc := pWelsSvcRc.pTemporalOverRc
	pDLayerParam := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
	kiDecompositionStages := pDLayerParam.iDecompositionStages
	kiHighestTid := int32(pDLayerParam.iHighestTemporalId)

	//Index 0:Virtual GOP size, Index 1:Frame rate
	//double WeightArray[4][4] = { {1.0, 0, 0, 0}, {0.6, 0.4, 0, 0}, {0.4, 0.3, 0.15, 0}, {0.25, 0.15, 0.125, 0.0875}};
	iWeightArray := [4][4]int32{{2000, 0, 0, 0}, {1200, 800, 0, 0}, {800, 600, 300, 0}, {500, 300, 250, 175}} // original*WEIGHT_MULTIPLY
	kiGopSize := int32(1) << kiDecompositionStages
	var i, k, n int32

	n = 0
	for n <= kiHighestTid {
		pTOverRc[n].iTlayerWeight = iWeightArray[kiDecompositionStages][n]
		pTOverRc[n].iMinQp = pWelsSvcRc.iMinQp + (n << 1)
		pTOverRc[n].iMinQp = common.WELS_CLIP3(pTOverRc[n].iMinQp, 0, 51)
		pTOverRc[n].iMaxQp = pWelsSvcRc.iMaxQp + (n << 1)
		pTOverRc[n].iMaxQp = common.WELS_CLIP3(pTOverRc[n].iMaxQp, pTOverRc[n].iMinQp, 51)
		n++
	}
	//Calculate the frame index for the current frame and its reference frame
	for n = 0; n < VGOP_SIZE; n += kiGopSize {
		pWelsSvcRc.iTlOfFrames[n] = 0
		for i = 1; i <= kiDecompositionStages; i++ {
			for k = 1 << (kiDecompositionStages - i); k < kiGopSize; k += (kiGopSize >> (i - 1)) {
				pWelsSvcRc.iTlOfFrames[k+n] = int8(i)
			}
		}
	}
	pWelsSvcRc.iPreviousGopSize = kiGopSize
	pWelsSvcRc.iGopNumberInVGop = VGOP_SIZE / kiGopSize
}

func RcUpdateBitrateFps(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pTOverRc := pWelsSvcRc.pTemporalOverRc

	pDLayerParam := &pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId]
	pDLayerParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
	kiGopSize := int32(1) << pDLayerParamInternal.iDecompositionStages
	kiHighestTid := int32(pDLayerParamInternal.iHighestTemporalId)
	input_iBitsPerFrame := rcDivRoundF32(pDLayerParam.ISpatialBitrate, pDLayerParamInternal.fOutputFrameRate)
	kiGopBits := int64(input_iBitsPerFrame) * int64(kiGopSize)
	var i int32

	pWelsSvcRc.iBitRate = int64(pDLayerParam.ISpatialBitrate)
	pWelsSvcRc.fFrameRate = float64(pDLayerParamInternal.fOutputFrameRate)

	iTargetVaryRange := ((MAX_BITS_VARY_PERCENTAGE - pWelsSvcRc.iRcVaryRatio) >> 1)
	iMinBitsRatio := MAX_BITS_VARY_PERCENTAGE - iTargetVaryRange
	iMaxBitsRatio := int32(MAX_BITS_VARY_PERCENTAGE_x3d2)

	for i = 0; i <= kiHighestTid; i++ {
		kdConstraitBits := kiGopBits * int64(pTOverRc[i].iTlayerWeight)
		pTOverRc[i].iMinBitsTl = common.WELS_DIV_ROUND(kdConstraitBits*int64(iMinBitsRatio),
			int64(MAX_BITS_VARY_PERCENTAGE*WEIGHT_MULTIPLY))
		pTOverRc[i].iMaxBitsTl = common.WELS_DIV_ROUND(kdConstraitBits*int64(iMaxBitsRatio),
			int64(MAX_BITS_VARY_PERCENTAGE*WEIGHT_MULTIPLY))
	}
	//When bitrate is changed, pBuffer size should be updated
	pWelsSvcRc.iBufferSizeSkip = common.WELS_DIV_ROUND(pWelsSvcRc.iBitRate*int64(pWelsSvcRc.iSkipBufferRatio), INT_MULTIPLY)
	pWelsSvcRc.iBufferSizePadding = common.WELS_DIV_ROUND(pWelsSvcRc.iBitRate*PADDING_BUFFER_RATIO, INT_MULTIPLY)

	//change remaining bits
	if pWelsSvcRc.iBitsPerFrame > REMAIN_BITS_TH {
		pWelsSvcRc.iRemainingBits = common.WELS_DIV_ROUND(int64(pWelsSvcRc.iRemainingBits)*int64(input_iBitsPerFrame),
			int64(pWelsSvcRc.iBitsPerFrame))
	}
	pWelsSvcRc.iBitsPerFrame = input_iBitsPerFrame
	pWelsSvcRc.iMaxBitsPerFrame = rcDivRoundF32(pDLayerParam.IMaxSpatialBitrate, pDLayerParamInternal.fOutputFrameRate)
}

func RcInitVGop(pEncCtx *sWelsEncCtx) {
	kiDid := int32(pEncCtx.uiDependencyId)
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[kiDid]
	pTOverRc := pWelsSvcRc.pTemporalOverRc
	kiHighestTid := int32(pEncCtx.pSvcParam.sDependencyLayers[kiDid].iHighestTemporalId)
	fix_rc_overshoot := pEncCtx.pSvcParam.BFixRCOverShoot

	if fix_rc_overshoot {
		// subtract unused bits if interrupted in a mid of VGOP
		iLeftInVGop := pWelsSvcRc.iGopNumberInVGop - pWelsSvcRc.iGopIndexInVGop
		pWelsSvcRc.iRemainingBits -= iLeftInVGop * (pWelsSvcRc.iLastAllocatedBits / pWelsSvcRc.iGopNumberInVGop)
	}

	if fix_rc_overshoot && pWelsSvcRc.iRemainingBits < 0 {
		// carry over bitrate deficit, so we don't overshoot
		pWelsSvcRc.iRemainingBits += VGOP_SIZE * pWelsSvcRc.iBitsPerFrame
	} else {
		// but never more than target bitrate.
		pWelsSvcRc.iRemainingBits = VGOP_SIZE * pWelsSvcRc.iBitsPerFrame
	}

	if fix_rc_overshoot {
		// store last allocated bits to correctly recalculate carry over
		pWelsSvcRc.iLastAllocatedBits = pWelsSvcRc.iRemainingBits
	}
	pWelsSvcRc.iRemainingWeights = pWelsSvcRc.iGopNumberInVGop * WEIGHT_MULTIPLY

	pWelsSvcRc.iFrameCodedInVGop = 0
	pWelsSvcRc.iGopIndexInVGop = 0

	for i := int32(0); i <= kiHighestTid; i++ {
		pTOverRc[i].iGopBitsDq = 0
	}
	pWelsSvcRc.iSkipFrameInVGop = 0
}

func RcInitRefreshParameter(pEncCtx *sWelsEncCtx) {
	kiDid := int32(pEncCtx.uiDependencyId)
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[kiDid]
	pTOverRc := pWelsSvcRc.pTemporalOverRc
	pDLayerParam := &pEncCtx.pSvcParam.SSpatialLayers[kiDid]
	pDLayerParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[kiDid]
	kiHighestTid := int32(pDLayerParamInternal.iHighestTemporalId)
	fix_rc_overshoot := pEncCtx.pSvcParam.BFixRCOverShoot
	var i int32

	//I frame R-Q Model
	pWelsSvcRc.iIntraComplexity = 0
	pWelsSvcRc.iIntraMbCount = 0
	pWelsSvcRc.iIntraComplxMean = 0
	//P frame R-Q Model
	for i = 0; i <= kiHighestTid; i++ {
		pTOverRc[i].iPFrameNum = 0
		pTOverRc[i].iLinearCmplx = 0
		pTOverRc[i].iFrameCmplxMean = 0
	}

	pWelsSvcRc.iBufferFullnessSkip = 0
	pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW] = 0
	pWelsSvcRc.iBufferMaxBRFullness[ODD_TIME_WINDOW] = 0
	pWelsSvcRc.iPredFrameBit = 0
	pWelsSvcRc.iBufferFullnessPadding = 0

	pWelsSvcRc.iGopIndexInVGop = 0
	if fix_rc_overshoot {
		pWelsSvcRc.iLastAllocatedBits = 0
	}
	pWelsSvcRc.iRemainingBits = 0
	pWelsSvcRc.iBitsPerFrame = 0

	//Backup the initial bitrate and fps
	pWelsSvcRc.iPreviousBitrate = pDLayerParam.ISpatialBitrate
	pWelsSvcRc.dPreviousFps = float64(pDLayerParamInternal.fOutputFrameRate)

	clear(pWelsSvcRc.pCurrentFrameGomSad[:pWelsSvcRc.iGomSize])

	RcInitTlWeight(pEncCtx)
	RcUpdateBitrateFps(pEncCtx)
	RcInitVGop(pEncCtx)
}

func RcJudgeBitrateFpsUpdate(pEncCtx *sWelsEncCtx) bool {
	iCurDid := int32(pEncCtx.uiDependencyId)
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[iCurDid]
	pDLayerParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[iCurDid]
	pDLayerParam := &pEncCtx.pSvcParam.SSpatialLayers[iCurDid]

	if (pWelsSvcRc.iPreviousBitrate != pDLayerParam.ISpatialBitrate) ||
		(pWelsSvcRc.dPreviousFps-float64(pDLayerParamInternal.fOutputFrameRate)) > float64(common.EPSN) ||
		(pWelsSvcRc.dPreviousFps-float64(pDLayerParamInternal.fOutputFrameRate)) < -float64(common.EPSN) {
		pWelsSvcRc.iPreviousBitrate = pDLayerParam.ISpatialBitrate
		pWelsSvcRc.dPreviousFps = float64(pDLayerParamInternal.fOutputFrameRate)
		return true
	}
	return false
}

func RcUpdateTemporalZero(pEncCtx *sWelsEncCtx) {
	kiDid := int32(pEncCtx.uiDependencyId)
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[kiDid]
	pDLayerParam := &pEncCtx.pSvcParam.sDependencyLayers[kiDid]
	kiGopSize := int32(1) << pDLayerParam.iDecompositionStages

	if pWelsSvcRc.iPreviousGopSize != kiGopSize {
		RcInitTlWeight(pEncCtx)
		RcInitVGop(pEncCtx)
	} else if pWelsSvcRc.iGopIndexInVGop == pWelsSvcRc.iGopNumberInVGop || pEncCtx.eSliceType == common.I_SLICE {
		RcInitVGop(pEncCtx)
	}
	pWelsSvcRc.iGopIndexInVGop++
}

// rcFrameComplexity returns pVaa->sComplexityAnalysisParam.iFrameComplexity,
// or the screen content complexity for SCREEN_CONTENT_REAL_TIME.
func rcFrameComplexity(pEncCtx *sWelsEncCtx) int64 {
	iFrameComplexity := pEncCtx.pVaa.sComplexityAnalysisParam.IFrameComplexity
	if pEncCtx.pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		pVaa := pEncCtx.pVaa.pExt
		iFrameComplexity = pVaa.sComplexityScreenParam.IFrameComplexity
	}
	return iFrameComplexity
}

func RcCalculateIdrQp(pEncCtx *sWelsEncCtx) {
	var dBpp float64
	var i int32

	//64k@6fps for 90p:     bpp 0.74    QP:24
	//192k@12fps for 180p:  bpp 0.28    QP:26
	//512k@24fps for 360p:  bpp 0.09    QP:30
	//1500k@30fps for 720p: bpp 0.05    QP:32
	dBppArray := [4][4]float64{{0.25, 0.5, 0.75, 1.0}, {0.1, 0.2, 0.3, 0.4}, {0.03, 0.05, 0.09, 0.13}, {0.01, 0.03, 0.06, 0.1}}
	dInitialQPArray := [4][5]int32{{34, 28, 26, 24, 22}, {36, 30, 28, 26, 24}, {36, 32, 30, 28, 26}, {36, 34, 32, 30, 28}}
	var iBppIndex int32
	iQpRangeArray := [5][2]int32{{40, 28}, {37, 25}, {36, 24}, {35, 23}, {34, 22}}
	iFrameComplexity := rcFrameComplexity(pEncCtx)
	fix_rc_overshoot := pEncCtx.pSvcParam.BFixRCOverShoot
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pDLayerParam := &pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId]
	pDLayerParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
	if pDLayerParamInternal.fOutputFrameRate > common.EPSN && pDLayerParam.IVideoWidth != 0 && pDLayerParam.IVideoHeight != 0 {
		// the denominator is evaluated in float
		fDen := pDLayerParamInternal.fOutputFrameRate * float32(pDLayerParam.IVideoWidth)
		fDen = fDen * float32(pDLayerParam.IVideoHeight)
		dBpp = float64(pDLayerParam.ISpatialBitrate) / float64(fDen)
	} else {
		dBpp = 0.1
	}
	//Area*2
	if pDLayerParam.IVideoWidth*pDLayerParam.IVideoHeight <= 28800 { // 90p video:160*90
		iBppIndex = 0
	} else if pDLayerParam.IVideoWidth*pDLayerParam.IVideoHeight <= 115200 { // 180p video:320*180
		iBppIndex = 1
	} else if pDLayerParam.IVideoWidth*pDLayerParam.IVideoHeight <= 460800 { // 360p video:640*360
		iBppIndex = 2
	} else {
		iBppIndex = 3
	}

	//Search
	if fix_rc_overshoot {
		i = 0
	} else {
		i = 1
	}
	for ; i < 4; i++ {
		if dBpp <= dBppArray[iBppIndex][i] {
			break
		}
	}
	iMaxQp := iQpRangeArray[i][0]
	iMinQp := iQpRangeArray[i][1]
	iMinQp = common.WELS_CLIP3(iMinQp, pWelsSvcRc.iMinQp, pWelsSvcRc.iMaxQp)
	iMaxQp = common.WELS_CLIP3(iMaxQp, pWelsSvcRc.iMinQp, pWelsSvcRc.iMaxQp)
	if 0 == pWelsSvcRc.iIdrNum { //the first IDR frame
		pWelsSvcRc.iInitialQp = dInitialQPArray[iBppIndex][i]
	} else {

		//obtain the idr qp using previous idr complexity
		if pWelsSvcRc.iNumberMbFrame != pWelsSvcRc.iIntraMbCount {
			pWelsSvcRc.iIntraComplexity = int64(int32(pWelsSvcRc.iIntraComplexity * int64(pWelsSvcRc.iNumberMbFrame) /
				int64(pWelsSvcRc.iIntraMbCount)))
		}

		iCmplxRatio := common.WELS_DIV_ROUND64(iFrameComplexity*INT_MULTIPLY,
			pWelsSvcRc.iIntraComplxMean)
		iCmplxRatio = common.WELS_CLIP3(iCmplxRatio, INT_MULTIPLY-FRAME_CMPLX_RATIO_RANGE, INT_MULTIPLY+FRAME_CMPLX_RATIO_RANGE)
		pWelsSvcRc.iQStep = int32(common.WELS_DIV_ROUND64(pWelsSvcRc.iIntraComplexity*iCmplxRatio,
			int64(pWelsSvcRc.iTargetBits)*INT_MULTIPLY))
		pWelsSvcRc.iInitialQp = RcConvertQStep2Qp(pWelsSvcRc.iQStep)
	}

	pWelsSvcRc.iInitialQp = common.WELS_CLIP3(pWelsSvcRc.iInitialQp, iMinQp, iMaxQp)
	pEncCtx.iGlobalQp = pWelsSvcRc.iInitialQp
	pWelsSvcRc.iQStep = RcConvertQp2QStep(pEncCtx.iGlobalQp)
	pWelsSvcRc.iLastCalculatedQScale = pEncCtx.iGlobalQp
	pWelsSvcRc.iMinFrameQp = common.WELS_CLIP3(pEncCtx.iGlobalQp-DELTA_QP_BGD_THD, iMinQp, iMaxQp)
	pWelsSvcRc.iMaxFrameQp = common.WELS_CLIP3(pEncCtx.iGlobalQp+DELTA_QP_BGD_THD, iMinQp, iMaxQp)
}

func RcCalculatePictureQp(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	iTl := int32(pEncCtx.uiTemporalId)
	pTOverRc := &pWelsSvcRc.pTemporalOverRc[iTl]
	var iLumaQp int32
	var iDeltaQpTemporal int32
	iFrameComplexity := rcFrameComplexity(pEncCtx)
	if 0 == pTOverRc.iPFrameNum {
		iLumaQp = pWelsSvcRc.iInitialQp
	} else if pWelsSvcRc.iCurrentBitsLevel == BITS_EXCEEDED {
		iLumaQp = pWelsSvcRc.iLastCalculatedQScale + DELTA_QP_BGD_THD
		//limit QP
		iLastIdxCodecInVGop := pWelsSvcRc.iFrameCodedInVGop - 1
		if iLastIdxCodecInVGop < 0 {
			iLastIdxCodecInVGop += VGOP_SIZE
		}
		iTlLast := int32(pWelsSvcRc.iTlOfFrames[iLastIdxCodecInVGop])
		iDeltaQpTemporal = iTl - iTlLast
		if 0 == iTlLast && iTl > 0 {
			iDeltaQpTemporal += 1
		} else if 0 == iTl && iTlLast > 0 {
			iDeltaQpTemporal -= 1
		}

	} else {
		iCmplxRatio := common.WELS_DIV_ROUND64(iFrameComplexity*INT_MULTIPLY,
			pTOverRc.iFrameCmplxMean)
		iCmplxRatio = common.WELS_CLIP3(iCmplxRatio, INT_MULTIPLY-FRAME_CMPLX_RATIO_RANGE, INT_MULTIPLY+FRAME_CMPLX_RATIO_RANGE)

		pWelsSvcRc.iQStep = int32(common.WELS_DIV_ROUND64(pTOverRc.iLinearCmplx*iCmplxRatio,
			int64(pWelsSvcRc.iTargetBits)*INT_MULTIPLY))
		iLumaQp = RcConvertQStep2Qp(pWelsSvcRc.iQStep)
		common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
			"iCmplxRatio = %d,frameComplexity = %d,iFrameCmplxMean = %d,iQStep = %d,iLumaQp = %d", int32(iCmplxRatio),
			iFrameComplexity, pTOverRc.iFrameCmplxMean, pWelsSvcRc.iQStep, iLumaQp)
		//limit QP
		iLastIdxCodecInVGop := pWelsSvcRc.iFrameCodedInVGop - 1
		if iLastIdxCodecInVGop < 0 {
			iLastIdxCodecInVGop += VGOP_SIZE
		}
		iTlLast := int32(pWelsSvcRc.iTlOfFrames[iLastIdxCodecInVGop])
		iDeltaQpTemporal = iTl - iTlLast
		if 0 == iTlLast && iTl > 0 {
			iDeltaQpTemporal += 1
		} else if 0 == iTl && iTlLast > 0 {
			iDeltaQpTemporal -= 1
		}
	}
	pWelsSvcRc.iMinFrameQp = common.WELS_CLIP3(pWelsSvcRc.iLastCalculatedQScale-pWelsSvcRc.iFrameDeltaQpLower+
		iDeltaQpTemporal, pTOverRc.iMinQp, pTOverRc.iMaxQp)
	pWelsSvcRc.iMaxFrameQp = common.WELS_CLIP3(pWelsSvcRc.iLastCalculatedQScale+pWelsSvcRc.iFrameDeltaQpUpper+
		iDeltaQpTemporal, pTOverRc.iMinQp, pTOverRc.iMaxQp)

	iLumaQp = common.WELS_CLIP3(iLumaQp, pWelsSvcRc.iMinFrameQp, pWelsSvcRc.iMaxFrameQp)

	if pEncCtx.pSvcParam.BEnableAdaptiveQuant {

		iLumaQp = common.WELS_DIV_ROUND(iLumaQp*INT_MULTIPLY-pEncCtx.pVaa.sAdaptiveQuantParam.IAverMotionTextureIndexToDeltaQp,
			INT_MULTIPLY)
		iLumaQp = common.WELS_CLIP3(iLumaQp, pWelsSvcRc.iMinFrameQp, pWelsSvcRc.iMaxFrameQp)
	}
	pWelsSvcRc.iQStep = RcConvertQp2QStep(iLumaQp)
	pWelsSvcRc.iLastCalculatedQScale = iLumaQp
	pEncCtx.iGlobalQp = iLumaQp
}

func GomRCInitForOneSlice(pSlice *SSlice, kiBitsPerMb int32) {
	pSOverRc := &pSlice.sSlicingOverRc
	pSOverRc.iStartMbSlice = pSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice
	pSOverRc.iEndMbSlice = pSOverRc.iStartMbSlice + pSlice.iCountMbNumInSlice - 1
	pSOverRc.iTargetBitsSlice = common.WELS_DIV_ROUND(int64(kiBitsPerMb)*int64(pSlice.iCountMbNumInSlice),
		INT_MULTIPLY)
}

func RcInitSliceInformation(pEncCtx *sWelsEncCtx) {
	ppSliceInLayer := pEncCtx.pCurDqLayer.ppSliceInLayer
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	kiSliceNum := pEncCtx.pCurDqLayer.iMaxSliceNum
	pWelsSvcRc.iBitsPerMb = common.WELS_DIV_ROUND(int64(pWelsSvcRc.iTargetBits)*INT_MULTIPLY,
		int64(pWelsSvcRc.iNumberMbFrame))
	pWelsSvcRc.bGomRC = !(api.RC_OFF_MODE == pEncCtx.pSvcParam.IRCMode ||
		api.RC_BUFFERBASED_MODE == pEncCtx.pSvcParam.IRCMode)
	for i := int32(0); i < kiSliceNum; i++ {
		pSOverRc := &ppSliceInLayer[i].sSlicingOverRc
		pSOverRc.iTotalQpSlice = 0
		pSOverRc.iTotalMbSlice = 0
		pSOverRc.iFrameBitsSlice = 0
		pSOverRc.iGomBitsSlice = 0
		pSOverRc.iStartMbSlice = 0
		pSOverRc.iEndMbSlice = 0
		pSOverRc.iTargetBitsSlice = 0
	}
}

func RcDecideTargetBits(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pTOverRc := &pWelsSvcRc.pTemporalOverRc[pEncCtx.uiTemporalId]

	pWelsSvcRc.iCurrentBitsLevel = BITS_NORMAL
	fix_rc_overshoot := pEncCtx.pSvcParam.BFixRCOverShoot
	//allocate bits
	if pEncCtx.eSliceType == common.I_SLICE {
		if pWelsSvcRc.iIdrNum != 0 {
			pWelsSvcRc.iTargetBits = int32(int64(pWelsSvcRc.iBitsPerFrame) * int64(pEncCtx.pSvcParam.IIdrBitrateRatio) / 100)
		} else {
			pWelsSvcRc.iTargetBits = int32(int64(pWelsSvcRc.iBitsPerFrame) * IDR_BITRATE_RATIO)
		}
	} else {
		if pWelsSvcRc.iRemainingWeights > pTOverRc.iTlayerWeight ||
			(fix_rc_overshoot && pWelsSvcRc.iRemainingWeights == pTOverRc.iTlayerWeight) {
			pWelsSvcRc.iTargetBits = common.WELS_DIV_ROUND(int64(pWelsSvcRc.iRemainingBits)*int64(pTOverRc.iTlayerWeight),
				int64(pWelsSvcRc.iRemainingWeights))
		} else { //this case should be not hit. needs to more test case to verify this
			pWelsSvcRc.iTargetBits = pWelsSvcRc.iRemainingBits
		}
		if (pWelsSvcRc.iTargetBits <= 0) && ((pEncCtx.pSvcParam.IRCMode == api.RC_BITRATE_MODE) &&
			(pEncCtx.pSvcParam.BEnableFrameSkip == false)) {
			pWelsSvcRc.iCurrentBitsLevel = BITS_EXCEEDED
		}
		pWelsSvcRc.iTargetBits = common.WELS_CLIP3(pWelsSvcRc.iTargetBits, pTOverRc.iMinBitsTl, pTOverRc.iMaxBitsTl)
	}
	pWelsSvcRc.iRemainingWeights -= pTOverRc.iTlayerWeight
}

// rcMinTh is static_cast<int32_t>((fFrameRate < 8) ? iBufferTh * 1.0 / 4 : iBufferTh * 2 / fFrameRate):
// the second operand is evaluated in float, the conditional in double.
func rcMinTh(iBufferTh int32, fFrameRate float32) int32 {
	if fFrameRate < 8 {
		return int32(float64(iBufferTh) * 1.0 / 4)
	}
	return int32(float64(float32(iBufferTh*2) / fFrameRate))
}

func RcDecideTargetBitsTimestamp(pEncCtx *sWelsEncCtx) {
	//decide one frame bits allocated
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pDLayerParam := &pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId]
	iTl := int32(pEncCtx.uiTemporalId)
	pTOverRc := &pWelsSvcRc.pTemporalOverRc[iTl]
	pWelsSvcRc.iCurrentBitsLevel = BITS_NORMAL

	if pEncCtx.eSliceType == common.I_SLICE {
		iBufferTh := int32(int64(pWelsSvcRc.iBufferSizeSkip) - pWelsSvcRc.iBufferFullnessSkip)
		if iBufferTh <= 0 {
			pWelsSvcRc.iCurrentBitsLevel = BITS_EXCEEDED
			pWelsSvcRc.iTargetBits = pTOverRc.iMinBitsTl
		} else {
			iMaxTh := iBufferTh * 3 / 4
			iMinTh := rcMinTh(iBufferTh, pDLayerParam.FFrameRate)
			if pDLayerParam.FFrameRate < (IDR_BITRATE_RATIO + 1) {
				pWelsSvcRc.iTargetBits = int32(float64(pDLayerParam.ISpatialBitrate) / float64(pDLayerParam.FFrameRate))
			} else {
				pWelsSvcRc.iTargetBits = int32(float64(pDLayerParam.ISpatialBitrate) / float64(pDLayerParam.FFrameRate) * IDR_BITRATE_RATIO)
			}
			common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"iMaxTh = %d,iMinTh = %d,pWelsSvcRc->iTargetBits = %d,pWelsSvcRc->iBufferSizeSkip = %d, pWelsSvcRc->iBufferFullnessSkip= %d",
				iMaxTh, iMinTh, pWelsSvcRc.iTargetBits, pWelsSvcRc.iBufferSizeSkip, pWelsSvcRc.iBufferFullnessSkip)
			pWelsSvcRc.iTargetBits = common.WELS_CLIP3(pWelsSvcRc.iTargetBits, iMinTh, iMaxTh)
		}

	} else {
		iBufferTh := int32(int64(pWelsSvcRc.iBufferSizeSkip) - pWelsSvcRc.iBufferFullnessSkip)
		if iBufferTh <= 0 {
			pWelsSvcRc.iCurrentBitsLevel = BITS_EXCEEDED
			pWelsSvcRc.iTargetBits = pTOverRc.iMinBitsTl
			common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"iMaxTh = %d,pWelsSvcRc->iTargetBits = %d,pWelsSvcRc->iBufferSizeSkip = %d, pWelsSvcRc->iBufferFullnessSkip= %d",
				iBufferTh, pWelsSvcRc.iTargetBits, pWelsSvcRc.iBufferSizeSkip, pWelsSvcRc.iBufferFullnessSkip)
		} else {

			pDLayerParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
			kiGopSize := int32(1) << pDLayerParamInternal.iDecompositionStages
			iAverageFrameSize := int32(float64(pDLayerParam.ISpatialBitrate) / float64(pDLayerParam.FFrameRate))
			kiGopBits := iAverageFrameSize * kiGopSize
			pWelsSvcRc.iTargetBits = common.WELS_DIV_ROUND(pTOverRc.iTlayerWeight*kiGopBits, INT_MULTIPLY*10*2)

			iMaxTh := iBufferTh / 2
			iMinTh := rcMinTh(iBufferTh, pDLayerParam.FFrameRate)
			common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"iMaxTh = %d,iMinTh = %d,pWelsSvcRc->iTargetBits = %d,pWelsSvcRc->iBufferSizeSkip = %d, pWelsSvcRc->iBufferFullnessSkip= % d",
				iMaxTh, iMinTh, pWelsSvcRc.iTargetBits, pWelsSvcRc.iBufferSizeSkip, pWelsSvcRc.iBufferFullnessSkip)
			pWelsSvcRc.iTargetBits = common.WELS_CLIP3(pWelsSvcRc.iTargetBits, iMinTh, iMaxTh)
		}
	}
}

func RcInitGomParameters(pEncCtx *sWelsEncCtx) {
	ppSliceInLayer := pEncCtx.pCurDqLayer.ppSliceInLayer
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	kiSliceNum := pEncCtx.pCurDqLayer.iMaxSliceNum
	kiGlobalQp := pEncCtx.iGlobalQp

	pWelsSvcRc.iAverageFrameQp = 0
	for i := int32(0); i < kiSliceNum; i++ {
		pSOverRc := &ppSliceInLayer[i].sSlicingOverRc
		pSOverRc.iComplexityIndexSlice = 0
		pSOverRc.iCalculatedQpSlice = kiGlobalQp
	}
	clear(pWelsSvcRc.pGomComplexity[:pWelsSvcRc.iGomSize])
	clear(pWelsSvcRc.pGomCost[:pWelsSvcRc.iGomSize])
}

func RcCalculateMbQp(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pSOverRc := &pSlice.sSlicingOverRc

	iLumaQp := pSOverRc.iCalculatedQpSlice
	pCurLayer := pEncCtx.pCurDqLayer
	kuiChromaQpIndexOffset := pCurLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset
	if pEncCtx.pSvcParam.BEnableAdaptiveQuant {
		iLumaQp = int32(int8(common.WELS_CLIP3(iLumaQp+
			int32(pEncCtx.pVaa.sAdaptiveQuantParam.PMotionTextureIndexToDeltaQp[pCurMb.iMbXY]), pWelsSvcRc.iMinFrameQp,
			pWelsSvcRc.iMaxFrameQp)))
	}
	pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(iLumaQp+int32(kuiChromaQpIndexOffset))]
	pCurMb.uiLumaQp = uint8(iLumaQp)
}

// returns a pointer into pEncCtx.pWelsSvcRc (or nil).
func RcJudgeBaseUsability(pEncCtx *sWelsEncCtx) *SWelsSvcRc {
	var pWelsSvcRc, pWelsSvcRc_Base *SWelsSvcRc
	var pDlpBase, pDLayerParam *api.SSpatialLayerConfig
	var pDlpBaseInternal *SSpatialLayerInternal
	if pEncCtx.uiDependencyId <= 0 {
		return nil
	}
	pDlpBaseInternal = &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId-1]
	if int32(pEncCtx.uiTemporalId) <= pDlpBaseInternal.iDecompositionStages {
		pWelsSvcRc = &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
		pWelsSvcRc_Base = &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId-1]
		pDLayerParam = &pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId]
		pDlpBase = &pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId-1]
		if (pDLayerParam.IVideoWidth * pDLayerParam.IVideoHeight / pWelsSvcRc.iNumberMbGom) ==
			(pDlpBase.IVideoWidth * pDlpBase.IVideoHeight / pWelsSvcRc_Base.iNumberMbGom) {
			return pWelsSvcRc_Base
		}
		return nil
	}
	return nil
}

func RcGomTargetBits(pEncCtx *sWelsEncCtx, pSlice *SSlice) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	var pWelsSvcRc_Base *SWelsSvcRc
	pSOverRc := &pSlice.sSlicingOverRc

	var iAllocateBits int32
	var iSumSad int32
	var iLastGomIndex int32
	var iLeftBits int32
	kiComplexityIndex := pSOverRc.iComplexityIndexSlice
	var i int32

	iLastGomIndex = pSOverRc.iEndMbSlice / pWelsSvcRc.iNumberMbGom
	iLeftBits = pSOverRc.iTargetBitsSlice - pSOverRc.iFrameBitsSlice
	if iLeftBits <= 0 {
		pSOverRc.iGomTargetBits = 0
		return
	} else if kiComplexityIndex >= iLastGomIndex {
		iAllocateBits = iLeftBits
	} else {
		pWelsSvcRc_Base = RcJudgeBaseUsability(pEncCtx)
		if pWelsSvcRc_Base == nil {
			pWelsSvcRc_Base = pWelsSvcRc
		}
		for i = kiComplexityIndex + 1; i <= iLastGomIndex; i++ {
			iSumSad += pWelsSvcRc_Base.pCurrentFrameGomSad[i]
		}

		if 0 == iSumSad {
			iAllocateBits = common.WELS_DIV_ROUND(iLeftBits, (iLastGomIndex - kiComplexityIndex))
		} else {
			iAllocateBits = common.WELS_DIV_ROUND(int64(iLeftBits)*int64(pWelsSvcRc_Base.pCurrentFrameGomSad[kiComplexityIndex+1]),
				int64(iSumSad))
		}
	}
	pSOverRc.iGomTargetBits = iAllocateBits
}

func RcCalculateGomQp(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pSOverRc := &pSlice.sSlicingOverRc
	var iBitsRatio int64

	iLeftBits := int64(pSOverRc.iTargetBitsSlice - pSOverRc.iFrameBitsSlice)
	iTargetLeftBits := iLeftBits + int64(pSOverRc.iGomBitsSlice-pSOverRc.iGomTargetBits)
	if (iLeftBits <= 0) || (iTargetLeftBits <= 0) {
		pSOverRc.iCalculatedQpSlice += 2
	} else {
		//globe decision
		iBitsRatio = 10000 * iLeftBits / (iTargetLeftBits + 1)
		if iBitsRatio < 8409 { //2^(-1.5/6)*10000
			pSOverRc.iCalculatedQpSlice += 2
		} else if iBitsRatio < 9439 { //2^(-0.5/6)*10000
			pSOverRc.iCalculatedQpSlice += 1
		} else if iBitsRatio > 10600 { //2^(0.5/6)*10000
			pSOverRc.iCalculatedQpSlice -= 1
		} else if iBitsRatio > 11900 { //2^(1.5/6)*10000
			pSOverRc.iCalculatedQpSlice -= 2
		}
	}
	pSOverRc.iCalculatedQpSlice = common.WELS_CLIP3(pSOverRc.iCalculatedQpSlice, pWelsSvcRc.iMinFrameQp,
		pWelsSvcRc.iMaxFrameQp)
	pSOverRc.iGomBitsSlice = 0
}

func RcVBufferCalculationSkip(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pTOverRc := pWelsSvcRc.pTemporalOverRc
	kiOutputBits := pWelsSvcRc.iBitsPerFrame
	kiOutputMaxBits := pWelsSvcRc.iMaxBitsPerFrame
	//condition 1: whole pBuffer fullness
	pWelsSvcRc.iBufferFullnessSkip += int64(pWelsSvcRc.iFrameDqBits - kiOutputBits)
	pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW] += int64(pWelsSvcRc.iFrameDqBits - kiOutputMaxBits)
	pWelsSvcRc.iBufferMaxBRFullness[ODD_TIME_WINDOW] += int64(pWelsSvcRc.iFrameDqBits - kiOutputMaxBits)

	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
		"[Rc] bits in buffer = %d, bits in Max bitrate buffer = %d",
		pWelsSvcRc.iBufferFullnessSkip, pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW])
	//condition 2: VGOP bits constraint
	var iVGopBitsPred int64
	for i := pWelsSvcRc.iFrameCodedInVGop + 1; i < VGOP_SIZE; i++ {
		iVGopBitsPred += int64(pTOverRc[pWelsSvcRc.iTlOfFrames[i]].iMinBitsTl)
	}
	iVGopBitsPred -= int64(pWelsSvcRc.iRemainingBits)
	dIncPercent := float64(iVGopBitsPred)*100.0/float64(pWelsSvcRc.iBitsPerFrame*VGOP_SIZE) -
		float64(VGOP_BITS_PERCENTAGE_DIFF)

	if (pWelsSvcRc.iBufferFullnessSkip > int64(pWelsSvcRc.iBufferSizeSkip) &&
		pWelsSvcRc.iAverageFrameQp > pWelsSvcRc.iSkipQpValue) ||
		(dIncPercent > float64(pWelsSvcRc.iRcVaryPercentage)) {
		pWelsSvcRc.bSkipFlag = true
	}
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
		"[Rc] VBV_Skip,dIncPercent = %f,iRcVaryPercentage = %d,pWelsSvcRc->bSkipFlag = %d", dIncPercent,
		pWelsSvcRc.iRcVaryPercentage, rcBool2Int(pWelsSvcRc.bSkipFlag))
}

// rcBool2Int converts a C bool for printing with %d.
func rcBool2Int(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// long long -> int64.
func CheckFrameSkipBasedMaxbr(pEncCtx *sWelsEncCtx, uiTimeStamp int64, iDidIdx int32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[iDidIdx]
	pDLayerParam := &pEncCtx.pSvcParam.SSpatialLayers[iDidIdx]
	if !pEncCtx.pSvcParam.BEnableFrameSkip {
		return
	}
	iSentBits := pWelsSvcRc.iBitsPerFrame
	kiOutputMaxBits := pWelsSvcRc.iMaxBitsPerFrame
	kiMaxSpatialBitRate := int64(pDLayerParam.IMaxSpatialBitrate)

	//estimate allowed continual skipped frames in the sequence
	iPredSkipFramesTarBr := (common.WELS_DIV_ROUND(pWelsSvcRc.iBufferFullnessSkip, int64(iSentBits)) + 1) >> 1
	iPredSkipFramesMaxBr := (common.WELS_MAX(common.WELS_DIV_ROUND(pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW],
		int64(kiOutputMaxBits)), 0) + 1) >> 1

	//calculate the remaining bits in TIME_CHECK_WINDOW
	iAvailableBitsInTimeWindow := common.WELS_DIV_ROUND(int64(TIME_CHECK_WINDOW-pEncCtx.iCheckWindowInterval)*
		kiMaxSpatialBitRate, 1000)
	iAvailableBitsInShiftTimeWindow := common.WELS_DIV_ROUND(int64(TIME_CHECK_WINDOW-pEncCtx.iCheckWindowIntervalShift)*
		kiMaxSpatialBitRate, 1000)

	var bJudgeMaxBRbSkip [TIME_WINDOW_TOTAL]bool //0: EVEN_TIME_WINDOW; 1: ODD_TIME_WINDOW
	fix_rc_overshoot := pEncCtx.pSvcParam.BFixRCOverShoot

	/* 4 cases for frame skipping
	1:skipping when buffer size larger than target threshold and current continual skip frames is allowed
	2:skipping when MaxBr buffer size + predict frame size - remaining bits in time window < 0 and current continual skip frames is allowed
	3:if in last ODD_TIME_WINDOW the MAX Br is overflowed, make more strict skipping conditions
	4:such as case 3 in the other window
	*/
	bJudgeBufferFullSkip := (pWelsSvcRc.iContinualSkipFrames <= iPredSkipFramesTarBr) &&
		(pWelsSvcRc.iBufferFullnessSkip > int64(pWelsSvcRc.iBufferSizeSkip))
	bJudgeMaxBRbufferFullSkip := (pWelsSvcRc.iContinualSkipFrames <= iPredSkipFramesMaxBr) &&
		(pEncCtx.iCheckWindowInterval > TIME_CHECK_WINDOW/2) &&
		(pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW]+int64(pWelsSvcRc.iPredFrameBit)-int64(iAvailableBitsInTimeWindow) > 0)
	bJudgeMaxBRbSkip[EVEN_TIME_WINDOW] = (pEncCtx.iCheckWindowInterval > TIME_CHECK_WINDOW/2) &&
		(pWelsSvcRc.bNeedShiftWindowCheck[EVEN_TIME_WINDOW]) &&
		(pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW]+int64(pWelsSvcRc.iPredFrameBit)-int64(iAvailableBitsInTimeWindow)+
			int64(kiOutputMaxBits) > 0)
	bJudgeMaxBRbSkip[ODD_TIME_WINDOW] = (pEncCtx.iCheckWindowIntervalShift > TIME_CHECK_WINDOW/2) &&
		(pWelsSvcRc.bNeedShiftWindowCheck[ODD_TIME_WINDOW]) &&
		(pWelsSvcRc.iBufferMaxBRFullness[ODD_TIME_WINDOW]+int64(pWelsSvcRc.iPredFrameBit)-int64(iAvailableBitsInShiftTimeWindow)+
			int64(kiOutputMaxBits) > 0)

	pWelsSvcRc.bSkipFlag = false
	if bJudgeBufferFullSkip || bJudgeMaxBRbufferFullSkip || bJudgeMaxBRbSkip[EVEN_TIME_WINDOW] ||
		bJudgeMaxBRbSkip[ODD_TIME_WINDOW] {
		pWelsSvcRc.bSkipFlag = true
		if !fix_rc_overshoot {
			pWelsSvcRc.iSkipFrameNum++
			pWelsSvcRc.iSkipFrameInVGop++
			pWelsSvcRc.iBufferFullnessSkip -= int64(iSentBits)
			pWelsSvcRc.iRemainingBits += iSentBits
			pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW] -= int64(kiOutputMaxBits)
			pWelsSvcRc.iBufferMaxBRFullness[ODD_TIME_WINDOW] -= int64(kiOutputMaxBits)
			common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"[Rc] bits in buffer = %d, bits in Max bitrate buffer = %d, Predict skip frames = %d and %d",
				pWelsSvcRc.iBufferFullnessSkip, pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW], iPredSkipFramesTarBr,
				iPredSkipFramesMaxBr)
			pWelsSvcRc.iBufferFullnessSkip = common.WELS_MAX(pWelsSvcRc.iBufferFullnessSkip, 0)
		}
	}
}

func WelsRcCheckFrameStatus(pEncCtx *sWelsEncCtx, uiTimeStamp int64, iSpatialNum int32, iCurDid int32) bool {

	bSkipMustFlag := false

	pSpatialIndexMap := pEncCtx.sSpatialIndexMap[:]

	//simul_cast AVC control
	if pEncCtx.pSvcParam.BSimulcastAVC {
		//check target_br skip and update info
		iDidIdx := iCurDid
		if pEncCtx.pFuncList.pfRc.pfWelsRcPicDelayJudge != nil {
			pEncCtx.pFuncList.pfRc.pfWelsRcPicDelayJudge(pEncCtx, uiTimeStamp, iDidIdx)
		}
		if true == pEncCtx.pWelsSvcRc[iDidIdx].bSkipFlag {
			bSkipMustFlag = true
		}
		//check max_br skip
		if pEncCtx.pFuncList.pfRc.pfWelsCheckSkipBasedMaxbr != nil {
			if (!bSkipMustFlag) && (pEncCtx.pSvcParam.SSpatialLayers[iDidIdx].IMaxSpatialBitrate != api.UNSPECIFIED_BIT_RATE) {
				pEncCtx.pFuncList.pfRc.pfWelsCheckSkipBasedMaxbr(pEncCtx, uiTimeStamp, iDidIdx)
				if true == pEncCtx.pWelsSvcRc[iDidIdx].bSkipFlag {
					bSkipMustFlag = true

				}
			}
		}
		if bSkipMustFlag {
			pEncCtx.pWelsSvcRc[iDidIdx].uiLastTimeStamp = uiTimeStamp
			pEncCtx.pWelsSvcRc[iDidIdx].bSkipFlag = false
			pEncCtx.pWelsSvcRc[iDidIdx].iContinualSkipFrames++
			return true
		}
	} else { //SVC control
		for i := int32(0); i < iSpatialNum; i++ {
			iDidIdx := pSpatialIndexMap[i].iDid
			//check target_br skip and update info

			if pEncCtx.pFuncList.pfRc.pfWelsRcPicDelayJudge != nil {
				pEncCtx.pFuncList.pfRc.pfWelsRcPicDelayJudge(pEncCtx, uiTimeStamp, iDidIdx)
			}
			if true == pEncCtx.pWelsSvcRc[iDidIdx].bSkipFlag {
				bSkipMustFlag = true
			}
			//check max_br skip
			if pEncCtx.pFuncList.pfRc.pfWelsCheckSkipBasedMaxbr != nil {
				if (!bSkipMustFlag) && (pEncCtx.pSvcParam.SSpatialLayers[iDidIdx].IMaxSpatialBitrate != api.UNSPECIFIED_BIT_RATE) {
					pEncCtx.pFuncList.pfRc.pfWelsCheckSkipBasedMaxbr(pEncCtx, uiTimeStamp, iDidIdx)
					if true == pEncCtx.pWelsSvcRc[iDidIdx].bSkipFlag {
						bSkipMustFlag = true
					}
				}
			}
			if bSkipMustFlag {
				break
			}
		}

		if bSkipMustFlag {
			for i := int32(0); i < iSpatialNum; i++ {
				iDidIdx := pSpatialIndexMap[i].iDid
				pEncCtx.pWelsSvcRc[iDidIdx].uiLastTimeStamp = uiTimeStamp
				pEncCtx.pWelsSvcRc[iDidIdx].bSkipFlag = false
				pEncCtx.pWelsSvcRc[iDidIdx].iContinualSkipFrames++
			}
			return true
		}
	}
	return false
}

func UpdateBufferWhenFrameSkipped(pEncCtx *sWelsEncCtx, iCurDid int32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[iCurDid]
	kiOutputBits := pWelsSvcRc.iBitsPerFrame
	kiOutputMaxBits := pWelsSvcRc.iMaxBitsPerFrame
	pWelsSvcRc.iBufferFullnessSkip = pWelsSvcRc.iBufferFullnessSkip - int64(kiOutputBits)
	pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW] -= int64(kiOutputMaxBits)
	pWelsSvcRc.iBufferMaxBRFullness[ODD_TIME_WINDOW] -= int64(kiOutputMaxBits)
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
		"[Rc] iDid = %d,bits in buffer = %d, bits in Max bitrate buffer = %d",
		iCurDid, pWelsSvcRc.iBufferFullnessSkip, pWelsSvcRc.iBufferMaxBRFullness[EVEN_TIME_WINDOW])

	pWelsSvcRc.iBufferFullnessSkip = common.WELS_MAX(pWelsSvcRc.iBufferFullnessSkip, 0)

	pWelsSvcRc.iRemainingBits += kiOutputBits
	pWelsSvcRc.iSkipFrameNum++
	pWelsSvcRc.iSkipFrameInVGop++

	if (pWelsSvcRc.iContinualSkipFrames % 3) == 0 {
		//output a warning when iContinualSkipFrames is large enough, which may indicate subjective quality problem
		//note that here iContinualSkipFrames must be >0, so the log output will be 3/6/....
		common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_WARNING, "[Rc] iDid = %d,iContinualSkipFrames(%d) is large",
			iCurDid, pWelsSvcRc.iContinualSkipFrames)
	}
}

func UpdateMaxBrCheckWindowStatus(pEncCtx *sWelsEncCtx, iSpatialNum int32, uiTimeStamp int64) {
	pSpatialIndexMap := pEncCtx.sSpatialIndexMap[:]
	if pEncCtx.bCheckWindowStatusRefreshFlag {
		pEncCtx.iCheckWindowCurrentTs = uiTimeStamp
	} else {
		pEncCtx.iCheckWindowStartTs = uiTimeStamp
		pEncCtx.iCheckWindowCurrentTs = uiTimeStamp
		pEncCtx.bCheckWindowStatusRefreshFlag = true
		for i := int32(0); i < iSpatialNum; i++ {
			iCurDid := pSpatialIndexMap[i].iDid
			pEncCtx.pWelsSvcRc[iCurDid].iBufferFullnessSkip = 0
			pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[ODD_TIME_WINDOW] = 0
			pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[EVEN_TIME_WINDOW] = 0
			pEncCtx.pWelsSvcRc[iCurDid].bNeedShiftWindowCheck[ODD_TIME_WINDOW] = false
			pEncCtx.pWelsSvcRc[iCurDid].bNeedShiftWindowCheck[EVEN_TIME_WINDOW] = false
		}

	}
	pEncCtx.iCheckWindowInterval = int32(pEncCtx.iCheckWindowCurrentTs - pEncCtx.iCheckWindowStartTs)
	if pEncCtx.iCheckWindowInterval >= (TIME_CHECK_WINDOW>>1) && !pEncCtx.bCheckWindowShiftResetFlag {
		pEncCtx.bCheckWindowShiftResetFlag = true
		for i := int32(0); i < iSpatialNum; i++ {
			iCurDid := pSpatialIndexMap[i].iDid
			if pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[ODD_TIME_WINDOW] > 0 &&
				pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[ODD_TIME_WINDOW] !=
					pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[0] {
				pEncCtx.pWelsSvcRc[iCurDid].bNeedShiftWindowCheck[EVEN_TIME_WINDOW] = true
			} else {
				pEncCtx.pWelsSvcRc[iCurDid].bNeedShiftWindowCheck[EVEN_TIME_WINDOW] = false
			}
			pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[ODD_TIME_WINDOW] = 0
		}
	}
	if pEncCtx.iCheckWindowInterval >= (TIME_CHECK_WINDOW >> 1) {
		pEncCtx.iCheckWindowIntervalShift = pEncCtx.iCheckWindowInterval - (TIME_CHECK_WINDOW >> 1)
	} else {
		pEncCtx.iCheckWindowIntervalShift = pEncCtx.iCheckWindowInterval + (TIME_CHECK_WINDOW >> 1)
	}

	if pEncCtx.iCheckWindowInterval >= TIME_CHECK_WINDOW || pEncCtx.iCheckWindowInterval == 0 {
		pEncCtx.iCheckWindowStartTs = pEncCtx.iCheckWindowCurrentTs
		pEncCtx.iCheckWindowInterval = 0
		pEncCtx.bCheckWindowShiftResetFlag = false
		for i := int32(0); i < iSpatialNum; i++ {
			iCurDid := pSpatialIndexMap[i].iDid
			if pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[EVEN_TIME_WINDOW] > 0 {
				pEncCtx.pWelsSvcRc[iCurDid].bNeedShiftWindowCheck[ODD_TIME_WINDOW] = true
			} else {
				pEncCtx.pWelsSvcRc[iCurDid].bNeedShiftWindowCheck[ODD_TIME_WINDOW] = false
			}
			pEncCtx.pWelsSvcRc[iCurDid].iBufferMaxBRFullness[EVEN_TIME_WINDOW] = 0
		}
	}
}

func WelsRcPostFrameSkipping(pCtx *sWelsEncCtx, iDid int32, uiTimeStamp int64) bool {
	//TODO: put in the decision of rate-control
	return false
}

func WelsRcPostFrameSkippedUpdate(pCtx *sWelsEncCtx, iDid int32) {
	//TODO: do something to update buffers after post-skipping is done
	//let RC know post-skipping happened and adjust strategy accordingly
}

func RcVBufferCalculationPadding(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	kiOutputBits := pWelsSvcRc.iBitsPerFrame
	kiBufferThreshold := common.WELS_DIV_ROUND(PADDING_THRESHOLD*(-pWelsSvcRc.iBufferSizePadding), INT_MULTIPLY)

	pWelsSvcRc.iBufferFullnessPadding += (pWelsSvcRc.iFrameDqBits - kiOutputBits)

	if pWelsSvcRc.iBufferFullnessPadding < kiBufferThreshold {
		pWelsSvcRc.iPaddingSize = -pWelsSvcRc.iBufferFullnessPadding
		pWelsSvcRc.iPaddingSize >>= 3 // /8
		pWelsSvcRc.iBufferFullnessPadding = 0
	} else {
		pWelsSvcRc.iPaddingSize = 0
	}
}

func RcTraceFrameBits(pEncCtx *sWelsEncCtx, uiTimeStamp int64, iFrameSize int32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
	if pWelsSvcRc.iPredFrameBit != 0 {
		// explicit conversions keep the products from being fused
		pWelsSvcRc.iPredFrameBit = int32(float64(LAST_FRAME_PREDICT_WEIGHT*float64(pWelsSvcRc.iFrameDqBits)) +
			float64((1-LAST_FRAME_PREDICT_WEIGHT)*float64(pWelsSvcRc.iPredFrameBit)))
	} else {
		pWelsSvcRc.iPredFrameBit = pWelsSvcRc.iFrameDqBits
	}

	iUsed := iFrameSize << 3
	if pWelsSvcRc.iFrameDqBits > 0 {
		iUsed = pWelsSvcRc.iFrameDqBits
	}
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
		"[Rc]Layer %d: Frame timestamp = %d, Frame type = %d, encoding_qp = %d, average qp = %d, max qp = %d, min qp = %d, index = %d, "+
			"iTid = %d, used = %d, bitsperframe = %d, target = %d, remainingbits = %d, skipbuffersize = %d",
		pEncCtx.uiDependencyId, uiTimeStamp, pEncCtx.eSliceType, pEncCtx.iGlobalQp, pWelsSvcRc.iAverageFrameQp,
		pWelsSvcRc.iMaxFrameQp,
		pWelsSvcRc.iMinFrameQp,
		pParamInternal.iFrameIndex, pEncCtx.uiTemporalId,
		iUsed,
		pWelsSvcRc.iBitsPerFrame,
		pWelsSvcRc.iTargetBits, pWelsSvcRc.iRemainingBits, pWelsSvcRc.iBufferSizeSkip)
}

func RcUpdatePictureQpBits(pEncCtx *sWelsEncCtx, iCodedBits int32) {
	ppSliceInLayer := pEncCtx.pCurDqLayer.ppSliceInLayer
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pCurSliceCtx := &pEncCtx.pCurDqLayer.sSliceEncCtx
	var iTotalQp, iTotalMb int32
	var i int32

	if pEncCtx.eSliceType == common.P_SLICE {
		for i = 0; i < pCurSliceCtx.iSliceNumInFrame; i++ {
			pSOverRc := &ppSliceInLayer[i].sSlicingOverRc
			iTotalQp += pSOverRc.iTotalQpSlice
			iTotalMb += pSOverRc.iTotalMbSlice
		}
		if iTotalMb > 0 {
			pWelsSvcRc.iAverageFrameQp = common.WELS_DIV_ROUND(INT_MULTIPLY*iTotalQp, iTotalMb*INT_MULTIPLY)
		} else {
			pWelsSvcRc.iAverageFrameQp = pEncCtx.iGlobalQp
		}
	} else {
		pWelsSvcRc.iAverageFrameQp = pEncCtx.iGlobalQp
	}
	pWelsSvcRc.iFrameDqBits = iCodedBits
	pWelsSvcRc.iLastCalculatedQScale = pWelsSvcRc.iAverageFrameQp
	pWelsSvcRc.pTemporalOverRc[pEncCtx.uiTemporalId].iGopBitsDq += pWelsSvcRc.iFrameDqBits
}

func RcUpdateIntraComplexity(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	iAlpha := common.WELS_DIV_ROUND(int32(INT_MULTIPLY), (1 + pWelsSvcRc.iIdrNum))
	if iAlpha < (INT_MULTIPLY / 4) {
		iAlpha = INT_MULTIPLY / 4
	}
	_ = iAlpha
	iQStep := RcConvertQp2QStep(pWelsSvcRc.iAverageFrameQp)
	iIntraCmplx := int64(iQStep) * int64(pWelsSvcRc.iFrameDqBits)
	iFrameComplexity := rcFrameComplexity(pEncCtx)
	if pWelsSvcRc.iIdrNum == 0 {
		pWelsSvcRc.iIntraComplexity = iIntraCmplx
		pWelsSvcRc.iIntraComplxMean = iFrameComplexity
	} else {
		pWelsSvcRc.iIntraComplexity = common.WELS_DIV_ROUND64(((LINEAR_MODEL_DECAY_FACTOR)*pWelsSvcRc.iIntraComplexity +
			(INT_MULTIPLY-LINEAR_MODEL_DECAY_FACTOR)*
				iIntraCmplx), INT_MULTIPLY)

		pWelsSvcRc.iIntraComplxMean = common.WELS_DIV_ROUND64(((LINEAR_MODEL_DECAY_FACTOR)*pWelsSvcRc.iIntraComplxMean +
			(INT_MULTIPLY-LINEAR_MODEL_DECAY_FACTOR)*(iFrameComplexity)),
			INT_MULTIPLY)
	}

	pWelsSvcRc.iIntraMbCount = pWelsSvcRc.iNumberMbFrame
	pWelsSvcRc.iIdrNum++
	if pWelsSvcRc.iIdrNum > 255 {
		pWelsSvcRc.iIdrNum = 255
	}
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
		"RcUpdateIntraComplexity iFrameDqBits = %d,iQStep= %d,iIntraCmplx = %d",
		pWelsSvcRc.iFrameDqBits, pWelsSvcRc.iQStep, pWelsSvcRc.iIntraComplexity)
}

func RcUpdateFrameComplexity(pEncCtx *sWelsEncCtx) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	kiTl := int32(pEncCtx.uiTemporalId)
	pTOverRc := &pWelsSvcRc.pTemporalOverRc[kiTl]

	iFrameComplexity := rcFrameComplexity(pEncCtx)
	iQStep := RcConvertQp2QStep(pWelsSvcRc.iAverageFrameQp)
	iAlpha := common.WELS_DIV_ROUND(int32(INT_MULTIPLY), (1 + pTOverRc.iPFrameNum))
	if iAlpha < SMOOTH_FACTOR_MIN_VALUE {
		iAlpha = SMOOTH_FACTOR_MIN_VALUE
	}
	_ = iAlpha
	if 0 == pTOverRc.iPFrameNum {
		pTOverRc.iLinearCmplx = int64(pWelsSvcRc.iFrameDqBits) * int64(iQStep)
		pTOverRc.iFrameCmplxMean = int64(int32(iFrameComplexity))
	} else {
		pTOverRc.iLinearCmplx = common.WELS_DIV_ROUND64(((LINEAR_MODEL_DECAY_FACTOR)*pTOverRc.iLinearCmplx +
			(INT_MULTIPLY-LINEAR_MODEL_DECAY_FACTOR)*(int64(pWelsSvcRc.iFrameDqBits)*int64(iQStep))),
			INT_MULTIPLY)
		pTOverRc.iFrameCmplxMean = common.WELS_DIV_ROUND64(((LINEAR_MODEL_DECAY_FACTOR)*pTOverRc.iFrameCmplxMean +
			(INT_MULTIPLY-LINEAR_MODEL_DECAY_FACTOR)*iFrameComplexity),
			INT_MULTIPLY)
	}

	pTOverRc.iPFrameNum++
	if pTOverRc.iPFrameNum > 255 {
		pTOverRc.iPFrameNum = 255
	}
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
		"RcUpdateFrameComplexity iFrameDqBits = %d,iQStep= %d,pWelsSvcRc->iQStep= %d,pTOverRc->iLinearCmplx = %d",
		pWelsSvcRc.iFrameDqBits,
		iQStep, pWelsSvcRc.iQStep, pTOverRc.iLinearCmplx)
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG, "iFrameCmplxMean = %d,iFrameComplexity = %d",
		pTOverRc.iFrameCmplxMean, iFrameComplexity)
}

func RcCalculateCascadingQp(pEncCtx *sWelsEncCtx, iQp int32) int32 {
	var iTemporalQp int32
	if pEncCtx.pSvcParam.iDecompStages != 0 {
		if pEncCtx.uiTemporalId == 0 {
			iTemporalQp = iQp - 3 - (int32(pEncCtx.pSvcParam.iDecompStages) - 1)
		} else {
			iTemporalQp = iQp - (int32(pEncCtx.pSvcParam.iDecompStages) - int32(pEncCtx.uiTemporalId))
		}
		iTemporalQp = common.WELS_CLIP3(iTemporalQp, 1, 51)
	} else {
		iTemporalQp = iQp
	}
	return iTemporalQp
}

func WelsRcPictureInitGom(pEncCtx *sWelsEncCtx, uiTimeStamp int64) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	kiSliceNum := pEncCtx.pCurDqLayer.iMaxSliceNum
	pWelsSvcRc.iContinualSkipFrames = 0

	if pEncCtx.eSliceType == common.I_SLICE {
		if 0 == pWelsSvcRc.iIdrNum { //iIdrNum == 0 means encoder has been initialed
			RcInitRefreshParameter(pEncCtx)
		}
	}
	if RcJudgeBitrateFpsUpdate(pEncCtx) {
		RcUpdateBitrateFps(pEncCtx)
	}
	if pEncCtx.uiTemporalId == 0 {
		RcUpdateTemporalZero(pEncCtx)
	}
	if pEncCtx.pSvcParam.IRCMode == api.RC_TIMESTAMP_MODE {
		RcDecideTargetBitsTimestamp(pEncCtx)
		pWelsSvcRc.uiLastTimeStamp = uiTimeStamp
	} else {
		RcDecideTargetBits(pEncCtx)
	}
	//turn off GOM QP when slicenum is larger 1
	if (kiSliceNum > 1) || ((pEncCtx.pSvcParam.IRCMode == api.RC_BITRATE_MODE) &&
		(pEncCtx.eSliceType == common.I_SLICE)) {
		pWelsSvcRc.bEnableGomQp = 0 // false
	} else {
		pWelsSvcRc.bEnableGomQp = 1 // true
	}

	//decide globe_qp
	if pEncCtx.eSliceType == common.I_SLICE {
		RcCalculateIdrQp(pEncCtx)
	} else {
		RcCalculatePictureQp(pEncCtx)
	}
	RcInitSliceInformation(pEncCtx)
	RcInitGomParameters(pEncCtx)
}

func WelsRcPictureInfoUpdateGom(pEncCtx *sWelsEncCtx, iLayerSize int32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	iCodedBits := (iLayerSize << 3)

	RcUpdatePictureQpBits(pEncCtx, iCodedBits)

	if pEncCtx.eSliceType == common.P_SLICE {
		RcUpdateFrameComplexity(pEncCtx)
	} else {
		RcUpdateIntraComplexity(pEncCtx)
	}
	pWelsSvcRc.iRemainingBits -= pWelsSvcRc.iFrameDqBits

	if pEncCtx.pSvcParam.BEnableFrameSkip { /*&& pEncCtx->uiDependencyId == pEncCtx->pSvcParam->iSpatialLayerNum - 1*/
		RcVBufferCalculationSkip(pEncCtx)
	}

	if pEncCtx.pSvcParam.IPaddingFlag != 0 {
		RcVBufferCalculationPadding(pEncCtx)
	}
	pWelsSvcRc.iFrameCodedInVGop++
}

func WelsRcMbInitGom(pEncCtx *sWelsEncCtx, pCurMb *SMB, pSlice *SSlice) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pSOverRc := &pSlice.sSlicingOverRc
	pCurLayer := pEncCtx.pCurDqLayer
	kuiChromaQpIndexOffset := pCurLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset

	pSOverRc.iBsPosSlice = pEncCtx.pFuncList.pfGetBsPosition(pSlice)
	if pWelsSvcRc.bEnableGomQp != 0 {
		//calculate gom qp and target bits at the beginning of gom
		if 0 == (pCurMb.iMbXY % pWelsSvcRc.iNumberMbGom) {
			if pCurMb.iMbXY != pSOverRc.iStartMbSlice {
				pSOverRc.iComplexityIndexSlice++
				RcCalculateGomQp(pEncCtx, pSlice, pCurMb)
			}
			RcGomTargetBits(pEncCtx, pSlice)
		}

		RcCalculateMbQp(pEncCtx, pSlice, pCurMb)
	} else {
		pCurMb.uiLumaQp = uint8(pEncCtx.iGlobalQp)
		pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+int32(kuiChromaQpIndexOffset))]
	}
}

func WelsRcMbInfoUpdateGom(pEncCtx *sWelsEncCtx, pCurMb *SMB, iCostLuma int32, pSlice *SSlice) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pSOverRc := &pSlice.sSlicingOverRc
	kiComplexityIndex := pSOverRc.iComplexityIndexSlice

	iCurMbBits := pEncCtx.pFuncList.pfGetBsPosition(pSlice) - pSOverRc.iBsPosSlice
	pSOverRc.iFrameBitsSlice += iCurMbBits
	pSOverRc.iGomBitsSlice += iCurMbBits

	pWelsSvcRc.pGomCost[kiComplexityIndex] += iCostLuma
	if iCurMbBits > 0 {
		pSOverRc.iTotalQpSlice += int32(pCurMb.uiLumaQp)
		pSOverRc.iTotalMbSlice++
	}
}

func WelsRcPictureInitDisable(pEncCtx *sWelsEncCtx, uiTimeStamp int64) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pDLayerParam := &pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId]
	kiQp := pDLayerParam.IDLayerQp

	pEncCtx.iGlobalQp = RcCalculateCascadingQp(pEncCtx, kiQp)

	if pEncCtx.pSvcParam.BEnableAdaptiveQuant && (pEncCtx.eSliceType == common.P_SLICE) {
		pEncCtx.iGlobalQp = common.WELS_CLIP3((pEncCtx.iGlobalQp*INT_MULTIPLY-
			pEncCtx.pVaa.sAdaptiveQuantParam.IAverMotionTextureIndexToDeltaQp)/INT_MULTIPLY, pWelsSvcRc.iMinQp,
			pWelsSvcRc.iMaxQp)
	} else {
		pEncCtx.iGlobalQp = common.WELS_CLIP3(pEncCtx.iGlobalQp, 0, 51)
	}

	pWelsSvcRc.iAverageFrameQp = pEncCtx.iGlobalQp
}

func WelsRcPictureInfoUpdateDisable(pEncCtx *sWelsEncCtx, iLayerSize int32) {
}

func WelsRcMbInitDisable(pEncCtx *sWelsEncCtx, pCurMb *SMB, pSlice *SSlice) {
	iLumaQp := pEncCtx.iGlobalQp
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pCurLayer := pEncCtx.pCurDqLayer

	kuiChromaQpIndexOffset := pCurLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset

	if pEncCtx.pSvcParam.BEnableAdaptiveQuant && (pEncCtx.eSliceType == common.P_SLICE) {
		iLumaQp = int32(int8(common.WELS_CLIP3(iLumaQp+
			int32(pEncCtx.pVaa.sAdaptiveQuantParam.PMotionTextureIndexToDeltaQp[pCurMb.iMbXY]), pWelsSvcRc.iMinQp, pWelsSvcRc.iMaxQp)))
	} else {
		iLumaQp = common.WELS_CLIP3(iLumaQp, 0, 51)
	}
	pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(iLumaQp+int32(kuiChromaQpIndexOffset))]
	pCurMb.uiLumaQp = uint8(iLumaQp)
}

func WelsRcMbInfoUpdateDisable(pEncCtx *sWelsEncCtx, pCurMb *SMB, iCostLuma int32, pSlice *SSlice) {
}

func WelRcPictureInitBufferBasedQp(pEncCtx *sWelsEncCtx, uiTimeStamp int64) {

	pVaa := pEncCtx.pVaa
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]

	iMinQp := pEncCtx.pSvcParam.IMinQp
	if pVaa.eSceneChangeIdc == processing.LARGE_CHANGED_SCENE {
		iMinQp += 2
	} else if pVaa.eSceneChangeIdc == processing.MEDIUM_CHANGED_SCENE {
		iMinQp += 1
	}
	if pEncCtx.bDeliveryFlag {
		pEncCtx.iGlobalQp -= 1
	} else {
		pEncCtx.iGlobalQp += 2
	}
	pEncCtx.iGlobalQp = common.WELS_CLIP3(pEncCtx.iGlobalQp, iMinQp, pWelsSvcRc.iMaxQp)
	pWelsSvcRc.iMinFrameQp = pEncCtx.iGlobalQp
	pWelsSvcRc.iMaxFrameQp = pWelsSvcRc.iMinFrameQp
	pWelsSvcRc.iAverageFrameQp = pWelsSvcRc.iMaxFrameQp
}

func WelRcPictureInitScc(pEncCtx *sWelsEncCtx, uiTimeStamp int64) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pVaa := pEncCtx.pVaa.pExt
	pDLayerConfig := &pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId]
	pDLayerParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
	iFrameCplx := pVaa.sComplexityScreenParam.IFrameComplexity
	iBitRate := pDLayerConfig.ISpatialBitrate // pEncCtx->pSvcParam->target_bitrate;

	iBaseQp := pWelsSvcRc.iBaseQp
	pEncCtx.iGlobalQp = iBaseQp
	var iDeltaQp int32
	if pEncCtx.eSliceType == common.I_SLICE {
		iTargetBits := int64(iBitRate*2) - pWelsSvcRc.iBufferFullnessSkip
		iTargetBits = common.WELS_MAX(1, iTargetBits)
		iQstep := common.WELS_DIV_ROUND(iFrameCplx*pWelsSvcRc.iCost2BitsIntra, iTargetBits)
		iQp := RcConvertQStep2Qp(iQstep)

		pEncCtx.iGlobalQp = common.WELS_CLIP3(iQp, pWelsSvcRc.iMinQp, pWelsSvcRc.iMaxQp)
	} else {
		iTargetBits := int64(common.WELS_ROUND(float32(iBitRate) / pDLayerParamInternal.fOutputFrameRate)) //iBitRate / 10;
		iQstep := common.WELS_DIV_ROUND(iFrameCplx*pWelsSvcRc.iAvgCost2Bits, iTargetBits)
		iQp := RcConvertQStep2Qp(iQstep)
		iDeltaQp = iQp - iBaseQp
		if pWelsSvcRc.iBufferFullnessSkip > int64(iBitRate) {
			if iDeltaQp > 0 {
				iBaseQp++
			}
		} else if pWelsSvcRc.iBufferFullnessSkip == 0 {
			if iDeltaQp < 0 {
				iBaseQp--
			}
		}
		if iDeltaQp >= 6 {
			iBaseQp += 3
		} else if iDeltaQp <= -6 {
			iBaseQp--
		}
		iBaseQp = common.WELS_CLIP3(iBaseQp, pWelsSvcRc.iMinQp, pWelsSvcRc.iMinQp)

		pEncCtx.iGlobalQp = iBaseQp

		if iDeltaQp < -6 {
			pEncCtx.iGlobalQp = common.WELS_CLIP3(pWelsSvcRc.iBaseQp-6, pWelsSvcRc.iMinQp, pWelsSvcRc.iMaxQp)
		}

		if iDeltaQp > 5 {
			if processing.LARGE_CHANGED_SCENE == pEncCtx.pVaa.eSceneChangeIdc || pWelsSvcRc.iBufferFullnessSkip > int64(2*iBitRate) ||
				iDeltaQp > 10 {
				pEncCtx.iGlobalQp = common.WELS_CLIP3(pWelsSvcRc.iBaseQp+iDeltaQp, pWelsSvcRc.iMinQp, pWelsSvcRc.iMaxQp)
			} else if processing.MEDIUM_CHANGED_SCENE == pEncCtx.pVaa.eSceneChangeIdc || pWelsSvcRc.iBufferFullnessSkip > int64(iBitRate) {
				pEncCtx.iGlobalQp = common.WELS_CLIP3(pWelsSvcRc.iBaseQp+5, pWelsSvcRc.iMinQp, pWelsSvcRc.iMaxQp)
			}
		}
		pWelsSvcRc.iBaseQp = iBaseQp
	}
	pWelsSvcRc.iAverageFrameQp = pEncCtx.iGlobalQp
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG, "WelRcPictureInitScc iLumaQp = %d\n", pEncCtx.iGlobalQp)
	pWelsSvcRc.uiLastTimeStamp = uiTimeStamp
}

func WelsRcDropFrameUpdate(pEncCtx *sWelsEncCtx, iDropSize uint32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[0]

	pWelsSvcRc.iBufferFullnessSkip -= int64(int32(iDropSize))
	pWelsSvcRc.iBufferFullnessSkip = common.WELS_MAX(0, pWelsSvcRc.iBufferFullnessSkip)
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG, "[WelsRcDropFrameUpdate:\tdrop:%d\t%d\n", iDropSize,
		pWelsSvcRc.iBufferFullnessSkip)
}

func WelsRcPictureInfoUpdateScc(pEncCtx *sWelsEncCtx, iNalSize int32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	iFrameBits := (iNalSize << 3)
	pWelsSvcRc.iBufferFullnessSkip += int64(iFrameBits)

	pVaa := pEncCtx.pVaa.pExt

	iQstep := RcConvertQp2QStep(pEncCtx.iGlobalQp)
	iCost2Bits := common.WELS_DIV_ROUND64(int64(iFrameBits)*int64(iQstep), pVaa.sComplexityScreenParam.IFrameComplexity)

	if pEncCtx.eSliceType == common.P_SLICE {
		pWelsSvcRc.iAvgCost2Bits = common.WELS_DIV_ROUND64((95*pWelsSvcRc.iAvgCost2Bits + 5*iCost2Bits), INT_MULTIPLY)
	} else {
		pWelsSvcRc.iCost2BitsIntra = common.WELS_DIV_ROUND64((90*pWelsSvcRc.iCost2BitsIntra + 10*iCost2Bits), INT_MULTIPLY)
	}
}

func WelsRcMbInitScc(pEncCtx *sWelsEncCtx, pCurMb *SMB, pSlice *SSlice) {
	/* Get delta iQp of this MB */
	pCurMb.uiLumaQp = uint8(pEncCtx.iGlobalQp)
	pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.WELS_CLIP3(int32(pCurMb.uiLumaQp)+int32(pEncCtx.pPps.uiChromaQpIndexOffset), 0, 51)]
}

func WelsRcFrameDelayJudgeTimeStamp(pEncCtx *sWelsEncCtx, uiTimeStamp int64, iDidIdx int32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[iDidIdx]
	pDLayerConfig := &pEncCtx.pSvcParam.SSpatialLayers[iDidIdx]

	iBitRate := pDLayerConfig.ISpatialBitrate
	var iEncTimeInv int32
	if pWelsSvcRc.uiLastTimeStamp != 0 {
		iEncTimeInv = int32(uiTimeStamp - pWelsSvcRc.uiLastTimeStamp)
	}
	if (iEncTimeInv < 0) || (iEncTimeInv > 1000) {
		iEncTimeInv = int32(1000.0 / float64(pDLayerConfig.FFrameRate))
		pWelsSvcRc.uiLastTimeStamp = uiTimeStamp - int64(iEncTimeInv)
	}
	// explicit conversion keeps the product from being fused with + 0.5
	iSentBits := int32(float64(float64(iBitRate)*float64(iEncTimeInv)*(1.0e-3)) + 0.5)
	iSentBits = common.WELS_MAX(iSentBits, 0)

	//When bitrate is changed, pBuffer size should be updated
	pWelsSvcRc.iBufferSizeSkip = common.WELS_DIV_ROUND(pDLayerConfig.ISpatialBitrate*pWelsSvcRc.iSkipBufferRatio,
		INT_MULTIPLY)
	pWelsSvcRc.iBufferSizePadding = common.WELS_DIV_ROUND(pDLayerConfig.ISpatialBitrate*PADDING_BUFFER_RATIO, INT_MULTIPLY)

	pWelsSvcRc.iBufferFullnessSkip -= int64(iSentBits)
	pWelsSvcRc.iBufferFullnessSkip = common.WELS_MAX(int64((-1)*(pDLayerConfig.ISpatialBitrate/4)),
		pWelsSvcRc.iBufferFullnessSkip)

	if pEncCtx.pSvcParam.BEnableFrameSkip {
		pWelsSvcRc.bSkipFlag = true
		if pWelsSvcRc.iBufferFullnessSkip < int64(pWelsSvcRc.iBufferSizeSkip) {
			pWelsSvcRc.bSkipFlag = false
		}
		if pWelsSvcRc.bSkipFlag {
			pWelsSvcRc.iSkipFrameNum++
			pWelsSvcRc.uiLastTimeStamp = uiTimeStamp
		}
	}
	common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DEBUG,
		"WelsRcFrameDelayJudgeTimeStamp iDidIdx = %d,iSkipFrameNum = %d,buffer = %d"+
			",threadhold = %d,bitrate = %d,iSentBits = %d,lasttimestamp = %d,timestamp=%d", iDidIdx,
		pWelsSvcRc.iSkipFrameNum, pWelsSvcRc.iBufferFullnessSkip, pWelsSvcRc.iBufferSizeSkip, iBitRate, iSentBits,
		pWelsSvcRc.uiLastTimeStamp, uiTimeStamp)
}

func WelsRcPictureInfoUpdateGomTimeStamp(pEncCtx *sWelsEncCtx, iLayerSize int32) {
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	iCodedBits := (iLayerSize << 3)

	RcUpdatePictureQpBits(pEncCtx, iCodedBits)
	if pEncCtx.eSliceType == common.P_SLICE {
		RcUpdateFrameComplexity(pEncCtx)
	} else {
		RcUpdateIntraComplexity(pEncCtx)
	}

	pWelsSvcRc.iRemainingBits -= pWelsSvcRc.iFrameDqBits
	//condition 1: whole pBuffer fullness
	pWelsSvcRc.iBufferFullnessSkip += int64(pWelsSvcRc.iFrameDqBits)

	if pEncCtx.pSvcParam.IPaddingFlag != 0 {
		RcVBufferCalculationPadding(pEncCtx)
	}
	pWelsSvcRc.iFrameCodedInVGop++
}

func WelsRcInitFuncPointers(pEncCtx *sWelsEncCtx, iRcMode api.RC_MODES) {
	pRcf := &pEncCtx.pFuncList.pfRc
	switch iRcMode {
	case api.RC_OFF_MODE:
		pRcf.pfWelsRcPictureInit = WelsRcPictureInitDisable
		pRcf.pfWelsRcPicDelayJudge = nil
		pRcf.pfWelsRcPictureInfoUpdate = WelsRcPictureInfoUpdateDisable
		pRcf.pfWelsRcMbInit = WelsRcMbInitDisable
		pRcf.pfWelsRcMbInfoUpdate = WelsRcMbInfoUpdateDisable
		pRcf.pfWelsCheckSkipBasedMaxbr = nil
		pRcf.pfWelsUpdateBufferWhenSkip = nil
		pRcf.pfWelsUpdateMaxBrWindowStatus = nil
		pRcf.pfWelsRcPostFrameSkipping = nil
	case api.RC_BUFFERBASED_MODE:
		pRcf.pfWelsRcPictureInit = WelRcPictureInitBufferBasedQp
		pRcf.pfWelsRcPicDelayJudge = nil
		pRcf.pfWelsRcPictureInfoUpdate = WelsRcPictureInfoUpdateDisable
		pRcf.pfWelsRcMbInit = WelsRcMbInitDisable
		pRcf.pfWelsRcMbInfoUpdate = WelsRcMbInfoUpdateDisable
		pRcf.pfWelsCheckSkipBasedMaxbr = nil
		pRcf.pfWelsUpdateBufferWhenSkip = nil
		pRcf.pfWelsUpdateMaxBrWindowStatus = nil
		pRcf.pfWelsRcPostFrameSkipping = nil
	case api.RC_BITRATE_MODE:
		pRcf.pfWelsRcPictureInit = WelsRcPictureInitGom
		pRcf.pfWelsRcPicDelayJudge = nil
		pRcf.pfWelsRcPictureInfoUpdate = WelsRcPictureInfoUpdateGom
		pRcf.pfWelsRcMbInit = WelsRcMbInitGom
		pRcf.pfWelsRcMbInfoUpdate = WelsRcMbInfoUpdateGom
		pRcf.pfWelsCheckSkipBasedMaxbr = CheckFrameSkipBasedMaxbr
		pRcf.pfWelsUpdateBufferWhenSkip = UpdateBufferWhenFrameSkipped
		pRcf.pfWelsUpdateMaxBrWindowStatus = UpdateMaxBrCheckWindowStatus
		pRcf.pfWelsRcPostFrameSkipping = WelsRcPostFrameSkipping
	case api.RC_BITRATE_MODE_POST_SKIP:
		pRcf.pfWelsRcPictureInit = WelsRcPictureInitGom
		pRcf.pfWelsRcPicDelayJudge = nil
		pRcf.pfWelsRcPictureInfoUpdate = WelsRcPictureInfoUpdateGom
		pRcf.pfWelsRcMbInit = WelsRcMbInitGom
		pRcf.pfWelsRcMbInfoUpdate = WelsRcMbInfoUpdateGom
		pRcf.pfWelsCheckSkipBasedMaxbr = CheckFrameSkipBasedMaxbr
		pRcf.pfWelsUpdateBufferWhenSkip = UpdateBufferWhenFrameSkipped
		pRcf.pfWelsUpdateMaxBrWindowStatus = UpdateMaxBrCheckWindowStatus
		pRcf.pfWelsRcPostFrameSkipping = WelsRcPostFrameSkipping
	case api.RC_TIMESTAMP_MODE:

		pRcf.pfWelsRcPictureInit = WelsRcPictureInitGom
		pRcf.pfWelsRcPictureInfoUpdate = WelsRcPictureInfoUpdateGomTimeStamp
		pRcf.pfWelsRcMbInit = WelsRcMbInitGom
		pRcf.pfWelsRcMbInfoUpdate = WelsRcMbInfoUpdateGom

		pRcf.pfWelsRcPicDelayJudge = WelsRcFrameDelayJudgeTimeStamp
		pRcf.pfWelsCheckSkipBasedMaxbr = nil
		pRcf.pfWelsUpdateBufferWhenSkip = nil
		pRcf.pfWelsUpdateMaxBrWindowStatus = nil
		pRcf.pfWelsRcPostFrameSkipping = nil
	default: // RC_QUALITY_MODE
		pRcf.pfWelsRcPictureInit = WelsRcPictureInitGom
		pRcf.pfWelsRcPicDelayJudge = nil
		pRcf.pfWelsRcPictureInfoUpdate = WelsRcPictureInfoUpdateGom
		pRcf.pfWelsRcMbInit = WelsRcMbInitGom
		pRcf.pfWelsRcMbInfoUpdate = WelsRcMbInfoUpdateGom
		pRcf.pfWelsCheckSkipBasedMaxbr = CheckFrameSkipBasedMaxbr
		pRcf.pfWelsUpdateBufferWhenSkip = UpdateBufferWhenFrameSkipped
		pRcf.pfWelsUpdateMaxBrWindowStatus = UpdateMaxBrCheckWindowStatus
		pRcf.pfWelsRcPostFrameSkipping = nil
	}
}

func WelsRcInitModule(pEncCtx *sWelsEncCtx, iRcMode api.RC_MODES) {
	WelsRcInitFuncPointers(pEncCtx, iRcMode)
	RcInitSequenceParameter(pEncCtx)
}

func WelsRcFreeMemory(pEncCtx *sWelsEncCtx) {
	for i := int32(0); i < pEncCtx.pSvcParam.ISpatialLayerNum; i++ {
		pWelsSvcRc := &pEncCtx.pWelsSvcRc[i]
		RcFreeLayerMemory(pWelsSvcRc)
	}
}

func GetTimestampForRc(uiTimeStamp int64, uiLastTimeStamp int64, fFrameRate float32) int64 {
	if (uiLastTimeStamp >= uiTimeStamp) || ((uiTimeStamp == 0) && (uiLastTimeStamp != -1)) {
		return (uiLastTimeStamp + int64(int32(1000.0/float64(fFrameRate))))
	}
	return uiTimeStamp
}
