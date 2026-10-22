// Port of codec/encoder/core/src/svc_encode_mb.cpp.

package encoder

import (
	"math"
	"math/bits"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func WelsDctMb(pRes []int16, pEncMb []uint8, iEncMbOff int, iEncStride int32, pBestPred []uint8, iBestPredOff int, pfDctFourT4 PDctFunc) {
	if dctLumaMbFast(pRes, pEncMb, iEncMbOff, iEncStride, pBestPred, iBestPredOff) {
		return
	}
	pfDctFourT4(pRes, pEncMb, iEncMbOff, iEncStride, pBestPred, iBestPredOff, 16)
	pfDctFourT4(pRes[64:], pEncMb, iEncMbOff+8, iEncStride, pBestPred, iBestPredOff+8, 16)
	pfDctFourT4(pRes[128:], pEncMb, iEncMbOff+8*int(iEncStride), iEncStride, pBestPred, iBestPredOff+128, 16)
	pfDctFourT4(pRes[192:], pEncMb, iEncMbOff+8*int(iEncStride)+8, iEncStride, pBestPred, iBestPredOff+136, 16)
}

// Direct architecture-selected calls in the hot paths below are intentional:
// indirect calls force local scratch onto the heap.
func WelsEncRecI16x16Y(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache) {
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

func WelsEncRecI4x4Y(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache, uiI4x4Idx uint8) {
	pFuncList := pEncCtx.pFuncList
	pCurDqLayer := pEncCtx.pCurDqLayer
	iEncStride := pCurDqLayer.iEncStride[0]
	uiQp := pCurMb.uiLumaQp

	pResI4x4 := pMbCache.pCoeffLevel

	pPred := pMbCache.SPicData.pCsMb[0]
	iPredOff := pMbCache.SPicData.iCsMbOff[0]
	iRecStride := pCurDqLayer.iCsStride[0]

	uiOffset := uint32(common.G_kuiMbCountScan4Idx[uiI4x4Idx])
	pEncMb := pMbCache.SPicData.pEncMb[0]
	iEncMbOff := pMbCache.SPicData.iEncMbOff[0]
	pBestPred := pMbCache.pBestPredI4x4Blk4
	pBlock := pMbCache.pDct.iLumaBlock[int(uiI4x4Idx)*16:]

	pMF := g_kiQuantMF[uiQp][:]
	pFF := g_iQuantIntraFF[uiQp][:]

	pStrideEncBlockOffset := pEncCtx.pStrideTab.pStrideEncBlockOffset[pEncCtx.uiDependencyId]
	iTid0 := 0
	if 0 == pEncCtx.uiTemporalId {
		iTid0 = 1
	}
	pStrideDecBlockOffset := pEncCtx.pStrideTab.pStrideDecBlockOffset[pEncCtx.uiDependencyId][iTid0]
	iNoneZeroCount := int32(0)

	pFuncList.pfDctT4(pResI4x4, pEncMb, iEncMbOff+int(pStrideEncBlockOffset[uiI4x4Idx]), iEncStride, pBestPred, 0, 4)
	pFuncList.pfQuantization4x4(pResI4x4, pFF, pMF)
	mask := coefficientScanMask(pBlock, pResI4x4, false)

	iNoneZeroCount = int32(bits.OnesCount16(mask))
	pCurMb.pNonZeroCount[uiOffset] = int8(iNoneZeroCount)

	iPredI4x4Off := iPredOff + int(pStrideDecBlockOffset[uiI4x4Idx])
	if iNoneZeroCount > 0 {
		pCurMb.uiCbp |= uint8(1 << (uiI4x4Idx >> 2))
		pFuncList.pfDequantization4x4(pResI4x4, common.G_kuiDequantCoeff[uiQp][:])
		pFuncList.pfIDctT4(pPred, iPredI4x4Off, iRecStride, pBestPred, 0, 4, pResI4x4)
	} else {
		pFuncList.pfCopy4x4(pPred, iPredI4x4Off, iRecStride, pBestPred, 0, 4)
	}
}

func WelsEncInterY(pFuncList *SWelsFuncPtrList, pCurMb *SMB, pMbCache *SMbCache) {
	pfSetMemZeroSize64 := pFuncList.pfSetMemZeroSize64
	pfDequantizationFour4x4 := pFuncList.pfDequantizationFour4x4
	pRes := pMbCache.pCoeffLevel
	iSingleCtrMb := int32(0)
	var iSingleCtr8x8 [4]int32
	pBlock := pMbCache.pDct.iLumaBlock[:]
	uiQp := pCurMb.uiLumaQp
	pMF := g_kiQuantMF[uiQp][:]
	pFF := g_kiQuantInterFF[uiQp][:]
	var aMax [16]int16
	var masks [16]uint16
	iNoneZeroCount := int32(0)

	quantScanLuma(pRes, pBlock, pFF, pMF, aMax[:], masks[:])

	iResOff := 0
	iBlockOff := 0
	for i := 0; i < 4; i++ {
		iSingleCtr8x8[i] = 0
		for j := 0; j < 4; j++ {
			if aMax[(i<<2)+j] != 0 {
				mask := masks[(i<<2)+j]
				if aMax[(i<<2)+j] > 1 {
					iSingleCtr8x8[i] += 9
				} else if iSingleCtr8x8[i] < 6 {
					iSingleCtr8x8[i] += coefficientScoreMask(mask)
				}
			}
			iResOff += 16
			iBlockOff += 16
		}
		iSingleCtrMb += iSingleCtr8x8[i]
	}
	iBlockOff -= 256
	iResOff -= 256

	clear(pCurMb.pNonZeroCount[:16])

	if iSingleCtrMb < 6 { //from JVT-O079
		pfSetMemZeroSize64(pRes[iResOff:], 768) // confirmed_safe_unsafe_usage
	} else {
		kpNoneZeroCountIdx := common.G_kuiMbCountScan4Idx[:]
		for i := 0; i < 4; i++ {
			if iSingleCtr8x8[i] >= 4 {
				for j := 0; j < 4; j++ {
					iNoneZeroCount = int32(bits.OnesCount16(masks[(i<<2)+j]))
					pCurMb.pNonZeroCount[kpNoneZeroCountIdx[0]] = int8(iNoneZeroCount)
					kpNoneZeroCountIdx = kpNoneZeroCountIdx[1:]
					iBlockOff += 16
				}
				pfDequantizationFour4x4(pRes[iResOff:], common.G_kuiDequantCoeff[uiQp][:])
				pCurMb.uiCbp |= uint8(1 << i)
			} else { // set zero for an 8x8 pBlock
				pfSetMemZeroSize64(pRes[iResOff:], 128) // confirmed_safe_unsafe_usage
				kpNoneZeroCountIdx = kpNoneZeroCountIdx[4:]
				iBlockOff += 64
			}
			iResOff += 64
		}
	}
}

func WelsEncRecUV(pFuncList *SWelsFuncPtrList, pCurMb *SMB, pMbCache *SMbCache, pRes []int16, iUV int32) {
	pfSetMemZeroSize64 := pFuncList.pfSetMemZeroSize64
	pfDequantizationFour4x4 := pFuncList.pfDequantizationFour4x4
	kiInterFlag := int32(0)
	if !common.IS_INTRA(pCurMb.uiMbType) {
		kiInterFlag = 1
	}
	kiQp := pCurMb.uiChromaQp
	var uiNoneZeroCount, uiNoneZeroCountMbDc uint8
	uiNoneZeroCountOffset := int((iUV - 1) << 1)   //UV==1 or 2
	uiSubMbIdx := int(16 + ((iUV - 1) << 2))       //uiSubMbIdx == 16 or 20
	iChromaDc := pMbCache.pDct.iChromaDc[iUV-1][:] //
	pBlock := pMbCache.pDct.iChromaBlock[((iUV-1)<<2)*16:]
	var aDct2x2, aMax [4]int16
	var masks [4]uint16
	iSingleCtr8x8 := int32(0)
	pMF := g_kiQuantMF[kiQp][:]
	iIntraFlag := int32(0)
	if kiInterFlag == 0 {
		iIntraFlag = 1
	}
	pFF := g_kiQuantInterFF[iIntraFlag*6+int32(kiQp)][:]

	uiNoneZeroCountMbDc = uint8(WelsHadamardQuant2x2_c(pRes, int16(int32(pFF[0])<<1), pMF[0]>>1, aDct2x2[:], iChromaDc))

	quantScanFour4x4(pRes, pBlock, pFF, pMF, aMax[:], masks[:], true)

	iResOff := 0
	iBlockOff := 0
	for j := 0; j < 4; j++ {
		if aMax[j] != 0 {
			mask := masks[j]
			if kiInterFlag != 0 {
				if aMax[j] > 1 {
					iSingleCtr8x8 += 9
				} else if iSingleCtr8x8 < 7 {
					iSingleCtr8x8 += coefficientScoreMask(mask)
				}
			} else {
				iSingleCtr8x8 = math.MaxInt32
			}
		}
		iResOff += 16
		iBlockOff += 16
	}
	iResOff -= 64

	if iSingleCtr8x8 < 7 { //from JVT-O079
		pfSetMemZeroSize64(pRes[iResOff:], 128) // confirmed_safe_unsafe_usage
		pCurMb.pNonZeroCount[16+uiNoneZeroCountOffset] = 0
		pCurMb.pNonZeroCount[16+uiNoneZeroCountOffset+1] = 0
		pCurMb.pNonZeroCount[20+uiNoneZeroCountOffset] = 0
		pCurMb.pNonZeroCount[20+uiNoneZeroCountOffset+1] = 0
	} else {
		kpNoneZeroCountIdx := common.G_kuiMbCountScan4Idx[uiSubMbIdx:]
		iBlockOff -= 64
		for i := 0; i < 4; i++ {
			uiNoneZeroCount = uint8(bits.OnesCount16(masks[i]))
			pCurMb.pNonZeroCount[kpNoneZeroCountIdx[i]] = int8(uiNoneZeroCount)
			iBlockOff += 16
		}
		pfDequantizationFour4x4(pRes[iResOff:], common.G_kuiDequantCoeff[pCurMb.uiChromaQp][:])
		pCurMb.uiCbp &= 0x0F
		pCurMb.uiCbp |= 0x20
	}

	if uiNoneZeroCountMbDc > 0 {
		WelsDequantIHadamard2x2Dc(aDct2x2[:], common.G_kuiDequantCoeff[kiQp][0])
		if 2 != (pCurMb.uiCbp >> 4) {
			pCurMb.uiCbp |= (0x01 << 4)
		}
		pRes[iResOff+0] = aDct2x2[0]
		pRes[iResOff+16] = aDct2x2[1]
		pRes[iResOff+32] = aDct2x2[2]
		pRes[iResOff+48] = aDct2x2[3]
	}
}

func WelsRecPskip(pCurLayer *SDqLayer, pFuncList *SWelsFuncPtrList, pCurMb *SMB, pMbCache *SMbCache) {
	iRecStride := pCurLayer.iCsStride[:]
	pCsMb := pMbCache.SPicData.pCsMb[:]
	iCsMbOff := pMbCache.SPicData.iCsMbOff[:]

	pFuncList.pfCopy16x16Aligned(pCsMb[0], iCsMbOff[0], iRecStride[0], pMbCache.pSkipMb, 0, 16)
	pFuncList.pfCopy8x8Aligned(pCsMb[1], iCsMbOff[1], iRecStride[1], pMbCache.pSkipMb, 256, 8)
	pFuncList.pfCopy8x8Aligned(pCsMb[2], iCsMbOff[2], iRecStride[2], pMbCache.pSkipMb, 320, 8)
	clear(pCurMb.pNonZeroCount[:24])
}

// WelsTryPYskip tests already-transformed luma coefficients.
func WelsTryPYskip(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache) bool {
	var score int32
	qp := pCurMb.uiLumaQp
	for group := 0; group < 4; group++ {
		offset := group * 64
		if !welsTryPYskipGroup(pMbCache.pCoeffLevel[offset:], pMbCache.pDct.iLumaBlock[offset:],
			g_kiQuantInterFF[qp][:], g_kiQuantMF[qp][:], &score) {
			return false
		}
	}
	return true
}

// Transform and test one 8x8 group at a time. A rejected skip attempt never
// consumes the remaining scratch coefficients; ordinary inter encoding
// recomputes its transforms from the winning prediction.
func welsTryPYskipPixels(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache) bool {
	var score int32
	qp := pCurMb.uiLumaQp
	stride := pEncCtx.pCurDqLayer.iEncStride[0]
	source := pMbCache.SPicData.pEncMb[0]
	sourceOffset := pMbCache.SPicData.iEncMbOff[0]
	for group := 0; group < 4; group++ {
		x, y := (group&1)*8, (group>>1)*8
		offset := group * 64
		pEncCtx.pFuncList.pfDctFourT4(pMbCache.pCoeffLevel[offset:], source,
			sourceOffset+y*int(stride)+x, stride, pMbCache.pSkipMb, y*16+x, 16)
		if !welsTryPYskipGroup(pMbCache.pCoeffLevel[offset:], pMbCache.pDct.iLumaBlock[offset:],
			g_kiQuantInterFF[qp][:], g_kiQuantMF[qp][:], &score) {
			return false
		}
	}
	return true
}

func welsTryPYskipGroup(residual, levels, rounding, multiplier []int16, score *int32) bool {
	var maximum [4]int16
	WelsQuantFour4x4Max_sse2(residual, rounding, multiplier, maximum[:])
	for block, value := range maximum {
		if uint16(value) > 1 {
			return false
		}
		if value == 1 {
			offset := block * 16
			mask := coefficientScanMask(levels[offset:], residual[offset:], false)
			*score += coefficientScoreMask(mask)
		}
		if *score >= 6 {
			return false
		}
	}
	return true
}

func WelsTryPUVskip(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache, iUV int32) bool {
	var pRes []int16
	if iUV == 1 {
		pRes = pMbCache.pCoeffLevel[256:]
	} else {
		pRes = pMbCache.pCoeffLevel[256+64:]
	}

	kuiQp := common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+
		int32(pEncCtx.pCurDqLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset))]

	pMF := g_kiQuantMF[kuiQp][:]
	pFF := g_kiQuantInterFF[kuiQp][:]

	if WelsHadamardQuant2x2Skip_c(pRes, int16(int32(pFF[0])<<1), pMF[0]>>1) != 0 {
		return false
	} else {
		var aMax [4]int16 // uint16_t aMax[4] in C, passed as (int16_t*)
		iSingleCtrMb := int32(0)
		pBlock := pMbCache.pDct.iChromaBlock[((iUV-1)<<2)*16:]
		WelsQuantFour4x4Max_sse2(pRes, pFF, pMF, aMax[:])

		iResOff := 0
		iBlockOff := 0
		for j := 0; j < 4; j++ {
			if uint16(aMax[j]) > 1 {
				return false // iSingleCtrMb += 9, can't be P_SKIP
			} else if uint16(aMax[j]) == 1 {
				pEncCtx.pFuncList.pfScan4x4Ac(pBlock[iBlockOff:], pRes[iResOff:])
				iSingleCtrMb += pEncCtx.pFuncList.pfCalculateSingleCtr4x4(pBlock[iBlockOff:])
			}
			if iSingleCtrMb >= 7 {
				return false //from JVT-O079
			}
			iResOff += 16
			iBlockOff += 16
		}
		return true
	}
}
