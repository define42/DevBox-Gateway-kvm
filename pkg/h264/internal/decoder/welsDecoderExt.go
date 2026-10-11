// Port of codec/decoder/plus/src/welsDecoderExt.cpp.
//
// Cisco OpenH264 decoder extension utilization.
//
// The Go port is single-threaded: m_iThreadCount always stays 0, so every
// `m_iThreadCount >= 1` / `GetThreadCount (pCtx) > 1` branch is unreachable.
// The thread procedures (pThrProcInit, pThrProcFrame) and the file-local
// static ConstructAccessUnit (CWelsDecoder*, PWelsDecoderThreadCTX) used only
// by them are not ported (the latter would also clash with decoder_core's
// exported ConstructAccessUnit). The threaded member functions are kept, on
// top of the inert event/semaphore placeholders of wels_decoder_thread.go.

package decoder

import (
	"math"
	"reflect"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// VERSION_NUMBER (codec/common/inc/version.h, non-generated build).
const VERSION_NUMBER = "openh264 default: 1.4"

// decExtIsNilOption reports whether a void* option argument is NULL (a nil
// interface or a typed nil pointer).
func decExtIsNilOption(pOption any) bool {
	if pOption == nil {
		return true
	}
	v := reflect.ValueOf(pOption)
	switch v.Kind() {
	case reflect.Pointer, reflect.Slice, reflect.Map, reflect.Func, reflect.Interface:
		return v.IsNil()
	}
	return false
}

// decExtGetIntOption reads `*((int*)pOption)` (also accepting *int, *uint32
// and *bool).
func decExtGetIntOption(pOption any) (int32, bool) {
	switch p := pOption.(type) {
	case *int32:
		if p != nil {
			return *p, true
		}
	case *int:
		if p != nil {
			return int32(*p), true
		}
	case *uint32:
		if p != nil {
			return int32(*p), true
		}
	case *bool:
		if p != nil {
			if *p {
				return 1, true
			}
			return 0, true
		}
	}
	return 0, false
}

// decExtSetIntOption performs `*((int*)pOption) = iVal` (also accepting
// *int and *uint32).
func decExtSetIntOption(pOption any, iVal int32) bool {
	switch p := pOption.(type) {
	case *int32:
		if p != nil {
			*p = iVal
			return true
		}
	case *int:
		if p != nil {
			*p = int(iVal)
			return true
		}
	case *uint32:
		if p != nil {
			*p = uint32(iVal)
			return true
		}
	}
	return false
}

// CWelsDecoder::CWelsDecoder (void)
//
// C++ constructor CWelsDecoder::CWelsDecoder().
// class CWelsDecoder constructor function, do initialization and
// alloc memory required
func NewCWelsDecoder() *CWelsDecoder {
	d := &CWelsDecoder{
		m_pWelsTrace:         nil,
		m_uiDecodeTimeStamp:  0,
		m_bIsBaseline:        false,
		m_iCpuCount:          1,
		m_iThreadCount:       0,
		m_iCtxCount:          1,
		m_pPicBuff:           nil,
		m_bParamSetsLostFlag: false,
		m_bFreezeOutput:      false,
		m_DecCtxActiveCount:  0,
		m_pDecThrCtx:         nil,
		m_pLastDecThrCtx:     nil,
		m_iLastBufferedIdx:   0,
		m_iStreamSeqNum:      0,
	}
	d.m_sReoderingStatus = SPictReoderingStatus{}
	d.m_sReoderingStatus.iMinPOC = IMinInt32
	for i := 0; i < 16; i++ {
		d.m_sPictInfoList[i] = SPictInfo{}
		d.m_sPictInfoList[i].iPOC = IMinInt32
	}

	d.m_pWelsTrace = common.NewWelsCodecTrace()
	if d.m_pWelsTrace != nil {
		d.m_pWelsTrace.SetCodecInstance(d)
		d.m_pWelsTrace.SetTraceLevel(api.WELS_LOG_ERROR)

		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "CWelsDecoder::CWelsDecoder() entry")
	}

	ResetReorderingPictureBuffers(&d.m_sReoderingStatus, d.m_sPictInfoList[:], true)

	d.m_iCpuCount = GetCPUCount()
	if d.m_iCpuCount > WELS_DEC_MAX_NUM_CPU {
		d.m_iCpuCount = WELS_DEC_MAX_NUM_CPU
	}

	d.m_pDecThrCtx = make([]SWelsDecoderThreadCTX, d.m_iCtxCount)
	for i := 0; i < WELS_DEC_MAX_NUM_CPU; i++ {
		d.m_pDecThrCtxActive[i] = nil
	}
	return d
}

// CWelsDecoder::~CWelsDecoder()
//
// C++ destructor CWelsDecoder::~CWelsDecoder().
// class CWelsDecoder destructor function, destroy allocced memory
func (d *CWelsDecoder) Destroy() {
	if d.m_pWelsTrace != nil {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "CWelsDecoder::~CWelsDecoder()")
	}
	d.CloseDecoderThreads()
	d.UninitDecoder()

	if d.m_pWelsTrace != nil {
		d.m_pWelsTrace = nil
	}
	if d.m_pDecThrCtx != nil {
		d.m_pDecThrCtx = nil
	}
}

// long CWelsDecoder::Initialize (const SDecodingParam* pParam)
func (d *CWelsDecoder) Initialize(pParam *api.SDecodingParam) int32 {
	var iRet int32 = ERR_NONE
	if d.m_pWelsTrace == nil {
		return int32(api.CmMallocMemeError)
	}

	if pParam == nil {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsDecoder::Initialize(), invalid input argument.")
		return int32(api.CmInitParaError)
	}

	// H.264 decoder initialization,including memory allocation,then open it ready to decode
	iRet = d.InitDecoder(pParam)
	if iRet != 0 {
		return iRet
	}

	return int32(api.CmResultSuccess)
}

// long CWelsDecoder::Uninitialize()
func (d *CWelsDecoder) Uninitialize() int32 {
	d.UninitDecoder()

	return ERR_NONE
}

// void CWelsDecoder::UninitDecoder (void)
func (d *CWelsDecoder) UninitDecoder() {
	for i := int32(0); i < d.m_iCtxCount; i++ {
		if d.m_pDecThrCtx[i].pCtx != nil {
			if i > 0 {
				WelsResetRefPicWithoutUnRef(d.m_pDecThrCtx[i].pCtx)
			}
			d.UninitDecoderCtx(&d.m_pDecThrCtx[i].pCtx)
		}
	}
}

// void CWelsDecoder::OpenDecoderThreads()
//
// No-op in practice: m_iThreadCount is always 0 in the Go port. Only the
// inert placeholder state is initialised; no thread is created.
func (d *CWelsDecoder) OpenDecoderThreads() {
	if d.m_iThreadCount >= 1 {
		d.m_uiDecodeTimeStamp = 0
		SemCreate(&d.m_sIsBusy, d.m_iThreadCount, d.m_iThreadCount)
		EventCreate(&d.m_sBufferingEvent, 1, 0)
		EventPost(&d.m_sBufferingEvent)
		EventCreate(&d.m_sReleaseBufferEvent, 1, 0)
		EventPost(&d.m_sReleaseBufferEvent)
		for i := int32(0); i < d.m_iThreadCount; i++ {
			d.m_pDecThrCtx[i].sThreadInfo.uiThrMaxNum = uint32(d.m_iThreadCount)
			d.m_pDecThrCtx[i].sThreadInfo.uiThrNum = uint32(i)
			d.m_pDecThrCtx[i].sThreadInfo.uiThrStackSize = WELS_DEC_MAX_THREAD_STACK_SIZE
			d.m_pDecThrCtx[i].sThreadInfo.pThrProcMain = nil // pThrProcFrame is not ported
			d.m_pDecThrCtx[i].sThreadInfo.sIsBusy = &d.m_sIsBusy
			d.m_pDecThrCtx[i].sThreadInfo.uiCommand = WELS_DEC_THREAD_COMMAND_RUN
			d.m_pDecThrCtx[i].threadCtxOwner = d
			d.m_pDecThrCtx[i].kpSrc = nil
			d.m_pDecThrCtx[i].kiSrcLen = 0
			d.m_pDecThrCtx[i].ppDst = nil
			d.m_pDecThrCtx[i].pDec = nil
			EventCreate(&d.m_pDecThrCtx[i].sImageReady, 1, 0)
			EventCreate(&d.m_pDecThrCtx[i].sSliceDecodeStart, 1, 0)
			EventCreate(&d.m_pDecThrCtx[i].sSliceDecodeFinish, 1, 0)
			SemCreate(&d.m_pDecThrCtx[i].sThreadInfo.sIsIdle, 0, 1)
			SemCreate(&d.m_pDecThrCtx[i].sThreadInfo.sIsActivated, 0, 1)
			ThreadCreate(&d.m_pDecThrCtx[i].sThreadInfo.sThrHandle, nil, &d.m_pDecThrCtx[i])
		}
	}
}

// void CWelsDecoder::CloseDecoderThreads()
//
// No-op in practice: m_iThreadCount is always 0 in the Go port.
func (d *CWelsDecoder) CloseDecoderThreads() {
	if d.m_iThreadCount >= 1 {
		EventPost(&d.m_sReleaseBufferEvent)
		for i := int32(0); i < d.m_iThreadCount; i++ { //waiting the completion begun slices
			SemWait(&d.m_pDecThrCtx[i].sThreadInfo.sIsIdle, WELS_DEC_THREAD_WAIT_INFINITE)
			d.m_pDecThrCtx[i].sThreadInfo.uiCommand = WELS_DEC_THREAD_COMMAND_ABORT
			SemRelease(&d.m_pDecThrCtx[i].sThreadInfo.sIsActivated, nil)
			ThreadWait(&d.m_pDecThrCtx[i].sThreadInfo.sThrHandle)
			EventDestroy(&d.m_pDecThrCtx[i].sImageReady)
			EventDestroy(&d.m_pDecThrCtx[i].sSliceDecodeStart)
			EventDestroy(&d.m_pDecThrCtx[i].sSliceDecodeFinish)
			SemDestroy(&d.m_pDecThrCtx[i].sThreadInfo.sIsIdle)
			SemDestroy(&d.m_pDecThrCtx[i].sThreadInfo.sIsActivated)
		}
		EventDestroy(&d.m_sBufferingEvent)
		EventDestroy(&d.m_sReleaseBufferEvent)
		SemDestroy(&d.m_sIsBusy)
	}
}

// void CWelsDecoder::UninitDecoderCtx (PWelsDecoderContext& pCtx)
//
// C++ PWelsDecoderContext& -> **SWelsDecoderContext.
func (d *CWelsDecoder) UninitDecoderCtx(pCtx **SWelsDecoderContext) {
	if *pCtx != nil {

		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "CWelsDecoder::UninitDecoderCtx(), openh264 codec version = %s.",
			VERSION_NUMBER)

		WelsEndDecoder(*pCtx)

		// pCtx->pMemAlign (CMemoryAlign) is not ported.

		if nil != *pCtx {
			*pCtx = nil
		}
		if d.m_iCtxCount <= 1 {
			d.m_pDecThrCtx[0].pCtx = nil
		}
	}
}

// int32_t CWelsDecoder::InitDecoder (const SDecodingParam* pParam)
//
// the return value of this function is not suitable, it need report failure info to upper layer.
func (d *CWelsDecoder) InitDecoder(pParam *api.SDecodingParam) int32 {

	bParseOnly := int32(0)
	if pParam.BParseOnly {
		bParseOnly = 1
	}
	common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
		"CWelsDecoder::init_decoder(), openh264 codec version = %s, ParseOnly = %d",
		VERSION_NUMBER, bParseOnly)
	if d.m_iThreadCount >= 1 && pParam.BParseOnly {
		d.m_iThreadCount = 0
	}
	d.OpenDecoderThreads()
	//reset decoder context
	d.m_sDecoderStatistics = api.SDecoderStatistics{}
	d.m_sLastDecPicInfo = SWelsLastDecPicInfo{}
	d.m_sVlcTable = SVlcTable{}
	d.UninitDecoder()
	WelsDecoderLastDecPicInfoDefaults(&d.m_sLastDecPicInfo)
	for i := int32(0); i < d.m_iCtxCount; i++ {
		d.InitDecoderCtx(&d.m_pDecThrCtx[i].pCtx, pParam)
		if d.m_iThreadCount >= 1 {
			d.m_pDecThrCtx[i].pCtx.pThreadCtx = &d.m_pDecThrCtx[i]
		}
	}
	d.m_bParamSetsLostFlag = false
	d.m_bFreezeOutput = false
	return int32(api.CmResultSuccess)
}

// int32_t CWelsDecoder::InitDecoderCtx (PWelsDecoderContext& pCtx, const SDecodingParam* pParam)
//
// C++ PWelsDecoderContext& -> **SWelsDecoderContext.
// the return value of this function is not suitable, it need report failure info to upper layer.
func (d *CWelsDecoder) InitDecoderCtx(pCtx **SWelsDecoderContext, pParam *api.SDecodingParam) int32 {

	bParseOnly := int32(0)
	if pParam.BParseOnly {
		bParseOnly = 1
	}
	common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
		"CWelsDecoder::init_decoder(), openh264 codec version = %s, ParseOnly = %d",
		VERSION_NUMBER, bParseOnly)

	//reset decoder context
	d.UninitDecoderCtx(pCtx)
	*pCtx = new(SWelsDecoderContext)
	ctx := *pCtx
	// CMemoryAlign (pCtx->pMemAlign) is not ported.
	if d.m_iCtxCount <= 1 {
		d.m_pDecThrCtx[0].pCtx = ctx
	}
	//fill in default value into context
	ctx.pLastDecPicInfo = &d.m_sLastDecPicInfo
	ctx.pDecoderStatistics = &d.m_sDecoderStatistics
	ctx.pVlcTable = &d.m_sVlcTable
	ctx.pPictInfoList = d.m_sPictInfoList[:]
	ctx.pPictReoderingStatus = &d.m_sReoderingStatus
	// pCtx->pCsDecoder (mutex) is dropped.
	ctx.pStreamSeqNum = &d.m_iStreamSeqNum
	WelsDecoderDefaults(ctx, &d.m_pWelsTrace.M_sLogCtx)
	WelsDecoderSpsPpsDefaults(&ctx.sSpsPpsCtx)
	//check param and update decoder context
	ctx.pParam = new(api.SDecodingParam)
	iRet := DecoderConfigParam(ctx, pParam)
	if iRet != int32(api.CmResultSuccess) {
		return iRet
	}

	//init decoder
	if WelsInitDecoder(ctx, &d.m_pWelsTrace.M_sLogCtx) != 0 {
		d.UninitDecoderCtx(pCtx)
		return int32(api.CmMallocMemeError)
	}
	ctx.pPicBuff = nil
	return int32(api.CmResultSuccess)
}

// int32_t CWelsDecoder::ResetDecoder (PWelsDecoderContext& pCtx)
//
// C++ PWelsDecoderContext& -> **SWelsDecoderContext.
func (d *CWelsDecoder) ResetDecoder(pCtx **SWelsDecoderContext) int32 {
	// TBC: need to be modified when context and trace point are null
	if d.m_iThreadCount >= 1 {
		d.ThreadResetDecoder(pCtx)
	} else {
		if *pCtx != nil && d.m_pWelsTrace != nil {
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "ResetDecoder(), context error code is %d",
				(*pCtx).iErrorCode)
			sPrevParam := *(*pCtx).pParam

			if d.InitDecoderCtx(pCtx, &sPrevParam) != 0 {
				d.UninitDecoderCtx(pCtx)
				return int32(api.CmInitParaError)
			}
		} else if d.m_pWelsTrace != nil {
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "ResetDecoder() failed as decoder context null")
		}
		ResetReorderingPictureBuffers(&d.m_sReoderingStatus, d.m_sPictInfoList[:], false)
	}
	return ERR_INFO_UNINIT
}

// int32_t CWelsDecoder::ThreadResetDecoder (PWelsDecoderContext& pCtx)
//
// C++ PWelsDecoderContext& -> **SWelsDecoderContext.
func (d *CWelsDecoder) ThreadResetDecoder(pCtx **SWelsDecoderContext) int32 {
	// TBC: need to be modified when context and trace point are null
	var sPrevParam api.SDecodingParam
	if *pCtx != nil && d.m_pWelsTrace != nil {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "ResetDecoder(), context error code is %d", (*pCtx).iErrorCode)
		sPrevParam = *(*pCtx).pParam
		ResetReorderingPictureBuffers(&d.m_sReoderingStatus, d.m_sPictInfoList[:], true)
		d.CloseDecoderThreads()
		d.UninitDecoder()
		d.InitDecoder(&sPrevParam)
	} else if d.m_pWelsTrace != nil {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "ResetDecoder() failed as decoder context null")
	}
	return ERR_INFO_UNINIT
}

// long CWelsDecoder::SetOption (DECODER_OPTION eOptID, void* pOption)
//
// pOption: see the option table in package api.
func (d *CWelsDecoder) SetOption(eOptID api.DECODER_OPTION, pOption any) int32 {
	var iVal int32
	if eOptID == api.DECODER_OPTION_NUM_OF_THREADS {
		if !decExtIsNilOption(pOption) {
			threadCount, _ := decExtGetIntOption(pOption)
			if threadCount < 0 {
				threadCount = 0
			}
			if threadCount > d.m_iCpuCount {
				threadCount = d.m_iCpuCount
			}
			if threadCount > 3 {
				threadCount = 3
			}
			// The Go port is single-threaded: the request is accepted but
			// decoding always runs sequentially (m_iThreadCount stays 0),
			// which yields the same output as any thread count in C.
			threadCount = 0
			if threadCount != d.m_iThreadCount {
				d.m_iThreadCount = threadCount
				if d.m_pDecThrCtx != nil {
					if d.m_iThreadCount == 0 {
						d.m_iCtxCount = 1
					} else {
						d.m_iCtxCount = d.m_iThreadCount
					}
					d.m_pDecThrCtx = make([]SWelsDecoderThreadCTX, d.m_iCtxCount)
				}
			}
		}
		return int32(api.CmResultSuccess)
	}
	for i := int32(0); i < d.m_iCtxCount; i++ {
		pDecContext := d.m_pDecThrCtx[i].pCtx
		if pDecContext == nil && eOptID != api.DECODER_OPTION_TRACE_LEVEL &&
			eOptID != api.DECODER_OPTION_TRACE_CALLBACK && eOptID != api.DECODER_OPTION_TRACE_CALLBACK_CONTEXT {
			return int32(api.DsInitialOptExpected)
		}
		if eOptID == api.DECODER_OPTION_END_OF_STREAM { // Indicate bit-stream of the final frame to be decoded
			if decExtIsNilOption(pOption) {
				return int32(api.CmInitParaError)
			}

			iVal, _ = decExtGetIntOption(pOption) // boolean value for whether enabled End Of Stream flag

			if pDecContext == nil {
				return int32(api.DsInitialOptExpected)
			}

			pDecContext.bEndOfStreamFlag = iVal != 0
			if iVal != 0 && d.m_iThreadCount >= 1 {
				EventPost(&d.m_sReleaseBufferEvent)
			}

			return int32(api.CmResultSuccess)
		} else if eOptID == api.DECODER_OPTION_ERROR_CON_IDC { // Indicate error concealment status
			if decExtIsNilOption(pOption) {
				return int32(api.CmInitParaError)
			}

			if pDecContext == nil {
				return int32(api.DsInitialOptExpected)
			}

			iVal, _ = decExtGetIntOption(pOption) // int value for error concealment idc
			iVal = common.WELS_CLIP3(iVal, int32(api.ERROR_CON_DISABLE), int32(api.ERROR_CON_SLICE_MV_COPY_CROSS_IDR_FREEZE_RES_CHANGE))
			if (pDecContext.pParam.BParseOnly) && (iVal != int32(api.ERROR_CON_DISABLE)) {
				common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
					"CWelsDecoder::SetOption for ERROR_CON_IDC = %d not allowd for parse only!.", iVal)
				return int32(api.CmInitParaError)
			}

			pDecContext.pParam.EEcActiveIdc = api.ERROR_CON_IDC(iVal)
			InitErrorCon(pDecContext)
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
				"CWelsDecoder::SetOption for ERROR_CON_IDC = %d.", iVal)

			return int32(api.CmResultSuccess)
		} else if eOptID == api.DECODER_OPTION_TRACE_LEVEL {
			if d.m_pWelsTrace != nil {
				if level, ok := decExtGetIntOption(pOption); ok {
					d.m_pWelsTrace.SetTraceLevel(level)
				}
			}
			return int32(api.CmResultSuccess)
		} else if eOptID == api.DECODER_OPTION_TRACE_CALLBACK {
			if d.m_pWelsTrace != nil {
				var callback api.WelsTraceCallback
				switch p := pOption.(type) {
				case *api.WelsTraceCallback:
					if p != nil {
						callback = *p
					}
				case api.WelsTraceCallback:
					callback = p
				case func(any, int32, string):
					callback = p
				}
				d.m_pWelsTrace.SetTraceCallback(callback)
				common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
					"CWelsDecoder::SetOption():DECODER_OPTION_TRACE_CALLBACK callback = %p.",
					callback)
			}
			return int32(api.CmResultSuccess)
		} else if eOptID == api.DECODER_OPTION_TRACE_CALLBACK_CONTEXT {
			if d.m_pWelsTrace != nil {
				var ctx any
				if p, ok := pOption.(*any); ok && p != nil {
					ctx = *p
				} else {
					ctx = pOption
				}
				d.m_pWelsTrace.SetTraceCallbackContext(ctx)
			}
			return int32(api.CmResultSuccess)
		} else if eOptID == api.DECODER_OPTION_GET_STATISTICS {
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING,
				"CWelsDecoder::SetOption():DECODER_OPTION_GET_STATISTICS: this option is get-only!")
			return int32(api.CmInitParaError)
		} else if eOptID == api.DECODER_OPTION_STATISTICS_LOG_INTERVAL {
			if !decExtIsNilOption(pOption) {
				if pDecContext == nil {
					return int32(api.DsInitialOptExpected)
				}
				v, _ := decExtGetIntOption(pOption)
				pDecContext.pDecoderStatistics.IStatisticsLogInterval = uint32(v)
				return int32(api.CmResultSuccess)
			}
		} else if eOptID == api.DECODER_OPTION_GET_SAR_INFO {
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING,
				"CWelsDecoder::SetOption():DECODER_OPTION_GET_SAR_INFO: this option is get-only!")
			return int32(api.CmInitParaError)
		}
	}
	return int32(api.CmInitParaError)
}

// long CWelsDecoder::GetOption (DECODER_OPTION eOptID, void* pOption)
//
// pOption: see the option table in package api.
func (d *CWelsDecoder) GetOption(eOptID api.DECODER_OPTION, pOption any) int32 {
	var iVal int32
	// setInt mirrors `* ((int*)pOption) = iVal; return cmResultSuccess;`.
	setInt := func(v int32) int32 {
		if !decExtSetIntOption(pOption, v) {
			return int32(api.CmInitParaError)
		}
		return int32(api.CmResultSuccess)
	}
	if api.DECODER_OPTION_NUM_OF_THREADS == eOptID {
		return setInt(d.m_iThreadCount)
	}
	pDecContext := d.m_pDecThrCtx[0].pCtx
	if pDecContext == nil {
		return int32(api.CmInitExpected)
	}

	if decExtIsNilOption(pOption) {
		return int32(api.CmInitParaError)
	}

	if api.DECODER_OPTION_END_OF_STREAM == eOptID {
		iVal = 0
		if pDecContext.bEndOfStreamFlag {
			iVal = 1
		}
		return setInt(iVal)
	} else if api.DECODER_OPTION_IDR_PIC_ID == eOptID { // LONG_TERM_REF
		iVal = int32(pDecContext.uiCurIdrPicId)
		return setInt(iVal)
	} else if api.DECODER_OPTION_FRAME_NUM == eOptID {
		iVal = pDecContext.iFrameNum
		return setInt(iVal)
	} else if api.DECODER_OPTION_LTR_MARKING_FLAG == eOptID {
		iVal = 0
		if pDecContext.bCurAuContainLtrMarkSeFlag {
			iVal = 1
		}
		return setInt(iVal)
	} else if api.DECODER_OPTION_LTR_MARKED_FRAME_NUM == eOptID {
		iVal = pDecContext.iFrameNumOfAuMarkedLtr
		return setInt(iVal)
	} else if api.DECODER_OPTION_VCL_NAL == eOptID { //feedback whether or not have VCL NAL in current AU
		iVal = pDecContext.iFeedbackVclNalInAu
		return setInt(iVal)
	} else if api.DECODER_OPTION_TEMPORAL_ID == eOptID { //if have VCL NAL in current AU, then feedback the temporal ID
		iVal = pDecContext.iFeedbackTidInAu
		return setInt(iVal)
	} else if api.DECODER_OPTION_IS_REF_PIC == eOptID {
		iVal = pDecContext.iFeedbackNalRefIdc
		if iVal > 0 {
			iVal = 1
		}
		return setInt(iVal)
	} else if api.DECODER_OPTION_ERROR_CON_IDC == eOptID {
		iVal = int32(pDecContext.pParam.EEcActiveIdc)
		return setInt(iVal)
	} else if api.DECODER_OPTION_GET_STATISTICS == eOptID { // get decoder statistics info for real time debugging
		pDecoderStatistics, ok := pOption.(*api.SDecoderStatistics)
		if !ok {
			return int32(api.CmInitParaError)
		}

		*pDecoderStatistics = *pDecContext.pDecoderStatistics

		if pDecContext.pDecoderStatistics.UiDecodedFrameCount != 0 { //not original status
			pDecoderStatistics.FAverageFrameSpeedInMs = float32(pDecContext.dDecTime) /
				float32(pDecContext.pDecoderStatistics.UiDecodedFrameCount)
			pDecoderStatistics.FActualAverageFrameSpeedInMs = float32(pDecContext.dDecTime) /
				float32(pDecContext.pDecoderStatistics.UiDecodedFrameCount+pDecContext.pDecoderStatistics.UiFreezingIDRNum+
					pDecContext.pDecoderStatistics.UiFreezingNonIDRNum)
		}
		return int32(api.CmResultSuccess)
	} else if eOptID == api.DECODER_OPTION_STATISTICS_LOG_INTERVAL {
		if !decExtIsNilOption(pOption) {
			iVal = int32(pDecContext.pDecoderStatistics.IStatisticsLogInterval)
			return setInt(iVal)
		}
	} else if api.DECODER_OPTION_GET_SAR_INFO == eOptID { //get decoder SAR info in VUI
		pVuiSarInfo, ok := pOption.(*api.SVuiSarInfo)
		if !ok {
			return int32(api.CmInitParaError)
		}
		*pVuiSarInfo = api.SVuiSarInfo{}
		if pDecContext.pSps == nil {
			return int32(api.CmInitExpected)
		} else {
			pVuiSarInfo.UiSarWidth = pDecContext.pSps.sVui.uiSarWidth
			pVuiSarInfo.UiSarHeight = pDecContext.pSps.sVui.uiSarHeight
			pVuiSarInfo.BOverscanAppropriateFlag = pDecContext.pSps.sVui.bOverscanAppropriateFlag
			return int32(api.CmResultSuccess)
		}
	} else if api.DECODER_OPTION_PROFILE == eOptID {
		if pDecContext.pSps == nil {
			return int32(api.CmInitExpected)
		}
		iVal = int32(pDecContext.pSps.uiProfileIdc)
		return setInt(iVal)
	} else if api.DECODER_OPTION_LEVEL == eOptID {
		if pDecContext.pSps == nil {
			return int32(api.CmInitExpected)
		}
		iVal = int32(pDecContext.pSps.uiLevelIdc)
		return setInt(iVal)
	} else if api.DECODER_OPTION_NUM_OF_FRAMES_REMAINING_IN_BUFFER == eOptID {
		for activeThread := int32(0); activeThread < d.m_DecCtxActiveCount; activeThread++ {
			SemWait(&d.m_pDecThrCtxActive[activeThread].sThreadInfo.sIsIdle, WELS_DEC_THREAD_WAIT_INFINITE)
			SemRelease(&d.m_pDecThrCtxActive[activeThread].sThreadInfo.sIsIdle, nil)
		}
		return setInt(d.m_sReoderingStatus.iNumOfPicts)
	}

	return int32(api.CmInitParaError)
}

// DECODING_STATE CWelsDecoder::DecodeFrameNoDelay (const unsigned char* kpSrc, const int kiSrcLen,
// unsigned char** ppDst, SBufferInfo* pDstInfo)
func (d *CWelsDecoder) DecodeFrameNoDelay(kpSrc []byte, kiSrcLen int32, ppDst *[3][]byte, pDstInfo *api.SBufferInfo) api.DECODING_STATE {
	var iRet int32 = int32(api.DsErrorFree)
	if d.m_iThreadCount >= 1 {
		EventPost(&d.m_sReleaseBufferEvent)
		iRet = d.ThreadDecodeFrameInternal(kpSrc, kiSrcLen, ppDst, pDstInfo)
		iNumOfPicts := d.m_sReoderingStatus.iNumOfPicts
		if iNumOfPicts != 0 {
			EventWait(&d.m_sBufferingEvent, WELS_DEC_THREAD_WAIT_INFINITE)
			EventReset(&d.m_sBufferingEvent)
			EventReset(&d.m_sReleaseBufferEvent)
			bHasBSlice := d.m_sReoderingStatus.bHasBSlice
			iNumOfPicts = d.m_sReoderingStatus.iNumOfPicts
			if !bHasBSlice {
				if iNumOfPicts > 1 {
					d.ReleaseBufferedReadyPictureNoReorder(nil, ppDst, pDstInfo)
				}
			} else {
				d.ReleaseBufferedReadyPictureReorder(nil, ppDst, pDstInfo, false)
			}
		}
		return api.DECODING_STATE(iRet)
	}
	iRet = int32(d.DecodeFrame2(kpSrc, kiSrcLen, ppDst, pDstInfo))
	iRet |= int32(d.DecodeFrame2(nil, 0, ppDst, pDstInfo))
	return api.DECODING_STATE(iRet)
}

// DECODING_STATE CWelsDecoder::DecodeFrame2WithCtx (PWelsDecoderContext pDecContext, const unsigned
// char* kpSrc, const int kiSrcLen, unsigned char** ppDst, SBufferInfo* pDstInfo)
func (d *CWelsDecoder) DecodeFrame2WithCtx(pDecContext *SWelsDecoderContext, kpSrc []byte, kiSrcLen int32, ppDst *[3][]byte, pDstInfo *api.SBufferInfo) api.DECODING_STATE {
	if pDecContext == nil || pDecContext.pParam == nil {
		if d.m_pWelsTrace != nil {
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "Call DecodeFrame2 without Initialize.\n")
		}
		return api.DsInitialOptExpected
	}

	if pDecContext.pParam.BParseOnly {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "bParseOnly should be false for this API calling! \n")
		pDecContext.iErrorCode |= int32(api.DsInvalidArgument)
		return api.DsInvalidArgument
	}
	if CheckBsBuffer(pDecContext, kiSrcLen) != 0 {
		if d.ResetDecoder(&pDecContext) != 0 {
			if pDstInfo != nil {
				pDstInfo.IBufferStatus = 0
			}
			return api.DsOutOfMemory
		}
		return api.DsErrorFree
	}
	if kiSrcLen > 0 && kpSrc != nil {
		pDecContext.bEndOfStreamFlag = false
		if GetThreadCount(pDecContext) <= 0 {
			d.m_uiDecodeTimeStamp++
			pDecContext.uiDecodingTimeStamp = d.m_uiDecodeTimeStamp
		}
	} else {
		//For application MODE, the error detection should be added for safe.
		//But for CONSOLE MODE, when decoding LAST AU, kiSrcLen==0 && kpSrc==NULL.
		pDecContext.bEndOfStreamFlag = true
		pDecContext.bInstantDecFlag = true
	}

	var iStart, iEnd int64
	iStart = common.WelsTime()

	if GetThreadCount(pDecContext) <= 1 {
		ppDst[0], ppDst[1], ppDst[2] = nil, nil, nil
	}
	pDecContext.iErrorCode = int32(api.DsErrorFree)                   //initialize at the starting of AU decoding.
	pDecContext.iFeedbackVclNalInAu = int32(api.FEEDBACK_UNKNOWN_NAL) //initialize
	uiInBsTimeStamp := pDstInfo.UiInBsTimeStamp
	if GetThreadCount(pDecContext) <= 1 {
		*pDstInfo = api.SBufferInfo{}
	}
	pDstInfo.UiInBsTimeStamp = uiInBsTimeStamp
	// LONG_TERM_REF
	pDecContext.bReferenceLostAtT0Flag = false //initialize for LTR
	pDecContext.bCurAuContainLtrMarkSeFlag = false
	pDecContext.iFrameNumOfAuMarkedLtr = 0
	pDecContext.iFrameNum = -1 //initialize

	if GetThreadCount(pDecContext) >= 1 {
		EventWait(&d.m_sReleaseBufferEvent, WELS_DEC_THREAD_WAIT_INFINITE)
	}

	pDecContext.iFeedbackTidInAu = -1   //initialize
	pDecContext.iFeedbackNalRefIdc = -1 //initialize
	if pDstInfo != nil {
		pDstInfo.UiOutYuvTimeStamp = 0
		pDecContext.uiTimeStamp = pDstInfo.UiInBsTimeStamp
	} else {
		pDecContext.uiTimeStamp = 0
	}
	WelsDecodeBs(pDecContext, kpSrc, kiSrcLen, ppDst,
		pDstInfo, nil) //iErrorCode has been modified in this function
	pDecContext.bInstantDecFlag = false //reset no-delay flag
	if pDecContext.iErrorCode != 0 {
		var eNalType common.EWelsNalUnitType = common.NAL_UNIT_UNSPEC_0 //for NBR, IDR frames are expected to decode as followed if error decoding an IDR currently

		eNalType = pDecContext.sCurNalHead.ENalUnitType
		if pDecContext.iErrorCode&int32(api.DsOutOfMemory) != 0 {
			if d.ResetDecoder(&pDecContext) != 0 {
				if pDstInfo != nil {
					pDstInfo.IBufferStatus = 0
				}
				return api.DsOutOfMemory
			}
			return api.DsErrorFree
		}
		if pDecContext.iErrorCode&int32(api.DsRefListNullPtrs) != 0 {
			if d.ResetDecoder(&pDecContext) != 0 {
				if pDstInfo != nil {
					pDstInfo.IBufferStatus = 0
				}
				return api.DsRefListNullPtrs
			}
			return api.DsErrorFree
		}
		//for AVC bitstream (excluding AVC with temporal scalability, including TP), as long as error occur, SHOULD notify upper layer key frame loss.
		if (common.IS_PARAM_SETS_NALS(eNalType) || common.NAL_UNIT_CODED_SLICE_IDR == eNalType) ||
			(api.VIDEO_BITSTREAM_AVC == pDecContext.eVideoType) {
			if pDecContext.pParam.EEcActiveIdc == api.ERROR_CON_DISABLE {
				// LONG_TERM_REF
				pDecContext.bParamSetsLostFlag = true
			}
		}

		if pDecContext.bPrintFrameErrorTraceFlag {
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "decode failed, failure type:%d \n",
				pDecContext.iErrorCode)
			pDecContext.bPrintFrameErrorTraceFlag = false
		} else {
			pDecContext.iIgnoredErrorInfoPacketCount++
			if pDecContext.iIgnoredErrorInfoPacketCount == math.MaxInt32 {
				common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING, "continuous error reached INT_MAX! Restart as 0.")
				pDecContext.iIgnoredErrorInfoPacketCount = 0
			}
		}
		if (pDecContext.pParam.EEcActiveIdc != api.ERROR_CON_DISABLE) && (pDstInfo.IBufferStatus == 1) {
			//TODO after dec status updated
			pDecContext.iErrorCode |= int32(api.DsDataErrorConcealed)

			pStat := pDecContext.pDecoderStatistics
			pStat.UiDecodedFrameCount++
			if pStat.UiDecodedFrameCount == 0 { //exceed max value of uint32_t
				ResetDecStatNums(pStat)
				pStat.UiDecodedFrameCount++
			}
			iMbConcealedNum := pDecContext.iMbEcedNum + pDecContext.iMbEcedPropNum
			if pDecContext.iMbNum == 0 {
				pStat.UiAvgEcRatio = pStat.UiAvgEcRatio * pStat.UiEcFrameNum
			} else {
				pStat.UiAvgEcRatio = (pStat.UiAvgEcRatio * pStat.UiEcFrameNum) +
					uint32((iMbConcealedNum*100)/pDecContext.iMbNum)
			}
			if pDecContext.iMbNum == 0 {
				pStat.UiAvgEcPropRatio = pStat.UiAvgEcPropRatio * pStat.UiEcFrameNum
			} else {
				pStat.UiAvgEcPropRatio = (pStat.UiAvgEcPropRatio * pStat.UiEcFrameNum) +
					uint32((pDecContext.iMbEcedPropNum*100)/pDecContext.iMbNum)
			}
			if iMbConcealedNum != 0 {
				pStat.UiEcFrameNum++
			}
			if pStat.UiEcFrameNum == 0 {
				pStat.UiAvgEcRatio = 0
			} else {
				pStat.UiAvgEcRatio = pStat.UiAvgEcRatio / pStat.UiEcFrameNum
			}
			if pStat.UiEcFrameNum == 0 {
				pStat.UiAvgEcPropRatio = 0
			} else {
				pStat.UiAvgEcPropRatio = pStat.UiAvgEcPropRatio / pStat.UiEcFrameNum
			}
		}
		iEnd = common.WelsTime()
		pDecContext.dDecTime += float64(iEnd-iStart) / 1e3

		d.OutputStatisticsLog(pDecContext.pDecoderStatistics)
		if GetThreadCount(pDecContext) >= 1 {
			d.BufferingReadyPicture(pDecContext, ppDst, pDstInfo)
			EventPost(&d.m_sBufferingEvent)
		} else {
			d.ReorderPicturesInDisplay(pDecContext, ppDst, pDstInfo)
		}

		return api.DECODING_STATE(pDecContext.iErrorCode)
	}
	// else Error free, the current codec works well

	if pDstInfo.IBufferStatus == 1 {

		pDecContext.pDecoderStatistics.UiDecodedFrameCount++
		if pDecContext.pDecoderStatistics.UiDecodedFrameCount == 0 { //exceed max value of uint32_t
			ResetDecStatNums(pDecContext.pDecoderStatistics)
			pDecContext.pDecoderStatistics.UiDecodedFrameCount++
		}

		d.OutputStatisticsLog(pDecContext.pDecoderStatistics)
	}
	iEnd = common.WelsTime()
	pDecContext.dDecTime += float64(iEnd-iStart) / 1e3

	if GetThreadCount(pDecContext) >= 1 {
		d.BufferingReadyPicture(pDecContext, ppDst, pDstInfo)
		EventPost(&d.m_sBufferingEvent)
	} else {
		d.ReorderPicturesInDisplay(pDecContext, ppDst, pDstInfo)
	}
	return api.DsErrorFree
}

// DECODING_STATE CWelsDecoder::DecodeFrame2 (const unsigned char* kpSrc, const int kiSrcLen, unsigned
// char** ppDst, SBufferInfo* pDstInfo)
func (d *CWelsDecoder) DecodeFrame2(kpSrc []byte, kiSrcLen int32, ppDst *[3][]byte, pDstInfo *api.SBufferInfo) api.DECODING_STATE {
	pDecContext := d.m_pDecThrCtx[0].pCtx
	return d.DecodeFrame2WithCtx(pDecContext, kpSrc, kiSrcLen, ppDst, pDstInfo)
}

// DECODING_STATE CWelsDecoder::FlushFrame (unsigned char** ppDst, SBufferInfo* pDstInfo)
func (d *CWelsDecoder) FlushFrame(ppDst *[3][]byte, pDstInfo *api.SBufferInfo) api.DECODING_STATE {
	bEndOfStreamFlag := true
	if d.m_iThreadCount <= 1 {
		for j := int32(0); j < d.m_iCtxCount; j++ {
			if !d.m_pDecThrCtx[j].pCtx.bEndOfStreamFlag {
				bEndOfStreamFlag = false
			}
		}
	}
	iNumOfPicts := d.m_sReoderingStatus.iNumOfPicts
	bHasBSlice := d.m_sReoderingStatus.bHasBSlice
	if bEndOfStreamFlag && iNumOfPicts > 0 {
		if !bHasBSlice {
			d.ReleaseBufferedReadyPictureNoReorder(nil, ppDst, pDstInfo)
		} else {
			d.ReleaseBufferedReadyPictureReorder(nil, ppDst, pDstInfo, true)
		}
	}
	return api.DsErrorFree
}

// void CWelsDecoder::OutputStatisticsLog (SDecoderStatistics& sDecoderStatistics)
//
// C++ reference -> pointer.
func (d *CWelsDecoder) OutputStatisticsLog(sDecoderStatistics *api.SDecoderStatistics) {
	if (sDecoderStatistics.UiDecodedFrameCount > 0) && (sDecoderStatistics.IStatisticsLogInterval > 0) &&
		((sDecoderStatistics.UiDecodedFrameCount % sDecoderStatistics.IStatisticsLogInterval) == 0) {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"DecoderStatistics: uiWidth=%d, uiHeight=%d, fAverageFrameSpeedInMs=%.1f, fActualAverageFrameSpeedInMs=%.1f, "+
				"              uiDecodedFrameCount=%d, uiResolutionChangeTimes=%d, uiIDRCorrectNum=%d, "+
				"              uiAvgEcRatio=%d, uiAvgEcPropRatio=%d, uiEcIDRNum=%d, uiEcFrameNum=%d, "+
				"              uiIDRLostNum=%d, uiFreezingIDRNum=%d, uiFreezingNonIDRNum=%d, iAvgLumaQp=%d, "+
				"              iSpsReportErrorNum=%d, iSubSpsReportErrorNum=%d, iPpsReportErrorNum=%d, iSpsNoExistNalNum=%d, iSubSpsNoExistNalNum=%d, iPpsNoExistNalNum=%d, "+
				"              uiProfile=%d, uiLevel=%d, "+
				"              iCurrentActiveSpsId=%d, iCurrentActivePpsId=%d,",
			sDecoderStatistics.UiWidth,
			sDecoderStatistics.UiHeight,
			float64(sDecoderStatistics.FAverageFrameSpeedInMs),
			float64(sDecoderStatistics.FActualAverageFrameSpeedInMs),

			sDecoderStatistics.UiDecodedFrameCount,
			sDecoderStatistics.UiResolutionChangeTimes,
			sDecoderStatistics.UiIDRCorrectNum,

			sDecoderStatistics.UiAvgEcRatio,
			sDecoderStatistics.UiAvgEcPropRatio,
			sDecoderStatistics.UiEcIDRNum,
			sDecoderStatistics.UiEcFrameNum,

			sDecoderStatistics.UiIDRLostNum,
			sDecoderStatistics.UiFreezingIDRNum,
			sDecoderStatistics.UiFreezingNonIDRNum,
			sDecoderStatistics.IAvgLumaQp,

			sDecoderStatistics.ISpsReportErrorNum,
			sDecoderStatistics.ISubSpsReportErrorNum,
			sDecoderStatistics.IPpsReportErrorNum,
			sDecoderStatistics.ISpsNoExistNalNum,
			sDecoderStatistics.ISubSpsNoExistNalNum,
			sDecoderStatistics.IPpsNoExistNalNum,

			sDecoderStatistics.UiProfile,
			sDecoderStatistics.UiLevel,

			sDecoderStatistics.ICurrentActiveSpsId,
			sDecoderStatistics.ICurrentActivePpsId)
	}
}

// void CWelsDecoder::BufferingReadyPicture (PWelsDecoderContext pCtx, unsigned char** ppDst,
// SBufferInfo* pDstInfo)
func (d *CWelsDecoder) BufferingReadyPicture(pCtx *SWelsDecoderContext, ppDst *[3][]byte, pDstInfo *api.SBufferInfo) {
	if pDstInfo.IBufferStatus == 0 {
		return
	}
	d.m_bIsBaseline = pCtx.pSps.uiProfileIdc == 66 || pCtx.pSps.uiProfileIdc == 83
	if !d.m_bIsBaseline {
		if pCtx.pSliceHeader.eSliceType == common.B_SLICE {
			d.m_sReoderingStatus.bHasBSlice = true
		}
	}
	for i := int32(0); i < 16; i++ {
		if d.m_sPictInfoList[i].iPOC == IMinInt32 {
			d.m_sPictInfoList[i].sBufferInfo = *pDstInfo
			d.m_sPictInfoList[i].iPOC = pCtx.pSliceHeader.iPicOrderCntLsb
			d.m_sPictInfoList[i].iSeqNum = pCtx.iSeqNum
			d.m_sPictInfoList[i].uiDecodingTimeStamp = pCtx.uiDecodingTimeStamp
			if pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb != nil {
				var pPrevPic *SPicture
				if GetThreadCount(pCtx) > 1 && pCtx.pThreadCtx != nil {
					pPrevPic = pCtx.pThreadCtx.pPreviousDecodedPictureInDpb
				} else {
					pPrevPic = pCtx.pLastDecPicInfo.pPreviousDecodedPictureInDpb
				}
				if pPrevPic != nil {
					d.m_sPictInfoList[i].iPicBuffIdx = pPrevPic.iPicBuffIdx
					if GetThreadCount(pCtx) <= 1 {
						pPrevPic.iRefCount++
					}
				}
			}
			d.m_iLastBufferedIdx = i
			pDstInfo.IBufferStatus = 0
			d.m_sReoderingStatus.iNumOfPicts++
			if i > d.m_sReoderingStatus.iLargestBufferedPicIndex {
				d.m_sReoderingStatus.iLargestBufferedPicIndex = i
			}
			break
		}
	}
}

// void CWelsDecoder::ReleaseBufferedReadyPictureReorder (PWelsDecoderContext pCtx, unsigned char**
// ppDst, SBufferInfo* pDstInfo, bool isFlush)
//
// C++ default argument isFlush = false: callers pass false explicitly.
func (d *CWelsDecoder) ReleaseBufferedReadyPictureReorder(pCtx *SWelsDecoderContext, ppDst *[3][]byte, pDstInfo *api.SBufferInfo, isFlush bool) {
	var pPicBuff *SPicBuff
	if pCtx != nil {
		pPicBuff = pCtx.pPicBuff
	} else {
		pPicBuff = d.m_pPicBuff
	}
	if pCtx == nil && d.m_iThreadCount <= 1 {
		pCtx = d.m_pDecThrCtx[0].pCtx
	}
	rs := &d.m_sReoderingStatus
	if rs.iNumOfPicts > 0 {
		rs.iMinPOC = IMinInt32
		var firstValidIdx int32 = -1
		for i := int32(0); i <= rs.iLargestBufferedPicIndex; i++ {
			if rs.iMinPOC == IMinInt32 && d.m_sPictInfoList[i].iPOC > IMinInt32 {
				rs.iMinPOC = d.m_sPictInfoList[i].iPOC
				rs.iMinSeqNum = d.m_sPictInfoList[i].iSeqNum
				rs.iPictInfoIndex = i
				firstValidIdx = i
				break
			}
		}
		for i := int32(0); i <= rs.iLargestBufferedPicIndex; i++ {
			if i == firstValidIdx {
				continue
			}
			pi := &d.m_sPictInfoList[i]
			var bBetter bool
			if pi.iSeqNum == rs.iMinSeqNum {
				bBetter = pi.iPOC < rs.iMinPOC
			} else {
				bBetter = pi.iSeqNum-rs.iMinSeqNum < 0
			}
			if pi.iPOC > IMinInt32 && bBetter {
				rs.iMinPOC = pi.iPOC
				rs.iMinSeqNum = pi.iSeqNum
				rs.iPictInfoIndex = i
			}
		}
	}
	if rs.iMinPOC > IMinInt32 {
		isReady := true
		if !isFlush {
			var iLastPOC, iLastSeqNum int32
			if pCtx != nil {
				iLastPOC = pCtx.pSliceHeader.iPicOrderCntLsb
				iLastSeqNum = pCtx.iSeqNum
			} else {
				iLastPOC = d.m_sPictInfoList[d.m_iLastBufferedIdx].iPOC
				iLastSeqNum = d.m_sPictInfoList[d.m_iLastBufferedIdx].iSeqNum
			}
			isReady = (rs.iLastWrittenPOC > IMinInt32 &&
				rs.iMinPOC-rs.iLastWrittenPOC <= 1) ||
				rs.iMinPOC < iLastPOC ||
				rs.iMinSeqNum-iLastSeqNum < 0
		}
		if isReady {
			rs.iLastWrittenPOC = rs.iMinPOC
			rs.iLastWrittenSeqNum = rs.iMinSeqNum
			*pDstInfo = d.m_sPictInfoList[rs.iPictInfoIndex].sBufferInfo
			ppDst[0] = pDstInfo.PDst[0]
			ppDst[1] = pDstInfo.PDst[1]
			ppDst[2] = pDstInfo.PDst[2]
			d.m_sPictInfoList[rs.iPictInfoIndex].iPOC = IMinInt32
			iPicBuffIdx := d.m_sPictInfoList[rs.iPictInfoIndex].iPicBuffIdx
			if pPicBuff != nil {
				if iPicBuffIdx >= 0 && iPicBuffIdx < pPicBuff.iCapacity {
					pPic := pPicBuff.ppPic[iPicBuffIdx]
					pPic.iRefCount--
					if pPic.iRefCount <= 0 && pPic.pSetUnRef != nil {
						pPic.pSetUnRef(pPic)
					}
				}
			}
			rs.iMinPOC = IMinInt32
			rs.iNumOfPicts--
		}
	}
}

// void CWelsDecoder::ReleaseBufferedReadyPictureNoReorder(PWelsDecoderContext pCtx, unsigned char**
// ppDst, SBufferInfo* pDstInfo)
//
// if there is no b-frame, no ordering based on values of POCs is necessary.
// The function is added to force to avoid picture reordering because some h.264 streams do not follow
// H.264 POC specifications.
func (d *CWelsDecoder) ReleaseBufferedReadyPictureNoReorder(pCtx *SWelsDecoderContext, ppDst *[3][]byte, pDstInfo *api.SBufferInfo) {
	rs := &d.m_sReoderingStatus
	var firstValidIdx int32 = -1
	var uiDecodingTimeStamp uint32
	for i := int32(0); i <= rs.iLargestBufferedPicIndex; i++ {
		if d.m_sPictInfoList[i].iPOC != IMinInt32 {
			uiDecodingTimeStamp = d.m_sPictInfoList[i].uiDecodingTimeStamp
			rs.iPictInfoIndex = i
			firstValidIdx = i
			break
		}
	}
	for i := int32(0); i <= rs.iLargestBufferedPicIndex; i++ {
		if i == firstValidIdx {
			continue
		}
		if d.m_sPictInfoList[i].iPOC != IMinInt32 && d.m_sPictInfoList[i].uiDecodingTimeStamp < uiDecodingTimeStamp {
			uiDecodingTimeStamp = d.m_sPictInfoList[i].uiDecodingTimeStamp
			rs.iPictInfoIndex = i
		}
	}
	if uiDecodingTimeStamp > 0 {
		rs.iLastWrittenPOC = d.m_sPictInfoList[rs.iPictInfoIndex].iPOC
		rs.iLastWrittenSeqNum = d.m_sPictInfoList[rs.iPictInfoIndex].iSeqNum
		*pDstInfo = d.m_sPictInfoList[rs.iPictInfoIndex].sBufferInfo
		ppDst[0] = pDstInfo.PDst[0]
		ppDst[1] = pDstInfo.PDst[1]
		ppDst[2] = pDstInfo.PDst[2]
		d.m_sPictInfoList[rs.iPictInfoIndex].iPOC = IMinInt32
		if pCtx != nil || d.m_pPicBuff != nil {
			var pPicBuff *SPicBuff
			if pCtx != nil {
				pPicBuff = pCtx.pPicBuff
			} else {
				pPicBuff = d.m_pPicBuff
			}
			iPicBuffIdx := d.m_sPictInfoList[rs.iPictInfoIndex].iPicBuffIdx
			if pPicBuff != nil && iPicBuffIdx >= 0 && iPicBuffIdx < pPicBuff.iCapacity {
				pPic := pPicBuff.ppPic[iPicBuffIdx]
				pPic.iRefCount--
				if pPic.iRefCount <= 0 && pPic.pSetUnRef != nil {
					pPic.pSetUnRef(pPic)
				}
			}
		}
		rs.iNumOfPicts--
	}
}

// DECODING_STATE CWelsDecoder::ReorderPicturesInDisplay(PWelsDecoderContext pDecContext, unsigned
// char** ppDst, SBufferInfo* pDstInfo)
func (d *CWelsDecoder) ReorderPicturesInDisplay(pDecContext *SWelsDecoderContext, ppDst *[3][]byte, pDstInfo *api.SBufferInfo) api.DECODING_STATE {
	iRet := api.DsErrorFree
	if pDecContext.pSps != nil {
		d.m_bIsBaseline = pDecContext.pSps.uiProfileIdc == 66 || pDecContext.pSps.uiProfileIdc == 83
		if !d.m_bIsBaseline {
			if pDstInfo.IBufferStatus == 1 {
				var bInOrder bool
				if pDecContext.iSeqNum == d.m_sReoderingStatus.iLastWrittenSeqNum {
					bInOrder = pDecContext.pSliceHeader.iPicOrderCntLsb <= d.m_sReoderingStatus.iLastWrittenPOC+2
				} else {
					bInOrder = pDecContext.iSeqNum-d.m_sReoderingStatus.iLastWrittenSeqNum == 1 &&
						pDecContext.pSliceHeader.iPicOrderCntLsb == 0
				}
				if pDecContext.pSliceHeader.eSliceType == common.B_SLICE && bInOrder {
					d.m_sReoderingStatus.iLastWrittenPOC = pDecContext.pSliceHeader.iPicOrderCntLsb
					d.m_sReoderingStatus.iLastWrittenSeqNum = pDecContext.iSeqNum
					//issue #3478, use b-slice type to determine correct picture order as the first priority as POC order is not as reliable as based on b-slice
					ppDst[0] = pDstInfo.PDst[0]
					ppDst[1] = pDstInfo.PDst[1]
					ppDst[2] = pDstInfo.PDst[2]
					return iRet
				}
				d.BufferingReadyPicture(pDecContext, ppDst, pDstInfo)
				if !d.m_sReoderingStatus.bHasBSlice && d.m_sReoderingStatus.iNumOfPicts > 1 {
					d.ReleaseBufferedReadyPictureNoReorder(pDecContext, ppDst, pDstInfo)
				} else {
					d.ReleaseBufferedReadyPictureReorder(pDecContext, ppDst, pDstInfo, false)
				}
			}
		}
	}
	return iRet
}

// DECODING_STATE CWelsDecoder::DecodeParser (const unsigned char* kpSrc, const int kiSrcLen,
// SParserBsInfo* pDstInfo)
func (d *CWelsDecoder) DecodeParser(kpSrc []byte, kiSrcLen int32, pDstInfo *api.SParserBsInfo) api.DECODING_STATE {
	pDecContext := d.m_pDecThrCtx[0].pCtx

	if pDecContext == nil || pDecContext.pParam == nil {
		if d.m_pWelsTrace != nil {
			common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "Call DecodeParser without Initialize.\n")
		}
		return api.DsInitialOptExpected
	}

	if !pDecContext.pParam.BParseOnly {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "bParseOnly should be true for this API calling! \n")
		pDecContext.iErrorCode |= int32(api.DsInvalidArgument)
		return api.DsInvalidArgument
	}
	var iEnd int64
	iStart := common.WelsTime()
	if CheckBsBuffer(pDecContext, kiSrcLen) != 0 {
		if d.ResetDecoder(&pDecContext) != 0 {
			return api.DsOutOfMemory
		}

		return api.DsErrorFree
	}
	if kiSrcLen > 0 && kpSrc != nil {
		pDecContext.bEndOfStreamFlag = false
	} else {
		//For application MODE, the error detection should be added for safe.
		//But for CONSOLE MODE, when decoding LAST AU, kiSrcLen==0 && kpSrc==NULL.
		pDecContext.bEndOfStreamFlag = true
		pDecContext.bInstantDecFlag = true
	}

	pDecContext.iErrorCode = int32(api.DsErrorFree)         //initialize at the starting of AU decoding.
	pDecContext.pParam.EEcActiveIdc = api.ERROR_CON_DISABLE //add protection to disable EC here.
	pDecContext.iFeedbackNalRefIdc = -1                     //initialize
	if !pDecContext.bFramePending {                         //frame complete
		pDecContext.pParserBsInfo.INalNum = 0
		// C: memset (pNalLenInByte, 0, MAX_NAL_UNITS_IN_LAYER) clears
		// MAX_NAL_UNITS_IN_LAYER bytes, i.e. the first MAX_NAL_UNITS_IN_LAYER/4 ints.
		n := api.MAX_NAL_UNITS_IN_LAYER / 4
		if n > len(pDecContext.pParserBsInfo.PNalLenInByte) {
			n = len(pDecContext.pParserBsInfo.PNalLenInByte)
		}
		clear(pDecContext.pParserBsInfo.PNalLenInByte[:n])
	}
	pDstInfo.INalNum = 0
	pDstInfo.ISpsWidthInPixel = 0
	pDstInfo.ISpsHeightInPixel = 0
	if pDstInfo != nil {
		pDecContext.uiTimeStamp = pDstInfo.UiInBsTimeStamp
		pDstInfo.UiOutBsTimeStamp = 0
	} else {
		pDecContext.uiTimeStamp = 0
	}
	WelsDecodeBs(pDecContext, kpSrc, kiSrcLen, nil, nil, pDstInfo)
	if pDecContext.iErrorCode&int32(api.DsOutOfMemory) != 0 {
		if d.ResetDecoder(&pDecContext) != 0 {
			return api.DsOutOfMemory
		}
		return api.DsErrorFree
	}

	if !pDecContext.bFramePending && pDecContext.pParserBsInfo.INalNum != 0 {
		*pDstInfo = *pDecContext.pParserBsInfo

		if pDecContext.iErrorCode == ERR_NONE { //update statistics: decoding frame count
			pDecContext.pDecoderStatistics.UiDecodedFrameCount++
			if pDecContext.pDecoderStatistics.UiDecodedFrameCount == 0 { //exceed max value of uint32_t
				ResetDecStatNums(pDecContext.pDecoderStatistics)
				pDecContext.pDecoderStatistics.UiDecodedFrameCount++
			}
		}
	}

	pDecContext.bInstantDecFlag = false //reset no-delay flag

	if pDecContext.iErrorCode != 0 && pDecContext.bPrintFrameErrorTraceFlag {
		common.WelsLog(&d.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "decode failed, failure type:%d \n", pDecContext.iErrorCode)
		pDecContext.bPrintFrameErrorTraceFlag = false
	}
	iEnd = common.WelsTime()
	pDecContext.dDecTime += float64(iEnd-iStart) / 1e3
	return api.DECODING_STATE(pDecContext.iErrorCode)
}

// DECODING_STATE CWelsDecoder::DecodeFrame (const unsigned char* kpSrc, const int kiSrcLen, unsigned
// char** ppDst, int* pStride, int& iWidth, int& iHeight)
func (d *CWelsDecoder) DecodeFrame(kpSrc []byte, kiSrcLen int32, ppDst *[3][]byte, pStride *[2]int32, iWidth *int32, iHeight *int32) api.DECODING_STATE {
	eDecState := api.DsErrorFree
	var DstInfo api.SBufferInfo

	DstInfo.UsrData.SSystemBuffer.IStride[0] = pStride[0]
	DstInfo.UsrData.SSystemBuffer.IStride[1] = pStride[1]
	DstInfo.UsrData.SSystemBuffer.IWidth = *iWidth
	DstInfo.UsrData.SSystemBuffer.IHeight = *iHeight

	eDecState = d.DecodeFrame2(kpSrc, kiSrcLen, ppDst, &DstInfo)
	if eDecState == api.DsErrorFree {
		pStride[0] = DstInfo.UsrData.SSystemBuffer.IStride[0]
		pStride[1] = DstInfo.UsrData.SSystemBuffer.IStride[1]
		*iWidth = DstInfo.UsrData.SSystemBuffer.IWidth
		*iHeight = DstInfo.UsrData.SSystemBuffer.IHeight
	}

	return eDecState
}

// DECODING_STATE CWelsDecoder::DecodeFrameEx (const unsigned char* kpSrc, const int kiSrcLen, unsigned
// char* pDst, int iDstStride, int& iDstLen, int& iWidth, int& iHeight, int& iColorFormat)
func (d *CWelsDecoder) DecodeFrameEx(kpSrc []byte, kiSrcLen int32, pDst []byte, iDstStride int32, iDstLen *int32, iWidth *int32, iHeight *int32, iColorFormat *int32) api.DECODING_STATE {
	state := api.DsErrorFree

	return state
}

// DECODING_STATE CWelsDecoder::ParseAccessUnit (SWelsDecoderThreadCTX& sThreadCtx)
//
// Threaded path only (never reached in the Go port).
func (d *CWelsDecoder) ParseAccessUnit(sThreadCtx *SWelsDecoderThreadCTX) api.DECODING_STATE {
	sThreadCtx.pCtx.bHasNewSps = false
	sThreadCtx.pCtx.bParamSetsLostFlag = d.m_bParamSetsLostFlag
	sThreadCtx.pCtx.bFreezeOutput = d.m_bFreezeOutput
	d.m_uiDecodeTimeStamp++
	sThreadCtx.pCtx.uiDecodingTimeStamp = d.m_uiDecodeTimeStamp
	bPicBuffChanged := false
	if d.m_pLastDecThrCtx != nil && sThreadCtx.pCtx.sSpsPpsCtx.iSeqId < d.m_pLastDecThrCtx.pCtx.sSpsPpsCtx.iSeqId {
		CopySpsPps(d.m_pLastDecThrCtx.pCtx, sThreadCtx.pCtx)
		sThreadCtx.pCtx.iPicQueueNumber = d.m_pLastDecThrCtx.pCtx.iPicQueueNumber
		if sThreadCtx.pCtx.pPicBuff != d.m_pPicBuff {
			bPicBuffChanged = true
			sThreadCtx.pCtx.pPicBuff = d.m_pPicBuff
			sThreadCtx.pCtx.bHaveGotMemory = d.m_pPicBuff != nil
			sThreadCtx.pCtx.iImgWidthInPixel = d.m_pLastDecThrCtx.pCtx.iImgWidthInPixel
			sThreadCtx.pCtx.iImgHeightInPixel = d.m_pLastDecThrCtx.pCtx.iImgHeightInPixel
		}
	}

	//if threadCount > 1, then each thread must contain exact one complete frame.
	if GetThreadCount(sThreadCtx.pCtx) > 1 {
		sThreadCtx.pCtx.pAccessUnitList.uiAvailUnitsNum = 0
		sThreadCtx.pCtx.pAccessUnitList.uiActualUnitsNum = 0
	}

	iRet := int32(d.DecodeFrame2WithCtx(sThreadCtx.pCtx, sThreadCtx.kpSrc, sThreadCtx.kiSrcLen, sThreadCtx.ppDst,
		&sThreadCtx.sDstInfo))

	iErr := WelsDecodeInitAccessUnitStart(sThreadCtx.pCtx, &sThreadCtx.sDstInfo)
	if ERR_NONE != iErr {
		return api.DECODING_STATE(iRet | iErr)
	}
	if sThreadCtx.pCtx.bNewSeqBegin {
		if GetThreadCount(sThreadCtx.pCtx) > 1 {
			// Wait for older workers before replacing shared DPB storage.
			for i := int32(0); i < d.m_DecCtxActiveCount; i++ {
				if d.m_pDecThrCtxActive[i] != nil && d.m_pDecThrCtxActive[i] != sThreadCtx {
					SemWait(&d.m_pDecThrCtxActive[i].sThreadInfo.sIsIdle, WELS_DEC_THREAD_WAIT_INFINITE)
					SemRelease(&d.m_pDecThrCtxActive[i].sThreadInfo.sIsIdle, nil)
				}
			}
			for i := int32(0); i < d.m_iCtxCount; i++ {
				if d.m_pDecThrCtx[i].pCtx != nil {
					EventReset(&d.m_pDecThrCtx[i].sSliceDecodeStart)
				}
			}
			sThreadCtx.pCtx.pLastThreadCtx = nil
		}
		iErr = AllocPicBuffOnNewSeqBegin(sThreadCtx.pCtx)
		if ERR_NONE != iErr {
			return api.DECODING_STATE(iRet | iErr)
		}
		d.m_pPicBuff = sThreadCtx.pCtx.pPicBuff
		// Keep sibling contexts from carrying stale DPB references.
		for i := int32(0); i < d.m_iCtxCount; i++ {
			if &d.m_pDecThrCtx[i] != sThreadCtx && d.m_pDecThrCtx[i].pCtx != nil {
				c := d.m_pDecThrCtx[i].pCtx
				c.pPicBuff = d.m_pPicBuff
				c.bHaveGotMemory = sThreadCtx.pCtx.bHaveGotMemory
				c.iPicQueueNumber = sThreadCtx.pCtx.iPicQueueNumber
				c.iImgWidthInPixel = sThreadCtx.pCtx.iImgWidthInPixel
				c.iImgHeightInPixel = sThreadCtx.pCtx.iImgHeightInPixel
				c.pDec = nil
				c.pLastThreadCtx = nil
				iErr = InitialDqLayersContext(c, sThreadCtx.pCtx.iImgWidthInPixel,
					sThreadCtx.pCtx.iImgHeightInPixel)
				if ERR_NONE != iErr {
					return api.DECODING_STATE(iRet | iErr)
				}
			}
		}
	} else if bPicBuffChanged {
		InitialDqLayersContext(sThreadCtx.pCtx, int32(sThreadCtx.pCtx.pSps.iMbWidth<<4), int32(sThreadCtx.pCtx.pSps.iMbHeight<<4))
	}
	if !sThreadCtx.pCtx.bNewSeqBegin && d.m_pLastDecThrCtx != nil {
		sThreadCtx.pCtx.sFrameCrop = d.m_pLastDecThrCtx.pCtx.pSps.sFrameCrop
	}
	if sThreadCtx.pCtx.bNewSeqBegin {
		d.m_bParamSetsLostFlag = false
		d.m_bFreezeOutput = false
	} else {
		d.m_bParamSetsLostFlag = sThreadCtx.pCtx.bParamSetsLostFlag
		d.m_bFreezeOutput = sThreadCtx.pCtx.bFreezeOutput
	}
	return api.DECODING_STATE(iRet | iErr)
}

// int CWelsDecoder::ThreadDecodeFrameInternal (const unsigned char* kpSrc, const int kiSrcLen,
// unsigned char** ppDst, SBufferInfo* pDstInfo)
//
// Threaded path only (never reached in the Go port).
// Run decoding picture in separate thread.
func (d *CWelsDecoder) ThreadDecodeFrameInternal(kpSrc []byte, kiSrcLen int32, ppDst *[3][]byte, pDstInfo *api.SBufferInfo) int32 {
	state := int32(api.DsErrorFree)
	var i, j int32
	var signal int32

	//serial using of threads
	if d.m_DecCtxActiveCount < d.m_iThreadCount {
		signal = d.m_DecCtxActiveCount
	} else {
		signal = int32(d.m_pDecThrCtxActive[0].sThreadInfo.uiThrNum)
	}

	SemWait(&d.m_pDecThrCtx[signal].sThreadInfo.sIsIdle, WELS_DEC_THREAD_WAIT_INFINITE)

	for i = 0; i < d.m_DecCtxActiveCount; i++ {
		if d.m_pDecThrCtxActive[i] == &d.m_pDecThrCtx[signal] {
			d.m_pDecThrCtxActive[i] = nil
			for j = i; j < d.m_DecCtxActiveCount-1; j++ {
				d.m_pDecThrCtxActive[j] = d.m_pDecThrCtxActive[j+1]
				d.m_pDecThrCtxActive[j+1] = nil
			}
			d.m_DecCtxActiveCount--
			break
		}
	}

	if d.m_pLastDecThrCtx != nil {
		d.m_pDecThrCtx[signal].pCtx.pLastThreadCtx = d.m_pLastDecThrCtx
	}
	d.m_pDecThrCtx[signal].kpSrc = kpSrc
	d.m_pDecThrCtx[signal].kiSrcLen = kiSrcLen
	d.m_pDecThrCtx[signal].ppDst = ppDst
	d.m_pDecThrCtx[signal].sDstInfo = *pDstInfo

	state = int32(d.ParseAccessUnit(&d.m_pDecThrCtx[signal]))
	if state != int32(api.DsErrorFree) {
		SemRelease(&d.m_pDecThrCtx[signal].sThreadInfo.sIsIdle, nil)
		return state
	}

	if d.m_iThreadCount > 1 && d.m_pDecThrCtx[signal].pCtx.pAccessUnitList.uiAvailUnitsNum == 0 {
		SemRelease(&d.m_pDecThrCtx[signal].sThreadInfo.sIsIdle, nil)
		return state
	}

	d.m_pDecThrCtxActive[d.m_DecCtxActiveCount] = &d.m_pDecThrCtx[signal]
	d.m_DecCtxActiveCount++
	if d.m_iThreadCount > 1 {
		d.m_pLastDecThrCtx = &d.m_pDecThrCtx[signal]
	}
	d.m_pDecThrCtx[signal].sThreadInfo.uiCommand = WELS_DEC_THREAD_COMMAND_RUN
	SemRelease(&d.m_pDecThrCtx[signal].sThreadInfo.sIsActivated, nil)

	// wait early picture
	if d.m_DecCtxActiveCount >= d.m_iThreadCount {
		SemWait(&d.m_pDecThrCtxActive[0].sThreadInfo.sIsIdle, WELS_DEC_THREAD_WAIT_INFINITE)
		SemRelease(&d.m_pDecThrCtxActive[0].sThreadInfo.sIsIdle, nil)
	}
	return state
}

// int WelsGetDecoderCapability (SDecoderCapability* pDecCapability)
//
// @return: DecCapability information
func WelsGetDecoderCapability(pDecCapability *api.SDecoderCapability) int32 {
	*pDecCapability = api.SDecoderCapability{}
	pDecCapability.IProfileIdc = 66   //Baseline
	pDecCapability.IProfileIop = 0xE0 //11100000b
	pDecCapability.ILevelIdc = 32     //level_idc = 3.2
	pDecCapability.IMaxMbps = 216000  //from level_idc = 3.2
	pDecCapability.IMaxFs = 5120      //from level_idc = 3.2
	pDecCapability.IMaxCpb = 20000    //from level_idc = 3.2
	pDecCapability.IMaxDpb = 20480    //from level_idc = 3.2
	pDecCapability.IMaxBr = 20000     //from level_idc = 3.2
	pDecCapability.BRedPicCap = false //not support redundant pic

	return ERR_NONE
}

// long WelsCreateDecoder (ISVCDecoder** ppDecoder)
//
// @return: success in return 0, otherwise failed.
func WelsCreateDecoder(ppDecoder *api.ISVCDecoder) int32 {

	if nil == ppDecoder {
		return ERR_INVALID_PARAMETERS
	}

	*ppDecoder = NewCWelsDecoder()

	if nil == *ppDecoder {
		return ERR_MALLOC_FAILED
	}

	return ERR_NONE
}

// void WelsDestroyDecoder (ISVCDecoder* pDecoder)
func WelsDestroyDecoder(pDecoder api.ISVCDecoder) {
	if nil != pDecoder {
		if d, ok := pDecoder.(*CWelsDecoder); ok && d != nil {
			d.Destroy()
		}
	}
}
