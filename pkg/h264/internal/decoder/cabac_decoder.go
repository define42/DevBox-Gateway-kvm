// Port of codec/decoder/core/src/cabac_decoder.cpp: CABAC state transition
// and related functions.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var g_kMvdBinPos2Ctx = [8]int16{0, 1, 2, 3, 3, 3, 3, 3}

// cabacShl64 is `v << s` for the 64-bit CABAC registers. A negative or
// too-large shift count is undefined behaviour in C; x86-64 masks the count
// to 6 bits, which is what is emulated here (Go would panic on a negative
// count).
func cabacShl64(v uint64, s int32) uint64 {
	return v << (uint32(s) & 63)
}

// cabacByteAt reads buf[i], returning 0 past the end of the buffer (the C
// code relies on the padding behind the bit-stream buffer).
func cabacByteAt(buf []uint8, i int) uint32 {
	if i >= 0 && i < len(buf) {
		return uint32(buf[i])
	}
	return 0
}

// void WelsCabacGlobalInit (PWelsDecoderContext pCtx)
func WelsCabacGlobalInit(pCtx *SWelsDecoderContext) {
	for iModel := 0; iModel < 4; iModel++ {
		for iQp := int32(0); iQp <= WELS_QP_MAX; iQp++ {
			for iIdx := 0; iIdx < common.WELS_CONTEXT_COUNT; iIdx++ {
				m := int32(common.G_kiCabacGlobalContextIdx[iIdx][iModel][0])
				n := int32(common.G_kiCabacGlobalContextIdx[iIdx][iModel][1])
				iPreCtxState := common.WELS_CLIP3(((m*iQp)>>4)+n, 1, 126)
				var uiValMps uint8
				var uiStateIdx uint8
				if iPreCtxState <= 63 {
					uiStateIdx = uint8(63 - iPreCtxState)
					uiValMps = 0
				} else {
					uiStateIdx = uint8(iPreCtxState - 64)
					uiValMps = 1
				}
				pCtx.sWelsCabacContexts[iModel][iQp][iIdx].uiState = uiStateIdx
				pCtx.sWelsCabacContexts[iModel][iQp][iIdx].uiMPS = uiValMps
			}
		}
	}
	pCtx.bCabacInited = true
}

// ------------------- 1. context initialization

// void WelsCabacContextInit (PWelsDecoderContext pCtx, uint8_t eSliceType, int32_t iCabacInitIdc,
// int32_t iQp)
func WelsCabacContextInit(pCtx *SWelsDecoderContext, eSliceType uint8, iCabacInitIdc int32, iQp int32) {
	var iIdx int32
	if pCtx.eSliceType == common.I_SLICE {
		iIdx = 0
	} else {
		iIdx = iCabacInitIdc + 1
	}
	if !pCtx.bCabacInited {
		WelsCabacGlobalInit(pCtx)
	}
	pCtx.pCabacCtx = pCtx.sWelsCabacContexts[iIdx][iQp]
}

// ------------------- 2. decoding Engine initialization

// int32_t InitCabacDecEngineFromBS (PWelsCabacDecEngine pDecEngine, PBitStringAux pBsAux)
func InitCabacDecEngineFromBS(pDecEngine *SWelsCabacDecEngine, pBsAux *common.SBitStringAux) int32 {
	iRemainingBits := -pBsAux.ILeftBits               //pBsAux->iLeftBits < 0
	iRemainingBytes := int((iRemainingBits >> 3) + 2) //+2: indicating the pre-read 2 bytes

	pBuf := pBsAux.PBuf
	pCurr := pBsAux.PCurBuf - iRemainingBytes
	if pCurr >= (pBsAux.PEndBuf - 1) {
		return ERR_INFO_INVALID_ACCESS
	}
	pDecEngine.uiOffset = uint64((cabacByteAt(pBuf, pCurr) << 16) | (cabacByteAt(pBuf, pCurr+1) << 8) | cabacByteAt(pBuf, pCurr+2))
	pDecEngine.uiOffset <<= 16
	pDecEngine.uiOffset |= uint64((cabacByteAt(pBuf, pCurr+3) << 8) | cabacByteAt(pBuf, pCurr+4))
	pDecEngine.iBitsLeft = 31
	pDecEngine.pBuff = pBuf
	pDecEngine.pBuffCurr = pCurr + 5

	pDecEngine.uiRange = WELS_CABAC_HALF
	pDecEngine.pBuffStart = pBsAux.PStartBuf
	pDecEngine.pBuffEnd = pBsAux.PEndBuf
	pBsAux.ILeftBits = 0
	return ERR_NONE
}

// void RestoreCabacDecEngineToBS (PWelsCabacDecEngine pDecEngine, PBitStringAux pBsAux)
func RestoreCabacDecEngineToBS(pDecEngine *SWelsCabacDecEngine, pBsAux *common.SBitStringAux) {
	//CABAC decoding finished, changing to SBitStringAux
	pDecEngine.pBuffCurr -= int(pDecEngine.iBitsLeft >> 3)
	pDecEngine.iBitsLeft = 0 //pcm_alignment_zero_bit in CABAC
	pBsAux.ILeftBits = 0
	pBsAux.PBuf = pDecEngine.pBuff
	pBsAux.PStartBuf = pDecEngine.pBuffStart
	pBsAux.PCurBuf = pDecEngine.pBuffCurr
	pBsAux.UiCurBits = 0
	pBsAux.IIndex = 0
}

// ------------------- 3. actual decoding

// int32_t Read32BitsCabac (PWelsCabacDecEngine pDecEngine, uint32_t& uiValue, int32_t& iNumBitsRead)
func Read32BitsCabac(pDecEngine *SWelsCabacDecEngine, uiValue *uint32, iNumBitsRead *int32) int32 {
	iLeftBytes := pDecEngine.pBuffEnd - pDecEngine.pBuffCurr
	*iNumBitsRead = 0
	*uiValue = 0
	if iLeftBytes <= 0 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_CABAC_NO_BS_TO_READ)
	}
	pBuf := pDecEngine.pBuff
	p := pDecEngine.pBuffCurr
	switch iLeftBytes {
	case 3:
		*uiValue = (cabacByteAt(pBuf, p) << 16) | (cabacByteAt(pBuf, p+1) << 8) | cabacByteAt(pBuf, p+2)
		pDecEngine.pBuffCurr += 3
		*iNumBitsRead = 24
	case 2:
		*uiValue = (cabacByteAt(pBuf, p) << 8) | cabacByteAt(pBuf, p+1)
		pDecEngine.pBuffCurr += 2
		*iNumBitsRead = 16
	case 1:
		*uiValue = cabacByteAt(pBuf, p)
		pDecEngine.pBuffCurr += 1
		*iNumBitsRead = 8
	default:
		*uiValue = (cabacByteAt(pBuf, p) << 24) | (cabacByteAt(pBuf, p+1) << 16) | (cabacByteAt(pBuf, p+2) << 8) |
			cabacByteAt(pBuf, p+3)
		pDecEngine.pBuffCurr += 4
		*iNumBitsRead = 32
	}
	return ERR_NONE
}

// int32_t DecodeBinCabac (PWelsCabacDecEngine pDecEngine, PWelsCabacCtx pBinCtx, uint32_t& uiBinVal)
func DecodeBinCabac(pDecEngine *SWelsCabacDecEngine, pBinCtx *SWelsCabacCtx, uiBinVal *uint32) int32 {
	var iErrorInfo int32 = ERR_NONE
	uiState := uint32(pBinCtx.uiState)
	*uiBinVal = uint32(pBinCtx.uiMPS)
	uiOffset := pDecEngine.uiOffset
	uiRange := pDecEngine.uiRange

	var iRenorm int32 = 1
	uiRangeLPS := uint32(common.G_kuiCabacRangeLps[uiState][(uiRange>>6)&0x03])
	uiRange -= uint64(uiRangeLPS)
	if uiOffset >= cabacShl64(uiRange, pDecEngine.iBitsLeft) { //LPS
		uiOffset -= cabacShl64(uiRange, pDecEngine.iBitsLeft)
		*uiBinVal ^= 0x0001
		if uiState == 0 {
			pBinCtx.uiMPS ^= 0x01
		}
		pBinCtx.uiState = common.G_kuiStateTransTable[uiState][0]
		iRenorm = int32(g_kRenormTable256[uiRangeLPS])
		uiRange = uint64(uiRangeLPS) << uint32(iRenorm)
	} else { //MPS
		pBinCtx.uiState = common.G_kuiStateTransTable[uiState][1]
		if uiRange >= WELS_CABAC_QUARTER {
			pDecEngine.uiRange = uiRange
			return ERR_NONE
		} else {
			uiRange <<= 1
		}
	}
	//Renorm
	pDecEngine.uiRange = uiRange
	pDecEngine.iBitsLeft -= iRenorm
	if pDecEngine.iBitsLeft > 0 {
		pDecEngine.uiOffset = uiOffset
		return ERR_NONE
	}
	var uiVal uint32
	var iNumBitsRead int32
	iErrorInfo = Read32BitsCabac(pDecEngine, &uiVal, &iNumBitsRead)
	pDecEngine.uiOffset = cabacShl64(uiOffset, iNumBitsRead) | uint64(uiVal)
	pDecEngine.iBitsLeft += iNumBitsRead
	if iErrorInfo != 0 && pDecEngine.iBitsLeft < 0 {
		return iErrorInfo
	}
	return ERR_NONE
}

// int32_t DecodeBypassCabac (PWelsCabacDecEngine pDecEngine, uint32_t& uiBinVal)
func DecodeBypassCabac(pDecEngine *SWelsCabacDecEngine, uiBinVal *uint32) int32 {
	var iErrorInfo int32 = ERR_NONE
	iBitsLeft := pDecEngine.iBitsLeft
	uiOffset := pDecEngine.uiOffset
	var uiRangeValue uint64

	if iBitsLeft <= 0 {
		var uiVal uint32
		var iNumBitsRead int32
		iErrorInfo = Read32BitsCabac(pDecEngine, &uiVal, &iNumBitsRead)
		uiOffset = cabacShl64(uiOffset, iNumBitsRead) | uint64(uiVal)
		iBitsLeft = iNumBitsRead
		if iErrorInfo != 0 && iBitsLeft == 0 {
			return iErrorInfo
		}
	}
	iBitsLeft--
	uiRangeValue = cabacShl64(pDecEngine.uiRange, iBitsLeft)
	if uiOffset >= uiRangeValue {
		pDecEngine.iBitsLeft = iBitsLeft
		pDecEngine.uiOffset = uiOffset - uiRangeValue
		*uiBinVal = 1
		return ERR_NONE
	}
	pDecEngine.iBitsLeft = iBitsLeft
	pDecEngine.uiOffset = uiOffset
	*uiBinVal = 0
	return ERR_NONE
}

// int32_t DecodeTerminateCabac (PWelsCabacDecEngine pDecEngine, uint32_t& uiBinVal)
func DecodeTerminateCabac(pDecEngine *SWelsCabacDecEngine, uiBinVal *uint32) int32 {
	var iErrorInfo int32 = ERR_NONE
	uiRange := pDecEngine.uiRange - 2
	uiOffset := pDecEngine.uiOffset

	if uiOffset >= cabacShl64(uiRange, pDecEngine.iBitsLeft) {
		*uiBinVal = 1
	} else {
		*uiBinVal = 0
		// Renorm
		if uiRange < WELS_CABAC_QUARTER {
			iRenorm := int32(g_kRenormTable256[uiRange])
			pDecEngine.uiRange = uiRange << uint32(iRenorm)
			pDecEngine.iBitsLeft -= iRenorm
			if pDecEngine.iBitsLeft < 0 {
				var uiVal uint32
				var iNumBitsRead int32
				iErrorInfo = Read32BitsCabac(pDecEngine, &uiVal, &iNumBitsRead)
				pDecEngine.uiOffset = cabacShl64(pDecEngine.uiOffset, iNumBitsRead) | uint64(uiVal)
				pDecEngine.iBitsLeft += iNumBitsRead
			}
			if iErrorInfo != 0 && pDecEngine.iBitsLeft < 0 {
				return iErrorInfo
			}
			return ERR_NONE
		} else {
			pDecEngine.uiRange = uiRange
			return ERR_NONE
		}
	}
	return ERR_NONE
}

// int32_t DecodeUnaryBinCabac (PWelsCabacDecEngine pDecEngine, PWelsCabacCtx pBinCtx, int32_t
// iCtxOffset, uint32_t& uiSymVal)
//
// pBinCtx: sub-slice of the context array starting at the C pointer (pBinCtx[0] is *pBinCtx,
// pBinCtx[iCtxOffset] is pBinCtx + iCtxOffset).
func DecodeUnaryBinCabac(pDecEngine *SWelsCabacDecEngine, pBinCtx []SWelsCabacCtx, iCtxOffset int32, uiSymVal *uint32) int32 {
	*uiSymVal = 0
	if uiRet := DecodeBinCabac(pDecEngine, &pBinCtx[0], uiSymVal); uiRet != ERR_NONE {
		return uiRet
	}
	if *uiSymVal == 0 {
		return ERR_NONE
	} else {
		var uiCode uint32
		pCtx := &pBinCtx[iCtxOffset]
		*uiSymVal = 0
		for {
			if uiRet := DecodeBinCabac(pDecEngine, pCtx, &uiCode); uiRet != ERR_NONE {
				return uiRet
			}
			*uiSymVal++
			if uiCode == 0 {
				break
			}
		}
		return ERR_NONE
	}
}

// int32_t DecodeExpBypassCabac (PWelsCabacDecEngine pDecEngine, int32_t iCount, uint32_t& uiSymVal)
func DecodeExpBypassCabac(pDecEngine *SWelsCabacDecEngine, iCount int32, uiSymVal *uint32) int32 {
	var uiCode uint32
	var iSymTmp int32
	var iSymTmp2 int32
	*uiSymVal = 0
	for {
		if uiRet := DecodeBypassCabac(pDecEngine, &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode == 1 {
			iSymTmp += int32(1) << uint32(iCount)
			iCount++
		}
		if !(uiCode != 0 && iCount != 16) {
			break
		}
	}
	if iCount == 16 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_CABAC_UNEXPECTED_VALUE)
	}

	for iCount != 0 {
		iCount--
		if uiRet := DecodeBypassCabac(pDecEngine, &uiCode); uiRet != ERR_NONE {
			return uiRet
		}
		if uiCode == 1 {
			iSymTmp2 |= int32(1) << uint32(iCount)
		}
	}
	*uiSymVal = uint32(iSymTmp + iSymTmp2)
	return ERR_NONE
}

// uint32_t DecodeUEGLevelCabac (PWelsCabacDecEngine pDecEngine, PWelsCabacCtx pBinCtx, uint32_t&
// uiCode)
func DecodeUEGLevelCabac(pDecEngine *SWelsCabacDecEngine, pBinCtx *SWelsCabacCtx, uiCode *uint32) uint32 {
	*uiCode = 0
	if uiRet := uint32(DecodeBinCabac(pDecEngine, pBinCtx, uiCode)); uiRet != ERR_NONE {
		return uiRet
	}
	if *uiCode == 0 {
		return ERR_NONE
	} else {
		var uiTmp uint32
		var uiCount uint32 = 1
		*uiCode = 0
		for {
			if uiRet := uint32(DecodeBinCabac(pDecEngine, pBinCtx, &uiTmp)); uiRet != ERR_NONE {
				return uiRet
			}
			*uiCode++
			uiCount++
			if !(uiTmp != 0 && uiCount != 13) {
				break
			}
		}

		if uiTmp != 0 {
			if uiRet := uint32(DecodeExpBypassCabac(pDecEngine, 0, &uiTmp)); uiRet != ERR_NONE {
				return uiRet
			}
			*uiCode += uiTmp + 1
		}
		return ERR_NONE
	}
}

// int32_t DecodeUEGMvCabac (PWelsCabacDecEngine pDecEngine, PWelsCabacCtx pBinCtx, uint32_t iMaxBin,
// uint32_t& uiCode)
//
// pBinCtx: sub-slice of the context array starting at the C pointer.
func DecodeUEGMvCabac(pDecEngine *SWelsCabacDecEngine, pBinCtx []SWelsCabacCtx, iMaxBin uint32, uiCode *uint32) int32 {
	if uiRet := DecodeBinCabac(pDecEngine, &pBinCtx[g_kMvdBinPos2Ctx[0]], uiCode); uiRet != ERR_NONE {
		return uiRet
	}
	if *uiCode == 0 {
		return ERR_NONE
	} else {
		var uiTmp uint32
		var uiCount uint32 = 1
		*uiCode = 0
		for {
			pCtx := &pBinCtx[g_kMvdBinPos2Ctx[uiCount]]
			uiCount++
			if uiRet := DecodeBinCabac(pDecEngine, pCtx, &uiTmp); uiRet != ERR_NONE {
				return uiRet
			}
			*uiCode++
			if !(uiTmp != 0 && uiCount != 8) {
				break
			}
		}

		if uiTmp != 0 {
			if uiRet := DecodeExpBypassCabac(pDecEngine, 3, &uiTmp); uiRet != ERR_NONE {
				return uiRet
			}
			*uiCode += uiTmp + 1
		}
		return ERR_NONE
	}
}
