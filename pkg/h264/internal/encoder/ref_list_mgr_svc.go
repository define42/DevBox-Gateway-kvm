// Port of codec/encoder/core/src/ref_list_mgr_svc.cpp.
//
// Reference list management (short/long term reference lists, LTR marking
// and recovery) and the IWelsReferenceStrategy classes.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const STR_ROOM = 1

func refB2I(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// ResetLtrState resets LTR marking, recovery, feedback state to default.
func ResetLtrState(pLtr *SLTRState) {
	pLtr.bReceivedT0LostFlag = false
	pLtr.iLastRecoverFrameNum = 0
	pLtr.iLastCorFrameNumDec = -1
	pLtr.iCurFrameNumInDec = -1

	// LTR mark
	pLtr.iLTRMarkMode = int32(LTR_DIRECT_MARK)
	pLtr.iLTRMarkSuccessNum = 0  //successful marked num
	pLtr.bLTRMarkingFlag = false //decide whether current frame marked as LTR
	pLtr.bLTRMarkEnable = false  //when LTR is confirmed and the interval is no smaller than the marking period
	pLtr.iCurLtrIdx = 0
	pLtr.iLastLtrIdx = [api.MAX_TEMPORAL_LAYER_NUM]int32{}
	pLtr.uiLtrMarkInterval = 0

	// LTR mark feedback
	pLtr.uiLtrMarkState = uint32(api.NO_LTR_MARKING_FEEDBACK)
	pLtr.iLtrMarkFbFrameNum = -1
}

// WelsResetRefList resets reference picture list.
func WelsResetRefList(pCtx *sWelsEncCtx) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	var i int32

	for i = 0; i < MAX_SHORT_REF_COUNT+1; i++ {
		pRefList.pShortRefList[i] = nil
	}
	for i = 0; i < pCtx.pSvcParam.ILTRRefNum+1; i++ {
		pRefList.pLongRefList[i] = nil
	}
	for i = 0; i < pCtx.pSvcParam.INumRefFrame+1; i++ {
		pRefList.pRef[i].SetUnref()
	}

	pRefList.uiLongRefCount = 0
	pRefList.uiShortRefCount = 0
	pRefList.pNextBuffer = pRefList.pRef[0]
}

func DeleteLTRFromLongList(pCtx *sWelsEncCtx, iIdx int32) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	var k int32

	for k = iIdx; k < int32(pRefList.uiLongRefCount)-1; k++ {
		pRefList.pLongRefList[k] = pRefList.pLongRefList[k+1]
	}
	pRefList.pLongRefList[k] = nil
	pRefList.uiLongRefCount--
}

func DeleteSTRFromShortList(pCtx *sWelsEncCtx, iIdx int32) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	var k int32

	for k = iIdx; k < int32(pRefList.uiShortRefCount)-1; k++ {
		pRefList.pShortRefList[k] = pRefList.pShortRefList[k+1]
	}
	pRefList.pShortRefList[k] = nil
	pRefList.uiShortRefCount--
}

func DeleteNonSceneLTR(pCtx *sWelsEncCtx) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	for i := int32(0); i < pCtx.pSvcParam.INumRefFrame; i++ {
		pRef := pRefList.pLongRefList[i]
		if pRef != nil && pRef.bUsedAsRef && pRef.bIsLongRef && (!pRef.bIsSceneLTR) &&
			(pCtx.uiTemporalId < pRef.uiTemporalId || pCtx.bCurFrameMarkedAsSceneLtr) {
			//this is our strategy to Unref all non-sceneLTR when the the current frame is sceneLTR
			pRef.SetUnref()
			DeleteLTRFromLongList(pCtx, i)
			i--
		}
	}
}

func welsAbsDiffInt64(a, b int64) int64 {
	if a > b {
		return a - b
	}
	return b - a
}

func CompareFrameNum(iFrameNumA int32, iFrameNumB int32, iMaxFrameNumPlus1 int32) int32 {
	var iNumA, iNumB, iDiffAB, iDiffMin int64
	if iFrameNumA > iMaxFrameNumPlus1 || iFrameNumB > iMaxFrameNumPlus1 {
		return -2
	}

	iDiffAB = welsAbsDiffInt64(int64(iFrameNumA), int64(iFrameNumB))

	iDiffMin = iDiffAB
	if iDiffMin == 0 {
		return int32(FRAME_NUM_EQUAL)
	}

	iNumA = welsAbsDiffInt64(int64(iFrameNumA+iMaxFrameNumPlus1), int64(iFrameNumB))
	if iNumA == 0 {
		return int32(FRAME_NUM_EQUAL)
	} else if iDiffMin > iNumA {
		return int32(FRAME_NUM_BIGGER)
	}

	iNumB = welsAbsDiffInt64(int64(iFrameNumB+iMaxFrameNumPlus1), int64(iFrameNumA))
	if iNumB == 0 {
		return int32(FRAME_NUM_EQUAL)
	} else if iDiffMin > iNumB {
		return int32(FRAME_NUM_SMALLER)
	}

	if iFrameNumA > iFrameNumB {
		return int32(FRAME_NUM_BIGGER)
	}
	return int32(FRAME_NUM_SMALLER)
}

// DeleteInvalidLTR deletes failed mark according LTR recovery pRequest.
func DeleteInvalidLTR(pCtx *sWelsEncCtx) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLongRefList := &pRefList.pLongRefList
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	iMaxFrameNumPlus1 := int32(1) << pCtx.pSps.uiLog2MaxFrameNum
	var i int32
	pParamInternal := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	pLogCtx := &pCtx.sLogCtx

	for i = 0; i < LONG_TERM_REF_NUM; i++ {
		if pLongRefList[i] != nil {
			if CompareFrameNum(pLongRefList[i].iFrameNum, pLtr.iLastCorFrameNumDec, iMaxFrameNumPlus1) == int32(FRAME_NUM_BIGGER) &&
				(CompareFrameNum(pLongRefList[i].iFrameNum, pLtr.iCurFrameNumInDec,
					iMaxFrameNumPlus1)&int32(FRAME_NUM_EQUAL|FRAME_NUM_SMALLER)) != 0 {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "LTR ,invalid LTR delete ,long_term_idx = %d , iFrameNum =%d ",
					pLongRefList[i].iLongTermPicNum, pLongRefList[i].iFrameNum)
				pLongRefList[i].SetUnref()
				DeleteLTRFromLongList(pCtx, i)
				pLtr.bLTRMarkEnable = true
				if pRefList.uiLongRefCount == 0 {
					pParamInternal.bEncCurFrmAsIdrFlag = true
				}
			} else if CompareFrameNum(pLongRefList[i].iMarkFrameNum, pLtr.iLastCorFrameNumDec,
				iMaxFrameNumPlus1) == int32(FRAME_NUM_BIGGER) &&
				(CompareFrameNum(pLongRefList[i].iMarkFrameNum, pLtr.iCurFrameNumInDec,
					iMaxFrameNumPlus1)&int32(FRAME_NUM_EQUAL|FRAME_NUM_SMALLER)) != 0 &&
				pLtr.iLTRMarkMode == int32(LTR_DELAY_MARK) {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "LTR ,iMarkFrameNum invalid LTR delete ,long_term_idx = %d , iFrameNum =%d ",
					pLongRefList[i].iLongTermPicNum, pLongRefList[i].iFrameNum)
				pLongRefList[i].SetUnref()
				DeleteLTRFromLongList(pCtx, i)
				pLtr.bLTRMarkEnable = true
				if pRefList.uiLongRefCount == 0 {
					pParamInternal.bEncCurFrmAsIdrFlag = true
				}
			}
		}
	}
}

// HandleLTRMarkFeedback handles LTR Mark feedback message.
func HandleLTRMarkFeedback(pCtx *sWelsEncCtx) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLongRefList := &pRefList.pLongRefList
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	pParamInternal := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	var i, j int32

	if pLtr.uiLtrMarkState == uint32(api.LTR_MARKING_SUCCESS) {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
			"pLtr->uiLtrMarkState = %d, pLtr.iCurLtrIdx = %d , pLtr->iLtrMarkFbFrameNum = %d ,pCtx->iFrameNum = %d ",
			pLtr.uiLtrMarkState, pLtr.iCurLtrIdx, pLtr.iLtrMarkFbFrameNum, pParamInternal.iFrameNum)
		for i = 0; i < int32(pRefList.uiLongRefCount); i++ {
			if pLongRefList[i].iFrameNum == pLtr.iLtrMarkFbFrameNum && pLongRefList[i].uiRecieveConfirmed != uint8(RECIEVE_SUCCESS) {

				pLongRefList[i].uiRecieveConfirmed = uint8(RECIEVE_SUCCESS)
				pCtx.pVaa.uiValidLongTermPicIdx = uint8(pLongRefList[i].iLongTermPicNum)

				pLtr.iLastCorFrameNumDec = pLtr.iLtrMarkFbFrameNum
				pLtr.iLastRecoverFrameNum = pLtr.iLastCorFrameNumDec
				pLtr.iCurFrameNumInDec = pLtr.iLastRecoverFrameNum

				for j = 0; j < int32(pRefList.uiLongRefCount); j++ {
					if pLongRefList[j].iLongTermPicNum != pLtr.iCurLtrIdx {
						pLongRefList[j].SetUnref()
						DeleteLTRFromLongList(pCtx, j)
					}
				}

				pLtr.iLTRMarkSuccessNum++
				pLtr.iCurLtrIdx = (pLtr.iCurLtrIdx + 1) % LONG_TERM_REF_NUM
				if pLtr.iLTRMarkSuccessNum >= (LONG_TERM_REF_NUM) {
					pLtr.iLTRMarkMode = int32(LTR_DELAY_MARK)
				} else {
					pLtr.iLTRMarkMode = int32(LTR_DIRECT_MARK)
				}
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING, "LTR mark mode =%d", pLtr.iLTRMarkMode)
				pLtr.bLTRMarkEnable = true
				break
			}
		}
		pLtr.uiLtrMarkState = uint32(api.NO_LTR_MARKING_FEEDBACK)
	} else if pLtr.uiLtrMarkState == uint32(api.LTR_MARKING_FAILED) {
		for i = 0; i < int32(pRefList.uiLongRefCount); i++ {
			if pLongRefList[i].iFrameNum == pLtr.iLtrMarkFbFrameNum {
				pLongRefList[i].SetUnref()
				DeleteLTRFromLongList(pCtx, i)
				break
			}
		}
		pLtr.uiLtrMarkState = uint32(api.NO_LTR_MARKING_FEEDBACK)
		pLtr.bLTRMarkEnable = true

		if pLtr.iLTRMarkSuccessNum == 0 {
			pParamInternal.bEncCurFrmAsIdrFlag = true // no LTR , means IDR recieve failed, force next frame IDR
		}
	}
}

func refGoPFrameNumInterval(pCtx *sWelsEncCtx) int32 {
	if (pCtx.pSvcParam.uiGopSize >> 1) > 1 {
		return int32(pCtx.pSvcParam.uiGopSize >> 1)
	}
	return 1
}

// LTRMarkProcess is the LTR mark process.
func LTRMarkProcess(pCtx *sWelsEncCtx) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLongRefList := &pRefList.pLongRefList
	pShortRefList := &pRefList.pShortRefList
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	iGoPFrameNumInterval := refGoPFrameNumInterval(pCtx)
	iMaxFrameNumPlus1 := int32(1) << pCtx.pSps.uiLog2MaxFrameNum
	var i int32
	var j int32
	bMoveLtrFromShortToLong := false
	pParamInternal := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]

	if pCtx.eSliceType == common.I_SLICE {
		i = 0
		pShortRefList[i].uiRecieveConfirmed = uint8(RECIEVE_SUCCESS)
	} else if pLtr.bLTRMarkingFlag {
		pCtx.pVaa.uiMarkLongTermPicIdx = uint8(pLtr.iCurLtrIdx)

		if pLtr.iLTRMarkMode == int32(LTR_DELAY_MARK) {
			for i = 0; i < int32(pRefList.uiShortRefCount); i++ {
				if CompareFrameNum(pParamInternal.iFrameNum, pShortRefList[i].iFrameNum+iGoPFrameNumInterval,
					iMaxFrameNumPlus1) == int32(FRAME_NUM_EQUAL) {
					break
				}
			}
		}
	}

	if pCtx.eSliceType == common.I_SLICE || pLtr.bLTRMarkingFlag {
		pShortRefList[i].bIsLongRef = true
		pShortRefList[i].iLongTermPicNum = pLtr.iCurLtrIdx
		pShortRefList[i].iMarkFrameNum = pParamInternal.iFrameNum
	}

	// delay one gop to move LTR from int16_t list to int32_t list
	if pLtr.iLTRMarkMode == int32(LTR_DIRECT_MARK) && pCtx.eSliceType != common.I_SLICE && !pLtr.bLTRMarkingFlag {
		for j = 0; j < int32(pRefList.uiShortRefCount); j++ {
			if pRefList.pShortRefList[j].bIsLongRef {
				i = j
				bMoveLtrFromShortToLong = true
				break
			}
		}
	}

	if (pLtr.iLTRMarkMode == int32(LTR_DELAY_MARK) && pLtr.bLTRMarkingFlag) ||
		((pLtr.iLTRMarkMode == int32(LTR_DIRECT_MARK)) && (bMoveLtrFromShortToLong)) {
		pCtx.bRefOfCurTidIsLtr[pCtx.uiDependencyId][pCtx.uiTemporalId] = true

		if pRefList.uiLongRefCount > 0 {
			kiCount := int(pRefList.uiLongRefCount)
			copy(pRefList.pLongRefList[1:1+kiCount], pRefList.pLongRefList[0:kiCount]) // confirmed_safe_unsafe_usage
		}
		pLongRefList[0] = pShortRefList[i]
		pRefList.uiLongRefCount++
		if int32(pRefList.uiLongRefCount) > pCtx.pSvcParam.ILTRRefNum {
			pRefList.pLongRefList[pRefList.uiLongRefCount-1].SetUnref()
			DeleteLTRFromLongList(pCtx, int32(pRefList.uiLongRefCount)-1)
		}
		DeleteSTRFromShortList(pCtx, i)
	}
}

func LTRMarkProcessScreen(pCtx *sWelsEncCtx) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLongRefList := &pRefList.pLongRefList
	iLtrIdx := pCtx.pDecPic.iLongTermPicNum
	pCtx.pVaa.uiMarkLongTermPicIdx = uint8(pCtx.pDecPic.iLongTermPicNum)

	if pLongRefList[iLtrIdx] != nil {
		pLongRefList[iLtrIdx].SetUnref()
	} else {
		pRefList.uiLongRefCount++
	}
	pLongRefList[iLtrIdx] = pCtx.pDecPic
}

func PrefetchNextBuffer(pCtx *sWelsEncCtx) {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	kiNumRef := pCtx.pSvcParam.INumRefFrame
	var i int32

	pRefList.pNextBuffer = nil
	for i = 0; i < kiNumRef+1; i++ {
		if !pRefList.pRef[i].bUsedAsRef {
			pRefList.pNextBuffer = pRefList.pRef[i]
			break
		}
	}

	if pRefList.pNextBuffer == nil && pRefList.uiShortRefCount > 0 {
		pRefList.pNextBuffer = pRefList.pShortRefList[pRefList.uiShortRefCount-1]
		pRefList.pNextBuffer.SetUnref()
	}

	pCtx.pDecPic = pRefList.pNextBuffer
}

func refExpandDecPic(pCtx *sWelsEncCtx) {
	pDecPic := pCtx.pDecPic
	common.ExpandReferencingPicture(pDecPic.pData[:], pDecPic.iDataOff[:], pDecPic.iWidthInPixel, pDecPic.iHeightInPixel,
		pDecPic.iLineSize[:],
		pCtx.pFuncList.sExpandPicFunc.PfExpandLumaPicture, pCtx.pFuncList.sExpandPicFunc.PfExpandChromaPicture)
}

// WelsUpdateRefList updates reference picture list.
func WelsUpdateRefList(pCtx *sWelsEncCtx) bool {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	pParamD := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]

	var iRefIdx int32
	kuiTid := pCtx.uiTemporalId
	kuiDid := pCtx.uiDependencyId
	keSliceType := pCtx.eSliceType
	var i uint32
	// Need update pRef list in case store base layer or target dependency layer construction
	if nil == pCtx.pCurDqLayer {
		return false
	}

	if nil == pRefList || nil == pRefList.pRef[0] {
		return false
	}

	if nil != pCtx.pDecPic {
		if (pParamD.iHighestTemporalId == 0) || (int32(kuiTid) < int32(pParamD.iHighestTemporalId)) {
			// Expanding picture for future reference
			refExpandDecPic(pCtx)
		}

		// move picture in list
		pCtx.pDecPic.uiTemporalId = kuiTid
		pCtx.pDecPic.uiSpatialId = kuiDid
		pCtx.pDecPic.iFrameNum = pParamD.iFrameNum
		pCtx.pDecPic.iFramePoc = pParamD.iPOC
		pCtx.pDecPic.uiRecieveConfirmed = uint8(RECIEVE_UNKOWN)
		pCtx.pDecPic.bUsedAsRef = true

		for iRefIdx = int32(pRefList.uiShortRefCount) - 1; iRefIdx >= 0; iRefIdx-- {
			pRefList.pShortRefList[iRefIdx+1] = pRefList.pShortRefList[iRefIdx]
		}
		pRefList.pShortRefList[0] = pCtx.pDecPic
		pRefList.uiShortRefCount++
	}

	if keSliceType == common.P_SLICE {
		if pCtx.uiTemporalId == 0 {
			if pCtx.pSvcParam.BEnableLongTermReference {
				LTRMarkProcess(pCtx)
				DeleteInvalidLTR(pCtx)
				HandleLTRMarkFeedback(pCtx)

				pLtr.bReceivedT0LostFlag = false // reset to false due to the recovery is finished
				pLtr.bLTRMarkingFlag = false
				pLtr.uiLtrMarkInterval++
			}

			for i = uint32(int32(pRefList.uiShortRefCount) - 1); i > 0; i-- {
				pRefList.pShortRefList[i].SetUnref()
				DeleteSTRFromShortList(pCtx, int32(i))
			}
			if pRefList.uiShortRefCount > 0 && (pRefList.pShortRefList[0].uiTemporalId > 0 ||
				pRefList.pShortRefList[0].iFrameNum != pParamD.iFrameNum) {
				pRefList.pShortRefList[0].SetUnref()
				DeleteSTRFromShortList(pCtx, 0)
			}
		}
	} else { // in case IDR currently coding
		if pCtx.pSvcParam.BEnableLongTermReference {
			LTRMarkProcess(pCtx)

			pLtr.iCurLtrIdx = (pLtr.iCurLtrIdx + 1) % LONG_TERM_REF_NUM
			pLtr.iLTRMarkSuccessNum = 1 //IDR default suceess
			pLtr.bLTRMarkEnable = true
			pLtr.uiLtrMarkInterval = 0

			pCtx.pVaa.uiValidLongTermPicIdx = 0
			pCtx.pVaa.uiMarkLongTermPicIdx = 0
		}
	}
	pCtx.pReferenceStrategy.EndofUpdateRefList()
	return true
}

func CheckCurMarkFrameNumUsed(pCtx *sWelsEncCtx) bool {
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLongRefList := &pRefList.pLongRefList
	iGoPFrameNumInterval := refGoPFrameNumInterval(pCtx)
	iMaxFrameNumPlus1 := int32(1) << pCtx.pSps.uiLog2MaxFrameNum
	pParamInternal := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	var i int32

	for i = 0; i < int32(pRefList.uiLongRefCount); i++ {
		if (pParamInternal.iFrameNum == pLongRefList[i].iFrameNum && pLtr.iLTRMarkMode == int32(LTR_DIRECT_MARK)) ||
			(CompareFrameNum(pParamInternal.iFrameNum+iGoPFrameNumInterval, pLongRefList[i].iFrameNum,
				iMaxFrameNumPlus1) == int32(FRAME_NUM_EQUAL) && pLtr.iLTRMarkMode == int32(LTR_DELAY_MARK)) {
			return false
		}
	}

	return true
}

func WelsMarkMMCORefInfoWithBase(ppSliceList []*SSlice, pBaseSlice *SSlice, kiCountSliceNum int32) {
	var iSliceIdx int32
	pBaseSHExt := &pBaseSlice.sSliceHeaderExt

	for iSliceIdx = 0; iSliceIdx < kiCountSliceNum; iSliceIdx++ {
		pSliceHdrExt := &ppSliceList[iSliceIdx].sSliceHeaderExt
		pSliceHdrExt.sSliceHeader.sRefMarking = pBaseSHExt.sSliceHeader.sRefMarking
	}
}

// WelsMarkMMCORefInfo: SSlice** ppSliceList -> []*SSlice (SDqLayer.ppSliceInLayer).
func WelsMarkMMCORefInfo(pCtx *sWelsEncCtx, pLtr *SLTRState, ppSliceList []*SSlice, kiCountSliceNum int32) {
	pBaseSlice := ppSliceList[0]
	pRefPicMark := &pBaseSlice.sSliceHeaderExt.sSliceHeader.sRefMarking
	iGoPFrameNumInterval := refGoPFrameNumInterval(pCtx)

	*pRefPicMark = SRefPicMarking{}

	if pCtx.pSvcParam.BEnableLongTermReference && pLtr.bLTRMarkingFlag {
		if pLtr.iLTRMarkMode == int32(LTR_DIRECT_MARK) {
			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMaxLongTermFrameIdx = LONG_TERM_REF_NUM - 1
			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMmcoType = common.MMCO_SET_MAX_LONG
			pRefPicMark.uiMmcoCount++

			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iDiffOfPicNum = iGoPFrameNumInterval
			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMmcoType = common.MMCO_SHORT2UNUSED
			pRefPicMark.uiMmcoCount++

			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iLongTermFrameIdx = pLtr.iCurLtrIdx
			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMmcoType = common.MMCO_LONG
			pRefPicMark.uiMmcoCount++
		} else if pLtr.iLTRMarkMode == int32(LTR_DELAY_MARK) {
			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iDiffOfPicNum = iGoPFrameNumInterval
			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iLongTermFrameIdx = pLtr.iCurLtrIdx
			pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMmcoType = common.MMCO_SHORT2LONG
			pRefPicMark.uiMmcoCount++
		}
	}

	WelsMarkMMCORefInfoWithBase(ppSliceList, pBaseSlice, kiCountSliceNum)
}

func WelsMarkPic(pCtx *sWelsEncCtx) {
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	kiCountSliceNum := pCtx.pCurDqLayer.iMaxSliceNum

	if pCtx.pSvcParam.BEnableLongTermReference && pLtr.bLTRMarkEnable && pCtx.uiTemporalId == 0 {
		if !pLtr.bReceivedT0LostFlag && pLtr.uiLtrMarkInterval > pCtx.pSvcParam.ILtrMarkPeriod &&
			CheckCurMarkFrameNumUsed(pCtx) {
			pLtr.bLTRMarkingFlag = true
			pLtr.bLTRMarkEnable = false
			pLtr.uiLtrMarkInterval = 0
			for i := int32(0); i < api.MAX_TEMPORAL_LAYER_NUM; i++ {
				if int32(pCtx.uiTemporalId) < i || pCtx.uiTemporalId == 0 {
					pLtr.iLastLtrIdx[i] = pLtr.iCurLtrIdx
				}
			}
		} else {
			pLtr.bLTRMarkingFlag = false
		}
	}

	WelsMarkMMCORefInfo(pCtx, pLtr, pCtx.pCurDqLayer.ppSliceInLayer, kiCountSliceNum)
}

func FilterLTRRecoveryRequest(pCtx *sWelsEncCtx, pLTRRecoverRequest *api.SLTRRecoverRequest) int32 {
	//if disable LTR, force IDR
	if !pCtx.pSvcParam.BEnableLongTermReference {
		for iDid := int32(0); iDid < pCtx.pSvcParam.ISpatialLayerNum; iDid++ {
			pParamInternal := &pCtx.pSvcParam.sDependencyLayers[iDid]
			pParamInternal.bEncCurFrmAsIdrFlag = true
		}
	} else {
		pRequest := pLTRRecoverRequest
		iLayerId := pLTRRecoverRequest.ILayerId
		if (iLayerId < 0) || (iLayerId >= pCtx.pSvcParam.ISpatialLayerNum) {
			return 0 // false
		}

		pLtr := &pCtx.pLtr[iLayerId]
		iMaxFrameNumPlus1 := int32(1) << pCtx.pSps.uiLog2MaxFrameNum
		pParamInternal := &pCtx.pSvcParam.sDependencyLayers[iLayerId]
		if pRequest.UiFeedbackType == uint32(api.LTR_RECOVERY_REQUEST) && pRequest.UiIDRPicId == uint32(pParamInternal.uiIdrPicId) {
			if pRequest.ILastCorrectFrameNum == -1 {
				pParamInternal.bEncCurFrmAsIdrFlag = true
				return 1 // true
			} else if pRequest.ICurrentFrameNum == -1 {
				pLtr.bReceivedT0LostFlag = true
				return 1 // true
			} else if (CompareFrameNum(pLtr.iLastRecoverFrameNum, pRequest.ILastCorrectFrameNum,
				iMaxFrameNumPlus1)&int32(FRAME_NUM_EQUAL|FRAME_NUM_SMALLER)) != 0 || // t0 lost
				((CompareFrameNum(pLtr.iLastRecoverFrameNum, pRequest.ICurrentFrameNum,
					iMaxFrameNumPlus1)&int32(FRAME_NUM_EQUAL|FRAME_NUM_SMALLER)) != 0 &&
					CompareFrameNum(pLtr.iLastRecoverFrameNum, pRequest.ILastCorrectFrameNum,
						iMaxFrameNumPlus1) == int32(FRAME_NUM_BIGGER)) { // recovery failed

				pLtr.bReceivedT0LostFlag = true
				pLtr.iLastCorFrameNumDec = pRequest.ILastCorrectFrameNum
				pLtr.iCurFrameNumInDec = pRequest.ICurrentFrameNum
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO,
					"Receive valid LTR recovery pRequest,feedback_type = %d ,uiIdrPicId = %d , current_frame_num = %d , last correct frame num = %d",
					pRequest.UiFeedbackType, pRequest.UiIDRPicId, pRequest.ICurrentFrameNum, pRequest.ILastCorrectFrameNum)
			}

			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO,
				"Receive LTR recovery pRequest,feedback_type = %d ,uiIdrPicId = %d , current_frame_num = %d , last correct frame num = %d",
				pRequest.UiFeedbackType, pRequest.UiIDRPicId, pRequest.ICurrentFrameNum, pRequest.ILastCorrectFrameNum)
		}
	}

	return 1 // true
}

func FilterLTRMarkingFeedback(pCtx *sWelsEncCtx, pLTRMarkingFeedback *api.SLTRMarkingFeedback) {
	iLayerId := pLTRMarkingFeedback.ILayerId
	if (iLayerId < 0) || (iLayerId >= pCtx.pSvcParam.ISpatialLayerNum) {
		return
	}
	pLtr := &pCtx.pLtr[iLayerId]
	if pCtx.pSvcParam.BEnableLongTermReference {
		pParamInternal := &pCtx.pSvcParam.sDependencyLayers[iLayerId]
		if pLTRMarkingFeedback.UiIDRPicId == uint32(pParamInternal.uiIdrPicId) &&
			(pLTRMarkingFeedback.UiFeedbackType == uint32(api.LTR_MARKING_SUCCESS) ||
				pLTRMarkingFeedback.UiFeedbackType == uint32(api.LTR_MARKING_FAILED)) { // avoid error pData
			pLtr.uiLtrMarkState = pLTRMarkingFeedback.UiFeedbackType
			pLtr.iLtrMarkFbFrameNum = pLTRMarkingFeedback.ILTRFrameNum
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO,
				"Receive valid LTR marking feedback, feedback_type = %d , uiIdrPicId = %d , LTR_frame_num = %d , cur_idr_pic_id = %d",
				pLTRMarkingFeedback.UiFeedbackType, pLTRMarkingFeedback.UiIDRPicId, pLTRMarkingFeedback.ILTRFrameNum,
				pParamInternal.uiIdrPicId)

		} else {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO,
				"Receive LTR marking feedback, feedback_type = %d , uiIdrPicId = %d , LTR_frame_num = %d , cur_idr_pic_id = %d",
				pLTRMarkingFeedback.UiFeedbackType, pLTRMarkingFeedback.UiIDRPicId, pLTRMarkingFeedback.ILTRFrameNum,
				pParamInternal.uiIdrPicId)
		}
	}
}

// WelsBuildRefList builds reference picture list.
func WelsBuildRefList(pCtx *sWelsEncCtx, iPOC int32, iBestLtrRefIdx int32) bool {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	kiNumRef := pCtx.pSvcParam.INumRefFrame
	kuiTid := pCtx.uiTemporalId
	var i uint32
	pParamD := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	// to support any type of cur_dq->mgs_control
	//    [ 0:    using current layer to do ME/MC;
	//     -1:    using store base layer to do ME/MC;
	//      2:    using highest layer to do ME/MC; ]

	// build reference list 0/1 if applicable

	pCtx.iNumRef0 = 0
	if pCtx.eSliceType != common.I_SLICE {
		if pCtx.pSvcParam.BEnableLongTermReference && pLtr.bReceivedT0LostFlag && pCtx.uiTemporalId == 0 {
			for i = 0; i < uint32(pRefList.uiLongRefCount); i++ {
				if pRefList.pLongRefList[i].uiRecieveConfirmed == uint8(RECIEVE_SUCCESS) {
					pCtx.pCurDqLayer.pRefOri[pCtx.iNumRef0] = pRefList.pLongRefList[i]
					pCtx.pRefList0[pCtx.iNumRef0] = pRefList.pLongRefList[i]
					pCtx.iNumRef0++
					pLtr.iLastRecoverFrameNum = pParamD.iFrameNum
					common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO,
						"pRef is int32_t !iLastRecoverFrameNum = %d, pRef iFrameNum = %d,LTR number = %d,",
						pLtr.iLastRecoverFrameNum, pCtx.pRefList0[0].iFrameNum, pRefList.uiLongRefCount)
					break
				}
			}
		} else {
			for i = 0; i < uint32(pRefList.uiShortRefCount); i++ {
				pRef := pRefList.pShortRefList[i]
				if pRef != nil && pRef.bUsedAsRef && pRef.iFramePoc >= 0 && pRef.uiTemporalId <= kuiTid {
					pCtx.pCurDqLayer.pRefOri[pCtx.iNumRef0] = pRef
					pCtx.pRefList0[pCtx.iNumRef0] = pRef
					pCtx.iNumRef0++
				}
			}
		}
	} else { // safe for IDR
		WelsResetRefList(pCtx)                         //for IDR, SHOULD reset pRef list.
		ResetLtrState(&pCtx.pLtr[pCtx.uiDependencyId]) //SHOULD update it when IDR.
		for k := 0; k < MAX_TEMPORAL_LEVEL; k++ {
			pCtx.bRefOfCurTidIsLtr[pCtx.uiDependencyId][k] = false
		}
		pCtx.pRefList0[0] = nil
	}

	if int32(pCtx.iNumRef0) > kiNumRef {
		pCtx.iNumRef0 = uint8(kiNumRef)
	}
	return pCtx.iNumRef0 > 0 || pCtx.eSliceType == common.I_SLICE
}

func UpdateBlockStatic(pCtx *sWelsEncCtx) {
	pVaaExt := pCtx.pVaa.pExt
	for idx := 0; idx < int(pCtx.iNumRef0); idx++ {
		//TODO: we need to re-factor the source picture storage first,
		//and then use original frame of the ref to do this calculation for better vaa algo implementation
		pRef := pCtx.pRefList0[idx]
		if pVaaExt.iVaaBestRefFrameNum != pRef.iFrameNum {
			//re-do the calculation
			pCtx.pVpp.UpdateBlockIdcForScreen(pVaaExt.pVaaBestBlockStaticIdc, pRef, pCtx.pEncPic)
		}
	}
}

func WelsUpdateSliceHeaderSyntax(pCtx *sWelsEncCtx, iAbsDiffPicNumMinus1 int32, ppSliceList []*SSlice, uiFrameType int32) {
	kiCountSliceNum := pCtx.pCurDqLayer.iMaxSliceNum
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	var iIdx int32

	for iIdx = 0; iIdx < kiCountSliceNum; iIdx++ {
		pSliceHdrExt := &ppSliceList[iIdx].sSliceHeaderExt
		pSliceHdr := &pSliceHdrExt.sSliceHeader
		pRefReorder := &pSliceHdr.sRefReordering
		pRefPicMark := &pSliceHdr.sRefMarking

		/*syntax for num_ref_idx_l0_active_minus1*/
		pSliceHdr.uiRefCount = pCtx.iNumRef0
		if pCtx.iNumRef0 > 0 {
			if (!pCtx.pRefList0[0].bIsLongRef) || (!pCtx.pSvcParam.BEnableLongTermReference) {
				pRefReorder.SReorderingSyntax[0].uiReorderingOfPicNumsIdc = 0
				pRefReorder.SReorderingSyntax[0].uiAbsDiffPicNumMinus1 = uint32(iAbsDiffPicNumMinus1)
				pRefReorder.SReorderingSyntax[1].uiReorderingOfPicNumsIdc = 3
			} else {
				var iRefIdx int32
				for iRefIdx = 0; iRefIdx < int32(pCtx.iNumRef0); iRefIdx++ {
					pRefReorder.SReorderingSyntax[iRefIdx].uiReorderingOfPicNumsIdc = 2
					pRefReorder.SReorderingSyntax[iRefIdx].iLongTermPicNum = uint16(pCtx.pRefList0[iRefIdx].iLongTermPicNum)
				}
				pRefReorder.SReorderingSyntax[iRefIdx].uiReorderingOfPicNumsIdc = 3
			}
		}

		/*syntax for dec_ref_pic_marking()*/
		if int32(api.VideoFrameTypeIDR) == uiFrameType {
			pRefPicMark.bNoOutputOfPriorPicsFlag = false
			pRefPicMark.bLongTermRefFlag = pCtx.pSvcParam.BEnableLongTermReference
		} else {
			if pCtx.pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
				pRefPicMark.bAdaptiveRefPicMarkingModeFlag = pCtx.pSvcParam.BEnableLongTermReference
			} else {
				pRefPicMark.bAdaptiveRefPicMarkingModeFlag = pCtx.pSvcParam.BEnableLongTermReference && pLtr.bLTRMarkingFlag
			}
		}
	}
}

// WelsUpdateRefSyntax updates syntax for reference base related.
func WelsUpdateRefSyntax(pCtx *sWelsEncCtx, iPOC int32, uiFrameType int32) {
	iAbsDiffPicNumMinus1 := int32(-1)
	pParamD := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	/*syntax for ref_pic_list_reordering()*/
	if pCtx.iNumRef0 > 0 {
		iAbsDiffPicNumMinus1 = pParamD.iFrameNum - (pCtx.pRefList0[0].iFrameNum) - 1

		if iAbsDiffPicNumMinus1 < 0 {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "WelsUpdateRefSyntax():::uiAbsDiffPicNumMinus1:%d", iAbsDiffPicNumMinus1)
			iAbsDiffPicNumMinus1 += (int32(1) << (pCtx.pSps.uiLog2MaxFrameNum))
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "WelsUpdateRefSyntax():::uiAbsDiffPicNumMinus1< 0, update as:%d",
				iAbsDiffPicNumMinus1)
		}
	}

	WelsUpdateSliceHeaderSyntax(pCtx, iAbsDiffPicNumMinus1, pCtx.pCurDqLayer.ppSliceInLayer, uiFrameType)
}

func UpdateOriginalPicInfo(pOrigPic *SPicture, pReconPic *SPicture) {
	if pOrigPic == nil {
		return
	}

	pOrigPic.iPictureType = pReconPic.iPictureType
	pOrigPic.iFramePoc = pReconPic.iFramePoc
	pOrigPic.iFrameNum = pReconPic.iFrameNum
	pOrigPic.uiSpatialId = pReconPic.uiSpatialId
	pOrigPic.uiTemporalId = pReconPic.uiTemporalId
	pOrigPic.iLongTermPicNum = pReconPic.iLongTermPicNum
	pOrigPic.bUsedAsRef = pReconPic.bUsedAsRef
	pOrigPic.bIsLongRef = pReconPic.bIsLongRef
	pOrigPic.bIsSceneLTR = pReconPic.bIsSceneLTR
	pOrigPic.iFrameAverageQp = pReconPic.iFrameAverageQp
}

func UpdateSrcPicListLosslessScreenRefSelectionWithLtr(pCtx *sWelsEncCtx) {
	iDIdx := int32(pCtx.uiDependencyId)
	//update info in src list
	UpdateOriginalPicInfo(pCtx.pEncPic, pCtx.pDecPic)
	PrefetchNextBuffer(pCtx)
	pCtx.pVpp.UpdateSrcListLosslessScreenRefSelectionWithLtr(pCtx.pEncPic, iDIdx, int32(pCtx.pVaa.uiMarkLongTermPicIdx),
		pCtx.ppRefPicListExt[iDIdx].pLongRefList[:])
}

func UpdateSrcPicList(pCtx *sWelsEncCtx) {
	iDIdx := int32(pCtx.uiDependencyId)
	//update info in src list
	UpdateOriginalPicInfo(pCtx.pEncPic, pCtx.pDecPic)
	PrefetchNextBuffer(pCtx)
	pCtx.pVpp.UpdateSrcList(pCtx.pEncPic, iDIdx, pCtx.ppRefPicListExt[iDIdx].pShortRefList[:],
		uint32(pCtx.ppRefPicListExt[iDIdx].uiShortRefCount))
}

func WelsUpdateRefListScreen(pCtx *sWelsEncCtx) bool {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	pParamD := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	kuiTid := pCtx.uiTemporalId
	// Need update ref list in case store base layer or target dependency layer construction
	if nil == pCtx.pCurDqLayer {
		return false
	}

	if nil == pRefList || nil == pRefList.pRef[0] {
		return false
	}

	if nil != pCtx.pDecPic {
		if (pParamD.iHighestTemporalId == 0) || (int32(kuiTid) < int32(pParamD.iHighestTemporalId)) {
			// Expanding picture for future reference
			refExpandDecPic(pCtx)
		}

		// move picture in list
		pCtx.pDecPic.uiTemporalId = pCtx.uiTemporalId
		pCtx.pDecPic.uiSpatialId = pCtx.uiDependencyId
		pCtx.pDecPic.iFrameNum = pParamD.iFrameNum
		pCtx.pDecPic.iFramePoc = pParamD.iPOC
		pCtx.pDecPic.bUsedAsRef = true
		pCtx.pDecPic.bIsLongRef = true
		pCtx.pDecPic.bIsSceneLTR = pLtr.bLTRMarkingFlag || (pCtx.pSvcParam.BEnableLongTermReference &&
			pCtx.eSliceType == common.I_SLICE)
		pCtx.pDecPic.iLongTermPicNum = pLtr.iCurLtrIdx
	}
	if pCtx.eSliceType == common.P_SLICE {
		DeleteNonSceneLTR(pCtx)
		LTRMarkProcessScreen(pCtx)
		pLtr.bLTRMarkingFlag = false
		pLtr.uiLtrMarkInterval++
	} else { // in case IDR currently coding
		LTRMarkProcessScreen(pCtx)
		pLtr.iCurLtrIdx = 1
		pLtr.iSceneLtrIdx = 1
		pLtr.uiLtrMarkInterval = 0
		pCtx.pVaa.uiValidLongTermPicIdx = 0
	}

	pCtx.pReferenceStrategy.EndofUpdateRefList()
	return true
}

func WelsBuildRefListScreen(pCtx *sWelsEncCtx, iPOC int32, iBestLtrRefIdx int32) bool {
	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	pParam := pCtx.pSvcParam
	pVaaExt := pCtx.pVaa.pExt
	iNumRef := pParam.INumRefFrame
	pParamD := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	pCtx.iNumRef0 = 0

	if pCtx.eSliceType != common.I_SLICE {
		var iLtrRefIdx int32
		var pRefOri *SPicture
		for idx := int32(0); idx < pVaaExt.iNumOfAvailableRef; idx++ {
			iLtrRefIdx = pCtx.pVpp.GetRefFrameInfo(idx, pCtx.bCurFrameMarkedAsSceneLtr, &pRefOri)
			if iLtrRefIdx >= 0 && iLtrRefIdx <= pParam.ILTRRefNum {
				pRefPic := pRefList.pLongRefList[iLtrRefIdx]
				if pRefPic != nil && pRefPic.bUsedAsRef && pRefPic.bIsLongRef {
					if pRefPic.uiTemporalId <= pCtx.uiTemporalId && (!pCtx.bCurFrameMarkedAsSceneLtr || pRefPic.bIsSceneLTR) {
						pCtx.pCurDqLayer.pRefOri[pCtx.iNumRef0] = pRefOri
						pCtx.pRefList0[pCtx.iNumRef0] = pRefPic
						pCtx.iNumRef0++
						common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
							"WelsBuildRefListScreen(), current iFrameNum = %d, current Tid = %d, ref iFrameNum = %d, ref uiTemporalId = %d, ref is Scene LTR = %d, LTR count = %d,iNumRef = %d",
							pParamD.iFrameNum, pCtx.uiTemporalId,
							pRefPic.iFrameNum, pRefPic.uiTemporalId, refB2I(pRefPic.bIsSceneLTR),
							pRefList.uiLongRefCount, iNumRef)
					}
				}
			} else {
				for i := iNumRef; i >= 0; i-- {
					if pRefList.pLongRefList[i] == nil {
						continue
					} else if pRefList.pLongRefList[i].uiTemporalId == 0 ||
						pRefList.pLongRefList[i].uiTemporalId < pCtx.uiTemporalId {
						pCtx.pCurDqLayer.pRefOri[pCtx.iNumRef0] = pRefOri
						pCtx.pRefList0[pCtx.iNumRef0] = pRefList.pLongRefList[i]
						pCtx.iNumRef0++
						common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
							"WelsBuildRefListScreen(), ref !current iFrameNum = %d, ref iFrameNum = %d,LTR number = %d",
							pParamD.iFrameNum, pCtx.pRefList0[pCtx.iNumRef0-1].iFrameNum, pRefList.uiLongRefCount)
						break
					}
				}
			}
		} // end of (int idx = 0; idx < pVaaExt->iNumOfAvailableRef; idx++)

		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
			"WelsBuildRefListScreen(), CurrentFramePoc=%d, isLTR=%d", iPOC, refB2I(pCtx.bCurFrameMarkedAsSceneLtr))
		for j := int32(0); j < iNumRef; j++ {
			pARefPicture := pRefList.pLongRefList[j]
			if pARefPicture != nil {
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
					"WelsBuildRefListScreen()\tRefLot[%d]: iPoc=%d, iPictureType=%d, bUsedAsRef=%d, bIsLongRef=%d, bIsSceneLTR=%d, uiTemporalId=%d, iFrameNum=%d, iMarkFrameNum=%d, iLongTermPicNum=%d, uiRecieveConfirmed=%d",
					j,
					pARefPicture.iFramePoc,
					pARefPicture.iPictureType,
					refB2I(pARefPicture.bUsedAsRef),
					refB2I(pARefPicture.bIsLongRef),
					refB2I(pARefPicture.bIsSceneLTR),
					pARefPicture.uiTemporalId,
					pARefPicture.iFrameNum,
					pARefPicture.iMarkFrameNum,
					pARefPicture.iLongTermPicNum,
					pARefPicture.uiRecieveConfirmed)
			} else {
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG, "WelsBuildRefListScreen()\tRefLot[%d]: NULL", j)
			}
		}
	} else {
		// dealing with IDR
		WelsResetRefList(pCtx)                         //for IDR, SHOULD reset pRef list.
		ResetLtrState(&pCtx.pLtr[pCtx.uiDependencyId]) //SHOULD update it when IDR.
		pCtx.pRefList0[0] = nil
	}
	if int32(pCtx.iNumRef0) > iNumRef {
		pCtx.iNumRef0 = uint8(iNumRef)
	}

	return pCtx.iNumRef0 > 0 || pCtx.eSliceType == common.I_SLICE
}

func IsValidFrameNum(kiFrameNum int32) bool {
	return kiFrameNum < (1 << 30) // TODO: use the original judge first, may be improved
}

func WelsMarkMMCORefInfoScreen(pCtx *sWelsEncCtx, pLtr *SLTRState, ppSliceList []*SSlice, kiCountSliceNum int32) {
	pBaseSlice := ppSliceList[0]
	pRefPicMark := &pBaseSlice.sSliceHeaderExt.sSliceHeader.sRefMarking
	iMaxLtrIdx := pCtx.pSvcParam.INumRefFrame - STR_ROOM - 1

	*pRefPicMark = SRefPicMarking{}
	if pCtx.pSvcParam.BEnableLongTermReference {
		pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMaxLongTermFrameIdx = iMaxLtrIdx
		pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMmcoType = common.MMCO_SET_MAX_LONG
		pRefPicMark.uiMmcoCount++

		pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iLongTermFrameIdx = pLtr.iCurLtrIdx
		pRefPicMark.SMmcoRef[pRefPicMark.uiMmcoCount].iMmcoType = common.MMCO_LONG
		pRefPicMark.uiMmcoCount++
	}

	WelsMarkMMCORefInfoWithBase(ppSliceList, pBaseSlice, kiCountSliceNum)
}

func WelsMarkPicScreen(pCtx *sWelsEncCtx) {
	pLtr := &pCtx.pLtr[pCtx.uiDependencyId]
	iMaxTid := common.WELS_LOG2(pCtx.pSvcParam.uiGopSize)
	iMaxActualLtrIdx := int32(-1)
	pParamD := &pCtx.pSvcParam.sDependencyLayers[pCtx.uiDependencyId]
	if pCtx.pSvcParam.BEnableLongTermReference {
		iMaxActualLtrIdx = pCtx.pSvcParam.INumRefFrame - STR_ROOM - 1 - common.WELS_MAX(iMaxTid, 1)
	}

	pRefList := pCtx.ppRefPicListExt[pCtx.uiDependencyId]
	ppLongRefList := &pRefList.pLongRefList
	iNumRef := pCtx.pSvcParam.INumRefFrame
	var i int32
	iLongRefNum := iNumRef - STR_ROOM
	bIsRefListNotFull := int32(pRefList.uiLongRefCount) < iLongRefNum

	if !pCtx.pSvcParam.BEnableLongTermReference {
		pLtr.iCurLtrIdx = int32(pCtx.uiTemporalId)
	} else {
		if iMaxActualLtrIdx != -1 && pCtx.uiTemporalId == 0 && pCtx.bCurFrameMarkedAsSceneLtr {
			//Scene LTR
			pLtr.bLTRMarkingFlag = true
			pLtr.uiLtrMarkInterval = 0
			pLtr.iCurLtrIdx = pLtr.iSceneLtrIdx % (iMaxActualLtrIdx + 1)
			pLtr.iSceneLtrIdx++
		} else {
			pLtr.bLTRMarkingFlag = false
			//for other LTR
			if bIsRefListNotFull {
				for i := int32(0); i < iLongRefNum; i++ {
					if pRefList.pLongRefList[i] == nil {
						pLtr.iCurLtrIdx = i
						break
					}
				}
			} else {
				var iRefNum_t [api.MAX_TEMPORAL_LAYER_NUM]int32
				for i = 0; i < int32(pRefList.uiLongRefCount); i++ {
					if ppLongRefList[i].bUsedAsRef && ppLongRefList[i].bIsLongRef && (!ppLongRefList[i].bIsSceneLTR) {
						iRefNum_t[ppLongRefList[i].uiTemporalId]++
					}
				}

				var iMaxMultiRefTid int32
				if iMaxTid != 0 {
					iMaxMultiRefTid = iMaxTid - 1
				}
				for i = 0; i < api.MAX_TEMPORAL_LAYER_NUM; i++ {
					if iRefNum_t[i] > 1 {
						iMaxMultiRefTid = i
					}
				}
				iLongestDeltaFrameNum := int32(-1)
				iMaxFrameNum := int32(1) << pCtx.pSps.uiLog2MaxFrameNum

				for i = 0; i < int32(pRefList.uiLongRefCount); i++ {
					if ppLongRefList[i].bUsedAsRef && ppLongRefList[i].bIsLongRef && (!ppLongRefList[i].bIsSceneLTR) &&
						iMaxMultiRefTid == int32(ppLongRefList[i].uiTemporalId) {
						if !IsValidFrameNum(ppLongRefList[i].iFrameNum) { // pLtr->iCurLtrIdx must have a value
							common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "WelsMarkPicScreen, Invalid Frame Number")
							return
						}
						var iDeltaFrameNum int32
						if pParamD.iFrameNum >= ppLongRefList[i].iFrameNum {
							iDeltaFrameNum = pParamD.iFrameNum - ppLongRefList[i].iFrameNum
						} else {
							iDeltaFrameNum = pParamD.iFrameNum + iMaxFrameNum - ppLongRefList[i].iFrameNum
						}

						if iDeltaFrameNum > iLongestDeltaFrameNum {
							pLtr.iCurLtrIdx = ppLongRefList[i].iLongTermPicNum
							iLongestDeltaFrameNum = iDeltaFrameNum
						}
					}
				}
			}
		}
	}

	for i = 0; i < api.MAX_TEMPORAL_LAYER_NUM; i++ {
		if (int32(pCtx.uiTemporalId) < i) || (pCtx.uiTemporalId == 0) {
			pLtr.iLastLtrIdx[i] = pLtr.iCurLtrIdx
		}
	}

	iSliceNum := pCtx.pCurDqLayer.iMaxSliceNum

	WelsMarkMMCORefInfoScreen(pCtx, pLtr, pCtx.pCurDqLayer.ppSliceInLayer, iSliceNum)
}

func DoNothing(pointer *sWelsEncCtx) {
}

// CreateReferenceStrategy is static IWelsReferenceStrategy::CreateReferenceStrategy.
func CreateReferenceStrategy(pCtx *sWelsEncCtx, keUsageType api.EUsageType, kbLtrEnabled bool) IWelsReferenceStrategy {
	var pReferenceStrategy IWelsReferenceStrategy
	switch keUsageType {
	case api.SCREEN_CONTENT_REAL_TIME:
		if kbLtrEnabled {
			pReferenceStrategy = &CWelsReference_LosslessWithLtr{}
		} else {
			pReferenceStrategy = &CWelsReference_Screen{}
		}
	case api.CAMERA_VIDEO_REAL_TIME, api.CAMERA_VIDEO_NON_REAL_TIME:
		fallthrough
	default:
		pReferenceStrategy = &CWelsReference_TemporalLayer{}
	}
	pReferenceStrategy.Init(pCtx)
	return pReferenceStrategy
}

func (p *CWelsReference_TemporalLayer) Init(pCtx *sWelsEncCtx) {
	p.m_pEncoderCtx = pCtx
}

func (p *CWelsReference_TemporalLayer) BuildRefList(iPOC int32, iBestLtrRefIdx int32) bool {
	return WelsBuildRefList(p.m_pEncoderCtx, iPOC, iBestLtrRefIdx)
}

func (p *CWelsReference_TemporalLayer) MarkPic() {
	WelsMarkPic(p.m_pEncoderCtx)
}

func (p *CWelsReference_TemporalLayer) UpdateRefList() bool {
	return WelsUpdateRefList(p.m_pEncoderCtx)
}

func (p *CWelsReference_TemporalLayer) EndofUpdateRefList() {
	PrefetchNextBuffer(p.m_pEncoderCtx)
}

func (p *CWelsReference_TemporalLayer) AfterBuildRefList() {
	DoNothing(p.m_pEncoderCtx)
}

func (p *CWelsReference_Screen) BuildRefList(iPOC int32, iBestLtrRefIdx int32) bool {
	return WelsBuildRefList(p.m_pEncoderCtx, iPOC, iBestLtrRefIdx)
}

func (p *CWelsReference_Screen) MarkPic() {
	WelsMarkPic(p.m_pEncoderCtx)
}

func (p *CWelsReference_Screen) UpdateRefList() bool {
	return WelsUpdateRefList(p.m_pEncoderCtx)
}

func (p *CWelsReference_Screen) EndofUpdateRefList() {
	UpdateSrcPicList(p.m_pEncoderCtx)
}

func (p *CWelsReference_Screen) AfterBuildRefList() {
	UpdateBlockStatic(p.m_pEncoderCtx)
}

func (p *CWelsReference_LosslessWithLtr) BuildRefList(iPOC int32, iBestLtrRefIdx int32) bool {
	return WelsBuildRefListScreen(p.m_pEncoderCtx, iPOC, iBestLtrRefIdx)
}

func (p *CWelsReference_LosslessWithLtr) MarkPic() {
	WelsMarkPicScreen(p.m_pEncoderCtx)
}

func (p *CWelsReference_LosslessWithLtr) UpdateRefList() bool {
	return WelsUpdateRefListScreen(p.m_pEncoderCtx)
}

func (p *CWelsReference_LosslessWithLtr) EndofUpdateRefList() {
	UpdateSrcPicListLosslessScreenRefSelectionWithLtr(p.m_pEncoderCtx)
}
