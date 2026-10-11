// Port of codec/encoder/core/src/encode_mb_aux.cpp (C code only).
// (g_kiQuantInterFF / g_kiQuantMF are defined in encode_mb_aux_h.go.)

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

/****************************************************************************
 * HDM and Quant functions
 ****************************************************************************/

// #define WELS_ABS_LC(a) ((iSign ^ (int32_t)(a)) - iSign)
func wels_ABS_LC(iSign int32, a int32) int32 {
	return (iSign ^ a) - iSign
}

// #define NEW_QUANT(pDct, iFF, iMF) (((iFF)+ WELS_ABS_LC(pDct))*(iMF)) >>16
func new_QUANT(iSign int32, pDct int32, iFF int32, iMF int32) int32 {
	return ((iFF + wels_ABS_LC(iSign, pDct)) * iMF) >> 16
}

// #define WELS_NEW_QUANT(pDct,iFF,iMF) WELS_ABS_LC(NEW_QUANT(pDct, iFF, iMF))
func wels_NEW_QUANT(pDct int16, iFF int16, iMF int16) int16 {
	iSign := common.WELS_SIGN(pDct)
	return int16(wels_ABS_LC(iSign, new_QUANT(iSign, int32(pDct), int32(iFF), int32(iMF))))
}

func WelsQuant4x4_c(pDct []int16, pFF []int16, pMF []int16) {
	for i := 0; i < 16; i += 4 {
		j := i & 0x07
		pDct[i] = wels_NEW_QUANT(pDct[i], pFF[j], pMF[j])
		pDct[i+1] = wels_NEW_QUANT(pDct[i+1], pFF[j+1], pMF[j+1])
		pDct[i+2] = wels_NEW_QUANT(pDct[i+2], pFF[j+2], pMF[j+2])
		pDct[i+3] = wels_NEW_QUANT(pDct[i+3], pFF[j+3], pMF[j+3])
	}
}

func WelsQuant4x4Dc_c(pDct []int16, iFF int16, iMF int16) {
	for i := 0; i < 16; i += 4 {
		pDct[i] = wels_NEW_QUANT(pDct[i], iFF, iMF)
		pDct[i+1] = wels_NEW_QUANT(pDct[i+1], iFF, iMF)
		pDct[i+2] = wels_NEW_QUANT(pDct[i+2], iFF, iMF)
		pDct[i+3] = wels_NEW_QUANT(pDct[i+3], iFF, iMF)
	}
}

func WelsQuantFour4x4_c(pDct []int16, pFF []int16, pMF []int16) {
	for i := 0; i < 64; i += 4 {
		j := i & 0x07
		pDct[i] = wels_NEW_QUANT(pDct[i], pFF[j], pMF[j])
		pDct[i+1] = wels_NEW_QUANT(pDct[i+1], pFF[j+1], pMF[j+1])
		pDct[i+2] = wels_NEW_QUANT(pDct[i+2], pFF[j+2], pMF[j+2])
		pDct[i+3] = wels_NEW_QUANT(pDct[i+3], pFF[j+3], pMF[j+3])
	}
}

func WelsQuantFour4x4Max_c(pDct []int16, pFF []int16, pMF []int16, pMax []int16) {
	off := 0
	for k := 0; k < 4; k++ {
		iMaxAbs := int16(0)
		for i := 0; i < 16; i++ {
			j := i & 0x07
			iSign := common.WELS_SIGN(pDct[off+i])
			pDct[off+i] = int16(new_QUANT(iSign, int32(pDct[off+i]), int32(pFF[j]), int32(pMF[j])))
			if iMaxAbs < pDct[off+i] {
				iMaxAbs = pDct[off+i]
			}
			pDct[off+i] = int16(wels_ABS_LC(iSign, int32(pDct[off+i])))
		}
		off += 16
		pMax[k] = iMaxAbs
	}
}

func WelsHadamardQuant2x2Skip_c(pRs []int16, iFF int16, iMF int16) int32 {
	var pDct, s [4]int16
	iThreshold := int16(((1<<16)-1)/int32(iMF) - int32(iFF))

	s[0] = pRs[0] + pRs[32]
	s[1] = pRs[0] - pRs[32]
	s[2] = pRs[16] + pRs[48]
	s[3] = pRs[16] - pRs[48]

	pDct[0] = s[0] + s[2]
	pDct[1] = s[0] - s[2]
	pDct[2] = s[1] + s[3]
	pDct[3] = s[1] - s[3]

	kiThreshold := int32(iThreshold)
	if common.WELS_ABS(int32(pDct[0])) > kiThreshold || common.WELS_ABS(int32(pDct[1])) > kiThreshold ||
		common.WELS_ABS(int32(pDct[2])) > kiThreshold || common.WELS_ABS(int32(pDct[3])) > kiThreshold {
		return 1
	}
	return 0
}

func WelsHadamardQuant2x2_c(pRs []int16, iFF int16, iMF int16, pDct []int16, pBlock []int16) int32 {
	var s [4]int16
	iDcNzc := int32(0)

	s[0] = pRs[0] + pRs[32]
	s[1] = pRs[0] - pRs[32]
	s[2] = pRs[16] + pRs[48]
	s[3] = pRs[16] - pRs[48]

	pRs[0] = 0
	pRs[16] = 0
	pRs[32] = 0
	pRs[48] = 0

	pDct[0] = s[0] + s[2]
	pDct[1] = s[0] - s[2]
	pDct[2] = s[1] + s[3]
	pDct[3] = s[1] - s[3]

	pDct[0] = wels_NEW_QUANT(pDct[0], iFF, iMF)
	pDct[1] = wels_NEW_QUANT(pDct[1], iFF, iMF)
	pDct[2] = wels_NEW_QUANT(pDct[2], iFF, iMF)
	pDct[3] = wels_NEW_QUANT(pDct[3], iFF, iMF)

	copy(pBlock[:4], pDct[:4])

	for i := 0; i < 4; i++ {
		if pBlock[i] != 0 {
			iDcNzc++
		}
	}
	return iDcNzc
}

/* dc value pick up and hdm_4x4 */
func WelsHadamardT4Dc_c(pLumaDc []int16, pDct []int16) {
	var p [16]int32
	var s [4]int32

	for i := 0; i < 16; i += 4 {
		iIdx := ((i & 0x08) << 4) + ((i & 0x04) << 3)
		s[0] = int32(pDct[iIdx]) + int32(pDct[iIdx+80])
		s[3] = int32(pDct[iIdx]) - int32(pDct[iIdx+80])
		s[1] = int32(pDct[iIdx+16]) + int32(pDct[iIdx+64])
		s[2] = int32(pDct[iIdx+16]) - int32(pDct[iIdx+64])

		p[i] = s[0] + s[1]
		p[i+2] = s[0] - s[1]
		p[i+1] = s[3] + s[2]
		p[i+3] = s[3] - s[2]
	}

	for i := 0; i < 4; i++ {
		s[0] = p[i] + p[i+12]
		s[3] = p[i] - p[i+12]
		s[1] = p[i+4] + p[i+8]
		s[2] = p[i+4] - p[i+8]

		pLumaDc[i] = int16(common.WELS_CLIP3((s[0]+s[1]+1)>>1, -32768, 32767))
		pLumaDc[i+8] = int16(common.WELS_CLIP3((s[0]-s[1]+1)>>1, -32768, 32767))
		pLumaDc[i+4] = int16(common.WELS_CLIP3((s[3]+s[2]+1)>>1, -32768, 32767))
		pLumaDc[i+12] = int16(common.WELS_CLIP3((s[3]-s[2]+1)>>1, -32768, 32767))
	}
}

/****************************************************************************
 * DCT functions
 ****************************************************************************/
func WelsDctT4_c(pDct []int16, pPixel1 []uint8, iPixel1Off int, iStride1 int32, pPixel2 []uint8, iPixel2Off int, iStride2 int32) {
	var pData [16]int16
	var s [4]int16

	for i := 0; i < 16; i += 4 {
		kiI1 := 1 + i
		kiI2 := 2 + i
		kiI3 := 3 + i

		pData[i] = int16(int32(pPixel1[iPixel1Off]) - int32(pPixel2[iPixel2Off]))
		pData[kiI1] = int16(int32(pPixel1[iPixel1Off+1]) - int32(pPixel2[iPixel2Off+1]))
		pData[kiI2] = int16(int32(pPixel1[iPixel1Off+2]) - int32(pPixel2[iPixel2Off+2]))
		pData[kiI3] = int16(int32(pPixel1[iPixel1Off+3]) - int32(pPixel2[iPixel2Off+3]))

		iPixel1Off += int(iStride1)
		iPixel2Off += int(iStride2)

		/*horizontal transform */
		s[0] = pData[i] + pData[kiI3]
		s[3] = pData[i] - pData[kiI3]
		s[1] = pData[kiI1] + pData[kiI2]
		s[2] = pData[kiI1] - pData[kiI2]

		pDct[i] = s[0] + s[1]
		pDct[kiI2] = s[0] - s[1]
		pDct[kiI1] = (s[3] * (1 << 1)) + s[2]
		pDct[kiI3] = s[3] - (s[2] * (1 << 1))
	}

	/* vertical transform */
	for i := 0; i < 4; i++ {
		kiI4 := 4 + i
		kiI8 := 8 + i
		kiI12 := 12 + i

		s[0] = pDct[i] + pDct[kiI12]
		s[3] = pDct[i] - pDct[kiI12]
		s[1] = pDct[kiI4] + pDct[kiI8]
		s[2] = pDct[kiI4] - pDct[kiI8]

		pDct[i] = s[0] + s[1]
		pDct[kiI8] = s[0] - s[1]
		pDct[kiI4] = (s[3] * (1 << 1)) + s[2]
		pDct[kiI12] = s[3] - (s[2] * (1 << 1))
	}
}

func WelsDctFourT4_c(pDct []int16, pPixel1 []uint8, iPixel1Off int, iStride1 int32, pPixel2 []uint8, iPixel2Off int, iStride2 int32) {
	stride_1 := int(iStride1) << 2
	stride_2 := int(iStride2) << 2

	WelsDctT4_c(pDct, pPixel1, iPixel1Off, iStride1, pPixel2, iPixel2Off, iStride2)
	WelsDctT4_c(pDct[16:], pPixel1, iPixel1Off+4, iStride1, pPixel2, iPixel2Off+4, iStride2)
	WelsDctT4_c(pDct[32:], pPixel1, iPixel1Off+stride_1, iStride1, pPixel2, iPixel2Off+stride_2, iStride2)
	WelsDctT4_c(pDct[48:], pPixel1, iPixel1Off+stride_1+4, iStride1, pPixel2, iPixel2Off+stride_2+4, iStride2)
}

/****************************************************************************
 * Scan and Score functions
 ****************************************************************************/
func WelsScan4x4DcAc_c(pLevel []int16, pDct []int16) {
	pLevel[0] = pDct[0]
	pLevel[1] = pDct[1]
	pLevel[2] = pDct[4]
	pLevel[3] = pDct[8]
	pLevel[4] = pDct[5]
	pLevel[5] = pDct[2]
	pLevel[6] = pDct[3]
	pLevel[7] = pDct[6]
	pLevel[8] = pDct[9]
	pLevel[9] = pDct[12]
	pLevel[10] = pDct[13]
	pLevel[11] = pDct[10]
	pLevel[12] = pDct[7]
	pLevel[13] = pDct[11]
	pLevel[14] = pDct[14]
	pLevel[15] = pDct[15]
}

func WelsScan4x4Ac_c(pLevel []int16, pDct []int16) {
	pLevel[0] = pDct[1]
	pLevel[1] = pDct[4]
	pLevel[2] = pDct[8]
	pLevel[3] = pDct[5]
	pLevel[4] = pDct[2]
	pLevel[5] = pDct[3]
	pLevel[6] = pDct[6]
	pLevel[7] = pDct[9]
	pLevel[8] = pDct[12]
	pLevel[9] = pDct[13]
	pLevel[10] = pDct[10]
	pLevel[11] = pDct[7]
	pLevel[12] = pDct[11]
	pLevel[13] = pDct[14]
	pLevel[14] = pDct[15]
	pLevel[15] = 0
}

func WelsScan4x4Dc(pLevel []int16, pDct []int16) {
	WelsScan4x4DcAc_c(pLevel, pDct)
}

// refer to JVT-O079
var kiTRunTable = [16]int32{3, 2, 2, 1, 1, 1, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}

func WelsCalculateSingleCtr4x4_c(pDct []int16) int32 {
	iSingleCtr := int32(0)
	iIdx := 15
	var iRun int

	for iIdx >= 0 && pDct[iIdx] == 0 {
		iIdx--
	}

	for iIdx >= 0 {
		iIdx--
		iRun = iIdx
		for iIdx >= 0 && pDct[iIdx] == 0 {
			iIdx--
		}
		iRun -= iIdx
		iSingleCtr += kiTRunTable[iRun]
	}
	return iSingleCtr
}

func WelsGetNoneZeroCount_c(pLevel []int16) int32 {
	iCnt := int32(0)
	for iIdx := 0; iIdx < 16; iIdx++ {
		if pLevel[iIdx] == 0 {
			iCnt++
		}
	}
	return 16 - iCnt
}

func WelsInitEncodingFuncs(pFuncList *SWelsFuncPtrList, uiCpuFlag uint32) {
	pFuncList.pfCopy8x8Aligned = common.WelsCopy8x8_c
	pFuncList.pfCopy16x16Aligned = common.WelsCopy16x16_c
	pFuncList.pfCopy16x16NotAligned = common.WelsCopy16x16_c
	pFuncList.pfCopy16x8NotAligned = common.WelsCopy16x8_c
	pFuncList.pfCopy8x16Aligned = common.WelsCopy8x16_c
	pFuncList.pfCopy4x4 = common.WelsCopy4x4_c
	pFuncList.pfCopy8x4 = common.WelsCopy8x4_c
	pFuncList.pfCopy4x8 = common.WelsCopy4x8_c
	pFuncList.pfQuantizationHadamard2x2 = WelsHadamardQuant2x2_c
	pFuncList.pfQuantizationHadamard2x2Skip = WelsHadamardQuant2x2Skip_c
	pFuncList.pfTransformHadamard4x4Dc = WelsHadamardT4Dc_c

	pFuncList.pfDctT4 = WelsDctT4_c
	pFuncList.pfDctFourT4 = WelsDctFourT4_c

	pFuncList.pfScan4x4 = WelsScan4x4DcAc_c
	pFuncList.pfScan4x4Ac = WelsScan4x4Ac_c
	pFuncList.pfCalculateSingleCtr4x4 = WelsCalculateSingleCtr4x4_c

	pFuncList.pfGetNoneZeroCount = WelsGetNoneZeroCount_c

	pFuncList.pfQuantization4x4 = WelsQuant4x4_c
	pFuncList.pfQuantizationDc4x4 = WelsQuant4x4Dc_c
	pFuncList.pfQuantizationFour4x4 = WelsQuantFour4x4_c
	pFuncList.pfQuantizationFour4x4Max = WelsQuantFour4x4Max_c

	if uiCpuFlag&common.WELS_CPU_SSE2 != 0 {
		pFuncList.pfDctT4 = WelsDctT4_sse2
		pFuncList.pfDctFourT4 = WelsDctFourT4_sse2
	}
	initQuantizationSIMD(pFuncList, uiCpuFlag)
}
