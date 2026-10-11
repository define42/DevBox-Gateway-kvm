// Port of codec/encoder/core/src/decode_mb_aux.cpp (C code only).

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

func WelsIHadamard4x4Dc(pRes []int16) { //pBuffer size : 4x4
	var iTemp [4]int16
	i := int32(4)

	for i--; i >= 0; i-- {
		kiIdx := i << 2
		kiIdx1 := 1 + kiIdx
		kiIdx2 := 1 + kiIdx1
		kiIdx3 := 1 + kiIdx2

		iTemp[0] = pRes[kiIdx] + pRes[kiIdx2]
		iTemp[1] = pRes[kiIdx] - pRes[kiIdx2]
		iTemp[2] = pRes[kiIdx1] - pRes[kiIdx3]
		iTemp[3] = pRes[kiIdx1] + pRes[kiIdx3]

		pRes[kiIdx] = iTemp[0] + iTemp[3]
		pRes[kiIdx1] = iTemp[1] + iTemp[2]
		pRes[kiIdx2] = iTemp[1] - iTemp[2]
		pRes[kiIdx3] = iTemp[0] - iTemp[3]
	}

	i = 4
	for i--; i >= 0; i-- {
		kiI4 := 4 + i
		kiI8 := 4 + kiI4
		kiI12 := 4 + kiI8

		iTemp[0] = pRes[i] + pRes[kiI8]
		iTemp[1] = pRes[i] - pRes[kiI8]
		iTemp[2] = pRes[kiI4] - pRes[kiI12]
		iTemp[3] = pRes[kiI4] + pRes[kiI12]

		pRes[i] = iTemp[0] + iTemp[3]
		pRes[kiI4] = iTemp[1] + iTemp[2]
		pRes[kiI8] = iTemp[1] - iTemp[2]
		pRes[kiI12] = iTemp[0] - iTemp[3]
	}
}

/* for qp < 12 */
func WelsDequantLumaDc4x4(pRes []int16, kiQp int32) {
	i := int32(15)
	kuiDequantValue := int32(common.G_kuiDequantCoeff[kiQp%6][0])
	kiQF0 := int16(kiQp / 6)
	kiQF1 := 2 - kiQF0
	kiQF0S := int32(int16(1 << uint(1-kiQF0)))

	for i >= 0 {
		pRes[i] = int16((int32(pRes[i])*kuiDequantValue + kiQF0S) >> uint(kiQF1))
		pRes[i-1] = int16((int32(pRes[i-1])*kuiDequantValue + kiQF0S) >> uint(kiQF1))
		pRes[i-2] = int16((int32(pRes[i-2])*kuiDequantValue + kiQF0S) >> uint(kiQF1))
		pRes[i-3] = int16((int32(pRes[i-3])*kuiDequantValue + kiQF0S) >> uint(kiQF1))

		i -= 4
	}
}

/* for qp >= 12 */
func WelsDequantIHadamard4x4_c(pRes []int16, kuiMF uint16) {
	var iTemp [4]int16
	kiMF := int32(kuiMF)

	for i := 0; i < 16; i += 4 {
		iTemp[0] = pRes[i] + pRes[i+2]
		iTemp[1] = pRes[i] - pRes[i+2]
		iTemp[2] = pRes[i+1] - pRes[i+3]
		iTemp[3] = pRes[i+1] + pRes[i+3]

		pRes[i] = iTemp[0] + iTemp[3]
		pRes[i+1] = iTemp[1] + iTemp[2]
		pRes[i+2] = iTemp[1] - iTemp[2]
		pRes[i+3] = iTemp[0] - iTemp[3]
	}

	for i := 0; i < 4; i++ {
		iTemp[0] = pRes[i] + pRes[i+8]
		iTemp[1] = pRes[i] - pRes[i+8]
		iTemp[2] = pRes[i+4] - pRes[i+12]
		iTemp[3] = pRes[i+4] + pRes[i+12]

		pRes[i] = int16((int32(iTemp[0]) + int32(iTemp[3])) * kiMF)
		pRes[i+4] = int16((int32(iTemp[1]) + int32(iTemp[2])) * kiMF)
		pRes[i+8] = int16((int32(iTemp[1]) - int32(iTemp[2])) * kiMF)
		pRes[i+12] = int16((int32(iTemp[0]) - int32(iTemp[3])) * kiMF)
	}
}

func WelsDequantIHadamard2x2Dc(pDct []int16, kuiMF uint16) {
	kiSumU := int32(pDct[0] + pDct[2])
	kiDelU := int32(pDct[0] - pDct[2])
	kiSumD := int32(pDct[1] + pDct[3])
	kiDelD := int32(pDct[1] - pDct[3])
	kiMF := int32(kuiMF)

	pDct[0] = int16(((kiSumU + kiSumD) * kiMF) >> 1)
	pDct[1] = int16(((kiSumU - kiSumD) * kiMF) >> 1)
	pDct[2] = int16(((kiDelU + kiDelD) * kiMF) >> 1)
	pDct[3] = int16(((kiDelU - kiDelD) * kiMF) >> 1)
}

func WelsDequant4x4_c(pRes []int16, kpMF []uint16) {
	for i := 0; i < 8; i++ {
		pRes[i] = int16(int32(pRes[i]) * int32(kpMF[i]))
		pRes[i+8] = int16(int32(pRes[i+8]) * int32(kpMF[i]))
	}
}

func WelsDequantFour4x4_c(pRes []int16, kpMF []uint16) {
	for i := 0; i < 8; i++ {
		kiMF := int32(kpMF[i])
		pRes[i] = int16(int32(pRes[i]) * kiMF)
		pRes[i+8] = int16(int32(pRes[i+8]) * kiMF)
		pRes[i+16] = int16(int32(pRes[i+16]) * kiMF)
		pRes[i+24] = int16(int32(pRes[i+24]) * kiMF)
		pRes[i+32] = int16(int32(pRes[i+32]) * kiMF)
		pRes[i+40] = int16(int32(pRes[i+40]) * kiMF)
		pRes[i+48] = int16(int32(pRes[i+48]) * kiMF)
		pRes[i+56] = int16(int32(pRes[i+56]) * kiMF)
	}
}

/****************************************************************************
 * IDCT functions, final output = prediction(CS) + IDCT(scaled_coeff)
 ****************************************************************************/
func WelsIDctT4Rec_c(pRec []uint8, iRecOff int, iStride int32, pPred []uint8, iPredOff int, iPredStride int32, pDct []int16) {
	var iTemp [16]int16

	iDstStride := int(iStride)
	iDstStridex2 := iDstStride << 1
	iDstStridex3 := iDstStride + iDstStridex2
	iPStride := int(iPredStride)
	iPredStridex2 := iPStride << 1
	iPredStridex3 := iPStride + iPredStridex2

	for i := 0; i < 4; i++ { //horizon
		iIdx := i << 2
		kiHorSumU := int32(pDct[iIdx]) + int32(pDct[iIdx+2])          // add 0-2
		kiHorDelU := int32(pDct[iIdx]) - int32(pDct[iIdx+2])          // sub 0-2
		kiHorSumD := int32(pDct[iIdx+1]) + (int32(pDct[iIdx+3]) >> 1) //
		kiHorDelD := (int32(pDct[iIdx+1]) >> 1) - int32(pDct[iIdx+3]) //

		iTemp[iIdx] = int16(kiHorSumU + kiHorSumD)
		iTemp[iIdx+1] = int16(kiHorDelU + kiHorDelD)
		iTemp[iIdx+2] = int16(kiHorDelU - kiHorDelD)
		iTemp[iIdx+3] = int16(kiHorSumU - kiHorSumD)
	}

	for i := 0; i < 4; i++ { //vertical
		kiVerSumL := int32(iTemp[i]) + int32(iTemp[8+i])
		kiVerDelL := int32(iTemp[i]) - int32(iTemp[8+i])
		kiVerDelR := (int32(iTemp[4+i]) >> 1) - int32(iTemp[12+i])
		kiVerSumR := int32(iTemp[4+i]) + (int32(iTemp[12+i]) >> 1)

		pRec[iRecOff+i] = common.WelsClip1(int32(pPred[iPredOff+i]) + ((kiVerSumL + kiVerSumR + 32) >> 6))
		pRec[iRecOff+iDstStride+i] = common.WelsClip1(int32(pPred[iPredOff+iPStride+i]) + ((kiVerDelL + kiVerDelR + 32) >> 6))
		pRec[iRecOff+iDstStridex2+i] = common.WelsClip1(int32(pPred[iPredOff+iPredStridex2+i]) + ((kiVerDelL - kiVerDelR + 32) >> 6))
		pRec[iRecOff+iDstStridex3+i] = common.WelsClip1(int32(pPred[iPredOff+iPredStridex3+i]) + ((kiVerSumL - kiVerSumR + 32) >> 6))
	}
}

func WelsIDctFourT4Rec_c(pRec []uint8, iRecOff int, iStride int32, pPred []uint8, iPredOff int, iPredStride int32, pDct []int16) {
	iDstStridex4 := int(iStride) << 2
	iPredStridex4 := int(iPredStride) << 2
	WelsIDctT4Rec_c(pRec, iRecOff, iStride, pPred, iPredOff, iPredStride, pDct)
	WelsIDctT4Rec_c(pRec, iRecOff+4, iStride, pPred, iPredOff+4, iPredStride, pDct[16:])
	WelsIDctT4Rec_c(pRec, iRecOff+iDstStridex4, iStride, pPred, iPredOff+iPredStridex4, iPredStride, pDct[32:])
	WelsIDctT4Rec_c(pRec, iRecOff+iDstStridex4+4, iStride, pPred, iPredOff+iPredStridex4+4, iPredStride, pDct[48:])
}

func WelsIDctT4RecOnMb(pDst []uint8, iDstOff int, iDstStride int32, pPred []uint8, iPredOff int, iPredStride int32, pDct []int16, pfIDctFourT4 PIDctFunc) {
	iDstStridex8 := int(iDstStride) << 3
	iPredStridex8 := int(iPredStride) << 3

	pfIDctFourT4(pDst, iDstOff, iDstStride, pPred, iPredOff, iPredStride, pDct)
	pfIDctFourT4(pDst, iDstOff+8, iDstStride, pPred, iPredOff+8, iPredStride, pDct[64:])
	pfIDctFourT4(pDst, iDstOff+iDstStridex8, iDstStride, pPred, iPredOff+iPredStridex8, iPredStride, pDct[128:])
	pfIDctFourT4(pDst, iDstOff+iDstStridex8+8, iDstStride, pPred, iPredOff+iPredStridex8+8, iPredStride, pDct[192:])
}

/*
 * pfIDctI16x16Dc: do luma idct of an MB for I16x16 mode, when only dc value are non-zero
 */
func WelsIDctRecI16x16Dc_c(pRec []uint8, iRecOff int, iStride int32, pPred []uint8, iPredOff int, iPredStride int32, pDctDc []int16) {
	for i := 0; i < 16; i++ {
		for j := 0; j < 16; j++ {
			pRec[iRecOff+j] = common.WelsClip1(int32(pPred[iPredOff+j]) + ((int32(pDctDc[(i&0x0C)+(j>>2)]) + 32) >> 6))
		}
		iRecOff += int(iStride)
		iPredOff += int(iPredStride)
	}
}

// pBlock: int32_t* (24 x 4 block offsets) -> []int32.
func WelsGetEncBlockStrideOffset(pBlock []int32, kiStrideY int32, kiStrideUV int32) {
	for j := int32(0); j < 4; j++ {
		i := j << 2
		k := (j & 0x01) << 1
		r := j & 0x02
		pBlock[i] = (0 + k + (0+r)*kiStrideY) << 2
		pBlock[i+1] = (1 + k + (0+r)*kiStrideY) << 2
		pBlock[i+2] = (0 + k + (1+r)*kiStrideY) << 2
		pBlock[i+3] = (1 + k + (1+r)*kiStrideY) << 2

		pBlock[16+j] = ((j & 0x01) + r*kiStrideUV) << 2
		pBlock[20+j] = pBlock[16+j]
	}
}

func WelsInitReconstructionFuncs(pFuncList *SWelsFuncPtrList, uiCpuFlag uint32) {
	pFuncList.pfDequantization4x4 = WelsDequant4x4_c
	pFuncList.pfDequantizationFour4x4 = WelsDequantFour4x4_c
	pFuncList.pfDequantizationIHadamard4x4 = WelsDequantIHadamard4x4_c

	pFuncList.pfIDctT4 = WelsIDctT4Rec_c
	pFuncList.pfIDctFourT4 = WelsIDctFourT4Rec_c
	pFuncList.pfIDctI16x16Dc = WelsIDctRecI16x16Dc_c
}
