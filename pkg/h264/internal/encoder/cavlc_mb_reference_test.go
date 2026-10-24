package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// Preserved pre-batching traversal uses the independent arithmetic block writer.
func welsWriteMbResidualReference(pFuncList *SWelsFuncPtrList, sMbCacheInfo *SMbCache, pCurMb *SMB, pBs *common.SBitStringAux) int32 {
	uiMbType := pCurMb.uiMbType
	kiCbpChroma := int32(pCurMb.uiCbp >> 4)
	kiCbpLuma := int32(pCurMb.uiCbp & 0x0F)
	pNonZeroCoeffCount := sMbCacheInfo.iNonZeroCoeffCount[:]
	var pBlock []int16
	var iA, iB, iC int8

	if common.IS_INTRA16x16(uiMbType) {
		/* DC luma */
		iA = pNonZeroCoeffCount[8]
		iB = pNonZeroCoeffCount[1]
		iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
		if writeBlockResidualCavlcReference(pFuncList, sMbCacheInfo.pDct.iLumaI16x16Dc[:], 15, 1, int32(LUMA_4x4), iC, pBs) != 0 {
			return ENC_RETURN_VLCOVERFLOWFOUND
		}

		/* AC Luma */
		if kiCbpLuma != 0 {
			pBlock = sMbCacheInfo.pDct.iLumaBlock[:]

			for i := 0; i < 16; i++ {
				iIdx := int(common.G_kuiCache48CountScan4Idx[i])
				iA = pNonZeroCoeffCount[iIdx-1]
				iB = pNonZeroCoeffCount[iIdx-8]
				iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
				if writeBlockResidualCavlcReference(pFuncList, pBlock, 14, cavlcBoolToInt32(pNonZeroCoeffCount[iIdx] > 0), int32(LUMA_AC), iC, pBs) != 0 {
					return ENC_RETURN_VLCOVERFLOWFOUND
				}
				pBlock = pBlock[16:]
			}
		}
	} else {
		/* Luma DC AC */
		if kiCbpLuma != 0 {
			pBlock = sMbCacheInfo.pDct.iLumaBlock[:]

			for i := 0; i < 16; i += 4 {
				if kiCbpLuma&(1<<uint(i>>2)) != 0 {
					iIdx := int(common.G_kuiCache48CountScan4Idx[i])
					kiA := pNonZeroCoeffCount[iIdx]
					kiB := pNonZeroCoeffCount[iIdx+1]
					kiC := pNonZeroCoeffCount[iIdx+8]
					kiD := pNonZeroCoeffCount[iIdx+9]
					iA = pNonZeroCoeffCount[iIdx-1]
					iB = pNonZeroCoeffCount[iIdx-8]
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock, 15, cavlcBoolToInt32(kiA > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}

					iA = kiA
					iB = pNonZeroCoeffCount[iIdx-7]
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock[16:], 15, cavlcBoolToInt32(kiB > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}

					iA = pNonZeroCoeffCount[iIdx+7]
					iB = kiA
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock[32:], 15, cavlcBoolToInt32(kiC > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}

					iA = kiC
					iB = kiB
					iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
					if writeBlockResidualCavlcReference(pFuncList, pBlock[48:], 15, cavlcBoolToInt32(kiD > 0), int32(LUMA_4x4), iC, pBs) != 0 {
						return ENC_RETURN_VLCOVERFLOWFOUND
					}
				}
				pBlock = pBlock[64:]
			}
		}
	}

	if kiCbpChroma != 0 {
		/* Chroma DC residual present */
		pBlock = sMbCacheInfo.pDct.iChromaDc[0][:] // Cb
		if writeBlockResidualCavlcReference(pFuncList, pBlock, 3, 1, int32(CHROMA_DC), CHROMA_DC_NC_OFFSET, pBs) != 0 {
			return ENC_RETURN_VLCOVERFLOWFOUND
		}

		pBlock = sMbCacheInfo.pDct.iChromaDc[1][:] // Cr
		if writeBlockResidualCavlcReference(pFuncList, pBlock, 3, 1, int32(CHROMA_DC), CHROMA_DC_NC_OFFSET, pBs) != 0 {
			return ENC_RETURN_VLCOVERFLOWFOUND
		}

		/* Chroma AC residual present */
		if kiCbpChroma&0x02 != 0 {
			kCache48CountScan4Idx16base := common.G_kuiCache48CountScan4Idx[16:]
			pBlock = sMbCacheInfo.pDct.iChromaBlock[:] // Cb

			for i := 0; i < 4; i++ {
				iIdx := int(kCache48CountScan4Idx16base[i])
				iA = pNonZeroCoeffCount[iIdx-1]
				iB = pNonZeroCoeffCount[iIdx-8]
				iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
				if writeBlockResidualCavlcReference(pFuncList, pBlock, 14, cavlcBoolToInt32(pNonZeroCoeffCount[iIdx] > 0), int32(CHROMA_AC), iC, pBs) != 0 {
					return ENC_RETURN_VLCOVERFLOWFOUND
				}
				pBlock = pBlock[16:]
			}

			pBlock = sMbCacheInfo.pDct.iChromaBlock[4*16:] // Cr

			for i := 0; i < 4; i++ {
				iIdx := 24 + int(kCache48CountScan4Idx16base[i])
				iA = pNonZeroCoeffCount[iIdx-1]
				iB = pNonZeroCoeffCount[iIdx-8]
				iC = common.WELS_NON_ZERO_COUNT_AVERAGE(iA, iB)
				if writeBlockResidualCavlcReference(pFuncList, pBlock, 14, cavlcBoolToInt32(pNonZeroCoeffCount[iIdx] > 0), int32(CHROMA_AC), iC, pBs) != 0 {
					return ENC_RETURN_VLCOVERFLOWFOUND
				}
				pBlock = pBlock[16:]
			}
		}
	}
	return 0
}
