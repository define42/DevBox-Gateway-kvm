package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"math/bits"
)

// Frozen round8 complete I16 caller, retaining the separate quant/scan sequence.
func intraACOriginalCaller(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache) {
	var aDctT4Dc [16]int16
	pFuncList := pEncCtx.pFuncList
	pCurDqLayer := pEncCtx.pCurDqLayer
	kiEncStride := pCurDqLayer.iEncStride[0]
	pRes := pMbCache.pCoeffLevel
	pPred := pMbCache.SPicData.pCsMb[0]
	iPredOff := pMbCache.SPicData.iCsMbOff[0]
	kiRecStride := pCurDqLayer.iCsStride[0]
	pBlock := pMbCache.pDct.iLumaBlock[:]
	pBestPred := pMbCache.pMemPredLuma
	kpNoneZeroCountIdx := common.G_kuiMbCountScan4Idx[:]
	uiQp := pCurMb.uiLumaQp
	var uiNoneZeroCount, uiNoneZeroCountMbAc, uiCountI16x16Dc uint32

	pMF := g_kiQuantMF[uiQp][:]
	pFF := g_iQuantIntraFF[uiQp][:]

	WelsDctMb(pRes, pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0], kiEncStride, pBestPred, 0, pEncCtx.pFuncList.pfDctFourT4)

	WelsHadamardT4Dc_c(aDctT4Dc[:], pRes)
	WelsQuant4x4Dc_sse2(aDctT4Dc[:], int16(int32(pFF[0])<<1), pMF[0]>>1)
	dcMask := coefficientScanMask(pMbCache.pDct.iLumaI16x16Dc[:], aDctT4Dc[:], false)
	uiCountI16x16Dc = uint32(bits.OnesCount16(dcMask))
	var masks [16]uint16

	for i := 0; i < 4; i++ {
		r := pRes[i*64:]
		b := pBlock[i*64:]
		pFuncList.pfQuantizationFour4x4(r, pFF, pMF)
		masks[i*4] = coefficientScanMask(b, r, true)
		masks[i*4+1] = coefficientScanMask(b[16:], r[16:], true)
		masks[i*4+2] = coefficientScanMask(b[32:], r[32:], true)
		masks[i*4+3] = coefficientScanMask(b[48:], r[48:], true)
	}

	for i := 0; i < 16; i++ {
		uiNoneZeroCount = uint32(bits.OnesCount16(masks[i]))
		pCurMb.pNonZeroCount[kpNoneZeroCountIdx[i]] = int8(uiNoneZeroCount)
		uiNoneZeroCountMbAc += uiNoneZeroCount
	}

	if uiCountI16x16Dc > 0 {
		if uiQp < 12 {
			WelsIHadamard4x4Dc(aDctT4Dc[:])
			WelsDequantLumaDc4x4(aDctT4Dc[:], int32(uiQp))
		} else {
			WelsDequantIHadamard4x4_c(aDctT4Dc[:], common.G_kuiDequantCoeff[uiQp][0]>>2)
		}
	}

	if uiNoneZeroCountMbAc > 0 {
		pCurMb.uiCbp = 15
		pFuncList.pfDequantizationFour4x4(pRes, common.G_kuiDequantCoeff[uiQp][:])
		pFuncList.pfDequantizationFour4x4(pRes[64:], common.G_kuiDequantCoeff[uiQp][:])
		pFuncList.pfDequantizationFour4x4(pRes[128:], common.G_kuiDequantCoeff[uiQp][:])
		pFuncList.pfDequantizationFour4x4(pRes[192:], common.G_kuiDequantCoeff[uiQp][:])

		pRes[0] = aDctT4Dc[0]
		pRes[16] = aDctT4Dc[1]
		pRes[32] = aDctT4Dc[4]
		pRes[48] = aDctT4Dc[5]
		pRes[64] = aDctT4Dc[2]
		pRes[80] = aDctT4Dc[3]
		pRes[96] = aDctT4Dc[6]
		pRes[112] = aDctT4Dc[7]
		pRes[128] = aDctT4Dc[8]
		pRes[144] = aDctT4Dc[9]
		pRes[160] = aDctT4Dc[12]
		pRes[176] = aDctT4Dc[13]
		pRes[192] = aDctT4Dc[10]
		pRes[208] = aDctT4Dc[11]
		pRes[224] = aDctT4Dc[14]
		pRes[240] = aDctT4Dc[15]

		pFuncList.pfIDctFourT4(pPred, iPredOff, kiRecStride, pBestPred, 0, 16, pRes)
		pFuncList.pfIDctFourT4(pPred, iPredOff+8, kiRecStride, pBestPred, 8, 16, pRes[64:])
		pFuncList.pfIDctFourT4(pPred, iPredOff+int(kiRecStride)*8, kiRecStride, pBestPred, 128, 16, pRes[128:])
		pFuncList.pfIDctFourT4(pPred, iPredOff+int(kiRecStride)*8+8, kiRecStride, pBestPred, 136, 16, pRes[192:])
	} else if uiCountI16x16Dc > 0 {
		// A direct call keeps the local DC coefficients on the stack. The amd64
		// wrapper uses baseline SSE2; other architectures keep the scalar path.
		WelsIDctRecI16x16Dc_sse2(pPred, iPredOff, kiRecStride, pBestPred, 0, 16, aDctT4Dc[:])
	} else {
		pFuncList.pfCopy16x16Aligned(pPred, iPredOff, kiRecStride, pBestPred, 0, 16)
	}
}
