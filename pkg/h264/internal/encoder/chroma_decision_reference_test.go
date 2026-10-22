package encoder

// Frozen before the chroma decision simplification. Keep the original fused
// quantize/scan stage for exact alias and malformed-buffer write ordering.
import (
	"math"
	"math/bits"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func chromaDecisionFrozen(pFuncList *SWelsFuncPtrList, pCurMb *SMB, pMbCache *SMbCache, pRes []int16, iUV int32) {
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

	uiNoneZeroCountMbDc = uint8(hadamardQuant2x2(pRes, int16(int32(pFF[0])<<1), pMF[0]>>1, aDct2x2[:], iChromaDc))

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
