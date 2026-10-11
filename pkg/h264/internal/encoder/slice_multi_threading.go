// Port of codec/encoder/core/src/slice_multi_threading.cpp.
//
// Slice-based worker support. Thread handles, events, and mutexes are dropped;
// the task manager assigns each bounded Go worker a private bitstream buffer.

package encoder

import (
	"math"
	"runtime"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// UpdateMbListNeighborParallel: pMbList is the layer's MB list (SDqLayer.sMbDataP).
func UpdateMbListNeighborParallel(pCurDq *SDqLayer, pMbList []SMB, uiSliceIdc int32) {
	pSliceCtx := &pCurDq.sSliceEncCtx
	kiMbWidth := int32(pSliceCtx.iMbWidth)
	iIdx := pCurDq.pFirstMbIdxOfSlice[uiSliceIdc]
	kiEndMbInSlice := iIdx + pCurDq.pCountMbNumInSlice[uiSliceIdc] - 1

	for {
		UpdateMbNeighbor(pCurDq, &pMbList[iIdx], kiMbWidth, uint16(uiSliceIdc))
		iIdx++
		if !(iIdx <= kiEndMbInSlice) {
			break
		}
	}
}

func CalcSliceComplexRatio(pCurDq *SDqLayer) {
	pSliceCtx := &pCurDq.sSliceEncCtx
	ppSliceInLayer := pCurDq.ppSliceInLayer
	iSumAv := int32(0)
	kiSliceCount := pSliceCtx.iSliceNumInFrame
	iSliceIdx := int32(0)
	var iAvI [MAX_SLICES_NUM]int32

	// assert (kiSliceCount <= MAX_SLICES_NUM)

	for iSliceIdx < kiSliceCount {
		// C: (int) * (uint32_t) operands are converted to unsigned
		iAvI[iSliceIdx] = common.WELS_DIV_ROUND(uint32(INT_MULTIPLY*ppSliceInLayer[iSliceIdx].iCountMbNumInSlice),
			ppSliceInLayer[iSliceIdx].uiSliceConsumeTime)
		iSumAv += iAvI[iSliceIdx]

		iSliceIdx++
	}
	for {
		iSliceIdx--
		if !(iSliceIdx >= 0) {
			break
		}
		ppSliceInLayer[iSliceIdx].iSliceComplexRatio = common.WELS_DIV_ROUND(INT_MULTIPLY*iAvI[iSliceIdx], iSumAv)
	}
}

// NeedDynamicAdjust: SSlice** ppSliceInLayer -> []*SSlice.
func NeedDynamicAdjust(ppSliceInLayer []*SSlice, iSliceNum int32) int32 {
	if nil == ppSliceInLayer {
		return 0
	}

	uiTotalConsume := uint32(0)
	iSliceIdx := int32(0)
	iNeedAdj := int32(0)

	for iSliceIdx < iSliceNum {
		if nil == ppSliceInLayer[iSliceIdx] {
			return 0
		}

		uiTotalConsume += ppSliceInLayer[iSliceIdx].uiSliceConsumeTime
		iSliceIdx++
	}
	if uiTotalConsume == 0 {
		return 0
	}

	iSliceIdx = 0
	fThr := common.EPSN                     // threshold for various cores cases
	fRmse := float32(0)                     // root mean square error of pSlice consume ratios
	kfMeanRatio := 1.0 / float32(iSliceNum) // C: 1.0f / iSliceNum
	for {
		fRatio := float32(ppSliceInLayer[iSliceIdx].uiSliceConsumeTime) / float32(uiTotalConsume)
		fDiffRatio := fRatio - kfMeanRatio
		fRmse += float32(fDiffRatio * fDiffRatio)
		iSliceIdx++
		if !(iSliceIdx+1 < iSliceNum) {
			break
		}
	}
	fRmse = float32(math.Sqrt(float64(fRmse / float32(iSliceNum))))
	if iSliceNum >= 8 {
		fThr += THRESHOLD_RMSE_CORE8
	} else if iSliceNum >= 4 {
		fThr += THRESHOLD_RMSE_CORE4
	} else if iSliceNum >= 2 {
		fThr += THRESHOLD_RMSE_CORE2
	} else {
		fThr = 1.0
	}
	if fRmse > fThr {
		iNeedAdj = 1
	}

	return iNeedAdj
}

func DynamicAdjustSlicing(pCtx *sWelsEncCtx, pCurDqLayer *SDqLayer, iCurDid int32) {
	pSliceCtx := &pCurDqLayer.sSliceEncCtx
	ppSliceInLayer := pCurDqLayer.ppSliceInLayer
	kiCountSliceNum := pSliceCtx.iSliceNumInFrame
	kiCountNumMb := pSliceCtx.iMbNumInFrame
	iMinimalMbNum := int32(pSliceCtx.iMbWidth) // in theory we need only 1 SMB, here let it as one SMB row required
	iMaximalMbNum := int32(0)                  // dynamically assign later
	iMbNumLeft := kiCountNumMb
	var iRunLen [MAX_THREADS_NUM]int32
	iSliceIdx := int32(0)

	iNumMbInEachGom := int32(0)
	pWelsSvcRc := &pCtx.pWelsSvcRc[iCurDid]
	if pCtx.pSvcParam.IRCMode != api.RC_OFF_MODE {
		iNumMbInEachGom = pWelsSvcRc.iNumberMbGom

		if iNumMbInEachGom <= 0 {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
				"[MT] DynamicAdjustSlicing(), invalid iNumMbInEachGom= %d from RC, iDid= %d, iCountNumMb= %d", iNumMbInEachGom,
				iCurDid, kiCountNumMb)
			return
		}

		// do not adjust in case no extra iNumMbInEachGom based left for slicing adjustment,
		// extra MB of non integrated GOM assigned at the last pSlice in default, keep up on early initial result.
		if iNumMbInEachGom*kiCountSliceNum >= kiCountNumMb {
			return
		}
		iMinimalMbNum = iNumMbInEachGom
	} else {
		// If the number of requested slices equals or exceeds the total macroblocks
		// in the frame, each slice cannot be assigned even 1 macroblock. In this
		// case, do not adjust slice sizes.
		if kiCountSliceNum >= kiCountNumMb {
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_WARNING,
				"[MT] DynamicAdjustSlicing(), requested slice number (%d) equals "+
					"or exceeds total macroblocks (%d), do not adjust",
				kiCountSliceNum, kiCountNumMb)
			return
		} else if iMinimalMbNum*kiCountSliceNum >= kiCountNumMb {
			// When rate control is off, iMinimalMbNum defaults to 1 macroblock row
			// (iMbWidth). For extreme aspect ratios (such as very wide resolutions),
			// requiring 1 row per slice may exceed total frame capacity. Fall back to
			// 1 macroblock per slice to ensure valid upper bounds and allow proper
			// load balancing across slices.
			iMinimalMbNum = 1
		}
	}

	if kiCountSliceNum < 2 || (kiCountSliceNum&0x01) != 0 { // we need suppose uiSliceNum is even for multiple threading
		return
	}

	iMaximalMbNum = kiCountNumMb - (kiCountSliceNum-1)*iMinimalMbNum

	iSliceIdx = 0
	for iSliceIdx+1 < kiCountSliceNum {
		iNumMbAssigning := common.WELS_DIV_ROUND(kiCountNumMb*ppSliceInLayer[iSliceIdx].iSliceComplexRatio, int32(INT_MULTIPLY))

		// GOM boundary aligned
		if pCtx.pSvcParam.IRCMode != api.RC_OFF_MODE {
			iNumMbAssigning = iNumMbAssigning / iNumMbInEachGom * iNumMbInEachGom
		}

		// make sure one GOM at least in each pSlice for safe
		if iNumMbAssigning < iMinimalMbNum {
			iNumMbAssigning = iMinimalMbNum
		} else if iNumMbAssigning > iMaximalMbNum {
			iNumMbAssigning = iMaximalMbNum
		}

		// assert (iNumMbAssigning > 0)

		iMbNumLeft -= iNumMbAssigning
		if iMbNumLeft <= 0 { // error due to we can not support slice_skip now yet, do not adjust this time
			// assert (0)
			return
		}
		iRunLen[iSliceIdx] = iNumMbAssigning
		iSliceIdx++
		iMaximalMbNum = iMbNumLeft - (kiCountSliceNum-iSliceIdx-1)*iMinimalMbNum // get maximal num_mb in left parts
	}
	iRunLen[iSliceIdx] = iMbNumLeft
	pCurDqLayer.bNeedAdjustingSlicing = DynamicAdjustSlicePEncCtxAll(pCurDqLayer, iRunLen[:]) == 0
}

func RequestMtResource(ppCtx **sWelsEncCtx, pCodingParam *SWelsSvcCodingParam, iCountBsLen int32, iMaxSliceBufferSize int32, bDynamicSlice bool) int32 {
	var pPara *SWelsSvcCodingParam
	var pSmt *SSliceThreading
	iNumSpatialLayers := int32(0)
	iThreadNum := int32(0)
	iIdx := int32(0)

	if nil == ppCtx || nil == pCodingParam || nil == *ppCtx || iCountBsLen <= 0 {
		return 1
	}
	pPara = pCodingParam
	iNumSpatialLayers = pPara.ISpatialLayerNum
	iThreadNum = int32(pPara.IMultipleThreadIdc)

	// assert (iThreadNum > 0)

	pSmt = new(SSliceThreading)
	(*ppCtx).pSliceThreading = pSmt
	pSmt.pThreadPEncCtx = make([]SSliceThreadPrivateData, iThreadNum)

	iIdx = 0
	for iIdx < iThreadNum {
		pSmt.pThreadPEncCtx[iIdx].pWelsPEncCtx = *ppCtx
		pSmt.pThreadPEncCtx[iIdx].iSliceIndex = iIdx
		pSmt.pThreadPEncCtx[iIdx].iThreadIndex = iIdx
		// Persistent thread handles and events are unnecessary for frame-scoped Go workers.
		iIdx++
	}

	(*ppCtx).pTaskManage = CreateTaskManage(*ppCtx, iNumSpatialLayers, bDynamicSlice)
	if nil == (*ppCtx).pTaskManage {
		return 1
	}

	iThreadBufferNum := common.WELS_MIN((*ppCtx).pTaskManage.GetThreadPoolThreadNum(), int32(MAX_THREADS_NUM))

	for iIdx = 0; iIdx < iThreadBufferNum; iIdx++ {
		pSmt.pThreadBsBuffer[iIdx] = make([]uint8, iCountBsLen)
	}

	return 0
}

func ReleaseMtResource(ppCtx **sWelsEncCtx) {
	var pSmt *SSliceThreading

	if nil == ppCtx || nil == *ppCtx {
		return
	}

	pSmt = (*ppCtx).pSliceThreading

	if nil == pSmt {
		return
	}

	// Frame-scoped workers have joined before these resources are released.
	pSmt.pThreadPEncCtx = nil

	for i := 0; i < MAX_THREADS_NUM; i++ {
		pSmt.pThreadBsBuffer[i] = nil
	}
	pSmt.bThreadBsBufferUsage = [MAX_THREADS_NUM]bool{}

	if (*ppCtx).pTaskManage != nil {
		(*ppCtx).pTaskManage.Destruct()
		(*ppCtx).pTaskManage = nil
	}

	(*ppCtx).pSliceThreading = nil
}

func AppendSliceToFrameBs(pCtx *sWelsEncCtx, pLbi *api.SLayerBSInfo, iSliceCount int32) int32 {
	ppSliceInlayer := pCtx.pCurDqLayer.ppSliceInLayer
	var pSliceBs *SWelsSliceBs
	iLayerSize := int32(0)
	iNalIdxBase := int32(0)
	iSliceIdx := int32(0)

	pLbi.INalCount = 0
	iNalIdxBase = 0
	for iSliceIdx < iSliceCount {
		pSliceBs = &ppSliceInlayer[iSliceIdx].sSliceBs
		if pSliceBs.uiBsPos > 0 {
			iNalIdx := int32(0)
			iCountNal := pSliceBs.iNalIndex

			if uint64(pCtx.iPosBsBuffer)+uint64(pSliceBs.uiBsPos) > uint64(pCtx.iFrameBsSize) {
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
					"AppendSliceToFrameBs(), insufficient memory for the allocation! "+
						"iPosBsBuffer:%d, uiBsPos:%d, iFrameBsSize:%d",
					pCtx.iPosBsBuffer, pSliceBs.uiBsPos, pCtx.iFrameBsSize)
				pCtx.iEncoderError |= ENC_RETURN_MEMALLOCERR
				return 0
			}

			copy(pCtx.pFrameBs[pCtx.iPosBsBuffer:pCtx.iPosBsBuffer+int32(pSliceBs.uiBsPos)], pSliceBs.pBs[:pSliceBs.uiBsPos]) // confirmed_safe_unsafe_usage
			pCtx.iPosBsBuffer += int32(pSliceBs.uiBsPos)

			iLayerSize += int32(pSliceBs.uiBsPos)

			for iNalIdx < iCountNal {
				pLbi.PNalLengthInByte[iNalIdxBase+iNalIdx] = pSliceBs.iNalLen[iNalIdx]
				iNalIdx++
			}
			pLbi.INalCount += iCountNal
			iNalIdxBase += iCountNal
		}
		iSliceIdx++
	}

	return iLayerSize
}

// WriteSliceBs: int32_t& iSliceSize -> *int32.
func WriteSliceBs(pCtx *sWelsEncCtx, pSliceBs *SWelsSliceBs, iSliceIdx int32, iSliceSize *int32) int32 {
	kiNalCnt := pSliceBs.iNalIndex
	iNalIdx := int32(0)
	iNalSize := int32(0)
	iReturn := int32(ENC_RETURN_SUCCESS)
	iTotalLeftLength := int32(pSliceBs.uiBsSize - pSliceBs.uiBsPos)
	pNalHdrExt := &pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt
	pDst := pSliceBs.pBs
	iDstOff := 0

	// assert (kiNalCnt <= 2)
	if kiNalCnt > 2 {
		return 0
	}

	*iSliceSize = 0
	for iNalIdx < kiNalCnt {
		iNalSize = 0
		iReturn = WelsEncodeNal(&pSliceBs.sNalList[iNalIdx], pNalHdrExt, iTotalLeftLength-*iSliceSize,
			pDst[iDstOff:], &iNalSize)
		if iReturn != ENC_RETURN_SUCCESS {
			return iReturn
		}

		pSliceBs.iNalLen[iNalIdx] = iNalSize
		*iSliceSize += iNalSize
		iDstOff += int(iNalSize)
		iNalIdx++
	}
	pSliceBs.uiBsPos = uint32(*iSliceSize)

	return iReturn
}

// DynamicDetectCpuCores returns the number of logical processors
// (WelsQueryLogicalProcessInfo).
func DynamicDetectCpuCores() int32 {
	return int32(runtime.NumCPU())
}

func AdjustBaseLayer(pCtx *sWelsEncCtx) int32 {
	pCurDq := pCtx.ppDqLayerList[0]
	iNeedAdj := int32(1)

	pCtx.pCurDqLayer = pCurDq

	// do not need adjust due to not different at both slices of consumed time
	iNeedAdj = NeedDynamicAdjust(pCtx.ppDqLayerList[0].ppSliceInLayer, pCurDq.sSliceEncCtx.iSliceNumInFrame)
	if iNeedAdj != 0 {
		DynamicAdjustSlicing(pCtx,
			pCurDq,
			0)
	}

	return iNeedAdj
}

func AdjustEnhanceLayer(pCtx *sWelsEncCtx, iCurDid int32) int32 {
	iNeedAdj := int32(1)
	// uiSliceMode of referencing spatial should be SM_FIXEDSLCNUM_SLICE
	// if using spatial base layer for complexity estimation

	kbModelingFromSpatial := (pCtx.pCurDqLayer.pRefLayer != nil && iCurDid > 0) &&
		(pCtx.pSvcParam.SSpatialLayers[iCurDid-1].SSliceArgument.UiSliceMode == api.SM_FIXEDSLCNUM_SLICE &&
			uint32(pCtx.pSvcParam.IMultipleThreadIdc) >= pCtx.pSvcParam.SSpatialLayers[iCurDid-1].SSliceArgument.UiSliceNum)

	if kbModelingFromSpatial { // using spatial base layer for complexity estimation
		// do not need adjust due to not different at both slices of consumed time
		iNeedAdj = NeedDynamicAdjust(pCtx.ppDqLayerList[iCurDid-1].ppSliceInLayer,
			pCtx.pCurDqLayer.sSliceEncCtx.iSliceNumInFrame)
		if iNeedAdj != 0 {
			DynamicAdjustSlicing(pCtx,
				pCtx.pCurDqLayer,
				iCurDid)
		}
	} else { // use temporal layer for complexity estimation
		// do not need adjust due to not different at both slices of consumed time
		iNeedAdj = NeedDynamicAdjust(pCtx.ppDqLayerList[iCurDid].ppSliceInLayer,
			pCtx.pCurDqLayer.sSliceEncCtx.iSliceNumInFrame)
		if iNeedAdj != 0 {
			DynamicAdjustSlicing(pCtx,
				pCtx.pCurDqLayer,
				iCurDid)
		}
	}

	return iNeedAdj
}

func SetOneSliceBsBufferUnderMultithread(pCtx *sWelsEncCtx, kiThreadIdx int32, pSlice *SSlice) {
	pSliceBs := &pSlice.sSliceBs
	pSliceBs.pBsBuffer = pCtx.pSliceThreading.pThreadBsBuffer[kiThreadIdx]
	pSliceBs.uiBsPos = 0
}
