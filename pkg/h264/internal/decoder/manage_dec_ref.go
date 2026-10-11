// Port of codec/decoder/core/src/manage_dec_ref.cpp.
//
// Reference picture management: list initialisation (P and B slices),
// reordering, marking (sliding window and MMCO) and error-concealment
// recovery of the DPB.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// SetUnRef (static).
func SetUnRef(pRef *SPicture) {
	if pRef == nil {
		return
	}

	if pRef.iRefCount <= 0 {
		pRef.bUsedAsRef = false
		pRef.bIsLongRef = false
		pRef.iFrameNum = -1
		pRef.iFrameWrapNum = -1
		//pRef->iFramePoc = 0;
		pRef.iLongTermFrameIdx = -1
		pRef.uiLongTermPicNum = 0
		pRef.uiQualityId = 0xff  // C: (uint8_t)-1
		pRef.uiTemporalId = 0xff // C: (uint8_t)-1
		pRef.uiSpatialId = 0xff  // C: (uint8_t)-1
		pRef.iSpsId = -1
		pRef.bIsComplete = false
		pRef.iRefCount = 0
		pRef.pSetUnRef = nil

		if pRef.eSliceType == common.I_SLICE {
			return
		}
		lists := 2
		if pRef.eSliceType == common.P_SLICE {
			lists = 1
		}
		for i := 0; i < MAX_DPB_COUNT; i++ {
			for list := 0; list < lists; list++ {
				pRef.pRefPic[list][i] = nil
			}
		}
	} else {
		pRef.pSetUnRef = SetUnRef
	}
}

// WelsResetRefPic ports void WelsResetRefPic (PWelsDecoderContext pCtx).
//
// reset pRefList when
// 1.sps arrived that is new sequence starting
// 2.IDR NAL i.e. 1st layer in IDR AU
func WelsResetRefPic(pCtx *SWelsDecoderContext) {
	pRefPic := &pCtx.sRefPic
	pCtx.sRefPic.uiShortRefCount[common.LIST_0] = 0
	pCtx.sRefPic.uiLongRefCount[common.LIST_0] = 0

	pRefPic.uiRefCount[common.LIST_0] = 0
	pRefPic.uiRefCount[common.LIST_1] = 0

	for i := 0; i < MAX_DPB_COUNT; i++ {
		if pRefPic.pShortRefList[common.LIST_0][i] != nil {
			SetUnRef(pRefPic.pShortRefList[common.LIST_0][i])
			pRefPic.pShortRefList[common.LIST_0][i] = nil
		}
	}
	pRefPic.uiShortRefCount[common.LIST_0] = 0

	for i := 0; i < MAX_DPB_COUNT; i++ {
		if pRefPic.pLongRefList[common.LIST_0][i] != nil {
			SetUnRef(pRefPic.pLongRefList[common.LIST_0][i])
			pRefPic.pLongRefList[common.LIST_0][i] = nil
		}
	}
	pRefPic.uiLongRefCount[common.LIST_0] = 0
}

// IsLivePicture (static): is this pointer still one of the pictures the
// buffer owns? A context that has been overtaken by a DPB reallocation can
// list pictures that no longer exist, and those must not be touched.
func IsLivePicture(pPicBuf *SPicBuff, pPic *SPicture) bool {
	if pPicBuf == nil || pPic == nil {
		return false
	}
	for i := int32(0); i < pPicBuf.iCapacity; i++ {
		if pPicBuf.ppPic[i] == pPic {
			return true
		}
	}
	return false
}

// WelsReleaseDroppedRefs ports void WelsReleaseDroppedRefs (PPicBuff pPicBuf, PRefPic pDst,
// PRefPic pSrc).
//
// Release what a whole-struct copy of SRefPic is about to drop. Every picture the destination
// still lists, and the source does not, leaves the reference lists when the copy lands; without
// this it keeps bUsedAsRef set with no list entry and no armed unref, and its buffer never
// returns to the pool.
func WelsReleaseDroppedRefs(pPicBuf *SPicBuff, pDst *SRefPic, pSrc *SRefPic) {
	if pPicBuf == nil || pDst == nil || pSrc == nil || pDst == pSrc {
		return
	}
	kiShort := common.WELS_MIN(int32(pDst.uiShortRefCount[common.LIST_0]), MAX_DPB_COUNT)
	kiLong := common.WELS_MIN(int32(pDst.uiLongRefCount[common.LIST_0]), MAX_DPB_COUNT)
	var pHeld [MAX_DPB_COUNT * 2]*SPicture
	iHeld := 0
	for i := int32(0); i < kiShort; i++ {
		if IsLivePicture(pPicBuf, pDst.pShortRefList[common.LIST_0][i]) {
			pHeld[iHeld] = pDst.pShortRefList[common.LIST_0][i]
			iHeld++
		}
	}
	for i := int32(0); i < kiLong; i++ {
		if IsLivePicture(pPicBuf, pDst.pLongRefList[common.LIST_0][i]) {
			pHeld[iHeld] = pDst.pLongRefList[common.LIST_0][i]
			iHeld++
		}
	}

	for i := 0; i < iHeld; i++ {
		pPic := pHeld[i]
		if !pPic.bUsedAsRef {
			continue
		}
		bKept := false
		kiSrcShort := common.WELS_MIN(int32(pSrc.uiShortRefCount[common.LIST_0]), MAX_DPB_COUNT)
		kiSrcLong := common.WELS_MIN(int32(pSrc.uiLongRefCount[common.LIST_0]), MAX_DPB_COUNT)
		for j := int32(0); !bKept && j < kiSrcShort; j++ {
			bKept = (pSrc.pShortRefList[common.LIST_0][j] == pPic)
		}
		for j := int32(0); !bKept && j < kiSrcLong; j++ {
			bKept = (pSrc.pLongRefList[common.LIST_0][j] == pPic)
		}
		if !bKept {
			SetUnRef(pPic)
		}
	}
}

// WelsResetRefPicWithoutUnRef ports void WelsResetRefPicWithoutUnRef (PWelsDecoderContext pCtx).
func WelsResetRefPicWithoutUnRef(pCtx *SWelsDecoderContext) {
	pRefPic := &pCtx.sRefPic
	pCtx.sRefPic.uiShortRefCount[common.LIST_0] = 0
	pCtx.sRefPic.uiLongRefCount[common.LIST_0] = 0

	pRefPic.uiRefCount[common.LIST_0] = 0
	pRefPic.uiRefCount[common.LIST_1] = 0

	for i := 0; i < MAX_DPB_COUNT; i++ {
		pRefPic.pShortRefList[common.LIST_0][i] = nil
	}
	pRefPic.uiShortRefCount[common.LIST_0] = 0

	for i := 0; i < MAX_DPB_COUNT; i++ {
		pRefPic.pLongRefList[common.LIST_0][i] = nil
	}
	pRefPic.uiLongRefCount[common.LIST_0] = 0
}

// memsetPlane does memset (pData + iOff, v, n).
func memsetPlane(pData []uint8, iOff int, v uint8, n int) {
	if n <= 0 {
		return
	}
	b := pData[iOff : iOff+n]
	for i := range b {
		b[i] = v
	}
}

// WelsCheckAndRecoverForFutureDecoding (static).
func WelsCheckAndRecoverForFutureDecoding(pCtx *SWelsDecoderContext) int32 {
	if (int32(pCtx.sRefPic.uiShortRefCount[common.LIST_0])+int32(pCtx.sRefPic.uiLongRefCount[common.LIST_0]) <= 0) &&
		(pCtx.eSliceType != common.I_SLICE &&
			pCtx.eSliceType != common.SI_SLICE) {
		if pCtx.pParam.EEcActiveIdc !=
			api.ERROR_CON_DISABLE { //IDR lost!, recover it for future decoding with data all set to 0
			pRef := PrefetchPic(pCtx.pPicBuff)
			if pRef != nil {
				// IDR lost, set new
				pRef.bIsComplete = false // Set complete flag to false for lost IDR ref picture
				pRef.iSpsId = pCtx.pSps.iSpsId
				pRef.iPpsId = pCtx.pPps.iPpsId
				if pCtx.eSliceType == common.B_SLICE {
					//reset reference's references when IDR is lost
					for list := common.LIST_0; list < common.LIST_A; list++ {
						for i := 0; i < MAX_DPB_COUNT; i++ {
							pRef.pRefPic[list][i] = nil
						}
					}
				}
				pCtx.iErrorCode |= int32(api.DsDataErrorConcealed)
				eEc := pCtx.pParam.EEcActiveIdc
				pPrev := pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb
				bCopyPrevious := ((api.ERROR_CON_FRAME_COPY_CROSS_IDR == eEc) ||
					(api.ERROR_CON_SLICE_COPY_CROSS_IDR == eEc) ||
					(api.ERROR_CON_SLICE_COPY_CROSS_IDR_FREEZE_RES_CHANGE == eEc) ||
					(api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR == eEc) ||
					(api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE == eEc)) &&
					(nil != pPrev)
				bCopyPrevious = bCopyPrevious &&
					(pRef.iWidthInPixel == pPrev.iWidthInPixel) &&
					(pRef.iHeightInPixel == pPrev.iHeightInPixel)

				if !bCopyPrevious {
					memsetPlane(pRef.pData[0], pRef.iDataOff[0], 128, int(pRef.iLinesize[0]*pRef.iHeightInPixel))
					memsetPlane(pRef.pData[1], pRef.iDataOff[1], 128, int(pRef.iLinesize[1]*pRef.iHeightInPixel/2))
					memsetPlane(pRef.pData[2], pRef.iDataOff[2], 128, int(pRef.iLinesize[2]*pRef.iHeightInPixel/2))
				} else if pRef == pPrev {
					common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "WelsInitRefList()::EC memcpy overlap.")
				} else {
					n0 := int(pRef.iLinesize[0] * pRef.iHeightInPixel)
					n1 := int(pRef.iLinesize[1] * pRef.iHeightInPixel / 2)
					n2 := int(pRef.iLinesize[2] * pRef.iHeightInPixel / 2)
					copy(pRef.pData[0][pRef.iDataOff[0]:pRef.iDataOff[0]+n0], pPrev.pData[0][pPrev.iDataOff[0]:pPrev.iDataOff[0]+n0])
					copy(pRef.pData[1][pRef.iDataOff[1]:pRef.iDataOff[1]+n1], pPrev.pData[1][pPrev.iDataOff[1]:pPrev.iDataOff[1]+n1])
					copy(pRef.pData[2][pRef.iDataOff[2]:pRef.iDataOff[2]+n2], pPrev.pData[2][pPrev.iDataOff[2]:pPrev.iDataOff[2]+n2])
				}
				pRef.iFrameNum = 0
				pRef.iFramePoc = 0
				pRef.uiTemporalId = 0
				pRef.uiQualityId = 0
				pRef.eSliceType = pCtx.eSliceType
				common.ExpandReferencingPicture(pRef.pData[:], pRef.iDataOff[:], pRef.iWidthInPixel, pRef.iHeightInPixel, pRef.iLinesize[:],
					pCtx.sExpandPicFunc.PfExpandLumaPicture, pCtx.sExpandPicFunc.PfExpandChromaPicture)
				AddShortTermToList(&pCtx.sRefPic, pRef)
			} else {
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "WelsInitRefList()::PrefetchPic for EC errors.")
				pCtx.iErrorCode |= int32(api.DsOutOfMemory)
				return ERR_INFO_REF_COUNT_OVERFLOW
			}
		}
	}
	return ERR_NONE
}

// WrapShortRefPicNum (static).
func WrapShortRefPicNum(pCtx *SWelsDecoderContext) {
	pSliceHeader := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader
	iMaxPicNum := int32(1) << pSliceHeader.pSps.uiLog2MaxFrameNum
	ppShoreRefList := &pCtx.sRefPic.pShortRefList[common.LIST_0]
	iShortRefCount := int32(pCtx.sRefPic.uiShortRefCount[common.LIST_0])
	//wrap pic num
	for i := int32(0); i < iShortRefCount; i++ {
		if ppShoreRefList[i] != nil {
			if ppShoreRefList[i].iFrameNum > pSliceHeader.iFrameNum {
				ppShoreRefList[i].iFrameWrapNum = ppShoreRefList[i].iFrameNum - iMaxPicNum
			} else {
				ppShoreRefList[i].iFrameWrapNum = ppShoreRefList[i].iFrameNum
			}
		}
	}
}

// WelsInitBSliceRefList ports int32_t WelsInitBSliceRefList (PWelsDecoderContext pCtx, int32_t iPoc).
//
// fills the pRefPic.pRefList LIST_0 and LIST_0 for B-Slice.
func WelsInitBSliceRefList(pCtx *SWelsDecoderContext, iPoc int32) int32 {

	err := WelsCheckAndRecoverForFutureDecoding(pCtx)
	if err != ERR_NONE {
		return err
	}

	WrapShortRefPicNum(pCtx)

	ppShoreRefList := &pCtx.sRefPic.pShortRefList[common.LIST_0]
	ppLongRefList := &pCtx.sRefPic.pLongRefList[common.LIST_0]
	pCtx.sRefPic.pRefList[common.LIST_0] = [MAX_DPB_COUNT]*SPicture{}
	pCtx.sRefPic.pRefList[common.LIST_1] = [MAX_DPB_COUNT]*SPicture{}
	pRefList0 := &pCtx.sRefPic.pRefList[common.LIST_0]
	pRefList1 := &pCtx.sRefPic.pRefList[common.LIST_1]
	var iLSCurrPocCount int32
	var iLTCurrPocCount int32
	var pLSCurrPocList0 [MAX_DPB_COUNT]*SPicture
	var pLTCurrPocList0 [MAX_DPB_COUNT]*SPicture
	iShortRefCount := int32(pCtx.sRefPic.uiShortRefCount[common.LIST_0])
	for i := int32(0); i < iShortRefCount; i++ {
		if ppShoreRefList[i].iFramePoc < iPoc {
			pLSCurrPocList0[iLSCurrPocCount] = ppShoreRefList[i]
			iLSCurrPocCount++
		}
	}
	for i := iShortRefCount - 1; i >= 0; i-- {
		if ppShoreRefList[i].iFramePoc > iPoc {
			pLTCurrPocList0[iLTCurrPocCount] = ppShoreRefList[i]
			iLTCurrPocCount++
		}
	}
	iLongRefCount := int32(pCtx.sRefPic.uiLongRefCount[common.LIST_0])
	if iLongRefCount > 1 {
		//long sorts in increasing order
		for i := int32(0); i < iLongRefCount; i++ {
			for j := i + 1; j < iLongRefCount; j++ {
				if ppLongRefList[j].iFramePoc < ppLongRefList[i].iFramePoc {
					ppLongRefList[i], ppLongRefList[j] = ppLongRefList[j], ppLongRefList[i]
				}
			}
		}
	}
	iCurrPocCount := iLSCurrPocCount + iLTCurrPocCount
	var iCount int32
	//LIST_0
	//short
	//It may need to sort LIST_0 and LIST_1 so that they will have the right default orders.
	for i := int32(0); i < iLSCurrPocCount; i++ {
		pRefList0[iCount] = pLSCurrPocList0[i]
		iCount++
	}
	if iLSCurrPocCount > 1 {
		//LIST_0 short sorts in decreasing order
		for i := int32(0); i < iLSCurrPocCount; i++ {
			for j := i + 1; j < iLSCurrPocCount; j++ {
				if pRefList0[j].iFramePoc > pRefList0[i].iFramePoc {
					pRefList0[i], pRefList0[j] = pRefList0[j], pRefList0[i]
				}
			}
		}
	}
	for i := int32(0); i < iLTCurrPocCount; i++ {
		pRefList0[iCount] = pLTCurrPocList0[i]
		iCount++
	}
	if iLTCurrPocCount > 1 {
		//LIST_0 short sorts in increasing order
		for i := iLSCurrPocCount; i < iCurrPocCount; i++ {
			for j := i + 1; j < iCurrPocCount; j++ {
				if pRefList0[j].iFramePoc < pRefList0[i].iFramePoc {
					pRefList0[i], pRefList0[j] = pRefList0[j], pRefList0[i]
				}
			}
		}
	}
	//long
	for i := int32(0); i < iLongRefCount; i++ {
		pRefList0[iCount] = ppLongRefList[i]
		iCount++
	}
	pCtx.sRefPic.uiRefCount[common.LIST_0] = uint8(iCount)

	iCount = 0
	//LIST_1
	//short
	for i := int32(0); i < iLTCurrPocCount; i++ {
		pRefList1[iCount] = pLTCurrPocList0[i]
		iCount++
	}
	if iLTCurrPocCount > 1 {
		//LIST_1 short sorts in increasing order
		for i := int32(0); i < iLTCurrPocCount; i++ {
			for j := i + 1; j < iLTCurrPocCount; j++ {
				if pRefList1[j].iFramePoc < pRefList1[i].iFramePoc {
					pRefList1[i], pRefList1[j] = pRefList1[j], pRefList1[i]
				}
			}
		}
	}
	for i := int32(0); i < iLSCurrPocCount; i++ {
		pRefList1[iCount] = pLSCurrPocList0[i]
		iCount++
	}
	if iLSCurrPocCount > 1 {
		//LIST_1 short sorts in decreasing order
		for i := iLTCurrPocCount; i < iCurrPocCount; i++ {
			for j := i + 1; j < iCurrPocCount; j++ {
				if pRefList1[j].iFramePoc > pRefList1[i].iFramePoc {
					pRefList1[i], pRefList1[j] = pRefList1[j], pRefList1[i]
				}
			}
		}
	}
	//long
	for i := int32(0); i < iLongRefCount; i++ {
		pRefList1[iCount] = ppLongRefList[i]
		iCount++
	}
	pCtx.sRefPic.uiRefCount[common.LIST_1] = uint8(iCount)
	return ERR_NONE
}

// WelsInitRefList ports int32_t WelsInitRefList (PWelsDecoderContext pCtx, int32_t iPoc).
//
// fills the pRefPic.pRefList.
func WelsInitRefList(pCtx *SWelsDecoderContext, iPoc int32) int32 {

	err := WelsCheckAndRecoverForFutureDecoding(pCtx)
	if err != ERR_NONE {
		return err
	}

	WrapShortRefPicNum(pCtx)

	ppShoreRefList := &pCtx.sRefPic.pShortRefList[common.LIST_0]
	ppLongRefList := &pCtx.sRefPic.pLongRefList[common.LIST_0]
	pCtx.sRefPic.pRefList[common.LIST_0] = [MAX_DPB_COUNT]*SPicture{}

	var i, iCount int32
	//short
	for i = 0; i < int32(pCtx.sRefPic.uiShortRefCount[common.LIST_0]) && iCount < MAX_REF_PIC_COUNT; i++ {
		pCtx.sRefPic.pRefList[common.LIST_0][iCount] = ppShoreRefList[i]
		iCount++
	}

	//long
	for i = 0; i < int32(pCtx.sRefPic.uiLongRefCount[common.LIST_0]) && iCount < MAX_REF_PIC_COUNT; i++ {
		pCtx.sRefPic.pRefList[common.LIST_0][iCount] = ppLongRefList[i]
		iCount++
	}
	pCtx.sRefPic.uiRefCount[common.LIST_0] = uint8(iCount)

	return ERR_NONE
}

// WelsReorderRefList ports int32_t WelsReorderRefList (PWelsDecoderContext pCtx).
func WelsReorderRefList(pCtx *SWelsDecoderContext) int32 {

	if pCtx.eSliceType == common.I_SLICE || pCtx.eSliceType == common.SI_SLICE {
		return ERR_NONE
	}

	pRefPicListReorderSyn := pCtx.pCurDqLayer.pRefPicListReordering
	pNalHeaderExt := &pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt
	pSliceHeader := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader
	ListCount := int32(1)
	if pCtx.eSliceType == common.B_SLICE {
		ListCount = 2
	}
	for listIdx := int32(0); listIdx < ListCount; listIdx++ {
		var pPic *SPicture
		ppRefList := &pCtx.sRefPic.pRefList[listIdx]
		iMaxRefIdx := pCtx.iPicQueueNumber
		if iMaxRefIdx > MAX_REF_PIC_COUNT {
			iMaxRefIdx = MAX_REF_PIC_COUNT
		}
		iRefCount := pSliceHeader.uiRefCount[listIdx]
		iPredFrameNum := pSliceHeader.iFrameNum
		iMaxPicNum := int32(1) << pSliceHeader.pSps.uiLog2MaxFrameNum
		var iAbsDiffPicNum int32 = -1
		var iReorderingIndex int32
		var i int32

		if iRefCount <= 0 {
			pCtx.iErrorCode = int32(api.DsNoParamSets) //No any reference for decoding, SHOULD request IDR
			return ERR_INFO_REFERENCE_PIC_LOST
		}

		if pRefPicListReorderSyn.bRefPicListReorderingFlag[listIdx] {
			for (iReorderingIndex <= iMaxRefIdx) &&
				(pRefPicListReorderSyn.sReorderingSyn[listIdx][iReorderingIndex].uiReorderingOfPicNumsIdc != 3) {
				uiReorderingOfPicNumsIdc :=
					pRefPicListReorderSyn.sReorderingSyn[listIdx][iReorderingIndex].uiReorderingOfPicNumsIdc
				if uiReorderingOfPicNumsIdc < 2 {
					iAbsDiffPicNum = int32(pRefPicListReorderSyn.sReorderingSyn[listIdx][iReorderingIndex].uiAbsDiffPicNumMinus1 + 1)

					if uiReorderingOfPicNumsIdc == 0 {
						iPredFrameNum -= iAbsDiffPicNum
					} else {
						iPredFrameNum += iAbsDiffPicNum
					}
					iPredFrameNum &= iMaxPicNum - 1

					for i = iMaxRefIdx - 1; i >= 0; i-- {
						if ppRefList[i] != nil && ppRefList[i].iFrameNum == iPredFrameNum && !ppRefList[i].bIsLongRef {
							if (pNalHeaderExt.UiQualityId == ppRefList[i].uiQualityId) &&
								(pSliceHeader.iSpsId != ppRefList[i].iSpsId) { //check;
								common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "WelsReorderRefList()::::BASE LAYER::::iSpsId:%d, ref_sps_id:%d",
									pSliceHeader.iSpsId, ppRefList[i].iSpsId)
								pCtx.iErrorCode = int32(api.DsNoParamSets) //cross-IDR reference frame selection, SHOULD request IDR.--
								return ERR_INFO_REFERENCE_PIC_LOST
							} else {
								break
							}
						}
					}

				} else if uiReorderingOfPicNumsIdc == 2 {
					for i = iMaxRefIdx - 1; i >= 0; i-- {
						if ppRefList[i] != nil && ppRefList[i].bIsLongRef &&
							ppRefList[i].iLongTermFrameIdx ==
								int32(pRefPicListReorderSyn.sReorderingSyn[listIdx][iReorderingIndex].uiLongTermPicNum) {
							if (pNalHeaderExt.UiQualityId == ppRefList[i].uiQualityId) &&
								(pSliceHeader.iSpsId != ppRefList[i].iSpsId) { //check;
								common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "WelsReorderRefList()::::BASE LAYER::::iSpsId:%d, ref_sps_id:%d",
									pSliceHeader.iSpsId, ppRefList[i].iSpsId)
								pCtx.iErrorCode = int32(api.DsNoParamSets) //cross-IDR reference frame selection, SHOULD request IDR.--
								return ERR_INFO_REFERENCE_PIC_LOST
							} else {
								break
							}
						}
					}
				}
				if i < 0 {
					return ERR_INFO_REFERENCE_PIC_LOST
				}
				pPic = ppRefList[i]
				if i > iReorderingIndex {
					n := i - iReorderingIndex
					copy(ppRefList[1+iReorderingIndex:1+iReorderingIndex+n], ppRefList[iReorderingIndex:iReorderingIndex+n])
				} else if i < iReorderingIndex {
					n := iMaxRefIdx - iReorderingIndex
					copy(ppRefList[1+iReorderingIndex:1+iReorderingIndex+n], ppRefList[iReorderingIndex:iReorderingIndex+n])
				}
				ppRefList[iReorderingIndex] = pPic
				iReorderingIndex++
			}
		}
	}
	return ERR_NONE
}

// WelsReorderRefList2 ports int32_t WelsReorderRefList2 (PWelsDecoderContext pCtx).
//
// WelsReorderRefList2 is the test code
func WelsReorderRefList2(pCtx *SWelsDecoderContext) int32 {

	if pCtx.eSliceType == common.I_SLICE || pCtx.eSliceType == common.SI_SLICE {
		return ERR_NONE
	}

	pRefPicListReorderSyn := pCtx.pCurDqLayer.pRefPicListReordering
	pSliceHeader := &pCtx.pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader

	ppShoreRefList := &pCtx.sRefPic.pShortRefList[common.LIST_0]
	iShortRefCount := int32(pCtx.sRefPic.uiShortRefCount[common.LIST_0])
	ppLongRefList := &pCtx.sRefPic.pLongRefList[common.LIST_0]
	iLongRefCount := int32(pCtx.sRefPic.uiLongRefCount[common.LIST_0])
	var i, j, k int32
	iMaxRefIdx := pCtx.iPicQueueNumber
	if iMaxRefIdx > MAX_REF_PIC_COUNT {
		iMaxRefIdx = MAX_REF_PIC_COUNT
	}
	iCurFrameNum := pSliceHeader.iFrameNum
	iMaxPicNum := int32(1) << pSliceHeader.pSps.uiLog2MaxFrameNum
	iListCount := int32(1)
	if pCtx.eSliceType == common.B_SLICE {
		iListCount = 2
	}
	for listIdx := int32(0); listIdx < iListCount; listIdx++ {
		ppRefList := &pCtx.sRefPic.pRefList[listIdx]
		var iCount int32
		iRefCount := pSliceHeader.uiRefCount[listIdx]
		var iAbsDiffPicNum int32 = -1

		if pRefPicListReorderSyn.bRefPicListReorderingFlag[listIdx] {
			iPredFrameNum := iCurFrameNum
			for i = 0; pRefPicListReorderSyn.sReorderingSyn[listIdx][i].uiReorderingOfPicNumsIdc != 3; i++ {
				if iCount >= iMaxRefIdx {
					break
				}

				for j = iRefCount; j > iCount; j-- {
					ppRefList[j] = ppRefList[j-1]
				}

				uiReorderingOfPicNumsIdc :=
					pRefPicListReorderSyn.sReorderingSyn[listIdx][i].uiReorderingOfPicNumsIdc

				if uiReorderingOfPicNumsIdc < 2 { // reorder short references
					iAbsDiffPicNum = int32(pRefPicListReorderSyn.sReorderingSyn[listIdx][i].uiAbsDiffPicNumMinus1 + 1)
					if uiReorderingOfPicNumsIdc == 0 {
						if iPredFrameNum-iAbsDiffPicNum < 0 {
							iPredFrameNum -= (iAbsDiffPicNum - iMaxPicNum)
						} else {
							iPredFrameNum -= iAbsDiffPicNum
						}
					} else {
						if iPredFrameNum+iAbsDiffPicNum >= iMaxPicNum {
							iPredFrameNum += (iAbsDiffPicNum - iMaxPicNum)
						} else {
							iPredFrameNum += iAbsDiffPicNum
						}
					}

					if iPredFrameNum > iCurFrameNum {
						iPredFrameNum -= iMaxPicNum
					}

					for j = 0; j < iShortRefCount; j++ {
						if ppShoreRefList[j] != nil {
							if ppShoreRefList[j].iFrameWrapNum == iPredFrameNum {
								ppRefList[iCount] = ppShoreRefList[j]
								iCount++
								break
							}
						}
					}
					k = iCount
					for j = k; j <= iRefCount; j++ {
						if ppRefList[j] != nil {
							if ppRefList[j].bIsLongRef || ppRefList[j].iFrameWrapNum != iPredFrameNum {
								ppRefList[k] = ppRefList[j]
								k++
							}
						}
					}
				} else { // reorder long term references uiReorderingOfPicNumsIdc == 2
					iPredFrameNum = int32(pRefPicListReorderSyn.sReorderingSyn[listIdx][i].uiLongTermPicNum)
					for j = 0; j < iLongRefCount; j++ {
						if ppLongRefList[j] != nil {
							if ppLongRefList[j].uiLongTermPicNum == uint32(iPredFrameNum) {
								ppRefList[iCount] = ppLongRefList[j]
								iCount++
								break
							}
						}
					}
					k = iCount
					for j = k; j <= iRefCount; j++ {
						if ppRefList[j] != nil {
							if !ppRefList[j].bIsLongRef || ppRefList[j].uiLongTermPicNum != uint32(iPredFrameNum) {
								ppRefList[k] = ppRefList[j]
								k++
							}
						}
					}
				}
			}
		}

		for i = common.WELS_MAX(1, common.WELS_MAX(iCount, int32(pCtx.sRefPic.uiRefCount[listIdx]))); i < iRefCount; i++ {
			ppRefList[i] = ppRefList[i-1]
		}
		pCtx.sRefPic.uiRefCount[listIdx] = uint8(common.WELS_MIN(common.WELS_MAX(iCount, int32(pCtx.sRefPic.uiRefCount[listIdx])),
			iRefCount))
	}
	return ERR_NONE
}

// WelsMarkAsRef ports int32_t WelsMarkAsRef (PWelsDecoderContext pCtx, PPicture pLastDec).
//
// C++ default argument pLastDec = NULL: callers pass nil.
func WelsMarkAsRef(pCtx *SWelsDecoderContext, pLastDec *SPicture) int32 {
	pDec := pLastDec
	isThreadCtx := true
	if pDec == nil {
		pDec = pCtx.pDec
		isThreadCtx = false
	}
	pRefPic := &pCtx.sRefPic
	if isThreadCtx {
		pRefPic = &pCtx.sTmpRefPic
	}
	pRefPicMarking := pCtx.pCurDqLayer.pRefPicMarking
	pCurAU := pCtx.pAccessUnitList
	bIsIDRAU := false

	var iRet int32 = ERR_NONE

	pDec.uiQualityId = pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.UiQualityId
	pDec.uiTemporalId = pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.UiTemporalId
	pDec.iSpsId = pCtx.pSps.iSpsId
	pDec.iPpsId = pCtx.pPps.iPpsId

	for j := pCurAU.uiStartPos; j <= pCurAU.uiEndPos; j++ {
		if pCurAU.pNalUnitsList[j].sNalHeaderExt.SNalUnitHeader.ENalUnitType == common.NAL_UNIT_CODED_SLICE_IDR ||
			pCurAU.pNalUnitsList[j].sNalHeaderExt.BIdrFlag {
			bIsIDRAU = true
			break
		}
	}
	if bIsIDRAU {
		if pRefPicMarking.bLongTermRefFlag {
			pRefPic.iMaxLongTermFrameIdx = 0
			AddLongTermToList(pRefPic, pDec, 0, 0)
		} else {
			pRefPic.iMaxLongTermFrameIdx = -1
		}
	} else {
		if pRefPicMarking.bAdaptiveRefPicMarkingModeFlag {
			iRet = MMCO(pCtx, pRefPic, pRefPicMarking)
			if iRet != ERR_NONE {
				if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
					iRet = RemainOneBufferInDpbForEC(pCtx, pRefPic)
					if iRet != 0 {
						return iRet
					}
				} else {
					return iRet
				}
			}

			if pCtx.pLastDecPicInfo.bLastHasMmco5 {
				pDec.iFrameNum = 0
				pDec.iFramePoc = 0
			}

		} else {
			iRet = SlidingWindow(pCtx, pRefPic)
			if iRet != ERR_NONE {
				if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
					iRet = RemainOneBufferInDpbForEC(pCtx, pRefPic)
					if iRet != 0 {
						return iRet
					}
				} else {
					return iRet
				}
			}
		}
	}

	if !pDec.bIsLongRef {
		if int32(pRefPic.uiLongRefCount[common.LIST_0])+int32(pRefPic.uiShortRefCount[common.LIST_0]) >= common.WELS_MAX(1, pCtx.pSps.iNumRefFrames) {
			if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
				iRet = RemainOneBufferInDpbForEC(pCtx, pRefPic)
				if iRet != 0 {
					return iRet
				}
			} else {
				return ERR_INFO_INVALID_MMCO_REF_NUM_OVERFLOW
			}
		}
		iRet = AddShortTermToList(pRefPic, pDec)
	}

	return iRet
}

// MMCO (static).
func MMCO(pCtx *SWelsDecoderContext, pRefPic *SRefPic, pRefPicMarking *SRefPicMarking) int32 {
	pSps := pCtx.pCurDqLayer.sLayerInfo.pSps
	var i int32
	var iRet int32 = ERR_NONE
	for i = 0; i < MAX_MMCO_COUNT && pRefPicMarking.sMmcoRef[i].uiMmcoType != common.MMCO_END; i++ {
		uiMmcoType := pRefPicMarking.sMmcoRef[i].uiMmcoType
		iShortFrameNum := (pCtx.iFrameNum - pRefPicMarking.sMmcoRef[i].iDiffOfPicNum) & ((int32(1) << pSps.uiLog2MaxFrameNum) - 1)
		uiLongTermPicNum := pRefPicMarking.sMmcoRef[i].uiLongTermPicNum
		iLongTermFrameIdx := pRefPicMarking.sMmcoRef[i].iLongTermFrameIdx
		iMaxLongTermFrameIdx := pRefPicMarking.sMmcoRef[i].iMaxLongTermFrameIdx
		if uiMmcoType > common.MMCO_LONG {
			return ERR_INFO_INVALID_MMCO_OPCODE_BASE
		}
		iRet = MMCOProcess(pCtx, pRefPic, uiMmcoType, iShortFrameNum, uiLongTermPicNum, iLongTermFrameIdx,
			iMaxLongTermFrameIdx)
		if iRet != ERR_NONE {
			return iRet
		}
	}
	if i == MAX_MMCO_COUNT { //although Rec does not handle this condition, we here prohibit too many MMCO op
		return ERR_INFO_INVALID_MMCO_NUM
	}

	return ERR_NONE
}

// MMCOProcess (static).
func MMCOProcess(pCtx *SWelsDecoderContext, pRefPic *SRefPic, uiMmcoType uint32,
	iShortFrameNum int32, uiLongTermPicNum uint32, iLongTermFrameIdx int32, iMaxLongTermFrameIdx int32) int32 {
	var pPic *SPicture
	var iRet int32 = ERR_NONE

	switch uiMmcoType {
	case common.MMCO_SHORT2UNUSED:
		pPic = WelsDelShortFromListSetUnref(pRefPic, iShortFrameNum)
		if pPic == nil {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "MMCO_SHORT2UNUSED: delete an empty entry from short term list")
		}
	case common.MMCO_LONG2UNUSED:
		pPic = WelsDelLongFromListSetUnref(pRefPic, uiLongTermPicNum)
		if pPic == nil {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "MMCO_LONG2UNUSED: delete an empty entry from long term list")
		}
	case common.MMCO_SHORT2LONG:
		if iLongTermFrameIdx > pRefPic.iMaxLongTermFrameIdx {
			return ERR_INFO_INVALID_MMCO_LONG_TERM_IDX_EXCEED_MAX
		}
		pPic = WelsDelShortFromList(pRefPic, iShortFrameNum)
		if pPic == nil {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "MMCO_LONG2LONG: delete an empty entry from short term list")
			break
		}
		WelsDelLongFromListSetUnref(pRefPic, uint32(iLongTermFrameIdx))
		// LONG_TERM_REF
		pCtx.bCurAuContainLtrMarkSeFlag = true
		pCtx.iFrameNumOfAuMarkedLtr = iShortFrameNum
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "ex_mark_avc():::MMCO_SHORT2LONG:::LTR marking....iFrameNum: %d",
			pCtx.iFrameNumOfAuMarkedLtr)

		MarkAsLongTerm(pRefPic, iShortFrameNum, iLongTermFrameIdx, uiLongTermPicNum)
	case common.MMCO_SET_MAX_LONG:
		pRefPic.iMaxLongTermFrameIdx = iMaxLongTermFrameIdx
		for i := int32(0); i < int32(pRefPic.uiLongRefCount[common.LIST_0]); i++ {
			if pRefPic.pLongRefList[common.LIST_0][i].iLongTermFrameIdx > pRefPic.iMaxLongTermFrameIdx {
				WelsDelLongFromListSetUnref(pRefPic, uint32(pRefPic.pLongRefList[common.LIST_0][i].iLongTermFrameIdx))
			}
		}
	case common.MMCO_RESET:
		WelsResetRefPic(pCtx)
		if pRefPic != &pCtx.sRefPic {
			// WelsResetRefPic() hard-codes pCtx->sRefPic and does not
			// touch the caller's active reference list. In the threaded predecessor
			// handoff path (decoder_core.cpp), pRefPic points at pCtx->sTmpRefPic, a
			// snapshot taken from pCtx->sRefPic before this call and later published
			// to the successor thread context. Left untouched here, sTmpRefPic would
			// keep pointers to pictures that WelsResetRefPic() just unreferenced
			// (and that may already be recycled by PrefetchPic()), so the successor
			// frame would inherit a stale/dangling reference list. Re-sync it to the
			// freshly-cleared sRefPic; the entries were already unreffed once by
			// WelsResetRefPic() above, so do not call SetUnRef again here.
			*pRefPic = pCtx.sRefPic
		}
		pCtx.pLastDecPicInfo.bLastHasMmco5 = true
	case common.MMCO_LONG:
		if iLongTermFrameIdx > pRefPic.iMaxLongTermFrameIdx {
			return ERR_INFO_INVALID_MMCO_LONG_TERM_IDX_EXCEED_MAX
		}
		WelsDelLongFromListSetUnref(pRefPic, uint32(iLongTermFrameIdx))
		if int32(pRefPic.uiLongRefCount[common.LIST_0])+int32(pRefPic.uiShortRefCount[common.LIST_0]) >= common.WELS_MAX(1, pCtx.pSps.iNumRefFrames) {
			return ERR_INFO_INVALID_MMCO_REF_NUM_OVERFLOW
		}
		// LONG_TERM_REF
		pCtx.bCurAuContainLtrMarkSeFlag = true
		pCtx.iFrameNumOfAuMarkedLtr = pCtx.iFrameNum
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "ex_mark_avc():::MMCO_LONG:::LTR marking....iFrameNum: %d",
			pCtx.iFrameNum)
		iRet = AddLongTermToList(pRefPic, pCtx.pDec, iLongTermFrameIdx, uiLongTermPicNum)
	default:
	}

	return iRet
}

// SlidingWindow (static).
func SlidingWindow(pCtx *SWelsDecoderContext, pRefPic *SRefPic) int32 {
	var pPic *SPicture

	if int32(pRefPic.uiShortRefCount[common.LIST_0])+int32(pRefPic.uiLongRefCount[common.LIST_0]) >= pCtx.pSps.iNumRefFrames {
		if pRefPic.uiShortRefCount[common.LIST_0] == 0 {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "No reference picture in short term list when sliding window")
			return ERR_INFO_INVALID_MMCO_REF_NUM_NOT_ENOUGH
		}
		for i := int32(pRefPic.uiShortRefCount[common.LIST_0]) - 1; i >= 0; i-- {
			pPic = WelsDelShortFromList(pRefPic, pRefPic.pShortRefList[common.LIST_0][i].iFrameNum)
			if pPic != nil {
				SetUnRef(pPic)
				break
			} else {
				return ERR_INFO_INVALID_MMCO_REF_NUM_OVERFLOW
			}
		}
	}
	return ERR_NONE
}

// WelsDelShortFromList (static).
func WelsDelShortFromList(pRefPic *SRefPic, iFrameNum int32) *SPicture {
	var pPic *SPicture

	for i := int32(0); i < int32(pRefPic.uiShortRefCount[common.LIST_0]); i++ {
		if pRefPic.pShortRefList[common.LIST_0][i].iFrameNum == iFrameNum {
			iMoveSize := int32(pRefPic.uiShortRefCount[common.LIST_0]) - i - 1
			pPic = pRefPic.pShortRefList[common.LIST_0][i]
			pPic.bUsedAsRef = false
			pRefPic.pShortRefList[common.LIST_0][i] = nil
			if iMoveSize > 0 {
				copy(pRefPic.pShortRefList[common.LIST_0][i:i+iMoveSize], pRefPic.pShortRefList[common.LIST_0][i+1:i+1+iMoveSize])
			}
			pRefPic.uiShortRefCount[common.LIST_0]--
			pRefPic.pShortRefList[common.LIST_0][pRefPic.uiShortRefCount[common.LIST_0]] = nil
			break
		}
	}
	return pPic
}

// WelsDelShortFromListSetUnref (static).
func WelsDelShortFromListSetUnref(pRefPic *SRefPic, iFrameNum int32) *SPicture {
	pPic := WelsDelShortFromList(pRefPic, iFrameNum)
	if pPic != nil {
		SetUnRef(pPic)
	}
	return pPic
}

// WelsDelLongFromList (static).
func WelsDelLongFromList(pRefPic *SRefPic, uiLongTermFrameIdx uint32) *SPicture {
	var pPic *SPicture
	for i := int32(0); i < int32(pRefPic.uiLongRefCount[common.LIST_0]); i++ {
		pPic = pRefPic.pLongRefList[common.LIST_0][i]
		if pPic.iLongTermFrameIdx == int32(uiLongTermFrameIdx) {
			iMoveSize := int32(pRefPic.uiLongRefCount[common.LIST_0]) - i - 1
			pPic.bUsedAsRef = false
			pPic.bIsLongRef = false
			if iMoveSize > 0 {
				copy(pRefPic.pLongRefList[common.LIST_0][i:i+iMoveSize], pRefPic.pLongRefList[common.LIST_0][i+1:i+1+iMoveSize])
			}
			pRefPic.uiLongRefCount[common.LIST_0]--
			pRefPic.pLongRefList[common.LIST_0][pRefPic.uiLongRefCount[common.LIST_0]] = nil
			return pPic
		}
	}
	return nil
}

// WelsDelLongFromListSetUnref (static).
func WelsDelLongFromListSetUnref(pRefPic *SRefPic, uiLongTermFrameIdx uint32) *SPicture {
	pPic := WelsDelLongFromList(pRefPic, uiLongTermFrameIdx)
	if pPic != nil {
		SetUnRef(pPic)
	}
	return pPic
}

// AddShortTermToList (static).
func AddShortTermToList(pRefPic *SRefPic, pPic *SPicture) int32 {
	pPic.bUsedAsRef = true
	pPic.bIsLongRef = false
	pPic.iLongTermFrameIdx = -1
	if pRefPic.uiShortRefCount[common.LIST_0] > 0 {
		// Check the duplicate frame_num in short ref list
		for iPos := int32(0); iPos < int32(pRefPic.uiShortRefCount[common.LIST_0]); iPos++ {
			if pRefPic.pShortRefList[common.LIST_0][iPos] == nil {
				return ERR_INFO_INVALID_PTR
			}
			if pPic.iFrameNum == pRefPic.pShortRefList[common.LIST_0][iPos].iFrameNum {
				// Replace the previous ref pic with the new one with the same frame_num
				pRefPic.pShortRefList[common.LIST_0][iPos] = pPic
				return ERR_INFO_DUPLICATE_FRAME_NUM
			}
		}

		// memmove (&list[1], &list[0], uiShortRefCount * sizeof (PPicture)); copy() clamps
		// at the end of the MAX_DPB_COUNT array (the C code would spill past it).
		n := int(pRefPic.uiShortRefCount[common.LIST_0])
		copy(pRefPic.pShortRefList[common.LIST_0][1:], pRefPic.pShortRefList[common.LIST_0][0:n])
	}
	pRefPic.pShortRefList[common.LIST_0][0] = pPic
	pRefPic.uiShortRefCount[common.LIST_0]++
	return ERR_NONE
}

// AddLongTermToList (static).
func AddLongTermToList(pRefPic *SRefPic, pPic *SPicture, iLongTermFrameIdx int32,
	uiLongTermPicNum uint32) int32 {
	var i int32

	pPic.bUsedAsRef = true
	pPic.bIsLongRef = true
	pPic.iLongTermFrameIdx = iLongTermFrameIdx
	pPic.uiLongTermPicNum = uiLongTermPicNum
	if pRefPic.uiLongRefCount[common.LIST_0] == 0 {
		pRefPic.pLongRefList[common.LIST_0][pRefPic.uiLongRefCount[common.LIST_0]] = pPic
	} else {
		for i = 0; i < common.WELS_MIN(int32(pRefPic.uiLongRefCount[common.LIST_0]), MAX_REF_PIC_COUNT); i++ {
			if pRefPic.pLongRefList[common.LIST_0][i] == nil {
				return ERR_INFO_INVALID_PTR
			}
			if pRefPic.pLongRefList[common.LIST_0][i].iLongTermFrameIdx > pPic.iLongTermFrameIdx {
				break
			}
		}
		n := int32(pRefPic.uiLongRefCount[common.LIST_0]) - i
		if n > 0 {
			copy(pRefPic.pLongRefList[common.LIST_0][i+1:], pRefPic.pLongRefList[common.LIST_0][i:i+n])
		}
		pRefPic.pLongRefList[common.LIST_0][i] = pPic
	}

	if pRefPic.uiLongRefCount[common.LIST_0] < MAX_REF_PIC_COUNT {
		pRefPic.uiLongRefCount[common.LIST_0]++
	}
	return ERR_NONE
}

// MarkAsLongTerm (static).
func MarkAsLongTerm(pRefPic *SRefPic, iFrameNum int32, iLongTermFrameIdx int32,
	uiLongTermPicNum uint32) int32 {
	var pPic *SPicture
	var iRet int32 = ERR_NONE
	WelsDelLongFromListSetUnref(pRefPic, uint32(iLongTermFrameIdx))

	for i := int32(0); i < int32(pRefPic.uiRefCount[common.LIST_0]); i++ {
		pPic = pRefPic.pRefList[common.LIST_0][i]
		if pPic.iFrameNum == iFrameNum && !pPic.bIsLongRef {
			iRet = AddLongTermToList(pRefPic, pPic, iLongTermFrameIdx, uiLongTermPicNum)
			break
		}
	}

	return iRet
}

// GetLTRFrameIndex ports int32_t GetLTRFrameIndex (PRefPic pRefPic, int32_t iAncLTRFrameNum).
func GetLTRFrameIndex(pRefPic *SRefPic, iAncLTRFrameNum int32) int32 {
	var iLTRFrameIndex int32 = -1
	var pPic *SPicture
	for i := int32(0); i < int32(pRefPic.uiLongRefCount[0]); i++ {
		pPic = pRefPic.pLongRefList[common.LIST_0][i]
		if pPic.iFrameNum == iAncLTRFrameNum {
			return pPic.iLongTermFrameIdx
		}
	}
	return iLTRFrameIndex
}

// RemainOneBufferInDpbForEC (static).
func RemainOneBufferInDpbForEC(pCtx *SWelsDecoderContext, pRefPic *SRefPic) int32 {
	var iRet int32 = ERR_NONE
	if int32(pRefPic.uiShortRefCount[0])+int32(pRefPic.uiLongRefCount[0]) < pCtx.pSps.iNumRefFrames {
		return iRet
	}

	if pRefPic.uiShortRefCount[0] > 0 {
		iRet = SlidingWindow(pCtx, pRefPic)
	} else { //all LTR, remove the smallest long_term_frame_idx
		var iLongTermFrameIdx int32
		iMaxLongTermFrameIdx := pRefPic.iMaxLongTermFrameIdx
		// LONG_TERM_REF
		iCurrLTRFrameIdx := GetLTRFrameIndex(pRefPic, pCtx.iFrameNumOfAuMarkedLtr)
		for (int32(pRefPic.uiLongRefCount[0]) >= pCtx.pSps.iNumRefFrames) && (iLongTermFrameIdx <= iMaxLongTermFrameIdx) {
			if iLongTermFrameIdx == iCurrLTRFrameIdx {
				iLongTermFrameIdx++
				continue
			}
			WelsDelLongFromListSetUnref(pRefPic, uint32(iLongTermFrameIdx))
			iLongTermFrameIdx++
		}
	}
	if int32(pRefPic.uiShortRefCount[0])+int32(pRefPic.uiLongRefCount[0]) >=
		pCtx.pSps.iNumRefFrames { //fail to remain one empty buffer in DPB
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "RemainOneBufferInDpbForEC(): empty one DPB failed for EC!")
		iRet = ERR_INFO_REF_COUNT_OVERFLOW
	}

	return iRet
}
