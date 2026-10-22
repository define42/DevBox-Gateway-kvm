package encoder

// Preserved pre-optimization writer, used as a bit-for-bit oracle for codes,
// partial words, and overflow exits. Hand-computed vectors remain in the
// original CAVLC tests.
import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

func writeBlockResidualCavlcReference(pFuncList *SWelsFuncPtrList, pCoffLevel []int16, iEndIdx int32, iCalRunLevelFlag int32, iResidualProperty int32, iNC int8, pBs *common.SBitStringAux) int32 {
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

	bs := cavlcWriterReference{cavlcBsInit(pBs)}

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
// the callers never read n afterwards). Symbol lengths are nonnegative.
func (s *sCavlcBs) write(n int32, v int32) {
	if n < s.iLeftBits {
		s.uiCurBits = (s.uiCurBits << n) | uint32(v)
		s.iLeftBits -= n
	} else {
		s.flush(n, v)
	}
}

// Keep word emission outside write so the common bit-accumulation path can be
// inlined at each CAVLC symbol. Signed shifts match the original writer.
//
//go:noinline
func (s *sCavlcBs) flush(n int32, v int32) {
	n -= s.iLeftBits
	s.uiCurBits = (s.uiCurBits << uint(s.iLeftBits)) | uint32(v>>uint(n))
	common.WRITE_BE_32(s.pBuf, s.pBufPtr, s.uiCurBits)
	s.pBufPtr += 4
	s.uiCurBits = uint32(v & ((1 << uint(n)) - 1))
	s.iLeftBits = 32 - n
}

// cavlcWriterReference preserves the unsplit bit writer for byte/state parity.
type cavlcWriterReference struct{ sCavlcBs }

func (s *cavlcWriterReference) write(n int32, v int32) {
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
func cavlcBoolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func welsWriteMbResidualReference(pFuncList *SWelsFuncPtrList, sMbCacheInfo *SMbCache, pCurMb *SMB, pBs *common.SBitStringAux) int32 {
	uiMbType := pCurMb.uiMbType
	kiCbpChroma := int32(pCurMb.uiCbp >> 4)
	kiCbpLuma := int32(pCurMb.uiCbp & 0x0F)
	pNonZeroCoeffCount := sMbCacheInfo.iNonZeroCoeffCount[:]
	var pBlock []int16
	var iA, iB, iC int8

	if common.IS_INTRA16x16(uiMbType) {
		/* DC luma */
		iA = pNonZeroCoeffCount[8]
		iB = pNonZeroCoeffCount[1]
		iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
		if writeBlockResidualCavlcReference(pFuncList, sMbCacheInfo.pDct.iLumaI16x16Dc[:], 15, 1, int32(LUMA_4x4), iC, pBs) != 0 {
			return ENC_RETURN_VLCOVERFLOWFOUND
		}

		/* AC Luma */
		if kiCbpLuma != 0 {
			pBlock = sMbCacheInfo.pDct.iLumaBlock[:]

			for i := 0; i < 16; i++ {
				iIdx := int(common.G_kuiCache48CountScan4Idx[i])
				iA = pNonZeroCoeffCount[iIdx-1]
				iB = pNonZeroCoeffCount[iIdx-8]
				iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
				if writeBlockResidualCavlcReference(pFuncList, pBlock, 14, cavlcBoolToInt32(pNonZeroCoeffCount[iIdx] > 0), int32(LUMA_AC), iC, pBs) != 0 {
					return ENC_RETURN_VLCOVERFLOWFOUND
				}
				pBlock = pBlock[16:]
			}
		}
	} else {
		/* Luma DC AC */
		if kiCbpLuma != 0 {
			pBlock = sMbCacheInfo.pDct.iLumaBlock[:]

			for i := 0; i < 16; i += 4 {
				if kiCbpLuma&(1<<uint(i>>2)) != 0 {
					iIdx := int(common.G_kuiCache48CountScan4Idx[i])
					kiA := pNonZeroCoeffCount[iIdx]
					kiB := pNonZeroCoeffCount[iIdx+1]
					kiC := pNonZeroCoeffCount[iIdx+8]
					kiD := pNonZeroCoeffCount[iIdx+9]
					iA = pNonZeroCoeffCount[iIdx-1]
					iB = pNonZeroCoeffCount[iIdx-8]
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock, 15, cavlcBoolToInt32(kiA > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}

					iA = kiA
					iB = pNonZeroCoeffCount[iIdx-7]
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock[16:], 15, cavlcBoolToInt32(kiB > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}

					iA = pNonZeroCoeffCount[iIdx+7]
					iB = kiA
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock[32:], 15, cavlcBoolToInt32(kiC > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}

					iA = kiC
					iB = kiB
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock[48:], 15, cavlcBoolToInt32(kiD > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}
				}
				pBlock = pBlock[64:]
			}
		}
	}

	if kiCbpChroma != 0 {
		/* Chroma DC residual present */
		pBlock = sMbCacheInfo.pDct.iChromaDc[0][:] // Cb
		if writeBlockResidualCavlcReference(pFuncList, pBlock, 3, 1, int32(CHROMA_DC), CHROMA_DC_NC_OFFSET, pBs) != 0 {
			return ENC_RETURN_VLCOVERFLOWFOUND
		}

		pBlock = sMbCacheInfo.pDct.iChromaDc[1][:] // Cr
		if writeBlockResidualCavlcReference(pFuncList, pBlock, 3, 1, int32(CHROMA_DC), CHROMA_DC_NC_OFFSET, pBs) != 0 {
			return ENC_RETURN_VLCOVERFLOWFOUND
		}

		/* Chroma AC residual present */
		if kiCbpChroma&0x02 != 0 {
			kCache48CountScan4Idx16base := common.G_kuiCache48CountScan4Idx[16:]
			pBlock = sMbCacheInfo.pDct.iChromaBlock[:] // Cb

			for i := 0; i < 4; i++ {
				iIdx := int(kCache48CountScan4Idx16base[i])
				iA = pNonZeroCoeffCount[iIdx-1]
				iB = pNonZeroCoeffCount[iIdx-8]
				iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
				if writeBlockResidualCavlcReference(pFuncList, pBlock, 14, cavlcBoolToInt32(pNonZeroCoeffCount[iIdx] > 0), int32(CHROMA_AC), iC, pBs) != 0 {
					return ENC_RETURN_VLCOVERFLOWFOUND
				}
				pBlock = pBlock[16:]
			}

			pBlock = sMbCacheInfo.pDct.iChromaBlock[4*16:] // Cr

			for i := 0; i < 4; i++ {
				iIdx := 24 + int(kCache48CountScan4Idx16base[i])
				iA = pNonZeroCoeffCount[iIdx-1]
				iB = pNonZeroCoeffCount[iIdx-8]
				iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
				if writeBlockResidualCavlcReference(pFuncList, pBlock, 14, cavlcBoolToInt32(pNonZeroCoeffCount[iIdx] > 0), int32(CHROMA_AC), iC, pBs) != 0 {
					return ENC_RETURN_VLCOVERFLOWFOUND
				}
				pBlock = pBlock[16:]
			}
		}
	}
	return 0
}
