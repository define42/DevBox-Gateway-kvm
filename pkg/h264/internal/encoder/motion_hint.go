package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// tryMotionHintMacroblock codes a macroblock with a motion hint as P16x16 with
// the hinted full-pel vector instead of searching. It mirrors the 16x16 path of
// WelsMdBackgroundMbEnc with that vector in place of the zero vector, and the
// patched native library's TryMotionHintMacroblock.
func tryMotionHintMacroblock(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pSlice *SSlice, pCurMb *SMB) bool {
	state := &pEncCtx.unchanged
	index := int(pCurMb.iMbXY)
	if index < 0 || index >= len(state.motion) {
		return false
	}
	hint := state.motion[index]
	full := SMVUnitXY{iMvX: hint.X, iMvY: hint.Y}
	if hint == (api.MacroblockMotion{}) || !hintReferenceValid(pEncCtx) ||
		!CheckMvInRange(full, pSlice.sMvStartMin, pSlice.sMvStartMax) {
		return false
	}

	pCurDqLayer := pEncCtx.pCurDqLayer
	pFunc := pEncCtx.pFuncList
	pMbCache := &pSlice.sMbCacheInfo
	sMv := SMVUnitXY{iMvX: full.iMvX * 4, iMvY: full.iMvY * 4}
	iLineSizeY := pCurDqLayer.pRefPic.iLineSize[0]
	iLineSizeUV := pCurDqLayer.pRefPic.iLineSize[1]
	pRefLuma := pMbCache.SPicData.pRefMb[0]
	iRefLumaOff := pMbCache.SPicData.iRefMbOff[0] + int(full.iMvY)*int(iLineSizeY) + int(full.iMvX)
	iChromaOffset := int((int32(sMv.iMvY)>>3)*iLineSizeUV + (int32(sMv.iMvX) >> 3))
	pRefCb, iRefCbOff := pMbCache.SPicData.pRefMb[1], pMbCache.SPicData.iRefMbOff[1]+iChromaOffset
	pRefCr, iRefCrOff := pMbCache.SPicData.pRefMb[2], pMbCache.SPicData.iRefMbOff[2]+iChromaOffset
	pDstLuma := pMbCache.pMemPredLuma
	pDstCb, iDstCbOff := pMbCache.pMemPredChroma, 0
	pDstCr, iDstCrOff := pMbCache.pMemPredChroma, 64

	//MC
	pFunc.sMcFuncs.PMcLumaFunc(pRefLuma, iRefLumaOff, iLineSizeY, pDstLuma, 0, 16, 0, 0, 16, 16)
	pFunc.sMcFuncs.PMcChromaFunc(pRefCb, iRefCbOff, iLineSizeUV, pDstCb, iDstCbOff, 8, sMv.iMvX, sMv.iMvY, 8, 8) //Cb
	pFunc.sMcFuncs.PMcChromaFunc(pRefCr, iRefCrOff, iLineSizeUV, pDstCr, iDstCrOff, 8, sMv.iMvX, sMv.iMvY, 8, 8) //Cr

	pSad := &pFunc.sSampleDealingFuncs.pfSampleSad
	*pCurMb.pSadCost = pSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
		pCurDqLayer.iEncStride[0], pRefLuma, iRefLumaOff, iLineSizeY)
	if pWelsMd.bMdUsingSad {
		pWelsMd.iCostLuma = *pCurMb.pSadCost
	} else {
		pWelsMd.iCostLuma = pFunc.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16](pMbCache.SPicData.pEncMb[0],
			pMbCache.SPicData.iEncMbOff[0], pCurDqLayer.iEncStride[0], pRefLuma, iRefLumaOff, iLineSizeY)
	}
	pWelsMd.iCostSkipMb = pSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
		pCurDqLayer.iEncStride[0], pDstLuma, 0, 16)
	pWelsMd.iCostSkipMb += pSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1],
		pCurDqLayer.iEncStride[1], pDstCb, iDstCbOff, 8)
	pWelsMd.iCostSkipMb += pSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2],
		pCurDqLayer.iEncStride[2], pDstCr, iDstCrOff, 8)
	pCurMb.sP16x16Mv = sMv
	pCurDqLayer.pDecPic.sMvList[index] = sMv

	pCurMb.uiMbType = common.MB_TYPE_16x16
	pWelsMd.sMe.sMe16x16.sMv = sMv
	PredMv(&pMbCache.sMvComponents, 0, 4, int32(pWelsMd.uiRef), &pWelsMd.sMe.sMe16x16.sMvp)
	pMbCache.sMbMvp[0] = pWelsMd.sMe.sMe16x16.sMvp
	UpdateP16x16MotionInfo(pMbCache, pCurMb, int8(pWelsMd.uiRef), &sMv)

	WelsMdInterEncode(pEncCtx, pSlice, pCurMb, pMbCache)
	WelsMdInterDoubleCheckPskip(pCurMb, pMbCache)

	return true
}
