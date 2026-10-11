// Port of codec/encoder/plus/src/welsEncoderExt.cpp.
//
// The OUTPUT_BIT_STREAM, DUMP_SRC_PICTURE, REC_FRAME_COUNT and
// ENABLE_FRAME_DUMP debug paths are not ported (they are compiled out in the
// default C build).

package encoder

import (
	"reflect"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// VERSION_NUMBER is the version string of codec/common/inc/version.h (used
// when no generated version header is present).
const VERSION_NUMBER = "openh264 default: 1.4"

/*
 *  CWelsH264SVCEncoder class implementation
 */

// constructor body CWelsH264SVCEncoder::CWelsH264SVCEncoder
func (p *CWelsH264SVCEncoder) ctorCWelsH264SVCEncoder() {
	p.m_pEncContext = nil
	p.m_pWelsTrace = nil
	p.m_iMaxPicWidth = 0
	p.m_iMaxPicHeight = 0
	p.m_iCspInternal = 0
	p.m_bInitialFlag = false

	p.InitEncoder()
}

// destructor CWelsH264SVCEncoder::~CWelsH264SVCEncoder
func (p *CWelsH264SVCEncoder) Destruct() {
	if p.m_pWelsTrace != nil {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "CWelsH264SVCEncoder::~CWelsH264SVCEncoder()")
	}

	p.Uninitialize()

	if p.m_pWelsTrace != nil {
		p.m_pWelsTrace = nil
	}
}

func (p *CWelsH264SVCEncoder) InitEncoder() {
	p.m_pWelsTrace = common.NewWelsCodecTrace()
	if p.m_pWelsTrace == nil {
		return
	}
	p.m_pWelsTrace.SetCodecInstance(p)
}

/* Interfaces override from ISVCEncoder */

func (p *CWelsH264SVCEncoder) GetDefaultParams(argv *api.SEncParamExt) int32 {
	FillDefaultParam(argv)
	return int32(api.CmResultSuccess)
}

/*
 *  SVC Encoder Initialization
 */
func (p *CWelsH264SVCEncoder) Initialize(argv *api.SEncParamBase) int32 {
	if p.m_pWelsTrace == nil {
		return int32(api.CmMallocMemeError)
	}

	common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "CWelsH264SVCEncoder::InitEncoder(), openh264 codec version = %s",
		VERSION_NUMBER)

	if nil == argv {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::Initialize(), invalid argv= 0x%p",
			argv)
		return int32(api.CmInitParaError)
	}

	sConfig := NewSWelsSvcCodingParam()
	// Convert SEncParamBase into WelsSVCParamConfig here..
	if sConfig.ParamBaseTranscode(argv) != 0 {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::Initialize(), parameter_translation failed.")
		p.TraceParamInfo(&sConfig.SEncParamExt)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}

	return p.InitializeInternal(sConfig)
}

func (p *CWelsH264SVCEncoder) InitializeExt(argv *api.SEncParamExt) int32 {
	if p.m_pWelsTrace == nil {
		return int32(api.CmMallocMemeError)
	}

	common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "CWelsH264SVCEncoder::InitEncoder(), openh264 codec version = %s",
		VERSION_NUMBER)

	if nil == argv {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::InitializeExt(), invalid argv= 0x%p",
			argv)
		return int32(api.CmInitParaError)
	}

	sConfig := NewSWelsSvcCodingParam()
	// Convert SEncParamExt into WelsSVCParamConfig here..
	if sConfig.ParamTranscode(argv) != 0 {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::InitializeExt(), parameter_translation failed.")
		p.TraceParamInfo(&sConfig.SEncParamExt)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}

	return p.InitializeInternal(sConfig)
}

func (p *CWelsH264SVCEncoder) InitializeInternal(pCfg *SWelsSvcCodingParam) int32 {
	if nil == pCfg {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::Initialize(), invalid argv= 0x%p.",
			pCfg)
		return int32(api.CmInitParaError)
	}

	if p.m_bInitialFlag {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING,
			"CWelsH264SVCEncoder::Initialize(), reinitialize, m_bInitialFlag= %d.",
			p.m_bInitialFlag)
		p.Uninitialize()
	}
	// Check valid parameters
	iNumOfLayers := pCfg.ISpatialLayerNum
	if iNumOfLayers < 1 || iNumOfLayers > MAX_DEPENDENCY_LAYER {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::Initialize(), invalid iSpatialLayerNum= %d, valid at range of [1, %d].", iNumOfLayers,
			MAX_DEPENDENCY_LAYER)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}
	if pCfg.ITemporalLayerNum < 1 {
		pCfg.ITemporalLayerNum = 1
	}
	if pCfg.ITemporalLayerNum > MAX_TEMPORAL_LEVEL {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::Initialize(), invalid iTemporalLayerNum= %d, valid at range of [1, %d].",
			pCfg.ITemporalLayerNum, MAX_TEMPORAL_LEVEL)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}

	//  assert( cfg.uiGopSize >= 1 && ( cfg.uiIntraPeriod && (cfg.uiIntraPeriod % cfg.uiGopSize) == 0) );

	if pCfg.uiGopSize < 1 || pCfg.uiGopSize > MAX_GOP_SIZE {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::Initialize(), invalid uiGopSize= %d, valid at range of [1, %d].", pCfg.uiGopSize,
			MAX_GOP_SIZE)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}

	if !common.WELS_POWER2_IF(pCfg.uiGopSize) {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::Initialize(), invalid uiGopSize= %d, valid at range of [1, %d] and yield to power of 2.",
			pCfg.uiGopSize, MAX_GOP_SIZE)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}

	if pCfg.UiIntraPeriod != 0 && pCfg.UiIntraPeriod < pCfg.uiGopSize {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::Initialize(), invalid uiIntraPeriod= %d, valid in case it equals to 0 for unlimited intra period or exceeds specified uiGopSize= %d.",
			pCfg.UiIntraPeriod, pCfg.uiGopSize)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}

	if pCfg.UiIntraPeriod != 0 && (pCfg.UiIntraPeriod&(pCfg.uiGopSize-1)) != 0 {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
			"CWelsH264SVCEncoder::Initialize(), invalid uiIntraPeriod= %d, valid in case it equals to 0 for unlimited intra period or exceeds specified uiGopSize= %d also multiple of it.",
			pCfg.UiIntraPeriod, pCfg.uiGopSize)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}
	if pCfg.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		if pCfg.BEnableLongTermReference {
			pCfg.ILTRRefNum = LONG_TERM_REF_NUM_SCREEN
			if pCfg.INumRefFrame == api.AUTO_REF_PIC_COUNT {
				pCfg.INumRefFrame = common.WELS_MAX(int32(1), common.WELS_LOG2(pCfg.uiGopSize)) + pCfg.ILTRRefNum
			}
		} else {
			pCfg.ILTRRefNum = 0
			if pCfg.INumRefFrame == api.AUTO_REF_PIC_COUNT {
				pCfg.INumRefFrame = int32(common.WELS_MAX(uint32(1), pCfg.uiGopSize>>1))
			}
		}
	} else {
		if pCfg.BEnableLongTermReference {
			pCfg.ILTRRefNum = LONG_TERM_REF_NUM
		} else {
			pCfg.ILTRRefNum = 0
		}
		if pCfg.INumRefFrame == api.AUTO_REF_PIC_COUNT {
			if (pCfg.uiGopSize >> 1) > 1 {
				pCfg.INumRefFrame = int32(pCfg.uiGopSize>>1) + pCfg.ILTRRefNum
			} else {
				pCfg.INumRefFrame = MIN_REF_PIC_COUNT + pCfg.ILTRRefNum
			}
			pCfg.INumRefFrame = common.WELS_CLIP3(pCfg.INumRefFrame, MIN_REF_PIC_COUNT, MAX_REFERENCE_PICTURE_COUNT_NUM_CAMERA)
		}
	}

	if pCfg.ILtrMarkPeriod == 0 {
		pCfg.ILtrMarkPeriod = 30
	}

	kiDecStages := common.WELS_LOG2(pCfg.uiGopSize)
	pCfg.ITemporalLayerNum = int32(int8(1 + kiDecStages))
	pCfg.ILoopFilterAlphaC0Offset = common.WELS_CLIP3(pCfg.ILoopFilterAlphaC0Offset, -6, 6)
	pCfg.ILoopFilterBetaOffset = common.WELS_CLIP3(pCfg.ILoopFilterBetaOffset, -6, 6)

	// decide property list size between INIT_TYPE_PARAMETER_BASED/INIT_TYPE_CONFIG_BASED
	p.m_iMaxPicWidth = pCfg.IPicWidth
	p.m_iMaxPicHeight = pCfg.IPicHeight

	p.TraceParamInfo(&pCfg.SEncParamExt)
	if WelsInitEncoderExt(&p.m_pEncContext, pCfg, &p.m_pWelsTrace.M_sLogCtx, nil) != 0 {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::Initialize(), WelsInitEncoderExt failed.")
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_DEBUG,
			"Problematic Input Base Param: iUsageType=%d, Resolution=%dx%d, FR=%f, TLayerNum=%d, DLayerNum=%d",
			pCfg.IUsageType, pCfg.IPicWidth, pCfg.IPicHeight, pCfg.FMaxFrameRate, pCfg.ITemporalLayerNum,
			pCfg.ISpatialLayerNum)
		p.Uninitialize()
		return int32(api.CmInitParaError)
	}

	p.m_bInitialFlag = true

	return int32(api.CmResultSuccess)
}

/*
 *  SVC Encoder Uninitialization
 */
func (p *CWelsH264SVCEncoder) Uninitialize() int32 {
	if !p.m_bInitialFlag {
		return 0
	}

	common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, "CWelsH264SVCEncoder::Uninitialize(), openh264 codec version = %s.",
		VERSION_NUMBER)

	if nil != p.m_pEncContext {
		WelsUninitEncoderExt(&p.m_pEncContext)
		p.m_pEncContext = nil
	}

	p.m_bInitialFlag = false

	return 0
}

/*
 *  SVC core encoding
 */
func (p *CWelsH264SVCEncoder) EncodeFrame(kpSrcPic *api.SSourcePicture, pBsInfo *api.SFrameBSInfo) int32 {
	if !(kpSrcPic != nil && p.m_bInitialFlag && pBsInfo != nil) {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::EncodeFrame(), cmInitParaError.")
		return int32(api.CmInitParaError)
	}
	if kpSrcPic.IColorFormat != int32(api.VideoFormatI420) {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::EncodeFrame(), wrong iColorFormat %d",
			kpSrcPic.IColorFormat)
		return int32(api.CmInitParaError)
	}

	kiEncoderReturn := p.EncodeFrameInternal(kpSrcPic, pBsInfo)

	if kiEncoderReturn != int32(api.CmResultSuccess) {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::EncodeFrame(), kiEncoderReturn %d",
			kiEncoderReturn)
		return kiEncoderReturn
	}

	return kiEncoderReturn
}

func (p *CWelsH264SVCEncoder) EncodeFrameInternal(pSrcPic *api.SSourcePicture, pBsInfo *api.SFrameBSInfo) int32 {
	if (pSrcPic.IPicWidth < 16) || (pSrcPic.IPicHeight < 16) {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "Don't support width(%d) or height(%d) which is less than 16!",
			pSrcPic.IPicWidth, pSrcPic.IPicHeight)
		return int32(api.CmUnsupportedData)
	}

	kiBeforeFrameUs := common.WelsTime()
	kiEncoderReturn := WelsEncoderEncodeExt(p.m_pEncContext, pBsInfo, pSrcPic)
	kiCurrentFrameMs := (common.WelsTime() - kiBeforeFrameUs) / 1000
	if (kiEncoderReturn == ENC_RETURN_MEMALLOCERR) || (kiEncoderReturn == ENC_RETURN_MEMOVERFLOWFOUND) ||
		(kiEncoderReturn == ENC_RETURN_VLCOVERFLOWFOUND) {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_DEBUG, "CWelsH264SVCEncoder::EncodeFrame() not succeed, err=%d",
			kiEncoderReturn)
		WelsUninitEncoderExt(&p.m_pEncContext)
		return int32(api.CmMallocMemeError)
	} else if kiEncoderReturn == ENC_RETURN_INVALIDINPUT {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "CWelsH264SVCEncoder::EncodeFrame() invalid input, err=%d",
			kiEncoderReturn)
		return int32(api.CmUnsupportedData)
	} else if (kiEncoderReturn != ENC_RETURN_SUCCESS) && (kiEncoderReturn == ENC_RETURN_CORRECTED) {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR, "unexpected return(%d) from EncodeFrameInternal()!",
			kiEncoderReturn)
		return int32(api.CmUnknownReason)
	}

	p.UpdateStatistics(pBsInfo, kiCurrentFrameMs)

	return int32(api.CmResultSuccess)
}

func (p *CWelsH264SVCEncoder) EncodeParameterSets(pBsInfo *api.SFrameBSInfo) int32 {
	return WelsEncoderEncodeParameterSets(p.m_pEncContext, pBsInfo)
}

/*
 *  Force key frame
 */
// C++ default argument iLayerId = -1.
func (p *CWelsH264SVCEncoder) ForceIntraFrame(bIDR bool, iLayerId int32) int32 {
	if bIDR {
		if !(p.m_pEncContext != nil && p.m_bInitialFlag) {
			return 1
		}

		ForceCodingIDR(p.m_pEncContext, iLayerId)
	} else {
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::ForceIntraFrame(),nothing to do as bIDR set to false")
	}

	return 0
}

func (p *CWelsH264SVCEncoder) TraceParamInfo(pParam *api.SEncParamExt) {
	common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
		"iUsageType = %d,iPicWidth= %d;iPicHeight= %d;iTargetBitrate= %d;iMaxBitrate= %d;iRCMode= %d;iPaddingFlag= %d;iTemporalLayerNum= %d;iSpatialLayerNum= %d;fFrameRate= %.6ff;uiIntraPeriod= %d;"+
			"eSpsPpsIdStrategy = %d;bPrefixNalAddingCtrl = %d;bSimulcastAVC=%d;bEnableDenoise= %d;bEnableBackgroundDetection= %d;bEnableSceneChangeDetect = %d;bEnableAdaptiveQuant= %d;bEnableFrameSkip= %d;bEnableLongTermReference= %d;iLtrMarkPeriod= %d, bIsLosslessLink=%d;"+
			"iComplexityMode = %d;iNumRefFrame = %d;iEntropyCodingModeFlag = %d;uiMaxNalSize = %d;iLTRRefNum = %d;iMultipleThreadIdc = %d;iLoopFilterDisableIdc = %d (offset(alpha/beta): %d,%d;iComplexityMode = %d,iMaxQp = %d;iMinQp = %d)",
		pParam.IUsageType,
		pParam.IPicWidth,
		pParam.IPicHeight,
		pParam.ITargetBitrate,
		pParam.IMaxBitrate,
		pParam.IRCMode,
		pParam.IPaddingFlag,
		pParam.ITemporalLayerNum,
		pParam.ISpatialLayerNum,
		pParam.FMaxFrameRate,
		pParam.UiIntraPeriod,
		pParam.ESpsPpsIdStrategy,
		welsEncExtBoolToInt32(pParam.BPrefixNalAddingCtrl),
		welsEncExtBoolToInt32(pParam.BSimulcastAVC),
		welsEncExtBoolToInt32(pParam.BEnableDenoise),
		welsEncExtBoolToInt32(pParam.BEnableBackgroundDetection),
		welsEncExtBoolToInt32(pParam.BEnableSceneChangeDetect),
		welsEncExtBoolToInt32(pParam.BEnableAdaptiveQuant),
		welsEncExtBoolToInt32(pParam.BEnableFrameSkip),
		welsEncExtBoolToInt32(pParam.BEnableLongTermReference),
		pParam.ILtrMarkPeriod,
		welsEncExtBoolToInt32(pParam.BIsLosslessLink),
		pParam.IComplexityMode,
		pParam.INumRefFrame,
		pParam.IEntropyCodingModeFlag,
		pParam.UiMaxNalSize,
		pParam.ILTRRefNum,
		pParam.IMultipleThreadIdc,
		pParam.ILoopFilterDisableIdc,
		pParam.ILoopFilterAlphaC0Offset,
		pParam.ILoopFilterBetaOffset,
		pParam.IComplexityMode,
		pParam.IMaxQp,
		pParam.IMinQp,
	)
	i := int32(0)
	iSpatialLayers := pParam.ISpatialLayerNum
	if iSpatialLayers >= api.MAX_SPATIAL_LAYER_NUM {
		iSpatialLayers = api.MAX_SPATIAL_LAYER_NUM
	}
	for i < iSpatialLayers {
		pSpatialCfg := &pParam.SSpatialLayers[i]
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"sSpatialLayers[%d]: .iVideoWidth= %d; .iVideoHeight= %d; .fFrameRate= %.6ff; .iSpatialBitrate= %d; .iMaxSpatialBitrate= %d; .sSliceArgument.uiSliceMode= %d; .sSliceArgument.iSliceNum= %d; .sSliceArgument.uiSliceSizeConstraint= %d;"+
				"uiProfileIdc = %d;uiLevelIdc = %d;iDLayerQp = %d",
			i, pSpatialCfg.IVideoWidth,
			pSpatialCfg.IVideoHeight,
			pSpatialCfg.FFrameRate,
			pSpatialCfg.ISpatialBitrate,
			pSpatialCfg.IMaxSpatialBitrate,
			pSpatialCfg.SSliceArgument.UiSliceMode,
			pSpatialCfg.SSliceArgument.UiSliceNum,
			pSpatialCfg.SSliceArgument.UiSliceSizeConstraint,
			pSpatialCfg.UiProfileIdc,
			pSpatialCfg.UiLevelIdc,
			pSpatialCfg.IDLayerQp,
		)
		i++
	}
}

func (p *CWelsH264SVCEncoder) LogStatistics(kiCurrentFrameTs int64, iMaxDid int32) {
	for iDid := int32(0); iDid <= iMaxDid; iDid++ {
		pStatistics := &p.m_pEncContext.sEncoderStatistics[iDid]
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"EncoderStatistics: SpatialId = %d,%dx%d, SpeedInMs: %f, fAverageFrameRate=%f, "+
				"LastFrameRate=%f, LatestBitRate=%d, LastFrameQP=%d, uiInputFrameCount=%d, uiSkippedFrameCount=%d, "+
				"uiResolutionChangeTimes=%d, uIDRReqNum=%d, uIDRSentNum=%d, uLTRSentNum=NA, iTotalEncodedBytes=%lu at Ts = %lld",
			iDid, pStatistics.UiWidth, pStatistics.UiHeight,
			pStatistics.FAverageFrameSpeedInMs, pStatistics.FAverageFrameRate,
			pStatistics.FLatestFrameRate, pStatistics.UiBitRate, pStatistics.UiAverageFrameQP,
			pStatistics.UiInputFrameCount, pStatistics.UiSkippedFrameCount,
			pStatistics.UiResolutionChangeTimes, pStatistics.UiIDRReqNum, pStatistics.UiIDRSentNum,
			pStatistics.ITotalEncodedBytes, kiCurrentFrameTs)
	}
}

func (p *CWelsH264SVCEncoder) UpdateStatistics(pBsInfo *api.SFrameBSInfo, kiCurrentFrameMs int64) {
	p.m_pEncContext.uiLastTimestamp = pBsInfo.UiTimeStamp
	kiCurrentFrameTs := p.m_pEncContext.uiLastTimestamp
	kiTimeDiff := kiCurrentFrameTs - p.m_pEncContext.iLastStatisticsLogTs

	iMaxDid := p.m_pEncContext.pSvcParam.ISpatialLayerNum - 1
	var pLayerInfo *api.SLayerBSInfo
	iMaxInputFrame := uint32(0)
	iMaxFrameRate := float32(0)
	for iDid := int32(0); iDid <= iMaxDid; iDid++ {
		eFrameType := api.VideoFrameTypeSkip
		kiCurrentFrameSize := int32(0)
		for iLayerNum := int32(0); iLayerNum < pBsInfo.ILayerNum; iLayerNum++ {
			pLayerInfo = &pBsInfo.SLayerInfo[iLayerNum]
			if (pLayerInfo.UiLayerType == uint8(api.VIDEO_CODING_LAYER)) && (int32(pLayerInfo.UiSpatialId) == iDid) {
				eFrameType = pLayerInfo.EFrameType
				for iNalIdx := int32(0); iNalIdx < pLayerInfo.INalCount; iNalIdx++ {
					kiCurrentFrameSize += pLayerInfo.PNalLengthInByte[iNalIdx]
				}
			}
		}
		pStatistics := &p.m_pEncContext.sEncoderStatistics[iDid]
		pSpatialLayerInternalParam := &p.m_pEncContext.pSvcParam.sDependencyLayers[iDid]

		if (0 != pStatistics.UiWidth && 0 != pStatistics.UiHeight) &&
			(pStatistics.UiWidth != uint32(pSpatialLayerInternalParam.iActualWidth) ||
				pStatistics.UiHeight != uint32(pSpatialLayerInternalParam.iActualHeight)) {
			pStatistics.UiResolutionChangeTimes++
		}
		pStatistics.UiWidth = uint32(pSpatialLayerInternalParam.iActualWidth)
		pStatistics.UiHeight = uint32(pSpatialLayerInternalParam.iActualHeight)

		kbCurrentFrameSkipped := (api.VideoFrameTypeSkip == eFrameType)
		pStatistics.UiInputFrameCount++
		if kbCurrentFrameSkipped {
			pStatistics.UiSkippedFrameCount++
		}
		iMaxInputFrame = common.WELS_MAX(pStatistics.UiInputFrameCount, iMaxInputFrame)
		iProcessedFrameCount := int32(pStatistics.UiInputFrameCount - pStatistics.UiSkippedFrameCount)
		if !kbCurrentFrameSkipped && iProcessedFrameCount != 0 {
			pStatistics.FAverageFrameSpeedInMs += (float32(kiCurrentFrameMs) - pStatistics.FAverageFrameSpeedInMs) / float32(iProcessedFrameCount)
		}
		// rate control related
		if 0 != p.m_pEncContext.uiStartTimestamp {
			if kiCurrentFrameTs > p.m_pEncContext.uiStartTimestamp+800 {
				pStatistics.FAverageFrameRate = (float32(pStatistics.UiInputFrameCount) * 1000 /
					float32(kiCurrentFrameTs-p.m_pEncContext.uiStartTimestamp))
			}
		} else {
			p.m_pEncContext.uiStartTimestamp = kiCurrentFrameTs
		}
		iMaxFrameRate = common.WELS_MAX(iMaxFrameRate, pStatistics.FAverageFrameRate)
		//pStatistics->fLatestFrameRate = m_pEncContext->pWelsSvcRc->fLatestFrameRate; //TODO: finish the calculation in RC
		//pStatistics->uiBitRate = m_pEncContext->pWelsSvcRc->iActualBitRate; //TODO: finish the calculation in RC
		pStatistics.UiAverageFrameQP = uint32(p.m_pEncContext.pWelsSvcRc[iDid].iAverageFrameQp)

		if api.VideoFrameTypeIDR == eFrameType || api.VideoFrameTypeI == eFrameType {
			pStatistics.UiIDRSentNum++
		}
		if p.m_pEncContext.pLtr[0].bLTRMarkingFlag {
			pStatistics.UiLTRSentNum++
		}

		pStatistics.ITotalEncodedBytes += uint32(kiCurrentFrameSize)

		kiDeltaFrames := int32(pStatistics.UiInputFrameCount - pStatistics.ILastStatisticsFrameCount)
		if float32(kiDeltaFrames) > (p.m_pEncContext.pSvcParam.FMaxFrameRate * 2) {
			if kiTimeDiff >= int64(p.m_pEncContext.iStatisticsLogInterval) {
				fTimeDiffSec := float32(kiTimeDiff) / 1000.0
				pStatistics.FLatestFrameRate = float32(pStatistics.UiInputFrameCount-
					pStatistics.ILastStatisticsFrameCount) / fTimeDiffSec
				pStatistics.UiBitRate = uint32(float32(uint64(pStatistics.ITotalEncodedBytes)*8) / fTimeDiffSec)

				if common.WELS_ABS(pStatistics.FLatestFrameRate-p.m_pEncContext.pSvcParam.FMaxFrameRate) > 30 {
					common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING,
						"Actual input fLatestFrameRate = %f is quite different from framerate in setting %f, please check setting or timestamp unit (ms), cur_Ts = %lld start_Ts = %lld",
						pStatistics.FLatestFrameRate, p.m_pEncContext.pSvcParam.FMaxFrameRate, kiCurrentFrameTs,
						p.m_pEncContext.iLastStatisticsLogTs)
				}

				if p.m_pEncContext.pSvcParam.IRCMode == api.RC_QUALITY_MODE || p.m_pEncContext.pSvcParam.IRCMode == api.RC_BITRATE_MODE {
					if (pStatistics.FLatestFrameRate > 0) &&
						common.WELS_ABS(p.m_pEncContext.pSvcParam.FMaxFrameRate-pStatistics.FLatestFrameRate) > 5 {
						common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING,
							"Actual input framerate %f is different from framerate in setting %f, suggest to use other rate control modes",
							pStatistics.FLatestFrameRate, p.m_pEncContext.pSvcParam.FMaxFrameRate)
					}
				}
				// update variables
				pStatistics.ILastStatisticsBytes = pStatistics.ITotalEncodedBytes
				pStatistics.ILastStatisticsFrameCount = pStatistics.UiInputFrameCount
				p.m_pEncContext.iLastStatisticsLogTs = kiCurrentFrameTs
				p.LogStatistics(kiCurrentFrameTs, iMaxDid)
				pStatistics.ITotalEncodedBytes = 0
				//TODO: the following statistics will be calculated and added later
				//pStatistics->uiLTRSentNum
			}
		}
	}
	_ = iMaxInputFrame
	_ = iMaxFrameRate
}

/************************************************************************
* InDataFormat, IDRInterval, SVC Encode Param, Frame Rate, Bitrate,..
************************************************************************/

// SetOption ports CWelsH264SVCEncoder::SetOption. pOption (void* in C) is
// any holding a pointer to the option value (see package api). A value of
// an unexpected type is rejected with cmInitParaError.
func (p *CWelsH264SVCEncoder) SetOption(eOptionId api.ENCODER_OPTION, pOption any) int32 {
	if welsEncExtOptionIsNil(pOption) {
		return int32(api.CmInitParaError)
	}

	if (nil == p.m_pEncContext || false == p.m_bInitialFlag) && eOptionId != api.ENCODER_OPTION_TRACE_LEVEL &&
		eOptionId != api.ENCODER_OPTION_TRACE_CALLBACK && eOptionId != api.ENCODER_OPTION_TRACE_CALLBACK_CONTEXT {
		return int32(api.CmInitExpected)
	}

	switch eOptionId {
	case api.ENCODER_OPTION_INTER_SPATIAL_PRED: // Inter spatial layer prediction flag
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"ENCODER_OPTION_INTER_SPATIAL_PRED, this feature not supported at present.")

	case api.ENCODER_OPTION_DATAFORMAT: // Input color space
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		iColorspace := iValue
		if iColorspace == 0 {
			return int32(api.CmInitParaError)
		}

		p.m_iCspInternal = iColorspace
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_DATAFORMAT, m_iCspInternal = 0x%x", p.m_iCspInternal)

	case api.ENCODER_OPTION_IDR_INTERVAL: // IDR Interval
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_IDR_INTERVAL iValue = %d", iValue)
		if iValue <= -1 {
			iValue = 0
		}
		if iValue == int32(p.m_pEncContext.pSvcParam.UiIntraPeriod) {
			return int32(api.CmResultSuccess)
		}
		p.m_pEncContext.pSvcParam.UiIntraPeriod = uint32(iValue)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_IDR_INTERVAL uiIntraPeriod updated to %d",
			p.m_pEncContext.pSvcParam.UiIntraPeriod)

	case api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE: // SVC Encoding Parameter
		pBase, ok := pOption.(*api.SEncParamBase)
		if !ok {
			return int32(api.CmInitParaError)
		}
		sEncodingParam := *pBase // memcpy
		sConfig := NewSWelsSvcCodingParam()
		iTargetWidth := int32(0)
		iTargetHeight := int32(0)

		if sConfig.ParamBaseTranscode(&sEncodingParam) != 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_BASE, ParamTranscode failed!")
			return int32(api.CmInitParaError)
		}
		/* New configuration available here */
		iTargetWidth = sConfig.IPicWidth
		iTargetHeight = sConfig.IPicHeight
		if p.m_iMaxPicWidth != iTargetWidth ||
			p.m_iMaxPicHeight != iTargetHeight {
			p.m_iMaxPicWidth = iTargetWidth
			p.m_iMaxPicHeight = iTargetHeight
		}
		if sConfig.DetermineTemporalSettings() != 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_BASE, DetermineTemporalSettings failed!")
			return int32(api.CmInitParaError)
		}
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_BASE iUsageType = %d,iPicWidth= %d;iPicHeight= %d;iTargetBitrate= %d;fMaxFrameRate=  %.6ff;iRCMode= %d",
			sEncodingParam.IUsageType,
			sEncodingParam.IPicWidth,
			sEncodingParam.IPicHeight,
			sEncodingParam.ITargetBitrate,
			sEncodingParam.FMaxFrameRate,
			sEncodingParam.IRCMode)
		if WelsEncoderParamAdjust(&p.m_pEncContext, sConfig) != 0 {
			return int32(api.CmInitParaError)
		}

		//LogStatistics
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_BASE, LogStatisticsBeforeNewEncoding")
		p.LogStatistics(p.m_pEncContext.iLastStatisticsLogTs, 0)

	case api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT: // SVC Encoding Parameter
		pExt, ok := pOption.(*api.SEncParamExt)
		if !ok {
			return int32(api.CmInitParaError)
		}
		sEncodingParam := *pExt // memcpy
		sConfig := NewSWelsSvcCodingParam()
		iTargetWidth := int32(0)
		iTargetHeight := int32(0)

		p.TraceParamInfo(&sEncodingParam)
		if sEncodingParam.ISpatialLayerNum < 1 ||
			sEncodingParam.ISpatialLayerNum > api.MAX_SPATIAL_LAYER_NUM { // verify number of spatial layer
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, iSpatialLayerNum(%d) failed!",
				sEncodingParam.ISpatialLayerNum)
			return int32(api.CmInitParaError)
		}

		if sConfig.ParamTranscode(&sEncodingParam) != 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, ParamTranscode failed!")
			return int32(api.CmInitParaError)
		}
		if sConfig.ISpatialLayerNum < 1 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, iSpatialLayerNum(%d) failed!",
				sConfig.ISpatialLayerNum)
			return int32(api.CmInitParaError)
		}
		if sConfig.DetermineTemporalSettings() != 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, DetermineTemporalSettings failed!")
			return int32(api.CmInitParaError)
		}

		/* New configuration available here */
		iTargetWidth = sConfig.IPicWidth
		iTargetHeight = sConfig.IPicHeight
		if p.m_iMaxPicWidth != iTargetWidth ||
			p.m_iMaxPicHeight != iTargetHeight {
			p.m_iMaxPicWidth = iTargetWidth
			p.m_iMaxPicHeight = iTargetHeight
		}
		/* Check every field whether there is new request for memory block changed or else, Oct. 24, 2008 */
		if WelsEncoderParamAdjust(&p.m_pEncContext, sConfig) != 0 {
			return int32(api.CmInitParaError)
		}

		//LogStatistics
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_EXT, LogStatisticsBeforeNewEncoding")
		p.LogStatistics(p.m_pEncContext.iLastStatisticsLogTs, sEncodingParam.ISpatialLayerNum-1)

	case api.ENCODER_OPTION_FRAME_RATE: // Maximal input frame rate
		pValue, ok := pOption.(*float32)
		if !ok {
			return int32(api.CmInitParaError)
		}
		iValue := *pValue
		if iValue <= 0 {
			return int32(api.CmInitParaError)
		}
		//adjust to valid range
		p.m_pEncContext.pSvcParam.FMaxFrameRate = common.WELS_CLIP3(iValue, MIN_FRAME_RATE, MAX_FRAME_RATE)
		WelsEncoderApplyFrameRate(p.m_pEncContext.pSvcParam)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_FRAME_RATE,m_pEncContext->pSvcParam->fMaxFrameRate= %f",
			p.m_pEncContext.pSvcParam.FMaxFrameRate)

	case api.ENCODER_OPTION_BITRATE: // Target bit-rate
		pInfo, ok := pOption.(*api.SBitrateInfo)
		if !ok {
			return int32(api.CmInitParaError)
		}
		iBitrate := pInfo.IBitrate
		if iBitrate <= 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_BITRATE,iBitrate = %d",
				iBitrate)
			return int32(api.CmInitParaError)
		}
		iBitrate = common.WELS_CLIP3(iBitrate, MIN_BIT_RATE, MAX_BIT_RATE)
		switch pInfo.ILayer {
		case api.SPATIAL_LAYER_ALL:
			p.m_pEncContext.pSvcParam.ITargetBitrate = iBitrate
		case api.SPATIAL_LAYER_0:
			p.m_pEncContext.pSvcParam.SSpatialLayers[0].ISpatialBitrate = iBitrate
		case api.SPATIAL_LAYER_1:
			p.m_pEncContext.pSvcParam.SSpatialLayers[1].ISpatialBitrate = iBitrate
		case api.SPATIAL_LAYER_2:
			p.m_pEncContext.pSvcParam.SSpatialLayers[2].ISpatialBitrate = iBitrate
		case api.SPATIAL_LAYER_3:
			p.m_pEncContext.pSvcParam.SSpatialLayers[3].ISpatialBitrate = iBitrate
		default:
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_BITRATE,iLayer = %d",
				pInfo.ILayer)
			return int32(api.CmInitParaError)
		}
		//adjust to valid range
		if WelsEncoderApplyBitRate(&p.m_pWelsTrace.M_sLogCtx, p.m_pEncContext.pSvcParam, int32(pInfo.ILayer)) != 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_BITRATE layerId= %d,iSpatialBitrate = %d", pInfo.ILayer, iBitrate)
			return int32(api.CmInitParaError)
		} else {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_BITRATE layerId= %d,iSpatialBitrate = %d", pInfo.ILayer, iBitrate)
		}

	case api.ENCODER_OPTION_MAX_BITRATE: // Target bit-rate
		pInfo, ok := pOption.(*api.SBitrateInfo)
		if !ok {
			return int32(api.CmInitParaError)
		}
		iBitrate := pInfo.IBitrate
		if iBitrate <= 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_MAX_BITRATE,iBitrate = %d",
				iBitrate)
			return int32(api.CmInitParaError)
		}
		iBitrate = common.WELS_CLIP3(iBitrate, MIN_BIT_RATE, MAX_BIT_RATE)
		switch pInfo.ILayer {
		case api.SPATIAL_LAYER_ALL:
			p.m_pEncContext.pSvcParam.IMaxBitrate = iBitrate
		case api.SPATIAL_LAYER_0:
			p.m_pEncContext.pSvcParam.SSpatialLayers[0].IMaxSpatialBitrate = iBitrate
		case api.SPATIAL_LAYER_1:
			p.m_pEncContext.pSvcParam.SSpatialLayers[1].IMaxSpatialBitrate = iBitrate
		case api.SPATIAL_LAYER_2:
			p.m_pEncContext.pSvcParam.SSpatialLayers[2].IMaxSpatialBitrate = iBitrate
		case api.SPATIAL_LAYER_3:
			p.m_pEncContext.pSvcParam.SSpatialLayers[3].IMaxSpatialBitrate = iBitrate
		default:
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_MAX_BITRATE,iLayer = %d",
				pInfo.ILayer)
			return int32(api.CmInitParaError)
		}
		//adjust to valid range
		if WelsEncoderApplyBitRate(&p.m_pWelsTrace.M_sLogCtx, p.m_pEncContext.pSvcParam, int32(pInfo.ILayer)) != 0 {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_BITRATE layerId= %d,iMaxSpatialBitrate = %d", pInfo.ILayer, iBitrate)
			return int32(api.CmInitParaError)
		} else {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_BITRATE layerId= %d,iMaxSpatialBitrate = %d", pInfo.ILayer, iBitrate)
		}

	case api.ENCODER_OPTION_RC_MODE: // 0:quality mode;1:bit-rate mode;2:bitrate limited mode
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.IRCMode = api.RC_MODES(iValue)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_RC_MODE iRCMode= %d (Note: not suggest changing RC-mode in middle of encoding)",
			iValue)
		WelsRcInitFuncPointers(p.m_pEncContext, p.m_pEncContext.pSvcParam.IRCMode)

	case api.ENCODER_OPTION_RC_FRAME_SKIP: // 0:FRAME-SKIP disabled;1:FRAME-SKIP enabled
		bValue, ok := welsEncExtOptGetBool(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		if p.m_pEncContext.pSvcParam.IRCMode != api.RC_OFF_MODE {
			p.m_pEncContext.pSvcParam.BEnableFrameSkip = bValue
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_RC_FRAME_SKIP, frame-skip setting(%d)",
				welsEncExtBoolToInt32(bValue))
		} else {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_RC_FRAME_SKIP, rc off, frame-skip setting(%d) un-useful",
				welsEncExtBoolToInt32(bValue))
		}

	case api.ENCODER_PADDING_PADDING: // 0:disable padding;1:padding
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.IPaddingFlag = iValue
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_PADDING_PADDING iPaddingFlag= %d ",
			iValue)

	case api.ENCODER_LTR_RECOVERY_REQUEST:
		pLTR_Recover_Request, ok := pOption.(*api.SLTRRecoverRequest)
		if !ok {
			return int32(api.CmInitParaError)
		}
		FilterLTRRecoveryRequest(p.m_pEncContext, pLTR_Recover_Request)

	case api.ENCODER_LTR_MARKING_FEEDBACK:
		fb, ok := pOption.(*api.SLTRMarkingFeedback)
		if !ok {
			return int32(api.CmInitParaError)
		}
		FilterLTRMarkingFeedback(p.m_pEncContext, fb)

	case api.ENCODER_LTR_MARKING_PERIOD:
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.ILtrMarkPeriod = uint32(iValue)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_LTR_MARKING_PERIOD iLtrMarkPeriod= %d ",
			uint32(iValue))

	case api.ENCODER_OPTION_LTR:
		pLTRValue, ok := pOption.(*api.SLTRConfig)
		if !ok {
			return int32(api.CmInitParaError)
		}
		if WelsEncoderApplyLTR(&p.m_pWelsTrace.M_sLogCtx, &p.m_pEncContext, pLTRValue) != 0 {
			return int32(api.CmInitParaError)
		}
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_LTR,expected bEnableLongTermReference = %d,expeced iLTRRefNum = %d,actual bEnableLongTermReference = %d,actual iLTRRefNum = %d",
			welsEncExtBoolToInt32(pLTRValue.BEnableLongTermReference), pLTRValue.ILTRRefNum,
			welsEncExtBoolToInt32(p.m_pEncContext.pSvcParam.BEnableLongTermReference),
			p.m_pEncContext.pSvcParam.ILTRRefNum)

	case api.ENCODER_OPTION_ENABLE_SSEI:
		iValue, ok := welsEncExtOptGetBool(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.BEnableSSEI = iValue
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			" CWelsH264SVCEncoder::SetOption enable SSEI = %d -- this is not supported yet",
			welsEncExtBoolToInt32(p.m_pEncContext.pSvcParam.BEnableSSEI))

	case api.ENCODER_OPTION_ENABLE_PREFIX_NAL_ADDING:
		iValue, ok := welsEncExtOptGetBool(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.BPrefixNalAddingCtrl = iValue
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, " CWelsH264SVCEncoder::SetOption bPrefixNalAddingCtrl = %d ",
			welsEncExtBoolToInt32(p.m_pEncContext.pSvcParam.BPrefixNalAddingCtrl))

	case api.ENCODER_OPTION_SPS_PPS_ID_STRATEGY:
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		eNewStrategy := api.CONSTANT_ID
		switch iValue {
		case 0:
			eNewStrategy = api.CONSTANT_ID
		case 0x01:
			eNewStrategy = api.INCREASING_ID
		case 0x02:
			eNewStrategy = api.SPS_LISTING
		case 0x03:
			eNewStrategy = api.SPS_LISTING_AND_PPS_INCREASING
		case 0x06:
			eNewStrategy = api.SPS_PPS_LISTING
		default:
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				" CWelsH264SVCEncoder::SetOption eSpsPpsIdStrategy(%d) not in valid range, unchanged! existing=%d",
				iValue, p.m_pEncContext.pSvcParam.ESpsPpsIdStrategy)
		}

		if ((eNewStrategy&api.SPS_LISTING) != 0 || (p.m_pEncContext.pSvcParam.ESpsPpsIdStrategy&api.SPS_LISTING) != 0) &&
			p.m_pEncContext.pSvcParam.ESpsPpsIdStrategy != eNewStrategy {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				" CWelsH264SVCEncoder::SetOption eSpsPpsIdStrategy changing in the middle of call is NOT allowed for eSpsPpsIdStrategy>INCREASING_ID: existing setting is %d and the new one is %d",
				p.m_pEncContext.pSvcParam.ESpsPpsIdStrategy, iValue)
			return int32(api.CmInitParaError)
		}
		sConfig := new(SWelsSvcCodingParam)
		*sConfig = *p.m_pEncContext.pSvcParam // memcpy
		sConfig.ESpsPpsIdStrategy = eNewStrategy
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO, " CWelsH264SVCEncoder::SetOption eSpsPpsIdStrategy = %d ",
			sConfig.ESpsPpsIdStrategy)

		if WelsEncoderParamAdjust(&p.m_pEncContext, sConfig) != 0 {
			return int32(api.CmInitParaError)
		}

	case api.ENCODER_OPTION_CURRENT_PATH:
		if p.m_pEncContext.pSvcParam != nil {
			switch path := pOption.(type) {
			case *string:
				p.m_pEncContext.pSvcParam.pCurPath = *path
			case string:
				p.m_pEncContext.pSvcParam.pCurPath = path
			default:
				return int32(api.CmInitParaError)
			}
		}

	case api.ENCODER_OPTION_DUMP_FILE:
		// ENABLE_FRAME_DUMP is not ported: nothing to do.

	case api.ENCODER_OPTION_TRACE_LEVEL:
		if p.m_pWelsTrace != nil {
			level, ok := welsEncExtOptGetInt32(pOption)
			if !ok {
				return int32(api.CmInitParaError)
			}
			p.m_pWelsTrace.SetTraceLevel(level)
		}

	case api.ENCODER_OPTION_TRACE_CALLBACK:
		if p.m_pWelsTrace != nil {
			var callback api.WelsTraceCallback
			switch cb := pOption.(type) {
			case *api.WelsTraceCallback:
				callback = *cb
			case api.WelsTraceCallback:
				callback = cb
			case func(ctx any, level int32, msg string):
				callback = cb
			default:
				return int32(api.CmInitParaError)
			}
			p.m_pWelsTrace.SetTraceCallback(callback)
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_TRACE_CALLBACK callback = %p.",
				callback)
		}

	case api.ENCODER_OPTION_TRACE_CALLBACK_CONTEXT:
		if p.m_pWelsTrace != nil {
			var ctx any
			if pp, ok := pOption.(*any); ok {
				ctx = *pp
			} else {
				ctx = pOption
			}
			p.m_pWelsTrace.SetTraceCallbackContext(ctx)
		}

	case api.ENCODER_OPTION_PROFILE:
		pProfileInfo, ok := pOption.(*api.SProfileInfo)
		if !ok {
			return int32(api.CmInitParaError)
		}
		if (pProfileInfo.ILayer < int32(api.SPATIAL_LAYER_0)) || (pProfileInfo.ILayer > int32(api.SPATIAL_LAYER_3)) {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_PROFILE,iLayer = %d(rang0-3)", pProfileInfo.ILayer)
			return int32(api.CmInitParaError)
		}
		CheckProfileSetting(&p.m_pWelsTrace.M_sLogCtx, p.m_pEncContext.pSvcParam, pProfileInfo.ILayer,
			pProfileInfo.UiProfileIdc)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_PROFILE,layerId = %d,expected profile = %d,actual profile = %d",
			pProfileInfo.ILayer, pProfileInfo.UiProfileIdc,
			p.m_pEncContext.pSvcParam.SSpatialLayers[pProfileInfo.ILayer].UiProfileIdc)

	case api.ENCODER_OPTION_LEVEL:
		pLevelInfo, ok := pOption.(*api.SLevelInfo)
		if !ok {
			return int32(api.CmInitParaError)
		}
		if (pLevelInfo.ILayer < int32(api.SPATIAL_LAYER_0)) || (pLevelInfo.ILayer > int32(api.SPATIAL_LAYER_3)) {
			common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_ERROR,
				"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_PROFILE,iLayer = %d(rang0-3)", pLevelInfo.ILayer)
			return int32(api.CmInitParaError)
		}
		CheckLevelSetting(&p.m_pWelsTrace.M_sLogCtx, p.m_pEncContext.pSvcParam, pLevelInfo.ILayer, pLevelInfo.UiLevelIdc)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_LEVEL,layerId = %d,expected level = %d,actual level = %d",
			pLevelInfo.ILayer, pLevelInfo.UiLevelIdc, p.m_pEncContext.pSvcParam.SSpatialLayers[pLevelInfo.ILayer].UiLevelIdc)

	case api.ENCODER_OPTION_NUMBER_REF:
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		CheckReferenceNumSetting(&p.m_pWelsTrace.M_sLogCtx, p.m_pEncContext.pSvcParam, iValue)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_NUMBER_REF,expected refNum = %d,actual refnum = %d", iValue,
			p.m_pEncContext.pSvcParam.INumRefFrame)

	case api.ENCODER_OPTION_DELIVERY_STATUS:
		pValue, ok := pOption.(*api.SDeliveryStatus)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.bDeliveryFlag = pValue.BDeliveryFlag
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_DEBUG,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_DELIVERY_STATUS,bDeliveryFlag = %d", welsEncExtBoolToInt32(pValue.BDeliveryFlag))

	case api.ENCODER_OPTION_COMPLEXITY:
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.IComplexityMode = api.ECOMPLEXITY_MODE(iValue)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_COMPLEXITY,iComplexityMode = %d", iValue)

	case api.ENCODER_OPTION_GET_STATISTICS:
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_WARNING,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_GET_STATISTICS: this option is get-only!")

	case api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL:
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.iStatisticsLogInterval = iValue
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_STATISTICS_LOG_INTERVAL,iStatisticsLogInterval = %d", iValue)

	case api.ENCODER_OPTION_IS_LOSSLESS_LINK:
		bValue, ok := welsEncExtOptGetBool(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.BIsLosslessLink = bValue
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_IS_LOSSLESS_LINK,bIsLosslessLink = %d", welsEncExtBoolToInt32(bValue))

	case api.ENCODER_OPTION_BITS_VARY_PERCENTAGE:
		iValue, ok := welsEncExtOptGetInt32(pOption)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.iBitsVaryPercentage = common.WELS_CLIP3(iValue, 0, 100)
		WelsEncoderApplyBitVaryRang(&p.m_pWelsTrace.M_sLogCtx, p.m_pEncContext.pSvcParam,
			p.m_pEncContext.pSvcParam.iBitsVaryPercentage)
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::SetOption():ENCODER_OPTION_BITS_VARY_PERCENTAGE,iBitsVaryPercentage = %d", iValue)

	default:
		return int32(api.CmInitParaError)
	}

	return 0
}

// GetOption ports CWelsH264SVCEncoder::GetOption. pOption (void* in C) is
// any holding a pointer that receives the option value.
func (p *CWelsH264SVCEncoder) GetOption(eOptionId api.ENCODER_OPTION, pOption any) int32 {
	if welsEncExtOptionIsNil(pOption) {
		return int32(api.CmInitParaError)
	}
	if nil == p.m_pEncContext || false == p.m_bInitialFlag {
		return int32(api.CmInitExpected)
	}

	switch eOptionId {
	case api.ENCODER_OPTION_INTER_SPATIAL_PRED: // Inter spatial layer prediction flag
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"ENCODER_OPTION_INTER_SPATIAL_PRED, this feature not supported at present.")

	case api.ENCODER_OPTION_DATAFORMAT: // Input color space
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::GetOption():ENCODER_OPTION_DATAFORMAT, m_iCspInternal= 0x%x", p.m_iCspInternal)
		if !welsEncExtOptSetInt32(pOption, p.m_iCspInternal) {
			return int32(api.CmInitParaError)
		}

	case api.ENCODER_OPTION_IDR_INTERVAL: // IDR Interval
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::GetOption():ENCODER_OPTION_IDR_INTERVAL, uiIntraPeriod= %d",
			p.m_pEncContext.pSvcParam.UiIntraPeriod)
		if !welsEncExtOptSetInt32(pOption, int32(p.m_pEncContext.pSvcParam.UiIntraPeriod)) {
			return int32(api.CmInitParaError)
		}

	case api.ENCODER_OPTION_SVC_ENCODE_PARAM_EXT: // SVC Encoding Parameter
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::GetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_EXT")
		pExt, ok := pOption.(*api.SEncParamExt)
		if !ok {
			return int32(api.CmInitParaError)
		}
		*pExt = p.m_pEncContext.pSvcParam.SEncParamExt // memcpy

	case api.ENCODER_OPTION_SVC_ENCODE_PARAM_BASE: // SVC Encoding Parameter
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::GetOption():ENCODER_OPTION_SVC_ENCODE_PARAM_BASE")
		pBase, ok := pOption.(*api.SEncParamBase)
		if !ok {
			return int32(api.CmInitParaError)
		}
		p.m_pEncContext.pSvcParam.GetBaseParams(pBase)

	case api.ENCODER_OPTION_FRAME_RATE: // Maximal input frame rate
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::GetOption():ENCODER_OPTION_FRAME_RATE, fMaxFrameRate = %.6ff",
			p.m_pEncContext.pSvcParam.FMaxFrameRate)
		pValue, ok := pOption.(*float32)
		if !ok {
			return int32(api.CmInitParaError)
		}
		*pValue = p.m_pEncContext.pSvcParam.FMaxFrameRate

	case api.ENCODER_OPTION_BITRATE: // Target bit-rate
		pInfo, ok := pOption.(*api.SBitrateInfo)
		if !ok {
			return int32(api.CmInitParaError)
		}
		if (pInfo.ILayer != api.SPATIAL_LAYER_ALL) && (pInfo.ILayer != api.SPATIAL_LAYER_0) && (pInfo.ILayer != api.SPATIAL_LAYER_1) &&
			(pInfo.ILayer != api.SPATIAL_LAYER_2) && (pInfo.ILayer != api.SPATIAL_LAYER_3) {
			return int32(api.CmInitParaError)
		}
		if pInfo.ILayer == api.SPATIAL_LAYER_ALL {
			pInfo.IBitrate = p.m_pEncContext.pSvcParam.ITargetBitrate
		} else {
			pInfo.IBitrate = p.m_pEncContext.pSvcParam.SSpatialLayers[pInfo.ILayer].ISpatialBitrate
		}
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::GetOption():ENCODER_OPTION_BITRATE, layerId =%d,iBitrate = %d",
			pInfo.ILayer, pInfo.IBitrate)

	case api.ENCODER_OPTION_MAX_BITRATE: // Target bit-rate
		pInfo, ok := pOption.(*api.SBitrateInfo)
		if !ok {
			return int32(api.CmInitParaError)
		}
		if (pInfo.ILayer != api.SPATIAL_LAYER_ALL) && (pInfo.ILayer != api.SPATIAL_LAYER_0) && (pInfo.ILayer != api.SPATIAL_LAYER_1) &&
			(pInfo.ILayer != api.SPATIAL_LAYER_2) && (pInfo.ILayer != api.SPATIAL_LAYER_3) {
			return int32(api.CmInitParaError)
		}
		if pInfo.ILayer == api.SPATIAL_LAYER_ALL {
			pInfo.IBitrate = p.m_pEncContext.pSvcParam.IMaxBitrate
		} else {
			pInfo.IBitrate = p.m_pEncContext.pSvcParam.SSpatialLayers[pInfo.ILayer].IMaxSpatialBitrate
		}
		common.WelsLog(&p.m_pWelsTrace.M_sLogCtx, api.WELS_LOG_INFO,
			"CWelsH264SVCEncoder::GetOption():ENCODER_OPTION_MAX_BITRATE,, layerId =%d,iBitrate = %d",
			pInfo.ILayer, pInfo.IBitrate)

	case api.ENCODER_OPTION_GET_STATISTICS:
		pStatistics, ok := pOption.(*api.SEncoderStatistics)
		if !ok {
			return int32(api.CmInitParaError)
		}
		pEncStatistics := &p.m_pEncContext.sEncoderStatistics[p.m_pEncContext.pSvcParam.ISpatialLayerNum-1]
		pStatistics.UiWidth = pEncStatistics.UiWidth
		pStatistics.UiHeight = pEncStatistics.UiHeight
		pStatistics.FAverageFrameSpeedInMs = pEncStatistics.FAverageFrameSpeedInMs

		// rate control related
		pStatistics.FAverageFrameRate = pEncStatistics.FAverageFrameRate
		pStatistics.FLatestFrameRate = pEncStatistics.FLatestFrameRate
		pStatistics.UiBitRate = pEncStatistics.UiBitRate
		pStatistics.UiAverageFrameQP = pEncStatistics.UiAverageFrameQP

		pStatistics.UiInputFrameCount = pEncStatistics.UiInputFrameCount
		pStatistics.UiSkippedFrameCount = pEncStatistics.UiSkippedFrameCount

		pStatistics.UiResolutionChangeTimes = pEncStatistics.UiResolutionChangeTimes
		pStatistics.UiIDRReqNum = pEncStatistics.UiIDRReqNum
		pStatistics.UiIDRSentNum = pEncStatistics.UiIDRSentNum
		pStatistics.UiLTRSentNum = pEncStatistics.UiLTRSentNum

	case api.ENCODER_OPTION_STATISTICS_LOG_INTERVAL:
		if !welsEncExtOptSetInt32(pOption, p.m_pEncContext.iStatisticsLogInterval) {
			return int32(api.CmInitParaError)
		}

	case api.ENCODER_OPTION_COMPLEXITY:
		if !welsEncExtOptSetInt32(pOption, int32(p.m_pEncContext.pSvcParam.IComplexityMode)) {
			return int32(api.CmInitParaError)
		}

	default:
		return int32(api.CmInitParaError)
	}

	return 0
}

// DumpSrcPicture is only active with DUMP_SRC_PICTURE, which is not ported.
func (p *CWelsH264SVCEncoder) DumpSrcPicture(pSrcPic *api.SSourcePicture, iUsageType int32) {
}

// ISVCEncoder** ppEncoder -> *api.ISVCEncoder.
func WelsCreateSVCEncoder(ppEncoder *api.ISVCEncoder) int32 {
	if ppEncoder == nil {
		return 1
	}
	pEnc := NewCWelsH264SVCEncoder()
	if pEnc != nil {
		*ppEncoder = pEnc
		return 0
	}

	return 1
}

func WelsDestroySVCEncoder(pEncoder api.ISVCEncoder) {
	pSVCEncoder, _ := pEncoder.(*CWelsH264SVCEncoder)

	if pSVCEncoder != nil {
		pSVCEncoder.Destruct()
	}
}

func WelsGetCodecVersion() api.OpenH264Version {
	return api.G_stCodecVersion
}

func WelsGetCodecVersionEx(pVersion *api.OpenH264Version) {
	*pVersion = api.G_stCodecVersion
}

// ---------------------------------------------------------------------------
// Go-only helpers for the void* option values.

// welsEncExtOptionIsNil reports whether pOption corresponds to a C NULL pointer.
func welsEncExtOptionIsNil(pOption any) bool {
	if pOption == nil {
		return true
	}
	rv := reflect.ValueOf(pOption)
	switch rv.Kind() {
	case reflect.Pointer, reflect.Func, reflect.Interface, reflect.Map, reflect.Slice:
		return rv.IsNil()
	}
	return false
}

// welsEncExtOptGetInt32 reads *(int32_t*)pOption. Pointers to the 32-bit integer
// enum types of package api, to int and to uint32 are accepted as well.
func welsEncExtOptGetInt32(pOption any) (int32, bool) {
	switch v := pOption.(type) {
	case *int32:
		return *v, true
	case *uint32:
		return int32(*v), true
	case *int:
		return int32(*v), true
	case *api.EVideoFormatType:
		return int32(*v), true
	case *api.RC_MODES:
		return int32(*v), true
	case *api.ECOMPLEXITY_MODE:
		return int32(*v), true
	case *api.EParameterSetStrategy:
		return int32(*v), true
	}
	return 0, false
}

// welsEncExtOptSetInt32 writes *(int32_t*)pOption = v.
func welsEncExtOptSetInt32(pOption any, v int32) bool {
	switch d := pOption.(type) {
	case *int32:
		*d = v
	case *uint32:
		*d = uint32(v)
	case *int:
		*d = int(v)
	case *api.EVideoFormatType:
		*d = api.EVideoFormatType(v)
	case *api.ECOMPLEXITY_MODE:
		*d = api.ECOMPLEXITY_MODE(v)
	default:
		return false
	}
	return true
}

// welsEncExtOptGetBool reads *(bool*)pOption.
func welsEncExtOptGetBool(pOption any) (bool, bool) {
	if v, ok := pOption.(*bool); ok {
		return *v, true
	}
	return false, false
}

func welsEncExtBoolToInt32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}
