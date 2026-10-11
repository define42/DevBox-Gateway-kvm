// Port of codec/encoder/core/src/nal_encap.cpp.
//
// NAL pRawNal pData encapsulation.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// WelsLoadNal loads an initialize NAL pRawNal pData.
func WelsLoadNal(pEncoderOuput *SWelsEncoderOutput, kiType int32, kiNalRefIdc int32) {
	pWelsEncoderOuput := pEncoderOuput
	pRawNal := &pWelsEncoderOuput.sNalList[pWelsEncoderOuput.iNalIndex]
	sNalUnitHeader := &pRawNal.sNalExt.SNalUnitHeader
	kiStartPos := (BsGetBitsPos(&pWelsEncoderOuput.sBsWrite) >> 3)

	sNalUnitHeader.ENalUnitType = common.EWelsNalUnitType(kiType)
	sNalUnitHeader.UiNalRefIdc = uint8(kiNalRefIdc)
	sNalUnitHeader.UiForbiddenZeroBit = 0

	pRawNal.pRawData = pWelsEncoderOuput.pBsBuffer[kiStartPos:]
	pRawNal.iStartPos = kiStartPos
	pRawNal.iPayloadSize = 0
}

// WelsUnloadNal unloads pRawNal NAL.
func WelsUnloadNal(pEncoderOuput *SWelsEncoderOutput) {
	pWelsEncoderOuput := pEncoderOuput
	pIdx := &pWelsEncoderOuput.iNalIndex
	pRawNal := &pWelsEncoderOuput.sNalList[*pIdx]
	kiEndPos := (BsGetBitsPos(&pWelsEncoderOuput.sBsWrite) >> 3)

	/* count payload size of pRawNal NAL */
	pRawNal.iPayloadSize = kiEndPos - pRawNal.iStartPos

	*pIdx++
}

// WelsLoadNalForSlice loads an initialize NAL pRawNal pData.
func WelsLoadNalForSlice(pSliceBs *SWelsSliceBs, kiType int32, kiNalRefIdc int32) {
	pRawNal := &pSliceBs.sNalList[pSliceBs.iNalIndex]
	sNalUnitHeader := &pRawNal.sNalExt.SNalUnitHeader
	pBitStringAux := &pSliceBs.sBsWrite
	kiStartPos := (BsGetBitsPos(pBitStringAux) >> 3)

	sNalUnitHeader.ENalUnitType = common.EWelsNalUnitType(kiType)
	sNalUnitHeader.UiNalRefIdc = uint8(kiNalRefIdc)
	sNalUnitHeader.UiForbiddenZeroBit = 0

	pRawNal.pRawData = pSliceBs.pBsBuffer[kiStartPos:]
	pRawNal.iStartPos = kiStartPos
	pRawNal.iPayloadSize = 0
}

// WelsUnloadNalForSlice unloads pRawNal NAL.
func WelsUnloadNalForSlice(pSliceBs *SWelsSliceBs) {
	pIdx := &pSliceBs.iNalIndex
	pRawNal := &pSliceBs.sNalList[*pIdx]
	pBitStringAux := &pSliceBs.sBsWrite
	kiEndPos := (BsGetBitsPos(pBitStringAux) >> 3)

	/* count payload size of pRawNal NAL */
	pRawNal.iPayloadSize = kiEndPos - pRawNal.iStartPos
	*pIdx++
}

var kuiStartCodePrefix = [NAL_HEADER_SIZE]uint8{0, 0, 0, 1}

func nalB2U8(b bool) uint8 {
	if b {
		return 1
	}
	return 0
}

// WelsEncodeNal encodes NAL with emulation forbidden three bytes checking.
// pNalHeaderExt: void* that is a SNalUnitHeaderExt* (may be nil); pDst: void* output buffer -> []uint8 starting at the destination.
func WelsEncodeNal(pRawNal *SWelsNalRaw, pNalHeaderExt *common.SNalUnitHeaderExt, kiDstBufferLen int32, pDst []uint8, pDstLen *int32) int32 {
	kbNALExt := pRawNal.sNalExt.SNalUnitHeader.ENalUnitType == common.NAL_UNIT_PREFIX ||
		pRawNal.sNalExt.SNalUnitHeader.ENalUnitType == common.NAL_UNIT_CODED_SLICE_EXT
	var iExt int32
	if kbNALExt {
		iExt = 3
	}
	iAssumedNeededLength := NAL_HEADER_SIZE + iExt + pRawNal.iPayloadSize + 1
	if iAssumedNeededLength <= 0 {
		return ENC_RETURN_UNEXPECTED
	}

	//since for each 0x000 need a 0x03, so the needed length will not exceed (iAssumeNeedLenth + iAssumeNeedLength/3), here adjust to >>1 to omit division
	if kiDstBufferLen < (iAssumedNeededLength + (iAssumedNeededLength >> 1)) {
		return ENC_RETURN_MEMALLOCERR
		//TODO: call the realloc&copy instead
	}
	iDst := 0
	pSrc := pRawNal.pRawData[:pRawNal.iPayloadSize]
	var iZeroCount int32
	var iNalLength int32
	if pDstLen != nil {
		*pDstLen = 0
	}

	copy(pDst[iDst:iDst+4], kuiStartCodePrefix[:])
	iDst += 4

	/* NAL Unit Header */
	pDst[iDst] = uint8((int32(pRawNal.sNalExt.SNalUnitHeader.UiNalRefIdc) << 5) | (int32(pRawNal.sNalExt.SNalUnitHeader.ENalUnitType) & 0x1f))
	iDst++

	if kbNALExt {
		sNalExt := pNalHeaderExt

		/* NAL UNIT Extension Header */
		pDst[iDst] = (0x80) | (nalB2U8(sNalExt.BIdrFlag) << 6)
		iDst++

		pDst[iDst] = (0x80) | (sNalExt.UiDependencyId << 4)
		iDst++

		pDst[iDst] = (sNalExt.UiTemporalId << 5) | (nalB2U8(sNalExt.BDiscardableFlag) << 3) | (0x07)
		iDst++
	}

	for _, b := range pSrc {
		if iZeroCount == 2 && b <= 3 {
			//add the code 03
			pDst[iDst] = 3
			iDst++
			iZeroCount = 0
		}
		if b == 0 {
			iZeroCount++
		} else {
			iZeroCount = 0
		}
		pDst[iDst] = b
		iDst++
	}

	/* count length of NAL Unit */
	iNalLength = int32(iDst)
	if nil != pDstLen {
		*pDstLen = iNalLength
	}

	return ENC_RETURN_SUCCESS
}

// WelsWriteSVCPrefixNal writes prefix nal.
func WelsWriteSVCPrefixNal(pBitStringAux *common.SBitStringAux, kiNalRefIdc int32, kbIdrFlag bool) int32 {
	if 0 < kiNalRefIdc {
		common.BsWriteOneBit(pBitStringAux, 0 /*bStoreRefBasePicFlag*/)
		common.BsWriteOneBit(pBitStringAux, 0)
		common.BsRbspTrailingBits(pBitStringAux)
	}
	return 0
}
