package processing

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// Port of codec/processing/src/common/WelsFrameWork.cpp.

/* interface API implement */

// WelsCreateVpInterface creates a processing interface.
//
// C: EResult WelsCreateVpInterface (void** ppCtx, int iVersion).
// With iVersion & 0x8000 (WELSVP_INTERFACE_VERION) ppCtx must be a *IWelsVP;
// otherwise (WELSVP_INTERFACE_VERION_C) it must be a **IWelsVPc.
func WelsCreateVpInterface(ppCtx any, iVersion int32) EResult {
	if iVersion&0x8000 != 0 {
		pp, ok := ppCtx.(*IWelsVP)
		if !ok || pp == nil {
			return RET_INVALIDPARAM
		}
		return CreateSpecificVpInterface(pp)
	} else if iVersion&0x7fff != 0 {
		pp, ok := ppCtx.(**IWelsVPc)
		if !ok || pp == nil {
			return RET_INVALIDPARAM
		}
		return CreateSpecificVpInterfaceC(pp)
	} else {
		return RET_INVALIDPARAM
	}
}

// WelsDestroyVpInterface destroys an interface created by
// WelsCreateVpInterface. pCtx is the IWelsVP (C++ version) or the *IWelsVPc
// (C version).
func WelsDestroyVpInterface(pCtx any, iVersion int32) EResult {
	if iVersion&0x8000 != 0 {
		p, _ := pCtx.(IWelsVP)
		return DestroySpecificVpInterface(p)
	} else if iVersion&0x7fff != 0 {
		p, _ := pCtx.(*IWelsVPc)
		return DestroySpecificVpInterfaceC(p)
	} else {
		return RET_INVALIDPARAM
	}
}

///////////////////////////////////////////////////////////////////////

func CreateSpecificVpInterface(ppCtx *IWelsVP) EResult {
	eReturn := RET_FAILED

	pFr := NewCVpFrameWork(1, &eReturn)
	if pFr != nil {
		*ppCtx = pFr
		eReturn = RET_SUCCESS
	}

	return eReturn
}

func DestroySpecificVpInterface(pCtx IWelsVP) EResult {
	if pFr, ok := pCtx.(*CVpFrameWork); ok && pFr != nil {
		pFr.destroy()
	}

	return RET_SUCCESS
}

///////////////////////////////////////////////////////////////////////////////

// NewCVpFrameWork is the CVpFrameWork constructor.
func NewCVpFrameWork(uiThreadsNum uint32, eReturn *EResult) *CVpFrameWork {
	f := &CVpFrameWork{}
	iCoreNum := int32(1)
	uiCPUFlag := common.WelsCPUFeatureDetect(&iCoreNum)

	for i := int32(0); i < MAX_STRATEGY_NUM; i++ {
		f.m_pStgChain[i] = f.CreateStrategy(EMethods(i+1), int32(uiCPUFlag))
	}

	*eReturn = RET_SUCCESS
	return f
}

// destroy is the CVpFrameWork destructor.
func (f *CVpFrameWork) destroy() {
	for i := int32(0); i < MAX_STRATEGY_NUM; i++ {
		if f.m_pStgChain[i] != nil {
			f.Uninit(f.m_pStgChain[i].strategyBase().m_eMethod)
			f.m_pStgChain[i] = nil
		}
	}
}

func (f *CVpFrameWork) Init(iType int32, pCfg any) EResult {
	eReturn := RET_SUCCESS
	iCurIdx := int32(WelsVpGetValidMethod(iType)) - 1

	f.Uninit(iType)

	pStrategy := f.m_pStgChain[iCurIdx]
	if pStrategy != nil {
		eReturn = pStrategy.Init(0, pCfg)
	}

	return eReturn
}

func (f *CVpFrameWork) Uninit(iType int32) EResult {
	eReturn := RET_SUCCESS
	iCurIdx := int32(WelsVpGetValidMethod(iType)) - 1

	pStrategy := f.m_pStgChain[iCurIdx]
	if pStrategy != nil {
		eReturn = pStrategy.Uninit(0)
	}

	return eReturn
}

func (f *CVpFrameWork) Flush(iType int32) EResult {
	eReturn := RET_SUCCESS

	return eReturn
}

func (f *CVpFrameWork) Process(iType int32, pSrcPixMap *SPixMap, pDstPixMap *SPixMap) EResult {
	eReturn := RET_NOTSUPPORTED
	eMethod := WelsVpGetValidMethod(iType)
	iCurIdx := int32(eMethod) - 1
	var sSrcPic SPixMap
	var sDstPic SPixMap

	if pSrcPixMap != nil {
		sSrcPic = *pSrcPixMap
	}
	if pDstPixMap != nil {
		sDstPic = *pDstPixMap
	}
	if !f.CheckValid(eMethod, &sSrcPic, &sDstPic) {
		return RET_INVALIDPARAM
	}

	pStrategy := f.m_pStgChain[iCurIdx]
	if pStrategy != nil {
		eReturn = pStrategy.Process(0, &sSrcPic, &sDstPic)
	}

	return eReturn
}

func (f *CVpFrameWork) Get(iType int32, pParam any) EResult {
	eReturn := RET_SUCCESS
	iCurIdx := int32(WelsVpGetValidMethod(iType)) - 1

	if pParam == nil {
		return RET_INVALIDPARAM
	}

	pStrategy := f.m_pStgChain[iCurIdx]
	if pStrategy != nil {
		eReturn = pStrategy.Get(0, pParam)
	}

	return eReturn
}

func (f *CVpFrameWork) Set(iType int32, pParam any) EResult {
	eReturn := RET_SUCCESS
	iCurIdx := int32(WelsVpGetValidMethod(iType)) - 1

	if pParam == nil {
		return RET_INVALIDPARAM
	}

	pStrategy := f.m_pStgChain[iCurIdx]
	if pStrategy != nil {
		eReturn = pStrategy.Set(0, pParam)
	}

	return eReturn
}

func (f *CVpFrameWork) SpecialFeature(iType int32, pIn any, pOut any) EResult {
	eReturn := RET_SUCCESS

	return eReturn
}

func (f *CVpFrameWork) CheckValid(eMethod EMethods, pSrcPixMap *SPixMap, pDstPixMap *SPixMap) bool {
	if eMethod == METHOD_NULL {
		return false
	}

	if eMethod != METHOD_COLORSPACE_CONVERT {
		if pSrcPixMap.PPixel[0] != nil {
			if pSrcPixMap.EFormat != VIDEO_FORMAT_I420 && pSrcPixMap.EFormat != VIDEO_FORMAT_YV12 {
				return false
			}
		}
		if pSrcPixMap.PPixel[0] != nil && pDstPixMap.PPixel[0] != nil {
			if pDstPixMap.EFormat != pSrcPixMap.EFormat {
				return false
			}
		}
	}

	if pSrcPixMap.PPixel[0] != nil {
		kiSrcChromaWidth := pSrcPixMap.SRect.IRectWidth >> 1
		if pSrcPixMap.SRect.IRectWidth <= 0 || pSrcPixMap.SRect.IRectHeight <= 0 ||
			int64(pSrcPixMap.SRect.IRectWidth)*int64(pSrcPixMap.SRect.IRectHeight) > (MAX_MBS_PER_FRAME<<8) {
			return false
		}
		if pSrcPixMap.SRect.IRectTop >= pSrcPixMap.SRect.IRectHeight ||
			pSrcPixMap.SRect.IRectLeft >= pSrcPixMap.SRect.IRectWidth || pSrcPixMap.SRect.IRectWidth > pSrcPixMap.IStride[0] {
			return false
		}
		if eMethod == METHOD_DOWNSAMPLE &&
			(pSrcPixMap.EFormat == VIDEO_FORMAT_I420 || pSrcPixMap.EFormat == VIDEO_FORMAT_YV12) &&
			(pSrcPixMap.PPixel[1] == nil || pSrcPixMap.PPixel[2] == nil ||
				pSrcPixMap.IStride[1] <= 0 || pSrcPixMap.IStride[2] <= 0 ||
				kiSrcChromaWidth > pSrcPixMap.IStride[1] || kiSrcChromaWidth > pSrcPixMap.IStride[2]) {
			return false
		}
	}
	if pDstPixMap.PPixel[0] != nil {
		kiDstChromaWidth := pDstPixMap.SRect.IRectWidth >> 1
		if pDstPixMap.SRect.IRectWidth <= 0 || pDstPixMap.SRect.IRectHeight <= 0 ||
			int64(pDstPixMap.SRect.IRectWidth)*int64(pDstPixMap.SRect.IRectHeight) > (MAX_MBS_PER_FRAME<<8) {
			return false
		}
		if pDstPixMap.SRect.IRectTop >= pDstPixMap.SRect.IRectHeight ||
			pDstPixMap.SRect.IRectLeft >= pDstPixMap.SRect.IRectWidth || pDstPixMap.SRect.IRectWidth > pDstPixMap.IStride[0] {
			return false
		}
		if eMethod == METHOD_DOWNSAMPLE &&
			(pDstPixMap.EFormat == VIDEO_FORMAT_I420 || pDstPixMap.EFormat == VIDEO_FORMAT_YV12) &&
			(pDstPixMap.PPixel[1] == nil || pDstPixMap.PPixel[2] == nil ||
				pDstPixMap.IStride[1] <= 0 || pDstPixMap.IStride[2] <= 0 ||
				kiDstChromaWidth > pDstPixMap.IStride[1] || kiDstChromaWidth > pDstPixMap.IStride[2]) {
			return false
		}
	}
	return true
}

func (f *CVpFrameWork) CreateStrategy(m_eMethod EMethods, iCpuFlag int32) IStrategy {
	var pStrategy IStrategy

	switch m_eMethod {
	case METHOD_COLORSPACE_CONVERT:
		//not support yet
	case METHOD_DENOISE:
		pStrategy = NewCDenoiser(iCpuFlag)
	case METHOD_SCROLL_DETECTION:
		pStrategy = NewCScrollDetection(iCpuFlag)
	case METHOD_SCENE_CHANGE_DETECTION_VIDEO, METHOD_SCENE_CHANGE_DETECTION_SCREEN:
		pStrategy = BuildSceneChangeDetection(m_eMethod, iCpuFlag)
	case METHOD_DOWNSAMPLE:
		pStrategy = NewCDownsampling(iCpuFlag)
	case METHOD_VAA_STATISTICS:
		pStrategy = NewCVAACalculation(iCpuFlag)
	case METHOD_BACKGROUND_DETECTION:
		pStrategy = NewCBackgroundDetection(iCpuFlag)
	case METHOD_ADAPTIVE_QUANT:
		pStrategy = NewCAdaptiveQuantization(iCpuFlag)
	case METHOD_COMPLEXITY_ANALYSIS:
		pStrategy = NewCComplexityAnalysis(iCpuFlag)
	case METHOD_COMPLEXITY_ANALYSIS_SCREEN:
		pStrategy = NewCComplexityAnalysisScreen(iCpuFlag)
	case METHOD_IMAGE_ROTATE:
		pStrategy = NewCImageRotating(iCpuFlag)
	default:
	}

	return pStrategy
}
