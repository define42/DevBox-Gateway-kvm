// Port of codec/decoder/core/src/au_parser.cpp.
//
// Interfaces introduced in Access Unit parsing: NAL header parsing,
// SPS/subset SPS/PPS/VUI/scaling list parsing, prefix NAL parsing etc.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// _PARSE_NALHRD_VCLHRD_PARAMS_ is defined (1) in the C source: HRD parameters
// in the VUI are parsed (and dropped) instead of being rejected.

// auParserB2I converts a bool to an int for log output (C prints bools as %d).
func auParserB2I(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// DetectStartCodePrefix ports uint8_t* DetectStartCodePrefix (const uint8_t* kpBuf, int32_t* pOffset,
// int32_t iBufSize): Start Code Prefix (0x 00 00 00 01) detection.
//
// Returns kpBuf[*pOffset:] (the byte after the start code) or nil (C: NULL).
func DetectStartCodePrefix(kpBuf []uint8, pOffset *int32, iBufSize int32) []uint8 {
	pBits := 0

	for {
		var iIdx int32
		for (iIdx < iBufSize) && (kpBuf[pBits] == 0) {
			pBits++
			iIdx++
		}
		if iIdx >= iBufSize {
			break
		}

		iIdx++
		pBits++

		if (iIdx >= 3) && (kpBuf[pBits-1] == 0x1) {
			*pOffset = int32(pBits)
			return kpBuf[pBits:]
		}

		iBufSize -= iIdx
	}

	return nil
}

// ParseNalHeader ports uint8_t* ParseNalHeader (PWelsDecoderContext pCtx, SNalUnitHeader*
// pNalUnitHeader, uint8_t* pSrcRbsp, int32_t iSrcRbspLen, uint8_t* pSrcNal, int32_t iSrcNalLen,
// int32_t* pConsumedBytes): to parse nal unit.
//
// pSrcRbsp/iSrcRbspOff: (slice, offset) into pCtx.sRawData.pHead (the slice bit reader keeps offsets
// relative to it). pSrcNal: sub-slice of the input starting at the C pointer (pSrcNal - 3 in
// WelsDecodeBs). Returns the payload as a (slice, offset) pair (pSrcRbsp, offset); (nil, 0) is C's
// NULL.
func ParseNalHeader(pCtx *SWelsDecoderContext, pNalUnitHeader *common.SNalUnitHeader, pSrcRbsp []uint8, iSrcRbspOff int, iSrcRbspLen int32, pSrcNal []uint8, iSrcNalLen int32, pConsumedBytes *int32) ([]uint8, int) {
	var pCurNal *SNalUnit
	pNal := iSrcRbspOff // offset of the C pointer pNal inside pSrcRbsp
	iNalSize := iSrcRbspLen
	var pBs *common.SBitStringAux
	bExtensionFlag := false
	var iErr int32 = ERR_NONE
	var iBitSize int32
	pSavedData := &pCtx.sSavedData
	pLogCtx := &pCtx.sLogCtx
	pNalUnitHeader.ENalUnitType = common.NAL_UNIT_UNSPEC_0 //SHOULD init it. because pCtx->sCurNalHead is common variable.

	//remove the consecutive ZERO at the end of current NAL in the reverse order.--2011.6.1
	{
		iIndex := iSrcRbspLen - 1
		var uiBsZero uint8
		for iIndex >= 0 {
			uiBsZero = pSrcRbsp[iSrcRbspOff+int(iIndex)]
			if uiBsZero == 0 {
				iNalSize--
				*pConsumedBytes++
				iIndex--
			} else {
				break
			}
		}
	}

	pNalUnitHeader.UiForbiddenZeroBit = pSrcRbsp[pNal] >> 7 // uiForbiddenZeroBit
	if pNalUnitHeader.UiForbiddenZeroBit != 0 {             //2010.4.14
		pCtx.iErrorCode |= int32(api.DsBitstreamError)
		return nil, 0 //uiForbiddenZeroBit should always equal to 0
	}

	pNalUnitHeader.UiNalRefIdc = pSrcRbsp[pNal] >> 5                             // uiNalRefIdc
	pNalUnitHeader.ENalUnitType = common.EWelsNalUnitType(pSrcRbsp[pNal] & 0x1f) // eNalUnitType

	pNal++
	iNalSize--
	*pConsumedBytes++

	eNalUnitType := pNalUnitHeader.ENalUnitType
	if !(common.IS_SEI_NAL(eNalUnitType) || common.IS_SPS_NAL(eNalUnitType) ||
		common.IS_AU_DELIMITER_NAL(eNalUnitType) || pCtx.sSpsPpsCtx.bSpsExistAheadFlag) {
		if pCtx.bPrintFrameErrorTraceFlag && pCtx.sSpsPpsCtx.iSpsErrorIgnored == 0 {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"parse_nal(), no exist Sequence Parameter Sets ahead of sequence when try to decode NAL(type:%d).",
				eNalUnitType)
		} else {
			pCtx.sSpsPpsCtx.iSpsErrorIgnored++
		}
		pCtx.pDecoderStatistics.ISpsNoExistNalNum++
		pCtx.iErrorCode = int32(api.DsNoParamSets)
		return nil, 0
	}
	pCtx.sSpsPpsCtx.iSpsErrorIgnored = 0
	if !(common.IS_SEI_NAL(eNalUnitType) || common.IS_PARAM_SETS_NALS(eNalUnitType) ||
		common.IS_AU_DELIMITER_NAL(eNalUnitType) || pCtx.sSpsPpsCtx.bPpsExistAheadFlag) {
		if pCtx.bPrintFrameErrorTraceFlag && pCtx.sSpsPpsCtx.iPpsErrorIgnored == 0 {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"parse_nal(), no exist Picture Parameter Sets ahead of sequence when try to decode NAL(type:%d).",
				eNalUnitType)
		} else {
			pCtx.sSpsPpsCtx.iPpsErrorIgnored++
		}
		pCtx.pDecoderStatistics.IPpsNoExistNalNum++
		pCtx.iErrorCode = int32(api.DsNoParamSets)
		return nil, 0
	}
	pCtx.sSpsPpsCtx.iPpsErrorIgnored = 0
	if (common.IS_VCL_NAL_AVC_BASE(eNalUnitType) && !(pCtx.sSpsPpsCtx.bSpsExistAheadFlag ||
		pCtx.sSpsPpsCtx.bPpsExistAheadFlag)) ||
		(common.IS_NEW_INTRODUCED_SVC_NAL(eNalUnitType) && !(pCtx.sSpsPpsCtx.bSpsExistAheadFlag ||
			pCtx.sSpsPpsCtx.bSubspsExistAheadFlag ||
			pCtx.sSpsPpsCtx.bPpsExistAheadFlag)) {
		if pCtx.bPrintFrameErrorTraceFlag && pCtx.sSpsPpsCtx.iSubSpsErrorIgnored == 0 {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"ParseNalHeader(), no exist Parameter Sets ahead of sequence when try to decode slice(type:%d).",
				eNalUnitType)
		} else {
			pCtx.sSpsPpsCtx.iSubSpsErrorIgnored++
		}
		pCtx.pDecoderStatistics.ISubSpsNoExistNalNum++
		pCtx.iErrorCode |= int32(api.DsNoParamSets)
		return nil, 0
	}
	pCtx.sSpsPpsCtx.iSubSpsErrorIgnored = 0

	switch eNalUnitType {
	case common.NAL_UNIT_AU_DELIMITER, common.NAL_UNIT_SEI:
		if pCtx.pAccessUnitList.uiAvailUnitsNum > 0 {
			pCtx.pAccessUnitList.uiEndPos = pCtx.pAccessUnitList.uiAvailUnitsNum - 1
			pCtx.bAuReadyFlag = true
		}

	case common.NAL_UNIT_PREFIX:
		pCurNal = &pCtx.sSpsPpsCtx.sPrefixNal
		pCurNal.uiTimeStamp = pCtx.uiTimeStamp

		if iNalSize < NAL_UNIT_HEADER_EXT_SIZE {
			pCurAu := pCtx.pAccessUnitList
			uiAvailNalNum := pCurAu.uiAvailUnitsNum

			if uiAvailNalNum > 0 {
				pCurAu.uiEndPos = uiAvailNalNum - 1
				if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
					pCtx.bAuReadyFlag = true
				}
			}
			pCurNal.sNalData.sPrefixNal.bPrefixNalCorrectFlag = false
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return nil, 0
		}

		DecodeNalHeaderExt(pCurNal, pSrcRbsp[pNal:])
		if (pCurNal.sNalHeaderExt.UiQualityId != 0) || pCurNal.sNalHeaderExt.BUseRefBasePicFlag {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"ParseNalHeader() in Prefix Nal Unit:uiQualityId (%d) != 0, bUseRefBasePicFlag (%d) != 0, not supported!",
				pCurNal.sNalHeaderExt.UiQualityId, auParserB2I(pCurNal.sNalHeaderExt.BUseRefBasePicFlag))
			pCurAu := pCtx.pAccessUnitList
			uiAvailNalNum := pCurAu.uiAvailUnitsNum

			if uiAvailNalNum > 0 {
				pCurAu.uiEndPos = uiAvailNalNum - 1
				if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
					pCtx.bAuReadyFlag = true
				}
			}
			pCurNal.sNalData.sPrefixNal.bPrefixNalCorrectFlag = false
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return nil, 0
		}

		pNal += NAL_UNIT_HEADER_EXT_SIZE
		iNalSize -= NAL_UNIT_HEADER_EXT_SIZE
		*pConsumedBytes += NAL_UNIT_HEADER_EXT_SIZE

		pCurNal.sNalHeaderExt.SNalUnitHeader.UiForbiddenZeroBit = pNalUnitHeader.UiForbiddenZeroBit
		pCurNal.sNalHeaderExt.SNalUnitHeader.UiNalRefIdc = pNalUnitHeader.UiNalRefIdc
		pCurNal.sNalHeaderExt.SNalUnitHeader.ENalUnitType = pNalUnitHeader.ENalUnitType
		if pNalUnitHeader.UiNalRefIdc != 0 {
			pBs = &pCtx.sBs
			iBitSize = (iNalSize << 3) - BsGetTrailingBits(pSrcRbsp[pNal+int(iNalSize)-1:]) // convert into bit

			iErr = DecInitBits(pBs, pSrcRbsp, pNal, iBitSize)
			if iErr != 0 {
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "NAL_UNIT_PREFIX: DecInitBits() fail due invalid access.")
				pCtx.iErrorCode |= int32(api.DsBitstreamError)
				return nil, 0
			}
			ParsePrefixNalUnit(pCtx, pBs)
		}
		pCurNal.sNalData.sPrefixNal.bPrefixNalCorrectFlag = true

	case common.NAL_UNIT_CODED_SLICE_EXT, common.NAL_UNIT_CODED_SLICE, common.NAL_UNIT_CODED_SLICE_IDR:
		if eNalUnitType == common.NAL_UNIT_CODED_SLICE_EXT {
			bExtensionFlag = true
		}
		var pCurAu *SAccessUnit
		var uiAvailNalNum uint32
		pCurNal = MemGetNextNal(&pCtx.pAccessUnitList)
		if pCurNal == nil {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "MemGetNextNal() fail due out of memory.")
			pCtx.iErrorCode |= int32(api.DsOutOfMemory)
			return nil, 0
		}
		pCurNal.uiTimeStamp = pCtx.uiTimeStamp
		pCurNal.sNalHeaderExt.SNalUnitHeader.UiForbiddenZeroBit = pNalUnitHeader.UiForbiddenZeroBit
		pCurNal.sNalHeaderExt.SNalUnitHeader.UiNalRefIdc = pNalUnitHeader.UiNalRefIdc
		pCurNal.sNalHeaderExt.SNalUnitHeader.ENalUnitType = pNalUnitHeader.ENalUnitType
		pCurAu = pCtx.pAccessUnitList
		uiAvailNalNum = pCurAu.uiAvailUnitsNum

		if pNalUnitHeader.ENalUnitType == common.NAL_UNIT_CODED_SLICE_EXT {
			if iNalSize < NAL_UNIT_HEADER_EXT_SIZE {
				ForceClearCurrentNal(pCurAu)

				if uiAvailNalNum > 1 {
					pCurAu.uiEndPos = uiAvailNalNum - 2
					if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
						pCtx.bAuReadyFlag = true
					}
				}
				pCtx.iErrorCode |= int32(api.DsBitstreamError)
				return nil, 0
			}

			DecodeNalHeaderExt(pCurNal, pSrcRbsp[pNal:])
			if pCurNal.sNalHeaderExt.UiQualityId != 0 ||
				pCurNal.sNalHeaderExt.BUseRefBasePicFlag {
				if pCurNal.sNalHeaderExt.UiQualityId != 0 {
					common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "ParseNalHeader():uiQualityId (%d) != 0, MGS not supported!",
						pCurNal.sNalHeaderExt.UiQualityId)
				}
				if pCurNal.sNalHeaderExt.BUseRefBasePicFlag {
					common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "ParseNalHeader():bUseRefBasePicFlag (%d) != 0, MGS not supported!",
						auParserB2I(pCurNal.sNalHeaderExt.BUseRefBasePicFlag))
				}

				ForceClearCurrentNal(pCurAu)

				if uiAvailNalNum > 1 {
					pCurAu.uiEndPos = uiAvailNalNum - 2
					if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
						pCtx.bAuReadyFlag = true
					}
				}
				pCtx.iErrorCode |= int32(api.DsBitstreamError)
				return nil, 0
			}
			pNal += NAL_UNIT_HEADER_EXT_SIZE
			iNalSize -= NAL_UNIT_HEADER_EXT_SIZE
			*pConsumedBytes += NAL_UNIT_HEADER_EXT_SIZE

			if pCtx.pParam.BParseOnly {
				var iTrailingZeroByte int32
				for pSrcNal[iSrcNalLen-iTrailingZeroByte-1] == 0x0 { //remove final trailing 0 bytes
					iTrailingZeroByte++
				}
				iActualLen := iSrcNalLen - iTrailingZeroByte
				//unify start code as 0x0001
				var iCurrStartByte int32 = 4                                     //4 for 0x0001, 3 for 0x001
				if pSrcNal[0] == 0x0 && pSrcNal[1] == 0x0 && pSrcNal[2] == 0x1 { //if 0x001
					iCurrStartByte = 3
				}
				iOffset := iCurrStartByte + 1 + NAL_UNIT_HEADER_EXT_SIZE
				iWriteLen := 5 + (iActualLen - iOffset) // 4-byte start code + NAL type byte + payload
				// Bounds check: ensure write fits in sSavedData buffer
				if pSavedData.pCurPos+int(iWriteLen) > pSavedData.pEnd {
					pSavedData.pCurPos = 0
				}
				pCurNal.sNalData.sVclNal.pNalPos = pSavedData.pHead
				pCurNal.sNalData.sVclNal.iNalPosOff = pSavedData.pCurPos
				pCurNal.sNalData.sVclNal.iNalLength = iActualLen - NAL_UNIT_HEADER_EXT_SIZE
				if iCurrStartByte == 3 {
					pCurNal.sNalData.sVclNal.iNalLength++
				}
				if pCurNal.sNalHeaderExt.BIdrFlag {
					pSrcNal[iCurrStartByte] &= 0xE0
					pSrcNal[iCurrStartByte] |= 0x05
				} else {
					pSrcNal[iCurrStartByte] &= 0xE0
					pSrcNal[iCurrStartByte] |= 0x01
				}
				pCur := pSavedData.pHead[pSavedData.pCurPos:]
				pCur[0], pCur[1], pCur[2] = 0x0, 0x0, 0x0
				pCur[3] = 0x1
				pCur[4] = pSrcNal[iCurrStartByte]
				pSavedData.pCurPos += 5
				copy(pSavedData.pHead[pSavedData.pCurPos:], pSrcNal[iOffset:iActualLen])
				pSavedData.pCurPos += int(iActualLen - iOffset)
			}
		} else {
			if pCtx.pParam.BParseOnly {
				var iTrailingZeroByte int32
				for pSrcNal[iSrcNalLen-iTrailingZeroByte-1] == 0x0 { //remove final trailing 0 bytes
					iTrailingZeroByte++
				}
				iActualLen := iSrcNalLen - iTrailingZeroByte
				//unify start code as 0x0001
				var iStartDeltaByte int32                                        //0 for 0x0001, 1 for 0x001
				if pSrcNal[0] == 0x0 && pSrcNal[1] == 0x0 && pSrcNal[2] == 0x1 { //if 0x001
					iStartDeltaByte = 1
				}
				iWriteLen := iStartDeltaByte + iActualLen
				// Bounds check: ensure write fits in sSavedData buffer
				if pSavedData.pCurPos+int(iWriteLen) > pSavedData.pEnd {
					pSavedData.pCurPos = 0
				}
				pCurNal.sNalData.sVclNal.pNalPos = pSavedData.pHead
				pCurNal.sNalData.sVclNal.iNalPosOff = pSavedData.pCurPos
				pCurNal.sNalData.sVclNal.iNalLength = iActualLen
				if iStartDeltaByte != 0 {
					pSavedData.pHead[pSavedData.pCurPos] = 0x0
					pCurNal.sNalData.sVclNal.iNalLength++
				}
				copy(pSavedData.pHead[pSavedData.pCurPos+int(iStartDeltaByte):], pSrcNal[:iActualLen])
				pSavedData.pCurPos += int(iStartDeltaByte + iActualLen)
			}
			if common.NAL_UNIT_PREFIX == pCtx.sSpsPpsCtx.sPrefixNal.sNalHeaderExt.SNalUnitHeader.ENalUnitType {
				if pCtx.sSpsPpsCtx.sPrefixNal.sNalData.sPrefixNal.bPrefixNalCorrectFlag {
					PrefetchNalHeaderExtSyntax(pCtx, pCurNal, &pCtx.sSpsPpsCtx.sPrefixNal)
				}
			}

			pCurNal.sNalHeaderExt.BIdrFlag = common.NAL_UNIT_CODED_SLICE_IDR == pNalUnitHeader.ENalUnitType //SHOULD update this flag for AVC if no prefix NAL
			pCurNal.sNalHeaderExt.INoInterLayerPredFlag = 1
		}

		pBs = &pCurAu.pNalUnitsList[uiAvailNalNum-1].sNalData.sVclNal.sSliceBitsRead
		iBitSize = (iNalSize << 3) - BsGetTrailingBits(pSrcRbsp[pNal+int(iNalSize)-1:]) // convert into bit
		iErr = DecInitBits(pBs, pSrcRbsp, pNal, iBitSize)
		if iErr != 0 {
			ForceClearCurrentNal(pCurAu)
			if uiAvailNalNum > 1 {
				pCurAu.uiEndPos = uiAvailNalNum - 2
				if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
					pCtx.bAuReadyFlag = true
				}
			}
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "NAL_UNIT_CODED_SLICE: DecInitBits() fail due invalid access.")
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return nil, 0
		}
		iErr = ParseSliceHeaderSyntaxs(pCtx, pBs, bExtensionFlag)
		if iErr != ERR_NONE {
			if (uiAvailNalNum == 1) && pCurNal.sNalHeaderExt.BIdrFlag { //IDR parse error
				ResetActiveSPSForEachLayer(pCtx)
			}
			//if current NAL occur error when parsing, should clean it from pNalUnitsList
			//otherwise, when Next good NAL decoding, this corrupt NAL is considered as normal NAL and lead to decoder crash
			ForceClearCurrentNal(pCurAu)

			if uiAvailNalNum > 1 {
				pCurAu.uiEndPos = uiAvailNalNum - 2
				if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
					pCtx.bAuReadyFlag = true
				}
			}
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return nil, 0
		}

		if (uiAvailNalNum == 1) &&
			CheckNextAuNewSeq(pCtx, pCurNal, pCurNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps) {
			ResetActiveSPSForEachLayer(pCtx)
		}
		if (uiAvailNalNum > 1) &&
			CheckAccessUnitBoundary(pCtx, pCurAu.pNalUnitsList[uiAvailNalNum-1], pCurAu.pNalUnitsList[uiAvailNalNum-2],
				pCurAu.pNalUnitsList[uiAvailNalNum-1].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps) {
			pCurAu.uiEndPos = uiAvailNalNum - 2
			pCtx.bAuReadyFlag = true
			pCtx.bNextNewSeqBegin = CheckNextAuNewSeq(pCtx, pCurNal, pCurNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps)
		}

	default:
	}

	return pSrcRbsp, pNal
}

// CheckAccessUnitBoundaryExt ports bool CheckAccessUnitBoundaryExt (PNalUnitHeaderExt pLastNalHdrExt,
// PNalUnitHeaderExt pCurNalHeaderExt, PSliceHeader pLastSliceHeader, PSliceHeader pCurSliceHeader).
func CheckAccessUnitBoundaryExt(pLastNalHdrExt *common.SNalUnitHeaderExt, pCurNalHeaderExt *common.SNalUnitHeaderExt, pLastSliceHeader *SSliceHeader, pCurSliceHeader *SSliceHeader) bool {
	kpSps := pCurSliceHeader.pSps

	//Sub-clause 7.1.4.1.1 temporal_id
	if pLastNalHdrExt.UiTemporalId != pCurNalHeaderExt.UiTemporalId {
		return true
	}

	// Subclause 7.4.1.2.5
	if pLastSliceHeader.iRedundantPicCnt > pCurSliceHeader.iRedundantPicCnt {
		return true
	}

	// Subclause G7.4.1.2.4
	if pLastNalHdrExt.UiDependencyId > pCurNalHeaderExt.UiDependencyId {
		return true
	}
	if pLastNalHdrExt.UiQualityId > pCurNalHeaderExt.UiQualityId {
		return true
	}

	// Subclause 7.4.1.2.4
	if pLastSliceHeader.iFrameNum != pCurSliceHeader.iFrameNum {
		return true
	}
	if pLastSliceHeader.iPpsId != pCurSliceHeader.iPpsId {
		return true
	}
	if pLastSliceHeader.pSps.iSpsId != pCurSliceHeader.pSps.iSpsId {
		return true
	}
	if pLastSliceHeader.bFieldPicFlag != pCurSliceHeader.bFieldPicFlag {
		return true
	}
	if pLastSliceHeader.bBottomFiledFlag != pCurSliceHeader.bBottomFiledFlag {
		return true
	}
	if (pLastNalHdrExt.SNalUnitHeader.UiNalRefIdc != common.NRI_PRI_LOWEST) != (pCurNalHeaderExt.SNalUnitHeader.UiNalRefIdc !=
		common.NRI_PRI_LOWEST) {
		return true
	}
	if pLastNalHdrExt.BIdrFlag != pCurNalHeaderExt.BIdrFlag {
		return true
	}
	if pCurNalHeaderExt.BIdrFlag {
		if pLastSliceHeader.uiIdrPicId != pCurSliceHeader.uiIdrPicId {
			return true
		}
	}
	if kpSps.uiPocType == 0 {
		if pLastSliceHeader.iPicOrderCntLsb != pCurSliceHeader.iPicOrderCntLsb {
			return true
		}
		if pLastSliceHeader.iDeltaPicOrderCntBottom != pCurSliceHeader.iDeltaPicOrderCntBottom {
			return true
		}
	} else if kpSps.uiPocType == 1 {
		if pLastSliceHeader.iDeltaPicOrderCnt[0] != pCurSliceHeader.iDeltaPicOrderCnt[0] {
			return true
		}
		if pLastSliceHeader.iDeltaPicOrderCnt[1] != pCurSliceHeader.iDeltaPicOrderCnt[1] {
			return true
		}
	}
	// C: memcmp of the whole SPps / SSps structures.
	if *pLastSliceHeader.pPps != *pCurSliceHeader.pPps ||
		*pLastSliceHeader.pSps != *pCurSliceHeader.pSps {
		return true
	}
	return false
}

// CheckAccessUnitBoundary ports bool CheckAccessUnitBoundary (PWelsDecoderContext pCtx, const PNalUnit
// kpCurNal, const PNalUnit kpLastNal, const PSps kpSps).
func CheckAccessUnitBoundary(pCtx *SWelsDecoderContext, kpCurNal *SNalUnit, kpLastNal *SNalUnit, kpSps *SSps) bool {
	kpLastNalHeaderExt := &kpLastNal.sNalHeaderExt
	kpCurNalHeaderExt := &kpCurNal.sNalHeaderExt
	kpLastSliceHeader := &kpLastNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader
	kpCurSliceHeader := &kpCurNal.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader
	if pCtx.sSpsPpsCtx.pActiveLayerSps[kpCurNalHeaderExt.UiDependencyId] != nil &&
		pCtx.sSpsPpsCtx.pActiveLayerSps[kpCurNalHeaderExt.UiDependencyId] != kpSps {
		return true // the active sps changed, new sequence begins, so the current au is ready
	}

	//Sub-clause 7.1.4.1.1 temporal_id
	if kpLastNalHeaderExt.UiTemporalId != kpCurNalHeaderExt.UiTemporalId {
		return true
	}
	if kpLastSliceHeader.iFrameNum != kpCurSliceHeader.iFrameNum {
		return true
	}
	// Subclause 7.4.1.2.5
	if kpLastSliceHeader.iRedundantPicCnt > kpCurSliceHeader.iRedundantPicCnt {
		return true
	}

	// Subclause G7.4.1.2.4
	if kpLastNalHeaderExt.UiDependencyId > kpCurNalHeaderExt.UiDependencyId {
		return true
	}
	// Subclause 7.4.1.2.4
	if kpLastNalHeaderExt.UiDependencyId == kpCurNalHeaderExt.UiDependencyId &&
		kpLastSliceHeader.iPpsId != kpCurSliceHeader.iPpsId {
		return true
	}
	if kpLastSliceHeader.bFieldPicFlag != kpCurSliceHeader.bFieldPicFlag {
		return true
	}
	if kpLastSliceHeader.bBottomFiledFlag != kpCurSliceHeader.bBottomFiledFlag {
		return true
	}
	if (kpLastNalHeaderExt.SNalUnitHeader.UiNalRefIdc != common.NRI_PRI_LOWEST) != (kpCurNalHeaderExt.SNalUnitHeader.UiNalRefIdc !=
		common.NRI_PRI_LOWEST) {
		return true
	}
	if kpLastNalHeaderExt.BIdrFlag != kpCurNalHeaderExt.BIdrFlag {
		return true
	}
	if kpCurNalHeaderExt.BIdrFlag {
		if kpLastSliceHeader.uiIdrPicId != kpCurSliceHeader.uiIdrPicId {
			return true
		}
	}
	if kpSps.uiPocType == 0 {
		if kpLastSliceHeader.iPicOrderCntLsb != kpCurSliceHeader.iPicOrderCntLsb {
			return true
		}
		if kpLastSliceHeader.iDeltaPicOrderCntBottom != kpCurSliceHeader.iDeltaPicOrderCntBottom {
			return true
		}
	} else if kpSps.uiPocType == 1 {
		if kpLastSliceHeader.iDeltaPicOrderCnt[0] != kpCurSliceHeader.iDeltaPicOrderCnt[0] {
			return true
		}
		if kpLastSliceHeader.iDeltaPicOrderCnt[1] != kpCurSliceHeader.iDeltaPicOrderCnt[1] {
			return true
		}
	}

	return false
}

// CheckNextAuNewSeq ports bool CheckNextAuNewSeq (PWelsDecoderContext pCtx, const PNalUnit kpCurNal,
// const PSps kpSps).
func CheckNextAuNewSeq(pCtx *SWelsDecoderContext, kpCurNal *SNalUnit, kpSps *SSps) bool {
	kpCurNalHeaderExt := &kpCurNal.sNalHeaderExt
	if pCtx.sSpsPpsCtx.pActiveLayerSps[kpCurNalHeaderExt.UiDependencyId] != nil &&
		pCtx.sSpsPpsCtx.pActiveLayerSps[kpCurNalHeaderExt.UiDependencyId] != kpSps {
		return true
	}
	if kpCurNalHeaderExt.BIdrFlag {
		return true
	}

	return false
}

// ParseNonVclNal ports int32_t ParseNonVclNal (PWelsDecoderContext pCtx, uint8_t* pRbsp, const int32_t
// kiSrcLen, uint8_t* pSrcNal, const int32_t kSrcNalLen): to parse NON VCL NAL Units.
//
// pRbsp/iRbspOff: (slice, offset) pair (as returned by ParseNalHeader). pSrcNal: sub-slice starting at
// the C pointer.
//
// return 0 - successed, 1 - failed
func ParseNonVclNal(pCtx *SWelsDecoderContext, pRbsp []uint8, iRbspOff int, kiSrcLen int32, pSrcNal []uint8, kSrcNalLen int32) int32 {
	var pBs *common.SBitStringAux
	var eNalType common.EWelsNalUnitType = common.NAL_UNIT_UNSPEC_0 // make initial value as unspecified
	var iPicWidth int32
	var iPicHeight int32
	var iBitSize int32
	var iErr int32 = ERR_NONE
	if kiSrcLen <= 0 {
		return iErr
	}

	pBs = &pCtx.sBs                                                                  // SBitStringAux instance for non VCL NALs decoding
	iBitSize = (kiSrcLen << 3) - BsGetTrailingBits(pRbsp[iRbspOff+int(kiSrcLen)-1:]) // convert into bit
	eNalType = pCtx.sCurNalHead.ENalUnitType

	switch eNalType {
	case common.NAL_UNIT_SPS, common.NAL_UNIT_SUBSET_SPS:
		if iBitSize > 0 {
			iErr = DecInitBits(pBs, pRbsp, iRbspOff, iBitSize)
			if ERR_NONE != iErr {
				if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
					pCtx.iErrorCode |= int32(api.DsNoParamSets)
				} else {
					pCtx.iErrorCode |= int32(api.DsBitstreamError)
				}
				return iErr
			}
		}
		iErr = ParseSps(pCtx, pBs, &iPicWidth, &iPicHeight, pSrcNal, kSrcNalLen)
		if ERR_NONE != iErr { // modified for pSps/pSubsetSps invalid, 12/1/2009
			if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
				pCtx.iErrorCode |= int32(api.DsNoParamSets)
			} else {
				pCtx.iErrorCode |= int32(api.DsBitstreamError)
			}
			return iErr
		}
		pCtx.bHasNewSps = true

	case common.NAL_UNIT_PPS:
		if iBitSize > 0 {
			iErr = DecInitBits(pBs, pRbsp, iRbspOff, iBitSize)
			if ERR_NONE != iErr {
				if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
					pCtx.iErrorCode |= int32(api.DsNoParamSets)
				} else {
					pCtx.iErrorCode |= int32(api.DsBitstreamError)
				}
				return iErr
			}
		}
		iErr = ParsePps(pCtx, pCtx.sSpsPpsCtx.sPpsBuffer[:], pBs, pSrcNal, kSrcNalLen)
		if ERR_NONE != iErr { // modified for pps invalid, 12/1/2009
			if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
				pCtx.iErrorCode |= int32(api.DsNoParamSets)
			} else {
				pCtx.iErrorCode |= int32(api.DsBitstreamError)
			}
			pCtx.bHasNewSps = false
			return iErr
		}

		pCtx.sSpsPpsCtx.bPpsExistAheadFlag = true
		pCtx.sSpsPpsCtx.iSeqId++

	case common.NAL_UNIT_SEI:

	case common.NAL_UNIT_PREFIX:
	case common.NAL_UNIT_CODED_SLICE_DPA, common.NAL_UNIT_CODED_SLICE_DPB, common.NAL_UNIT_CODED_SLICE_DPC:

	default:
	}

	return iErr
}

// ParseRefBasePicMarking ports int32_t ParseRefBasePicMarking (PBitStringAux pBs, PRefBasePicMarking
// pRefBasePicMarking).
func ParseRefBasePicMarking(pBs *common.SBitStringAux, pRefBasePicMarking *SRefBasePicMarking) int32 {
	var uiCode uint32
	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //adaptive_ref_base_pic_marking_mode_flag
		return int32(uiRet)
	}
	kbAdaptiveMarkingModeFlag := uiCode != 0
	pRefBasePicMarking.bAdaptiveRefBasePicMarkingModeFlag = kbAdaptiveMarkingModeFlag
	if kbAdaptiveMarkingModeFlag {
		iIdx := 0
		for {
			if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //MMCO_base
				return int32(uiRet)
			}
			kuiMmco := uiCode

			pRefBasePicMarking.mmco_base[iIdx].uiMmcoType = kuiMmco

			if kuiMmco == common.MMCO_END {
				break
			}

			if kuiMmco == common.MMCO_SHORT2UNUSED {
				if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //difference_of_base_pic_nums_minus1
					return int32(uiRet)
				}
				pRefBasePicMarking.mmco_base[iIdx].uiDiffOfPicNums = 1 + uiCode
				pRefBasePicMarking.mmco_base[iIdx].iShortFrameNum = 0
			} else if kuiMmco == common.MMCO_LONG2UNUSED {
				if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //long_term_base_pic_num
					return int32(uiRet)
				}
				pRefBasePicMarking.mmco_base[iIdx].uiLongTermPicNum = uiCode
			}
			iIdx++
			if !(iIdx < MAX_MMCO_COUNT) {
				break
			}
		}
	}
	return ERR_NONE
}

// ParsePrefixNalUnit ports int32_t ParsePrefixNalUnit (PWelsDecoderContext pCtx, PBitStringAux pBs).
func ParsePrefixNalUnit(pCtx *SWelsDecoderContext, pBs *common.SBitStringAux) int32 {
	pCurNal := &pCtx.sSpsPpsCtx.sPrefixNal
	var uiCode uint32

	if pCurNal.sNalHeaderExt.SNalUnitHeader.UiNalRefIdc != 0 {
		head_ext := &pCurNal.sNalHeaderExt
		sPrefixNal := &pCurNal.sNalData.sPrefixNal
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //store_ref_base_pic_flag
			return int32(uiRet)
		}
		sPrefixNal.bStoreRefBasePicFlag = uiCode != 0
		if (head_ext.BUseRefBasePicFlag || sPrefixNal.bStoreRefBasePicFlag) && !head_ext.BIdrFlag {
			if iRet := ParseRefBasePicMarking(pBs, &sPrefixNal.sRefPicBaseMarking); iRet != ERR_NONE {
				return iRet
			}
		}
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //additional_prefix_nal_unit_extension_flag
			return int32(uiRet)
		}
		sPrefixNal.bPrefixNalUnitAdditionalExtFlag = uiCode != 0
		if sPrefixNal.bPrefixNalUnitAdditionalExtFlag {
			if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //additional_prefix_nal_unit_extension_data_flag
				return int32(uiRet)
			}
			sPrefixNal.bPrefixNalUnitExtFlag = uiCode != 0
		}
	}
	return ERR_NONE
}

const (
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_LEFT_OFFSET_MIN   = -32768
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_LEFT_OFFSET_MAX   = 32767
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_TOP_OFFSET_MIN    = -32768
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_TOP_OFFSET_MAX    = 32767
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_RIGHT_OFFSET_MIN  = -32768
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_RIGHT_OFFSET_MAX  = 32767
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_BOTTOM_OFFSET_MIN = -32768
	SUBSET_SPS_SEQ_SCALED_REF_LAYER_BOTTOM_OFFSET_MAX = 32767
)

// DecodeSpsSvcExt ports int32_t DecodeSpsSvcExt (PWelsDecoderContext pCtx, PSubsetSps pSpsExt,
// PBitStringAux pBs).
func DecodeSpsSvcExt(pCtx *SWelsDecoderContext, pSpsExt *SSubsetSps, pBs *common.SBitStringAux) int32 {
	var pExt *SSpsSvcExt
	var uiCode uint32
	var iCode int32

	pExt = &pSpsExt.sSpsSvcExt

	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //inter_layer_deblocking_filter_control_present_flag
		return int32(uiRet)
	}
	pExt.bInterLayerDeblockingFilterCtrlPresentFlag = uiCode != 0
	if iRet := BsGetBits(pBs, 2, &uiCode); iRet != ERR_NONE { //extended_spatial_scalability_idc
		return iRet
	}
	pExt.uiExtendedSpatialScalability = uint8(uiCode)
	if pExt.uiExtendedSpatialScalability > 2 {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING,
			"DecodeSpsSvcExt():extended_spatial_scalability (%d) != 0, ESS not supported!",
			pExt.uiExtendedSpatialScalability)
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_ESS)
	}

	pExt.uiChromaPhaseXPlus1Flag =
		0 // FIXME: Incoherent with JVT X201 standard (= 1), but conformance to JSVM (= 0) implementation.
	pExt.uiChromaPhaseYPlus1 = 1

	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //chroma_phase_x_plus1_flag
		return int32(uiRet)
	}
	pExt.uiChromaPhaseXPlus1Flag = uint8(uiCode)
	if iRet := BsGetBits(pBs, 2, &uiCode); iRet != ERR_NONE { //chroma_phase_y_plus1
		return iRet
	}
	pExt.uiChromaPhaseYPlus1 = uint8(uiCode)

	pExt.uiSeqRefLayerChromaPhaseXPlus1Flag = pExt.uiChromaPhaseXPlus1Flag
	pExt.uiSeqRefLayerChromaPhaseYPlus1 = pExt.uiChromaPhaseYPlus1
	pExt.sSeqScaledRefLayer = SPosOffset{}

	if pExt.uiExtendedSpatialScalability == 1 {
		kpPos := &pExt.sSeqScaledRefLayer
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //seq_ref_layer_chroma_phase_x_plus1_flag
			return int32(uiRet)
		}
		pExt.uiSeqRefLayerChromaPhaseXPlus1Flag = uint8(uiCode)
		if iRet := BsGetBits(pBs, 2, &uiCode); iRet != ERR_NONE { //seq_ref_layer_chroma_phase_y_plus1
			return iRet
		}
		pExt.uiSeqRefLayerChromaPhaseYPlus1 = uint8(uiCode)

		if iRet := BsGetSe(pBs, &iCode); iRet != ERR_NONE { //seq_scaled_ref_layer_left_offset
			return iRet
		}
		kpPos.iLeftOffset = iCode
		WELS_CHECK_SE_BOTH_WARNING(pCtx, kpPos.iLeftOffset, SUBSET_SPS_SEQ_SCALED_REF_LAYER_LEFT_OFFSET_MIN,
			SUBSET_SPS_SEQ_SCALED_REF_LAYER_LEFT_OFFSET_MAX, "seq_scaled_ref_layer_left_offset")
		if iRet := BsGetSe(pBs, &iCode); iRet != ERR_NONE { //seq_scaled_ref_layer_top_offset
			return iRet
		}
		kpPos.iTopOffset = iCode
		WELS_CHECK_SE_BOTH_WARNING(pCtx, kpPos.iTopOffset, SUBSET_SPS_SEQ_SCALED_REF_LAYER_TOP_OFFSET_MIN,
			SUBSET_SPS_SEQ_SCALED_REF_LAYER_TOP_OFFSET_MAX, "seq_scaled_ref_layer_top_offset")
		if iRet := BsGetSe(pBs, &iCode); iRet != ERR_NONE { //seq_scaled_ref_layer_right_offset
			return iRet
		}
		kpPos.iRightOffset = iCode
		WELS_CHECK_SE_BOTH_WARNING(pCtx, kpPos.iRightOffset, SUBSET_SPS_SEQ_SCALED_REF_LAYER_RIGHT_OFFSET_MIN,
			SUBSET_SPS_SEQ_SCALED_REF_LAYER_RIGHT_OFFSET_MAX, "seq_scaled_ref_layer_right_offset")
		if iRet := BsGetSe(pBs, &iCode); iRet != ERR_NONE { //seq_scaled_ref_layer_bottom_offset
			return iRet
		}
		kpPos.iBottomOffset = iCode
		WELS_CHECK_SE_BOTH_WARNING(pCtx, kpPos.iBottomOffset, SUBSET_SPS_SEQ_SCALED_REF_LAYER_BOTTOM_OFFSET_MIN,
			SUBSET_SPS_SEQ_SCALED_REF_LAYER_BOTTOM_OFFSET_MAX, "seq_scaled_ref_layer_bottom_offset")
	}

	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //seq_tcoeff_level_prediction_flag
		return int32(uiRet)
	}
	pExt.bSeqTCoeffLevelPredFlag = uiCode != 0
	pExt.bAdaptiveTCoeffLevelPredFlag = false
	if pExt.bSeqTCoeffLevelPredFlag {
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //adaptive_tcoeff_level_prediction_flag
			return int32(uiRet)
		}
		pExt.bAdaptiveTCoeffLevelPredFlag = uiCode != 0
	}
	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //slice_header_restriction_flag
		return int32(uiRet)
	}
	pExt.bSliceHeaderRestrictionFlag = uiCode != 0

	return ERR_NONE
}

// GetLevelLimits ports const SLevelLimits* GetLevelLimits (int32_t iLevelIdx, bool bConstraint3).
//
// Returns a pointer into common.G_ksLevelLimits or nil.
func GetLevelLimits(iLevelIdx int32, bConstraint3 bool) *common.SLevelLimits {
	switch iLevelIdx {
	case 9:
		return &common.G_ksLevelLimits[1]
	case 10:
		return &common.G_ksLevelLimits[0]
	case 11:
		if bConstraint3 {
			return &common.G_ksLevelLimits[1]
		}
		return &common.G_ksLevelLimits[2]
	case 12:
		return &common.G_ksLevelLimits[3]
	case 13:
		return &common.G_ksLevelLimits[4]
	case 20:
		return &common.G_ksLevelLimits[5]
	case 21:
		return &common.G_ksLevelLimits[6]
	case 22:
		return &common.G_ksLevelLimits[7]
	case 30:
		return &common.G_ksLevelLimits[8]
	case 31:
		return &common.G_ksLevelLimits[9]
	case 32:
		return &common.G_ksLevelLimits[10]
	case 40:
		return &common.G_ksLevelLimits[11]
	case 41:
		return &common.G_ksLevelLimits[12]
	case 42:
		return &common.G_ksLevelLimits[13]
	case 50:
		return &common.G_ksLevelLimits[14]
	case 51:
		return &common.G_ksLevelLimits[15]
	case 52:
		return &common.G_ksLevelLimits[16]
	default:
		return nil
	}
}

// CheckSpsActive ports bool CheckSpsActive (PWelsDecoderContext pCtx, PSps pSps, bool bUseSubsetFlag).
func CheckSpsActive(pCtx *SWelsDecoderContext, pSps *SSps, bUseSubsetFlag bool) bool {
	for i := 0; i < MAX_LAYER_NUM; i++ {
		if pCtx.sSpsPpsCtx.pActiveLayerSps[i] == pSps {
			return true
		}
	}
	// Pre-active, will be used soon
	if bUseSubsetFlag {
		if pSps.iMbWidth > 0 && pSps.iMbHeight > 0 && pCtx.sSpsPpsCtx.bSubspsAvailFlags[pSps.iSpsId] {
			if pCtx.iTotalNumMbRec > 0 {
				return true
			}
			if pCtx.pAccessUnitList.uiAvailUnitsNum > 0 {
				i, iNum := 0, int32(pCtx.pAccessUnitList.uiAvailUnitsNum)
				for int32(i) < iNum {
					pNalUnit := pCtx.pAccessUnitList.pNalUnitsList[i]
					if pNalUnit.sNalData.sVclNal.bSliceHeaderExtFlag { //ext data
						pNextUsedSps := pNalUnit.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps
						if pNextUsedSps.iSpsId == pSps.iSpsId {
							return true
						}
					}
					i++
				}
			}
		}
	} else {
		if pSps.iMbWidth > 0 && pSps.iMbHeight > 0 && pCtx.sSpsPpsCtx.bSpsAvailFlags[pSps.iSpsId] {
			if pCtx.iTotalNumMbRec > 0 {
				return true
			}
			if pCtx.pAccessUnitList.uiAvailUnitsNum > 0 {
				i, iNum := 0, int32(pCtx.pAccessUnitList.uiAvailUnitsNum)
				for int32(i) < iNum {
					pNalUnit := pCtx.pAccessUnitList.pNalUnitsList[i]
					if !pNalUnit.sNalData.sVclNal.bSliceHeaderExtFlag { //non-ext data
						pNextUsedSps := pNalUnit.sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps
						if pNextUsedSps.iSpsId == pSps.iSpsId {
							return true
						}
					}
					i++
				}
			}
		}
	}
	return false
}

const (
	SPS_LOG2_MAX_FRAME_NUM_MINUS4_MAX             = 12
	SPS_LOG2_MAX_PIC_ORDER_CNT_LSB_MINUS4_MAX     = 12
	SPS_NUM_REF_FRAMES_IN_PIC_ORDER_CNT_CYCLE_MAX = 255
	SPS_MAX_NUM_REF_FRAMES_MAX                    = 16
	PPS_PIC_INIT_QP_QS_MIN                        = 0
	PPS_PIC_INIT_QP_QS_MAX                        = 51
	PPS_CHROMA_QP_INDEX_OFFSET_MIN                = -12
	PPS_CHROMA_QP_INDEX_OFFSET_MAX                = 12
	SCALING_LIST_DELTA_SCALE_MAX                  = 127
	SCALING_LIST_DELTA_SCALE_MIN                  = -128
)

// ParseSps ports int32_t ParseSps (PWelsDecoderContext pCtx, PBitStringAux pBsAux, int32_t*
// pPicWidth, int32_t* pPicHeight, uint8_t* pSrcNal, const int32_t kSrcNalLen): to parse Sequence
// Parameter Set (SPS).
//
// pSrcNal: sub-slice starting at the C pointer.
//
// return 0 - successed, 1 - failed. Call it in case eNalUnitType is SPS.
func ParseSps(pCtx *SWelsDecoderContext, pBsAux *common.SBitStringAux, pPicWidth *int32, pPicHeight *int32, pSrcNal []uint8, kSrcNalLen int32) int32 {
	pBs := pBsAux
	var sTempSubsetSps SSubsetSps
	var pSps *SSps
	var pSubsetSps *SSubsetSps
	pNalHead := &pCtx.sCurNalHead
	var uiProfileIdc ProfileIdc
	var uiLevelIdc uint8
	var iSpsId int32
	var uiCode uint32
	var iCode int32
	var iRet int32 = ERR_NONE
	var bConstraintSetFlags [6]bool
	kbUseSubsetFlag := common.IS_SUBSET_SPS_NAL(pNalHead.ENalUnitType)

	if iRet := BsGetBits(pBs, 8, &uiCode); iRet != ERR_NONE { //profile_idc
		return iRet
	}
	uiProfileIdc = ProfileIdc(uiCode)
	eProfile := api.EProfileIdc(uiProfileIdc)
	if eProfile != api.PRO_BASELINE && eProfile != api.PRO_MAIN && eProfile != api.PRO_SCALABLE_BASELINE &&
		eProfile != api.PRO_SCALABLE_HIGH &&
		eProfile != api.PRO_EXTENDED && eProfile != api.PRO_HIGH {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "SPS ID can not be supported!\n")
		return 0 // C: return false;
	}
	for i := 0; i < 6; i++ { //constraint_set0_flag .. constraint_set5_flag
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE {
			return int32(uiRet)
		}
		bConstraintSetFlags[i] = uiCode != 0
	}
	if iRet := BsGetBits(pBs, 2, &uiCode); iRet != ERR_NONE { // reserved_zero_2bits, equal to 0
		return iRet
	}
	if iRet := BsGetBits(pBs, 8, &uiCode); iRet != ERR_NONE { // level_idc
		return iRet
	}
	uiLevelIdc = uint8(uiCode)
	if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //seq_parameter_set_id
		return int32(uiRet)
	}
	if uiCode >= common.MAX_SPS_COUNT { // Modified to check invalid negative iSpsId, 12/1/2009
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, " iSpsId is out of range! \n")
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_SPS_ID_OVERFLOW)
	}
	iSpsId = int32(uiCode)
	pSubsetSps = &sTempSubsetSps
	pSps = &sTempSubsetSps.sSps
	// Use the level 5.2 for compatibility
	pSMaxLevelLimits := GetLevelLimits(52, false)
	pSLevelLimits := GetLevelLimits(int32(uiLevelIdc), bConstraintSetFlags[3])
	if pSLevelLimits == nil {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "ParseSps(): level_idx (%d).\n", uiLevelIdc)
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_UNSUPPORTED_NON_BASELINE)
	}
	pSps.pSLevelLimits = pSLevelLimits
	// syntax elements in default
	pSps.uiChromaFormatIdc = 1
	pSps.uiChromaArrayType = 1

	pSps.uiProfileIdc = uiProfileIdc
	pSps.uiLevelIdc = uiLevelIdc
	pSps.iSpsId = iSpsId

	if api.PRO_SCALABLE_BASELINE == eProfile || api.PRO_SCALABLE_HIGH == eProfile ||
		api.PRO_HIGH == eProfile || api.PRO_HIGH10 == eProfile ||
		api.PRO_HIGH422 == eProfile || api.PRO_HIGH444 == eProfile ||
		api.PRO_CAVLC444 == eProfile || 44 == uiProfileIdc {

		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //chroma_format_idc
			return int32(uiRet)
		}
		pSps.uiChromaFormatIdc = uint8(uiCode)
		if pSps.uiChromaFormatIdc > 1 {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "ParseSps(): chroma_format_idc (%d) <=1 supported.",
				pSps.uiChromaFormatIdc)
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_UNSUPPORTED_NON_BASELINE)

		} // To support 4:0:0; 4:2:0
		pSps.uiChromaArrayType = pSps.uiChromaFormatIdc
		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //bit_depth_luma_minus8
			return int32(uiRet)
		}
		if uiCode != 0 {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "ParseSps(): bit_depth_luma (%d) Only 8 bit supported.", 8+uiCode)
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_UNSUPPORTED_NON_BASELINE)
		}
		pSps.uiBitDepthLuma = 8

		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //bit_depth_chroma_minus8
			return int32(uiRet)
		}
		if uiCode != 0 {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "ParseSps(): bit_depth_chroma (%d). Only 8 bit supported.", 8+uiCode)
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_UNSUPPORTED_NON_BASELINE)
		}
		pSps.uiBitDepthChroma = 8

		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //qpprime_y_zero_transform_bypass_flag
			return int32(uiRet)
		}
		pSps.bQpPrimeYZeroTransfBypassFlag = uiCode != 0
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //seq_scaling_matrix_present_flag
			return int32(uiRet)
		}
		pSps.bSeqScalingMatrixPresentFlag = uiCode != 0

		if pSps.bSeqScalingMatrixPresentFlag {
			if iRet := ParseScalingList(pSps, pBs, false, false, pSps.bSeqScalingListPresentFlag[:], &pSps.iScalingList4x4,
				&pSps.iScalingList8x8); iRet != ERR_NONE {
				return iRet
			}
		}
	}
	if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //log2_max_frame_num_minus4
		return int32(uiRet)
	}
	if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, SPS_LOG2_MAX_FRAME_NUM_MINUS4_MAX, "log2_max_frame_num_minus4") {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_LOG2_MAX_FRAME_NUM_MINUS4)
	}
	pSps.uiLog2MaxFrameNum = LOG2_MAX_FRAME_NUM_OFFSET + uiCode
	if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //pic_order_cnt_type
		return int32(uiRet)
	}
	pSps.uiPocType = uiCode

	if 0 == pSps.uiPocType {
		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //log2_max_pic_order_cnt_lsb_minus4
			return int32(uiRet)
		}
		// log2_max_pic_order_cnt_lsb_minus4 should be in range 0 to 12, inclusive. (sec. 7.4.3)
		if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, SPS_LOG2_MAX_PIC_ORDER_CNT_LSB_MINUS4_MAX, "log2_max_pic_order_cnt_lsb_minus4") {
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_LOG2_MAX_PIC_ORDER_CNT_LSB_MINUS4)
		}
		pSps.iLog2MaxPocLsb = int32(LOG2_MAX_PIC_ORDER_CNT_LSB_OFFSET + uiCode) // log2_max_pic_order_cnt_lsb_minus4

	} else if 1 == pSps.uiPocType {
		var i int32
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //delta_pic_order_always_zero_flag
			return int32(uiRet)
		}
		pSps.bDeltaPicOrderAlwaysZeroFlag = uiCode != 0
		if iRet := BsGetSe(pBs, &iCode); iRet != ERR_NONE { //offset_for_non_ref_pic
			return iRet
		}
		pSps.iOffsetForNonRefPic = iCode
		if iRet := BsGetSe(pBs, &iCode); iRet != ERR_NONE { //offset_for_top_to_bottom_field
			return iRet
		}
		pSps.iOffsetForTopToBottomField = iCode
		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //num_ref_frames_in_pic_order_cnt_cycle
			return int32(uiRet)
		}
		if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, SPS_NUM_REF_FRAMES_IN_PIC_ORDER_CNT_CYCLE_MAX,
			"num_ref_frames_in_pic_order_cnt_cycle") {
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_NUM_REF_FRAME_IN_PIC_ORDER_CNT_CYCLE)
		}
		pSps.iNumRefFramesInPocCycle = int32(uiCode)
		for i = 0; i < pSps.iNumRefFramesInPocCycle; i++ {
			if iRet := BsGetSe(pBs, &iCode); iRet != ERR_NONE { //offset_for_ref_frame[ i ]
				return iRet
			}
			pSps.iOffsetForRefFrame[i] = int8(iCode)
		}
	}
	if pSps.uiPocType > 2 {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, " illegal pic_order_cnt_type: %d ! ", pSps.uiPocType)
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_POC_TYPE)
	}

	if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //max_num_ref_frames
		return int32(uiRet)
	}
	pSps.iNumRefFrames = int32(uiCode)
	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //gaps_in_frame_num_value_allowed_flag
		return int32(uiRet)
	}
	pSps.bGapsInFrameNumValueAllowedFlag = uiCode != 0
	if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //pic_width_in_mbs_minus1
		return int32(uiRet)
	}
	pSps.iMbWidth = PIC_WIDTH_IN_MBS_OFFSET + uiCode
	if pSps.iMbWidth > MAX_MB_SIZE || pSps.iMbWidth == 0 {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "pic_width_in_mbs(%d) invalid!", pSps.iMbWidth)
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_MAX_MB_SIZE)
	}
	if (uint64(pSps.iMbWidth) * uint64(pSps.iMbWidth)) > uint64(8*pSLevelLimits.UiMaxFS) {
		if (uint64(pSps.iMbWidth) * uint64(pSps.iMbWidth)) > uint64(8*pSMaxLevelLimits.UiMaxFS) {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "the pic_width_in_mbs exceeds the level limits!")
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_MAX_MB_SIZE)
		}
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "the pic_width_in_mbs exceeds the level limits!")
	}
	if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //pic_height_in_map_units_minus1
		return int32(uiRet)
	}
	pSps.iMbHeight = PIC_HEIGHT_IN_MAP_UNITS_OFFSET + uiCode
	if pSps.iMbHeight > MAX_MB_SIZE || pSps.iMbHeight == 0 {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "pic_height_in_mbs(%d) invalid!", pSps.iMbHeight)
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_MAX_MB_SIZE)
	}
	if (uint64(pSps.iMbHeight) * uint64(pSps.iMbHeight)) > uint64(8*pSLevelLimits.UiMaxFS) {
		if (uint64(pSps.iMbHeight) * uint64(pSps.iMbHeight)) > uint64(8*pSMaxLevelLimits.UiMaxFS) {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "the pic_height_in_mbs exceeds the level limits!")
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_MAX_MB_SIZE)
		}
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "the pic_height_in_mbs exceeds the level limits!")
	}
	uiTmp64 := uint64(pSps.iMbWidth) * uint64(pSps.iMbHeight)
	if uiTmp64 > uint64(pSLevelLimits.UiMaxFS) {
		if uiTmp64 > uint64(pSMaxLevelLimits.UiMaxFS) {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "the total count of mb exceeds the level limits!")
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_MAX_MB_SIZE)
		}
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "the total count of mb exceeds the level limits!")
	}
	pSps.uiTotalMbCount = uint32(uiTmp64)
	if WELS_CHECK_SE_UPPER_ERROR(pCtx, pSps.iNumRefFrames, SPS_MAX_NUM_REF_FRAMES_MAX, "max_num_ref_frames") {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_MAX_NUM_REF_FRAMES)
	}
	// here we check max_num_ref_frames
	uiMaxDpbMbs := pSLevelLimits.UiMaxDPBMbs
	uiMaxDpbFrames := uiMaxDpbMbs / pSps.uiTotalMbCount
	if uiMaxDpbFrames > SPS_MAX_NUM_REF_FRAMES_MAX {
		uiMaxDpbFrames = SPS_MAX_NUM_REF_FRAMES_MAX
	}
	if uint32(pSps.iNumRefFrames) > uiMaxDpbFrames {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, " max_num_ref_frames exceeds level limits!")
	}
	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //frame_mbs_only_flag
		return int32(uiRet)
	}
	pSps.bFrameMbsOnlyFlag = uiCode != 0
	if !pSps.bFrameMbsOnlyFlag {
		common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "ParseSps(): frame_mbs_only_flag (%d) not supported.",
			auParserB2I(pSps.bFrameMbsOnlyFlag))
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_UNSUPPORTED_MBAFF)
	}
	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //direct_8x8_inference_flag
		return int32(uiRet)
	}
	pSps.bDirect8x8InferenceFlag = uiCode != 0
	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //frame_cropping_flag
		return int32(uiRet)
	}
	pSps.bFrameCroppingFlag = uiCode != 0
	if pSps.bFrameCroppingFlag {
		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //frame_crop_left_offset
			return int32(uiRet)
		}
		pSps.sFrameCrop.iLeftOffset = int32(uiCode)
		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //frame_crop_right_offset
			return int32(uiRet)
		}
		pSps.sFrameCrop.iRightOffset = int32(uiCode)
		if (pSps.sFrameCrop.iLeftOffset + pSps.sFrameCrop.iRightOffset) > (int32(pSps.iMbWidth) * 16 / 2) {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "frame_crop_left_offset + frame_crop_right_offset exceeds limits!")
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_CROPPING_DATA)
		}
		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //frame_crop_top_offset
			return int32(uiRet)
		}
		pSps.sFrameCrop.iTopOffset = int32(uiCode)
		if uiRet := BsGetUe(pBs, &uiCode); uiRet != ERR_NONE { //frame_crop_bottom_offset
			return int32(uiRet)
		}
		pSps.sFrameCrop.iBottomOffset = int32(uiCode)
		if (pSps.sFrameCrop.iTopOffset + pSps.sFrameCrop.iBottomOffset) > (int32(pSps.iMbHeight) * 16 / 2) {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "frame_crop_top_offset + frame_crop_right_offset exceeds limits!")
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_CROPPING_DATA)
		}
	} else {
		pSps.sFrameCrop.iLeftOffset = 0   // frame_crop_left_offset
		pSps.sFrameCrop.iRightOffset = 0  // frame_crop_right_offset
		pSps.sFrameCrop.iTopOffset = 0    // frame_crop_top_offset
		pSps.sFrameCrop.iBottomOffset = 0 // frame_crop_bottom_offset
	}
	if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //vui_parameters_present_flag
		return int32(uiRet)
	}
	pSps.bVuiParamPresentFlag = uiCode != 0
	if pSps.bVuiParamPresentFlag {
		iRetVui := ParseVui(pCtx, pSps, pBsAux)
		if iRetVui == GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_UNSUPPORTED_VUI_HRD) {
			if kbUseSubsetFlag { //Currently do no support VUI with HRD enable in subsetSPS
				common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_ERROR, "hrd parse in vui of subsetSPS is not supported!")
				return iRetVui
			}
		} else {
			if iRetVui != ERR_NONE {
				return iRetVui
			}
		}
	}

	if pCtx.pParam.BParseOnly {
		if kSrcNalLen >= SPS_PPS_BS_SIZE-4 { //sps bs exceeds!
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "sps payload size (%d) too large for parse only (%d), not supported!",
				kSrcNalLen, SPS_PPS_BS_SIZE-4)
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_OUT_OF_MEMORY)
		}
		if !kbUseSubsetFlag { //SPS
			pSpsBs := &pCtx.sSpsBsInfo[iSpsId]
			pSpsBs.iSpsId = iSpsId
			var iTrailingZeroByte int32
			for pSrcNal[kSrcNalLen-iTrailingZeroByte-1] == 0x0 { //remove final trailing 0 bytes
				iTrailingZeroByte++
			}
			iActualLen := kSrcNalLen - iTrailingZeroByte
			pSpsBs.uiSpsBsLen = uint16(iActualLen)
			//unify start code as 0x0001
			var iStartDeltaByte int32                                        //0 for 0x0001, 1 for 0x001
			if pSrcNal[0] == 0x0 && pSrcNal[1] == 0x0 && pSrcNal[2] == 0x1 { //if 0x001
				pSpsBs.pSpsBsBuf[0] = 0x0 //add 0 to form 0x0001
				iStartDeltaByte++
				pSpsBs.uiSpsBsLen++
			}
			copy(pSpsBs.pSpsBsBuf[iStartDeltaByte:], pSrcNal[:iActualLen])
		} else { //subset SPS
			pSpsBs := &pCtx.sSubsetSpsBsInfo[iSpsId]
			pSpsBs.iSpsId = iSpsId
			pSpsBs.pSpsBsBuf[0], pSpsBs.pSpsBsBuf[1], pSpsBs.pSpsBsBuf[2] = 0x00, 0x00, 0x00
			pSpsBs.pSpsBsBuf[3] = 0x01
			pSpsBs.pSpsBsBuf[4] = 0x67

			//re-write subset SPS to SPS
			var sSubsetSpsBs common.SBitStringAux

			pBsBuf := make([]uint8, SPS_PPS_BS_SIZE+4) //to reserve 4 bytes for UVLC writing buffer
			common.InitBits(&sSubsetSpsBs, pBsBuf, 0, int32(pBs.PEndBuf-pBs.PStartBuf))
			common.BsWriteBits(&sSubsetSpsBs, 8, 77)                                           //profile_idc, forced to Main profile
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bConstraintSet0Flag))) // constraint_set0_flag
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bConstraintSet1Flag))) // constraint_set1_flag
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bConstraintSet2Flag))) // constraint_set2_flag
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bConstraintSet3Flag))) // constraint_set3_flag
			common.BsWriteBits(&sSubsetSpsBs, 4, 0)                                            //constraint_set4_flag, constraint_set5_flag, reserved_zero_2bits
			common.BsWriteBits(&sSubsetSpsBs, 8, uint32(pSps.uiLevelIdc))                      //level_idc
			common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.iSpsId))                               //sps_id
			common.BsWriteUE(&sSubsetSpsBs, pSps.uiLog2MaxFrameNum-4)                          //log2_max_frame_num_minus4
			common.BsWriteUE(&sSubsetSpsBs, pSps.uiPocType)                                    //pic_order_cnt_type
			if pSps.uiPocType == 0 {
				common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.iLog2MaxPocLsb-4)) //log2_max_pic_order_cnt_lsb_minus4
			} else if pSps.uiPocType == 1 {
				common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bDeltaPicOrderAlwaysZeroFlag))) //delta_pic_order_always_zero_flag
				common.BsWriteSE(&sSubsetSpsBs, pSps.iOffsetForNonRefPic)                                   //offset_for_no_ref_pic
				common.BsWriteSE(&sSubsetSpsBs, pSps.iOffsetForTopToBottomField)                            //offset_for_top_to_bottom_field
				common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.iNumRefFramesInPocCycle))                       //num_ref_frames_in_pic_order_cnt_cycle
				for i := int32(0); i < pSps.iNumRefFramesInPocCycle; i++ {
					common.BsWriteSE(&sSubsetSpsBs, int32(pSps.iOffsetForRefFrame[i])) //offset_for_ref_frame[i]
				}
			}
			common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.iNumRefFrames))                                    //max_num_ref_frames
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bGapsInFrameNumValueAllowedFlag))) //gaps_in_frame_num_value_allowed_flag
			common.BsWriteUE(&sSubsetSpsBs, pSps.iMbWidth-1)                                               //pic_width_in_mbs_minus1
			common.BsWriteUE(&sSubsetSpsBs, pSps.iMbHeight-1)                                              //pic_height_in_map_units_minus1
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bFrameMbsOnlyFlag)))               //frame_mbs_only_flag
			if !pSps.bFrameMbsOnlyFlag {
				common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bMbaffFlag))) //mb_adaptive_frame_field_flag
			}
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bDirect8x8InferenceFlag))) //direct_8x8_inference_flag
			common.BsWriteOneBit(&sSubsetSpsBs, uint32(auParserB2I(pSps.bFrameCroppingFlag)))      //frame_cropping_flag
			if pSps.bFrameCroppingFlag {
				common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.sFrameCrop.iLeftOffset))   //frame_crop_left_offset
				common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.sFrameCrop.iRightOffset))  //frame_crop_right_offset
				common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.sFrameCrop.iTopOffset))    //frame_crop_top_offset
				common.BsWriteUE(&sSubsetSpsBs, uint32(pSps.sFrameCrop.iBottomOffset)) //frame_crop_bottom_offset
			}
			common.BsWriteOneBit(&sSubsetSpsBs, 0)   //vui_parameters_present_flag
			common.BsRbspTrailingBits(&sSubsetSpsBs) //finished, rbsp trailing bit
			iRbspSize := int32(sSubsetSpsBs.PCurBuf - sSubsetSpsBs.PStartBuf)
			RBSP2EBSP(pSpsBs.pSpsBsBuf[5:], sSubsetSpsBs.PBuf[sSubsetSpsBs.PStartBuf:], iRbspSize)
			pSpsBs.uiSpsBsLen = uint16(sSubsetSpsBs.PCurBuf - sSubsetSpsBs.PStartBuf + 5)
		}
	}
	// Check if SPS SVC extension applicated
	if kbUseSubsetFlag && (api.PRO_SCALABLE_BASELINE == eProfile || api.PRO_SCALABLE_HIGH == eProfile) {
		if iRet = DecodeSpsSvcExt(pCtx, pSubsetSps, pBs); iRet != ERR_NONE {
			return iRet
		}

		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE { //svc_vui_parameters_present_flag
			return int32(uiRet)
		}
		pSubsetSps.bSvcVuiParamPresentFlag = uiCode != 0
		if pSubsetSps.bSvcVuiParamPresentFlag {
		}
	}

	if api.PRO_SCALABLE_BASELINE == eProfile || api.PRO_SCALABLE_HIGH == eProfile {
		pCtx.sSpsPpsCtx.bAvcBasedFlag = false
	}

	*pPicWidth = int32(pSps.iMbWidth << 4)
	*pPicHeight = int32(pSps.iMbHeight << 4)
	var pTmpSps *SSps
	if kbUseSubsetFlag {
		pTmpSps = &pCtx.sSpsPpsCtx.sSubsetSpsBuffer[iSpsId].sSps
	} else {
		pTmpSps = &pCtx.sSpsPpsCtx.sSpsBuffer[iSpsId]
	}
	if CheckSpsActive(pCtx, pTmpSps, kbUseSubsetFlag) {
		// we are overwriting the active sps, copy a temp buffer
		if kbUseSubsetFlag {
			if pCtx.sSpsPpsCtx.sSubsetSpsBuffer[iSpsId] != *pSubsetSps {
				if pCtx.pAccessUnitList.uiAvailUnitsNum > 0 {
					pCtx.sSpsPpsCtx.sSubsetSpsBuffer[common.MAX_SPS_COUNT] = *pSubsetSps
					pCtx.bAuReadyFlag = true
					pCtx.pAccessUnitList.uiEndPos = pCtx.pAccessUnitList.uiAvailUnitsNum - 1
					pCtx.sSpsPpsCtx.iOverwriteFlags |= OVERWRITE_SUBSETSPS
				} else if (pCtx.pSps != nil) && (pCtx.pSps.iSpsId == pSubsetSps.sSps.iSpsId) {
					pCtx.sSpsPpsCtx.sSubsetSpsBuffer[common.MAX_SPS_COUNT] = *pSubsetSps
					pCtx.sSpsPpsCtx.iOverwriteFlags |= OVERWRITE_SUBSETSPS
				} else {
					pCtx.sSpsPpsCtx.sSubsetSpsBuffer[iSpsId] = *pSubsetSps
				}
			}
		} else {
			if pCtx.sSpsPpsCtx.sSpsBuffer[iSpsId] != *pSps {
				if pCtx.pAccessUnitList.uiAvailUnitsNum > 0 {
					pCtx.sSpsPpsCtx.sSpsBuffer[common.MAX_SPS_COUNT] = *pSps
					pCtx.sSpsPpsCtx.iOverwriteFlags |= OVERWRITE_SPS
					pCtx.bAuReadyFlag = true
					pCtx.pAccessUnitList.uiEndPos = pCtx.pAccessUnitList.uiAvailUnitsNum - 1
				} else if (pCtx.pSps != nil) && (pCtx.pSps.iSpsId == pSps.iSpsId) {
					pCtx.sSpsPpsCtx.sSpsBuffer[common.MAX_SPS_COUNT] = *pSps
					pCtx.sSpsPpsCtx.iOverwriteFlags |= OVERWRITE_SPS
				} else {
					pCtx.sSpsPpsCtx.sSpsBuffer[iSpsId] = *pSps
				}
			}
		}
	} else if kbUseSubsetFlag {
		// Not overwrite active sps, just copy to final place
		pCtx.sSpsPpsCtx.sSubsetSpsBuffer[iSpsId] = *pSubsetSps
		pCtx.sSpsPpsCtx.bSubspsAvailFlags[iSpsId] = true
		pCtx.sSpsPpsCtx.bSubspsExistAheadFlag = true
	} else {
		pCtx.sSpsPpsCtx.sSpsBuffer[iSpsId] = *pSps
		pCtx.sSpsPpsCtx.bSpsAvailFlags[iSpsId] = true
		pCtx.sSpsPpsCtx.bSpsExistAheadFlag = true
	}
	return ERR_NONE
}

// ParsePps ports int32_t ParsePps (PWelsDecoderContext pCtx, PPps pPpsList, PBitStringAux pBsAux,
// uint8_t* pSrcNal, const int32_t kSrcNalLen): to parse Picture Parameter Set (PPS).
//
// pPpsList: C PPps pointing at sSpsPpsCtx.sPpsBuffer[0] -> sPpsBuffer[:] (unused, as in C). pSrcNal:
// sub-slice starting at the C pointer.
//
// return 0 - successed, 1 - failed. Call it in case eNalUnitType is PPS.
func ParsePps(pCtx *SWelsDecoderContext, pPpsList []SPps, pBsAux *common.SBitStringAux, pSrcNal []uint8, kSrcNalLen int32) int32 {
	var pPps *SPps
	var sTempPps SPps
	var uiPpsId uint32
	var iTmp uint32
	var uiCode uint32
	var iCode int32

	if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //pic_parameter_set_id
		return int32(uiRet)
	}
	uiPpsId = uiCode
	if uiPpsId >= MAX_PPS_COUNT {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_PPS_ID_OVERFLOW)
	}
	pPps = &sTempPps

	pPps.iPpsId = int32(uiPpsId)
	if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //seq_parameter_set_id
		return int32(uiRet)
	}
	pPps.iSpsId = int32(uiCode)

	if pPps.iSpsId >= common.MAX_SPS_COUNT {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_SPS_ID_OVERFLOW)
	}

	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //entropy_coding_mode_flag
		return int32(uiRet)
	}
	pPps.bEntropyCodingModeFlag = uiCode != 0
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //bottom_field_pic_order_in_frame_present_flag
		return int32(uiRet)
	}
	pPps.bPicOrderPresentFlag = uiCode != 0

	if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //num_slice_groups_minus1
		return int32(uiRet)
	}
	pPps.uiNumSliceGroups = NUM_SLICE_GROUPS_OFFSET + uiCode

	if pPps.uiNumSliceGroups > MAX_SLICEGROUP_IDS {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_SLICEGROUP)
	}

	if pPps.uiNumSliceGroups > 1 {
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //slice_group_map_type
			return int32(uiRet)
		}
		pPps.uiSliceGroupMapType = uiCode
		if pPps.uiSliceGroupMapType > 1 {
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "ParsePps(): slice_group_map_type (%d): support only 0,1.",
				pPps.uiSliceGroupMapType)
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_UNSUPPORTED_FMOTYPE)
		}

		switch pPps.uiSliceGroupMapType {
		case 0:
			for iTmp = 0; iTmp < pPps.uiNumSliceGroups; iTmp++ {
				if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //run_length_minus1[ iGroup ]
					return int32(uiRet)
				}
				if WELS_CHECK_SE_UPPER_ERROR(pCtx, uiCode, MAX_MB_SIZE-1, "run_length_minus1") {
					return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_SLICEGROUP)
				}
				pPps.uiRunLength[iTmp] = RUN_LENGTH_OFFSET + uiCode
			}
		default:
		}
	}

	if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //num_ref_idx_l0_default_active_minus1
		return int32(uiRet)
	}
	pPps.uiNumRefIdxL0Active = NUM_REF_IDX_L0_DEFAULT_ACTIVE_OFFSET + uiCode
	if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //num_ref_idx_l1_default_active_minus1
		return int32(uiRet)
	}
	pPps.uiNumRefIdxL1Active = NUM_REF_IDX_L1_DEFAULT_ACTIVE_OFFSET + uiCode

	if pPps.uiNumRefIdxL0Active > MAX_REF_PIC_COUNT ||
		pPps.uiNumRefIdxL1Active > MAX_REF_PIC_COUNT {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_REF_COUNT_OVERFLOW)
	}

	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //weighted_pred_flag
		return int32(uiRet)
	}
	pPps.bWeightedPredFlag = uiCode != 0
	if iRet := BsGetBits(pBsAux, 2, &uiCode); iRet != ERR_NONE { //weighted_bipred_idc
		return iRet
	}
	pPps.uiWeightedBipredIdc = uint8(uiCode)
	// weighted_bipred_idc > 0 NOT supported now, but no impact when we ignore it

	if iRet := BsGetSe(pBsAux, &iCode); iRet != ERR_NONE { //pic_init_qp_minus26
		return iRet
	}
	pPps.iPicInitQp = PIC_INIT_QP_OFFSET + iCode
	if WELS_CHECK_SE_BOTH_ERROR(pCtx, pPps.iPicInitQp, PPS_PIC_INIT_QP_QS_MIN, PPS_PIC_INIT_QP_QS_MAX, "pic_init_qp_minus26 + 26") {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_PIC_INIT_QP)
	}
	if iRet := BsGetSe(pBsAux, &iCode); iRet != ERR_NONE { //pic_init_qs_minus26
		return iRet
	}
	pPps.iPicInitQs = PIC_INIT_QS_OFFSET + iCode
	if WELS_CHECK_SE_BOTH_ERROR(pCtx, pPps.iPicInitQs, PPS_PIC_INIT_QP_QS_MIN, PPS_PIC_INIT_QP_QS_MAX, "pic_init_qs_minus26 + 26") {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_PIC_INIT_QS)
	}
	if iRet := BsGetSe(pBsAux, &iCode); iRet != ERR_NONE { //chroma_qp_index_offset,cb
		return iRet
	}
	pPps.iChromaQpIndexOffset[0] = iCode
	if WELS_CHECK_SE_BOTH_ERROR(pCtx, pPps.iChromaQpIndexOffset[0], PPS_CHROMA_QP_INDEX_OFFSET_MIN, PPS_CHROMA_QP_INDEX_OFFSET_MAX,
		"chroma_qp_index_offset") {
		return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_CHROMA_QP_INDEX_OFFSET)
	}
	pPps.iChromaQpIndexOffset[1] = pPps.iChromaQpIndexOffset[0]   //init cr qp offset
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //deblocking_filter_control_present_flag
		return int32(uiRet)
	}
	pPps.bDeblockingFilterControlPresentFlag = uiCode != 0
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //constrained_intra_pred_flag
		return int32(uiRet)
	}
	pPps.bConstainedIntraPredFlag = uiCode != 0
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //redundant_pic_cnt_present_flag
		return int32(uiRet)
	}
	pPps.bRedundantPicCntPresentFlag = uiCode != 0

	if CheckMoreRBSPData(pBsAux) {
		if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //transform_8x8_mode_flag
			return int32(uiRet)
		}
		pPps.bTransform8x8ModeFlag = uiCode != 0
		if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //pic_scaling_matrix_present_flag
			return int32(uiRet)
		}
		pPps.bPicScalingMatrixPresentFlag = uiCode != 0
		if pPps.bPicScalingMatrixPresentFlag {
			// C indexes bSpsAvailFlags[iSpsId] even for a negative iSpsId (out of bounds read); a
			// negative id is treated as "not available" here.
			if pPps.iSpsId >= 0 && pCtx.sSpsPpsCtx.bSpsAvailFlags[pPps.iSpsId] {
				if iRet := ParseScalingList(&pCtx.sSpsPpsCtx.sSpsBuffer[pPps.iSpsId], pBsAux, true, pPps.bTransform8x8ModeFlag,
					pPps.bPicScalingListPresentFlag[:], &pPps.iScalingList4x4, &pPps.iScalingList8x8); iRet != ERR_NONE {
					return iRet
				}
			} else {
				common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING,
					"ParsePps(): sps_id (%d) does not exist for scaling_list. This PPS (%d) is marked as invalid.", pPps.iSpsId,
					pPps.iPpsId)
				return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_SPS_ID)
			}
		}
		if iRet := BsGetSe(pBsAux, &iCode); iRet != ERR_NONE { //second_chroma_qp_index_offset
			return iRet
		}
		pPps.iChromaQpIndexOffset[1] = iCode
		if WELS_CHECK_SE_BOTH_ERROR(pCtx, pPps.iChromaQpIndexOffset[1], PPS_CHROMA_QP_INDEX_OFFSET_MIN,
			PPS_CHROMA_QP_INDEX_OFFSET_MAX, "chroma_qp_index_offset") {
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_INVALID_CHROMA_QP_INDEX_OFFSET)
		}
	}

	if pCtx.pPps != nil && pCtx.pPps.iPpsId == pPps.iPpsId {
		if *pCtx.pPps != *pPps {
			pCtx.sSpsPpsCtx.sPpsBuffer[MAX_PPS_COUNT] = *pPps
			pCtx.sSpsPpsCtx.iOverwriteFlags |= OVERWRITE_PPS
			if pCtx.pAccessUnitList.uiAvailUnitsNum > 0 {
				pCtx.bAuReadyFlag = true
				pCtx.pAccessUnitList.uiEndPos = pCtx.pAccessUnitList.uiAvailUnitsNum - 1
			}
		}
	} else {
		pCtx.sSpsPpsCtx.sPpsBuffer[uiPpsId] = *pPps
		pCtx.sSpsPpsCtx.bPpsAvailFlags[uiPpsId] = true
	}
	if pCtx.pParam.BParseOnly {
		if kSrcNalLen >= SPS_PPS_BS_SIZE-4 { //pps bs exceeds
			common.WelsLog(&(pCtx.sLogCtx), api.WELS_LOG_WARNING, "pps payload size (%d) too large for parse only (%d), not supported!",
				kSrcNalLen, SPS_PPS_BS_SIZE-4)
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return GENERATE_ERROR_NO(ERR_LEVEL_PARAM_SETS, ERR_INFO_OUT_OF_MEMORY)
		}
		pPpsBs := &pCtx.sPpsBsInfo[uiPpsId]
		pPpsBs.iPpsId = int32(uiPpsId)
		var iTrailingZeroByte int32
		for pSrcNal[kSrcNalLen-iTrailingZeroByte-1] == 0x0 { //remove final trailing 0 bytes
			iTrailingZeroByte++
		}
		iActualLen := kSrcNalLen - iTrailingZeroByte
		pPpsBs.uiPpsBsLen = uint16(iActualLen)
		//unify start code as 0x0001
		var iStartDeltaByte int32                                        //0 for 0x0001, 1 for 0x001
		if pSrcNal[0] == 0x0 && pSrcNal[1] == 0x0 && pSrcNal[2] == 0x1 { //if 0x001
			pPpsBs.pPpsBsBuf[0] = 0x0 //add 0 to form 0x0001
			iStartDeltaByte++
			pPpsBs.uiPpsBsLen++
		}
		copy(pPpsBs.pPpsBsBuf[iStartDeltaByte:], pSrcNal[:iActualLen])
	}
	return ERR_NONE
}

const (
	VUI_MAX_CHROMA_LOG_TYPE_TOP_BOTTOM_FIELD_MAX = 5
	VUI_NUM_UNITS_IN_TICK_MIN                    = 1
	VUI_TIME_SCALE_MIN                           = 1
	VUI_MAX_BYTES_PER_PIC_DENOM_MAX              = 16
	VUI_MAX_BITS_PER_MB_DENOM_MAX                = 16
	VUI_LOG2_MAX_MV_LENGTH_HOR_MAX               = 16
	VUI_LOG2_MAX_MV_LENGTH_VER_MAX               = 16
	VUI_MAX_DEC_FRAME_BUFFERING_MAX              = 16
)

// ParseVui ports int32_t ParseVui (PWelsDecoderContext pCtx, PSps pSps, PBitStringAux pBsAux).
func ParseVui(pCtx *SWelsDecoderContext, pSps *SSps, pBsAux *common.SBitStringAux) int32 {
	var uiCode uint32
	pVui := &pSps.sVui
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //aspect_ratio_info_present_flag
		return int32(uiRet)
	}
	pVui.bAspectRatioInfoPresentFlag = uiCode != 0
	if pSps.sVui.bAspectRatioInfoPresentFlag {
		if iRet := BsGetBits(pBsAux, 8, &uiCode); iRet != ERR_NONE { //aspect_ratio_idc
			return iRet
		}
		pVui.uiAspectRatioIdc = uiCode
		if pVui.uiAspectRatioIdc < 17 {
			pVui.uiSarWidth = g_ksVuiSampleAspectRatio[pVui.uiAspectRatioIdc].uiWidth
			pVui.uiSarHeight = g_ksVuiSampleAspectRatio[pVui.uiAspectRatioIdc].uiHeight
		} else if pVui.uiAspectRatioIdc == EXTENDED_SAR {
			if iRet := BsGetBits(pBsAux, 16, &uiCode); iRet != ERR_NONE { //sar_width
				return iRet
			}
			pVui.uiSarWidth = uiCode
			if iRet := BsGetBits(pBsAux, 16, &uiCode); iRet != ERR_NONE { //sar_height
				return iRet
			}
			pVui.uiSarHeight = uiCode
		}
	}
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //overscan_info_present_flag
		return int32(uiRet)
	}
	pVui.bOverscanInfoPresentFlag = uiCode != 0
	if pVui.bOverscanInfoPresentFlag {
		if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //overscan_appropriate_flag
			return int32(uiRet)
		}
		pVui.bOverscanAppropriateFlag = uiCode != 0
	}
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //video_signal_type_present_flag
		return int32(uiRet)
	}
	pVui.bVideoSignalTypePresentFlag = uiCode != 0
	if pVui.bVideoSignalTypePresentFlag {
		if iRet := BsGetBits(pBsAux, 3, &uiCode); iRet != ERR_NONE { //video_format
			return iRet
		}
		pVui.uiVideoFormat = uint8(uiCode)
		if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //video_full_range_flag
			return int32(uiRet)
		}
		pVui.bVideoFullRangeFlag = uiCode != 0
		if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //colour_description_present_flag
			return int32(uiRet)
		}
		pVui.bColourDescripPresentFlag = uiCode != 0
		if pVui.bColourDescripPresentFlag {
			if iRet := BsGetBits(pBsAux, 8, &uiCode); iRet != ERR_NONE { //colour_primaries
				return iRet
			}
			pVui.uiColourPrimaries = uint8(uiCode)
			if iRet := BsGetBits(pBsAux, 8, &uiCode); iRet != ERR_NONE { //transfer_characteristics
				return iRet
			}
			pVui.uiTransferCharacteristics = uint8(uiCode)
			if iRet := BsGetBits(pBsAux, 8, &uiCode); iRet != ERR_NONE { //matrix_coefficients
				return iRet
			}
			pVui.uiMatrixCoeffs = uint8(uiCode)
		}
	}
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //chroma_loc_info_present_flag
		return int32(uiRet)
	}
	pVui.bChromaLocInfoPresentFlag = uiCode != 0
	if pVui.bChromaLocInfoPresentFlag {
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //chroma_sample_loc_type_top_field
			return int32(uiRet)
		}
		pVui.uiChromaSampleLocTypeTopField = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiChromaSampleLocTypeTopField, VUI_MAX_CHROMA_LOG_TYPE_TOP_BOTTOM_FIELD_MAX,
			"chroma_sample_loc_type_top_field")
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //chroma_sample_loc_type_bottom_field
			return int32(uiRet)
		}
		pVui.uiChromaSampleLocTypeBottomField = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiChromaSampleLocTypeBottomField, VUI_MAX_CHROMA_LOG_TYPE_TOP_BOTTOM_FIELD_MAX,
			"chroma_sample_loc_type_bottom_field")
	}
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //timing_info_present_flag
		return int32(uiRet)
	}
	pVui.bTimingInfoPresentFlag = uiCode != 0
	if pVui.bTimingInfoPresentFlag {
		var uiTmp uint32
		if iRet := BsGetBits(pBsAux, 16, &uiCode); iRet != ERR_NONE { //num_units_in_tick
			return iRet
		}
		uiTmp = uiCode << 16
		if iRet := BsGetBits(pBsAux, 16, &uiCode); iRet != ERR_NONE { //num_units_in_tick
			return iRet
		}
		uiTmp |= uiCode
		pVui.uiNumUnitsInTick = uiTmp
		WELS_CHECK_SE_LOWER_WARNING(pCtx, pVui.uiNumUnitsInTick, VUI_NUM_UNITS_IN_TICK_MIN, "num_units_in_tick")
		if iRet := BsGetBits(pBsAux, 16, &uiCode); iRet != ERR_NONE { //time_scale
			return iRet
		}
		uiTmp = uiCode << 16
		if iRet := BsGetBits(pBsAux, 16, &uiCode); iRet != ERR_NONE { //time_scale
			return iRet
		}
		uiTmp |= uiCode
		pVui.uiTimeScale = uiTmp
		WELS_CHECK_SE_LOWER_WARNING(pCtx, pVui.uiNumUnitsInTick, VUI_TIME_SCALE_MIN, "time_scale")
		if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //fixed_frame_rate_flag
			return int32(uiRet)
		}
		pVui.bFixedFrameRateFlag = uiCode != 0
	}
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //nal_hrd_parameters_present_flag
		return int32(uiRet)
	}
	pVui.bNalHrdParamPresentFlag = uiCode != 0
	if pVui.bNalHrdParamPresentFlag { //Add HRD parse. the values are not being used though.
		parseVuiHrdParameters(pBsAux)
	}
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //vcl_hrd_parameters_present_flag
		return int32(uiRet)
	}
	pVui.bVclHrdParamPresentFlag = uiCode != 0
	if pVui.bVclHrdParamPresentFlag { //Add HRD parse. the values are not being used though.
		parseVuiHrdParameters(pBsAux)
	}
	if pVui.bNalHrdParamPresentFlag || pVui.bVclHrdParamPresentFlag {
		/*low_delay_hrd_flag = */ BsGetOneBit(pBsAux, &uiCode)
	}
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //pic_struct_present_flag
		return int32(uiRet)
	}
	pVui.bPicStructPresentFlag = uiCode != 0
	if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //bitstream_restriction_flag
		return int32(uiRet)
	}
	pVui.bBitstreamRestrictionFlag = uiCode != 0
	if pVui.bBitstreamRestrictionFlag {
		if uiRet := BsGetOneBit(pBsAux, &uiCode); uiRet != ERR_NONE { //motion_vectors_over_pic_boundaries_flag
			return int32(uiRet)
		}
		pVui.bMotionVectorsOverPicBoundariesFlag = uiCode != 0
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //max_bytes_per_pic_denom
			return int32(uiRet)
		}
		pVui.uiMaxBytesPerPicDenom = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiMaxBytesPerPicDenom, VUI_MAX_BYTES_PER_PIC_DENOM_MAX,
			"max_bytes_per_pic_denom")
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //max_bits_per_mb_denom
			return int32(uiRet)
		}
		pVui.uiMaxBitsPerMbDenom = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiMaxBitsPerMbDenom, VUI_MAX_BITS_PER_MB_DENOM_MAX,
			"max_bits_per_mb_denom")
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //log2_max_mv_length_horizontal
			return int32(uiRet)
		}
		pVui.uiLog2MaxMvLengthHorizontal = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiLog2MaxMvLengthHorizontal, VUI_LOG2_MAX_MV_LENGTH_HOR_MAX,
			"log2_max_mv_length_horizontal")
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //log2_max_mv_length_vertical
			return int32(uiRet)
		}
		pVui.uiLog2MaxMvLengthVertical = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiLog2MaxMvLengthVertical, VUI_LOG2_MAX_MV_LENGTH_VER_MAX,
			"log2_max_mv_length_vertical")
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //max_num_reorder_frames
			return int32(uiRet)
		}
		pVui.uiMaxNumReorderFrames = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiMaxNumReorderFrames, VUI_MAX_DEC_FRAME_BUFFERING_MAX,
			"max_num_reorder_frames")
		if uiRet := BsGetUe(pBsAux, &uiCode); uiRet != ERR_NONE { //max_dec_frame_buffering
			return int32(uiRet)
		}
		pVui.uiMaxDecFrameBuffering = uiCode
		WELS_CHECK_SE_UPPER_WARNING(pCtx, pVui.uiMaxDecFrameBuffering, VUI_MAX_DEC_FRAME_BUFFERING_MAX,
			"max_num_reorder_frames")
	}
	return ERR_NONE
}

// parseVuiHrdParameters is the (duplicated, inline in C) _PARSE_NALHRD_VCLHRD_PARAMS_ block of
// ParseVui that skips nal/vcl hrd_parameters(). Errors are ignored as in C.
//
// Note: as in the C code, cpb_cnt_minus1 receives the *return value* of BsGetUe (the error code,
// normally 0), not the decoded value.
func parseVuiHrdParameters(pBsAux *common.SBitStringAux) {
	var uiCode uint32
	cpb_cnt_minus1 := int32(BsGetUe(pBsAux, &uiCode))
	/*bit_rate_scale = */ BsGetBits(pBsAux, 4, &uiCode)
	/*cpb_size_scale = */ BsGetBits(pBsAux, 4, &uiCode)
	for i := int32(0); i <= cpb_cnt_minus1; i++ {
		/*bit_rate_value_minus1[i] = */ BsGetUe(pBsAux, &uiCode)
		/*cpb_size_value_minus1[i] = */ BsGetUe(pBsAux, &uiCode)
		/*cbr_flag[i] = */ BsGetOneBit(pBsAux, &uiCode)
	}
	/*initial_cpb_removal_delay_length_minus1 = */ BsGetBits(pBsAux, 5, &uiCode)
	/*cpb_removal_delay_length_minus1 = */ BsGetBits(pBsAux, 5, &uiCode)
	/*dpb_output_delay_length_minus1 = */ BsGetBits(pBsAux, 5, &uiCode)
	/*time_offset_length = */ BsGetBits(pBsAux, 5, &uiCode)
}

// ParseSei ports int32_t ParseSei (void* pSei, PBitStringAux pBsAux): to parse SEI message payload
// (reserved Sei_Msg type).
func ParseSei(pSei any, pBsAux *common.SBitStringAux) int32 {
	return ERR_NONE
}

// SetScalingListValue ports int32_t SetScalingListValue (uint8_t* pScalingList, int iScalingListNum,
// bool* bUseDefaultScalingMatrixFlag, PBitStringAux pBsAux).
//
// pScalingList: sub-slice (e.g. iScalingList4x4[i][:]).
func SetScalingListValue(pScalingList []uint8, iScalingListNum int32, bUseDefaultScalingMatrixFlag *bool, pBsAux *common.SBitStringAux) int32 {
	var iLastScale int32 = 8
	var iNextScale int32 = 8
	var iDeltaScale int32
	var iCode int32
	var iIdx int32
	for j := int32(0); j < iScalingListNum; j++ {
		if iNextScale != 0 {
			if iRet := BsGetSe(pBsAux, &iCode); iRet != ERR_NONE {
				return iRet
			}
			if WELS_CHECK_SE_BOTH_ERROR_NOLOG(iCode, SCALING_LIST_DELTA_SCALE_MIN, SCALING_LIST_DELTA_SCALE_MAX, "DeltaScale") {
				return ERR_SCALING_LIST_DELTA_SCALE
			}
			iDeltaScale = iCode
			iNextScale = (iLastScale + iDeltaScale + 256) % 256
			*bUseDefaultScalingMatrixFlag = (j == 0 && iNextScale == 0)
			if *bUseDefaultScalingMatrixFlag {
				break
			}
		}
		if iScalingListNum == 16 {
			iIdx = int32(g_kuiZigzagScan[j])
		} else {
			iIdx = int32(g_kuiZigzagScan8x8[j])
		}
		if iNextScale == 0 {
			pScalingList[iIdx] = uint8(iLastScale)
		} else {
			pScalingList[iIdx] = uint8(iNextScale)
		}
		iLastScale = int32(pScalingList[iIdx])
	}

	return ERR_NONE
}

// ParseScalingList ports int32_t ParseScalingList (PSps pSps, PBitStringAux pBs, bool bPPS, const bool
// kbTrans8x8ModeFlag, bool* pScalingListPresentFlag, uint8_t (*iScalingList4x4)[16], uint8_t
// (*iScalingList8x8)[64]).
//
// pScalingListPresentFlag: the 12-entry flag array as a slice.
func ParseScalingList(pSps *SSps, pBs *common.SBitStringAux, bPPS bool, kbTrans8x8ModeFlag bool, pScalingListPresentFlag []bool, iScalingList4x4 *[6][16]uint8, iScalingList8x8 *[6][64]uint8) int32 {
	var uiScalingListNum uint32
	var uiCode uint32

	bUseDefaultScalingMatrixFlag4x4 := false
	bUseDefaultScalingMatrixFlag8x8 := false
	bInit := false
	var defaultScaling [4][]uint8

	if !bPPS { //sps scaling_list
		if pSps.uiChromaFormatIdc != 3 {
			uiScalingListNum = 8
		} else {
			uiScalingListNum = 12
		}
	} else { //pps scaling_list
		var iMul uint32 = 6
		if pSps.uiChromaFormatIdc != 3 {
			iMul = 2
		}
		uiScalingListNum = 6 + uint32(auParserB2I(kbTrans8x8ModeFlag))*iMul
		bInit = pSps.bSeqScalingMatrixPresentFlag
	}

	//Init default_scaling_list value for sps or pps
	if bInit {
		defaultScaling[0] = pSps.iScalingList4x4[0][:]
		defaultScaling[1] = pSps.iScalingList4x4[3][:]
		defaultScaling[2] = pSps.iScalingList8x8[0][:]
		defaultScaling[3] = pSps.iScalingList8x8[1][:]
	} else {
		defaultScaling[0] = common.G_kuiDequantScaling4x4Default[0][:]
		defaultScaling[1] = common.G_kuiDequantScaling4x4Default[1][:]
		defaultScaling[2] = common.G_kuiDequantScaling8x8Default[0][:]
		defaultScaling[3] = common.G_kuiDequantScaling8x8Default[1][:]
	}

	for i := uint32(0); i < uiScalingListNum; i++ {
		if uiRet := BsGetOneBit(pBs, &uiCode); uiRet != ERR_NONE {
			return int32(uiRet)
		}
		pScalingListPresentFlag[i] = uiCode != 0
		if uiCode != 0 {
			if i < 6 { //4x4 scaling list
				if iRet := SetScalingListValue(iScalingList4x4[i][:], 16, &bUseDefaultScalingMatrixFlag4x4, pBs); iRet != ERR_NONE {
					return iRet
				}
				if bUseDefaultScalingMatrixFlag4x4 {
					bUseDefaultScalingMatrixFlag4x4 = false
					iScalingList4x4[i] = common.G_kuiDequantScaling4x4Default[i/3]
				}

			} else {
				if iRet := SetScalingListValue(iScalingList8x8[i-6][:], 64, &bUseDefaultScalingMatrixFlag8x8, pBs); iRet != ERR_NONE {
					return iRet
				}

				if bUseDefaultScalingMatrixFlag8x8 {
					bUseDefaultScalingMatrixFlag8x8 = false
					iScalingList8x8[i-6] = common.G_kuiDequantScaling8x8Default[(i-6)&1]
				}
			}

		} else {
			if i < 6 {
				if (i != 0) && (i != 3) {
					iScalingList4x4[i] = iScalingList4x4[i-1]
				} else {
					copy(iScalingList4x4[i][:], defaultScaling[i/3])
				}

			} else {
				if (i == 6) || (i == 7) {
					copy(iScalingList8x8[i-6][:], defaultScaling[(i&1)+2])
				} else {
					iScalingList8x8[i-6] = iScalingList8x8[i-8]
				}

			}
		}
	}
	return ERR_NONE

}

// ResetFmoList ports int32_t ResetFmoList (PWelsDecoderContext pCtx): reset fmo list due to got Sps
// now.
//
// return count number of fmo context units are reset
func ResetFmoList(pCtx *SWelsDecoderContext) int32 {
	var iCountNum int32
	if pCtx != nil {
		// Fixed memory leak due to PPS_ID might not be continuous sometimes, 1/5/2010
		UninitFmoList(pCtx.sFmoList[:], MAX_PPS_COUNT, pCtx.iActiveFmoNum)
		iCountNum = pCtx.iActiveFmoNum
		pCtx.iActiveFmoNum = 0
	}
	return iCountNum
}
