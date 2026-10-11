// Port of codec/encoder/core/inc/wels_task_encoder.h.

package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// WriteSliceBs is declared extern here and defined in slice_multi_threading.go.
// (WriteSliceToFrameBs and CreateSliceEncodingTask are declared but never
// defined in the C++ sources and are not ported.)

// iCWelsSliceEncodingTaskVirtual lists the virtual methods that
// CWelsSliceEncodingTask::Execute dispatches to.
type iCWelsSliceEncodingTaskVirtual interface {
	InitTask() WelsErrorType
	ExecuteTask() WelsErrorType
	FinishTask()
}

// CWelsSliceEncodingTask encodes one slice.
type CWelsSliceEncodingTask struct {
	CWelsBaseTask

	// Go-only: the most-derived object, for virtual dispatch from Execute.
	pVirt iCWelsSliceEncodingTaskVirtual

	// protected:
	m_eTaskResult WelsErrorType

	m_pCtx         *sWelsEncCtx
	m_pPrivateData *SSliceThreadPrivateData
	m_pLbi         *api.SLayerBSInfo
	m_iStartMbIdx  int32
	m_iEndMbIdx    int32

	m_eNalType       common.EWelsNalUnitType
	m_eNalRefIdc     common.EWelsNalRefIdc
	m_bNeedPrefix    bool
	m_uiDependencyId uint32

	m_pSlice     *SSlice
	m_pSliceBs   *SWelsSliceBs
	m_iSliceIdx  int32
	m_iSliceSize int32
	m_iThreadIdx int32

	// Go-only: bounded task workers assign stable bitstream-buffer ownership.
	m_bThreadIndexAssigned bool
}

func (p *CWelsSliceEncodingTask) setThreadIndex(iThreadIdx int32) {
	p.m_iThreadIdx = iThreadIdx
	p.m_bThreadIndexAssigned = true
}

// NewCWelsSliceEncodingTask is the C++ constructor (new CWelsSliceEncodingTask (pSink, pCtx, iSliceIdx)).
func NewCWelsSliceEncodingTask(pSink IWelsTaskSink, pCtx *sWelsEncCtx, iSliceIdx int32) *CWelsSliceEncodingTask {
	p := &CWelsSliceEncodingTask{}
	p.pVirt = p
	p.ctorCWelsSliceEncodingTask(pSink, pCtx, iSliceIdx)
	return p
}

func (p *CWelsSliceEncodingTask) GetTaskType() uint32 {
	return uint32(WELS_ENC_TASK_ENCODE_FIXED_SLICE)
}

// CWelsLoadBalancingSlicingEncodingTask records the slice encoding time.
type CWelsLoadBalancingSlicingEncodingTask struct {
	CWelsSliceEncodingTask

	m_iSliceStart int64 // private
}

// NewCWelsLoadBalancingSlicingEncodingTask is the C++ constructor.
func NewCWelsLoadBalancingSlicingEncodingTask(pSink IWelsTaskSink, pCtx *sWelsEncCtx, iSliceIdx int32) *CWelsLoadBalancingSlicingEncodingTask {
	p := &CWelsLoadBalancingSlicingEncodingTask{}
	p.pVirt = p
	p.ctorCWelsSliceEncodingTask(pSink, pCtx, iSliceIdx)
	return p
}

func (p *CWelsLoadBalancingSlicingEncodingTask) GetTaskType() uint32 {
	return uint32(WELS_ENC_TASK_ENCODE_SLICE_LOADBALANCING)
}

// CWelsConstrainedSizeSlicingEncodingTask encodes size-limited slices.
type CWelsConstrainedSizeSlicingEncodingTask struct {
	CWelsLoadBalancingSlicingEncodingTask
}

// NewCWelsConstrainedSizeSlicingEncodingTask is the C++ constructor.
func NewCWelsConstrainedSizeSlicingEncodingTask(pSink IWelsTaskSink, pCtx *sWelsEncCtx, iSliceIdx int32) *CWelsConstrainedSizeSlicingEncodingTask {
	p := &CWelsConstrainedSizeSlicingEncodingTask{}
	p.pVirt = p
	p.ctorCWelsSliceEncodingTask(pSink, pCtx, iSliceIdx)
	return p
}

func (p *CWelsConstrainedSizeSlicingEncodingTask) GetTaskType() uint32 {
	return uint32(WELS_ENC_TASK_ENCODE_SLICE_SIZECONSTRAINED)
}

// CWelsUpdateMbMapTask updates the MB neighbour info of one slice.
type CWelsUpdateMbMapTask struct {
	CWelsBaseTask

	// protected:
	m_pCtx      *sWelsEncCtx
	m_iSliceIdx int32
}

// NewCWelsUpdateMbMapTask is the C++ constructor.
func NewCWelsUpdateMbMapTask(pSink IWelsTaskSink, pCtx *sWelsEncCtx, iSliceIdx int32) *CWelsUpdateMbMapTask {
	p := &CWelsUpdateMbMapTask{}
	p.ctorCWelsUpdateMbMapTask(pSink, pCtx, iSliceIdx)
	return p
}

func (p *CWelsUpdateMbMapTask) GetTaskType() uint32 {
	return uint32(WELS_ENC_TASK_UPDATEMBMAP)
}

var (
	_ IWelsBaseTask = (*CWelsSliceEncodingTask)(nil)
	_ IWelsBaseTask = (*CWelsLoadBalancingSlicingEncodingTask)(nil)
	_ IWelsBaseTask = (*CWelsConstrainedSizeSlicingEncodingTask)(nil)
	_ IWelsBaseTask = (*CWelsUpdateMbMapTask)(nil)

	_ iCWelsSliceEncodingTaskVirtual = (*CWelsSliceEncodingTask)(nil)
	_ iCWelsSliceEncodingTaskVirtual = (*CWelsLoadBalancingSlicingEncodingTask)(nil)
	_ iCWelsSliceEncodingTaskVirtual = (*CWelsConstrainedSizeSlicingEncodingTask)(nil)
)
