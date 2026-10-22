// Port of codec/encoder/core/src/svc_set_mb_syn_cavlc.cpp.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var g_kuiIntra4x4CbpMap = [48]uint32{
	3, 29, 30, 17, 31, 18, 37, 8, 32, 38, 19, 9, 20, 10, 11, 2, //15
	16, 33, 34, 21, 35, 22, 39, 4, 36, 40, 23, 5, 24, 6, 7, 1, //31
	41, 42, 43, 25, 44, 26, 46, 12, 45, 47, 27, 13, 28, 14, 15, 0, //47
}

var g_kuiInterCbpMap = [48]uint32{
	0, 2, 3, 7, 4, 8, 17, 13, 5, 18, 9, 14, 10, 15, 16, 11, //15
	1, 32, 33, 36, 34, 37, 44, 40, 35, 45, 38, 41, 39, 42, 43, 19, //31
	6, 24, 25, 20, 26, 21, 46, 28, 27, 47, 22, 29, 23, 30, 31, 12, //47
}

// ============================Enhance Layer CAVLC Writing===========================
func WelsSpatialWriteMbPred(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) {
	pMbCache := &pSlice.sMbCacheInfo
	pBs := pSlice.pSliceBsa
	pSliceHeadExt := &pSlice.sSliceHeaderExt
	iNumRefIdxl0ActiveMinus1 := int32(pSliceHeadExt.sSliceHeader.uiNumRefIdxL0Active) - 1

	uiMbType := pCurMb.uiMbType
	iCbpChroma := int32(pCurMb.uiCbp >> 4)
	iCbpLuma := int32(pCurMb.uiCbp & 15)
	i := 0

	var sMvd [2]SMVUnitXY

	iMbOffset := int32(0)

	switch pSliceHeadExt.sSliceHeader.eSliceType {
	case common.I_SLICE:
		iMbOffset = 0
	case common.P_SLICE:
		iMbOffset = 5
	default:
		return
	}

	switch uiMbType {
	case common.MB_TYPE_INTRA4x4:
		/* mb type */
		common.BsWriteUE(pBs, uint32(iMbOffset+0))

		/* prediction: luma */
		pPredFlag := pMbCache.pPrevIntra4x4PredModeFlag
		pRemMode := pMbCache.pRemIntra4x4PredModeFlag
		for {
			var uiPredFlag uint32
			if pPredFlag[i] {
				uiPredFlag = 1
			}
			common.BsWriteOneBit(pBs, uiPredFlag) /* b_prev_intra4x4_pred_mode */

			if !pPredFlag[i] {
				common.BsWriteBits(pBs, 3, uint32(pRemMode[i]))
			}

			i++
			if !(i < 16) {
				break
			}
		}

		/* prediction: chroma */
		common.BsWriteUE(pBs, uint32(g_kiMapModeIntraChroma[pMbCache.uiChmaI8x8Mode]))

	case common.MB_TYPE_INTRA16x16:
		/* mb type */
		iCbpLumaFlag := int32(0)
		if iCbpLuma != 0 {
			iCbpLumaFlag = 12
		}
		common.BsWriteUE(pBs, uint32(1+iMbOffset+int32(g_kiMapModeI16x16[pMbCache.uiLumaI16x16Mode])+(iCbpChroma<<2)+
			iCbpLumaFlag))

		/* prediction: chroma */
		common.BsWriteUE(pBs, uint32(g_kiMapModeIntraChroma[pMbCache.uiChmaI8x8Mode]))

	case common.MB_TYPE_16x16:
		common.BsWriteUE(pBs, 0) //uiMbType
		sMvd[0].sDeltaMv(pCurMb.sMv[0], pMbCache.sMbMvp[0])

		if iNumRefIdxl0ActiveMinus1 > 0 {
			BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[0]))
		}

		common.BsWriteSE(pBs, int32(sMvd[0].iMvX))
		common.BsWriteSE(pBs, int32(sMvd[0].iMvY))

	case common.MB_TYPE_16x8:
		common.BsWriteUE(pBs, 1) //uiMbType

		sMvd[0].sDeltaMv(pCurMb.sMv[0], pMbCache.sMbMvp[0])
		sMvd[1].sDeltaMv(pCurMb.sMv[8], pMbCache.sMbMvp[1])

		if iNumRefIdxl0ActiveMinus1 > 0 {
			BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[0]))
			BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[2]))
		}
		common.BsWriteSE(pBs, int32(sMvd[0].iMvX)) //block0
		common.BsWriteSE(pBs, int32(sMvd[0].iMvY))
		common.BsWriteSE(pBs, int32(sMvd[1].iMvX)) //block1
		common.BsWriteSE(pBs, int32(sMvd[1].iMvY))

	case common.MB_TYPE_8x16:
		common.BsWriteUE(pBs, 2) //uiMbType

		sMvd[0].sDeltaMv(pCurMb.sMv[0], pMbCache.sMbMvp[0])
		sMvd[1].sDeltaMv(pCurMb.sMv[2], pMbCache.sMbMvp[1])

		if iNumRefIdxl0ActiveMinus1 > 0 {
			BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[0]))
			BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[1]))
		}
		common.BsWriteSE(pBs, int32(sMvd[0].iMvX)) //block0
		common.BsWriteSE(pBs, int32(sMvd[0].iMvY))
		common.BsWriteSE(pBs, int32(sMvd[1].iMvX)) //block1
		common.BsWriteSE(pBs, int32(sMvd[1].iMvY))
	}
}

func WelsSpatialWriteSubMbPred(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) {
	pMbCache := &pSlice.sMbCacheInfo
	pBs := pSlice.pSliceBsa
	pSliceHeadExt := &pSlice.sSliceHeaderExt

	iNumRefIdxl0ActiveMinus1 := int32(pSliceHeadExt.sSliceHeader.uiNumRefIdxL0Active) - 1

	bSubRef0 := false
	kpScan4 := common.G_kuiMbCountScan4Idx[:]

	/* mb type */
	if pCurMb.pRefIndex[0] == 0 && pCurMb.pRefIndex[1] == 0 && pCurMb.pRefIndex[2] == 0 && pCurMb.pRefIndex[3] == 0 {
		common.BsWriteUE(pBs, 4)
		bSubRef0 = false
	} else {
		common.BsWriteUE(pBs, 3)
		bSubRef0 = true
	}

	//step 1: sub_mb_type
	for i := 0; i < 4; i++ {
		switch uint32(pCurMb.uiSubMbType[i]) {
		case common.SUB_MB_TYPE_8x8:
			common.BsWriteUE(pBs, 0)
		case common.SUB_MB_TYPE_8x4:
			common.BsWriteUE(pBs, 1)
		case common.SUB_MB_TYPE_4x8:
			common.BsWriteUE(pBs, 2)
		case common.SUB_MB_TYPE_4x4:
			common.BsWriteUE(pBs, 3)
		default: //should not enter
		}
	}

	//step 2: get and write uiRefIndex and sMvd
	if iNumRefIdxl0ActiveMinus1 > 0 && bSubRef0 {
		BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[0]))
		BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[1]))
		BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[2]))
		BsWriteTE(pBs, iNumRefIdxl0ActiveMinus1, uint32(pCurMb.pRefIndex[3]))
	}

	writeMvd := func(k uint8) {
		common.BsWriteSE(pBs, int32(pCurMb.sMv[k].iMvX)-int32(pMbCache.sMbMvp[k].iMvX))
		common.BsWriteSE(pBs, int32(pCurMb.sMv[k].iMvY)-int32(pMbCache.sMbMvp[k].iMvY))
	}

	//write sMvd
	for i := 0; i < 4; i++ {
		uiSubMbType := uint32(pCurMb.uiSubMbType[i])
		if common.SUB_MB_TYPE_8x8 == uiSubMbType {
			writeMvd(kpScan4[0])
		} else if common.SUB_MB_TYPE_4x4 == uiSubMbType {
			writeMvd(kpScan4[0])
			writeMvd(kpScan4[1])
			writeMvd(kpScan4[2])
			writeMvd(kpScan4[3])
		} else if common.SUB_MB_TYPE_8x4 == uiSubMbType {
			writeMvd(kpScan4[0])
			writeMvd(kpScan4[2])
		} else if common.SUB_MB_TYPE_4x8 == uiSubMbType {
			writeMvd(kpScan4[0])
			writeMvd(kpScan4[1])
		}
		kpScan4 = kpScan4[4:]
	}
}

func CheckBitstreamBuffer(kuiSliceIdx uint32, pEncCtx *sWelsEncCtx, pBs *common.SBitStringAux) int32 {
	iLeftLength := pBs.PEndBuf - pBs.PCurBuf - 1
	// assert (iLeftLength > 0);

	if iLeftLength < MAX_MACROBLOCK_SIZE_IN_BYTE_x2 {
		return ENC_RETURN_VLCOVERFLOWFOUND //ENC_RETURN_MEMALLOCERR;
		//TODO: call the realloc&copy instead
	}
	return ENC_RETURN_SUCCESS
}

// ============================Base Layer CAVLC Writing===============================
func WelsSpatialWriteMbSyn(pEncCtx *sWelsEncCtx, pSlice *SSlice, pCurMb *SMB) int32 {
	pBs := pSlice.pSliceBsa
	pMbCache := &pSlice.sMbCacheInfo
	kuiChromaQpIndexOffset := pEncCtx.pCurDqLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset

	if common.IS_SKIP(pCurMb.uiMbType) {
		pCurMb.uiLumaQp = pSlice.uiLastMbQp
		pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+int32(kuiChromaQpIndexOffset))]

		pSlice.iMbSkipRun++
		return ENC_RETURN_SUCCESS
	} else {
		if pEncCtx.eSliceType != common.I_SLICE {
			common.BsWriteUE(pBs, uint32(pSlice.iMbSkipRun))
			pSlice.iMbSkipRun = 0
		}
		/* Step 1: write mb type and pred */
		if common.IS_Inter_8x8(pCurMb.uiMbType) {
			WelsSpatialWriteSubMbPred(pEncCtx, pSlice, pCurMb)
		} else {
			WelsSpatialWriteMbPred(pEncCtx, pSlice, pCurMb)
		}

		/* Step 2: write coded block patern */
		if common.IS_INTRA4x4(pCurMb.uiMbType) {
			common.BsWriteUE(pBs, g_kuiIntra4x4CbpMap[pCurMb.uiCbp])
		} else if !common.IS_INTRA16x16(pCurMb.uiMbType) {
			common.BsWriteUE(pBs, g_kuiInterCbpMap[pCurMb.uiCbp])
		}

		/* Step 3: write QP and residual */
		if pCurMb.uiCbp > 0 || common.IS_INTRA16x16(pCurMb.uiMbType) {
			kiDeltaQp := int32(pCurMb.uiLumaQp) - int32(pSlice.uiLastMbQp)
			pSlice.uiLastMbQp = pCurMb.uiLumaQp

			common.BsWriteSE(pBs, kiDeltaQp)
			if WelsWriteMbResidual(pEncCtx.pFuncList, pMbCache, pCurMb, pBs) != 0 {
				return ENC_RETURN_VLCOVERFLOWFOUND
			}
		} else {
			pCurMb.uiLumaQp = pSlice.uiLastMbQp
			pCurMb.uiChromaQp = common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+
				int32(pEncCtx.pCurDqLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset))]
		}

		/* Step 4: Check the left buffer */
		return CheckBitstreamBuffer(uint32(pSlice.iSliceIdx), pEncCtx, pBs)
	}
}
