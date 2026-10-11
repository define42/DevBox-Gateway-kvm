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

// sCavlcBs holds the local writer state of the CAVLC_BS_INIT / CAVLC_BS_UNINIT
// macros (pBufPtr, uiCurBits, iLeftBits).
type sCavlcBs struct {
	pBuf      []uint8
	pBufPtr   int
	uiCurBits uint32
	iLeftBits int32
}

// CAVLC_BS_INIT( pBs )
func cavlcBsInit(pBs *common.SBitStringAux) sCavlcBs {
	return sCavlcBs{pBuf: pBs.PBuf, pBufPtr: pBs.PCurBuf, uiCurBits: pBs.UiCurBits, iLeftBits: pBs.ILeftBits}
}

// CAVLC_BS_UNINIT( pBs )
func (s *sCavlcBs) uninit(pBs *common.SBitStringAux) {
	pBs.PCurBuf = s.pBufPtr
	pBs.UiCurBits = s.uiCurBits
	pBs.ILeftBits = s.iLeftBits
}

// CAVLC_BS_WRITE( n,  v ) (n is modified by the macro only in its local copy here;
// the callers never read n afterwards).
func (s *sCavlcBs) write(n int32, v int32) {
	if n < s.iLeftBits {
		s.uiCurBits = (s.uiCurBits << uint(n)) | uint32(v)
		s.iLeftBits -= n
	} else {
		n -= s.iLeftBits
		s.uiCurBits = (s.uiCurBits << uint(s.iLeftBits)) | uint32(v>>uint(n))
		common.WRITE_BE_32(s.pBuf, s.pBufPtr, s.uiCurBits)
		s.pBufPtr += 4
		s.uiCurBits = uint32(v & ((1 << uint(n)) - 1))
		s.iLeftBits = 32 - n
	}
}

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

func WriteBlockResidualCavlc(pFuncList *SWelsFuncPtrList, pCoffLevel []int16, iEndIdx int32, iCalRunLevelFlag int32, iResidualProperty int32, iNC int8, pBs *common.SBitStringAux) int32 {
	var iLevel [16]int16
	var uiRun [16]uint8

	iTotalCoeffs := int32(0)
	iTrailingOnes := int32(0)
	iTotalZeros, iZerosLeft := int32(0), int32(0)
	uiSign := uint32(0)
	var iLevelCode, iLevelPrefix, iLevelSuffix, uiSuffixLength, iLevelSuffixSize int32
	var iValue, iThreshold, iZeroLeft int32
	var n int32
	var i int32

	bs := cavlcBsInit(pBs)

	/*Step 1: calculate iLevel and iRun and total */

	if iCalRunLevelFlag != 0 {
		iCount := int32(0)
		// No alternate CAVLC kernel exists in the Go port. The direct call keeps
		// these per-block run and level buffers on the stack.
		iTotalZeros = CavlcParamCal_c(pCoffLevel, uiRun[:], iLevel[:], &iTotalCoeffs, iEndIdx)
		if iTotalCoeffs > 3 {
			iCount = 3
		} else {
			iCount = iTotalCoeffs
		}
		for i = 0; i < iCount; i++ {
			if common.WELS_ABS(int32(iLevel[i])) == 1 {
				iTrailingOnes++
				uiSign <<= 1
				if iLevel[i] < 0 {
					uiSign |= 1
				}
			} else {
				break
			}
		}
	}
	/*Step 3: coeff token */
	upCoeffToken := &g_kuiVlcCoeffToken[g_kuiEncNcMapTable[iNC]][iTotalCoeffs][iTrailingOnes]
	iValue = int32(upCoeffToken[0])
	n = int32(upCoeffToken[1])

	if iTotalCoeffs == 0 {
		bs.write(n, iValue)

		bs.uninit(pBs)
		return ENC_RETURN_SUCCESS
	}

	/* Step 4: */
	/*  trailing */
	n += iTrailingOnes
	iValue = int32(uint32(iValue<<uint(iTrailingOnes)) + uiSign)
	bs.write(n, iValue)

	/*  levels */
	if iTotalCoeffs > 10 && iTrailingOnes < 3 {
		uiSuffixLength = 1
	} else {
		uiSuffixLength = 0
	}

	for i = iTrailingOnes; i < iTotalCoeffs; i++ {
		iVal := int32(iLevel[i])

		iLevelCode = (iVal - 1) * (1 << 1)
		uiSign = uint32(iLevelCode >> 31)
		iLevelCode = int32((uint32(iLevelCode) ^ uiSign) + (uiSign << 1))
		if i == iTrailingOnes && iTrailingOnes < 3 {
			iLevelCode -= 1 << 1
		}

		iLevelPrefix = iLevelCode >> uint(uiSuffixLength)
		iLevelSuffixSize = uiSuffixLength
		iLevelSuffix = iLevelCode - (iLevelPrefix << uint(uiSuffixLength))

		if iLevelPrefix >= 14 && iLevelPrefix < 30 && uiSuffixLength == 0 {
			iLevelPrefix = 14
			iLevelSuffix = iLevelCode - iLevelPrefix
			iLevelSuffixSize = 4
		} else if iLevelPrefix >= 15 {
			iLevelPrefix = 15
			iLevelSuffix = iLevelCode - (iLevelPrefix << uint(uiSuffixLength))
			//for baseline profile,overflow when the length of iLevelSuffix is larger than 11.
			if (iLevelSuffix >> 11) != 0 {
				return ENC_RETURN_VLCOVERFLOWFOUND
			}
			if uiSuffixLength == 0 {
				iLevelSuffix -= 15
			}
			iLevelSuffixSize = 12
		}

		n = iLevelPrefix + 1 + iLevelSuffixSize
		iValue = (1 << uint(iLevelSuffixSize)) | iLevelSuffix
		bs.write(n, iValue)

		if uiSuffixLength == 0 {
			uiSuffixLength++
		}
		iThreshold = 3 << uint(uiSuffixLength-1)
		if (iVal > iThreshold || iVal < -iThreshold) && uiSuffixLength < 6 {
			uiSuffixLength++
		}
	}

	/* Step 5: total zeros */

	if iTotalCoeffs < iEndIdx+1 {
		if int32(CHROMA_DC) != iResidualProperty {
			upTotalZeros := &g_kuiVlcTotalZeros[iTotalCoeffs][iTotalZeros]
			n = int32(upTotalZeros[1])
			iValue = int32(upTotalZeros[0])
			bs.write(n, iValue)
		} else {
			upTotalZeros := &g_kuiVlcTotalZerosChromaDc[iTotalCoeffs][iTotalZeros]
			n = int32(upTotalZeros[1])
			iValue = int32(upTotalZeros[0])
			bs.write(n, iValue)
		}
	}

	/* Step 6: pRun before */
	iZerosLeft = iTotalZeros
	for i = 0; i+1 < iTotalCoeffs && iZerosLeft > 0; i++ {
		uirun := uiRun[i]
		iZeroLeft = int32(g_kuiZeroLeftMap[iZerosLeft])
		n = int32(g_kuiVlcRunBefore[iZeroLeft][uirun][1])
		iValue = int32(g_kuiVlcRunBefore[iZeroLeft][uirun][0])
		bs.write(n, iValue)
		iZerosLeft -= int32(uirun)
	}

	bs.uninit(pBs)
	return ENC_RETURN_SUCCESS
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
