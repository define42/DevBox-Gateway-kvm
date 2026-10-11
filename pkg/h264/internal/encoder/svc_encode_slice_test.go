// Port of test/encoder/EncUT_SliceBufferReallocate.cpp.
//
// The C fixture builds a partial sWelsEncCtx by hand and only uses the full
// encoder (WelsCreateSVCEncoder / InitializeExt / AcquireLayersNals /
// FreeDqLayer) for default parameters, parameter validation and NAL count
// estimation. The Go port builds the same partial context directly, so the
// tests only exercise the slice buffer (re)allocation code of
// svc_encode_slice.go.

package encoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const (
	sbrMAX_WIDTH    = 4096
	sbrMAX_HEIGH    = 2304
	sbrMAX_QP       = 51
	sbrMIN_QP       = 0
	sbrTestRepeat   = 8
	sbrMaxPicWidth  = 1920 // keep the allocations of the test moderate
	sbrMaxPicHeight = 1088
)

type sliceBufferReallocTest struct {
	t            *testing.T
	rnd          *rand.Rand
	m_EncContext sWelsEncCtx
}

func newSliceBufferReallocTest(t *testing.T, seed int64) *sliceBufferReallocTest {
	f := &sliceBufferReallocTest{t: t, rnd: rand.New(rand.NewSource(seed))}
	f.m_EncContext.pSvcParam = NewSWelsSvcCodingParam()
	return f
}

func (f *sliceBufferReallocTest) rand() int32 { return int32(f.rnd.Int31()) }

func sbrRandAvailableThread(pCtx *sWelsEncCtx, kiMinBufferNum int32, f *sliceBufferReallocTest) int32 {
	var aiThrdList [MAX_THREADS_NUM]int32
	iAvailableThrdNum := int32(0)

	if nil == pCtx || nil == pCtx.pCurDqLayer || pCtx.iActiveThreadsNum <= 0 {
		return -1
	}

	for iThrdIdx := int32(0); iThrdIdx < int32(pCtx.iActiveThreadsNum); iThrdIdx++ {
		iCodedSlcNum := pCtx.pCurDqLayer.sSliceBufferInfo[iThrdIdx].iCodedSliceNum
		iMaxSlcNumInThrd := pCtx.pCurDqLayer.sSliceBufferInfo[iThrdIdx].iMaxSliceNum

		if (iCodedSlcNum + kiMinBufferNum) <= iMaxSlcNumInThrd {
			aiThrdList[iAvailableThrdNum] = iThrdIdx
			iAvailableThrdNum++
		}
	}

	if 0 == iAvailableThrdNum {
		return -1
	}
	return aiThrdList[f.rand()%iAvailableThrdNum]
}

func sbrAllocateLayerBuffer(pCtx *sWelsEncCtx, iLayerIdx int32) int32 {
	pLayerCfg := &pCtx.pSvcParam.SSpatialLayers[iLayerIdx]
	pDqLayer := &SDqLayer{}

	pDqLayer.iMbWidth = int16((pLayerCfg.IVideoWidth + 15) >> 4)
	pDqLayer.iMbHeight = int16((pLayerCfg.IVideoHeight + 15) >> 4)
	pDqLayer.iMaxSliceNum = GetInitialSliceNum(&pLayerCfg.SSliceArgument)

	iRet := InitSliceInLayer(pCtx, pDqLayer, iLayerIdx)
	if ENC_RETURN_SUCCESS != iRet {
		return ENC_RETURN_MEMALLOCERR
	}

	pCtx.ppDqLayerList[iLayerIdx] = pDqLayer
	return ENC_RETURN_SUCCESS
}

func sbrSetPartitonMBNum(pCurDqLayer *SDqLayer, pLayerCfg *api.SSpatialLayerConfig, iPartNum int32) {
	iMBWidth := (pLayerCfg.IVideoWidth + 15) >> 4
	iMBHeight := (pLayerCfg.IVideoHeight + 15) >> 4
	iMbNumInFrame := iMBWidth * iMBHeight
	iMBPerPart := iMbNumInFrame / iPartNum

	if 0 == iMBPerPart {
		iPartNum = 1
		iMBPerPart = iMbNumInFrame
	}

	for iPartIdx := int32(0); iPartIdx < (iPartNum - 1); iPartIdx++ {
		pCurDqLayer.FirstMbIdxOfPartition[iPartIdx] = iMBPerPart * iPartIdx
		pCurDqLayer.EndMbIdxOfPartition[iPartIdx] = pCurDqLayer.FirstMbIdxOfPartition[iPartIdx] + iMBPerPart - 1
	}

	pCurDqLayer.FirstMbIdxOfPartition[iPartNum-1] = iMBPerPart * (iPartNum - 1)
	pCurDqLayer.EndMbIdxOfPartition[iPartNum-1] = iMbNumInFrame - 1

	for iPartIdx := iPartNum; iPartIdx < MAX_THREADS_NUM; iPartIdx++ {
		pCurDqLayer.FirstMbIdxOfPartition[iPartIdx] = 0
		pCurDqLayer.EndMbIdxOfPartition[iPartIdx] = 0
	}
}

func sbrLayerBsSize(pLayerCfg *api.SSpatialLayerConfig) int32 {
	return common.WELS_ROUND(float32((3*pLayerCfg.IVideoWidth*pLayerCfg.IVideoHeight)>>1)*COMPRESS_RATIO_THR) +
		MAX_MACROBLOCK_SIZE_IN_BYTE_x2
}

func sbrInitParamForSizeLimitSlcMode(pCtx *sWelsEncCtx, iLayerIdx int32) int32 {
	pLayerCfg := &pCtx.pSvcParam.SSpatialLayers[iLayerIdx]
	pSliceArgument := &pLayerCfg.SSliceArgument
	iLayerBsSize := sbrLayerBsSize(pLayerCfg)
	pSliceArgument.UiSliceSizeConstraint = 600
	pCtx.pSvcParam.UiMaxNalSize = 1500

	iMaxSliceNumEstimation := common.WELS_MIN(int32(AVERSLICENUM_CONSTRAINT),
		(iLayerBsSize/int32(pSliceArgument.UiSliceSizeConstraint))+1)
	pCtx.iMaxSliceCount = common.WELS_MAX(pCtx.iMaxSliceCount, iMaxSliceNumEstimation)
	iSliceBufferSize := (common.WELS_MAX(int32(pSliceArgument.UiSliceSizeConstraint),
		iLayerBsSize/iMaxSliceNumEstimation) << 1) + MAX_MACROBLOCK_SIZE_IN_BYTE_x2
	pCtx.iSliceBufferSize[iLayerIdx] = iSliceBufferSize

	iRet := sbrAllocateLayerBuffer(pCtx, iLayerIdx)
	if ENC_RETURN_SUCCESS != iRet {
		return ENC_RETURN_MEMALLOCERR
	}

	sbrSetPartitonMBNum(pCtx.ppDqLayerList[iLayerIdx], pLayerCfg, int32(pCtx.iActiveThreadsNum))
	return ENC_RETURN_SUCCESS
}

func (f *sliceBufferReallocTest) InitParamForRasterSlcMode(pCtx *sWelsEncCtx, iLayerIdx int32) {
	pLayerCfg := &pCtx.pSvcParam.SSpatialLayers[iLayerIdx]
	pSliceArgument := &pLayerCfg.SSliceArgument
	iMBWidth := (pLayerCfg.IVideoWidth + 15) >> 4
	iMBHeight := (pLayerCfg.IVideoHeight + 15) >> 4
	iMbNumInFrame := iMBWidth * iMBHeight
	iSliceMBNum := int32(0)

	pSliceArgument.UiSliceMbNum[0] = uint32(f.rand() % 2)
	if 0 == pSliceArgument.UiSliceMbNum[0] && iMBHeight > MAX_SLICES_NUM {
		pSliceArgument.UiSliceNum = MAX_SLICES_NUM
		pSliceArgument.UiSliceMbNum[0] = 1
	}

	if 0 != pSliceArgument.UiSliceMbNum[0] {
		iSliceMBNum = iMbNumInFrame / int32(pSliceArgument.UiSliceNum)
		for iSlcIdx := int32(0); iSlcIdx < int32(pSliceArgument.UiSliceNum)-1; iSlcIdx++ {
			pSliceArgument.UiSliceMbNum[iSlcIdx] = uint32(iSliceMBNum)
		}
		iSliceMBNum = iMbNumInFrame / int32(pSliceArgument.UiSliceNum)
		pSliceArgument.UiSliceMbNum[pSliceArgument.UiSliceNum-1] = uint32(iMbNumInFrame - iSliceMBNum*
			(int32(pSliceArgument.UiSliceNum)-1))
	}
}

func (f *sliceBufferReallocTest) SetParamForReallocateTest(pCtx *sWelsEncCtx, iLayerIdx int32,
	iThreadIndex int32, iPartitionNum int32) {
	pLayerCfg := &pCtx.pSvcParam.SSpatialLayers[iLayerIdx]
	iPartitionID := f.rand() % iPartitionNum
	iCodedSlcNum := pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum - 1
	iLastCodeSlcIdx := iPartitionID + iCodedSlcNum*iPartitionNum
	pLastCodedSlc := &pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].pSliceBuffer[iCodedSlcNum-1]
	pLastCodedSlc.iSliceIdx = iLastCodeSlcIdx

	sbrSetPartitonMBNum(pCtx.ppDqLayerList[iLayerIdx], pLayerCfg, iPartitionNum)

	iMBNumInPatition := pCtx.pCurDqLayer.EndMbIdxOfPartition[iPartitionID] -
		pCtx.pCurDqLayer.FirstMbIdxOfPartition[iPartitionID] + 1
	pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iCodedSliceNum = iCodedSlcNum
	pCtx.pCurDqLayer.LastCodedMbIdxOfPartition[iPartitionID] = f.rand()%iMBNumInPatition + 1
}

func (f *sliceBufferReallocTest) InitParam() int32 {
	pCtx := &f.m_EncContext
	pCtx.pFuncList = &SWelsFuncPtrList{}

	//always multi thread cases
	pCtx.pSvcParam.IMultipleThreadIdc = uint16((f.rand() % MAX_THREADS_NUM) + 1)
	if pCtx.pSvcParam.IMultipleThreadIdc <= 1 {
		pCtx.pSvcParam.IMultipleThreadIdc = 2
	}
	pCtx.iActiveThreadsNum = int16(pCtx.pSvcParam.IMultipleThreadIdc)
	pCtx.pSvcParam.ISpatialLayerNum = 1
	pCtx.pSvcParam.BSimulcastAVC = f.rand()%2 == 1

	pCtx.pSvcParam.IPicHeight = (((f.rand() % sbrMaxPicHeight) >> 4) << 4) + 16
	pCtx.pSvcParam.IPicWidth = (((f.rand() % sbrMaxPicWidth) >> 4) << 4) + 16
	pCtx.iGlobalQp = common.WELS_CLIP3(f.rand()%sbrMAX_QP, sbrMIN_QP, sbrMAX_QP)
	pCtx.pSvcParam.IRCMode = api.RC_OFF_MODE

	pCtx.ppDqLayerList = make([]*SDqLayer, pCtx.pSvcParam.ISpatialLayerNum)
	return ENC_RETURN_SUCCESS
}

func (f *sliceBufferReallocTest) InitFrameBsBuffer() int32 {
	const iLayerIdx = 0
	pCtx := &f.m_EncContext
	pLayerCfg := &pCtx.pSvcParam.SSpatialLayers[iLayerIdx]
	pLayerCfg.IVideoWidth = pCtx.pSvcParam.IPicWidth
	pLayerCfg.IVideoHeight = pCtx.pSvcParam.IPicHeight

	iNonVclLayersBsSizeCount := int32(SSEI_BUFFER_SIZE + 2*SPS_BUFFER_SIZE + 2*PPS_BUFFER_SIZE)
	iLayerBsSize := sbrLayerBsSize(pLayerCfg)
	iVclLayersBsSizeCount := common.WELS_ALIGN(iLayerBsSize, 4)
	iCountBsLen := iNonVclLayersBsSizeCount + iVclLayersBsSizeCount
	// AcquireLayersNals estimation (one spatial layer, maximal slice count)
	iCountNals := int32(MAX_SLICES_NUM*2 + 4)

	// Output
	pCtx.pOut = &SWelsEncoderOutput{}
	pCtx.pOut.pBsBuffer = make([]uint8, iCountBsLen)
	pCtx.pOut.uiSize = uint32(iCountBsLen)
	common.InitBits(&pCtx.pOut.sBsWrite, pCtx.pOut.pBsBuffer, 0, iCountBsLen)
	pCtx.pOut.sNalList = make([]SWelsNalRaw, iCountNals)
	pCtx.pOut.pNalLen = make([]int32, iCountNals)
	pCtx.pOut.iCountNals = iCountNals
	pCtx.pOut.iNalIndex = 0
	pCtx.pOut.iLayerBsIndex = 0
	pCtx.pFrameBs = make([]uint8, iCountBsLen)
	pCtx.iFrameBsSize = iCountBsLen
	pCtx.iPosBsBuffer = 0
	return ENC_RETURN_SUCCESS
}

func (f *sliceBufferReallocTest) InitLayerSliceBuffer(iLayerIdx int32) int32 {
	pCtx := &f.m_EncContext
	pLayerCfg := &pCtx.pSvcParam.SSpatialLayers[iLayerIdx]
	pSliceArgument := &pLayerCfg.SSliceArgument

	pLayerCfg.IVideoWidth = pCtx.pSvcParam.IPicWidth >> (pCtx.pSvcParam.ISpatialLayerNum - 1 - iLayerIdx)
	pLayerCfg.IVideoHeight = pCtx.pSvcParam.IPicHeight >> (pCtx.pSvcParam.ISpatialLayerNum - 1 - iLayerIdx)
	pLayerCfg.ISpatialBitrate = pCtx.pSvcParam.ITargetBitrate / pCtx.pSvcParam.ISpatialLayerNum

	//Slice argument
	pSliceArgument.UiSliceMode = api.SliceModeEnum(f.rand() % 4)
	pSliceArgument.UiSliceNum = uint32(f.rand()%MAX_SLICES_NUM + 1)

	if pSliceArgument.UiSliceMode == api.SM_SIZELIMITED_SLICE {
		if ENC_RETURN_SUCCESS != sbrInitParamForSizeLimitSlcMode(pCtx, iLayerIdx) {
			return ENC_RETURN_MEMALLOCERR
		}
	} else {
		if pSliceArgument.UiSliceMode == api.SM_RASTER_SLICE {
			f.InitParamForRasterSlcMode(pCtx, iLayerIdx)
		}

		iLayerBsSize := sbrLayerBsSize(pLayerCfg)
		pCtx.iMaxSliceCount = common.WELS_MAX(pCtx.iMaxSliceCount, int32(pSliceArgument.UiSliceNum))
		iSliceBufferSize := ((iLayerBsSize / int32(pSliceArgument.UiSliceNum)) << 1) + MAX_MACROBLOCK_SIZE_IN_BYTE_x2

		pCtx.iSliceBufferSize[iLayerIdx] = iSliceBufferSize
		if ENC_RETURN_SUCCESS != sbrAllocateLayerBuffer(pCtx, iLayerIdx) {
			return ENC_RETURN_MEMALLOCERR
		}
	}

	if nil == pCtx.ppDqLayerList[iLayerIdx] {
		return ENC_RETURN_MEMALLOCERR
	}

	pCtx.uiDependencyId = uint8(iLayerIdx)
	pCtx.pCurDqLayer = pCtx.ppDqLayerList[iLayerIdx]
	return ENC_RETURN_SUCCESS
}

func (f *sliceBufferReallocTest) InitParamForTestCase(iLayerIdx int32) int32 {
	if ENC_RETURN_SUCCESS != f.InitParam() || ENC_RETURN_SUCCESS != f.InitFrameBsBuffer() ||
		ENC_RETURN_SUCCESS != f.InitLayerSliceBuffer(iLayerIdx) {
		return ENC_RETURN_MEMALLOCERR
	}
	return ENC_RETURN_SUCCESS
}

func (f *sliceBufferReallocTest) InitParamForSizeLimitSlcModeCase(iLayerIdx int32) int32 {
	pSliceArgument := &f.m_EncContext.pSvcParam.SSpatialLayers[iLayerIdx].SSliceArgument

	if ENC_RETURN_SUCCESS != f.InitParamForTestCase(iLayerIdx) {
		return ENC_RETURN_MEMALLOCERR
	}

	if api.SM_SIZELIMITED_SLICE != pSliceArgument.UiSliceMode && nil != f.m_EncContext.ppDqLayerList[iLayerIdx] {
		f.m_EncContext.ppDqLayerList[iLayerIdx] = nil
		pSliceArgument.UiSliceMode = api.SM_SIZELIMITED_SLICE
		if ENC_RETURN_SUCCESS != sbrInitParamForSizeLimitSlcMode(&f.m_EncContext, iLayerIdx) ||
			nil == f.m_EncContext.ppDqLayerList[iLayerIdx] {
			return ENC_RETURN_MEMALLOCERR
		}
	}
	return ENC_RETURN_SUCCESS
}

func (f *sliceBufferReallocTest) SimulateEncodedOneSlice(kiSlcIdx int32, kiThreadIdx int32) {
	if f.m_EncContext.pCurDqLayer.bThreadSlcBufferFlag {
		iCodedSlcNumInThrd := f.m_EncContext.pCurDqLayer.sSliceBufferInfo[kiThreadIdx].iCodedSliceNum

		if nil == f.m_EncContext.pCurDqLayer.sSliceBufferInfo[kiThreadIdx].pSliceBuffer {
			f.t.Fatal("pSliceBuffer is NULL")
		}

		f.m_EncContext.pCurDqLayer.sSliceBufferInfo[kiThreadIdx].pSliceBuffer[iCodedSlcNumInThrd].iSliceIdx = kiSlcIdx
		f.m_EncContext.pCurDqLayer.sSliceBufferInfo[kiThreadIdx].iCodedSliceNum++
	} else {
		f.m_EncContext.pCurDqLayer.sSliceBufferInfo[0].pSliceBuffer[kiSlcIdx].iSliceIdx = kiSlcIdx
	}
}

func (f *sliceBufferReallocTest) SimulateSliceInOnePartition(kiPartNum int32, kiPartIdx int32, kiSlcNumInPart int32) {
	//slice within same partition will encoded by same thread in current design
	iPartitionThrdIdx := sbrRandAvailableThread(&f.m_EncContext, kiSlcNumInPart, f)
	if -1 == iPartitionThrdIdx {
		f.t.Fatal("no available thread")
	}

	for iSlcIdx := int32(0); iSlcIdx < kiSlcNumInPart; iSlcIdx++ {
		iSlcIdxInPart := kiPartIdx + kiPartNum*iSlcIdx

		f.SimulateEncodedOneSlice(iSlcIdxInPart, iPartitionThrdIdx)

		f.m_EncContext.pCurDqLayer.NumSliceCodedOfPartition[kiPartIdx]++
		f.m_EncContext.pCurDqLayer.sSliceEncCtx.iSliceNumInFrame++
	}
}

func (f *sliceBufferReallocTest) SimulateSliceInOneLayer() {
	iLayerIdx := 0
	pLayerCfg := &f.m_EncContext.pSvcParam.SSpatialLayers[iLayerIdx]
	iTotalSliceBuffer := f.m_EncContext.pCurDqLayer.iMaxSliceNum
	iSimulateSliceNum := f.rand()%iTotalSliceBuffer + 1

	if api.SM_SIZELIMITED_SLICE == pLayerCfg.SSliceArgument.UiSliceMode {
		iPartNum := int32(f.m_EncContext.iActiveThreadsNum)
		iSlicNumPerPart := iSimulateSliceNum / iPartNum
		iMaxSlcNumInThrd := f.m_EncContext.pCurDqLayer.sSliceBufferInfo[0].iMaxSliceNum

		iSlicNumPerPart = common.WELS_CLIP3(iSlicNumPerPart, 1, iMaxSlcNumInThrd)
		iLastPartSlcNum := iSimulateSliceNum - iSlicNumPerPart*(iPartNum-1)
		iLastPartSlcNum = common.WELS_CLIP3(iLastPartSlcNum, 1, iMaxSlcNumInThrd)

		for iPartIdx := int32(0); iPartIdx < iPartNum; iPartIdx++ {
			iSlcNumInPart := iLastPartSlcNum
			if iPartIdx < (iPartNum - 1) {
				iSlcNumInPart = iSlicNumPerPart
			}
			f.SimulateSliceInOnePartition(iPartNum, iPartIdx, iSlcNumInPart)
		}
	} else {
		for iSlcIdx := int32(0); iSlcIdx < iSimulateSliceNum; iSlcIdx++ {
			iSlcThrdIdx := sbrRandAvailableThread(&f.m_EncContext, 1, f)
			if -1 == iSlcThrdIdx {
				f.t.Fatal("no available thread")
			}

			f.SimulateEncodedOneSlice(iSlcIdx, iSlcThrdIdx)
			f.m_EncContext.pCurDqLayer.sSliceEncCtx.iSliceNumInFrame++
		}
	}
}

func TestSliceBufferReallocate_Reallocate_in_one_partition(t *testing.T) {
	for iRepeat := int64(0); iRepeat < sbrTestRepeat; iRepeat++ {
		f := newSliceBufferReallocTest(t, 100+iRepeat)
		pCtx := &f.m_EncContext
		iLayerIdx := int32(0)

		if iRet := f.InitParamForSizeLimitSlcModeCase(iLayerIdx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitParamForSizeLimitSlcModeCase: %d", iRet)
		}
		pCtx.pCurDqLayer = pCtx.ppDqLayerList[iLayerIdx]
		if iRet := InitAllSlicesInThread(pCtx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitAllSlicesInThread: %d", iRet)
		}

		//case: reallocate during encoding one partition
		//      include cases which part num less than thread num
		iThreadIndex := f.rand() % int32(pCtx.iActiveThreadsNum)
		iPartitionNum := f.rand()%int32(pCtx.iActiveThreadsNum) + 1 //include cases which part num less than thread num
		iSlcBufferNum := pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum

		f.SetParamForReallocateTest(pCtx, iLayerIdx, iThreadIndex, iPartitionNum)
		iRet := ReallocateSliceInThread(pCtx, pCtx.pCurDqLayer, iLayerIdx, iThreadIndex)
		if iRet != ENC_RETURN_SUCCESS {
			t.Errorf("ReallocateSliceInThread: %d", iRet)
		}
		if nil == pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].pSliceBuffer {
			t.Error("pSliceBuffer is NULL")
		}
		if !(iSlcBufferNum < pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum) {
			t.Errorf("slice buffer not extended: %d -> %d", iSlcBufferNum, pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum)
		}
		if int32(len(pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].pSliceBuffer)) != pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum {
			t.Error("slice buffer length mismatch")
		}
	}
}

func TestSliceBufferReallocate_Reallocate_in_one_thread(t *testing.T) {
	for iRepeat := int64(0); iRepeat < sbrTestRepeat; iRepeat++ {
		f := newSliceBufferReallocTest(t, 200+iRepeat)
		pCtx := &f.m_EncContext
		iLayerIdx := int32(0)

		if iRet := f.InitParamForSizeLimitSlcModeCase(iLayerIdx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitParamForSizeLimitSlcModeCase: %d", iRet)
		}
		pCtx.pCurDqLayer = pCtx.ppDqLayerList[iLayerIdx]
		if iRet := InitAllSlicesInThread(pCtx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitAllSlicesInThread: %d", iRet)
		}

		//case: all partitions encoded by one thread
		iThreadIndex := f.rand() % int32(pCtx.iActiveThreadsNum)
		iPartitionNum := int32(pCtx.iActiveThreadsNum)

		f.SetParamForReallocateTest(pCtx, iLayerIdx, iThreadIndex, iPartitionNum)

		for iPartIdx := int32(0); iPartIdx < iPartitionNum; iPartIdx++ {
			iSlcBufferNum := pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum

			iRet := ReallocateSliceInThread(pCtx, pCtx.pCurDqLayer, iLayerIdx, iThreadIndex)
			if iRet != ENC_RETURN_SUCCESS {
				t.Errorf("ReallocateSliceInThread: %d", iRet)
			}
			if nil == pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].pSliceBuffer {
				t.Error("pSliceBuffer is NULL")
			}
			if !(iSlcBufferNum < pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum) {
				t.Errorf("slice buffer not extended: %d -> %d", iSlcBufferNum, pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIndex].iMaxSliceNum)
			}
		}
	}
}

func TestSliceBufferReallocate_ExtendLayerBufferTest(t *testing.T) {
	for iRepeat := int64(0); iRepeat < sbrTestRepeat; iRepeat++ {
		f := newSliceBufferReallocTest(t, 300+iRepeat)
		pCtx := &f.m_EncContext
		iLayerIdx := int32(0)
		iMaxSliceNumNew := int32(0)

		if iRet := f.InitParamForSizeLimitSlcModeCase(iLayerIdx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitParamForSizeLimitSlcModeCase: %d", iRet)
		}
		pCtx.pCurDqLayer = pCtx.ppDqLayerList[iLayerIdx]
		if iRet := InitAllSlicesInThread(pCtx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitAllSlicesInThread: %d", iRet)
		}

		//before extend, simulate reallocate slice buffer in one thread
		iReallocateThrdIdx := f.rand() % int32(pCtx.iActiveThreadsNum)
		iSlcBuffNumInThrd := pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].iMaxSliceNum
		pSlcListInThrd := pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].pSliceBuffer

		iRet := ReallocateSliceList(pCtx, &pCtx.pSvcParam.SSpatialLayers[iLayerIdx].SSliceArgument,
			&pSlcListInThrd, iSlcBuffNumInThrd, iSlcBuffNumInThrd*2)
		if iRet != ENC_RETURN_SUCCESS || nil == pSlcListInThrd {
			t.Fatalf("ReallocateSliceList: %d", iRet)
		}
		pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].pSliceBuffer = pSlcListInThrd
		pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].iMaxSliceNum = iSlcBuffNumInThrd * 2

		for iThreadIdx := int32(0); iThreadIdx < int32(pCtx.iActiveThreadsNum); iThreadIdx++ {
			iMaxSliceNumNew += pCtx.pCurDqLayer.sSliceBufferInfo[iThreadIdx].iMaxSliceNum
		}

		iRet = ExtendLayerBuffer(pCtx, pCtx.pCurDqLayer.iMaxSliceNum, iMaxSliceNumNew)
		if iRet != ENC_RETURN_SUCCESS {
			t.Errorf("ExtendLayerBuffer: %d", iRet)
		}
		if nil == pCtx.pCurDqLayer.ppSliceInLayer || nil == pCtx.pCurDqLayer.pFirstMbIdxOfSlice ||
			nil == pCtx.pCurDqLayer.pCountMbNumInSlice {
			t.Error("layer buffers are NULL")
		}
		if int32(len(pCtx.pCurDqLayer.ppSliceInLayer)) != iMaxSliceNumNew {
			t.Errorf("ppSliceInLayer has %d entries, want %d", len(pCtx.pCurDqLayer.ppSliceInLayer), iMaxSliceNumNew)
		}
	}
}

func TestSliceBufferReallocate_FrameBsReallocateTest(t *testing.T) {
	for iRepeat := int64(0); iRepeat < sbrTestRepeat; iRepeat++ {
		f := newSliceBufferReallocTest(t, 400+iRepeat)
		pCtx := &f.m_EncContext
		iLayerIdx := int32(0)
		var FrameBsInfo api.SFrameBSInfo
		iCurLayerIdx := f.rand() % api.MAX_LAYER_NUM_OF_FRAME

		if iRet := f.InitParamForTestCase(iLayerIdx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitParamForTestCase: %d", iRet)
		}

		//init for FrameBs and LayerBs
		pCtx.iPosBsBuffer = f.rand()%pCtx.iFrameBsSize + 1
		pLayerBsInfo := &FrameBsInfo.SLayerInfo[iCurLayerIdx]
		pLayerBsInfo.PBsBuf = pCtx.pFrameBs[pCtx.iPosBsBuffer:]
		pCtx.bNeedPrefixNalFlag = f.rand()%2 == 1

		iCodedNalCount := pCtx.pOut.iCountNals
		iRet := FrameBsRealloc(pCtx, &FrameBsInfo, pLayerBsInfo, iCodedNalCount)

		if iRet != ENC_RETURN_SUCCESS {
			t.Errorf("FrameBsRealloc: %d", iRet)
		}
		if !(iCodedNalCount < pCtx.pOut.iCountNals) {
			t.Errorf("iCountNals not extended: %d -> %d", iCodedNalCount, pCtx.pOut.iCountNals)
		}
		if nil == pCtx.pOut.sNalList || nil == pCtx.pOut.pNalLen ||
			int32(len(pCtx.pOut.sNalList)) != pCtx.pOut.iCountNals || int32(len(pCtx.pOut.pNalLen)) != pCtx.pOut.iCountNals {
			t.Error("NAL list not reallocated")
		}
		if len(pLayerBsInfo.PNalLengthInByte) != len(pCtx.pOut.pNalLen) {
			t.Error("pNalLengthInByte of the current layer not rebased")
		}
	}
}

func TestSliceBufferReallocate_ReorderTest(t *testing.T) {
	for iRepeat := int64(0); iRepeat < sbrTestRepeat; iRepeat++ {
		f := newSliceBufferReallocTest(t, 500+iRepeat)
		pCtx := &f.m_EncContext
		iLayerIdx := int32(0)
		pSliceArgument := &pCtx.pSvcParam.SSpatialLayers[iLayerIdx].SSliceArgument

		if iRet := f.InitParamForSizeLimitSlcModeCase(iLayerIdx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitParamForSizeLimitSlcModeCase: %d", iRet)
		}
		pCtx.pCurDqLayer = pCtx.ppDqLayerList[iLayerIdx]
		if iRet := InitAllSlicesInThread(pCtx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitAllSlicesInThread: %d", iRet)
		}

		f.SimulateSliceInOneLayer()

		iRet := ReOrderSliceInLayer(pCtx, pSliceArgument.UiSliceMode, int32(pCtx.iActiveThreadsNum))
		if iRet != ENC_RETURN_SUCCESS {
			t.Errorf("ReOrderSliceInLayer: %d", iRet)
		}

		iCodedSlcNum := pCtx.pCurDqLayer.sSliceEncCtx.iSliceNumInFrame
		iMaxSlicNum := pCtx.pCurDqLayer.iMaxSliceNum
		if !(iCodedSlcNum <= iMaxSlicNum) {
			t.Errorf("iCodedSlcNum %d > iMaxSlicNum %d", iCodedSlcNum, iMaxSlicNum)
		}
		for iSlcIdx := int32(0); iSlcIdx < iCodedSlcNum; iSlcIdx++ {
			if pCtx.pCurDqLayer.ppSliceInLayer[iSlcIdx] == nil || iSlcIdx != pCtx.pCurDqLayer.ppSliceInLayer[iSlcIdx].iSliceIdx {
				t.Fatalf("slice %d not reordered", iSlcIdx)
			}
		}
	}
}

func TestSliceBufferReallocate_LayerInfoUpdateTest(t *testing.T) {
	for iRepeat := int64(0); iRepeat < sbrTestRepeat; iRepeat++ {
		f := newSliceBufferReallocTest(t, 600+iRepeat)
		pCtx := &f.m_EncContext
		iLayerIdx := int32(0)
		var FrameBsInfo api.SFrameBSInfo

		if iRet := f.InitParamForSizeLimitSlcModeCase(iLayerIdx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitParamForSizeLimitSlcModeCase: %d", iRet)
		}
		pCtx.pCurDqLayer = pCtx.ppDqLayerList[iLayerIdx]
		if iRet := InitAllSlicesInThread(pCtx); iRet != ENC_RETURN_SUCCESS {
			t.Fatalf("InitAllSlicesInThread: %d", iRet)
		}

		f.SimulateSliceInOneLayer()

		//simulate reallocate slice buffer in one thread
		iReallocateThrdIdx := f.rand() % int32(pCtx.iActiveThreadsNum)
		iSlcBuffNumInThrd := pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].iMaxSliceNum
		iCodedSlcNumInThrd := pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].iCodedSliceNum
		pSlcListInThrd := pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].pSliceBuffer
		eSlcMode := pCtx.pSvcParam.SSpatialLayers[iLayerIdx].SSliceArgument.UiSliceMode
		iPartitionNum := int32(1)
		if api.SM_SIZELIMITED_SLICE == eSlcMode {
			iPartitionNum = int32(pCtx.iActiveThreadsNum)
		}
		iPartitionIdx := int32(0)
		if iCodedSlcNumInThrd > 0 {
			iPartitionIdx = pSlcListInThrd[iCodedSlcNumInThrd-1].iSliceIdx % int32(pCtx.iActiveThreadsNum)
		}

		iRet := ReallocateSliceList(pCtx, &pCtx.pSvcParam.SSpatialLayers[iLayerIdx].SSliceArgument,
			&pSlcListInThrd, iSlcBuffNumInThrd, iSlcBuffNumInThrd*2)
		if iRet != ENC_RETURN_SUCCESS || nil == pSlcListInThrd {
			t.Fatalf("ReallocateSliceList: %d", iRet)
		}
		pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].pSliceBuffer = pSlcListInThrd
		pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].iMaxSliceNum = iSlcBuffNumInThrd * 2

		//update reallocate slice idx/NalNum info
		for iSlcIdx := iCodedSlcNumInThrd; iSlcIdx < iSlcBuffNumInThrd*2; iSlcIdx++ {
			iSlcIdxInThrd := int32(0)
			if api.SM_SIZELIMITED_SLICE == eSlcMode {
				iSlcIdxInThrd = iPartitionIdx + pCtx.pCurDqLayer.NumSliceCodedOfPartition[iPartitionIdx]*iPartitionNum
				pCtx.pCurDqLayer.NumSliceCodedOfPartition[iPartitionIdx]++
			} else {
				iSlcIdxInThrd = pCtx.pCurDqLayer.sSliceEncCtx.iSliceNumInFrame
			}
			pSlcListInThrd[iSlcIdx].iSliceIdx = iSlcIdxInThrd
			pSlcListInThrd[iSlcIdx].sSliceBs.iNalIndex = f.rand()%2 + 1
			pSlcListInThrd[iSlcIdx].sSliceBs.uiBsPos = uint32(f.rand())%pSlcListInThrd[iSlcIdx].sSliceBs.uiSize + 1
			pCtx.pCurDqLayer.sSliceEncCtx.iSliceNumInFrame++
			pCtx.pCurDqLayer.sSliceBufferInfo[iReallocateThrdIdx].iCodedSliceNum++
		}

		//simulate for layer bs
		iCurLayerIdx := f.rand() % api.MAX_LAYER_NUM_OF_FRAME

		pCtx.iPosBsBuffer = f.rand()%pCtx.iFrameBsSize + 1
		pLayerBsInfo := &FrameBsInfo.SLayerInfo[iCurLayerIdx]
		pLayerBsInfo.PBsBuf = pCtx.pFrameBs[pCtx.iPosBsBuffer:]
		pCtx.bNeedPrefixNalFlag = f.rand()%2 == 1

		iRet = SliceLayerInfoUpdate(pCtx, &FrameBsInfo, pLayerBsInfo, eSlcMode)
		if iRet != ENC_RETURN_SUCCESS {
			t.Errorf("SliceLayerInfoUpdate: %d", iRet)
		}
	}
}
