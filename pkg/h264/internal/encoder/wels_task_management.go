// Port of codec/encoder/core/src/wels_task_management.cpp.
//
// Fixed-slice encoding tasks can run on bounded worker goroutines. Other task
// lists remain sequential because their upstream synchronization has not been
// ported.

package encoder

import (
	"sync"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

type threadIndexedTask interface {
	setThreadIndex(int32)
}

// CreateTaskManage is the static IWelsTaskManage::CreateTaskManage.
func CreateTaskManage(pCtx *sWelsEncCtx, iSpatialLayer int32, bNeedLock bool) IWelsTaskManage {
	if nil == pCtx {
		return nil
	}

	pTaskManage := NewCWelsTaskManageBase()

	if ENC_RETURN_SUCCESS != pTaskManage.Init(pCtx) {
		pTaskManage.Uninit()
		pTaskManage.Destruct()
		return nil
	}
	return pTaskManage
}

// ctorCWelsTaskManageBase is the constructor body CWelsTaskManageBase::CWelsTaskManageBase.
func (p *CWelsTaskManageBase) ctorCWelsTaskManageBase() {
	p.m_pEncCtx = nil
	p.m_iWaitTaskNum = 0

	for iDid := 0; iDid < MAX_DEPENDENCY_LAYER; iDid++ {
		p.m_iTaskNum[iDid] = 0
		p.m_cEncodingTaskList[iDid] = new(TASKLIST_TYPE)
		p.m_cPreEncodingTaskList[iDid] = new(TASKLIST_TYPE)
	}
}

// Destruct is the destructor CWelsTaskManageBase::~CWelsTaskManageBase.
func (p *CWelsTaskManageBase) Destruct() {
	p.Uninit()
}

func (p *CWelsTaskManageBase) Init(pEncCtx *sWelsEncCtx) WelsErrorType {
	p.m_pEncCtx = pEncCtx
	p.m_iThreadNum = int32(p.m_pEncCtx.pSvcParam.IMultipleThreadIdc)
	// CWelsThreadPool::SetThreadNum: a non-positive count becomes 1.
	if p.m_iThreadNum <= 0 {
		p.m_iThreadNum = 1
	}

	iReturn := int32(ENC_RETURN_SUCCESS)
	for iDid := 0; iDid < MAX_DEPENDENCY_LAYER; iDid++ {
		p.m_pcAllTaskList[WELS_ENC_TASK_ENCODING][iDid] = p.m_cEncodingTaskList[iDid]
		p.m_pcAllTaskList[WELS_ENC_TASK_UPDATEMBMAP][iDid] = p.m_cPreEncodingTaskList[iDid]
		iReturn |= p.CreateTasks(pEncCtx, int32(iDid))
	}

	return iReturn
}

func (p *CWelsTaskManageBase) Uninit() {
	p.DestroyTasks()

	for iDid := 0; iDid < MAX_DEPENDENCY_LAYER; iDid++ {
		p.m_cEncodingTaskList[iDid] = nil
		p.m_cPreEncodingTaskList[iDid] = nil
	}
}

func (p *CWelsTaskManageBase) CreateTasks(pEncCtx *sWelsEncCtx, kiCurDid int32) WelsErrorType {
	var pTask IWelsBaseTask
	var kiTaskCount int32
	uiSliceMode := pEncCtx.pSvcParam.SSpatialLayers[kiCurDid].SSliceArgument.UiSliceMode

	if uiSliceMode != api.SM_SIZELIMITED_SLICE {
		p.m_iTaskNum[kiCurDid] = int32(pEncCtx.pSvcParam.SSpatialLayers[kiCurDid].SSliceArgument.UiSliceNum)
	} else {
		p.m_iTaskNum[kiCurDid] = int32(pEncCtx.iActiveThreadsNum)
	}
	kiTaskCount = p.m_iTaskNum[kiCurDid]

	for idx := int32(0); idx < kiTaskCount; idx++ {
		pTask = NewCWelsUpdateMbMapTask(p, pEncCtx, idx)
		*p.m_cPreEncodingTaskList[kiCurDid] = append(*p.m_cPreEncodingTaskList[kiCurDid], pTask)
	}

	for idx := int32(0); idx < kiTaskCount; idx++ {
		if uiSliceMode == api.SM_SIZELIMITED_SLICE {
			pTask = NewCWelsConstrainedSizeSlicingEncodingTask(p, pEncCtx, idx)
		} else {
			if pEncCtx.pSvcParam.BUseLoadBalancing {
				pTask = NewCWelsLoadBalancingSlicingEncodingTask(p, pEncCtx, idx)
			} else {
				pTask = NewCWelsSliceEncodingTask(p, pEncCtx, idx)
			}
		}
		*p.m_cEncodingTaskList[kiCurDid] = append(*p.m_cEncodingTaskList[kiCurDid], pTask)
	}

	return ENC_RETURN_SUCCESS
}

func (p *CWelsTaskManageBase) DestroyTaskList(pTargetTaskList *TASKLIST_TYPE) {
	if nil == pTargetTaskList {
		return
	}
	for len(*pTargetTaskList) > 0 {
		pTask := (*pTargetTaskList)[0]
		pTask.Destruct()
		*pTargetTaskList = (*pTargetTaskList)[1:]
	}
	*pTargetTaskList = nil
}

func (p *CWelsTaskManageBase) DestroyTasks() {
	for iDid := 0; iDid < MAX_DEPENDENCY_LAYER; iDid++ {
		if p.m_iTaskNum[iDid] > 0 {
			p.DestroyTaskList(p.m_cEncodingTaskList[iDid])
			p.DestroyTaskList(p.m_cPreEncodingTaskList[iDid])
			p.m_iTaskNum[iDid] = 0
			p.m_pcAllTaskList[WELS_ENC_TASK_ENCODING][iDid] = nil
		}
	}
}

// OnTaskMinusOne records completion after a task has finished. Parallel task
// execution invokes sinks on the caller after all workers have joined.
func (p *CWelsTaskManageBase) OnTaskMinusOne() {
	p.m_iWaitTaskNum--
}

func (p *CWelsTaskManageBase) OnTaskCancelled() WelsErrorType {
	p.OnTaskMinusOne()
	return ENC_RETURN_SUCCESS
}

func (p *CWelsTaskManageBase) OnTaskExecuted() WelsErrorType {
	p.OnTaskMinusOne()
	return ENC_RETURN_SUCCESS
}

// ExecuteTaskList runs a task row in queue order on the calling goroutine.
func (p *CWelsTaskManageBase) ExecuteTaskList(pTaskList *[MAX_DEPENDENCY_LAYER]*TASKLIST_TYPE) WelsErrorType {
	p.m_iWaitTaskNum = p.m_iTaskNum[p.m_iCurDid]
	pTargetTaskList := pTaskList[p.m_iCurDid]
	if p.m_iWaitTaskNum == 0 {
		return ENC_RETURN_SUCCESS
	}

	iCurrentTaskCount := p.m_iWaitTaskNum
	iReturn := WelsErrorType(ENC_RETURN_SUCCESS)
	for iIdx := int32(0); iIdx < iCurrentTaskCount; iIdx++ {
		pTask := (*pTargetTaskList)[iIdx]
		if indexed, ok := pTask.(threadIndexedTask); ok {
			indexed.setThreadIndex(0)
		}
		iReturn |= pTask.Execute()
		if pSink := pTask.GetSink(); pSink != nil {
			pSink.OnTaskExecuted()
		}
	}

	if iReturn != ENC_RETURN_SUCCESS {
		p.m_pEncCtx.iEncoderError |= iReturn
	}
	return iReturn
}

// canExecuteTasksInParallel limits the concurrent path to the configuration
// used by RDP. Dynamic slicing and GOM rate control still contain shared
// updates that require the upstream synchronization strategy.
func (p *CWelsTaskManageBase) canExecuteTasksInParallel(iTaskType ETaskType) bool {
	if iTaskType != WELS_ENC_TASK_ENCODING || p.m_iThreadNum <= 1 || p.m_pEncCtx == nil || p.m_pEncCtx.pSvcParam == nil {
		return false
	}
	param := p.m_pEncCtx.pSvcParam
	if p.m_iCurDid < 0 || p.m_iCurDid >= param.ISpatialLayerNum ||
		param.IUsageType != api.CAMERA_VIDEO_REAL_TIME ||
		param.IRCMode != api.RC_OFF_MODE ||
		param.IEntropyCodingModeFlag != 0 ||
		param.BUseLoadBalancing ||
		param.BEnableBackgroundDetection ||
		param.BEnableAdaptiveQuant ||
		param.SSpatialLayers[p.m_iCurDid].SSliceArgument.UiSliceMode != api.SM_FIXEDSLCNUM_SLICE {
		return false
	}
	pTaskList := p.m_pcAllTaskList[iTaskType][p.m_iCurDid]
	if pTaskList == nil {
		return false
	}
	for _, pTask := range *pTaskList {
		if _, ok := pTask.(threadIndexedTask); !ok {
			return false
		}
	}
	return true
}

// executeTaskListParallel assigns one private bitstream buffer to each worker.
// A worker processes its lane in order, so a buffer is never used by two tasks
// concurrently. Task results and sink notifications are reduced after the
// barrier on the caller goroutine.
func (p *CWelsTaskManageBase) executeTaskListParallel(pTaskList *[MAX_DEPENDENCY_LAYER]*TASKLIST_TYPE) WelsErrorType {
	p.m_iWaitTaskNum = p.m_iTaskNum[p.m_iCurDid]
	pTargetTaskList := pTaskList[p.m_iCurDid]
	iTaskCount := p.m_iWaitTaskNum
	if iTaskCount == 0 {
		return ENC_RETURN_SUCCESS
	}

	iWorkerCount := p.m_iThreadNum
	if iWorkerCount > iTaskCount {
		iWorkerCount = iTaskCount
	}
	if iWorkerCount > MAX_THREADS_NUM {
		iWorkerCount = MAX_THREADS_NUM
	}

	var taskResults [MAX_THREADS_NUM]WelsErrorType
	var workers sync.WaitGroup
	workers.Add(int(iWorkerCount))
	for iWorker := int32(0); iWorker < iWorkerCount; iWorker++ {
		go func(iThreadIdx int32) {
			defer workers.Done()
			iResult := WelsErrorType(ENC_RETURN_SUCCESS)
			for iTask := iThreadIdx; iTask < iTaskCount; iTask += iWorkerCount {
				pTask := (*pTargetTaskList)[iTask]
				if indexed, ok := pTask.(threadIndexedTask); ok {
					indexed.setThreadIndex(iThreadIdx)
				}
				iResult |= pTask.Execute()
			}
			taskResults[iThreadIdx] = iResult
		}(iWorker)
	}
	workers.Wait()

	iReturn := WelsErrorType(ENC_RETURN_SUCCESS)
	for iWorker := int32(0); iWorker < iWorkerCount; iWorker++ {
		iReturn |= taskResults[iWorker]
	}
	for iTask := int32(0); iTask < iTaskCount; iTask++ {
		if pSink := (*pTargetTaskList)[iTask].GetSink(); pSink != nil {
			pSink.OnTaskExecuted()
		}
	}
	if iReturn != ENC_RETURN_SUCCESS {
		p.m_pEncCtx.iEncoderError |= iReturn
	}

	return iReturn
}

func (p *CWelsTaskManageBase) InitFrame(kiCurDid int32) {
	p.m_iCurDid = kiCurDid
	if p.m_pEncCtx.pCurDqLayer.bNeedAdjustingSlicing {
		p.ExecuteTaskList(&p.m_pcAllTaskList[WELS_ENC_TASK_UPDATEMBMAP])
	}
}

func (p *CWelsTaskManageBase) ExecuteTasks(iTaskType ETaskType) WelsErrorType {
	if p.canExecuteTasksInParallel(iTaskType) {
		return p.executeTaskListParallel(&p.m_pcAllTaskList[iTaskType])
	}
	return p.ExecuteTaskList(&p.m_pcAllTaskList[iTaskType])
}

// GetThreadPoolThreadNum returns the configured slice worker count.
func (p *CWelsTaskManageBase) GetThreadPoolThreadNum() int32 {
	return p.m_iThreadNum
}

// CWelsTaskManageOne is for test

func (p *CWelsTaskManageOne) Init(pEncCtx *sWelsEncCtx) WelsErrorType {
	p.m_pEncCtx = pEncCtx

	return p.CreateTasks(pEncCtx, pEncCtx.iMaxSliceCount)
}

func (p *CWelsTaskManageOne) ExecuteTasks(iTaskType ETaskType) WelsErrorType {
	p.m_iWaitTaskNum = int32(len(*p.m_cEncodingTaskList[0]))
	iReturn := WelsErrorType(ENC_RETURN_SUCCESS)
	for len(*p.m_cEncodingTaskList[0]) > 0 {
		pTask := (*p.m_cEncodingTaskList[0])[0]
		if indexed, ok := pTask.(threadIndexedTask); ok {
			indexed.setThreadIndex(0)
		}
		iReturn |= pTask.Execute()
		if pSink := pTask.GetSink(); pSink != nil {
			pSink.OnTaskExecuted()
		}
		*p.m_cEncodingTaskList[0] = (*p.m_cEncodingTaskList[0])[1:]
	}
	if iReturn != ENC_RETURN_SUCCESS {
		p.m_pEncCtx.iEncoderError |= iReturn
	}
	return iReturn
}
