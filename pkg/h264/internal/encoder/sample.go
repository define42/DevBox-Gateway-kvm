// Port of codec/encoder/core/src/sample.cpp.

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func WelsSampleSatd4x4_c(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32 {
	var iSatdSum int32
	var pSampleMix [4][4]int32
	var iSample0, iSample1, iSample2, iSample3 int32
	pSrc1 := iSample1Off
	pSrc2 := iSample2Off

	//step 1: get the difference
	for i := 0; i < 4; i++ {
		pSampleMix[i][0] = int32(pSample1[pSrc1+0]) - int32(pSample2[pSrc2+0])
		pSampleMix[i][1] = int32(pSample1[pSrc1+1]) - int32(pSample2[pSrc2+1])
		pSampleMix[i][2] = int32(pSample1[pSrc1+2]) - int32(pSample2[pSrc2+2])
		pSampleMix[i][3] = int32(pSample1[pSrc1+3]) - int32(pSample2[pSrc2+3])

		pSrc1 += int(iStride1)
		pSrc2 += int(iStride2)
	}

	//step 2: horizontal transform
	for i := 0; i < 4; i++ {
		iSample0 = pSampleMix[i][0] + pSampleMix[i][2]
		iSample1 = pSampleMix[i][1] + pSampleMix[i][3]
		iSample2 = pSampleMix[i][0] - pSampleMix[i][2]
		iSample3 = pSampleMix[i][1] - pSampleMix[i][3]

		pSampleMix[i][0] = iSample0 + iSample1
		pSampleMix[i][1] = iSample2 + iSample3
		pSampleMix[i][2] = iSample2 - iSample3
		pSampleMix[i][3] = iSample0 - iSample1
	}

	//step 3: vertical transform and get the sum of SATD
	for i := 0; i < 4; i++ {
		iSample0 = pSampleMix[0][i] + pSampleMix[2][i]
		iSample1 = pSampleMix[1][i] + pSampleMix[3][i]
		iSample2 = pSampleMix[0][i] - pSampleMix[2][i]
		iSample3 = pSampleMix[1][i] - pSampleMix[3][i]

		pSampleMix[0][i] = iSample0 + iSample1
		pSampleMix[1][i] = iSample2 + iSample3
		pSampleMix[2][i] = iSample2 - iSample3
		pSampleMix[3][i] = iSample0 - iSample1

		iSatdSum += common.WELS_ABS(pSampleMix[0][i]) + common.WELS_ABS(pSampleMix[1][i]) +
			common.WELS_ABS(pSampleMix[2][i]) + common.WELS_ABS(pSampleMix[3][i])
	}

	return (iSatdSum + 1) >> 1
}

func WelsSampleSatd8x4_c(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32 {
	var iSatdSum int32
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off, iStride1, pSample2, iSample2Off, iStride2)
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off+4, iStride1, pSample2, iSample2Off+4, iStride2)
	return iSatdSum
}

func WelsSampleSatd4x8_c(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32 {
	var iSatdSum int32
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off, iStride1, pSample2, iSample2Off, iStride2)
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off+int(iStride1<<2), iStride1, pSample2, iSample2Off+int(iStride2<<2), iStride2)
	return iSatdSum
}

func WelsSampleSatd8x8_c(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32 {
	var iSatdSum int32
	s1 := int(iStride1 << 2)
	s2 := int(iStride2 << 2)
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off, iStride1, pSample2, iSample2Off, iStride2)
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off+4, iStride1, pSample2, iSample2Off+4, iStride2)
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off+s1, iStride1, pSample2, iSample2Off+s2, iStride2)
	iSatdSum += WelsSampleSatd4x4_c(pSample1, iSample1Off+s1+4, iStride1, pSample2, iSample2Off+s2+4, iStride2)
	return iSatdSum
}

func WelsSampleSatd16x8_c(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32 {
	var iSatdSum int32
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off, iStride1, pSample2, iSample2Off, iStride2)
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off+8, iStride1, pSample2, iSample2Off+8, iStride2)
	return iSatdSum
}

func WelsSampleSatd8x16_c(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32 {
	var iSatdSum int32
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off, iStride1, pSample2, iSample2Off, iStride2)
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off+int(iStride1<<3), iStride1, pSample2, iSample2Off+int(iStride2<<3), iStride2)
	return iSatdSum
}

func WelsSampleSatd16x16_c(pSample1 []uint8, iSample1Off int, iStride1 int32, pSample2 []uint8, iSample2Off int, iStride2 int32) int32 {
	var iSatdSum int32
	s1 := int(iStride1 << 3)
	s2 := int(iStride2 << 3)
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off, iStride1, pSample2, iSample2Off, iStride2)
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off+8, iStride1, pSample2, iSample2Off+8, iStride2)
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off+s1, iStride1, pSample2, iSample2Off+s2, iStride2)
	iSatdSum += WelsSampleSatd8x8_c(pSample1, iSample1Off+s1+8, iStride1, pSample2, iSample2Off+s2+8, iStride2)
	return iSatdSum
}

func WelsSampleSatdIntra4x4Combined3_c(pDec []uint8, iDecOff int, iDecStride int32, pEnc []uint8, iEncOff int, iEncStride int32, pDst []uint8, iDstOff int, pBestMode *int32, iLambda2 int32, iLambda1 int32, iLambda0 int32) int32 {
	var iBestMode int32 = -1
	var iCurCost int32
	var iBestCost int32 = math.MaxInt32
	var uiLocalBuffer [3][16]uint8

	WelsI4x4LumaPredDc_c(uiLocalBuffer[2][:], 0, pDec, iDecOff, iDecStride)
	iCurCost = WelsSampleSatd4x4_c(uiLocalBuffer[2][:], 0, 4, pEnc, iEncOff, iEncStride) + iLambda2
	if iCurCost < iBestCost {
		iBestMode = 2
		iBestCost = iCurCost
	}

	WelsI4x4LumaPredH_c(uiLocalBuffer[1][:], 0, pDec, iDecOff, iDecStride)
	iCurCost = WelsSampleSatd4x4_c(uiLocalBuffer[1][:], 0, 4, pEnc, iEncOff, iEncStride) + iLambda1
	if iCurCost < iBestCost {
		iBestMode = 1
		iBestCost = iCurCost
	}
	WelsI4x4LumaPredV_c(uiLocalBuffer[0][:], 0, pDec, iDecOff, iDecStride)
	iCurCost = WelsSampleSatd4x4_c(uiLocalBuffer[0][:], 0, 4, pEnc, iEncOff, iEncStride) + iLambda0
	if iCurCost < iBestCost {
		iBestMode = 0
		iBestCost = iCurCost
	}

	copy(pDst[iDstOff:iDstOff+16], uiLocalBuffer[iBestMode][:])
	*pBestMode = iBestMode

	return iBestCost
}

func welsSampleIntra8x8Combined3(pfCost PSampleSadSatdCostFunc, pDecCb []uint8, iDecCbOff int, iDecStride int32, pEncCb []uint8, iEncCbOff int, iEncStride int32, pBestMode *int32, iLambda int32, pDstChroma []uint8, iDstChromaOff int, pDecCr []uint8, iDecCrOff int, pEncCr []uint8, iEncCrOff int) int32 {
	var iBestMode int32 = -1
	var iCurCost int32
	var iBestCost int32 = math.MaxInt32

	WelsIChromaPredV_c(pDstChroma, iDstChromaOff, pDecCb, iDecCbOff, iDecStride)
	WelsIChromaPredV_c(pDstChroma, iDstChromaOff+64, pDecCr, iDecCrOff, iDecStride)
	iCurCost = pfCost(pDstChroma, iDstChromaOff, 8, pEncCb, iEncCbOff, iEncStride)
	iCurCost += pfCost(pDstChroma, iDstChromaOff+64, 8, pEncCr, iEncCrOff, iEncStride) + iLambda*2

	if iCurCost < iBestCost {
		iBestMode = 2
		iBestCost = iCurCost
	}

	WelsIChromaPredH_c(pDstChroma, iDstChromaOff, pDecCb, iDecCbOff, iDecStride)
	WelsIChromaPredH_c(pDstChroma, iDstChromaOff+64, pDecCr, iDecCrOff, iDecStride)
	iCurCost = pfCost(pDstChroma, iDstChromaOff, 8, pEncCb, iEncCbOff, iEncStride)
	iCurCost += pfCost(pDstChroma, iDstChromaOff+64, 8, pEncCr, iEncCrOff, iEncStride) + iLambda*2
	if iCurCost < iBestCost {
		iBestMode = 1
		iBestCost = iCurCost
	}
	WelsIChromaPredDc_c(pDstChroma, iDstChromaOff, pDecCb, iDecCbOff, iDecStride)
	WelsIChromaPredDc_c(pDstChroma, iDstChromaOff+64, pDecCr, iDecCrOff, iDecStride)
	iCurCost = pfCost(pDstChroma, iDstChromaOff, 8, pEncCb, iEncCbOff, iEncStride)
	iCurCost += pfCost(pDstChroma, iDstChromaOff+64, 8, pEncCr, iEncCrOff, iEncStride)
	if iCurCost < iBestCost {
		iBestMode = 0
		iBestCost = iCurCost
	}

	*pBestMode = iBestMode

	return iBestCost
}

func WelsSampleSatdIntra8x8Combined3_c(pDecCb []uint8, iDecCbOff int, iDecStride int32, pEncCb []uint8, iEncCbOff int, iEncStride int32, pBestMode *int32, iLambda int32, pDstChroma []uint8, iDstChromaOff int, pDecCr []uint8, iDecCrOff int, pEncCr []uint8, iEncCrOff int) int32 {
	return welsSampleIntra8x8Combined3(WelsSampleSatd8x8_c, pDecCb, iDecCbOff, iDecStride, pEncCb, iEncCbOff, iEncStride,
		pBestMode, iLambda, pDstChroma, iDstChromaOff, pDecCr, iDecCrOff, pEncCr, iEncCrOff)
}

func WelsSampleSadIntra8x8Combined3_c(pDecCb []uint8, iDecCbOff int, iDecStride int32, pEncCb []uint8, iEncCbOff int, iEncStride int32, pBestMode *int32, iLambda int32, pDstChroma []uint8, iDstChromaOff int, pDecCr []uint8, iDecCrOff int, pEncCr []uint8, iEncCrOff int) int32 {
	return welsSampleIntra8x8Combined3(common.WelsSampleSad8x8_c, pDecCb, iDecCbOff, iDecStride, pEncCb, iEncCbOff, iEncStride,
		pBestMode, iLambda, pDstChroma, iDstChromaOff, pDecCr, iDecCrOff, pEncCr, iEncCrOff)
}

func welsSampleIntra16x16Combined3(pfCost PSampleSadSatdCostFunc, pDec []uint8, iDecOff int, iDecStride int32, pEnc []uint8, iEncOff int, iEncStride int32, pBestMode *int32, iLambda int32, pDst []uint8, iDstOff int) int32 {
	var iBestMode int32 = -1
	var iCurCost int32
	var iBestCost int32 = math.MaxInt32

	common.WelsI16x16LumaPredV_c(pDst, iDstOff, pDec, iDecOff, iDecStride)
	iCurCost = pfCost(pDst, iDstOff, 16, pEnc, iEncOff, iEncStride)

	if iCurCost < iBestCost {
		iBestMode = 0
		iBestCost = iCurCost
	}

	common.WelsI16x16LumaPredH_c(pDst, iDstOff, pDec, iDecOff, iDecStride)
	iCurCost = pfCost(pDst, iDstOff, 16, pEnc, iEncOff, iEncStride) + iLambda*2
	if iCurCost < iBestCost {
		iBestMode = 1
		iBestCost = iCurCost
	}
	WelsI16x16LumaPredDc_c(pDst, iDstOff, pDec, iDecOff, iDecStride)
	iCurCost = pfCost(pDst, iDstOff, 16, pEnc, iEncOff, iEncStride) + iLambda*2
	if iCurCost < iBestCost {
		iBestMode = 2
		iBestCost = iCurCost
	}

	*pBestMode = iBestMode

	return iBestCost
}

func WelsSampleSatdIntra16x16Combined3_c(pDec []uint8, iDecOff int, iDecStride int32, pEnc []uint8, iEncOff int, iEncStride int32, pBestMode *int32, iLambda int32, pDst []uint8, iDstOff int) int32 {
	return welsSampleIntra16x16Combined3(WelsSampleSatd16x16_c, pDec, iDecOff, iDecStride, pEnc, iEncOff, iEncStride,
		pBestMode, iLambda, pDst, iDstOff)
}

func WelsSampleSadIntra16x16Combined3_c(pDec []uint8, iDecOff int, iDecStride int32, pEnc []uint8, iEncOff int, iEncStride int32, pBestMode *int32, iLambda int32, pDst []uint8, iDstOff int) int32 {
	return welsSampleIntra16x16Combined3(common.WelsSampleSad16x16_c, pDec, iDecOff, iDecStride, pEnc, iEncOff, iEncStride,
		pBestMode, iLambda, pDst, iDstOff)
}

func WelsInitSampleSadFunc(pFuncList *SWelsFuncPtrList, uiCpuFlag uint32) {
	s := &pFuncList.sSampleDealingFuncs
	//pfSampleSad init
	s.pfSampleSad[BLOCK_16x16] = common.WelsSampleSad16x16_c
	s.pfSampleSad[BLOCK_16x8] = common.WelsSampleSad16x8_c
	s.pfSampleSad[BLOCK_8x16] = common.WelsSampleSad8x16_c
	s.pfSampleSad[BLOCK_8x8] = common.WelsSampleSad8x8_c
	s.pfSampleSad[BLOCK_4x4] = common.WelsSampleSad4x4_c
	s.pfSampleSad[BLOCK_8x4] = common.WelsSampleSad8x4_c
	s.pfSampleSad[BLOCK_4x8] = common.WelsSampleSad4x8_c

	//pfSampleSatd init
	s.pfSampleSatd[BLOCK_16x16] = WelsSampleSatd16x16_c
	s.pfSampleSatd[BLOCK_16x8] = WelsSampleSatd16x8_c
	s.pfSampleSatd[BLOCK_8x16] = WelsSampleSatd8x16_c
	s.pfSampleSatd[BLOCK_8x8] = WelsSampleSatd8x8_c
	s.pfSampleSatd[BLOCK_4x4] = WelsSampleSatd4x4_c
	s.pfSampleSatd[BLOCK_8x4] = WelsSampleSatd8x4_c
	s.pfSampleSatd[BLOCK_4x8] = WelsSampleSatd4x8_c

	s.pfSample4Sad[BLOCK_16x16] = common.WelsSampleSadFour16x16_c
	s.pfSample4Sad[BLOCK_16x8] = common.WelsSampleSadFour16x8_c
	s.pfSample4Sad[BLOCK_8x16] = common.WelsSampleSadFour8x16_c
	s.pfSample4Sad[BLOCK_8x8] = common.WelsSampleSadFour8x8_c
	s.pfSample4Sad[BLOCK_4x4] = common.WelsSampleSadFour4x4_c
	s.pfSample4Sad[BLOCK_8x4] = common.WelsSampleSadFour8x4_c
	s.pfSample4Sad[BLOCK_4x8] = common.WelsSampleSadFour4x8_c

	if uiCpuFlag&common.WELS_CPU_SSE2 != 0 {
		s.pfSampleSad[BLOCK_16x16] = common.WelsSampleSad16x16_sse2
		s.pfSampleSad[BLOCK_16x8] = common.WelsSampleSad16x8_sse2
		s.pfSampleSad[BLOCK_8x16] = common.WelsSampleSad8x16_sse2
		s.pfSampleSad[BLOCK_8x8] = common.WelsSampleSad8x8_sse2
		s.pfSampleSad[BLOCK_4x4] = common.WelsSampleSad4x4_sse2
		s.pfSampleSad[BLOCK_8x4] = common.WelsSampleSad8x4_sse2
		s.pfSampleSad[BLOCK_4x8] = common.WelsSampleSad4x8_sse2

		s.pfSample4Sad[BLOCK_16x16] = common.WelsSampleSadFour16x16_sse2
		s.pfSample4Sad[BLOCK_16x8] = common.WelsSampleSadFour16x8_sse2
		s.pfSample4Sad[BLOCK_8x16] = common.WelsSampleSadFour8x16_sse2
		s.pfSample4Sad[BLOCK_8x8] = common.WelsSampleSadFour8x8_sse2
		s.pfSample4Sad[BLOCK_4x4] = common.WelsSampleSadFour4x4_sse2
		s.pfSample4Sad[BLOCK_8x4] = common.WelsSampleSadFour8x4_sse2
		s.pfSample4Sad[BLOCK_4x8] = common.WelsSampleSadFour4x8_sse2
	}

	if uiCpuFlag&common.WELS_CPU_SSE41 != 0 && uiCpuFlag&common.WELS_CPU_SSSE3 != 0 {
		s.pfSampleSatd[BLOCK_16x16] = WelsSampleSatd16x16_sse41
		s.pfSampleSatd[BLOCK_16x8] = WelsSampleSatd16x8_sse41
		s.pfSampleSatd[BLOCK_8x16] = WelsSampleSatd8x16_sse41
		s.pfSampleSatd[BLOCK_8x8] = WelsSampleSatd8x8_sse41
		s.pfSampleSatd[BLOCK_4x4] = WelsSampleSatd4x4_sse41
		s.pfSampleSatd[BLOCK_8x4] = WelsSampleSatd8x4_sse41
		s.pfSampleSatd[BLOCK_4x8] = WelsSampleSatd4x8_sse41

		if uiCpuFlag&common.WELS_CPU_AVX2 != 0 {
			s.pfSampleSatd[BLOCK_16x16] = WelsSampleSatd16x16_avx2
			s.pfSampleSatd[BLOCK_16x8] = WelsSampleSatd16x8_avx2
			s.pfSampleSatd[BLOCK_8x16] = WelsSampleSatd8x16_avx2
			s.pfSampleSatd[BLOCK_8x8] = WelsSampleSatd8x8_avx2
		}
	}

	s.pfIntra4x4Combined3Satd = nil
	s.pfIntra8x8Combined3Satd = nil
	s.pfIntra8x8Combined3Sad = nil
	s.pfIntra16x16Combined3Satd = nil
	s.pfIntra16x16Combined3Sad = nil
}
