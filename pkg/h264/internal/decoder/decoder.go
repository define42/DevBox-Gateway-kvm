// Port of codec/decoder/core/src/decoder.cpp.
//
// Interfaces implementation introduced in decoder system architecture.
// Only the single-threaded, plain C (`_c`) code paths are ported: SIMD
// function-table overrides and the multi-threaded branches (thread count is
// always <= 1 in the Go port) are dropped.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// static int32_t CreatePicBuff (PWelsDecoderContext pCtx, PPicBuff* ppPicBuf, const int32_t kiSize,
// const int32_t kiPicWidth, const int32_t kiPicHeight)
func CreatePicBuff(pCtx *SWelsDecoderContext, ppPicBuf **SPicBuff, kiSize int32, kiPicWidth int32,
	kiPicHeight int32) int32 {
	var pPicBuf *SPicBuff
	var iPicIdx int32
	if kiSize <= 0 || kiPicWidth <= 0 || kiPicHeight <= 0 {
		return ERR_INFO_INVALID_PARAM
	}

	pPicBuf = new(SPicBuff)

	pPicBuf.ppPic = make([]*SPicture, kiSize)

	for iPicIdx = 0; iPicIdx < kiSize; iPicIdx++ {
		pPic := AllocPicture(pCtx, kiPicWidth, kiPicHeight)
		if nil == pPic {
			// init capacity first for free memory
			pPicBuf.iCapacity = iPicIdx
			DestroyPicBuff(pCtx, &pPicBuf)
			return ERR_INFO_OUT_OF_MEMORY
		}
		pPicBuf.ppPic[iPicIdx] = pPic
	}

	// initialize context in queue
	pPicBuf.iCapacity = kiSize
	pPicBuf.iCurrentIdx = 0
	*ppPicBuf = pPicBuf

	return ERR_NONE
}

// static int32_t IncreasePicBuff (PWelsDecoderContext pCtx, PPicBuff* ppPicBuf, const int32_t kiOldSize,
// const int32_t kiPicWidth, const int32_t kiPicHeight, const int32_t kiNewSize)
func IncreasePicBuff(pCtx *SWelsDecoderContext, ppPicBuf **SPicBuff, kiOldSize int32, kiPicWidth int32,
	kiPicHeight int32, kiNewSize int32) int32 {
	pPicOldBuf := *ppPicBuf
	var pPicNewBuf *SPicBuff
	var iPicIdx int32
	if kiOldSize <= 0 || kiNewSize <= 0 || kiPicWidth <= 0 || kiPicHeight <= 0 {
		return ERR_INFO_INVALID_PARAM
	}

	pPicNewBuf = new(SPicBuff)

	pPicNewBuf.ppPic = make([]*SPicture, kiNewSize)

	// increase new PicBuf
	for iPicIdx = kiOldSize; iPicIdx < kiNewSize; iPicIdx++ {
		pPic := AllocPicture(pCtx, kiPicWidth, kiPicHeight)
		if nil == pPic {
			// Set maximum capacity as the new malloc memory at the tail
			pPicNewBuf.iCapacity = iPicIdx
			DestroyPicBuff(pCtx, &pPicNewBuf)
			return ERR_INFO_OUT_OF_MEMORY
		}
		pPicNewBuf.ppPic[iPicIdx] = pPic
	}

	// copy old PicBuf to new PicBuf
	copy(pPicNewBuf.ppPic[:kiOldSize], pPicOldBuf.ppPic[:kiOldSize])

	// initialize context in queue
	pPicNewBuf.iCapacity = kiNewSize
	pPicNewBuf.iCurrentIdx = pPicOldBuf.iCurrentIdx
	*ppPicBuf = pPicNewBuf

	// only initialize new slots; old slots must preserve iRefCount pinned by output buffering
	for i := kiOldSize; i < pPicNewBuf.iCapacity; i++ {
		pPicNewBuf.ppPic[i].bUsedAsRef = false
		pPicNewBuf.ppPic[i].bIsLongRef = false
		pPicNewBuf.ppPic[i].iRefCount = 0
		pPicNewBuf.ppPic[i].pSetUnRef = nil
		pPicNewBuf.ppPic[i].bIsComplete = false
	}
	// remove old PicBuf
	if pPicOldBuf.ppPic != nil {
		pPicOldBuf.ppPic = nil
	}
	pPicOldBuf.iCapacity = 0
	pPicOldBuf.iCurrentIdx = 0
	return ERR_NONE
}

// static int32_t DecreasePicBuff (PWelsDecoderContext pCtx, PPicBuff* ppPicBuf, const int32_t kiOldSize,
// const int32_t kiPicWidth, const int32_t kiPicHeight, const int32_t kiNewSize)
func DecreasePicBuff(pCtx *SWelsDecoderContext, ppPicBuf **SPicBuff, kiOldSize int32, kiPicWidth int32,
	kiPicHeight int32, kiNewSize int32) int32 {
	pPicOldBuf := *ppPicBuf
	var pPicNewBuf *SPicBuff
	var iPicIdx int32
	if kiOldSize <= 0 || kiNewSize <= 0 || kiPicWidth <= 0 || kiPicHeight <= 0 {
		return ERR_INFO_INVALID_PARAM
	}

	pPicNewBuf = new(SPicBuff)

	pPicNewBuf.ppPic = make([]*SPicture, kiNewSize)

	ResetReorderingPictureBuffers(pCtx.pPictReoderingStatus, pCtx.pPictInfoList, false)

	var iPrevPicIdx int32 = -1
	for iPrevPicIdx = 0; iPrevPicIdx < kiOldSize; iPrevPicIdx++ {
		if pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb == pPicOldBuf.ppPic[iPrevPicIdx] {
			break
		}
	}
	var iDelIdx int32
	if iPrevPicIdx < kiOldSize && iPrevPicIdx >= kiNewSize {
		// found pPreviousDecodedPictureInDpb,
		pPicNewBuf.ppPic[0] = pPicOldBuf.ppPic[iPrevPicIdx]
		pPicNewBuf.iCurrentIdx = 0
		copy(pPicNewBuf.ppPic[1:kiNewSize], pPicOldBuf.ppPic[:kiNewSize-1])
		iDelIdx = kiNewSize - 1
	} else {
		copy(pPicNewBuf.ppPic[:kiNewSize], pPicOldBuf.ppPic[:kiNewSize])
		if iPrevPicIdx < kiNewSize {
			pPicNewBuf.iCurrentIdx = iPrevPicIdx
		} else {
			pPicNewBuf.iCurrentIdx = 0
		}
		iDelIdx = kiNewSize
	}

	//update references due to allocation changes
	//all references' references have to be reset oss-buzz 14423
	for i := int32(0); i < kiNewSize; i++ {
		for listIdx := common.LIST_0; listIdx < common.LIST_A; listIdx++ {
			j := -1
			for {
				j++
				if !(j < MAX_DPB_COUNT && pPicNewBuf.ppPic[i].pRefPic[listIdx][j] != nil) {
					break
				}
				pPicNewBuf.ppPic[i].pRefPic[listIdx][j] = nil
			}
		}
	}

	for iPicIdx = iDelIdx; iPicIdx < kiOldSize; iPicIdx++ {
		if iPrevPicIdx != iPicIdx {
			if pPicOldBuf.ppPic[iPicIdx] != nil {
				FreePicture(pPicOldBuf.ppPic[iPicIdx])
				pPicOldBuf.ppPic[iPicIdx] = nil
			}
		}
	}

	// initialize context in queue
	pPicNewBuf.iCapacity = kiNewSize
	*ppPicBuf = pPicNewBuf

	for i := int32(0); i < pPicNewBuf.iCapacity; i++ {
		pPicNewBuf.ppPic[i].bUsedAsRef = false
		pPicNewBuf.ppPic[i].bIsLongRef = false
		pPicNewBuf.ppPic[i].iRefCount = 0
		pPicNewBuf.ppPic[i].pSetUnRef = nil
		pPicNewBuf.ppPic[i].bIsComplete = false
	}
	// remove old PicBuf
	if pPicOldBuf.ppPic != nil {
		pPicOldBuf.ppPic = nil
	}
	pPicOldBuf.iCapacity = 0
	pPicOldBuf.iCurrentIdx = 0

	return ERR_NONE
}

// void DestroyPicBuff (PWelsDecoderContext pCtx, PPicBuff* ppPicBuf, CMemoryAlign* pMa)
//
// The C CMemoryAlign* pMa parameter is dropped.
func DestroyPicBuff(pCtx *SWelsDecoderContext, ppPicBuf **SPicBuff) {
	var pPicBuf *SPicBuff

	ResetReorderingPictureBuffers(pCtx.pPictReoderingStatus, pCtx.pPictInfoList, false)

	if nil == ppPicBuf || nil == *ppPicBuf {
		return
	}

	pPicBuf = *ppPicBuf
	for pPicBuf.ppPic != nil {
		var iPicIdx int32
		for iPicIdx < pPicBuf.iCapacity {
			pPic := pPicBuf.ppPic[iPicIdx]
			if pPic != nil {
				FreePicture(pPic)
			}
			iPicIdx++
		}

		pPicBuf.ppPic = nil
	}
	pPicBuf.iCapacity = 0
	pPicBuf.iCurrentIdx = 0

	*ppPicBuf = nil
}

// void ResetReorderingPictureBuffers (PPictReoderingStatus pPictReoderingStatus, PPictInfo pPictInfo,
// const bool& fullReset)
//
// pPictInfo: the 16-entry picture info list (C: PPictInfo pointing at its first element).
// reset picture reodering buffer list
func ResetReorderingPictureBuffers(pPictReoderingStatus *SPictReoderingStatus, pPictInfo []SPictInfo, fullReset bool) {
	if pPictReoderingStatus != nil && pPictInfo != nil {
		var pictInfoListCount int32
		if fullReset {
			pictInfoListCount = 16
		} else {
			pictInfoListCount = pPictReoderingStatus.iLargestBufferedPicIndex + 1
		}
		pPictReoderingStatus.iPictInfoIndex = 0
		pPictReoderingStatus.iMinPOC = IMinInt32
		pPictReoderingStatus.iNumOfPicts = 0
		pPictReoderingStatus.iLastWrittenPOC = IMinInt32
		pPictReoderingStatus.iLargestBufferedPicIndex = 0
		for i := int32(0); i < pictInfoListCount; i++ {
			pPictInfo[i].iPOC = IMinInt32
			pPictInfo[i].iPicBuffIdx = -1 //ensure a deterministic invalid sentinel so error-path decoding cannot leave heap garbage
		}
		pPictInfo[0].sBufferInfo.IBufferStatus = 0
		pPictReoderingStatus.bHasBSlice = false
	}
}

// void WelsDecoderDefaults (PWelsDecoderContext pCtx, SLogContext* pLogCtx)
//
// fill data fields in default for decoder context
func WelsDecoderDefaults(pCtx *SWelsDecoderContext, pLogCtx *common.SLogContext) {
	var iCpuCores int32 = 1
	pCtx.sLogCtx = *pLogCtx

	pCtx.pArgDec = nil

	pCtx.bHaveGotMemory = false // not ever request memory blocks for decoder context related
	pCtx.uiCpuFlag = 0

	pCtx.bAuReadyFlag = false // au data is not ready
	pCtx.bCabacInited = false

	pCtx.uiCpuFlag = common.WelsCPUFeatureDetect(&iCpuCores)

	pCtx.iImgWidthInPixel = 0
	pCtx.iImgHeightInPixel = 0 // alloc picture data when picture size is available
	pCtx.iLastImgWidthInPixel = 0
	pCtx.iLastImgHeightInPixel = 0
	pCtx.bFreezeOutput = true

	pCtx.iFrameNum = -1
	pCtx.pLastDecPicInfo.iPrevFrameNum = -1
	pCtx.iErrorCode = ERR_NONE

	pCtx.pDec = nil

	pCtx.pTempDec = nil

	WelsResetRefPic(pCtx)

	pCtx.iActiveFmoNum = 0

	pCtx.pPicBuff = nil

	//pCtx->sSpsPpsCtx.bAvcBasedFlag             = true;
	pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = nil
	pCtx.pDecoderStatistics.IAvgLumaQp = -1
	pCtx.pDecoderStatistics.IStatisticsLogInterval = 1000
	pCtx.bUseScalingList = false
	pCtx.iFeedbackNalRefIdc = -1 //initialize
	pCtx.pLastDecPicInfo.iPrevPicOrderCntMsb = 0
	pCtx.pLastDecPicInfo.iPrevPicOrderCntLsb = 0
}

// void WelsDecoderSpsPpsDefaults (SWelsDecoderSpsPpsCTX& sSpsPpsCtx)
//
// C++ reference -> pointer.
// fill data fields in SPS and PPS default for decoder context
func WelsDecoderSpsPpsDefaults(sSpsPpsCtx *SWelsDecoderSpsPpsCTX) {
	sSpsPpsCtx.bSpsExistAheadFlag = false
	sSpsPpsCtx.bSubspsExistAheadFlag = false
	sSpsPpsCtx.bPpsExistAheadFlag = false
	sSpsPpsCtx.bAvcBasedFlag = true
	sSpsPpsCtx.iSpsErrorIgnored = 0
	sSpsPpsCtx.iSubSpsErrorIgnored = 0
	sSpsPpsCtx.iPpsErrorIgnored = 0
	sSpsPpsCtx.iPPSInvalidNum = 0
	sSpsPpsCtx.iPPSLastInvalidId = -1
	sSpsPpsCtx.iSPSInvalidNum = 0
	sSpsPpsCtx.iSPSLastInvalidId = -1
	sSpsPpsCtx.iSubSPSInvalidNum = 0
	sSpsPpsCtx.iSubSPSLastInvalidId = -1
	sSpsPpsCtx.iSeqId = -1
}

// void WelsDecoderLastDecPicInfoDefaults (SWelsLastDecPicInfo& sLastDecPicInfo)
//
// C++ reference -> pointer.
// fill last decoded picture info
func WelsDecoderLastDecPicInfoDefaults(sLastDecPicInfo *SWelsLastDecPicInfo) {
	sLastDecPicInfo.iPrevPicOrderCntMsb = 0
	sLastDecPicInfo.iPrevPicOrderCntLsb = 0
	sLastDecPicInfo.pPreviousDecodedPictureInDpb = nil
	sLastDecPicInfo.iPrevFrameNum = -1
	sLastDecPicInfo.bLastHasMmco5 = false
	sLastDecPicInfo.uiDecodingTimeStamp = 0
}

// void CopySpsPps (PWelsDecoderContext pFromCtx, PWelsDecoderContext pToCtx)
//
// copy SpsPps from one Ctx to another ctx for threaded code
func CopySpsPps(pFromCtx *SWelsDecoderContext, pToCtx *SWelsDecoderContext) {
	pToCtx.sSpsPpsCtx = pFromCtx.sSpsPpsCtx
	pFromCurAu := pFromCtx.pAccessUnitList
	var pTmpLayerSps [MAX_LAYER_NUM]*SSps
	for i := 0; i < MAX_LAYER_NUM; i++ {
		pTmpLayerSps[i] = nil
	}
	// track the layer sps for the current au
	for i := pFromCurAu.uiStartPos; i <= pFromCurAu.uiEndPos; i++ {
		uiDid := uint32(pFromCurAu.pNalUnitsList[i].sNalHeaderExt.UiDependencyId)
		pTmpLayerSps[uiDid] = pFromCurAu.pNalUnitsList[i].sNalData.sVclNal.sSliceHeaderExt.sSliceHeader.pSps
		for j := 0; j < common.MAX_SPS_COUNT+1; j++ {
			if &pFromCtx.sSpsPpsCtx.sSpsBuffer[j] == pTmpLayerSps[uiDid] {
				pTmpLayerSps[uiDid] = &pToCtx.sSpsPpsCtx.sSpsBuffer[j]
				break
			}
		}
	}
	for i := 0; i < MAX_LAYER_NUM; i++ {
		if pTmpLayerSps[i] != nil {
			pToCtx.sSpsPpsCtx.pActiveLayerSps[i] = pTmpLayerSps[i]
		}
	}
}

// static inline int32_t GetTargetRefListSize (PWelsDecoderContext pCtx)
//
// get size of reference picture list in target layer incoming, = (iNumRefFrames
func GetTargetRefListSize(pCtx *SWelsDecoderContext) int32 {
	var iNumRefFrames int32
	// +2 for EC MV Copy buffer exchange
	if (pCtx == nil) || (pCtx.pSps == nil) {
		iNumRefFrames = MAX_REF_PIC_COUNT + 2
	} else {
		iNumRefFrames = pCtx.pSps.iNumRefFrames + 2
		iThreadCount := GetThreadCount(pCtx)
		if iThreadCount > 1 {
			//due to thread and reordering buffering, it needs more dpb space
			iNumRefFrames = MAX_DPB_COUNT + iThreadCount
		}
	}

	// LONG_TERM_REF
	//pic_queue size minimum set 2
	if iNumRefFrames < 2 {
		iNumRefFrames = 2
	}

	return iNumRefFrames
}

// int32_t WelsRequestMem (PWelsDecoderContext pCtx, const int32_t kiMbWidth, const int32_t kiMbHeight,
// bool& bReallocFlag)
//
// request memory blocks for decoder avc part
func WelsRequestMem(pCtx *SWelsDecoderContext, kiMbWidth int32, kiMbHeight int32, bReallocFlag *bool) int32 {
	kiPicWidth := kiMbWidth << 4
	kiPicHeight := kiMbHeight << 4
	var iErr int32 = ERR_NONE

	var iPicQueueSize int32 // adaptive size of picture queue, = (pSps->iNumRefFrames x 2)
	*bReallocFlag = false
	bNeedChangePicQueue := true

	if nil == pCtx || kiPicWidth <= 0 || kiPicHeight <= 0 {
		return ERR_INFO_INVALID_PARAM
	}

	// Fixed the issue about different gop size over last, 5/17/2010
	// get picture queue size currently
	iPicQueueSize = GetTargetRefListSize(pCtx) // adaptive size of picture queue, = (pSps->iNumRefFrames x 2)
	pCtx.iPicQueueNumber = iPicQueueSize
	if pCtx.pPicBuff != nil &&
		pCtx.pPicBuff.iCapacity == iPicQueueSize { // comparing current picture queue size requested and previous allocation picture queue
		bNeedChangePicQueue = false
	}
	// HD based pic buffer need consider memory size consumed when switch from 720p to other lower size
	if pCtx.bHaveGotMemory && (kiPicWidth == pCtx.iImgWidthInPixel &&
		kiPicHeight == pCtx.iImgHeightInPixel) && (!bNeedChangePicQueue) { // have same scaled buffer
		return ERR_NONE
	}

	// sync update pRefList
	if GetThreadCount(pCtx) <= 1 {
		WelsResetRefPic(pCtx) // added to sync update ref list due to pictures are free
	}

	if pCtx.bHaveGotMemory && (kiPicWidth == pCtx.iImgWidthInPixel && kiPicHeight == pCtx.iImgHeightInPixel) &&
		pCtx.pPicBuff != nil && pCtx.pPicBuff.iCapacity != iPicQueueSize {
		// currently only active for LIST_0 due to have no B frames
		// Actually just need one memory allocation for the PicBuff. While it needs two pointer list (LIST_0 and LIST_1).
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO,
			"WelsRequestMem(): memory re-alloc for no resolution change (size = %d * %d), ref list size change from %d to %d",
			kiPicWidth, kiPicHeight, pCtx.pPicBuff.iCapacity, iPicQueueSize)
		if pCtx.pPicBuff.iCapacity < iPicQueueSize {
			iErr = IncreasePicBuff(pCtx, &pCtx.pPicBuff, pCtx.pPicBuff.iCapacity, kiPicWidth, kiPicHeight,
				iPicQueueSize)
		} else {
			iErr = DecreasePicBuff(pCtx, &pCtx.pPicBuff, pCtx.pPicBuff.iCapacity, kiPicWidth, kiPicHeight,
				iPicQueueSize)
		}
	} else {
		if pCtx.bHaveGotMemory {
			var iOldCapacity int32
			if pCtx.pPicBuff != nil {
				iOldCapacity = pCtx.pPicBuff.iCapacity
			}
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO,
				"WelsRequestMem(): memory re-alloc for resolution change, size change from %d * %d to %d * %d, ref list size change from %d to %d",
				pCtx.iImgWidthInPixel, pCtx.iImgHeightInPixel, kiPicWidth, kiPicHeight, iOldCapacity,
				iPicQueueSize)
		} else {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "WelsRequestMem(): memory alloc size = %d * %d, ref list size = %d",
				kiPicWidth, kiPicHeight, iPicQueueSize)
		}
		// for Recycled_Pic_Queue
		ppPic := &pCtx.pPicBuff
		if nil != ppPic && nil != *ppPic {
			DestroyPicBuff(pCtx, ppPic)
		}

		pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb = nil

		// currently only active for LIST_0 due to have no B frames
		iErr = CreatePicBuff(pCtx, &pCtx.pPicBuff, iPicQueueSize, kiPicWidth, kiPicHeight)
	}

	if iErr != ERR_NONE {
		return iErr
	}

	pCtx.iImgWidthInPixel = kiPicWidth   // target width of image to be reconstruted while decoding
	pCtx.iImgHeightInPixel = kiPicHeight // target height of image to be reconstruted while decoding

	pCtx.bHaveGotMemory = true // global memory for decoder context related is requested
	pCtx.pDec = nil            // need prefetch a new pic due to spatial size changed

	if pCtx.pCabacDecEngine == nil {
		pCtx.pCabacDecEngine = new(SWelsCabacDecEngine)
	}

	*bReallocFlag = true // memory re-allocation successfully finished
	return ERR_NONE
}

// void WelsFreeDynamicMemory (PWelsDecoderContext pCtx)
//
// free memory dynamically allocated during decoder
func WelsFreeDynamicMemory(pCtx *SWelsDecoderContext) {
	//free dq layer memory
	UninitialDqLayersContext(pCtx)

	//free FMO memory
	ResetFmoList(pCtx)

	//free ref-pic list & picture memory
	WelsResetRefPic(pCtx)

	pPicBuff := &pCtx.pPicBuff
	if nil != pPicBuff && nil != *pPicBuff {
		DestroyPicBuff(pCtx, pPicBuff)
	}
	// The GetThreadCount (pCtx) > 1 branch (clearing sibling thread contexts'
	// pPicBuff) is not ported: the Go port is single-threaded.

	if pCtx.pTempDec != nil {
		FreePicture(pCtx.pTempDec)
		pCtx.pTempDec = nil
	}

	// added for safe memory
	pCtx.iImgWidthInPixel = 0
	pCtx.iImgHeightInPixel = 0
	pCtx.iLastImgWidthInPixel = 0
	pCtx.iLastImgHeightInPixel = 0
	pCtx.bFreezeOutput = true
	pCtx.bHaveGotMemory = false

	//free CABAC memory
	pCtx.pCabacDecEngine = nil
}

// int32_t WelsOpenDecoder (PWelsDecoderContext pCtx, SLogContext* pLogCtx)
//
// Open decoder
func WelsOpenDecoder(pCtx *SWelsDecoderContext, pLogCtx *common.SLogContext) int32 {
	var iRet int32 = ERR_NONE
	// function pointers
	InitDecFuncs(pCtx, pCtx.uiCpuFlag)

	// vlc tables
	InitVlcTable(pCtx.pVlcTable)

	// static memory
	iRet = WelsInitStaticMemory(pCtx)
	if ERR_NONE != iRet {
		pCtx.iErrorCode |= int32(api.DsOutOfMemory)
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsInitStaticMemory() failed in WelsOpenDecoder().")
		return iRet
	}

	// LONG_TERM_REF
	pCtx.bParamSetsLostFlag = true
	pCtx.bNewSeqBegin = true
	pCtx.bPrintFrameErrorTraceFlag = true
	pCtx.iIgnoredErrorInfoPacketCount = 0
	pCtx.bFrameFinish = true
	pCtx.iSeqNum = 0
	return iRet
}

// void WelsCloseDecoder (PWelsDecoderContext pCtx)
//
// Close decoder
func WelsCloseDecoder(pCtx *SWelsDecoderContext) {
	WelsFreeDynamicMemory(pCtx)

	WelsFreeStaticMemory(pCtx)

	// LONG_TERM_REF
	pCtx.bParamSetsLostFlag = false
	pCtx.bNewSeqBegin = false
	pCtx.bPrintFrameErrorTraceFlag = false
}

// int32_t DecoderConfigParam (PWelsDecoderContext pCtx, const SDecodingParam* kpParam)
//
// configure decoder parameters
func DecoderConfigParam(pCtx *SWelsDecoderContext, kpParam *api.SDecodingParam) int32 {
	if nil == pCtx || nil == kpParam {
		return ERR_INFO_INVALID_PARAM
	}

	*pCtx.pParam = *kpParam
	if (pCtx.pParam.EEcActiveIdc > api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE) ||
		(pCtx.pParam.EEcActiveIdc < api.ERROR_CON_DISABLE) {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
			"eErrorConMethod (%d) not in range: (%d - %d). Set as default value: (%d).", int32(pCtx.pParam.EEcActiveIdc),
			int32(api.ERROR_CON_DISABLE), int32(api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE),
			int32(api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE))
		pCtx.pParam.EEcActiveIdc = api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE
	}

	if pCtx.pParam.BParseOnly { //parse only, disable EC method
		pCtx.pParam.EEcActiveIdc = api.ERROR_CON_DISABLE
	}
	InitErrorCon(pCtx)

	if api.VIDEO_BITSTREAM_SVC == pCtx.pParam.SVideoProperty.EVideoBsType ||
		api.VIDEO_BITSTREAM_AVC == pCtx.pParam.SVideoProperty.EVideoBsType {
		pCtx.eVideoType = pCtx.pParam.SVideoProperty.EVideoBsType
	} else {
		pCtx.eVideoType = api.VIDEO_BITSTREAM_DEFAULT
	}

	common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "eVideoType: %d", int32(pCtx.eVideoType))

	return ERR_NONE
}

// int32_t WelsInitDecoder (PWelsDecoderContext pCtx, SLogContext* pLogCtx)
//
// Initialize Wels decoder parameters and memory.
// return 0 - successed, 1 - failed
func WelsInitDecoder(pCtx *SWelsDecoderContext, pLogCtx *common.SLogContext) int32 {
	if pCtx == nil {
		return ERR_INFO_INVALID_PTR
	}

	// open decoder
	return WelsOpenDecoder(pCtx, pLogCtx)
}

// void WelsEndDecoder (PWelsDecoderContext pCtx)
//
// Uninitialize Wels decoder parameters and memory
func WelsEndDecoder(pCtx *SWelsDecoderContext) {
	// close decoder
	WelsCloseDecoder(pCtx)
}

// void GetVclNalTemporalId (PWelsDecoderContext pCtx)
func GetVclNalTemporalId(pCtx *SWelsDecoderContext) {
	pAccessUnit := pCtx.pAccessUnitList
	idx := pAccessUnit.uiStartPos

	pCtx.iFeedbackVclNalInAu = int32(api.FEEDBACK_VCL_NAL)
	pCtx.iFeedbackTidInAu = int32(pAccessUnit.pNalUnitsList[idx].sNalHeaderExt.UiTemporalId)
	pCtx.iFeedbackNalRefIdc = int32(pAccessUnit.pNalUnitsList[idx].sNalHeaderExt.SNalUnitHeader.UiNalRefIdc)
}

// static bool RawDataWrapIsClean (PWelsDecoderContext pCtx, int32_t iNewBytes)
//
// Returns false if wrapping sRawData to pHead would overwrite bytes still referenced
// by a queued NAL's slice bit-reader, indicating the wrap is unsafe.
func RawDataWrapIsClean(pCtx *SWelsDecoderContext, iNewBytes int32) bool {
	pAu := pCtx.pAccessUnitList
	if pAu == nil || pAu.pNalUnitsList == nil || pAu.uiAvailUnitsNum == 0 {
		return true
	}
	pHead := pCtx.sRawData.pHead
	for i := uint32(0); i < pAu.uiAvailUnitsNum; i++ {
		pNal := pAu.pNalUnitsList[i]
		if pNal == nil {
			continue
		}
		pBits := &pNal.sNalData.sVclNal.sSliceBitsRead
		// C: pStart != NULL && pStart >= pHead && pStart < pHead + iNewBytes.
		// The bit reader's start lies inside pHead only if it reads the very
		// same allocation; its offset is then relative to pHead.
		if sameBuffer(pBits.PBuf, pHead) && pBits.PStartBuf >= 0 && pBits.PStartBuf < int(iNewBytes) {
			return false
		}
	}
	return true
}

// sameBuffer reports whether a and b share the same backing array start
// (C pointer identity of two whole allocations).
func sameBuffer(a, b []uint8) bool {
	if cap(a) == 0 || cap(b) == 0 {
		return false
	}
	return &a[:1][0] == &b[:1][0]
}

// int32_t WelsDecodeBs (PWelsDecoderContext pCtx, const uint8_t* kpBsBuf, const int32_t kiBsLen,
// uint8_t** ppDst, SBufferInfo* pDstBufInfo, SParserBsInfo* pDstBsInfo)
//
// kpBsBuf: the input bit stream (C pointer = kpBsBuf[0]). ppDst: see ConstructAccessUnit.
//
// First entrance to decoding core interface.
func WelsDecodeBs(pCtx *SWelsDecoderContext, kpBsBuf []uint8, kiBsLen int32, ppDst *[3][]uint8, pDstBufInfo *api.SBufferInfo, pDstBsInfo *api.SParserBsInfo) int32 {
	if !pCtx.bEndOfStreamFlag {
		pRawData := &pCtx.sRawData
		var pSavedData *SDataBuffer

		var iSrcIdx int32      //the index of source bit-stream till now after parsing one or more NALs
		var iSrcConsumed int32 // consumed bit count of source bs
		var iDstIdx int32      //the size of current NAL after 0x03 removal and 00 00 01 removal
		var iSrcLength int32   //the total size of current AU or NAL
		var iRet int32 = 0
		var iConsumedBytes int32 = 0
		var iOffset int32 = 0

		// pSrcNal: offset into kpBsBuf; pDstNal: offset into pRawData.pHead.
		var pSrcNal int
		var pDstNal int
		var pNalPayload []uint8
		var iNalPayloadOff int

		if nil == DetectStartCodePrefix(kpBsBuf, &iOffset,
			kiBsLen) { //CAN'T find the 00 00 01 start prefix from the source buffer
			pCtx.iErrorCode |= int32(api.DsBitstreamError)
			return int32(api.DsBitstreamError)
		}

		pSrcNal = int(iOffset)
		iSrcLength = kiBsLen - iOffset

		if int(kiBsLen+4) > (pRawData.pEnd - pRawData.pCurPos) {
			if !RawDataWrapIsClean(pCtx, kiBsLen+4) {
				pCtx.iErrorCode |= int32(api.DsOutOfMemory)
				return pCtx.iErrorCode
			}
			pRawData.pCurPos = 0 // pHead
		}

		if pCtx.pParam.BParseOnly {
			pSavedData = &pCtx.sSavedData
			if int(kiBsLen+4) > (pSavedData.pEnd - pSavedData.pCurPos) {
				pSavedData.pCurPos = 0 // pHead
			}
		}
		//copy raw data from source buffer (application) to raw data buffer (codec inside)
		//0x03 removal and extract all of NAL Unit from current raw data
		pDstNal = pRawData.pCurPos

		bNalStartBytes := false

		for iSrcConsumed < iSrcLength {
			pDst := pRawData.pHead
			if (2+iSrcConsumed < iSrcLength) && (0 == common.LD16(kpBsBuf, pSrcNal+int(iSrcIdx))) &&
				(kpBsBuf[pSrcNal+2+int(iSrcIdx)] <= 0x03) {
				b2 := kpBsBuf[pSrcNal+2+int(iSrcIdx)]
				if bNalStartBytes && (b2 != 0x00 && b2 != 0x01) {
					pCtx.iErrorCode |= int32(api.DsBitstreamError)
					return pCtx.iErrorCode
				}

				if b2 == 0x02 {
					pCtx.iErrorCode |= int32(api.DsBitstreamError)
					return pCtx.iErrorCode
				} else if b2 == 0x00 {
					pDst[pDstNal+int(iDstIdx)] = kpBsBuf[pSrcNal+int(iSrcIdx)]
					iDstIdx++
					iSrcIdx++
					iSrcConsumed++
					bNalStartBytes = true
				} else if b2 == 0x03 {
					if (3+iSrcConsumed < iSrcLength) && kpBsBuf[pSrcNal+3+int(iSrcIdx)] > 0x03 {
						/* Just skip */
					} else {
						common.ST16(pDst, pDstNal+int(iDstIdx), 0)
						iDstIdx += 2
					}
					iSrcIdx += 3
					iSrcConsumed += 3
				} else { // 0x01
					bNalStartBytes = false

					iConsumedBytes = 0
					d := pDstNal + int(iDstIdx)
					pDst[d], pDst[d+1], pDst[d+2], pDst[d+3] = 0, 0, 0, 0 // set 4 reserved bytes to zero
					pNalPayload, iNalPayloadOff = ParseNalHeader(pCtx, &pCtx.sCurNalHead, pRawData.pHead, pDstNal, iDstIdx,
						kpBsBuf[pSrcNal-3:], iSrcIdx+3, &iConsumedBytes)
					if pNalPayload != nil { //parse correct
						if common.IS_PARAM_SETS_NALS(pCtx.sCurNalHead.ENalUnitType) {
							iRet = ParseNonVclNal(pCtx, pNalPayload, iNalPayloadOff, iDstIdx-iConsumedBytes, kpBsBuf[pSrcNal-3:],
								iSrcIdx+3)
						}
						CheckAndFinishLastPic(pCtx, ppDst, pDstBufInfo)
						if pCtx.bAuReadyFlag && pCtx.pAccessUnitList.uiAvailUnitsNum != 0 {
							if GetThreadCount(pCtx) <= 1 {
								ConstructAccessUnit(pCtx, ppDst, pDstBufInfo)
							}
						}
					}
					DecodeFinishUpdate(pCtx)

					if (int32(api.DsOutOfMemory)|int32(api.DsNoParamSets))&pCtx.iErrorCode != 0 {
						// LONG_TERM_REF
						pCtx.bParamSetsLostFlag = true
						if int32(api.DsOutOfMemory)&pCtx.iErrorCode != 0 {
							return pCtx.iErrorCode
						}
					}
					if iRet != 0 {
						iRet = 0
						if int32(api.DsNoParamSets)&pCtx.iErrorCode != 0 {
							// LONG_TERM_REF
							pCtx.bParamSetsLostFlag = true
						}
						return pCtx.iErrorCode
					}

					if pCtx.iErrorCode != ERR_NONE && (pCtx.iErrorCode&int32(api.DsDataErrorConcealed)) == 0 {
						return pCtx.iErrorCode
					}

					pDstNal += int(iDstIdx + 4) //init, increase 4 reserved zero bytes, used to store the next NAL
					if int(iSrcLength-iSrcConsumed+4) > (pRawData.pEnd - pDstNal) {
						if !RawDataWrapIsClean(pCtx, iSrcLength-iSrcConsumed+4) {
							pCtx.iErrorCode |= int32(api.DsOutOfMemory)
							return pCtx.iErrorCode
						}
						pRawData.pCurPos = 0 // pHead
						pDstNal = 0
					} else {
						pRawData.pCurPos = pDstNal
					}

					pSrcNal += int(iSrcIdx + 3)
					iSrcConsumed += 3
					iSrcIdx = 0
					iDstIdx = 0 //reset 0, used to statistic the length of next NAL
				}
				continue
			}
			pDst[pDstNal+int(iDstIdx)] = kpBsBuf[pSrcNal+int(iSrcIdx)]
			iDstIdx++
			iSrcIdx++
			iSrcConsumed++
		}

		//last NAL decoding

		iConsumedBytes = 0
		{
			pDst := pRawData.pHead
			d := pDstNal + int(iDstIdx)
			pDst[d], pDst[d+1], pDst[d+2], pDst[d+3] = 0, 0, 0, 0 // set 4 reserved bytes to zero
		}
		pRawData.pCurPos = pDstNal + int(iDstIdx) + 4 //init, increase 4 reserved zero bytes, used to store the next NAL
		pNalPayload, iNalPayloadOff = ParseNalHeader(pCtx, &pCtx.sCurNalHead, pRawData.pHead, pDstNal, iDstIdx,
			kpBsBuf[pSrcNal-3:], iSrcIdx+3, &iConsumedBytes)
		if pNalPayload != nil { //parse correct
			if common.IS_PARAM_SETS_NALS(pCtx.sCurNalHead.ENalUnitType) {
				iRet = ParseNonVclNal(pCtx, pNalPayload, iNalPayloadOff, iDstIdx-iConsumedBytes, kpBsBuf[pSrcNal-3:], iSrcIdx+3)
			}
			if GetThreadCount(pCtx) <= 1 {
				CheckAndFinishLastPic(pCtx, ppDst, pDstBufInfo)
			}
			if pCtx.bAuReadyFlag && pCtx.pAccessUnitList.uiAvailUnitsNum != 0 {
				if GetThreadCount(pCtx) <= 1 {
					ConstructAccessUnit(pCtx, ppDst, pDstBufInfo)
				}
			}
		}
		DecodeFinishUpdate(pCtx)

		if (int32(api.DsOutOfMemory)|int32(api.DsNoParamSets))&pCtx.iErrorCode != 0 {
			// LONG_TERM_REF
			pCtx.bParamSetsLostFlag = true
			return pCtx.iErrorCode
		}
		if iRet != 0 {
			iRet = 0
			if int32(api.DsNoParamSets)&pCtx.iErrorCode != 0 {
				// LONG_TERM_REF
				pCtx.bParamSetsLostFlag = true
			}
			return pCtx.iErrorCode
		}
	} else { /* no supplementary picture payload input, but stored a picture */
		pCurAu := pCtx.pAccessUnitList // current access unit, it will never point to NULL after decode's successful initialization

		if pCurAu.uiAvailUnitsNum == 0 {
			return pCtx.iErrorCode
		} else {
			pCtx.pAccessUnitList.uiEndPos = pCtx.pAccessUnitList.uiAvailUnitsNum - 1

			ConstructAccessUnit(pCtx, ppDst, pDstBufInfo)
		}
		DecodeFinishUpdate(pCtx)

		if (int32(api.DsOutOfMemory)|int32(api.DsNoParamSets))&pCtx.iErrorCode != 0 {
			// LONG_TERM_REF
			pCtx.bParamSetsLostFlag = true
			return pCtx.iErrorCode
		}
	}

	return pCtx.iErrorCode
}

// int32_t SyncPictureResolutionExt (PWelsDecoderContext pCtx, const int32_t kiMbWidth, const int32_t
// kiMbHeight)
//
// make sure synchonozization picture resolution (get from slice header) among different parts (i.e,
// memory related and so on) over decoder internal
// ( MB coordinate and parts of data within decoder context structure )
// return 0 - successful; none 0 - something wrong
func SyncPictureResolutionExt(pCtx *SWelsDecoderContext, kiMbWidth int32, kiMbHeight int32) int32 {
	var iErr int32 = ERR_NONE
	kiPicWidth := kiMbWidth << 4
	kiPicHeight := kiMbHeight << 4
	//fix Bugzilla Bug1479656 reallocate temp dec picture
	if pCtx.pTempDec != nil && (pCtx.pTempDec.iWidthInPixel != kiPicWidth ||
		pCtx.pTempDec.iHeightInPixel != kiPicHeight) {
		FreePicture(pCtx.pTempDec)
		pCtx.pTempDec = AllocPicture(pCtx, int32(pCtx.pSps.iMbWidth<<4), int32(pCtx.pSps.iMbHeight<<4))
	}
	bReallocFlag := false
	iErr = WelsRequestMem(pCtx, kiMbWidth, kiMbHeight, &bReallocFlag) // common memory used
	if ERR_NONE != iErr {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
			"SyncPictureResolutionExt()::WelsRequestMem--buffer allocated failure.")
		pCtx.iErrorCode |= int32(api.DsOutOfMemory)
		return iErr
	}

	iErr = InitialDqLayersContext(pCtx, kiPicWidth, kiPicHeight)
	if ERR_NONE != iErr {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
			"SyncPictureResolutionExt()::InitialDqLayersContext--buffer allocated failure.")
		pCtx.iErrorCode |= int32(api.DsOutOfMemory)
	}
	return iErr
}

// void InitDecFuncs (PWelsDecoderContext pCtx, uint32_t uiCpuFlag)
func InitDecFuncs(pCtx *SWelsDecoderContext, uiCpuFlag uint32) {
	WelsBlockFuncInit(&pCtx.sBlockFunc, int32(uiCpuFlag))
	InitPredFunc(pCtx, uiCpuFlag)
	common.InitMcFunc(&pCtx.sMcFunc, uiCpuFlag)
	common.InitExpandPictureFunc(&pCtx.sExpandPicFunc, uiCpuFlag)
	DeblockingInit(&pCtx.sDeblockingFunc, int32(uiCpuFlag))
}

// template<void pfIdctResAddPred (uint8_t* pPred, int32_t iStride, int16_t* pRs)>
// void IdctFourResAddPred_ (uint8_t* pPred, int32_t iStride, int16_t* pRs, const int8_t* pNzc)
//
// (anonymous-namespace template; instantiated with IdctResAddPred_c.)
func IdctFourResAddPred_(pfIdctResAddPred PIdctResAddPredFunc) PIdctFourResAddPredFunc {
	return func(pPred []uint8, iPredOff int, iStride int32, pRs []int16, pNzc []int8) {
		s := int(iStride)
		if pNzc[0] != 0 || pRs[0*16] != 0 {
			pfIdctResAddPred(pPred, iPredOff+0*s+0, iStride, pRs[0*16:])
		}
		if pNzc[1] != 0 || pRs[1*16] != 0 {
			pfIdctResAddPred(pPred, iPredOff+0*s+4, iStride, pRs[1*16:])
		}
		if pNzc[4] != 0 || pRs[2*16] != 0 {
			pfIdctResAddPred(pPred, iPredOff+4*s+0, iStride, pRs[2*16:])
		}
		if pNzc[5] != 0 || pRs[3*16] != 0 {
			pfIdctResAddPred(pPred, iPredOff+4*s+4, iStride, pRs[3*16:])
		}
	}
}

// idctFourResAddPred_c is IdctFourResAddPred_<IdctResAddPred_c>.
var idctFourResAddPred_c = IdctFourResAddPred_(IdctResAddPred_c)

// void InitPredFunc (PWelsDecoderContext pCtx, uint32_t uiCpuFlag)
//
// IdctFourResAddPred_<IdctResAddPred_c> (anonymous-namespace template) becomes a file-local helper.
func InitPredFunc(pCtx *SWelsDecoderContext, uiCpuFlag uint32) {
	pCtx.pGetI16x16LumaPredFunc[common.I16_PRED_V] = WelsI16x16LumaPredV_c
	pCtx.pGetI16x16LumaPredFunc[common.I16_PRED_H] = WelsI16x16LumaPredH_c
	pCtx.pGetI16x16LumaPredFunc[common.I16_PRED_DC] = WelsI16x16LumaPredDc_c
	pCtx.pGetI16x16LumaPredFunc[common.I16_PRED_P] = WelsI16x16LumaPredPlane_c
	pCtx.pGetI16x16LumaPredFunc[common.I16_PRED_DC_L] = WelsI16x16LumaPredDcLeft_c
	pCtx.pGetI16x16LumaPredFunc[common.I16_PRED_DC_T] = WelsI16x16LumaPredDcTop_c
	pCtx.pGetI16x16LumaPredFunc[common.I16_PRED_DC_128] = WelsI16x16LumaPredDcNA_c

	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_V] = WelsI4x4LumaPredV_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_H] = WelsI4x4LumaPredH_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_DC] = WelsI4x4LumaPredDc_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_DC_L] = WelsI4x4LumaPredDcLeft_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_DC_T] = WelsI4x4LumaPredDcTop_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_DC_128] = WelsI4x4LumaPredDcNA_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_DDL] = WelsI4x4LumaPredDDL_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_DDL_TOP] = WelsI4x4LumaPredDDLTop_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_DDR] = WelsI4x4LumaPredDDR_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_VL] = WelsI4x4LumaPredVL_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_VL_TOP] = WelsI4x4LumaPredVLTop_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_VR] = WelsI4x4LumaPredVR_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_HU] = WelsI4x4LumaPredHU_c
	pCtx.pGetI4x4LumaPredFunc[common.I4_PRED_HD] = WelsI4x4LumaPredHD_c

	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_V] = WelsI8x8LumaPredV_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_H] = WelsI8x8LumaPredH_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_DC] = WelsI8x8LumaPredDc_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_DC_L] = WelsI8x8LumaPredDcLeft_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_DC_T] = WelsI8x8LumaPredDcTop_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_DC_128] = WelsI8x8LumaPredDcNA_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_DDL] = WelsI8x8LumaPredDDL_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_DDL_TOP] = WelsI8x8LumaPredDDLTop_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_DDR] = WelsI8x8LumaPredDDR_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_VL] = WelsI8x8LumaPredVL_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_VL_TOP] = WelsI8x8LumaPredVLTop_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_VR] = WelsI8x8LumaPredVR_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_HU] = WelsI8x8LumaPredHU_c
	pCtx.pGetI8x8LumaPredFunc[common.I4_PRED_HD] = WelsI8x8LumaPredHD_c

	pCtx.pGetIChromaPredFunc[common.C_PRED_DC] = WelsIChromaPredDc_c
	pCtx.pGetIChromaPredFunc[common.C_PRED_H] = WelsIChromaPredH_c
	pCtx.pGetIChromaPredFunc[common.C_PRED_V] = WelsIChromaPredV_c
	pCtx.pGetIChromaPredFunc[common.C_PRED_P] = WelsIChromaPredPlane_c
	pCtx.pGetIChromaPredFunc[common.C_PRED_DC_L] = WelsIChromaPredDcLeft_c
	pCtx.pGetIChromaPredFunc[common.C_PRED_DC_T] = WelsIChromaPredDcTop_c
	pCtx.pGetIChromaPredFunc[common.C_PRED_DC_128] = WelsIChromaPredDcNA_c

	pCtx.pIdctResAddPredFunc = IdctResAddPred_c
	pCtx.pIdctFourResAddPredFunc = idctFourResAddPred_c

	pCtx.pIdctResAddPredFunc8x8 = IdctResAddPred8x8_c

	// SIMD overrides (NEON / AArch64 / X86 / MMI / LSX) are not ported.
}

// void ResetDecStatNums (SDecoderStatistics* pDecStat)
//
// reset decoder number related statistics info
func ResetDecStatNums(pDecStat *api.SDecoderStatistics) {
	uiWidth := pDecStat.UiWidth
	uiHeight := pDecStat.UiHeight
	iAvgLumaQp := pDecStat.IAvgLumaQp
	iLogInterval := pDecStat.IStatisticsLogInterval
	uiProfile := pDecStat.UiProfile
	uiLevel := pDecStat.UiLevel
	*pDecStat = api.SDecoderStatistics{}
	pDecStat.UiWidth = uiWidth
	pDecStat.UiHeight = uiHeight
	pDecStat.IAvgLumaQp = iAvgLumaQp
	pDecStat.IStatisticsLogInterval = iLogInterval
	pDecStat.UiProfile = uiProfile
	pDecStat.UiLevel = uiLevel
}

// void UpdateDecStatFreezingInfo (const bool kbIdrFlag, SDecoderStatistics* pDecStat)
//
// update information when freezing occurs, including IDR/non-IDR number
func UpdateDecStatFreezingInfo(kbIdrFlag bool, pDecStat *api.SDecoderStatistics) {
	if kbIdrFlag {
		pDecStat.UiFreezingIDRNum++
	} else {
		pDecStat.UiFreezingNonIDRNum++
	}
}

// void UpdateDecStatNoFreezingInfo (PWelsDecoderContext pCtx)
//
// update information when no freezing occurs, including QP, correct IDR number, ECed IDR number
func UpdateDecStatNoFreezingInfo(pCtx *SWelsDecoderContext) {
	pCurDq := pCtx.pCurDqLayer
	pPic := pCtx.pDec
	pDecStat := pCtx.pDecoderStatistics

	if pDecStat.IAvgLumaQp == -1 { //first correct frame received
		pDecStat.IAvgLumaQp = 0
	}

	//update QP info
	var iTotalQp int32
	kiMbNum := pCurDq.iMbWidth * pCurDq.iMbHeight
	if pCtx.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE { //all correct
		for iMb := int32(0); iMb < kiMbNum; iMb++ {
			iTotalQp += int32(pCurDq.pLumaQp[iMb])
		}
		iTotalQp /= kiMbNum
	} else {
		var iCorrectMbNum int32
		for iMb := int32(0); iMb < kiMbNum; iMb++ {
			if pCurDq.pMbCorrectlyDecodedFlag[iMb] {
				iCorrectMbNum++
				iTotalQp += int32(pCurDq.pLumaQp[iMb])
			}
		}
		if iCorrectMbNum == 0 { //non MB is correct, should remain QP statistic info
			iTotalQp = pDecStat.IAvgLumaQp
		} else {
			iTotalQp /= iCorrectMbNum
		}
	}
	if pDecStat.UiDecodedFrameCount+1 == 0 { //maximum uint32_t reached
		ResetDecStatNums(pDecStat)
		pDecStat.IAvgLumaQp = iTotalQp
	} else {
		// C: (int) ((uint64_t) (iAvgLumaQp * uiDecodedFrameCount + iTotalQp) / (uiDecodedFrameCount + 1))
		// where the inner arithmetic is unsigned 32-bit.
		pDecStat.IAvgLumaQp = int32(uint64(uint32(pDecStat.IAvgLumaQp)*pDecStat.UiDecodedFrameCount+uint32(iTotalQp)) /
			uint64(pDecStat.UiDecodedFrameCount+1))
	}

	//update IDR number
	if pCurDq.sLayerInfo.sNalHeaderExt.BIdrFlag {
		if pPic.bIsComplete {
			pDecStat.UiIDRCorrectNum++
		}
		if pCtx.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE {
			if !pPic.bIsComplete {
				pDecStat.UiEcIDRNum++
			}
		}
	}
}

// void UpdateDecStat (PWelsDecoderContext pCtx, const bool kbOutput)
//
// update decoder statistics information
func UpdateDecStat(pCtx *SWelsDecoderContext, kbOutput bool) {
	if pCtx.bFreezeOutput {
		UpdateDecStatFreezingInfo(pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt.BIdrFlag, pCtx.pDecoderStatistics)
	} else if kbOutput {
		UpdateDecStatNoFreezingInfo(pCtx)
	}
}
