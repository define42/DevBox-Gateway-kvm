// Port of codec/decoder/core/src/rec_mb.cpp.
//
// Reconstruction of intra macroblocks, motion compensation driving
// (BaseMC / GetInterPred / GetInterBPred), weighted prediction and
// residual add.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// WelsFillRecNeededMbInfo ports void WelsFillRecNeededMbInfo (PWelsDecoderContext pCtx, bool bOutput, PDqLayer pCurDqLayer).
func WelsFillRecNeededMbInfo(pCtx *SWelsDecoderContext, bOutput bool, pCurDqLayer *SDqLayer) {
	pCurPic := pCtx.pDec
	iLumaStride := pCurPic.iLinesize[0]
	iChromaStride := pCurPic.iLinesize[1]
	iMbX := pCurDqLayer.iMbX
	iMbY := pCurDqLayer.iMbY

	pCurDqLayer.iLumaStride = iLumaStride
	pCurDqLayer.iChromaStride = iChromaStride

	if bOutput {
		pCurDqLayer.pPred[0] = pCurPic.pData[0]
		pCurDqLayer.iPredOff[0] = pCurPic.iDataOff[0] + int((iMbY*iLumaStride+iMbX)<<4)
		pCurDqLayer.pPred[1] = pCurPic.pData[1]
		pCurDqLayer.iPredOff[1] = pCurPic.iDataOff[1] + int((iMbY*iChromaStride+iMbX)<<3)
		pCurDqLayer.pPred[2] = pCurPic.pData[2]
		pCurDqLayer.iPredOff[2] = pCurPic.iDataOff[2] + int((iMbY*iChromaStride+iMbX)<<3)
	}
}

// RecI8x8Mb ports int32_t RecI8x8Mb (int32_t iMbXy, PWelsDecoderContext pCtx, int16_t* pScoeffLevel, PDqLayer pDqLayer).
//
// pScoeffLevel: coefficient sub-slice (pScaledTCoeff[iMbXy][:]).
func RecI8x8Mb(iMbXy int32, pCtx *SWelsDecoderContext, pScoeffLevel []int16, pDqLayer *SDqLayer) int32 {
	RecI8x8Luma(iMbXy, pCtx, pScoeffLevel, pDqLayer)
	RecI4x4Chroma(iMbXy, pCtx, pScoeffLevel, pDqLayer)
	return ERR_NONE
}

// RecI8x8Luma ports int32_t RecI8x8Luma (int32_t iMbXy, PWelsDecoderContext pCtx, int16_t* pScoeffLevel, PDqLayer pDqLayer).
func RecI8x8Luma(iMbXy int32, pCtx *SWelsDecoderContext, pScoeffLevel []int16, pDqLayer *SDqLayer) int32 {
	/*****get local variable from outer variable********/
	/*prediction info*/
	pPred := pDqLayer.pPred[0]
	iPredOff := pDqLayer.iPredOff[0]

	iLumaStride := pDqLayer.iLumaStride
	pBlockOffset := &pCtx.iDecBlockOffsetArray
	pGetI8x8LumaPredFunc := &pCtx.pGetI8x8LumaPredFunc

	pIntra8x8PredMode := &pDqLayer.pIntra4x4FinalMode[iMbXy] // I_NxN
	pRS := pScoeffLevel
	/*itransform info*/
	pIdctResAddPredFunc := pCtx.pIdctResAddPredFunc8x8

	/*************local variable********************/
	var bTLAvail, bTRAvail [4]bool
	uiAvail := pDqLayer.pIntraNxNAvailFlag[iMbXy]
	// Top-Right : Left : Top-Left : Top
	bTLAvail[0] = (uiAvail & 0x02) != 0
	bTLAvail[1] = (uiAvail & 0x01) != 0
	bTLAvail[2] = (uiAvail & 0x04) != 0
	bTLAvail[3] = true

	bTRAvail[0] = (uiAvail & 0x01) != 0
	bTRAvail[1] = (uiAvail & 0x08) != 0
	bTRAvail[2] = true
	bTRAvail[3] = false

	pNzc := &pDqLayer.pNzc[iMbXy]

	/*************real process*********************/
	for i := 0; i < 4; i++ {
		iPredI8x8Off := iPredOff + int(pBlockOffset[i<<2])
		uiMode := uint8(pIntra8x8PredMode[g_kuiScan4[i<<2]])

		pGetI8x8LumaPredFunc[uiMode](pPred, iPredI8x8Off, iLumaStride, bTLAvail[i], bTRAvail[i])

		iIndex := int(common.G_kuiMbCountScan4Idx[i<<2])
		if pNzc[iIndex] != 0 || pNzc[iIndex+1] != 0 || pNzc[iIndex+4] != 0 || pNzc[iIndex+5] != 0 {
			pRSI8x8 := pRS[i<<6:]
			pIdctResAddPredFunc(pPred, iPredI8x8Off, iLumaStride, pRSI8x8)
		}
	}

	return ERR_NONE
}

// RecI4x4Mb ports int32_t RecI4x4Mb (int32_t iMBXY, PWelsDecoderContext pCtx, int16_t* pScoeffLevel, PDqLayer pDqLayer).
func RecI4x4Mb(iMBXY int32, pCtx *SWelsDecoderContext, pScoeffLevel []int16, pDqLayer *SDqLayer) int32 {
	RecI4x4Luma(iMBXY, pCtx, pScoeffLevel, pDqLayer)
	RecI4x4Chroma(iMBXY, pCtx, pScoeffLevel, pDqLayer)
	return ERR_NONE
}

// RecI4x4Luma ports int32_t RecI4x4Luma (int32_t iMBXY, PWelsDecoderContext pCtx, int16_t* pScoeffLevel, PDqLayer pDqLayer).
func RecI4x4Luma(iMBXY int32, pCtx *SWelsDecoderContext, pScoeffLevel []int16, pDqLayer *SDqLayer) int32 {
	/*****get local variable from outer variable********/
	/*prediction info*/
	pPred := pDqLayer.pPred[0]
	iPredOff := pDqLayer.iPredOff[0]

	iLumaStride := pDqLayer.iLumaStride
	pBlockOffset := &pCtx.iDecBlockOffsetArray
	pGetI4x4LumaPredFunc := &pCtx.pGetI4x4LumaPredFunc

	pIntra4x4PredMode := &pDqLayer.pIntra4x4FinalMode[iMBXY]
	pRS := pScoeffLevel
	/*itransform info*/
	pIdctResAddPredFunc := pCtx.pIdctResAddPredFunc

	pNzc := &pDqLayer.pNzc[iMBXY]

	/*************real process*********************/
	for i := 0; i < 16; i++ {
		iPredI4x4Off := iPredOff + int(pBlockOffset[i])
		uiMode := uint8(pIntra4x4PredMode[g_kuiScan4[i]])

		pGetI4x4LumaPredFunc[uiMode](pPred, iPredI4x4Off, iLumaStride)

		if pNzc[common.G_kuiMbCountScan4Idx[i]] != 0 {
			pRSI4x4 := pRS[i<<4:]
			pIdctResAddPredFunc(pPred, iPredI4x4Off, iLumaStride, pRSI4x4)
		}
	}

	return ERR_NONE
}

// RecI4x4Chroma ports int32_t RecI4x4Chroma (int32_t iMBXY, PWelsDecoderContext pCtx, int16_t* pScoeffLevel, PDqLayer pDqLayer).
func RecI4x4Chroma(iMBXY int32, pCtx *SWelsDecoderContext, pScoeffLevel []int16, pDqLayer *SDqLayer) int32 {
	iChromaStride := pCtx.pCurDqLayer.pDec.iLinesize[1]

	iChromaPredMode := pDqLayer.pChromaPredMode[iMBXY]

	pGetIChromaPredFunc := &pCtx.pGetIChromaPredFunc

	pGetIChromaPredFunc[iChromaPredMode](pDqLayer.pPred[1], pDqLayer.iPredOff[1], iChromaStride)
	pGetIChromaPredFunc[iChromaPredMode](pDqLayer.pPred[2], pDqLayer.iPredOff[2], iChromaStride)

	RecChroma(iMBXY, pCtx, pScoeffLevel, pDqLayer)

	return ERR_NONE
}

// RecI16x16Mb ports int32_t RecI16x16Mb (int32_t iMBXY, PWelsDecoderContext pCtx, int16_t* pScoeffLevel, PDqLayer pDqLayer).
func RecI16x16Mb(iMBXY int32, pCtx *SWelsDecoderContext, pScoeffLevel []int16, pDqLayer *SDqLayer) int32 {
	/*decoder use, encoder no use*/
	iI16x16PredMode := pDqLayer.pIntraPredMode[iMBXY][7]
	iChromaPredMode := pDqLayer.pChromaPredMode[iMBXY]
	pGetIChromaPredFunc := &pCtx.pGetIChromaPredFunc
	pGetI16x16LumaPredFunc := &pCtx.pGetI16x16LumaPredFunc
	iUVStride := pCtx.pCurDqLayer.pDec.iLinesize[1]

	/*common use by decoder&encoder*/
	iYStride := pDqLayer.iLumaStride
	pRS := pScoeffLevel

	pPred := pDqLayer.pPred[0]
	iPredOff := pDqLayer.iPredOff[0]

	pIdctFourResAddPredFunc := pCtx.pIdctFourResAddPredFunc

	/*decode i16x16 y*/
	pGetI16x16LumaPredFunc[iI16x16PredMode](pPred, iPredOff, iYStride)

	/*1 mb is divided 16 4x4_block to idct*/
	pNzc := pDqLayer.pNzc[iMBXY][:]
	ys := int(iYStride)
	pIdctFourResAddPredFunc(pPred, iPredOff+0*ys+0, iYStride, pRS[0*64:], pNzc[0:])
	pIdctFourResAddPredFunc(pPred, iPredOff+0*ys+8, iYStride, pRS[1*64:], pNzc[2:])
	pIdctFourResAddPredFunc(pPred, iPredOff+8*ys+0, iYStride, pRS[2*64:], pNzc[8:])
	pIdctFourResAddPredFunc(pPred, iPredOff+8*ys+8, iYStride, pRS[3*64:], pNzc[10:])

	/*decode intra mb cb&cr*/
	pGetIChromaPredFunc[iChromaPredMode](pDqLayer.pPred[1], pDqLayer.iPredOff[1], iUVStride)
	pGetIChromaPredFunc[iChromaPredMode](pDqLayer.pPred[2], pDqLayer.iPredOff[2], iUVStride)
	RecChroma(iMBXY, pCtx, pScoeffLevel, pDqLayer)

	return ERR_NONE
}

// GetRefPic: according to current 8*8 block ref_index to gain reference picture.
func GetRefPic(pMCRefMem *sMCRefMember, pCtx *SWelsDecoderContext, iRefIdx int8, listIdx int32) int32 {
	if iRefIdx >= 0 && int(iRefIdx) < len(pCtx.sRefPic.pRefList[listIdx]) {
		pRefPic := pCtx.sRefPic.pRefList[listIdx][iRefIdx]

		if pRefPic != nil {
			pMCRefMem.iSrcLineLuma = pRefPic.iLinesize[0]
			pMCRefMem.iSrcLineChroma = pRefPic.iLinesize[1]

			pMCRefMem.pSrcY = pRefPic.pData[0]
			pMCRefMem.iSrcYOff = pRefPic.iDataOff[0]
			pMCRefMem.pSrcU = pRefPic.pData[1]
			pMCRefMem.iSrcUOff = pRefPic.iDataOff[1]
			pMCRefMem.pSrcV = pRefPic.pData[2]
			pMCRefMem.iSrcVOff = pRefPic.iDataOff[2]
			if pMCRefMem.pSrcY == nil || pMCRefMem.pSrcU == nil || pMCRefMem.pSrcV == nil {
				return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_DATA, ERR_INFO_REFERENCE_PIC_LOST)
			}
			return ERR_NONE
		}
	}
	return GENERATE_ERROR_NO(ERR_LEVEL_SLICE_DATA, ERR_INFO_REFERENCE_PIC_LOST)
}

// BaseMC ports void BaseMC (PWelsDecoderContext pCtx, sMCRefMember* pMCRefMem, const int32_t& listIdx, const
// int8_t& iRefIdx, int32_t iXOffset, int32_t iYOffset, SMcFunc* pMCFunc, int32_t iBlkWidth, int32_t
// iBlkHeight, int16_t iMVs[2]).
//
// const C++ references -> values. The multi-threaded reference wait is not
// ported (GetThreadCount(pCtx) is always <= 1).
func BaseMC(pCtx *SWelsDecoderContext, pMCRefMem *sMCRefMember, listIdx int32, iRefIdx int8, iXOffset int32, iYOffset int32, pMCFunc *common.SMcFunc, iBlkWidth int32, iBlkHeight int32, iMVs *[2]int16) {
	iFullMVx := (iXOffset << 2) + int32(iMVs[0]) //quarter pixel
	iFullMVy := (iYOffset << 2) + int32(iMVs[1])
	iFullMVx = common.WELS_CLIP3(iFullMVx, ((-common.PADDING_LENGTH + 2) * (1 << 2)),
		((pMCRefMem.iPicWidth + common.PADDING_LENGTH - 19) * (1 << 2)))
	iFullMVy = common.WELS_CLIP3(iFullMVy, ((-common.PADDING_LENGTH + 2) * (1 << 2)),
		((pMCRefMem.iPicHeight + common.PADDING_LENGTH - 19) * (1 << 2)))

	iSrcPixOffsetLuma := (iFullMVx >> 2) + (iFullMVy>>2)*pMCRefMem.iSrcLineLuma
	iSrcPixOffsetChroma := (iFullMVx >> 3) + (iFullMVy>>3)*pMCRefMem.iSrcLineChroma

	iBlkWidthChroma := iBlkWidth >> 1
	iBlkHeightChroma := iBlkHeight >> 1

	iSrcYOff := pMCRefMem.iSrcYOff + int(iSrcPixOffsetLuma)
	iSrcUOff := pMCRefMem.iSrcUOff + int(iSrcPixOffsetChroma)
	iSrcVOff := pMCRefMem.iSrcVOff + int(iSrcPixOffsetChroma)

	pMCFunc.PMcLumaFunc(pMCRefMem.pSrcY, iSrcYOff, pMCRefMem.iSrcLineLuma, pMCRefMem.pDstY, pMCRefMem.iDstYOff,
		pMCRefMem.iDstLineLuma, int16(iFullMVx), int16(iFullMVy), iBlkWidth, iBlkHeight)
	pMCFunc.PMcChromaFunc(pMCRefMem.pSrcU, iSrcUOff, pMCRefMem.iSrcLineChroma, pMCRefMem.pDstU, pMCRefMem.iDstUOff,
		pMCRefMem.iDstLineChroma, int16(iFullMVx), int16(iFullMVy), iBlkWidthChroma, iBlkHeightChroma)
	pMCFunc.PMcChromaFunc(pMCRefMem.pSrcV, iSrcVOff, pMCRefMem.iSrcLineChroma, pMCRefMem.pDstV, pMCRefMem.iDstVOff,
		pMCRefMem.iDstLineChroma, int16(iFullMVx), int16(iFullMVy), iBlkWidthChroma, iBlkHeightChroma)
}

func WeightPrediction(pCurDqLayer *SDqLayer, pMCRefMem *sMCRefMember, listIdx int32, iRefIdx int32,
	iBlkWidth int32, iBlkHeight int32) {
	var iLog2denom, iWoc, iOoc int32
	var iPredTemp, iLineStride int32
	pWt := pCurDqLayer.pPredWeightTable
	//luma
	iLog2denom = int32(pWt.uiLumaLog2WeightDenom)
	iWoc = pWt.sPredList[listIdx].iLumaWeight[iRefIdx]
	iOoc = pWt.sPredList[listIdx].iLumaOffset[iRefIdx]
	iLineStride = pMCRefMem.iDstLineLuma

	pDstY := pMCRefMem.pDstY
	for i := int32(0); i < iBlkHeight; i++ {
		for j := int32(0); j < iBlkWidth; j++ {
			iPixel := pMCRefMem.iDstYOff + int(j+i*iLineStride)
			if iLog2denom >= 1 {
				iPredTemp = ((int32(pDstY[iPixel])*iWoc + (1 << (iLog2denom - 1))) >> iLog2denom) + iOoc
			} else {
				iPredTemp = int32(pDstY[iPixel])*iWoc + iOoc
			}
			pDstY[iPixel] = uint8(common.WELS_CLIP3(iPredTemp, 0, 255))
		}
	}

	//UV
	iBlkWidth = iBlkWidth >> 1
	iBlkHeight = iBlkHeight >> 1
	iLog2denom = int32(pWt.uiChromaLog2WeightDenom)
	iLineStride = pMCRefMem.iDstLineChroma

	for k := 0; k < 2; k++ {
		iWoc = pWt.sPredList[listIdx].iChromaWeight[iRefIdx][k]
		iOoc = pWt.sPredList[listIdx].iChromaOffset[iRefIdx][k]
		pDst, iDstOff := pMCRefMem.pDstU, pMCRefMem.iDstUOff
		if k != 0 {
			pDst, iDstOff = pMCRefMem.pDstV, pMCRefMem.iDstVOff
		}

		for i := int32(0); i < iBlkHeight; i++ {
			for j := int32(0); j < iBlkWidth; j++ {
				iPixel := iDstOff + int(j+i*iLineStride)
				if iLog2denom >= 1 {
					iPredTemp = ((int32(pDst[iPixel])*iWoc + (1 << (iLog2denom - 1))) >> iLog2denom) + iOoc
				} else {
					iPredTemp = int32(pDst[iPixel])*iWoc + iOoc
				}
				pDst[iPixel] = uint8(common.WELS_CLIP3(iPredTemp, 0, 255))
			}
		}
	}
}

func BiWeightPrediction(pCurDqLayer *SDqLayer, pMCRefMem *sMCRefMember, pTempMCRefMem *sMCRefMember,
	iRefIdx1 int32, iRefIdx2 int32, bWeightedBipredIdcIs1 bool, iBlkWidth int32, iBlkHeight int32) {
	var iWoc1, iOoc1, iWoc2, iOoc2 int32
	var iPredTemp, iLineStride int32
	pWt := pCurDqLayer.pPredWeightTable
	//luma
	iLog2denom := int32(pWt.uiLumaLog2WeightDenom)
	if bWeightedBipredIdcIs1 {
		iWoc1 = pWt.sPredList[common.LIST_0].iLumaWeight[iRefIdx1]
		iOoc1 = pWt.sPredList[common.LIST_0].iLumaOffset[iRefIdx1]
		iWoc2 = pWt.sPredList[common.LIST_1].iLumaWeight[iRefIdx2]
		iOoc2 = pWt.sPredList[common.LIST_1].iLumaOffset[iRefIdx2]
	} else {
		iWoc1 = pWt.iImplicitWeight[iRefIdx1][iRefIdx2]
		iWoc2 = 64 - iWoc1
	}
	iLineStride = pMCRefMem.iDstLineLuma

	for i := int32(0); i < iBlkHeight; i++ {
		for j := int32(0); j < iBlkWidth; j++ {
			iPixel := int(j + i*iLineStride)
			a := pMCRefMem.iDstYOff + iPixel
			b := pTempMCRefMem.iDstYOff + iPixel
			iPredTemp = ((int32(pMCRefMem.pDstY[a])*iWoc1 + int32(pTempMCRefMem.pDstY[b])*iWoc2 + (1 << iLog2denom)) >>
				(iLog2denom + 1)) + ((iOoc1 + iOoc2 + 1) >> 1)
			pMCRefMem.pDstY[a] = uint8(common.WELS_CLIP3(iPredTemp, 0, 255))
		}
	}

	//UV
	iBlkWidth = iBlkWidth >> 1
	iBlkHeight = iBlkHeight >> 1
	iLog2denom = int32(pWt.uiChromaLog2WeightDenom)
	iLineStride = pMCRefMem.iDstLineChroma

	for k := 0; k < 2; k++ {
		if bWeightedBipredIdcIs1 {
			iWoc1 = pWt.sPredList[common.LIST_0].iChromaWeight[iRefIdx1][k]
			iOoc1 = pWt.sPredList[common.LIST_0].iChromaOffset[iRefIdx1][k]
			iWoc2 = pWt.sPredList[common.LIST_1].iChromaWeight[iRefIdx2][k]
			iOoc2 = pWt.sPredList[common.LIST_1].iChromaOffset[iRefIdx2][k]
		}
		pDst, iDstOff := pMCRefMem.pDstU, pMCRefMem.iDstUOff
		pTempDst, iTempDstOff := pTempMCRefMem.pDstU, pTempMCRefMem.iDstUOff
		if k != 0 {
			pDst, iDstOff = pMCRefMem.pDstV, pMCRefMem.iDstVOff
			pTempDst, iTempDstOff = pTempMCRefMem.pDstV, pTempMCRefMem.iDstVOff
		}

		for i := int32(0); i < iBlkHeight; i++ {
			for j := int32(0); j < iBlkWidth; j++ {
				iPixel := int(j + i*iLineStride)
				iPredTemp = ((int32(pDst[iDstOff+iPixel])*iWoc1 + int32(pTempDst[iTempDstOff+iPixel])*iWoc2 + (1 << iLog2denom)) >>
					(iLog2denom + 1)) + ((iOoc1 + iOoc2 + 1) >> 1)
				pDst[iDstOff+iPixel] = uint8(common.WELS_CLIP3(iPredTemp, 0, 255))
			}
		}
	}
}

func BiPrediction(pCurDqLayer *SDqLayer, pMCRefMem *sMCRefMember, pTempMCRefMem *sMCRefMember, iBlkWidth int32,
	iBlkHeight int32) {
	var iPredTemp, iLineStride int32
	//luma
	iLineStride = pMCRefMem.iDstLineLuma

	for i := int32(0); i < iBlkHeight; i++ {
		for j := int32(0); j < iBlkWidth; j++ {
			iPixel := int(j + i*iLineStride)
			a := pMCRefMem.iDstYOff + iPixel
			b := pTempMCRefMem.iDstYOff + iPixel
			iPredTemp = (int32(pMCRefMem.pDstY[a]) + int32(pTempMCRefMem.pDstY[b]) + 1) >> 1
			pMCRefMem.pDstY[a] = uint8(common.WELS_CLIP3(iPredTemp, 0, 255))
		}
	}

	//UV
	iBlkWidth = iBlkWidth >> 1
	iBlkHeight = iBlkHeight >> 1
	iLineStride = pMCRefMem.iDstLineChroma

	for k := 0; k < 2; k++ {
		pDst, iDstOff := pMCRefMem.pDstU, pMCRefMem.iDstUOff
		pTempDst, iTempDstOff := pTempMCRefMem.pDstU, pTempMCRefMem.iDstUOff
		if k != 0 {
			pDst, iDstOff = pMCRefMem.pDstV, pMCRefMem.iDstVOff
			pTempDst, iTempDstOff = pTempMCRefMem.pDstV, pTempMCRefMem.iDstVOff
		}

		for i := int32(0); i < iBlkHeight; i++ {
			for j := int32(0); j < iBlkWidth; j++ {
				iPixel := int(j + i*iLineStride)
				iPredTemp = (int32(pDst[iDstOff+iPixel]) + int32(pTempDst[iTempDstOff+iPixel]) + 1) >> 1
				pDst[iDstOff+iPixel] = uint8(common.WELS_CLIP3(iPredTemp, 0, 255))
			}
		}
	}
}

// setDst sets the three destination (slice, offset) pairs of an sMCRefMember.
func (m *sMCRefMember) setDst(pY []uint8, iYOff int, pU []uint8, iUOff int, pV []uint8, iVOff int) {
	m.pDstY, m.iDstYOff = pY, iYOff
	m.pDstU, m.iDstUOff = pU, iUOff
	m.pDstV, m.iDstVOff = pV, iVOff
}

// GetInterPred ports int32_t GetInterPred (uint8_t* pPredY, uint8_t* pPredCb, uint8_t* pPredCr, PWelsDecoderContext pCtx).
//
// pPredY / pPredCb / pPredCr: (slice, offset) pairs.
func GetInterPred(pPredY []uint8, iPredYOff int, pPredCb []uint8, iPredCbOff int, pPredCr []uint8, iPredCrOff int, pCtx *SWelsDecoderContext) int32 {
	var pMCRefMem sMCRefMember
	pCurDqLayer := pCtx.pCurDqLayer
	pMCFunc := &pCtx.sMcFunc

	iMBXY := pCurDqLayer.iMbXyIndex

	var iMVs [2]int16

	iMBType := pCurDqLayer.pDec.pMbType[iMBXY]

	iMBOffsetX := pCurDqLayer.iMbX << 4
	iMBOffsetY := pCurDqLayer.iMbY << 4

	iDstLineLuma := pCtx.pDec.iLinesize[0]
	iDstLineChroma := pCtx.pDec.iLinesize[1]
	dll := int(iDstLineLuma)
	dlc := int(iDstLineChroma)

	pMCRefMem.iPicWidth = (pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.iMbWidth << 4)
	pMCRefMem.iPicHeight = (pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.iMbHeight << 4)

	pMCRefMem.setDst(pPredY, iPredYOff, pPredCb, iPredCbOff, pPredCr, iPredCrOff)

	pMCRefMem.iDstLineLuma = iDstLineLuma
	pMCRefMem.iDstLineChroma = iDstLineChroma

	var iRefIndex int8

	pDec := pCurDqLayer.pDec
	pMv0 := &pDec.pMv[0][iMBXY]
	pRefIdx0 := &pDec.pRefIndex[0][iMBXY]

	switch iMBType {
	case common.MB_TYPE_SKIP, common.MB_TYPE_16x16:
		iMVs[0] = pMv0[0][0]
		iMVs[1] = pMv0[0][1]
		iRefIndex = pRefIdx0[0]
		if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, common.LIST_0)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iMBOffsetX, iMBOffsetY, pMCFunc, 16, 16, &iMVs)

		if pCurDqLayer.bUseWeightPredictionFlag {
			iRefIndex = pRefIdx0[0]
			WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 16, 16)
		}
	case common.MB_TYPE_16x8:
		iMVs[0] = pMv0[0][0]
		iMVs[1] = pMv0[0][1]
		iRefIndex = pRefIdx0[0]
		if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, common.LIST_0)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iMBOffsetX, iMBOffsetY, pMCFunc, 16, 8, &iMVs)

		if pCurDqLayer.bUseWeightPredictionFlag {
			WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 16, 8)
		}

		iMVs[0] = pMv0[8][0]
		iMVs[1] = pMv0[8][1]
		iRefIndex = pRefIdx0[8]
		if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, common.LIST_0)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		pMCRefMem.setDst(pPredY, iPredYOff+(dll<<3), pPredCb, iPredCbOff+(dlc<<2), pPredCr, iPredCrOff+(dlc<<2))
		BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iMBOffsetX, iMBOffsetY+8, pMCFunc, 16, 8, &iMVs)

		if pCurDqLayer.bUseWeightPredictionFlag {
			WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 16, 8)
		}
	case common.MB_TYPE_8x16:
		iMVs[0] = pMv0[0][0]
		iMVs[1] = pMv0[0][1]
		iRefIndex = pRefIdx0[0]
		if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, common.LIST_0)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iMBOffsetX, iMBOffsetY, pMCFunc, 8, 16, &iMVs)
		if pCurDqLayer.bUseWeightPredictionFlag {
			WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 8, 16)
		}

		iMVs[0] = pMv0[2][0]
		iMVs[1] = pMv0[2][1]
		iRefIndex = pRefIdx0[2]
		if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, common.LIST_0)); uiRetTmp != ERR_NONE {
			return int32(uiRetTmp)
		}
		pMCRefMem.setDst(pPredY, iPredYOff+8, pPredCb, iPredCbOff+4, pPredCr, iPredCrOff+4)
		BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iMBOffsetX+8, iMBOffsetY, pMCFunc, 8, 16, &iMVs)

		if pCurDqLayer.bUseWeightPredictionFlag {
			WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 8, 16)
		}
	case common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0:
		for i := int32(0); i < 4; i++ {
			iSubMBType := pCurDqLayer.pSubMbType[iMBXY][i]
			iBlk8X := (i & 1) << 3
			iBlk8Y := (i >> 1) << 3
			iXOffset := iMBOffsetX + iBlk8X
			iYOffset := iMBOffsetY + iBlk8Y

			iIIdx := ((i >> 1) << 3) + ((i & 1) << 1)
			iRefIndex = pRefIdx0[iIIdx]
			if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, common.LIST_0)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			iDstYOff := iPredYOff + int(iBlk8X+iBlk8Y*iDstLineLuma)
			iDstUOff := iPredCbOff + int((iBlk8X>>1)+(iBlk8Y>>1)*iDstLineChroma)
			iDstVOff := iPredCrOff + int((iBlk8X>>1)+(iBlk8Y>>1)*iDstLineChroma)
			pMCRefMem.setDst(pPredY, iDstYOff, pPredCb, iDstUOff, pPredCr, iDstVOff)
			switch iSubMBType {
			case common.SUB_MB_TYPE_8x8:
				iMVs[0] = pMv0[iIIdx][0]
				iMVs[1] = pMv0[iIIdx][1]
				BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iXOffset, iYOffset, pMCFunc, 8, 8, &iMVs)
				if pCurDqLayer.bUseWeightPredictionFlag {
					WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 8, 8)
				}
			case common.SUB_MB_TYPE_8x4:
				iMVs[0] = pMv0[iIIdx][0]
				iMVs[1] = pMv0[iIIdx][1]
				BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iXOffset, iYOffset, pMCFunc, 8, 4, &iMVs)
				if pCurDqLayer.bUseWeightPredictionFlag {
					WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 8, 4)
				}

				iMVs[0] = pMv0[iIIdx+4][0]
				iMVs[1] = pMv0[iIIdx+4][1]
				pMCRefMem.iDstYOff += (dll << 2)
				pMCRefMem.iDstUOff += (dlc << 1)
				pMCRefMem.iDstVOff += (dlc << 1)
				BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iXOffset, iYOffset+4, pMCFunc, 8, 4, &iMVs)
				if pCurDqLayer.bUseWeightPredictionFlag {
					WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 8, 4)
				}
			case common.SUB_MB_TYPE_4x8:
				iMVs[0] = pMv0[iIIdx][0]
				iMVs[1] = pMv0[iIIdx][1]
				BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iXOffset, iYOffset, pMCFunc, 4, 8, &iMVs)
				if pCurDqLayer.bUseWeightPredictionFlag {
					WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 4, 8)
				}

				iMVs[0] = pMv0[iIIdx+1][0]
				iMVs[1] = pMv0[iIIdx+1][1]
				pMCRefMem.iDstYOff += 4
				pMCRefMem.iDstUOff += 2
				pMCRefMem.iDstVOff += 2
				BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iXOffset+4, iYOffset, pMCFunc, 4, 8, &iMVs)
				if pCurDqLayer.bUseWeightPredictionFlag {
					WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 4, 8)
				}
			case common.SUB_MB_TYPE_4x4:
				for j := int32(0); j < 4; j++ {
					iJIdx := ((j >> 1) << 2) + (j & 1)

					iBlk4X := (j & 1) << 2
					iBlk4Y := (j >> 1) << 2

					iUVLineStride := int((iBlk4X >> 1) + (iBlk4Y>>1)*iDstLineChroma)
					pMCRefMem.iDstYOff = iDstYOff + int(iBlk4X+iBlk4Y*iDstLineLuma)
					pMCRefMem.iDstUOff = iDstUOff + iUVLineStride
					pMCRefMem.iDstVOff = iDstVOff + iUVLineStride

					iMVs[0] = pMv0[iIIdx+iJIdx][0]
					iMVs[1] = pMv0[iIIdx+iJIdx][1]
					BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex, iXOffset+iBlk4X, iYOffset+iBlk4Y, pMCFunc, 4, 4, &iMVs)
					if pCurDqLayer.bUseWeightPredictionFlag {
						WeightPrediction(pCurDqLayer, &pMCRefMem, common.LIST_0, int32(iRefIndex), 4, 4)
					}
				}
			default:
			}
		}
	default:
	}
	return ERR_NONE
}

// GetInterBPred ports int32_t GetInterBPred (uint8_t* pPredYCbCr[3], uint8_t* pTempPredYCbCr[3], PWelsDecoderContext pCtx).
//
// C uint8_t* pPredYCbCr[3] / pTempPredYCbCr[3] (read-only) -> arrays of (slice, offset) pairs.
func GetInterBPred(pPredYCbCr [3][]uint8, iPredYCbCrOff [3]int, pTempPredYCbCr [3][]uint8, iTempPredYCbCrOff [3]int, pCtx *SWelsDecoderContext) int32 {
	var pMCRefMem sMCRefMember
	var pTempMCRefMem sMCRefMember

	pCurDqLayer := pCtx.pCurDqLayer
	pMCFunc := &pCtx.sMcFunc

	iMBXY := pCurDqLayer.iMbXyIndex

	var iMVs [2]int16

	iMBType := pCurDqLayer.pDec.pMbType[iMBXY]

	iMBOffsetX := pCurDqLayer.iMbX << 4
	iMBOffsetY := pCurDqLayer.iMbY << 4

	iDstLineLuma := pCtx.pDec.iLinesize[0]
	iDstLineChroma := pCtx.pDec.iLinesize[1]
	dll := int(iDstLineLuma)
	dlc := int(iDstLineChroma)

	pMCRefMem.iPicWidth = (pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.iMbWidth << 4)
	pMCRefMem.iPicHeight = (pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.iMbHeight << 4)

	pMCRefMem.setDst(pPredYCbCr[0], iPredYCbCrOff[0], pPredYCbCr[1], iPredYCbCrOff[1], pPredYCbCr[2], iPredYCbCrOff[2])

	pMCRefMem.iDstLineLuma = iDstLineLuma
	pMCRefMem.iDstLineChroma = iDstLineChroma

	pTempMCRefMem = pMCRefMem
	pTempMCRefMem.setDst(pTempPredYCbCr[0], iTempPredYCbCrOff[0], pTempPredYCbCr[1], iTempPredYCbCrOff[1],
		pTempPredYCbCr[2], iTempPredYCbCrOff[2])

	var iRefIndex0, iRefIndex1, iRefIndex int8

	bWeightedBipredIdcIs1 := pCurDqLayer.sLayerInfo.pPps.uiWeightedBipredIdc == 1

	pDec := pCurDqLayer.pDec

	if common.IS_INTER_16x16(iMBType) {
		if common.IS_TYPE_L0(iMBType) && common.IS_TYPE_L1(iMBType) {
			iMVs[0] = pDec.pMv[common.LIST_0][iMBXY][0][0]
			iMVs[1] = pDec.pMv[common.LIST_0][iMBXY][0][1]
			iRefIndex0 = pDec.pRefIndex[common.LIST_0][iMBXY][0]
			if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex0, common.LIST_0)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex0, iMBOffsetX, iMBOffsetY, pMCFunc, 16, 16, &iMVs)

			iMVs[0] = pDec.pMv[common.LIST_1][iMBXY][0][0]
			iMVs[1] = pDec.pMv[common.LIST_1][iMBXY][0][1]
			iRefIndex1 = pDec.pRefIndex[common.LIST_1][iMBXY][0]
			if uiRetTmp := uint32(GetRefPic(&pTempMCRefMem, pCtx, iRefIndex1, common.LIST_1)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			BaseMC(pCtx, &pTempMCRefMem, common.LIST_1, iRefIndex1, iMBOffsetX, iMBOffsetY, pMCFunc, 16, 16, &iMVs)
			if pCurDqLayer.bUseWeightedBiPredIdc {
				BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1), bWeightedBipredIdcIs1, 16, 16)
			} else {
				BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 16, 16)
			}
		} else {
			listIdx := int32(common.LIST_1)
			if iMBType&common.MB_TYPE_P0L0 != 0 {
				listIdx = common.LIST_0
			}
			iMVs[0] = pDec.pMv[listIdx][iMBXY][0][0]
			iMVs[1] = pDec.pMv[listIdx][iMBXY][0][1]
			iRefIndex = pDec.pRefIndex[listIdx][iMBXY][0]
			if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, listIdx)); uiRetTmp != ERR_NONE {
				return int32(uiRetTmp)
			}
			BaseMC(pCtx, &pMCRefMem, listIdx, iRefIndex, iMBOffsetX, iMBOffsetY, pMCFunc, 16, 16, &iMVs)
			if bWeightedBipredIdcIs1 {
				WeightPrediction(pCurDqLayer, &pMCRefMem, listIdx, int32(iRefIndex), 16, 16)
			}
		}
	} else if common.IS_INTER_16x8(iMBType) {
		for i := int32(0); i < 2; i++ {
			iPartIdx := i << 3
			listCount := uint32(0)
			lastListIdx := int32(common.LIST_0)
			// The partition offset belongs to the partition, not to the reference
			// list, so both destinations are (re)set once here for each partition.
			offY, offC := 0, 0
			if i != 0 {
				offY, offC = dll<<3, dlc<<2
			}
			pMCRefMem.setDst(pPredYCbCr[0], iPredYCbCrOff[0]+offY, pPredYCbCr[1], iPredYCbCrOff[1]+offC,
				pPredYCbCr[2], iPredYCbCrOff[2]+offC)
			pTempMCRefMem.setDst(pTempPredYCbCr[0], iTempPredYCbCrOff[0]+offY, pTempPredYCbCr[1], iTempPredYCbCrOff[1]+offC,
				pTempPredYCbCr[2], iTempPredYCbCrOff[2]+offC)
			for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
				if common.IS_DIR(iMBType, i, listIdx) {
					// The first list of a partition predicts into pMCRefMem and the second
					// into pTempMCRefMem, so the second BaseMC() no longer overwrites the
					// prediction made for the first list.
					pTarget := &pMCRefMem
					if listCount != 0 {
						pTarget = &pTempMCRefMem
					}
					lastListIdx = listIdx
					iMVs[0] = pDec.pMv[listIdx][iMBXY][iPartIdx][0]
					iMVs[1] = pDec.pMv[listIdx][iMBXY][iPartIdx][1]
					iRefIndex = pDec.pRefIndex[listIdx][iMBXY][iPartIdx]
					if uiRetTmp := uint32(GetRefPic(pTarget, pCtx, iRefIndex, listIdx)); uiRetTmp != ERR_NONE {
						return int32(uiRetTmp)
					}
					BaseMC(pCtx, pTarget, listIdx, iRefIndex, iMBOffsetX, iMBOffsetY+iPartIdx, pMCFunc, 16, 8, &iMVs)
					listCount++
				}
			}
			if listCount == 2 {
				iRefIndex0 = pDec.pRefIndex[common.LIST_0][iMBXY][iPartIdx]
				iRefIndex1 = pDec.pRefIndex[common.LIST_1][iMBXY][iPartIdx]
				if pCurDqLayer.bUseWeightedBiPredIdc {
					BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1),
						bWeightedBipredIdcIs1, 16, 8)
				} else {
					BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 16, 8)
				}
			} else if listCount == 1 {
				if bWeightedBipredIdcIs1 {
					iRefIndex = pDec.pRefIndex[lastListIdx][iMBXY][iPartIdx]
					WeightPrediction(pCurDqLayer, &pMCRefMem, lastListIdx, int32(iRefIndex), 16, 8)
				}
			}
		}
	} else if common.IS_INTER_8x16(iMBType) {
		for i := int32(0); i < 2; i++ {
			listCount := uint32(0)
			lastListIdx := int32(common.LIST_0)
			// The partition offset belongs to the partition, not to the reference
			// list, so both destinations are (re)set once here for each partition.
			offY, offC := 0, 0
			if i != 0 {
				offY, offC = 8, 4
			}
			pMCRefMem.setDst(pPredYCbCr[0], iPredYCbCrOff[0]+offY, pPredYCbCr[1], iPredYCbCrOff[1]+offC,
				pPredYCbCr[2], iPredYCbCrOff[2]+offC)
			pTempMCRefMem.setDst(pTempPredYCbCr[0], iTempPredYCbCrOff[0]+offY, pTempPredYCbCr[1], iTempPredYCbCrOff[1]+offC,
				pTempPredYCbCr[2], iTempPredYCbCrOff[2]+offC)
			for listIdx := int32(common.LIST_0); listIdx < common.LIST_A; listIdx++ {
				if common.IS_DIR(iMBType, i, listIdx) {
					// The first list of a partition predicts into pMCRefMem and the second
					// into pTempMCRefMem, so the second BaseMC() no longer overwrites the
					// prediction made for the first list.
					pTarget := &pMCRefMem
					if listCount != 0 {
						pTarget = &pTempMCRefMem
					}
					lastListIdx = listIdx
					iMVs[0] = pDec.pMv[listIdx][iMBXY][i<<1][0]
					iMVs[1] = pDec.pMv[listIdx][iMBXY][i<<1][1]
					iRefIndex = pDec.pRefIndex[listIdx][iMBXY][i<<1]
					if uiRetTmp := uint32(GetRefPic(pTarget, pCtx, iRefIndex, listIdx)); uiRetTmp != ERR_NONE {
						return int32(uiRetTmp)
					}
					BaseMC(pCtx, pTarget, listIdx, iRefIndex, iMBOffsetX+int32(offY), iMBOffsetY, pMCFunc, 8, 16, &iMVs)
					listCount++
				}
			}
			if listCount == 2 {
				iRefIndex0 = pDec.pRefIndex[common.LIST_0][iMBXY][i<<1]
				iRefIndex1 = pDec.pRefIndex[common.LIST_1][iMBXY][i<<1]
				if pCurDqLayer.bUseWeightedBiPredIdc {
					BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1),
						bWeightedBipredIdcIs1, 8, 16)
				} else {
					BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 8, 16)
				}
			} else if listCount == 1 {
				if bWeightedBipredIdcIs1 {
					iRefIndex = pDec.pRefIndex[lastListIdx][iMBXY][i<<1]
					WeightPrediction(pCurDqLayer, &pMCRefMem, lastListIdx, int32(iRefIndex), 8, 16)
				}
			}
		}
	} else if common.IS_Inter_8x8(iMBType) {
		for i := int32(0); i < 4; i++ {
			iSubMBType := pCurDqLayer.pSubMbType[iMBXY][i]
			iBlk8X := (i & 1) << 3
			iBlk8Y := (i >> 1) << 3
			iXOffset := iMBOffsetX + iBlk8X
			iYOffset := iMBOffsetY + iBlk8Y

			iIIdx := ((i >> 1) << 3) + ((i & 1) << 1)

			iDstYOff := iPredYCbCrOff[0] + int(iBlk8X+iBlk8Y*iDstLineLuma)
			iDstUOff := iPredYCbCrOff[1] + int((iBlk8X>>1)+(iBlk8Y>>1)*iDstLineChroma)
			iDstVOff := iPredYCbCrOff[2] + int((iBlk8X>>1)+(iBlk8Y>>1)*iDstLineChroma)
			pMCRefMem.setDst(pPredYCbCr[0], iDstYOff, pPredYCbCr[1], iDstUOff, pPredYCbCr[2], iDstVOff)

			pTempMCRefMem = pMCRefMem
			iDstY2Off := iTempPredYCbCrOff[0] + int(iBlk8X+iBlk8Y*iDstLineLuma)
			iDstU2Off := iTempPredYCbCrOff[1] + int((iBlk8X>>1)+(iBlk8Y>>1)*iDstLineChroma)
			iDstV2Off := iTempPredYCbCrOff[2] + int((iBlk8X>>1)+(iBlk8Y>>1)*iDstLineChroma)

			pTempMCRefMem.setDst(pTempPredYCbCr[0], iDstY2Off, pTempPredYCbCr[1], iDstU2Off, pTempPredYCbCr[2], iDstV2Off)

			bBi := common.IS_TYPE_L0(iSubMBType) && common.IS_TYPE_L1(iSubMBType)
			if bBi {
				iRefIndex0 = pDec.pRefIndex[common.LIST_0][iMBXY][iIIdx]
				if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex0, common.LIST_0)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp)
				}

				iRefIndex1 = pDec.pRefIndex[common.LIST_1][iMBXY][iIIdx]
				if uiRetTmp := uint32(GetRefPic(&pTempMCRefMem, pCtx, iRefIndex1, common.LIST_1)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp)
				}
			} else {
				listIdx := int32(common.LIST_1)
				if common.IS_TYPE_L0(iSubMBType) {
					listIdx = common.LIST_0
				}
				iRefIndex = pDec.pRefIndex[listIdx][iMBXY][iIIdx]
				if uiRetTmp := uint32(GetRefPic(&pMCRefMem, pCtx, iRefIndex, listIdx)); uiRetTmp != ERR_NONE {
					return int32(uiRetTmp)
				}
			}

			// listIdx for the single-list cases (B_L0_* / B_L1_*)
			listIdx := int32(common.LIST_1)
			if common.IS_TYPE_L0(iSubMBType) {
				listIdx = common.LIST_0
			}

			if common.IS_SUB_8x8(iSubMBType) {
				if bBi {
					iMVs[0] = pDec.pMv[common.LIST_0][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[common.LIST_0][iMBXY][iIIdx][1]
					BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex0, iXOffset, iYOffset, pMCFunc, 8, 8, &iMVs)

					iMVs[0] = pDec.pMv[common.LIST_1][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[common.LIST_1][iMBXY][iIIdx][1]
					BaseMC(pCtx, &pTempMCRefMem, common.LIST_1, iRefIndex1, iXOffset, iYOffset, pMCFunc, 8, 8, &iMVs)

					if pCurDqLayer.bUseWeightedBiPredIdc {
						BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1), bWeightedBipredIdcIs1, 8, 8)
					} else {
						BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 8, 8)
					}
				} else {
					iMVs[0] = pDec.pMv[listIdx][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[listIdx][iMBXY][iIIdx][1]
					iRefIndex = pDec.pRefIndex[listIdx][iMBXY][iIIdx]
					BaseMC(pCtx, &pMCRefMem, listIdx, iRefIndex, iXOffset, iYOffset, pMCFunc, 8, 8, &iMVs)
					if bWeightedBipredIdcIs1 {
						WeightPrediction(pCurDqLayer, &pMCRefMem, listIdx, int32(iRefIndex), 8, 8)
					}
				}
			} else if common.IS_SUB_8x4(iSubMBType) {
				if bBi { //B_Bi_8x4
					iMVs[0] = pDec.pMv[common.LIST_0][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[common.LIST_0][iMBXY][iIIdx][1]
					BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex0, iXOffset, iYOffset, pMCFunc, 8, 4, &iMVs)
					iMVs[0] = pDec.pMv[common.LIST_1][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[common.LIST_1][iMBXY][iIIdx][1]
					BaseMC(pCtx, &pTempMCRefMem, common.LIST_1, iRefIndex1, iXOffset, iYOffset, pMCFunc, 8, 4, &iMVs)

					if pCurDqLayer.bUseWeightedBiPredIdc {
						BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1), bWeightedBipredIdcIs1, 8, 4)
					} else {
						BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 8, 4)
					}

					pMCRefMem.iDstYOff += (dll << 2)
					pMCRefMem.iDstUOff += (dlc << 1)
					pMCRefMem.iDstVOff += (dlc << 1)
					iMVs[0] = pDec.pMv[common.LIST_0][iMBXY][iIIdx+4][0]
					iMVs[1] = pDec.pMv[common.LIST_0][iMBXY][iIIdx+4][1]
					BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex0, iXOffset, iYOffset+4, pMCFunc, 8, 4, &iMVs)

					pTempMCRefMem.iDstYOff += (dll << 2)
					pTempMCRefMem.iDstUOff += (dlc << 1)
					pTempMCRefMem.iDstVOff += (dlc << 1)
					iMVs[0] = pDec.pMv[common.LIST_1][iMBXY][iIIdx+4][0]
					iMVs[1] = pDec.pMv[common.LIST_1][iMBXY][iIIdx+4][1]
					BaseMC(pCtx, &pTempMCRefMem, common.LIST_1, iRefIndex1, iXOffset, iYOffset+4, pMCFunc, 8, 4, &iMVs)

					if pCurDqLayer.bUseWeightedBiPredIdc {
						BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1), bWeightedBipredIdcIs1, 8, 4)
					} else {
						BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 8, 4)
					}
				} else { //B_L0_8x4 B_L1_8x4
					iMVs[0] = pDec.pMv[listIdx][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[listIdx][iMBXY][iIIdx][1]
					iRefIndex = pDec.pRefIndex[listIdx][iMBXY][iIIdx]
					BaseMC(pCtx, &pMCRefMem, listIdx, iRefIndex, iXOffset, iYOffset, pMCFunc, 8, 4, &iMVs)
					pMCRefMem.iDstYOff += (dll << 2)
					pMCRefMem.iDstUOff += (dlc << 1)
					pMCRefMem.iDstVOff += (dlc << 1)
					iMVs[0] = pDec.pMv[listIdx][iMBXY][iIIdx+4][0]
					iMVs[1] = pDec.pMv[listIdx][iMBXY][iIIdx+4][1]
					BaseMC(pCtx, &pMCRefMem, listIdx, iRefIndex, iXOffset, iYOffset+4, pMCFunc, 8, 4, &iMVs)
					if bWeightedBipredIdcIs1 {
						WeightPrediction(pCurDqLayer, &pMCRefMem, listIdx, int32(iRefIndex), 8, 4)
					}
				}
			} else if common.IS_SUB_4x8(iSubMBType) {
				if bBi { //B_Bi_4x8
					iMVs[0] = pDec.pMv[common.LIST_0][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[common.LIST_0][iMBXY][iIIdx][1]
					BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex0, iXOffset, iYOffset, pMCFunc, 4, 8, &iMVs)
					iMVs[0] = pDec.pMv[common.LIST_1][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[common.LIST_1][iMBXY][iIIdx][1]
					BaseMC(pCtx, &pTempMCRefMem, common.LIST_1, iRefIndex1, iXOffset, iYOffset, pMCFunc, 4, 8, &iMVs)

					if pCurDqLayer.bUseWeightedBiPredIdc {
						BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1), bWeightedBipredIdcIs1, 4, 8)
					} else {
						BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 4, 8)
					}

					pMCRefMem.iDstYOff += 4
					pMCRefMem.iDstUOff += 2
					pMCRefMem.iDstVOff += 2
					iMVs[0] = pDec.pMv[common.LIST_0][iMBXY][iIIdx+1][0]
					iMVs[1] = pDec.pMv[common.LIST_0][iMBXY][iIIdx+1][1]
					BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex0, iXOffset+4, iYOffset, pMCFunc, 4, 8, &iMVs)

					pTempMCRefMem.iDstYOff += 4
					pTempMCRefMem.iDstUOff += 2
					pTempMCRefMem.iDstVOff += 2
					iMVs[0] = pDec.pMv[common.LIST_1][iMBXY][iIIdx+1][0]
					iMVs[1] = pDec.pMv[common.LIST_1][iMBXY][iIIdx+1][1]
					BaseMC(pCtx, &pTempMCRefMem, common.LIST_1, iRefIndex1, iXOffset+4, iYOffset, pMCFunc, 4, 8, &iMVs)

					if pCurDqLayer.bUseWeightedBiPredIdc {
						BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1), bWeightedBipredIdcIs1, 4, 8)
					} else {
						BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 4, 8)
					}
				} else { //B_L0_4x8 B_L1_4x8
					iMVs[0] = pDec.pMv[listIdx][iMBXY][iIIdx][0]
					iMVs[1] = pDec.pMv[listIdx][iMBXY][iIIdx][1]
					iRefIndex = pDec.pRefIndex[listIdx][iMBXY][iIIdx]
					BaseMC(pCtx, &pMCRefMem, listIdx, iRefIndex, iXOffset, iYOffset, pMCFunc, 4, 8, &iMVs)
					pMCRefMem.iDstYOff += 4
					pMCRefMem.iDstUOff += 2
					pMCRefMem.iDstVOff += 2
					iMVs[0] = pDec.pMv[listIdx][iMBXY][iIIdx+1][0]
					iMVs[1] = pDec.pMv[listIdx][iMBXY][iIIdx+1][1]
					BaseMC(pCtx, &pMCRefMem, listIdx, iRefIndex, iXOffset+4, iYOffset, pMCFunc, 4, 8, &iMVs)
					if bWeightedBipredIdcIs1 {
						WeightPrediction(pCurDqLayer, &pMCRefMem, listIdx, int32(iRefIndex), 4, 8)
					}
				}
			} else if common.IS_SUB_4x4(iSubMBType) {
				if bBi {
					for j := int32(0); j < 4; j++ {
						iJIdx := ((j >> 1) << 2) + (j & 1)

						iBlk4X := (j & 1) << 2
						iBlk4Y := (j >> 1) << 2

						iUVLineStride := int((iBlk4X >> 1) + (iBlk4Y>>1)*iDstLineChroma)
						pMCRefMem.iDstYOff = iDstYOff + int(iBlk4X+iBlk4Y*iDstLineLuma)
						pMCRefMem.iDstUOff = iDstUOff + iUVLineStride
						pMCRefMem.iDstVOff = iDstVOff + iUVLineStride

						iMVs[0] = pDec.pMv[common.LIST_0][iMBXY][iIIdx+iJIdx][0]
						iMVs[1] = pDec.pMv[common.LIST_0][iMBXY][iIIdx+iJIdx][1]
						BaseMC(pCtx, &pMCRefMem, common.LIST_0, iRefIndex0, iXOffset+iBlk4X, iYOffset+iBlk4Y, pMCFunc, 4, 4, &iMVs)

						// NOTE: the C code offsets the temp luma block by iBlk8X/iBlk8Y
						// here (not iBlk4X/iBlk4Y); kept for bit-exactness.
						pTempMCRefMem.iDstYOff = iDstY2Off + int(iBlk8X+iBlk8Y*iDstLineLuma)
						pTempMCRefMem.iDstUOff = iDstU2Off + iUVLineStride
						pTempMCRefMem.iDstVOff = iDstV2Off + iUVLineStride

						iMVs[0] = pDec.pMv[common.LIST_1][iMBXY][iIIdx+iJIdx][0]
						iMVs[1] = pDec.pMv[common.LIST_1][iMBXY][iIIdx+iJIdx][1]
						BaseMC(pCtx, &pTempMCRefMem, common.LIST_1, iRefIndex1, iXOffset+iBlk4X, iYOffset+iBlk4Y, pMCFunc, 4, 4, &iMVs)

						if pCurDqLayer.bUseWeightedBiPredIdc {
							BiWeightPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, int32(iRefIndex0), int32(iRefIndex1), bWeightedBipredIdcIs1, 4, 4)
						} else {
							BiPrediction(pCurDqLayer, &pMCRefMem, &pTempMCRefMem, 4, 4)
						}
					}
				} else {
					iRefIndex = pDec.pRefIndex[listIdx][iMBXY][iIIdx]
					for j := int32(0); j < 4; j++ {
						iJIdx := ((j >> 1) << 2) + (j & 1)

						iBlk4X := (j & 1) << 2
						iBlk4Y := (j >> 1) << 2

						iUVLineStride := int((iBlk4X >> 1) + (iBlk4Y>>1)*iDstLineChroma)
						pMCRefMem.iDstYOff = iDstYOff + int(iBlk4X+iBlk4Y*iDstLineLuma)
						pMCRefMem.iDstUOff = iDstUOff + iUVLineStride
						pMCRefMem.iDstVOff = iDstVOff + iUVLineStride

						iMVs[0] = pDec.pMv[listIdx][iMBXY][iIIdx+iJIdx][0]
						iMVs[1] = pDec.pMv[listIdx][iMBXY][iIIdx+iJIdx][1]
						BaseMC(pCtx, &pMCRefMem, listIdx, iRefIndex, iXOffset+iBlk4X, iYOffset+iBlk4Y, pMCFunc, 4, 4, &iMVs)
						if bWeightedBipredIdcIs1 {
							WeightPrediction(pCurDqLayer, &pMCRefMem, listIdx, int32(iRefIndex), 4, 4)
						}
					}
				}
			}
		}
	}
	return ERR_NONE
}

// RecChroma ports int32_t RecChroma (int32_t iMBXY, PWelsDecoderContext pCtx, int16_t* pScoeffLevel, PDqLayer pDqLayer).
func RecChroma(iMBXY int32, pCtx *SWelsDecoderContext, pScoeffLevel []int16, pDqLayer *SDqLayer) int32 {
	iChromaStride := pCtx.pCurDqLayer.pDec.iLinesize[1]
	pIdctFourResAddPredFunc := pCtx.pIdctFourResAddPredFunc

	uiCbpC := uint8(pDqLayer.pCbp[iMBXY] >> 4)

	if 1 == uiCbpC || 2 == uiCbpC {
		for i := 0; i < 2; i++ {
			pRS := pScoeffLevel[256+(i<<6):]
			pNzc := pDqLayer.pNzc[iMBXY][16+2*i:]

			/*1 chroma is divided 4 4x4_block to idct*/
			pIdctFourResAddPredFunc(pDqLayer.pPred[i+1], pDqLayer.iPredOff[i+1], iChromaStride, pRS, pNzc)
		}
	}

	return ERR_NONE
}
