// Port of codec/decoder/core/src/decoder_core.cpp.
//
// Wels decoder framework core implementation.
//
// Only the single-threaded path is ported: GetThreadCount() is always 0 in
// the Go port (pCtx.pThreadCtx / pCtx.pLastThreadCtx are always nil), so the
// multi-threaded branches (events, per-thread contexts) are dropped.

package decoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// dcBool2Int32 converts a C bool used in integer arithmetic.
func dcBool2Int32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// static inline int32_t DecodeFrameConstruction (PWelsDecoderContext pCtx, uint8_t** ppDst, SBufferInfo* pDstInfo)
func DecodeFrameConstruction(pCtx *SWelsDecoderContext, ppDst *[3][]uint8, pDstInfo *api.SBufferInfo) int32 {
	pCurDq := pCtx.pCurDqLayer
	pPic := pCtx.pDec

	kiWidth := pCurDq.iMbWidth << 4
	kiHeight := pCurDq.iMbHeight << 4

	kiTotalNumMbInCurLayer := pCurDq.iMbWidth * pCurDq.iMbHeight
	bFrameCompleteFlag := true

	if pPic.bNewSeqBegin {
		pCtx.sFrameCrop = pCurDq.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.pSps.sFrameCrop
		// LONG_TERM_REF
		pCtx.bParamSetsLostFlag = false
		if pCtx.iTotalNumMbRec == kiTotalNumMbInCurLayer {
			pCtx.bPrintFrameErrorTraceFlag = true
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_INFO,
				"DecodeFrameConstruction(): will output first frame of new sequence, %d x %d, crop_left:%d, crop_right:%d, crop_top:%d, crop_bottom:%d, ignored error packet:%d.",
				kiWidth, kiHeight, pCtx.sFrameCrop.iLeftOffset, pCtx.sFrameCrop.iRightOffset, pCtx.sFrameCrop.iTopOffset,
				pCtx.sFrameCrop.iBottomOffset, pCtx.iIgnoredErrorInfoPacketCount)
			pCtx.iIgnoredErrorInfoPacketCount = 0
		}
	}

	kiActualWidth := kiWidth - (pCtx.sFrameCrop.iLeftOffset+pCtx.sFrameCrop.iRightOffset)*2
	kiActualHeight := kiHeight - (pCtx.sFrameCrop.iTopOffset+pCtx.sFrameCrop.iBottomOffset)*2

	if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
		if (pCtx.pDecoderStatistics.UiWidth != uint32(kiActualWidth)) ||
			(pCtx.pDecoderStatistics.UiHeight != uint32(kiActualHeight)) {
			pCtx.pDecoderStatistics.UiResolutionChangeTimes++
			pCtx.pDecoderStatistics.UiWidth = uint32(kiActualWidth)
			pCtx.pDecoderStatistics.UiHeight = uint32(kiActualHeight)
		}
		UpdateDecStatNoFreezingInfo(pCtx)
	}

	if pCtx.pParam.BParseOnly { //should exit for parse only to prevent access NULL pDstInfo
		pCurAu := pCtx.pAccessUnitList
		if int32(api.DsErrorFree) == pCtx.iErrorCode { //correct decoding, add to data buffer
			pParser := pCtx.pParserBsInfo
			var pCurNal *SNalUnit
			var iTotalNalLen int32
			var iNalLen int32
			var iNum int32
			for iNum < pParser.INalNum {
				iTotalNalLen += pParser.PNalLenInByte[iNum]
				iNum++
			}
			// pDstBuf: offset into pParser.PDstBuff
			pDstBuf := int(iTotalNalLen)
			iIdx := int32(pCurAu.uiStartPos)
			iEndIdx := int32(pCurAu.uiEndPos)
			if pCurAu.pNalUnitsList[iIdx] != nil {
				pParser.UiOutBsTimeStamp = pCurAu.pNalUnitsList[iIdx].uiTimeStamp
			} else {
				pParser.UiOutBsTimeStamp = 0
			}
			//pParser->iNalNum = 0;
			pParser.ISpsWidthInPixel = int32(pCtx.pSps.iMbWidth<<4) - ((pCtx.pSps.sFrameCrop.iLeftOffset +
				pCtx.pSps.sFrameCrop.iRightOffset) << 1)
			pParser.ISpsHeightInPixel = int32(pCtx.pSps.iMbHeight<<4) - ((pCtx.pSps.sFrameCrop.iTopOffset +
				pCtx.pSps.sFrameCrop.iBottomOffset) << 1)

			if pCurAu.pNalUnitsList[iIdx].sNalHeaderExt.BIdrFlag { //IDR
				if pCtx.bFrameFinish { //add required sps/pps
					if pParser.INalNum > pCtx.iMaxNalNum-2 { //2 reserved for sps+pps
						common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_INFO,
							"DecodeFrameConstruction(): current NAL num (%d) plus sps & pps exceeds permitted num (%d). Will expand",
							pParser.INalNum, pCtx.iMaxNalNum)
						if ExpandBsLenBuffer(pCtx, pParser.INalNum+2) != 0 {
							return ERR_INFO_OUT_OF_MEMORY
						}
					}
					bSubSps := (common.NAL_UNIT_CODED_SLICE_EXT == pCurAu.pNalUnitsList[iIdx].sNalHeaderExt.SNalUnitHeader.ENalUnitType)
					var pSpsBs *SSpsBsInfo
					var pPpsBs *SPpsBsInfo
					iSpsId := pCtx.pSps.iSpsId
					iPpsId := pCtx.pPps.iPpsId
					pCtx.bParamSetsLostFlag = false
					//find required sps, pps and write into dst buff
					if bSubSps {
						pSpsBs = &pCtx.sSubsetSpsBsInfo[iSpsId]
					} else {
						pSpsBs = &pCtx.sSpsBsInfo[iSpsId]
					}
					pPpsBs = &pCtx.sPpsBsInfo[iPpsId]
					if pDstBuf+int(pSpsBs.uiSpsBsLen)+int(pPpsBs.uiPpsBsLen) >= MAX_ACCESS_UNIT_CAPACITY {
						common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR,
							"DecodeFrameConstruction(): sps pps size: (%d %d) too large. Failed to parse. \n", pSpsBs.uiSpsBsLen,
							pPpsBs.uiPpsBsLen)
						pCtx.iErrorCode |= int32(api.DsOutOfMemory)
						pCtx.pParserBsInfo.INalNum = 0
						return ERR_INFO_OUT_OF_MEMORY
					}
					copy(pParser.PDstBuff[pDstBuf:pDstBuf+int(pSpsBs.uiSpsBsLen)], pSpsBs.pSpsBsBuf[:pSpsBs.uiSpsBsLen])
					pParser.PNalLenInByte[pParser.INalNum] = int32(pSpsBs.uiSpsBsLen)
					pParser.INalNum++
					pDstBuf += int(pSpsBs.uiSpsBsLen)
					copy(pParser.PDstBuff[pDstBuf:pDstBuf+int(pPpsBs.uiPpsBsLen)], pPpsBs.pPpsBsBuf[:pPpsBs.uiPpsBsLen])
					pParser.PNalLenInByte[pParser.INalNum] = int32(pPpsBs.uiPpsBsLen)
					pParser.INalNum++
					pDstBuf += int(pPpsBs.uiPpsBsLen)
					pCtx.bFrameFinish = false
				}
			}
			//then VCL data re-write
			if pParser.INalNum+iEndIdx-iIdx+1 > pCtx.iMaxNalNum { //calculate total NAL num
				common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_INFO,
					"DecodeFrameConstruction(): current NAL num (%d) exceeds permitted num (%d). Will expand",
					pParser.INalNum+iEndIdx-iIdx+1, pCtx.iMaxNalNum)
				if ExpandBsLenBuffer(pCtx, pParser.INalNum+iEndIdx-iIdx+1) != 0 {
					return ERR_INFO_OUT_OF_MEMORY
				}
			}
			for iIdx <= iEndIdx {
				pCurNal = pCurAu.pNalUnitsList[iIdx]
				iIdx++
				iNalLen = pCurNal.sNalData.sVclNal.iNalLength
				pParser.PNalLenInByte[pParser.INalNum] = iNalLen
				pParser.INalNum++
				if pDstBuf+int(iNalLen) >= MAX_ACCESS_UNIT_CAPACITY {
					common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR,
						"DecodeFrameConstruction(): composed output size (%d) exceeds (%d). Failed to parse. current data pos %d out of %d:, previously accumulated num: %d, total num: %d, previously accumulated len: %d, current len: %d, current buf pos: %d, header buf pos: %d \n",
						int64(pDstBuf+int(iNalLen)), MAX_ACCESS_UNIT_CAPACITY, iIdx, iEndIdx, iNum, pParser.INalNum,
						iTotalNalLen, iNalLen, pDstBuf, 0)
					pCtx.iErrorCode |= int32(api.DsOutOfMemory)
					pCtx.pParserBsInfo.INalNum = 0
					return ERR_INFO_OUT_OF_MEMORY
				}

				pNalBs := pCurNal.sNalData.sVclNal.iNalPosOff
				copy(pParser.PDstBuff[pDstBuf:pDstBuf+int(iNalLen)], pCurNal.sNalData.sVclNal.pNalPos[pNalBs:pNalBs+int(iNalLen)])
				pDstBuf += int(iNalLen)
			}
			if pCtx.iTotalNumMbRec == kiTotalNumMbInCurLayer { //frame complete
				pCtx.iTotalNumMbRec = 0
				pCtx.bFramePending = false
				pCtx.bFrameFinish = true //finish current frame and mark it
			} else if pCtx.iTotalNumMbRec != 0 { //frame incomplete
				pCtx.bFramePending = true
				pCtx.pDec.bIsComplete = false
				pCtx.bFrameFinish = false //current frame not finished
				pCtx.iErrorCode |= int32(api.DsFramePending)
				return ERR_INFO_PARSEONLY_PENDING
				//pCtx->pParserBsInfo->iNalNum = 0;
			}
		} else { //error
			pCtx.pParserBsInfo.UiOutBsTimeStamp = 0
			pCtx.pParserBsInfo.INalNum = 0
			pCtx.pParserBsInfo.ISpsWidthInPixel = 0
			pCtx.pParserBsInfo.ISpsHeightInPixel = 0
			return ERR_INFO_PARSEONLY_ERROR
		}
		return ERR_NONE
	}

	if pCtx.iTotalNumMbRec != kiTotalNumMbInCurLayer {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_DEBUG,
			"DecodeFrameConstruction(): iTotalNumMbRec:%d, total_num_mb_sps:%d, cur_layer_mb_width:%d, cur_layer_mb_height:%d ",
			pCtx.iTotalNumMbRec, kiTotalNumMbInCurLayer, pCurDq.iMbWidth, pCurDq.iMbHeight)
		bFrameCompleteFlag = false //return later after output buffer is done
		if pCtx.bInstantDecFlag {  //no-delay decoding, wait for new slice
			return ERR_INFO_MB_NUM_INADEQUATE
		}
	} else if pCurDq.sLayerInfo.sNalHeaderExt.BIdrFlag &&
		(pCtx.iErrorCode == int32(api.DsErrorFree)) { //complete non-ECed IDR frame done
		pCtx.pDec.bIsComplete = true
		pCtx.bFreezeOutput = false
	}

	pCtx.iTotalNumMbRec = 0

	//////output:::normal path
	pDstInfo.UiOutYuvTimeStamp = pPic.uiTimeStamp

	pDstInfo.UsrData.SSystemBuffer.IFormat = int32(api.VideoFormatI420)

	pDstInfo.UsrData.SSystemBuffer.IWidth = kiActualWidth
	pDstInfo.UsrData.SSystemBuffer.IHeight = kiActualHeight
	pDstInfo.UsrData.SSystemBuffer.IStride[0] = pPic.iLinesize[0]
	pDstInfo.UsrData.SSystemBuffer.IStride[1] = pPic.iLinesize[1]
	ppDst[0] = pPic.pData[0][pPic.iDataOff[0]+int(pCtx.sFrameCrop.iTopOffset*2*pPic.iLinesize[0]+pCtx.sFrameCrop.iLeftOffset*2):]
	ppDst[1] = pPic.pData[1][pPic.iDataOff[1]+int(pCtx.sFrameCrop.iTopOffset*pPic.iLinesize[1]+pCtx.sFrameCrop.iLeftOffset):]
	ppDst[2] = pPic.pData[2][pPic.iDataOff[2]+int(pCtx.sFrameCrop.iTopOffset*pPic.iLinesize[1]+pCtx.sFrameCrop.iLeftOffset):]
	for i := 0; i < 3; i++ {
		pDstInfo.PDst[i] = ppDst[i]
	}
	pDstInfo.IBufferStatus = 1
	// GetThreadCount (pCtx) > 1 branches (bIsComplete forcing, ready events) dropped: single-threaded.
	bOutResChange := false
	// GetThreadCount (pCtx) <= 1 || pCtx->pLastThreadCtx == NULL
	bOutResChange = (pCtx.iLastImgWidthInPixel != pDstInfo.UsrData.SSystemBuffer.IWidth) ||
		(pCtx.iLastImgHeightInPixel != pDstInfo.UsrData.SSystemBuffer.IHeight)
	pCtx.iLastImgWidthInPixel = pDstInfo.UsrData.SSystemBuffer.IWidth
	pCtx.iLastImgHeightInPixel = pDstInfo.UsrData.SSystemBuffer.IHeight
	if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE { //no buffer output if EC is disabled and frame incomplete
		pDstInfo.IBufferStatus = dcBool2Int32(bFrameCompleteFlag &&
			pPic.bIsComplete) // When EC disable, ECed picture not output
	} else if (pCtx.pParam.EEcActiveIdc == api.ERROR_CON_SLICE_COPY_CROSS_IDR_FREEZE_RES_CHANGE ||
		pCtx.pParam.EEcActiveIdc == api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE) &&
		pCtx.iErrorCode != 0 && bOutResChange {
		pCtx.bFreezeOutput = true
	}

	if pDstInfo.IBufferStatus == 0 {
		if !bFrameCompleteFlag {
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
		}
		return ERR_INFO_MB_NUM_INADEQUATE
	}
	if pCtx.bFreezeOutput {
		pDstInfo.IBufferStatus = 0
		if pPic.bNewSeqBegin {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_INFO,
				"DecodeFrameConstruction():New sequence detected, but freezed, correct MBs (%d) out of whole MBs (%d).",
				kiTotalNumMbInCurLayer-pCtx.iMbEcedNum, kiTotalNumMbInCurLayer)
		}
	}
	pCtx.iMbEcedNum = pPic.iMbEcedNum
	pCtx.iMbNum = pPic.iMbNum
	pCtx.iMbEcedPropNum = pPic.iMbEcedPropNum
	if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
		if pDstInfo.IBufferStatus != 0 && ((pCtx.pDecoderStatistics.UiWidth != uint32(kiActualWidth)) ||
			(pCtx.pDecoderStatistics.UiHeight != uint32(kiActualHeight))) {
			pCtx.pDecoderStatistics.UiResolutionChangeTimes++
			pCtx.pDecoderStatistics.UiWidth = uint32(kiActualWidth)
			pCtx.pDecoderStatistics.UiHeight = uint32(kiActualHeight)
		}
		UpdateDecStat(pCtx, pDstInfo.IBufferStatus != 0)
	}
	return ERR_NONE
}

// inline bool CheckSliceNeedReconstruct (uint8_t uiLayerDqId, uint8_t uiTargetDqId)
func CheckSliceNeedReconstruct(uiLayerDqId uint8, uiTargetDqId uint8) bool {
	return (uiLayerDqId == uiTargetDqId) // target layer
}

// inline uint8_t GetTargetDqId (uint8_t uiTargetDqId, SDecodingParam* psParam)
func GetTargetDqId(uiTargetDqId uint8, psParam *api.SDecodingParam) uint8 {
	uiRequiredDqId := uint8(255)
	if psParam != nil {
		uiRequiredDqId = psParam.UiTargetDqLayer
	}

	return common.WELS_MIN(uiTargetDqId, uiRequiredDqId)
}

// inline void HandleReferenceLostL0 (PWelsDecoderContext pCtx, PNalUnit pCurNal)
func HandleReferenceLostL0(pCtx *SWelsDecoderContext, pCurNal *SNalUnit) {
	if 0 == pCurNal.sNalHeaderExt.UiTemporalId {
		pCtx.bReferenceLostAtT0Flag = true
	}
	pCtx.iErrorCode |= int32(api.DsBitstreamError)
}

// inline void HandleReferenceLost (PWelsDecoderContext pCtx, PNalUnit pCurNal)
func HandleReferenceLost(pCtx *SWelsDecoderContext, pCurNal *SNalUnit) {
	if (0 == pCurNal.sNalHeaderExt.UiTemporalId) || (1 == pCurNal.sNalHeaderExt.UiTemporalId) {
		pCtx.bReferenceLostAtT0Flag = true
	}
	pCtx.iErrorCode |= int32(api.DsRefLost)
}

// inline int32_t WelsDecodeConstructSlice (PWelsDecoderContext pCtx, PNalUnit pCurNal)
func WelsDecodeConstructSlice(pCtx *SWelsDecoderContext, pCurNal *SNalUnit) int32 {
	iRet := WelsTargetSliceConstruction(pCtx)

	if iRet != 0 {
		HandleReferenceLostL0(pCtx, pCurNal)
	}

	return iRet
}

// int32_t ParsePredWeightedTable (PBitStringAux pBs, PSliceHeader pSh)
func ParsePredWeightedTable(pBs *common.SBitStringAux, pSh *SSliceHeader) int32 {
	var uiCode uint32
	var iList int32
	var iCode int32

	if r := BsGetUe(pBs, &uiCode); r != ERR_NONE {
		return int32(r)
	}
	if WELS_CHECK_SE_BOTH_ERROR_NOLOG(uiCode, 0, 7, "luma_log2_weight_denom") {
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_LUMA_LOG2_WEIGHT_DENOM)
	}
	pSh.sPredWeightTable.uiLumaLog2WeightDenom = uiCode
	if pSh.pSps.uiChromaArrayType != 0 {
		if r := BsGetUe(pBs, &uiCode); r != ERR_NONE {
			return int32(r)
		}
		if WELS_CHECK_SE_BOTH_ERROR_NOLOG(uiCode, 0, 7, "chroma_log2_weight_denom") {
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_CHROMA_LOG2_WEIGHT_DENOM)
		}
		pSh.sPredWeightTable.uiChromaLog2WeightDenom = uiCode
	}

	if (pSh.sPredWeightTable.uiLumaLog2WeightDenom | pSh.sPredWeightTable.uiChromaLog2WeightDenom) > 7 {
		return ERR_NONE
	}

	for {
		for i := int32(0); i < pSh.uiRefCount[iList]; i++ {
			//luma
			if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE {
				return int32(r)
			}
			if uiCode != 0 {
				if r := BsGetSe(pBs, &iCode); r != ERR_NONE {
					return r
				}
				if WELS_CHECK_SE_BOTH_ERROR_NOLOG(iCode, -128, 127, "luma_weight") {
					return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_LUMA_WEIGHT)
				}
				pSh.sPredWeightTable.sPredList[iList].iLumaWeight[i] = iCode

				if r := BsGetSe(pBs, &iCode); r != ERR_NONE {
					return r
				}
				if WELS_CHECK_SE_BOTH_ERROR_NOLOG(iCode, -128, 127, "luma_offset") {
					return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_LUMA_OFFSET)
				}
				pSh.sPredWeightTable.sPredList[iList].iLumaOffset[i] = iCode
			} else {
				pSh.sPredWeightTable.sPredList[iList].iLumaWeight[i] = int32(1) << pSh.sPredWeightTable.uiLumaLog2WeightDenom
				pSh.sPredWeightTable.sPredList[iList].iLumaOffset[i] = 0
			}
			//chroma
			if pSh.pSps.uiChromaArrayType == 0 {
				continue
			}

			if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE {
				return int32(r)
			}
			if uiCode != 0 {
				for j := 0; j < 2; j++ {
					if r := BsGetSe(pBs, &iCode); r != ERR_NONE {
						return r
					}
					if WELS_CHECK_SE_BOTH_ERROR_NOLOG(iCode, -128, 127, "chroma_weight") {
						return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_CHROMA_WEIGHT)
					}
					pSh.sPredWeightTable.sPredList[iList].iChromaWeight[i][j] = iCode

					if r := BsGetSe(pBs, &iCode); r != ERR_NONE {
						return r
					}
					if WELS_CHECK_SE_BOTH_ERROR_NOLOG(iCode, -128, 127, "chroma_offset") {
						return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_CHROMA_OFFSET)
					}
					pSh.sPredWeightTable.sPredList[iList].iChromaOffset[i][j] = iCode
				}
			} else {
				for j := 0; j < 2; j++ {
					pSh.sPredWeightTable.sPredList[iList].iChromaWeight[i][j] = int32(1) << pSh.sPredWeightTable.uiChromaLog2WeightDenom
					pSh.sPredWeightTable.sPredList[iList].iChromaOffset[i][j] = 0
				}
			}
		}
		iList++
		if pSh.eSliceType != common.B_SLICE {
			break
		}
		if !(iList < common.LIST_A) { //TODO: SUPPORT LIST_A
			break
		}
	}
	return ERR_NONE
}

// void CreateImplicitWeightTable (PWelsDecoderContext pCtx)
func CreateImplicitWeightTable(pCtx *SWelsDecoderContext) {
	pSlice := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	pCurDqLayer := pCtx.pCurDqLayer
	if pCurDqLayer.bUseWeightedBiPredIdc && pSliceHeader.pPps.uiWeightedBipredIdc == 2 {
		iPoc := pSliceHeader.iPicOrderCntLsb

		//fix Bugzilla 1485229 check if pointers are NULL
		if pCtx.sRefPic.pRefList[common.LIST_0][0] != nil && pCtx.sRefPic.pRefList[common.LIST_1][0] != nil {
			if pSliceHeader.uiRefCount[0] == 1 && pSliceHeader.uiRefCount[1] == 1 &&
				int64(pCtx.sRefPic.pRefList[common.LIST_0][0].iFramePoc)+int64(pCtx.sRefPic.pRefList[common.LIST_1][0].iFramePoc) == 2*int64(iPoc) {
				pCurDqLayer.bUseWeightedBiPredIdc = false
				return
			}
		}

		pCurDqLayer.pPredWeightTable.uiLumaLog2WeightDenom = 5
		pCurDqLayer.pPredWeightTable.uiChromaLog2WeightDenom = 5
		for iRef0 := int32(0); iRef0 < pSliceHeader.uiRefCount[0]; iRef0++ {
			if pCtx.sRefPic.pRefList[common.LIST_0][iRef0] != nil {
				iPoc0 := pCtx.sRefPic.pRefList[common.LIST_0][iRef0].iFramePoc
				bIsLongRef0 := pCtx.sRefPic.pRefList[common.LIST_0][iRef0].bIsLongRef
				for iRef1 := int32(0); iRef1 < pSliceHeader.uiRefCount[1]; iRef1++ {
					if pCtx.sRefPic.pRefList[common.LIST_1][iRef1] != nil {
						iPoc1 := pCtx.sRefPic.pRefList[common.LIST_1][iRef1].iFramePoc
						bIsLongRef1 := pCtx.sRefPic.pRefList[common.LIST_1][iRef1].bIsLongRef
						pCurDqLayer.pPredWeightTable.iImplicitWeight[iRef0][iRef1] = 32
						if !bIsLongRef0 && !bIsLongRef1 {
							iTd := common.WELS_CLIP3(iPoc1-iPoc0, -128, 127)
							if iTd != 0 {
								iTb := common.WELS_CLIP3(iPoc-iPoc0, -128, 127)
								iTx := (16384 + (common.WELS_ABS(iTd) >> 1)) / iTd
								iDistScaleFactor := (iTb*iTx + 32) >> 8
								if iDistScaleFactor >= -64 && iDistScaleFactor <= 128 {
									pCurDqLayer.pPredWeightTable.iImplicitWeight[iRef0][iRef1] = 64 - iDistScaleFactor
								}
							}
						}
					}
				}
			}
		}
	}
}

/*
 *  Predeclared function routines ..
 */

// int32_t ParseRefPicListReordering (PBitStringAux pBs, PSliceHeader pSh)
func ParseRefPicListReordering(pBs *common.SBitStringAux, pSh *SSliceHeader) int32 {
	var iList int32
	keSt := pSh.eSliceType
	pRefPicListReordering := &pSh.pRefPicListReordering
	pSps := pSh.pSps
	var uiCode uint32
	if keSt == common.I_SLICE || keSt == common.SI_SLICE {
		return ERR_NONE
	}

	// Common syntaxs for P or B slices: list0, list1 followed if B slices used.
	for {
		if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //ref_pic_list_modification_flag_l0
			return int32(r)
		}
		pRefPicListReordering.bRefPicListReorderingFlag[iList] = uiCode != 0

		if pRefPicListReordering.bRefPicListReorderingFlag[iList] {
			var iIdx int32
			for {
				if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //modification_of_pic_nums_idc
					return int32(r)
				}
				kuiIdc := uiCode

				//Fixed the referrence list reordering crash issue.(fault kIdc value > 3 case)---
				if ((iIdx >= MAX_REF_PIC_COUNT) && (kuiIdc != 3)) || (kuiIdc > 3) {
					return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_REF_REORDERING)
				}
				pRefPicListReordering.sReorderingSyn[iList][iIdx].uiReorderingOfPicNumsIdc = uint16(kuiIdc)
				if kuiIdc == 3 {
					break
				}

				if iIdx >= pSh.uiRefCount[iList] || iIdx >= MAX_REF_PIC_COUNT {
					return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_REF_REORDERING)
				}

				if kuiIdc == 0 || kuiIdc == 1 {
					// abs_diff_pic_num_minus1 should be in range 0 to MaxPicNum-1, MaxPicNum is derived as
					// 2^(4+log2_max_frame_num_minus4)
					if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //abs_diff_pic_num_minus1
						return int32(r)
					}
					if WELS_CHECK_SE_UPPER_ERROR_NOLOG(uiCode, uint32(int32(1)<<pSps.uiLog2MaxFrameNum), "abs_diff_pic_num_minus1") {
						return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_REF_REORDERING)
					}
					pRefPicListReordering.sReorderingSyn[iList][iIdx].uiAbsDiffPicNumMinus1 = uiCode // uiAbsDiffPicNumMinus1
				} else if kuiIdc == 2 {
					if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //long_term_pic_num
						return int32(r)
					}
					pRefPicListReordering.sReorderingSyn[iList][iIdx].uiLongTermPicNum = uint16(uiCode)
				}

				iIdx++
			}
		}
		if keSt != common.B_SLICE {
			break
		}
		iList++
		if !(iList < common.LIST_A) {
			break
		}
	}

	return ERR_NONE
}

// int32_t ParseDecRefPicMarking (PWelsDecoderContext pCtx, PBitStringAux pBs, PSliceHeader pSh, PSps
// pSps, const bool kbIdrFlag)
func ParseDecRefPicMarking(pCtx *SWelsDecoderContext, pBs *common.SBitStringAux, pSh *SSliceHeader, pSps *SSps, kbIdrFlag bool) int32 {
	kpRefMarking := &pSh.sRefMarking
	var uiCode uint32
	if kbIdrFlag {
		if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //no_output_of_prior_pics_flag
			return int32(r)
		}
		kpRefMarking.bNoOutputOfPriorPicsFlag = uiCode != 0
		if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //long_term_reference_flag
			return int32(r)
		}
		kpRefMarking.bLongTermRefFlag = uiCode != 0
	} else {
		if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //adaptive_ref_pic_marking_mode_flag
			return int32(r)
		}
		kpRefMarking.bAdaptiveRefPicMarkingModeFlag = uiCode != 0
		if kpRefMarking.bAdaptiveRefPicMarkingModeFlag {
			iIdx := 0
			bAllowMmco5, bMmco4Exist, bMmco5Exist, bMmco6Exist := true, false, false, false
			for {
				if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //memory_management_control_operation
					return int32(r)
				}
				kuiMmco := uiCode

				kpRefMarking.sMmcoRef[iIdx].uiMmcoType = kuiMmco
				if kuiMmco == common.MMCO_END {
					break
				}

				if kuiMmco == common.MMCO_SHORT2UNUSED || kuiMmco == common.MMCO_SHORT2LONG {
					bAllowMmco5 = false
					if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //difference_of_pic_nums_minus1
						return int32(r)
					}
					kpRefMarking.sMmcoRef[iIdx].iDiffOfPicNum = int32(1 + uiCode)
					kpRefMarking.sMmcoRef[iIdx].iShortFrameNum = (pSh.iFrameNum - kpRefMarking.sMmcoRef[iIdx].iDiffOfPicNum) &
						((int32(1) << pSps.uiLog2MaxFrameNum) - 1)
				} else if kuiMmco == common.MMCO_LONG2UNUSED {
					bAllowMmco5 = false
					if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //long_term_pic_num
						return int32(r)
					}
					kpRefMarking.sMmcoRef[iIdx].uiLongTermPicNum = uiCode
				}
				if kuiMmco == common.MMCO_SHORT2LONG || kuiMmco == common.MMCO_LONG {
					if kuiMmco == common.MMCO_LONG {
						if bMmco6Exist {
							return -1
						}
						bMmco6Exist = true
					}
					if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //long_term_frame_idx
						return int32(r)
					}
					kpRefMarking.sMmcoRef[iIdx].iLongTermFrameIdx = int32(uiCode)
				} else if kuiMmco == common.MMCO_SET_MAX_LONG {
					if bMmco4Exist {
						return -1
					}
					bMmco4Exist = true
					if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //max_long_term_frame_idx_plus1
						return int32(r)
					}
					iMaxLongTermFrameIdx := int32(uiCode - 1)
					if iMaxLongTermFrameIdx > pSps.iNumRefFrames {
						//ISO/IEC 14496-10:2009(E) 7.4.3.3 Decoded reference picture marking semantics page 96
						return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_REF_MARKING)
					}
					kpRefMarking.sMmcoRef[iIdx].iMaxLongTermFrameIdx = iMaxLongTermFrameIdx
				} else if kuiMmco == common.MMCO_RESET {
					if !bAllowMmco5 || bMmco5Exist {
						return -1
					}
					bMmco5Exist = true

					pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb = 0
					pCtx.pLastDecPicInfo.iPrevPicOrderCntMsb = 0
					pSh.iPicOrderCntLsb = 0
					if pCtx.pSliceHeader != nil {
						pCtx.pSliceHeader.iPicOrderCntLsb = 0
					}
				}
				iIdx++

				if !(iIdx < MAX_MMCO_COUNT) {
					break
				}
			}
		}
	}

	return ERR_NONE
}

// bool FillDefaultSliceHeaderExt (PSliceHeaderExt pShExt, PNalUnitHeaderExt pNalExt)
func FillDefaultSliceHeaderExt(pShExt *SSliceHeaderExt, pNalExt *common.SNalUnitHeaderExt) bool {
	if pShExt == nil || pNalExt == nil {
		return false
	}

	if pNalExt.INoInterLayerPredFlag != 0 || pNalExt.UiQualityId > 0 {
		pShExt.bBasePredWeightTableFlag = false
	} else {
		pShExt.bBasePredWeightTableFlag = true
	}
	pShExt.uiRefLayerDqId = 0xff // (uint8_t) - 1
	pShExt.uiDisableInterLayerDeblockingFilterIdc = 0
	pShExt.iInterLayerSliceAlphaC0Offset = 0
	pShExt.iInterLayerSliceBetaOffset = 0
	pShExt.bConstrainedIntraResamplingFlag = false
	pShExt.uiRefLayerChromaPhaseXPlus1Flag = 0
	pShExt.uiRefLayerChromaPhaseYPlus1 = 1
	//memset(&pShExt->sScaledRefLayer, 0, sizeof(SPosOffset));

	pShExt.iScaledRefLayerPicWidthInSampleLuma = pShExt.sSliceHeader.iMbWidth << 4
	pShExt.iScaledRefLayerPicHeightInSampleLuma = pShExt.sSliceHeader.iMbHeight << 4

	pShExt.bSliceSkipFlag = false
	pShExt.bAdaptiveBaseModeFlag = false
	pShExt.bDefaultBaseModeFlag = false
	pShExt.bAdaptiveMotionPredFlag = false
	pShExt.bDefaultMotionPredFlag = false
	pShExt.bAdaptiveResidualPredFlag = false
	pShExt.bDefaultResidualPredFlag = false
	pShExt.bTCoeffLevelPredFlag = false
	pShExt.uiScanIdxStart = 0
	pShExt.uiScanIdxEnd = 15

	return true
}

// int32_t InitBsBuffer (PWelsDecoderContext pCtx)
func InitBsBuffer(pCtx *SWelsDecoderContext) int32 {
	if pCtx == nil {
		return ERR_INFO_INVALID_PTR
	}

	pCtx.iMaxBsBufferSizeInByte = MIN_ACCESS_UNIT_CAPACITY * MAX_BUFFERED_NUM
	pCtx.sRawData.pHead = make([]uint8, pCtx.iMaxBsBufferSizeInByte)
	pCtx.sRawData.pStartPos = 0
	pCtx.sRawData.pCurPos = 0
	pCtx.sRawData.pEnd = int(pCtx.iMaxBsBufferSizeInByte)
	if pCtx.pParam.BParseOnly {
		pCtx.pParserBsInfo = &api.SParserBsInfo{}
		pCtx.pParserBsInfo.PDstBuff = make([]uint8, MAX_ACCESS_UNIT_CAPACITY)

		pCtx.sSavedData.pHead = make([]uint8, pCtx.iMaxBsBufferSizeInByte)
		pCtx.sSavedData.pStartPos = 0
		pCtx.sSavedData.pCurPos = 0
		pCtx.sSavedData.pEnd = int(pCtx.iMaxBsBufferSizeInByte)

		pCtx.iMaxNalNum = api.MAX_NAL_UNITS_IN_LAYER + 2 //2 reserved for SPS+PPS
		pCtx.pParserBsInfo.PNalLenInByte = make([]int32, pCtx.iMaxNalNum)
	}
	return ERR_NONE
}

// dcSameBuffer reports whether a and b are (views of) the same allocation
// that end at the same element (the decoder buffers are always whole
// allocations).
func dcSameBuffer(a, b []uint8) bool {
	if cap(a) == 0 || cap(b) == 0 {
		return false
	}
	return &a[:cap(a)][cap(a)-1] == &b[:cap(b)][cap(b)-1]
}

// int32_t ExpandBsBuffer (PWelsDecoderContext pCtx, const int kiSrcLen)
//
// The queued NALs' sSliceBitsRead keep their offsets; only PBuf is switched to the new buffer
// (likewise pNalPos for sSavedData).
func ExpandBsBuffer(pCtx *SWelsDecoderContext, kiSrcLen int32) int32 {
	if pCtx == nil {
		return ERR_INFO_INVALID_PTR
	}
	iExpandStepShift := int32(1)
	iNewBuffLen := common.WELS_MAX((kiSrcLen * MAX_BUFFERED_NUM), (pCtx.iMaxBsBufferSizeInByte << iExpandStepShift))
	//allocate new bs buffer

	//Realloc sRawData
	pNewBsBuff := make([]uint8, iNewBuffLen)

	// Retarget all queued NAL units in current AU list. uiAvailUnitsNum is the
	// queued-NAL count that can be consumed later; using uiActualUnitsNum here
	// can leave queued entries pointing to freed old buffer.
	kuiRetargetNum := pCtx.pAccessUnitList.uiAvailUnitsNum
	for i := uint32(0); i < kuiRetargetNum; i++ {
		pNal := pCtx.pAccessUnitList.pNalUnitsList[i]
		if pNal == nil {
			continue
		}
		pSliceBitsRead := &pNal.sNalData.sVclNal.sSliceBitsRead
		// offsets (pStartBuf / pEndBuf / pCurBuf) are relative to the buffer and stay unchanged
		pSliceBitsRead.PBuf = pNewBsBuff
	}

	//Copy current buffer status to new buffer
	copy(pNewBsBuff, pCtx.sRawData.pHead[:pCtx.iMaxBsBufferSizeInByte])
	// pStartPos / pCurPos are offsets: unchanged
	pCtx.sRawData.pEnd = int(iNewBuffLen)
	pCtx.sRawData.pHead = pNewBsBuff

	if pCtx.pParam.BParseOnly {
		//Realloc sSavedData
		pOldSavedBsBuff := pCtx.sSavedData.pHead
		pNewSavedBsBuff := make([]uint8, iNewBuffLen)

		//Copy current buffer status to new buffer
		copy(pNewSavedBsBuff, pCtx.sSavedData.pHead[:pCtx.iMaxBsBufferSizeInByte])
		pCtx.sSavedData.pEnd = int(iNewBuffLen)

		// Retarget pNalPos for all queued NALs (current AU + next AU) before freeing.
		for i := uint32(0); i < kuiRetargetNum; i++ {
			pNal := pCtx.pAccessUnitList.pNalUnitsList[i]
			if pNal == nil || pNal.sNalData.sVclNal.pNalPos == nil {
				continue
			}
			kuiNalPos := pNal.sNalData.sVclNal.iNalPosOff
			if dcSameBuffer(pNal.sNalData.sVclNal.pNalPos, pOldSavedBsBuff) &&
				kuiNalPos >= 0 && kuiNalPos < int(pCtx.iMaxBsBufferSizeInByte) {
				pNal.sNalData.sVclNal.pNalPos = pNewSavedBsBuff
			}
		}

		pCtx.sSavedData.pHead = pNewSavedBsBuff
	}

	pCtx.iMaxBsBufferSizeInByte = iNewBuffLen
	return ERR_NONE
}

// int32_t ExpandBsLenBuffer (PWelsDecoderContext pCtx, const int kiCurrLen)
func ExpandBsLenBuffer(pCtx *SWelsDecoderContext, kiCurrLen int32) int32 {
	pParser := pCtx.pParserBsInfo
	if pParser.PNalLenInByte == nil {
		return ERR_INFO_INVALID_ACCESS
	}

	iNewLen := kiCurrLen
	if kiCurrLen >= MAX_MB_SIZE+2 { //exceeds the max MB number of level 5.2
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "Current nal num (%d) exceededs %d.", kiCurrLen, MAX_MB_SIZE)
		pCtx.iErrorCode |= int32(api.DsOutOfMemory)
		return ERR_INFO_OUT_OF_MEMORY
	} else {
		iNewLen = kiCurrLen << 1
		iNewLen = common.WELS_MIN(iNewLen, MAX_MB_SIZE+2)
	}

	pNewLenBuffer := make([]int32, iNewLen)

	//copy existing data from old length buffer to new
	copy(pNewLenBuffer, pParser.PNalLenInByte[:pCtx.iMaxNalNum])
	pParser.PNalLenInByte = pNewLenBuffer
	pCtx.iMaxNalNum = iNewLen
	return ERR_NONE
}

// int32_t CheckBsBuffer (PWelsDecoderContext pCtx, const int32_t kiSrcLen)
func CheckBsBuffer(pCtx *SWelsDecoderContext, kiSrcLen int32) int32 {
	if kiSrcLen > MAX_ACCESS_UNIT_CAPACITY { //exceeds max allowed data
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "Max AU size exceeded. Allowed size = %d, current size = %d",
			MAX_ACCESS_UNIT_CAPACITY,
			kiSrcLen)
		pCtx.iErrorCode |= int32(api.DsBitstreamError)
		return ERR_INFO_INVALID_ACCESS
	} else if kiSrcLen > pCtx.iMaxBsBufferSizeInByte/
		MAX_BUFFERED_NUM { //may lead to buffer overwrite, prevent it by expanding buffer
		if ExpandBsBuffer(pCtx, kiSrcLen) != 0 {
			return ERR_INFO_OUT_OF_MEMORY
		}
	}

	return ERR_NONE
}

/*
 * WelsInitStaticMemory
 * Memory request for new introduced data
 * Especially for:
 * rbsp_au_buffer, cur_dq_layer_ptr and ref_dq_layer_ptr in MB info cache.
 * return:
 *  0 - success; otherwise returned error_no defined in error_no.h.
 */

// int32_t WelsInitStaticMemory (PWelsDecoderContext pCtx)
func WelsInitStaticMemory(pCtx *SWelsDecoderContext) int32 {
	if pCtx == nil {
		return ERR_INFO_INVALID_PTR
	}

	if MemInitNalList(&pCtx.pAccessUnitList, MAX_NAL_UNIT_NUM_IN_AU) != 0 {
		return ERR_INFO_OUT_OF_MEMORY
	}

	if InitBsBuffer(pCtx) != 0 {
		return ERR_INFO_OUT_OF_MEMORY
	}

	pCtx.uiTargetDqId = 0xff // (uint8_t) - 1
	pCtx.bEndOfStreamFlag = false

	return ERR_NONE
}

/*
 * WelsFreeStaticMemory
 * Free memory introduced in WelsInitStaticMemory at destruction of decoder.
 *
 */

// void WelsFreeStaticMemory (PWelsDecoderContext pCtx)
func WelsFreeStaticMemory(pCtx *SWelsDecoderContext) {
	if pCtx == nil {
		return
	}

	MemFreeNalList(&pCtx.pAccessUnitList)

	pCtx.sRawData.pHead = nil
	pCtx.sRawData.pEnd = 0
	pCtx.sRawData.pStartPos = 0
	pCtx.sRawData.pCurPos = 0
	if pCtx.pParam.BParseOnly {
		pCtx.sSavedData.pHead = nil
		pCtx.sSavedData.pEnd = 0
		pCtx.sSavedData.pStartPos = 0
		pCtx.sSavedData.pCurPos = 0
		if pCtx.pParserBsInfo != nil {
			if pCtx.pParserBsInfo.PNalLenInByte != nil {
				pCtx.pParserBsInfo.PNalLenInByte = nil
				pCtx.iMaxNalNum = 0
			}
			if pCtx.pParserBsInfo.PDstBuff != nil {
				pCtx.pParserBsInfo.PDstBuff = nil
			}
			pCtx.pParserBsInfo = nil
		}
	}

	if nil != pCtx.pParam {
		pCtx.pParam = nil
	}
}

/*
 *  DecodeNalHeaderExt
 *  Trigger condition: NAL_UNIT_TYPE = NAL_UNIT_PREFIX or NAL_UNIT_CODED_SLICE_EXT
 *  Parameter:
 *  pNal:   target NALUnit ptr
 *  pSrc:   NAL Unit bitstream
 */

// void DecodeNalHeaderExt (PNalUnit pNal, uint8_t* pSrc)
//
// pSrc: sub-slice starting at the C pointer.
func DecodeNalHeaderExt(pNal *SNalUnit, pSrc []uint8) {
	pHeaderExt := &pNal.sNalHeaderExt

	uiCurByte := pSrc[0]
	pHeaderExt.BIdrFlag = (uiCurByte & 0x40) != 0
	pHeaderExt.UiPriorityId = uiCurByte & 0x3F

	uiCurByte = pSrc[1]
	pHeaderExt.INoInterLayerPredFlag = int8(uiCurByte >> 7)
	pHeaderExt.UiDependencyId = (uiCurByte & 0x70) >> 4
	pHeaderExt.UiQualityId = uiCurByte & 0x0F
	uiCurByte = pSrc[2]
	pHeaderExt.UiTemporalId = uiCurByte >> 5
	pHeaderExt.BUseRefBasePicFlag = (uiCurByte & 0x10) != 0
	pHeaderExt.BDiscardableFlag = (uiCurByte & 0x08) != 0
	pHeaderExt.BOutputFlag = (uiCurByte & 0x04) != 0
	pHeaderExt.UiReservedThree2Bits = uiCurByte & 0x03
	pHeaderExt.UiLayerDqId = (pHeaderExt.UiDependencyId << 4) | pHeaderExt.UiQualityId
}

// void UpdateDecoderStatisticsForActiveParaset (SDecoderStatistics* pDecoderStatistics, PSps pSps,
// PPps pPps)
func UpdateDecoderStatisticsForActiveParaset(pDecoderStatistics *api.SDecoderStatistics, pSps *SSps, pPps *SPps) {
	pDecoderStatistics.ICurrentActiveSpsId = pSps.iSpsId

	pDecoderStatistics.ICurrentActivePpsId = pPps.iPpsId
	pDecoderStatistics.UiProfile = uint32(pSps.uiProfileIdc)
	pDecoderStatistics.UiLevel = uint32(pSps.uiLevelIdc)
}

const (
	SLICE_HEADER_IDR_PIC_ID_MAX                      = 65535
	SLICE_HEADER_REDUNDANT_PIC_CNT_MAX               = 127
	SLICE_HEADER_ALPHAC0_BETA_OFFSET_MIN             = -12
	SLICE_HEADER_ALPHAC0_BETA_OFFSET_MAX             = 12
	SLICE_HEADER_INTER_LAYER_ALPHAC0_BETA_OFFSET_MIN = -12
	SLICE_HEADER_INTER_LAYER_ALPHAC0_BETA_OFFSET_MAX = 12
	MAX_NUM_REF_IDX_L0_ACTIVE_MINUS1                 = 15
	MAX_NUM_REF_IDX_L1_ACTIVE_MINUS1                 = 15
	SLICE_HEADER_CABAC_INIT_IDC_MAX                  = 2
)

/*
 *  decode_slice_header_avc
 *  Parse slice header of bitstream in avc for storing data structure
 */

// int32_t ParseSliceHeaderSyntaxs (PWelsDecoderContext pCtx, PBitStringAux pBs, const bool
// kbExtensionFlag)
func ParseSliceHeaderSyntaxs(pCtx *SWelsDecoderContext, pBs *common.SBitStringAux, kbExtensionFlag bool) int32 {
	kpCurNal := pCtx.pAccessUnitList.pNalUnitsList[pCtx.pAccessUnitList.uiAvailUnitsNum-1]

	var pNalHeaderExt *common.SNalUnitHeaderExt
	var pSliceHead *SSliceHeader
	var pSliceHeadExt *SSliceHeaderExt
	var pSubsetSps *SSubsetSps
	var pSps *SSps
	var pPps *SPps
	var eNalType common.EWelsNalUnitType
	var iPpsId int32
	iRet := int32(ERR_NONE)
	var uiSliceType uint8
	uiQualityId := uint8(common.BASE_QUALITY_ID)
	bIdrFlag := false
	bSgChangeCycleInvolved := false // involved slice group change cycle ?
	var uiCode uint32
	var iCode int32
	pLogCtx := &(pCtx.sLogCtx)

	if kpCurNal == nil {
		return ERR_INFO_OUT_OF_MEMORY
	}

	pNalHeaderExt = &kpCurNal.sNalHeaderExt
	pSliceHead = &kpCurNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader
	eNalType = pNalHeaderExt.SNalUnitHeader.ENalUnitType

	pSliceHeadExt = &kpCurNal.sNalData.sVclNal.sSliceHeaderExt

	if pSliceHeadExt != nil {
		kbStoreRefBaseFlag := pSliceHeadExt.bStoreRefBasePicFlag
		sBaseMarking := pSliceHeadExt.sRefBasePicMarking
		*pSliceHeadExt = SSliceHeaderExt{}
		pSliceHeadExt.bStoreRefBasePicFlag = kbStoreRefBaseFlag
		pSliceHeadExt.sRefBasePicMarking = sBaseMarking
	}

	kpCurNal.sNalData.sVclNal.bSliceHeaderExtFlag = kbExtensionFlag

	// first_mb_in_slice
	if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //first_mb_in_slice
		return int32(r)
	}
	if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, 36863, "first_mb_in_slice") {
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_FIRST_MB_IN_SLICE)
	}
	pSliceHead.iFirstMbInSlice = int32(uiCode)

	if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //slice_type
		return int32(r)
	}
	uiSliceType = uint8(uiCode)
	if uiSliceType > 9 {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "slice type too large (%d) at first_mb(%d)", uiSliceType,
			pSliceHead.iFirstMbInSlice)
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_SLICE_TYPE)
	}
	if uiSliceType > 4 {
		uiSliceType -= 5
	}

	if (common.NAL_UNIT_CODED_SLICE_IDR == eNalType) && (common.I_SLICE != uiSliceType) {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "Invalid slice type(%d) in IDR picture. ", uiSliceType)
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_SLICE_TYPE)
	}

	if kbExtensionFlag {
		if uiSliceType > 2 {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "Invalid slice type(%d).", uiSliceType)
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_SLICE_TYPE)
		}
	}

	pSliceHead.eSliceType = common.EWelsSliceType(uiSliceType)

	if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //pic_parameter_set_id
		return int32(r)
	}
	if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, (MAX_PPS_COUNT - 1), "iPpsId out of range") {
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER,
			ERR_INFO_PPS_ID_OVERFLOW)
	}
	iPpsId = int32(uiCode)

	//add check PPS available here
	if pCtx.sSpsPpsCtx.bPpsAvailFlags[iPpsId] == false {
		pCtx.pDecoderStatistics.IPpsReportErrorNum++
		if pCtx.sSpsPpsCtx.iPPSLastInvalidId != iPpsId {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "PPS id (%d) is invalid, previous id (%d) error ignored (%d)!", iPpsId,
				pCtx.sSpsPpsCtx.iPPSLastInvalidId, pCtx.sSpsPpsCtx.iPPSInvalidNum)
			pCtx.sSpsPpsCtx.iPPSLastInvalidId = iPpsId
			pCtx.sSpsPpsCtx.iPPSInvalidNum = 0
		} else {
			pCtx.sSpsPpsCtx.iPPSInvalidNum++
		}
		pCtx.iErrorCode |= int32(api.DsNoParamSets)
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_PPS_ID)
	}
	pCtx.sSpsPpsCtx.iPPSLastInvalidId = -1

	pPps = &pCtx.sSpsPpsCtx.sPpsBuffer[iPpsId]

	if pPps.uiNumSliceGroups == 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "Invalid PPS referenced")
		pCtx.iErrorCode |= int32(api.DsNoParamSets)
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_NO_PARAM_SETS)
	}

	if kbExtensionFlag {
		pSubsetSps = &pCtx.sSpsPpsCtx.sSubsetSpsBuffer[pPps.iSpsId]
		pSps = &pSubsetSps.sSps
		if pCtx.sSpsPpsCtx.bSubspsAvailFlags[pPps.iSpsId] == false {
			pCtx.pDecoderStatistics.ISubSpsReportErrorNum++
			if pCtx.sSpsPpsCtx.iSubSPSLastInvalidId != pPps.iSpsId {
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "Sub SPS id (%d) is invalid, previous id (%d) error ignored (%d)!", pPps.iSpsId,
					pCtx.sSpsPpsCtx.iSubSPSLastInvalidId, pCtx.sSpsPpsCtx.iSubSPSInvalidNum)
				pCtx.sSpsPpsCtx.iSubSPSLastInvalidId = pPps.iSpsId
				pCtx.sSpsPpsCtx.iSubSPSInvalidNum = 0
			} else {
				pCtx.sSpsPpsCtx.iSubSPSInvalidNum++
			}
			pCtx.iErrorCode |= int32(api.DsNoParamSets)
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_SPS_ID)
		}
		pCtx.sSpsPpsCtx.iSubSPSLastInvalidId = -1
	} else {
		if pCtx.sSpsPpsCtx.bSpsAvailFlags[pPps.iSpsId] == false {
			pCtx.pDecoderStatistics.ISpsReportErrorNum++
			if pCtx.sSpsPpsCtx.iSPSLastInvalidId != pPps.iSpsId {
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "SPS id (%d) is invalid, previous id (%d) error ignored (%d)!", pPps.iSpsId,
					pCtx.sSpsPpsCtx.iSPSLastInvalidId, pCtx.sSpsPpsCtx.iSPSInvalidNum)
				pCtx.sSpsPpsCtx.iSPSLastInvalidId = pPps.iSpsId
				pCtx.sSpsPpsCtx.iSPSInvalidNum = 0
			} else {
				pCtx.sSpsPpsCtx.iSPSInvalidNum++
			}
			pCtx.iErrorCode |= int32(api.DsNoParamSets)
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_SPS_ID)
		}
		pCtx.sSpsPpsCtx.iSPSLastInvalidId = -1
		pSps = &pCtx.sSpsPpsCtx.sSpsBuffer[pPps.iSpsId]
	}
	pSliceHead.iPpsId = iPpsId
	pSliceHead.iSpsId = pPps.iSpsId
	pSliceHead.pPps = pPps
	pSliceHead.pSps = pSps

	pSliceHeadExt.pSubsetSps = pSubsetSps

	if pSps.iNumRefFrames == 0 {
		if (uiSliceType != common.I_SLICE) && (uiSliceType != common.SI_SLICE) {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "slice_type (%d) not supported for num_ref_frames = 0.", uiSliceType)
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_SLICE_TYPE)
		}
	}

	bIdrFlag = (!kbExtensionFlag && eNalType == common.NAL_UNIT_CODED_SLICE_IDR) || (kbExtensionFlag && pNalHeaderExt.BIdrFlag)
	pSliceHead.bIdrFlag = bIdrFlag

	if pSps.uiLog2MaxFrameNum == 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "non existing SPS referenced")
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_NO_PARAM_SETS)
	}
	// check first_mb_in_slice
	if WELS_CHECK_SE_UPPER_ERROR(pCtx, uint32(pSliceHead.iFirstMbInSlice), (pSps.uiTotalMbCount - 1), "first_mb_in_slice") {
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_FIRST_MB_IN_SLICE)
	}
	if r := BsGetBits(pBs, int32(pSps.uiLog2MaxFrameNum), &uiCode); r != ERR_NONE { //frame_num
		return r
	}
	pSliceHead.iFrameNum = int32(uiCode)

	pSliceHead.bFieldPicFlag = false
	pSliceHead.bBottomFiledFlag = false
	if !pSps.bFrameMbsOnlyFlag {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "ParseSliceHeaderSyntaxs(): frame_mbs_only_flag = %d not supported. ",
			dcBool2Int32(pSps.bFrameMbsOnlyFlag))
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_MBAFF)
	}
	pSliceHead.iMbWidth = int32(pSps.iMbWidth)
	pSliceHead.iMbHeight = int32(pSps.iMbHeight / uint32(1+dcBool2Int32(pSliceHead.bFieldPicFlag)))

	if bIdrFlag {
		if pSliceHead.iFrameNum != 0 {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"ParseSliceHeaderSyntaxs(), invaild frame number: %d due to IDR frame introduced!",
				pSliceHead.iFrameNum)
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_FRAME_NUM)
		}
		if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //idr_pic_id
			return int32(r)
		}
		// standard 7.4.3 idr_pic_id should be in range 0 to 65535, inclusive.
		if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, SLICE_HEADER_IDR_PIC_ID_MAX, "idr_pic_id") {
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER,
				ERR_INFO_INVALID_IDR_PIC_ID)
		}
		pSliceHead.uiIdrPicId = uint16(uiCode) /* uiIdrPicId */
		// LONG_TERM_REF
		pCtx.uiCurIdrPicId = pSliceHead.uiIdrPicId
	}

	pSliceHead.iDeltaPicOrderCntBottom = 0
	pSliceHead.iDeltaPicOrderCnt[0] = 0
	pSliceHead.iDeltaPicOrderCnt[1] = 0
	if pSps.uiPocType == 0 {
		if r := BsGetBits(pBs, pSps.iLog2MaxPocLsb, &uiCode); r != ERR_NONE { //pic_order_cnt_lsb
			return r
		}
		iMaxPocLsb := int32(1) << pSps.iLog2MaxPocLsb
		pSliceHead.iPicOrderCntLsb = int32(uiCode)
		if pPps.bPicOrderPresentFlag && !pSliceHead.bFieldPicFlag {
			if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //delta_pic_order_cnt_bottom
				return r
			}
			pSliceHead.iDeltaPicOrderCntBottom = iCode
		}
		//Calculate poc if necessary
		pocLsb := pSliceHead.iPicOrderCntLsb
		if pSliceHead.bIdrFlag || kpCurNal.sNalHeaderExt.SNalUnitHeader.ENalUnitType == common.NAL_UNIT_CODED_SLICE_IDR {
			pCtx.pLastDecPicInfo.iPrevPicOrderCntMsb = 0
			pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb = 0
		}
		var pocMsb int32
		if pocLsb < pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb &&
			pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb-pocLsb >= iMaxPocLsb/2 {
			pocMsb = pCtx.pLastDecPicInfo.iPrevPicOrderCntMsb + iMaxPocLsb
		} else if pocLsb > pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb &&
			pocLsb-pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb > iMaxPocLsb/2 {
			pocMsb = pCtx.pLastDecPicInfo.iPrevPicOrderCntMsb - iMaxPocLsb
		} else {
			pocMsb = pCtx.pLastDecPicInfo.iPrevPicOrderCntMsb
		}
		pSliceHead.iPicOrderCntLsb = pocMsb + pocLsb

		if pPps.bPicOrderPresentFlag && !pSliceHead.bFieldPicFlag {
			pSliceHead.iPicOrderCntLsb += pSliceHead.iDeltaPicOrderCntBottom
		}

		if kpCurNal.sNalHeaderExt.SNalUnitHeader.UiNalRefIdc != 0 {
			pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb = pocLsb
			pCtx.pLastDecPicInfo.iPrevPicOrderCntMsb = pocMsb
		}
		//End of Calculating poc
	} else if pSps.uiPocType == 1 && !pSps.bDeltaPicOrderAlwaysZeroFlag {
		if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //delta_pic_order_cnt[ 0 ]
			return r
		}
		pSliceHead.iDeltaPicOrderCnt[0] = iCode
		if pPps.bPicOrderPresentFlag && !pSliceHead.bFieldPicFlag {
			if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //delta_pic_order_cnt[ 1 ]
				return r
			}
			pSliceHead.iDeltaPicOrderCnt[1] = iCode
		}
	}
	pSliceHead.iRedundantPicCnt = 0
	if pPps.bRedundantPicCntPresentFlag {
		if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //redundant_pic_cnt
			return int32(r)
		}
		// standard section 7.4.3, redundant_pic_cnt should be in range 0 to 127, inclusive.
		if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, SLICE_HEADER_REDUNDANT_PIC_CNT_MAX, "redundant_pic_cnt") {
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_REDUNDANT_PIC_CNT)
		}
		pSliceHead.iRedundantPicCnt = int32(uiCode)
		if pSliceHead.iRedundantPicCnt > 0 {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "Redundant picture not supported!")
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_REDUNDANT_PIC_CNT)
		}
	}

	if common.B_SLICE == uiSliceType {
		//fix me: it needs to use the this flag somewhere for B-Sclice
		if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //direct_spatial_mv_pred_flag
			return int32(r)
		}
		pSliceHead.iDirectSpatialMvPredFlag = int32(uiCode)
	}

	//set defaults, might be overriden a few line later
	pSliceHead.uiRefCount[0] = int32(pPps.uiNumRefIdxL0Active)
	pSliceHead.uiRefCount[1] = int32(pPps.uiNumRefIdxL1Active)

	bReadNumRefFlag := (common.P_SLICE == uiSliceType || common.B_SLICE == uiSliceType)
	if kbExtensionFlag {
		bReadNumRefFlag = bReadNumRefFlag && (common.BASE_QUALITY_ID == pNalHeaderExt.UiQualityId)
	}
	if bReadNumRefFlag {
		if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //num_ref_idx_active_override_flag
			return int32(r)
		}
		pSliceHead.bNumRefIdxActiveOverrideFlag = uiCode != 0
		if pSliceHead.bNumRefIdxActiveOverrideFlag {
			if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //num_ref_idx_l0_active_minus1
				return int32(r)
			}
			if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, MAX_NUM_REF_IDX_L0_ACTIVE_MINUS1, "num_ref_idx_l0_active_minus1") {
				return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_NUM_REF_IDX_L0_ACTIVE_MINUS1)
			}
			pSliceHead.uiRefCount[0] = int32(1 + uiCode)
			if common.B_SLICE == uiSliceType {
				if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //num_ref_idx_l1_active_minus1
					return int32(r)
				}
				if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, MAX_NUM_REF_IDX_L1_ACTIVE_MINUS1, "num_ref_idx_l1_active_minus1") {
					return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_NUM_REF_IDX_L1_ACTIVE_MINUS1)
				}
				pSliceHead.uiRefCount[1] = int32(1 + uiCode)
			}
		}
	}

	if pSliceHead.uiRefCount[0] > MAX_REF_PIC_COUNT || pSliceHead.uiRefCount[1] > MAX_REF_PIC_COUNT {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "reference overflow")
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_REF_COUNT_OVERFLOW)
	}

	if common.BASE_QUALITY_ID == uiQualityId {
		iRet = ParseRefPicListReordering(pBs, pSliceHead)
		if iRet != ERR_NONE {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "invalid ref pPic list reordering syntaxs!")
			return iRet
		}

		if (pPps.bWeightedPredFlag && uiSliceType == common.P_SLICE) || (pPps.uiWeightedBipredIdc == 1 && uiSliceType == common.B_SLICE) {
			iRet = ParsePredWeightedTable(pBs, pSliceHead)
			if iRet != ERR_NONE {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "invalid weighted prediction syntaxs!")
				return iRet
			}
		}

		if kbExtensionFlag {
			if pNalHeaderExt.INoInterLayerPredFlag != 0 || pNalHeaderExt.UiQualityId > 0 {
				pSliceHeadExt.bBasePredWeightTableFlag = false
			} else {
				pSliceHeadExt.bBasePredWeightTableFlag = true
			}
		}

		if kpCurNal.sNalHeaderExt.SNalUnitHeader.UiNalRefIdc != 0 {
			iRet = ParseDecRefPicMarking(pCtx, pBs, pSliceHead, pSps, bIdrFlag)
			if iRet != ERR_NONE {
				return iRet
			}

			if kbExtensionFlag && !pSubsetSps.sSpsSvcExt.bSliceHeaderRestrictionFlag {
				if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //store_ref_base_pic_flag
					return int32(r)
				}
				pSliceHeadExt.bStoreRefBasePicFlag = uiCode != 0
				if (pNalHeaderExt.BUseRefBasePicFlag || pSliceHeadExt.bStoreRefBasePicFlag) && !bIdrFlag {
					common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
						"ParseSliceHeaderSyntaxs(): bUseRefBasePicFlag or bStoreRefBasePicFlag = 1 not supported.")
					return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_ILP)
				}
			}
		}
	}

	if pPps.bEntropyCodingModeFlag {
		if pSliceHead.eSliceType != common.I_SLICE && pSliceHead.eSliceType != common.SI_SLICE {
			if r := BsGetUe(pBs, &uiCode); r != ERR_NONE {
				return int32(r)
			}
			if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, SLICE_HEADER_CABAC_INIT_IDC_MAX, "cabac_init_idc") {
				return ERR_INFO_INVALID_CABAC_INIT_IDC
			}
			pSliceHead.iCabacInitIdc = int32(uiCode)
		} else {
			pSliceHead.iCabacInitIdc = 0
		}
	}

	if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //slice_qp_delta
		return r
	}
	pSliceHead.iSliceQpDelta = iCode
	pSliceHead.iSliceQp = pPps.iPicInitQp + pSliceHead.iSliceQpDelta
	if pSliceHead.iSliceQp < 0 || pSliceHead.iSliceQp > 51 {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "QP %d out of range", pSliceHead.iSliceQp)
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_QP)
	}

	//FIXME qscale / qp ... stuff
	if !kbExtensionFlag {
		if uiSliceType == common.SP_SLICE || uiSliceType == common.SI_SLICE {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "SP/SI not supported")
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_SPSI)
		}
	}

	pSliceHead.uiDisableDeblockingFilterIdc = 0
	pSliceHead.iSliceAlphaC0Offset = 0
	pSliceHead.iSliceBetaOffset = 0
	if pPps.bDeblockingFilterControlPresentFlag {
		if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //disable_deblocking_filter_idc
			return int32(r)
		}
		pSliceHead.uiDisableDeblockingFilterIdc = uiCode
		//refer to JVT-X201wcm1.doc G.7.4.3.4--2010.4.20
		if pSliceHead.uiDisableDeblockingFilterIdc > 6 {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "disable_deblock_filter_idc (%d) out of range [0, 6]",
				pSliceHead.uiDisableDeblockingFilterIdc)
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_DBLOCKING_IDC)
		}
		if pSliceHead.uiDisableDeblockingFilterIdc != 1 {
			if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //slice_alpha_c0_offset_div2
				return r
			}
			pSliceHead.iSliceAlphaC0Offset = iCode * 2
			if WELS_CHECK_SE_BOTH_ERROR(pCtx, pSliceHead.iSliceAlphaC0Offset, SLICE_HEADER_ALPHAC0_BETA_OFFSET_MIN,
				SLICE_HEADER_ALPHAC0_BETA_OFFSET_MAX, "slice_alpha_c0_offset_div2 * 2") {
				return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER,
					ERR_INFO_INVALID_SLICE_ALPHA_C0_OFFSET_DIV2)
			}
			if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //slice_beta_offset_div2
				return r
			}
			pSliceHead.iSliceBetaOffset = iCode * 2
			if WELS_CHECK_SE_BOTH_ERROR(pCtx, pSliceHead.iSliceBetaOffset, SLICE_HEADER_ALPHAC0_BETA_OFFSET_MIN,
				SLICE_HEADER_ALPHAC0_BETA_OFFSET_MAX, "slice_beta_offset_div2 * 2") {
				return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER,
					ERR_INFO_INVALID_SLICE_BETA_OFFSET_DIV2)
			}
		}
	}

	bSgChangeCycleInvolved = (pPps.uiNumSliceGroups > 1 && pPps.uiSliceGroupMapType >= 3 &&
		pPps.uiSliceGroupMapType <= 5)
	if kbExtensionFlag && bSgChangeCycleInvolved {
		bSgChangeCycleInvolved = (bSgChangeCycleInvolved && (uiQualityId == common.BASE_QUALITY_ID))
	}
	if bSgChangeCycleInvolved {
		if pPps.uiSliceGroupChangeRate > 0 {
			kiNumBits := int32(common.WELS_CEIL(math.Log(float64(1 + pPps.uiPicSizeInMapUnits/
				pPps.uiSliceGroupChangeRate))))
			if r := BsGetBits(pBs, kiNumBits, &uiCode); r != ERR_NONE { //lice_group_change_cycle
				return r
			}
			pSliceHead.iSliceGroupChangeCycle = int32(uiCode)
		} else {
			pSliceHead.iSliceGroupChangeCycle = 0
		}
	}

	if !kbExtensionFlag {
		FillDefaultSliceHeaderExt(pSliceHeadExt, pNalHeaderExt)
	} else {
		/* Extra syntax elements newly introduced */
		pSliceHeadExt.pSubsetSps = pSubsetSps

		if pNalHeaderExt.INoInterLayerPredFlag == 0 && common.BASE_QUALITY_ID == uiQualityId {
			//the following should be deleted for CODE_CLEAN
			if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //ref_layer_dq_id
				return int32(r)
			}
			pSliceHeadExt.uiRefLayerDqId = uint8(uiCode)
			if pSubsetSps.sSpsSvcExt.bInterLayerDeblockingFilterCtrlPresentFlag {
				if r := BsGetUe(pBs, &uiCode); r != ERR_NONE { //disable_inter_layer_deblocking_filter_idc
					return int32(r)
				}
				pSliceHeadExt.uiDisableInterLayerDeblockingFilterIdc = uiCode
				//refer to JVT-X201wcm1.doc G.7.4.3.4--2010.4.20
				if pSliceHeadExt.uiDisableInterLayerDeblockingFilterIdc > 6 {
					common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "disable_inter_layer_deblock_filter_idc (%d) out of range [0, 6]",
						pSliceHeadExt.uiDisableInterLayerDeblockingFilterIdc)
					return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_DBLOCKING_IDC)
				}
				if pSliceHeadExt.uiDisableInterLayerDeblockingFilterIdc != 1 {
					if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //inter_layer_slice_alpha_c0_offset_div2
						return r
					}
					pSliceHeadExt.iInterLayerSliceAlphaC0Offset = iCode * 2
					if WELS_CHECK_SE_BOTH_ERROR(pCtx, pSliceHeadExt.iInterLayerSliceAlphaC0Offset,
						SLICE_HEADER_INTER_LAYER_ALPHAC0_BETA_OFFSET_MIN, SLICE_HEADER_INTER_LAYER_ALPHAC0_BETA_OFFSET_MAX,
						"inter_layer_alpha_c0_offset_div2 * 2") {
						return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER,
							ERR_INFO_INVALID_SLICE_ALPHA_C0_OFFSET_DIV2)
					}
					if r := BsGetSe(pBs, &iCode); r != ERR_NONE { //inter_layer_slice_beta_offset_div2
						return r
					}
					pSliceHeadExt.iInterLayerSliceBetaOffset = iCode * 2
					if WELS_CHECK_SE_BOTH_ERROR(pCtx, pSliceHeadExt.iInterLayerSliceBetaOffset, SLICE_HEADER_INTER_LAYER_ALPHAC0_BETA_OFFSET_MIN,
						SLICE_HEADER_INTER_LAYER_ALPHAC0_BETA_OFFSET_MAX, "inter_layer_slice_beta_offset_div2 * 2") {
						return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_INVALID_SLICE_BETA_OFFSET_DIV2)
					}
				}
			}

			pSliceHeadExt.uiRefLayerChromaPhaseXPlus1Flag = pSubsetSps.sSpsSvcExt.uiSeqRefLayerChromaPhaseXPlus1Flag
			pSliceHeadExt.uiRefLayerChromaPhaseYPlus1 = pSubsetSps.sSpsSvcExt.uiSeqRefLayerChromaPhaseYPlus1

			if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //constrained_intra_resampling_flag
				return int32(r)
			}
			pSliceHeadExt.bConstrainedIntraResamplingFlag = uiCode != 0

			{
				var pos SPosOffset
				pos.iLeftOffset = pSubsetSps.sSpsSvcExt.sSeqScaledRefLayer.iLeftOffset
				pos.iTopOffset = pSubsetSps.sSpsSvcExt.sSeqScaledRefLayer.iTopOffset * (2 - dcBool2Int32(pSps.bFrameMbsOnlyFlag))
				pos.iRightOffset = pSubsetSps.sSpsSvcExt.sSeqScaledRefLayer.iRightOffset
				pos.iBottomOffset = pSubsetSps.sSpsSvcExt.sSeqScaledRefLayer.iBottomOffset * (2 - dcBool2Int32(pSps.bFrameMbsOnlyFlag))
				//memcpy(&pSliceHeadExt->sScaledRefLayer, &pos, sizeof(SPosOffset));//confirmed_safe_unsafe_usage
				pSliceHeadExt.iScaledRefLayerPicWidthInSampleLuma = (pSliceHead.iMbWidth << 4) -
					(pos.iLeftOffset + pos.iRightOffset)
				pSliceHeadExt.iScaledRefLayerPicHeightInSampleLuma = (pSliceHead.iMbHeight << 4) -
					(pos.iTopOffset+pos.iBottomOffset)/(1+dcBool2Int32(pSliceHead.bFieldPicFlag))
			}
		} else if uiQualityId > common.BASE_QUALITY_ID {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "MGS not supported.")
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_MGS)
		} else {
			pSliceHeadExt.uiRefLayerDqId = 0xff // (uint8_t) - 1
		}

		pSliceHeadExt.bSliceSkipFlag = false
		pSliceHeadExt.bAdaptiveBaseModeFlag = false
		pSliceHeadExt.bDefaultBaseModeFlag = false
		pSliceHeadExt.bAdaptiveMotionPredFlag = false
		pSliceHeadExt.bDefaultMotionPredFlag = false
		pSliceHeadExt.bAdaptiveResidualPredFlag = false
		pSliceHeadExt.bDefaultResidualPredFlag = false
		if pNalHeaderExt.INoInterLayerPredFlag != 0 {
			pSliceHeadExt.bTCoeffLevelPredFlag = false
		} else {
			pSliceHeadExt.bTCoeffLevelPredFlag = pSubsetSps.sSpsSvcExt.bSeqTCoeffLevelPredFlag
		}

		if pNalHeaderExt.INoInterLayerPredFlag == 0 {
			if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //slice_skip_flag
				return int32(r)
			}
			pSliceHeadExt.bSliceSkipFlag = uiCode != 0
			if pSliceHeadExt.bSliceSkipFlag {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "bSliceSkipFlag == 1 not supported.")
				return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_SLICESKIP)
			} else {
				if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //adaptive_base_mode_flag
					return int32(r)
				}
				pSliceHeadExt.bAdaptiveBaseModeFlag = uiCode != 0
				if !pSliceHeadExt.bAdaptiveBaseModeFlag {
					if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //default_base_mode_flag
						return int32(r)
					}
					pSliceHeadExt.bDefaultBaseModeFlag = uiCode != 0
				}
				if !pSliceHeadExt.bDefaultBaseModeFlag {
					if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //adaptive_motion_prediction_flag
						return int32(r)
					}
					pSliceHeadExt.bAdaptiveMotionPredFlag = uiCode != 0
					if !pSliceHeadExt.bAdaptiveMotionPredFlag {
						if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //default_motion_prediction_flag
							return int32(r)
						}
						pSliceHeadExt.bDefaultMotionPredFlag = uiCode != 0
					}
				}

				if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //adaptive_residual_prediction_flag
					return int32(r)
				}
				pSliceHeadExt.bAdaptiveResidualPredFlag = uiCode != 0
				if !pSliceHeadExt.bAdaptiveResidualPredFlag {
					if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //default_residual_prediction_flag
						return int32(r)
					}
					pSliceHeadExt.bDefaultResidualPredFlag = uiCode != 0
				}
			}
			if pSubsetSps.sSpsSvcExt.bAdaptiveTCoeffLevelPredFlag {
				if r := BsGetOneBit(pBs, &uiCode); r != ERR_NONE { //tcoeff_level_prediction_flag
					return int32(r)
				}
				pSliceHeadExt.bTCoeffLevelPredFlag = uiCode != 0
			}
		}

		if !pSubsetSps.sSpsSvcExt.bSliceHeaderRestrictionFlag {
			if r := BsGetBits(pBs, 4, &uiCode); r != ERR_NONE { //scan_idx_start
				return r
			}
			pSliceHeadExt.uiScanIdxStart = uint8(uiCode)
			if r := BsGetBits(pBs, 4, &uiCode); r != ERR_NONE { //scan_idx_end
				return r
			}
			pSliceHeadExt.uiScanIdxEnd = uint8(uiCode)
			if pSliceHeadExt.uiScanIdxStart != 0 || pSliceHeadExt.uiScanIdxEnd != 15 {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "uiScanIdxStart (%d) != 0 and uiScanIdxEnd (%d) !=15 not supported here",
					pSliceHeadExt.uiScanIdxStart, pSliceHeadExt.uiScanIdxEnd)
				return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_MGS)
			}
		} else {
			pSliceHeadExt.uiScanIdxStart = 0
			pSliceHeadExt.uiScanIdxEnd = 15
		}
	}

	return ERR_NONE
}

/*
 *  Copy relative syntax elements of NALUnitHeaderExt, sRefPicBaseMarking and bStoreRefBasePicFlag in prefix nal unit.
 *  pSrc:   mark as decoded prefix NAL
 *  ppDst:  succeeded VCL NAL based AVC (I/P Slice)
 */

// bool PrefetchNalHeaderExtSyntax (PWelsDecoderContext pCtx, PNalUnit const kppDst, PNalUnit const
// kpSrc)
func PrefetchNalHeaderExtSyntax(pCtx *SWelsDecoderContext, kppDst *SNalUnit, kpSrc *SNalUnit) bool {
	var pNalHdrExtD, pNalHdrExtS *common.SNalUnitHeaderExt
	var pShExtD *SSliceHeaderExt
	var pPrefixS *SPrefixNalUnit
	var pSps *SSps
	iIdx := 0

	if kppDst == nil || kpSrc == nil {
		return false
	}

	pNalHdrExtD = &kppDst.sNalHeaderExt
	pNalHdrExtS = &kpSrc.sNalHeaderExt
	pShExtD = &kppDst.sNalData.sVclNal.sSliceHeaderExt
	pPrefixS = &kpSrc.sNalData.sPrefixNal
	pSps = &pCtx.sSpsPpsCtx.sSpsBuffer[pCtx.sSpsPpsCtx.sPpsBuffer[pShExtD.sSliceHeader.iPpsId].iSpsId]

	pNalHdrExtD.UiDependencyId = pNalHdrExtS.UiDependencyId
	pNalHdrExtD.UiQualityId = pNalHdrExtS.UiQualityId
	pNalHdrExtD.UiTemporalId = pNalHdrExtS.UiTemporalId
	pNalHdrExtD.UiPriorityId = pNalHdrExtS.UiPriorityId
	pNalHdrExtD.BIdrFlag = pNalHdrExtS.BIdrFlag
	pNalHdrExtD.INoInterLayerPredFlag = pNalHdrExtS.INoInterLayerPredFlag
	pNalHdrExtD.BDiscardableFlag = pNalHdrExtS.BDiscardableFlag
	pNalHdrExtD.BOutputFlag = pNalHdrExtS.BOutputFlag
	pNalHdrExtD.BUseRefBasePicFlag = pNalHdrExtS.BUseRefBasePicFlag
	pNalHdrExtD.UiLayerDqId = pNalHdrExtS.UiLayerDqId

	pShExtD.bStoreRefBasePicFlag = pPrefixS.bStoreRefBasePicFlag
	pShExtD.sRefBasePicMarking = pPrefixS.sRefPicBaseMarking
	if pShExtD.sRefBasePicMarking.bAdaptiveRefBasePicMarkingModeFlag {
		pRefBasePicMarking := &pShExtD.sRefBasePicMarking
		iIdx = 0
		for {
			if pRefBasePicMarking.mmco_base[iIdx].uiMmcoType == common.MMCO_END {
				break
			}
			if pRefBasePicMarking.mmco_base[iIdx].uiMmcoType == common.MMCO_SHORT2UNUSED {
				pRefBasePicMarking.mmco_base[iIdx].iShortFrameNum = int32((uint32(pShExtD.sSliceHeader.iFrameNum) -
					pRefBasePicMarking.mmco_base[iIdx].uiDiffOfPicNums) & uint32((int32(1)<<pSps.uiLog2MaxFrameNum)-1))
			}
			iIdx++
			if !(iIdx < MAX_MMCO_COUNT) {
				break
			}
		}
	}

	return true
}

// int32_t UpdateAccessUnit (PWelsDecoderContext pCtx)
func UpdateAccessUnit(pCtx *SWelsDecoderContext) int32 {
	pCurAu := pCtx.pAccessUnitList
	iIdx := int32(pCurAu.uiEndPos)

	// Conversed iterator
	pCtx.uiTargetDqId = pCurAu.pNalUnitsList[iIdx].sNalHeaderExt.UiLayerDqId
	pCurAu.uiActualUnitsNum = uint32(iIdx + 1)
	pCurAu.bCompletedAuFlag = true

	// Added for mosaic avoidance, 11/19/2009
	// LONG_TERM_REF
	if pCtx.bParamSetsLostFlag || pCtx.bNewSeqBegin {
		uiActualIdx := uint32(0)
		for uiActualIdx < pCurAu.uiActualUnitsNum {
			nal := pCurAu.pNalUnitsList[uiActualIdx]

			if nal.sNalHeaderExt.SNalUnitHeader.ENalUnitType == common.NAL_UNIT_CODED_SLICE_IDR || nal.sNalHeaderExt.BIdrFlag {
				break
			}
			uiActualIdx++
		}
		if uiActualIdx ==
			pCurAu.uiActualUnitsNum { // no found IDR nal within incoming AU, need exit to avoid mosaic issue, 11/19/2009

			pCtx.pDecoderStatistics.UiIDRLostNum++
			if !pCtx.bParamSetsLostFlag {
				common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING,
					"UpdateAccessUnit():::::Key frame lost.....CAN NOT find IDR from current AU.")
			}
			pCtx.iErrorCode |= int32(api.DsRefLost)
			if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
				// LONG_TERM_REF
				pCtx.iErrorCode |= int32(api.DsNoParamSets)
				return int32(api.DsNoParamSets)
			}
		}
	}

	return ERR_NONE
}

// int32_t InitialDqLayersContext (PWelsDecoderContext pCtx, const int32_t kiMaxWidth, const int32_t
// kiMaxHeight)
func InitialDqLayersContext(pCtx *SWelsDecoderContext, kiMaxWidth int32, kiMaxHeight int32) int32 {
	i := 0
	if nil == pCtx || kiMaxWidth <= 0 || kiMaxHeight <= 0 {
		return ERR_INFO_INVALID_PARAM
	}
	pCtx.sMb.iMbWidth = uint32((kiMaxWidth + 15) >> 4)
	pCtx.sMb.iMbHeight = uint32((kiMaxHeight + 15) >> 4)

	if pCtx.bInitialDqLayersMem && kiMaxWidth <= pCtx.iPicWidthReq &&
		kiMaxHeight <= pCtx.iPicHeightReq { // have same dimension memory, skipped
		return ERR_NONE
	}

	UninitialDqLayersContext(pCtx)

	for {
		pDq := &SDqLayer{}

		pCtx.pDqLayersList[i] = pDq //to keep consistence with in UninitialDqLayersContext()

		n := int(pCtx.sMb.iMbWidth * pCtx.sMb.iMbHeight)
		pCtx.sMb.pMbType[i] = make([]uint32, n)
		pCtx.sMb.pMv[i][common.LIST_0] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, n)
		pCtx.sMb.pMv[i][common.LIST_1] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, n)

		pCtx.sMb.pRefIndex[i][common.LIST_0] = make([][common.MB_BLOCK4x4_NUM]int8, n)
		pCtx.sMb.pRefIndex[i][common.LIST_1] = make([][common.MB_BLOCK4x4_NUM]int8, n)
		pCtx.sMb.pDirect[i] = make([][common.MB_BLOCK4x4_NUM]int8, n)
		pCtx.sMb.pLumaQp[i] = make([]int8, n)
		pCtx.sMb.pNoSubMbPartSizeLessThan8x8Flag[i] = make([]bool, n)
		pCtx.sMb.pTransformSize8x8Flag[i] = make([]bool, n)
		pCtx.sMb.pChromaQp[i] = make([][2]int8, n)
		pCtx.sMb.pMvd[i][common.LIST_0] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, n)
		pCtx.sMb.pMvd[i][common.LIST_1] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, n)
		pCtx.sMb.pCbfDc[i] = make([]uint16, n)
		pCtx.sMb.pNzc[i] = make([][24]int8, n)
		pCtx.sMb.pNzcRs[i] = make([][24]int8, n)
		pCtx.sMb.pScaledTCoeff[i] = make([][common.MB_COEFF_LIST_SIZE]int16, n)
		pCtx.sMb.pIntraPredMode[i] = make([][8]int8, n)
		pCtx.sMb.pIntra4x4FinalMode[i] = make([][common.MB_BLOCK4x4_NUM]int8, n)
		pCtx.sMb.pIntraNxNAvailFlag[i] = make([]uint8, n)
		pCtx.sMb.pChromaPredMode[i] = make([]int8, n)
		pCtx.sMb.pCbp[i] = make([]int8, n)
		pCtx.sMb.pSubMbType[i] = make([][MB_SUB_PARTITION_SIZE]uint32, n)
		pCtx.sMb.pSliceIdc[i] = make([]int32, n) // using int32_t for slice_idc, 4/21/2010
		pCtx.sMb.pResidualPredFlag[i] = make([]int8, n)
		pCtx.sMb.pInterPredictionDoneFlag[i] = make([]int8, n)

		pCtx.sMb.pMbCorrectlyDecodedFlag[i] = make([]bool, n)
		pCtx.sMb.pMbRefConcealedFlag[i] = make([]bool, n)

		// (allocations never fail in Go: the C NULL checks are dropped)

		// memset (pCtx->sMb.pSliceIdc[i], 0xff, ...)
		for k := range pCtx.sMb.pSliceIdc[i] {
			pCtx.sMb.pSliceIdc[i][k] = -1
		}

		i++
		if !(i < LAYER_NUM_EXCHANGEABLE) {
			break
		}
	}

	pCtx.bInitialDqLayersMem = true
	pCtx.iPicWidthReq = kiMaxWidth
	pCtx.iPicHeightReq = kiMaxHeight

	return ERR_NONE
}

// void UninitialDqLayersContext (PWelsDecoderContext pCtx)
func UninitialDqLayersContext(pCtx *SWelsDecoderContext) {
	for i := 0; i < LAYER_NUM_EXCHANGEABLE; i++ {
		pDq := pCtx.pDqLayersList[i]
		if pDq == nil {
			continue
		}

		pCtx.sMb.pMbType[i] = nil

		for listIdx := common.LIST_0; listIdx < common.LIST_A; listIdx++ {
			pCtx.sMb.pMv[i][listIdx] = nil
			pCtx.sMb.pRefIndex[i][listIdx] = nil
			pCtx.sMb.pDirect[i] = nil
			pCtx.sMb.pMvd[i][listIdx] = nil
		}

		pCtx.sMb.pNoSubMbPartSizeLessThan8x8Flag[i] = nil
		pCtx.sMb.pTransformSize8x8Flag[i] = nil
		pCtx.sMb.pLumaQp[i] = nil
		pCtx.sMb.pChromaQp[i] = nil
		pCtx.sMb.pCbfDc[i] = nil
		pCtx.sMb.pNzc[i] = nil
		pCtx.sMb.pNzcRs[i] = nil
		pCtx.sMb.pScaledTCoeff[i] = nil
		pCtx.sMb.pIntraPredMode[i] = nil
		pCtx.sMb.pIntra4x4FinalMode[i] = nil
		pCtx.sMb.pIntraNxNAvailFlag[i] = nil
		pCtx.sMb.pChromaPredMode[i] = nil
		pCtx.sMb.pCbp[i] = nil
		pCtx.sMb.pSubMbType[i] = nil
		pCtx.sMb.pSliceIdc[i] = nil
		pCtx.sMb.pResidualPredFlag[i] = nil
		pCtx.sMb.pInterPredictionDoneFlag[i] = nil
		pCtx.sMb.pMbCorrectlyDecodedFlag[i] = nil
		pCtx.sMb.pMbRefConcealedFlag[i] = nil

		pCtx.pDqLayersList[i] = nil
	}

	pCtx.iPicWidthReq = 0
	pCtx.iPicHeightReq = 0
	pCtx.bInitialDqLayersMem = false
}

// void ResetCurrentAccessUnit (PWelsDecoderContext pCtx)
func ResetCurrentAccessUnit(pCtx *SWelsDecoderContext) {
	pCurAu := pCtx.pAccessUnitList
	pCurAu.uiStartPos = 0
	pCurAu.uiEndPos = 0
	pCurAu.bCompletedAuFlag = false
	if pCurAu.uiActualUnitsNum > 0 {
		iIdx := uint32(0)
		kuiActualNum := pCurAu.uiActualUnitsNum
		// a more simpler method to do nal units list management prefered here
		kuiAvailNum := pCurAu.uiAvailUnitsNum
		// Guard: counter mismatch after timeout-early-return can cause unsigned underflow.
		if kuiActualNum > kuiAvailNum {
			pCurAu.uiAvailUnitsNum = 0
			pCurAu.uiActualUnitsNum = 0
			return
		}
		kuiLeftNum := kuiAvailNum - kuiActualNum
		// Guard: swap must stay within allocated list capacity.
		kuiSwapLimit := uint32(0)
		if kuiAvailNum <= pCurAu.uiCountUnitsNum {
			kuiSwapLimit = kuiLeftNum
		}

		// Swapping active nal unit nodes of succeeding AU with leading of list
		for iIdx < kuiSwapLimit {
			t := pCurAu.pNalUnitsList[kuiActualNum+iIdx]
			pCurAu.pNalUnitsList[kuiActualNum+iIdx] = pCurAu.pNalUnitsList[iIdx]
			pCurAu.pNalUnitsList[iIdx] = t
			iIdx++
		}
		pCurAu.uiAvailUnitsNum = kuiSwapLimit
		pCurAu.uiActualUnitsNum = kuiSwapLimit
	}
}

/*!
 * \brief   Force reset current Acess Unit Nal list in case error parsing/decoding in current AU
 * \author
 * \history 11/16/2009
 */

// void ForceResetCurrentAccessUnit (PAccessUnit pAu)
func ForceResetCurrentAccessUnit(pAu *SAccessUnit) {
	uiSucAuIdx := pAu.uiEndPos + 1
	uiCurAuIdx := uint32(0)

	// swap the succeeding AU's nal units to the front
	for uiSucAuIdx < pAu.uiAvailUnitsNum {
		t := pAu.pNalUnitsList[uiSucAuIdx]
		pAu.pNalUnitsList[uiSucAuIdx] = pAu.pNalUnitsList[uiCurAuIdx]
		pAu.pNalUnitsList[uiCurAuIdx] = t
		uiSucAuIdx++
		uiCurAuIdx++
	}

	// Update avail/actual units num accordingly for next AU parsing
	if pAu.uiAvailUnitsNum > pAu.uiEndPos {
		pAu.uiAvailUnitsNum -= (pAu.uiEndPos + 1)
	} else {
		pAu.uiAvailUnitsNum = 0
	}
	pAu.uiActualUnitsNum = 0
	pAu.uiStartPos = 0
	pAu.uiEndPos = 0
	pAu.bCompletedAuFlag = false
}

// void ForceClearCurrentNal (PAccessUnit pAu)
//
// clear current corrupted NAL from pNalUnitsList
func ForceClearCurrentNal(pAu *SAccessUnit) {
	if pAu.uiAvailUnitsNum > 0 {
		pAu.uiAvailUnitsNum--
	}
}

// void ForceResetParaSetStatusAndAUList (PWelsDecoderContext pCtx)
func ForceResetParaSetStatusAndAUList(pCtx *SWelsDecoderContext) {
	pCtx.sSpsPpsCtx.bSpsExistAheadFlag = false
	pCtx.sSpsPpsCtx.bSubspsExistAheadFlag = false
	pCtx.sSpsPpsCtx.bPpsExistAheadFlag = false

	// Force clear the AU list
	pCtx.pAccessUnitList.uiAvailUnitsNum = 0
	pCtx.pAccessUnitList.uiActualUnitsNum = 0
	pCtx.pAccessUnitList.uiStartPos = 0
	pCtx.pAccessUnitList.uiEndPos = 0
	pCtx.pAccessUnitList.bCompletedAuFlag = false
}

// void CheckAvailNalUnitsListContinuity (PWelsDecoderContext pCtx, int32_t iStartIdx, int32_t iEndIdx)
func CheckAvailNalUnitsListContinuity(pCtx *SWelsDecoderContext, iStartIdx int32, iEndIdx int32) {
	pCurAu := pCtx.pAccessUnitList

	var uiLastNuDependencyId, uiLastNuLayerDqId uint8
	var uiCurNuDependencyId, uiCurNuQualityId, uiCurNuLayerDqId, uiCurNuRefLayerDqId uint8

	var iCurNalUnitIdx int32

	//check the continuity of pNalUnitsList forwards (from pIdxNoInterLayerPred to end_postion)
	uiLastNuDependencyId = pCurAu.pNalUnitsList[iStartIdx].sNalHeaderExt.UiDependencyId //starting nal unit
	uiLastNuLayerDqId = pCurAu.pNalUnitsList[iStartIdx].sNalHeaderExt.UiLayerDqId       //starting nal unit
	iCurNalUnitIdx = iStartIdx + 1                                                      //current nal unit
	for iCurNalUnitIdx <= iEndIdx {
		uiCurNuDependencyId = pCurAu.pNalUnitsList[iCurNalUnitIdx].sNalHeaderExt.UiDependencyId
		uiCurNuQualityId = pCurAu.pNalUnitsList[iCurNalUnitIdx].sNalHeaderExt.UiQualityId
		uiCurNuLayerDqId = pCurAu.pNalUnitsList[iCurNalUnitIdx].sNalHeaderExt.UiLayerDqId
		uiCurNuRefLayerDqId = pCurAu.pNalUnitsList[iCurNalUnitIdx].sNalData.sVclNal.sSliceHeaderExt.uiRefLayerDqId

		if uiCurNuDependencyId == uiLastNuDependencyId {
			uiLastNuLayerDqId = uiCurNuLayerDqId
			iCurNalUnitIdx++
		} else { //uiCurNuDependencyId != uiLastNuDependencyId, new dependency arrive
			if uiCurNuQualityId == 0 {
				uiLastNuDependencyId = uiCurNuDependencyId
				if uiCurNuRefLayerDqId == uiLastNuLayerDqId {
					uiLastNuLayerDqId = uiCurNuLayerDqId
					iCurNalUnitIdx++
				} else { //cur_nu_layer_id != next_nu_ref_layer_dq_id, the chain is broken at this point
					break
				}
			} else { //new dependency arrive, but no base quality layer, so we must stop in this point
				break
			}
		}
	}

	iCurNalUnitIdx--
	pCurAu.uiEndPos = uint32(iCurNalUnitIdx)
	pCtx.uiTargetDqId = pCurAu.pNalUnitsList[iCurNalUnitIdx].sNalHeaderExt.UiLayerDqId
}

// void RefineIdxNoInterLayerPred (PAccessUnit pCurAu, int32_t* pIdxNoInterLayerPred)
//
// main purpose: to support multi-slice and to include all slice which have the same uiDependencyId, uiQualityId and frame_num
// for single slice, pIdxNoInterLayerPred SHOULD NOT be modified
func RefineIdxNoInterLayerPred(pCurAu *SAccessUnit, pIdxNoInterLayerPred *int32) {
	iLastNalDependId := int32(pCurAu.pNalUnitsList[*pIdxNoInterLayerPred].sNalHeaderExt.UiDependencyId)
	iLastNalQualityId := int32(pCurAu.pNalUnitsList[*pIdxNoInterLayerPred].sNalHeaderExt.UiQualityId)
	uiLastNalTId := pCurAu.pNalUnitsList[*pIdxNoInterLayerPred].sNalHeaderExt.UiTemporalId
	iLastNalFrameNum :=
		pCurAu.pNalUnitsList[*pIdxNoInterLayerPred].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iFrameNum
	iLastNalPoc :=
		pCurAu.pNalUnitsList[*pIdxNoInterLayerPred].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iPicOrderCntLsb
	iLastNalFirstMb :=
		pCurAu.pNalUnitsList[*pIdxNoInterLayerPred].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice
	var iCurNalDependId, iCurNalQualityId, iCurNalTId, iCurNalFrameNum, iCurNalPoc, iCurNalFirstMb, iCurIdx,
		iFinalIdxNoInterLayerPred int32

	bMultiSliceFind := false

	iFinalIdxNoInterLayerPred = 0
	iCurIdx = *pIdxNoInterLayerPred - 1
	for iCurIdx >= 0 {
		if pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.INoInterLayerPredFlag != 0 {
			iCurNalDependId = int32(pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiDependencyId)
			iCurNalQualityId = int32(pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiQualityId)
			iCurNalTId = int32(pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiTemporalId)
			iCurNalFrameNum = pCurAu.pNalUnitsList[iCurIdx].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iFrameNum
			iCurNalPoc = pCurAu.pNalUnitsList[iCurIdx].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iPicOrderCntLsb
			iCurNalFirstMb = pCurAu.pNalUnitsList[iCurIdx].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice

			if iCurNalDependId == iLastNalDependId &&
				iCurNalQualityId == iLastNalQualityId &&
				iCurNalTId == int32(uiLastNalTId) &&
				iCurNalFrameNum == iLastNalFrameNum &&
				iCurNalPoc == iLastNalPoc &&
				iCurNalFirstMb != iLastNalFirstMb {
				bMultiSliceFind = true
				iFinalIdxNoInterLayerPred = iCurIdx
				iCurIdx--
				continue
			} else {
				break
			}
		}
		iCurIdx--
	}

	if bMultiSliceFind && *pIdxNoInterLayerPred != iFinalIdxNoInterLayerPred {
		*pIdxNoInterLayerPred = iFinalIdxNoInterLayerPred
	}
}

// bool CheckPocOfCurValidNalUnits (PAccessUnit pCurAu, int32_t pIdxNoInterLayerPred)
func CheckPocOfCurValidNalUnits(pCurAu *SAccessUnit, pIdxNoInterLayerPred int32) bool {
	iEndIdx := int32(pCurAu.uiEndPos)
	iCurAuPoc :=
		pCurAu.pNalUnitsList[pIdxNoInterLayerPred].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iPicOrderCntLsb
	var iTmpPoc, i int32
	for i = pIdxNoInterLayerPred + 1; i < iEndIdx; i++ {
		iTmpPoc = pCurAu.pNalUnitsList[i].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iPicOrderCntLsb
		if iTmpPoc != iCurAuPoc {
			return false
		}
	}

	return true
}

// bool CheckIntegrityNalUnitsList (PWelsDecoderContext pCtx)
func CheckIntegrityNalUnitsList(pCtx *SWelsDecoderContext) bool {
	pCurAu := pCtx.pAccessUnitList
	kiEndPos := int32(pCurAu.uiEndPos)
	var iIdxNoInterLayerPred int32

	if !pCurAu.bCompletedAuFlag {
		return false
	}

	if pCtx.bNewSeqBegin {
		pCurAu.uiStartPos = 0
		//step1: search the pNalUnit whose iNoInterLayerPredFlag equal to 1 backwards (from uiEndPos to 0)
		iIdxNoInterLayerPred = kiEndPos
		for iIdxNoInterLayerPred >= 0 {
			if pCurAu.pNalUnitsList[iIdxNoInterLayerPred].sNalHeaderExt.INoInterLayerPredFlag != 0 {
				break
			}
			iIdxNoInterLayerPred--
		}
		if iIdxNoInterLayerPred < 0 {
			//can not find the Nal Unit whose no_inter_pred_falg equal to 1, MUST STOP decode
			return false
		}

		//step2: support multi-slice, to include all base layer slice
		RefineIdxNoInterLayerPred(pCurAu, &iIdxNoInterLayerPred)
		pCurAu.uiStartPos = uint32(iIdxNoInterLayerPred)
		CheckAvailNalUnitsListContinuity(pCtx, iIdxNoInterLayerPred, kiEndPos)

		if !CheckPocOfCurValidNalUnits(pCurAu, iIdxNoInterLayerPred) {
			return false
		}

		pCtx.iCurSeqIntervalTargetDependId = int32(pCurAu.pNalUnitsList[pCurAu.uiEndPos].sNalHeaderExt.UiDependencyId)
		pCtx.iCurSeqIntervalMaxPicWidth =
			pCurAu.pNalUnitsList[pCurAu.uiEndPos].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iMbWidth << 4
		pCtx.iCurSeqIntervalMaxPicHeight =
			pCurAu.pNalUnitsList[pCurAu.uiEndPos].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.iMbHeight << 4
	} else { //P_SLICE
		//step 1: search uiDependencyId equal to pCtx->cur_seq_interval_target_dependency_id
		bGetDependId := false
		var iIdxDependId int32

		iIdxDependId = kiEndPos
		for iIdxDependId >= 0 {
			if pCtx.iCurSeqIntervalTargetDependId == int32(pCurAu.pNalUnitsList[iIdxDependId].sNalHeaderExt.UiDependencyId) {
				bGetDependId = true
				break
			} else {
				iIdxDependId--
			}
		}

		//step 2: switch according to whether or not find the index of pNalUnit whose uiDependencyId equal to iCurSeqIntervalTargetDependId
		if bGetDependId { //get the index of pNalUnit whose uiDependencyId equal to iCurSeqIntervalTargetDependId
			bGetNoInterPredFront := false
			//step 2a: search iNoInterLayerPredFlag [0....iIdxDependId]
			iIdxNoInterLayerPred = iIdxDependId
			for iIdxNoInterLayerPred >= 0 {
				if pCurAu.pNalUnitsList[iIdxNoInterLayerPred].sNalHeaderExt.INoInterLayerPredFlag != 0 {
					bGetNoInterPredFront = true
					break
				}
				iIdxNoInterLayerPred--
			}
			//step 2b: switch, whether or not find the NAL unit whose no_inter_pred_flag equal to 1 among [0....iIdxDependId]
			if bGetNoInterPredFront { //YES
				RefineIdxNoInterLayerPred(pCurAu, &iIdxNoInterLayerPred)
				pCurAu.uiStartPos = uint32(iIdxNoInterLayerPred)
				CheckAvailNalUnitsListContinuity(pCtx, iIdxNoInterLayerPred, iIdxDependId)

				if !CheckPocOfCurValidNalUnits(pCurAu, iIdxNoInterLayerPred) {
					return false
				}
			} else { //NO, should find the NAL unit whose no_inter_pred_flag equal to 1 among [iIdxDependId....uiEndPos]
				iIdxNoInterLayerPred = iIdxDependId
				for iIdxNoInterLayerPred <= kiEndPos {
					if pCurAu.pNalUnitsList[iIdxNoInterLayerPred].sNalHeaderExt.INoInterLayerPredFlag != 0 {
						break
					}
					iIdxNoInterLayerPred++
				}

				if iIdxNoInterLayerPred > kiEndPos {
					return false //cann't find the index of pNalUnit whose no_inter_pred_flag = 1
				}

				RefineIdxNoInterLayerPred(pCurAu, &iIdxNoInterLayerPred)
				pCurAu.uiStartPos = uint32(iIdxNoInterLayerPred)
				CheckAvailNalUnitsListContinuity(pCtx, iIdxNoInterLayerPred, kiEndPos)

				if !CheckPocOfCurValidNalUnits(pCurAu, iIdxNoInterLayerPred) {
					return false
				}
			}
		} else { //without the index of pNalUnit, should process this AU as common case
			iIdxNoInterLayerPred = kiEndPos
			for iIdxNoInterLayerPred >= 0 {
				if pCurAu.pNalUnitsList[iIdxNoInterLayerPred].sNalHeaderExt.INoInterLayerPredFlag != 0 {
					break
				}
				iIdxNoInterLayerPred--
			}
			if iIdxNoInterLayerPred < 0 {
				return false //cann't find the index of pNalUnit whose iNoInterLayerPredFlag = 1
			}

			RefineIdxNoInterLayerPred(pCurAu, &iIdxNoInterLayerPred)
			pCurAu.uiStartPos = uint32(iIdxNoInterLayerPred)
			CheckAvailNalUnitsListContinuity(pCtx, iIdxNoInterLayerPred, kiEndPos)

			if !CheckPocOfCurValidNalUnits(pCurAu, iIdxNoInterLayerPred) {
				return false
			}
		}
	}

	return true
}

// void CheckOnlyOneLayerInAu (PWelsDecoderContext pCtx)
func CheckOnlyOneLayerInAu(pCtx *SWelsDecoderContext) {
	pCurAu := pCtx.pAccessUnitList

	iEndIdx := int32(pCurAu.uiEndPos)
	iCurIdx := int32(pCurAu.uiStartPos)
	uiDId := pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiDependencyId
	uiQId := pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiQualityId
	uiTId := pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiTemporalId

	var uiCurDId, uiCurQId, uiCurTId uint8

	pCtx.bOnlyOneLayerInCurAuFlag = true

	if iEndIdx == iCurIdx { //only one NAL in pNalUnitsList
		return
	}

	iCurIdx++
	for iCurIdx <= iEndIdx {
		uiCurDId = pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiDependencyId
		uiCurQId = pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiQualityId
		uiCurTId = pCurAu.pNalUnitsList[iCurIdx].sNalHeaderExt.UiTemporalId

		if uiDId != uiCurDId || uiQId != uiCurQId || uiTId != uiCurTId {
			pCtx.bOnlyOneLayerInCurAuFlag = false
			return
		}

		iCurIdx++
	}
}

// int32_t WelsDecodeAccessUnitStart (PWelsDecoderContext pCtx)
func WelsDecodeAccessUnitStart(pCtx *SWelsDecoderContext) int32 {
	// Roll back NAL units not being belong to current access unit list for proceeded access unit
	iRet := UpdateAccessUnit(pCtx)
	if iRet != ERR_NONE {
		return iRet
	}

	pCtx.pAccessUnitList.uiStartPos = 0
	if !pCtx.sSpsPpsCtx.bAvcBasedFlag && !CheckIntegrityNalUnitsList(pCtx) {
		pCtx.iErrorCode |= int32(api.DsBitstreamError)
		return int32(api.DsBitstreamError)
	}

	//check current AU has only one layer or not
	//If YES, can use deblocking based on AVC
	if !pCtx.sSpsPpsCtx.bAvcBasedFlag {
		CheckOnlyOneLayerInAu(pCtx)
	}

	return ERR_NONE
}

// void WelsDecodeAccessUnitEnd (PWelsDecoderContext pCtx)
func WelsDecodeAccessUnitEnd(pCtx *SWelsDecoderContext) {
	//save previous header info
	pCurAu := pCtx.pAccessUnitList
	pCurNal := pCurAu.pNalUnitsList[pCurAu.uiEndPos]
	pCtx.pLastDecPicInfo.sLastNalHdrExt = pCurNal.sNalHeaderExt
	pCtx.pLastDecPicInfo.sLastSliceHeader = pCurNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader
	// uninitialize context of current access unit and rbsp buffer clean
	ResetCurrentAccessUnit(pCtx)
}

/* CheckNewSeqBeginAndUpdateActiveLayerSps
 * return:
 * true - the AU to be construct is the start of new sequence; false - not
 */
func CheckNewSeqBeginAndUpdateActiveLayerSps(pCtx *SWelsDecoderContext) bool {
	bNewSeq := false
	pCurAu := pCtx.pAccessUnitList
	var pTmpLayerSps [MAX_LAYER_NUM]*SSps
	for i := 0; i < MAX_LAYER_NUM; i++ {
		pTmpLayerSps[i] = nil
	}
	// track the layer sps for the current au
	for i := pCurAu.uiStartPos; i <= pCurAu.uiEndPos; i++ {
		uiDid := uint32(pCurAu.pNalUnitsList[i].sNalHeaderExt.UiDependencyId)
		pTmpLayerSps[uiDid] = pCurAu.pNalUnitsList[i].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps
		if (pCurAu.pNalUnitsList[i].sNalHeaderExt.SNalUnitHeader.ENalUnitType == common.NAL_UNIT_CODED_SLICE_IDR) ||
			(pCurAu.pNalUnitsList[i].sNalHeaderExt.BIdrFlag) {
			bNewSeq = true
		}
	}
	iMaxActiveLayer, iMaxCurrentLayer := 0, 0
	for i := MAX_LAYER_NUM - 1; i >= 0; i-- {
		if pCtx.sSpsPpsCtx.pActiveLayerSps[i] != nil {
			iMaxActiveLayer = i
			break
		}
	}
	for i := MAX_LAYER_NUM - 1; i >= 0; i-- {
		if pTmpLayerSps[i] != nil {
			iMaxCurrentLayer = i
			break
		}
	}
	if (iMaxCurrentLayer != iMaxActiveLayer) ||
		(pTmpLayerSps[iMaxCurrentLayer] != pCtx.sSpsPpsCtx.pActiveLayerSps[iMaxActiveLayer]) {
		bNewSeq = true
	}
	// fill active sps if the current sps is not null while active layer is null
	if !bNewSeq {
		for i := 0; i < MAX_LAYER_NUM; i++ {
			if pCtx.sSpsPpsCtx.pActiveLayerSps[i] == nil && pTmpLayerSps[i] != nil {
				pCtx.sSpsPpsCtx.pActiveLayerSps[i] = pTmpLayerSps[i]
			}
		}
	} else {
		// UpdateActiveLayerSps if new sequence start
		pCtx.sSpsPpsCtx.pActiveLayerSps = pTmpLayerSps
	}
	return bNewSeq
}

// static void WriteBackActiveParameters (PWelsDecoderContext pCtx)
func WriteBackActiveParameters(pCtx *SWelsDecoderContext) {
	if pCtx.sSpsPpsCtx.iOverwriteFlags&OVERWRITE_PPS != 0 {
		pCtx.sSpsPpsCtx.sPpsBuffer[pCtx.sSpsPpsCtx.sPpsBuffer[MAX_PPS_COUNT].iPpsId] = pCtx.sSpsPpsCtx.sPpsBuffer[MAX_PPS_COUNT]
	}
	if pCtx.sSpsPpsCtx.iOverwriteFlags&OVERWRITE_SPS != 0 {
		pCtx.sSpsPpsCtx.sSpsBuffer[pCtx.sSpsPpsCtx.sSpsBuffer[common.MAX_SPS_COUNT].iSpsId] = pCtx.sSpsPpsCtx.sSpsBuffer[common.MAX_SPS_COUNT]
		pCtx.bNewSeqBegin = true
	}
	if pCtx.sSpsPpsCtx.iOverwriteFlags&OVERWRITE_SUBSETSPS != 0 {
		pCtx.sSpsPpsCtx.sSubsetSpsBuffer[pCtx.sSpsPpsCtx.sSubsetSpsBuffer[common.MAX_SPS_COUNT].sSps.iSpsId] =
			pCtx.sSpsPpsCtx.sSubsetSpsBuffer[common.MAX_SPS_COUNT]
		pCtx.bNewSeqBegin = true
	}
	pCtx.sSpsPpsCtx.iOverwriteFlags = OVERWRITE_NONE
}

/*
 * DecodeFinishUpdate
 * decoder finish decoding, update active parameter sets and new seq status
 *
 */

// void DecodeFinishUpdate (PWelsDecoderContext pCtx)
func DecodeFinishUpdate(pCtx *SWelsDecoderContext) {
	pCtx.bNewSeqBegin = false
	WriteBackActiveParameters(pCtx)
	pCtx.bNewSeqBegin = pCtx.bNewSeqBegin || pCtx.bNextNewSeqBegin
	pCtx.bNextNewSeqBegin = false // reset it
	if pCtx.bNewSeqBegin {
		ResetActiveSPSForEachLayer(pCtx)
	}
}

/*
* WelsDecodeInitAccessUnitStart
* check and (re)allocate picture buffers on new sequence begin
*  bit_len:    size in bit length of data
*  buf_len:    size in byte length of data
*  coded_au:   mark an Access Unit decoding finished
* return:
*  0 - success; otherwise returned error_no defined in error_no.h
 */

// int32_t WelsDecodeInitAccessUnitStart (PWelsDecoderContext pCtx, SBufferInfo* pDstInfo)
func WelsDecodeInitAccessUnitStart(pCtx *SWelsDecoderContext, pDstInfo *api.SBufferInfo) int32 {
	iErr := int32(ERR_NONE)
	pCurAu := pCtx.pAccessUnitList
	pCtx.bAuReadyFlag = false
	pCtx.pLastDecPicInfo.bLastHasMmco5 = false
	bTmpNewSeqBegin := CheckNewSeqBeginAndUpdateActiveLayerSps(pCtx)
	if bTmpNewSeqBegin {
		if pCtx.pStreamSeqNum != nil {
			(*pCtx.pStreamSeqNum)++
		} else {
			pCtx.iSeqNum++
		}
	}
	pCtx.bNewSeqBegin = pCtx.bNewSeqBegin || bTmpNewSeqBegin
	if pCtx.pStreamSeqNum != nil {
		pCtx.iSeqNum = *pCtx.pStreamSeqNum
	}
	iErr = WelsDecodeAccessUnitStart(pCtx)
	GetVclNalTemporalId(pCtx)

	if ERR_NONE != iErr {
		ForceResetCurrentAccessUnit(pCtx.pAccessUnitList)
		if !pCtx.pParam.BParseOnly {
			pDstInfo.IBufferStatus = 0
		}
		pCtx.bNewSeqBegin = pCtx.bNewSeqBegin || pCtx.bNextNewSeqBegin
		pCtx.bNextNewSeqBegin = false // reset it
		if pCtx.bNewSeqBegin {
			ResetActiveSPSForEachLayer(pCtx)
		}
		return iErr
	}

	pCtx.pSps = pCurAu.pNalUnitsList[pCurAu.uiStartPos].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps
	pCtx.pPps = pCurAu.pNalUnitsList[pCurAu.uiStartPos].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pPps

	return iErr
}

/*
* AllocPicBuffOnNewSeqBegin
* check and (re)allocate picture buffers on new sequence begin
* return:
*  0 - success; otherwise returned error_no defined in error_no.h
 */

// int32_t AllocPicBuffOnNewSeqBegin (PWelsDecoderContext pCtx)
func AllocPicBuffOnNewSeqBegin(pCtx *SWelsDecoderContext) int32 {
	//try to allocate or relocate DPB memory only when new sequence is coming.
	if GetThreadCount(pCtx) <= 1 {
		WelsResetRefPic(pCtx) //clear ref pPic when IDR NAL
	}
	iErr := SyncPictureResolutionExt(pCtx, int32(pCtx.pSps.iMbWidth), int32(pCtx.pSps.iMbHeight))

	if ERR_NONE != iErr {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "sync picture resolution ext failed,  the error is %d", iErr)
		return iErr
	}

	return iErr
}

/*
* InitConstructAccessUnit
* Init before constructing an access unit for given input bitstream, maybe partial NAL Unit, one or more Units are involved to
* joint a collective access unit.
* parameter\
*  SBufferInfo:    Buffer info
* return:
*  0 - success; otherwise returned error_no defined in error_no.h
 */

// int32_t InitConstructAccessUnit (PWelsDecoderContext pCtx, SBufferInfo* pDstInfo)
func InitConstructAccessUnit(pCtx *SWelsDecoderContext, pDstInfo *api.SBufferInfo) int32 {
	iErr := int32(ERR_NONE)

	iErr = WelsDecodeInitAccessUnitStart(pCtx, pDstInfo)
	if ERR_NONE != iErr {
		return iErr
	}
	if pCtx.bNewSeqBegin {
		iErr = AllocPicBuffOnNewSeqBegin(pCtx)
		if ERR_NONE != iErr {
			return iErr
		}
	}

	return iErr
}

/*
 * ConstructAccessUnit
 * construct an access unit for given input bitstream, maybe partial NAL Unit, one or more Units are involved to
 * joint a collective access unit.
 * parameter\
 *  buf:        bitstream data buffer
 *  bit_len:    size in bit length of data
 *  buf_len:    size in byte length of data
 *  coded_au:   mark an Access Unit decoding finished
 * return:
 *  0 - success; otherwise returned error_no defined in error_no.h
 */

// int32_t ConstructAccessUnit (PWelsDecoderContext pCtx, uint8_t** ppDst, SBufferInfo* pDstInfo)
//
// ppDst: C uint8_t** (3 plane pointers); each entry is set to a slice starting at the (cropped) plane
// origin, as api.SBufferInfo.PDst.
func ConstructAccessUnit(pCtx *SWelsDecoderContext, ppDst *[3][]uint8, pDstInfo *api.SBufferInfo) int32 {
	iErr := int32(ERR_NONE)
	if GetThreadCount(pCtx) <= 1 {
		iErr = InitConstructAccessUnit(pCtx, pDstInfo)
		if ERR_NONE != iErr {
			return iErr
		}
	}
	if pCtx.pCabacDecEngine == nil {
		pCtx.pCabacDecEngine = &SWelsCabacDecEngine{}
	}

	iErr = DecodeCurrentAccessUnit(pCtx, ppDst, pDstInfo)

	WelsDecodeAccessUnitEnd(pCtx)

	if ERR_NONE != iErr {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_DEBUG, "returned error from decoding:[0x%x]", iErr)
		return iErr
	}

	return ERR_NONE
}

// static inline void InitDqLayerInfo (PDqLayer pDqLayer, PLayerInfo pLayerInfo, PNalUnit pNalUnit, PPicture pPicDec)
func InitDqLayerInfo(pDqLayer *SDqLayer, pLayerInfo *SLayerInfo, pNalUnit *SNalUnit, pPicDec *SPicture) {
	pNalHdrExt := &pNalUnit.sNalHeaderExt
	pShExt := &pNalUnit.sNalData.sVclNal.sSliceHeaderExt
	pSh := &pShExt.sSliceHeader
	kuiQualityId := pNalHdrExt.UiQualityId

	pDqLayer.sLayerInfo = *pLayerInfo

	pDqLayer.pDec = pPicDec
	pDqLayer.iMbWidth = pSh.iMbWidth   // MB width of this picture
	pDqLayer.iMbHeight = pSh.iMbHeight // MB height of this picture

	pDqLayer.iSliceIdcBackup = (pSh.iFirstMbInSlice << 7) | (int32(pNalHdrExt.UiDependencyId) << 4) | int32(pNalHdrExt.UiQualityId)

	/* Common syntax elements across all slices of a DQLayer */
	pDqLayer.uiPpsId = uint32(pLayerInfo.pPps.iPpsId)
	pDqLayer.uiDisableInterLayerDeblockingFilterIdc = pShExt.uiDisableInterLayerDeblockingFilterIdc
	pDqLayer.iInterLayerSliceAlphaC0Offset = pShExt.iInterLayerSliceAlphaC0Offset
	pDqLayer.iInterLayerSliceBetaOffset = pShExt.iInterLayerSliceBetaOffset
	pDqLayer.iSliceGroupChangeCycle = pSh.iSliceGroupChangeCycle
	pDqLayer.bStoreRefBasePicFlag = pShExt.bStoreRefBasePicFlag
	pDqLayer.bTCoeffLevelPredFlag = pShExt.bTCoeffLevelPredFlag
	pDqLayer.bConstrainedIntraResamplingFlag = pShExt.bConstrainedIntraResamplingFlag
	pDqLayer.uiRefLayerDqId = pShExt.uiRefLayerDqId
	pDqLayer.uiRefLayerChromaPhaseXPlus1Flag = pShExt.uiRefLayerChromaPhaseXPlus1Flag
	pDqLayer.uiRefLayerChromaPhaseYPlus1 = pShExt.uiRefLayerChromaPhaseYPlus1
	pDqLayer.bUseWeightPredictionFlag = false
	pDqLayer.bUseWeightedBiPredIdc = false
	//memcpy(&pDqLayer->sScaledRefLayer, &pShExt->sScaledRefLayer, sizeof(SPosOffset));//confirmed_safe_unsafe_usage

	if kuiQualityId == common.BASE_QUALITY_ID {
		pDqLayer.pRefPicListReordering = &pSh.pRefPicListReordering
		pDqLayer.pRefPicMarking = &pSh.sRefMarking

		pDqLayer.bUseWeightPredictionFlag = pSh.pPps.bWeightedPredFlag
		pDqLayer.bUseWeightedBiPredIdc = pSh.pPps.uiWeightedBipredIdc != 0
		if pSh.pPps.bWeightedPredFlag || pSh.pPps.uiWeightedBipredIdc != 0 {
			pDqLayer.pPredWeightTable = &pSh.sPredWeightTable
		}
		pDqLayer.pRefPicBaseMarking = &pShExt.sRefBasePicMarking
	}

	pDqLayer.uiLayerDqId = pNalHdrExt.UiLayerDqId // dq_id of current layer
	pDqLayer.bUseRefBasePicFlag = pNalHdrExt.BUseRefBasePicFlag
}

// void WelsDqLayerDecodeStart (PWelsDecoderContext pCtx, PNalUnit pCurNal, PSps pSps, PPps pPps)
func WelsDqLayerDecodeStart(pCtx *SWelsDecoderContext, pCurNal *SNalUnit, pSps *SSps, pPps *SPps) {
	pSh := &pCurNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader

	pCtx.eSliceType = pSh.eSliceType
	pCtx.pSliceHeader = pSh
	pCtx.bUsedAsRef = false

	pCtx.iFrameNum = pSh.iFrameNum
	UpdateDecoderStatisticsForActiveParaset(pCtx.pDecoderStatistics, pSps, pPps)
}

// int32_t InitRefPicList (PWelsDecoderContext pCtx, const uint8_t kuiNRi, int32_t iPoc)
func InitRefPicList(pCtx *SWelsDecoderContext, kuiNRi uint8, iPoc int32) int32 {
	iRet := int32(ERR_NONE)
	if pCtx.eSliceType == common.B_SLICE {
		iRet = WelsInitBSliceRefList(pCtx, iPoc)
		CreateImplicitWeightTable(pCtx)
	} else {
		iRet = WelsInitRefList(pCtx, iPoc)
	}
	if pCtx.eSliceType != common.I_SLICE && pCtx.eSliceType != common.SI_SLICE {
		if pCtx.pSps.uiProfileIdc != 66 && pCtx.pPps.bEntropyCodingModeFlag {
			iRet = WelsReorderRefList2(pCtx)
		} else {
			iRet = WelsReorderRefList(pCtx)
		}
	}

	return iRet
}

// void InitCurDqLayerData (PWelsDecoderContext pCtx, PDqLayer pCurDq)
func InitCurDqLayerData(pCtx *SWelsDecoderContext, pCurDq *SDqLayer) {
	if nil != pCtx && nil != pCurDq {
		pCurDq.pMbType = pCtx.sMb.pMbType[0]
		pCurDq.pSliceIdc = pCtx.sMb.pSliceIdc[0]
		pCurDq.pMv[common.LIST_0] = pCtx.sMb.pMv[0][common.LIST_0]
		pCurDq.pMv[common.LIST_1] = pCtx.sMb.pMv[0][common.LIST_1]
		pCurDq.pRefIndex[common.LIST_0] = pCtx.sMb.pRefIndex[0][common.LIST_0]
		pCurDq.pRefIndex[common.LIST_1] = pCtx.sMb.pRefIndex[0][common.LIST_1]
		pCurDq.pDirect = pCtx.sMb.pDirect[0]
		pCurDq.pNoSubMbPartSizeLessThan8x8Flag = pCtx.sMb.pNoSubMbPartSizeLessThan8x8Flag[0]
		pCurDq.pTransformSize8x8Flag = pCtx.sMb.pTransformSize8x8Flag[0]
		pCurDq.pLumaQp = pCtx.sMb.pLumaQp[0]
		pCurDq.pChromaQp = pCtx.sMb.pChromaQp[0]
		pCurDq.pMvd[common.LIST_0] = pCtx.sMb.pMvd[0][common.LIST_0]
		pCurDq.pMvd[common.LIST_1] = pCtx.sMb.pMvd[0][common.LIST_1]
		pCurDq.pCbfDc = pCtx.sMb.pCbfDc[0]
		pCurDq.pNzc = pCtx.sMb.pNzc[0]
		pCurDq.pNzcRs = pCtx.sMb.pNzcRs[0]
		pCurDq.pScaledTCoeff = pCtx.sMb.pScaledTCoeff[0]
		pCurDq.pIntraPredMode = pCtx.sMb.pIntraPredMode[0]
		pCurDq.pIntra4x4FinalMode = pCtx.sMb.pIntra4x4FinalMode[0]
		pCurDq.pIntraNxNAvailFlag = pCtx.sMb.pIntraNxNAvailFlag[0]
		pCurDq.pChromaPredMode = pCtx.sMb.pChromaPredMode[0]
		pCurDq.pCbp = pCtx.sMb.pCbp[0]
		pCurDq.pSubMbType = pCtx.sMb.pSubMbType[0]
		pCurDq.pInterPredictionDoneFlag = pCtx.sMb.pInterPredictionDoneFlag[0]
		pCurDq.pResidualPredFlag = pCtx.sMb.pResidualPredFlag[0]
		pCurDq.pMbCorrectlyDecodedFlag = pCtx.sMb.pMbCorrectlyDecodedFlag[0]
		pCurDq.pMbRefConcealedFlag = pCtx.sMb.pMbRefConcealedFlag[0]
	}
}

/*
 * DecodeCurrentAccessUnit
 * Decode current access unit when current AU is completed.
 */

// int32_t DecodeCurrentAccessUnit (PWelsDecoderContext pCtx, uint8_t** ppDst, SBufferInfo* pDstInfo)
//
// ppDst: see ConstructAccessUnit.
func DecodeCurrentAccessUnit(pCtx *SWelsDecoderContext, ppDst *[3][]uint8, pDstInfo *api.SBufferInfo) int32 {
	pCtx.pNalCur = nil
	var pNalCur *SNalUnit
	pCurAu := pCtx.pAccessUnitList

	iIdx := int32(pCurAu.uiStartPos)
	iEndIdx := int32(pCurAu.uiEndPos)

	// pThreadCtx / pLastThreadCtx are always nil in the single-threaded Go port.
	iThreadCount := GetThreadCount(pCtx)
	var iPpsId int32
	iRet := int32(ERR_NONE)

	bAllRefComplete := true // Assume default all ref picutres are complete

	kuiTargetLayerDqId := GetTargetDqId(pCtx.uiTargetDqId, pCtx.pParam)
	kuiDependencyIdMax := (kuiTargetLayerDqId & 0x7F) >> 4
	iLastIdD, iLastIdQ := int16(-1), int16(-1)
	iCurrIdD, iCurrIdQ := int16(0), int16(0)
	pCtx.uiNalRefIdc = 0
	bFreshSliceAvailable :=
		true // Another fresh slice comingup for given dq layer, for multiple slices in case of header parts of slices sometimes loss over error-prone channels, 8/14/2008

	//update pCurDqLayer at the starting of AU decoding
	if pCtx.bInitialDqLayersMem || pCtx.pCurDqLayer == nil {
		pCtx.pCurDqLayer = pCtx.pDqLayersList[0]
	}

	InitCurDqLayerData(pCtx, pCtx.pCurDqLayer)

	pNalCur = pCurAu.pNalUnitsList[iIdx]
	for iIdx <= iEndIdx {
		dq_cur := pCtx.pCurDqLayer
		var pLayerInfo SLayerInfo
		var pShExt *SSliceHeaderExt
		var pSh *SSliceHeader
		isNewFrame := true
		// iThreadCount > 1: isNewFrame = pCtx->pDec == NULL (dropped)
		if pCtx.pDec == nil {
			//make call PrefetchPic first before updating reference lists in threaded mode
			//this prevents from possible thread-decoding hanging
			pCtx.pDec = PrefetchPic(pCtx.pPicBuff)
			// pLastThreadCtx != NULL branch dropped (single-threaded).
			// GetThreadCount (pCtx) > 1 && pCtx->bNewSeqBegin: WelsResetRefPic dropped (single-threaded).
			if pCtx.iTotalNumMbRec != 0 {
				pCtx.iTotalNumMbRec = 0
			}

			if nil == pCtx.pDec {
				common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR,
					"DecodeCurrentAccessUnit()::::::PrefetchPic ERROR, pSps->iNumRefFrames:%d.",
					pCtx.pSps.iNumRefFrames)
				// The error code here need to be separated from the dsOutOfMemory
				pCtx.iErrorCode |= int32(api.DsOutOfMemory)
				return ERR_INFO_REF_COUNT_OVERFLOW
			}
			// pThreadCtx != NULL branch dropped (single-threaded).
			pCtx.pDec.bNewSeqBegin = pCtx.bNewSeqBegin //set flag for start decoding
		} else if pCtx.iTotalNumMbRec == 0 { //pDec != NULL, already start
			pCtx.pDec.bNewSeqBegin = pCtx.bNewSeqBegin //set flag for start decoding
		}
		pCtx.pDec.uiTimeStamp = pNalCur.uiTimeStamp
		pCtx.pDec.uiDecodingTimeStamp = pCtx.uiDecodingTimeStamp
		// pThreadCtx != NULL branch dropped (single-threaded).

		if pCtx.iTotalNumMbRec == 0 { //Picture start to decode
			kiMbCount := int(pCtx.sMb.iMbWidth * pCtx.sMb.iMbHeight)
			for i := 0; i < LAYER_NUM_EXCHANGEABLE; i++ {
				for k := 0; k < kiMbCount; k++ {
					pCtx.sMb.pSliceIdc[i][k] = -1
				}
			}
			kiSpsMbCount := int(pCtx.pSps.iMbWidth * pCtx.pSps.iMbHeight)
			clear(pCtx.pCurDqLayer.pMbCorrectlyDecodedFlag[:kiSpsMbCount])
			clear(pCtx.pCurDqLayer.pMbRefConcealedFlag[:kiSpsMbCount])
			pCtx.pDec.pRefPic[common.LIST_0] = [MAX_DPB_COUNT]*SPicture{}
			pCtx.pDec.pRefPic[common.LIST_1] = [MAX_DPB_COUNT]*SPicture{}
			pCtx.pDec.iMbNum = int32(pCtx.pSps.iMbWidth * pCtx.pSps.iMbHeight)
			pCtx.pDec.iMbEcedNum = 0
			pCtx.pDec.iMbEcedPropNum = 0
		}
		pCtx.bRPLRError = false
		GetI4LumaIChromaAddrTable(pCtx.iDecBlockOffsetArray[:], pCtx.pDec.iLinesize[0], pCtx.pDec.iLinesize[1])

		if pNalCur.sNalHeaderExt.UiLayerDqId > kuiTargetLayerDqId { // confirmed pNalCur will never be NULL
			break // Per formance it need not to decode the remaining bits any more due to given uiLayerDqId required, 9/2/2009
		}

		pLayerInfo = SLayerInfo{}

		/*
		 *  Loop decoding for slices (even FMO and/ multiple slices) within a dq layer
		 */
		for iIdx <= iEndIdx {
			var bReconstructSlice bool
			iCurrIdQ = int16(pNalCur.sNalHeaderExt.UiQualityId)
			iCurrIdD = int16(pNalCur.sNalHeaderExt.UiDependencyId)
			pSh = &pNalCur.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader
			pShExt = &pNalCur.sNalData.sVclNal.sSliceHeaderExt
			pCtx.bRPLRError = false
			bReconstructSlice = CheckSliceNeedReconstruct(pNalCur.sNalHeaderExt.UiLayerDqId, kuiTargetLayerDqId)

			pLayerInfo.sNalHeaderExt = pNalCur.sNalHeaderExt

			pCtx.pDec.iFrameNum = pSh.iFrameNum
			pCtx.pDec.iFramePoc = pSh.iPicOrderCntLsb // still can not obtain correct, because current do not support POCtype 2
			pCtx.pDec.bIdrFlag = pNalCur.sNalHeaderExt.BIdrFlag
			pCtx.pDec.eSliceType = pSh.eSliceType

			pLayerInfo.sSliceInLayer.sSliceHeaderExt = *pShExt
			pLayerInfo.sSliceInLayer.bSliceHeaderExtFlag = pNalCur.sNalData.sVclNal.bSliceHeaderExtFlag
			pLayerInfo.sSliceInLayer.eSliceType = uint8(pSh.eSliceType)
			pLayerInfo.sSliceInLayer.iLastMbQp = pSh.iSliceQp
			dq_cur.pBitStringAux = &pNalCur.sNalData.sVclNal.sSliceBitsRead

			pCtx.uiNalRefIdc = pNalCur.sNalHeaderExt.SNalUnitHeader.UiNalRefIdc

			iPpsId = pSh.iPpsId

			pLayerInfo.pPps = pSh.pPps
			pLayerInfo.pSps = pSh.pSps
			pLayerInfo.pSubsetSps = pShExt.pSubsetSps

			pCtx.pFmo = &pCtx.sFmoList[iPpsId]
			iRet = FmoParamUpdate(pCtx.pFmo, pLayerInfo.pSps, pLayerInfo.pPps, &pCtx.iActiveFmoNum)
			if ERR_NONE != iRet {
				if iRet == ERR_INFO_OUT_OF_MEMORY {
					pCtx.iErrorCode |= int32(api.DsOutOfMemory)
					common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "DecodeCurrentAccessUnit(), Fmo param alloc failed")
				} else {
					pCtx.iErrorCode |= int32(api.DsBitstreamError)
					common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "DecodeCurrentAccessUnit(), FmoParamUpdate failed, eSliceType: %d.",
						pSh.eSliceType)
				}
				return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_FMO_INIT_FAIL)
			}

			bFreshSliceAvailable = (iCurrIdD != iLastIdD ||
				iCurrIdQ != iLastIdQ) // do not need condition of (first_mb == 0) due multiple slices might be disorder

			WelsDqLayerDecodeStart(pCtx, pNalCur, pLayerInfo.pSps, pLayerInfo.pPps)

			if (iLastIdD < 0) || //case 1: first layer
				(iLastIdD == iCurrIdD) { //case 2: same uiDId
				InitDqLayerInfo(dq_cur, &pLayerInfo, pNalCur, pCtx.pDec)

				if !dq_cur.sLayerInfo.pSps.bGapsInFrameNumValueAllowedFlag {
					kbIdrFlag := dq_cur.sLayerInfo.sNalHeaderExt.BIdrFlag ||
						(dq_cur.sLayerInfo.sNalHeaderExt.SNalUnitHeader.ENalUnitType == common.NAL_UNIT_CODED_SLICE_IDR)
					// Subclause 8.2.5.2 Decoding process for gaps in frame_num
					iPrevFrameNum := pCtx.pLastDecPicInfo.iPrevFrameNum
					// pLastThreadCtx != NULL branch dropped (single-threaded).
					if !kbIdrFlag &&
						pSh.iFrameNum != iPrevFrameNum &&
						pSh.iFrameNum != ((iPrevFrameNum+1)&((int32(1)<<dq_cur.sLayerInfo.pSps.uiLog2MaxFrameNum)-
							1)) {
						common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING,
							"referencing pictures lost due frame gaps exist, prev_frame_num: %d, curr_frame_num: %d",
							iPrevFrameNum,
							pSh.iFrameNum)

						bAllRefComplete = false
						pCtx.iErrorCode |= int32(api.DsRefLost)
						if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
							// LONG_TERM_REF
							pCtx.bParamSetsLostFlag = true
							return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_REFERENCE_PIC_LOST)
						}
					}
				}

				if int32(iCurrIdD) == int32(kuiDependencyIdMax) && iCurrIdQ == common.BASE_QUALITY_ID && isNewFrame {
					iRet = InitRefPicList(pCtx, pCtx.uiNalRefIdc, pSh.iPicOrderCntLsb)
					if iRet != 0 {
						pCtx.bRPLRError = true
						bAllRefComplete = false // RPLR error, set ref pictures complete flag false
						HandleReferenceLost(pCtx, pNalCur)
						common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_DEBUG,
							"reference picture introduced by this frame is lost during transmission! uiTId: %d",
							pNalCur.sNalHeaderExt.UiTemporalId)
						if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
							if pCtx.iTotalNumMbRec == 0 {
								pCtx.pDec = nil
							}
							return iRet
						}
					}
				}
				//calculate Colocated mv scaling factor for temporal direct prediction
				if pSh.eSliceType == common.B_SLICE && pSh.iDirectSpatialMvPredFlag == 0 {
					ComputeColocatedTemporalScaling(pCtx)
				}

				// iThreadCount > 1: WelsDecodeAndConstructSlice (dropped, single-threaded)
				iRet = WelsDecodeSlice(pCtx, bFreshSliceAvailable, pNalCur)

				//Output good store_base reconstruction when enhancement quality layer occurred error for MGS key picture case
				if iRet != ERR_NONE {
					common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING,
						"DecodeCurrentAccessUnit() failed (%d) in frame: %d uiDId: %d uiQId: %d",
						iRet, pSh.iFrameNum, iCurrIdD, iCurrIdQ)
					bAllRefComplete = false
					HandleReferenceLostL0(pCtx, pNalCur)
					if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
						if pCtx.iTotalNumMbRec == 0 {
							pCtx.pDec = nil
						}
						return iRet
					}
				}

				if iThreadCount <= 1 && bReconstructSlice {
					if iRet = WelsDecodeConstructSlice(pCtx, pNalCur); iRet != ERR_NONE {
						pCtx.pDec.bIsComplete = false // reconstruction error, directly set the flag false
						return iRet
					}
				}
				if bAllRefComplete && pCtx.eSliceType != common.I_SLICE {
					if iThreadCount <= 1 {
						if pCtx.sRefPic.uiRefCount[common.LIST_0] > 0 {
							bComplete := CheckRefPicturesComplete(pCtx)
							bAllRefComplete = bAllRefComplete && bComplete
						} else {
							bAllRefComplete = false
						}
					}
				}
			}
			iLastIdD = iCurrIdD
			iLastIdQ = iCurrIdQ

			//pNalUnitsList overflow.
			iIdx++
			if iIdx <= iEndIdx {
				pNalCur = pCurAu.pNalUnitsList[iIdx]
			} else {
				pNalCur = nil
			}

			if pNalCur == nil ||
				int32(iLastIdD) != int32(pNalCur.sNalHeaderExt.UiDependencyId) ||
				int32(iLastIdQ) != int32(pNalCur.sNalHeaderExt.UiQualityId) {
				break
			}
		}

		// Set the current dec picture complete flag. The flag will be reset when current picture need do ErrorCon.
		pCtx.pDec.bIsComplete = bAllRefComplete
		if !pCtx.pDec.bIsComplete { // Ref pictures ECed, result in ECed
			pCtx.iErrorCode |= int32(api.DsDataErrorConcealed)
		}

		// A dq layer decoded here

		if dq_cur.uiLayerDqId == kuiTargetLayerDqId {
			if !pCtx.bInstantDecFlag {
				if !pCtx.pParam.BParseOnly {
					//Do error concealment here
					if (NeedErrorCon(pCtx)) && (pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE) {
						ImplementErrorCon(pCtx)
						pCtx.iTotalNumMbRec = int32(pCtx.pSps.iMbWidth * pCtx.pSps.iMbHeight)
						pCtx.pDec.iSpsId = pCtx.pSps.iSpsId
						pCtx.pDec.iPpsId = pCtx.pPps.iPpsId
					}
				}
			}

			// iThreadCount >= 1 branch (wait for other threads' slice decoding) dropped:
			// GetThreadCount() is always 0 in the Go port.
			iRet = DecodeFrameConstruction(pCtx, ppDst, pDstInfo)
			if iRet != 0 {
				return iRet
			}

			pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = pCtx.pDec //store latest decoded picture for EC
			pCtx.bUsedAsRef = pCtx.uiNalRefIdc > 0
			if iThreadCount <= 1 {
				if pCtx.bUsedAsRef {
					for listIdx := common.LIST_0; listIdx < common.LIST_A; listIdx++ {
						i := 0
						for i < MAX_DPB_COUNT && pCtx.sRefPic.pRefList[listIdx][i] != nil {
							pCtx.pDec.pRefPic[listIdx][i] = pCtx.sRefPic.pRefList[listIdx][i]
							i++
						}
					}
					iRet = WelsMarkAsRef(pCtx, nil)
					if iRet != ERR_NONE {
						if iRet == ERR_INFO_DUPLICATE_FRAME_NUM {
							pCtx.iErrorCode |= int32(api.DsBitstreamError)
						}
						if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
							pCtx.pDec = nil
							return iRet
						}
					}
					if !pCtx.pParam.BParseOnly {
						common.ExpandReferencingPicture(pCtx.pDec.pData[:], pCtx.pDec.iDataOff[:], pCtx.pDec.iWidthInPixel, pCtx.pDec.iHeightInPixel,
							pCtx.pDec.iLinesize[:],
							pCtx.sExpandPicFunc.PfExpandLumaPicture, pCtx.sExpandPicFunc.PfExpandChromaPicture)
					}
				}
			}
			pCtx.pDec = nil //after frame decoding, always set to NULL
		}

		// need update frame_num due current frame is well decoded
		if pCurAu.pNalUnitsList[pCurAu.uiStartPos].sNalHeaderExt.SNalUnitHeader.UiNalRefIdc > 0 {
			pCtx.pLastDecPicInfo.iPrevFrameNum = pSh.iFrameNum
		}
		if pCtx.pLastDecPicInfo.bLastHasMmco5 {
			pCtx.pLastDecPicInfo.iPrevFrameNum = 0
		}
		// iThreadCount > 1 branches (CopySpsPps from newer thread contexts, events) dropped.
	}
	return ERR_NONE
}

// bool CheckAndFinishLastPic (PWelsDecoderContext pCtx, uint8_t** ppDst, SBufferInfo* pDstInfo)
//
// ppDst: see ConstructAccessUnit.
//
// Note: the C function returns ERR_NONE (== false) on its normal path.
func CheckAndFinishLastPic(pCtx *SWelsDecoderContext, ppDst *[3][]uint8, pDstInfo *api.SBufferInfo) bool {
	pAu := pCtx.pAccessUnitList
	bAuBoundaryFlag := false
	if common.IS_VCL_NAL(pCtx.sCurNalHead.ENalUnitType, 1) { //VCL data, AU list should have data
		pCurNal := pAu.pNalUnitsList[pAu.uiEndPos]
		bAuBoundaryFlag = (pCtx.iTotalNumMbRec != 0) &&
			(CheckAccessUnitBoundaryExt(&pCtx.pLastDecPicInfo.sLastNalHdrExt, &pCurNal.sNalHeaderExt,
				&pCtx.pLastDecPicInfo.sLastSliceHeader,
				&pCurNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader))
	} else { //non VCL
		if pCtx.sCurNalHead.ENalUnitType == common.NAL_UNIT_AU_DELIMITER {
			bAuBoundaryFlag = true
		} else if pCtx.sCurNalHead.ENalUnitType == common.NAL_UNIT_SEI {
			bAuBoundaryFlag = true
		} else if pCtx.sCurNalHead.ENalUnitType == common.NAL_UNIT_SPS {
			bAuBoundaryFlag = (pCtx.sSpsPpsCtx.iOverwriteFlags & OVERWRITE_SPS) != 0
		} else if pCtx.sCurNalHead.ENalUnitType == common.NAL_UNIT_SUBSET_SPS {
			bAuBoundaryFlag = (pCtx.sSpsPpsCtx.iOverwriteFlags & OVERWRITE_SUBSETSPS) != 0
		} else if pCtx.sCurNalHead.ENalUnitType == common.NAL_UNIT_PPS {
			bAuBoundaryFlag = (pCtx.sSpsPpsCtx.iOverwriteFlags & OVERWRITE_PPS) != 0
		}
		if bAuBoundaryFlag && pCtx.pAccessUnitList.uiAvailUnitsNum != 0 { //Construct remaining data first
			ConstructAccessUnit(pCtx, ppDst, pDstInfo)
		}
	}

	//Do Error Concealment here
	if bAuBoundaryFlag && (pCtx.iTotalNumMbRec != 0) && NeedErrorCon(pCtx) { //AU ready but frame not completely reconed
		if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
			ImplementErrorCon(pCtx)
			pCtx.iTotalNumMbRec = int32(pCtx.pSps.iMbWidth * pCtx.pSps.iMbHeight)
			pCtx.pDec.iSpsId = pCtx.pSps.iSpsId
			pCtx.pDec.iPpsId = pCtx.pPps.iPpsId

			DecodeFrameConstruction(pCtx, ppDst, pDstInfo)
			pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = pCtx.pDec //save ECed pic for future use
			// pThreadCtx != NULL && GetThreadCount (pCtx) > 1 branch dropped (single-threaded).
			if pCtx.pLastDecPicInfo.sLastNalHdrExt.SNalUnitHeader.UiNalRefIdc > 0 {
				if MarkECFrameAsRef(pCtx) == ERR_INFO_INVALID_PTR {
					pCtx.iErrorCode |= int32(api.DsRefListNullPtrs)
					return false
				}
			}
		} else if pCtx.pParam.BParseOnly { //clear parse only internal data status
			pCtx.pParserBsInfo.INalNum = 0
			pCtx.bFrameFinish = true //clear frame pending status here!
		} else {
			if DecodeFrameConstruction(pCtx, ppDst, pDstInfo) != 0 {
				if (pCtx.pLastDecPicInfo.sLastNalHdrExt.SNalUnitHeader.UiNalRefIdc > 0) &&
					(pCtx.pLastDecPicInfo.sLastNalHdrExt.UiTemporalId == 0) {
					pCtx.iErrorCode |= int32(api.DsNoParamSets)
				} else {
					pCtx.iErrorCode |= int32(api.DsBitstreamError)
				}
				pCtx.pDec = nil
				return false
			}
		}
		pCtx.pDec = nil
		if pAu.pNalUnitsList[pAu.uiStartPos].sNalHeaderExt.SNalUnitHeader.UiNalRefIdc > 0 {
			pCtx.pLastDecPicInfo.iPrevFrameNum = pCtx.pLastDecPicInfo.sLastSliceHeader.iFrameNum //save frame_num
		}
		if pCtx.pLastDecPicInfo.bLastHasMmco5 {
			pCtx.pLastDecPicInfo.iPrevFrameNum = 0
		}
	}
	return ERR_NONE != 0
}

// dcRefComplete returns pCtx->sRefPic.pRefList[LIST_0][iRefIdx]->bIsComplete.
// The C code dereferences without checks; an invalid index or a NULL entry
// (which would crash in C) is reported as an incomplete reference here.
func dcRefComplete(pCtx *SWelsDecoderContext, iRefIdx int8) bool {
	if iRefIdx < 0 || int(iRefIdx) >= MAX_DPB_COUNT {
		return false
	}
	pRef := pCtx.sRefPic.pRefList[common.LIST_0][iRefIdx]
	if pRef == nil {
		return false
	}
	return pRef.bIsComplete
}

// bool CheckRefPicturesComplete (PWelsDecoderContext pCtx)
func CheckRefPicturesComplete(pCtx *SWelsDecoderContext) bool {
	// Multi Reference, RefIdx may differ
	bAllRefComplete := true
	iRealMbIdx := pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice
	for iMbIdx := int32(0); bAllRefComplete &&
		iMbIdx < pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer.iTotalMbInCurSlice; iMbIdx++ {
		pRefIndex0 := &pCtx.pCurDqLayer.pDec.pRefIndex[0][iRealMbIdx]
		switch pCtx.pCurDqLayer.pDec.pMbType[iRealMbIdx] {
		case common.MB_TYPE_SKIP, common.MB_TYPE_16x16:
			b0 := dcRefComplete(pCtx, pRefIndex0[0])
			bAllRefComplete = bAllRefComplete && b0

		case common.MB_TYPE_16x8:
			b0 := dcRefComplete(pCtx, pRefIndex0[0])
			bAllRefComplete = bAllRefComplete && b0
			b8 := dcRefComplete(pCtx, pRefIndex0[8])
			bAllRefComplete = bAllRefComplete && b8

		case common.MB_TYPE_8x16:
			b0 := dcRefComplete(pCtx, pRefIndex0[0])
			bAllRefComplete = bAllRefComplete && b0
			b2 := dcRefComplete(pCtx, pRefIndex0[2])
			bAllRefComplete = bAllRefComplete && b2

		case common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0:
			b0 := dcRefComplete(pCtx, pRefIndex0[0])
			bAllRefComplete = bAllRefComplete && b0
			b2 := dcRefComplete(pCtx, pRefIndex0[2])
			bAllRefComplete = bAllRefComplete && b2
			b8 := dcRefComplete(pCtx, pRefIndex0[8])
			bAllRefComplete = bAllRefComplete && b8
			b10 := dcRefComplete(pCtx, pRefIndex0[10])
			bAllRefComplete = bAllRefComplete && b10

		default:
		}
		if pCtx.pPps.uiNumSliceGroups > 1 {
			iRealMbIdx = FmoNextMb(pCtx.pFmo, iRealMbIdx)
		} else {
			iRealMbIdx = pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice + iMbIdx
		}
		if iRealMbIdx == -1 { //caused by abnormal return of FmoNextMb()
			return false
		}
	}

	return bAllRefComplete
}
