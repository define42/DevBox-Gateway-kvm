// Port of codec/encoder/core/src/svc_motion_estimate.cpp.
//
// The X86_ASM-only helpers (CalcMvdCostx8_c, VerticalFullSearchUsingSSE41,
// HorizontalFullSearchUsingSSE41) are not ported.

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var QStepx16ByQp = [52]int32{ /* save QStep<<4 for int32_t */
	10, 11, 13, 14, 16, 18, /* 0~5   */
	20, 22, 26, 28, 32, 36, /* 6~11  */
	40, 44, 52, 56, 64, 72, /* 12~17 */
	80, 88, 104, 112, 128, 144, /* 18~23 */
	160, 176, 208, 224, 256, 288, /* 24~29 */
	320, 352, 416, 448, 512, 576, /* 30~35 */
	640, 704, 832, 896, 1024, 1152, /* 36~41 */
	1280, 1408, 1664, 1792, 2048, 2304, /* 42~47 */
	2560, 2816, 3328, 3584, /* 48~51 */
}

// pRef: pixel pointer -> (slice, offset).
func UpdateMeResults(ksBestMv SMVUnitXY, kiBestSadCost uint32, pRef []uint8, iRefOff int, pMe *SWelsME) {
	pMe.sMv = ksBestMv
	pMe.pRefMb = pRef
	pMe.iRefMbOff = iRefOff
	pMe.uiSadCost = kiBestSadCost
}

func MeEndIntepelSearch(pMe *SWelsME) {
	/* -> qpel mv */
	pMe.sMv.iMvX *= (1 << 2)
	pMe.sMv.iMvY *= (1 << 2)
	pMe.uiSatdCost = pMe.uiSadCost
}

// (QStepx16ByQp is defined in this file.)
func WelsInitMeFunc(pFuncList *SWelsFuncPtrList, uiCpuFlag uint32, bScreenContent bool) {
	pFuncList.pfUpdateFMESwitch = UpdateFMESwitchNull

	if !bScreenContent {
		pFuncList.pfCheckDirectionalMv = CheckDirectionalMvFalse
		pFuncList.pfCalculateBlockFeatureOfFrame[0] = nil
		pFuncList.pfCalculateBlockFeatureOfFrame[1] = nil
		pFuncList.pfCalculateSingleBlockFeature[0] = nil
		pFuncList.pfCalculateSingleBlockFeature[1] = nil
	} else {
		pFuncList.pfCheckDirectionalMv = CheckDirectionalMv

		//for cross serarch
		pFuncList.pfVerticalFullSearch = LineFullSearch_c
		pFuncList.pfHorizontalFullSearch = LineFullSearch_c

		//for feature search
		pFuncList.pfInitializeHashforFeature = InitializeHashforFeature_c
		pFuncList.pfFillQpelLocationByFeatureValue = FillQpelLocationByFeatureValue_c
		pFuncList.pfCalculateBlockFeatureOfFrame[0] = SumOf8x8BlockOfFrame_c
		pFuncList.pfCalculateBlockFeatureOfFrame[1] = SumOf16x16BlockOfFrame_c
		//TODO: it is possible to differentiate width that is times of 8, so as to accelerate the speed when width is times of 8?
		pFuncList.pfCalculateSingleBlockFeature[0] = SumOf8x8SingleBlock_c
		pFuncList.pfCalculateSingleBlockFeature[1] = SumOf16x16SingleBlock_c
	}
}

/*!
 * \brief  BL mb motion estimate search
 */
func WelsMotionEstimateSearch(pFuncList *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pMe *SWelsME, pSlice *SSlice) {
	kiStrideEnc := pCurDqLayer.iEncStride[0]
	kiStrideRef := pCurDqLayer.pRefPic.iLineSize[0]

	//  Step 1: Initial point prediction
	if !WelsMotionEstimateInitialPoint(pFuncList, pMe, pSlice, kiStrideEnc, kiStrideRef) {
		pFuncList.pfSearchMethod[pMe.uiBlockSize](pFuncList, pMe, pSlice, kiStrideEnc, kiStrideRef)
		MeEndIntepelSearch(pMe)
	}

	pFuncList.pfCalculateSatd(pFuncList.sSampleDealingFuncs.pfSampleSatd[pMe.uiBlockSize], pMe, kiStrideEnc,
		kiStrideRef)
}

func WelsMotionEstimateSearchStatic(pFuncList *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pMe *SWelsME, pLpslice *SSlice) {
	kiStrideEnc := pCurDqLayer.iEncStride[0]
	kiStrideRef := pCurDqLayer.pRefPic.iLineSize[0]

	pMe.sMv.iMvX = 0
	pMe.sMv.iMvY = 0
	pMe.uiSadCost = uint32(pFuncList.sSampleDealingFuncs.pfSampleSad[pMe.uiBlockSize](pMe.pEncMb, pMe.iEncMbOff, kiStrideEnc,
		pMe.pRefMb, pMe.iRefMbOff, kiStrideRef))
	pMe.uiSadCost += uint32(COST_MVD(pMe.pMvdCost, pMe.iMvdCostOff, -int32(pMe.sMvp.iMvX), -int32(pMe.sMvp.iMvY)))
	MeEndIntepelSearch(pMe)
	pFuncList.pfCalculateSatd(pFuncList.sSampleDealingFuncs.pfSampleSatd[pMe.uiBlockSize], pMe, kiStrideEnc,
		kiStrideRef)
}

func WelsMotionEstimateSearchScrolled(pFuncList *SWelsFuncPtrList, pCurDqLayer *SDqLayer, pMe *SWelsME, pSlice *SSlice) {
	kiStrideEnc := pCurDqLayer.iEncStride[0]
	kiStrideRef := pCurDqLayer.pRefPic.iLineSize[0]

	pMe.sMv = pMe.sDirectionalMv
	pMe.pRefMb = pMe.pColoRefMb
	pMe.iRefMbOff = pMe.iColoRefMbOff + int(int32(pMe.sMv.iMvY)*kiStrideRef+int32(pMe.sMv.iMvX))
	pMe.uiSadCost = uint32(pFuncList.sSampleDealingFuncs.pfSampleSad[pMe.uiBlockSize](pMe.pEncMb, pMe.iEncMbOff, kiStrideEnc,
		pMe.pRefMb, pMe.iRefMbOff, kiStrideRef) +
		COST_MVD(pMe.pMvdCost, pMe.iMvdCostOff, (int32(pMe.sMv.iMvX)*(1<<2))-int32(pMe.sMvp.iMvX),
			(int32(pMe.sMv.iMvY)*(1<<2))-int32(pMe.sMvp.iMvY)))
	MeEndIntepelSearch(pMe)
	pFuncList.pfCalculateSatd(pFuncList.sSampleDealingFuncs.pfSampleSatd[pMe.uiBlockSize], pMe, kiStrideEnc,
		kiStrideRef)
}

/*!
 * \brief  EL mb motion estimate initial point testing
 */
func WelsMotionEstimateInitialPoint(pFuncList *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, iStrideEnc int32, iStrideRef int32) bool {
	pSad := pFuncList.sSampleDealingFuncs.pfSampleSad[pMe.uiBlockSize]
	kpMvdCost, kiMvdCostOff := pMe.pMvdCost, pMe.iMvdCostOff
	kpEncMb, kiEncMbOff := pMe.pEncMb, pMe.iEncMbOff
	var iMvc0, iMvc1 int16
	var iSadCost int32
	var iBestSadCost int32
	var pRefMb []uint8
	var iRefMbOff int
	kuiMvcNum := uint32(pSlice.uiMvcNum)
	kpMvcList := pSlice.sMvc[:]
	ksMvStartMin := pSlice.sMvStartMin
	ksMvStartMax := pSlice.sMvStartMax
	ksMvp := pMe.sMvp
	var sMv SMVUnitXY

	//  Step 1: Initial point prediction
	// init with sMvp
	sMv.iMvX = int16(common.WELS_CLIP3((2+int32(ksMvp.iMvX))>>2, int32(ksMvStartMin.iMvX), int32(ksMvStartMax.iMvX)))
	sMv.iMvY = int16(common.WELS_CLIP3((2+int32(ksMvp.iMvY))>>2, int32(ksMvStartMin.iMvY), int32(ksMvStartMax.iMvY)))

	pRefMb = pMe.pRefMb
	iRefMbOff = pMe.iRefMbOff + int(int32(sMv.iMvY)*iStrideRef+int32(sMv.iMvX))

	iBestSadCost = pSad(kpEncMb, kiEncMbOff, iStrideEnc, pRefMb, iRefMbOff, iStrideRef)
	iBestSadCost += COST_MVD(kpMvdCost, kiMvdCostOff, (int32(sMv.iMvX)*(1<<2))-int32(ksMvp.iMvX),
		(int32(sMv.iMvY)*(1<<2))-int32(ksMvp.iMvY))

	for i := uint32(0); i < kuiMvcNum; i++ {
		//clipping here is essential since some pOut-of-range MVC may happen here (i.e., refer to baseMV)
		iMvc0 = int16(common.WELS_CLIP3((2+int32(kpMvcList[i].iMvX))>>2, int32(ksMvStartMin.iMvX), int32(ksMvStartMax.iMvX)))
		iMvc1 = int16(common.WELS_CLIP3((2+int32(kpMvcList[i].iMvY))>>2, int32(ksMvStartMin.iMvY), int32(ksMvStartMax.iMvY)))

		if (int32(iMvc0)-int32(sMv.iMvX)) != 0 || (int32(iMvc1)-int32(sMv.iMvY)) != 0 {
			iFref2Off := pMe.iRefMbOff + int(int32(iMvc1)*iStrideRef+int32(iMvc0))

			iSadCost = pSad(kpEncMb, kiEncMbOff, iStrideEnc, pMe.pRefMb, iFref2Off, iStrideRef) +
				COST_MVD(kpMvdCost, kiMvdCostOff, (int32(iMvc0)*(1<<2))-int32(ksMvp.iMvX), (int32(iMvc1)*(1<<2))-int32(ksMvp.iMvY))

			if iSadCost < iBestSadCost {
				sMv.iMvX = iMvc0
				sMv.iMvY = iMvc1
				pRefMb = pMe.pRefMb
				iRefMbOff = iFref2Off
				iBestSadCost = iSadCost
			}
		}
	}

	if pFuncList.pfCheckDirectionalMv(pSad, pMe, ksMvStartMin, ksMvStartMax, iStrideEnc, iStrideRef, &pMe.iDirectionalSadCost) {
		sMv = pMe.sDirectionalMv
		pRefMb = pMe.pColoRefMb
		iRefMbOff = pMe.iColoRefMbOff + int(int32(sMv.iMvY)*iStrideRef+int32(sMv.iMvX))
		iBestSadCost = pMe.iDirectionalSadCost
	}

	UpdateMeResults(sMv, uint32(iBestSadCost), pRefMb, iRefMbOff, pMe)
	if iBestSadCost < int32(pMe.uSadPredISatd) {
		//Initial point early Stop
		MeEndIntepelSearch(pMe)
		return true
	}
	return false
}

func CalculateSatdCost(pSatd PSampleSadSatdCostFunc, pMe *SWelsME, kiEncStride int32, kiRefStride int32) {
	pMe.uSadPredISatd = uint32(pSatd(pMe.pEncMb, pMe.iEncMbOff, kiEncStride, pMe.pRefMb, pMe.iRefMbOff, kiRefStride))
	pMe.uiSatdCost = pMe.uSadPredISatd + uint32(COST_MVD(pMe.pMvdCost, pMe.iMvdCostOff,
		int32(pMe.sMv.iMvX)-int32(pMe.sMvp.iMvX), int32(pMe.sMv.iMvY)-int32(pMe.sMvp.iMvY)))
}

func NotCalculateSatdCost(pSatd PSampleSadSatdCostFunc, pMe *SWelsME, kiEncStride int32, kiRefStride int32) {
}

/////////////////////////
// Diamond Search Basics
/////////////////////////

// iSadCost: int32_t[4]; kpMvdCost: negatively indexed MVD cost table -> (slice, offset).
func WelsMeSadCostSelect(iSadCost []int32, kpMvdCost []uint16, iMvdCostOff int, pBestCost *int32, kiDx int32, kiDy int32, pIx *int32, pIy *int32) bool {
	var iTempSadCost [4]int32
	iInputSadCost := *pBestCost
	iTempSadCost[0] = iSadCost[0] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx, kiDy-4)
	iTempSadCost[1] = iSadCost[1] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx, kiDy+4)
	iTempSadCost[2] = iSadCost[2] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx-4, kiDy)
	iTempSadCost[3] = iSadCost[3] + COST_MVD(kpMvdCost, iMvdCostOff, kiDx+4, kiDy)

	if iTempSadCost[0] < *pBestCost {
		*pBestCost = iTempSadCost[0]
		*pIx = 0
		*pIy = 1
	}

	if iTempSadCost[1] < *pBestCost {
		*pBestCost = iTempSadCost[1]
		*pIx = 0
		*pIy = -1
	}

	if iTempSadCost[2] < *pBestCost {
		*pBestCost = iTempSadCost[2]
		*pIx = 1
		*pIy = 0
	}

	if iTempSadCost[3] < *pBestCost {
		*pBestCost = iTempSadCost[3]
		*pIx = -1
		*pIy = 0
	}
	return *pBestCost == iInputSadCost
}

func WelsDiamondSearch(pFuncList *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, kiStrideEnc int32, kiStrideRef int32) {
	pSad := pFuncList.sSampleDealingFuncs.pfSample4Sad[pMe.uiBlockSize]

	kpEncMb, kiEncMbOff := pMe.pEncMb, pMe.iEncMbOff
	kpMvdCost, kiMvdCostOff := pMe.pMvdCost, pMe.iMvdCostOff

	ksMvStartMin := pSlice.sMvStartMin
	ksMvStartMax := pSlice.sMvStartMax

	iMvDx := (int32(pMe.sMv.iMvX) * (1 << 2)) - int32(pMe.sMvp.iMvX)
	iMvDy := (int32(pMe.sMv.iMvY) * (1 << 2)) - int32(pMe.sMvp.iMvY)

	pRefMb, iRefMbOff := pMe.pRefMb, pMe.iRefMbOff
	iBestCost := int32(pMe.uiSadCost)

	iTimeThreshold := int32(ITERATIVE_TIMES)
	iSadCosts := pSlice.sMbCacheInfo.iDiamondSad[:]

	for iTimeThreshold != 0 {
		iTimeThreshold--
		pMe.sMv.iMvX = int16((iMvDx + int32(pMe.sMvp.iMvX)) >> 2)
		pMe.sMv.iMvY = int16((iMvDy + int32(pMe.sMvp.iMvY)) >> 2)
		if !CheckMvInRange(pMe.sMv, ksMvStartMin, ksMvStartMax) {
			continue
		}
		pSad(kpEncMb, kiEncMbOff, kiStrideEnc, pRefMb, iRefMbOff, kiStrideRef, iSadCosts)

		var iX, iY int32

		kbIsBestCostWorse := WelsMeSadCostSelect(iSadCosts, kpMvdCost, kiMvdCostOff, &iBestCost, iMvDx, iMvDy, &iX, &iY)
		if kbIsBestCostWorse {
			break
		}

		iMvDx -= iX * (1 << 2)
		iMvDy -= iY * (1 << 2)

		iRefMbOff -= int(iX + iY*kiStrideRef)
	}

	/* integer-pel mv */
	pMe.sMv.iMvX = int16((iMvDx + int32(pMe.sMvp.iMvX)) >> 2)
	pMe.sMv.iMvY = int16((iMvDy + int32(pMe.sMvp.iMvY)) >> 2)
	pMe.uiSadCost = uint32(iBestCost)
	pMe.uiSatdCost = pMe.uiSadCost
	pMe.pRefMb = pRefMb
	pMe.iRefMbOff = iRefMbOff
}

/////////////////////////
// DirectionalMv Basics
/////////////////////////

// int32_t& iBestSadCost -> *int32.
func CheckDirectionalMv(pSad PSampleSadSatdCostFunc, pMe *SWelsME, ksMinMv SMVUnitXY, ksMaxMv SMVUnitXY, kiEncStride int32, kiRefStride int32, iBestSadCost *int32) bool {
	kiMvX := pMe.sDirectionalMv.iMvX
	kiMvY := pMe.sDirectionalMv.iMvY

	//Check MV from scrolling detection
	if (BLOCK_16x16 != pMe.uiBlockSize) && //scrolled_MV with P16x16 is checked SKIP checking function
		(kiMvX|kiMvY) != 0 && //(0,0) checked in ordinary initial point checking
		CheckMvInRange(pMe.sDirectionalMv, ksMinMv, ksMaxMv) {
		iRefOff := pMe.iColoRefMbOff + int(int32(kiMvY)*kiRefStride+int32(kiMvX))
		uiCurrentSadCost := uint32(pSad(pMe.pEncMb, pMe.iEncMbOff, kiEncStride, pMe.pColoRefMb, iRefOff, kiRefStride) +
			COST_MVD(pMe.pMvdCost, pMe.iMvdCostOff, (int32(kiMvX)*(1<<2))-int32(pMe.sMvp.iMvX), (int32(kiMvY)*(1<<2))-int32(pMe.sMvp.iMvY)))
		if uiCurrentSadCost < pMe.uiSadCost {
			*iBestSadCost = int32(uiCurrentSadCost)
			return true
		}
	}
	return false
}

func CheckDirectionalMvFalse(pSad PSampleSadSatdCostFunc, vpMe *SWelsME, ksMinMv SMVUnitXY, ksMaxMv SMVUnitXY, kiEncStride int32, kiRefStride int32, iBestSadCost *int32) bool {
	return false
}

/////////////////////////
// Cross Search Basics
/////////////////////////

// pMvdTable: negatively indexed MVD cost table -> (slice, offset).
func LineFullSearch_c(pFuncList *SWelsFuncPtrList, pMe *SWelsME, pMvdTable []uint16, iMvdTableOff int, kiEncStride int32, kiRefStride int32, iMinMv int16, iMaxMv int16, bVerticalSearch bool) {
	pSad := pFuncList.sSampleDealingFuncs.pfSampleSad[pMe.uiBlockSize]
	kiCurMeBlockPixX := pMe.iCurMeBlockPixX
	kiCurMeBlockPixY := pMe.iCurMeBlockPixY
	var iMinPos, iMaxPos int32
	var iFixedMvd int32
	var iCurMeBlockPix int32
	var iStride int32
	var iMvdCostIdx int

	if bVerticalSearch {
		iMinPos = kiCurMeBlockPixY + int32(iMinMv)
		iMaxPos = kiCurMeBlockPixY + int32(iMaxMv)
		iFixedMvd = int32(pMvdTable[iMvdTableOff-int(pMe.sMvp.iMvX)])
		iCurMeBlockPix = pMe.iCurMeBlockPixY
		iStride = kiRefStride
		iMvdCostIdx = iMvdTableOff + int((int32(iMinMv)*(1<<2))-int32(pMe.sMvp.iMvY))
	} else {
		iMinPos = kiCurMeBlockPixX + int32(iMinMv)
		iMaxPos = kiCurMeBlockPixX + int32(iMaxMv)
		iFixedMvd = int32(pMvdTable[iMvdTableOff-int(pMe.sMvp.iMvY)])
		iCurMeBlockPix = pMe.iCurMeBlockPixX
		iStride = 1
		iMvdCostIdx = iMvdTableOff + int((int32(iMinMv)*(1<<2))-int32(pMe.sMvp.iMvX))
	}
	iRefOff := pMe.iColoRefMbOff + int(int32(iMinMv)*iStride)
	uiBestCost := uint32(0xFFFFFFFF)
	var iBestPos int32

	for iTargetPos := iMinPos; iTargetPos < iMaxPos; iTargetPos++ {
		uiSadCost := uint32(pSad(pMe.pEncMb, pMe.iEncMbOff, kiEncStride, pMe.pColoRefMb, iRefOff, kiRefStride) +
			(iFixedMvd + int32(pMvdTable[iMvdCostIdx])))
		if uiSadCost < uiBestCost {
			uiBestCost = uiSadCost
			iBestPos = iTargetPos
		}
		iRefOff += int(iStride)
		iMvdCostIdx += 4
	}

	if uiBestCost < pMe.uiSadCost {
		var sBestMv SMVUnitXY
		if bVerticalSearch {
			sBestMv.iMvX = 0
			sBestMv.iMvY = int16(iBestPos - iCurMeBlockPix)
		} else {
			sBestMv.iMvX = int16(iBestPos - iCurMeBlockPix)
			sBestMv.iMvY = 0
		}
		UpdateMeResults(sBestMv, uiBestCost, pMe.pColoRefMb,
			pMe.iColoRefMbOff+int(int32(sBestMv.iMvY)*kiRefStride+int32(sBestMv.iMvX)), pMe)
	}
}

func WelsMotionCrossSearch(pFuncList *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, kiEncStride int32, kiRefStride int32) {
	pfVerticalFullSearchFunc := pFuncList.pfVerticalFullSearch
	pfHorizontalFullSearchFunc := pFuncList.pfHorizontalFullSearch

	//vertical search
	pfVerticalFullSearchFunc(pFuncList, pMe,
		pMe.pMvdCost, pMe.iMvdCostOff,
		kiEncStride, kiRefStride,
		pSlice.sMvStartMin.iMvY,
		pSlice.sMvStartMax.iMvY, true)

	//horizontal search
	if pMe.uiSadCost >= pMe.uiSadCostThreshold {
		pfHorizontalFullSearchFunc(pFuncList, pMe,
			pMe.pMvdCost, pMe.iMvdCostOff,
			kiEncStride, kiRefStride,
			pSlice.sMvStartMin.iMvX,
			pSlice.sMvStartMax.iMvX,
			false)
	}
}

/////////////////////////
// Feature Search Basics
/////////////////////////
//memory related

// CMemoryAlign* pMa dropped (also below).
func RequestFeatureSearchPreparation(kiFrameWidth int32, kiFrameHeight int32, iNeedFeatureStorage int32, pFeatureSearchPreparation *SFeatureSearchPreparation) int32 {
	kiFeatureStrategyIndex := iNeedFeatureStorage >> 16
	bFme8x8 := (iNeedFeatureStorage & 0x0000FF & ME_FME) == ME_FME
	kiMarginSize := int32(16)
	if bFme8x8 {
		kiMarginSize = 8
	}
	kiFrameSize := (kiFrameWidth - kiMarginSize) * (kiFrameHeight - kiMarginSize)
	var iListOfFeatureOfBlock int32 // in bytes

	if 0 == kiFeatureStrategyIndex {
		iListOfFeatureOfBlock = 2 * kiFrameSize
	} else {
		iListOfFeatureOfBlock = 2*kiFrameSize +
			(kiFrameWidth-kiMarginSize)*4 + kiFrameWidth*8
	}
	pFeatureSearchPreparation.pFeatureOfBlock = make([]uint16, (iListOfFeatureOfBlock+1)/2)

	pFeatureSearchPreparation.uiFeatureStrategyIndex = uint8(kiFeatureStrategyIndex)
	pFeatureSearchPreparation.bFMESwitchFlag = true
	pFeatureSearchPreparation.uiFMEGoodFrameCount = FMESWITCH_DEFAULT_GOODFRAME_NUM
	pFeatureSearchPreparation.iHighFreMbCount = 0

	return ENC_RETURN_SUCCESS
}

// uint16_t*& pFeatureOfBlock -> *[]uint16.
func ReleaseFeatureSearchPreparation(pFeatureOfBlock *[]uint16) int32 {
	if pFeatureOfBlock != nil && *pFeatureOfBlock != nil {
		*pFeatureOfBlock = nil
		return ENC_RETURN_SUCCESS
	}
	return ENC_RETURN_UNEXPECTED
}

func RequestScreenBlockFeatureStorage(kiFrameWidth int32, kiFrameHeight int32, iNeedFeatureStorage int32, pScreenBlockFeatureStorage *SScreenBlockFeatureStorage) int32 {
	kiFeatureStrategyIndex := iNeedFeatureStorage >> 16
	kiMe8x8FME := iNeedFeatureStorage & 0x0000FF & ME_FME
	kiMe16x16FME := ((iNeedFeatureStorage & 0x00FF00) >> 8) & ME_FME
	if (kiMe8x8FME == ME_FME) && (kiMe16x16FME == ME_FME) {
		return ENC_RETURN_UNSUPPORTED_PARA
		//the following memory allocation cannot support when FME at both size
	}

	bIsBlock8x8 := kiMe8x8FME == ME_FME
	kiMarginSize := int32(16)
	if bIsBlock8x8 {
		kiMarginSize = 8
	}
	kiFrameSize := (kiFrameWidth - kiMarginSize) * (kiFrameHeight - kiMarginSize)
	var kiListSize int32 = 256
	if 0 == kiFeatureStrategyIndex {
		if bIsBlock8x8 {
			kiListSize = LIST_SIZE_SUM_8x8
		} else {
			kiListSize = LIST_SIZE_SUM_16x16
		}
	}

	pScreenBlockFeatureStorage.pTimesOfFeatureValue = make([]uint32, kiListSize)
	pScreenBlockFeatureStorage.pLocationOfFeature = make([][]uint16, kiListSize)
	pScreenBlockFeatureStorage.pLocationPointer = make([]uint16, 2*kiFrameSize)
	//  uint16_t* pFeatureValuePointerList[WELS_MAX (LIST_SIZE_SUM_16x16, LIST_SIZE_MSE_16x16)] = {0};
	pScreenBlockFeatureStorage.pFeatureValuePointerList = make([][]uint16, common.WELS_MAX(LIST_SIZE_SUM_16x16, LIST_SIZE_MSE_16x16))

	pScreenBlockFeatureStorage.pFeatureOfBlockPointer = nil
	if bIsBlock8x8 {
		pScreenBlockFeatureStorage.iIs16x16 = 0
	} else {
		pScreenBlockFeatureStorage.iIs16x16 = 1
	}
	pScreenBlockFeatureStorage.uiFeatureStrategyIndex = uint8(kiFeatureStrategyIndex)
	pScreenBlockFeatureStorage.iActualListSize = kiListSize
	common.WelsSetMemMultiplebytes_c(pScreenBlockFeatureStorage.uiSadCostThreshold[:], math.MaxUint32, BLOCK_SIZE_ALL, 4)
	pScreenBlockFeatureStorage.bRefBlockFeatureCalculated = false

	return ENC_RETURN_SUCCESS
}

func ReleaseScreenBlockFeatureStorage(pScreenBlockFeatureStorage *SScreenBlockFeatureStorage) int32 {
	if pScreenBlockFeatureStorage != nil {
		pScreenBlockFeatureStorage.pTimesOfFeatureValue = nil
		pScreenBlockFeatureStorage.pLocationOfFeature = nil
		pScreenBlockFeatureStorage.pLocationPointer = nil
		pScreenBlockFeatureStorage.pFeatureValuePointerList = nil
		return ENC_RETURN_SUCCESS
	}
	return ENC_RETURN_UNEXPECTED
}

// preprocess related
func SumOf8x8SingleBlock_c(pRef []uint8, iRefOff int, kiRefStride int32) int32 {
	var iSum int32
	p := iRefOff
	for i := 0; i < 8; i++ {
		for k := 0; k < 8; k++ {
			iSum += int32(pRef[p+k])
		}
		p += int(kiRefStride)
	}
	return iSum
}

func SumOf16x16SingleBlock_c(pRef []uint8, iRefOff int, kiRefStride int32) int32 {
	var iSum int32
	p := iRefOff
	for i := 0; i < 16; i++ {
		for k := 0; k < 16; k++ {
			iSum += int32(pRef[p+k])
		}
		p += int(kiRefStride)
	}
	return iSum
}

func SumOf8x8BlockOfFrame_c(pRefPicture []uint8, iRefPictureOff int, kiWidth int32, kiHeight int32, kiRefStride int32, pFeatureOfBlock []uint16, pTimesOfFeatureValue []uint32) {
	for y := int32(0); y < kiHeight; y++ {
		pRef := iRefPictureOff + int(kiRefStride*y)
		pBuffer := int(kiWidth * y)
		for x := int32(0); x < kiWidth; x++ {
			iSum := SumOf8x8SingleBlock_c(pRefPicture, pRef+int(x), kiRefStride)

			pFeatureOfBlock[pBuffer+int(x)] = uint16(iSum)
			pTimesOfFeatureValue[iSum]++
		}
	}
}

func SumOf16x16BlockOfFrame_c(pRefPicture []uint8, iRefPictureOff int, kiWidth int32, kiHeight int32, kiRefStride int32, pFeatureOfBlock []uint16, pTimesOfFeatureValue []uint32) {
	//TODO: this is similar to SumOf8x8BlockOfFrame_c expect the calling of single block func, refactor-able?
	for y := int32(0); y < kiHeight; y++ {
		pRef := iRefPictureOff + int(kiRefStride*y)
		pBuffer := int(kiWidth * y)
		for x := int32(0); x < kiWidth; x++ {
			iSum := SumOf16x16SingleBlock_c(pRefPicture, pRef+int(x), kiRefStride)

			pFeatureOfBlock[pBuffer+int(x)] = uint16(iSum)
			pTimesOfFeatureValue[iSum]++
		}
	}
}

// uint16_t** -> [][]uint16 whose entries become sub-slices of pBuf.
func InitializeHashforFeature_c(pTimesOfFeatureValue []uint32, pBuf []uint16, kiListSize int32, pLocationOfFeature [][]uint16, pFeatureValuePointerList [][]uint16) {
	//assign location pointer
	pBufPos := 0
	for i := int32(0); i < kiListSize; i++ {
		pLocationOfFeature[i] = pBuf[pBufPos:]
		pFeatureValuePointerList[i] = pBuf[pBufPos:]
		pBufPos += int(pTimesOfFeatureValue[i] << 1)
	}
}

func FillQpelLocationByFeatureValue_c(pFeatureOfBlock []uint16, kiWidth int32, kiHeight int32, pFeatureValuePointerList [][]uint16) {
	//assign each pixel's position
	pSrcPointer := 0
	var iQpelY int32
	for y := int32(0); y < kiHeight; y++ {
		for x := int32(0); x < kiWidth; x++ {
			uiFeature := pFeatureOfBlock[pSrcPointer+int(x)]
			p := pFeatureValuePointerList[uiFeature]
			p[0] = uint16(x << 2)
			p[1] = uint16(iQpelY)
			pFeatureValuePointerList[uiFeature] = p[2:]
		}
		iQpelY += 4
		pSrcPointer += int(kiWidth)
	}
}

func CalculateFeatureOfBlock(pFunc *SWelsFuncPtrList, pRef *SPicture, pScreenBlockFeatureStorage *SScreenBlockFeatureStorage) bool {
	pFeatureOfBlock := pScreenBlockFeatureStorage.pFeatureOfBlockPointer
	pTimesOfFeatureValue := pScreenBlockFeatureStorage.pTimesOfFeatureValue
	pLocationOfFeature := pScreenBlockFeatureStorage.pLocationOfFeature
	pBuf := pScreenBlockFeatureStorage.pLocationPointer

	if nil == pFeatureOfBlock || nil == pTimesOfFeatureValue || nil == pLocationOfFeature || nil == pBuf ||
		nil == pRef.pData[0] {
		return false
	}

	pRefData, iRefDataOff := pRef.pData[0], pRef.iDataOff[0]
	iRefStride := pRef.iLineSize[0]
	iIs16x16 := pScreenBlockFeatureStorage.iIs16x16
	iEdgeDiscard := int32(8) //this is to save complexity of padding on pRef
	if iIs16x16 != 0 {
		iEdgeDiscard = 16
	}
	iWidth := pRef.iWidthInPixel - iEdgeDiscard
	kiHeight := pRef.iHeightInPixel - iEdgeDiscard
	kiActualListSize := pScreenBlockFeatureStorage.iActualListSize

	clear(pTimesOfFeatureValue[:kiActualListSize])
	pFunc.pfCalculateBlockFeatureOfFrame[iIs16x16](pRefData, iRefDataOff, iWidth, kiHeight, iRefStride, pFeatureOfBlock,
		pTimesOfFeatureValue)

	//assign pLocationOfFeature pointer
	pFunc.pfInitializeHashforFeature(pTimesOfFeatureValue, pBuf, kiActualListSize,
		pLocationOfFeature, pScreenBlockFeatureStorage.pFeatureValuePointerList)

	//assign each pixel's pLocationOfFeature
	pFunc.pfFillQpelLocationByFeatureValue(pFeatureOfBlock, iWidth, kiHeight,
		pScreenBlockFeatureStorage.pFeatureValuePointerList)
	return true
}

func PerformFMEPreprocess(pFunc *SWelsFuncPtrList, pRef *SPicture, pFeatureOfBlock []uint16, pScreenBlockFeatureStorage *SScreenBlockFeatureStorage) {
	pScreenBlockFeatureStorage.pFeatureOfBlockPointer = pFeatureOfBlock
	pScreenBlockFeatureStorage.bRefBlockFeatureCalculated = CalculateFeatureOfBlock(pFunc, pRef,
		pScreenBlockFeatureStorage)

	if pScreenBlockFeatureStorage.bRefBlockFeatureCalculated {
		uiRefPictureAvgQstepx16 := uint32(QStepx16ByQp[common.WelsMedian(0, pRef.iFrameAverageQp, 51)])
		uiSadCostThreshold16x16 := (30 * (uiRefPictureAvgQstepx16 + 160)) >> 3
		pScreenBlockFeatureStorage.uiSadCostThreshold[BLOCK_16x16] = uiSadCostThreshold16x16
		pScreenBlockFeatureStorage.uiSadCostThreshold[BLOCK_8x8] = uiSadCostThreshold16x16 >> 2
		pScreenBlockFeatureStorage.uiSadCostThreshold[BLOCK_16x8] = math.MaxUint32
		pScreenBlockFeatureStorage.uiSadCostThreshold[BLOCK_8x16] = math.MaxUint32
		pScreenBlockFeatureStorage.uiSadCostThreshold[BLOCK_4x4] = math.MaxUint32
	}
}

// search related

// const SWelsME& sMe -> value.
func SetFeatureSearchIn(pFunc *SWelsFuncPtrList, sMe SWelsME, pSlice *SSlice, pRefFeatureStorage *SScreenBlockFeatureStorage, kiEncStride int32, kiRefStride int32, pFeatureSearchIn *SFeatureSearchIn) bool {
	pFeatureSearchIn.pSad = pFunc.sSampleDealingFuncs.pfSampleSad[sMe.uiBlockSize]
	iIdx := 0
	if BLOCK_16x16 == sMe.uiBlockSize {
		iIdx = 1
	}
	pFeatureSearchIn.iFeatureOfCurrent = pFunc.pfCalculateSingleBlockFeature[iIdx](sMe.pEncMb, sMe.iEncMbOff,
		kiEncStride)

	pFeatureSearchIn.pEnc, pFeatureSearchIn.iEncOff = sMe.pEncMb, sMe.iEncMbOff
	pFeatureSearchIn.pColoRef, pFeatureSearchIn.iColoRefOff = sMe.pColoRefMb, sMe.iColoRefMbOff
	pFeatureSearchIn.iEncStride = kiEncStride
	pFeatureSearchIn.iRefStride = kiRefStride
	pFeatureSearchIn.uiSadCostThresh = uint16(sMe.uiSadCostThreshold)

	pFeatureSearchIn.iCurPixX = sMe.iCurMeBlockPixX
	pFeatureSearchIn.iCurPixXQpel = pFeatureSearchIn.iCurPixX << 2
	pFeatureSearchIn.iCurPixY = sMe.iCurMeBlockPixY
	pFeatureSearchIn.iCurPixYQpel = pFeatureSearchIn.iCurPixY << 2

	pFeatureSearchIn.pTimesOfFeature = pRefFeatureStorage.pTimesOfFeatureValue
	pFeatureSearchIn.pQpelLocationOfFeature = pRefFeatureStorage.pLocationOfFeature
	pFeatureSearchIn.pMvdCostX = sMe.pMvdCost
	pFeatureSearchIn.iMvdCostXOff = sMe.iMvdCostOff - int(pFeatureSearchIn.iCurPixXQpel) - int(sMe.sMvp.iMvX)
	pFeatureSearchIn.pMvdCostY = sMe.pMvdCost
	pFeatureSearchIn.iMvdCostYOff = sMe.iMvdCostOff - int(pFeatureSearchIn.iCurPixYQpel) - int(sMe.sMvp.iMvY)

	pFeatureSearchIn.iMinQpelX = pFeatureSearchIn.iCurPixXQpel + (int32(pSlice.sMvStartMin.iMvX) * (1 << 2))
	pFeatureSearchIn.iMinQpelY = pFeatureSearchIn.iCurPixYQpel + (int32(pSlice.sMvStartMin.iMvY) * (1 << 2))
	pFeatureSearchIn.iMaxQpelX = pFeatureSearchIn.iCurPixXQpel + (int32(pSlice.sMvStartMax.iMvX) * (1 << 2))
	pFeatureSearchIn.iMaxQpelY = pFeatureSearchIn.iCurPixYQpel + (int32(pSlice.sMvStartMax.iMvY) * (1 << 2))

	if nil == pFeatureSearchIn.pSad || nil == pFeatureSearchIn.pTimesOfFeature ||
		nil == pFeatureSearchIn.pQpelLocationOfFeature {
		return false
	}
	return true
}

// pRef: pixel pointer -> (slice, offset).
func SaveFeatureSearchOut(sBestMv SMVUnitXY, uiBestSadCost uint32, pRef []uint8, iRefOff int, pFeatureSearchOut *SFeatureSearchOut) {
	pFeatureSearchOut.sBestMv = sBestMv
	pFeatureSearchOut.uiBestSadCost = uiBestSadCost
	pFeatureSearchOut.pBestRef = pRef
	pFeatureSearchOut.iBestRefOff = iRefOff
}

// SFeatureSearchIn& -> *SFeatureSearchIn.
func FeatureSearchOne(sFeatureSearchIn *SFeatureSearchIn, iFeatureDifference int32, kuiExpectedSearchTimes uint32, pFeatureSearchOut *SFeatureSearchOut) bool {
	iFeatureOfRef := sFeatureSearchIn.iFeatureOfCurrent + iFeatureDifference
	if iFeatureOfRef < 0 || iFeatureOfRef >= LIST_SIZE {
		return true
	}

	pSad := sFeatureSearchIn.pSad
	pEnc, iEncOff := sFeatureSearchIn.pEnc, sFeatureSearchIn.iEncOff
	pColoRef, iColoRefOff := sFeatureSearchIn.pColoRef, sFeatureSearchIn.iColoRefOff
	iEncStride := sFeatureSearchIn.iEncStride
	iRefStride := sFeatureSearchIn.iRefStride
	uiSadCostThresh := sFeatureSearchIn.uiSadCostThresh

	iCurPixX := sFeatureSearchIn.iCurPixX
	iCurPixY := sFeatureSearchIn.iCurPixY
	iCurPixXQpel := sFeatureSearchIn.iCurPixXQpel
	iCurPixYQpel := sFeatureSearchIn.iCurPixYQpel

	iMinQpelX := sFeatureSearchIn.iMinQpelX
	iMinQpelY := sFeatureSearchIn.iMinQpelY
	iMaxQpelX := sFeatureSearchIn.iMaxQpelX
	iMaxQpelY := sFeatureSearchIn.iMaxQpelY

	iSearchTimes := int32(common.WELS_MIN(sFeatureSearchIn.pTimesOfFeature[iFeatureOfRef], kuiExpectedSearchTimes))
	iSearchTimesx2 := iSearchTimes << 1
	pQpelPosition := sFeatureSearchIn.pQpelLocationOfFeature[iFeatureOfRef]

	var sBestMv SMVUnitXY
	var uiBestCost, uiTmpCost uint32
	var pBestRef []uint8
	var iBestRefOff int
	var iQpelX, iQpelY int32
	var iIntepelX, iIntepelY int32
	var i int32

	sBestMv.iMvX = pFeatureSearchOut.sBestMv.iMvX
	sBestMv.iMvY = pFeatureSearchOut.sBestMv.iMvY
	uiBestCost = pFeatureSearchOut.uiBestSadCost
	pBestRef, iBestRefOff = pFeatureSearchOut.pBestRef, pFeatureSearchOut.iBestRefOff

	for i = 0; i < iSearchTimesx2; i += 2 {
		iQpelX = int32(pQpelPosition[i])
		iQpelY = int32(pQpelPosition[i+1])

		if (iQpelX > iMaxQpelX) || (iQpelX < iMinQpelX) ||
			(iQpelY > iMaxQpelY) || (iQpelY < iMinQpelY) ||
			(iQpelX == iCurPixXQpel) || (iQpelY == iCurPixYQpel) {
			continue
		}

		uiTmpCost = uint32(int32(sFeatureSearchIn.pMvdCostX[sFeatureSearchIn.iMvdCostXOff+int(iQpelX)]) +
			int32(sFeatureSearchIn.pMvdCostY[sFeatureSearchIn.iMvdCostYOff+int(iQpelY)]))
		if uiTmpCost+uint32(iFeatureDifference) >= uiBestCost {
			continue
		}

		iIntepelX = (iQpelX >> 2) - iCurPixX
		iIntepelY = (iQpelY >> 2) - iCurPixY
		iCurRefOff := iColoRefOff + int(iIntepelX+iIntepelY*iRefStride)
		uiTmpCost += uint32(pSad(pEnc, iEncOff, iEncStride, pColoRef, iCurRefOff, iRefStride))
		if uiTmpCost < uiBestCost {
			sBestMv.iMvX = int16(iIntepelX)
			sBestMv.iMvY = int16(iIntepelY)
			uiBestCost = uiTmpCost
			pBestRef, iBestRefOff = pColoRef, iCurRefOff

			if uiBestCost < uint32(uiSadCostThresh) {
				break
			}
		}
	}
	SaveFeatureSearchOut(sBestMv, uiBestCost, pBestRef, iBestRefOff, pFeatureSearchOut)
	return i < iSearchTimesx2
}

func MotionEstimateFeatureFullSearch(sFeatureSearchIn *SFeatureSearchIn, kuiMaxSearchPoint uint32, pMe *SWelsME) {
	var sFeatureSearchOut SFeatureSearchOut //TODO: this can be refactored and removed
	sFeatureSearchOut.uiBestSadCost = pMe.uiSadCost
	sFeatureSearchOut.sBestMv = pMe.sMv
	sFeatureSearchOut.pBestRef = pMe.pRefMb
	sFeatureSearchOut.iBestRefOff = pMe.iRefMbOff

	var iFeatureDifference int32 //TODO: change it according to computational-complexity setting when needed
	FeatureSearchOne(sFeatureSearchIn, iFeatureDifference, kuiMaxSearchPoint, &sFeatureSearchOut)
	if sFeatureSearchOut.uiBestSadCost < pMe.uiSadCost { //TODO: this may be refactored and removed
		UpdateMeResults(sFeatureSearchOut.sBestMv,
			sFeatureSearchOut.uiBestSadCost, sFeatureSearchOut.pBestRef, sFeatureSearchOut.iBestRefOff,
			pMe)
	}
}

// switch related
func CountFMECostDown(pCurLayer *SDqLayer) uint32 {
	var uiCostDownSum uint32
	kiSliceCount := GetCurrentSliceNum(pCurLayer)
	if kiSliceCount >= 1 {
		var iSliceIndex int32
		for iSliceIndex < kiSliceCount {
			pSlice := pCurLayer.ppSliceInLayer[iSliceIndex]
			uiCostDownSum += pSlice.uiSliceFMECostDown
			iSliceIndex++
		}
	}
	return uiCostDownSum
}

const (
	FMESWITCH_MBAVERCOSTSAVING_THRESHOLD = 2 //empirically set.
	FMESWITCH_GOODFRAMECOUNT_MAX         = 5 //empirically set.
)

func UpdateFMEGoodFrameCount(iAvMBNormalizedRDcostDown uint32, uiFMEGoodFrameCount *uint8) {
	//this strategy may be changed, here the number is derived from empirical-numbers
	// uiFMEGoodFrameCount lies in [0,FMESWITCH_GOODFRAMECOUNT_MAX]
	if iAvMBNormalizedRDcostDown > FMESWITCH_MBAVERCOSTSAVING_THRESHOLD {
		if *uiFMEGoodFrameCount < FMESWITCH_GOODFRAMECOUNT_MAX {
			*uiFMEGoodFrameCount++
		}
	} else {
		if *uiFMEGoodFrameCount > 0 {
			*uiFMEGoodFrameCount--
		}
	}
}

func UpdateFMESwitch(pCurLayer *SDqLayer) {
	iFMECost := CountFMECostDown(pCurLayer)
	iAvMBNormalizedRDcostDown := iFMECost / uint32(int32(pCurLayer.iMbWidth)*int32(pCurLayer.iMbHeight))
	UpdateFMEGoodFrameCount(iAvMBNormalizedRDcostDown, &pCurLayer.pFeatureSearchPreparation.uiFMEGoodFrameCount)
}

func UpdateFMESwitchNull(pCurLayer *SDqLayer) {
}

/////////////////////////
// Search function options
/////////////////////////

func WelsDiamondCrossSearch(pFunc *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, kiEncStride int32, kiRefStride int32) {
	//  Step 1: diamond search
	WelsDiamondSearch(pFunc, pMe, pSlice, kiEncStride, kiRefStride)

	//  Step 2: CROSS search
	pMe.uiSadCostThreshold = pMe.pRefFeatureStorage.uiSadCostThreshold[pMe.uiBlockSize]
	if pMe.uiSadCost >= pMe.uiSadCostThreshold {
		WelsMotionCrossSearch(pFunc, pMe, pSlice, kiEncStride, kiRefStride)
	}
}

func WelsDiamondCrossFeatureSearch(pFunc *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, kiEncStride int32, kiRefStride int32) {
	//  Step 1: diamond search + cross
	WelsDiamondCrossSearch(pFunc, pMe, pSlice, kiEncStride, kiRefStride)

	// Step 2: FeatureSearch
	if pMe.uiSadCost >= pMe.uiSadCostThreshold {
		pSlice.uiSliceFMECostDown += pMe.uiSadCost

		uiMaxSearchPoint := uint32(math.MaxInt32) //TODO: change it according to computational-complexity setting
		var sFeatureSearchIn SFeatureSearchIn
		if SetFeatureSearchIn(pFunc, *pMe, pSlice, pMe.pRefFeatureStorage,
			kiEncStride, kiRefStride,
			&sFeatureSearchIn) {
			MotionEstimateFeatureFullSearch(&sFeatureSearchIn, uiMaxSearchPoint, pMe)
		}
		pSlice.uiSliceFMECostDown -= pMe.uiSadCost
	}
}
