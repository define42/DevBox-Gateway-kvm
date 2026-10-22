// Port of codec/encoder/core/src/set_mb_syn_cavlc.cpp (C code only).

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var g_kuiZeroLeftMap = [16]uint8{
	0, 1, 2, 3, 4, 5, 6, 7, 7, 7, 7, 7, 7, 7, 7, 7,
}

/*
 *  Exponential Golomb codes encoding routines
 */

func CavlcParamCal_c(pCoffLevel []int16, pRun []uint8, pLevel []int16, pTotalCoeff *int32, iLastIndex int32) int32 {
	iTotalZeros := int32(0)
	iTotalCoeffs := int32(0)

	for iLastIndex >= 0 && pCoffLevel[iLastIndex] == 0 {
		iLastIndex--
	}

	for iLastIndex >= 0 {
		iCountZero := int32(0)
		pLevel[iTotalCoeffs] = pCoffLevel[iLastIndex]
		iLastIndex--

		for iLastIndex >= 0 && pCoffLevel[iLastIndex] == 0 {
			iCountZero++
			iLastIndex--
		}
		iTotalZeros += iCountZero
		pRun[iTotalCoeffs] = uint8(iCountZero)
		iTotalCoeffs++
	}
	*pTotalCoeff = iTotalCoeffs
	return iTotalZeros
}

func StashMBStatusCavlc(pDss *SDynamicSlicingStack, pSlice *SSlice, iMbSkipRun int32) {
	pBs := pSlice.pSliceBsa
	pDss.pBsStackBufPtr = pBs.PCurBuf
	pDss.uiBsStackCurBits = pBs.UiCurBits
	pDss.iBsStackLeftBits = pBs.ILeftBits
	pDss.uiLastMbQp = pSlice.uiLastMbQp
	pDss.iMbSkipRunStack = iMbSkipRun
}

func StashPopMBStatusCavlc(pDss *SDynamicSlicingStack, pSlice *SSlice) int32 {
	pBs := pSlice.pSliceBsa
	pBs.PCurBuf = pDss.pBsStackBufPtr
	pBs.UiCurBits = pDss.uiBsStackCurBits
	pBs.ILeftBits = pDss.iBsStackLeftBits
	pSlice.uiLastMbQp = pDss.uiLastMbQp
	return pDss.iMbSkipRunStack
}

func StashMBStatusCabac(pDss *SDynamicSlicingStack, pSlice *SSlice, iMbSkipRun int32) {
	pCtx := &pSlice.sCabacCtx
	pDss.sStoredCabac = *pCtx
	if pDss.pRestoreBuffer != nil {
		iPosBitOffset := GetBsPosCabac(pSlice) - pDss.iStartPos
		iLen := int(iPosBitOffset >> 3)
		if iPosBitOffset&0x07 != 0 {
			iLen++
		}
		copy(pDss.pRestoreBuffer[:iLen], pCtx.m_pBuf[pCtx.m_pBufStart:pCtx.m_pBufStart+iLen])
	}
	pDss.uiLastMbQp = pSlice.uiLastMbQp
	pDss.iMbSkipRunStack = iMbSkipRun
}

func StashPopMBStatusCabac(pDss *SDynamicSlicingStack, pSlice *SSlice) int32 {
	pCtx := &pSlice.sCabacCtx
	*pCtx = pDss.sStoredCabac
	if pDss.pRestoreBuffer != nil {
		iPosBitOffset := GetBsPosCabac(pSlice) - pDss.iStartPos
		iLen := int(iPosBitOffset >> 3)
		if iPosBitOffset&0x07 != 0 {
			iLen++
		}
		copy(pCtx.m_pBuf[pCtx.m_pBufStart:pCtx.m_pBufStart+iLen], pDss.pRestoreBuffer[:iLen])
	}
	pSlice.uiLastMbQp = pDss.uiLastMbQp
	return pDss.iMbSkipRunStack
}

func GetBsPosCavlc(pSlice *SSlice) int32 {
	return BsGetBitsPos(pSlice.pSliceBsa)
}

func GetBsPosCabac(pSlice *SSlice) int32 {
	return int32((pSlice.sCabacCtx.m_pBufCur-pSlice.sCabacCtx.m_pBufStart)<<3) +
		(pSlice.sCabacCtx.m_iLowBitCnt - 9)
}

func WelsWriteSliceEndSyn(pSlice *SSlice, bEntropyCodingModeFlag bool) {
	pBs := pSlice.pSliceBsa
	if bEntropyCodingModeFlag {
		WelsCabacEncodeFlush(&pSlice.sCabacCtx)
		pBs.PCurBuf = WelsCabacEncodeGetPtr(&pSlice.sCabacCtx)
	} else {
		common.BsRbspTrailingBits(pBs)
		common.BsFlush(pBs)
	}
}

func InitCoeffFunc(pFuncList *SWelsFuncPtrList, uiCpuFlag uint32, iEntropyCodingModeFlag int32) {
	pFuncList.pfCavlcParamCal = CavlcParamCal_c

	if iEntropyCodingModeFlag != 0 {
		pFuncList.pfStashMBStatus = StashMBStatusCabac
		pFuncList.pfStashPopMBStatus = StashPopMBStatusCabac
		pFuncList.pfWelsSpatialWriteMbSyn = WelsSpatialWriteMbSynCabac
		pFuncList.pfGetBsPosition = GetBsPosCabac
	} else {
		pFuncList.pfStashMBStatus = StashMBStatusCavlc
		pFuncList.pfStashPopMBStatus = StashPopMBStatusCavlc
		pFuncList.pfWelsSpatialWriteMbSyn = WelsSpatialWriteMbSyn
		pFuncList.pfGetBsPosition = GetBsPosCavlc
	}
}
