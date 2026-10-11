// Port of codec/decoder/core/src/error_concealment.cpp.
//
// Error concealment: frame copy, slice copy and slice MV copy methods.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// GetEcPicWidthInPixel (static inline).
func GetEcPicWidthInPixel(pCtx *SWelsDecoderContext, pPic *SPicture) int32 {
	if pPic != nil && pPic.iWidthInPixel > 0 {
		return pPic.iWidthInPixel
	}
	return int32(pCtx.pSps.iMbWidth << 4)
}

// GetEcPicHeightInPixel (static inline).
func GetEcPicHeightInPixel(pCtx *SWelsDecoderContext, pPic *SPicture) int32 {
	if pPic != nil && pPic.iHeightInPixel > 0 {
		return pPic.iHeightInPixel
	}
	return int32(pCtx.pSps.iMbHeight << 4)
}

// IsEcRefPicCompatible (static inline).
func IsEcRefPicCompatible(pCtx *SWelsDecoderContext, pDstPic *SPicture, pSrcPic *SPicture) bool {
	if pDstPic == nil || pSrcPic == nil {
		return false
	}

	iDstWidthInPixel := GetEcPicWidthInPixel(pCtx, pDstPic)
	iDstHeightInPixel := GetEcPicHeightInPixel(pCtx, pDstPic)
	iSrcWidthInPixel := GetEcPicWidthInPixel(pCtx, pSrcPic)
	iSrcHeightInPixel := GetEcPicHeightInPixel(pCtx, pSrcPic)

	if iDstWidthInPixel != iSrcWidthInPixel || iDstHeightInPixel != iSrcHeightInPixel {
		return false
	}

	return pSrcPic.iLinesize[0] >= pDstPic.iLinesize[0] &&
		pSrcPic.iLinesize[1] >= pDstPic.iLinesize[1] &&
		pSrcPic.iLinesize[2] >= pDstPic.iLinesize[2]
}

// InitErrorCon ports void InitErrorCon (PWelsDecoderContext pCtx).
func InitErrorCon(pCtx *SWelsDecoderContext) {
	eEc := pCtx.pParam.EEcActiveIdc
	if (eEc == api.ERROR_CON_SLICE_COPY) ||
		(eEc == api.ERROR_CON_SLICE_COPY_CROSS_IDR) ||
		(eEc == api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR) ||
		(eEc == api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE) ||
		(eEc == api.ERROR_CON_SLICE_COPY_CROSS_IDR_FREEZE_RES_CHANGE) {
		if (eEc != api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE) &&
			(eEc != api.ERROR_CON_SLICE_COPY_CROSS_IDR_FREEZE_RES_CHANGE) {
			pCtx.bFreezeOutput = false
		}
		pCtx.sCopyFunc.pCopyLumaFunc = common.WelsCopy16x16_c
		pCtx.sCopyFunc.pCopyChromaFunc = common.WelsCopy8x8_c
		// SIMD variants are not ported.
	} //TODO add more methods here
}

// DoErrorConFrameCopy ports void DoErrorConFrameCopy (PWelsDecoderContext pCtx).
//
// Do error concealment using frame copy method
func DoErrorConFrameCopy(pCtx *SWelsDecoderContext) {
	pDstPic := pCtx.pDec
	pSrcPic := pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb
	iHeightInPixelY := GetEcPicHeightInPixel(pCtx, pDstPic)
	iStrideY := pDstPic.iLinesize[0]
	iStrideUV := pDstPic.iLinesize[1]
	pCtx.pDec.iMbEcedNum = int32(pCtx.pSps.iMbWidth * pCtx.pSps.iMbHeight)
	if (pCtx.pParam.EEcActiveIdc == api.ERROR_CON_FRAME_COPY) && (pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag) {
		pSrcPic = nil //no cross IDR method, should fill in data instead of copy
	}
	if !IsEcRefPicCompatible(pCtx, pDstPic, pSrcPic) {
		pSrcPic = nil
	}
	nY := int(iHeightInPixelY * iStrideY)
	nUV := int((iHeightInPixelY >> 1) * iStrideUV)
	if pSrcPic == nil { //no ref pic, assign specific data to picture
		memsetPlane(pDstPic.pData[0], pDstPic.iDataOff[0], 128, nY)
		memsetPlane(pDstPic.pData[1], pDstPic.iDataOff[1], 128, nUV)
		memsetPlane(pDstPic.pData[2], pDstPic.iDataOff[2], 128, nUV)
	} else if pSrcPic == pDstPic {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "DoErrorConFrameCopy()::EC memcpy overlap.")
	} else { //has ref pic here
		copy(pDstPic.pData[0][pDstPic.iDataOff[0]:pDstPic.iDataOff[0]+nY], pSrcPic.pData[0][pSrcPic.iDataOff[0]:pSrcPic.iDataOff[0]+nY])
		copy(pDstPic.pData[1][pDstPic.iDataOff[1]:pDstPic.iDataOff[1]+nUV], pSrcPic.pData[1][pSrcPic.iDataOff[1]:pSrcPic.iDataOff[1]+nUV])
		copy(pDstPic.pData[2][pDstPic.iDataOff[2]:pDstPic.iDataOff[2]+nUV], pSrcPic.pData[2][pSrcPic.iDataOff[2]:pSrcPic.iDataOff[2]+nUV])
	}
}

// ecFillMb128 fills one MB (16x16 luma, 8x8 Cb/Cr) of pDstPic with 128, using
// the C strides (luma stride, chroma = luma stride / 2).
func ecFillMb128(pDstPic *SPicture, iMbX int32, iMbY int32, iDstStride int) {
	//Y component
	off := pDstPic.iDataOff[0] + int(iMbY)*16*iDstStride + int(iMbX)*16
	for i := 0; i < 16; i++ {
		memsetPlane(pDstPic.pData[0], off, 128, 16)
		off += iDstStride
	}
	//U component
	off = pDstPic.iDataOff[1] + int(iMbY)*8*iDstStride/2 + int(iMbX)*8
	for i := 0; i < 8; i++ {
		memsetPlane(pDstPic.pData[1], off, 128, 8)
		off += iDstStride / 2
	}
	//V component
	off = pDstPic.iDataOff[2] + int(iMbY)*8*iDstStride/2 + int(iMbX)*8
	for i := 0; i < 8; i++ {
		memsetPlane(pDstPic.pData[2], off, 128, 8)
		off += iDstStride / 2
	}
}

// DoErrorConSliceCopy ports void DoErrorConSliceCopy (PWelsDecoderContext pCtx).
//
// Do error concealment using slice copy method
func DoErrorConSliceCopy(pCtx *SWelsDecoderContext) {
	iMbWidth := int32(pCtx.pSps.iMbWidth)
	iMbHeight := int32(pCtx.pSps.iMbHeight)
	pDstPic := pCtx.pDec
	pSrcPic := pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb
	if (pCtx.pParam.EEcActiveIdc == api.ERROR_CON_SLICE_COPY) && (pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag) {
		pSrcPic = nil //no cross IDR method, should fill in data instead of copy
	}
	if !IsEcRefPicCompatible(pCtx, pDstPic, pSrcPic) {
		pSrcPic = nil
	}

	pMbCorrectlyDecodedFlag := pCtx.pCurDqLayer.pMbCorrectlyDecodedFlag
	//Do slice copy late
	var iMbXyIndex int32
	var iSrcStride int // = pSrcPic->iLinesize[0];
	iDstStride := int(uint32(pDstPic.iLinesize[0]))
	if pSrcPic == pDstPic {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "DoErrorConSliceCopy()::EC memcpy overlap.")
		return
	}
	for iMbY := int32(0); iMbY < iMbHeight; iMbY++ {
		for iMbX := int32(0); iMbX < iMbWidth; iMbX++ {
			iMbXyIndex = iMbY*iMbWidth + iMbX
			if !pMbCorrectlyDecodedFlag[iMbXyIndex] {
				pCtx.pDec.iMbEcedNum++
				if pSrcPic != nil {
					iSrcStride = int(uint32(pSrcPic.iLinesize[0]))
					//Y component
					iDstOff := pDstPic.iDataOff[0] + int(iMbY)*16*iDstStride + int(iMbX)*16
					iSrcOff := pSrcPic.iDataOff[0] + int(iMbY)*16*iSrcStride + int(iMbX)*16
					pCtx.sCopyFunc.pCopyLumaFunc(pDstPic.pData[0], iDstOff, int32(iDstStride), pSrcPic.pData[0], iSrcOff, int32(iSrcStride))
					//U component
					iDstOff = pDstPic.iDataOff[1] + int(iMbY)*8*iDstStride/2 + int(iMbX)*8
					iSrcOff = pSrcPic.iDataOff[1] + int(iMbY)*8*iSrcStride/2 + int(iMbX)*8
					pCtx.sCopyFunc.pCopyChromaFunc(pDstPic.pData[1], iDstOff, int32(iDstStride/2), pSrcPic.pData[1], iSrcOff, int32(iSrcStride/2))
					//V component
					iDstOff = pDstPic.iDataOff[2] + int(iMbY)*8*iDstStride/2 + int(iMbX)*8
					iSrcOff = pSrcPic.iDataOff[2] + int(iMbY)*8*iSrcStride/2 + int(iMbX)*8
					pCtx.sCopyFunc.pCopyChromaFunc(pDstPic.pData[2], iDstOff, int32(iDstStride/2), pSrcPic.pData[2], iSrcOff, int32(iSrcStride/2))
				} else { //pSrcPic == NULL
					ecFillMb128(pDstPic, iMbX, iMbY, iDstStride)
				} //
			} //!pMbCorrectlyDecodedFlag[iMbXyIndex]
		} //iMbX
	} //iMbY
}

// DoMbECMvCopy ports void DoMbECMvCopy (PWelsDecoderContext pCtx, PPicture pDec, PPicture pRef,
// int32_t iMbXy, int32_t iMbX, int32_t iMbY, sMCRefMember* pMCRefMem).
//
// Do error concealment using slice MV copy method
func DoMbECMvCopy(pCtx *SWelsDecoderContext, pDec *SPicture, pRef *SPicture, iMbXy int32, iMbX int32, iMbY int32, pMCRefMem *sMCRefMember) {
	if pDec == pRef {
		return // for protection, shall never go into this logic, error info printed outside.
	}
	var iMVs [2]int16
	iMbXInPix := iMbX << 4
	iMbYInPix := iMbY << 4
	var iScale0 int32
	var iScale1 int32
	var iDstOff [3]int
	iCurrPoc := pDec.iFramePoc
	iDstOff[0] = pDec.iDataOff[0] + int(iMbXInPix+iMbYInPix*pMCRefMem.iDstLineLuma)
	iDstOff[1] = pDec.iDataOff[1] + int((iMbXInPix>>1)+(iMbYInPix>>1)*pMCRefMem.iDstLineChroma)
	iDstOff[2] = pDec.iDataOff[2] + int((iMbXInPix>>1)+(iMbYInPix>>1)*pMCRefMem.iDstLineChroma)
	if pDec.bIdrFlag == true || pCtx.pECRefPic[0] == nil {
		//Y component
		iSrcOff := pMCRefMem.iSrcYOff + int(iMbY*16*pMCRefMem.iSrcLineLuma+iMbX*16)
		pCtx.sCopyFunc.pCopyLumaFunc(pDec.pData[0], iDstOff[0], pMCRefMem.iDstLineLuma, pMCRefMem.pSrcY, iSrcOff, pMCRefMem.iSrcLineLuma)
		//U component
		iSrcOff = pMCRefMem.iSrcUOff + int(iMbY*8*pMCRefMem.iSrcLineChroma+iMbX*8)
		pCtx.sCopyFunc.pCopyChromaFunc(pDec.pData[1], iDstOff[1], pMCRefMem.iDstLineChroma, pMCRefMem.pSrcU, iSrcOff, pMCRefMem.iSrcLineChroma)
		//V component
		iSrcOff = pMCRefMem.iSrcVOff + int(iMbY*8*pMCRefMem.iSrcLineChroma+iMbX*8)
		pCtx.sCopyFunc.pCopyChromaFunc(pDec.pData[2], iDstOff[2], pMCRefMem.iDstLineChroma, pMCRefMem.pSrcV, iSrcOff, pMCRefMem.iSrcLineChroma)
		return
	}

	if pCtx.pECRefPic[0] != nil {
		if pCtx.pECRefPic[0] == pRef {
			iMVs[0] = int16(pCtx.iECMVs[0][0])
			iMVs[1] = int16(pCtx.iECMVs[0][1])
		} else {
			iScale0 = pCtx.pECRefPic[0].iFramePoc - iCurrPoc
			iScale1 = pRef.iFramePoc - iCurrPoc
			if iScale0 == 0 {
				iMVs[0] = 0
				iMVs[1] = 0
			} else {
				iMVs[0] = int16(pCtx.iECMVs[0][0] * iScale1 / iScale0)
				iMVs[1] = int16(pCtx.iECMVs[0][1] * iScale1 / iScale0)
			}
		}
		pMCRefMem.pDstY = pDec.pData[0]
		pMCRefMem.iDstYOff = iDstOff[0]
		pMCRefMem.pDstU = pDec.pData[1]
		pMCRefMem.iDstUOff = iDstOff[1]
		pMCRefMem.pDstV = pDec.pData[2]
		pMCRefMem.iDstVOff = iDstOff[2]
		iFullMVx := (iMbXInPix << 2) + int32(iMVs[0]) //quarter pixel
		iFullMVy := (iMbYInPix << 2) + int32(iMVs[1])
		// only use to be output pixels to EC;
		var iPicWidthLeftLimit int32
		var iPicHeightTopLimit int32
		iPicWidthRightLimit := pMCRefMem.iPicWidth
		iPicHeightBottomLimit := pMCRefMem.iPicHeight
		if pCtx.pSps.bFrameCroppingFlag {
			iPicWidthLeftLimit = 0 + pCtx.sFrameCrop.iLeftOffset*2
			iPicWidthRightLimit = (pMCRefMem.iPicWidth - pCtx.sFrameCrop.iRightOffset*2)
			iPicHeightTopLimit = 0 + pCtx.sFrameCrop.iTopOffset*2
			iPicHeightBottomLimit = (pMCRefMem.iPicHeight - pCtx.sFrameCrop.iTopOffset*2)
		}
		// further make sure no need to expand picture
		iMinLeftOffset := (iPicWidthLeftLimit + 2) * (1 << 2)
		iMaxRightOffset := ((iPicWidthRightLimit - 18) * (1 << 2))
		iMinTopOffset := (iPicHeightTopLimit + 2) * (1 << 2)
		iMaxBottomOffset := ((iPicHeightBottomLimit - 18) * (1 << 2))
		if iFullMVx < iMinLeftOffset {
			iFullMVx = (iFullMVx >> 2) * (1 << 2)
			iFullMVx = common.WELS_MAX(iPicWidthLeftLimit, iFullMVx)
		} else if iFullMVx > iMaxRightOffset {
			iFullMVx = (iFullMVx >> 2) * (1 << 2)
			iFullMVx = common.WELS_MIN(((iPicWidthRightLimit - 16) * (1 << 2)), iFullMVx)
		}
		if iFullMVy < iMinTopOffset {
			iFullMVy = (iFullMVy >> 2) * (1 << 2)
			iFullMVy = common.WELS_MAX(iPicHeightTopLimit, iFullMVy)
		} else if iFullMVy > iMaxBottomOffset {
			iFullMVy = (iFullMVy >> 2) * (1 << 2)
			iFullMVy = common.WELS_MIN(((iPicHeightBottomLimit - 16) * (1 << 2)), iFullMVy)
		}
		iMVs[0] = int16(iFullMVx - (iMbXInPix << 2))
		iMVs[1] = int16(iFullMVy - (iMbYInPix << 2))
		BaseMC(pCtx, pMCRefMem, -1, -1, iMbXInPix, iMbYInPix, &pCtx.sMcFunc, 16, 16, &iMVs)
	}
}

// ecRefIdxValid guards the C array accesses iECMVs[iRefIdx] / pECRefPic[iRefIdx]
// (16 entries). A correctly decoded P MB always has 0 <= iRefIdx < 16; the C
// code would index out of bounds otherwise, which the Go port skips instead
// of panicking.
func ecRefIdxValid(iRefIdx int8) bool {
	return iRefIdx >= 0 && iRefIdx < 16
}

// GetAvilInfoFromCorrectMb ports void GetAvilInfoFromCorrectMb (PWelsDecoderContext pCtx).
func GetAvilInfoFromCorrectMb(pCtx *SWelsDecoderContext) {
	iMbWidth := int32(pCtx.pSps.iMbWidth)
	iMbHeight := int32(pCtx.pSps.iMbHeight)
	pMbCorrectlyDecodedFlag := pCtx.pCurDqLayer.pMbCorrectlyDecodedFlag
	pCurDqLayer := pCtx.pCurDqLayer
	var iInterMbCorrectNum [16]int32
	var iMbXyIndex int32

	var iRefIdx int8
	pCtx.iECMVs = [16][2]int32{}
	pCtx.pECRefPic = [16]*SPicture{}

	// accumulate adds the MV of 4x4 block iBlk to iECMVs[iRefIdx].
	accumulate := func(iRefIdx int8, iBlk int) {
		pCtx.iECMVs[iRefIdx][0] += int32(pCurDqLayer.pDec.pMv[0][iMbXyIndex][iBlk][0])
		pCtx.iECMVs[iRefIdx][1] += int32(pCurDqLayer.pDec.pMv[0][iMbXyIndex][iBlk][1])
	}

	for iMbY := int32(0); iMbY < iMbHeight; iMbY++ {
		for iMbX := int32(0); iMbX < iMbWidth; iMbX++ {
			iMbXyIndex = iMbY*iMbWidth + iMbX
			if pMbCorrectlyDecodedFlag[iMbXyIndex] && common.IS_INTER(pCurDqLayer.pDec.pMbType[iMbXyIndex]) {
				iMBType := pCurDqLayer.pDec.pMbType[iMbXyIndex]
				switch iMBType {
				case common.MB_TYPE_SKIP, common.MB_TYPE_16x16:
					iRefIdx = pCurDqLayer.pDec.pRefIndex[0][iMbXyIndex][0]
					if ecRefIdxValid(iRefIdx) {
						accumulate(iRefIdx, 0)
						pCtx.pECRefPic[iRefIdx] = pCtx.sRefPic.pRefList[common.LIST_0][iRefIdx]
						iInterMbCorrectNum[iRefIdx]++
					}
				case common.MB_TYPE_16x8:
					iRefIdx = pCurDqLayer.pDec.pRefIndex[0][iMbXyIndex][0]
					if ecRefIdxValid(iRefIdx) {
						accumulate(iRefIdx, 0)
						pCtx.pECRefPic[iRefIdx] = pCtx.sRefPic.pRefList[common.LIST_0][iRefIdx]
						iInterMbCorrectNum[iRefIdx]++
					}

					iRefIdx = pCurDqLayer.pDec.pRefIndex[0][iMbXyIndex][8]
					if ecRefIdxValid(iRefIdx) {
						accumulate(iRefIdx, 8)
						pCtx.pECRefPic[iRefIdx] = pCtx.sRefPic.pRefList[common.LIST_0][iRefIdx]
						iInterMbCorrectNum[iRefIdx]++
					}
				case common.MB_TYPE_8x16:
					iRefIdx = pCurDqLayer.pDec.pRefIndex[0][iMbXyIndex][0]
					if ecRefIdxValid(iRefIdx) {
						accumulate(iRefIdx, 0)
						pCtx.pECRefPic[iRefIdx] = pCtx.sRefPic.pRefList[common.LIST_0][iRefIdx]
						iInterMbCorrectNum[iRefIdx]++
					}

					iRefIdx = pCurDqLayer.pDec.pRefIndex[0][iMbXyIndex][2]
					if ecRefIdxValid(iRefIdx) {
						accumulate(iRefIdx, 2)
						pCtx.pECRefPic[iRefIdx] = pCtx.sRefPic.pRefList[common.LIST_0][iRefIdx]
						iInterMbCorrectNum[iRefIdx]++
					}
				case common.MB_TYPE_8x8, common.MB_TYPE_8x8_REF0:
					var iSubMBType uint32
					var i, j, iIIdx, iJIdx int32

					for i = 0; i < 4; i++ {
						iSubMBType = pCurDqLayer.pSubMbType[iMbXyIndex][i]
						iIIdx = ((i >> 1) << 3) + ((i & 1) << 1)
						iRefIdx = pCurDqLayer.pDec.pRefIndex[0][iMbXyIndex][iIIdx]
						if !ecRefIdxValid(iRefIdx) {
							continue
						}
						pCtx.pECRefPic[iRefIdx] = pCtx.sRefPic.pRefList[common.LIST_0][iRefIdx]
						switch iSubMBType {
						case common.SUB_MB_TYPE_8x8:
							accumulate(iRefIdx, int(iIIdx))
							iInterMbCorrectNum[iRefIdx]++

						case common.SUB_MB_TYPE_8x4:
							accumulate(iRefIdx, int(iIIdx))

							accumulate(iRefIdx, int(iIIdx+4))
							iInterMbCorrectNum[iRefIdx] += 2

						case common.SUB_MB_TYPE_4x8:
							accumulate(iRefIdx, int(iIIdx))

							accumulate(iRefIdx, int(iIIdx+1))
							iInterMbCorrectNum[iRefIdx] += 2
						case common.SUB_MB_TYPE_4x4:
							for j = 0; j < 4; j++ {
								iJIdx = ((j >> 1) << 2) + (j & 1)
								accumulate(iRefIdx, int(iIIdx+iJIdx))
							}
							iInterMbCorrectNum[iRefIdx] += 4
						default:
						}
					}
				default:
				}
			} //pMbCorrectlyDecodedFlag[iMbXyIndex]
		} //iMbX
	} //iMbY
	for i := 0; i < 16; i++ {
		if iInterMbCorrectNum[i] != 0 {
			pCtx.iECMVs[i][0] = pCtx.iECMVs[i][0] / iInterMbCorrectNum[i]
			pCtx.iECMVs[i][1] = pCtx.iECMVs[i][1] / iInterMbCorrectNum[i]
		}
	}
}

// DoErrorConSliceMVCopy ports void DoErrorConSliceMVCopy (PWelsDecoderContext pCtx).
func DoErrorConSliceMVCopy(pCtx *SWelsDecoderContext) {
	iMbWidth := int32(pCtx.pSps.iMbWidth)
	iMbHeight := int32(pCtx.pSps.iMbHeight)
	pDstPic := pCtx.pDec
	pSrcPic := pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb

	pMbCorrectlyDecodedFlag := pCtx.pCurDqLayer.pMbCorrectlyDecodedFlag
	var iMbXyIndex int32
	iDstStride := int(uint32(pDstPic.iLinesize[0]))
	var sMCRefMem sMCRefMember
	if pSrcPic != nil {
		sMCRefMem.iSrcLineLuma = pSrcPic.iLinesize[0]
		sMCRefMem.iSrcLineChroma = pSrcPic.iLinesize[1]
		sMCRefMem.pSrcY = pSrcPic.pData[0]
		sMCRefMem.iSrcYOff = pSrcPic.iDataOff[0]
		sMCRefMem.pSrcU = pSrcPic.pData[1]
		sMCRefMem.iSrcUOff = pSrcPic.iDataOff[1]
		sMCRefMem.pSrcV = pSrcPic.pData[2]
		sMCRefMem.iSrcVOff = pSrcPic.iDataOff[2]
		sMCRefMem.iDstLineLuma = pDstPic.iLinesize[0]
		sMCRefMem.iDstLineChroma = pDstPic.iLinesize[1]
		sMCRefMem.iPicWidth = pDstPic.iWidthInPixel
		sMCRefMem.iPicHeight = pDstPic.iHeightInPixel
		if pDstPic == pSrcPic {
			// output error info, EC will be ignored in DoMbECMvCopy
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "DoErrorConSliceMVCopy()::EC memcpy overlap.")
			return
		}
	}

	for iMbY := int32(0); iMbY < iMbHeight; iMbY++ {
		for iMbX := int32(0); iMbX < iMbWidth; iMbX++ {
			iMbXyIndex = iMbY*iMbWidth + iMbX
			if !pMbCorrectlyDecodedFlag[iMbXyIndex] {
				pCtx.pDec.iMbEcedNum++
				if pSrcPic != nil {
					DoMbECMvCopy(pCtx, pDstPic, pSrcPic, iMbXyIndex, iMbX, iMbY, &sMCRefMem)
				} else { //pSrcPic == NULL
					ecFillMb128(pDstPic, iMbX, iMbY, iDstStride)
				} //

			} //!pMbCorrectlyDecodedFlag[iMbXyIndex]
		} //iMbX
	} //iMbY
}

// MarkECFrameAsRef ports int32_t MarkECFrameAsRef (PWelsDecoderContext pCtx).
//
// Mark erroneous frame as Ref Pic into DPB
func MarkECFrameAsRef(pCtx *SWelsDecoderContext) int32 {
	iRet := WelsMarkAsRef(pCtx, nil)
	// Under EC mode, the ERR_INFO_DUPLICATE_FRAME_NUM does not need to be process
	if iRet != ERR_NONE {
		return iRet
	}
	common.ExpandReferencingPicture(pCtx.pDec.pData[:], pCtx.pDec.iDataOff[:], pCtx.pDec.iWidthInPixel, pCtx.pDec.iHeightInPixel,
		pCtx.pDec.iLinesize[:],
		pCtx.sExpandPicFunc.PfExpandLumaPicture, pCtx.sExpandPicFunc.PfExpandChromaPicture)

	return ERR_NONE
}

// NeedErrorCon ports bool NeedErrorCon (PWelsDecoderContext pCtx).
func NeedErrorCon(pCtx *SWelsDecoderContext) bool {
	bNeedEC := false
	iMbNum := int32(pCtx.pSps.iMbWidth * pCtx.pSps.iMbHeight)
	for i := int32(0); i < iMbNum; i++ {
		if !pCtx.pCurDqLayer.pMbCorrectlyDecodedFlag[i] {
			bNeedEC = true
			break
		}
	}
	return bNeedEC
}

// ImplementErrorCon ports void ImplementErrorCon (PWelsDecoderContext pCtx).
//
// ImplementErrorConceal
// Do actual error concealment
func ImplementErrorCon(pCtx *SWelsDecoderContext) {
	eEc := pCtx.pParam.EEcActiveIdc
	if api.ERROR_CON_DISABLE == eEc {
		pCtx.iErrorCode |= int32(api.DsBitstreamError)
		return
	} else if (api.ERROR_CON_FRAME_COPY == eEc) ||
		(api.ERROR_CON_FRAME_COPY_CROSS_IDR == eEc) {
		DoErrorConFrameCopy(pCtx)
	} else if (api.ERROR_CON_SLICE_COPY == eEc) ||
		(api.ERROR_CON_SLICE_COPY_CROSS_IDR == eEc) ||
		(api.ERROR_CON_SLICE_COPY_CROSS_IDR_FREEZE_RES_CHANGE == eEc) {
		DoErrorConSliceCopy(pCtx)
	} else if (api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR == eEc) ||
		(api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE == eEc) {
		GetAvilInfoFromCorrectMb(pCtx)
		DoErrorConSliceMVCopy(pCtx)
	} //TODO add other EC methods here in the future
	pCtx.iErrorCode |= int32(api.DsDataErrorConcealed)
	pCtx.pDec.bIsComplete = false // Set complete flag to false after do EC.
}
