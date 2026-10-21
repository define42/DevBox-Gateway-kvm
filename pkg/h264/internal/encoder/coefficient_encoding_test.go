package encoder

import (
	"math"
	"math/rand"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Compare every output field against the pre-fusion scalar decisions, including
// residual dropping, skipped groups, separate chroma DC, and int16 wraparound.
func TestCoefficientEncodingStateParity(t *testing.T) {
	rng := rand.New(rand.NewSource(3317))
	var fast, reference SWelsFuncPtrList
	WelsInitEncodingFuncs(&fast, common.WELS_CPU_SSE2)
	WelsInitReconstructionFuncs(&fast, common.WELS_CPU_SSE2)
	WelsInitEncodingFuncs(&reference, 0)
	WelsInitReconstructionFuncs(&reference, 0)
	fast.pfSetMemZeroSize64, reference.pfSetMemZeroSize64 = WelsSetMemZero_c, WelsSetMemZero_c
	reference.pfGetNoneZeroCount = WelsGetNoneZeroCount_c
	for qp := 0; qp <= 51; qp++ {
		for fixture := 0; fixture < 24; fixture++ {
			var gotCoeff, wantCoeff [384]int16
			var gotNZ, wantNZ [24]int8
			var gotDCT, wantDCT SDCTCoeff
			amplitude := []int{1, 8, 32, 128, 512, 4096}[fixture%6]
			for i := range gotCoeff {
				if fixture != 0 && rng.Intn(4) < fixture%4 {
					gotCoeff[i] = int16(rng.Intn(amplitude*2+1) - amplitude)
				}
			}
			for i := range gotNZ {
				gotNZ[i], wantNZ[i] = 13, 13
			}
			for i := range gotDCT.iLumaBlock {
				gotDCT.iLumaBlock[i] = -1234
			}
			for i := range gotDCT.iChromaBlock {
				gotDCT.iChromaBlock[i] = 4321
			}
			wantCoeff, wantDCT = gotCoeff, gotDCT
			gotCache := SMbCache{pCoeffLevel: gotCoeff[:], pDct: &gotDCT}
			wantCache := SMbCache{pCoeffLevel: wantCoeff[:], pDct: &wantDCT}
			gotMB := SMB{uiMbType: common.MB_TYPE_16x16, uiLumaQp: uint8(qp), uiChromaQp: uint8(qp), pNonZeroCount: gotNZ[:]}
			wantMB := gotMB
			wantMB.pNonZeroCount = wantNZ[:]
			WelsEncInterY(&fast, &gotMB, &gotCache)
			coefficientInterYReference(&reference, &wantMB, &wantCache)
			if fixture%2 != 0 {
				gotMB.uiMbType, wantMB.uiMbType = common.MB_TYPE_INTRA16x16, common.MB_TYPE_INTRA16x16
			}
			for uv := int32(1); uv <= 2; uv++ {
				offset := 256 + (uv-1)*64
				WelsEncRecUV(&fast, &gotMB, &gotCache, gotCoeff[offset:], uv)
				coefficientUVReference(&reference, &wantMB, &wantCache, wantCoeff[offset:], uv)
			}
			if gotMB.uiCbp != wantMB.uiCbp || !slices.Equal(gotNZ[:], wantNZ[:]) || gotCoeff != wantCoeff || gotDCT != wantDCT {
				t.Fatalf("QP=%d fixture=%d encoded coefficient state differs: CBP=%#x want=%#x", qp, fixture, gotMB.uiCbp, wantMB.uiCbp)
			}
		}
	}
}

// Frozen pre-optimization reference, retaining independent scan/count/score.
func coefficientInterYReference(pFuncList *SWelsFuncPtrList, pCurMb *SMB, pMbCache *SMbCache) {
	pfSetMemZeroSize64 := pFuncList.pfSetMemZeroSize64
	pfScan4x4 := pFuncList.pfScan4x4
	pfCalculateSingleCtr4x4 := pFuncList.pfCalculateSingleCtr4x4
	pfGetNoneZeroCount := pFuncList.pfGetNoneZeroCount
	pfDequantizationFour4x4 := pFuncList.pfDequantizationFour4x4
	pRes := pMbCache.pCoeffLevel
	iSingleCtrMb := int32(0)
	var iSingleCtr8x8 [4]int32
	pBlock := pMbCache.pDct.iLumaBlock[:]
	uiQp := pCurMb.uiLumaQp
	pMF := g_kiQuantMF[uiQp][:]
	pFF := g_kiQuantInterFF[uiQp][:]
	var aMax [16]int16
	iNoneZeroCount := int32(0)

	iResOff := 0
	iBlockOff := 0
	for i := 0; i < 4; i++ {
		WelsQuantFour4x4Max_sse2(pRes[iResOff:], pFF, pMF, aMax[i<<2:])
		iSingleCtr8x8[i] = 0
		for j := 0; j < 4; j++ {
			if aMax[(i<<2)+j] == 0 {
				clear(pBlock[iBlockOff : iBlockOff+16])
			} else {
				pfScan4x4(pBlock[iBlockOff:], pRes[iResOff:])
				if aMax[(i<<2)+j] > 1 {
					iSingleCtr8x8[i] += 9
				} else if iSingleCtr8x8[i] < 6 {
					iSingleCtr8x8[i] += pfCalculateSingleCtr4x4(pBlock[iBlockOff:])
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
					iNoneZeroCount = pfGetNoneZeroCount(pBlock[iBlockOff:])
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

func coefficientUVReference(pFuncList *SWelsFuncPtrList, pCurMb *SMB, pMbCache *SMbCache, pRes []int16, iUV int32) {
	pfSetMemZeroSize64 := pFuncList.pfSetMemZeroSize64
	pfScan4x4Ac := pFuncList.pfScan4x4Ac
	pfCalculateSingleCtr4x4 := pFuncList.pfCalculateSingleCtr4x4
	pfGetNoneZeroCount := pFuncList.pfGetNoneZeroCount
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
	iSingleCtr8x8 := int32(0)
	pMF := g_kiQuantMF[kiQp][:]
	iIntraFlag := int32(0)
	if kiInterFlag == 0 {
		iIntraFlag = 1
	}
	pFF := g_kiQuantInterFF[iIntraFlag*6+int32(kiQp)][:]

	uiNoneZeroCountMbDc = uint8(WelsHadamardQuant2x2_c(pRes, int16(int32(pFF[0])<<1), pMF[0]>>1, aDct2x2[:], iChromaDc))

	WelsQuantFour4x4Max_sse2(pRes, pFF, pMF, aMax[:])

	iResOff := 0
	iBlockOff := 0
	for j := 0; j < 4; j++ {
		if aMax[j] == 0 {
			clear(pBlock[iBlockOff : iBlockOff+16])
		} else {
			pfScan4x4Ac(pBlock[iBlockOff:], pRes[iResOff:])
			if kiInterFlag != 0 {
				if aMax[j] > 1 {
					iSingleCtr8x8 += 9
				} else if iSingleCtr8x8 < 7 {
					iSingleCtr8x8 += pfCalculateSingleCtr4x4(pBlock[iBlockOff:])
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
			uiNoneZeroCount = uint8(pfGetNoneZeroCount(pBlock[iBlockOff:]))
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
