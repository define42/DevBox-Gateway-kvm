// Port of codec/encoder/core/src/svc_encode_slice.cpp.
//
// SVC slice encoding (base layer and spatial layers).

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// PWelsCodingSliceFunc is the file-local typedef of the slice coding functions.
type PWelsCodingSliceFunc func(pCtx *sWelsEncCtx, pSlice *SSlice) int32

// PWelsSliceHeaderWriteFunc is the file-local typedef of the slice header writers.
type PWelsSliceHeaderWriteFunc func(pCtx *sWelsEncCtx, pBs *common.SBitStringAux, pCurLayer *SDqLayer, pSlice *SSlice,
	pParametersetStrategy IWelsParametersetStrategy)

func sesBoolToU32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

func UpdateNonZeroCountCache(pMb *SMB, pMbCache *SMbCache) {
	copy(pMbCache.iNonZeroCoeffCount[9:13], pMb.pNonZeroCount[0:4])
	copy(pMbCache.iNonZeroCoeffCount[17:21], pMb.pNonZeroCount[4:8])
	copy(pMbCache.iNonZeroCoeffCount[25:29], pMb.pNonZeroCount[8:12])
	copy(pMbCache.iNonZeroCoeffCount[33:37], pMb.pNonZeroCount[12:16])

	copy(pMbCache.iNonZeroCoeffCount[14:16], pMb.pNonZeroCount[16:18])
	copy(pMbCache.iNonZeroCoeffCount[38:40], pMb.pNonZeroCount[18:20])
	copy(pMbCache.iNonZeroCoeffCount[22:24], pMb.pNonZeroCount[20:22])
	copy(pMbCache.iNonZeroCoeffCount[46:48], pMb.pNonZeroCount[22:24])
}

func WelsSliceHeaderScalExtInit(pCurLayer *SDqLayer, pSlice *SSlice) {
	pSliceHeadExt := &pSlice.sSliceHeaderExt
	pNalHeadExt := &pCurLayer.sLayerInfo.sNalHeaderExt

	uiDependencyId := pNalHeadExt.UiDependencyId

	pSliceHeadExt.bSliceSkipFlag = false

	if uiDependencyId > 0 { //spatial EL
		//bothe adaptive and default flags should equal to 0.
		pSliceHeadExt.bAdaptiveBaseModeFlag = false
		pSliceHeadExt.bAdaptiveMotionPredFlag = false
		pSliceHeadExt.bAdaptiveResidualPredFlag = false

		pSliceHeadExt.bDefaultBaseModeFlag = false
		pSliceHeadExt.bDefaultMotionPredFlag = false
		pSliceHeadExt.bDefaultResidualPredFlag = false
	}
}

func WelsSliceHeaderExtInit(pEncCtx *sWelsEncCtx, pCurLayer *SDqLayer, pSlice *SSlice) {
	pCurSliceExt := &pSlice.sSliceHeaderExt
	pCurSliceHeader := &pCurSliceExt.sSliceHeader
	pParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
	pCurSliceHeader.eSliceType = pEncCtx.eSliceType

	pCurSliceExt.bStoreRefBasePicFlag = false

	pCurSliceHeader.iFrameNum = pParamInternal.iFrameNum
	pCurSliceHeader.uiIdrPicId = pParamInternal.uiIdrPicId
	pCurSliceHeader.iPicOrderCntLsb = pEncCtx.pEncPic.iFramePoc // 0

	if common.P_SLICE == pEncCtx.eSliceType {
		pCurSliceHeader.uiNumRefIdxL0Active = 1
		if pCurSliceHeader.uiRefCount > 0 &&
			int32(pCurSliceHeader.uiRefCount) <= int32(pCurLayer.sLayerInfo.pSpsP.iNumRefFrames) {
			pCurSliceHeader.bNumRefIdxActiveOverrideFlag = true
			pCurSliceHeader.uiNumRefIdxL0Active = pCurSliceHeader.uiRefCount
		} else {
			//to solve mismatch between debug&release
			pCurSliceHeader.bNumRefIdxActiveOverrideFlag = false
		}
	}

	pCurSliceHeader.iSliceQpDelta = int8(pEncCtx.iGlobalQp - int32(pCurLayer.sLayerInfo.pPpsP.iPicInitQp))

	//for deblocking initial
	pCurSliceHeader.uiDisableDeblockingFilterIdc = pCurLayer.iLoopFilterDisableIdc
	pCurSliceHeader.iSliceAlphaC0Offset =
		pCurLayer.iLoopFilterAlphaC0Offset // need update iSliceAlphaC0Offset & iSliceBetaOffset for pSlice-header if loop_filter_idc != 1
	pCurSliceHeader.iSliceBetaOffset = pCurLayer.iLoopFilterBetaOffset
	pCurSliceExt.uiDisableInterLayerDeblockingFilterIdc = pCurLayer.uiDisableInterLayerDeblockingFilterIdc

	if pSlice.bSliceHeaderExtFlag {
		WelsSliceHeaderScalExtInit(pCurLayer, pSlice)
	} else {
		//both adaptive and default flags should equal to 0.
		pCurSliceExt.bAdaptiveBaseModeFlag = false
		pCurSliceExt.bAdaptiveMotionPredFlag = false
		pCurSliceExt.bAdaptiveResidualPredFlag = false

		pCurSliceExt.bDefaultBaseModeFlag = false
		pCurSliceExt.bDefaultMotionPredFlag = false
		pCurSliceExt.bDefaultResidualPredFlag = false
	}
}

func UpdateMbNeighbor(pCurDq *SDqLayer, pMb *SMB, kiMbWidth int32, uiSliceIdc uint16) {
	uiNeighborAvailFlag := uint32(0)
	kiMbXY := pMb.iMbXY
	kiMbX := int32(pMb.iMbX)
	kiMbY := int32(pMb.iMbY)
	var bLeft, bTop, bLeftTop, bRightTop bool
	var iLeftXY, iTopXY, iLeftTopXY, iRightTopXY int32

	pMb.uiSliceIdc = uiSliceIdc
	iLeftXY = kiMbXY - 1
	iTopXY = kiMbXY - kiMbWidth
	iLeftTopXY = iTopXY - 1
	iRightTopXY = iTopXY + 1

	bLeft = (kiMbX > 0) && (uiSliceIdc == WelsMbToSliceIdc(pCurDq, iLeftXY))
	bTop = (kiMbY > 0) && (uiSliceIdc == WelsMbToSliceIdc(pCurDq, iTopXY))
	bLeftTop = (kiMbX > 0) && (kiMbY > 0) && (uiSliceIdc == WelsMbToSliceIdc(pCurDq, iLeftTopXY))
	bRightTop = (kiMbX < (kiMbWidth - 1)) && (kiMbY > 0) && (uiSliceIdc == WelsMbToSliceIdc(pCurDq, iRightTopXY))

	if bLeft {
		uiNeighborAvailFlag |= LEFT_MB_POS
	}
	if bTop {
		uiNeighborAvailFlag |= TOP_MB_POS
	}
	if bLeftTop {
		uiNeighborAvailFlag |= TOPLEFT_MB_POS
	}
	if bRightTop {
		uiNeighborAvailFlag |= TOPRIGHT_MB_POS
	}
	pMb.uiNeighborAvail = uint8(uiNeighborAvailFlag)
}

// WriteReferenceReorder writes reference picture list on reordering syntax in Slice header.
func WriteReferenceReorder(pBs *common.SBitStringAux, sSliceHeader *SSliceHeader) {
	pRefOrdering := &sSliceHeader.sRefReordering
	eSliceType := uint8(sSliceHeader.eSliceType % 5)
	n := 0

	if common.I_SLICE != eSliceType && common.SI_SLICE != eSliceType { // !I && !SI
		common.BsWriteOneBit(pBs, 1)
		var uiReorderingOfPicNumsIdc uint16
		for {
			uiReorderingOfPicNumsIdc = pRefOrdering.SReorderingSyntax[n].uiReorderingOfPicNumsIdc
			common.BsWriteUE(pBs, uint32(uiReorderingOfPicNumsIdc))
			if 0 == uiReorderingOfPicNumsIdc || 1 == uiReorderingOfPicNumsIdc {
				common.BsWriteUE(pBs, pRefOrdering.SReorderingSyntax[n].uiAbsDiffPicNumMinus1)
			} else if 2 == uiReorderingOfPicNumsIdc {
				common.BsWriteUE(pBs, uint32(pRefOrdering.SReorderingSyntax[n].iLongTermPicNum))
			}

			n++
			if 3 == uiReorderingOfPicNumsIdc {
				break
			}
		}
	}
}

// WriteRefPicMarking writes reference picture marking syntax in pSlice header.
func WriteRefPicMarking(pBs *common.SBitStringAux, pSliceHeader *SSliceHeader, pNalHdrExt *common.SNalUnitHeaderExt) {
	sRefMarking := &pSliceHeader.sRefMarking
	n := 0

	if pNalHdrExt.BIdrFlag {
		common.BsWriteOneBit(pBs, sesBoolToU32(sRefMarking.bNoOutputOfPriorPicsFlag))
		common.BsWriteOneBit(pBs, sesBoolToU32(sRefMarking.bLongTermRefFlag))
	} else {
		common.BsWriteOneBit(pBs, sesBoolToU32(sRefMarking.bAdaptiveRefPicMarkingModeFlag))

		if sRefMarking.bAdaptiveRefPicMarkingModeFlag {
			var iMmcoType int32
			for {
				iMmcoType = sRefMarking.SMmcoRef[n].iMmcoType
				common.BsWriteUE(pBs, uint32(iMmcoType))
				if 1 == iMmcoType || 3 == iMmcoType {
					common.BsWriteUE(pBs, uint32(sRefMarking.SMmcoRef[n].iDiffOfPicNum-1))
				}

				if 2 == iMmcoType {
					common.BsWriteUE(pBs, uint32(sRefMarking.SMmcoRef[n].iLongTermPicNum))
				}

				if 3 == iMmcoType || 6 == iMmcoType {
					common.BsWriteUE(pBs, uint32(sRefMarking.SMmcoRef[n].iLongTermFrameIdx))
				}

				if 4 == iMmcoType {
					common.BsWriteUE(pBs, uint32(sRefMarking.SMmcoRef[n].iMaxLongTermFrameIdx+1))
				}

				n++
				if 0 == iMmcoType {
					break
				}
			}
		}
	}
}

func WelsSliceHeaderWrite(pCtx *sWelsEncCtx, pBs *common.SBitStringAux, pCurLayer *SDqLayer, pSlice *SSlice, pParametersetStrategy IWelsParametersetStrategy) {
	pSps := pCurLayer.sLayerInfo.pSpsP
	pPps := pCurLayer.sLayerInfo.pPpsP
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	pNalHead := &pCurLayer.sLayerInfo.sNalHeaderExt

	common.BsWriteUE(pBs, uint32(pSliceHeader.iFirstMbInSlice))
	common.BsWriteUE(pBs, uint32(pSliceHeader.eSliceType)) /* same type things */

	common.BsWriteUE(pBs, pSliceHeader.pPps.iPpsId+uint32(pParametersetStrategy.GetPpsIdOffset(int32(pSliceHeader.pPps.iPpsId))))

	common.BsWriteBits(pBs, int32(pSps.uiLog2MaxFrameNum), uint32(pSliceHeader.iFrameNum))

	if pNalHead.BIdrFlag { /* NAL IDR */
		common.BsWriteUE(pBs, uint32(pSliceHeader.uiIdrPicId))
	}

	if pSps.uiPocType == 0 {
		common.BsWriteBits(pBs, pSps.iLog2MaxPocLsb, uint32(pSliceHeader.iPicOrderCntLsb))
	} else if pSps.uiPocType == 1 {
		// TODO: implement.
		// assert (0)
	} else {
		// no-op for uiPocType == 2.
	}

	if common.P_SLICE == pSliceHeader.eSliceType {
		common.BsWriteOneBit(pBs, sesBoolToU32(pSliceHeader.bNumRefIdxActiveOverrideFlag))
		if pSliceHeader.bNumRefIdxActiveOverrideFlag {
			common.BsWriteUE(pBs, uint32(common.WELS_CLIP3(int32(pSliceHeader.uiNumRefIdxL0Active)-1, 0, MAX_REF_PIC_COUNT)))
		}
	}

	if !pNalHead.BIdrFlag {
		WriteReferenceReorder(pBs, pSliceHeader)
	}

	if pNalHead.SNalUnitHeader.UiNalRefIdc != 0 {
		WriteRefPicMarking(pBs, pSliceHeader, pNalHead)
	}

	if pPps.bEntropyCodingModeFlag && pSliceHeader.eSliceType != common.I_SLICE {
		common.BsWriteUE(pBs, uint32(pSlice.iCabacInitIdc))
	}
	common.BsWriteSE(pBs, int32(pSliceHeader.iSliceQpDelta)) /* pSlice qp delta */

	if pPps.bDeblockingFilterControlPresentFlag {
		switch pSliceHeader.uiDisableDeblockingFilterIdc {
		case 0, 3, 4, 6:
			common.BsWriteUE(pBs, 0)
		case 1:
			common.BsWriteUE(pBs, 1)
		case 2, 5:
			common.BsWriteUE(pBs, 2)
		default:
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "Invalid uiDisableDeblockingFilterIdc %d",
				pSliceHeader.uiDisableDeblockingFilterIdc)
		}
		if 1 != pSliceHeader.uiDisableDeblockingFilterIdc {
			common.BsWriteSE(pBs, int32(pSliceHeader.iSliceAlphaC0Offset)>>1)
			common.BsWriteSE(pBs, int32(pSliceHeader.iSliceBetaOffset)>>1)
		}
	}
}

func WelsSliceHeaderExtWrite(pCtx *sWelsEncCtx, pBs *common.SBitStringAux, pCurLayer *SDqLayer, pSlice *SSlice, pParametersetStrategy IWelsParametersetStrategy) {
	pSps := pCurLayer.sLayerInfo.pSpsP
	pPps := pCurLayer.sLayerInfo.pPpsP
	pSubSps := pCurLayer.sLayerInfo.pSubsetSpsP
	pSliceHeadExt := &pSlice.sSliceHeaderExt
	pSliceHeader := &pSliceHeadExt.sSliceHeader
	pNalHead := &pCurLayer.sLayerInfo.sNalHeaderExt

	common.BsWriteUE(pBs, uint32(pSliceHeader.iFirstMbInSlice))
	common.BsWriteUE(pBs, uint32(pSliceHeader.eSliceType)) /* same type things */

	common.BsWriteUE(pBs, pSliceHeader.pPps.iPpsId+
		uint32(pParametersetStrategy.GetPpsIdOffset(int32(pSliceHeader.pPps.iPpsId))))

	common.BsWriteBits(pBs, int32(pSps.uiLog2MaxFrameNum), uint32(pSliceHeader.iFrameNum))

	if pNalHead.BIdrFlag { /* NAL IDR */
		common.BsWriteUE(pBs, uint32(pSliceHeader.uiIdrPicId))
	}

	if pSps.uiPocType == 0 {
		common.BsWriteBits(pBs, pSps.iLog2MaxPocLsb, uint32(pSliceHeader.iPicOrderCntLsb))
	} else if pSps.uiPocType == 1 {
		// TODO: implement.
		// assert (0)
	} else {
		// no-op for uiPocType == 2.
	}

	if common.P_SLICE == pSliceHeader.eSliceType {
		common.BsWriteOneBit(pBs, sesBoolToU32(pSliceHeader.bNumRefIdxActiveOverrideFlag))
		if pSliceHeader.bNumRefIdxActiveOverrideFlag {
			common.BsWriteUE(pBs, uint32(common.WELS_CLIP3(int32(pSliceHeader.uiNumRefIdxL0Active)-1, 0, MAX_REF_PIC_COUNT)))
		}
	}

	if !pNalHead.BIdrFlag {
		WriteReferenceReorder(pBs, pSliceHeader)
	}

	if pNalHead.SNalUnitHeader.UiNalRefIdc != 0 {
		WriteRefPicMarking(pBs, pSliceHeader, pNalHead)

		if !pSubSps.sSpsSvcExt.bSliceHeaderRestrictionFlag {
			common.BsWriteOneBit(pBs, sesBoolToU32(pSliceHeadExt.bStoreRefBasePicFlag))
		}
	}

	if pPps.bEntropyCodingModeFlag && pSliceHeader.eSliceType != common.I_SLICE {
		common.BsWriteUE(pBs, uint32(pSlice.iCabacInitIdc))
	}

	common.BsWriteSE(pBs, int32(pSliceHeader.iSliceQpDelta)) /* pSlice qp delta */

	if pPps.bDeblockingFilterControlPresentFlag {
		common.BsWriteUE(pBs, uint32(pSliceHeader.uiDisableDeblockingFilterIdc))
		if 1 != pSliceHeader.uiDisableDeblockingFilterIdc {
			common.BsWriteSE(pBs, int32(pSliceHeader.iSliceAlphaC0Offset)>>1)
			common.BsWriteSE(pBs, int32(pSliceHeader.iSliceBetaOffset)>>1)
		}
	}

	// DISABLE_FMO_FEATURE is defined (as264_common.h): no slice group change cycle.

	// if (false) { ... slice skip / adaptive prediction flags ... } : dead code in C.

	if !pSubSps.sSpsSvcExt.bSliceHeaderRestrictionFlag {
		common.BsWriteBits(pBs, 4, 0)
		common.BsWriteBits(pBs, 4, 15)
	}
}

// WelsInterMbEncode: only BaseLayer inter MB and SpatialLayer (uiQualityId = 0)
// inter MB calling this pFunc. only for inter part
func WelsInterMbEncode(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) {
	pMbCache := &pSlice.sMbCacheInfo

	WelsDctMb(pMbCache.pCoeffLevel, pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0], pEncCtx.pCurDqLayer.iEncStride[0],
		pMbCache.pMemPredLuma, 0, pEncCtx.pFuncList.pfDctFourT4)
	WelsEncInterY(pEncCtx.pFuncList, pCurMb, pMbCache)
}

// WelsIMbChromaEncode: only BaseLayer inter MB and SpatialLayer (uiQualityId = 0)
// inter MB calling this pFunc. only for I SSlice
func WelsIMbChromaEncode(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache) {
	pFunc := pEncCtx.pFuncList
	pCurLayer := pEncCtx.pCurDqLayer
	kiEncStride := pCurLayer.iEncStride[1]
	kiCsStride := pCurLayer.iCsStride[1]
	pCurRS := pMbCache.pCoeffLevel
	pBestPred := pMbCache.pBestPredIntraChroma
	pCsCb, iCsCbOff := pMbCache.SPicData.pCsMb[1], pMbCache.SPicData.iCsMbOff[1]
	pCsCr, iCsCrOff := pMbCache.SPicData.pCsMb[2], pMbCache.SPicData.iCsMbOff[2]

	//cb
	pFunc.pfDctFourT4(pCurRS, pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1], kiEncStride, pBestPred, 0, 8)
	WelsEncRecUV(pFunc, pCurMb, pMbCache, pCurRS, 1)
	pFunc.pfIDctFourT4(pCsCb, iCsCbOff, kiCsStride, pBestPred, 0, 8, pCurRS)

	//cr
	pFunc.pfDctFourT4(pCurRS[64:], pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2], kiEncStride, pBestPred, 64, 8)
	WelsEncRecUV(pFunc, pCurMb, pMbCache, pCurRS[64:], 2)
	pFunc.pfIDctFourT4(pCsCr, iCsCrOff, kiCsStride, pBestPred, 64, 8, pCurRS[64:])
}

// WelsPMbChromaEncode: only BaseLayer inter MB and SpatialLayer (uiQualityId = 0)
// inter MB calling this pFunc. for P SSlice (intra part + inter part)
func WelsPMbChromaEncode(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) {
	pFunc := pEncCtx.pFuncList
	pCurLayer := pEncCtx.pCurDqLayer
	kiEncStride := pCurLayer.iEncStride[1]
	pMbCache := &pSlice.sMbCacheInfo
	pCurRS := pMbCache.pCoeffLevel[256:]
	pBestPred := pMbCache.pMemPredChroma

	pFunc.pfDctFourT4(pCurRS, pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1], kiEncStride, pBestPred, 0, 8)
	pFunc.pfDctFourT4(pCurRS[64:], pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2], kiEncStride, pBestPred, 64, 8)

	WelsEncRecUV(pFunc, pCurMb, pMbCache, pCurRS, 1)
	WelsEncRecUV(pFunc, pCurMb, pMbCache, pCurRS[64:], 2)
}

func OutputPMbWithoutConstructCsRsNoCopy(pCtx *sWelsEncCtx, pDq *SDqLayer, pSlice *SSlice, pMb *SMB) {
	if (common.IS_INTER(pMb.uiMbType) && !common.IS_SKIP(pMb.uiMbType)) ||
		common.IS_I_BL(pMb.uiMbType) { //intra have been reconstructed, NO COPY from CS to pDecPic--
		pMbCache := &pSlice.sMbCacheInfo
		pDecY, iDecYOff := pMbCache.SPicData.pDecMb[0], pMbCache.SPicData.iDecMbOff[0]
		pDecU, iDecUOff := pMbCache.SPicData.pDecMb[1], pMbCache.SPicData.iDecMbOff[1]
		pDecV, iDecVOff := pMbCache.SPicData.pDecMb[2], pMbCache.SPicData.iDecMbOff[2]
		pScaledTcoeff := pMbCache.pCoeffLevel
		kiDecStrideLuma := pDq.pDecPic.iLineSize[0]
		kiDecStrideChroma := pDq.pDecPic.iLineSize[1]
		pfIdctFour4x4 := pCtx.pFuncList.pfIDctFourT4

		WelsIDctT4RecOnMb(pDecY, iDecYOff, kiDecStrideLuma, pDecY, iDecYOff, kiDecStrideLuma, pScaledTcoeff, pfIdctFour4x4)
		pfIdctFour4x4(pDecU, iDecUOff, kiDecStrideChroma, pDecU, iDecUOff, kiDecStrideChroma, pScaledTcoeff[256:])
		pfIdctFour4x4(pDecV, iDecVOff, kiDecStrideChroma, pDecV, iDecVOff, kiDecStrideChroma, pScaledTcoeff[320:])
	}
}

func UpdateQpForOverflow(pCurMb *SMB, kuiChromaQpIndexOffset uint8) {
	pCurMb.uiLumaQp += DELTA_QP
	pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+int32(kuiChromaQpIndexOffset))]
}

// WelsISliceMdEnc is for intra non-dynamic pSlice.
// encapsulate two kinds of reconstruction:
// first. store base or highest Dependency Layer with only one quality (without CS RS reconstruction)
// second. lower than highest Dependency Layer, and for every Dependency Layer with one quality layer(single layer)
func WelsISliceMdEnc(pEncCtx *sWelsEncCtx, pSlice *SSlice) int32 { //pMd + encoding
	pCurLayer := pEncCtx.pCurDqLayer
	pMbCache := &pSlice.sMbCacheInfo
	pSliceHdExt := &pSlice.sSliceHeaderExt
	pMbList := pCurLayer.sMbDataP
	var pCurMb *SMB
	kiSliceFirstMbXY := pSliceHdExt.sSliceHeader.iFirstMbInSlice
	iNextMbIdx := kiSliceFirstMbXY
	kiTotalNumMb := int32(pCurLayer.iMbWidth) * int32(pCurLayer.iMbHeight)
	iCurMbIdx, iNumMbCoded := int32(0), int32(0)
	kuiChromaQpIndexOffset := pCurLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset

	var sMd SWelsMD
	iEncReturn := int32(ENC_RETURN_SUCCESS)
	var sDss SDynamicSlicingStack
	if pEncCtx.pSvcParam.IEntropyCodingModeFlag != 0 {
		WelsInitSliceCabac(pEncCtx, pSlice)
		sDss.pRestoreBuffer = nil
		sDss.iStartPos = 0
		sDss.iCurrentPos = 0
	}
	for {
		if pEncCtx.pSvcParam.IEntropyCodingModeFlag == 0 {
			pEncCtx.pFuncList.pfStashMBStatus(&sDss, pSlice, 0)
		}
		iCurMbIdx = iNextMbIdx
		pCurMb = &pMbList[iCurMbIdx]

		pEncCtx.pFuncList.pfRc.pfWelsRcMbInit(pEncCtx, pCurMb, pSlice)
		WelsMdIntraInit(pEncCtx, pCurMb, pMbCache, kiSliceFirstMbXY)

		for { // TRY_REENCODING:
			sMd.iLambda = g_kiQpCostTable[pCurMb.uiLumaQp]
			WelsMdIntraMb(pEncCtx, &sMd, pCurMb, pMbCache)
			UpdateNonZeroCountCache(pCurMb, pMbCache)

			iEncReturn = pEncCtx.pFuncList.pfWelsSpatialWriteMbSyn(pEncCtx, pSlice, pCurMb)
			if pEncCtx.pSvcParam.IEntropyCodingModeFlag == 0 {
				if (iEncReturn == ENC_RETURN_VLCOVERFLOWFOUND) && (pCurMb.uiLumaQp < 50) {
					pEncCtx.pFuncList.pfStashPopMBStatus(&sDss, pSlice)
					UpdateQpForOverflow(pCurMb, kuiChromaQpIndexOffset)
					continue // goto TRY_REENCODING
				}
			}
			break
		}
		if ENC_RETURN_SUCCESS != iEncReturn {
			return iEncReturn
		}

		// uiSliceIdc and neighbour availability are initialized for the whole
		// picture before workers start. Rewriting the ID here races with
		// deblocking in an already completed slice.
		pEncCtx.pFuncList.pfMdBackgroundInfoUpdate(pCurLayer, pCurMb, pMbCache.bCollocatedPredFlag, common.I_SLICE)
		pEncCtx.pFuncList.pfRc.pfWelsRcMbInfoUpdate(pEncCtx, pCurMb, sMd.iCostLuma, pSlice)

		iNumMbCoded++
		iNextMbIdx = WelsGetNextMbOfSlice(pCurLayer, iCurMbIdx)
		if iNextMbIdx == -1 || iNextMbIdx >= kiTotalNumMb || iNumMbCoded >= kiTotalNumMb {
			break
		}
	}

	return ENC_RETURN_SUCCESS
}

// WelsISliceMdEncDynamic is only for intra dynamic slicing.
func WelsISliceMdEncDynamic(pEncCtx *sWelsEncCtx, pSlice *SSlice) int32 { //pMd + encoding
	pBs := pSlice.pSliceBsa
	pCurLayer := pEncCtx.pCurDqLayer
	pSliceCtx := &pCurLayer.sSliceEncCtx
	pMbCache := &pSlice.sMbCacheInfo
	pSliceHdExt := &pSlice.sSliceHeaderExt
	pMbList := pCurLayer.sMbDataP
	var pCurMb *SMB
	kiSliceFirstMbXY := pSliceHdExt.sSliceHeader.iFirstMbInSlice
	iNextMbIdx := kiSliceFirstMbXY
	kiTotalNumMb := int32(pCurLayer.iMbWidth) * int32(pCurLayer.iMbHeight)
	iCurMbIdx, iNumMbCoded := int32(0), int32(0)
	kiSliceIdx := pSlice.iSliceIdx
	kiPartitionId := (kiSliceIdx % int32(pEncCtx.iActiveThreadsNum))
	kuiChromaQpIndexOffset := pCurLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset
	iEncReturn := int32(ENC_RETURN_SUCCESS)

	var sMd SWelsMD
	var sDss SDynamicSlicingStack
	if pEncCtx.pSvcParam.IEntropyCodingModeFlag != 0 {
		WelsInitSliceCabac(pEncCtx, pSlice)
		sDss.pRestoreBuffer = pEncCtx.pDynamicBsBuffer[kiPartitionId]
		sDss.iStartPos = 0
		sDss.iCurrentPos = 0
	} else {
		sDss.iStartPos = BsGetBitsPos(pBs)
	}
	for {
		iCurMbIdx = iNextMbIdx
		pCurMb = &pMbList[iCurMbIdx]

		pEncCtx.pFuncList.pfStashMBStatus(&sDss, pSlice, 0)
		pEncCtx.pFuncList.pfRc.pfWelsRcMbInit(pEncCtx, pCurMb, pSlice)
		// if already reaches the largest number of slices, set QPs to the upper bound
		if pSlice.bDynamicSlicingSliceSizeCtrlFlag {
			pCurMb.uiLumaQp = uint8(pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId].iMaxQp)
			pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+int32(kuiChromaQpIndexOffset))]
		}
		WelsMdIntraInit(pEncCtx, pCurMb, pMbCache, kiSliceFirstMbXY)

		for { // TRY_REENCODING:
			sMd.iLambda = g_kiQpCostTable[pCurMb.uiLumaQp]
			WelsMdIntraMb(pEncCtx, &sMd, pCurMb, pMbCache)
			UpdateNonZeroCountCache(pCurMb, pMbCache)

			iEncReturn = pEncCtx.pFuncList.pfWelsSpatialWriteMbSyn(pEncCtx, pSlice, pCurMb)
			if iEncReturn == ENC_RETURN_VLCOVERFLOWFOUND && (pCurMb.uiLumaQp < 50) {
				pEncCtx.pFuncList.pfStashPopMBStatus(&sDss, pSlice)
				UpdateQpForOverflow(pCurMb, kuiChromaQpIndexOffset)
				continue // goto TRY_REENCODING
			}
			break
		}
		if ENC_RETURN_SUCCESS != iEncReturn {
			return iEncReturn
		}

		sDss.iCurrentPos = pEncCtx.pFuncList.pfGetBsPosition(pSlice)

		if DynSlcJudgeSliceBoundaryStepBack(pEncCtx, pSlice, pSliceCtx, pCurMb, &sDss) { //islice
			pEncCtx.pFuncList.pfStashPopMBStatus(&sDss, pSlice)
			pCurLayer.LastCodedMbIdxOfPartition[kiPartitionId] = iCurMbIdx -
				1 // update LastCodedMbIdxOfPartition, need to -1 due to stepping back
			pCurLayer.NumSliceCodedOfPartition[kiPartitionId]++

			break
		}

		pCurMb.uiSliceIdc = uint16(kiSliceIdx)

		pEncCtx.pFuncList.pfRc.pfWelsRcMbInfoUpdate(pEncCtx, pCurMb, sMd.iCostLuma, pSlice)

		iNumMbCoded++

		iNextMbIdx = WelsGetNextMbOfSlice(pCurLayer, iCurMbIdx)
		//whether all of MB in current pSlice encoded or not
		if iNextMbIdx == -1 || iNextMbIdx >= kiTotalNumMb || iNumMbCoded >= kiTotalNumMb {
			pSlice.iCountMbNumInSlice = iCurMbIdx - pCurLayer.LastCodedMbIdxOfPartition[kiPartitionId]
			pCurLayer.LastCodedMbIdxOfPartition[kiPartitionId] = iCurMbIdx
			pCurLayer.NumSliceCodedOfPartition[kiPartitionId]++

			break
		}
	}
	return iEncReturn
}

// WelsPSliceMdEnc encapsulates two kinds of reconstruction:
// first. store base or highest Dependency Layer with only one quality (without CS RS reconstruction)
// second. lower than highest Dependency Layer, and for every Dependency Layer with one quality layer(single layer)
func WelsPSliceMdEnc(pEncCtx *sWelsEncCtx, pSlice *SSlice, kbIsHighestDlayerFlag bool, pfInterMd PInterMdFunc) int32 { //pMd + encoding
	kpShExt := &pSlice.sSliceHeaderExt
	kpSh := &kpShExt.sSliceHeader
	kiSliceFirstMbXY := kpSh.iFirstMbInSlice
	var sMd SWelsMD

	sMd.uiRef = kpSh.uiRefIndex
	sMd.bMdUsingSad = (pEncCtx.pSvcParam.IComplexityMode == api.LOW_COMPLEXITY)
	if !pEncCtx.pCurDqLayer.bBaseLayerAvailableFlag || !kbIsHighestDlayerFlag {
		sMd.sMe = SWelsMDMe{}
	}

	//pMb loop
	return WelsMdInterMbLoop(pEncCtx, pSlice, &sMd, kiSliceFirstMbXY, pfInterMd)
}

func WelsPSliceMdEncDynamic(pEncCtx *sWelsEncCtx, pSlice *SSlice, kbIsHighestDlayerFlag bool, pfInterMd PInterMdFunc) int32 {
	kpShExt := &pSlice.sSliceHeaderExt
	kpSh := &kpShExt.sSliceHeader
	kiSliceFirstMbXY := kpSh.iFirstMbInSlice
	var sMd SWelsMD

	sMd.uiRef = kpSh.uiRefIndex
	sMd.bMdUsingSad = (pEncCtx.pSvcParam.IComplexityMode == api.LOW_COMPLEXITY)
	if !pEncCtx.pCurDqLayer.bBaseLayerAvailableFlag || !kbIsHighestDlayerFlag {
		sMd.sMe = SWelsMDMe{}
	}

	//mb loop
	return WelsMdInterMbLoopOverDynamicSlice(pEncCtx, pSlice, &sMd, kiSliceFirstMbXY, pfInterMd)
}

func WelsCodePSlice(pEncCtx *sWelsEncCtx, pSlice *SSlice) int32 {
	//pSlice-level init should be outside and before this function
	pCurLayer := pEncCtx.pCurDqLayer

	kbBaseAvail := pCurLayer.bBaseLayerAvailableFlag
	kbHighestSpatial := pEncCtx.pSvcParam.ISpatialLayerNum ==
		(int32(pCurLayer.sLayerInfo.sNalHeaderExt.UiDependencyId) + 1)

	pfInterMd := PInterMdFunc(WelsMdInterMb)
	if kbBaseAvail && kbHighestSpatial {
		pfInterMd = WelsMdInterMbEnhancelayer
	}
	return WelsPSliceMdEnc(pEncCtx, pSlice, kbHighestSpatial, pfInterMd)
}

func WelsCodePOverDynamicSlice(pEncCtx *sWelsEncCtx, pSlice *SSlice) int32 {
	//pSlice-level init should be outside and before this function
	pCurLayer := pEncCtx.pCurDqLayer

	kbBaseAvail := pCurLayer.bBaseLayerAvailableFlag
	kbHighestSpatial := pEncCtx.pSvcParam.ISpatialLayerNum ==
		(int32(pCurLayer.sLayerInfo.sNalHeaderExt.UiDependencyId) + 1)

	pfInterMd := PInterMdFunc(WelsMdInterMb)
	if kbBaseAvail && kbHighestSpatial {
		pfInterMd = WelsMdInterMbEnhancelayer
	}
	return WelsPSliceMdEncDynamic(pEncCtx, pSlice, kbHighestSpatial, pfInterMd)
}

// 1st index: 0: for P pSlice; 1: for I pSlice;
// 2nd index: 0: for non-dynamic pSlice; 1: for dynamic I pSlice;
var g_pWelsSliceCoding = [2][2]PWelsCodingSliceFunc{
	{WelsCodePSlice, WelsCodePOverDynamicSlice}, // P SSlice
	{WelsISliceMdEnc, WelsISliceMdEncDynamic},   // I SSlice
}

var g_pWelsWriteSliceHeader = [2]PWelsSliceHeaderWriteFunc{ // 0: for base; 1: for ext;
	WelsSliceHeaderWrite,
	WelsSliceHeaderExtWrite,
}

// AllocMbCacheAligned allocates slice's MB cache buffer.
// CMemoryAlign* pMa dropped (also below).
func AllocMbCacheAligned(pMbCache *SMbCache) int32 {
	pMbCache.pMemPredMb = make([]uint8, 2*256)

	pMbCache.pCoeffLevel = make([]int16, common.MB_COEFF_LIST_SIZE)
	pMbCache.pSkipMb = make([]uint8, 384)
	pMbCache.pMemPredBlk4 = make([]uint8, 2*16)
	pMbCache.pBufferInterPredMe = make([]uint8, 4*640)
	pMbCache.pPrevIntra4x4PredModeFlag = make([]bool, 16)
	pMbCache.pRemIntra4x4PredModeFlag = make([]int8, 16)
	pMbCache.pDct = new(SDCTCoeff)

	return 0
}

// FreeMbCache frees slice's MB cache buffer.
func FreeMbCache(pMbCache *SMbCache) {
	pMbCache.pCoeffLevel = nil
	pMbCache.pMemPredMb = nil
	pMbCache.pSkipMb = nil
	pMbCache.pMemPredBlk4 = nil
	pMbCache.pBufferInterPredMe = nil
	pMbCache.pPrevIntra4x4PredModeFlag = nil
	pMbCache.pRemIntra4x4PredModeFlag = nil
	pMbCache.pDct = nil
}

// InitSliceBoundaryInfo initializes slice's boundary info.
func InitSliceBoundaryInfo(pCurLayer *SDqLayer, pSliceArgument *api.SSliceArgument, kiSliceNumInFrame int32) int32 {
	kpSlicesAssignList := &pSliceArgument.UiSliceMbNum // C: viewed as int32_t*
	kiMBWidth := int32(pCurLayer.iMbWidth)
	kiMBHeight := int32(pCurLayer.iMbHeight)
	kiCountNumMbInFrame := kiMBWidth * kiMBHeight
	iSliceIdx := int32(0)
	iFirstMBInSlice := int32(0)
	iMbNumInSlice := int32(0)

	for ; iSliceIdx < kiSliceNumInFrame; iSliceIdx++ {
		if api.SM_SINGLE_SLICE == pSliceArgument.UiSliceMode {
			iFirstMBInSlice = 0
			iMbNumInSlice = kiCountNumMbInFrame
		} else if (api.SM_RASTER_SLICE == pSliceArgument.UiSliceMode) && (0 == pSliceArgument.UiSliceMbNum[0]) {
			iFirstMBInSlice = iSliceIdx * kiMBWidth
			iMbNumInSlice = kiMBWidth
		} else if api.SM_RASTER_SLICE == pSliceArgument.UiSliceMode ||
			api.SM_FIXEDSLCNUM_SLICE == pSliceArgument.UiSliceMode {
			iMbIdx := int32(0)
			for i := int32(0); i < iSliceIdx; i++ {
				iMbIdx += int32(kpSlicesAssignList[i])
			}

			if iMbIdx >= kiCountNumMbInFrame {
				return ENC_RETURN_UNEXPECTED
			}

			iFirstMBInSlice = iMbIdx
			iMbNumInSlice = int32(kpSlicesAssignList[iSliceIdx])
		} else if api.SM_SIZELIMITED_SLICE == pSliceArgument.UiSliceMode {
			iFirstMBInSlice = 0
			iMbNumInSlice = kiCountNumMbInFrame
		} else { // any else uiSliceMode?
			// assert (0)
		}

		pCurLayer.pCountMbNumInSlice[iSliceIdx] = iMbNumInSlice
		pCurLayer.pFirstMbIdxOfSlice[iSliceIdx] = iFirstMBInSlice
	}

	return ENC_RETURN_SUCCESS
}

func SetSliceBoundaryInfo(pCurLayer *SDqLayer, pSlice *SSlice, kiSliceIdx int32) int32 {
	if nil == pCurLayer || nil == pSlice ||
		nil == pCurLayer.pFirstMbIdxOfSlice ||
		nil == pCurLayer.pCountMbNumInSlice {
		return ENC_RETURN_UNEXPECTED
	}

	pSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice = pCurLayer.pFirstMbIdxOfSlice[kiSliceIdx]
	pSlice.iCountMbNumInSlice = pCurLayer.pCountMbNumInSlice[kiSliceIdx]

	return ENC_RETURN_SUCCESS
}

// AllocateSliceMBBuffer allocates slice's MB info buffer.
func AllocateSliceMBBuffer(pSlice *SSlice) int32 {
	if AllocMbCacheAligned(&pSlice.sMbCacheInfo) != 0 {
		return ENC_RETURN_MEMALLOCERR
	}

	return ENC_RETURN_SUCCESS
}

// InitSliceBsBuffer initializes slice bs buffer info.
func InitSliceBsBuffer(pSlice *SSlice, pBsWrite *common.SBitStringAux, bIndependenceBsBuffer bool, iMaxSliceBufferSize int32) int32 {
	pSlice.sSliceBs.uiSize = uint32(iMaxSliceBufferSize)
	pSlice.sSliceBs.uiBsPos = 0

	if bIndependenceBsBuffer {
		pSlice.pSliceBsa = &pSlice.sSliceBs.sBsWrite
		pSlice.sSliceBs.pBs = make([]uint8, iMaxSliceBufferSize)
		pSlice.sSliceBs.uiBsSize = uint32(iMaxSliceBufferSize)
	} else {
		pSlice.pSliceBsa = pBsWrite
		pSlice.sSliceBs.pBs = nil
		pSlice.sSliceBs.uiBsSize = 0
	}
	return ENC_RETURN_SUCCESS
}

// FreeSliceBuffer frees the slice bs buffers and the slice list.
func FreeSliceBuffer(pSliceList *[]SSlice, kiMaxSliceNum int32, kpTag string) {
	if nil != *pSliceList {
		iSliceIdx := int32(0)
		for iSliceIdx < kiMaxSliceNum && int(iSliceIdx) < len(*pSliceList) {
			pSlice := &(*pSliceList)[iSliceIdx]
			FreeMbCache(&pSlice.sMbCacheInfo)

			//slice bs buffer
			pSlice.sSliceBs.pBs = nil
			iSliceIdx++
		}
		*pSliceList = nil
	}
}

func InitSliceList(pSliceList *[]SSlice, pBsWrite *common.SBitStringAux, kiMaxSliceNum int32, kiMaxSliceBufferSize int32, bIndependenceBsBuffer bool) int32 {
	iSliceIdx := int32(0)
	iRet := int32(0)

	if kiMaxSliceBufferSize <= 0 {
		return ENC_RETURN_UNEXPECTED
	}

	for iSliceIdx < kiMaxSliceNum {
		if int(iSliceIdx) >= len(*pSliceList) {
			return ENC_RETURN_MEMALLOCERR
		}
		pSlice := &(*pSliceList)[iSliceIdx]

		pSlice.iSliceIdx = iSliceIdx
		pSlice.uiBufferIdx = 0
		pSlice.iCountMbNumInSlice = 0
		pSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice = 0

		iRet = InitSliceBsBuffer(pSlice,
			pBsWrite,
			bIndependenceBsBuffer,
			kiMaxSliceBufferSize)
		if ENC_RETURN_SUCCESS != iRet {
			return iRet
		}

		iRet = AllocateSliceMBBuffer(pSlice)

		if ENC_RETURN_SUCCESS != iRet {
			return iRet
		}
		iSliceIdx++
	}
	return ENC_RETURN_SUCCESS
}

func InitAllSlicesInThread(pCtx *sWelsEncCtx) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	iSliceIdx := int32(0)
	iSlcBuffIdx := int32(0)

	for ; iSliceIdx < pCurDqLayer.iMaxSliceNum; iSliceIdx++ {
		if nil == pCurDqLayer.ppSliceInLayer[iSliceIdx] {
			return ENC_RETURN_UNEXPECTED
		}

		pCurDqLayer.ppSliceInLayer[iSliceIdx].iSliceIdx = -1
	}

	for ; iSlcBuffIdx < int32(pCtx.iActiveThreadsNum); iSlcBuffIdx++ {
		pCurDqLayer.sSliceBufferInfo[iSlcBuffIdx].iCodedSliceNum = 0
	}

	return ENC_RETURN_SUCCESS
}

// InitOneSliceInThread: SSlice*& pSlice -> **SSlice (receives a pointer into sSliceBufferInfo[kiSlcBuffIdx].pSliceBuffer).
func InitOneSliceInThread(pCtx *sWelsEncCtx, pSlice **SSlice, kiSlcBuffIdx int32, kiDlayerIdx int32, kiSliceIdx int32) int32 {
	if pCtx.pCurDqLayer.bThreadSlcBufferFlag {
		kiCodedNumInThread := pCtx.pCurDqLayer.sSliceBufferInfo[kiSlcBuffIdx].iCodedSliceNum
		// assert (kiCodedNumInThread <= pCtx->pCurDqLayer->sSliceBufferInfo[kiSlcBuffIdx].iMaxSliceNum - 1)
		*pSlice = &pCtx.pCurDqLayer.sSliceBufferInfo[kiSlcBuffIdx].pSliceBuffer[kiCodedNumInThread]
	} else {
		*pSlice = &pCtx.pCurDqLayer.sSliceBufferInfo[0].pSliceBuffer[kiSliceIdx]
	}
	(*pSlice).iSliceIdx = kiSliceIdx
	(*pSlice).uiBufferIdx = uint32(kiSlcBuffIdx)

	// Initialize slice bs buffer info
	(*pSlice).sSliceBs.uiBsPos = 0
	(*pSlice).sSliceBs.iNalIndex = 0
	(*pSlice).sSliceBs.pBsBuffer = pCtx.pSliceThreading.pThreadBsBuffer[kiSlcBuffIdx]
	(*pSlice).sSliceBs.uiSize = uint32(pCtx.iFrameBsSize)

	return ENC_RETURN_SUCCESS
}

// sesAllocInt32ForPtrSize mirrors WelsMallocz (sizeof (int32_t*) * n) of the C
// code: on 64-bit targets the arrays hold 2*n int32 entries.
func sesAllocInt32ForPtrSize(n int32) []int32 {
	if n < 0 {
		n = 0
	}
	return make([]int32, 2*int(n))
}

func InitSliceThreadInfo(pCtx *sWelsEncCtx, pDqLayer *SDqLayer, kiDlayerIndex int32) int32 {
	iThreadNum := int32(pCtx.pSvcParam.IMultipleThreadIdc)
	iMaxSliceNum := int32(0)
	iSlcBufferNum := int32(0)
	iIdx := int32(0)
	iRet := int32(0)

	// assert (iThreadNum > 0)

	//for fixed slice num case, no need to reallocate, so one slice buffer for all thread
	if pDqLayer.bThreadSlcBufferFlag {
		iMaxSliceNum = pDqLayer.iMaxSliceNum/iThreadNum + 1
		iSlcBufferNum = iThreadNum
	} else {
		iMaxSliceNum = pDqLayer.iMaxSliceNum
		iSlcBufferNum = 1
	}

	for iIdx < iSlcBufferNum {
		pDqLayer.sSliceBufferInfo[iIdx].iMaxSliceNum = iMaxSliceNum
		pDqLayer.sSliceBufferInfo[iIdx].iCodedSliceNum = 0
		pDqLayer.sSliceBufferInfo[iIdx].pSliceBuffer = make([]SSlice, iMaxSliceNum)
		iRet = InitSliceList(&pDqLayer.sSliceBufferInfo[iIdx].pSliceBuffer,
			&pCtx.pOut.sBsWrite,
			iMaxSliceNum,
			pCtx.iSliceBufferSize[kiDlayerIndex],
			pDqLayer.bSliceBsBufferFlag)
		if ENC_RETURN_SUCCESS != iRet {
			return iRet
		}
		iIdx++
	}

	for ; iIdx < MAX_THREADS_NUM; iIdx++ {
		pDqLayer.sSliceBufferInfo[iIdx].iMaxSliceNum = 0
		pDqLayer.sSliceBufferInfo[iIdx].iCodedSliceNum = 0
		pDqLayer.sSliceBufferInfo[iIdx].pSliceBuffer = nil
	}

	return ENC_RETURN_SUCCESS
}

func InitSliceInLayer(pCtx *sWelsEncCtx, pDqLayer *SDqLayer, kiDlayerIndex int32) int32 {
	iRet := int32(0)
	iSliceIdx := int32(0)
	iSlcBuffIdx := int32(0)
	iStartIdx := int32(0)
	iMaxSliceNum := pDqLayer.iMaxSliceNum
	pSliceArgument := &pCtx.pSvcParam.SSpatialLayers[kiDlayerIndex].SSliceArgument

	//SM_SINGLE_SLICE mode using single-thread bs writer pOut->sBsWrite
	//even though multi-thread is on for other layers
	pDqLayer.bSliceBsBufferFlag = pCtx.pSvcParam.IMultipleThreadIdc > 1 &&
		api.SM_SINGLE_SLICE != pSliceArgument.UiSliceMode

	pDqLayer.bThreadSlcBufferFlag = pCtx.pSvcParam.IMultipleThreadIdc > 1 &&
		api.SM_SIZELIMITED_SLICE == pSliceArgument.UiSliceMode

	iRet = InitSliceThreadInfo(pCtx,
		pDqLayer,
		kiDlayerIndex)
	if ENC_RETURN_SUCCESS != iRet {
		return ENC_RETURN_MEMALLOCERR
	}

	pDqLayer.iMaxSliceNum = 0
	for iSlcBuffIdx = 0; iSlcBuffIdx < int32(pCtx.iActiveThreadsNum); iSlcBuffIdx++ {
		pDqLayer.iMaxSliceNum += pDqLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum
	}

	pDqLayer.ppSliceInLayer = make([]*SSlice, pDqLayer.iMaxSliceNum)

	pDqLayer.pFirstMbIdxOfSlice = sesAllocInt32ForPtrSize(pDqLayer.iMaxSliceNum)

	pDqLayer.pCountMbNumInSlice = sesAllocInt32ForPtrSize(pDqLayer.iMaxSliceNum)

	iRet = InitSliceBoundaryInfo(pDqLayer, pSliceArgument, iMaxSliceNum)
	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	iStartIdx = 0
	for iSlcBuffIdx = 0; iSlcBuffIdx < int32(pCtx.iActiveThreadsNum); iSlcBuffIdx++ {
		for iSliceIdx = 0; iSliceIdx < pDqLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum; iSliceIdx++ {
			pDqLayer.ppSliceInLayer[iStartIdx+iSliceIdx] = &pDqLayer.sSliceBufferInfo[iSlcBuffIdx].pSliceBuffer[iSliceIdx]
		}

		iStartIdx += pDqLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum
	}

	return ENC_RETURN_SUCCESS
}

func InitSliceHeadWithBase(pSlice *SSlice, pBaseSlice *SSlice) {
	if nil == pSlice || nil == pBaseSlice {
		return
	}

	pBaseSHExt := &pBaseSlice.sSliceHeaderExt
	pSHExt := &pSlice.sSliceHeaderExt

	pSlice.bSliceHeaderExtFlag = pBaseSlice.bSliceHeaderExtFlag
	pSHExt.sSliceHeader.iPpsId = pBaseSHExt.sSliceHeader.iPpsId
	pSHExt.sSliceHeader.pPps = pBaseSHExt.sSliceHeader.pPps
	pSHExt.sSliceHeader.iSpsId = pBaseSHExt.sSliceHeader.iSpsId
	pSHExt.sSliceHeader.pSps = pBaseSHExt.sSliceHeader.pSps
}

func InitSliceRefInfoWithBase(pSlice *SSlice, pBaseSlice *SSlice, kuiRefCount uint8) {
	if nil == pSlice || nil == pBaseSlice {
		return
	}

	pBaseSHExt := &pBaseSlice.sSliceHeaderExt
	pSHExt := &pSlice.sSliceHeaderExt

	pSHExt.sSliceHeader.uiRefCount = kuiRefCount
	pSHExt.sSliceHeader.sRefMarking = pBaseSHExt.sSliceHeader.sRefMarking
	pSHExt.sSliceHeader.sRefReordering = pBaseSHExt.sSliceHeader.sRefReordering
}

func initSliceRC(pSlice *SSlice, kiGlobalQp int32) int32 {
	if nil == pSlice || kiGlobalQp < 0 {
		return ENC_RETURN_INVALIDINPUT
	}

	pSlice.sSlicingOverRc.iComplexityIndexSlice = 0
	pSlice.sSlicingOverRc.iCalculatedQpSlice = kiGlobalQp
	pSlice.sSlicingOverRc.iTotalQpSlice = 0
	pSlice.sSlicingOverRc.iTotalMbSlice = 0
	pSlice.sSlicingOverRc.iTargetBitsSlice = 0
	pSlice.sSlicingOverRc.iFrameBitsSlice = 0
	pSlice.sSlicingOverRc.iGomBitsSlice = 0

	return ENC_RETURN_SUCCESS
}

// ReallocateSliceList: SSlice*& pSliceList -> *[]SSlice (the slice buffer array,
// e.g. &sSliceBufferInfo[i].pSliceBuffer).
func ReallocateSliceList(pCtx *sWelsEncCtx, pSliceArgument *api.SSliceArgument, pSliceList *[]SSlice, kiMaxSliceNumOld int32, kiMaxSliceNumNew int32) int32 {
	var pBaseSlice *SSlice
	var pNewSliceList []SSlice
	var pSlice *SSlice
	iSliceIdx := int32(0)
	iRet := int32(0)
	kiCurDid := int32(pCtx.uiDependencyId)
	iMaxSliceBufferSize := pCtx.iSliceBufferSize[kiCurDid]

	if nil == pSliceList || nil == *pSliceList || nil == pSliceArgument {
		return ENC_RETURN_INVALIDINPUT
	}

	bIndependenceBsBuffer := pCtx.pSvcParam.IMultipleThreadIdc > 1 &&
		api.SM_SINGLE_SLICE != pSliceArgument.UiSliceMode

	pNewSliceList = make([]SSlice, kiMaxSliceNumNew)

	copy(pNewSliceList[:kiMaxSliceNumOld], (*pSliceList)[:kiMaxSliceNumOld])

	//update Bs writer
	for iSliceIdx = 0; iSliceIdx < kiMaxSliceNumOld; iSliceIdx++ {
		pSlice = &pNewSliceList[iSliceIdx]

		if bIndependenceBsBuffer {
			pSlice.pSliceBsa = &pSlice.sSliceBs.sBsWrite
		}
	}

	pBaseSlice = &(*pSliceList)[0]

	for iSliceIdx = kiMaxSliceNumOld; iSliceIdx < kiMaxSliceNumNew; iSliceIdx++ {
		pSlice = &pNewSliceList[iSliceIdx]

		pSlice.iSliceIdx = -1
		pSlice.uiBufferIdx = 0
		pSlice.iCountMbNumInSlice = 0
		pSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice = 0

		iRet = InitSliceBsBuffer(pSlice,
			&pCtx.pOut.sBsWrite,
			bIndependenceBsBuffer,
			iMaxSliceBufferSize)
		if ENC_RETURN_SUCCESS != iRet {
			FreeSliceBuffer(&pNewSliceList, kiMaxSliceNumNew, "pSliceBuffer")
			return iRet
		}

		iRet = AllocateSliceMBBuffer(pSlice)
		if ENC_RETURN_SUCCESS != iRet {
			FreeSliceBuffer(&pNewSliceList, kiMaxSliceNumNew, "pSliceBuffer")
			return iRet
		}

		InitSliceHeadWithBase(pSlice, pBaseSlice)
		InitSliceRefInfoWithBase(pSlice, pBaseSlice, pCtx.iNumRef0)

		iRet = initSliceRC(pSlice, pCtx.iGlobalQp)
		if ENC_RETURN_SUCCESS != iRet {
			FreeSliceBuffer(&pNewSliceList, kiMaxSliceNumNew, "pSliceBuffer")
			return iRet
		}
	}

	*pSliceList = pNewSliceList

	return ENC_RETURN_SUCCESS
}

// CalculateNewSliceNum: int32_t& iMaxSliceNumNew -> *int32.
func CalculateNewSliceNum(pCtx *sWelsEncCtx, pLastCodedSlice *SSlice, iMaxSliceNumOld int32, iMaxSliceNumNew *int32) int32 {
	if nil == pCtx || nil == pLastCodedSlice || 0 == iMaxSliceNumOld {
		return ENC_RETURN_INVALIDINPUT
	}

	if 1 == pCtx.iActiveThreadsNum {
		*iMaxSliceNumNew = iMaxSliceNumOld * SLICE_NUM_EXPAND_COEF
		return ENC_RETURN_SUCCESS
	}

	iPartitionID := pLastCodedSlice.iSliceIdx % int32(pCtx.iActiveThreadsNum)
	iMBNumInPatition := pCtx.pCurDqLayer.EndMbIdxOfPartition[iPartitionID] -
		pCtx.pCurDqLayer.FirstMbIdxOfPartition[iPartitionID] + 1
	iLeftMBNum := pCtx.pCurDqLayer.EndMbIdxOfPartition[iPartitionID] -
		pCtx.pCurDqLayer.LastCodedMbIdxOfPartition[iPartitionID] + 1
	iIncreaseSlicNum := (iLeftMBNum * INT_MULTIPLY / iMBNumInPatition) * iMaxSliceNumOld

	if 0 == (iIncreaseSlicNum / INT_MULTIPLY) {
		iIncreaseSlicNum = 1
	} else {
		iIncreaseSlicNum = iIncreaseSlicNum / INT_MULTIPLY
	}
	if iIncreaseSlicNum < iMaxSliceNumOld/2 {
		iIncreaseSlicNum = iMaxSliceNumOld / 2
	}
	*iMaxSliceNumNew = iMaxSliceNumOld + iIncreaseSlicNum

	return ENC_RETURN_SUCCESS
}

func ReallocateSliceInThread(pCtx *sWelsEncCtx, pDqLayer *SDqLayer, kiDlayerIdx int32, KiSlcBuffIdx int32) int32 {
	iMaxSliceNum := pDqLayer.sSliceBufferInfo[KiSlcBuffIdx].iMaxSliceNum
	iCodedSliceNum := pDqLayer.sSliceBufferInfo[KiSlcBuffIdx].iCodedSliceNum
	iMaxSliceNumNew := int32(0)
	iRet := int32(0)
	pLastCodedSlice := &pDqLayer.sSliceBufferInfo[KiSlcBuffIdx].pSliceBuffer[iCodedSliceNum-1]
	pSliceArgument := &pCtx.pSvcParam.SSpatialLayers[kiDlayerIdx].SSliceArgument

	iRet = CalculateNewSliceNum(pCtx,
		pLastCodedSlice,
		iMaxSliceNum,
		&iMaxSliceNumNew)
	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	iRet = ReallocateSliceList(pCtx,
		pSliceArgument,
		&pDqLayer.sSliceBufferInfo[KiSlcBuffIdx].pSliceBuffer,
		iMaxSliceNum,
		iMaxSliceNumNew)
	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	pDqLayer.sSliceBufferInfo[KiSlcBuffIdx].iMaxSliceNum = iMaxSliceNumNew

	return ENC_RETURN_SUCCESS
}

func ExtendLayerBuffer(pCtx *sWelsEncCtx, kiMaxSliceNumOld int32, kiMaxSliceNumNew int32) int32 {
	pCurLayer := pCtx.pCurDqLayer

	// update for ppsliceInlayer
	ppSlice := make([]*SSlice, kiMaxSliceNumNew)
	pCurLayer.ppSliceInLayer = ppSlice

	// update for pFirstMbIdxInSlice
	pFirstMbIdxOfSlice := sesAllocInt32ForPtrSize(kiMaxSliceNumNew)
	copy(pFirstMbIdxOfSlice[:kiMaxSliceNumOld], pCurLayer.pFirstMbIdxOfSlice[:kiMaxSliceNumOld])
	pCurLayer.pFirstMbIdxOfSlice = pFirstMbIdxOfSlice

	// update for pCountMbNumInSlice
	pCountMbNumInSlice := sesAllocInt32ForPtrSize(kiMaxSliceNumNew)
	copy(pCountMbNumInSlice[:kiMaxSliceNumOld], pCurLayer.pCountMbNumInSlice[:kiMaxSliceNumOld])
	pCurLayer.pCountMbNumInSlice = pCountMbNumInSlice

	return ENC_RETURN_SUCCESS
}

func ReallocSliceBuffer(pCtx *sWelsEncCtx) int32 {
	pCurLayer := pCtx.pCurDqLayer
	iMaxSliceNumOld := pCurLayer.sSliceBufferInfo[0].iMaxSliceNum
	iMaxSliceNumNew := int32(0)
	iRet := int32(0)
	iSliceIdx := int32(0)
	iSlcBuffIdx := int32(0)
	iStartIdx := int32(0)
	kiCurDid := int32(pCtx.uiDependencyId)
	pLastCodedSlice := &pCurLayer.sSliceBufferInfo[0].pSliceBuffer[iMaxSliceNumOld-1]
	pSliceArgument := &pCtx.pSvcParam.SSpatialLayers[kiCurDid].SSliceArgument
	iRet = CalculateNewSliceNum(pCtx,
		pLastCodedSlice,
		iMaxSliceNumOld,
		&iMaxSliceNumNew)

	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	iRet = ReallocateSliceList(pCtx,
		pSliceArgument,
		&pCurLayer.sSliceBufferInfo[0].pSliceBuffer,
		iMaxSliceNumOld,
		iMaxSliceNumNew)
	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	pCurLayer.sSliceBufferInfo[0].iMaxSliceNum = iMaxSliceNumNew

	iMaxSliceNumNew = 0
	for iSlcBuffIdx = 0; iSlcBuffIdx < int32(pCtx.iActiveThreadsNum); iSlcBuffIdx++ {
		iMaxSliceNumNew += pCurLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum
	}

	iRet = ExtendLayerBuffer(pCtx, pCurLayer.iMaxSliceNum, iMaxSliceNumNew)
	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	for iSlcBuffIdx = 0; iSlcBuffIdx < int32(pCtx.iActiveThreadsNum); iSlcBuffIdx++ {
		for iSliceIdx = 0; iSliceIdx < pCurLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum; iSliceIdx++ {
			pCurLayer.ppSliceInLayer[iStartIdx+iSliceIdx] = &pCurLayer.sSliceBufferInfo[iSlcBuffIdx].pSliceBuffer[iSliceIdx]
		}
		iStartIdx += pCurLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum
	}

	pCurLayer.iMaxSliceNum = iMaxSliceNumNew

	return ENC_RETURN_SUCCESS
}

func checkAllSliceBuffer(pCurLayer *SDqLayer, kiCodedSliceNum int32) int32 {
	iSliceIdx := int32(0)
	for ; iSliceIdx < kiCodedSliceNum; iSliceIdx++ {
		if nil == pCurLayer.ppSliceInLayer[iSliceIdx] {
			return ENC_RETURN_UNEXPECTED
		}

		if iSliceIdx != pCurLayer.ppSliceInLayer[iSliceIdx].iSliceIdx {
			return ENC_RETURN_UNEXPECTED
		}
	}

	return ENC_RETURN_SUCCESS
}

func ReOrderSliceInLayer(pCtx *sWelsEncCtx, kuiSliceMode api.SliceModeEnum, kiThreadNum int32) int32 {
	pCurLayer := pCtx.pCurDqLayer
	var pSliceBuffer *SSlice
	iSlcBuffIdx := int32(0)
	iPartitionIdx := int32(0)
	iPartitionID := int32(0)
	iSliceIdx := int32(0)
	iSliceNumInThread := int32(0)
	iEncodeSliceNum := int32(0)
	iActualSliceIdx := int32(0)
	iNonUsedBufferNum := int32(0)
	iUsedSliceNum := int32(0)

	iPartitionNum := int32(0)
	var aiPartitionOffset [MAX_THREADS_NUM]int32

	//for non-dynamic slice mode, iPartitionNum = 1, iPartitionOffset = 0
	if api.SM_SIZELIMITED_SLICE == kuiSliceMode {
		iPartitionNum = kiThreadNum
	} else {
		iPartitionNum = 1
	}
	for iPartitionIdx = 0; iPartitionIdx < iPartitionNum; iPartitionIdx++ {
		aiPartitionOffset[iPartitionIdx] = iEncodeSliceNum
		if api.SM_SIZELIMITED_SLICE == kuiSliceMode {
			iEncodeSliceNum += pCurLayer.NumSliceCodedOfPartition[iPartitionIdx]
		} else {
			iEncodeSliceNum = pCurLayer.sSliceEncCtx.iSliceNumInFrame
		}
	}

	if iEncodeSliceNum != pCurLayer.sSliceEncCtx.iSliceNumInFrame {
		return ENC_RETURN_UNEXPECTED
	}

	//before encode all slices in layer, slices' index are init with -1
	//pSliceBuffer->iSliceIdx will be set to actual slice index when encode one slice
	for iSlcBuffIdx = 0; iSlcBuffIdx < kiThreadNum; iSlcBuffIdx++ {
		iSliceNumInThread = pCurLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum

		for iSliceIdx = 0; iSliceIdx < iSliceNumInThread; iSliceIdx++ {
			if int(iSliceIdx) >= len(pCurLayer.sSliceBufferInfo[iSlcBuffIdx].pSliceBuffer) {
				return ENC_RETURN_UNEXPECTED
			}
			pSliceBuffer = &pCurLayer.sSliceBufferInfo[iSlcBuffIdx].pSliceBuffer[iSliceIdx]

			if -1 != pSliceBuffer.iSliceIdx {
				iPartitionID = pSliceBuffer.iSliceIdx % iPartitionNum
				iActualSliceIdx = aiPartitionOffset[iPartitionID] + pSliceBuffer.iSliceIdx/iPartitionNum
				pSliceBuffer.iSliceIdx = iActualSliceIdx
				pCurLayer.ppSliceInLayer[iActualSliceIdx] = pSliceBuffer
				iUsedSliceNum++
			} else {
				pCurLayer.ppSliceInLayer[iEncodeSliceNum+iNonUsedBufferNum] = pSliceBuffer
				iNonUsedBufferNum++
			}
		}
	}

	if iUsedSliceNum != iEncodeSliceNum ||
		pCurLayer.iMaxSliceNum != (iNonUsedBufferNum+iUsedSliceNum) {
		return ENC_RETURN_UNEXPECTED
	}

	if ENC_RETURN_SUCCESS != checkAllSliceBuffer(pCurLayer, iEncodeSliceNum) {
		return ENC_RETURN_UNEXPECTED
	}

	return ENC_RETURN_SUCCESS
}

func GetCurLayerNalCount(pCurDq *SDqLayer, kiCodedSliceNum int32) int32 {
	iTotalNalCount := int32(0)
	iSliceIdx := int32(0)
	for ; iSliceIdx < kiCodedSliceNum; iSliceIdx++ {
		pSliceBs := &pCurDq.ppSliceInLayer[iSliceIdx].sSliceBs
		if pSliceBs.uiBsPos > 0 {
			iTotalNalCount += pSliceBs.iNalIndex
		}
	}

	return iTotalNalCount
}

func GetTotalCodedNalCount(pFbi *api.SFrameBSInfo) int32 {
	iTotalCodedNalCount := int32(0)
	for iNalIdx := 0; iNalIdx < api.MAX_LAYER_NUM_OF_FRAME; iNalIdx++ {
		iTotalCodedNalCount += pFbi.SLayerInfo[iNalIdx].INalCount
	}

	return iTotalCodedNalCount
}

// FrameBsRealloc: pLayerBsInfo is the current layer, an element of
// pFrameBsInfo.SLayerInfo (compared by address).
func FrameBsRealloc(pCtx *sWelsEncCtx, pFrameBsInfo *api.SFrameBSInfo, pLayerBsInfo *api.SLayerBSInfo, kiMaxSliceNumOld int32) int32 {
	iCountNals := pCtx.pOut.iCountNals
	iCountNals += kiMaxSliceNumOld * (pCtx.pSvcParam.ISpatialLayerNum + int32(sesBoolToU32(pCtx.bNeedPrefixNalFlag)))

	pNalList := make([]SWelsNalRaw, iCountNals)
	copy(pNalList, pCtx.pOut.sNalList[:pCtx.pOut.iCountNals])
	pCtx.pOut.sNalList = pNalList

	pNalLen := make([]int32, iCountNals)
	copy(pNalLen, pCtx.pOut.pNalLen[:pCtx.pOut.iCountNals])
	pCtx.pOut.pNalLen = pNalLen

	pCtx.pOut.iCountNals = iCountNals
	iLbi := 0
	pFrameBsInfo.SLayerInfo[0].PNalLengthInByte = pCtx.pOut.pNalLen
	iNalOff := 0
	for iLbi+1 < len(pFrameBsInfo.SLayerInfo) && &pFrameBsInfo.SLayerInfo[iLbi] != pLayerBsInfo {
		iNalOff += int(pFrameBsInfo.SLayerInfo[iLbi].INalCount)
		iLbi++
		pFrameBsInfo.SLayerInfo[iLbi].PNalLengthInByte = pCtx.pOut.pNalLen[iNalOff:]
	}

	return ENC_RETURN_SUCCESS
}

func SliceLayerInfoUpdate(pCtx *sWelsEncCtx, pFrameBsInfo *api.SFrameBSInfo, pLayerBsInfo *api.SLayerBSInfo, kuiSliceMode api.SliceModeEnum) int32 {
	iMaxSliceNum := int32(0)
	iCodedSliceNum := int32(0)
	iCodedNalCount := int32(0)
	iRet := int32(0)

	for iSlcBuffIdx := int32(0); iSlcBuffIdx < int32(pCtx.iActiveThreadsNum); iSlcBuffIdx++ {
		iMaxSliceNum += pCtx.pCurDqLayer.sSliceBufferInfo[iSlcBuffIdx].iMaxSliceNum
	}

	//reallocate ppSliceInLayer if total encoded slice num exceed max slice num
	if iMaxSliceNum > pCtx.pCurDqLayer.iMaxSliceNum {
		iRet = ExtendLayerBuffer(pCtx, pCtx.pCurDqLayer.iMaxSliceNum, iMaxSliceNum)
		if ENC_RETURN_SUCCESS != iRet {
			return iRet
		}
		pCtx.pCurDqLayer.iMaxSliceNum = iMaxSliceNum
	}

	//update ppSliceInLayer based on pSliceBuffer, reordering based on slice index
	iRet = ReOrderSliceInLayer(pCtx, kuiSliceMode, int32(pCtx.iActiveThreadsNum))
	if ENC_RETURN_SUCCESS != iRet {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::SliceLayerInfoUpdate: ReOrderSliceInLayer failed")
		return iRet
	}

	//Extend NalList buffer if exceed
	iCodedSliceNum = GetCurrentSliceNum(pCtx.pCurDqLayer)
	pLayerBsInfo.INalCount = GetCurLayerNalCount(pCtx.pCurDqLayer, iCodedSliceNum)
	iCodedNalCount = GetTotalCodedNalCount(pFrameBsInfo)

	if iCodedNalCount > pCtx.pOut.iCountNals {
		iRet = FrameBsRealloc(pCtx, pFrameBsInfo, pLayerBsInfo, pCtx.pCurDqLayer.iMaxSliceNum)
		if ENC_RETURN_SUCCESS != iRet {
			return iRet
		}
	}

	return ENC_RETURN_SUCCESS
}

func WelsCodeOneSlice(pEncCtx *sWelsEncCtx, pCurSlice *SSlice, kiNalType int32) int32 {
	pCurLayer := pEncCtx.pCurDqLayer
	pWelsSvcRc := &pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId]
	pNalHeadExt := &pCurLayer.sLayerInfo.sNalHeaderExt
	pBs := pCurSlice.pSliceBsa
	kiDynamicSliceFlag := 0
	if pEncCtx.pSvcParam.SSpatialLayers[pEncCtx.uiDependencyId].SSliceArgument.UiSliceMode == api.SM_SIZELIMITED_SLICE {
		kiDynamicSliceFlag = 1
	}
	if common.I_SLICE == pEncCtx.eSliceType {
		pCurSlice.sScaleShift = 0
	} else {
		kuiTemporalId := uint32(pNalHeadExt.UiTemporalId)
		if kuiTemporalId != 0 {
			pCurSlice.sScaleShift = uint8(kuiTemporalId - uint32(pEncCtx.pRefPic.uiTemporalId))
		} else {
			pCurSlice.sScaleShift = 0
		}
	}

	WelsSliceHeaderExtInit(pEncCtx, pCurLayer, pCurSlice)

	//RomRC init slice by slice
	if pWelsSvcRc.bGomRC {
		GomRCInitForOneSlice(pCurSlice, pWelsSvcRc.iBitsPerMb)
	}

	iHdrIdx := 0
	if pCurSlice.bSliceHeaderExtFlag {
		iHdrIdx = 1
	}
	g_pWelsWriteSliceHeader[iHdrIdx](pEncCtx, pBs, pCurLayer, pCurSlice,
		pEncCtx.pFuncList.pParametersetStrategy)

	pCurSlice.uiLastMbQp = uint8(int32(pCurLayer.sLayerInfo.pPpsP.iPicInitQp) + int32(pCurSlice.sSliceHeaderExt.sSliceHeader.iSliceQpDelta))

	iIdrIdx := 0
	if pNalHeadExt.BIdrFlag {
		iIdrIdx = 1
	}
	iEncReturn := g_pWelsSliceCoding[iIdrIdx][kiDynamicSliceFlag](pEncCtx, pCurSlice)
	if ENC_RETURN_SUCCESS != iEncReturn {
		return iEncReturn
	}

	WelsWriteSliceEndSyn(pCurSlice, pEncCtx.pSvcParam.IEntropyCodingModeFlag != 0)

	return ENC_RETURN_SUCCESS
}

// UpdateMbNeighbourInfoForNextSlice: pMbList is the layer's MB list (SDqLayer.sMbDataP).
func UpdateMbNeighbourInfoForNextSlice(pCurDq *SDqLayer, pMbList []SMB, kiFirstMbIdxOfNextSlice int32, kiLastMbIdxInPartition int32) {
	pSliceCtx := &pCurDq.sSliceEncCtx
	kiMbWidth := int32(pSliceCtx.iMbWidth)
	iIdx := kiFirstMbIdxOfNextSlice
	iNextSliceFirstMbIdxRowStart := int32(0)
	if kiFirstMbIdxOfNextSlice%kiMbWidth != 0 {
		iNextSliceFirstMbIdxRowStart = 1
	}
	iCountMbUpdate := kiMbWidth +
		iNextSliceFirstMbIdxRowStart //need to update MB(iMbXY+1) to MB(iMbXY+1+row) in common case
	kiEndMbNeedUpdate := kiFirstMbIdxOfNextSlice + iCountMbUpdate

	for {
		pMb := &pMbList[iIdx]
		UpdateMbNeighbor(pCurDq, pMb, kiMbWidth, WelsMbToSliceIdc(pCurDq, pMb.iMbXY))
		iIdx++
		if !((iIdx < kiEndMbNeedUpdate) && (iIdx <= kiLastMbIdxInPartition)) {
			break
		}
	}
}

func AddSliceBoundary(pEncCtx *sWelsEncCtx, pCurSlice *SSlice, pSliceCtx *SSliceCtx, pCurMb *SMB, iFirstMbIdxOfNextSlice int32, kiLastMbIdxInPartition int32) {
	pCurLayer := pEncCtx.pCurDqLayer
	pSliceBuffer := pCurLayer.sSliceBufferInfo[pCurSlice.uiBufferIdx].pSliceBuffer
	iCodedSliceNum := pCurLayer.sSliceBufferInfo[pCurSlice.uiBufferIdx].iCodedSliceNum
	iCurMbIdx := pCurMb.iMbXY
	iCurSliceIdc := pSliceCtx.pOverallMbMap[iCurMbIdx]
	kiSliceIdxStep := int32(pEncCtx.iActiveThreadsNum)
	iNextSliceIdc := uint16(int32(iCurSliceIdc) + kiSliceIdxStep)
	var pNextSlice *SSlice

	pMbList := pCurLayer.sMbDataP

	//update cur pSlice info
	pCurSlice.sSliceHeaderExt.uiNumMbsInSlice = uint32(1 + iCurMbIdx - pCurSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice)

	//pNextSlice pointer/initialization
	if pEncCtx.iActiveThreadsNum > 1 {
		pNextSlice = &pSliceBuffer[iCodedSliceNum+1]
	} else {
		pNextSlice = &pSliceBuffer[iNextSliceIdc]
	}

	//init next pSlice info
	pNextSlice.bSliceHeaderExtFlag =
		(common.NAL_UNIT_CODED_SLICE_EXT == pCurLayer.sLayerInfo.sNalHeaderExt.SNalUnitHeader.ENalUnitType)
	pNextSlice.sSliceHeaderExt = pCurSlice.sSliceHeaderExt // confirmed_safe_unsafe_usage
	pNextSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice = iFirstMbIdxOfNextSlice
	common.WelsSetMemMultiplebytes_c(pSliceCtx.pOverallMbMap[iFirstMbIdxOfNextSlice:], uint32(iNextSliceIdc),
		(kiLastMbIdxInPartition - iFirstMbIdxOfNextSlice + 1), 2)

	//DYNAMIC_SLICING_ONE_THREAD: update pMbList slice_neighbor_info
	UpdateMbNeighbourInfoForNextSlice(pCurLayer, pMbList, iFirstMbIdxOfNextSlice, kiLastMbIdxInPartition)
}

// DynSlcJudgeSliceBoundaryStepBack: pCtx / pSlice: void* that are a sWelsEncCtx* / SSlice*.
func DynSlcJudgeSliceBoundaryStepBack(pCtx *sWelsEncCtx, pSlice *SSlice, pSliceCtx *SSliceCtx, pCurMb *SMB, pDss *SDynamicSlicingStack) bool {
	pEncCtx := pCtx
	pCurSlice := pSlice
	iCurMbIdx := pCurMb.iMbXY
	uiLen := uint32(0)
	iPosBitOffset := int32(0)
	kiActiveThreadsNum := int32(pEncCtx.iActiveThreadsNum)
	kiPartitaionId := pCurSlice.iSliceIdx % kiActiveThreadsNum
	kiEndMbIdxOfPartition := pEncCtx.pCurDqLayer.EndMbIdxOfPartition[kiPartitaionId]
	kbCurMbNotFirstMbOfCurSlice := (iCurMbIdx > 0) && (pSliceCtx.pOverallMbMap[iCurMbIdx] ==
		pSliceCtx.pOverallMbMap[iCurMbIdx-1])
	kbCurMbNotLastMbOfCurPartition := iCurMbIdx < kiEndMbIdxOfPartition

	if pCurSlice.bDynamicSlicingSliceSizeCtrlFlag {
		return false
	}

	iPosBitOffset = (pDss.iCurrentPos - pDss.iStartPos)
	uiLen = uint32(iPosBitOffset >> 3)
	if iPosBitOffset&0x07 != 0 {
		uiLen++
	}

	// JUMPPACKETSIZE_JUDGE (uiLen, iCurMbIdx, pSliceCtx->uiSliceSizeConstraint): evaluated in uint32 as in C
	if (kbCurMbNotFirstMbOfCurSlice &&
		uiLen > pSliceCtx.uiSliceSizeConstraint-AVER_MARGIN_BYTES) /*jump_avoiding_pack_exceed*/ &&
		kbCurMbNotLastMbOfCurPartition { //decide to add new pSlice

		common.WelsLog(&pEncCtx.sLogCtx, api.WELS_LOG_DETAIL,
			"DynSlcJudgeSliceBoundaryStepBack: AddSliceBoundary: iCurMbIdx=%d, uiLen=%d, iSliceIdx=%d", iCurMbIdx, uiLen,
			pCurSlice.iSliceIdx)

		//tmp choice to avoid complex memory operation, 100520, to be modify
		//TODO: pSliceCtx->iSliceNumInFrame should match max slice num limitation in given profile based on standard
		//      current change is tmp solution which equal to origin design,
		//      as iMaxSliceNum is always equal to iMaxSliceNumConstraint in origin design
		//      and will also extend when reallocated,
		//  tmp change is:  iMaxSliceNumConstraint is alway set to be MAXSLICENUM, will not change even reallocate
		AddSliceBoundary(pEncCtx, pCurSlice, pSliceCtx, pCurMb, iCurMbIdx, kiEndMbIdxOfPartition)
		pSliceCtx.iSliceNumInFrame++

		return true
	}

	return false
}

///////////////
//  pMb loop
///////////////

// WelsInitInterMDStruc (inline in C) pMvdCostTable: &pEncCtx->pMvdCostTable[iMvdCostTableSize] -> (slice, offset).
func WelsInitInterMDStruc(pCurMb *SMB, pMvdCostTable []uint16, iMvdCostTableOff int, kiMvdInterTableStride int32, pMd *SWelsMD) {
	pMd.iLambda = g_kiQpCostTable[pCurMb.uiLumaQp]
	pMd.pMvdCost = pMvdCostTable
	pMd.iMvdCostOff = iMvdCostTableOff + int(int32(pCurMb.uiLumaQp)*kiMvdInterTableStride)
	pMd.iMbPixX = (int32(pCurMb.iMbX) << 4)
	pMd.iMbPixY = (int32(pCurMb.iMbY) << 4)
	pMd.iBlock8x8StaticIdc = [4]int32{}
}

// WelsMdInterMbLoop is for inter non-dynamic pSlice.
// pWelsMd: void* that is a SWelsMD*.
func WelsMdInterMbLoop(pEncCtx *sWelsEncCtx, pSlice *SSlice, pWelsMd *SWelsMD, kiSliceFirstMbXY int32, pfInterMd PInterMdFunc) int32 {
	pMd := pWelsMd
	pBs := pSlice.pSliceBsa
	pCurLayer := pEncCtx.pCurDqLayer
	pMbCache := &pSlice.sMbCacheInfo
	pMbList := pCurLayer.sMbDataP
	var pCurMb *SMB
	iNumMbCoded := int32(0)
	iNextMbIdx := kiSliceFirstMbXY
	iCurMbIdx := int32(-1)
	kiTotalNumMb := int32(pCurLayer.iMbWidth) * int32(pCurLayer.iMbHeight)
	kiMvdInterTableStride := pEncCtx.iMvdCostTableStride
	pMvdCostTable, iMvdCostTableOff := pEncCtx.pMvdCostTable, int(pEncCtx.iMvdCostTableSize)
	kuiChromaQpIndexOffset := pCurLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset
	iEncReturn := int32(ENC_RETURN_SUCCESS)
	var sDss SDynamicSlicingStack
	if pEncCtx.pSvcParam.IEntropyCodingModeFlag != 0 {
		WelsInitSliceCabac(pEncCtx, pSlice)
		sDss.pRestoreBuffer = nil
		sDss.iStartPos = 0
		sDss.iCurrentPos = 0
	}
	pSlice.iMbSkipRun = 0
	for {
		if pEncCtx.pSvcParam.IEntropyCodingModeFlag == 0 {
			pEncCtx.pFuncList.pfStashMBStatus(&sDss, pSlice, pSlice.iMbSkipRun)
		}
		//point to current pMb
		iCurMbIdx = iNextMbIdx
		pCurMb = &pMbList[iCurMbIdx]

		//step(1): set QP for the current MB
		pEncCtx.pFuncList.pfRc.pfWelsRcMbInit(pEncCtx, pCurMb, pSlice)

		//step (2). save some vale for future use, initial pWelsMd
		WelsMdIntraInit(pEncCtx, pCurMb, pMbCache, kiSliceFirstMbXY)
		WelsMdInterInit(pEncCtx, pSlice, pCurMb, kiSliceFirstMbXY)

		for { // TRY_REENCODING:
			WelsInitInterMDStruc(pCurMb, pMvdCostTable, iMvdCostTableOff, kiMvdInterTableStride, pMd)
			pfInterMd(pEncCtx, pMd, pSlice, pCurMb, pMbCache)
			//mb_qp

			//step (4): save from the MD process from future use
			WelsMdInterSaveSadAndRefMbType(pCurLayer.pDecPic.uiRefMbType, pMbCache, pCurMb, pMd)

			pEncCtx.pFuncList.pfMdBackgroundInfoUpdate(pCurLayer, pCurMb, pMbCache.bCollocatedPredFlag,
				pEncCtx.pRefPic.iPictureType)

			//step (5): update cache
			UpdateNonZeroCountCache(pCurMb, pMbCache)

			//step (6): begin to write bit stream; if the pSlice size is controlled, the writing may be skipped

			iEncReturn = pEncCtx.pFuncList.pfWelsSpatialWriteMbSyn(pEncCtx, pSlice, pCurMb)
			if pEncCtx.pSvcParam.IEntropyCodingModeFlag == 0 {
				if iEncReturn == ENC_RETURN_VLCOVERFLOWFOUND && (pCurMb.uiLumaQp < 50) {
					pSlice.iMbSkipRun = pEncCtx.pFuncList.pfStashPopMBStatus(&sDss, pSlice)
					UpdateQpForOverflow(pCurMb, kuiChromaQpIndexOffset)
					continue // goto TRY_REENCODING
				}
			}
			break
		}
		if ENC_RETURN_SUCCESS != iEncReturn {
			return iEncReturn
		}

		// uiSliceIdc is initialized before workers start; deblocking in another
		// completed slice can read it while this slice is still encoding.
		//step (7): reconstruct current MB
		OutputPMbWithoutConstructCsRsNoCopy(pEncCtx, pCurLayer, pSlice, pCurMb)

		//step (8): update status and other parameters
		pEncCtx.pFuncList.pfRc.pfWelsRcMbInfoUpdate(pEncCtx, pCurMb, pMd.iCostLuma, pSlice)

		/*judge if all pMb in cur pSlice has been encoded*/
		iNumMbCoded++
		iNextMbIdx = WelsGetNextMbOfSlice(pCurLayer, iCurMbIdx)
		//whether all of MB in current pSlice encoded or not
		if iNextMbIdx == -1 || iNextMbIdx >= kiTotalNumMb || iNumMbCoded >= kiTotalNumMb {
			break
		}
	}

	if pSlice.iMbSkipRun != 0 {
		common.BsWriteUE(pBs, uint32(pSlice.iMbSkipRun))
	}

	return iEncReturn
}

// WelsMdInterMbLoopOverDynamicSlice is only for inter dynamic slicing.
func WelsMdInterMbLoopOverDynamicSlice(pEncCtx *sWelsEncCtx, pSlice *SSlice, pWelsMd *SWelsMD, kiSliceFirstMbXY int32, pfInterMd PInterMdFunc) int32 {
	pMd := pWelsMd
	pBs := pSlice.pSliceBsa
	pCurLayer := pEncCtx.pCurDqLayer
	pSliceCtx := &pCurLayer.sSliceEncCtx
	pMbCache := &pSlice.sMbCacheInfo
	pMbList := pCurLayer.sMbDataP
	var pCurMb *SMB
	iNumMbCoded := int32(0)
	kiTotalNumMb := int32(pCurLayer.iMbWidth) * int32(pCurLayer.iMbHeight)
	iNextMbIdx := kiSliceFirstMbXY
	iCurMbIdx := int32(-1)
	kiMvdInterTableStride := pEncCtx.iMvdCostTableStride
	pMvdCostTable, iMvdCostTableOff := pEncCtx.pMvdCostTable, int(pEncCtx.iMvdCostTableSize)
	kiSliceIdx := pSlice.iSliceIdx
	kiPartitionId := (kiSliceIdx % int32(pEncCtx.iActiveThreadsNum))
	kuiChromaQpIndexOffset := pCurLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset
	iEncReturn := int32(ENC_RETURN_SUCCESS)

	var sDss SDynamicSlicingStack
	if pEncCtx.pSvcParam.IEntropyCodingModeFlag != 0 {
		WelsInitSliceCabac(pEncCtx, pSlice)
		sDss.iStartPos = 0
		sDss.iCurrentPos = 0
		sDss.pRestoreBuffer = pEncCtx.pDynamicBsBuffer[kiPartitionId]
	} else {
		sDss.iStartPos = BsGetBitsPos(pBs)
	}
	pSlice.iMbSkipRun = 0
	for {
		//DYNAMIC_SLICING_ONE_THREAD - MultiD
		//stack pBs pointer
		pEncCtx.pFuncList.pfStashMBStatus(&sDss, pSlice, pSlice.iMbSkipRun)

		//point to current pMb
		iCurMbIdx = iNextMbIdx
		pCurMb = &pMbList[iCurMbIdx]

		//step(1): set QP for the current MB
		pEncCtx.pFuncList.pfRc.pfWelsRcMbInit(pEncCtx, pCurMb, pSlice)
		// if already reaches the largest number of slices, set QPs to the upper bound
		if pSlice.bDynamicSlicingSliceSizeCtrlFlag {
			//a clearer logic may be:
			//if there is no need from size control from the pSlice size, the QP will be decided by RC; else it will be set to the max QP
			//    however, there are some parameter updating in the rc_mb_init() function, so it cannot be skipped?
			pCurMb.uiLumaQp = uint8(pEncCtx.pWelsSvcRc[pEncCtx.uiDependencyId].iMaxQp)
			pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+int32(kuiChromaQpIndexOffset))]
		}

		//step (2). save some vale for future use, initial pWelsMd
		WelsMdIntraInit(pEncCtx, pCurMb, pMbCache, kiSliceFirstMbXY)
		WelsMdInterInit(pEncCtx, pSlice, pCurMb, kiSliceFirstMbXY)

		for { // TRY_REENCODING:
			WelsInitInterMDStruc(pCurMb, pMvdCostTable, iMvdCostTableOff, kiMvdInterTableStride, pMd)
			pfInterMd(pEncCtx, pMd, pSlice, pCurMb, pMbCache)
			//mb_qp

			//step (4): save from the MD process from future use
			WelsMdInterSaveSadAndRefMbType(pCurLayer.pDecPic.uiRefMbType, pMbCache, pCurMb, pMd)

			pEncCtx.pFuncList.pfMdBackgroundInfoUpdate(pCurLayer, pCurMb, pMbCache.bCollocatedPredFlag,
				pEncCtx.pRefPic.iPictureType)

			//step (5): update cache
			UpdateNonZeroCountCache(pCurMb, pMbCache)

			//step (6): begin to write bit stream; if the pSlice size is controlled, the writing may be skipped

			iEncReturn = pEncCtx.pFuncList.pfWelsSpatialWriteMbSyn(pEncCtx, pSlice, pCurMb)
			if iEncReturn == ENC_RETURN_VLCOVERFLOWFOUND && (pCurMb.uiLumaQp < 50) {
				pSlice.iMbSkipRun = pEncCtx.pFuncList.pfStashPopMBStatus(&sDss, pSlice)
				UpdateQpForOverflow(pCurMb, kuiChromaQpIndexOffset)
				continue // goto TRY_REENCODING
			}
			break
		}
		if ENC_RETURN_SUCCESS != iEncReturn {
			return iEncReturn
		}

		//DYNAMIC_SLICING_ONE_THREAD - MultiD
		sDss.iCurrentPos = pEncCtx.pFuncList.pfGetBsPosition(pSlice)
		if DynSlcJudgeSliceBoundaryStepBack(pEncCtx, pSlice, pSliceCtx, pCurMb, &sDss) {
			pSlice.iMbSkipRun = pEncCtx.pFuncList.pfStashPopMBStatus(&sDss, pSlice)
			pCurLayer.LastCodedMbIdxOfPartition[kiPartitionId] = iCurMbIdx -
				1 // update LastCodedMbIdxOfPartition, need to -1 due to stepping back
			pCurLayer.NumSliceCodedOfPartition[kiPartitionId]++

			break
		}

		//step (7): reconstruct current MB
		pCurMb.uiSliceIdc = uint16(kiSliceIdx)
		OutputPMbWithoutConstructCsRsNoCopy(pEncCtx, pCurLayer, pSlice, pCurMb)

		//step (8): update status and other parameters
		pEncCtx.pFuncList.pfRc.pfWelsRcMbInfoUpdate(pEncCtx, pCurMb, pMd.iCostLuma, pSlice)

		/*judge if all pMb in cur pSlice has been encoded*/
		iNumMbCoded++
		iNextMbIdx = WelsGetNextMbOfSlice(pCurLayer, iCurMbIdx)
		//whether all of MB in current pSlice encoded or not
		if iNextMbIdx == -1 || iNextMbIdx >= kiTotalNumMb || iNumMbCoded >= kiTotalNumMb {
			pCurLayer.LastCodedMbIdxOfPartition[kiPartitionId] = iCurMbIdx
			pCurLayer.NumSliceCodedOfPartition[kiPartitionId]++

			break
		}
	}

	if pSlice.iMbSkipRun != 0 {
		common.BsWriteUE(pBs, uint32(pSlice.iMbSkipRun))
	}

	return iEncReturn
}
