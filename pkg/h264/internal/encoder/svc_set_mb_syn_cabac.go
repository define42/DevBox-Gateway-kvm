// Port of codec/encoder/core/src/svc_set_mb_syn_cabac.cpp.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var uiSignificantCoeffFlagOffset = [5]uint16{0, 15, 29, 44, 47}
var uiLastCoeffFlagOffset = [5]uint16{0, 15, 29, 44, 47}
var uiCoeffAbsLevelMinus1Offset = [5]uint16{0, 10, 20, 30, 39}
var uiCodecBlockFlagOffset = [5]uint16{0, 4, 8, 12, 16}

func cabacBoolToUint32(b bool) uint32 {
	if b {
		return 1
	}
	return 0
}

func cabacBoolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func WelsCabacMbType(pCabacCtx *SCabacCtx, pCurMb *SMB, pMbCache *SMbCache, iMbWidth int32,
	eSliceType common.EWelsSliceType) {

	if eSliceType == common.I_SLICE {
		uiNeighborAvail := uint32(pCurMb.uiNeighborAvail)
		iCtx := int32(3)
		if (uiNeighborAvail&LEFT_MB_POS) != 0 && !common.IS_INTRA4x4(pCurMb.Add(-1).uiMbType) {
			iCtx++
		}
		if (uiNeighborAvail&TOP_MB_POS) != 0 && !common.IS_INTRA4x4(pCurMb.Add(-iMbWidth).uiMbType) { //TOP MB
			iCtx++
		}

		if pCurMb.uiMbType == common.MB_TYPE_INTRA4x4 {
			WelsCabacEncodeDecision(pCabacCtx, iCtx, 0)
		} else {
			iCbpChroma := int32(pCurMb.uiCbp >> 4)
			iCbpLuma := int32(pCurMb.uiCbp & 15)
			iPredMode := int32(g_kiMapModeI16x16[pMbCache.uiLumaI16x16Mode])

			WelsCabacEncodeDecision(pCabacCtx, iCtx, 1)
			WelsCabacEncodeTerminate(pCabacCtx, 0)
			if iCbpLuma != 0 {
				WelsCabacEncodeDecision(pCabacCtx, 6, 1)
			} else {
				WelsCabacEncodeDecision(pCabacCtx, 6, 0)
			}

			if iCbpChroma == 0 {
				WelsCabacEncodeDecision(pCabacCtx, 7, 0)
			} else {
				WelsCabacEncodeDecision(pCabacCtx, 7, 1)
				WelsCabacEncodeDecision(pCabacCtx, 8, uint32(iCbpChroma>>1))
			}
			WelsCabacEncodeDecision(pCabacCtx, 9, uint32(iPredMode>>1))
			WelsCabacEncodeDecision(pCabacCtx, 10, uint32(iPredMode&1))
		}
	} else if eSliceType == common.P_SLICE {
		uiMbType := pCurMb.uiMbType
		if uiMbType == common.MB_TYPE_16x16 {
			WelsCabacEncodeDecision(pCabacCtx, 14, 0)
			WelsCabacEncodeDecision(pCabacCtx, 15, 0)
			WelsCabacEncodeDecision(pCabacCtx, 16, 0)
		} else if (uiMbType == common.MB_TYPE_16x8) || (uiMbType == common.MB_TYPE_8x16) {

			WelsCabacEncodeDecision(pCabacCtx, 14, 0)
			WelsCabacEncodeDecision(pCabacCtx, 15, 1)
			WelsCabacEncodeDecision(pCabacCtx, 17, cabacBoolToUint32(pCurMb.uiMbType == common.MB_TYPE_16x8))

		} else if (uiMbType == common.MB_TYPE_8x8) || (uiMbType == common.MB_TYPE_8x8_REF0) {
			WelsCabacEncodeDecision(pCabacCtx, 14, 0)
			WelsCabacEncodeDecision(pCabacCtx, 15, 0)
			WelsCabacEncodeDecision(pCabacCtx, 16, 1)
		} else if pCurMb.uiMbType == common.MB_TYPE_INTRA4x4 {
			WelsCabacEncodeDecision(pCabacCtx, 14, 1)
			WelsCabacEncodeDecision(pCabacCtx, 17, 0)
		} else {

			iCbpChroma := int32(pCurMb.uiCbp >> 4)
			iCbpLuma := int32(pCurMb.uiCbp & 15)
			iPredMode := int32(g_kiMapModeI16x16[pMbCache.uiLumaI16x16Mode])
			//prefix
			WelsCabacEncodeDecision(pCabacCtx, 14, 1)

			//suffix
			WelsCabacEncodeDecision(pCabacCtx, 17, 1)
			WelsCabacEncodeTerminate(pCabacCtx, 0)
			if iCbpLuma != 0 {
				WelsCabacEncodeDecision(pCabacCtx, 18, 1)
			} else {
				WelsCabacEncodeDecision(pCabacCtx, 18, 0)
			}
			if iCbpChroma == 0 {
				WelsCabacEncodeDecision(pCabacCtx, 19, 0)
			} else {
				WelsCabacEncodeDecision(pCabacCtx, 19, 1)
				WelsCabacEncodeDecision(pCabacCtx, 19, uint32(iCbpChroma>>1))
			}
			WelsCabacEncodeDecision(pCabacCtx, 20, uint32(iPredMode>>1))
			WelsCabacEncodeDecision(pCabacCtx, 20, uint32(iPredMode&1))

		}
	}

}

func WelsCabacMbIntra4x4PredMode(pCabacCtx *SCabacCtx, pMbCache *SMbCache) {

	for iMode := 0; iMode < 16; iMode++ {

		bPredFlag := pMbCache.pPrevIntra4x4PredModeFlag[iMode]
		iRemMode := int32(pMbCache.pRemIntra4x4PredModeFlag[iMode])

		if bPredFlag {
			WelsCabacEncodeDecision(pCabacCtx, 68, 1)
		} else {
			WelsCabacEncodeDecision(pCabacCtx, 68, 0)

			WelsCabacEncodeDecision(pCabacCtx, 69, uint32(iRemMode&0x01))
			WelsCabacEncodeDecision(pCabacCtx, 69, uint32((iRemMode>>1)&0x01))
			WelsCabacEncodeDecision(pCabacCtx, 69, uint32(iRemMode>>2))
		}
	}
}

func WelsCabacMbIntraChromaPredMode(pCabacCtx *SCabacCtx, pCurMb *SMB, pMbCache *SMbCache, iMbWidth int32) {
	uiNeighborAvail := uint32(pCurMb.uiNeighborAvail)

	iPredMode := int32(g_kiMapModeIntraChroma[pMbCache.uiChmaI8x8Mode])
	iCtx := int32(64)
	if (uiNeighborAvail&LEFT_MB_POS) != 0 && g_kiMapModeIntraChroma[pCurMb.Add(-1).uiChromPredMode] != 0 {
		iCtx++
	}
	if (uiNeighborAvail&TOP_MB_POS) != 0 && g_kiMapModeIntraChroma[pCurMb.Add(-iMbWidth).uiChromPredMode] != 0 {
		iCtx++
	}

	if iPredMode == 0 {
		WelsCabacEncodeDecision(pCabacCtx, iCtx, 0)
	} else if iPredMode == 1 {
		WelsCabacEncodeDecision(pCabacCtx, iCtx, 1)
		WelsCabacEncodeDecision(pCabacCtx, 67, 0)
	} else if iPredMode == 2 {
		WelsCabacEncodeDecision(pCabacCtx, iCtx, 1)
		WelsCabacEncodeDecision(pCabacCtx, 67, 1)
		WelsCabacEncodeDecision(pCabacCtx, 67, 0)
	} else {
		WelsCabacEncodeDecision(pCabacCtx, iCtx, 1)
		WelsCabacEncodeDecision(pCabacCtx, 67, 1)
		WelsCabacEncodeDecision(pCabacCtx, 67, 1)
	}
}

func WelsCabacMbCbp(pCurMb *SMB, iMbWidth int32, pCabacCtx *SCabacCtx) {
	uiCbp := int32(pCurMb.uiCbp)
	iCbpBlockLuma := [4]int32{(uiCbp) & 1, (uiCbp >> 1) & 1, (uiCbp >> 2) & 1, (uiCbp >> 3) & 1}
	iCbpChroma := uiCbp >> 4
	iCbpBlockLeft := [4]int32{0, 0, 0, 0}
	iCbpBlockTop := [4]int32{0, 0, 0, 0}
	iCbpLeftChroma := int32(0)
	iCbpTopChroma := int32(0)
	iCbp := int32(0)
	iCtx := int32(0)
	uiNeighborAvail := uint32(pCurMb.uiNeighborAvail)
	if uiNeighborAvail&LEFT_MB_POS != 0 {
		iCbp = int32(pCurMb.Add(-1).uiCbp)
		iCbpBlockLeft[0] = cabacBoolToInt32((iCbp & 1) == 0)
		iCbpBlockLeft[1] = cabacBoolToInt32(((iCbp >> 1) & 1) == 0)
		iCbpBlockLeft[2] = cabacBoolToInt32(((iCbp >> 2) & 1) == 0)
		iCbpBlockLeft[3] = cabacBoolToInt32(((iCbp >> 3) & 1) == 0)
		iCbpLeftChroma = iCbp >> 4
		if iCbpLeftChroma != 0 {
			iCtx += 1
		}
	}
	if uiNeighborAvail&TOP_MB_POS != 0 {
		iCbp = int32(pCurMb.Add(-iMbWidth).uiCbp)
		iCbpBlockTop[0] = cabacBoolToInt32((iCbp & 1) == 0)
		iCbpBlockTop[1] = cabacBoolToInt32(((iCbp >> 1) & 1) == 0)
		iCbpBlockTop[2] = cabacBoolToInt32(((iCbp >> 2) & 1) == 0)
		iCbpBlockTop[3] = cabacBoolToInt32(((iCbp >> 3) & 1) == 0)
		iCbpTopChroma = iCbp >> 4
		if iCbpTopChroma != 0 {
			iCtx += 2
		}
	}
	WelsCabacEncodeDecision(pCabacCtx, 73+iCbpBlockLeft[1]+iCbpBlockTop[2]*2, uint32(iCbpBlockLuma[0]))
	WelsCabacEncodeDecision(pCabacCtx, 73+cabacBoolToInt32(iCbpBlockLuma[0] == 0)+iCbpBlockTop[3]*2, uint32(iCbpBlockLuma[1]))
	WelsCabacEncodeDecision(pCabacCtx, 73+iCbpBlockLeft[3]+cabacBoolToInt32(iCbpBlockLuma[0] == 0)*2, uint32(iCbpBlockLuma[2]))
	WelsCabacEncodeDecision(pCabacCtx, 73+cabacBoolToInt32(iCbpBlockLuma[2] == 0)+cabacBoolToInt32(iCbpBlockLuma[1] == 0)*2, uint32(iCbpBlockLuma[3]))

	//chroma
	if iCbpChroma != 0 {
		WelsCabacEncodeDecision(pCabacCtx, 77+iCtx, 1)
		WelsCabacEncodeDecision(pCabacCtx, 81+(iCbpLeftChroma>>1)+((iCbpTopChroma>>1)*2), cabacBoolToUint32(iCbpChroma > 1))
	} else {
		WelsCabacEncodeDecision(pCabacCtx, 77+iCtx, 0)
	}
}

func WelsCabacMbDeltaQp(pCurMb *SMB, pCabacCtx *SCabacCtx, bFirstMbInSlice bool) {
	var pPrevMb *SMB
	iCtx := int32(0)

	if !bFirstMbInSlice {
		pPrevMb = pCurMb.Add(-1)
		pCurMb.iLumaDQp = int32(pCurMb.uiLumaQp) - int32(pPrevMb.uiLumaQp)

		if common.IS_SKIP(pPrevMb.uiMbType) || ((pPrevMb.uiMbType != common.MB_TYPE_INTRA16x16) && (pPrevMb.uiCbp == 0)) ||
			(pPrevMb.iLumaDQp == 0) {
			iCtx = 0
		} else {
			iCtx = 1
		}
	}

	if pCurMb.iLumaDQp != 0 {
		var iValue int32
		if pCurMb.iLumaDQp < 0 {
			iValue = -2 * pCurMb.iLumaDQp
		} else {
			iValue = 2*pCurMb.iLumaDQp - 1
		}
		WelsCabacEncodeDecision(pCabacCtx, 60+iCtx, 1)
		if iValue == 1 {
			WelsCabacEncodeDecision(pCabacCtx, 60+2, 0)
		} else {
			WelsCabacEncodeDecision(pCabacCtx, 60+2, 1)
			iValue--
			for iValue--; iValue > 0; iValue-- {
				WelsCabacEncodeDecision(pCabacCtx, 60+3, 1)
			}
			WelsCabacEncodeDecision(pCabacCtx, 60+3, 0)
		}
	} else {
		WelsCabacEncodeDecision(pCabacCtx, 60+iCtx, 0)
	}
}

func WelsMbSkipCabac(pCabacCtx *SCabacCtx, pCurMb *SMB, iMbWidth int32, eSliceType common.EWelsSliceType, bSkipFlag int16) {
	iCtx := int32(24)
	if eSliceType == common.P_SLICE {
		iCtx = 11
	}
	uiNeighborAvail := uint32(pCurMb.uiNeighborAvail)
	if uiNeighborAvail&LEFT_MB_POS != 0 { //LEFT MB
		if !common.IS_SKIP(pCurMb.Add(-1).uiMbType) {
			iCtx++
		}
	}
	if uiNeighborAvail&TOP_MB_POS != 0 { //TOP MB
		if !common.IS_SKIP(pCurMb.Add(-iMbWidth).uiMbType) {
			iCtx++
		}
	}
	WelsCabacEncodeDecision(pCabacCtx, iCtx, uint32(bSkipFlag))

	if bSkipFlag != 0 {
		for i := 0; i < 16; i++ {
			pCurMb.sMvd[i].iMvX = 0
			pCurMb.sMvd[i].iMvY = 0
		}
		pCurMb.iCbpDc = 0
		pCurMb.uiCbp = 0
	}
}

func WelsCabacMbRef(pCabacCtx *SCabacCtx, pCurMb *SMB, pMbCache *SMbCache, iIdx int16) {
	pMvComp := &pMbCache.sMvComponents
	iRefIdxA := int16(pMvComp.iRefIndexCache[iIdx+6])
	iRefIdxB := int16(pMvComp.iRefIndexCache[iIdx+1])
	iRefIdx := int16(pMvComp.iRefIndexCache[iIdx+7])
	iCtx := int16(0)

	if (iRefIdxA > 0) && !pMbCache.bMbTypeSkip[3] {
		iCtx++
	}
	if (iRefIdxB > 0) && !pMbCache.bMbTypeSkip[1] {
		iCtx += 2
	}

	for iRefIdx > 0 {
		WelsCabacEncodeDecision(pCabacCtx, 54+int32(iCtx), 1)
		iCtx = (iCtx >> 2) + 4
		iRefIdx--
	}
	WelsCabacEncodeDecision(pCabacCtx, 54+int32(iCtx), 0)
}

// (inline in C)
func WelsCabacMbMvdLx(pCabacCtx *SCabacCtx, sMvd int32, iCtx int32, iPredMvd int32) {
	iAbsMvd := common.WELS_ABS(sMvd)
	iCtxInc := int32(0)
	iPrefix := common.WELS_MIN(iAbsMvd, 9)
	i := int32(0)

	if iPredMvd > 32 {
		iCtxInc += 2
	} else if iPredMvd > 2 {
		iCtxInc += 1
	}

	if iPrefix != 0 {
		if iPrefix < 9 {
			WelsCabacEncodeDecision(pCabacCtx, iCtx+iCtxInc, 1)
			iCtxInc = 3
			for i = 0; i < iPrefix-1; i++ {
				WelsCabacEncodeDecision(pCabacCtx, iCtx+iCtxInc, 1)
				if i < 3 {
					iCtxInc++
				}
			}
			WelsCabacEncodeDecision(pCabacCtx, iCtx+iCtxInc, 0)
			WelsCabacEncodeBypassOne(pCabacCtx, cabacBoolToInt32(sMvd < 0))
		} else {
			WelsCabacEncodeDecision(pCabacCtx, iCtx+iCtxInc, 1)
			iCtxInc = 3
			for i = 0; i < (9 - 1); i++ {
				WelsCabacEncodeDecision(pCabacCtx, iCtx+iCtxInc, 1)
				if i < 3 {
					iCtxInc++
				}
			}
			WelsCabacEncodeUeBypass(pCabacCtx, 3, uint32(iAbsMvd-9))
			WelsCabacEncodeBypassOne(pCabacCtx, cabacBoolToInt32(sMvd < 0))
		}
	} else {
		WelsCabacEncodeDecision(pCabacCtx, iCtx+iCtxInc, 0)
	}
}

func WelsCabacMbMvd(pCabacCtx *SCabacCtx, pCurMb *SMB, iMbWidth uint32, sCurMv SMVUnitXY, sPredMv SMVUnitXY, i4x4ScanIdx int16) SMVUnitXY {
	var iAbsMvd0, iAbsMvd1 uint32
	uiNeighborAvail := pCurMb.uiNeighborAvail
	var sMvd SMVUnitXY
	var sMvdLeft SMVUnitXY
	var sMvdTop SMVUnitXY

	sMvd.sDeltaMv(sCurMv, sPredMv)
	if (i4x4ScanIdx < 4) && (uiNeighborAvail&TOP_MB_POS) != 0 { //top row blocks
		sMvdTop.sAssignMv(pCurMb.Add(-int32(iMbWidth)).sMvd[i4x4ScanIdx+12])
	} else if i4x4ScanIdx >= 4 {
		sMvdTop.sAssignMv(pCurMb.sMvd[i4x4ScanIdx-4])
	}
	if ((i4x4ScanIdx & 0x03) == 0) && (uiNeighborAvail&LEFT_MB_POS) != 0 { //left column blocks
		sMvdLeft.sAssignMv(pCurMb.Add(-1).sMvd[i4x4ScanIdx+3])
	} else if (i4x4ScanIdx & 0x03) != 0 {
		sMvdLeft.sAssignMv(pCurMb.sMvd[i4x4ScanIdx-1])
	}

	iAbsMvd0 = uint32(common.WELS_ABS(int32(sMvdLeft.iMvX)) + common.WELS_ABS(int32(sMvdTop.iMvX)))
	iAbsMvd1 = uint32(common.WELS_ABS(int32(sMvdLeft.iMvY)) + common.WELS_ABS(int32(sMvdTop.iMvY)))

	WelsCabacMbMvdLx(pCabacCtx, int32(sMvd.iMvX), 40, int32(iAbsMvd0))
	WelsCabacMbMvdLx(pCabacCtx, int32(sMvd.iMvY), 47, int32(iAbsMvd1))
	return sMvd
}

func welsCabacSubMbType(pCabacCtx *SCabacCtx, pCurMb *SMB) {
	for i8x8Idx := 0; i8x8Idx < 4; i8x8Idx++ {
		uiSubMbType := uint32(pCurMb.uiSubMbType[i8x8Idx])
		if common.SUB_MB_TYPE_8x8 == uiSubMbType {
			WelsCabacEncodeDecision(pCabacCtx, 21, 1)
			continue
		}
		WelsCabacEncodeDecision(pCabacCtx, 21, 0)
		if common.SUB_MB_TYPE_8x4 == uiSubMbType {
			WelsCabacEncodeDecision(pCabacCtx, 22, 0)
		} else {
			WelsCabacEncodeDecision(pCabacCtx, 22, 1)
			WelsCabacEncodeDecision(pCabacCtx, 23, cabacBoolToUint32(common.SUB_MB_TYPE_4x8 == uiSubMbType))
		}
	} //for
}

func welsCabacSubMbMvd(pCabacCtx *SCabacCtx, pCurMb *SMB, pMbCache *SMbCache, kiMbWidth int32) {
	var sMvd SMVUnitXY
	var i4x4ScanIdx int32
	for i8x8Idx := int32(0); i8x8Idx < 4; i8x8Idx++ {
		uiSubMbType := uint32(pCurMb.uiSubMbType[i8x8Idx])
		if common.SUB_MB_TYPE_8x8 == uiSubMbType {
			i4x4ScanIdx = int32(common.G_kuiMbCountScan4Idx[i8x8Idx<<2])
			sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(kiMbWidth), pCurMb.sMv[i4x4ScanIdx], pMbCache.sMbMvp[i4x4ScanIdx],
				int16(i4x4ScanIdx))
			pCurMb.sMvd[i4x4ScanIdx].sAssignMv(sMvd)
			pCurMb.sMvd[1+i4x4ScanIdx].sAssignMv(sMvd)
			pCurMb.sMvd[4+i4x4ScanIdx].sAssignMv(sMvd)
			pCurMb.sMvd[5+i4x4ScanIdx].sAssignMv(sMvd)
		} else if common.SUB_MB_TYPE_4x4 == uiSubMbType {
			for i4x4Idx := int32(0); i4x4Idx < 4; i4x4Idx++ {
				i4x4ScanIdx = int32(common.G_kuiMbCountScan4Idx[(i8x8Idx<<2)+i4x4Idx])
				sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(kiMbWidth), pCurMb.sMv[i4x4ScanIdx], pMbCache.sMbMvp[i4x4ScanIdx],
					int16(i4x4ScanIdx))
				pCurMb.sMvd[i4x4ScanIdx].sAssignMv(sMvd)
			}
		} else if common.SUB_MB_TYPE_8x4 == uiSubMbType {
			for i8x4Idx := int32(0); i8x4Idx < 2; i8x4Idx++ {
				i4x4ScanIdx = int32(common.G_kuiMbCountScan4Idx[(i8x8Idx<<2)+(i8x4Idx<<1)])
				sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(kiMbWidth), pCurMb.sMv[i4x4ScanIdx], pMbCache.sMbMvp[i4x4ScanIdx],
					int16(i4x4ScanIdx))
				pCurMb.sMvd[i4x4ScanIdx].sAssignMv(sMvd)
				pCurMb.sMvd[1+i4x4ScanIdx].sAssignMv(sMvd)
			}
		} else if common.SUB_MB_TYPE_4x8 == uiSubMbType {
			for i4x8Idx := int32(0); i4x8Idx < 2; i4x8Idx++ {
				i4x4ScanIdx = int32(common.G_kuiMbCountScan4Idx[(i8x8Idx<<2)+i4x8Idx])
				sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(kiMbWidth), pCurMb.sMv[i4x4ScanIdx], pMbCache.sMbMvp[i4x4ScanIdx],
					int16(i4x4ScanIdx))
				pCurMb.sMvd[i4x4ScanIdx].sAssignMv(sMvd)
				pCurMb.sMvd[4+i4x4ScanIdx].sAssignMv(sMvd)
			}
		}
	}
}

func WelsGetMbCtxCabac(pMbCache *SMbCache, pCurMb *SMB, iMbWidth uint32, eCtxBlockCat ECtxBlockCat, iIdx int16) int16 {
	iNzA, iNzB := int16(-1), int16(-1)
	pNonZeroCoeffCount := pMbCache.iNonZeroCoeffCount[:]
	bIntra := common.IS_INTRA(pCurMb.uiMbType)
	iCtxInc := int32(0)
	switch eCtxBlockCat {
	case LUMA_AC, CHROMA_AC, LUMA_4x4:
		iNzA = int16(pNonZeroCoeffCount[iIdx-1])
		iNzB = int16(pNonZeroCoeffCount[iIdx-8])
	case LUMA_DC, CHROMA_DC:
		if uint32(pCurMb.uiNeighborAvail)&LEFT_MB_POS != 0 {
			iNzA = int16(pCurMb.Add(-1).iCbpDc & (1 << uint(iIdx)))
		}
		if uint32(pCurMb.uiNeighborAvail)&TOP_MB_POS != 0 {
			iNzB = int16(pCurMb.Add(-int32(iMbWidth)).iCbpDc & (1 << uint(iIdx)))
		}
	default:
	}
	if ((iNzA == -1) && bIntra) || (iNzA > 0) {
		iCtxInc += 1
	}
	if ((iNzB == -1) && bIntra) || (iNzB > 0) {
		iCtxInc += 2
	}
	return int16(85 + int32(uiCodecBlockFlagOffset[eCtxBlockCat]) + iCtxInc)
}

func WelsWriteBlockResidualCabac(pMbCache *SMbCache, pCurMb *SMB, iMbWidth uint32, pCabacCtx *SCabacCtx, eCtxBlockCat ECtxBlockCat, iIdx int16, iNonZeroCount int16, pBlock []int16, iEndIdx int16) {
	iCtx := int32(WelsGetMbCtxCabac(pMbCache, pCurMb, iMbWidth, eCtxBlockCat, iIdx))
	if iNonZeroCount != 0 {
		var iLevel [16]int16
		iCtxSig := 105 + int32(uiSignificantCoeffFlagOffset[eCtxBlockCat])
		iCtxLast := 166 + int32(uiLastCoeffFlagOffset[eCtxBlockCat])
		iCtxLevel := 227 + int32(uiCoeffAbsLevelMinus1Offset[eCtxBlockCat])
		iNonZeroIdx := int32(0)
		i := int32(0)

		WelsCabacEncodeDecision(pCabacCtx, iCtx, 1)
		for {
			if pBlock[i] != 0 {
				iLevel[iNonZeroIdx] = pBlock[i]

				iNonZeroIdx++
				WelsCabacEncodeDecision(pCabacCtx, iCtxSig+i, 1)
				if iNonZeroIdx != int32(iNonZeroCount) {
					WelsCabacEncodeDecision(pCabacCtx, iCtxLast+i, 0)
				} else {
					WelsCabacEncodeDecision(pCabacCtx, iCtxLast+i, 1)
					break
				}
			} else {
				WelsCabacEncodeDecision(pCabacCtx, iCtxSig+i, 0)
			}
			i++
			if i == int32(iEndIdx) {
				iLevel[iNonZeroIdx] = pBlock[i]
				iNonZeroIdx++
				break
			}
		}

		iNumAbsLevelGt1 := int32(0)
		iCtx1 := iCtxLevel + 1

		for {
			iPrefix := int32(0)
			iNonZeroIdx--
			iPrefix = common.WELS_ABS(int32(iLevel[iNonZeroIdx])) - 1
			if iPrefix != 0 {
				iPrefix = common.WELS_MIN(iPrefix, 14)
				iCtx = common.WELS_MIN(iCtxLevel+4, iCtx1)
				WelsCabacEncodeDecision(pCabacCtx, iCtx, 1)
				iNumAbsLevelGt1++
				iCtx = iCtxLevel + 4 + common.WELS_MIN(5-cabacBoolToInt32(eCtxBlockCat == CHROMA_DC), iNumAbsLevelGt1)
				for i = 1; i < iPrefix; i++ {
					WelsCabacEncodeDecision(pCabacCtx, iCtx, 1)
				}
				if common.WELS_ABS(int32(iLevel[iNonZeroIdx])) < 15 {
					WelsCabacEncodeDecision(pCabacCtx, iCtx, 0)
				} else {
					WelsCabacEncodeUeBypass(pCabacCtx, 0, uint32(common.WELS_ABS(int32(iLevel[iNonZeroIdx]))-15))
				}
				iCtx1 = iCtxLevel
			} else {
				iCtx = common.WELS_MIN(iCtxLevel+4, iCtx1)
				WelsCabacEncodeDecision(pCabacCtx, iCtx, 0)
				iCtx1 += cabacBoolToInt32(iNumAbsLevelGt1 == 0)
			}
			WelsCabacEncodeBypassOne(pCabacCtx, cabacBoolToInt32(iLevel[iNonZeroIdx] < 0))
			if !(iNonZeroIdx > 0) {
				break
			}
		}

	} else {
		WelsCabacEncodeDecision(pCabacCtx, iCtx, 0)
	}

}

func WelsCalNonZeroCount2x2Block(pBlock []int16) int32 {
	return cabacBoolToInt32(pBlock[0] != 0) +
		cabacBoolToInt32(pBlock[1] != 0) +
		cabacBoolToInt32(pBlock[2] != 0) +
		cabacBoolToInt32(pBlock[3] != 0)
}

func WelsWriteMbResidualCabac(pFuncList *SWelsFuncPtrList, pSlice *SSlice, sMbCacheInfo *SMbCache, pCurMb *SMB, pCabacCtx *SCabacCtx, iMbWidth int16, uiChromaQpIndexOffset uint32) int32 {

	uiMbType := uint16(pCurMb.uiMbType)
	pMbCache := &pSlice.sMbCacheInfo
	pNonZeroCoeffCount := pMbCache.iNonZeroCoeffCount[:]
	pSliceHeadExt := &pSlice.sSliceHeaderExt
	iSliceFirstMbXY := pSliceHeadExt.sSliceHeader.iFirstMbInSlice
	kuiMbWidth := uint32(iMbWidth)

	pCurMb.iCbpDc = 0
	pCurMb.iLumaDQp = 0

	if (pCurMb.uiCbp > 0) || (uiMbType == common.MB_TYPE_INTRA16x16) {
		iCbpChroma := int32(pCurMb.uiCbp >> 4)
		iCbpLuma := int32(pCurMb.uiCbp & 15)

		pCurMb.iLumaDQp = int32(pCurMb.uiLumaQp) - int32(pSlice.uiLastMbQp)
		WelsCabacMbDeltaQp(pCurMb, pCabacCtx, (pCurMb.iMbXY == iSliceFirstMbXY))
		pSlice.uiLastMbQp = pCurMb.uiLumaQp

		if uiMbType == common.MB_TYPE_INTRA16x16 {
			//Luma DC
			iNonZeroCount := pFuncList.pfGetNoneZeroCount(pMbCache.pDct.iLumaI16x16Dc[:])
			WelsWriteBlockResidualCabac(pMbCache, pCurMb, kuiMbWidth, pCabacCtx, LUMA_DC, 0, int16(iNonZeroCount),
				pMbCache.pDct.iLumaI16x16Dc[:], 15)
			if iNonZeroCount != 0 {
				pCurMb.iCbpDc |= 1
			}
			//Luma AC

			if iCbpLuma != 0 {
				for i := 0; i < 16; i++ {
					iIdx := int16(common.G_kuiCache48CountScan4Idx[i])
					WelsWriteBlockResidualCabac(pMbCache, pCurMb, kuiMbWidth, pCabacCtx, LUMA_AC, iIdx,
						int16(pNonZeroCoeffCount[iIdx]), pMbCache.pDct.iLumaBlock[i*16:], 14)
				}
			}
		} else {
			//Luma AC
			for i := 0; i < 16; i++ {
				if iCbpLuma&(1<<uint(i>>2)) != 0 {
					iIdx := int16(common.G_kuiCache48CountScan4Idx[i])
					WelsWriteBlockResidualCabac(pMbCache, pCurMb, kuiMbWidth, pCabacCtx, LUMA_4x4, iIdx,
						int16(pNonZeroCoeffCount[iIdx]), pMbCache.pDct.iLumaBlock[i*16:], 15)
				}

			}
		}

		if iCbpChroma != 0 {
			iNonZeroCount := int32(0)
			//chroma DC
			iNonZeroCount = WelsCalNonZeroCount2x2Block(pMbCache.pDct.iChromaDc[0][:])
			if iNonZeroCount != 0 {
				pCurMb.iCbpDc |= 0x2
			}
			WelsWriteBlockResidualCabac(pMbCache, pCurMb, kuiMbWidth, pCabacCtx, CHROMA_DC, 1, int16(iNonZeroCount),
				pMbCache.pDct.iChromaDc[0][:], 3)

			iNonZeroCount = WelsCalNonZeroCount2x2Block(pMbCache.pDct.iChromaDc[1][:])
			if iNonZeroCount != 0 {
				pCurMb.iCbpDc |= 0x4
			}
			WelsWriteBlockResidualCabac(pMbCache, pCurMb, kuiMbWidth, pCabacCtx, CHROMA_DC, 2, int16(iNonZeroCount),
				pMbCache.pDct.iChromaDc[1][:], 3)
			if iCbpChroma&0x02 != 0 {
				g_kuiCache48CountScan4Idx_16base := common.G_kuiCache48CountScan4Idx[16:]
				//Cb AC
				for i := 0; i < 4; i++ {
					iIdx := int16(g_kuiCache48CountScan4Idx_16base[i])
					WelsWriteBlockResidualCabac(pMbCache, pCurMb, kuiMbWidth, pCabacCtx, CHROMA_AC, iIdx,
						int16(pNonZeroCoeffCount[iIdx]), pMbCache.pDct.iChromaBlock[i*16:], 14)

				}

				//Cr AC

				for i := 0; i < 4; i++ {
					iIdx := 24 + int16(g_kuiCache48CountScan4Idx_16base[i])
					WelsWriteBlockResidualCabac(pMbCache, pCurMb, kuiMbWidth, pCabacCtx, CHROMA_AC, iIdx,
						int16(pNonZeroCoeffCount[iIdx]), pMbCache.pDct.iChromaBlock[(4+i)*16:], 14)
				}
			}
		}
	} else {
		pCurMb.iLumaDQp = 0
		pCurMb.uiLumaQp = pSlice.uiLastMbQp
		pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(uint32(pCurMb.uiLumaQp)+uiChromaQpIndexOffset)]
	}
	return 0
}

func WelsInitSliceCabac(pEncCtx *sWelsEncCtx, pSlice *SSlice) {
	/* alignment needed */
	pBs := pSlice.pSliceBsa
	BsAlign(pBs)

	/* init cabac */
	WelsCabacContextInit(pEncCtx, &pSlice.sCabacCtx, pSlice.iCabacInitIdc)
	WelsCabacEncodeInit(&pSlice.sCabacCtx, pBs.PBuf, pBs.PCurBuf, pBs.PEndBuf)
}

func WelsSpatialWriteMbSynCabac(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) int32 {
	pCabacCtx := &pSlice.sCabacCtx
	pMbCache := &pSlice.sMbCacheInfo
	uiMbType := uint16(pCurMb.uiMbType)
	pSliceHeadExt := &pSlice.sSliceHeaderExt
	uiNumRefIdxL0Active := uint32(int32(pSliceHeadExt.sSliceHeader.uiNumRefIdxL0Active) - 1)
	iSliceFirstMbXY := pSliceHeadExt.sSliceHeader.iFirstMbInSlice
	iMbWidth := pEncCtx.pCurDqLayer.iMbWidth
	uiChromaQpIndexOffset := uint32(pEncCtx.pCurDqLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset)
	var sMvd SMVUnitXY
	iRet := int32(0)
	if pCurMb.iMbXY > iSliceFirstMbXY {
		WelsCabacEncodeTerminate(&pSlice.sCabacCtx, 0)
	}

	if common.IS_SKIP(pCurMb.uiMbType) {
		pCurMb.uiLumaQp = pSlice.uiLastMbQp
		pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(uint32(pCurMb.uiLumaQp)+uiChromaQpIndexOffset)]
		WelsMbSkipCabac(&pSlice.sCabacCtx, pCurMb, int32(iMbWidth), pEncCtx.eSliceType, 1)

	} else {
		//skip flag
		if pEncCtx.eSliceType != common.I_SLICE {
			WelsMbSkipCabac(&pSlice.sCabacCtx, pCurMb, int32(iMbWidth), pEncCtx.eSliceType, 0)
		}

		//write mb type
		WelsCabacMbType(pCabacCtx, pCurMb, pMbCache, int32(iMbWidth), pEncCtx.eSliceType)

		if common.IS_INTRA(uiMbType) {
			if uiMbType == common.MB_TYPE_INTRA4x4 {
				WelsCabacMbIntra4x4PredMode(pCabacCtx, pMbCache)
			}
			WelsCabacMbIntraChromaPredMode(pCabacCtx, pCurMb, pMbCache, int32(iMbWidth))
			sMvd.iMvX = 0
			sMvd.iMvY = 0
			for i := 0; i < 16; i++ {
				pCurMb.sMvd[i].sAssignMv(sMvd)
			}

		} else if uiMbType == common.MB_TYPE_16x16 {

			if uiNumRefIdxL0Active > 0 {
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 0)
			}
			sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(iMbWidth), pCurMb.sMv[0], pMbCache.sMbMvp[0], 0)

			for i := 0; i < 16; i++ {
				pCurMb.sMvd[i].sAssignMv(sMvd)
			}

		} else if uiMbType == common.MB_TYPE_16x8 {
			if uiNumRefIdxL0Active > 0 {
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 0)
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 12)
			}
			sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(iMbWidth), pCurMb.sMv[0], pMbCache.sMbMvp[0], 0)
			for i := 0; i < 8; i++ {
				pCurMb.sMvd[i].sAssignMv(sMvd)
			}
			sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(iMbWidth), pCurMb.sMv[8], pMbCache.sMbMvp[1], 8)
			for i := 8; i < 16; i++ {
				pCurMb.sMvd[i].sAssignMv(sMvd)
			}
		} else if uiMbType == common.MB_TYPE_8x16 {
			if uiNumRefIdxL0Active > 0 {
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 0)
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 2)
			}
			sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(iMbWidth), pCurMb.sMv[0], pMbCache.sMbMvp[0], 0)
			for i := 0; i < 16; i += 4 {
				pCurMb.sMvd[i].sAssignMv(sMvd)
				pCurMb.sMvd[i+1].sAssignMv(sMvd)
			}
			sMvd = WelsCabacMbMvd(pCabacCtx, pCurMb, uint32(iMbWidth), pCurMb.sMv[2], pMbCache.sMbMvp[1], 2)
			for i := 0; i < 16; i += 4 {
				pCurMb.sMvd[i+2].sAssignMv(sMvd)
				pCurMb.sMvd[i+3].sAssignMv(sMvd)
			}
		} else if (uiMbType == common.MB_TYPE_8x8) || (uiMbType == common.MB_TYPE_8x8_REF0) {
			//write sub_mb_type
			welsCabacSubMbType(pCabacCtx, pCurMb)

			if uiNumRefIdxL0Active > 0 {
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 0)
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 2)
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 12)
				WelsCabacMbRef(pCabacCtx, pCurMb, pMbCache, 14)
			}
			//write sub8x8 mvd
			welsCabacSubMbMvd(pCabacCtx, pCurMb, pMbCache, int32(iMbWidth))
		}
		if uiMbType != common.MB_TYPE_INTRA16x16 {
			WelsCabacMbCbp(pCurMb, int32(iMbWidth), pCabacCtx)
		}
		iRet = WelsWriteMbResidualCabac(pEncCtx.pFuncList, pSlice, pMbCache, pCurMb, pCabacCtx, iMbWidth,
			uiChromaQpIndexOffset)
	}
	if !common.IS_INTRA(pCurMb.uiMbType) {
		pCurMb.uiChromPredMode = 0
	}

	return iRet
}
