// Port of test/encoder/EncUT_EncoderTaskManagement.cpp, with sequential and
// bounded-parallel execution checks.

package encoder

import (
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
)

func newTaskMgmtTestCtx() *sWelsEncCtx {
	sCtx := &sWelsEncCtx{}
	sWelsSvcCodingParam := NewSWelsSvcCodingParam()
	sCtx.pSvcParam = sWelsSvcCodingParam
	sWelsSvcCodingParam.IMultipleThreadIdc = 4
	sCtx.iMaxSliceCount = 35
	return sCtx
}

func TestEncoderTaskManagement_CWelsTaskManageBase(t *testing.T) {
	sCtx := newTaskMgmtTestCtx()
	pTaskManage := CreateTaskManage(sCtx, 1, false)
	if pTaskManage == nil {
		t.Fatal("CreateTaskManage returned NULL")
	}
	pTaskManage.Destruct()
}

func TestEncoderTaskManagement_CWelsTaskManageParallel(t *testing.T) {
	sCtx := newTaskMgmtTestCtx()
	pTaskManage := CreateTaskManage(sCtx, 1, true)
	if pTaskManage == nil {
		t.Fatal("CreateTaskManage returned NULL")
	}
	pTaskManage.Destruct()
}

func TestEncoderTaskManagement_CWelsTaskManageMultiD(t *testing.T) {
	sCtx := newTaskMgmtTestCtx()
	sCtx.pSvcParam.SSpatialLayers[0].SSliceArgument.UiSliceNum = 35

	pTaskManage := CreateTaskManage(sCtx, 4, true)
	if pTaskManage == nil {
		t.Fatal("CreateTaskManage returned NULL")
	}
	pBase := pTaskManage.(*CWelsTaskManageBase)
	if pBase.m_iTaskNum[0] != 35 || len(*pBase.m_cEncodingTaskList[0]) != 35 || len(*pBase.m_cPreEncodingTaskList[0]) != 35 {
		t.Fatalf("unexpected task count %d", pBase.m_iTaskNum[0])
	}
	if pTaskManage.GetThreadPoolThreadNum() != 4 {
		t.Fatalf("thread num %d", pTaskManage.GetThreadPoolThreadNum())
	}
	pTaskManage.Destruct()
}

type testOrderTask struct {
	CWelsBaseTask
	id    int32
	order *[]int32
}

func (p *testOrderTask) Execute() WelsErrorType {
	*p.order = append(*p.order, p.id)
	return ENC_RETURN_SUCCESS
}
func (p *testOrderTask) GetTaskType() uint32 { return uint32(WELS_ENC_TASK_ENCODING) }

func TestEncoderTaskManagement_SequentialOrder(t *testing.T) {
	sCtx := newTaskMgmtTestCtx()
	sCtx.pSvcParam.SSpatialLayers[1].SSliceArgument.UiSliceNum = 5
	pTaskManage := CreateTaskManage(sCtx, 2, false)
	if pTaskManage == nil {
		t.Fatal("CreateTaskManage returned NULL")
	}
	pBase := pTaskManage.(*CWelsTaskManageBase)
	var order []int32
	list := TASKLIST_TYPE{}
	for i := int32(0); i < 5; i++ {
		task := &testOrderTask{id: i, order: &order}
		task.ctorCWelsBaseTask(pBase)
		list = append(list, task)
	}
	*pBase.m_cEncodingTaskList[1] = list
	sCtx.pCurDqLayer = &SDqLayer{}
	pTaskManage.InitFrame(1)
	if ret := pTaskManage.ExecuteTasks(WELS_ENC_TASK_ENCODING); ret != ENC_RETURN_SUCCESS {
		t.Fatalf("ExecuteTasks returned %d", ret)
	}
	if len(order) != 5 {
		t.Fatalf("executed %d tasks", len(order))
	}
	for i, id := range order {
		if int32(i) != id {
			t.Fatalf("order %v", order)
		}
	}
	if pBase.m_iWaitTaskNum != 0 {
		t.Fatalf("m_iWaitTaskNum %d", pBase.m_iWaitTaskNum)
	}
	pTaskManage.Destruct()
}

type testParallelTask struct {
	CWelsBaseTask
	id       int32
	thread   int32
	started  chan<- int32
	release  <-chan struct{}
	result   WelsErrorType
	executed bool
}

func (p *testParallelTask) Execute() WelsErrorType {
	p.executed = true
	p.started <- p.id
	<-p.release
	return p.result
}

func (p *testParallelTask) GetTaskType() uint32 { return uint32(WELS_ENC_TASK_ENCODING) }
func (p *testParallelTask) setThreadIndex(thread int32) {
	p.thread = thread
}

func TestEncoderTaskManagement_ParallelWorkers(t *testing.T) {
	sCtx := newTaskMgmtTestCtx()
	sCtx.pSvcParam.IMultipleThreadIdc = 2
	sCtx.pSvcParam.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	sCtx.pSvcParam.IRCMode = api.RC_OFF_MODE
	sCtx.pSvcParam.IEntropyCodingModeFlag = 0
	sCtx.pSvcParam.BUseLoadBalancing = false
	sCtx.pSvcParam.BEnableBackgroundDetection = false
	sCtx.pSvcParam.BEnableAdaptiveQuant = false
	sCtx.pSvcParam.SSpatialLayers[0].SSliceArgument.UiSliceMode = api.SM_FIXEDSLCNUM_SLICE
	sCtx.pSvcParam.SSpatialLayers[0].SSliceArgument.UiSliceNum = 5

	pTaskManage := CreateTaskManage(sCtx, 1, false)
	if pTaskManage == nil {
		t.Fatal("CreateTaskManage returned NULL")
	}
	defer pTaskManage.Destruct()
	pBase := pTaskManage.(*CWelsTaskManageBase)
	started := make(chan int32, 5)
	release := make(chan struct{})
	list := make(TASKLIST_TYPE, 5)
	for i := range list {
		task := &testParallelTask{
			id: int32(i), thread: -1, started: started, release: release,
		}
		if i == 3 {
			task.result = ENC_RETURN_UNEXPECTED
		}
		task.ctorCWelsBaseTask(pBase)
		list[i] = task
	}
	*pBase.m_cEncodingTaskList[0] = list
	sCtx.pCurDqLayer = &SDqLayer{}
	pTaskManage.InitFrame(0)

	done := make(chan WelsErrorType, 1)
	go func() {
		done <- pTaskManage.ExecuteTasks(WELS_ENC_TASK_ENCODING)
	}()

	first := make(map[int32]bool, 2)
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	for len(first) < 2 {
		select {
		case id := <-started:
			first[id] = true
		case <-timer.C:
			close(release)
			<-done
			t.Fatal("two slice workers did not overlap")
		}
	}
	if !first[0] || !first[1] {
		close(release)
		<-done
		t.Fatalf("first worker tasks = %v, want 0 and 1", first)
	}
	select {
	case id := <-started:
		close(release)
		<-done
		t.Fatalf("task %d started before one of the two workers was released", id)
	default:
	}
	close(release)

	if ret := <-done; ret != ENC_RETURN_UNEXPECTED {
		t.Fatalf("ExecuteTasks returned %d, want %d", ret, ENC_RETURN_UNEXPECTED)
	}
	if sCtx.iEncoderError != ENC_RETURN_UNEXPECTED {
		t.Fatalf("iEncoderError = %d, want %d", sCtx.iEncoderError, ENC_RETURN_UNEXPECTED)
	}
	if pBase.m_iWaitTaskNum != 0 {
		t.Fatalf("m_iWaitTaskNum = %d", pBase.m_iWaitTaskNum)
	}
	for i, base := range list {
		task := base.(*testParallelTask)
		if !task.executed {
			t.Errorf("task %d was not executed", i)
		}
		if want := int32(i % 2); task.thread != want {
			t.Errorf("task %d used worker %d, want %d", i, task.thread, want)
		}
	}
}
