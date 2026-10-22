package encoder

// Frozen round7 lazy skip path, before the fused transform/quantization.
func legacySkipDctQuantPixels(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache) bool {
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
		if !legacySkipDctQuantGroup(pMbCache.pCoeffLevel[offset:], pMbCache.pDct.iLumaBlock[offset:],
			g_kiQuantInterFF[qp][:], g_kiQuantMF[qp][:], &score) {
			return false
		}
	}
	return true
}

func legacySkipDctQuantGroup(residual, levels, rounding, multiplier []int16, score *int32) bool {
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
