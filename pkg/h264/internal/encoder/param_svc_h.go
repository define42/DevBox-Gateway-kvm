// Port of codec/encoder/core/inc/param_svc.h.

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const INVALID_TEMPORAL_ID = uint8(0xff)

// g_kuiTemporalIdListTable is defined in encoder_data_tables.go.

// GetLogFactor gets Logarithms base 2 of (upper/base).
// Returns the 2 based scaling factor, or math.MaxUint32 (UINT_MAX) when
// upper/base is not a power of two.
func GetLogFactor(base float32, upper float32) uint32 {
	dLog2factor := math.Log10(1.0*float64(upper)/float64(base)) / math.Log10(2.0)
	const dEpsilon = 0.0001
	dRound := math.Floor(dLog2factor + 0.5)

	if dLog2factor < dRound+dEpsilon && dRound < dLog2factor+dEpsilon {
		return uint32(dRound)
	}
	return math.MaxUint32
}

// SSpatialLayerInternal is the dependency layer parameter (internal part).
// (sRecFileName only exists with ENABLE_FRAME_DUMP and is not ported.)
type SSpatialLayerInternal struct {
	iActualWidth           int32 // input source picture actual width
	iActualHeight          int32 // input source picture actual height
	iTemporalResolution    int32
	iDecompositionStages   int32
	uiCodingIdx2TemporalId [(1 << MAX_TEMPORAL_LEVEL) + 1]uint8

	iHighestTemporalId int8
	fInputFrameRate    float32 // input frame rate
	fOutputFrameRate   float32 // output frame rate

	uiIdrPicId          uint16 // IDR picture id: [0, 65535], this one is used for LTR
	iCodingIndex        int32
	iFrameIndex         int32 // count how many frames elapsed during coding context currently
	bEncCurFrmAsIdrFlag bool
	iFrameNum           int32 // current frame number coding
	iPOC                int32 // frame iPOC
}

// SUsedPicRect is the rect in input picture that encoder actually used
// (anonymous struct SWelsSvcCodingParam.SUsedPicRect in C).
type SUsedPicRect struct {
	iLeft   int32
	iTop    int32
	iWidth  int32
	iHeight int32
}

// SWelsSvcCodingParam is the Cisco OpenH264 Encoder Parameter Configuration
// (struct TagWelsSvcCodingParam: SEncParamExt). The SEncParamExt base is
// embedded, so its fields are reached as p.IPicWidth, p.SSpatialLayers, ...
//
// Note: C AllocCodingParam allocates it zeroed WITHOUT running the
// constructor; use NewSWelsSvcCodingParam where C++ constructs one.
type SWelsSvcCodingParam struct {
	api.SEncParamExt

	sDependencyLayers [MAX_DEPENDENCY_LAYER]SSpatialLayerInternal

	/* General */
	uiGopSize    uint32 // GOP size (at maximal frame rate: 16)
	SUsedPicRect SUsedPicRect
	pCurPath     string // C char*: record current lib path such as:/pData/pData/com.wels.enc/lib/ ("" == NULL)

	bDeblockingParallelFlag bool // deblocking filter parallelization control flag
	iBitsVaryPercentage     int32
	iDecompStages           int8 // GOP size dependency
	iMaxNumRefFrame         int32
}

// NewSWelsSvcCodingParam is the C++ constructor TagWelsSvcCodingParam().
func NewSWelsSvcCodingParam() *SWelsSvcCodingParam {
	p := &SWelsSvcCodingParam{}
	p.FillDefault()
	return p
}

// FillDefaultParam is the static C++ overload
// TagWelsSvcCodingParam::FillDefault (SEncParamExt& param).
func FillDefaultParam(param *api.SEncParamExt) {
	*param = api.SEncParamExt{}

	param.UiIntraPeriod = 0                     // intra period (multiple of GOP size as desired)
	param.INumRefFrame = api.AUTO_REF_PIC_COUNT // number of reference frame used

	param.IPicWidth = 0                  // actual input picture width
	param.IPicHeight = 0                 // actual input picture height
	param.FMaxFrameRate = MAX_FRAME_RATE // maximal frame rate [Hz / fps]

	param.IComplexityMode = api.LOW_COMPLEXITY
	param.ITargetBitrate = api.UNSPECIFIED_BIT_RATE // overall target bitrate introduced in RC module
	param.IMaxBitrate = api.UNSPECIFIED_BIT_RATE
	param.IMultipleThreadIdc = 1
	param.BUseLoadBalancing = true

	param.ILTRRefNum = 0
	param.ILtrMarkPeriod = 30 // the min distance of two int32_t references

	param.BEnableSSEI = false
	param.BSimulcastAVC = false
	param.BEnableFrameCroppingFlag = true // enable frame cropping flag: true alwayse in application
	// false: Streaming Video Sharing; true: Video Conferencing Meeting;

	/* Deblocking loop filter */
	param.ILoopFilterDisableIdc = 0    // 0: on, 1: off, 2: on except for slice boundaries
	param.ILoopFilterAlphaC0Offset = 0 // AlphaOffset: valid range [-6, 6], default 0
	param.ILoopFilterBetaOffset = 0    // BetaOffset:  valid range [-6, 6], default 0

	/* Rate Control */
	param.IRCMode = api.RC_QUALITY_MODE
	param.IPaddingFlag = 0
	param.IEntropyCodingModeFlag = 0
	param.BEnableDenoise = false                // denoise control
	param.BEnableSceneChangeDetect = true       // scene change detection control
	param.BEnableBackgroundDetection = true     // background detection control
	param.BEnableAdaptiveQuant = true           // adaptive quantization control
	param.BEnableFrameSkip = true               // frame skipping
	param.BEnableLongTermReference = false      // long term reference control
	param.ESpsPpsIdStrategy = api.INCREASING_ID // pSps pPps id addition control
	param.BPrefixNalAddingCtrl = false          // prefix NAL adding control
	param.ISpatialLayerNum = 1                  // number of dependency(Spatial/CGS) layers used to be encoded
	param.ITemporalLayerNum = 1                 // number of temporal layer specified

	param.IMaxQp = QP_MAX_VALUE
	param.IMinQp = QP_MIN_VALUE
	param.IUsageType = api.CAMERA_VIDEO_REAL_TIME
	param.UiMaxNalSize = 0
	param.BIsLosslessLink = false
	param.BFixRCOverShoot = true
	param.IIdrBitrateRatio = IDR_BITRATE_RATIO * 100
	param.BPsnrY = false
	param.BPsnrU = false
	param.BPsnrV = false
	for iLayer := 0; iLayer < api.MAX_SPATIAL_LAYER_NUM; iLayer++ {
		pLayer := &param.SSpatialLayers[iLayer]
		pLayer.UiProfileIdc = api.PRO_UNKNOWN
		pLayer.UiLevelIdc = api.LEVEL_UNKNOWN
		pLayer.IDLayerQp = SVC_QUALITY_BASE_QP
		pLayer.FFrameRate = param.FMaxFrameRate
		pLayer.IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE

		pLayer.SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
		pLayer.SSliceArgument.UiSliceNum = 0 // AUTO, using number of CPU cores
		pLayer.SSliceArgument.UiSliceSizeConstraint = 1500

		pLayer.BAspectRatioPresent = false // do not write any of the following information to the header
		pLayer.EAspectRatio = api.ASP_UNSPECIFIED
		pLayer.SAspectRatioExtWidth = 0
		pLayer.SAspectRatioExtHeight = 0

		kiLesserSliceNum := MAX_SLICES_NUM
		if api.MAX_SLICES_NUM_TMP < MAX_SLICES_NUM {
			kiLesserSliceNum = api.MAX_SLICES_NUM_TMP
		}
		for idx := 0; idx < kiLesserSliceNum; idx++ {
			pLayer.SSliceArgument.UiSliceMbNum[idx] = 0 // default, using one row a slice if uiSliceMode is SM_RASTER_MODE
		}

		// See codec_app_def.h for more info about members bVideoSignalTypePresent through uiColorMatrix.  The default values
		// used below preserve the previous behavior; i.e., no additional information will be written to the output file.
		pLayer.BVideoSignalTypePresent = false                  // do not write any of the following information to the header
		pLayer.UiVideoFormat = uint8(api.VF_UNDEF)              // undefined
		pLayer.BFullRange = false                               // analog video data range [16, 235]
		pLayer.BColorDescriptionPresent = false                 // do not write any of the following three items to the header
		pLayer.UiColorPrimaries = uint8(api.CP_UNDEF)           // undefined
		pLayer.UiTransferCharacteristics = uint8(api.TRC_UNDEF) // undefined
		pLayer.UiColorMatrix = uint8(api.CM_UNDEF)              // undefined
	}
}

// FillDefault is the member C++ overload TagWelsSvcCodingParam::FillDefault().
func (p *SWelsSvcCodingParam) FillDefault() {
	FillDefaultParam(&p.SEncParamExt)
	p.uiGopSize = 1 // GOP size (at maximal frame rate: 16)
	p.iMaxNumRefFrame = api.AUTO_REF_PIC_COUNT
	p.SUsedPicRect.iLeft = 0
	p.SUsedPicRect.iTop = 0
	p.SUsedPicRect.iWidth = 0
	p.SUsedPicRect.iHeight = 0 // the rect in input picture that encoder actually used

	p.pCurPath = ""                   // record current lib path such as:/pData/pData/com.wels.enc/lib/
	p.bDeblockingParallelFlag = false // deblocking filter parallelization control flag
	p.iDecompStages = 0               // GOP size dependency, unknown here and be revised later
	p.iBitsVaryPercentage = 10
}

// ParamBaseTranscode applies the basic parameters.
func (p *SWelsSvcCodingParam) ParamBaseTranscode(pCodingParam *api.SEncParamBase) int32 {
	p.FMaxFrameRate = common.WELS_CLIP3(pCodingParam.FMaxFrameRate, MIN_FRAME_RATE, MAX_FRAME_RATE)
	p.ITargetBitrate = pCodingParam.ITargetBitrate
	p.IUsageType = pCodingParam.IUsageType
	p.IPicWidth = pCodingParam.IPicWidth
	p.IPicHeight = pCodingParam.IPicHeight

	p.SUsedPicRect.iLeft = 0
	p.SUsedPicRect.iTop = 0
	p.SUsedPicRect.iWidth = ((p.IPicWidth >> 1) * (1 << 1))
	p.SUsedPicRect.iHeight = ((p.IPicHeight >> 1) * (1 << 1))

	p.IRCMode = pCodingParam.IRCMode // rc mode

	iIdxSpatial := int8(0)
	uiProfileIdc := api.PRO_UNKNOWN
	if p.IEntropyCodingModeFlag != 0 {
		uiProfileIdc = api.PRO_MAIN
	}
	pDlpIdx := 0 // SSpatialLayerInternal* pDlp = &sDependencyLayers[0];

	for int32(iIdxSpatial) < p.ISpatialLayerNum {
		pDlp := &p.sDependencyLayers[pDlpIdx]
		// note: the C code writes sSpatialLayers->X (i.e. layer 0) for some fields
		p.SSpatialLayers[0].UiProfileIdc = uiProfileIdc
		p.SSpatialLayers[0].UiLevelIdc = api.LEVEL_UNKNOWN
		p.SSpatialLayers[iIdxSpatial].FFrameRate = common.WELS_CLIP3(pCodingParam.FMaxFrameRate,
			MIN_FRAME_RATE, MAX_FRAME_RATE)
		pDlp.fOutputFrameRate = common.WELS_CLIP3(p.SSpatialLayers[iIdxSpatial].FFrameRate, MIN_FRAME_RATE,
			MAX_FRAME_RATE)
		pDlp.fInputFrameRate = pDlp.fOutputFrameRate

		p.SSpatialLayers[iIdxSpatial].IVideoWidth = p.IPicWidth
		pDlp.iActualWidth = p.SSpatialLayers[iIdxSpatial].IVideoWidth
		p.SSpatialLayers[iIdxSpatial].IVideoHeight = p.IPicHeight
		pDlp.iActualHeight = p.SSpatialLayers[iIdxSpatial].IVideoHeight

		p.SSpatialLayers[iIdxSpatial].ISpatialBitrate = pCodingParam.ITargetBitrate // target bitrate for current spatial layer
		p.SSpatialLayers[0].ISpatialBitrate = p.SSpatialLayers[iIdxSpatial].ISpatialBitrate

		p.SSpatialLayers[0].IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE

		p.SSpatialLayers[0].IDLayerQp = SVC_QUALITY_BASE_QP

		if !p.BSimulcastAVC {
			uiProfileIdc = api.PRO_SCALABLE_BASELINE
		}
		pDlpIdx++
		iIdxSpatial++
	}
	p.SetActualPicResolution()

	return 0
}

// GetBaseParams copies the basic parameters out.
func (p *SWelsSvcCodingParam) GetBaseParams(pCodingParam *api.SEncParamBase) {
	pCodingParam.IUsageType = p.IUsageType
	pCodingParam.IPicWidth = p.IPicWidth
	pCodingParam.IPicHeight = p.IPicHeight
	pCodingParam.ITargetBitrate = p.ITargetBitrate
	pCodingParam.IRCMode = p.IRCMode
	pCodingParam.FMaxFrameRate = p.FMaxFrameRate
}

// ParamTranscode applies the extension parameters.
func (p *SWelsSvcCodingParam) ParamTranscode(pCodingParam *api.SEncParamExt) int32 {
	fParamMaxFrameRate := common.WELS_CLIP3(pCodingParam.FMaxFrameRate, MIN_FRAME_RATE, MAX_FRAME_RATE)

	p.IUsageType = pCodingParam.IUsageType
	p.IPicWidth = pCodingParam.IPicWidth
	p.IPicHeight = pCodingParam.IPicHeight
	p.FMaxFrameRate = fParamMaxFrameRate
	p.IComplexityMode = pCodingParam.IComplexityMode

	p.SUsedPicRect.iLeft = 0
	p.SUsedPicRect.iTop = 0
	p.SUsedPicRect.iWidth = ((p.IPicWidth >> 1) << 1)
	p.SUsedPicRect.iHeight = ((p.IPicHeight >> 1) << 1)

	p.IMultipleThreadIdc = pCodingParam.IMultipleThreadIdc
	p.BUseLoadBalancing = pCodingParam.BUseLoadBalancing

	/* Deblocking loop filter */
	p.ILoopFilterDisableIdc = pCodingParam.ILoopFilterDisableIdc       // 0: on, 1: off, 2: on except for slice boundaries,
	p.ILoopFilterAlphaC0Offset = pCodingParam.ILoopFilterAlphaC0Offset // AlphaOffset: valid range [-6, 6], default 0
	p.ILoopFilterBetaOffset = pCodingParam.ILoopFilterBetaOffset       // BetaOffset:  valid range [-6, 6], default 0

	p.IEntropyCodingModeFlag = pCodingParam.IEntropyCodingModeFlag
	p.BEnableFrameCroppingFlag = pCodingParam.BEnableFrameCroppingFlag

	/* Rate Control */
	p.IRCMode = pCodingParam.IRCMode // rc mode
	p.BSimulcastAVC = pCodingParam.BSimulcastAVC
	p.IPaddingFlag = pCodingParam.IPaddingFlag

	p.ITargetBitrate = pCodingParam.ITargetBitrate // target bitrate
	p.IMaxBitrate = pCodingParam.IMaxBitrate
	if (p.IMaxBitrate != api.UNSPECIFIED_BIT_RATE) && (p.IMaxBitrate < p.ITargetBitrate) {
		p.IMaxBitrate = p.ITargetBitrate
	}
	p.IMaxQp = pCodingParam.IMaxQp
	p.IMinQp = pCodingParam.IMinQp
	p.UiMaxNalSize = pCodingParam.UiMaxNalSize
	/* Denoise Control */
	p.BEnableDenoise = pCodingParam.BEnableDenoise // Denoise Control  // only support 0 or 1 now

	/* Scene change detection control */
	p.BEnableSceneChangeDetect = pCodingParam.BEnableSceneChangeDetect

	/* Background detection Control */
	p.BEnableBackgroundDetection = pCodingParam.BEnableBackgroundDetection

	/* Adaptive quantization control */
	p.BEnableAdaptiveQuant = pCodingParam.BEnableAdaptiveQuant

	/* Frame skipping */
	p.BEnableFrameSkip = pCodingParam.BEnableFrameSkip

	/* Enable int32_t term reference */
	p.BEnableLongTermReference = pCodingParam.BEnableLongTermReference
	p.ILtrMarkPeriod = pCodingParam.ILtrMarkPeriod
	p.BIsLosslessLink = pCodingParam.BIsLosslessLink
	p.BFixRCOverShoot = pCodingParam.BFixRCOverShoot
	p.IIdrBitrateRatio = pCodingParam.IIdrBitrateRatio
	p.BPsnrY = pCodingParam.BPsnrY
	p.BPsnrU = pCodingParam.BPsnrU
	p.BPsnrV = pCodingParam.BPsnrV
	if p.IUsageType == api.SCREEN_CONTENT_REAL_TIME && !p.BIsLosslessLink && p.BEnableLongTermReference {
		p.BEnableLongTermReference = false
	}

	/* For ssei information */
	p.BEnableSSEI = pCodingParam.BEnableSSEI
	p.BSimulcastAVC = pCodingParam.BSimulcastAVC

	/* Layer definition */
	p.ISpatialLayerNum = int32(int8(common.WELS_CLIP3(pCodingParam.ISpatialLayerNum, 1,
		MAX_DEPENDENCY_LAYER))) // number of dependency(Spatial/CGS) layers used to be encoded
	p.ITemporalLayerNum = int32(int8(common.WELS_CLIP3(pCodingParam.ITemporalLayerNum, 1,
		MAX_TEMPORAL_LEVEL))) // number of temporal layer specified

	p.uiGopSize = uint32(1) << uint32(p.ITemporalLayerNum-1) // Override GOP size based temporal layer
	p.iDecompStages = int8(p.ITemporalLayerNum - 1)          // WELS_LOG2( uiGopSize );// GOP size dependency
	p.UiIntraPeriod = pCodingParam.UiIntraPeriod             // intra period (multiple of GOP size as desired)
	if p.UiIntraPeriod == math.MaxUint32 {                   // (uint32_t) (-1)
		p.UiIntraPeriod = 0
	} else if p.UiIntraPeriod&(p.uiGopSize-1) != 0 { // none multiple of GOP size
		p.UiIntraPeriod = ((p.UiIntraPeriod + p.uiGopSize - 1) / p.uiGopSize) * p.uiGopSize
	}

	if ((pCodingParam.INumRefFrame != api.AUTO_REF_PIC_COUNT) &&
		!((pCodingParam.INumRefFrame > MAX_REF_PIC_COUNT) || (pCodingParam.INumRefFrame < MIN_REF_PIC_COUNT))) ||
		((p.INumRefFrame != api.AUTO_REF_PIC_COUNT) && (pCodingParam.INumRefFrame == api.AUTO_REF_PIC_COUNT)) {
		p.INumRefFrame = pCodingParam.INumRefFrame
	}
	if (p.INumRefFrame != api.AUTO_REF_PIC_COUNT) && (p.INumRefFrame > p.iMaxNumRefFrame) {
		p.iMaxNumRefFrame = p.INumRefFrame
	}
	if pCodingParam.BEnableLongTermReference {
		p.ILTRRefNum = pCodingParam.ILTRRefNum
	} else {
		p.ILTRRefNum = 0
	}
	p.ILtrMarkPeriod = pCodingParam.ILtrMarkPeriod

	p.BPrefixNalAddingCtrl = pCodingParam.BPrefixNalAddingCtrl

	if (api.CONSTANT_ID == pCodingParam.ESpsPpsIdStrategy) ||
		(api.INCREASING_ID == pCodingParam.ESpsPpsIdStrategy) ||
		(api.SPS_LISTING == pCodingParam.ESpsPpsIdStrategy) ||
		(api.SPS_LISTING_AND_PPS_INCREASING == pCodingParam.ESpsPpsIdStrategy) ||
		(api.SPS_PPS_LISTING == pCodingParam.ESpsPpsIdStrategy) {
		p.ESpsPpsIdStrategy = pCodingParam.ESpsPpsIdStrategy // For SVC meeting application, to avoid mosaic issue caused by cross-IDR reference.
		// SHOULD enable this feature.
	} else {
		// keep the default value
	}

	uiProfileIdc := api.PRO_BASELINE
	if p.IEntropyCodingModeFlag != 0 {
		uiProfileIdc = api.PRO_HIGH
	}
	iIdxSpatial := int8(0)
	for int32(iIdxSpatial) < p.ISpatialLayerNum {
		pDlp := &p.sDependencyLayers[iIdxSpatial]
		pSpatialLayer := &p.SSpatialLayers[iIdxSpatial]
		pSrcLayer := &pCodingParam.SSpatialLayers[iIdxSpatial]

		if pSrcLayer.UiProfileIdc == api.PRO_UNKNOWN {
			pSpatialLayer.UiProfileIdc = uiProfileIdc
		} else {
			pSpatialLayer.UiProfileIdc = pSrcLayer.UiProfileIdc
		}
		pSpatialLayer.UiLevelIdc = pSrcLayer.UiLevelIdc

		fLayerFrameRate := common.WELS_CLIP3(pSrcLayer.FFrameRate, MIN_FRAME_RATE, fParamMaxFrameRate)
		pDlp.fInputFrameRate = fParamMaxFrameRate
		pDlp.fOutputFrameRate = common.WELS_CLIP3(fLayerFrameRate, MIN_FRAME_RATE, fParamMaxFrameRate)
		pSpatialLayer.FFrameRate = pDlp.fOutputFrameRate

		pSpatialLayer.IVideoWidth = common.WELS_CLIP3(pSrcLayer.IVideoWidth, 0, p.IPicWidth)    // frame width
		pSpatialLayer.IVideoHeight = common.WELS_CLIP3(pSrcLayer.IVideoHeight, 0, p.IPicHeight) // frame height

		pSpatialLayer.ISpatialBitrate = pSrcLayer.ISpatialBitrate // target bitrate for current spatial layer
		pSpatialLayer.IMaxSpatialBitrate = pSrcLayer.IMaxSpatialBitrate

		if (p.ISpatialLayerNum == 1) && (iIdxSpatial == 0) {
			if pSpatialLayer.IVideoWidth == 0 {
				pSpatialLayer.IVideoWidth = p.IPicWidth
			}
			if pSpatialLayer.IVideoHeight == 0 {
				pSpatialLayer.IVideoHeight = p.IPicHeight
			}
			if pSpatialLayer.ISpatialBitrate == 0 {
				pSpatialLayer.ISpatialBitrate = p.ITargetBitrate
			}
			if pSpatialLayer.IMaxSpatialBitrate == 0 {
				pSpatialLayer.IMaxSpatialBitrate = p.IMaxBitrate
			}
		}

		// multi slice
		pSpatialLayer.SSliceArgument = pSrcLayer.SSliceArgument

		pSpatialLayer.IDLayerQp = pSrcLayer.IDLayerQp

		// See codec_app_def.h and parameter_sets.h for more info about members bVideoSignalTypePresent through uiColorMatrix.
		pSpatialLayer.BVideoSignalTypePresent = pSrcLayer.BVideoSignalTypePresent
		pSpatialLayer.UiVideoFormat = pSrcLayer.UiVideoFormat
		pSpatialLayer.BFullRange = pSrcLayer.BFullRange
		pSpatialLayer.BColorDescriptionPresent = pSrcLayer.BColorDescriptionPresent
		pSpatialLayer.UiColorPrimaries = pSrcLayer.UiColorPrimaries
		pSpatialLayer.UiTransferCharacteristics = pSrcLayer.UiTransferCharacteristics
		pSpatialLayer.UiColorMatrix = pSrcLayer.UiColorMatrix

		pSpatialLayer.BAspectRatioPresent = pSrcLayer.BAspectRatioPresent
		pSpatialLayer.EAspectRatio = pSrcLayer.EAspectRatio
		pSpatialLayer.SAspectRatioExtWidth = pSrcLayer.SAspectRatioExtWidth
		pSpatialLayer.SAspectRatioExtHeight = pSrcLayer.SAspectRatioExtHeight

		if !p.BSimulcastAVC {
			uiProfileIdc = api.PRO_SCALABLE_BASELINE // it is used in the D>0 layer if SVC is applied, so set to PRO_SCALABLE_BASELINE
		}
		iIdxSpatial++
	}

	p.SetActualPicResolution()

	return 0
}

// SetActualPicResolution assumes that the width/height ratio of all spatial layers are the same.
func (p *SWelsSvcCodingParam) SetActualPicResolution() {
	iSpatialIdx := p.ISpatialLayerNum - 1
	for ; iSpatialIdx >= 0; iSpatialIdx-- {
		pDlayerInternal := &p.sDependencyLayers[iSpatialIdx]
		pDlayer := &p.SSpatialLayers[iSpatialIdx]

		pDlayerInternal.iActualWidth = pDlayer.IVideoWidth
		pDlayerInternal.iActualHeight = pDlayer.IVideoHeight
		pDlayer.IVideoWidth = common.WELS_ALIGN(pDlayerInternal.iActualWidth, common.MB_WIDTH_LUMA)
		pDlayer.IVideoHeight = common.WELS_ALIGN(pDlayerInternal.iActualHeight, common.MB_HEIGHT_LUMA)
	}
}

// DetermineTemporalSettings determines key coding tables for temporal
// scalability, uiProfileIdc etc for each spatial layer settings (should
// ensure valid parameter before this procedure).
func (p *SWelsSvcCodingParam) DetermineTemporalSettings() int32 {
	iDecStages := common.WELS_LOG2(p.uiGopSize) // (int8_t)GetLogFactor(1.0f, 1.0f * pcfg->uiGopSize);  //log2(uiGopSize)
	pTemporalIdList := g_kuiTemporalIdListTable[iDecStages][:]
	i := int8(0)

	for int32(i) < p.ISpatialLayerNum {
		pDlp := &p.sDependencyLayers[i]
		kuiLogFactorInOutRate := GetLogFactor(pDlp.fOutputFrameRate, pDlp.fInputFrameRate)
		kuiLogFactorMaxInRate := GetLogFactor(pDlp.fInputFrameRate, p.FMaxFrameRate)
		if math.MaxUint32 == kuiLogFactorInOutRate || math.MaxUint32 == kuiLogFactorMaxInRate {
			return ENC_RETURN_INVALIDINPUT
		}
		iNotCodedMask := int32(0)
		iMaxTemporalId := int8(0)

		for k := range pDlp.uiCodingIdx2TemporalId {
			pDlp.uiCodingIdx2TemporalId[k] = INVALID_TEMPORAL_ID
		}
		iNotCodedMask = int32((uint32(1) << (kuiLogFactorInOutRate + kuiLogFactorMaxInRate)) - 1)
		for uiFrameIdx := uint32(0); uiFrameIdx <= p.uiGopSize; uiFrameIdx++ {
			if 0 == (uiFrameIdx & uint32(iNotCodedMask)) {
				kiTemporalId := int8(pTemporalIdList[uiFrameIdx])
				pDlp.uiCodingIdx2TemporalId[uiFrameIdx] = uint8(kiTemporalId)
				if kiTemporalId > iMaxTemporalId {
					iMaxTemporalId = kiTemporalId
				}
			}
		}

		pDlp.iHighestTemporalId = iMaxTemporalId
		pDlp.iTemporalResolution = int32(kuiLogFactorMaxInRate + kuiLogFactorInOutRate)
		pDlp.iDecompositionStages = int32(uint32(iDecStages) - kuiLogFactorMaxInRate - kuiLogFactorInOutRate)
		if pDlp.iDecompositionStages < 0 {
			return ENC_RETURN_INVALIDINPUT
		}
		i++
	}
	p.iDecompStages = int8(iDecStages)
	return ENC_RETURN_SUCCESS
}

// SExistingParasetList holds the parameter sets kept across re-initialisation.
type SExistingParasetList struct {
	sSps       [common.MAX_SPS_COUNT]SWelsSPS
	sSubsetSps [common.MAX_SPS_COUNT]SSubsetSps
	sPps       [MAX_PPS_COUNT]SWelsPPS

	uiInUseSpsNum       uint32
	uiInUseSubsetSpsNum uint32
	uiInUsePpsNum       uint32
}

// FreeCodingParam releases *pParam (CMemoryAlign parameter dropped).
func FreeCodingParam(pParam **SWelsSvcCodingParam) int32 {
	if pParam == nil || *pParam == nil {
		return 1
	}
	*pParam = nil
	return 0
}

// AllocCodingParam allocates a zeroed *pParam (the C++ constructor is NOT
// run, as with WelsMallocz). CMemoryAlign parameter dropped.
func AllocCodingParam(pParam **SWelsSvcCodingParam) int32 {
	if pParam == nil {
		return 1
	}
	if *pParam != nil {
		FreeCodingParam(pParam)
	}
	*pParam = &SWelsSvcCodingParam{}
	return 0
}
