package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// Preserved pre-extraction writer: unlike the older full reference, this uses
// the same fast symbol table and split bit writer to isolate extraction cost.
func writeBlockResidualCavlcScalarExtraction(pFuncList *SWelsFuncPtrList, pCoffLevel []int16, iEndIdx int32, iCalRunLevelFlag int32, iResidualProperty int32, iNC int8, pBs *common.SBitStringAux) int32 {
	var iLevel [16]int16
	var uiRun [16]uint8

	iTotalCoeffs := int32(0)
	iTrailingOnes := int32(0)
	iTotalZeros, iZerosLeft := int32(0), int32(0)
	uiSign := uint32(0)
	var uiSuffixLength int32
	var iValue, iZeroLeft int32
	var n int32
	var i int32

	bs := cavlcBsInit(pBs)

	/*Step 1: calculate iLevel and iRun and total */

	if iCalRunLevelFlag != 0 {
		iCount := int32(0)
		// The direct call keeps these per-block run and level buffers on the
		// stack. Scalar extraction is faster than mask enumeration here.
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

	// The first non-trailing level has a distinct code adjustment. Keeping
	// both forms in the small table also supplies the next suffix length.
	adjustment := 0
	if iTrailingOnes < 3 {
		adjustment = 1
	}
	for i = iTrailingOnes; i < iTotalCoeffs; i++ {
		value := int32(iLevel[i])
		var symbol cavlcLevelSymbol
		if index := uint32(value + cavlcLevelLimit); index < 2*cavlcLevelLimit {
			symbol = cavlcLevelTable[adjustment][uiSuffixLength][index]
		} else {
			var ok bool
			symbol, ok = cavlcLevelSymbolSlow(value, uiSuffixLength, adjustment != 0)
			if !ok {
				return ENC_RETURN_VLCOVERFLOWFOUND
			}
		}
		bs.write(int32(symbol.length), int32(symbol.code))
		uiSuffixLength = int32(symbol.nextSuffix)
		adjustment = 0
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
