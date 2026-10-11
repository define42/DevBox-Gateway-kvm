// Port of codec/decoder/core/src/decode_slice.cpp (current slice decoding).
//
// Only the single-threaded, non-SIMD paths are ported. The MB-level residual
// parsing blocks that are textually duplicated in the C++ source (CAVLC I/P/B,
// CABAC I/P/B and the CAVLC I_PCM copy) are shared through file-local helpers
// whose bodies are verbatim translations of the C++ blocks.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// dsBoolToInt8 converts a C bool stored into an int8_t field.
func dsBoolToInt8(b bool) int8 {
	if b {
		return 1
	}
	return 0
}

// dsCopyNzc implements ST32/ST16 (dst, LD32/LD16 (src)) between the uint8_t
// non-zero-count cache and the int8_t per-MB pNzc array.
func dsCopyNzc(pDst []int8, iDstOff int, pSrc []uint8, iSrcOff int, n int) {
	for k := 0; k < n; k++ {
		pDst[iDstOff+k] = int8(pSrc[iSrcOff+k])
	}
}

// dsZeroNzc implements ST32 (&pNzc[0..20], 0) for all 24 entries.
func dsZeroNzc(pNzc []int8) {
	for k := 0; k < 24; k++ {
		pNzc[k] = 0
	}
}

// static bool CheckRefPics (const PWelsDecoderContext& pCtx)
func CheckRefPics(pCtx *SWelsDecoderContext) bool {
	var listCount int32 = 1
	if pCtx.eSliceType == common.B_SLICE {
		listCount++
	}
	for list := int32(common.LIST_0); list < listCount; list++ {
		shortRefCount := int32(pCtx.sRefPic.uiShortRefCount[list])
		for refIdx := int32(0); refIdx < shortRefCount; refIdx++ {
			if pCtx.sRefPic.pShortRefList[list][refIdx] == nil {
				return false
			}
		}
		longRefCount := int32(pCtx.sRefPic.uiLongRefCount[list])
		for refIdx := int32(0); refIdx < longRefCount; refIdx++ {
			if pCtx.sRefPic.pLongRefList[list][refIdx] == nil {
				return false
			}
		}
	}
	return true
}

// int32_t WelsTargetSliceConstruction (PWelsDecoderContext pCtx)
func WelsTargetSliceConstruction(pCtx *SWelsDecoderContext) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pCurSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pCurSlice.sSliceHeaderExt.sSliceHeader

	iTotalMbTargetLayer := int32(pSliceHeader.pSps.uiTotalMbCount)

	iCurLayerWidth := pCurDqLayer.iMbWidth << 4
	iCurLayerHeight := pCurDqLayer.iMbHeight << 4

	var iNextMbXyIndex int32
	pFmo := pCtx.pFmo

	iTotalNumMb := pCurSlice.iTotalMbInCurSlice
	var iCountNumMb int32
	var pDeblockMb PDeblockingFilterMbFunc = WelsDeblockingMb

	if !pCtx.sSpsPpsCtx.bAvcBasedFlag && iCurLayerWidth != pCtx.iCurSeqIntervalMaxPicWidth {
		return ERR_INFO_WIDTH_MISMATCH
	}

	iNextMbXyIndex = pSliceHeader.iFirstMbInSlice
	pCurDqLayer.iMbX = iNextMbXyIndex % pCurDqLayer.iMbWidth
	pCurDqLayer.iMbY = iNextMbXyIndex / pCurDqLayer.iMbWidth
	pCurDqLayer.iMbXyIndex = iNextMbXyIndex

	if 0 == iNextMbXyIndex {
		pCurDqLayer.pDec.iSpsId = pCtx.pSps.iSpsId
		pCurDqLayer.pDec.iPpsId = pCtx.pPps.iPpsId

		pCurDqLayer.pDec.uiQualityId = pCurDqLayer.sLayerInfo.sNalHeaderExt.UiQualityId
	}

	for {
		if iCountNumMb >= iTotalNumMb {
			break
		}

		if !pCtx.pParam.BParseOnly { //for parse only, actual recon MB unnecessary
			if WelsTargetMbConstruction(pCtx) != 0 {
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
					"WelsTargetSliceConstruction():::MB(%d, %d) construction error. pCurSlice_type:%d",
					pCurDqLayer.iMbX, pCurDqLayer.iMbY, pCurSlice.eSliceType)

				return ERR_INFO_MB_RECON_FAIL
			}
		}

		iCountNumMb++
		if !pCurDqLayer.pMbCorrectlyDecodedFlag[iNextMbXyIndex] { //already con-ed, overwrite
			pCurDqLayer.pMbCorrectlyDecodedFlag[iNextMbXyIndex] = true
			if pCurDqLayer.pMbRefConcealedFlag[iNextMbXyIndex] {
				pCtx.pDec.iMbEcedPropNum++
			}
			pCtx.iTotalNumMbRec++
		}

		if pCtx.iTotalNumMbRec > iTotalMbTargetLayer {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
				"WelsTargetSliceConstruction():::pCtx->iTotalNumMbRec:%d, iTotalMbTargetLayer:%d",
				pCtx.iTotalNumMbRec, iTotalMbTargetLayer)

			return ERR_INFO_MB_NUM_EXCEED_FAIL
		}

		if pSliceHeader.pPps.uiNumSliceGroups > 1 {
			iNextMbXyIndex = FmoNextMb(pFmo, iNextMbXyIndex)
		} else {
			iNextMbXyIndex++
		}
		if -1 == iNextMbXyIndex || iNextMbXyIndex >= iTotalMbTargetLayer { // slice group boundary or end of a frame
			break
		}
		pCurDqLayer.iMbX = iNextMbXyIndex % pCurDqLayer.iMbWidth
		pCurDqLayer.iMbY = iNextMbXyIndex / pCurDqLayer.iMbWidth
		pCurDqLayer.iMbXyIndex = iNextMbXyIndex
	}

	pCtx.pDec.iWidthInPixel = iCurLayerWidth
	pCtx.pDec.iHeightInPixel = iCurLayerHeight

	if (pCurSlice.eSliceType != common.I_SLICE) && (pCurSlice.eSliceType != common.P_SLICE) && (pCurSlice.eSliceType != common.B_SLICE) {
		return ERR_NONE //no error but just ignore the type unsupported
	}

	if pCtx.pParam.BParseOnly { //for parse only, deblocking should not go on
		return ERR_NONE
	}

	if 1 == pSliceHeader.uiDisableDeblockingFilterIdc ||
		pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer.iTotalMbInCurSlice <= 0 {
		return ERR_NONE //NO_SUPPORTED_FILTER_IDX
	} else {
		WelsDeblockingFilterSlice(pCtx, pDeblockMb)
	}
	// any other filter_idc not supported here, 7/22/2010

	return ERR_NONE
}

// int32_t WelsMbInterSampleConstruction (PWelsDecoderContext pCtx, PDqLayer pCurDqLayer, uint8_t*
// pDstY, uint8_t* pDstU, uint8_t* pDstV, int32_t iStrideL, int32_t iStrideC)
//
// pDstY / pDstU / pDstV: (slice, offset) pairs.
func WelsMbInterSampleConstruction(pCtx *SWelsDecoderContext, pCurDqLayer *SDqLayer, pDstY []uint8, iDstYOff int, pDstU []uint8, iDstUOff int, pDstV []uint8, iDstVOff int, iStrideL int32, iStrideC int32) int32 {
	iMbXy := pCurDqLayer.iMbXyIndex
	var i, iIndex, iOffset int32

	if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
		for i = 0; i < 4; i++ {
			iIndex = int32(common.G_kuiMbCountScan4Idx[i<<2])
			if pCurDqLayer.pNzc[iMbXy][iIndex] != 0 || pCurDqLayer.pNzc[iMbXy][iIndex+1] != 0 || pCurDqLayer.pNzc[iMbXy][iIndex+4] != 0 ||
				pCurDqLayer.pNzc[iMbXy][iIndex+5] != 0 {
				iOffset = ((iIndex>>2)<<2)*iStrideL + ((iIndex % 4) << 2)
				pCtx.pIdctResAddPredFunc8x8(pDstY, iDstYOff+int(iOffset), iStrideL, pCurDqLayer.pScaledTCoeff[iMbXy][i<<6:])
			}
		}
	} else {
		// luma.
		pNzc := pCurDqLayer.pNzc[iMbXy][:]
		pScaledTCoeff := pCurDqLayer.pScaledTCoeff[iMbXy][:]
		pCtx.pIdctFourResAddPredFunc(pDstY, iDstYOff+int(0*iStrideL+0), iStrideL, pScaledTCoeff[0*64:], pNzc[0:])
		pCtx.pIdctFourResAddPredFunc(pDstY, iDstYOff+int(0*iStrideL+8), iStrideL, pScaledTCoeff[1*64:], pNzc[2:])
		pCtx.pIdctFourResAddPredFunc(pDstY, iDstYOff+int(8*iStrideL+0), iStrideL, pScaledTCoeff[2*64:], pNzc[8:])
		pCtx.pIdctFourResAddPredFunc(pDstY, iDstYOff+int(8*iStrideL+8), iStrideL, pScaledTCoeff[3*64:], pNzc[10:])
	}

	pNzc := pCurDqLayer.pNzc[iMbXy][:]
	pScaledTCoeff := pCurDqLayer.pScaledTCoeff[iMbXy][:]
	// Cb.
	pCtx.pIdctFourResAddPredFunc(pDstU, iDstUOff, iStrideC, pScaledTCoeff[4*64:], pNzc[16:])
	// Cr.
	pCtx.pIdctFourResAddPredFunc(pDstV, iDstVOff, iStrideC, pScaledTCoeff[5*64:], pNzc[18:])

	return ERR_NONE
}

// dsInterPredict is the prediction part shared verbatim by
// WelsMbInterConstruction and WelsMbInterPrediction. It returns the C
// WELS_B_MB_REC_VERIFY error (if any) and the destination offsets.
func dsInterPredict(pCtx *SWelsDecoderContext, pCurDqLayer *SDqLayer) (int32, int, int, int) {
	iMbX := pCurDqLayer.iMbX
	iMbY := pCurDqLayer.iMbY

	iLumaStride := pCtx.pDec.iLinesize[0]
	iChromaStride := pCtx.pDec.iLinesize[1]

	pDec := pCurDqLayer.pDec
	iDstY := pDec.iDataOff[0] + int((iMbY*iLumaStride+iMbX)<<4)
	iDstCb := pDec.iDataOff[1] + int((iMbY*iChromaStride+iMbX)<<3)
	iDstCr := pDec.iDataOff[2] + int((iMbY*iChromaStride+iMbX)<<3)

	if pCtx.eSliceType == common.P_SLICE {
		if uiRetTmp := uint32(GetInterPred(pDec.pData[0], iDstY, pDec.pData[1], iDstCb, pDec.pData[2], iDstCr, pCtx)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp), iDstY, iDstCb, iDstCr
		}
	} else {
		if pCtx.pTempDec == nil {
			pCtx.pTempDec = AllocPicture(pCtx, int32(pCtx.pSps.iMbWidth<<4), int32(pCtx.pSps.iMbHeight<<4))
		}
		var pTempDstYCbCr [3][]uint8
		var iTempDstYCbCrOff [3]int
		var pDstYCbCr [3][]uint8
		var iDstYCbCrOff [3]int
		pTempDstYCbCr[0] = pCtx.pTempDec.pData[0]
		pTempDstYCbCr[1] = pCtx.pTempDec.pData[1]
		pTempDstYCbCr[2] = pCtx.pTempDec.pData[2]
		iTempDstYCbCrOff[0] = pCtx.pTempDec.iDataOff[0] + int((iMbY*iLumaStride+iMbX)<<4)
		iTempDstYCbCrOff[1] = pCtx.pTempDec.iDataOff[1] + int((iMbY*iChromaStride+iMbX)<<3)
		iTempDstYCbCrOff[2] = pCtx.pTempDec.iDataOff[2] + int((iMbY*iChromaStride+iMbX)<<3)
		pDstYCbCr[0] = pDec.pData[0]
		pDstYCbCr[1] = pDec.pData[1]
		pDstYCbCr[2] = pDec.pData[2]
		iDstYCbCrOff[0] = iDstY
		iDstYCbCrOff[1] = iDstCb
		iDstYCbCrOff[2] = iDstCr
		if uiRetTmp := uint32(GetInterBPred(pDstYCbCr, iDstYCbCrOff, pTempDstYCbCr, iTempDstYCbCrOff, pCtx)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp), iDstY, iDstCb, iDstCr
		}
	}
	return ERR_NONE, iDstY, iDstCb, iDstCr
}

// int32_t WelsMbInterConstruction (PWelsDecoderContext pCtx, PDqLayer pCurDqLayer)
func WelsMbInterConstruction(pCtx *SWelsDecoderContext, pCurDqLayer *SDqLayer) int32 {
	iRet, iDstY, iDstCb, iDstCr := dsInterPredict(pCtx, pCurDqLayer)
	if iRet != ERR_NONE {
		return iRet
	}
	iLumaStride := pCtx.pDec.iLinesize[0]
	iChromaStride := pCtx.pDec.iLinesize[1]
	pDec := pCurDqLayer.pDec
	WelsMbInterSampleConstruction(pCtx, pCurDqLayer, pDec.pData[0], iDstY, pDec.pData[1], iDstCb, pDec.pData[2], iDstCr,
		iLumaStride, iChromaStride)

	if GetThreadCount(pCtx) <= 1 {
		pCtx.sBlockFunc.pWelsSetNonZeroCountFunc(
			pCurDqLayer.pNzc[pCurDqLayer.iMbXyIndex][:]) // set all none-zero nzc to 1; dbk can be opti!
	}
	return ERR_NONE
}

// void WelsLumaDcDequantIdct (int16_t* pBlock, int32_t iQp, PWelsDecoderContext pCtx)
func WelsLumaDcDequantIdct(pBlock []int16, iQp int32, pCtx *SWelsDecoderContext) {
	var kiQMul int32
	if pCtx.bUseScalingList {
		kiQMul = int32(pCtx.pDequant_coeff4x4[0][iQp][0])
	} else {
		kiQMul = int32(common.G_kuiDequantCoeff[iQp][0]) << 4
	}
	const STRIDE = 16
	var i int32
	var iTemp [16]int32 //FIXME check if this is a good idea
	pBlk := pBlock
	kiXOffset := [4]int32{0, STRIDE, STRIDE << 2, 5 * STRIDE}
	kiYOffset := [4]int32{0, STRIDE << 1, STRIDE << 3, 10 * STRIDE}

	for i = 0; i < 4; i++ {
		kiOffset := kiYOffset[i]
		kiX1 := kiOffset + kiXOffset[2]
		kiX2 := STRIDE + kiOffset
		kiX3 := kiOffset + kiXOffset[3]
		kiI4 := i << 2 // 4*i
		kiZ0 := int32(pBlk[kiOffset]) + int32(pBlk[kiX1])
		kiZ1 := int32(pBlk[kiOffset]) - int32(pBlk[kiX1])
		kiZ2 := int32(pBlk[kiX2]) - int32(pBlk[kiX3])
		kiZ3 := int32(pBlk[kiX2]) + int32(pBlk[kiX3])

		iTemp[kiI4] = kiZ0 + kiZ3
		iTemp[1+kiI4] = kiZ1 + kiZ2
		iTemp[2+kiI4] = kiZ1 - kiZ2
		iTemp[3+kiI4] = kiZ0 - kiZ3
	}

	for i = 0; i < 4; i++ {
		kiOffset := kiXOffset[i]
		kiI4 := 4 + i
		kiZ0 := iTemp[i] + iTemp[4+kiI4]
		kiZ1 := iTemp[i] - iTemp[4+kiI4]
		kiZ2 := iTemp[kiI4] - iTemp[8+kiI4]
		kiZ3 := iTemp[kiI4] + iTemp[8+kiI4]

		pBlk[kiOffset] = int16(((kiZ0+kiZ3)*kiQMul + (1 << 5)) >> 6) //FIXME think about merging this into decode_resdual
		pBlk[kiYOffset[1]+kiOffset] = int16(((kiZ1+kiZ2)*kiQMul + (1 << 5)) >> 6)
		pBlk[kiYOffset[2]+kiOffset] = int16(((kiZ1-kiZ2)*kiQMul + (1 << 5)) >> 6)
		pBlk[kiYOffset[3]+kiOffset] = int16(((kiZ0-kiZ3)*kiQMul + (1 << 5)) >> 6)
	}
}

// int32_t WelsMbIntraPredictionConstruction (PWelsDecoderContext pCtx, PDqLayer pCurDqLayer, bool
// bOutput)
func WelsMbIntraPredictionConstruction(pCtx *SWelsDecoderContext, pCurDqLayer *SDqLayer, bOutput bool) int32 {
	//seems IPCM should not enter this path
	iMbXy := pCurDqLayer.iMbXyIndex

	WelsFillRecNeededMbInfo(pCtx, bOutput, pCurDqLayer)

	if common.IS_INTRA16x16(pCurDqLayer.pDec.pMbType[iMbXy]) {
		RecI16x16Mb(iMbXy, pCtx, pCurDqLayer.pScaledTCoeff[iMbXy][:], pCurDqLayer)
	} else if common.IS_INTRA8x8(pCurDqLayer.pDec.pMbType[iMbXy]) {
		RecI8x8Mb(iMbXy, pCtx, pCurDqLayer.pScaledTCoeff[iMbXy][:], pCurDqLayer)
	} else if common.IS_INTRA4x4(pCurDqLayer.pDec.pMbType[iMbXy]) {
		RecI4x4Mb(iMbXy, pCtx, pCurDqLayer.pScaledTCoeff[iMbXy][:], pCurDqLayer)
	}
	return ERR_NONE
}

// int32_t WelsMbInterPrediction (PWelsDecoderContext pCtx, PDqLayer pCurDqLayer)
func WelsMbInterPrediction(pCtx *SWelsDecoderContext, pCurDqLayer *SDqLayer) int32 {
	iRet, _, _, _ := dsInterPredict(pCtx, pCurDqLayer)
	if iRet != ERR_NONE {
		return iRet
	}
	return ERR_NONE
}

// int32_t WelsTargetMbConstruction (PWelsDecoderContext pCtx)
func WelsTargetMbConstruction(pCtx *SWelsDecoderContext) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	if common.MB_TYPE_INTRA_PCM == pCurDqLayer.pDec.pMbType[pCurDqLayer.iMbXyIndex] {
		//already decoded and reconstructed when parsing
		return ERR_NONE
	} else if common.IS_INTRA(pCurDqLayer.pDec.pMbType[pCurDqLayer.iMbXyIndex]) {
		WelsMbIntraPredictionConstruction(pCtx, pCurDqLayer, true)
	} else if common.IS_INTER(pCurDqLayer.pDec.pMbType[pCurDqLayer.iMbXyIndex]) { //InterMB
		if 0 == pCurDqLayer.pCbp[pCurDqLayer.iMbXyIndex] { //uiCbp==0 include SKIP
			if !CheckRefPics(pCtx) {
				return ERR_INFO_MB_RECON_FAIL
			}
			return WelsMbInterPrediction(pCtx, pCurDqLayer)
		} else {
			WelsMbInterConstruction(pCtx, pCurDqLayer)
		}
	} else {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "WelsTargetMbConstruction():::::Unknown MB type: %d",
			pCurDqLayer.pDec.pMbType[pCurDqLayer.iMbXyIndex])
		return ERR_INFO_MB_RECON_FAIL
	}

	return ERR_NONE
}

// void WelsChromaDcIdct (int16_t* pBlock)
func WelsChromaDcIdct(pBlock []int16) {
	var iStride int32 = 32
	var iXStride int32 = 16
	iStride1 := iXStride + iStride
	pBlk := pBlock
	var iA, iB, iC, iD, iE int32

	iA = int32(pBlk[0])
	iB = int32(pBlk[iXStride])
	iC = int32(pBlk[iStride])
	iD = int32(pBlk[iStride1])

	iE = iA - iB
	iA += iB
	iB = iC - iD
	iC += iD

	pBlk[0] = int16(iA + iC)
	pBlk[iXStride] = int16(iE + iB)
	pBlk[iStride] = int16(iA - iC)
	pBlk[iStride1] = int16(iE - iB)
}

// void WelsMapNxNNeighToSampleNormal (PWelsNeighAvail pNeighAvail, int32_t* pSampleAvail)
func WelsMapNxNNeighToSampleNormal(pNeighAvail *SWelsNeighAvail, pSampleAvail []int32) {
	if pNeighAvail.iLeftAvail != 0 { //left
		pSampleAvail[6] = 1
		pSampleAvail[12] = 1
		pSampleAvail[18] = 1
		pSampleAvail[24] = 1
	}
	if pNeighAvail.iLeftTopAvail != 0 { //top_left
		pSampleAvail[0] = 1
	}
	if pNeighAvail.iTopAvail != 0 { //top
		pSampleAvail[1] = 1
		pSampleAvail[2] = 1
		pSampleAvail[3] = 1
		pSampleAvail[4] = 1
	}
	if pNeighAvail.iRightTopAvail != 0 { //top_right
		pSampleAvail[5] = 1
	}
}

// void WelsMapNxNNeighToSampleConstrain1 (PWelsNeighAvail pNeighAvail, int32_t* pSampleAvail)
func WelsMapNxNNeighToSampleConstrain1(pNeighAvail *SWelsNeighAvail, pSampleAvail []int32) {
	if pNeighAvail.iLeftAvail != 0 && common.IS_INTRA(pNeighAvail.iLeftType) { //left
		pSampleAvail[6] = 1
		pSampleAvail[12] = 1
		pSampleAvail[18] = 1
		pSampleAvail[24] = 1
	}
	if pNeighAvail.iLeftTopAvail != 0 && common.IS_INTRA(pNeighAvail.iLeftTopType) { //top_left
		pSampleAvail[0] = 1
	}
	if pNeighAvail.iTopAvail != 0 && common.IS_INTRA(pNeighAvail.iTopType) { //top
		pSampleAvail[1] = 1
		pSampleAvail[2] = 1
		pSampleAvail[3] = 1
		pSampleAvail[4] = 1
	}
	if pNeighAvail.iRightTopAvail != 0 && common.IS_INTRA(pNeighAvail.iRightTopType) { //top_right
		pSampleAvail[5] = 1
	}
}

// void WelsMap16x16NeighToSampleNormal (PWelsNeighAvail pNeighAvail, uint8_t* pSampleAvail)
func WelsMap16x16NeighToSampleNormal(pNeighAvail *SWelsNeighAvail, pSampleAvail *uint8) {
	if pNeighAvail.iLeftAvail != 0 {
		*pSampleAvail = (1 << 2)
	}
	if pNeighAvail.iLeftTopAvail != 0 {
		*pSampleAvail |= (1 << 1)
	}
	if pNeighAvail.iTopAvail != 0 {
		*pSampleAvail |= 1
	}
}

// void WelsMap16x16NeighToSampleConstrain1 (PWelsNeighAvail pNeighAvail, uint8_t* pSampleAvail)
func WelsMap16x16NeighToSampleConstrain1(pNeighAvail *SWelsNeighAvail, pSampleAvail *uint8) {
	if pNeighAvail.iLeftAvail != 0 && common.IS_INTRA(pNeighAvail.iLeftType) {
		*pSampleAvail = (1 << 2)
	}
	if pNeighAvail.iLeftTopAvail != 0 && common.IS_INTRA(pNeighAvail.iLeftTopType) {
		*pSampleAvail |= (1 << 1)
	}
	if pNeighAvail.iTopAvail != 0 && common.IS_INTRA(pNeighAvail.iTopType) {
		*pSampleAvail |= 1
	}
}

// dsParseIntraChromaPredMode is the intra_chroma_pred_mode tail shared
// verbatim by ParseIntra4x4Mode, ParseIntra8x8Mode and ParseIntra16x16Mode.
func dsParseIntraChromaPredMode(pCtx *SWelsDecoderContext, uiNeighAvail uint8, pBs *common.SBitStringAux, pCurDqLayer *SDqLayer) int32 {
	iMbXy := pCurDqLayer.iMbXyIndex
	var uiCode uint32
	var iCode int32
	if pCurDqLayer.sLayerInfo.pPps.bEntropyCodingModeFlag {
		if uiRetTmp := uint32(ParseIntraPredModeChromaCabac(pCtx, uiNeighAvail, &iCode)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		if iCode > MAX_PRED_MODE_ID_CHROMA {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I_CHROMA_PRED_MODE)
		}
		pCurDqLayer.pChromaPredMode[iMbXy] = int8(iCode)
	} else {
		if uiRetTmp := BsGetUe(pBs, &uiCode); uiRetTmp != ERR_NONE { //intra_chroma_pred_mode
			return int32(uiRetTmp)
		}
		if uiCode > MAX_PRED_MODE_ID_CHROMA {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I_CHROMA_PRED_MODE)
		}
		pCurDqLayer.pChromaPredMode[iMbXy] = int8(uiCode)
	}

	if -1 == pCurDqLayer.pChromaPredMode[iMbXy] ||
		CheckIntraChromaPredMode(uiNeighAvail, &pCurDqLayer.pChromaPredMode[iMbXy]) != 0 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I_CHROMA_PRED_MODE)
	}
	return ERR_NONE
}

// int32_t ParseIntra4x4Mode (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, int8_t*
// pIntraPredMode, PBitStringAux pBs, PDqLayer pCurDqLayer)
//
// pIntraPredMode: the MB intra pred mode cache (sub-slice).
func ParseIntra4x4Mode(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pIntraPredMode []int8, pBs *common.SBitStringAux, pCurDqLayer *SDqLayer) int32 {
	var iSampleAvail [5 * 6]int32 //initialize as 0
	iMbXy := pCurDqLayer.iMbXyIndex
	var iFinalMode, i int32

	var uiNeighAvail uint8
	var uiCode uint32
	var iCode int32
	pCtx.pMapNxNNeighToSampleFunc(pNeighAvail, iSampleAvail[:])
	uiNeighAvail = uint8((iSampleAvail[6] << 2) | (iSampleAvail[0] << 1) | (iSampleAvail[1]))
	for i = 0; i < 16; i++ {
		var iPrevIntra4x4PredMode int32
		if pCurDqLayer.sLayerInfo.pPps.bEntropyCodingModeFlag {
			if uiRetTmp := uint32(ParseIntraPredModeLumaCabac(pCtx, &iCode)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			iPrevIntra4x4PredMode = iCode
		} else {
			if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			iPrevIntra4x4PredMode = int32(uiCode)
		}
		kiPredMode := PredIntra4x4Mode(pIntraPredMode, i)

		var iBestMode int8
		if pCurDqLayer.sLayerInfo.pPps.bEntropyCodingModeFlag {
			if iPrevIntra4x4PredMode == -1 {
				iBestMode = int8(kiPredMode)
			} else {
				iBestMode = int8(iPrevIntra4x4PredMode + int32(dsBoolToInt8(iPrevIntra4x4PredMode >= kiPredMode)))
			}
		} else {
			if iPrevIntra4x4PredMode != 0 {
				iBestMode = int8(kiPredMode)
			} else {
				if uiRetTmp := uint32(BsGetBits(pBs, 3, &uiCode)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp)
				}
				iBestMode = int8(uiCode + uint32(dsBoolToInt8(int32(uiCode) >= kiPredMode)))
			}
		}

		iFinalMode = CheckIntraNxNPredMode(iSampleAvail[:], &iBestMode, i, false)
		if iFinalMode == GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INVALID_INTRA4X4_MODE) {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I4x4_PRED_MODE)
		}

		pCurDqLayer.pIntra4x4FinalMode[iMbXy][g_kuiScan4[i]] = int8(iFinalMode)

		pIntraPredMode[g_kuiScan8[i]] = iBestMode

		iSampleAvail[common.G_kuiCache30ScanIdx[i]] = 1
	}
	copy(pCurDqLayer.pIntraPredMode[iMbXy][0:4], pIntraPredMode[1+8*4:1+8*4+4])
	pCurDqLayer.pIntraPredMode[iMbXy][4] = pIntraPredMode[4+8*1]
	pCurDqLayer.pIntraPredMode[iMbXy][5] = pIntraPredMode[4+8*2]
	pCurDqLayer.pIntraPredMode[iMbXy][6] = pIntraPredMode[4+8*3]

	if pCtx.pSps.uiChromaFormatIdc == 0 { //no need parse chroma
		return ERR_NONE
	}

	return dsParseIntraChromaPredMode(pCtx, uiNeighAvail, pBs, pCurDqLayer)
}

// int32_t ParseIntra8x8Mode (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, int8_t*
// pIntraPredMode, PBitStringAux pBs, PDqLayer pCurDqLayer)
//
// pIntraPredMode: the MB intra pred mode cache (sub-slice).
func ParseIntra8x8Mode(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pIntraPredMode []int8, pBs *common.SBitStringAux, pCurDqLayer *SDqLayer) int32 {
	// Similar with Intra_4x4, can put them together when needed
	var iSampleAvail [5 * 6]int32 //initialize as 0
	iMbXy := pCurDqLayer.iMbXyIndex
	var iFinalMode, i int32

	var uiNeighAvail uint8
	var uiCode uint32
	var iCode int32
	pCtx.pMapNxNNeighToSampleFunc(pNeighAvail, iSampleAvail[:])
	// Top-Right : Left : Top-Left : Top
	uiNeighAvail = uint8((iSampleAvail[5] << 3) | (iSampleAvail[6] << 2) | (iSampleAvail[0] << 1) | (iSampleAvail[1]))

	pCurDqLayer.pIntraNxNAvailFlag[iMbXy] = uiNeighAvail

	for i = 0; i < 4; i++ {
		var iPrevIntra4x4PredMode int32
		if pCurDqLayer.sLayerInfo.pPps.bEntropyCodingModeFlag {
			if uiRetTmp := uint32(ParseIntraPredModeLumaCabac(pCtx, &iCode)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			iPrevIntra4x4PredMode = iCode
		} else {
			if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			iPrevIntra4x4PredMode = int32(uiCode)
		}
		kiPredMode := PredIntra4x4Mode(pIntraPredMode, i<<2)

		var iBestMode int8
		if pCurDqLayer.sLayerInfo.pPps.bEntropyCodingModeFlag {
			if iPrevIntra4x4PredMode == -1 {
				iBestMode = int8(kiPredMode)
			} else {
				iBestMode = int8(iPrevIntra4x4PredMode + int32(dsBoolToInt8(iPrevIntra4x4PredMode >= kiPredMode)))
			}
		} else {
			if iPrevIntra4x4PredMode != 0 {
				iBestMode = int8(kiPredMode)
			} else {
				if uiRetTmp := uint32(BsGetBits(pBs, 3, &uiCode)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp)
				}
				iBestMode = int8(uiCode + uint32(dsBoolToInt8(int32(uiCode) >= kiPredMode)))
			}
		}

		iFinalMode = CheckIntraNxNPredMode(iSampleAvail[:], &iBestMode, i<<2, true)

		if iFinalMode == GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INVALID_INTRA4X4_MODE) {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I4x4_PRED_MODE)
		}

		for j := int32(0); j < 4; j++ {
			pCurDqLayer.pIntra4x4FinalMode[iMbXy][g_kuiScan4[(i<<2)+j]] = int8(iFinalMode)
			pIntraPredMode[g_kuiScan8[(i<<2)+j]] = iBestMode
			iSampleAvail[common.G_kuiCache30ScanIdx[(i<<2)+j]] = 1
		}
	}
	copy(pCurDqLayer.pIntraPredMode[iMbXy][0:4], pIntraPredMode[1+8*4:1+8*4+4])
	pCurDqLayer.pIntraPredMode[iMbXy][4] = pIntraPredMode[4+8*1]
	pCurDqLayer.pIntraPredMode[iMbXy][5] = pIntraPredMode[4+8*2]
	pCurDqLayer.pIntraPredMode[iMbXy][6] = pIntraPredMode[4+8*3]

	if pCtx.pSps.uiChromaFormatIdc == 0 {
		return ERR_NONE
	}

	return dsParseIntraChromaPredMode(pCtx, uiNeighAvail, pBs, pCurDqLayer)
}

// int32_t ParseIntra16x16Mode (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail, PBitStringAux
// pBs, PDqLayer pCurDqLayer)
func ParseIntra16x16Mode(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pBs *common.SBitStringAux, pCurDqLayer *SDqLayer) int32 {
	iMbXy := pCurDqLayer.iMbXyIndex
	var uiNeighAvail uint8 //0x07 = 0 1 1 1, means left, top-left, top avail or not. (1: avail, 0: unavail)
	pCtx.pMap16x16NeighToSampleFunc(pNeighAvail, &uiNeighAvail)

	if CheckIntra16x16PredMode(uiNeighAvail,
		&pCurDqLayer.pIntraPredMode[iMbXy][7]) != 0 { //invalid iPredMode, must stop decoding
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_I16x16_PRED_MODE)
	}
	if pCtx.pSps.uiChromaFormatIdc == 0 {
		return ERR_NONE
	}

	return dsParseIntraChromaPredMode(pCtx, uiNeighAvail, pBs, pCurDqLayer)
}

// dsStoreLumaNzc implements the four
// ST32 (&pNzc[4*k], LD32 (&pNonZeroCount[1 + 8 * (k+1)])) stores.
func dsStoreLumaNzc(pNzc []int8, pNonZeroCount []uint8) {
	dsCopyNzc(pNzc, 0, pNonZeroCount, 1+8*1, 4)
	dsCopyNzc(pNzc, 4, pNonZeroCount, 1+8*2, 4)
	dsCopyNzc(pNzc, 8, pNonZeroCount, 1+8*3, 4)
	dsCopyNzc(pNzc, 12, pNonZeroCount, 1+8*4, 4)
}

// dsStoreChromaNzc implements the four ST16 chroma pNzc stores.
func dsStoreChromaNzc(pNzc []int8, pNonZeroCount []uint8) {
	dsCopyNzc(pNzc, 16, pNonZeroCount, 6+8*1, 2)
	dsCopyNzc(pNzc, 20, pNonZeroCount, 6+8*2, 2)
	dsCopyNzc(pNzc, 18, pNonZeroCount, 6+8*4, 2)
	dsCopyNzc(pNzc, 22, pNonZeroCount, 6+8*5, 2)
}

// dsSetChromaQpFromLumaQp: pChromaQp[iMbXy][i] = g_kuiChromaQpTable[CLIP3(qp + offset[i], 0, 51)].
func dsSetChromaQp(pCurDqLayer *SDqLayer, iMbXy int32, iQp int32, pPps *SPps) {
	for i := 0; i < 2; i++ {
		pCurDqLayer.pChromaQp[iMbXy][i] = int8(common.G_kuiChromaQpTable[common.WELS_CLIP3(iQp+
			pPps.iChromaQpIndexOffset[i], 0, 51)])
	}
}

// dsParseResidualCabac is the residual parsing block (from the coefficient
// memset to the chroma AC) that is textually shared by
// WelsDecodeMbCabacISliceBaseMode0, WelsDecodeMbCabacPSliceBaseMode0 and
// WelsDecodeMbCabacBSliceBaseMode0 (for I slices every MB is intra, so the
// IS_INTRA () selections of the P/B variant give the I-slice constants).
func dsParseResidualCabac(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8,
	uiCbpLuma uint32, uiCbpChroma uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBsAux := pCurDqLayer.pBitStringAux
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	iScanIdxStart := int32(pSlice.sSliceHeaderExt.uiScanIdxStart)
	iScanIdxEnd := int32(pSlice.sSliceHeaderExt.uiScanIdxEnd)
	iMbXy := pCurDqLayer.iMbXyIndex
	var iMbResProperty int32
	var i int32

	clear(pCurDqLayer.pScaledTCoeff[iMbXy][:384])

	var iQpDelta, iId8x8, iId4x4 int32

	if uiRetTmp := uint32(ParseDeltaQpCabac(pCtx, &iQpDelta)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	if iQpDelta > 25 || iQpDelta < -26 { //out of iQpDelta range
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_QP)
	}
	pCurDqLayer.pLumaQp[iMbXy] = int8((pSlice.iLastMbQp + iQpDelta + 52) % 52) //update last_mb_qp
	pSlice.iLastMbQp = int32(pCurDqLayer.pLumaQp[iMbXy])
	dsSetChromaQp(pCurDqLayer, iMbXy, pSlice.iLastMbQp, pSliceHeader.pPps)

	pNzc := pCurDqLayer.pNzc[iMbXy][:]
	pTCoeff := pCurDqLayer.pScaledTCoeff[iMbXy][:]
	iScanStart1 := common.WELS_MAX(iScanIdxStart, 1)

	if common.MB_TYPE_INTRA16x16 == pCurDqLayer.pDec.pMbType[iMbXy] {
		//step1: Luma DC
		if uiRetTmp := uint32(ParseResidualBlockCabac(pNeighAvail, pNonZeroCount, pBsAux, 0, 16, g_kuiLumaDcZigzagScan[:],
			I16_LUMA_DC, pTCoeff, uint8(pCurDqLayer.pLumaQp[iMbXy]), pCtx)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		//step2: Luma AC
		if uiCbpLuma != 0 {
			for i = 0; i < 16; i++ {
				if uiRetTmp := uint32(ParseResidualBlockCabac(pNeighAvail, pNonZeroCount, pBsAux, i, iScanIdxEnd-iScanStart1+1,
					g_kuiZigzagScan[iScanStart1:], I16_LUMA_AC, pTCoeff[i<<4:],
					uint8(pCurDqLayer.pLumaQp[iMbXy]), pCtx)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp)
				}
			}
			dsStoreLumaNzc(pNzc, pNonZeroCount)
		} else {
			for k := 0; k < 16; k++ {
				pNzc[k] = 0
			}
		}
	} else { //non-MB_TYPE_INTRA16x16
		if pCtx.pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
			// Transform 8x8 support for CABAC
			for iId8x8 = 0; iId8x8 < 4; iId8x8++ {
				if uiCbpLuma&(1<<iId8x8) != 0 {
					iProp := int32(LUMA_DC_AC_INTER_8)
					if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
						iProp = LUMA_DC_AC_INTRA_8
					}
					if uiRetTmp := uint32(ParseResidualBlockCabac8x8(pNeighAvail, pNonZeroCount, pBsAux, (iId8x8 << 2),
						iScanIdxEnd-iScanIdxStart+1, g_kuiZigzagScan8x8[iScanIdxStart:], iProp,
						pTCoeff[iId8x8<<6:], uint8(pCurDqLayer.pLumaQp[iMbXy]), pCtx)); uiRetTmp != ERR_NONE {
						return int32(uiRetTmp)
					}
				} else {
					common.ST16(pNonZeroCount, int(g_kCacheNzcScanIdx[(iId8x8<<2)]), 0)
					common.ST16(pNonZeroCount, int(g_kCacheNzcScanIdx[(iId8x8<<2)+2]), 0)
				}
			}
			dsStoreLumaNzc(pNzc, pNonZeroCount)
		} else {
			if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
				iMbResProperty = LUMA_DC_AC_INTRA
			} else {
				iMbResProperty = LUMA_DC_AC_INTER
			}
			for iId8x8 = 0; iId8x8 < 4; iId8x8++ {
				if uiCbpLuma&(1<<iId8x8) != 0 {
					iIdx := (iId8x8 << 2)
					for iId4x4 = 0; iId4x4 < 4; iId4x4++ {
						//Luma (DC and AC decoding together)
						if uiRetTmp := uint32(ParseResidualBlockCabac(pNeighAvail, pNonZeroCount, pBsAux, iIdx, iScanIdxEnd-iScanIdxStart+1,
							g_kuiZigzagScan[iScanIdxStart:], iMbResProperty, pTCoeff[iIdx<<4:],
							uint8(pCurDqLayer.pLumaQp[iMbXy]),
							pCtx)); uiRetTmp != ERR_NONE {
							return int32(uiRetTmp)
						}
						iIdx++
					}
				} else {
					common.ST16(pNonZeroCount, int(g_kCacheNzcScanIdx[iId8x8<<2]), 0)
					common.ST16(pNonZeroCount, int(g_kCacheNzcScanIdx[(iId8x8<<2)+2]), 0)
				}
			}
			dsStoreLumaNzc(pNzc, pNonZeroCount)
		}
	}

	//chroma
	//step1: DC
	if 1 == uiCbpChroma || 2 == uiCbpChroma {
		for i = 0; i < 2; i++ {
			if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
				if i != 0 {
					iMbResProperty = CHROMA_DC_V
				} else {
					iMbResProperty = CHROMA_DC_U
				}
			} else {
				if i != 0 {
					iMbResProperty = CHROMA_DC_V_INTER
				} else {
					iMbResProperty = CHROMA_DC_U_INTER
				}
			}

			if uiRetTmp := uint32(ParseResidualBlockCabac(pNeighAvail, pNonZeroCount, pBsAux, 16+(i<<2), 4, g_kuiChromaDcScan[:],
				iMbResProperty, pTCoeff[256+(i<<6):], uint8(pCurDqLayer.pChromaQp[iMbXy][i]), pCtx)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
		}
	}
	//step2: AC
	if 2 == uiCbpChroma {
		for i = 0; i < 2; i++ {
			if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
				if i != 0 {
					iMbResProperty = CHROMA_AC_V
				} else {
					iMbResProperty = CHROMA_AC_U
				}
			} else {
				if i != 0 {
					iMbResProperty = CHROMA_AC_V_INTER
				} else {
					iMbResProperty = CHROMA_AC_U_INTER
				}
			}
			index := 16 + (i << 2)
			for iId4x4 = 0; iId4x4 < 4; iId4x4++ {
				if uiRetTmp := uint32(ParseResidualBlockCabac(pNeighAvail, pNonZeroCount, pBsAux, index,
					iScanIdxEnd-iScanStart1+1, g_kuiZigzagScan[iScanStart1:],
					iMbResProperty, pTCoeff[index<<4:], uint8(pCurDqLayer.pChromaQp[iMbXy][i]), pCtx)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp)
				}
				index++
			}
		}
		dsStoreChromaNzc(pNzc, pNonZeroCount)
	} else {
		for k := 16; k < 24; k++ {
			pNzc[k] = 0
		}
	}
	return ERR_NONE
}

// dsParseEndOfSliceCabac: the ParseEndOfSliceCabac + RestoreCabacDecEngineToBS
// tail of the CABAC MB decoders.
func dsParseEndOfSliceCabac(pCtx *SWelsDecoderContext, uiEosFlag *uint32) int32 {
	if uiRetTmp := uint32(ParseEndOfSliceCabac(pCtx, uiEosFlag)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	if *uiEosFlag != 0 {
		RestoreCabacDecEngineToBS(pCtx.pCabacDecEngine, pCtx.pCurDqLayer.pBitStringAux)
	}
	return ERR_NONE
}

// int32_t WelsDecodeMbCabacISliceBaseMode0 (PWelsDecoderContext pCtx, uint32_t& uiEosFlag)
func WelsDecodeMbCabacISliceBaseMode0(pCtx *SWelsDecoderContext, uiEosFlag *uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBsAux := pCurDqLayer.pBitStringAux
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	var sNeighAvail SWelsNeighAvail
	iMbXy := pCurDqLayer.iMbXyIndex
	var uiMbType, uiCbp, uiCbpLuma, uiCbpChroma uint32

	var pNonZeroCount [48]uint8

	pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
	pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false

	pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
	pCurDqLayer.pResidualPredFlag[iMbXy] = dsBoolToInt8(pSlice.sSliceHeaderExt.bDefaultResidualPredFlag)
	GetNeighborAvailMbType(&sNeighAvail, pCurDqLayer)
	if uiRetTmp := uint32(ParseMBTypeISliceCabac(pCtx, &sNeighAvail, &uiMbType)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	if uiMbType > 25 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE)
	} else if pCtx.pSps.uiChromaFormatIdc == 0 && ((uiMbType >= 5 && uiMbType <= 12) || (uiMbType >= 17 &&
		uiMbType <= 24)) {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE)
	} else if 25 == uiMbType { //I_PCM
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG, "I_PCM mode exists in I slice!")
		if uiRetTmp := uint32(ParseIPCMInfoCabac(pCtx)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		pSlice.iLastDeltaQp = 0
		return dsParseEndOfSliceCabac(pCtx, uiEosFlag)
	} else if 0 == uiMbType { //I4x4
		var pIntraPredMode [48]int8
		pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA4x4
		if pCtx.pPps.bTransform8x8ModeFlag {
			// Transform 8x8 cabac will be added soon
			if uiRetTmp := uint32(ParseTransformSize8x8FlagCabac(pCtx, &sNeighAvail, &pCtx.pCurDqLayer.pTransformSize8x8Flag[iMbXy])); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
		}
		if pCtx.pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
			pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA8x8
			uiMbType = common.MB_TYPE_INTRA8x8
			pCtx.pFillInfoCacheIntraNxNFunc(&sNeighAvail, pNonZeroCount[:], pIntraPredMode[:], pCurDqLayer)
			if uiRetTmp := uint32(ParseIntra8x8Mode(pCtx, &sNeighAvail, pIntraPredMode[:], pBsAux, pCurDqLayer)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
		} else {
			pCtx.pFillInfoCacheIntraNxNFunc(&sNeighAvail, pNonZeroCount[:], pIntraPredMode[:], pCurDqLayer)
			if uiRetTmp := uint32(ParseIntra4x4Mode(pCtx, &sNeighAvail, pIntraPredMode[:], pBsAux, pCurDqLayer)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
		}
		//get uiCbp for I4x4
		if uiRetTmp := uint32(ParseCbpInfoCabac(pCtx, &sNeighAvail, &uiCbp)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		pCurDqLayer.pCbp[iMbXy] = int8(uiCbp)
		if uiCbp == 0 {
			pSlice.iLastDeltaQp = 0
		}
		if pCtx.pSps.uiChromaFormatIdc != 0 {
			uiCbpChroma = uiCbp >> 4
		} else {
			uiCbpChroma = 0
		}
		uiCbpLuma = uiCbp & 15
	} else { //I16x16;
		pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA16x16
		pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false
		pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
		pCurDqLayer.pIntraPredMode[iMbXy][7] = int8((uiMbType - 1) & 3)
		pCurDqLayer.pCbp[iMbXy] = int8(g_kuiI16CbpTable[(uiMbType-1)>>2])
		if pCtx.pSps.uiChromaFormatIdc != 0 {
			uiCbpChroma = uint32(int32(pCurDqLayer.pCbp[iMbXy]) >> 4)
		} else {
			uiCbpChroma = 0
		}
		uiCbpLuma = uint32(int32(pCurDqLayer.pCbp[iMbXy]) & 15)
		WelsFillCacheNonZeroCount(&sNeighAvail, pNonZeroCount[:], pCurDqLayer)
		if uiRetTmp := uint32(ParseIntra16x16Mode(pCtx, &sNeighAvail, pBsAux, pCurDqLayer)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
	}
	_ = uiMbType

	dsZeroNzc(pCurDqLayer.pNzc[iMbXy][:])
	pCurDqLayer.pCbfDc[iMbXy] = 0

	if pCurDqLayer.pCbp[iMbXy] == 0 && common.IS_INTRANxN(pCurDqLayer.pDec.pMbType[iMbXy]) {
		pCurDqLayer.pLumaQp[iMbXy] = int8(pSlice.iLastMbQp)
		dsSetChromaQp(pCurDqLayer, iMbXy, int32(pCurDqLayer.pLumaQp[iMbXy]), pSliceHeader.pPps)
	}

	if pCurDqLayer.pCbp[iMbXy] != 0 || common.MB_TYPE_INTRA16x16 == pCurDqLayer.pDec.pMbType[iMbXy] {
		if iRet := dsParseResidualCabac(pCtx, &sNeighAvail, pNonZeroCount[:], uiCbpLuma, uiCbpChroma); iRet != ERR_NONE {
			return iRet
		}
	} else {
		dsZeroNzc(pCurDqLayer.pNzc[iMbXy][:])
	}

	return dsParseEndOfSliceCabac(pCtx, uiEosFlag)
}

// int32_t WelsDecodeMbCabacISlice (PWelsDecoderContext pCtx, PNalUnit pNalCur, uint32_t& uiEosFlag)
func WelsDecodeMbCabacISlice(pCtx *SWelsDecoderContext, pNalCur *SNalUnit, uiEosFlag *uint32) int32 {
	if uiRetTmp := uint32(WelsDecodeMbCabacISliceBaseMode0(pCtx, uiEosFlag)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	return ERR_NONE
}

// dsDecodeMbCabacIntraInPB is the intra MB branch shared verbatim by
// WelsDecodeMbCabacPSliceBaseMode0 and WelsDecodeMbCabacBSliceBaseMode0
// (uiMbType already has the inter offset removed). bDone reports the I_PCM
// early return.
func dsDecodeMbCabacIntraInPB(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8,
	uiMbType uint32, kpSliceName string, uiEosFlag *uint32, uiCbpLuma *uint32, uiCbpChroma *uint32) (int32, bool) {
	pCurDqLayer := pCtx.pCurDqLayer
	pBsAux := pCurDqLayer.pBitStringAux
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	iMbXy := pCurDqLayer.iMbXyIndex

	if uiMbType > 25 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE), true
	}
	if pCtx.pSps.uiChromaFormatIdc == 0 && ((uiMbType >= 5 && uiMbType <= 12) || (uiMbType >= 17 && uiMbType <= 24)) {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE), true
	}

	if 25 == uiMbType { //I_PCM
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG, "I_PCM mode exists in "+kpSliceName+" slice!")
		if uiRetTmp := uint32(ParseIPCMInfoCabac(pCtx)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp), true
		}
		pSlice.iLastDeltaQp = 0
		return dsParseEndOfSliceCabac(pCtx, uiEosFlag), true
	} else { //normal Intra mode
		if 0 == uiMbType { //Intra4x4
			var pIntraPredMode [48]int8
			pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA4x4
			if pCtx.pPps.bTransform8x8ModeFlag {
				if uiRetTmp := uint32(ParseTransformSize8x8FlagCabac(pCtx, pNeighAvail, &pCtx.pCurDqLayer.pTransformSize8x8Flag[iMbXy])); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp), true
				}
			}
			if pCtx.pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
				pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA8x8
				pCtx.pFillInfoCacheIntraNxNFunc(pNeighAvail, pNonZeroCount, pIntraPredMode[:], pCurDqLayer)
				if uiRetTmp := uint32(ParseIntra8x8Mode(pCtx, pNeighAvail, pIntraPredMode[:], pBsAux, pCurDqLayer)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp), true
				}
			} else {
				pCtx.pFillInfoCacheIntraNxNFunc(pNeighAvail, pNonZeroCount, pIntraPredMode[:], pCurDqLayer)
				if uiRetTmp := uint32(ParseIntra4x4Mode(pCtx, pNeighAvail, pIntraPredMode[:], pBsAux, pCurDqLayer)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp), true
				}
			}
		} else { //Intra16x16
			pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA16x16
			pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false
			pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
			pCurDqLayer.pIntraPredMode[iMbXy][7] = int8((uiMbType - 1) & 3)
			pCurDqLayer.pCbp[iMbXy] = int8(g_kuiI16CbpTable[(uiMbType-1)>>2])
			if pCtx.pSps.uiChromaFormatIdc != 0 {
				*uiCbpChroma = uint32(int32(pCurDqLayer.pCbp[iMbXy]) >> 4)
			} else {
				*uiCbpChroma = 0
			}
			*uiCbpLuma = uint32(int32(pCurDqLayer.pCbp[iMbXy]) & 15)
			WelsFillCacheNonZeroCount(pNeighAvail, pNonZeroCount, pCurDqLayer)
			if uiRetTmp := uint32(ParseIntra16x16Mode(pCtx, pNeighAvail, pBsAux, pCurDqLayer)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp), true
			}
		}
	}
	return ERR_NONE, false
}

// dsDecodeMbCabacPBTail is the common tail (after the MB type parsing) of
// WelsDecodeMbCabacPSliceBaseMode0 and WelsDecodeMbCabacBSliceBaseMode0; the
// two only differ in the bNeedParseTransformSize8x8Flag condition (bBSlice).
func dsDecodeMbCabacPBTail(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, pNonZeroCount []uint8,
	uiCbpLuma uint32, uiCbpChroma uint32, bBSlice bool, uiEosFlag *uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	iMbXy := pCurDqLayer.iMbXyIndex
	var uiCbp uint32

	dsZeroNzc(pCurDqLayer.pNzc[iMbXy][:])

	if common.MB_TYPE_INTRA16x16 != pCurDqLayer.pDec.pMbType[iMbXy] {
		if uiRetTmp := uint32(ParseCbpInfoCabac(pCtx, pNeighAvail, &uiCbp)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}

		pCurDqLayer.pCbp[iMbXy] = int8(uiCbp)
		if uiCbp == 0 {
			pSlice.iLastDeltaQp = 0
		}
		if pCtx.pSps.uiChromaFormatIdc != 0 {
			uiCbpChroma = uint32(int32(pCurDqLayer.pCbp[iMbXy]) >> 4)
		} else {
			uiCbpChroma = 0
		}
		uiCbpLuma = uint32(int32(pCurDqLayer.pCbp[iMbXy]) & 15)
	}

	if pCurDqLayer.pCbp[iMbXy] != 0 || common.MB_TYPE_INTRA16x16 == pCurDqLayer.pDec.pMbType[iMbXy] {

		if common.MB_TYPE_INTRA16x16 != pCurDqLayer.pDec.pMbType[iMbXy] {
			// Need modification when B picutre add in
			kuiMbType := pCurDqLayer.pDec.pMbType[iMbXy]
			var bPartOk bool
			if bBSlice {
				bPartOk = common.IS_INTER_16x16(kuiMbType) || common.IS_DIRECT(kuiMbType) ||
					common.IS_INTER_16x8(kuiMbType) || common.IS_INTER_8x16(kuiMbType)
			} else {
				bPartOk = kuiMbType >= common.MB_TYPE_16x16 && kuiMbType <= common.MB_TYPE_8x16
			}
			bNeedParseTransformSize8x8Flag :=
				(bPartOk || pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy]) &&
					(kuiMbType != common.MB_TYPE_INTRA8x8) &&
					(kuiMbType != common.MB_TYPE_INTRA4x4) &&
					((pCurDqLayer.pCbp[iMbXy] & 0x0F) > 0) &&
					(pCtx.pPps.bTransform8x8ModeFlag)

			if bNeedParseTransformSize8x8Flag {
				if uiRetTmp := uint32(ParseTransformSize8x8FlagCabac(pCtx, pNeighAvail,
					&pCtx.pCurDqLayer.pTransformSize8x8Flag[iMbXy])); uiRetTmp != ERR_NONE { //transform_size_8x8_flag
					return int32(uiRetTmp)
				}
			}
		}

		if iRet := dsParseResidualCabac(pCtx, pNeighAvail, pNonZeroCount, uiCbpLuma, uiCbpChroma); iRet != ERR_NONE {
			return iRet
		}
	} else {
		pCurDqLayer.pLumaQp[iMbXy] = int8(pSlice.iLastMbQp)
		dsSetChromaQp(pCurDqLayer, iMbXy, int32(pCurDqLayer.pLumaQp[iMbXy]), pSliceHeader.pPps)
	}

	return dsParseEndOfSliceCabac(pCtx, uiEosFlag)
}

// int32_t WelsDecodeMbCabacPSliceBaseMode0 (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail,
// uint32_t& uiEosFlag)
func WelsDecodeMbCabacPSliceBaseMode0(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiEosFlag *uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	iMbXy := pCurDqLayer.iMbXyIndex
	var uiMbType, uiCbpLuma, uiCbpChroma uint32

	var pNonZeroCount [48]uint8

	pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0

	if uiRetTmp := uint32(ParseMBTypePSliceCabac(pCtx, pNeighAvail, &uiMbType)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	// uiMbType = 4 is not allowded.
	if uiMbType < 4 { //Inter mode
		var pMotionVector [common.LIST_A][30][common.MV_A]int16
		var pMvdCache [common.LIST_A][30][common.MV_A]int16
		var pRefIndex [common.LIST_A][30]int8
		pCurDqLayer.pDec.pMbType[iMbXy] = g_ksInterPMbTypeInfo[uiMbType].iType
		WelsFillCacheInterCabac(pNeighAvail, pNonZeroCount[:], &pMotionVector, &pMvdCache, &pRefIndex, pCurDqLayer)
		if uiRetTmp := uint32(ParseInterPMotionInfoCabac(pCtx, pNeighAvail, pNonZeroCount[:], &pMotionVector, &pMvdCache, &pRefIndex)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
	} else { //Intra mode
		uiMbType -= 5
		if iRet, bDone := dsDecodeMbCabacIntraInPB(pCtx, pNeighAvail, pNonZeroCount[:], uiMbType, "P", uiEosFlag,
			&uiCbpLuma, &uiCbpChroma); bDone {
			return iRet
		}
	}

	return dsDecodeMbCabacPBTail(pCtx, pNeighAvail, pNonZeroCount[:], uiCbpLuma, uiCbpChroma, false, uiEosFlag)
}

// int32_t WelsDecodeMbCabacBSliceBaseMode0 (PWelsDecoderContext pCtx, PWelsNeighAvail pNeighAvail,
// uint32_t& uiEosFlag)
func WelsDecodeMbCabacBSliceBaseMode0(pCtx *SWelsDecoderContext, pNeighAvail *SWelsNeighAvail, uiEosFlag *uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	iMbXy := pCurDqLayer.iMbXyIndex
	var uiMbType, uiCbpLuma, uiCbpChroma uint32

	var pNonZeroCount [48]uint8

	pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0

	if uiRetTmp := uint32(ParseMBTypeBSliceCabac(pCtx, pNeighAvail, &uiMbType)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}

	if uiMbType < 23 { //Inter B mode
		var pMotionVector [common.LIST_A][30][common.MV_A]int16
		var pMvdCache [common.LIST_A][30][common.MV_A]int16
		var pRefIndex [common.LIST_A][30]int8
		var pDirect [30]int8
		pCurDqLayer.pDec.pMbType[iMbXy] = g_ksInterBMbTypeInfo[uiMbType].iType
		WelsFillCacheInterCabac(pNeighAvail, pNonZeroCount[:], &pMotionVector, &pMvdCache, &pRefIndex, pCurDqLayer)
		WelsFillDirectCacheCabac(pNeighAvail, &pDirect, pCurDqLayer)
		if uiRetTmp := uint32(ParseInterBMotionInfoCabac(pCtx, pNeighAvail, pNonZeroCount[:], &pMotionVector, &pMvdCache, &pRefIndex,
			&pDirect)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
	} else { //Intra mode
		uiMbType -= 23
		if iRet, bDone := dsDecodeMbCabacIntraInPB(pCtx, pNeighAvail, pNonZeroCount[:], uiMbType, "B", uiEosFlag,
			&uiCbpLuma, &uiCbpChroma); bDone {
			return iRet
		}
	}

	return dsDecodeMbCabacPBTail(pCtx, pNeighAvail, pNonZeroCount[:], uiCbpLuma, uiCbpChroma, true, uiEosFlag)
}

// int32_t WelsDecodeMbCabacPSlice (PWelsDecoderContext pCtx, PNalUnit pNalCur, uint32_t& uiEosFlag)
func WelsDecodeMbCabacPSlice(pCtx *SWelsDecoderContext, pNalCur *SNalUnit, uiEosFlag *uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	ppRefPic := &pCtx.sRefPic.pRefList[common.LIST_0]
	var uiCode uint32
	iMbXy := pCurDqLayer.iMbXyIndex
	var i int32
	var uiNeighAvail SWelsNeighAvail
	pCurDqLayer.pCbp[iMbXy] = 0
	pCurDqLayer.pCbfDc[iMbXy] = 0
	pCurDqLayer.pChromaPredMode[iMbXy] = common.C_PRED_DC

	pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
	pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false

	GetNeighborAvailMbType(&uiNeighAvail, pCurDqLayer)
	if uiRetTmp := uint32(ParseSkipFlagCabac(pCtx, &uiNeighAvail, &uiCode)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}

	if uiCode != 0 {
		var pMv [2]int16
		pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_SKIP
		dsZeroNzc(pCurDqLayer.pNzc[iMbXy][:])

		pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
		pCurDqLayer.pDec.pRefIndex[0][iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}
		bIsPending := GetThreadCount(pCtx) > 1
		pCtx.bMbRefConcealed = pCtx.bRPLRError || pCtx.bMbRefConcealed || !(ppRefPic[0] != nil && (ppRefPic[0].bIsComplete ||
			bIsPending))
		//predict mv
		PredPSkipMvFromNeighbor(pCurDqLayer, &pMv)
		for i = 0; i < 16; i++ {
			pCurDqLayer.pDec.pMv[0][iMbXy][i] = pMv
			pCurDqLayer.pMvd[0][iMbXy][i] = [common.MV_A]int16{}
		}

		//reset rS
		pCurDqLayer.pLumaQp[iMbXy] = int8(pSlice.iLastMbQp) //??????????????? dqaunt of previous mb
		dsSetChromaQp(pCurDqLayer, iMbXy, int32(pCurDqLayer.pLumaQp[iMbXy]), pSliceHeader.pPps)

		//for neighboring CABAC usage
		pSlice.iLastDeltaQp = 0

		if uiRetTmp := uint32(ParseEndOfSliceCabac(pCtx, uiEosFlag)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}

		return ERR_NONE
	}

	if uiRetTmp := uint32(WelsDecodeMbCabacPSliceBaseMode0(pCtx, &uiNeighAvail, uiEosFlag)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	return ERR_NONE
}

// int32_t WelsDecodeMbCabacBSlice (PWelsDecoderContext pCtx, PNalUnit pNalCur, uint32_t& uiEosFlag)
func WelsDecodeMbCabacBSlice(pCtx *SWelsDecoderContext, pNalCur *SNalUnit, uiEosFlag *uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	ppRefPicL0 := &pCtx.sRefPic.pRefList[common.LIST_0]
	ppRefPicL1 := &pCtx.sRefPic.pRefList[common.LIST_1]
	var uiCode uint32
	iMbXy := pCurDqLayer.iMbXyIndex
	var uiNeighAvail SWelsNeighAvail
	pCurDqLayer.pCbp[iMbXy] = 0
	pCurDqLayer.pCbfDc[iMbXy] = 0
	pCurDqLayer.pChromaPredMode[iMbXy] = common.C_PRED_DC

	pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
	pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false

	GetNeighborAvailMbType(&uiNeighAvail, pCurDqLayer)
	if uiRetTmp := uint32(ParseSkipFlagCabac(pCtx, &uiNeighAvail, &uiCode)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}

	pCurDqLayer.pDirect[iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}

	bIsPending := GetThreadCount(pCtx) > 1

	if uiCode != 0 {
		var pMv [common.LIST_A][2]int16
		var ref [common.LIST_A]int8
		pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_SKIP | common.MB_TYPE_DIRECT
		dsZeroNzc(pCurDqLayer.pNzc[iMbXy][:])

		pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
		pCurDqLayer.pDec.pRefIndex[common.LIST_0][iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}
		pCurDqLayer.pDec.pRefIndex[common.LIST_1][iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}
		pCtx.bMbRefConcealed = pCtx.bRPLRError || pCtx.bMbRefConcealed || !(ppRefPicL0[0] != nil && (ppRefPicL0[0].bIsComplete ||
			bIsPending)) || !(ppRefPicL1[0] != nil && (ppRefPicL1[0].bIsComplete || bIsPending))

		if pCtx.bMbRefConcealed {
			pLogCtx := &pCtx.sLogCtx
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "Ref Picture for B-Slice is lost, B-Slice decoding cannot be continued!")
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_DATA, ERR_INFO_REFERENCE_PIC_LOST)
		}

		var subMbType SubMbType
		if pSliceHeader.iDirectSpatialMvPredFlag != 0 {

			//predict direct spatial mv
			ret := PredMvBDirectSpatial(pCtx, &pMv, &ref, &subMbType)
			if ret != ERR_NONE {
				return ret
			}
		} else {
			//temporal direct mode
			ret := PredBDirectTemporal(pCtx, &pMv, &ref, &subMbType)
			if ret != ERR_NONE {
				return ret
			}
		}

		//reset rS
		pCurDqLayer.pLumaQp[iMbXy] = int8(pSlice.iLastMbQp) //??????????????? dqaunt of previous mb
		dsSetChromaQp(pCurDqLayer, iMbXy, int32(pCurDqLayer.pLumaQp[iMbXy]), pSliceHeader.pPps)

		//for neighboring CABAC usage
		pSlice.iLastDeltaQp = 0

		if uiRetTmp := uint32(ParseEndOfSliceCabac(pCtx, uiEosFlag)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}

		return ERR_NONE
	}

	if uiRetTmp := uint32(WelsDecodeMbCabacBSliceBaseMode0(pCtx, &uiNeighAvail, uiEosFlag)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	return ERR_NONE
}

// int32_t WelsCalcDeqCoeffScalingList (PWelsDecoderContext pCtx)
//
// Calculate deqaunt coeff scaling list value
func WelsCalcDeqCoeffScalingList(pCtx *SWelsDecoderContext) int32 {
	if pCtx.pSps.bSeqScalingMatrixPresentFlag || pCtx.pPps.bPicScalingMatrixPresentFlag {
		pCtx.bUseScalingList = true

		if !pCtx.bDequantCoeff4x4Init || (pCtx.iDequantCoeffPpsid != pCtx.pPps.iPpsId) {
			var i, q, x, y int
			//Init dequant coeff value for different QP
			for i = 0; i < 6; i++ {
				pCtx.pDequant_coeff4x4[i] = pCtx.pDequant_coeff_buffer4x4[i][:]
				pCtx.pDequant_coeff8x8[i] = pCtx.pDequant_coeff_buffer8x8[i][:]
				for q = 0; q < 51; q++ {
					for x = 0; x < 16; x++ {
						if pCtx.pPps.bPicScalingMatrixPresentFlag {
							pCtx.pDequant_coeff4x4[i][q][x] = uint16(int32(pCtx.pPps.iScalingList4x4[i][x]) *
								int32(common.G_kuiDequantCoeff[q][x&0x07]))
						} else {
							pCtx.pDequant_coeff4x4[i][q][x] = uint16(int32(pCtx.pSps.iScalingList4x4[i][x]) *
								int32(common.G_kuiDequantCoeff[q][x&0x07]))
						}
					}
					for y = 0; y < 64; y++ {
						if pCtx.pPps.bPicScalingMatrixPresentFlag {
							pCtx.pDequant_coeff8x8[i][q][y] = uint16(int32(pCtx.pPps.iScalingList8x8[i][y]) *
								int32(common.G_kuiMatrixV[q%6][y/8][y%8]))
						} else {
							pCtx.pDequant_coeff8x8[i][q][y] = uint16(int32(pCtx.pSps.iScalingList8x8[i][y]) *
								int32(common.G_kuiMatrixV[q%6][y/8][y%8]))
						}
					}
				}
			}
			pCtx.bDequantCoeff4x4Init = true
			pCtx.iDequantCoeffPpsid = pCtx.pPps.iPpsId
		}
	} else {
		pCtx.bUseScalingList = false
	}
	return ERR_NONE
}

// dsInitSliceDecoding is the prologue shared verbatim by WelsDecodeSlice and
// WelsDecodeAndConstructSlice: it selects the MB decoding function and the
// intra neighbour helpers, initialises CABAC and the dequant tables.
// It returns the MB function and the C early-return code (if bRet).
func dsInitSliceDecoding(pCtx *SWelsDecoderContext) (PWelsDecMbFunc, int32, bool) {
	pCurDqLayer := pCtx.pCurDqLayer
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeaderExt := &pSlice.sSliceHeaderExt
	pSliceHeader := &pSliceHeaderExt.sSliceHeader
	var pDecMbFunc PWelsDecMbFunc

	pSlice.iTotalMbInCurSlice = 0 //initialize at the starting of slice decoding.

	if pCtx.pPps.bEntropyCodingModeFlag {
		if pSlice.sSliceHeaderExt.bAdaptiveMotionPredFlag ||
			pSlice.sSliceHeaderExt.bAdaptiveBaseModeFlag ||
			pSlice.sSliceHeaderExt.bAdaptiveResidualPredFlag {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
				"WelsDecodeSlice()::::ILP flag exist, not supported with CABAC enabled!")
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return nil, int32(api.DsBitstreamError), true
		}
		if common.P_SLICE == pSliceHeader.eSliceType {
			pDecMbFunc = WelsDecodeMbCabacPSlice
		} else if common.B_SLICE == pSliceHeader.eSliceType {
			pDecMbFunc = WelsDecodeMbCabacBSlice
		} else { //I_SLICE. B_SLICE is being supported
			pDecMbFunc = WelsDecodeMbCabacISlice
		}
	} else {
		if common.P_SLICE == pSliceHeader.eSliceType {
			pDecMbFunc = WelsDecodeMbCavlcPSlice
		} else if common.B_SLICE == pSliceHeader.eSliceType {
			pDecMbFunc = WelsDecodeMbCavlcBSlice
		} else { //I_SLICE
			pDecMbFunc = WelsDecodeMbCavlcISlice
		}
	}

	if pSliceHeader.pPps.bConstainedIntraPredFlag {
		pCtx.pFillInfoCacheIntraNxNFunc = WelsFillCacheConstrain1IntraNxN
		pCtx.pMapNxNNeighToSampleFunc = WelsMapNxNNeighToSampleConstrain1
		pCtx.pMap16x16NeighToSampleFunc = WelsMap16x16NeighToSampleConstrain1
	} else {
		pCtx.pFillInfoCacheIntraNxNFunc = WelsFillCacheConstrain0IntraNxN
		pCtx.pMapNxNNeighToSampleFunc = WelsMapNxNNeighToSampleNormal
		pCtx.pMap16x16NeighToSampleFunc = WelsMap16x16NeighToSampleNormal
	}

	pCtx.eSliceType = pSliceHeader.eSliceType
	if pCurDqLayer.sLayerInfo.pPps.bEntropyCodingModeFlag {
		iQp := pSlice.sSliceHeaderExt.sSliceHeader.iSliceQp
		iCabacInitIdc := pSlice.sSliceHeaderExt.sSliceHeader.iCabacInitIdc
		WelsCabacContextInit(pCtx, pSlice.eSliceType, iCabacInitIdc, iQp)
		//InitCabacCtx (pCtx->pCabacCtx, pSlice->eSliceType, iCabacInitIdc, iQp);
		pSlice.iLastDeltaQp = 0
		if uiRetTmp := uint32(InitCabacDecEngineFromBS(pCtx.pCabacDecEngine, pCtx.pCurDqLayer.pBitStringAux)); uiRetTmp != ERR_NONE {
			return nil, int32(uiRetTmp), true
		}
	}
	//try to calculate  the dequant_coeff
	WelsCalcDeqCoeffScalingList(pCtx)

	return pDecMbFunc, ERR_NONE, false
}

// int32_t WelsDecodeSlice (PWelsDecoderContext pCtx, bool bFirstSliceInLayer, PNalUnit pNalCur)
func WelsDecodeSlice(pCtx *SWelsDecoderContext, bFirstSliceInLayer bool, pNalCur *SNalUnit) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pFmo := pCtx.pFmo
	var iRet int32
	var iNextMbXyIndex, iSliceIdc int32

	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeaderExt := &pSlice.sSliceHeaderExt
	pSliceHeader := &pSliceHeaderExt.sSliceHeader
	var iMbX, iMbY int32
	kiCountNumMb := int32(pSliceHeader.pSps.uiTotalMbCount) //need to be correct when fmo or multi slice
	var uiEosFlag uint32

	pDecMbFunc, iInitRet, bRet := dsInitSliceDecoding(pCtx)
	if bRet {
		return iInitRet
	}

	iNextMbXyIndex = pSliceHeader.iFirstMbInSlice
	iMbX = iNextMbXyIndex % pCurDqLayer.iMbWidth
	iMbY = iNextMbXyIndex / pCurDqLayer.iMbWidth // error is introduced by multiple slices case, 11/23/2009
	pSlice.iMbSkipRun = -1
	iSliceIdc = (pSliceHeader.iFirstMbInSlice << 7) + int32(pCurDqLayer.uiLayerDqId)

	pCurDqLayer.iMbX = iMbX
	pCurDqLayer.iMbY = iMbY
	pCurDqLayer.iMbXyIndex = iNextMbXyIndex

	for {
		if (-1 == iNextMbXyIndex) || (iNextMbXyIndex >= kiCountNumMb) { // slice group boundary or end of a frame
			break
		}

		pCurDqLayer.pSliceIdc[iNextMbXyIndex] = iSliceIdc
		pCtx.bMbRefConcealed = false
		iRet = pDecMbFunc(pCtx, pNalCur, &uiEosFlag)
		pCurDqLayer.pMbRefConcealedFlag[iNextMbXyIndex] = pCtx.bMbRefConcealed
		if iRet != ERR_NONE {
			return iRet
		}

		pSlice.iTotalMbInCurSlice++
		if uiEosFlag != 0 { //end of slice
			break
		}
		if pSliceHeader.pPps.uiNumSliceGroups > 1 {
			iNextMbXyIndex = FmoNextMb(pFmo, iNextMbXyIndex)
		} else {
			iNextMbXyIndex++
		}
		iMbX = iNextMbXyIndex % pCurDqLayer.iMbWidth
		iMbY = iNextMbXyIndex / pCurDqLayer.iMbWidth
		pCurDqLayer.iMbX = iMbX
		pCurDqLayer.iMbY = iMbY
		pCurDqLayer.iMbXyIndex = iNextMbXyIndex
	}

	return ERR_NONE
}

// int32_t WelsDecodeAndConstructSlice (PWelsDecoderContext pCtx)
//
// Only called by the C++ multi-threaded decoding path. The per-picture pNzc
// array it copies into exists only when threads are used; the copy is
// skipped when it is nil (single-threaded Go port) instead of crashing.
func WelsDecodeAndConstructSlice(pCtx *SWelsDecoderContext) int32 {
	pNalCur := pCtx.pNalCur
	pCurDqLayer := pCtx.pCurDqLayer
	pFmo := pCtx.pFmo
	var iRet int32
	var iNextMbXyIndex, iSliceIdc int32

	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeaderExt := &pSlice.sSliceHeaderExt
	pSliceHeader := &pSliceHeaderExt.sSliceHeader
	var iMbX, iMbY int32
	kiCountNumMb := int32(pSliceHeader.pSps.uiTotalMbCount) //need to be correct when fmo or multi slice
	iTotalMbTargetLayer := kiCountNumMb
	var uiEosFlag uint32

	pDecMbFunc, iInitRet, bRet := dsInitSliceDecoding(pCtx)
	if bRet {
		return iInitRet
	}

	iNextMbXyIndex = pSliceHeader.iFirstMbInSlice
	iMbX = iNextMbXyIndex % pCurDqLayer.iMbWidth
	iMbY = iNextMbXyIndex / pCurDqLayer.iMbWidth // error is introduced by multiple slices case, 11/23/2009
	pSlice.iMbSkipRun = -1
	iSliceIdc = (pSliceHeader.iFirstMbInSlice << 7) + int32(pCurDqLayer.uiLayerDqId)

	pCurDqLayer.iMbX = iMbX
	pCurDqLayer.iMbY = iMbY
	pCurDqLayer.iMbXyIndex = iNextMbXyIndex

	var pDeblockMb PDeblockingFilterMbFunc = WelsDeblockingMb

	var pFilter SDeblockingFilter
	var iFilterIdc int32 = 1
	if pSliceHeader.uiDisableDeblockingFilterIdc != 1 {
		WelsDeblockingInitFilter(pCtx, &pFilter, &iFilterIdc)
	}

	for {
		if (-1 == iNextMbXyIndex) || (iNextMbXyIndex >= kiCountNumMb) { // slice group boundary or end of a frame
			break
		}

		pCurDqLayer.pSliceIdc[iNextMbXyIndex] = iSliceIdc
		pCtx.bMbRefConcealed = false
		iRet = pDecMbFunc(pCtx, pNalCur, &uiEosFlag)
		pCurDqLayer.pMbRefConcealedFlag[iNextMbXyIndex] = pCtx.bMbRefConcealed
		if iRet != ERR_NONE {
			return iRet
		}
		if WelsTargetMbConstruction(pCtx) != 0 {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
				"WelsTargetSliceConstruction():::MB(%d, %d) construction error. pCurSlice_type:%d",
				pCurDqLayer.iMbX, pCurDqLayer.iMbY, pSlice.eSliceType)

			return ERR_INFO_MB_RECON_FAIL
		}
		if pCtx.pDec.pNzc != nil {
			pCtx.pDec.pNzc[pCurDqLayer.iMbXyIndex] = pCurDqLayer.pNzc[pCurDqLayer.iMbXyIndex]
			if pCtx.eSliceType != common.I_SLICE {
				pCtx.sBlockFunc.pWelsSetNonZeroCountFunc(
					pCtx.pDec.pNzc[pCurDqLayer.iMbXyIndex][:]) // set all none-zero nzc to 1; dbk can be opti!
			}
		}
		WelsDeblockingFilterMB(pCurDqLayer, &pFilter, &iFilterIdc, pDeblockMb)
		if pCtx.uiNalRefIdc > 0 {
			if pCurDqLayer.iMbX == 0 || pCurDqLayer.iMbX == pCurDqLayer.iMbWidth-1 || pCurDqLayer.iMbY == 0 ||
				pCurDqLayer.iMbY == pCurDqLayer.iMbHeight-1 {
				pDec := pCurDqLayer.pDec
				common.PadMBLuma_c(pDec.pData[0], pDec.iDataOff[0], pDec.iLinesize[0], pDec.iWidthInPixel,
					pDec.iHeightInPixel, pCurDqLayer.iMbX, pCurDqLayer.iMbY, pCurDqLayer.iMbWidth, pCurDqLayer.iMbHeight)
				common.PadMBChroma_c(pDec.pData[1], pDec.iDataOff[1], pDec.iLinesize[1], pDec.iWidthInPixel/2,
					pDec.iHeightInPixel/2, pCurDqLayer.iMbX, pCurDqLayer.iMbY, pCurDqLayer.iMbWidth,
					pCurDqLayer.iMbHeight)
				common.PadMBChroma_c(pDec.pData[2], pDec.iDataOff[2], pDec.iLinesize[2], pDec.iWidthInPixel/2,
					pDec.iHeightInPixel/2, pCurDqLayer.iMbX, pCurDqLayer.iMbY, pCurDqLayer.iMbWidth,
					pCurDqLayer.iMbHeight)
			}
		}
		if !pCurDqLayer.pMbCorrectlyDecodedFlag[iNextMbXyIndex] { //already con-ed, overwrite
			pCurDqLayer.pMbCorrectlyDecodedFlag[iNextMbXyIndex] = true
			if pCurDqLayer.pMbRefConcealedFlag[iNextMbXyIndex] {
				pCtx.pDec.iMbEcedPropNum++
			}
			pCtx.iTotalNumMbRec++
		}

		if pCtx.iTotalNumMbRec > iTotalMbTargetLayer {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
				"WelsTargetSliceConstruction():::pCtx->iTotalNumMbRec:%d, iTotalMbTargetLayer:%d",
				pCtx.iTotalNumMbRec, iTotalMbTargetLayer)

			return ERR_INFO_MB_NUM_EXCEED_FAIL
		}

		pSlice.iTotalMbInCurSlice++
		if uiEosFlag != 0 { //end of slice
			// SET_EVENT (&pCtx->pDec->pReadyEvent[pCurDqLayer->iMbY]): no threads.
			break
		}
		if pSliceHeader.pPps.uiNumSliceGroups > 1 {
			iNextMbXyIndex = FmoNextMb(pFmo, iNextMbXyIndex)
		} else {
			iNextMbXyIndex++
		}
		iMbX = iNextMbXyIndex % pCurDqLayer.iMbWidth
		iMbY = iNextMbXyIndex / pCurDqLayer.iMbWidth
		pCurDqLayer.iMbX = iMbX
		pCurDqLayer.iMbY = iMbY
		pCurDqLayer.iMbXyIndex = iNextMbXyIndex
		// GetThreadCount (pCtx) > 1: SET_EVENT on finished MB rows (no threads).
	}
	return ERR_NONE
}

// dsParseIPcmCavlc is the CAVLC I_PCM block shared verbatim by
// WelsActualDecodeMbCavlcISlice / PSlice / BSlice.
func dsParseIPcmCavlc(pCtx *SWelsDecoderContext, kpSliceName string) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBs := pCurDqLayer.pBitStringAux
	iMbX := pCurDqLayer.iMbX
	iMbY := pCurDqLayer.iMbY
	iMbXy := pCurDqLayer.iMbXyIndex
	pNzc := pCurDqLayer.pNzc[iMbXy][:]

	common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG, "I_PCM mode exists in "+kpSliceName+" slice!")
	iDecStrideL := pCurDqLayer.pDec.iLinesize[0]
	iDecStrideC := pCurDqLayer.pDec.iLinesize[1]

	iOffsetL := (iMbX + iMbY*iDecStrideL) << 4
	iOffsetC := (iMbX + iMbY*iDecStrideC) << 3

	pDec := pCurDqLayer.pDec
	pDecY := pDec.iDataOff[0] + int(iOffsetL)
	pDecU := pDec.iDataOff[1] + int(iOffsetC)
	pDecV := pDec.iDataOff[2] + int(iOffsetC)

	var pTmpBsBuf int

	var i int32
	iCopySizeY := 1 << 4
	iCopySizeUV := 1 << 3

	iIndex := ((-pBs.ILeftBits) >> 3) + 2

	pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA_PCM

	//step 1: locating bit-stream pointer [must align into integer byte]
	pBs.PCurBuf -= int(iIndex)

	//bounds check: I_PCM copies I_PCM_MB_SIZE_IN_BYTE (256 luma + 128 chroma) bytes
	//directly from the bitstream buffer; reject when fewer bytes remain to avoid
	//an out-of-bounds read (mirrors ParseIPCMInfoCabac).
	if pBs.PEndBuf-pBs.PCurBuf < I_PCM_MB_SIZE_IN_BYTE {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_BS_INCOMPLETE)
	}

	//step 2: copy pixel from bit-stream into fdec [reconstruction]
	pTmpBsBuf = pBs.PCurBuf
	if !pCtx.pParam.BParseOnly {
		for i = 0; i < 16; i++ { //luma
			copy(pDec.pData[0][pDecY:pDecY+iCopySizeY], pBs.PBuf[pTmpBsBuf:pTmpBsBuf+iCopySizeY])
			pDecY += int(iDecStrideL)
			pTmpBsBuf += 16
		}
		for i = 0; i < 8; i++ { //cb
			copy(pDec.pData[1][pDecU:pDecU+iCopySizeUV], pBs.PBuf[pTmpBsBuf:pTmpBsBuf+iCopySizeUV])
			pDecU += int(iDecStrideC)
			pTmpBsBuf += 8
		}
		for i = 0; i < 8; i++ { //cr
			copy(pDec.pData[2][pDecV:pDecV+iCopySizeUV], pBs.PBuf[pTmpBsBuf:pTmpBsBuf+iCopySizeUV])
			pDecV += int(iDecStrideC)
			pTmpBsBuf += 8
		}
	}

	pBs.PCurBuf += I_PCM_MB_SIZE_IN_BYTE

	//step 3: update QP and pNonZeroCount
	pCurDqLayer.pLumaQp[iMbXy] = 0
	pCurDqLayer.pChromaQp[iMbXy][0] = 0
	pCurDqLayer.pChromaQp[iMbXy][1] = 0
	//Rec. 9.2.1 for PCM, nzc=16
	for k := 0; k < 24; k++ {
		pNzc[k] = 16
	}
	if uiRetTmp := uint32(InitReadBits(pBs, 0)); uiRetTmp != ERR_NONE {
		return int32(uiRetTmp)
	}
	return ERR_NONE
}

// dsParseResidualCavlc is the CAVLC residual block (mb_qp_delta through
// BsEndCavlc) shared verbatim by WelsActualDecodeMbCavlcISlice / PSlice /
// BSlice (for I slices every MB is intra, so the IS_INTRA () selections of
// the P/B variant give the I-slice constants).
func dsParseResidualCavlc(pCtx *SWelsDecoderContext, pNonZeroCount []uint8, uiCbpL uint32, uiCbpC uint32) int32 {
	pVlcTable := pCtx.pVlcTable
	pCurDqLayer := pCtx.pCurDqLayer
	pBs := pCurDqLayer.pBitStringAux
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader

	iScanIdxStart := int32(pSlice.sSliceHeaderExt.uiScanIdxStart)
	iScanIdxEnd := int32(pSlice.sSliceHeaderExt.uiScanIdxEnd)
	iScanStart1 := common.WELS_MAX(iScanIdxStart, 1)

	iMbXy := pCurDqLayer.iMbXyIndex
	pNzc := pCurDqLayer.pNzc[iMbXy][:]
	pTCoeff := pCurDqLayer.pScaledTCoeff[iMbXy][:]
	var i int32
	var iRet int32
	var iCode int32
	var iMbResProperty int32

	var iQpDelta, iId8x8, iId4x4 int32
	clear(pCurDqLayer.pScaledTCoeff[iMbXy][:common.MB_COEFF_LIST_SIZE])
	if uiRetTmp := uint32(BsGetSe(pBs, &iCode)); uiRetTmp != ERR_NONE { //mb_qp_delta
		return int32(uiRetTmp)
	}
	iQpDelta = iCode

	if iQpDelta > 25 || iQpDelta < -26 { //out of iQpDelta range
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_QP)
	}

	pCurDqLayer.pLumaQp[iMbXy] = int8((pSlice.iLastMbQp + iQpDelta + 52) % 52) //update last_mb_qp
	pSlice.iLastMbQp = int32(pCurDqLayer.pLumaQp[iMbXy])
	dsSetChromaQp(pCurDqLayer, iMbXy, pSlice.iLastMbQp, pSliceHeader.pPps)

	BsStartCavlc(pBs)

	if common.MB_TYPE_INTRA16x16 == pCurDqLayer.pDec.pMbType[iMbXy] {
		//step1: Luma DC
		if iRet = WelsResidualBlockCavlc(pVlcTable, pNonZeroCount, pBs, 0, 16, g_kuiLumaDcZigzagScan[:], I16_LUMA_DC,
			pTCoeff, uint8(pCurDqLayer.pLumaQp[iMbXy]), pCtx); iRet != ERR_NONE {
			return iRet //abnormal
		}
		//step2: Luma AC
		if uiCbpL != 0 {
			for i = 0; i < 16; i++ {
				if iRet = WelsResidualBlockCavlc(pVlcTable, pNonZeroCount, pBs, i, iScanIdxEnd-iScanStart1+1,
					g_kuiZigzagScan[iScanStart1:], I16_LUMA_AC, pTCoeff[i<<4:],
					uint8(pCurDqLayer.pLumaQp[iMbXy]), pCtx); iRet != ERR_NONE {
					return iRet //abnormal
				}
			}
			dsStoreLumaNzc(pNzc, pNonZeroCount)
		}
	} else { //non-MB_TYPE_INTRA16x16
		if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
			for iId8x8 = 0; iId8x8 < 4; iId8x8++ {
				if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
					iMbResProperty = LUMA_DC_AC_INTRA_8
				} else {
					iMbResProperty = LUMA_DC_AC_INTER_8
				}
				if uiCbpL&(1<<iId8x8) != 0 {
					iIndex := (iId8x8 << 2)
					for iId4x4 = 0; iId4x4 < 4; iId4x4++ {
						if iRet = WelsResidualBlockCavlc8x8(pVlcTable, pNonZeroCount, pBs, iIndex, iScanIdxEnd-iScanIdxStart+1,
							g_kuiZigzagScan8x8[iScanIdxStart:], iMbResProperty, pTCoeff[iId8x8<<6:], iId4x4,
							uint8(pCurDqLayer.pLumaQp[iMbXy]), pCtx); iRet != ERR_NONE {
							return iRet
						}
						iIndex++
					}
				} else {
					common.ST16(pNonZeroCount, int(common.G_kuiCache48CountScan4Idx[iId8x8<<2]), 0)
					common.ST16(pNonZeroCount, int(common.G_kuiCache48CountScan4Idx[(iId8x8<<2)+2]), 0)
				}
			}
			dsStoreLumaNzc(pNzc, pNonZeroCount)
		} else { // Normal T4x4
			for iId8x8 = 0; iId8x8 < 4; iId8x8++ {
				if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
					iMbResProperty = LUMA_DC_AC_INTRA
				} else {
					iMbResProperty = LUMA_DC_AC_INTER
				}
				if uiCbpL&(1<<iId8x8) != 0 {
					iIndex := (iId8x8 << 2)
					for iId4x4 = 0; iId4x4 < 4; iId4x4++ {
						//Luma (DC and AC decoding together)
						if iRet = WelsResidualBlockCavlc(pVlcTable, pNonZeroCount, pBs, iIndex, iScanIdxEnd-iScanIdxStart+1,
							g_kuiZigzagScan[iScanIdxStart:], iMbResProperty, pTCoeff[iIndex<<4:],
							uint8(pCurDqLayer.pLumaQp[iMbXy]), pCtx); iRet != ERR_NONE {
							return iRet //abnormal
						}
						iIndex++
					}
				} else {
					common.ST16(pNonZeroCount, int(common.G_kuiCache48CountScan4Idx[iId8x8<<2]), 0)
					common.ST16(pNonZeroCount, int(common.G_kuiCache48CountScan4Idx[(iId8x8<<2)+2]), 0)
				}
			}
			dsStoreLumaNzc(pNzc, pNonZeroCount)
		}
	}

	//chroma
	//step1: DC
	if 1 == uiCbpC || 2 == uiCbpC {
		for i = 0; i < 2; i++ { //Cb Cr
			if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
				if i != 0 {
					iMbResProperty = CHROMA_DC_V
				} else {
					iMbResProperty = CHROMA_DC_U
				}
			} else {
				if i != 0 {
					iMbResProperty = CHROMA_DC_V_INTER
				} else {
					iMbResProperty = CHROMA_DC_U_INTER
				}
			}

			if iRet = WelsResidualBlockCavlc(pVlcTable, pNonZeroCount, pBs, 16+(i<<2), 4, g_kuiChromaDcScan[:], iMbResProperty,
				pTCoeff[256+(i<<6):], uint8(pCurDqLayer.pChromaQp[iMbXy][i]), pCtx); iRet != ERR_NONE {
				return iRet //abnormal
			}
		}
	}
	//step2: AC
	if 2 == uiCbpC {
		for i = 0; i < 2; i++ { //Cb Cr
			if common.IS_INTRA(pCurDqLayer.pDec.pMbType[iMbXy]) {
				if i != 0 {
					iMbResProperty = CHROMA_AC_V
				} else {
					iMbResProperty = CHROMA_AC_U
				}
			} else {
				if i != 0 {
					iMbResProperty = CHROMA_AC_V_INTER
				} else {
					iMbResProperty = CHROMA_AC_U_INTER
				}
			}

			iIndex := 16 + (i << 2)
			for iId4x4 = 0; iId4x4 < 4; iId4x4++ {
				if iRet = WelsResidualBlockCavlc(pVlcTable, pNonZeroCount, pBs, iIndex, iScanIdxEnd-iScanStart1+1,
					g_kuiZigzagScan[iScanStart1:], iMbResProperty,
					pTCoeff[iIndex<<4:],
					uint8(pCurDqLayer.pChromaQp[iMbXy][i]), pCtx); iRet != ERR_NONE {
					return iRet //abnormal
				}
				iIndex++
			}
		}
		dsStoreChromaNzc(pNzc, pNonZeroCount)
	}
	BsEndCavlc(pBs)

	return ERR_NONE
}

// int32_t WelsActualDecodeMbCavlcISlice (PWelsDecoderContext pCtx)
func WelsActualDecodeMbCavlcISlice(pCtx *SWelsDecoderContext) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBs := pCurDqLayer.pBitStringAux
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader

	var sNeighAvail SWelsNeighAvail

	iMbXy := pCurDqLayer.iMbXyIndex
	pNzc := pCurDqLayer.pNzc[iMbXy][:]
	var uiMbType, uiCbp, uiCbpL, uiCbpC uint32
	var uiCode uint32

	var pNonZeroCount [48]uint8
	GetNeighborAvailMbType(&sNeighAvail, pCurDqLayer)
	pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
	pCurDqLayer.pResidualPredFlag[iMbXy] = dsBoolToInt8(pSlice.sSliceHeaderExt.bDefaultResidualPredFlag)

	pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
	pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false

	if uiRetTmp := BsGetUe(pBs, &uiCode); uiRetTmp != ERR_NONE { //uiMbType
		return int32(uiRetTmp)
	}
	uiMbType = uiCode
	if uiMbType > 25 {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE)
	}
	if pCtx.pSps.uiChromaFormatIdc == 0 && ((uiMbType >= 5 && uiMbType <= 12) || (uiMbType >= 17 && uiMbType <= 24)) {
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE)
	}

	if 25 == uiMbType {
		return dsParseIPcmCavlc(pCtx, "I")
	} else if 0 == uiMbType { //reference to JM
		var pIntraPredMode [48]int8
		pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA4x4
		if pCtx.pPps.bTransform8x8ModeFlag {
			if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE { //transform_size_8x8_flag
				return int32(uiRetTmp)
			}
			pCurDqLayer.pTransformSize8x8Flag[iMbXy] = uiCode != 0
			if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
				pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA8x8
			}
		}
		if !pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
			pCtx.pFillInfoCacheIntraNxNFunc(&sNeighAvail, pNonZeroCount[:], pIntraPredMode[:], pCurDqLayer)
			if uiRetTmp := uint32(ParseIntra4x4Mode(pCtx, &sNeighAvail, pIntraPredMode[:], pBs, pCurDqLayer)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
		} else {
			pCtx.pFillInfoCacheIntraNxNFunc(&sNeighAvail, pNonZeroCount[:], pIntraPredMode[:], pCurDqLayer)
			if uiRetTmp := uint32(ParseIntra8x8Mode(pCtx, &sNeighAvail, pIntraPredMode[:], pBs, pCurDqLayer)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
		}

		//uiCbp
		if uiRetTmp := BsGetUe(pBs, &uiCode); uiRetTmp != ERR_NONE { //coded_block_pattern
			return int32(uiRetTmp)
		}
		uiCbp = uiCode
		//G.9.1 Alternative parsing process for coded pBlock pattern
		if pCtx.pSps.uiChromaFormatIdc != 0 && (uiCbp > 47) {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_CBP)
		}
		if pCtx.pSps.uiChromaFormatIdc == 0 && (uiCbp > 15) {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_CBP)
		}

		if pCtx.pSps.uiChromaFormatIdc != 0 {
			uiCbp = uint32(g_kuiIntra4x4CbpTable[uiCbp])
		} else {
			uiCbp = uint32(g_kuiIntra4x4CbpTable400[uiCbp])
		}
		pCurDqLayer.pCbp[iMbXy] = int8(uiCbp)
		uiCbpC = uiCbp >> 4
		uiCbpL = uiCbp & 15
	} else { //I_PCM exclude, we can ignore it
		pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA16x16
		pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false
		pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
		pCurDqLayer.pIntraPredMode[iMbXy][7] = int8((uiMbType - 1) & 3)
		pCurDqLayer.pCbp[iMbXy] = int8(g_kuiI16CbpTable[(uiMbType-1)>>2])
		if pCtx.pSps.uiChromaFormatIdc != 0 {
			uiCbpC = uint32(int32(pCurDqLayer.pCbp[iMbXy]) >> 4)
		} else {
			uiCbpC = 0
		}
		uiCbpL = uint32(int32(pCurDqLayer.pCbp[iMbXy]) & 15)
		WelsFillCacheNonZeroCount(&sNeighAvail, pNonZeroCount[:], pCurDqLayer)
		if uiRetTmp := uint32(ParseIntra16x16Mode(pCtx, &sNeighAvail, pBs, pCurDqLayer)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
	}

	dsZeroNzc(pNzc)

	if pCurDqLayer.pCbp[iMbXy] == 0 && common.IS_INTRANxN(pCurDqLayer.pDec.pMbType[iMbXy]) {
		pCurDqLayer.pLumaQp[iMbXy] = int8(pSlice.iLastMbQp)
		dsSetChromaQp(pCurDqLayer, iMbXy, int32(pCurDqLayer.pLumaQp[iMbXy]), pSliceHeader.pPps)
	}

	if pCurDqLayer.pCbp[iMbXy] != 0 || common.MB_TYPE_INTRA16x16 == pCurDqLayer.pDec.pMbType[iMbXy] {
		if iRet := dsParseResidualCavlc(pCtx, pNonZeroCount[:], uiCbpL, uiCbpC); iRet != ERR_NONE {
			return iRet
		}
	}

	return ERR_NONE
}

// dsCheckCavlcSliceEnd is the "check whether there is left bits to read
// next time in case multiple slices" tail of the CAVLC MB decoders.
func dsCheckCavlcSliceEnd(pCtx *SWelsDecoderContext, uiEosFlag *uint32, kpFuncName string) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBs := pCurDqLayer.pBitStringAux
	var iUsedBits int64
	iUsedBits = (int64(pBs.PCurBuf-pBs.PStartBuf) << 3) - int64(16-pBs.ILeftBits)
	// sub 1, for stop bit
	if (iUsedBits == int64(pBs.IBits-1)) && (0 >= pCurDqLayer.sLayerInfo.sSliceInLayer.iMbSkipRun) { // slice boundary
		*uiEosFlag = 1
	}
	if iUsedBits > int64(pBs.IBits-1) { //When BS incomplete, as long as find it, SHOULD stop decoding to avoid mosaic or crash.
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
			kpFuncName+"()::::pBs incomplete, iUsedBits:%d > pBs->iBits:%d, MUST stop decoding.",
			iUsedBits, pBs.IBits)
		return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_BS_INCOMPLETE)
	}
	return ERR_NONE
}

// int32_t WelsDecodeMbCavlcISlice (PWelsDecoderContext pCtx, PNalUnit pNalCur, uint32_t& uiEosFlag)
func WelsDecodeMbCavlcISlice(pCtx *SWelsDecoderContext, pNalCur *SNalUnit, uiEosFlag *uint32) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBs := pCurDqLayer.pBitStringAux
	pSliceHeaderExt := &pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt
	var iBaseModeFlag int32
	var iRet int32 //should have the return value to indicate decoding error or not, It's NECESSARY--2010.4.15
	var uiCode uint32
	if pSliceHeaderExt.bAdaptiveBaseModeFlag {
		if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE { //base_mode_flag
			return int32(uiRetTmp)
		}
		iBaseModeFlag = int32(uiCode)
	} else {
		iBaseModeFlag = int32(dsBoolToInt8(pSliceHeaderExt.bDefaultBaseModeFlag))
	}
	if iBaseModeFlag == 0 {
		iRet = WelsActualDecodeMbCavlcISlice(pCtx)
	} else {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "iBaseModeFlag (%d) != 0, inter-layer prediction not supported.",
			iBaseModeFlag)
		return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_ILP)
	}
	if iRet != 0 { //occur error when parsing, MUST STOP decoding
		return iRet
	}

	return dsCheckCavlcSliceEnd(pCtx, uiEosFlag, "WelsDecodeMbCavlcISlice")
}

// dsActualDecodeMbCavlcPB is the body shared verbatim by
// WelsActualDecodeMbCavlcPSlice and WelsActualDecodeMbCavlcBSlice; the two
// only differ in the inter MB type range, the MB type table and the inter
// parsing function (bBSlice).
func dsActualDecodeMbCavlcPB(pCtx *SWelsDecoderContext, bBSlice bool) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBs := pCurDqLayer.pBitStringAux
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader

	var sNeighAvail SWelsNeighAvail
	iMbXy := pCurDqLayer.iMbXyIndex
	pNzc := pCurDqLayer.pNzc[iMbXy][:]
	var iRet int32
	var uiMbType, uiCbp, uiCbpL, uiCbpC uint32
	var uiCode uint32

	var uiInterTypeNum uint32 = 5
	kpSliceName := "P"
	if bBSlice {
		uiInterTypeNum = 23
		kpSliceName = "B"
	}

	GetNeighborAvailMbType(&sNeighAvail, pCurDqLayer)
	var pNonZeroCount [48]uint8
	pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0              //2009.10.23
	if uiRetTmp := BsGetUe(pBs, &uiCode); uiRetTmp != ERR_NONE { //uiMbType
		return int32(uiRetTmp)
	}
	uiMbType = uiCode
	if uiMbType < uiInterTypeNum { //inter MB type
		var iMotionVector [common.LIST_A][30][common.MV_A]int16
		var iRefIndex [common.LIST_A][30]int8
		if bBSlice {
			pCurDqLayer.pDec.pMbType[iMbXy] = g_ksInterBMbTypeInfo[uiMbType].iType
		} else {
			pCurDqLayer.pDec.pMbType[iMbXy] = g_ksInterPMbTypeInfo[uiMbType].iType
		}
		WelsFillCacheInter(&sNeighAvail, pNonZeroCount[:], &iMotionVector, &iRefIndex, pCurDqLayer)

		if bBSlice {
			iRet = ParseInterBInfo(pCtx, &iMotionVector, &iRefIndex, pBs)
		} else {
			iRet = ParseInterInfo(pCtx, &iMotionVector, &iRefIndex, pBs)
		}
		if iRet != ERR_NONE {
			return iRet //abnormal
		}

		if pSlice.sSliceHeaderExt.bAdaptiveResidualPredFlag {
			if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE { //residual_prediction_flag
				return int32(uiRetTmp)
			}
			pCurDqLayer.pResidualPredFlag[iMbXy] = int8(uiCode)
		} else {
			pCurDqLayer.pResidualPredFlag[iMbXy] = dsBoolToInt8(pSlice.sSliceHeaderExt.bDefaultResidualPredFlag)
		}

		if pCurDqLayer.pResidualPredFlag[iMbXy] == 0 {
			pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
		} else {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "residual_pred_flag = 1 not supported.")
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_UNSUPPORTED_ILP)
		}
	} else { //intra MB type
		uiMbType -= uiInterTypeNum
		if uiMbType > 25 {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE)
		}
		if pCtx.pSps.uiChromaFormatIdc == 0 && ((uiMbType >= 5 && uiMbType <= 12) || (uiMbType >= 17 && uiMbType <= 24)) {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_TYPE)
		}

		if 25 == uiMbType {
			return dsParseIPcmCavlc(pCtx, kpSliceName)
		} else {
			if 0 == uiMbType {
				var pIntraPredMode [48]int8
				pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA4x4
				if pCtx.pPps.bTransform8x8ModeFlag {
					if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE { //transform_size_8x8_flag
						return int32(uiRetTmp)
					}
					pCurDqLayer.pTransformSize8x8Flag[iMbXy] = uiCode != 0
					if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
						pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA8x8
					}
				}
				if !pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
					pCtx.pFillInfoCacheIntraNxNFunc(&sNeighAvail, pNonZeroCount[:], pIntraPredMode[:], pCurDqLayer)
					if uiRetTmp := uint32(ParseIntra4x4Mode(pCtx, &sNeighAvail, pIntraPredMode[:], pBs, pCurDqLayer)); uiRetTmp != ERR_NONE {
						return int32(uiRetTmp)
					}
				} else {
					pCtx.pFillInfoCacheIntraNxNFunc(&sNeighAvail, pNonZeroCount[:], pIntraPredMode[:], pCurDqLayer)
					if uiRetTmp := uint32(ParseIntra8x8Mode(pCtx, &sNeighAvail, pIntraPredMode[:], pBs, pCurDqLayer)); uiRetTmp != ERR_NONE {
						return int32(uiRetTmp)
					}
				}
			} else { //I_PCM exclude, we can ignore it
				pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_INTRA16x16
				pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false
				pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
				pCurDqLayer.pIntraPredMode[iMbXy][7] = int8((uiMbType - 1) & 3)
				pCurDqLayer.pCbp[iMbXy] = int8(g_kuiI16CbpTable[(uiMbType-1)>>2])
				if pCtx.pSps.uiChromaFormatIdc != 0 {
					uiCbpC = uint32(int32(pCurDqLayer.pCbp[iMbXy]) >> 4)
				} else {
					uiCbpC = 0
				}
				uiCbpL = uint32(int32(pCurDqLayer.pCbp[iMbXy]) & 15)
				WelsFillCacheNonZeroCount(&sNeighAvail, pNonZeroCount[:], pCurDqLayer)
				if iRet = ParseIntra16x16Mode(pCtx, &sNeighAvail, pBs, pCurDqLayer); iRet != ERR_NONE {
					return iRet
				}
			}
		}
	}

	if common.MB_TYPE_INTRA16x16 != pCurDqLayer.pDec.pMbType[iMbXy] {
		if uiRetTmp := BsGetUe(pBs, &uiCode); uiRetTmp != ERR_NONE { //coded_block_pattern
			return int32(uiRetTmp)
		}
		uiCbp = uiCode
		{
			if pCtx.pSps.uiChromaFormatIdc != 0 && (uiCbp > 47) {
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_CBP)
			}
			if pCtx.pSps.uiChromaFormatIdc == 0 && (uiCbp > 15) {
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_CBP)
			}
			if common.MB_TYPE_INTRA4x4 == pCurDqLayer.pDec.pMbType[iMbXy] || common.MB_TYPE_INTRA8x8 == pCurDqLayer.pDec.pMbType[iMbXy] {
				if pCtx.pSps.uiChromaFormatIdc != 0 {
					uiCbp = uint32(g_kuiIntra4x4CbpTable[uiCbp])
				} else {
					uiCbp = uint32(g_kuiIntra4x4CbpTable400[uiCbp])
				}
			} else { //inter
				if pCtx.pSps.uiChromaFormatIdc != 0 {
					uiCbp = uint32(g_kuiInterCbpTable[uiCbp])
				} else {
					uiCbp = uint32(g_kuiInterCbpTable400[uiCbp])
				}
			}
		}

		pCurDqLayer.pCbp[iMbXy] = int8(uiCbp)
		uiCbpC = uint32(int32(pCurDqLayer.pCbp[iMbXy]) >> 4)
		uiCbpL = uint32(int32(pCurDqLayer.pCbp[iMbXy]) & 15)

		// Need modification when B picutre add in
		kuiMbType := pCurDqLayer.pDec.pMbType[iMbXy]
		bNeedParseTransformSize8x8Flag :=
			((kuiMbType >= common.MB_TYPE_16x16 && kuiMbType <= common.MB_TYPE_8x16) ||
				pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy]) &&
				(kuiMbType != common.MB_TYPE_INTRA8x8) &&
				(kuiMbType != common.MB_TYPE_INTRA4x4) &&
				(uiCbpL > 0) &&
				(pCtx.pPps.bTransform8x8ModeFlag)

		if bNeedParseTransformSize8x8Flag {
			if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE { //transform_size_8x8_flag
				return int32(uiRetTmp)
			}
			pCurDqLayer.pTransformSize8x8Flag[iMbXy] = uiCode != 0
		}
	}

	dsZeroNzc(pNzc)
	if pCurDqLayer.pCbp[iMbXy] == 0 && !common.IS_INTRA16x16(pCurDqLayer.pDec.pMbType[iMbXy]) &&
		!common.IS_I_BL(pCurDqLayer.pDec.pMbType[iMbXy]) {
		pCurDqLayer.pLumaQp[iMbXy] = int8(pSlice.iLastMbQp)
		dsSetChromaQp(pCurDqLayer, iMbXy, int32(pCurDqLayer.pLumaQp[iMbXy]), pSliceHeader.pPps)
	}

	if pCurDqLayer.pCbp[iMbXy] != 0 || common.MB_TYPE_INTRA16x16 == pCurDqLayer.pDec.pMbType[iMbXy] {
		if iRet = dsParseResidualCavlc(pCtx, pNonZeroCount[:], uiCbpL, uiCbpC); iRet != ERR_NONE {
			return iRet
		}
	}

	return ERR_NONE
}

// int32_t WelsActualDecodeMbCavlcPSlice (PWelsDecoderContext pCtx)
func WelsActualDecodeMbCavlcPSlice(pCtx *SWelsDecoderContext) int32 {
	return dsActualDecodeMbCavlcPB(pCtx, false)
}

// dsDecodeMbCavlcPB is the body shared verbatim by WelsDecodeMbCavlcPSlice
// and WelsDecodeMbCavlcBSlice (bBSlice selects the B-specific parts).
func dsDecodeMbCavlcPB(pCtx *SWelsDecoderContext, pNalCur *SNalUnit, uiEosFlag *uint32, bBSlice bool) int32 {
	pCurDqLayer := pCtx.pCurDqLayer
	pBs := pCurDqLayer.pBitStringAux
	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	ppRefPicL0 := &pCtx.sRefPic.pRefList[common.LIST_0]
	ppRefPicL1 := &pCtx.sRefPic.pRefList[common.LIST_1]
	iMbXy := pCurDqLayer.iMbXyIndex
	pNzc := pCurDqLayer.pNzc[iMbXy][:]
	var iBaseModeFlag int32
	var iRet int32 //should have the return value to indicate decoding error or not, It's NECESSARY--2010.4.15
	var uiCode uint32

	pCurDqLayer.pNoSubMbPartSizeLessThan8x8Flag[iMbXy] = true
	pCurDqLayer.pTransformSize8x8Flag[iMbXy] = false

	if -1 == pSlice.iMbSkipRun {
		if uiRetTmp := BsGetUe(pBs, &uiCode); uiRetTmp != ERR_NONE { //mb_skip_run
			return int32(uiRetTmp)
		}
		pSlice.iMbSkipRun = int32(uiCode)
		if -1 == pSlice.iMbSkipRun {
			return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_SKIP_RUN)
		}
		if bBSlice {
			if uint32(pSlice.iMbSkipRun) > uint32(pCurDqLayer.iMbWidth*pCurDqLayer.iMbHeight-iMbXy) {
				return GENERATE_ERROR_NO(ERR_LEVEL_MB_DATA, ERR_INFO_INVALID_MB_SKIP_RUN)
			}
		}
	}
	bSkip := pSlice.iMbSkipRun != 0
	pSlice.iMbSkipRun--
	if bSkip {
		dsZeroNzc(pNzc)
		pCurDqLayer.pInterPredictionDoneFlag[iMbXy] = 0
		if !bBSlice {
			var iMv [2]int16

			pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_SKIP

			pCurDqLayer.pDec.pRefIndex[0][iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}
			bIsPending := GetThreadCount(pCtx) > 1
			pCtx.bMbRefConcealed = pCtx.bRPLRError || pCtx.bMbRefConcealed || !(ppRefPicL0[0] != nil && (ppRefPicL0[0].bIsComplete ||
				bIsPending))
			//predict iMv
			PredPSkipMvFromNeighbor(pCurDqLayer, &iMv)
			for i := 0; i < 16; i++ {
				pCurDqLayer.pDec.pMv[0][iMbXy][i] = iMv
			}
		} else {
			var iMv [common.LIST_A][2]int16
			var ref [common.LIST_A]int8

			pCurDqLayer.pDec.pMbType[iMbXy] = common.MB_TYPE_SKIP | common.MB_TYPE_DIRECT

			pCurDqLayer.pDec.pRefIndex[common.LIST_0][iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}
			pCurDqLayer.pDec.pRefIndex[common.LIST_1][iMbXy] = [common.MB_BLOCK4x4_NUM]int8{}
			bIsPending := GetThreadCount(pCtx) > 1
			pCtx.bMbRefConcealed = pCtx.bRPLRError || pCtx.bMbRefConcealed || !(ppRefPicL0[0] != nil && (ppRefPicL0[0].bIsComplete ||
				bIsPending)) || !(ppRefPicL1[0] != nil && (ppRefPicL1[0].bIsComplete || bIsPending))

			//predict iMv
			var subMbType SubMbType
			if pSliceHeader.iDirectSpatialMvPredFlag != 0 {

				//predict direct spatial mv
				ret := PredMvBDirectSpatial(pCtx, &iMv, &ref, &subMbType)
				if ret != ERR_NONE {
					return ret
				}
			} else {
				//temporal direct mode
				ret := PredBDirectTemporal(pCtx, &iMv, &ref, &subMbType)
				if ret != ERR_NONE {
					return ret
				}
			}
		}

		//reset rS
		if !pSlice.sSliceHeaderExt.bDefaultResidualPredFlag ||
			(pNalCur.sNalHeaderExt.UiQualityId == 0 && pNalCur.sNalHeaderExt.UiDependencyId == 0) {
			pCurDqLayer.pLumaQp[iMbXy] = int8(pSlice.iLastMbQp)
			dsSetChromaQp(pCurDqLayer, iMbXy, int32(pCurDqLayer.pLumaQp[iMbXy]), pSliceHeader.pPps)
		}

		pCurDqLayer.pCbp[iMbXy] = 0
	} else {
		if pSlice.sSliceHeaderExt.bAdaptiveBaseModeFlag {
			if uiRetTmp := BsGetOneBit(pBs, &uiCode); uiRetTmp != ERR_NONE { //base_mode_flag
				return int32(uiRetTmp)
			}
			iBaseModeFlag = int32(uiCode)
		} else {
			iBaseModeFlag = int32(dsBoolToInt8(pSlice.sSliceHeaderExt.bDefaultBaseModeFlag))
		}
		if iBaseModeFlag == 0 {
			if bBSlice {
				iRet = WelsActualDecodeMbCavlcBSlice(pCtx)
			} else {
				iRet = WelsActualDecodeMbCavlcPSlice(pCtx)
			}
		} else {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "iBaseModeFlag (%d) != 0, inter-layer prediction not supported.",
				iBaseModeFlag)
			return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_HEADER, ERR_INFO_UNSUPPORTED_ILP)
		}
		if iRet != 0 { //occur error when parsing, MUST STOP decoding
			return iRet
		}
	}
	// check whether there is left bits to read next time in case multiple slices
	if bBSlice {
		return dsCheckCavlcSliceEnd(pCtx, uiEosFlag, "WelsDecodeMbCavlcBSlice")
	}
	return dsCheckCavlcSliceEnd(pCtx, uiEosFlag, "WelsDecodeMbCavlcISlice")
}

// int32_t WelsDecodeMbCavlcPSlice (PWelsDecoderContext pCtx, PNalUnit pNalCur, uint32_t& uiEosFlag)
func WelsDecodeMbCavlcPSlice(pCtx *SWelsDecoderContext, pNalCur *SNalUnit, uiEosFlag *uint32) int32 {
	return dsDecodeMbCavlcPB(pCtx, pNalCur, uiEosFlag, false)
}

// int32_t WelsDecodeMbCavlcBSlice (PWelsDecoderContext pCtx, PNalUnit pNalCur, uint32_t& uiEosFlag)
func WelsDecodeMbCavlcBSlice(pCtx *SWelsDecoderContext, pNalCur *SNalUnit, uiEosFlag *uint32) int32 {
	return dsDecodeMbCavlcPB(pCtx, pNalCur, uiEosFlag, true)
}

// int32_t WelsActualDecodeMbCavlcBSlice (PWelsDecoderContext pCtx)
func WelsActualDecodeMbCavlcBSlice(pCtx *SWelsDecoderContext) int32 {
	return dsActualDecodeMbCavlcPB(pCtx, true)
}

// void WelsBlockFuncInit (SBlockFunc* pFunc, int32_t iCpu)
func WelsBlockFuncInit(pFunc *SBlockFunc, iCpu int32) {
	pFunc.pWelsSetNonZeroCountFunc = common.WelsNonZeroCount_c
	pFunc.pWelsBlockZero16x16Func = WelsBlockZero16x16_c
	pFunc.pWelsBlockZero8x8Func = WelsBlockZero8x8_c
}

// void WelsBlockInit (int16_t* pBlock, int iW, int iH, int iStride, uint8_t uiVal)
//
// pBlock: coefficient sub-slice; iStride counts int16 elements.
func WelsBlockInit(pBlock []int16, iW int32, iH int32, iStride int32, uiVal uint8) {
	var i int32
	pDst := 0
	// memset fills every byte of the int16_t elements with uiVal.
	kiVal := int16(uint16(uiVal)<<8 | uint16(uiVal))

	for i = 0; i < iH; i++ {
		for k := 0; k < int(iW); k++ {
			pBlock[pDst+k] = kiVal
		}
		pDst += int(iStride)
	}
}

// void WelsBlockZero16x16_c (int16_t* pBlock, int32_t iStride)
func WelsBlockZero16x16_c(pBlock []int16, iStride int32) {
	WelsBlockInit(pBlock, 16, 16, iStride, 0)
}

// void WelsBlockZero8x8_c (int16_t* pBlock, int32_t iStride)
func WelsBlockZero8x8_c(pBlock []int16, iStride int32) {
	WelsBlockInit(pBlock, 8, 8, iStride, 0)
}

// bool ComputeColocatedTemporalScaling (PWelsDecoderContext pCtx)
//
// Compute the temporal-direct scaling factor that's common
// to all direct MBs in this slice, as per clause 8.4.1.2.3
// of T-REC H.264 201704
func ComputeColocatedTemporalScaling(pCtx *SWelsDecoderContext) bool {
	pCurDqLayer := pCtx.pCurDqLayer
	pCurSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pCurSlice.sSliceHeaderExt.sSliceHeader
	if pSliceHeader.iDirectSpatialMvPredFlag == 0 {
		uiRefCount := uint32(pSliceHeader.uiRefCount[common.LIST_0])
		if pCtx.sRefPic.pRefList[common.LIST_1][0] != nil {
			for i := uint32(0); i < uiRefCount; i++ {
				if pCtx.sRefPic.pRefList[common.LIST_0][i] != nil {
					poc0 := pCtx.sRefPic.pRefList[common.LIST_0][i].iFramePoc
					poc1 := pCtx.sRefPic.pRefList[common.LIST_1][0].iFramePoc
					poc := pSliceHeader.iPicOrderCntLsb
					td := common.WELS_CLIP3(poc1-poc0, -128, 127)
					if td == 0 {
						pCurSlice.iMvScale[common.LIST_0][i] = 1 << 8
					} else {
						tb := common.WELS_CLIP3(poc-poc0, -128, 127)
						var iAbsTd int32 = td
						if iAbsTd < 0 {
							iAbsTd = -iAbsTd
						}
						tx := (16384 + (iAbsTd >> 1)) / td
						pCurSlice.iMvScale[common.LIST_0][i] = int16(common.WELS_CLIP3((tb*tx+32)>>6, -1024, 1023))
					}
				}
			}
		}
	}
	return true
}
