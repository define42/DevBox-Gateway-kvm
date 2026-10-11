// Port of codec/encoder/core/src/svc_mode_decision.cpp (SVC mode decision,
// background detection and screen content mode decision).

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"
)

// ////////////
// MD for enhancement layers
// ////////////
func WelsMdSpatialelInterMbIlfmdNoilp(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, kuiRefMbType Mb_Type) {
	pCurDqLayer := pEncCtx.pCurDqLayer
	pMbCache := &pSlice.sMbCacheInfo

	kuiNeighborAvail := uint32(pCurMb.uiNeighborAvail)
	kiMbWidth := int32(pCurDqLayer.iMbWidth)
	// kpTopMb = pCurMb - kiMbWidth (only dereferenced when available)
	kbMbLeftAvailPskip := kuiNeighborAvail&LEFT_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-1).uiMbType)
	kbMbTopAvailPskip := kuiNeighborAvail&TOP_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-kiMbWidth).uiMbType)
	kbMbTopLeftAvailPskip := kuiNeighborAvail&TOPLEFT_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-kiMbWidth-1).uiMbType)
	kbMbTopRightAvailPskip := kuiNeighborAvail&TOPRIGHT_MB_POS != 0 && common.IS_SKIP(pCurMb.Add(-kiMbWidth+1).uiMbType)

	bTrySkip := kbMbLeftAvailPskip || kbMbTopAvailPskip || kbMbTopLeftAvailPskip || kbMbTopRightAvailPskip
	pMbCache.bKeepSkipScratch = kbMbLeftAvailPskip && kbMbTopAvailPskip && kbMbTopRightAvailPskip
	bSkip := false

	if pEncCtx.pFuncList.pfInterMdBackgroundDecision(pEncCtx, pWelsMd, pSlice, pCurMb, pMbCache, &pMbCache.bKeepSkipScratch) {
		return
	}

	//step 1: try SKIP
	bSkip = WelsMdInterJudgePskip(pEncCtx, pWelsMd, pSlice, pCurMb, pMbCache, bTrySkip)

	if bSkip && pMbCache.bKeepSkipScratch {
		WelsMdInterDecidedPskip(pEncCtx, pSlice, pCurMb, pMbCache)
		return
	}

	if !common.IS_SVC_INTRA(kuiRefMbType) {
		if !bSkip {
			PredictSad(pMbCache.sMvComponents.iRefIndexCache[:], pMbCache.iSadCost[:], 0, &pWelsMd.iSadPredMb)

			//step 2: P_16x16
			pWelsMd.iCostLuma = WelsMdP16x16(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice, pCurMb)
			pCurMb.uiMbType = common.MB_TYPE_16x16
		}

		WelsMdInterSecondaryModesEnc(pEncCtx, pWelsMd, pSlice, pCurMb, pMbCache, bSkip)
	} else { //BLMODE == SVC_INTRA
		//initial prediction memory for I_16x16
		kiCostI16x16 := WelsMdI16x16(pEncCtx.pFuncList, pEncCtx.pCurDqLayer, pMbCache, pWelsMd.iLambda)
		if bSkip && (pWelsMd.iCostLuma <= kiCostI16x16) {
			WelsMdInterDecidedPskip(pEncCtx, pSlice, pCurMb, pMbCache)
		} else {
			pWelsMd.iCostLuma = kiCostI16x16
			pCurMb.uiMbType = common.MB_TYPE_INTRA16x16

			WelsMdIntraSecondaryModesEnc(pEncCtx, pWelsMd, pCurMb, pMbCache)
		}
	}
}

func WelsMdInterMbEnhancelayer(pEncCtx *sWelsEncCtx, pMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache) {
	pCurLayer := pEncCtx.pCurDqLayer
	pWelsMd := pMd
	kpInterLayerRefMb := GetRefMb(pCurLayer, pCurMb)
	kuiInterLayerRefMbType := kpInterLayerRefMb.uiMbType

	SetMvBaseEnhancelayer(pWelsMd, pCurMb,
		kpInterLayerRefMb) // initial sMvBase here only when pRef mb type is inter, if not sMvBase will be not used!
	//step (3): do the MD process
	WelsMdSpatialelInterMbIlfmdNoilp(pEncCtx, pWelsMd, pSlice, pCurMb, kuiInterLayerRefMbType) //MD process
}

// GetRefMb does the initiation for noILP (needed by ILFMD); returns a
// pointer into pCurLayer.pRefLayer.sMbDataP.
func GetRefMb(pCurLayer *SDqLayer, pCurMb *SMB) *SMB {
	kpRefLayer := pCurLayer.pRefLayer
	kiRefMbIdx := (int32(pCurMb.iMbY)>>1)*int32(kpRefLayer.iMbWidth) + (int32(pCurMb.iMbX) >>
		1) //because current lower layer is half size on both vertical and horizontal
	// For non-dyadic spatial ratios the index can lie past the end of the
	// reference layer's MB list; C then reads the next layers' MBs, which
	// follow it in the same allocation (see InitMbListD), so index the
	// list through its full capacity.
	return &kpRefLayer.sMbDataP[:cap(kpRefLayer.sMbDataP)][kiRefMbIdx]
}

func SetMvBaseEnhancelayer(pMd *SWelsMD, pCurMb *SMB, kpRefMb *SMB) {
	kuiRefMbType := kpRefMb.uiMbType

	if !common.IS_SVC_INTRA(kuiRefMbType) {
		var sMv SMVUnitXY
		iRefMbPartIdx := ((int32(pCurMb.iMbY) & 0x01) << 1) + (int32(pCurMb.iMbX) & 0x01) //may be need modified
		iScan4RefPartIdx := common.G_kuiMbCountScan4Idx[iRefMbPartIdx<<2]
		sMv.iMvX = int16(int32(kpRefMb.sMv[iScan4RefPartIdx].iMvX) * (1 << 1))
		sMv.iMvY = int16(int32(kpRefMb.sMv[iScan4RefPartIdx].iMvY) * (1 << 1))

		pMd.sMe.sMe16x16.sMvBase = sMv

		pMd.sMe.sMe8x8[0].sMvBase = sMv
		pMd.sMe.sMe8x8[1].sMvBase = sMv
		pMd.sMe.sMe8x8[2].sMvBase = sMv
		pMd.sMe.sMe8x8[3].sMvBase = sMv

		pMd.sMe.sMe16x8[0].sMvBase = sMv
		pMd.sMe.sMe16x8[1].sMvBase = sMv
		pMd.sMe.sMe8x16[0].sMvBase = sMv
		pMd.sMe.sMe8x16[1].sMvBase = sMv
	}
}

// ////////////
// MD for Background decision
// ////////////

// GetChromaCost (inline in C) for the BGD Pskip.
// pCalculateFunc: PSampleSadSatdCostFunc* (a cost function table such as pfMdCost).
func GetChromaCost(pCalculateFunc *[MAX_BLOCK_TYPE]PSampleSadSatdCostFunc, pSrcChroma []uint8, iSrcChromaOff int, iSrcStride int32, pRefChroma []uint8, iRefChromaOff int, iRefStride int32) int32 {
	return pCalculateFunc[BLOCK_8x8](pSrcChroma, iSrcChromaOff, iSrcStride, pRefChroma, iRefChromaOff, iRefStride)
}

// (inline in C)
func IsCostLessEqualSkipCost(iCurCost int32, iPredPskipSad int32, iRefMbType int32, pRef *SPicture, iMbXy int32, iSmallestInvisibleTh int32) bool {
	return (iPredPskipSad > iSmallestInvisibleTh && iCurCost >= iPredPskipSad) ||
		(pRef.iPictureType == common.P_SLICE &&
			iRefMbType == common.MB_TYPE_SKIP &&
			pRef.pMbSkipSad[iMbXy] > iSmallestInvisibleTh &&
			iCurCost >= pRef.pMbSkipSad[iMbXy])
}

const (
	KNOWN_CHROMA_TOO_LARGE = 640
	SMALLEST_INVISIBLE     = 128 //2*64, 2 in pixel maybe the smallest not visible for luma
)

func CheckChromaCost(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pMbCache *SMbCache, iCurMbXy int32) bool {
	pSad := &pEncCtx.pFuncList.sSampleDealingFuncs.pfSampleSad
	pCurDqLayer := pEncCtx.pCurDqLayer

	pCbEnc, iCbEncOff := pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1]
	pCrEnc, iCrEncOff := pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2]
	pCbRef, iCbRefOff := pMbCache.SPicData.pRefMb[1], pMbCache.SPicData.iRefMbOff[1]
	pCrRef, iCrRefOff := pMbCache.SPicData.pRefMb[2], pMbCache.SPicData.iRefMbOff[2]

	iCbEncStride := pCurDqLayer.iEncStride[1]
	iCrEncStride := pCurDqLayer.iEncStride[2]
	iChromaRefStride := pCurDqLayer.pRefPic.iLineSize[1]

	iCbSad := GetChromaCost(pSad, pCbEnc, iCbEncOff, iCbEncStride, pCbRef, iCbRefOff, iChromaRefStride)
	iCrSad := GetChromaCost(pSad, pCrEnc, iCrEncOff, iCrEncStride, pCrRef, iCrRefOff, iChromaRefStride)

	//01/17/13
	//the in-question error area is
	//from: (yellow) Y=212, V=023, U=145
	//to:     (grey)    Y=213, V=136, U=124
	//visible difference can be seen on the U plane
	//so the allowing chroma difference should be at least no larger than
	//20*8*8 = 1280 for U or V
	//one local test case show that "either one >640" will become a too strict criteria, which will appear when QP is large(36) and maybe no much harm for visual
	//another local test case show that "either one >960" will be a moderate criteria, an area is changed from light green to light pink, but without careful observation it won't be obvious, but people will feel the unclean area (and note that, the color visible criteria is also related to the luma of them!)
	//another case show that color changed from black to very dark red can be visible even under the threshold 960, the color difference is about 13*64=832 (U123V145->U129V132)
	//TODO:
	//OPTI-ABLE: the visible color criteria may be related to luma (very bright or very dark), or related to the ratio of U/V rather than the absolute value
	bChromaTooLarge := iCbSad > KNOWN_CHROMA_TOO_LARGE || iCrSad > KNOWN_CHROMA_TOO_LARGE

	iChromaSad := iCbSad + iCrSad
	PredictSadSkip(pMbCache.sMvComponents.iRefIndexCache[:], pMbCache.bMbTypeSkip[:], pMbCache.iSadCostSkip[:], 0,
		&pWelsMd.iSadPredSkip)
	bChromaCostCannotSkip := IsCostLessEqualSkipCost(iChromaSad, pWelsMd.iSadPredSkip, int32(pMbCache.uiRefMbType),
		pCurDqLayer.pRefPic, iCurMbXy, SMALLEST_INVISIBLE)

	return !bChromaCostCannotSkip && !bChromaTooLarge
}

// WelsMdInterJudgeBGDPskip tries the BGD Pskip.
// 01/17/2013. USE the NEW BGD Pskip with COLOR CHECK for screen content and camera because of color artifact seen in test
func WelsMdInterJudgeBGDPskip(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache, bKeepSkip *bool) bool {
	pCurDqLayer := pEncCtx.pCurDqLayer

	kiRefMbQp := int32(pCurDqLayer.pRefPic.pRefMbQp[pCurMb.iMbXY])
	kiCurMbQp := int32(pCurMb.uiLumaQp) // unsigned -> signed
	pVaaBgMbFlag := pEncCtx.pVaa.pVaaBackgroundMbFlag
	iVaaBgMbFlagOff := int(pCurMb.iMbXY)

	kiMbWidth := int(pCurDqLayer.iMbWidth)

	*bKeepSkip = (*bKeepSkip) &&
		((pVaaBgMbFlag[iVaaBgMbFlagOff-1] == 0) &&
			(pVaaBgMbFlag[iVaaBgMbFlagOff-kiMbWidth] == 0) &&
			(pVaaBgMbFlag[iVaaBgMbFlagOff-kiMbWidth+1] == 0))

	if pVaaBgMbFlag[iVaaBgMbFlagOff] != 0 &&
		!common.IS_INTRA(pMbCache.uiRefMbType) &&
		(kiRefMbQp-kiCurMbQp <= DELTA_QP_BGD_THD || kiRefMbQp <= 26) {
		//01/16/13
		//the current BGD method uses luma SAD in first step judging of Background blocks
		//and uses chroma edges to confirm the Background blocks
		//HOWEVER, there is such case in SCC,
		//that the luma of two collocated blocks (block in reference frame and in current frame) is very similar
		//but the chroma are very different, at the same time the chroma are plain and without edge
		//IN SUCH A CASE,
		//it will be not proper to just use Pskip
		//TODO: consider reusing this result of ChromaCheck when SCDSkip needs this as well

		if CheckChromaCost(pEncCtx, pWelsMd, pMbCache, pCurMb.iMbXY) {
			sVaaPredSkipMv := SMVUnitXY{}
			PredSkipMv(pMbCache, &sVaaPredSkipMv)
			WelsMdBackgroundMbEnc(pEncCtx, pWelsMd, pCurMb, pMbCache, pSlice, sVaaPredSkipMv == SMVUnitXY{})
			return true
		}
	}

	return false
}

func WelsMdInterJudgeBGDPskipFalse(pCtx *sWelsEncCtx, pMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache, bKeepSkip *bool) bool {
	return false
}

// WelsMdUpdateBGDInfo updates BGD related info.
func WelsMdUpdateBGDInfo(pCurLayer *SDqLayer, pCurMb *SMB, bCollocatedPredFlag bool, iRefPictureType int32) {
	pTargetRefMbQpList := pCurLayer.pDecPic.pRefMbQp
	kiMbXY := pCurMb.iMbXY

	if pCurMb.uiCbp != 0 || common.I_SLICE == iRefPictureType || !bCollocatedPredFlag {
		pTargetRefMbQpList[kiMbXY] = pCurMb.uiLumaQp
	} else { //unchange, do not need to evaluation?
		pRefPicRefMbQpList := pCurLayer.pRefPic.pRefMbQp
		pTargetRefMbQpList[kiMbXY] = pRefPicRefMbQpList[kiMbXY]
	}

	if pCurMb.uiMbType == MB_TYPE_BACKGROUND {
		pCurMb.uiMbType = common.MB_TYPE_SKIP
	}
}

func WelsMdUpdateBGDInfoNULL(pCurLayer *SDqLayer, pCurMb *SMB, bCollocatedPredFlag bool, iRefPictureType int32) {
	WelsMdUpdateBGDInfo(pCurLayer, pCurMb, bCollocatedPredFlag, iRefPictureType)
}

// ////////////
// MD for screen contents
// ////////////

// (inline in C) pBlockType: SWelsMD.iBlock8x8StaticIdc.
func IsMbStatic(pBlockType []int32, eType processing.EStaticBlockIdc) bool {
	return pBlockType != nil &&
		eType == pBlockType[0] &&
		eType == pBlockType[1] &&
		eType == pBlockType[2] &&
		eType == pBlockType[3]
}

func IsMbCollocatedStatic(pBlockType []int32) bool {
	return IsMbStatic(pBlockType, processing.COLLOCATED_STATIC)
}

func IsMbScrolledStatic(pBlockType []int32) bool {
	return IsMbStatic(pBlockType, processing.SCROLLED_STATIC)
}

// (inline in C)
func CalUVSadCost(pFunc *SWelsFuncPtrList, pEncOri []uint8, iEncOriOff int, iStrideUV int32, pRefOri []uint8, iRefOriOff int, iRefLineSize int32) int32 {
	return pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8](pEncOri, iEncOriOff, iStrideUV, pRefOri, iRefOriOff, iRefLineSize)
}

func CheckBorder(iMbX int32, iMbY int32, iScrollMvX int32, iScrollMvY int32, iMbWidth int32, iMbHeight int32) bool {
	return (iMbX<<4)+iScrollMvX < 0 ||
		(iMbX<<4)+iScrollMvX > (iMbWidth-1)<<4 ||
		(iMbY<<4)+iScrollMvY < 0 ||
		(iMbY<<4)+iScrollMvY > (iMbHeight-1)<<4 //border check for safety
}

func JudgeStaticSkip(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache, pWelsMd *SWelsMD) bool {
	pCurDqLayer := pEncCtx.pCurDqLayer
	kiMbX := int32(pCurMb.iMbX)
	kiMbY := int32(pCurMb.iMbY)

	bTryStaticSkip := IsMbCollocatedStatic(pWelsMd.iBlock8x8StaticIdc[:])
	if bTryStaticSkip {
		var iStrideUV, iOffsetUV int32
		pFunc := pEncCtx.pFuncList
		pRefOri := pCurDqLayer.pRefOri[0]
		if pRefOri != nil {
			iStrideUV = pCurDqLayer.iEncStride[1]
			iOffsetUV = (kiMbX + kiMbY*iStrideUV) << 3

			iSadCostCb := CalUVSadCost(pFunc, pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1], iStrideUV,
				pRefOri.pData[1], pRefOri.iDataOff[1]+int(iOffsetUV), pRefOri.iLineSize[1])
			if iSadCostCb == 0 {
				iSadCostCr := CalUVSadCost(pFunc, pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2], iStrideUV,
					pRefOri.pData[2], pRefOri.iDataOff[2]+int(iOffsetUV), pRefOri.iLineSize[1])
				bTryStaticSkip = 0 == iSadCostCr
			} else {
				bTryStaticSkip = false
			}
		} else {
			bTryStaticSkip = false
		}
	}
	return bTryStaticSkip
}

func JudgeScrollSkip(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache, pWelsMd *SWelsMD) bool {
	pCurDqLayer := pEncCtx.pCurDqLayer
	kiMbX := int32(pCurMb.iMbX)
	kiMbY := int32(pCurMb.iMbY)
	kiMbWidth := int32(pCurDqLayer.iMbWidth)
	kiMbHeight := int32(pCurDqLayer.iMbHeight)
	// const int32_t block_width = mb_width << 1;
	pVaaExt := pEncCtx.pVaa.pExt

	bTryScrollSkip := false

	if pVaaExt.sScrollDetectInfo.BScrollDetectFlag {
		bTryScrollSkip = IsMbScrolledStatic(pWelsMd.iBlock8x8StaticIdc[:])
	} else {
		return false
	}

	if bTryScrollSkip {
		var iStrideUV, iOffsetUV int32
		pFunc := pEncCtx.pFuncList
		pRefOri := pCurDqLayer.pRefOri[0]
		if pRefOri != nil {
			iScrollMvX := pVaaExt.sScrollDetectInfo.IScrollMvX
			iScrollMvY := pVaaExt.sScrollDetectInfo.IScrollMvY
			if CheckBorder(kiMbX, kiMbY, iScrollMvX, iScrollMvY, kiMbWidth, kiMbHeight) {
				bTryScrollSkip = false
			} else {
				iStrideUV = pCurDqLayer.iEncStride[1]
				iOffsetUV = (kiMbX << 3) + (iScrollMvX >> 1) + ((kiMbY<<3)+(iScrollMvY>>1))*iStrideUV

				iSadCostCb := CalUVSadCost(pFunc, pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1], iStrideUV,
					pRefOri.pData[1], pRefOri.iDataOff[1]+int(iOffsetUV), pRefOri.iLineSize[1])
				if iSadCostCb == 0 {
					iSadCostCr := CalUVSadCost(pFunc, pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2], iStrideUV,
						pRefOri.pData[2], pRefOri.iDataOff[2]+int(iOffsetUV), pRefOri.iLineSize[1])
					bTryScrollSkip = 0 == iSadCostCr
				} else {
					bTryScrollSkip = false
				}
			}
		}
	}
	return bTryScrollSkip
}

// SMVUnitXY sCurMbMv[] -> []SMVUnitXY.
func SvcMdSCDMbEnc(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache, pSlice *SSlice, bQpSimilarFlag bool, bMbSkipFlag bool, sCurMbMv []SMVUnitXY, eSkipMode ESkipModes) {
	pCurDqLayer := pEncCtx.pCurDqLayer
	pFunc := pEncCtx.pFuncList
	sMvp := SMVUnitXY{}
	sMvp.iMvX = sCurMbMv[eSkipMode].iMvX
	sMvp.iMvY = sCurMbMv[eSkipMode].iMvY
	pRefLuma, iRefLumaOff := pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]
	pRefCb, iRefCbOff := pMbCache.SPicData.pRefMb[1], pMbCache.SPicData.iRefMbOff[1]
	pRefCr, iRefCrOff := pMbCache.SPicData.pRefMb[2], pMbCache.SPicData.iRefMbOff[2]
	iLineSizeY := pCurDqLayer.pRefPic.iLineSize[0]
	iLineSizeUV := pCurDqLayer.pRefPic.iLineSize[1]
	pDstLuma, iDstLumaOff := pMbCache.pSkipMb, 0
	pDstCb, iDstCbOff := pMbCache.pSkipMb, 256
	pDstCr, iDstCrOff := pMbCache.pSkipMb, 256+64

	iOffsetY := int((int32(sCurMbMv[eSkipMode].iMvX) >> 2) + (int32(sCurMbMv[eSkipMode].iMvY)>>2)*iLineSizeY)
	iOffsetUV := int((int32(sCurMbMv[eSkipMode].iMvX) >> 3) + (int32(sCurMbMv[eSkipMode].iMvY)>>3)*iLineSizeUV)

	if !bQpSimilarFlag || !bMbSkipFlag {
		pDstLuma, iDstLumaOff = pMbCache.pMemPredLuma, 0
		pDstCb, iDstCbOff = pMbCache.pMemPredChroma, 0
		pDstCr, iDstCrOff = pMbCache.pMemPredChroma, 64
	}
	//MC
	pFunc.sMcFuncs.PMcLumaFunc(pRefLuma, iRefLumaOff+iOffsetY, iLineSizeY, pDstLuma, iDstLumaOff, 16, 0, 0, 16, 16)
	pFunc.sMcFuncs.PMcChromaFunc(pRefCb, iRefCbOff+iOffsetUV, iLineSizeUV, pDstCb, iDstCbOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8)
	pFunc.sMcFuncs.PMcChromaFunc(pRefCr, iRefCrOff+iOffsetUV, iLineSizeUV, pDstCr, iDstCrOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8)

	pCurMb.uiCbp = 0
	pWelsMd.iCostLuma = 0
	*pCurMb.pSadCost = pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
		pCurDqLayer.iEncStride[0], pRefLuma, iRefLumaOff+iOffsetY, iLineSizeY)

	pWelsMd.iCostSkipMb = *pCurMb.pSadCost

	pCurMb.sP16x16Mv.iMvX = sCurMbMv[eSkipMode].iMvX
	pCurMb.sP16x16Mv.iMvY = sCurMbMv[eSkipMode].iMvY

	pCurDqLayer.pDecPic.sMvList[pCurMb.iMbXY].iMvX = sCurMbMv[eSkipMode].iMvX
	pCurDqLayer.pDecPic.sMvList[pCurMb.iMbXY].iMvY = sCurMbMv[eSkipMode].iMvY

	if bQpSimilarFlag && bMbSkipFlag {
		//update motion info to current MB
		setRefIndexZero(pCurMb)
		pFunc.pfUpdateMbMv(pCurMb.sMv, sMvp)
		pCurMb.uiMbType = common.MB_TYPE_SKIP
		WelsRecPskip(pCurDqLayer, pEncCtx.pFuncList, pCurMb, pMbCache)
		WelsMdInterUpdatePskip(pCurDqLayer, pSlice, pCurMb, pMbCache)
		return
	}

	pCurMb.uiMbType = common.MB_TYPE_16x16

	pWelsMd.sMe.sMe16x16.sMv.iMvX = sCurMbMv[eSkipMode].iMvX
	pWelsMd.sMe.sMe16x16.sMv.iMvY = sCurMbMv[eSkipMode].iMvY
	PredMv(&pMbCache.sMvComponents, 0, 4, 0, &pWelsMd.sMe.sMe16x16.sMvp)
	pMbCache.sMbMvp[0] = pWelsMd.sMe.sMe16x16.sMvp

	UpdateP16x16MotionInfo(pMbCache, pCurMb, 0, &pWelsMd.sMe.sMe16x16.sMv)

	if pWelsMd.bMdUsingSad {
		pWelsMd.iCostLuma = *pCurMb.pSadCost
	} else {
		pWelsMd.iCostLuma = pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
			pCurDqLayer.iEncStride[0], pRefLuma, iRefLumaOff, iLineSizeY)
	}

	WelsInterMbEncode(pEncCtx, pSlice, pCurMb)
	WelsPMbChromaEncode(pEncCtx, pSlice, pCurMb)

	pFunc.pfCopy16x16Aligned(pMbCache.SPicData.pCsMb[0], pMbCache.SPicData.iCsMbOff[0], pCurDqLayer.iCsStride[0], pMbCache.pMemPredLuma, 0, 16)
	pFunc.pfCopy8x8Aligned(pMbCache.SPicData.pCsMb[1], pMbCache.SPicData.iCsMbOff[1], pCurDqLayer.iCsStride[1], pMbCache.pMemPredChroma, 0, 8)
	pFunc.pfCopy8x8Aligned(pMbCache.SPicData.pCsMb[2], pMbCache.SPicData.iCsMbOff[2], pCurDqLayer.iCsStride[1], pMbCache.pMemPredChroma, 64, 8)
}

func MdInterSCDPskipProcess(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, pMbCache *SMbCache, eSkipMode ESkipModes) bool {
	pVaaExt := pEncCtx.pVaa.pExt
	pCurDqLayer := pEncCtx.pCurDqLayer

	kiRefMbQp := int32(pCurDqLayer.pRefPic.pRefMbQp[pCurMb.iMbXY])
	kiCurMbQp := int32(pCurMb.uiLumaQp) // unsigned -> signed

	pJudeSkip := [2]pJudgeSkipFun{JudgeStaticSkip, JudgeScrollSkip}
	bSkipFlag := pJudeSkip[eSkipMode](pEncCtx, pCurMb, pMbCache, pWelsMd)

	if bSkipFlag {
		bQpSimilarFlag := kiRefMbQp-kiCurMbQp <= DELTA_QP_SCD_THD || kiRefMbQp <= 26
		sVaaPredSkipMv := SMVUnitXY{0, 0}
		sCurMbMv := [2]SMVUnitXY{{0, 0}, {0, 0}}
		PredSkipMv(pMbCache, &sVaaPredSkipMv)

		if eSkipMode == SCROLLED {
			sCurMbMv[1].iMvX = int16(common.WELS_CLIP3(pVaaExt.sScrollDetectInfo.IScrollMvX,
				-pEncCtx.iMvRange, pEncCtx.iMvRange) << 2)
			sCurMbMv[1].iMvY = int16(common.WELS_CLIP3(pVaaExt.sScrollDetectInfo.IScrollMvY,
				-pEncCtx.iMvRange, pEncCtx.iMvRange) << 2)
		}

		bMbSkipFlag := sVaaPredSkipMv == sCurMbMv[eSkipMode]
		SvcMdSCDMbEnc(pEncCtx, pWelsMd, pCurMb, pMbCache, pSlice, bQpSimilarFlag, bMbSkipFlag, sCurMbMv[:], eSkipMode)

		return true
	}

	return false
}

// pVaa: void* that is the SVAAFrameInfo base of an SVAAFrameInfoExt (pVaa.pExt).
func SetBlockStaticIdcToMd(pVaa *SVAAFrameInfo, pWelsMd *SWelsMD, pCurMb *SMB, pDqLayer *SDqLayer) {
	pVaaExt := pVaa.pExt

	kiMbX := int32(pCurMb.iMbX)
	kiMbY := int32(pCurMb.iMbY)
	kiMbWidth := int32(pDqLayer.iMbWidth)
	kiWidth := kiMbWidth << 1

	kiBlockIndexUp := (kiMbY<<1)*kiWidth + (kiMbX << 1)
	kiBlockIndexLow := ((kiMbY<<1)+1)*kiWidth + (kiMbX << 1)

	//fill_blockstaticidc with pVaaExt->pVaaBestBlockStaticIdc
	pWelsMd.iBlock8x8StaticIdc[0] = int32(pVaaExt.pVaaBestBlockStaticIdc[kiBlockIndexUp])
	pWelsMd.iBlock8x8StaticIdc[1] = int32(pVaaExt.pVaaBestBlockStaticIdc[kiBlockIndexUp+1])
	pWelsMd.iBlock8x8StaticIdc[2] = int32(pVaaExt.pVaaBestBlockStaticIdc[kiBlockIndexLow])
	pWelsMd.iBlock8x8StaticIdc[3] = int32(pVaaExt.pVaaBestBlockStaticIdc[kiBlockIndexLow+1])
}

// WelsMdInterJudgeSCDPskip is the Scene Change Detection (SCD) PSkip
// Decision for screen content.
func WelsMdInterJudgeSCDPskip(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, slice *SSlice, pCurMb *SMB, pMbCache *SMbCache) bool {
	pCurDqLayer := pEncCtx.pCurDqLayer

	SetBlockStaticIdcToMd(pEncCtx.pVaa, pWelsMd, pCurMb, pCurDqLayer)

	//try static Pskip;
	if MdInterSCDPskipProcess(pEncCtx, pWelsMd, slice, pCurMb, pMbCache, STATIC) {
		return true
	}

	//try scrolled Pskip
	if MdInterSCDPskipProcess(pEncCtx, pWelsMd, slice, pCurMb, pMbCache, SCROLLED) {
		return true
	}

	return false
}

func WelsMdInterJudgeSCDPskipFalse(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, slice *SSlice, pCurMb *SMB, pMbCache *SMbCache) bool {
	return false
}

func WelsInitSCDPskipFunc(pFuncList *SWelsFuncPtrList, bScrollingDetection bool) {
	if bScrollingDetection {
		pFuncList.pfSCDPSkipDecision = WelsMdInterJudgeSCDPskip
	} else {
		pFuncList.pfSCDPSkipDecision = WelsMdInterJudgeSCDPskipFalse
	}
}

// ///////////////////////
// SubP16x16 Mode Decision for screen content
// //////////////////////
//
// func pointer of inter MD for sub16x16 INTER MD for screen content coding
func MergeSub16Me(sSrcMe0 *SWelsME, sSrcMe1 *SWelsME, pTarMe *SWelsME) {
	*pTarMe = *sSrcMe0 // confirmed_safe_unsafe_usage

	pTarMe.uiSadCost = sSrcMe0.uiSadCost + sSrcMe1.uiSadCost    //not precise cost since MVD cost is not the same
	pTarMe.uiSatdCost = sSrcMe0.uiSatdCost + sSrcMe1.uiSatdCost //not precise cost since MVD cost is not the same
}

func IsSameMv(sMv0 SMVUnitXY, sMv1 SMVUnitXY) bool {
	return (sMv0.iMvX == sMv1.iMvX) && (sMv0.iMvY == sMv1.iMvY)
}

func TryModeMerge(pMbCache *SMbCache, pWelsMd *SWelsMD, pCurMb *SMB) bool {
	pMe8x8 := &pWelsMd.sMe.sMe8x8
	bSameMv16x8_0 := IsSameMv(pMe8x8[0].sMv, pMe8x8[1].sMv)
	bSameMv16x8_1 := IsSameMv(pMe8x8[2].sMv, pMe8x8[3].sMv)

	bSameMv8x16_0 := IsSameMv(pMe8x8[0].sMv, pMe8x8[2].sMv)
	bSameMv8x16_1 := IsSameMv(pMe8x8[1].sMv, pMe8x8[3].sMv)
	//need to consider iRefIdx when multi ref is available
	bSameRefIdx16x8_0 := true //pMe8x8[0].iRefIdx == pMe8x8[1].iRefIdx;
	bSameRefIdx16x8_1 := true //pMe8x8[2].iRefIdx == pMe8x8[3].iRefIdx;
	bSameRefIdx8x16_0 := true //pMe8x8[0].iRefIdx == pMe8x8[2].iRefIdx;
	bSameRefIdx8x16_1 := true //pMe8x8[1].iRefIdx == pMe8x8[3].iRefIdx;
	iSameMv := (mdBoolToInt32(bSameMv16x8_0 && bSameRefIdx16x8_0 && bSameMv16x8_1 && bSameRefIdx16x8_1) << 1) |
		mdBoolToInt32(bSameMv8x16_0 && bSameRefIdx8x16_0 && bSameMv8x16_1 && bSameRefIdx8x16_1)

	//TODO: did not consider the MVD cost here, may consider later
	switch iSameMv {
	case 3:
		//MERGE_16x16
		//from test results of multiple sequences show that using the following 0x0F to merge 16x16
		//for some seq there is BR saving some loss
		//on the whole the BR will increase little bit
		//to save complexity we decided not to merge 16x16 at present (10/12/2012)
		//TODO: agjusted order, consider re-test later
	case 2:
		pCurMb.uiMbType = common.MB_TYPE_16x8
		MergeSub16Me(&pMe8x8[0], &pMe8x8[1], &pWelsMd.sMe.sMe16x8[0])
		MergeSub16Me(&pMe8x8[2], &pMe8x8[3], &pWelsMd.sMe.sMe16x8[1])
		PredInter16x8Mv(pMbCache, 0, 0, &pWelsMd.sMe.sMe16x8[0].sMvp)
		PredInter16x8Mv(pMbCache, 8, 0, &pWelsMd.sMe.sMe16x8[1].sMvp)
	case 1:
		pCurMb.uiMbType = common.MB_TYPE_8x16
		MergeSub16Me(&pMe8x8[0], &pMe8x8[2], &pWelsMd.sMe.sMe8x16[0])
		MergeSub16Me(&pMe8x8[1], &pMe8x8[3], &pWelsMd.sMe.sMe8x16[1])
		PredInter8x16Mv(pMbCache, 0, 0, &pWelsMd.sMe.sMe8x16[0].sMvp)
		PredInter8x16Mv(pMbCache, 4, 0, &pWelsMd.sMe.sMe8x16[1].sMvp)
	default:
	}
	return common.MB_TYPE_8x8 != pCurMb.uiMbType
}

func WelsMdInterFinePartitionVaaOnScreen(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB, iBestCost int32) {
	pMbCache := &pSlice.sMbCacheInfo
	pCurDqLayer := pEncCtx.pCurDqLayer
	var iCostP8x8 int32
	uiMbSign := pEncCtx.pFuncList.pfGetMbSignFromInterVaa(pEncCtx.pVaa.sVaaCalcInfo.PSad8x8[pCurMb.iMbXY][:])

	if MBVAASIGN_FLAT == uiMbSign {
		return
	}

	iCostP8x8 = WelsMdP8x8(pEncCtx.pFuncList, pCurDqLayer, pWelsMd, pSlice)
	if iCostP8x8 < iBestCost {
		iBestCost = iCostP8x8
		pCurMb.uiMbType = common.MB_TYPE_8x8
		setSubMbType8x8(pCurMb)
		// (the sub8x8 mode search is disabled by "#if 0" in C)
		TryModeMerge(pMbCache, pWelsMd, pCurMb)
	}
	pWelsMd.iCostLuma = iBestCost
}

// SetScrollingMvToMd
func SetScrollingMvToMd(pVaa *SVAAFrameInfo, pWelsMd *SWelsMD) {
	pVaaExt := pVaa.pExt

	var sTempMv SMVUnitXY
	sTempMv.iMvX = int16(pVaaExt.sScrollDetectInfo.IScrollMvX)
	sTempMv.iMvY = int16(pVaaExt.sScrollDetectInfo.IScrollMvY)

	pWelsMd.sMe.sMe16x16.sDirectionalMv = sTempMv
	pWelsMd.sMe.sMe8x8[0].sDirectionalMv = sTempMv
	pWelsMd.sMe.sMe8x8[1].sDirectionalMv = sTempMv
	pWelsMd.sMe.sMe8x8[2].sDirectionalMv = sTempMv
	pWelsMd.sMe.sMe8x8[3].sDirectionalMv = sTempMv
}

func SetScrollingMvToMdNull(pVaa *SVAAFrameInfo, pWelsMd *SWelsMD) {
}
