// Port of codec/encoder/core/src/wels_task_encoder.cpp.
//
// Fixed-slice tasks use stable worker indexes so every worker owns one private
// bitstream buffer without a lock.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// ctorCWelsSliceEncodingTask is the constructor body
// CWelsSliceEncodingTask::CWelsSliceEncodingTask (pSink, pCtx, iSliceIdx) : CWelsBaseTask (pSink), m_eTaskResult (ENC_RETURN_SUCCESS)
func (p *CWelsSliceEncodingTask) ctorCWelsSliceEncodingTask(pSink IWelsTaskSink, pCtx *sWelsEncCtx, iSliceIdx int32) {
	p.ctorCWelsBaseTask(pSink)
	p.m_eTaskResult = ENC_RETURN_SUCCESS
	p.m_pCtx = pCtx
	p.m_iSliceIdx = iSliceIdx
	p.m_iThreadIdx = -1
}

// Destruct is the destructor CWelsSliceEncodingTask::~CWelsSliceEncodingTask (empty).
func (p *CWelsSliceEncodingTask) Destruct() {
}

// Execute dispatches InitTask / ExecuteTask / FinishTask through p.pVirt.
func (p *CWelsSliceEncodingTask) Execute() WelsErrorType {
	p.m_eTaskResult = p.pVirt.InitTask()
	if p.m_eTaskResult != ENC_RETURN_SUCCESS {
		return p.m_eTaskResult
	}

	p.m_eTaskResult = p.pVirt.ExecuteTask()

	p.pVirt.FinishTask()

	return p.m_eTaskResult
}

func (p *CWelsSliceEncodingTask) SetBoundary(iStartIdx int32, iEndIdx int32) WelsErrorType {
	p.m_iStartMbIdx = iStartIdx
	p.m_iEndMbIdx = iEndIdx
	return ENC_RETURN_SUCCESS
}

// QueryEmptyThread: bool* pThreadBsBufferUsage (MAX_THREADS_NUM entries) -> []bool.
func (p *CWelsSliceEncodingTask) QueryEmptyThread(pThreadBsBufferUsage []bool) int32 {
	for k := int32(0); k < MAX_THREADS_NUM; k++ {
		if pThreadBsBufferUsage[k] == false {
			pThreadBsBufferUsage[k] = true
			return k
		}
	}
	return -1
}

func (p *CWelsSliceEncodingTask) InitTask() WelsErrorType {
	p.m_eNalType = p.m_pCtx.eNalType
	p.m_eNalRefIdc = p.m_pCtx.eNalPriority
	p.m_bNeedPrefix = p.m_pCtx.bNeedPrefixNalFlag

	if !p.m_bThreadIndexAssigned {
		p.m_iThreadIdx = p.QueryEmptyThread(p.m_pCtx.pSliceThreading.bThreadBsBufferUsage[:])
	}

	if p.m_iThreadIdx < 0 || p.m_iThreadIdx >= MAX_THREADS_NUM ||
		p.m_pCtx.pSliceThreading.pThreadBsBuffer[p.m_iThreadIdx] == nil {
		common.WelsLog(&p.m_pCtx.sLogCtx, api.WELS_LOG_WARNING,
			"[MT] CWelsSliceEncodingTask InitTask(), Cannot find available thread for m_iSliceIdx = %d", p.m_iSliceIdx)
		p.releaseThreadBuffer()
		return ENC_RETURN_UNEXPECTED
	}

	iReturn := InitOneSliceInThread(p.m_pCtx, &p.m_pSlice, p.m_iThreadIdx, int32(p.m_pCtx.uiDependencyId), p.m_iSliceIdx)
	if iReturn != ENC_RETURN_SUCCESS {
		p.releaseThreadBuffer()
		return iReturn
	}
	p.m_pSliceBs = &p.m_pSlice.sSliceBs

	iReturn = SetSliceBoundaryInfo(p.m_pCtx.pCurDqLayer, p.m_pSlice, p.m_iSliceIdx)
	if iReturn != ENC_RETURN_SUCCESS {
		p.releaseThreadBuffer()
		return iReturn
	}

	SetOneSliceBsBufferUnderMultithread(p.m_pCtx, p.m_iThreadIdx, p.m_pSlice)

	// assert ((void*) (&m_pSliceBs->sBsWrite) == (void*)m_pSlice->pSliceBsa)
	common.InitBits(&p.m_pSliceBs.sBsWrite, p.m_pSliceBs.pBsBuffer, 0, int32(p.m_pSliceBs.uiSize))

	return ENC_RETURN_SUCCESS
}

func (p *CWelsSliceEncodingTask) releaseThreadBuffer() {
	if p.m_bThreadIndexAssigned || p.m_iThreadIdx < 0 || p.m_iThreadIdx >= MAX_THREADS_NUM {
		return
	}
	p.m_pCtx.pSliceThreading.bThreadBsBufferUsage[p.m_iThreadIdx] = false
	p.m_iThreadIdx = -1
}

func (p *CWelsSliceEncodingTask) FinishTask() {
	p.releaseThreadBuffer()
}

// writePrefixNal writes the prefix NAL (if needed) into the task's slice bs.
func (p *CWelsSliceEncodingTask) writePrefixNal() {
	if p.m_bNeedPrefix {
		if p.m_eNalRefIdc != common.NRI_PRI_LOWEST {
			WelsLoadNalForSlice(p.m_pSliceBs, common.NAL_UNIT_PREFIX, int32(p.m_eNalRefIdc))
			WelsWriteSVCPrefixNal(&p.m_pSliceBs.sBsWrite, int32(p.m_eNalRefIdc), (common.NAL_UNIT_CODED_SLICE_IDR == p.m_eNalType))
			WelsUnloadNalForSlice(p.m_pSliceBs)
		} else { // No Prefix NAL Unit RBSP syntax here, but need add NAL Unit Header extension
			WelsLoadNalForSlice(p.m_pSliceBs, common.NAL_UNIT_PREFIX, int32(p.m_eNalRefIdc))
			// No need write any syntax of prefix NAL Unit RBSP here
			WelsUnloadNalForSlice(p.m_pSliceBs)
		}
	}
}

func (p *CWelsSliceEncodingTask) ExecuteTask() WelsErrorType {
	pParamInternal := &p.m_pCtx.pSvcParam.sDependencyLayers[p.m_pCtx.uiDependencyId]
	p.writePrefixNal()

	WelsLoadNalForSlice(p.m_pSliceBs, int32(p.m_eNalType), int32(p.m_eNalRefIdc))
	// assert (m_iSliceIdx == (int) m_pSlice->iSliceIdx)
	iReturn := WelsCodeOneSlice(p.m_pCtx, p.m_pSlice, int32(p.m_eNalType))
	if ENC_RETURN_SUCCESS != iReturn {
		return iReturn
	}
	WelsUnloadNalForSlice(p.m_pSliceBs)

	p.m_iSliceSize = 0
	iReturn = WriteSliceBs(p.m_pCtx, p.m_pSliceBs, p.m_iSliceIdx, &p.m_iSliceSize)
	if ENC_RETURN_SUCCESS != iReturn {
		common.WelsLog(&p.m_pCtx.sLogCtx, api.WELS_LOG_WARNING,
			"[MT] CWelsSliceEncodingTask ExecuteTask(), WriteSliceBs not successful: coding_idx %d, um_iSliceIdx %d",
			pParamInternal.iCodingIndex,
			p.m_iSliceIdx)
		return iReturn
	}

	p.m_pCtx.pFuncList.pfDeblocking.pfDeblockingFilterSlice(p.m_pCtx.pCurDqLayer, p.m_pCtx.pFuncList, p.m_pSlice)

	return ENC_RETURN_SUCCESS
}

// CWelsLoadBalancingSlicingEncodingTask

func (p *CWelsLoadBalancingSlicingEncodingTask) InitTask() WelsErrorType {
	iReturn := p.CWelsSliceEncodingTask.InitTask()
	if ENC_RETURN_SUCCESS != iReturn {
		return iReturn
	}

	p.m_iSliceStart = common.WelsTime()

	return ENC_RETURN_SUCCESS
}

func (p *CWelsLoadBalancingSlicingEncodingTask) FinishTask() {
	p.CWelsSliceEncodingTask.FinishTask()
	p.m_pSlice.uiSliceConsumeTime = uint32(common.WelsTime() - p.m_iSliceStart)
}

// CWelsConstrainedSizeSlicingEncodingTask

func (p *CWelsConstrainedSizeSlicingEncodingTask) ExecuteTask() WelsErrorType {
	pCurDq := p.m_pCtx.pCurDqLayer
	kiSliceIdxStep := int32(p.m_pCtx.iActiveThreadsNum)
	pParamInternal := &p.m_pCtx.pSvcParam.sDependencyLayers[p.m_pCtx.uiDependencyId]
	kiPartitionId := p.m_iSliceIdx % kiSliceIdxStep
	kiFirstMbInPartition := pCurDq.FirstMbIdxOfPartition[kiPartitionId]
	kiEndMbIdxInPartition := pCurDq.EndMbIdxOfPartition[kiPartitionId]
	kiCodedSliceNumByThread := pCurDq.sSliceBufferInfo[p.m_iThreadIdx].iCodedSliceNum
	p.m_pSlice = &pCurDq.sSliceBufferInfo[p.m_iThreadIdx].pSliceBuffer[kiCodedSliceNumByThread]
	p.m_pSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice = kiFirstMbInPartition
	iReturn := int32(0)
	bNeedReallocate := false

	iDiffMbIdx := kiEndMbIdxInPartition - kiFirstMbInPartition
	if 0 == iDiffMbIdx {
		p.m_pSlice.iSliceIdx = -1
		return ENC_RETURN_SUCCESS
	}

	iAnyMbLeftInPartition := iDiffMbIdx + 1
	iLocalSliceIdx := p.m_iSliceIdx
	for iAnyMbLeftInPartition > 0 {
		bNeedReallocate = pCurDq.sSliceBufferInfo[p.m_iThreadIdx].iCodedSliceNum >=
			pCurDq.sSliceBufferInfo[p.m_iThreadIdx].iMaxSliceNum-1
		if bNeedReallocate {
			//for memory statistic variable
			iReturn = ReallocateSliceInThread(p.m_pCtx, pCurDq, int32(p.m_pCtx.uiDependencyId), p.m_iThreadIdx)
			if ENC_RETURN_SUCCESS != iReturn {
				return iReturn
			}
		}

		iReturn = InitOneSliceInThread(p.m_pCtx, &p.m_pSlice, p.m_iThreadIdx, int32(p.m_pCtx.uiDependencyId), iLocalSliceIdx)
		if iReturn != ENC_RETURN_SUCCESS {
			return iReturn
		}
		p.m_pSliceBs = &p.m_pSlice.sSliceBs
		common.InitBits(&p.m_pSliceBs.sBsWrite, p.m_pSliceBs.pBsBuffer, 0, int32(p.m_pSliceBs.uiSize))

		p.writePrefixNal()

		WelsLoadNalForSlice(p.m_pSliceBs, int32(p.m_eNalType), int32(p.m_eNalRefIdc))

		// assert (iLocalSliceIdx == (int) m_pSlice->iSliceIdx)
		iReturn := WelsCodeOneSlice(p.m_pCtx, p.m_pSlice, int32(p.m_eNalType))
		if ENC_RETURN_SUCCESS != iReturn {
			return iReturn
		}
		WelsUnloadNalForSlice(p.m_pSliceBs)

		iReturn = WriteSliceBs(p.m_pCtx, p.m_pSliceBs, iLocalSliceIdx, &p.m_iSliceSize)
		if ENC_RETURN_SUCCESS != iReturn {
			common.WelsLog(&p.m_pCtx.sLogCtx, api.WELS_LOG_WARNING,
				"[MT] CWelsConstrainedSizeSlicingEncodingTask ExecuteTask(), WriteSliceBs not successful: coding_idx %d, uiLocalSliceIdx %d, BufferSize %d, m_iSliceSize %d, iPayloadSize %d",
				pParamInternal.iCodingIndex,
				iLocalSliceIdx, p.m_pSliceBs.uiSize, p.m_iSliceSize, p.m_pSliceBs.sNalList[0].iPayloadSize)
			return iReturn
		}
		p.m_pCtx.pFuncList.pfDeblocking.pfDeblockingFilterSlice(pCurDq, p.m_pCtx.pFuncList, p.m_pSlice)

		iAnyMbLeftInPartition = kiEndMbIdxInPartition - pCurDq.LastCodedMbIdxOfPartition[kiPartitionId]
		iLocalSliceIdx += kiSliceIdxStep
		p.m_pCtx.pCurDqLayer.sSliceBufferInfo[p.m_iThreadIdx].iCodedSliceNum++
	}

	return ENC_RETURN_SUCCESS
}

// ctorCWelsUpdateMbMapTask is the constructor body
// CWelsUpdateMbMapTask::CWelsUpdateMbMapTask (pSink, pCtx, iSliceIdx): CWelsBaseTask (pSink)
func (p *CWelsUpdateMbMapTask) ctorCWelsUpdateMbMapTask(pSink IWelsTaskSink, pCtx *sWelsEncCtx, iSliceIdx int32) {
	p.ctorCWelsBaseTask(pSink)
	p.m_pCtx = pCtx
	p.m_iSliceIdx = iSliceIdx
}

// Destruct is the destructor CWelsUpdateMbMapTask::~CWelsUpdateMbMapTask (empty).
func (p *CWelsUpdateMbMapTask) Destruct() {
}

func (p *CWelsUpdateMbMapTask) Execute() WelsErrorType {
	UpdateMbListNeighborParallel(p.m_pCtx.pCurDqLayer, p.m_pCtx.pCurDqLayer.sMbDataP, p.m_iSliceIdx)
	return ENC_RETURN_SUCCESS
}
