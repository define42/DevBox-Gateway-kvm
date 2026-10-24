// Port of codec/encoder/core/src/encoder_ext.cpp (core encoder for SVC).
//
// The C++ code is translated function by function. The CMemoryAlign
// parameters are dropped (see doc.go); allocations use make/new and frees
// assign nil. The fixed-slice RDP encoder path uses bounded Go workers;
// persistent OS thread handles and joins are dropped.

package encoder

import (
	"math"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/processing"
)

// encExtB2I converts a bool to the int the C code passes to printf.
func encExtB2I(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

func WelsBitRateVerification(pLogCtx *common.SLogContext, pLayerParam *api.SSpatialLayerConfig, iLayerId int32) int32 {
	if (pLayerParam.ISpatialBitrate <= 0) ||
		(float32(pLayerParam.ISpatialBitrate) < pLayerParam.FFrameRate) {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "Invalid bitrate settings in layer %d, bitrate= %d at FrameRate(%f)", iLayerId,
			pLayerParam.ISpatialBitrate, pLayerParam.FFrameRate)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	// deal with LEVEL_MAX_BR and MAX_BR setting
	iLevelIdx := 0
	for (common.G_ksLevelLimits[iLevelIdx].UiLevelIdc != api.LEVEL_5_2) &&
		(common.G_ksLevelLimits[iLevelIdx].UiLevelIdc != pLayerParam.UiLevelIdc) {
		iLevelIdx++
	}
	pCurLevelLimit := &common.G_ksLevelLimits[iLevelIdx]
	iLevelMaxBitrate := int32(pCurLevelLimit.UiMaxBR * common.CpbBrNalFactor)
	iLevel52MaxBitrate := int32(common.G_ksLevelLimits[common.LEVEL_NUMBER-1].UiMaxBR * common.CpbBrNalFactor)
	if api.UNSPECIFIED_BIT_RATE != iLevelMaxBitrate {
		if (pLayerParam.IMaxSpatialBitrate == api.UNSPECIFIED_BIT_RATE) ||
			(pLayerParam.IMaxSpatialBitrate > iLevel52MaxBitrate) {
			pLayerParam.IMaxSpatialBitrate = iLevelMaxBitrate
			common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
				"Current MaxSpatialBitrate is invalid (UNSPECIFIED_BIT_RATE or larger than LEVEL5_2) but level setting is valid, set iMaxSpatialBitrate to %d from level (%d)",
				pLayerParam.IMaxSpatialBitrate, pLayerParam.UiLevelIdc)
		} else if pLayerParam.IMaxSpatialBitrate > iLevelMaxBitrate {
			iCurLevel := pLayerParam.UiLevelIdc
			WelsAdjustLevel(pLayerParam, pCurLevelLimit)
			common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
				"LevelIdc is changed from (%d) to (%d) according to the iMaxSpatialBitrate(%d)",
				iCurLevel, pLayerParam.UiLevelIdc, pLayerParam.IMaxSpatialBitrate)
		}
	} else if (pLayerParam.IMaxSpatialBitrate != api.UNSPECIFIED_BIT_RATE) &&
		(pLayerParam.IMaxSpatialBitrate > iLevel52MaxBitrate) {
		// no level limitation, just need to check if iMaxSpatialBitrate is too big from reasonable
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"No LevelIdc setting and iMaxSpatialBitrate (%d) is considered too big to be valid, changed to UNSPECIFIED_BIT_RATE",
			pLayerParam.IMaxSpatialBitrate)
		pLayerParam.IMaxSpatialBitrate = api.UNSPECIFIED_BIT_RATE
	}

	// deal with iSpatialBitrate and iMaxSpatialBitrate setting
	if pLayerParam.IMaxSpatialBitrate != api.UNSPECIFIED_BIT_RATE {
		if pLayerParam.IMaxSpatialBitrate == pLayerParam.ISpatialBitrate {
			common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
				"Setting MaxSpatialBitrate (%d) the same at SpatialBitrate (%d) will make the actual bit rate lower than SpatialBitrate",
				pLayerParam.IMaxSpatialBitrate, pLayerParam.ISpatialBitrate)
		} else if pLayerParam.IMaxSpatialBitrate < pLayerParam.ISpatialBitrate {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
				"MaxSpatialBitrate (%d) should be larger than SpatialBitrate (%d), considering it as error setting",
				pLayerParam.IMaxSpatialBitrate, pLayerParam.ISpatialBitrate)
			return ENC_RETURN_UNSUPPORTED_PARA
		}
	}
	return ENC_RETURN_SUCCESS
}

func CheckProfileSetting(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam, iLayer int32, uiProfileIdc api.EProfileIdc) {
	pLayerInfo := &pParam.SSpatialLayers[iLayer]
	pLayerInfo.UiProfileIdc = uiProfileIdc
	if pParam.BSimulcastAVC {
		if (uiProfileIdc != api.PRO_BASELINE) && (uiProfileIdc != api.PRO_MAIN) && (uiProfileIdc != api.PRO_HIGH) {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "layerId(%d) doesn't support profile(%d), change to UNSPECIFIC profile", iLayer,
				uiProfileIdc)
			pLayerInfo.UiProfileIdc = api.PRO_UNKNOWN
		}
	} else {
		if iLayer == int32(api.SPATIAL_LAYER_0) {
			if (uiProfileIdc != api.PRO_BASELINE) && (uiProfileIdc != api.PRO_MAIN) && (uiProfileIdc != api.PRO_HIGH) {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "layerId(%d) doesn't support profile(%d), change to UNSPECIFIC profile", iLayer,
					uiProfileIdc)
				pLayerInfo.UiProfileIdc = api.PRO_UNKNOWN
			}
		} else {
			if (uiProfileIdc != api.PRO_SCALABLE_BASELINE) && (uiProfileIdc != api.PRO_SCALABLE_HIGH) {
				pLayerInfo.UiProfileIdc = api.PRO_SCALABLE_BASELINE
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "layerId(%d) doesn't support profile(%d), change to scalable baseline profile",
					iLayer, uiProfileIdc)
			}
		}
	}
}

func CheckLevelSetting(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam, iLayer int32, uiLevelIdc api.ELevelIdc) {
	pLayerInfo := &pParam.SSpatialLayers[iLayer]
	pLayerInfo.UiLevelIdc = api.LEVEL_UNKNOWN
	iLevelIdx := int32(common.LEVEL_NUMBER - 1)
	for {
		if common.G_ksLevelLimits[iLevelIdx].UiLevelIdc == uiLevelIdc {
			pLayerInfo.UiLevelIdc = uiLevelIdc
			break
		}
		iLevelIdx--
		if !(iLevelIdx >= 0) {
			break
		}
	}
}

func CheckReferenceNumSetting(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam, iNumRef int32) {
	iRefUpperBound := int32(MAX_REFERENCE_PICTURE_COUNT_NUM_SCREEN)
	if pParam.IUsageType == api.CAMERA_VIDEO_REAL_TIME {
		iRefUpperBound = MAX_REFERENCE_PICTURE_COUNT_NUM_CAMERA
	}
	pParam.INumRefFrame = iNumRef
	if (iNumRef < MIN_REF_PIC_COUNT) || (iNumRef > iRefUpperBound) {
		pParam.INumRefFrame = api.AUTO_REF_PIC_COUNT
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"doesn't support the number of reference frame(%d) change to auto select mode", iNumRef)
	}
}

func SliceArgumentValidationFixedSliceMode(pLogCtx *common.SLogContext, pSliceArgument *api.SSliceArgument, kiRCMode api.RC_MODES, kiPicWidth int32, kiPicHeight int32) int32 {
	iCpuCores := int32(0)
	iIdx := 0
	iMbWidth := (kiPicWidth + 15) >> 4
	iMbHeight := (kiPicHeight + 15) >> 4
	iMbNumInFrame := iMbWidth * iMbHeight
	bSingleMode := false

	pSliceArgument.UiSliceSizeConstraint = 0

	if pSliceArgument.UiSliceNum == 0 {
		common.WelsCPUFeatureDetect(&iCpuCores)
		if 0 == iCpuCores {
			// cpuid not supported or doesn't expose the number of cores,
			// use high level system API as followed to detect number of pysical/logic processor
			iCpuCores = DynamicDetectCpuCores()
		}
		pSliceArgument.UiSliceNum = uint32(iCpuCores)
	}

	if pSliceArgument.UiSliceNum <= 1 {
		common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
			"SliceArgumentValidationFixedSliceMode(), uiSliceNum(%d) you set for SM_FIXEDSLCNUM_SLICE, now turn to SM_SINGLE_SLICE type!",
			pSliceArgument.UiSliceNum)
		bSingleMode = true
	}

	// considering the coding efficient and performance,
	// iCountMbNum constraint by MIN_NUM_MB_PER_SLICE condition of multi-pSlice mode settting
	if iMbNumInFrame <= MIN_NUM_MB_PER_SLICE {
		common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
			"SliceArgumentValidationFixedSliceMode(), uiSliceNum(%d) you set for SM_FIXEDSLCNUM_SLICE, now turn to SM_SINGLE_SLICE type as CountMbNum less than MIN_NUM_MB_PER_SLICE!",
			pSliceArgument.UiSliceNum)
		bSingleMode = true
	}

	if bSingleMode {
		pSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
		pSliceArgument.UiSliceNum = 1
		for iIdx = 0; iIdx < MAX_SLICES_NUM; iIdx++ {
			pSliceArgument.UiSliceMbNum[iIdx] = 0
		}
		return ENC_RETURN_SUCCESS
	}

	if pSliceArgument.UiSliceNum > MAX_SLICES_NUM {
		pSliceArgument.UiSliceNum = MAX_SLICES_NUM
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"SliceArgumentValidationFixedSliceMode(), uiSliceNum exceed MAX_SLICES_NUM! So setting slice num eqaul to MAX_SLICES_NUM(%d)!",
			pSliceArgument.UiSliceNum)
	}

	if kiRCMode != api.RC_OFF_MODE { // multiple slices verify with gom
		//check uiSliceNum and set uiSliceMbNum with current uiSliceNum
		if !GomValidCheckSliceNum(iMbWidth, iMbHeight, &pSliceArgument.UiSliceNum) {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"SliceArgumentValidationFixedSliceMode(), unsupported setting with Resolution and uiSliceNum combination under RC on! So uiSliceNum is changed to %d!",
				pSliceArgument.UiSliceNum)
		}

		if pSliceArgument.UiSliceNum <= 1 ||
			!GomValidCheckSliceMbNum(iMbWidth, iMbHeight, pSliceArgument) {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
				"SliceArgumentValidationFixedSliceMode(), unsupported setting with Resolution and uiSliceNum (%d) combination  under RC on! Consider setting single slice with this resolution!",
				pSliceArgument.UiSliceNum)
			return ENC_RETURN_UNSUPPORTED_PARA
		}
	} else if !CheckFixedSliceNumMultiSliceSetting(iMbNumInFrame, pSliceArgument) {
		//check uiSliceMbNum with current uiSliceNum
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
			"SliceArgumentValidationFixedSliceMode(), invalid uiSliceMbNum (%d) settings!,now turn to SM_SINGLE_SLICE type",
			pSliceArgument.UiSliceMbNum[0])
		pSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
		pSliceArgument.UiSliceNum = 1
		for iIdx = 0; iIdx < MAX_SLICES_NUM; iIdx++ {
			pSliceArgument.UiSliceMbNum[iIdx] = 0
		}
	}

	return ENC_RETURN_SUCCESS
}

// ParamValidation validates the parameter configuration.
// Returns 0 on success, otherwise none 0 for failed.
func ParamValidation(pLogCtx *common.SLogContext, pCfg *SWelsSvcCodingParam) int32 {
	const fEpsn = float32(0.000001)
	i := int32(0)

	if !(pCfg.IUsageType < api.INPUT_CONTENT_TYPE_ALL) {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidation(),Invalid usage type = %d", pCfg.IUsageType)
		return ENC_RETURN_UNSUPPORTED_PARA
	}
	if pCfg.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		if pCfg.ISpatialLayerNum > 1 {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidation(),Invalid the number of Spatial layer(%d)for screen content",
				pCfg.ISpatialLayerNum)
			return ENC_RETURN_UNSUPPORTED_PARA
		}
		if pCfg.BEnableAdaptiveQuant {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"ParamValidation(), AdaptiveQuant(%d) is not supported yet for screen content, auto turned off",
				encExtB2I(pCfg.BEnableAdaptiveQuant))
			pCfg.BEnableAdaptiveQuant = false
		}
		if pCfg.BEnableBackgroundDetection {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"ParamValidation(), BackgroundDetection(%d) is not supported yet for screen content, auto turned off",
				encExtB2I(pCfg.BEnableBackgroundDetection))
			pCfg.BEnableBackgroundDetection = false
		}
		if pCfg.BEnableSceneChangeDetect == false {
			pCfg.BEnableSceneChangeDetect = true
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"ParamValidation(), screen change detection should be turned on, change bEnableSceneChangeDetect as true")
		}
	}

	//turn off adaptive quant now, algorithms needs to be refactored
	pCfg.BEnableAdaptiveQuant = false

	if pCfg.ISpatialLayerNum > 1 {
		for i = pCfg.ISpatialLayerNum - 1; i > 0; i-- {
			fDlpUp := &pCfg.SSpatialLayers[i]
			fDlp := &pCfg.SSpatialLayers[i-1]
			if (fDlp.IVideoWidth > fDlpUp.IVideoWidth) || (fDlp.IVideoHeight > fDlpUp.IVideoHeight) {
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
					"ParamValidation,Invalid resolution layer(%d) resolution(%d x %d) should be less than the upper spatial layer resolution(%d x %d) ",
					i, fDlp.IVideoWidth, fDlp.IVideoHeight, fDlpUp.IVideoWidth, fDlpUp.IVideoHeight)
				return ENC_RETURN_UNSUPPORTED_PARA
			}
		}
	}

	if !common.CheckInRangeCloseOpen(int16(pCfg.ILoopFilterDisableIdc), api.DEBLOCKING_IDC_0, api.DEBLOCKING_IDC_2+1) ||
		!common.CheckInRangeCloseOpen(int16(pCfg.ILoopFilterAlphaC0Offset), api.DEBLOCKING_OFFSET_MINUS, api.DEBLOCKING_OFFSET+1) ||
		!common.CheckInRangeCloseOpen(int16(pCfg.ILoopFilterBetaOffset), api.DEBLOCKING_OFFSET_MINUS, api.DEBLOCKING_OFFSET+1) {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
			"ParamValidation, Invalid iLoopFilterDisableIdc(%d) or iLoopFilterAlphaC0Offset(%d) or iLoopFilterBetaOffset(%d)!",
			pCfg.ILoopFilterDisableIdc, pCfg.ILoopFilterAlphaC0Offset, pCfg.ILoopFilterBetaOffset)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	for i = 0; i < pCfg.ISpatialLayerNum; i++ {
		fDlp := &pCfg.sDependencyLayers[i]
		pConfig := &pCfg.SSpatialLayers[i]
		if fDlp.fOutputFrameRate > fDlp.fInputFrameRate || (fDlp.fInputFrameRate >= -fEpsn &&
			fDlp.fInputFrameRate <= fEpsn) ||
			(fDlp.fOutputFrameRate >= -fEpsn && fDlp.fOutputFrameRate <= fEpsn) {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
				"Invalid settings in input frame rate(%.6f) or output frame rate(%.6f) of layer #%d config file..",
				fDlp.fInputFrameRate, fDlp.fOutputFrameRate, i)
			return ENC_RETURN_INVALIDINPUT
		}
		if math.MaxUint32 == GetLogFactor(fDlp.fOutputFrameRate, fDlp.fInputFrameRate) {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"AUTO CORRECT: Invalid settings in input frame rate(%.6f) and output frame rate(%.6f) of layer #%d config file: iResult of output frame rate divided by input frame rate should be power of 2(i.e,in/pOut=2^n). \n Auto correcting Output Framerate to Input Framerate %f!\n",
				fDlp.fInputFrameRate, fDlp.fOutputFrameRate, i, fDlp.fInputFrameRate)
			fDlp.fOutputFrameRate = fDlp.fInputFrameRate
			pConfig.FFrameRate = fDlp.fOutputFrameRate
		}
	}

	if (pCfg.IRCMode != api.RC_OFF_MODE) && (pCfg.IRCMode != api.RC_QUALITY_MODE) && (pCfg.IRCMode != api.RC_BUFFERBASED_MODE) &&
		(pCfg.IRCMode != api.RC_BITRATE_MODE) && (pCfg.IRCMode != api.RC_TIMESTAMP_MODE) {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidation(),Invalid iRCMode = %d", pCfg.IRCMode)
		return ENC_RETURN_UNSUPPORTED_PARA
	}
	//bitrate setting validation
	if pCfg.IRCMode != api.RC_OFF_MODE {
		iTotalBitrate := int32(0)
		if pCfg.ITargetBitrate <= 0 {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "Invalid bitrate settings in total configure, bitrate= %d", pCfg.ITargetBitrate)
			return ENC_RETURN_INVALIDINPUT
		}
		for i = 0; i < pCfg.ISpatialLayerNum; i++ {
			pSpatialLayer := &pCfg.SSpatialLayers[i]
			iTotalBitrate += pSpatialLayer.ISpatialBitrate

			if WelsBitRateVerification(pLogCtx, pSpatialLayer, i) != ENC_RETURN_SUCCESS {
				return ENC_RETURN_INVALIDINPUT
			}
		}
		if iTotalBitrate > pCfg.ITargetBitrate {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
				"Invalid settings in bitrate. the sum of each layer bitrate(%d) is larger than total bitrate setting(%d)",
				iTotalBitrate, pCfg.ITargetBitrate)
			return ENC_RETURN_INVALIDINPUT
		}
		if (pCfg.IRCMode == api.RC_QUALITY_MODE) || (pCfg.IRCMode == api.RC_BITRATE_MODE) || (pCfg.IRCMode == api.RC_TIMESTAMP_MODE) {
			if !pCfg.BEnableFrameSkip {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
					"bEnableFrameSkip = %d,bitrate can't be controlled for RC_QUALITY_MODE,RC_BITRATE_MODE and RC_TIMESTAMP_MODE without enabling skip frame.",
					encExtB2I(pCfg.BEnableFrameSkip))
			}
		}
		if (pCfg.IMaxQp <= 0) || (pCfg.IMinQp <= 0) {
			if pCfg.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
				common.WelsLog(pLogCtx, api.WELS_LOG_INFO, "Change QP Range from(%d,%d) to (%d,%d)", pCfg.IMinQp, pCfg.IMaxQp, MIN_SCREEN_QP,
					MAX_SCREEN_QP)
				pCfg.IMinQp = MIN_SCREEN_QP
				pCfg.IMaxQp = MAX_SCREEN_QP
			} else {
				common.WelsLog(pLogCtx, api.WELS_LOG_INFO, "Change QP Range from(%d,%d) to (%d,%d)", pCfg.IMinQp, pCfg.IMaxQp,
					GOM_MIN_QP_MODE, MAX_LOW_BR_QP)
				pCfg.IMinQp = GOM_MIN_QP_MODE
				pCfg.IMaxQp = MAX_LOW_BR_QP
			}
		}
		pCfg.IMinQp = common.WELS_CLIP3(pCfg.IMinQp, GOM_MIN_QP_MODE, QP_MAX_VALUE)
		pCfg.IMaxQp = common.WELS_CLIP3(pCfg.IMaxQp, pCfg.IMinQp, QP_MAX_VALUE)
	}
	// ref-frames validation
	var iRefCheck int32
	if (pCfg.IUsageType == api.CAMERA_VIDEO_REAL_TIME) || (pCfg.IUsageType == api.SCREEN_CONTENT_REAL_TIME) {
		iRefCheck = WelsCheckRefFrameLimitationNumRefFirst(pLogCtx, pCfg)
	} else {
		iRefCheck = WelsCheckRefFrameLimitationLevelIdcFirst(pLogCtx, pCfg)
	}
	if iRefCheck != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsCheckRefFrameLimitation failed")
		return ENC_RETURN_INVALIDINPUT
	}
	return ENC_RETURN_SUCCESS
}

func ParamValidationExt(pLogCtx *common.SLogContext, pCodingParam *SWelsSvcCodingParam) int32 {
	i := int8(0)
	iIdx := 0

	if nil == pCodingParam {
		return ENC_RETURN_INVALIDINPUT
	}

	if (pCodingParam.IUsageType != api.CAMERA_VIDEO_REAL_TIME) && (pCodingParam.IUsageType != api.SCREEN_CONTENT_REAL_TIME) {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(),Invalid usage type = %d", pCodingParam.IUsageType)
		return ENC_RETURN_UNSUPPORTED_PARA
	}
	if (pCodingParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME) && (!pCodingParam.BIsLosslessLink &&
		pCodingParam.BEnableLongTermReference) {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"ParamValidationExt(), setting lossy link for LTR under screen, which is not supported yet! Auto disabled LTR!")
		pCodingParam.BEnableLongTermReference = false
	}
	if pCodingParam.ISpatialLayerNum < 1 || pCodingParam.ISpatialLayerNum > MAX_DEPENDENCY_LAYER {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), monitor invalid pCodingParam->iSpatialLayerNum: %d!",
			pCodingParam.ISpatialLayerNum)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	if pCodingParam.ITemporalLayerNum < 1 || pCodingParam.ITemporalLayerNum > MAX_TEMPORAL_LEVEL {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), monitor invalid pCodingParam->iTemporalLayerNum: %d!",
			pCodingParam.ITemporalLayerNum)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	if pCodingParam.uiGopSize < 1 || pCodingParam.uiGopSize > MAX_GOP_SIZE {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), monitor invalid pCodingParam->uiGopSize: %d!",
			pCodingParam.uiGopSize)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	if pCodingParam.UiIntraPeriod != 0 && pCodingParam.UiIntraPeriod < pCodingParam.uiGopSize {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
			"ParamValidationExt(), uiIntraPeriod(%d) should be not less than that of uiGopSize(%d) or -1 specified!",
			pCodingParam.UiIntraPeriod, pCodingParam.uiGopSize)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	if pCodingParam.UiIntraPeriod != 0 && (pCodingParam.UiIntraPeriod&(pCodingParam.uiGopSize-1)) != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
			"ParamValidationExt(), uiIntraPeriod(%d) should be multiple of uiGopSize(%d) or -1 specified!",
			pCodingParam.UiIntraPeriod, pCodingParam.uiGopSize)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	//about iMultipleThreadIdc, bDeblockingParallelFlag, iLoopFilterDisableIdc, & uiSliceMode
	// (1) Single Thread
	//    if (THREAD==1)//single thread
	//            no parallel_deblocking: bDeblockingParallelFlag = 0;
	// (2) Multi Thread: see uiSliceMode decision
	if pCodingParam.IMultipleThreadIdc == 1 {
		//now is single thread. no parallel deblocking, set flag=0
		pCodingParam.bDeblockingParallelFlag = false
	} else {
		pCodingParam.bDeblockingParallelFlag = true
	}

	// eSpsPpsIdStrategy checkings
	if pCodingParam.ISpatialLayerNum > 1 && (!pCodingParam.BSimulcastAVC) &&
		(api.SPS_LISTING&pCodingParam.ESpsPpsIdStrategy) != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"ParamValidationExt(), eSpsPpsIdStrategy setting (%d) with multiple svc SpatialLayers (%d) not supported! eSpsPpsIdStrategy adjusted to CONSTANT_ID",
			pCodingParam.ESpsPpsIdStrategy, pCodingParam.ISpatialLayerNum)
		pCodingParam.ESpsPpsIdStrategy = api.CONSTANT_ID
	}
	if pCodingParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME && (api.SPS_LISTING&pCodingParam.ESpsPpsIdStrategy) != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			"ParamValidationExt(), eSpsPpsIdStrategy setting (%d) with iUsageType (%d) not supported! eSpsPpsIdStrategy adjusted to CONSTANT_ID",
			pCodingParam.ESpsPpsIdStrategy, pCodingParam.IUsageType)
		pCodingParam.ESpsPpsIdStrategy = api.CONSTANT_ID
	}

	if pCodingParam.BSimulcastAVC && (api.SPS_LISTING&pCodingParam.ESpsPpsIdStrategy) != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
			"ParamValidationExt(), eSpsPpsIdStrategy(%d) under bSimulcastAVC(%d) not supported yet, adjusted to INCREASING_ID",
			pCodingParam.ESpsPpsIdStrategy, encExtB2I(pCodingParam.BSimulcastAVC))
		pCodingParam.ESpsPpsIdStrategy = api.INCREASING_ID
	}

	if pCodingParam.BSimulcastAVC && pCodingParam.BPrefixNalAddingCtrl {
		common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
			"ParamValidationExt(), bSimulcastAVC(%d) is not compatible with bPrefixNalAddingCtrl(%d) true, adjusted bPrefixNalAddingCtrl to false",
			pCodingParam.ESpsPpsIdStrategy, encExtB2I(pCodingParam.BSimulcastAVC))
		pCodingParam.BPrefixNalAddingCtrl = false
	}

	for i = 0; int32(i) < pCodingParam.ISpatialLayerNum; i++ {
		pSpatialLayer := &pCodingParam.SSpatialLayers[i]
		kiPicWidth := pSpatialLayer.IVideoWidth
		kiPicHeight := pSpatialLayer.IVideoHeight
		kiMaxPixelsPerFrame := int64(common.MAX_MBS_PER_FRAME) << 8
		iMbWidth := uint32(0)
		iMbHeight := uint32(0)
		iMbNumInFrame := int32(0)
		iMaxSliceNum := uint32(MAX_SLICES_NUM)
		iReturn := int32(0)

		if (pCodingParam.IPicWidth > 0) && (pCodingParam.IPicHeight > 0) &&
			(kiPicWidth == 0) && (kiPicHeight == 0) &&
			(pCodingParam.ISpatialLayerNum == 1) {
			pSpatialLayer.IVideoWidth = pCodingParam.IPicWidth
			kiPicWidth = pSpatialLayer.IVideoWidth
			pSpatialLayer.IVideoHeight = pCodingParam.IPicHeight
			kiPicHeight = pSpatialLayer.IVideoHeight
			common.WelsLog(pLogCtx, api.WELS_LOG_DEBUG,
				"ParamValidationExt(), layer resolution is not set, set to general resolution %d x %d",
				pSpatialLayer.IVideoWidth, pSpatialLayer.IVideoHeight)
		}

		if (kiPicWidth <= 0) || (kiPicHeight <= 0) ||
			(int64(kiPicWidth)*int64(kiPicHeight) > kiMaxPixelsPerFrame) {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
				"ParamValidationExt(), width > 0, height > 0, width * height <= %d, invalid %d x %d in dependency layer settings!",
				(common.MAX_MBS_PER_FRAME << 8), kiPicWidth, kiPicHeight)
			return ENC_RETURN_UNSUPPORTED_PARA
		}
		if (kiPicWidth&0x0F) != 0 || (kiPicHeight&0x0F) != 0 {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
				"ParamValidationExt(), in layer #%d iWidth x iHeight(%d x %d) both should be multiple of 16, can not support with arbitrary size currently!",
				i, kiPicWidth, kiPicHeight)
			return ENC_RETURN_UNSUPPORTED_PARA
		}

		if pSpatialLayer.SSliceArgument.UiSliceMode >= api.SM_RESERVED {
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), invalid uiSliceMode (%d) settings!",
				pSpatialLayer.SSliceArgument.UiSliceMode)
			return ENC_RETURN_UNSUPPORTED_PARA
		}
		if (pCodingParam.UiMaxNalSize != 0) && (pSpatialLayer.SSliceArgument.UiSliceMode != api.SM_SIZELIMITED_SLICE) {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"ParamValidationExt(), current layer %d uiSliceMode (%d) settings may not fulfill MaxNalSize = %d", i,
				pSpatialLayer.SSliceArgument.UiSliceMode, pCodingParam.UiMaxNalSize)
		}
		CheckProfileSetting(pLogCtx, pCodingParam, int32(i), pSpatialLayer.UiProfileIdc)
		CheckLevelSetting(pLogCtx, pCodingParam, int32(i), pSpatialLayer.UiLevelIdc)
		//check pSlice settings under multi-pSlice
		if kiPicWidth <= 16 && kiPicHeight <= 16 {
			//only have one MB, set to single_slice
			pSpatialLayer.SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
		}
		switch pSpatialLayer.SSliceArgument.UiSliceMode {
		case api.SM_SINGLE_SLICE:
			pSpatialLayer.SSliceArgument.UiSliceNum = 1
			pSpatialLayer.SSliceArgument.UiSliceSizeConstraint = 0
			for iIdx = 0; iIdx < MAX_SLICES_NUM; iIdx++ {
				pSpatialLayer.SSliceArgument.UiSliceMbNum[iIdx] = 0
			}
		case api.SM_FIXEDSLCNUM_SLICE:
			iReturn = SliceArgumentValidationFixedSliceMode(pLogCtx, &pSpatialLayer.SSliceArgument, pCodingParam.IRCMode,
				kiPicWidth, kiPicHeight)
			if iReturn != 0 {
				return ENC_RETURN_UNSUPPORTED_PARA
			}
		case api.SM_RASTER_SLICE:
			pSpatialLayer.SSliceArgument.UiSliceSizeConstraint = 0

			iMbWidth = uint32((kiPicWidth + 15) >> 4)
			iMbHeight = uint32((kiPicHeight + 15) >> 4)
			iMbNumInFrame = int32(iMbWidth * iMbHeight)
			iMaxSliceNum = MAX_SLICES_NUM
			if pSpatialLayer.SSliceArgument.UiSliceMbNum[0] == 0 {
				if iMbHeight > iMaxSliceNum {
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), invalid uiSliceNum (%d) settings more than MAX(%d)!",
						iMbHeight, MAX_SLICES_NUM)
					return ENC_RETURN_UNSUPPORTED_PARA
				}
				pSpatialLayer.SSliceArgument.UiSliceNum = iMbHeight
				for j := uint32(0); j < iMbHeight; j++ {
					pSpatialLayer.SSliceArgument.UiSliceMbNum[j] = iMbWidth
				}
				if !CheckRowMbMultiSliceSetting(int32(iMbWidth),
					&pSpatialLayer.SSliceArgument) { // verify interleave mode settings
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), invalid uiSliceMbNum (%d) settings!",
						pSpatialLayer.SSliceArgument.UiSliceMbNum[0])
					return ENC_RETURN_UNSUPPORTED_PARA
				}
				break
			}

			if !CheckRasterMultiSliceSetting(iMbNumInFrame,
				&pSpatialLayer.SSliceArgument) { // verify interleave mode settings
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), invalid uiSliceMbNum (%d) settings!",
					pSpatialLayer.SSliceArgument.UiSliceMbNum[0])
				return ENC_RETURN_UNSUPPORTED_PARA
			}
			if pSpatialLayer.SSliceArgument.UiSliceNum <= 0 ||
				pSpatialLayer.SSliceArgument.UiSliceNum > iMaxSliceNum { // verify interleave mode settings
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), invalid uiSliceNum (%d) in SM_RASTER_SLICE settings!",
					pSpatialLayer.SSliceArgument.UiSliceNum)
				return ENC_RETURN_UNSUPPORTED_PARA
			}
			if pSpatialLayer.SSliceArgument.UiSliceNum == 1 {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
					"ParamValidationExt(), pSlice setting for SM_RASTER_SLICE now turn to SM_SINGLE_SLICE!")
				pSpatialLayer.SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
				break
			}
			if (pCodingParam.IRCMode != api.RC_OFF_MODE) && pSpatialLayer.SSliceArgument.UiSliceNum > 1 {
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), WARNING: GOM based RC do not support SM_RASTER_SLICE!")
			}
			// considering the coding efficient and performance, iCountMbNum constraint by MIN_NUM_MB_PER_SLICE condition of multi-pSlice mode settting
			if iMbNumInFrame <= MIN_NUM_MB_PER_SLICE {
				pSpatialLayer.SSliceArgument.UiSliceMode = api.SM_SINGLE_SLICE
				pSpatialLayer.SSliceArgument.UiSliceNum = 1
				break
			}
		case api.SM_SIZELIMITED_SLICE:
			iMbWidth = uint32((kiPicWidth + 15) >> 4)
			iMbHeight = uint32((kiPicHeight + 15) >> 4)
			if pSpatialLayer.SSliceArgument.UiSliceSizeConstraint <= MAX_MACROBLOCK_SIZE_IN_BYTE {
				common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
					"ParamValidationExt(), invalid iSliceSize (%d) settings!should be larger than  MAX_MACROBLOCK_SIZE_IN_BYTE(%d)",
					pSpatialLayer.SSliceArgument.UiSliceSizeConstraint, MAX_MACROBLOCK_SIZE_IN_BYTE)
				return ENC_RETURN_UNSUPPORTED_PARA
			}

			if pCodingParam.UiMaxNalSize > 0 {
				if pCodingParam.UiMaxNalSize < (NAL_HEADER_ADD_0X30BYTES + MAX_MACROBLOCK_SIZE_IN_BYTE) {
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
						"ParamValidationExt(), invalid uiMaxNalSize (%d) settings! should be larger than (NAL_HEADER_ADD_0X30BYTES + MAX_MACROBLOCK_SIZE_IN_BYTE)(%d)",
						pCodingParam.UiMaxNalSize, (NAL_HEADER_ADD_0X30BYTES + MAX_MACROBLOCK_SIZE_IN_BYTE))
					return ENC_RETURN_UNSUPPORTED_PARA
				}

				if pSpatialLayer.SSliceArgument.UiSliceSizeConstraint > (pCodingParam.UiMaxNalSize -
					NAL_HEADER_ADD_0X30BYTES) {
					common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
						"ParamValidationExt(), slice mode = SM_SIZELIMITED_SLICE, uiSliceSizeConstraint = %d ,uiMaxNalsize = %d, will take uiMaxNalsize!",
						pSpatialLayer.SSliceArgument.UiSliceSizeConstraint, pCodingParam.UiMaxNalSize)
					pSpatialLayer.SSliceArgument.UiSliceSizeConstraint = pCodingParam.UiMaxNalSize - NAL_HEADER_ADD_0X30BYTES
				}
			}
			pSpatialLayer.SSliceArgument.UiSliceSizeConstraint -= NAL_HEADER_ADD_0X30BYTES
		default:
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "ParamValidationExt(), invalid uiSliceMode (%d) settings!",
				pCodingParam.SSpatialLayers[0].SSliceArgument.UiSliceMode)
			return ENC_RETURN_UNSUPPORTED_PARA
		}
		_ = iMbWidth
		_ = iMbHeight
	}
	for i = 0; int32(i) < pCodingParam.ISpatialLayerNum; i++ {
		pLayerInfo := &pCodingParam.SSpatialLayers[i]
		if (pLayerInfo.UiProfileIdc == api.PRO_BASELINE) || (pLayerInfo.UiProfileIdc == api.PRO_SCALABLE_BASELINE) {
			if pCodingParam.IEntropyCodingModeFlag != 0 {
				pCodingParam.IEntropyCodingModeFlag = 0
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "layerId(%d) Profile is baseline, Change CABAC to CAVLC", i)
			}
		} else if pLayerInfo.UiProfileIdc == api.PRO_UNKNOWN {
			if (i == 0) || pCodingParam.BSimulcastAVC {
				if pCodingParam.IEntropyCodingModeFlag != 0 {
					pLayerInfo.UiProfileIdc = api.PRO_HIGH
				} else {
					pLayerInfo.UiProfileIdc = api.PRO_BASELINE
				}
			} else {
				pLayerInfo.UiProfileIdc = api.PRO_SCALABLE_BASELINE
			}
		}
	}
	return ParamValidation(pLogCtx, pCodingParam)
}

func WelsEncoderApplyFrameRate(pParam *SWelsSvcCodingParam) {
	const kfEpsn = float32(0.000001)
	kiNumLayer := pParam.ISpatialLayerNum
	kfMaxFrameRate := pParam.FMaxFrameRate
	var fRatio float32
	var fTargetOutputFrameRate float32

	//set input frame rate to each layer
	for i := int32(0); i < kiNumLayer; i++ {
		pLayerParamInternal := &pParam.sDependencyLayers[i]
		pLayerParam := &pParam.SSpatialLayers[i]
		fRatio = pLayerParamInternal.fOutputFrameRate / pLayerParamInternal.fInputFrameRate
		if (kfMaxFrameRate-pLayerParamInternal.fInputFrameRate) > kfEpsn ||
			(kfMaxFrameRate-pLayerParamInternal.fInputFrameRate) < -kfEpsn {
			pLayerParamInternal.fInputFrameRate = kfMaxFrameRate
			fTargetOutputFrameRate = float32(kfMaxFrameRate * fRatio)
			if fTargetOutputFrameRate >= 6 {
				pLayerParamInternal.fOutputFrameRate = fTargetOutputFrameRate
			} else {
				pLayerParamInternal.fOutputFrameRate = pLayerParamInternal.fInputFrameRate
			}
			pLayerParam.FFrameRate = pLayerParamInternal.fOutputFrameRate
			//TODO:{Sijia} from design, there is no sense to have temporal layer when under 6fps even with such setting?
		}
	}
}

func WelsEncoderApplyBitRate(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam, iLayer int32) int32 {
	//TODO (Sijia):  this is a temporary solution which keep the ratio between layers
	//but it is also possible to fulfill the bitrate of lower layer first

	iNumLayers := pParam.ISpatialLayerNum
	var i int32
	iOrigTotalBitrate := int32(0)
	if iLayer == int32(api.SPATIAL_LAYER_ALL) {
		//read old BR
		for i = 0; i < iNumLayers; i++ {
			iOrigTotalBitrate += pParam.SSpatialLayers[i].ISpatialBitrate
		}
		//write new BR
		fRatio := float32(0.0)
		for i = 0; i < iNumLayers; i++ {
			pLayerParam := &pParam.SSpatialLayers[i]
			fRatio = float32(pLayerParam.ISpatialBitrate) / float32(iOrigTotalBitrate)
			pLayerParam.ISpatialBitrate = int32(float32(float32(pParam.ITargetBitrate) * fRatio))

			if WelsBitRateVerification(pLogCtx, pLayerParam, i) != ENC_RETURN_SUCCESS {
				return ENC_RETURN_UNSUPPORTED_PARA
			}
		}
	} else {
		return WelsBitRateVerification(pLogCtx, &pParam.SSpatialLayers[iLayer], iLayer)
	}
	return ENC_RETURN_SUCCESS
}

func WelsEncoderApplyBitVaryRang(pLogCtx *common.SLogContext, pParam *SWelsSvcCodingParam, iRang int32) int32 {
	iNumLayers := pParam.ISpatialLayerNum
	for i := int32(0); i < iNumLayers; i++ {
		pLayerParam := &pParam.SSpatialLayers[i]
		pLayerParam.IMaxSpatialBitrate = common.WELS_MIN(int32(float64(pLayerParam.ISpatialBitrate)*(1+float64(iRang)/100.0)),
			pLayerParam.IMaxSpatialBitrate)
		if WelsBitRateVerification(pLogCtx, pLayerParam, i) != ENC_RETURN_SUCCESS {
			return ENC_RETURN_UNSUPPORTED_PARA
		}
		common.WelsLog(pLogCtx, api.WELS_LOG_INFO,
			"WelsEncoderApplyBitVaryRang:UpdateMaxBitrate layerId= %d,iMaxSpatialBitrate = %d", i, pLayerParam.IMaxSpatialBitrate)
	}
	return ENC_RETURN_SUCCESS
}

// AcquireLayersNals acquires the count number of layers and NALs based on
// configurable parameters dependency. Returns 0 on success.
func AcquireLayersNals(ppCtx **sWelsEncCtx, pParam *SWelsSvcCodingParam, pCountLayers *int32, pCountNals *int32) int32 {
	iCountNumLayers := int32(0)
	iCountNumNals := int32(0)
	iNumDependencyLayers := int32(0)
	iDIndex := int32(0)

	if nil == pParam || nil == ppCtx || nil == *ppCtx {
		return 1
	}

	iNumDependencyLayers = pParam.ISpatialLayerNum

	for {
		pDLayer := &pParam.SSpatialLayers[iDIndex]
		iOrgNumNals := iCountNumNals

		//Note: Sep. 2010
		//Review this part and suggest no change, since the memory over-use
		//(1) counts little to the overall performance
		//(2) should not be critial even under mobile case
		if api.SM_SIZELIMITED_SLICE == pDLayer.SSliceArgument.UiSliceMode {
			iCountNumNals += MAX_SLICES_NUM
			// plus prefix NALs
			if iDIndex == 0 {
				iCountNumNals += MAX_SLICES_NUM
			}
			// MAX_SLICES_NUM < MAX_LAYER_NUM_OF_FRAME ensured at svc_enc_slice_segment.h
			if iCountNumNals-iOrgNumNals > api.MAX_NAL_UNITS_IN_LAYER {
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_ERROR,
					"AcquireLayersNals(), num_of_slice(%d) > existing slice(%d) at (iDid= %d), max=%d",
					iCountNumNals, iOrgNumNals, iDIndex, api.MAX_NAL_UNITS_IN_LAYER)
				return 1
			}
		} else { /*if ( SM_SINGLE_SLICE != pDLayer->sSliceArgument.uiSliceMode )*/
			kiNumOfSlice := GetInitialSliceNum(&pDLayer.SSliceArgument)

			// NEED check iCountNals value in case multiple slices is used
			iCountNumNals += kiNumOfSlice // for pSlice VCL NALs
			// plus prefix NALs
			if iDIndex == 0 {
				iCountNumNals += kiNumOfSlice
			}
			if kiNumOfSlice > MAX_SLICES_NUM {
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_ERROR,
					"AcquireLayersNals(), num_of_slice(%d) > MAX_SLICES_NUM(%d) per (iDid= %d, qid= %d) settings!",
					kiNumOfSlice, MAX_SLICES_NUM, iDIndex, 0)
				return 1
			}
		}

		if iCountNumNals-iOrgNumNals > api.MAX_NAL_UNITS_IN_LAYER {
			common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_ERROR,
				"AcquireLayersNals(), num_of_nals(%d) > MAX_NAL_UNITS_IN_LAYER(%d) per (iDid= %d, qid= %d) settings!",
				(iCountNumNals - iOrgNumNals), api.MAX_NAL_UNITS_IN_LAYER, iDIndex, 0)
			return 1
		}

		iCountNumLayers++

		iDIndex++
		if !(iDIndex < iNumDependencyLayers) {
			break
		}
	}

	if nil == (*ppCtx).pFuncList || nil == (*ppCtx).pFuncList.pParametersetStrategy {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_ERROR,
			"AcquireLayersNals(), pFuncList and pParametersetStrategy needed to be initialized first!")
		return 1
	}
	// count parasets
	iCountNumNals = int32(uint32(iCountNumNals+1+iNumDependencyLayers+(iCountNumLayers<<1)+
		iCountNumLayers) + // plus iCountNumLayers for reserved application
		(*ppCtx).pFuncList.pParametersetStrategy.GetAllNeededParasetNum())

	// to check number of layers / nals / slices dependencies, 12/8/2010
	if iCountNumLayers > api.MAX_LAYER_NUM_OF_FRAME {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_ERROR, "AcquireLayersNals(), iCountNumLayers(%d) > MAX_LAYER_NUM_OF_FRAME(%d)!",
			iCountNumLayers, api.MAX_LAYER_NUM_OF_FRAME)
		return 1
	}

	if nil != pCountLayers {
		*pCountLayers = iCountNumLayers
	}
	if nil != pCountNals {
		*pCountNals = iCountNumNals
	}
	return 0
}

// InitMbInfo (static) initializes the MB list pList of layer pLayer.
func InitMbInfo(pEnc *sWelsEncCtx, pList []SMB, pLayer *SDqLayer, kiDlayerId int32, kiMaxMbNum int32) {
	iMbWidth := int32(pLayer.iMbWidth)
	iMbHeight := int32(pLayer.iMbHeight)
	iMbNum := iMbWidth * iMbHeight
	var uiNeighborAvail uint32
	kiOffset := int((kiDlayerId & 0x01) * kiMaxMbNum)

	for iIdx := int32(0); iIdx < iMbNum; iIdx++ {
		var bLeft, bTop, bLeftTop, bRightTop bool
		var iLeftXY, iTopXY, iLeftTopXY, iRightTopXY int32
		var uiSliceIdc uint16 //[0..65535] > 36864 of LEVEL5.2

		pMb := &pList[iIdx]
		pMb.iMbX = pEnc.pStrideTab.pMbIndexX[kiDlayerId][iIdx]
		pMb.iMbY = pEnc.pStrideTab.pMbIndexY[kiDlayerId][iIdx]
		pMb.iMbXY = iIdx

		uiSliceIdc = WelsMbToSliceIdc(pLayer, iIdx)
		iLeftXY = iIdx - 1
		iTopXY = iIdx - iMbWidth
		iLeftTopXY = iTopXY - 1
		iRightTopXY = iTopXY + 1

		bLeft = (pMb.iMbX > 0) && (uiSliceIdc == WelsMbToSliceIdc(pLayer, iLeftXY))
		bTop = (pMb.iMbY > 0) && (uiSliceIdc == WelsMbToSliceIdc(pLayer, iTopXY))
		bLeftTop = (pMb.iMbX > 0) && (pMb.iMbY > 0) && (uiSliceIdc ==
			WelsMbToSliceIdc(pLayer, iLeftTopXY))
		bRightTop = (int32(pMb.iMbX) < (iMbWidth - 1)) && (pMb.iMbY > 0) && (uiSliceIdc ==
			WelsMbToSliceIdc(pLayer, iRightTopXY))

		uiNeighborAvail = 0
		if bLeft {
			uiNeighborAvail |= LEFT_MB_POS
		}
		if bTop {
			uiNeighborAvail |= TOP_MB_POS
		}
		if bLeftTop {
			uiNeighborAvail |= TOPLEFT_MB_POS
		}
		if bRightTop {
			uiNeighborAvail |= TOPRIGHT_MB_POS
		}
		pMb.uiSliceIdc = uiSliceIdc // merge from svc_hd_opt_b for multiple slices coding
		pMb.uiNeighborAvail = uint8(uiNeighborAvail)
		// (the C code computes a second, unused neighbour mask here)
		uiNeighborAvail = 0
		if int32(pMb.iMbX) >= BASE_MV_MB_NMB {
			uiNeighborAvail |= LEFT_MB_POS
		}
		if int32(pMb.iMbX) <= (iMbWidth - 1 - BASE_MV_MB_NMB) {
			uiNeighborAvail |= RIGHT_MB_POS
		}
		if int32(pMb.iMbY) >= BASE_MV_MB_NMB {
			uiNeighborAvail |= TOP_MB_POS
		}
		if int32(pMb.iMbY) <= (iMbHeight - 1 - BASE_MV_MB_NMB) {
			uiNeighborAvail |= BOTTOM_MB_POS
		}

		kiMbOff := kiOffset + int(iIdx)
		pMb.sMv = pEnc.pMvUnitBlock4x4[common.MB_BLOCK4x4_NUM*kiMbOff : common.MB_BLOCK4x4_NUM*(kiMbOff+1)]
		pMb.pRefIndex = pEnc.pRefIndexBlock4x4[common.MB_BLOCK8x8_NUM*kiMbOff : common.MB_BLOCK8x8_NUM*(kiMbOff+1)]
		pMb.pSadCost = &pEnc.pSadCostMb[iIdx]
		pMb.pIntra4x4PredMode = pEnc.pIntra4x4PredModeBlocks[int(iIdx)*INTRA_4x4_MODE_NUM : int(iIdx+1)*INTRA_4x4_MODE_NUM]
		pMb.pNonZeroCount = pEnc.pNonZeroCountBlocks[int(iIdx)*MB_LUMA_CHROMA_BLOCK4x4_NUM : int(iIdx+1)*MB_LUMA_CHROMA_BLOCK4x4_NUM]
		pMb.pMbList = pList
	}
	_ = uiNeighborAvail
}

func InitMbListD(ppCtx **sWelsEncCtx) int32 {
	iNumDlayer := (*ppCtx).pSvcParam.ISpatialLayerNum
	var iMbSize [MAX_DEPENDENCY_LAYER]int32
	iOverallMbNum := int32(0)
	iMbWidth := int32(0)
	iMbHeight := int32(0)
	var i int32

	if iNumDlayer > MAX_DEPENDENCY_LAYER {
		return 1
	}

	for i = 0; i < iNumDlayer; i++ {
		iMbWidth = ((*ppCtx).pSvcParam.SSpatialLayers[i].IVideoWidth + 15) >> 4
		iMbHeight = ((*ppCtx).pSvcParam.SSpatialLayers[i].IVideoHeight + 15) >> 4
		iMbSize[i] = iMbWidth * iMbHeight
		iOverallMbNum += iMbSize[i]
	}

	(*ppCtx).ppMbListD = make([][]SMB, iNumDlayer)
	pAll := make([]SMB, iOverallMbNum)
	iOff := int32(0)
	// As in C, the layers' MB lists are views into one allocation; their
	// capacity extends to the end of it (GetRefMb may read past the end of
	// the reference layer's list into the following layers).
	(*ppCtx).ppMbListD[0] = pAll[iOff : iOff+iMbSize[0]]
	(*ppCtx).ppDqLayerList[0].sMbDataP = (*ppCtx).ppMbListD[0]
	InitMbInfo(*ppCtx, (*ppCtx).ppMbListD[0], (*ppCtx).ppDqLayerList[0], 0, iMbSize[iNumDlayer-1])
	for i = 1; i < iNumDlayer; i++ {
		iOff += iMbSize[i-1]
		(*ppCtx).ppMbListD[i] = pAll[iOff : iOff+iMbSize[i]]
		(*ppCtx).ppDqLayerList[i].sMbDataP = (*ppCtx).ppMbListD[i]
		InitMbInfo(*ppCtx, (*ppCtx).ppMbListD[i], (*ppCtx).ppDqLayerList[i], i, iMbSize[iNumDlayer-1])
	}

	return 0
}

// FreeSliceInLayer frees the slice buffers of all threads of a layer.
// CMemoryAlign* pMa dropped.
func FreeSliceInLayer(pDq *SDqLayer) {
	for iIdx := 0; iIdx < MAX_THREADS_NUM; iIdx++ {
		FreeSliceBuffer(&pDq.sSliceBufferInfo[iIdx].pSliceBuffer,
			pDq.sSliceBufferInfo[iIdx].iMaxSliceNum,
			"pSliceBuffer")
	}
}

// FreeDqLayer frees a DQ layer. SDqLayer*& pDq -> **SDqLayer; CMemoryAlign* pMa dropped.
func FreeDqLayer(pDq **SDqLayer) {
	if nil == *pDq {
		return
	}
	pLayer := *pDq

	FreeSliceInLayer(pLayer)

	if pLayer.ppSliceInLayer != nil {
		pLayer.ppSliceInLayer = nil
	}

	if pLayer.pFirstMbIdxOfSlice != nil {
		pLayer.pFirstMbIdxOfSlice = nil
	}

	if pLayer.pCountMbNumInSlice != nil {
		pLayer.pCountMbNumInSlice = nil
	}

	if pLayer.pFeatureSearchPreparation != nil {
		ReleaseFeatureSearchPreparation(&pLayer.pFeatureSearchPreparation.pFeatureOfBlock)
		pLayer.pFeatureSearchPreparation = nil
	}

	UninitSlicePEncCtx(pLayer)
	pLayer.iMaxSliceNum = 0

	*pDq = nil
}

// FreeRefList frees a reference list. SRefList*& pRefList -> **SRefList; CMemoryAlign* pMa dropped.
func FreeRefList(pRefList **SRefList, iMaxNumRefFrame int32) {
	if nil == *pRefList {
		return
	}
	pList := *pRefList

	iRef := int32(0)
	for {
		if pList.pRef[iRef] != nil {
			FreePicture(&pList.pRef[iRef])
		}
		iRef++
		if !(iRef < 1+iMaxNumRefFrame) {
			break
		}
	}

	*pRefList = nil
}

// InitDqLayers (static) initializes ppDqLayerList and slicepEncCtx_list due
// to count number of layers available. Returns 0 on success.
func InitDqLayers(ppCtx **sWelsEncCtx, pExistingParasetList *SExistingParasetList) int32 {
	var pParam *SWelsSvcCodingParam
	var pSps *SWelsSPS
	var pSubsetSps *SSubsetSps
	var pPps *SWelsPPS
	iDlayerCount := int32(0)
	iDlayerIndex := int32(0)
	iSpsId := int32(0)
	iPpsId := uint32(0)
	iNumRef := uint32(0)
	iResult := int32(0)

	if nil == ppCtx || nil == *ppCtx {
		return 1
	}

	pParam = (*ppCtx).pSvcParam
	iDlayerCount = pParam.ISpatialLayerNum
	iNumRef = uint32(pParam.iMaxNumRefFrame)

	const kiFeatureStrategyIndex = FME_DEFAULT_FEATURE_INDEX
	const kiMe16x16 = ME_DIA_CROSS
	const kiMe8x8 = ME_DIA_CROSS_FME
	kiNeedFeatureStorage := int32(0)
	if pParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		kiNeedFeatureStorage = int32((kiFeatureStrategyIndex << 16) + ((kiMe16x16 & 0x00FF) << 8) + (kiMe8x8 & 0x00FF))
	}

	iDlayerIndex = 0
	for iDlayerIndex < iDlayerCount {
		var pRefList *SRefList
		i := uint32(0)
		kiWidth := pParam.SSpatialLayers[iDlayerIndex].IVideoWidth
		kiHeight := pParam.SSpatialLayers[iDlayerIndex].IVideoHeight
		iPicWidth := common.WELS_ALIGN(kiWidth, common.MB_WIDTH_LUMA) + (common.PADDING_LENGTH << 1) // with iWidth of horizon
		iPicChromaWidth := iPicWidth >> 1

		iPicWidth = common.WELS_ALIGN(iPicWidth,
			32) // 32(or 16 for chroma below) to match original imp. here instead of iCacheLineSize
		iPicChromaWidth = common.WELS_ALIGN(iPicChromaWidth, 16)

		WelsGetEncBlockStrideOffset((*ppCtx).pStrideTab.pStrideEncBlockOffset[iDlayerIndex], iPicWidth, iPicChromaWidth)

		// pRef list
		pRefList = &SRefList{}
		for {
			iNeedFeatureStorage := int32(0)
			if iDlayerIndex == iDlayerCount-1 {
				iNeedFeatureStorage = kiNeedFeatureStorage
			}
			pRefList.pRef[i] = AllocPicture(kiWidth, kiHeight, true, iNeedFeatureStorage) // to use actual size of current layer
			if nil == pRefList.pRef[i] {
				FreeRefList(&pRefList, int32(iNumRef))
				return 1
			}
			i++
			if !(i < 1+iNumRef) {
				break
			}
		}

		pRefList.pNextBuffer = pRefList.pRef[0]
		(*ppCtx).ppRefPicListExt[iDlayerIndex] = pRefList
		iDlayerIndex++
	}

	iDlayerIndex = 0
	for iDlayerIndex < iDlayerCount {
		var pDqLayer *SDqLayer
		pDlayer := &pParam.SSpatialLayers[iDlayerIndex]
		pParamInternal := &pParam.sDependencyLayers[iDlayerIndex]
		kiMbW := (pDlayer.IVideoWidth + 0x0f) >> 4
		kiMbH := (pDlayer.IVideoHeight + 0x0f) >> 4

		pParamInternal.iCodingIndex = 0
		pParamInternal.iFrameIndex = 0
		pParamInternal.iFrameNum = 0
		pParamInternal.iPOC = 0
		pParamInternal.uiIdrPicId = 0
		pParamInternal.bEncCurFrmAsIdrFlag = true // make sure first frame is IDR
		// pDq layers list
		pDqLayer = &SDqLayer{}

		pDqLayer.bNeedAdjustingSlicing = false

		pDqLayer.iMbWidth = int16(kiMbW)
		pDqLayer.iMbHeight = int16(kiMbH)

		iMaxSliceNum := int32(1)
		kiSliceNum := GetInitialSliceNum(&pDlayer.SSliceArgument)
		if iMaxSliceNum < kiSliceNum {
			iMaxSliceNum = kiSliceNum
		}
		pDqLayer.iMaxSliceNum = iMaxSliceNum

		iResult = InitSliceInLayer(*ppCtx, pDqLayer, iDlayerIndex)
		if iResult != 0 {
			common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "InitDqLayers(), InitSliceInLayer failed(%d)!", iResult)
			FreeDqLayer(&pDqLayer)
			return iResult
		}

		//deblocking parameters initialization
		//target-layer deblocking
		pDqLayer.iLoopFilterDisableIdc = uint8(pParam.ILoopFilterDisableIdc)
		pDqLayer.iLoopFilterAlphaC0Offset = int8((pParam.ILoopFilterAlphaC0Offset) << 1)
		pDqLayer.iLoopFilterBetaOffset = int8((pParam.ILoopFilterBetaOffset) << 1)
		//parallel deblocking
		pDqLayer.bDeblockingParallelFlag = pParam.bDeblockingParallelFlag

		//deblocking parameter adjustment
		if api.SM_SINGLE_SLICE == pDlayer.SSliceArgument.UiSliceMode {
			//iLoopFilterDisableIdc: will be 0 or 1 under single_slice
			if 2 == pParam.ILoopFilterDisableIdc {
				pDqLayer.iLoopFilterDisableIdc = 0
			}
			//bDeblockingParallelFlag
			pDqLayer.bDeblockingParallelFlag = false
		} else {
			//multi-pSlice
			if 0 == pDqLayer.iLoopFilterDisableIdc {
				pDqLayer.bDeblockingParallelFlag = false
			}
		}

		//
		if kiNeedFeatureStorage != 0 && iDlayerIndex == iDlayerCount-1 {
			pDqLayer.pFeatureSearchPreparation = &SFeatureSearchPreparation{}
			iReturn := RequestFeatureSearchPreparation(pDlayer.IVideoWidth, pDlayer.IVideoHeight,
				kiNeedFeatureStorage,
				pDqLayer.pFeatureSearchPreparation)
			if ENC_RETURN_SUCCESS != iReturn {
				return 1
			}
		} else {
			pDqLayer.pFeatureSearchPreparation = nil
		}

		(*ppCtx).ppDqLayerList[iDlayerIndex] = pDqLayer

		iDlayerIndex++
	}

	// for dynamically malloc for parameter sets memory instead of maximal items for standard to reduce size, 3/18/2010
	if nil == (*ppCtx).pFuncList {
		return 1
	}
	if nil == (*ppCtx).pFuncList.pParametersetStrategy {
		return 1
	}
	kiNeededSpsNum := (*ppCtx).pFuncList.pParametersetStrategy.GetNeededSpsNum()
	kiNeededSubsetSpsNum := (*ppCtx).pFuncList.pParametersetStrategy.GetNeededSubsetSpsNum()
	(*ppCtx).pSpsArray = make([]SWelsSPS, kiNeededSpsNum)
	if kiNeededSubsetSpsNum > 0 {
		(*ppCtx).pSubsetArray = make([]SSubsetSps, kiNeededSubsetSpsNum)
	} else {
		(*ppCtx).pSubsetArray = nil
	}

	// PPS
	kiNeededPpsNum := (*ppCtx).pFuncList.pParametersetStrategy.GetNeededPpsNum()
	(*ppCtx).pPPSArray = make([]SWelsPPS, kiNeededPpsNum)

	(*ppCtx).pFuncList.pParametersetStrategy.LoadPrevious(pExistingParasetList, (*ppCtx).pSpsArray,
		(*ppCtx).pSubsetArray, (*ppCtx).pPPSArray)

	(*ppCtx).pDqIdcMap = make([]SDqIdc, iDlayerCount)

	iDlayerIndex = 0
	for iDlayerIndex < iDlayerCount {
		pDqIdc := &(*ppCtx).pDqIdcMap[iDlayerIndex]
		bUseSubsetSps := (!pParam.BSimulcastAVC) && (iDlayerIndex > BASE_DEPENDENCY_ID)
		pDlayerParam := &pParam.SSpatialLayers[iDlayerIndex]
		bSvcBaselayer := (!pParam.BSimulcastAVC) && (iDlayerCount > BASE_DEPENDENCY_ID) &&
			(iDlayerIndex == BASE_DEPENDENCY_ID)
		pDqIdc.uiSpatialId = int8(iDlayerIndex)

		iSpsId = int32((*ppCtx).pFuncList.pParametersetStrategy.GenerateNewSps(*ppCtx, bUseSubsetSps, iDlayerIndex,
			iDlayerCount, uint32(iSpsId), &pSps, &pSubsetSps, bSvcBaselayer))
		if 0 > iSpsId {
			return ENC_RETURN_UNSUPPORTED_PARA
		}
		if !bUseSubsetSps {
			pSps = &(*ppCtx).pSpsArray[iSpsId]
		} else {
			pSubsetSps = &(*ppCtx).pSubsetArray[iSpsId]
		}

		iPpsId = (*ppCtx).pFuncList.pParametersetStrategy.InitPps(*ppCtx, uint32(iSpsId), pSps, pSubsetSps, iPpsId, true,
			bUseSubsetSps, pParam.IEntropyCodingModeFlag != 0)
		pPps = &(*ppCtx).pPPSArray[iPpsId]

		// Not using FMO in SVC coding so far, come back if need FMO
		{
			iResult = InitSlicePEncCtx((*ppCtx).ppDqLayerList[iDlayerIndex],
				false,
				int32(pSps.iMbWidth),
				int32(pSps.iMbHeight),
				&pDlayerParam.SSliceArgument,
				pPps)
			if iResult != 0 {
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "InitDqLayers(), InitSlicePEncCtx failed(%d)!", iResult)
				return iResult
			}
		}
		pDqIdc.iSpsId = uint8(iSpsId)
		pDqIdc.iPpsId = uint16(iPpsId)

		if (pParam.BSimulcastAVC) || (bUseSubsetSps) {
			iSpsId++
		}
		iPpsId++
		if bUseSubsetSps {
			(*ppCtx).iSubsetSpsNum++
		} else {
			(*ppCtx).iSpsNum++
		}
		(*ppCtx).iPpsNum++

		iDlayerIndex++
	}

	(*ppCtx).pFuncList.pParametersetStrategy.UpdateParaSetNum(*ppCtx)
	return ENC_RETURN_SUCCESS
}

func AllocStrideTables(ppCtx **sWelsEncCtx, kiNumSpatialLayers int32) int32 {
	pParam := (*ppCtx).pSvcParam
	var pPtr *SStrideTables
	type mbSizeMap struct {
		iMbWidth             int32
		iCountMbNum          int32 // count number of SMB in each spatial
		iSizeAllMbAlignCache int32 // cache line size aligned in each spatial
	}
	var sMbSizeMap [MAX_DEPENDENCY_LAYER]mbSizeMap
	var iLineSizeY [MAX_DEPENDENCY_LAYER][2]int32
	var iLineSizeUV [MAX_DEPENDENCY_LAYER][2]int32
	var iMapSpatialIdx [MAX_DEPENDENCY_LAYER][2]int32
	var iCountLayersNeedCs [2]int32
	const kiUnit1Size = 24 // 24 * sizeof (int32_t) bytes: number of int32 entries
	iMaxMbWidth := int16(0)
	iMaxMbHeight := int16(0)
	i := int32(0)
	iSpatialIdx := int32(0)
	iTemporalIdx := int32(0)
	iCntTid := int32(0)

	if kiNumSpatialLayers <= 0 || kiNumSpatialLayers > MAX_DEPENDENCY_LAYER {
		return 1
	}

	pPtr = &SStrideTables{}
	(*ppCtx).pStrideTab = pPtr

	iCntTid = 1
	if pParam.ITemporalLayerNum > 1 {
		iCntTid = 2
	}

	iSpatialIdx = 0
	for iSpatialIdx < kiNumSpatialLayers {
		kiTmpWidth := (pParam.SSpatialLayers[iSpatialIdx].IVideoWidth + 15) >> 4
		kiTmpHeight := (pParam.SSpatialLayers[iSpatialIdx].IVideoHeight + 15) >> 4
		iNumMb := kiTmpWidth * kiTmpHeight

		sMbSizeMap[iSpatialIdx].iMbWidth = kiTmpWidth
		sMbSizeMap[iSpatialIdx].iCountMbNum = iNumMb

		sMbSizeMap[iSpatialIdx].iSizeAllMbAlignCache = iNumMb // in int16_t entries

		iSpatialIdx++
	}

	// Adaptive size_cs, size_fdec by implementation dependency
	iTemporalIdx = 0
	for iTemporalIdx < iCntTid {
		kbBaseTemporalFlag := 0
		if iTemporalIdx == 0 {
			kbBaseTemporalFlag = 1
		}

		iSpatialIdx = 0
		for iSpatialIdx < kiNumSpatialLayers {
			fDlp := &pParam.SSpatialLayers[iSpatialIdx]

			kiWidthPad := common.WELS_ALIGN(fDlp.IVideoWidth, 16) + (common.PADDING_LENGTH << 1)
			iLineSizeY[iSpatialIdx][kbBaseTemporalFlag] = common.WELS_ALIGN(kiWidthPad, 32)
			iLineSizeUV[iSpatialIdx][kbBaseTemporalFlag] = common.WELS_ALIGN((kiWidthPad >> 1), 16)

			iMapSpatialIdx[iCountLayersNeedCs[kbBaseTemporalFlag]][kbBaseTemporalFlag] = iSpatialIdx
			iCountLayersNeedCs[kbBaseTemporalFlag]++
			iSpatialIdx++
		}
		iTemporalIdx++
	}

	iTemporalIdx = 0
	for iTemporalIdx < iCntTid {
		kbBaseTemporalFlag := 0
		if iTemporalIdx == 0 {
			kbBaseTemporalFlag = 1
		}

		iSpatialIdx = 0
		for iSpatialIdx < iCountLayersNeedCs[kbBaseTemporalFlag] {
			kiActualSpatialIdx := iMapSpatialIdx[iSpatialIdx][kbBaseTemporalFlag]
			kiLumaWidth := iLineSizeY[kiActualSpatialIdx][kbBaseTemporalFlag]
			kiChromaWidth := iLineSizeUV[kiActualSpatialIdx][kbBaseTemporalFlag]

			pBaseDec := make([]int32, kiUnit1Size)
			WelsGetEncBlockStrideOffset(pBaseDec, kiLumaWidth, kiChromaWidth)

			pPtr.pStrideDecBlockOffset[kiActualSpatialIdx][kbBaseTemporalFlag] = pBaseDec

			iSpatialIdx++
		}
		iTemporalIdx++
	}
	iTemporalIdx = 0
	for iTemporalIdx < iCntTid {
		kbBaseTemporalFlag := 0
		if iTemporalIdx == 0 {
			kbBaseTemporalFlag = 1
		}

		iSpatialIdx = 0
		for iSpatialIdx < kiNumSpatialLayers {
			iMatchIndex := int32(0)
			bInMap := false
			bMatchFlag := false

			i = 0
			for i < iCountLayersNeedCs[kbBaseTemporalFlag] {
				kiActualIdx := iMapSpatialIdx[i][kbBaseTemporalFlag]
				if kiActualIdx == iSpatialIdx {
					bInMap = true
					break
				}
				if !bMatchFlag {
					iMatchIndex = kiActualIdx
					bMatchFlag = true
				}
				i++
			}

			if bInMap {
				iSpatialIdx++
				continue
			}

			// not in spatial map and assign match one to it
			pPtr.pStrideDecBlockOffset[iSpatialIdx][kbBaseTemporalFlag] =
				pPtr.pStrideDecBlockOffset[iMatchIndex][kbBaseTemporalFlag]

			iSpatialIdx++
		}
		iTemporalIdx++
	}

	iSpatialIdx = 0
	for iSpatialIdx < kiNumSpatialLayers {
		kiAllocMbSize := sMbSizeMap[iSpatialIdx].iSizeAllMbAlignCache

		pPtr.pStrideEncBlockOffset[iSpatialIdx] = make([]int32, kiUnit1Size)

		pPtr.pMbIndexX[iSpatialIdx] = make([]int16, kiAllocMbSize)
		pPtr.pMbIndexY[iSpatialIdx] = make([]int16, kiAllocMbSize)

		iSpatialIdx++
	}

	for iSpatialIdx < MAX_DEPENDENCY_LAYER {
		pPtr.pStrideDecBlockOffset[iSpatialIdx][0] = nil
		pPtr.pStrideDecBlockOffset[iSpatialIdx][1] = nil
		pPtr.pStrideEncBlockOffset[iSpatialIdx] = nil
		pPtr.pMbIndexX[iSpatialIdx] = nil
		pPtr.pMbIndexY[iSpatialIdx] = nil

		iSpatialIdx++
	}

	// initialize pMbIndexX and pMbIndexY tables as below

	iMaxMbWidth = int16(sMbSizeMap[kiNumSpatialLayers-1].iMbWidth)
	iMaxMbWidth = common.WELS_ALIGN(iMaxMbWidth, 4) // 4 loops for int16_t required introduced as below

	pTmpRow := make([]int16, iMaxMbWidth)
	pRowX := pTmpRow
	// initialize pRowX & pRowY
	for i = 0; i < int32(iMaxMbWidth); i++ {
		pRowX[i] = int16(i)
	}

	iSpatialIdx = kiNumSpatialLayers
	for {
		iSpatialIdx--
		if !(iSpatialIdx >= 0) {
			break
		}
		pMbIndexX := pPtr.pMbIndexX[iSpatialIdx]
		kiMbWidth := sMbSizeMap[iSpatialIdx].iMbWidth
		kiMbHeight := sMbSizeMap[iSpatialIdx].iCountMbNum / kiMbWidth

		iOff := int32(0)
		for i = 0; i < kiMbHeight; i++ {
			copy(pMbIndexX[iOff:iOff+kiMbWidth], pRowX[:kiMbWidth])
			iOff += kiMbWidth
		}
	}

	iMaxMbHeight = int16(sMbSizeMap[kiNumSpatialLayers-1].iCountMbNum / sMbSizeMap[kiNumSpatialLayers-1].iMbWidth)
	for i = 0; i < int32(iMaxMbHeight); i++ {
		for iSpatialIdx = kiNumSpatialLayers - 1; iSpatialIdx >= 0; iSpatialIdx-- {
			kiMbWidth := sMbSizeMap[iSpatialIdx].iMbWidth
			kiMbHeight := sMbSizeMap[iSpatialIdx].iCountMbNum / kiMbWidth
			if i < kiMbHeight {
				pMbIndexY := pPtr.pMbIndexY[iSpatialIdx][i*kiMbWidth : (i+1)*kiMbWidth]
				for j := range pMbIndexY {
					pMbIndexY[j] = int16(i)
				}
			}
		}
	}

	return 0
}

// RequestMemoryVaaScreen allocates the static block idc maps of the screen
// content VAA. pVaa is the base of an SVAAFrameInfoExt (pVaa.pExt).
func RequestMemoryVaaScreen(pVaa *SVAAFrameInfo, iNumRef int32, iCountMax8x8BNum int32) int32 {
	pVaaExt := pVaa.pExt

	pBuf := make([]uint8, iNumRef*iCountMax8x8BNum)
	pVaaExt.pVaaBlockStaticIdc[0] = pBuf[0:iCountMax8x8BNum]

	for idx := int32(1); idx < iNumRef; idx++ {
		pVaaExt.pVaaBlockStaticIdc[idx] = pBuf[idx*iCountMax8x8BNum : (idx+1)*iCountMax8x8BNum]
	}
	return 0
}

func ReleaseMemoryVaaScreen(pVaa *SVAAFrameInfo, iNumRef int32) {
	if pVaa == nil {
		return
	}
	pVaaExt := pVaa.pExt
	if pVaaExt != nil && pVaaExt.pVaaBlockStaticIdc[0] != nil {
		for idx := int32(0); idx < iNumRef; idx++ {
			pVaaExt.pVaaBlockStaticIdc[idx] = nil
		}
	}
}

// GetMvMvdRange computes the MV and MVD ranges. int32_t& -> *int32.
func GetMvMvdRange(pParam *SWelsSvcCodingParam, iMvRange *int32, iMvdRange *int32) {
	iMinLevelIdc := api.LEVEL_5_2
	iMinMv := int32(0)
	iMaxMv := int32(0)
	iFixMvRange := int32(CAMERA_STARTMV_RANGE)
	iFixMvdRange := int32(CAMERA_HIGHLAYER_MVD_RANGE)
	if pParam.IUsageType != 0 {
		iFixMvRange = EXPANDED_MV_RANGE
		iFixMvdRange = EXPANDED_MVD_RANGE
	} else if pParam.ISpatialLayerNum == 1 {
		iFixMvdRange = CAMERA_MVD_RANGE
	}
	for iLayer := int32(0); iLayer < pParam.ISpatialLayerNum; iLayer++ {
		if pParam.SSpatialLayers[iLayer].UiLevelIdc < iMinLevelIdc {
			iMinLevelIdc = pParam.SSpatialLayers[iLayer].UiLevelIdc
		}
	}
	iLevelIdx := 0
	for (common.G_ksLevelLimits[iLevelIdx].UiLevelIdc != api.LEVEL_5_2) &&
		(common.G_ksLevelLimits[iLevelIdx].UiLevelIdc != iMinLevelIdc) {
		iLevelIdx++
	}
	pLevelLimit := &common.G_ksLevelLimits[iLevelIdx]
	iMinMv = int32(pLevelLimit.IMinVmv) >> 2
	iMaxMv = int32(pLevelLimit.IMaxVmv) >> 2

	*iMvRange = common.WELS_MIN(common.WELS_ABS(iMinMv), iMaxMv)

	*iMvRange = common.WELS_MIN(*iMvRange, iFixMvRange)

	*iMvdRange = (*iMvRange + 1) << 1

	*iMvdRange = common.WELS_MIN(*iMvdRange, iFixMvdRange)
}

// RequestMemorySvc requests the specific memory for SVC.
// Returns 0 on success, otherwise none 0 for failed.
func RequestMemorySvc(ppCtx **sWelsEncCtx, pExistingParasetList *SExistingParasetList) int32 {
	pParam := (*ppCtx).pSvcParam
	var pFinalSpatial *api.SSpatialLayerConfig
	iCountBsLen := int32(0)
	iCountNals := int32(0)
	iMaxPicWidth := int32(0)
	iMaxPicHeight := int32(0)
	iCountMaxMbNum := int32(0)
	iIndex := int32(0)
	iCountLayers := int32(0)
	iResult := int32(0)
	fCompressRatioThr := float32(.5)
	kiNumDependencyLayers := pParam.ISpatialLayerNum
	iVclLayersBsSizeCount := int32(0)
	iNonVclLayersBsSizeCount := int32(0)
	iTargetSpatialBsSize := int32(0)

	if kiNumDependencyLayers < 1 || kiNumDependencyLayers > MAX_DEPENDENCY_LAYER {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "RequestMemorySvc() failed due to invalid iNumDependencyLayers(%d)!",
			kiNumDependencyLayers)
		return 1
	}

	if pParam.uiGopSize == 0 || (pParam.UiIntraPeriod != 0 && ((pParam.UiIntraPeriod % pParam.uiGopSize) != 0)) {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING,
			"RequestMemorySvc() failed due to invalid uiIntraPeriod(%d) (=multipler of uiGopSize(%d)!",
			pParam.UiIntraPeriod, pParam.uiGopSize)
		return 1
	}

	pFinalSpatial = &pParam.SSpatialLayers[kiNumDependencyLayers-1]
	iMaxPicWidth = pFinalSpatial.IVideoWidth
	iMaxPicHeight = pFinalSpatial.IVideoHeight
	iCountMaxMbNum = ((15 + iMaxPicWidth) >> 4) * ((15 + iMaxPicHeight) >> 4)

	iResult = AcquireLayersNals(ppCtx, pParam, &iCountLayers, &iCountNals)
	if iResult != 0 {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "RequestMemorySvc(), AcquireLayersNals failed(%d)!", iResult)
		return 1
	}

	kiSpsSize := int32((*ppCtx).pFuncList.pParametersetStrategy.GetNeededSpsNum() * SPS_BUFFER_SIZE)
	kiPpsSize := int32((*ppCtx).pFuncList.pParametersetStrategy.GetNeededPpsNum() * PPS_BUFFER_SIZE)
	iNonVclLayersBsSizeCount = SSEI_BUFFER_SIZE + kiSpsSize + kiPpsSize

	bDynamicSlice := false
	uiMaxSliceNumEstimation := uint32(0)
	iSliceBufferSize := int32(0)
	iMaxSliceBufferSize := int32(0)
	iTotalLength := int32(0)
	iLayerBsSize := int32(0)
	iIndex = 0
	for iIndex < pParam.ISpatialLayerNum {
		fDlp := &pParam.SSpatialLayers[iIndex]

		fCompressRatioThr = COMPRESS_RATIO_THR

		iLayerBsSize = common.WELS_ROUND(float32(((3*fDlp.IVideoWidth*fDlp.IVideoHeight)>>1))*fCompressRatioThr) +
			MAX_MACROBLOCK_SIZE_IN_BYTE_x2
		iLayerBsSize = common.WELS_ALIGN(iLayerBsSize, 4) // 4 bytes alinged
		iMaxLayerBsSize := int32(0)
		pSliceArgument := &fDlp.SSliceArgument
		if pSliceArgument.UiSliceMode == api.SM_SIZELIMITED_SLICE {
			bDynamicSlice = true
			uiMaxSliceNumEstimation = common.WELS_MIN(uint32(AVERSLICENUM_CONSTRAINT),
				(uint32(iLayerBsSize)/pSliceArgument.UiSliceSizeConstraint)+1)
			(*ppCtx).iMaxSliceCount = common.WELS_MAX((*ppCtx).iMaxSliceCount, int32(uiMaxSliceNumEstimation))
			iSliceBufferSize = int32((common.WELS_MAX(pSliceArgument.UiSliceSizeConstraint,
				uint32(iLayerBsSize)/uiMaxSliceNumEstimation) << 1) + MAX_MACROBLOCK_SIZE_IN_BYTE_x2)
			iMaxLayerBsSize = int32(uint32(iSliceBufferSize) * uiMaxSliceNumEstimation)
		} else {
			(*ppCtx).iMaxSliceCount = common.WELS_MAX((*ppCtx).iMaxSliceCount, int32(pSliceArgument.UiSliceNum))
			if pParam.BUseLoadBalancing {
				iSliceBufferSize = iLayerBsSize + MAX_MACROBLOCK_SIZE_IN_BYTE_x2
			} else {
				iSliceBufferSize = int32(((uint32(iLayerBsSize) / pSliceArgument.UiSliceNum) << 1) + MAX_MACROBLOCK_SIZE_IN_BYTE_x2)
			}
			iMaxLayerBsSize = int32(uint32(iSliceBufferSize) * pSliceArgument.UiSliceNum)
		}
		iMaxLayerBsSize = common.WELS_MAX(iMaxLayerBsSize, iLayerBsSize)
		iVclLayersBsSizeCount += iMaxLayerBsSize
		iMaxSliceBufferSize = common.WELS_MAX(iMaxSliceBufferSize, iSliceBufferSize)
		(*ppCtx).iSliceBufferSize[iIndex] = iSliceBufferSize
		iIndex++
	}
	iTargetSpatialBsSize = iVclLayersBsSizeCount
	iCountBsLen = iNonVclLayersBsSizeCount + iVclLayersBsSizeCount

	iMaxSliceBufferSize = common.WELS_MIN(iMaxSliceBufferSize, iTargetSpatialBsSize)
	iTotalLength = iCountBsLen

	iNumRefUpper := int32(MAX_REFERENCE_PICTURE_COUNT_NUM_SCREEN)
	if pParam.IUsageType == api.CAMERA_VIDEO_REAL_TIME {
		iNumRefUpper = MAX_REFERENCE_PICTURE_COUNT_NUM_CAMERA
	}
	pParam.INumRefFrame = common.WELS_CLIP3(pParam.INumRefFrame, MIN_REF_PIC_COUNT, iNumRefUpper)

	// Output
	(*ppCtx).pOut = &SWelsEncoderOutput{}
	(*ppCtx).pOut.pBsBuffer = make([]uint8, iCountBsLen)
	(*ppCtx).pOut.uiSize = uint32(iCountBsLen)
	(*ppCtx).pOut.sNalList = make([]SWelsNalRaw, iCountNals)
	(*ppCtx).pOut.pNalLen = make([]int32, iCountNals)
	(*ppCtx).pOut.iCountNals = iCountNals
	(*ppCtx).pOut.iNalIndex = 0
	(*ppCtx).pOut.iLayerBsIndex = 0

	(*ppCtx).pFrameBs = make([]uint8, iTotalLength)
	(*ppCtx).iFrameBsSize = iTotalLength
	(*ppCtx).iPosBsBuffer = 0

	// for dynamic slice mode&& CABAC,allocate slice buffer to restore slice data
	if bDynamicSlice && pParam.IEntropyCodingModeFlag != 0 {
		for iIdx := 0; iIdx < MAX_THREADS_NUM; iIdx++ {
			(*ppCtx).pDynamicBsBuffer[iIdx] = make([]uint8, iMaxSliceBufferSize)
		}
	}
	// for pSlice bs buffers
	if pParam.IMultipleThreadIdc > 1 &&
		RequestMtResource(ppCtx, pParam, iCountBsLen, iMaxSliceBufferSize, bDynamicSlice) != 0 {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "RequestMemorySvc(), RequestMtResource failed!")
		return 1
	}

	(*ppCtx).pReferenceStrategy = CreateReferenceStrategy(*ppCtx, pParam.IUsageType,
		pParam.BEnableLongTermReference)
	if nil == (*ppCtx).pReferenceStrategy {
		return 1
	}

	(*ppCtx).pIntra4x4PredModeBlocks = make([]int8, iCountMaxMbNum*INTRA_4x4_MODE_NUM)

	(*ppCtx).pNonZeroCountBlocks = make([]int8, iCountMaxMbNum*MB_LUMA_CHROMA_BLOCK4x4_NUM)

	(*ppCtx).pMvUnitBlock4x4 = make([]SMVUnitXY, iCountMaxMbNum*2*common.MB_BLOCK4x4_NUM)

	(*ppCtx).pRefIndexBlock4x4 = make([]int8, iCountMaxMbNum*2*common.MB_BLOCK8x8_NUM)

	(*ppCtx).pSadCostMb = make([]int32, iCountMaxMbNum)

	(*ppCtx).iGlobalQp = 26 // global qp in default

	(*ppCtx).pLtr = make([]SLTRState, kiNumDependencyLayers)
	for i := int32(0); i < kiNumDependencyLayers; i++ {
		ResetLtrState(&(*ppCtx).pLtr[i])
	}

	// stride tables
	if AllocStrideTables(ppCtx, kiNumDependencyLayers) != 0 {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "RequestMemorySvc(), AllocStrideTables failed!")
		return 1
	}

	//Rate control module memory allocation
	// only malloc once for RC pData, 12/14/2009
	(*ppCtx).pWelsSvcRc = make([]SWelsSvcRc, kiNumDependencyLayers)
	//End of Rate control module memory allocation

	//pVaa memory allocation
	if pParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		pVaaExt := NewSVAAFrameInfoExt()
		(*ppCtx).pVaa = &pVaaExt.SVAAFrameInfo
		if RequestMemoryVaaScreen((*ppCtx).pVaa, (*ppCtx).pSvcParam.iMaxNumRefFrame, iCountMaxMbNum<<2) != 0 {
			common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "RequestMemorySvc(), RequestMemoryVaaScreen failed!")
			return 1
		}
	} else {
		(*ppCtx).pVaa = &SVAAFrameInfo{}
	}

	if (*ppCtx).pSvcParam.BEnableAdaptiveQuant { //malloc mem
		(*ppCtx).pVaa.sAdaptiveQuantParam.PMotionTextureUnit = make([]processing.SMotionTextureUnit, iCountMaxMbNum)
		(*ppCtx).pVaa.sAdaptiveQuantParam.PMotionTextureIndexToDeltaQp = make([]int8, iCountMaxMbNum)
	}

	(*ppCtx).pVaa.pVaaBackgroundMbFlag = make([]int8, iCountMaxMbNum)

	(*ppCtx).pVaa.sVaaCalcInfo.PSad8x8 = make([][4]int32, iCountMaxMbNum)
	(*ppCtx).pVaa.sVaaCalcInfo.PSsd16x16 = make([]int32, iCountMaxMbNum)
	(*ppCtx).pVaa.sVaaCalcInfo.PSum16x16 = make([]int32, iCountMaxMbNum)
	(*ppCtx).pVaa.sVaaCalcInfo.PSumOfSquare16x16 = make([]int32, iCountMaxMbNum)

	if (*ppCtx).pSvcParam.BEnableBackgroundDetection { //BGD control
		(*ppCtx).pVaa.sVaaCalcInfo.PSumOfDiff8x8 = make([][4]int32, iCountMaxMbNum)
		(*ppCtx).pVaa.sVaaCalcInfo.PMad8x8 = make([][4]uint8, iCountMaxMbNum)
	}

	//End of pVaa memory allocation

	(*ppCtx).ppRefPicListExt = make([]*SRefList, kiNumDependencyLayers)

	(*ppCtx).ppDqLayerList = make([]*SDqLayer, kiNumDependencyLayers)

	iResult = InitDqLayers(ppCtx, pExistingParasetList)
	if iResult != 0 {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "RequestMemorySvc(), InitDqLayers failed(%d)!", iResult)
		return iResult
	}

	if InitMbListD(ppCtx) != 0 {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_WARNING, "RequestMemorySvc(), InitMbListD failed!")
		return 1
	}

	iMvdRange := int32(0)
	GetMvMvdRange(pParam, &(*ppCtx).iMvRange, &iMvdRange)
	kuiMvdInterTableSize := uint32(iMvdRange << 2)            //intepel*4=qpel
	kuiMvdInterTableStride := 1 + (kuiMvdInterTableSize << 1) //qpel_mv_range*2=(+/-);
	kuiMvdCacheAlignedSize := kuiMvdInterTableStride          // in uint16_t entries (C: * sizeof (uint16_t) bytes)

	(*ppCtx).iMvdCostTableSize = int32(kuiMvdInterTableSize)
	(*ppCtx).iMvdCostTableStride = int32(kuiMvdInterTableStride)
	(*ppCtx).pMvdCostTable = make([]uint16, 52*kuiMvdCacheAlignedSize)
	MvdCostInit((*ppCtx).pMvdCostTable, int32(kuiMvdInterTableStride)) //should put to a better place?

	if (*ppCtx).ppRefPicListExt[0] != nil && (*ppCtx).ppRefPicListExt[0].pRef[0] != nil {
		(*ppCtx).pDecPic = (*ppCtx).ppRefPicListExt[0].pRef[0]
	} else {
		(*ppCtx).pDecPic = nil // error here
	}

	(*ppCtx).pSps = &(*ppCtx).pSpsArray[0]
	(*ppCtx).pPps = &(*ppCtx).pPPSArray[0]

	return 0
}

// FreeMemorySvc frees the memory of the SVC core encoder.
func FreeMemorySvc(ppCtx **sWelsEncCtx) {
	if nil != *ppCtx {
		pCtx := *ppCtx
		pParam := pCtx.pSvcParam
		ilayer := int32(0)

		// SStrideTables
		if nil != pCtx.pStrideTab {
			pCtx.pStrideTab = nil
		}
		// pDq idc map
		if nil != pCtx.pDqIdcMap {
			pCtx.pDqIdcMap = nil
		}

		if nil != pCtx.pOut {
			// bs pBuffer
			pCtx.pOut.pBsBuffer = nil
			// NALs list
			pCtx.pOut.sNalList = nil
			// NALs len
			pCtx.pOut.pNalLen = nil
			pCtx.pOut = nil
		}

		if pParam != nil && pParam.IMultipleThreadIdc > 1 {
			ReleaseMtResource(ppCtx)
		}

		if nil != pCtx.pReferenceStrategy {
			pCtx.pReferenceStrategy = nil
		}

		// frame bitstream pBuffer
		pCtx.pFrameBs = nil
		for iIdx := 0; iIdx < MAX_THREADS_NUM; iIdx++ {
			pCtx.pDynamicBsBuffer[iIdx] = nil
		}
		// pSpsArray
		pCtx.pSpsArray = nil
		// pPPSArray
		pCtx.pPPSArray = nil
		// subset_sps_array
		pCtx.pSubsetArray = nil

		pCtx.pIntra4x4PredModeBlocks = nil
		pCtx.pNonZeroCountBlocks = nil
		pCtx.pMvUnitBlock4x4 = nil
		pCtx.pRefIndexBlock4x4 = nil
		pCtx.ppMbListD = nil
		pCtx.pSadCostMb = nil

		// SLTRState
		pCtx.pLtr = nil

		// pDq layers list
		ilayer = 0
		if nil != pCtx.ppDqLayerList && pParam != nil {
			for ilayer < pParam.ISpatialLayerNum {
				pDq := pCtx.ppDqLayerList[ilayer]
				// pDq layers
				if nil != pDq {
					FreeDqLayer(&pDq)
					pCtx.ppDqLayerList[ilayer] = nil
				}
				ilayer++
			}
			pCtx.ppDqLayerList = nil
		}
		// reference picture list extension
		if nil != pCtx.ppRefPicListExt && pParam != nil {
			ilayer = 0
			for ilayer < pParam.ISpatialLayerNum {
				FreeRefList(&pCtx.ppRefPicListExt[ilayer], pParam.iMaxNumRefFrame)
				pCtx.ppRefPicListExt[ilayer] = nil
				ilayer++
			}

			pCtx.ppRefPicListExt = nil
		}

		// VAA
		if nil != pCtx.pVaa {
			if pCtx.pSvcParam.BEnableAdaptiveQuant { //free mem
				pCtx.pVaa.sAdaptiveQuantParam.PMotionTextureUnit = nil
				pCtx.pVaa.sAdaptiveQuantParam.PMotionTextureIndexToDeltaQp = nil
			}

			pCtx.pVaa.pVaaBackgroundMbFlag = nil
			pCtx.pVaa.sVaaCalcInfo.PSad8x8 = nil
			pCtx.pVaa.sVaaCalcInfo.PSsd16x16 = nil
			pCtx.pVaa.sVaaCalcInfo.PSum16x16 = nil
			pCtx.pVaa.sVaaCalcInfo.PSumOfSquare16x16 = nil

			if pCtx.pSvcParam.BEnableBackgroundDetection { //BGD control
				pCtx.pVaa.sVaaCalcInfo.PSumOfDiff8x8 = nil
				pCtx.pVaa.sVaaCalcInfo.PMad8x8 = nil
			}
			if pCtx.pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
				ReleaseMemoryVaaScreen(pCtx.pVaa, pCtx.pSvcParam.iMaxNumRefFrame)
			}
			pCtx.pVaa = nil
		}

		// rate control module memory free
		if nil != pCtx.pWelsSvcRc {
			WelsRcFreeMemory(pCtx)
			pCtx.pWelsSvcRc = nil
		}

		/* MVD cost tables for Inter */
		pCtx.pMvdCostTable = nil

		FreeCodingParam(&pCtx.pSvcParam)
		if nil != pCtx.pFuncList {
			if nil != pCtx.pFuncList.pParametersetStrategy {
				pCtx.pFuncList.pParametersetStrategy = nil
			}

			pCtx.pFuncList = nil
		}

		// (CMemoryAlign dropped: there is no memory usage to verify)
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO, "FreeMemorySvc(), verify memory usage (%d bytes) after free..",
			0)

		*ppCtx = nil
	}
}

func InitSliceSettings(pLogCtx *common.SLogContext, pCodingParam *SWelsSvcCodingParam, kiCpuCores int32, pMaxSliceCount *int16) int32 {
	iSpatialIdx := int32(0)
	iSpatialNum := pCodingParam.ISpatialLayerNum
	iMaxSliceCount := uint16(0)

	for {
		pDlp := &pCodingParam.SSpatialLayers[iSpatialIdx]
		pSliceArgument := &pDlp.SSliceArgument
		iReturn := int32(0)

		switch pSliceArgument.UiSliceMode {
		case api.SM_SIZELIMITED_SLICE:
			iMaxSliceCount = AVERSLICENUM_CONSTRAINT
			// go through for SM_SIZELIMITED_SLICE?
		case api.SM_FIXEDSLCNUM_SLICE:
			iReturn = SliceArgumentValidationFixedSliceMode(pLogCtx, &pDlp.SSliceArgument, pCodingParam.IRCMode,
				pDlp.IVideoWidth, pDlp.IVideoHeight)
			if iReturn != 0 {
				return ENC_RETURN_UNSUPPORTED_PARA
			}

			if pSliceArgument.UiSliceNum > uint32(iMaxSliceCount) {
				iMaxSliceCount = uint16(pSliceArgument.UiSliceNum)
			}
		case api.SM_SINGLE_SLICE:
			if pSliceArgument.UiSliceNum > uint32(iMaxSliceCount) {
				iMaxSliceCount = uint16(pSliceArgument.UiSliceNum)
			}
		case api.SM_RASTER_SLICE:
			if pSliceArgument.UiSliceNum > uint32(iMaxSliceCount) {
				iMaxSliceCount = uint16(pSliceArgument.UiSliceNum)
			}
		default:
		}

		iSpatialIdx++
		if !(iSpatialIdx < iSpatialNum) {
			break
		}
	}

	pCodingParam.IMultipleThreadIdc = uint16(common.WELS_MIN(kiCpuCores, int32(iMaxSliceCount)))
	if pCodingParam.ILoopFilterDisableIdc == 0 &&
		pCodingParam.IMultipleThreadIdc != 1 { // Loop filter requested to be enabled, with threading enabled
		pCodingParam.ILoopFilterDisableIdc =
			2 // Disable loop filter on slice boundaries since that's not allowed with multithreading
	}
	*pMaxSliceCount = int16(iMaxSliceCount)

	return ENC_RETURN_SUCCESS
}

// OutputCpuFeaturesLog logs the cpu features/capabilities.
func OutputCpuFeaturesLog(pLogCtx *common.SLogContext, uiCpuFeatureFlags uint32, uiCpuCores uint32, iCacheLineSize int32) {
	yn := func(uiFlag uint32) byte {
		if uiCpuFeatureFlags&uiFlag != 0 {
			return 'Y'
		}
		return 'N'
	}
	// welstracer output
	common.WelsLog(pLogCtx, api.WELS_LOG_INFO, "WELS CPU features/capacities (0x%x) detected: \t"+
		"HTT:      %c, "+
		"MMX:      %c, "+
		"MMXEX:    %c, "+
		"SSE:      %c, "+
		"SSE2:     %c, "+
		"SSE3:     %c, "+
		"SSSE3:    %c, "+
		"SSE4.1:   %c, "+
		"SSE4.2:   %c, "+
		"AVX:      %c, "+
		"FMA:      %c, "+
		"X87-FPU:  %c, "+
		"3DNOW:    %c, "+
		"3DNOWEX:  %c, "+
		"ALTIVEC:  %c, "+
		"CMOV:     %c, "+
		"MOVBE:    %c, "+
		"AES:      %c, "+
		"NUMBER OF LOGIC PROCESSORS ON CHIP: %d, "+
		"CPU CACHE LINE SIZE (BYTES):        %d",
		uiCpuFeatureFlags,
		yn(common.WELS_CPU_HTT),
		yn(common.WELS_CPU_MMX),
		yn(common.WELS_CPU_MMXEXT),
		yn(common.WELS_CPU_SSE),
		yn(common.WELS_CPU_SSE2),
		yn(common.WELS_CPU_SSE3),
		yn(common.WELS_CPU_SSSE3),
		yn(common.WELS_CPU_SSE41),
		yn(common.WELS_CPU_SSE42),
		yn(common.WELS_CPU_AVX),
		yn(common.WELS_CPU_FMA),
		yn(common.WELS_CPU_FPU),
		yn(common.WELS_CPU_3DNOW),
		yn(common.WELS_CPU_3DNOWEXT),
		yn(common.WELS_CPU_ALTIVEC),
		yn(common.WELS_CPU_CMOV),
		yn(common.WELS_CPU_MOVBE),
		yn(common.WELS_CPU_AES),
		uiCpuCores,
		iCacheLineSize)
}

// GetMultipleThreadIdc decides the thread count and the slice settings.
// int16_t& iSliceNum, int32_t& iCacheLineSize, uint32_t& uiCpuFeatureFlags -> pointers.
func GetMultipleThreadIdc(pLogCtx *common.SLogContext, pCodingParam *SWelsSvcCodingParam, iSliceNum *int16, iCacheLineSize *int32, uiCpuFeatureFlags *uint32) int32 {
	// for cpu features detection, Only detect once??
	uiCpuCores :=
		int32(0) // number of logic processors on physical processor package, zero logic processors means HTT not supported
	*uiCpuFeatureFlags = common.WelsCPUFeatureDetect(&uiCpuCores) // detect cpu capacity features

	*iCacheLineSize = 16 // 16 bytes aligned in default

	if 0 == pCodingParam.IMultipleThreadIdc && uiCpuCores == 0 {
		// cpuid not supported or doesn't expose the number of cores,
		// use high level system API as followed to detect number of pysical/logic processor
		uiCpuCores = DynamicDetectCpuCores()
	}

	if 0 == pCodingParam.IMultipleThreadIdc {
		if uiCpuCores > 0 {
			pCodingParam.IMultipleThreadIdc = uint16(uiCpuCores)
		} else {
			pCodingParam.IMultipleThreadIdc = 1
		}
	}

	// So far so many cpu cores up to MAX_THREADS_NUM mean for server platforms,
	// for client application here it is constrained by maximal to MAX_THREADS_NUM
	pCodingParam.IMultipleThreadIdc = uint16(common.WELS_CLIP3(int32(pCodingParam.IMultipleThreadIdc), 1, MAX_THREADS_NUM))
	uiCpuCores = int32(pCodingParam.IMultipleThreadIdc)

	if InitSliceSettings(pLogCtx, pCodingParam, uiCpuCores, iSliceNum) != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "GetMultipleThreadIdc(), InitSliceSettings failed.")
		return 1
	}
	return 0
}

// WelsUninitEncoderExt uninitializes the Wels encoder core library.
func WelsUninitEncoderExt(ppCtx **sWelsEncCtx) {
	if nil == ppCtx || nil == *ppCtx {
		return
	}

	iMultipleThreadIdc := int32(0)
	if (*ppCtx).pSvcParam != nil {
		iMultipleThreadIdc = int32((*ppCtx).pSvcParam.IMultipleThreadIdc)
	}
	common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
		"WelsUninitEncoderExt(), pCtx= %p, iMultipleThreadIdc= %d.",
		*ppCtx, iMultipleThreadIdc)

	// Slice workers are scoped to one frame and have already joined here.

	if (*ppCtx).pVpp != nil {
		(*ppCtx).pVpp.FreeSpatialPictures(*ppCtx)
		(*ppCtx).pVpp.Destruct()
		(*ppCtx).pVpp = nil
	}
	FreeMemorySvc(ppCtx)
	*ppCtx = nil
}

// WelsInitEncoderExt initializes the Wels avc encoder core library.
// Returns 0 on success, otherwise none 0 for failed.
func WelsInitEncoderExt(ppCtx **sWelsEncCtx, pCodingParam *SWelsSvcCodingParam, pLogCtx *common.SLogContext, pExistingParasetList *SExistingParasetList) int32 {
	var pCtx *sWelsEncCtx
	iRet := int32(0)
	iSliceNum := int16(1)       // number of slices used
	iCacheLineSize := int32(16) // on chip cache line size in byte
	uiCpuFeatureFlags := uint32(0)
	if nil == ppCtx || nil == pCodingParam {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsInitEncoderExt(), NULL == ppCtx(0x%p) or NULL == pCodingParam(0x%p).",
			ppCtx, pCodingParam)
		return 1
	}

	iRet = ParamValidationExt(pLogCtx, pCodingParam)
	if iRet != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsInitEncoderExt(), ParamValidationExt failed return %d.", iRet)
		return iRet
	}
	iRet = pCodingParam.DetermineTemporalSettings()
	if iRet != ENC_RETURN_SUCCESS {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
			"WelsInitEncoderExt(), DetermineTemporalSettings failed return %d (check in/out frame rate and temporal layer setting! -- in/out = 2^x, x <= temppral_layer_num)",
			iRet)
		return iRet
	}
	iRet = GetMultipleThreadIdc(pLogCtx, pCodingParam, &iSliceNum, &iCacheLineSize, &uiCpuFeatureFlags)
	if iRet != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsInitEncoderExt(), GetMultipleThreadIdc failed return %d.", iRet)
		return iRet
	}

	*ppCtx = nil

	pCtx = &sWelsEncCtx{}

	pCtx.sLogCtx = *pLogCtx

	iRet = AllocCodingParam(&pCtx.pSvcParam)
	if iRet != 0 {
		WelsUninitEncoderExt(&pCtx)
		return iRet
	}
	*pCtx.pSvcParam = *pCodingParam // confirmed_safe_unsafe_usage

	pCtx.pFuncList = &SWelsFuncPtrList{}
	InitFunctionPointers(pCtx, pCtx.pSvcParam, uiCpuFeatureFlags)

	pCtx.iActiveThreadsNum = int16(pCodingParam.IMultipleThreadIdc)
	pCtx.iMaxSliceCount = int32(iSliceNum)
	iRet = RequestMemorySvc(&pCtx, pExistingParasetList)
	if iRet != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsInitEncoderExt(), RequestMemorySvc failed return %d.", iRet)
		WelsUninitEncoderExt(&pCtx)
		return iRet
	}

	if pCodingParam.IEntropyCodingModeFlag != 0 {
		WelsCabacInit(pCtx)
	}
	WelsRcInitModule(pCtx, pCtx.pSvcParam.IRCMode)

	pCtx.pVpp = CreatePreProcess(pCtx)
	if pCtx.pVpp == nil {
		iRet = 1
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsInitEncoderExt(), pOut of memory in case new CWelsPreProcess().")
		WelsUninitEncoderExt(&pCtx)
		return iRet
	}
	if iRet = pCtx.pVpp.AllocSpatialPictures(pCtx, pCtx.pSvcParam); iRet != 0 {
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsInitEncoderExt(), pVPP alloc spatial pictures failed")
		WelsUninitEncoderExt(&pCtx)
		return iRet
	}

	pCtx.iStatisticsLogInterval = STATISTICS_LOG_INTERVAL_MS
	pCtx.uiLastTimestamp = -1
	pCtx.bDeliveryFlag = true
	*ppCtx = pCtx

	common.WelsLog(pLogCtx, api.WELS_LOG_INFO, "WelsInitEncoderExt(), pCtx= 0x%p.", pCtx)

	return 0
}

// GetTemporalLevel gets the temporal level due to configuration and coding context.
func GetTemporalLevel(fDlp *SSpatialLayerInternal, kiFrameNum int32, kiGopSize int32) int32 {
	kiCodingIdx := kiFrameNum & (kiGopSize - 1)

	return int32(fDlp.uiCodingIdx2TemporalId[kiCodingIdx])
}

// DynslcUpdateMbNeighbourInfoListForAllSlices updates the MB neighbour
// information of all MBs. pMbList: the layer's MB list (SDqLayer.sMbDataP).
func DynslcUpdateMbNeighbourInfoListForAllSlices(pCurDq *SDqLayer, pMbList []SMB) {
	pSliceCtx := &pCurDq.sSliceEncCtx
	kiMbWidth := int32(pSliceCtx.iMbWidth)
	kiEndMbInSlice := pSliceCtx.iMbNumInFrame - 1
	iIdx := int32(0)

	for {
		pMb := &pMbList[iIdx]
		UpdateMbNeighbor(pCurDq, pMb, kiMbWidth, WelsMbToSliceIdc(pCurDq, pMb.iMbXY))
		iIdx++
		if !(iIdx <= kiEndMbInSlice) {
			break
		}
	}
}

// PicPartitionNumDecision: TUNE back if number of picture partition decision
// algorithm based on past if available.
func PicPartitionNumDecision(pCtx *sWelsEncCtx) int32 {
	iPartitionNum := int32(1)
	if pCtx.pSvcParam.IMultipleThreadIdc > 1 {
		iPartitionNum = int32(pCtx.pSvcParam.IMultipleThreadIdc)
	}
	return iPartitionNum
}

func WelsInitCurrentQBLayerMltslc(pCtx *sWelsEncCtx) {
	//pData init
	pCurDq := pCtx.pCurDqLayer
	//mb_neighbor
	DynslcUpdateMbNeighbourInfoListForAllSlices(pCurDq, pCurDq.sMbDataP)
}

func UpdateSlicepEncCtxWithPartition(pCurDq *SDqLayer, iPartitionNum int32) {
	pSliceCtx := &pCurDq.sSliceEncCtx
	kiMbNumInFrame := pSliceCtx.iMbNumInFrame
	iCountMbNumPerPartition := kiMbNumInFrame
	iAssignableMbLeft := kiMbNumInFrame
	iCountMbNumInPartition := int32(0)
	iFirstMbIdx := int32(0)
	var i int32

	if iPartitionNum <= 0 {
		iPartitionNum = 1
	} else if iPartitionNum > AVERSLICENUM_CONSTRAINT {
		iPartitionNum = AVERSLICENUM_CONSTRAINT // AVERSLICENUM_CONSTRAINT might be variable, however not fixed by MACRO
	}
	iCountMbNumPerPartition /= iPartitionNum
	if iCountMbNumPerPartition == 0 || iCountMbNumPerPartition == 1 {
		iCountMbNumPerPartition = kiMbNumInFrame
		iPartitionNum = 1
	}

	pSliceCtx.iSliceNumInFrame = iPartitionNum

	i = 0
	for i < iPartitionNum {
		if i+1 == iPartitionNum {
			iCountMbNumInPartition = iAssignableMbLeft
		} else {
			iCountMbNumInPartition = iCountMbNumPerPartition
		}

		pCurDq.FirstMbIdxOfPartition[i] = iFirstMbIdx
		pCurDq.EndMbIdxOfPartition[i] = iFirstMbIdx + iCountMbNumInPartition - 1
		pCurDq.LastCodedMbIdxOfPartition[i] = 0
		pCurDq.NumSliceCodedOfPartition[i] = 0

		common.WelsSetMemMultiplebytes_c(pSliceCtx.pOverallMbMap[iFirstMbIdx:], uint32(i),
			iCountMbNumInPartition, 2 /* sizeof (uint16_t) */)

		// for next partition(or pSlice)
		iFirstMbIdx += iCountMbNumInPartition
		iAssignableMbLeft -= iCountMbNumInPartition
		i++
	}

	for i < MAX_THREADS_NUM {
		pCurDq.FirstMbIdxOfPartition[i] = 0
		pCurDq.EndMbIdxOfPartition[i] = 0
		pCurDq.LastCodedMbIdxOfPartition[i] = 0
		pCurDq.NumSliceCodedOfPartition[i] = 0
		i++
	}
}

func WelsInitCurrentDlayerMltslc(pCtx *sWelsEncCtx, iPartitionNum int32) {
	pCurDq := pCtx.pCurDqLayer
	pSliceCtx := &pCurDq.sSliceEncCtx
	uiMiniPacketSize := uint32(0)

	UpdateSlicepEncCtxWithPartition(pCurDq, iPartitionNum)

	if common.I_SLICE == pCtx.eSliceType { //check if uiSliceSizeConstraint too small
		const byte_complexIMBat26 = 60
		iCurDid := pCtx.uiDependencyId
		uiFrmByte := uint32(0)

		if pCtx.pSvcParam.IRCMode != api.RC_OFF_MODE {
			//RC case
			uiFrmByte = (uint32(pCtx.pSvcParam.SSpatialLayers[iCurDid].ISpatialBitrate) /
				uint32(pCtx.pSvcParam.sDependencyLayers[iCurDid].fInputFrameRate)) >> 3
		} else {
			//fixed QP case
			iTtlMbNumInFrame := pSliceCtx.iMbNumInFrame
			iQDeltaTo26 := (26 - pCtx.pSvcParam.SSpatialLayers[iCurDid].IDLayerQp)

			uiFrmByte = uint32(iTtlMbNumInFrame * byte_complexIMBat26)
			if iQDeltaTo26 > 0 {
				//smaller QP than 26
				uiFrmByte = uint32(float32(uiFrmByte) * (float32(iQDeltaTo26) / 4))
			} else if iQDeltaTo26 < 0 {
				//larger QP than 26
				iQDeltaTo26 = ((-iQDeltaTo26) >> 2)            //delta mod 4
				uiFrmByte = (uiFrmByte >> uint32(iQDeltaTo26)) //if delta 4, byte /2
			}
		}

		//MINPACKETSIZE_CONSTRAINT
		//suppose 16 byte per mb at average
		uiMiniPacketSize = uiFrmByte / uint32(pSliceCtx.iMaxSliceNumConstraint)
		if pSliceCtx.uiSliceSizeConstraint < uiMiniPacketSize {
			common.WelsLog(&pCtx.sLogCtx,
				api.WELS_LOG_WARNING,
				"Set-SliceConstraint(%d) too small for current resolution (MB# %d) under QP/BR!",
				pSliceCtx.uiSliceSizeConstraint,
				pSliceCtx.iMbNumInFrame)
		}
	}

	WelsInitCurrentQBLayerMltslc(pCtx)
}

// WelsInitCurrentLayer initializes the current layer.
func WelsInitCurrentLayer(pCtx *sWelsEncCtx, kiWidth int32, kiHeight int32) {
	pParam := pCtx.pSvcParam
	pEncPic := pCtx.pEncPic
	pDecPic := pCtx.pDecPic
	pCurDq := pCtx.pCurDqLayer
	pBaseSlice := pCurDq.ppSliceInLayer[0]
	kiCurDid := pCtx.uiDependencyId
	kbUseSubsetSpsFlag := (!pParam.BSimulcastAVC) && (kiCurDid > BASE_DEPENDENCY_ID)
	pNalHdExt := &pCurDq.sLayerInfo.sNalHeaderExt
	pDqIdc := &pCtx.pDqIdcMap[kiCurDid]
	iIdx := int32(0)
	iSliceCount := pCurDq.iMaxSliceNum
	pParamInternal := &pParam.sDependencyLayers[kiCurDid]
	if nil == pCurDq || nil == pBaseSlice {
		return
	}

	pCurDq.pDecPic = pDecPic

	iCurPpsId := int32(pDqIdc.iPpsId)
	iCurSpsId := int32(pDqIdc.iSpsId)

	iCurPpsId = pCtx.pFuncList.pParametersetStrategy.GetCurrentPpsId(iCurPpsId,
		common.WELS_ABS(int32(pParamInternal.uiIdrPicId)-1)%MAX_PPS_COUNT)

	pBaseSlice.sSliceHeaderExt.sSliceHeader.iPpsId = iCurPpsId
	pBaseSlice.sSliceHeaderExt.sSliceHeader.pPps = &pCtx.pPPSArray[iCurPpsId]
	pCurDq.sLayerInfo.pPpsP = pBaseSlice.sSliceHeaderExt.sSliceHeader.pPps

	pBaseSlice.sSliceHeaderExt.sSliceHeader.iSpsId = iCurSpsId
	if kbUseSubsetSpsFlag {
		pCurDq.sLayerInfo.pSubsetSpsP = &pCtx.pSubsetArray[iCurSpsId]
		pBaseSlice.sSliceHeaderExt.sSliceHeader.pSps = &pCurDq.sLayerInfo.pSubsetSpsP.pSps
		pCurDq.sLayerInfo.pSpsP = pBaseSlice.sSliceHeaderExt.sSliceHeader.pSps
	} else {
		pCurDq.sLayerInfo.pSubsetSpsP = nil
		pBaseSlice.sSliceHeaderExt.sSliceHeader.pSps = &pCtx.pSpsArray[iCurSpsId]
		pCurDq.sLayerInfo.pSpsP = pBaseSlice.sSliceHeaderExt.sSliceHeader.pSps
	}

	pBaseSlice.bSliceHeaderExtFlag = (common.NAL_UNIT_CODED_SLICE_EXT == pCtx.eNalType)

	iIdx = 1
	for iIdx < iSliceCount {
		InitSliceHeadWithBase(pCurDq.ppSliceInLayer[iIdx], pBaseSlice)
		iIdx++
	}

	*pNalHdExt = common.SNalUnitHeaderExt{}
	pNalHd := &pNalHdExt.SNalUnitHeader
	pNalHd.UiNalRefIdc = uint8(pCtx.eNalPriority)
	pNalHd.ENalUnitType = pCtx.eNalType

	pNalHdExt.UiDependencyId = kiCurDid
	if pCtx.bNeedPrefixNalFlag {
		pNalHdExt.BDiscardableFlag = (pNalHd.UiNalRefIdc == common.NRI_PRI_LOWEST)
	} else {
		pNalHdExt.BDiscardableFlag = false
	}
	pNalHdExt.BIdrFlag = pCtx.eSliceType == common.I_SLICE ||
		((pParamInternal.iFrameNum == 0) && pCtx.eNalType == common.NAL_UNIT_CODED_SLICE_IDR)
	pNalHdExt.UiTemporalId = pCtx.uiTemporalId

	// pEncPic pData
	for i := 0; i < 3; i++ {
		pCurDq.pEncData[i] = pEncPic.pData[i]
		pCurDq.iEncDataOff[i] = pEncPic.iDataOff[i]
		pCurDq.iEncStride[i] = pEncPic.iLineSize[i]
	}
	// cs pData
	for i := 0; i < 3; i++ {
		pCurDq.pCsData[i] = pDecPic.pData[i]
		pCurDq.iCsDataOff[i] = pDecPic.iDataOff[i]
		pCurDq.iCsStride[i] = pDecPic.iLineSize[i]
	}

	if pCurDq.pRefLayer != nil {
		pCurDq.bBaseLayerAvailableFlag = true
	} else {
		pCurDq.bBaseLayerAvailableFlag = false
	}

	if pCtx.pTaskManage != nil {
		pCtx.pTaskManage.InitFrame(int32(kiCurDid))
	}
}

// SetFastCodingFunc (static).
func SetFastCodingFunc(pFuncList *SWelsFuncPtrList) {
	pFuncList.pfIntraFineMd = WelsMdIntraFinePartitionVaa
	pFuncList.sSampleDealingFuncs.pfMdCost = &pFuncList.sSampleDealingFuncs.pfSampleSad
	pFuncList.sSampleDealingFuncs.pfIntra16x16Combined3 = pFuncList.sSampleDealingFuncs.pfIntra16x16Combined3Sad
	pFuncList.sSampleDealingFuncs.pfIntra8x8Combined3 = pFuncList.sSampleDealingFuncs.pfIntra8x8Combined3Sad
}

// SetNormalCodingFunc (static).
func SetNormalCodingFunc(pFuncList *SWelsFuncPtrList) {
	pFuncList.pfIntraFineMd = WelsMdIntraFinePartition
	pFuncList.sSampleDealingFuncs.pfMdCost = &pFuncList.sSampleDealingFuncs.pfSampleSatd
	pFuncList.sSampleDealingFuncs.pfIntra16x16Combined3 =
		pFuncList.sSampleDealingFuncs.pfIntra16x16Combined3Satd
	pFuncList.sSampleDealingFuncs.pfIntra8x8Combined3 =
		pFuncList.sSampleDealingFuncs.pfIntra8x8Combined3Satd
	pFuncList.sSampleDealingFuncs.pfIntra4x4Combined3 =
		pFuncList.sSampleDealingFuncs.pfIntra4x4Combined3Satd
}

// SetMeMethod selects the search method. PSearchMethodFunc& -> *PSearchMethodFunc.
func SetMeMethod(uiMethod uint8, pSearchMethodFunc *PSearchMethodFunc) bool {
	switch uiMethod {
	case ME_DIA:
		*pSearchMethodFunc = WelsDiamondSearch
	case ME_CROSS:
		*pSearchMethodFunc = WelsMotionCrossSearch
	case ME_DIA_CROSS:
		*pSearchMethodFunc = WelsDiamondCrossSearch
	case ME_DIA_CROSS_FME:
		*pSearchMethodFunc = WelsDiamondCrossFeatureSearch
	case ME_FULL:
		*pSearchMethodFunc = WelsDiamondSearch
		return false
	default:
		*pSearchMethodFunc = WelsDiamondSearch
		return false
	}
	return true
}

func PreprocessSliceCoding(pCtx *sWelsEncCtx) {
	pCurLayer := pCtx.pCurDqLayer
	//const bool kbBaseAvail      = pCurLayer->bBaseLayerAvailableFlag;
	bFastMode := (pCtx.pSvcParam.IComplexityMode == api.LOW_COMPLEXITY)
	pFuncList := pCtx.pFuncList
	pFuncList.bWholeIntegerSearch = false
	pLogCtx := &pCtx.sLogCtx
	/* function pointers conditional assignment under sWelsEncCtx, layer_mb_enc_rec (in stack) is exclusive */
	if (pCtx.pSvcParam.IUsageType == api.CAMERA_VIDEO_REAL_TIME && bFastMode) ||
		(pCtx.pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME && common.P_SLICE == pCtx.eSliceType &&
			bFastMode) { //TODO: here is for sync with the origin code, consider the design again with more tests
		SetFastCodingFunc(pFuncList)
	} else {
		SetNormalCodingFunc(pFuncList)
	}

	if common.P_SLICE == pCtx.eSliceType {
		for i := 0; i < int(processing.BLOCK_STATIC_IDC_ALL); i++ {
			pFuncList.pfMotionSearch[i] = WelsMotionEstimateSearch
		}
		pFuncList.pfSearchMethod[BLOCK_16x16] = WelsDiamondSearch
		pFuncList.pfSearchMethod[BLOCK_16x8] = WelsDiamondSearch
		pFuncList.pfSearchMethod[BLOCK_8x16] = WelsDiamondSearch
		pFuncList.pfSearchMethod[BLOCK_8x8] = WelsDiamondSearch
		pFuncList.pfSearchMethod[BLOCK_4x4] = WelsDiamondSearch
		pFuncList.pfSearchMethod[BLOCK_8x4] = WelsDiamondSearch
		pFuncList.pfSearchMethod[BLOCK_4x8] = WelsDiamondSearch
		pFuncList.bWholeIntegerSearch = pCtx.pSvcParam.IUsageType == api.CAMERA_VIDEO_REAL_TIME
		pFuncList.pfFirstIntraMode = WelsMdFirstIntraMode
		pFuncList.sSampleDealingFuncs.pfMeCost = &pCtx.pFuncList.sSampleDealingFuncs.pfSampleSatd
		pFuncList.pfSetScrollingMv = SetScrollingMvToMdNull

		if bFastMode {
			pFuncList.pfCalculateSatd = NotCalculateSatdCost
			pFuncList.pfInterFineMd = WelsMdInterFinePartitionVaa
		} else {
			pFuncList.pfCalculateSatd = CalculateSatdCost
			pFuncList.pfInterFineMd = WelsMdInterFinePartition
		}
	} else {
		pFuncList.sSampleDealingFuncs.pfMeCost = nil
	}

	//to init at each frame will be needed when dealing with hybrid content (camera+screen)
	if pCtx.pSvcParam.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		if common.P_SLICE == pCtx.eSliceType {
			//MD related func pointers
			pFuncList.pfInterFineMd = WelsMdInterFinePartitionVaaOnScreen

			//ME related func pointers
			pVaaExt := pCtx.pVaa.pExt
			if pVaaExt.sScrollDetectInfo.BScrollDetectFlag &&
				(pVaaExt.sScrollDetectInfo.IScrollMvX|pVaaExt.sScrollDetectInfo.IScrollMvY) != 0 {
				pFuncList.pfSetScrollingMv = SetScrollingMvToMd
			} else {
				pFuncList.pfSetScrollingMv = SetScrollingMvToMdNull
			}

			pFuncList.pfMotionSearch[processing.NO_STATIC] = WelsMotionEstimateSearch
			pFuncList.pfMotionSearch[processing.COLLOCATED_STATIC] = WelsMotionEstimateSearchStatic
			pFuncList.pfMotionSearch[processing.SCROLLED_STATIC] = WelsMotionEstimateSearchScrolled
			//ME16x16
			if !SetMeMethod(ME_DIA_CROSS, &pFuncList.pfSearchMethod[BLOCK_16x16]) {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "SetMeMethod(BLOCK_16x16) ME_DIA_CROSS unsuccessful, switched to default search")
			}
			//ME8x8
			pFeatureSearchPreparation := pCurLayer.pFeatureSearchPreparation
			if pFeatureSearchPreparation != nil {
				pFeatureSearchPreparation.iHighFreMbCount = 0

				//calculate bFMESwitchFlag
				pVaaExt := pCtx.pVaa.pExt
				kiMbSize := int32(pCurLayer.iMbHeight) * int32(pCurLayer.iMbWidth)
				pFeatureSearchPreparation.bFMESwitchFlag = CalcFMESwitchFlag(pFeatureSearchPreparation.uiFMEGoodFrameCount,
					pFeatureSearchPreparation.iHighFreMbCount*100/kiMbSize, pCtx.pVaa.sVaaCalcInfo.IFrameSad/kiMbSize,
					pVaaExt.sScrollDetectInfo.BScrollDetectFlag)

				//PerformFMEPreprocess
				pScreenBlockFeatureStorage := pCurLayer.pRefPic.pScreenBlockFeatureStorage
				pFeatureSearchPreparation.pRefBlockFeature = pScreenBlockFeatureStorage
				if pFeatureSearchPreparation.bFMESwitchFlag &&
					!pScreenBlockFeatureStorage.bRefBlockFeatureCalculated {
					pRef := pCurLayer.pRefPic
					if pCtx.pSvcParam.BEnableLongTermReference {
						pRef = pCurLayer.pRefOri[0]
					}
					PerformFMEPreprocess(pFuncList, pRef, pFeatureSearchPreparation.pFeatureOfBlock,
						pScreenBlockFeatureStorage)
				}

				//assign ME pointer
				if pFeatureSearchPreparation.bFMESwitchFlag && pScreenBlockFeatureStorage.bRefBlockFeatureCalculated &&
					(pScreenBlockFeatureStorage.iIs16x16 == 0) {
					if !SetMeMethod(ME_DIA_CROSS_FME, &pFuncList.pfSearchMethod[BLOCK_8x8]) {
						common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
							"SetMeMethod(BLOCK_8x8) ME_DIA_CROSS_FME unsuccessful, switched to default search")
					}
				}

				//assign UpdateFMESwitch pointer
				if pFeatureSearchPreparation.bFMESwitchFlag {
					pFuncList.pfUpdateFMESwitch = UpdateFMESwitch
				} else {
					pFuncList.pfUpdateFMESwitch = UpdateFMESwitchNull
				}
			} //if (pFeatureSearchPreparation)
		} else {
			//reset some status when at common.I_SLICE
			pCurLayer.pFeatureSearchPreparation.bFMESwitchFlag = true
			pCurLayer.pFeatureSearchPreparation.uiFMEGoodFrameCount = FMESWITCH_DEFAULT_GOODFRAME_NUM
		}
	}

	// update some layer dependent variable to save judgements in mb-level
	pCurLayer.bSatdInMdFlag = ((pFuncList.sSampleDealingFuncs.pfMeCost == &pFuncList.sSampleDealingFuncs.pfSampleSatd) &&
		(pFuncList.sSampleDealingFuncs.pfMdCost == &pFuncList.sSampleDealingFuncs.pfSampleSatd))

	kiCurDid := int32(pCtx.uiDependencyId)
	kiCurTid := int32(pCtx.uiTemporalId)
	if pCurLayer.bDeblockingParallelFlag && (pCurLayer.iLoopFilterDisableIdc != 1) &&
		(common.NRI_PRI_LOWEST != pCtx.eNalPriority) &&
		(pCtx.pSvcParam.sDependencyLayers[kiCurDid].iHighestTemporalId == 0 ||
			kiCurTid < int32(pCtx.pSvcParam.sDependencyLayers[kiCurDid].iHighestTemporalId)) {
		pFuncList.pfDeblocking.pfDeblockingFilterSlice = DeblockingFilterSliceAvcbase
	} else {
		pFuncList.pfDeblocking.pfDeblockingFilterSlice = DeblockingFilterSliceAvcbaseNull
	}
}

// WelsSwapDqLayers (static) swaps pDq layers between current pDq layer and
// reference pDq layer.
func WelsSwapDqLayers(pCtx *sWelsEncCtx, kiNextDqIdx int32) {
	// swap and assign reference
	pTmpLayer := pCtx.ppDqLayerList[kiNextDqIdx]
	pRefLayer := pCtx.pCurDqLayer
	pCtx.pCurDqLayer = pTmpLayer
	pCtx.pCurDqLayer.pRefLayer = pRefLayer
}

// PrefetchReferencePicture (static) prefetches the reference picture after
// WelsBuildRefList.
func PrefetchReferencePicture(pCtx *sWelsEncCtx, keFrameType api.EVideoFrameType) {
	kiSliceCount := pCtx.pCurDqLayer.iMaxSliceNum
	iIdx := int32(0)
	uiRefIdx := uint8(0xff) // -1

	if keFrameType != api.VideoFrameTypeIDR {
		pCtx.pRefPic = pCtx.pRefList0[0] // always get item 0 due to reordering done
		pCtx.pCurDqLayer.pRefPic = pCtx.pRefPic
		uiRefIdx = 0 // reordered reference iIndex
	} else { // safe for IDR coding
		pCtx.pRefPic = nil
		pCtx.pCurDqLayer.pRefPic = nil
	}

	iIdx = 0
	for iIdx < kiSliceCount {
		pCtx.pCurDqLayer.ppSliceInLayer[iIdx].sSliceHeaderExt.sSliceHeader.uiRefIndex = uiRefIdx
		iIdx++
	}
}

// WelsWriteOneSPS writes one SPS NAL. int32_t& iNalSize -> *int32.
func WelsWriteOneSPS(pCtx *sWelsEncCtx, kiSpsIdx int32, iNalSize *int32) int32 {
	iNal := pCtx.pOut.iNalIndex
	WelsLoadNal(pCtx.pOut, common.NAL_UNIT_SPS, common.NRI_PRI_HIGHEST)

	WelsWriteSpsNal(&pCtx.pSpsArray[kiSpsIdx], &pCtx.pOut.sBsWrite,
		pCtx.pFuncList.pParametersetStrategy.GetSpsIdOffsetList(PARA_SET_TYPE_AVCSPS))
	WelsUnloadNal(pCtx.pOut)

	iReturn := WelsEncodeNal(&pCtx.pOut.sNalList[iNal], nil,
		pCtx.iFrameBsSize-pCtx.iPosBsBuffer, //available buffer to be written, so need to substract the used length
		pCtx.pFrameBs[pCtx.iPosBsBuffer:],
		iNalSize)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}

	pCtx.iPosBsBuffer += *iNalSize
	return ENC_RETURN_SUCCESS
}

func WelsWriteOnePPS(pCtx *sWelsEncCtx, kiPpsIdx int32, iNalSize *int32) int32 {
	//TODO
	iNal := pCtx.pOut.iNalIndex
	/* generate picture parameter set */
	WelsLoadNal(pCtx.pOut, common.NAL_UNIT_PPS, common.NRI_PRI_HIGHEST)

	WelsWritePpsSyntax(&pCtx.pPPSArray[kiPpsIdx], &pCtx.pOut.sBsWrite,
		pCtx.pFuncList.pParametersetStrategy)
	WelsUnloadNal(pCtx.pOut)

	iReturn := WelsEncodeNal(&pCtx.pOut.sNalList[iNal], nil,
		pCtx.iFrameBsSize-pCtx.iPosBsBuffer,
		pCtx.pFrameBs[pCtx.iPosBsBuffer:],
		iNalSize)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}

	pCtx.iPosBsBuffer += *iNalSize
	return ENC_RETURN_SUCCESS
}

// WelsWriteParameterSets writes all parameter sets introduced in SVC
// extension. pNalLen: int32_t* array of NAL lengths -> []int32.
func WelsWriteParameterSets(pCtx *sWelsEncCtx, pNalLen []int32, pNumNal *int32, pTotalLength *int32) int32 {
	iSize := int32(0)
	iNal := int32(0)
	iIdx := int32(0)
	iId := int32(0)
	iCountNal := int32(0)
	iNalLength := int32(0)
	iReturn := int32(ENC_RETURN_SUCCESS)

	if nil == pCtx || nil == pNalLen || nil == pNumNal || nil == pCtx.pFuncList.pParametersetStrategy {
		return ENC_RETURN_UNEXPECTED
	}

	*pTotalLength = 0
	/* write all SPS */
	iIdx = 0
	for iIdx < pCtx.iSpsNum {
		pCtx.pFuncList.pParametersetStrategy.Update(pCtx.pSpsArray[iIdx].uiSpsId, PARA_SET_TYPE_AVCSPS)
		/* generate sequence parameters set */
		iId = pCtx.pFuncList.pParametersetStrategy.GetSpsIdx(iIdx)

		WelsWriteOneSPS(pCtx, iId, &iNalLength)

		pNalLen[iCountNal] = iNalLength
		iSize += iNalLength

		iIdx++
		iCountNal++
	}

	/* write all Subset SPS */
	iIdx = 0
	for iIdx < pCtx.iSubsetSpsNum {
		iNal = pCtx.pOut.iNalIndex

		pCtx.pFuncList.pParametersetStrategy.Update(pCtx.pSubsetArray[iIdx].pSps.uiSpsId, PARA_SET_TYPE_SUBSETSPS)

		iId = iIdx

		/* generate Subset SPS */
		WelsLoadNal(pCtx.pOut, common.NAL_UNIT_SUBSET_SPS, common.NRI_PRI_HIGHEST)

		WelsWriteSubsetSpsSyntax(&pCtx.pSubsetArray[iId], &pCtx.pOut.sBsWrite,
			pCtx.pFuncList.pParametersetStrategy.GetSpsIdOffsetList(PARA_SET_TYPE_SUBSETSPS))
		WelsUnloadNal(pCtx.pOut)

		iReturn = WelsEncodeNal(&pCtx.pOut.sNalList[iNal], nil,
			pCtx.iFrameBsSize-pCtx.iPosBsBuffer, //available buffer to be written, so need to substract the used length
			pCtx.pFrameBs[pCtx.iPosBsBuffer:],
			&iNalLength)
		if iReturn != ENC_RETURN_SUCCESS {
			return iReturn
		}
		pNalLen[iCountNal] = iNalLength

		pCtx.iPosBsBuffer += iNalLength
		iSize += iNalLength

		iIdx++
		iCountNal++
	}

	pCtx.pFuncList.pParametersetStrategy.UpdatePpsList(pCtx)

	iIdx = 0
	for iIdx < pCtx.iPpsNum {
		pCtx.pFuncList.pParametersetStrategy.Update(pCtx.pPPSArray[iIdx].iPpsId, PARA_SET_TYPE_PPS)

		WelsWriteOnePPS(pCtx, iIdx, &iNalLength)

		pNalLen[iCountNal] = iNalLength
		iSize += iNalLength

		iIdx++
		iCountNal++
	}

	*pNumNal = iCountNal
	*pTotalLength = iSize

	return ENC_RETURN_SUCCESS
}

// AddPrefixNal (static) writes a prefix NAL unit. pNalLen is
// pLayerBsInfo.PNalLengthInByte; int32_t& iPayloadSize -> *int32.
func AddPrefixNal(pCtx *sWelsEncCtx,
	pLayerBsInfo *api.SLayerBSInfo,
	pNalLen []int32,
	pNalIdxInLayer *int32,
	keNalType common.EWelsNalUnitType,
	keNalRefIdc common.EWelsNalRefIdc,
	iPayloadSize *int32) int32 {
	iReturn := int32(ENC_RETURN_SUCCESS)
	*iPayloadSize = 0

	if keNalRefIdc != common.NRI_PRI_LOWEST {
		WelsLoadNal(pCtx.pOut, common.NAL_UNIT_PREFIX, int32(keNalRefIdc))

		WelsWriteSVCPrefixNal(&pCtx.pOut.sBsWrite, int32(keNalRefIdc), (common.NAL_UNIT_CODED_SLICE_IDR == keNalType))

		WelsUnloadNal(pCtx.pOut)

		iReturn = WelsEncodeNal(&pCtx.pOut.sNalList[pCtx.pOut.iNalIndex-1],
			&pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt,
			pCtx.iFrameBsSize-pCtx.iPosBsBuffer,
			pCtx.pFrameBs[pCtx.iPosBsBuffer:],
			&pNalLen[*pNalIdxInLayer])
		if iReturn != ENC_RETURN_SUCCESS {
			return iReturn
		}
		*iPayloadSize = pNalLen[*pNalIdxInLayer]

		pCtx.iPosBsBuffer += *iPayloadSize

		(*pNalIdxInLayer)++
	} else { // No Prefix NAL Unit RBSP syntax here, but need add NAL Unit Header extension
		WelsLoadNal(pCtx.pOut, common.NAL_UNIT_PREFIX, int32(keNalRefIdc))
		// No need write any syntax of prefix NAL Unit RBSP here
		WelsUnloadNal(pCtx.pOut)

		iReturn = WelsEncodeNal(&pCtx.pOut.sNalList[pCtx.pOut.iNalIndex-1],
			&pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt,
			pCtx.iFrameBsSize-pCtx.iPosBsBuffer,
			pCtx.pFrameBs[pCtx.iPosBsBuffer:],
			&pNalLen[*pNalIdxInLayer])
		if iReturn != ENC_RETURN_SUCCESS {
			return iReturn
		}
		*iPayloadSize = pNalLen[*pNalIdxInLayer]

		pCtx.iPosBsBuffer += *iPayloadSize

		(*pNalIdxInLayer)++
	}

	return ENC_RETURN_SUCCESS
}

func WritePadding(pCtx *sWelsEncCtx, iLen int32, iSize *int32) int32 {
	i := int32(0)
	iNal := int32(0)
	var pBs *common.SBitStringAux
	var iNalLen int32

	*iSize = 0
	iNal = pCtx.pOut.iNalIndex
	pBs = &pCtx.pOut.sBsWrite // SBitStringAux instance for non VCL NALs decoding

	if (pBs.PEndBuf-pBs.PCurBuf) < int(iLen) || iNal >= pCtx.pOut.iCountNals {
		return ENC_RETURN_MEMOVERFLOWFOUND
	}

	WelsLoadNal(pCtx.pOut, common.NAL_UNIT_FILLER_DATA, common.NRI_PRI_LOWEST)

	for i = 0; i < iLen; i++ {
		common.BsWriteBits(pBs, 8, 0xff)
	}

	common.BsRbspTrailingBits(pBs)

	WelsUnloadNal(pCtx.pOut)
	iReturn := WelsEncodeNal(&pCtx.pOut.sNalList[iNal], nil,
		pCtx.iFrameBsSize-pCtx.iPosBsBuffer,
		pCtx.pFrameBs[pCtx.iPosBsBuffer:],
		&iNalLen)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}

	pCtx.iPosBsBuffer += iNalLen
	*iSize += iNalLen

	return ENC_RETURN_SUCCESS
}

// ForceCodingIDR forces coding IDR.
func ForceCodingIDR(pCtx *sWelsEncCtx, iLayerId int32) int32 {
	if nil == pCtx {
		return 1
	}
	if (iLayerId < 0) || (iLayerId >= api.MAX_SPATIAL_LAYER_NUM) || (!pCtx.pSvcParam.BSimulcastAVC) {
		for iDid := int32(0); iDid < pCtx.pSvcParam.ISpatialLayerNum; iDid++ {
			pParamInternal := &pCtx.pSvcParam.sDependencyLayers[iDid]
			pParamInternal.iCodingIndex = 0
			pParamInternal.iFrameIndex = 0
			pParamInternal.iFrameNum = 0
			pParamInternal.iPOC = 0
			pParamInternal.bEncCurFrmAsIdrFlag = true
			pCtx.sEncoderStatistics[0].UiIDRReqNum++
		}
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "ForceCodingIDR(iDid 0-%d)at InputFrameCount=%d\n",
			pCtx.pSvcParam.ISpatialLayerNum-1, pCtx.sEncoderStatistics[0].UiInputFrameCount)

	} else {
		pParamInternal := &pCtx.pSvcParam.sDependencyLayers[iLayerId]
		pParamInternal.iCodingIndex = 0
		pParamInternal.iFrameIndex = 0
		pParamInternal.iFrameNum = 0
		pParamInternal.iPOC = 0
		pParamInternal.bEncCurFrmAsIdrFlag = true
		pCtx.sEncoderStatistics[iLayerId].UiIDRReqNum++
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_INFO, "ForceCodingIDR(iDid %d)at InputFrameCount=%d\n", iLayerId,
			pCtx.sEncoderStatistics[iLayerId].UiInputFrameCount)
	}
	pCtx.bCheckWindowStatusRefreshFlag = false

	return 0
}

// WelsEncoderEncodeParameterSets writes the parameter sets only.
// pDst: void* that is always an SFrameBSInfo*.
func WelsEncoderEncodeParameterSets(pCtx *sWelsEncCtx, pDst *api.SFrameBSInfo) int32 {
	if nil == pCtx || nil == pDst {
		return ENC_RETURN_UNEXPECTED
	}

	pFbi := pDst
	pLayerBsInfo := &pFbi.SLayerInfo[0]
	iCountNal := int32(0)
	iTotalLength := int32(0)

	pLayerBsInfo.PBsBuf = pCtx.pFrameBs
	pLayerBsInfo.PNalLengthInByte = pCtx.pOut.pNalLen
	common.InitBits(&pCtx.pOut.sBsWrite, pCtx.pOut.pBsBuffer, 0, int32(pCtx.pOut.uiSize))

	pCtx.iPosBsBuffer = 0
	iReturn := WelsWriteParameterSets(pCtx, pLayerBsInfo.PNalLengthInByte, &iCountNal, &iTotalLength)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}

	pLayerBsInfo.UiSpatialId = 0
	pLayerBsInfo.UiTemporalId = 0
	pLayerBsInfo.UiQualityId = 0
	pLayerBsInfo.UiLayerType = uint8(api.NON_VIDEO_CODING_LAYER)
	pLayerBsInfo.INalCount = iCountNal
	pLayerBsInfo.EFrameType = api.VideoFrameTypeInvalid
	pLayerBsInfo.ISubSeqId = 0
	//pCtx->eLastNalPriority      = common.NRI_PRI_HIGHEST;
	pFbi.ILayerNum = 1
	pFbi.EFrameType = api.VideoFrameTypeInvalid
	common.WelsEmms()

	return ENC_RETURN_SUCCESS
}

func GetSubSequenceId(pCtx *sWelsEncCtx, eFrameType api.EVideoFrameType) int32 {
	iSubSeqId := int32(0)
	if eFrameType == api.VideoFrameTypeIDR {
		iSubSeqId = 0
	} else if eFrameType == api.VideoFrameTypeI {
		iSubSeqId = 1
	} else if eFrameType == api.VideoFrameTypeP {
		if pCtx.bCurFrameMarkedAsSceneLtr {
			iSubSeqId = 2
		} else {
			iSubSeqId = 3 + int32(pCtx.uiTemporalId) //T0:3 T1:4 T2:5 T3:6
		}
	} else {
		iSubSeqId = 3 + api.MAX_TEMPORAL_LAYER_NUM
	}
	return iSubSeqId
}

// encExtNextLayerBsInfo moves the layer bitstream info cursor to the next
// layer: ++pLayerBsInfo; pLayerBsInfo->pBsBuf = pFrameBs + iPosBsBuffer;
// pLayerBsInfo->pNalLengthInByte = (pLayerBsInfo - 1)->pNalLengthInByte + kiCountNal.
func encExtNextLayerBsInfo(pCtx *sWelsEncCtx, pLayerBsInfo []api.SLayerBSInfo, iLayerBsInfoIdx *int, kiCountNal int32) {
	(*iLayerBsInfoIdx)++
	pCur := &pLayerBsInfo[*iLayerBsInfoIdx]
	pCur.PBsBuf = pCtx.pFrameBs[pCtx.iPosBsBuffer:]
	pCur.PNalLengthInByte = pLayerBsInfo[*iLayerBsInfoIdx-1].PNalLengthInByte[kiCountNal:]
}

// WriteSsvcParaset writes the parasets for (simulcast) svc.
func WriteSsvcParaset(pCtx *sWelsEncCtx, kiSpatialNum int32, pLayerBsInfo []api.SLayerBSInfo, iLayerBsInfoIdx *int, iLayerNum *int32, iFrameSize *int32) int32 {
	iNonVclSize := int32(0)
	iCountNal := int32(0)
	iReturn := int32(ENC_RETURN_SUCCESS)
	pCur := &pLayerBsInfo[*iLayerBsInfoIdx]
	iReturn = WelsWriteParameterSets(pCtx, pCur.PNalLengthInByte, &iCountNal, &iNonVclSize)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}
	for iSpatialId := int32(0); iSpatialId < kiSpatialNum; iSpatialId++ {
		pParamInternal := &pCtx.pSvcParam.sDependencyLayers[iSpatialId]
		if pParamInternal.uiIdrPicId < 65535 {
			pParamInternal.uiIdrPicId++
		} else {
			pParamInternal.uiIdrPicId = 0
		}
	}
	pCur.UiSpatialId = 0
	pCur.UiTemporalId = 0
	pCur.UiQualityId = 0
	pCur.UiLayerType = uint8(api.NON_VIDEO_CODING_LAYER)
	pCur.INalCount = iCountNal
	pCur.EFrameType = api.VideoFrameTypeIDR
	pCur.ISubSeqId = GetSubSequenceId(pCtx, api.VideoFrameTypeIDR)
	//point to next pLayerBsInfo
	encExtNextLayerBsInfo(pCtx, pLayerBsInfo, iLayerBsInfoIdx, iCountNal)
	pCtx.pOut.iLayerBsIndex++

	//update for external countings
	(*iLayerNum)++
	*iFrameSize += iNonVclSize
	return iReturn
}

// WriteSavcParaset writes the parasets for simulcast avc.
func WriteSavcParaset(pCtx *sWelsEncCtx, iIdx int32, pLayerBsInfo []api.SLayerBSInfo, iLayerBsInfoIdx *int, iLayerNum *int32, iFrameSize *int32) int32 {
	iNonVclSize := int32(0)
	iCountNal := int32(0)
	iReturn := int32(ENC_RETURN_SUCCESS)

	// write SPS
	iNonVclSize = 0

	//writing one NAL
	iNalSize := int32(0)
	iCountNal = 0

	if pCtx.pFuncList.pParametersetStrategy != nil {
		pCtx.pFuncList.pParametersetStrategy.Update(pCtx.pSpsArray[iIdx].uiSpsId, PARA_SET_TYPE_AVCSPS)
	}

	iReturn = WelsWriteOneSPS(pCtx, iIdx, &iNalSize)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}

	pCur := &pLayerBsInfo[*iLayerBsInfoIdx]
	pCur.PNalLengthInByte[iCountNal] = iNalSize
	iNonVclSize += iNalSize
	iCountNal = 1

	//finish writing one NAL

	pCur.UiSpatialId = uint8(iIdx)
	pCur.UiTemporalId = 0
	pCur.UiQualityId = 0
	pCur.UiLayerType = uint8(api.NON_VIDEO_CODING_LAYER)
	pCur.INalCount = iCountNal
	pCur.EFrameType = api.VideoFrameTypeIDR
	pCur.ISubSeqId = GetSubSequenceId(pCtx, api.VideoFrameTypeIDR)
	//point to next pLayerBsInfo
	encExtNextLayerBsInfo(pCtx, pLayerBsInfo, iLayerBsInfoIdx, iCountNal)
	pCtx.pOut.iLayerBsIndex++
	//update for external countings
	(*iLayerNum)++

	// write PPS

	//TODO: under new strategy, will PPS be correctly updated?

	//writing one NAL
	iNalSize = 0
	iCountNal = 0

	if pCtx.pFuncList.pParametersetStrategy != nil {
		pCtx.pFuncList.pParametersetStrategy.Update(pCtx.pPPSArray[iIdx].iPpsId, PARA_SET_TYPE_PPS)
	}

	iReturn = WelsWriteOnePPS(pCtx, iIdx, &iNalSize)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}

	pCur = &pLayerBsInfo[*iLayerBsInfoIdx]
	pCur.PNalLengthInByte[iCountNal] = iNalSize
	iNonVclSize += iNalSize
	iCountNal = 1
	//finish writing one NAL

	pCur.UiSpatialId = uint8(iIdx)
	pCur.UiTemporalId = 0
	pCur.UiQualityId = 0
	pCur.UiLayerType = uint8(api.NON_VIDEO_CODING_LAYER)
	pCur.INalCount = iCountNal
	pCur.EFrameType = api.VideoFrameTypeIDR
	pCur.ISubSeqId = GetSubSequenceId(pCtx, api.VideoFrameTypeIDR)
	//point to next pLayerBsInfo
	encExtNextLayerBsInfo(pCtx, pLayerBsInfo, iLayerBsInfoIdx, iCountNal)
	pCtx.pOut.iLayerBsIndex++
	//update for external countings
	(*iLayerNum)++

	// to check number of layers / nals / slices dependencies
	if *iLayerNum > api.MAX_LAYER_NUM_OF_FRAME {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "WriteSavcParaset(), iLayerNum(%d) > MAX_LAYER_NUM_OF_FRAME(%d)!",
			*iLayerNum, api.MAX_LAYER_NUM_OF_FRAME)
		return 1
	}

	*iFrameSize += iNonVclSize
	return iReturn
}

// WriteSavcParaset_Listing covers the logic of simulcast avc + sps_pps_listing.
func WriteSavcParaset_Listing(pCtx *sWelsEncCtx, kiSpatialNum int32, pLayerBsInfo []api.SLayerBSInfo, iLayerBsInfoIdx *int, iLayerNum *int32, iFrameSize *int32) int32 {
	iNonVclSize := int32(0)
	iCountNal := int32(0)
	iReturn := int32(ENC_RETURN_SUCCESS)

	// write SPS
	iNonVclSize = 0

	for iSpatialId := int32(0); iSpatialId < kiSpatialNum; iSpatialId++ {
		pParamInternal := &pCtx.pSvcParam.sDependencyLayers[iSpatialId]
		if pParamInternal.uiIdrPicId < 65535 {
			pParamInternal.uiIdrPicId++
		} else {
			pParamInternal.uiIdrPicId = 0
		}

		iCountNal = 0

		for iIdx := int32(0); iIdx < pCtx.iSpsNum; iIdx++ {
			//writing one NAL
			iNalSize := int32(0)
			iReturn = WelsWriteOneSPS(pCtx, iIdx, &iNalSize)
			if iReturn != ENC_RETURN_SUCCESS {
				return iReturn
			}

			pLayerBsInfo[*iLayerBsInfoIdx].PNalLengthInByte[iCountNal] = iNalSize
			iNonVclSize += iNalSize
			iCountNal++
			//finish writing one NAL
		}

		pCur := &pLayerBsInfo[*iLayerBsInfoIdx]
		pCur.UiSpatialId = uint8(iSpatialId)
		pCur.UiTemporalId = 0
		pCur.UiQualityId = 0
		pCur.UiLayerType = uint8(api.NON_VIDEO_CODING_LAYER)
		pCur.INalCount = iCountNal
		pCur.EFrameType = api.VideoFrameTypeIDR
		pCur.ISubSeqId = GetSubSequenceId(pCtx, api.VideoFrameTypeIDR)
		//point to next pLayerBsInfo
		encExtNextLayerBsInfo(pCtx, pLayerBsInfo, iLayerBsInfoIdx, iCountNal)
		pCtx.pOut.iLayerBsIndex++
		//update for external countings
		(*iLayerNum)++
	}

	// write PPS
	pCtx.pFuncList.pParametersetStrategy.UpdatePpsList(pCtx)

	//TODO: under new strategy, will PPS be correctly updated?
	for iSpatialId := int32(0); iSpatialId < kiSpatialNum; iSpatialId++ {
		iCountNal = 0
		for iIdx := int32(0); iIdx < pCtx.iPpsNum; iIdx++ {
			//writing one NAL
			iNalSize := int32(0)
			iReturn = WelsWriteOnePPS(pCtx, iIdx, &iNalSize)
			if iReturn != ENC_RETURN_SUCCESS {
				return iReturn
			}

			pLayerBsInfo[*iLayerBsInfoIdx].PNalLengthInByte[iCountNal] = iNalSize
			iNonVclSize += iNalSize
			iCountNal++
			//finish writing one NAL
		}

		pCur := &pLayerBsInfo[*iLayerBsInfoIdx]
		pCur.UiSpatialId = uint8(iSpatialId)
		pCur.UiTemporalId = 0
		pCur.UiQualityId = 0
		pCur.UiLayerType = uint8(api.NON_VIDEO_CODING_LAYER)
		pCur.INalCount = iCountNal
		pCur.EFrameType = api.VideoFrameTypeIDR
		pCur.ISubSeqId = GetSubSequenceId(pCtx, api.VideoFrameTypeIDR)
		//point to next pLayerBsInfo
		encExtNextLayerBsInfo(pCtx, pLayerBsInfo, iLayerBsInfoIdx, iCountNal)
		pCtx.pOut.iLayerBsIndex++
		//update for external countings
		(*iLayerNum)++
	}

	// to check number of layers / nals / slices dependencies
	if *iLayerNum > api.MAX_LAYER_NUM_OF_FRAME {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "WriteSavcParaset(), iLayerNum(%d) > MAX_LAYER_NUM_OF_FRAME(%d)!",
			*iLayerNum, api.MAX_LAYER_NUM_OF_FRAME)
		return ENC_RETURN_UNEXPECTED
	}

	*iFrameSize += iNonVclSize
	return iReturn
}

func StackBackEncoderStatus(pEncCtx *sWelsEncCtx, keFrameType api.EVideoFrameType) {
	pParamInternal := &pEncCtx.pSvcParam.sDependencyLayers[pEncCtx.uiDependencyId]
	// for bitstream writing
	pEncCtx.iPosBsBuffer = 0       // reset bs pBuffer position
	pEncCtx.pOut.iNalIndex = 0     // reset NAL index
	pEncCtx.pOut.iLayerBsIndex = 0 // reset index of Layer Bs

	common.InitBits(&pEncCtx.pOut.sBsWrite, pEncCtx.pOut.pBsBuffer, 0, int32(pEncCtx.pOut.uiSize))
	if (keFrameType == api.VideoFrameTypeP) || (keFrameType == api.VideoFrameTypeI) {
		pParamInternal.iFrameIndex--
		if pParamInternal.iPOC != 0 {
			pParamInternal.iPOC -= 2
		} else {
			pParamInternal.iPOC = (1 << uint32(pEncCtx.pSps.iLog2MaxPocLsb)) - 2
		}

		LoadBackFrameNum(pEncCtx, int32(pEncCtx.uiDependencyId))

		pEncCtx.eNalType = common.NAL_UNIT_CODED_SLICE
		pEncCtx.eSliceType = common.P_SLICE
		//pEncCtx->eNalPriority = pEncCtx->eLastNalPriority; //not need this since eNalPriority will be updated at the beginning of coding a frame
	} else if keFrameType == api.VideoFrameTypeIDR {
		pParamInternal.uiIdrPicId--

		//set the next frame to be IDR
		ForceCodingIDR(pEncCtx, int32(pEncCtx.uiDependencyId))
	} else { // B pictures are not supported now, any else?
		// assert (0)
	}

	// no need to stack back RC info since the info is still useful for later RQ model calculation
	// no need to stack back MB slicing info for dynamic balancing, since the info is still refer-able
}

func ClearFrameBsInfo(pCtx *sWelsEncCtx, pFbi *api.SFrameBSInfo) {
	pFbi.SLayerInfo[0].PBsBuf = pCtx.pFrameBs
	pFbi.SLayerInfo[0].PNalLengthInByte = pCtx.pOut.pNalLen

	for i := int32(0); i < pFbi.ILayerNum; i++ {
		pFbi.SLayerInfo[i].INalCount = 0
		pFbi.SLayerInfo[i].EFrameType = api.VideoFrameTypeSkip
	}
	pFbi.ILayerNum = 0
	pFbi.IFrameSizeInBytes = 0
}

// PrepareEncodeFrame decides the frame type of the frame (or layer) to code
// and writes the parameter sets for IDR frames.
func PrepareEncodeFrame(pCtx *sWelsEncCtx, pLayerBsInfo []api.SLayerBSInfo, iLayerBsInfoIdx *int, iSpatialNum int32, iCurDid *int8, iCurTid *int32, iLayerNum *int32, iFrameSize *int32, uiTimeStamp int64) api.EVideoFrameType {
	pSvcParam := pCtx.pSvcParam
	pSpatialIndexMap := pCtx.sSpatialIndexMap[:]

	bSkipFrameFlag := WelsRcCheckFrameStatus(pCtx, uiTimeStamp, iSpatialNum, int32(*iCurDid))
	eFrameType := DecideFrameType(pCtx, int8(iSpatialNum), int32(*iCurDid), bSkipFrameFlag)
	if eFrameType == api.VideoFrameTypeSkip {
		if pSvcParam.BSimulcastAVC {
			if pCtx.pFuncList.pfRc.pfWelsUpdateBufferWhenSkip != nil {
				pCtx.pFuncList.pfRc.pfWelsUpdateBufferWhenSkip(pCtx, int32(*iCurDid))
			}
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"[Rc] Frame timestamp = %lld, iDid = %d,skip one frame due to target_br, continual skipped %d frames",
				uiTimeStamp, *iCurDid, pCtx.pWelsSvcRc[*iCurDid].iContinualSkipFrames)
		} else {
			if pCtx.pFuncList.pfRc.pfWelsUpdateBufferWhenSkip != nil {
				for i := int32(0); i < iSpatialNum; i++ {
					pCtx.pFuncList.pfRc.pfWelsUpdateBufferWhenSkip(pCtx, pSpatialIndexMap[i].iDid)
				}
			}
			common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
				"[Rc] Frame timestamp = %lld, iDid = %d,skip one frame due to target_br, continual skipped %d frames",
				uiTimeStamp, *iCurDid, pCtx.pWelsSvcRc[*iCurDid].iContinualSkipFrames)
		}

	} else {
		pParamInternal := &pSvcParam.sDependencyLayers[*iCurDid]

		*iCurTid = GetTemporalLevel(pParamInternal, pParamInternal.iCodingIndex,
			int32(pSvcParam.uiGopSize))
		pCtx.uiTemporalId = uint8(*iCurTid)
		if eFrameType == api.VideoFrameTypeIDR {
			// write parameter sets bitstream or SEI/SSEI (if any) here
			// TODO: use function pointer instead
			if (api.SPS_LISTING & pCtx.pSvcParam.ESpsPpsIdStrategy) == 0 {
				if pSvcParam.BSimulcastAVC {
					pCtx.iEncoderError = WriteSavcParaset(pCtx, int32(*iCurDid), pLayerBsInfo, iLayerBsInfoIdx, iLayerNum, iFrameSize)
					pParamInternal.uiIdrPicId++
				} else {
					pCtx.iEncoderError = WriteSsvcParaset(pCtx, iSpatialNum, pLayerBsInfo, iLayerBsInfoIdx, iLayerNum, iFrameSize)
				}
			} else {
				pCtx.iEncoderError = WriteSavcParaset_Listing(pCtx, iSpatialNum, pLayerBsInfo, iLayerBsInfoIdx, iLayerNum, iFrameSize)
			}
		}
	}
	return eFrameType
}

// WelsEncoderEncodeExt is the core svc encoding process.
func WelsEncoderEncodeExt(pCtx *sWelsEncCtx, pFbi *api.SFrameBSInfo, pSrcPic *api.SSourcePicture) int32 {
	if pCtx == nil {
		return ENC_RETURN_MEMALLOCERR
	}
	// SLayerBSInfo* pLayerBsInfo = &pFbi->sLayerInfo[0] -> (slice, index)
	pLayerBsInfoList := pFbi.SLayerInfo[:]
	iLayerBsInfoIdx := 0
	pSvcParam := pCtx.pSvcParam
	pSpatialIndexMap := pCtx.sSpatialIndexMap[:]
	var fsnr *SPicture
	var pEncPic *SPicture // to be decided later
	iLayerNum := int32(0)
	iLayerSize := int32(0)
	iSpatialNum :=
		int32(0) // available count number of spatial layers due to frame size changed in this given frame
	iSpatialIdx := int32(0) // iIndex of spatial layers due to frame size changed in this given frame
	iFrameSize := int32(0)
	iNalIdxInLayer := int32(0)
	iCountNal := int32(0)
	eFrameType := api.VideoFrameTypeInvalid
	iCurWidth := int32(0)
	iCurHeight := int32(0)
	eNalType := common.EWelsNalUnitType(common.NAL_UNIT_UNSPEC_0)
	eNalRefIdc := common.EWelsNalRefIdc(common.NRI_PRI_LOWEST)
	iCurDid := int8(0)
	iCurTid := int32(0)
	bAvcBased := false
	pLogCtx := &pCtx.sLogCtx
	fSnrY, fSnrU, fSnrV := float32(.0), float32(.0), float32(.0)

	pLayerBsInfo := func() *api.SLayerBSInfo { return &pLayerBsInfoList[iLayerBsInfoIdx] }

	pCtx.iEncoderError = ENC_RETURN_SUCCESS
	pCtx.bCurFrameMarkedAsSceneLtr = false
	pFbi.EFrameType = api.VideoFrameTypeSkip
	pFbi.ILayerNum = 0 // for initialization
	pFbi.UiTimeStamp = GetTimestampForRc(pSrcPic.UiTimeStamp, pCtx.uiLastTimestamp,
		pCtx.pSvcParam.SSpatialLayers[pCtx.pSvcParam.ISpatialLayerNum-1].FFrameRate)
	for iNalIdx := 0; iNalIdx < api.MAX_LAYER_NUM_OF_FRAME; iNalIdx++ {
		pFbi.SLayerInfo[iNalIdx].EFrameType = api.VideoFrameTypeSkip
		pFbi.SLayerInfo[iNalIdx].INalCount = 0
	}
	// perform csc/denoise/downsample/padding, generate spatial layers
	iRet := pCtx.pVpp.BuildSpatialPicList(pCtx, pSrcPic, &iSpatialNum)
	if iRet != ENC_RETURN_SUCCESS {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "Failed in BuildSpatialPicList, error=%d", iRet)
		return iRet
	}

	if pCtx.pFuncList.pfRc.pfWelsUpdateMaxBrWindowStatus != nil {
		pCtx.pFuncList.pfRc.pfWelsUpdateMaxBrWindowStatus(pCtx, iSpatialNum, pFbi.UiTimeStamp)
	}

	if iSpatialNum < 1 {
		for iDidIdx := int32(0); iDidIdx < pSvcParam.ISpatialLayerNum; iDidIdx++ {
			pParamInternal := &pSvcParam.sDependencyLayers[iDidIdx]
			pParamInternal.iCodingIndex++
		}
		pFbi.EFrameType = api.VideoFrameTypeSkip
		pLayerBsInfo().EFrameType = api.VideoFrameTypeSkip
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_DEBUG,
			"[Rc] Frame timestamp = %lld, skip one frame due to preprocessing return (temporal layer settings or else)",
			pSrcPic.UiTimeStamp)
		return ENC_RETURN_SUCCESS
	}

	InitBitStream(pCtx)
	pLayerBsInfo().PBsBuf = pCtx.pFrameBs
	pLayerBsInfo().PNalLengthInByte = pCtx.pOut.pNalLen
	iCurDid = int8(pSpatialIndexMap[0].iDid)
	pCtx.pCurDqLayer = pCtx.ppDqLayerList[iCurDid]
	pCtx.pCurDqLayer.pRefLayer = nil
	if !pSvcParam.BSimulcastAVC {
		eFrameType = PrepareEncodeFrame(pCtx, pLayerBsInfoList, &iLayerBsInfoIdx, iSpatialNum, &iCurDid, &iCurTid, &iLayerNum, &iFrameSize,
			pFbi.UiTimeStamp)
		if eFrameType == api.VideoFrameTypeSkip {
			pFbi.EFrameType = api.VideoFrameTypeSkip
			pLayerBsInfo().EFrameType = api.VideoFrameTypeSkip
			return ENC_RETURN_SUCCESS
		}
	} else {
		for iDidIdx := int32(0); iDidIdx < pSvcParam.ISpatialLayerNum; iDidIdx++ {
			pParamInternal := &pSvcParam.sDependencyLayers[iDidIdx]
			iTemporalId := GetTemporalLevel(pParamInternal, pParamInternal.iCodingIndex,
				int32(pSvcParam.uiGopSize))
			if iTemporalId == int32(INVALID_TEMPORAL_ID) {
				pParamInternal.iCodingIndex++
			}
		}
	}

	for iSpatialIdx < iSpatialNum {
		iCurDid = int8(pSpatialIndexMap[iSpatialIdx].iDid)
		pParam := &pSvcParam.SSpatialLayers[iCurDid]
		pParamInternal := &pSvcParam.sDependencyLayers[iCurDid]
		iDecompositionStages := pSvcParam.sDependencyLayers[iCurDid].iDecompositionStages
		pCtx.pCurDqLayer = pCtx.ppDqLayerList[iCurDid]
		pCtx.uiDependencyId = uint8(iCurDid)

		if pSvcParam.BSimulcastAVC {
			eFrameType = PrepareEncodeFrame(pCtx, pLayerBsInfoList, &iLayerBsInfoIdx, iSpatialNum, &iCurDid, &iCurTid, &iLayerNum, &iFrameSize,
				pFbi.UiTimeStamp)
			if eFrameType == api.VideoFrameTypeSkip {
				pLayerBsInfo().EFrameType = api.VideoFrameTypeSkip
				iSpatialIdx++
				continue
			}
		}
		InitFrameCoding(pCtx, eFrameType, int32(iCurDid))
		pCtx.pVpp.AnalyzeSpatialPic(pCtx, int32(iCurDid))

		pEncPic = pSpatialIndexMap[iSpatialIdx].pSrc
		pCtx.pEncPic = pEncPic
		pCtx.pEncPic.iPictureType = int32(pCtx.eSliceType)
		pCtx.pEncPic.iFramePoc = pParamInternal.iPOC

		iCurWidth = pParam.IVideoWidth
		iCurHeight = pParam.IVideoHeight
		// Encoding this picture might mulitiple sQualityStat layers potentially be encoded as followed
		switch pParam.SSliceArgument.UiSliceMode {
		case api.SM_FIXEDSLCNUM_SLICE:
			if (pSvcParam.IMultipleThreadIdc > 1) &&
				(pSvcParam.BUseLoadBalancing &&
					uint32(pSvcParam.IMultipleThreadIdc) >= pSvcParam.SSpatialLayers[iCurDid].SSliceArgument.UiSliceNum) {
				if iCurDid > 0 {
					AdjustEnhanceLayer(pCtx, int32(iCurDid))
				} else {
					AdjustBaseLayer(pCtx)
				}
			}
		case api.SM_SIZELIMITED_SLICE:
			iPicIPartitionNum := PicPartitionNumDecision(pCtx)
			// MT compatibility
			pCtx.iActiveThreadsNum =
				int16(iPicIPartitionNum) // we try to active number of threads, equal to number of picture partitions
			WelsInitCurrentDlayerMltslc(pCtx, iPicIPartitionNum)
		default:
		}

		/* coding each spatial layer, only one sQualityStat layer within spatial support */
		iSliceCount := int32(1)
		if iLayerNum >= api.MAX_LAYER_NUM_OF_FRAME { // check available layer_bs_info writing as follows
			common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsEncoderEncodeExt(), iLayerNum(%d) overflow(max:%d)!", iLayerNum,
				api.MAX_LAYER_NUM_OF_FRAME)
			return ENC_RETURN_UNSUPPORTED_PARA
		}

		iNalIdxInLayer = 0
		bAvcBased = ((pSvcParam.BSimulcastAVC) || (iCurDid == BASE_DEPENDENCY_ID))
		pCtx.bNeedPrefixNalFlag = ((!pSvcParam.BSimulcastAVC) && (bAvcBased &&
			(pSvcParam.BPrefixNalAddingCtrl ||
				(pSvcParam.ISpatialLayerNum > 1))))

		if eFrameType == api.VideoFrameTypeP {
			if bAvcBased {
				eNalType = common.NAL_UNIT_CODED_SLICE
			} else {
				eNalType = common.NAL_UNIT_CODED_SLICE_EXT
			}
		} else if eFrameType == api.VideoFrameTypeIDR {
			if bAvcBased {
				eNalType = common.NAL_UNIT_CODED_SLICE_IDR
			} else {
				eNalType = common.NAL_UNIT_CODED_SLICE_EXT
			}
		}
		if iCurTid == 0 || pCtx.eSliceType == common.I_SLICE {
			eNalRefIdc = common.NRI_PRI_HIGHEST
		} else if iCurTid == iDecompositionStages {
			eNalRefIdc = common.NRI_PRI_LOWEST
		} else if 1+iCurTid == iDecompositionStages {
			eNalRefIdc = common.NRI_PRI_LOW
		} else if 2+iCurTid == iDecompositionStages {
			eNalRefIdc = common.NRI_PRI_HIGH
		} else { // more details for other temporal layers?
			eNalRefIdc = common.NRI_PRI_HIGHEST
		}
		pCtx.eNalType = eNalType
		pCtx.eNalPriority = eNalRefIdc

		pCtx.pDecPic = pCtx.ppRefPicListExt[iCurDid].pNextBuffer
		fsnr = pCtx.pDecPic
		pCtx.pDecPic.iPictureType = int32(pCtx.eSliceType)
		pCtx.pDecPic.iFramePoc = pParamInternal.iPOC

		WelsInitCurrentLayer(pCtx, iCurWidth, iCurHeight)

		pCtx.pReferenceStrategy.MarkPic()
		if !pCtx.pReferenceStrategy.BuildRefList(pParamInternal.iPOC, 0) {
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"WelsEncoderEncodeExt(), WelsBuildRefList failed for P frames, pCtx->iNumRef0= %d. ForceCodingIDR!",
				pCtx.iNumRef0)
			eFrameType = api.VideoFrameTypeIDR
			pCtx.iEncoderError = ENC_RETURN_CORRECTED
			break
		}
		if pCtx.eSliceType != common.I_SLICE {
			pCtx.pReferenceStrategy.AfterBuildRefList()
		}
		if pSvcParam.IRCMode != api.RC_OFF_MODE {
			var pRefPicture *SPicture
			if (pCtx.eSliceType == common.P_SLICE) && (pCtx.iNumRef0 > 0) {
				pRefPicture = pCtx.pRefList0[0]
			}
			pCtx.pVpp.AnalyzePictureComplexity(pCtx, pCtx.pEncPic, pRefPicture,
				int32(iCurDid), (pCtx.eSliceType == common.P_SLICE) && pSvcParam.BEnableBackgroundDetection)
		}
		WelsUpdateRefSyntax(pCtx, pParamInternal.iPOC,
			int32(eFrameType)) //get reordering syntax used for writing slice header and transmit to encoder.
		PrefetchReferencePicture(pCtx, eFrameType) // update reference picture for current pDq layer
		pCtx.pFuncList.pfRc.pfWelsRcPictureInit(pCtx, pFbi.UiTimeStamp)
		PreprocessSliceCoding(pCtx) // MUST be called after pfWelsRcPictureInit() and WelsInitCurrentLayer()

		//TODO Complexity Calculation here for screen content
		iLayerSize = 0
		if api.SM_SINGLE_SLICE == pParam.SSliceArgument.UiSliceMode { // only one slice within a sQualityStat layer
			iSliceSize := int32(0)
			iPayloadSize := int32(0)
			pCurSlice := &pCtx.pCurDqLayer.sSliceBufferInfo[0].pSliceBuffer[0]

			if pCtx.bNeedPrefixNalFlag {
				pCtx.iEncoderError = AddPrefixNal(pCtx, pLayerBsInfo(), pLayerBsInfo().PNalLengthInByte, &iNalIdxInLayer, eNalType,
					eNalRefIdc,
					&iPayloadSize)
				if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
					return pCtx.iEncoderError
				}
				iLayerSize += iPayloadSize
			}

			WelsLoadNal(pCtx.pOut, int32(eNalType), int32(eNalRefIdc))
			pCtx.iEncoderError = SetSliceBoundaryInfo(pCtx.pCurDqLayer, pCurSlice, 0)
			if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
				return pCtx.iEncoderError
			}

			pCtx.iEncoderError = WelsCodeOneSlice(pCtx, pCurSlice, int32(eNalType))
			if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
				return pCtx.iEncoderError
			}

			WelsUnloadNal(pCtx.pOut)

			pCtx.iEncoderError = WelsEncodeNal(&pCtx.pOut.sNalList[pCtx.pOut.iNalIndex-1],
				&pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt,
				pCtx.iFrameBsSize-pCtx.iPosBsBuffer,
				pCtx.pFrameBs[pCtx.iPosBsBuffer:],
				&pLayerBsInfo().PNalLengthInByte[iNalIdxInLayer])
			if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
				return pCtx.iEncoderError
			}
			iSliceSize = pLayerBsInfo().PNalLengthInByte[iNalIdxInLayer]

			iLayerSize += iSliceSize
			pCtx.iPosBsBuffer += iSliceSize
			pLbi := pLayerBsInfo()
			pLbi.UiLayerType = uint8(api.VIDEO_CODING_LAYER)
			pLbi.UiSpatialId = uint8(iCurDid)
			pLbi.UiTemporalId = uint8(iCurTid)
			pLbi.UiQualityId = 0
			iNalIdxInLayer++
			pLbi.INalCount = iNalIdxInLayer
			pLbi.EFrameType = eFrameType
			pLbi.ISubSeqId = GetSubSequenceId(pCtx, eFrameType)
		} else if (api.SM_SIZELIMITED_SLICE == pParam.SSliceArgument.UiSliceMode) && (pSvcParam.IMultipleThreadIdc <= 1) {
			// for dynamic slicing single threading..
			kiLastMbInFrame := pCtx.pCurDqLayer.sSliceEncCtx.iMbNumInFrame
			pCtx.iEncoderError = WelsCodeOnePicPartition(pCtx, pFbi, pLayerBsInfo(), &iNalIdxInLayer, &iLayerSize, 0,
				kiLastMbInFrame-1, 0)
			pLayerBsInfo().EFrameType = eFrameType
			pLayerBsInfo().ISubSeqId = GetSubSequenceId(pCtx, eFrameType)
			if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
				return pCtx.iEncoderError
			}
		} else {
			//other multi-slice uiSliceMode
			// THREAD_FULLY_FIRE_MODE/THREAD_PICK_UP_MODE for any mode of non-SM_SIZELIMITED_SLICE
			if (api.SM_SIZELIMITED_SLICE != pParam.SSliceArgument.UiSliceMode) && (pSvcParam.IMultipleThreadIdc > 1) {
				iSliceCount = GetCurrentSliceNum(pCtx.pCurDqLayer)
				if iLayerNum+1 >= api.MAX_LAYER_NUM_OF_FRAME { // check available layer_bs_info for further writing as followed
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
						"WelsEncoderEncodeExt(), iLayerNum(%d) overflow(max:%d) at iDid= %d uiSliceMode= %d, iSliceCount= %d!",
						iLayerNum, api.MAX_LAYER_NUM_OF_FRAME, iCurDid, pParam.SSliceArgument.UiSliceMode, iSliceCount)
					return ENC_RETURN_UNSUPPORTED_PARA
				}
				if iSliceCount <= 1 {
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
						"WelsEncoderEncodeExt(), iSliceCount(%d) from GetCurrentSliceNum() is untrusted due stack/heap crupted!",
						iSliceCount)
					return ENC_RETURN_UNEXPECTED
				}
				//note: the old codes are removed at commit: 3e0ee69
				pLbi := pLayerBsInfo()
				pLbi.PBsBuf = pCtx.pFrameBs[pCtx.iPosBsBuffer:]
				pLbi.UiLayerType = uint8(api.VIDEO_CODING_LAYER)
				pLbi.UiSpatialId = pCtx.uiDependencyId
				pLbi.UiTemporalId = pCtx.uiTemporalId
				pLbi.UiQualityId = 0
				pLbi.INalCount = 0
				pLbi.EFrameType = eFrameType
				pLbi.ISubSeqId = GetSubSequenceId(pCtx, eFrameType)

				pCtx.pTaskManage.ExecuteTasks(WELS_ENC_TASK_ENCODING)
				if pCtx.iEncoderError != 0 {
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
						"WelsEncoderEncodeExt(), multi-slice (mode %d) encoding error!",
						pParam.SSliceArgument.UiSliceMode)
					return pCtx.iEncoderError
				}

				iLayerSize = AppendSliceToFrameBs(pCtx, pLbi, iSliceCount)
				if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
					return pCtx.iEncoderError
				}
			} else if (api.SM_SIZELIMITED_SLICE == pParam.SSliceArgument.UiSliceMode) && (pSvcParam.IMultipleThreadIdc > 1) {
				// THREAD_FULLY_FIRE_MODE && SM_SIZELIMITED_SLICE
				kiPartitionCnt := int32(pCtx.iActiveThreadsNum)

				//TODO: use a function to remove duplicate code here and ln3994
				iLayerBsIdx := pCtx.pOut.iLayerBsIndex
				pLbi := &pFbi.SLayerInfo[iLayerBsIdx]
				pLbi.PBsBuf = pCtx.pFrameBs[pCtx.iPosBsBuffer:]
				pLbi.UiLayerType = uint8(api.VIDEO_CODING_LAYER)
				pLbi.UiSpatialId = pCtx.uiDependencyId
				pLbi.UiTemporalId = pCtx.uiTemporalId
				pLbi.UiQualityId = 0
				pLbi.INalCount = 0
				pLbi.EFrameType = eFrameType
				pLbi.ISubSeqId = GetSubSequenceId(pCtx, eFrameType)
				iIdx := int32(0)
				for iIdx < kiPartitionCnt {
					pCtx.pSliceThreading.pThreadPEncCtx[iIdx].pFrameBsInfo = pFbi
					pCtx.pSliceThreading.pThreadPEncCtx[iIdx].iSliceIndex = iIdx
					iIdx++
				}

				iRet := InitAllSlicesInThread(pCtx)
				if iRet != 0 {
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
						"WelsEncoderEncodeExt(), multi-slice (mode %d) InitAllSlicesInThread() error!",
						pParam.SSliceArgument.UiSliceMode)
					return ENC_RETURN_UNEXPECTED
				}
				pCtx.pTaskManage.ExecuteTasks(WELS_ENC_TASK_ENCODING)

				if pCtx.iEncoderError != 0 {
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
						"WelsEncoderEncodeExt(), multi-slice (mode %d) encoding error = %d!",
						pParam.SSliceArgument.UiSliceMode, pCtx.iEncoderError)
					return pCtx.iEncoderError
				}

				iRet = SliceLayerInfoUpdate(pCtx, pFbi, pLayerBsInfo(), pParam.SSliceArgument.UiSliceMode)
				if iRet != 0 {
					common.WelsLog(pLogCtx, api.WELS_LOG_ERROR,
						"WelsEncoderEncodeExt(), multi-slice (mode %d) InitAllSlicesInThread() error!",
						pParam.SSliceArgument.UiSliceMode)
					return ENC_RETURN_UNEXPECTED
				}

				iSliceCount = GetCurrentSliceNum(pCtx.pCurDqLayer)
				iLayerSize = AppendSliceToFrameBs(pCtx, pLayerBsInfo(), iSliceCount)
				if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
					return pCtx.iEncoderError
				}
			} else { // for non-dynamic-slicing mode single threading branch..
				bNeedPrefix := pCtx.bNeedPrefixNalFlag
				iSliceIdx := int32(0)
				var pCurSlice *SSlice

				iSliceCount = GetCurrentSliceNum(pCtx.pCurDqLayer)
				for iSliceIdx < iSliceCount {
					iSliceSize := int32(0)
					iPayloadSize := int32(0)

					if bNeedPrefix {
						pCtx.iEncoderError = AddPrefixNal(pCtx, pLayerBsInfo(), pLayerBsInfo().PNalLengthInByte, &iNalIdxInLayer, eNalType,
							eNalRefIdc,
							&iPayloadSize)
						if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
							return pCtx.iEncoderError
						}
						iLayerSize += iPayloadSize
					}

					WelsLoadNal(pCtx.pOut, int32(eNalType), int32(eNalRefIdc))

					pCurSlice = &pCtx.pCurDqLayer.sSliceBufferInfo[0].pSliceBuffer[iSliceIdx]
					pCtx.iEncoderError = SetSliceBoundaryInfo(pCtx.pCurDqLayer, pCurSlice, iSliceIdx)

					pCtx.iEncoderError = WelsCodeOneSlice(pCtx, pCurSlice, int32(eNalType))
					if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
						return pCtx.iEncoderError
					}

					WelsUnloadNal(pCtx.pOut)

					pCtx.iEncoderError = WelsEncodeNal(&pCtx.pOut.sNalList[pCtx.pOut.iNalIndex-1],
						&pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt,
						pCtx.iFrameBsSize-pCtx.iPosBsBuffer,
						pCtx.pFrameBs[pCtx.iPosBsBuffer:], &pLayerBsInfo().PNalLengthInByte[iNalIdxInLayer])
					if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
						return pCtx.iEncoderError
					}
					iSliceSize = pLayerBsInfo().PNalLengthInByte[iNalIdxInLayer]

					pCtx.iPosBsBuffer += iSliceSize
					iLayerSize += iSliceSize

					iNalIdxInLayer++
					iSliceIdx++
				}

				pLbi := pLayerBsInfo()
				pLbi.UiLayerType = uint8(api.VIDEO_CODING_LAYER)
				pLbi.UiSpatialId = uint8(iCurDid)
				pLbi.UiTemporalId = uint8(iCurTid)
				pLbi.UiQualityId = 0
				pLbi.INalCount = iNalIdxInLayer
				pLbi.EFrameType = eFrameType
				pLbi.ISubSeqId = GetSubSequenceId(pCtx, eFrameType)
			}
		}

		if nil != pCtx.pFuncList.pfRc.pfWelsRcPostFrameSkipping &&
			pCtx.pFuncList.pfRc.pfWelsRcPostFrameSkipping(pCtx, int32(iCurDid), pFbi.UiTimeStamp) {

			StackBackEncoderStatus(pCtx, eFrameType)
			ClearFrameBsInfo(pCtx, pFbi)

			iFrameSize = 0
			iLayerSize = 0
			iLayerNum = 0

			if pCtx.pFuncList.pfRc.pfWelsUpdateBufferWhenSkip != nil {
				pCtx.pFuncList.pfRc.pfWelsUpdateBufferWhenSkip(pCtx, iSpatialNum)
			}

			WelsRcPostFrameSkippedUpdate(pCtx, int32(iCurDid))
			pCtx.iEncoderError = ENC_RETURN_SUCCESS
			return ENC_RETURN_SUCCESS
		}

		// deblocking filter
		if (!pCtx.pCurDqLayer.bDeblockingParallelFlag) &&
			((eNalRefIdc != common.NRI_PRI_LOWEST) && (pSvcParam.sDependencyLayers[iCurDid].iHighestTemporalId == 0 ||
				iCurTid < int32(pSvcParam.sDependencyLayers[iCurDid].iHighestTemporalId))) {
			PerformDeblockingFilter(pCtx)
		}

		pCtx.pFuncList.pfRc.pfWelsRcPictureInfoUpdate(pCtx, iLayerSize)
		iFrameSize += iLayerSize
		RcUpdatePredictedFrameBits(pCtx)
		pCtx.pDecPic.iFrameAverageQp = pCtx.pWelsSvcRc[iCurDid].iAverageFrameQp

		//update scc related
		pCtx.pFuncList.pfUpdateFMESwitch(pCtx.pCurDqLayer)

		// reference picture list update
		if eNalRefIdc != common.NRI_PRI_LOWEST {
			if !pCtx.pReferenceStrategy.UpdateRefList() {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING, "WelsEncoderEncodeExt(), WelsUpdateRefList failed. ForceCodingIDR!")
				//the above is to set the next frame to be IDR
				pCtx.iEncoderError = ENC_RETURN_CORRECTED
				break
			}
		}

		//check MinCr
		{
			iMinCrFrameSize := (pParam.IVideoWidth * pParam.IVideoHeight * 3) >> 2 //MinCr = 2;
			if pParam.UiLevelIdc == api.LEVEL_3_1 || pParam.UiLevelIdc == api.LEVEL_3_2 || pParam.UiLevelIdc == api.LEVEL_4_0 {
				iMinCrFrameSize >>= 1 //MinCr = 4
			}
			if iFrameSize > iMinCrFrameSize {
				common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
					"WelsEncoderEncodeExt()MinCr Checking,codec bitstream size is larger than Level limitation")
			}
		}

		if fsnr != nil && (pSvcParam.BPsnrY || pSrcPic.BPsnrY) {
			fSnrY = common.WelsCalcPsnr(fsnr.pData[0], fsnr.iDataOff[0],
				fsnr.iLineSize[0],
				pEncPic.pData[0], pEncPic.iDataOff[0],
				pEncPic.iLineSize[0],
				iCurWidth,
				iCurHeight)
		}
		if fsnr != nil && (pSvcParam.BPsnrU || pSrcPic.BPsnrU) {
			fSnrU = common.WelsCalcPsnr(fsnr.pData[1], fsnr.iDataOff[1],
				fsnr.iLineSize[1],
				pEncPic.pData[1], pEncPic.iDataOff[1],
				pEncPic.iLineSize[1],
				(iCurWidth >> 1),
				(iCurHeight >> 1))
		}
		if fsnr != nil && (pSvcParam.BPsnrV || pSrcPic.BPsnrV) {
			fSnrV = common.WelsCalcPsnr(fsnr.pData[2], fsnr.iDataOff[2],
				fsnr.iLineSize[2],
				pEncPic.pData[2], pEncPic.iDataOff[2],
				pEncPic.iLineSize[2],
				(iCurWidth >> 1),
				(iCurHeight >> 1))
		}

		pLbi := pLayerBsInfo()
		pLbi.RPsnr[0] = 0
		pLbi.RPsnr[1] = 0
		pLbi.RPsnr[2] = 0
		if pSrcPic.BPsnrY {
			pLbi.RPsnr[0] = fSnrY
		}
		if pSrcPic.BPsnrU {
			pLbi.RPsnr[1] = fSnrU
		}
		if pSrcPic.BPsnrV {
			pLbi.RPsnr[2] = fSnrV
		}

		iCountNal = pLbi.INalCount
		iLayerNum++
		encExtNextLayerBsInfo(pCtx, pLayerBsInfoList, &iLayerBsInfoIdx, iCountNal)
		pCtx.pOut.iLayerBsIndex++

		if pSvcParam.IPaddingFlag != 0 && pCtx.pWelsSvcRc[pCtx.uiDependencyId].iPaddingSize > 0 {
			iPaddingNalSize := int32(0)
			pCtx.iEncoderError = WritePadding(pCtx, pCtx.pWelsSvcRc[pCtx.uiDependencyId].iPaddingSize, &iPaddingNalSize)
			if pCtx.iEncoderError != ENC_RETURN_SUCCESS {
				return pCtx.iEncoderError
			}

			if iPaddingNalSize <= 0 {
				return ENC_RETURN_UNEXPECTED
			}

			pCtx.pWelsSvcRc[pCtx.uiDependencyId].iPaddingBitrateStat += pCtx.pWelsSvcRc[pCtx.uiDependencyId].iPaddingSize

			pCtx.pWelsSvcRc[pCtx.uiDependencyId].iPaddingSize = 0

			pLbi = pLayerBsInfo()
			pLbi.UiSpatialId = 0
			pLbi.UiTemporalId = 0
			pLbi.UiQualityId = 0
			pLbi.UiLayerType = uint8(api.NON_VIDEO_CODING_LAYER)
			pLbi.INalCount = 1
			pLbi.PNalLengthInByte[0] = iPaddingNalSize
			pLbi.EFrameType = eFrameType
			pLbi.ISubSeqId = GetSubSequenceId(pCtx, eFrameType)
			encExtNextLayerBsInfo(pCtx, pLayerBsInfoList, &iLayerBsInfoIdx, 1)
			pCtx.pOut.iLayerBsIndex++
			iLayerNum++

			iFrameSize += iPaddingNalSize
		}

		if (pParam.SSliceArgument.UiSliceMode == api.SM_FIXEDSLCNUM_SLICE) &&
			pSvcParam.BUseLoadBalancing &&
			pSvcParam.IMultipleThreadIdc > 1 &&
			uint32(pSvcParam.IMultipleThreadIdc) >= pParam.SSliceArgument.UiSliceNum {
			CalcSliceComplexRatio(pCtx.pCurDqLayer)
		}

		pCtx.eLastNalPriority[iCurDid] = eNalRefIdc
		iSpatialIdx++

		if int32(iCurDid)+1 < pSvcParam.ISpatialLayerNum {
			//for next layer, note that iSpatialIdx has been ++ so it is pointer to next layer
			WelsSwapDqLayers(pCtx, pSpatialIndexMap[iSpatialIdx].iDid)
		}

		if pCtx.pVpp.UpdateSpatialPictures(pCtx, pSvcParam, int8(iCurTid), int32(iCurDid)) != 0 {
			ForceCodingIDR(pCtx, int32(iCurDid))
			common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
				"WelsEncoderEncodeExt(), Logic Error Found in Preprocess updating. ForceCodingIDR!")
			//the above is to set the next frame IDR
			pFbi.EFrameType = eFrameType
			pLayerBsInfo().EFrameType = eFrameType
			return ENC_RETURN_CORRECTED
		}

		if pSvcParam.BEnableLongTermReference && ((pCtx.pLtr[pCtx.uiDependencyId].bLTRMarkingFlag &&
			(pCtx.pLtr[pCtx.uiDependencyId].iLTRMarkMode == int32(LTR_DIRECT_MARK))) || eFrameType == api.VideoFrameTypeIDR) {
			pCtx.bRefOfCurTidIsLtr[iCurDid][iCurTid] = true
		}
		if pSvcParam.BSimulcastAVC {
			pParamInternal.iCodingIndex++
		}
	} //end of (iSpatialIdx/iSpatialNum)

	if !pSvcParam.BSimulcastAVC {
		for i := int32(0); i < pSvcParam.ISpatialLayerNum; i++ {
			pParamInternal := &pSvcParam.sDependencyLayers[i]
			pParamInternal.iCodingIndex++
		}
	}

	if ENC_RETURN_CORRECTED == pCtx.iEncoderError {
		pCtx.pVpp.UpdateSpatialPictures(pCtx, pSvcParam, int8(iCurTid), pSpatialIndexMap[iSpatialIdx].iDid)
		ForceCodingIDR(pCtx, pSpatialIndexMap[iSpatialIdx].iDid)
		common.WelsLog(pLogCtx, api.WELS_LOG_ERROR, "WelsEncoderEncodeExt(), Logic Error Found in temporal level. ForceCodingIDR!")
		//the above is to set the next frame IDR
		pFbi.EFrameType = eFrameType
		pLayerBsInfo().EFrameType = eFrameType
		return ENC_RETURN_CORRECTED
	}

	// to check number of layers / nals / slices dependencies
	if iLayerNum > api.MAX_LAYER_NUM_OF_FRAME {
		common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR, "WelsEncoderEncodeExt(), iLayerNum(%d) > MAX_LAYER_NUM_OF_FRAME(%d)!",
			iLayerNum, api.MAX_LAYER_NUM_OF_FRAME)
		return 1
	}

	pFbi.ILayerNum = iLayerNum

	common.WelsEmms()

	pLayerBsInfo().EFrameType = eFrameType
	pFbi.IFrameSizeInBytes = iFrameSize
	pFbi.EFrameType = eFrameType
	for k := int32(0); k < pFbi.ILayerNum; k++ {
		if pFbi.EFrameType != pFbi.SLayerInfo[k].EFrameType {
			pFbi.EFrameType = api.VideoFrameTypeIPMixed
		}
	}
	return ENC_RETURN_SUCCESS
}

// WelsEncoderParamAdjust adjusts the Wels SVC encoder parameters.
// SVC adjustment results in new requirement in memory blocks adjustment.
func WelsEncoderParamAdjust(ppCtx **sWelsEncCtx, pNewParam *SWelsSvcCodingParam) int32 {
	var pOldParam *SWelsSvcCodingParam
	iReturn := int32(ENC_RETURN_SUCCESS)
	iIndexD := int8(0)
	bNeedReset := false
	iSliceNum := int16(1)       // number of slices used
	iCacheLineSize := int32(16) // on chip cache line size in byte
	uiCpuFeatureFlags := uint32(0)

	if nil == ppCtx || nil == *ppCtx || nil == pNewParam {
		return 1
	}

	/* Check validation in new parameters */
	iReturn = ParamValidationExt(&(*ppCtx).sLogCtx, pNewParam)
	if iReturn != ENC_RETURN_SUCCESS {
		return iReturn
	}

	iReturn = GetMultipleThreadIdc(&(*ppCtx).sLogCtx, pNewParam, &iSliceNum, &iCacheLineSize, &uiCpuFeatureFlags)
	if iReturn != ENC_RETURN_SUCCESS {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_ERROR, "WelsEncoderParamAdjust(), GetMultipleThreadIdc failed return %d.",
			iReturn)
		return iReturn
	}

	pOldParam = (*ppCtx).pSvcParam

	if pOldParam.IUsageType != pNewParam.IUsageType {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_ERROR,
			"WelsEncoderParamAdjust(), does not expect in-middle change of iUsgaeType from %d to %d", pOldParam.IUsageType,
			pNewParam.IUsageType)
		return ENC_RETURN_UNSUPPORTED_PARA
	}

	/* Decide whether need reset for IDR frame based on adjusting prarameters changed */
	/* Temporal levels, spatial settings and/ or quality settings changed need update parameter sets related. */
	bNeedReset = (pOldParam == nil) ||
		(pOldParam.BSimulcastAVC != pNewParam.BSimulcastAVC) ||
		(pOldParam.ISpatialLayerNum != pNewParam.ISpatialLayerNum) ||
		(pOldParam.IPicWidth != pNewParam.IPicWidth ||
			pOldParam.IPicHeight != pNewParam.IPicHeight) ||
		(pOldParam.SUsedPicRect.iWidth != pNewParam.SUsedPicRect.iWidth ||
			pOldParam.SUsedPicRect.iHeight != pNewParam.SUsedPicRect.iHeight) ||
		(pOldParam.BEnableLongTermReference != pNewParam.BEnableLongTermReference) ||
		(pOldParam.ILTRRefNum != pNewParam.ILTRRefNum) ||
		(pOldParam.IMultipleThreadIdc != pNewParam.IMultipleThreadIdc) ||
		(pOldParam.BEnableBackgroundDetection != pNewParam.BEnableBackgroundDetection) ||
		(pOldParam.BEnableAdaptiveQuant != pNewParam.BEnableAdaptiveQuant) ||
		(pOldParam.ESpsPpsIdStrategy != pNewParam.ESpsPpsIdStrategy)
	if (pNewParam.iMaxNumRefFrame > pOldParam.iMaxNumRefFrame) ||
		((pOldParam.iMaxNumRefFrame == 1) && (pOldParam.ITemporalLayerNum == 1) && (pNewParam.ITemporalLayerNum == 2)) {
		bNeedReset = true
	}
	if bNeedReset {
		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
			"WelsEncoderParamAdjust(),bSimulcastAVC(%d,%d),iSpatialLayerNum(%d,%d),iPicWidth(%d,%d),iPicHeight(%d,%d),Rect.iWidth(%d,%d),Rect.iHeight(%d,%d)",
			encExtB2I(pOldParam.BSimulcastAVC), encExtB2I(pNewParam.BSimulcastAVC),
			pOldParam.ISpatialLayerNum, pNewParam.ISpatialLayerNum,
			pOldParam.IPicWidth, pNewParam.IPicWidth,
			pOldParam.IPicHeight, pNewParam.IPicHeight,
			pOldParam.SUsedPicRect.iWidth, pNewParam.SUsedPicRect.iWidth,
			pOldParam.SUsedPicRect.iHeight, pNewParam.SUsedPicRect.iHeight)

		common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
			"WelsEncoderParamAdjust(),bEnableLongTermReference(%d,%d),iLTRRefNum(%d,%d),iMultipleThreadIdc(%d,%d),bEnableBackgroundDetection(%d,%d),bEnableAdaptiveQuant(%d,%d),eSpsPpsIdStrategy(%d,%d),iMaxNumRefFrame(%d,%d),iTemporalLayerNum(%d,%d)",
			encExtB2I(pOldParam.BEnableLongTermReference), encExtB2I(pNewParam.BEnableLongTermReference),
			pOldParam.ILTRRefNum, pNewParam.ILTRRefNum,
			pOldParam.IMultipleThreadIdc, pNewParam.IMultipleThreadIdc,
			encExtB2I(pOldParam.BEnableBackgroundDetection), encExtB2I(pNewParam.BEnableBackgroundDetection),
			encExtB2I(pOldParam.BEnableAdaptiveQuant), encExtB2I(pNewParam.BEnableAdaptiveQuant),
			pOldParam.ESpsPpsIdStrategy, pNewParam.ESpsPpsIdStrategy,
			pOldParam.iMaxNumRefFrame, pNewParam.iMaxNumRefFrame,
			pOldParam.ITemporalLayerNum, pNewParam.ITemporalLayerNum)
	}
	if !bNeedReset { // Check its picture resolutions/quality settings respectively in each dependency layer
		iIndexD = 0
		for {
			kpOldDlp := &pOldParam.sDependencyLayers[iIndexD]
			kpNewDlp := &pNewParam.sDependencyLayers[iIndexD]
			fT1 := float32(.0)
			fT2 := float32(.0)

			// check frame size settings
			if pOldParam.SSpatialLayers[iIndexD].IVideoWidth != pNewParam.SSpatialLayers[iIndexD].IVideoWidth ||
				pOldParam.SSpatialLayers[iIndexD].IVideoHeight != pNewParam.SSpatialLayers[iIndexD].IVideoHeight ||
				kpOldDlp.iActualWidth != kpNewDlp.iActualWidth ||
				kpOldDlp.iActualHeight != kpNewDlp.iActualHeight {
				bNeedReset = true
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
					"WelsEncoderParamAdjust(),iIndexD = %d,sSpatialLayers.wxh_old(%d,%d),sSpatialLayers.wxh_new(%d,%d),iActualwxh_old(%d,%d),iActualwxh_new(%d,%d)",
					iIndexD, pOldParam.SSpatialLayers[iIndexD].IVideoWidth, pOldParam.SSpatialLayers[iIndexD].IVideoHeight,
					pNewParam.SSpatialLayers[iIndexD].IVideoWidth, pNewParam.SSpatialLayers[iIndexD].IVideoHeight,
					kpOldDlp.iActualWidth, kpOldDlp.iActualHeight,
					kpNewDlp.iActualWidth, kpNewDlp.iActualHeight)
				break
			}

			if pOldParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceMode !=
				pNewParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceMode ||
				pOldParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceNum !=
					pNewParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceNum {

				bNeedReset = true
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
					"WelsEncoderParamAdjust(),iIndexD = %d,uiSliceMode (%d,%d),uiSliceNum(%d,%d)", iIndexD,
					pOldParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceMode,
					pNewParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceMode,
					pOldParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceNum,
					pNewParam.SSpatialLayers[iIndexD].SSliceArgument.UiSliceNum)

				break
			}

			// check frame rate
			// we can not check whether corresponding fFrameRate is equal or not,
			// only need to check d_max/d_min and max_fr/d_max whether it is equal or not
			if kpNewDlp.fInputFrameRate > common.EPSN && kpOldDlp.fInputFrameRate > common.EPSN {
				fT1 = kpNewDlp.fOutputFrameRate/kpNewDlp.fInputFrameRate - kpOldDlp.fOutputFrameRate/kpOldDlp.fInputFrameRate
			}
			if kpNewDlp.fOutputFrameRate > common.EPSN && kpOldDlp.fOutputFrameRate > common.EPSN {
				fT2 = pNewParam.FMaxFrameRate/kpNewDlp.fOutputFrameRate - pOldParam.FMaxFrameRate/kpOldDlp.fOutputFrameRate
			}
			if fT1 > common.EPSN || fT1 < -common.EPSN || fT2 > common.EPSN || fT2 < -common.EPSN {
				bNeedReset = true
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
					"WelsEncoderParamAdjust() iIndexD = %d,fInputFrameRate(%f,%f),fOutputFrameRate(%f,%f),fMaxFrameRate(%f,%f)", iIndexD,
					kpOldDlp.fInputFrameRate, kpNewDlp.fInputFrameRate,
					kpOldDlp.fOutputFrameRate, kpNewDlp.fOutputFrameRate,
					pOldParam.FMaxFrameRate, pNewParam.FMaxFrameRate)
				break
			}
			if pOldParam.SSpatialLayers[iIndexD].UiProfileIdc != pNewParam.SSpatialLayers[iIndexD].UiProfileIdc {
				bNeedReset = true
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
					"WelsEncoderParamAdjust(),iIndexD = %d,uiProfileIdc(%d,%d)", iIndexD,
					pOldParam.SSpatialLayers[iIndexD].UiProfileIdc, pNewParam.SSpatialLayers[iIndexD].UiProfileIdc)
				break
			}
			//check level change,if new level is smaller than old level,don't reset encoder. still use old level.

			if pNewParam.SSpatialLayers[iIndexD].UiLevelIdc > pOldParam.SSpatialLayers[iIndexD].UiLevelIdc {
				bNeedReset = true
				common.WelsLog(&(*ppCtx).sLogCtx, api.WELS_LOG_INFO,
					"WelsEncoderParamAdjust(),iIndexD = %d,uiLevelIdc(%d,%d)", iIndexD,
					pOldParam.SSpatialLayers[iIndexD].UiLevelIdc, pNewParam.SSpatialLayers[iIndexD].UiLevelIdc)
				break
			}
			iIndexD++
			if !(int32(iIndexD) < pOldParam.ISpatialLayerNum) {
				break
			}
		}
	}

	if bNeedReset {
		sLogCtx := (*ppCtx).sLogCtx

		iOldSpsPpsIdStrategy := pOldParam.ESpsPpsIdStrategy
		var sTmpPsoVariable [PARA_SET_TYPE]SParaSetOffsetVariable
		var iTmpPpsIdList [MAX_DQ_LAYER_NUM][MAX_PPS_COUNT]int32
		//for LTR or SPS,PPS ID update
		uiMaxIdrPicId := uint16(0)
		for iIndexD = 0; int32(iIndexD) < pOldParam.ISpatialLayerNum; iIndexD++ {
			if pOldParam.sDependencyLayers[iIndexD].uiIdrPicId > uiMaxIdrPicId {
				uiMaxIdrPicId = pOldParam.sDependencyLayers[iIndexD].uiIdrPicId
			}
		}

		//for sEncoderStatistics
		sTempEncoderStatistics := (*ppCtx).sEncoderStatistics
		uiStartTimestamp := (*ppCtx).uiStartTimestamp
		iStatisticsLogInterval := (*ppCtx).iStatisticsLogInterval
		iLastStatisticsLogTs := (*ppCtx).iLastStatisticsLogTs
		//for sEncoderStatistics

		var sExistingParasetList SExistingParasetList
		var pExistingParasetList *SExistingParasetList

		if (api.CONSTANT_ID != iOldSpsPpsIdStrategy) && (api.CONSTANT_ID != pNewParam.ESpsPpsIdStrategy) {
			(*ppCtx).pFuncList.pParametersetStrategy.OutputCurrentStructure(&sTmpPsoVariable, &iTmpPpsIdList, *ppCtx,
				&sExistingParasetList)

			if (api.SPS_LISTING&iOldSpsPpsIdStrategy) != 0 &&
				(api.SPS_LISTING&pNewParam.ESpsPpsIdStrategy) != 0 {
				pExistingParasetList = &sExistingParasetList
			}
		}

		WelsUninitEncoderExt(ppCtx)

		/* Update new parameters */
		if WelsInitEncoderExt(ppCtx, pNewParam, &sLogCtx, pExistingParasetList) != 0 {
			return 1
		}
		//if WelsInitEncoderExt succeed
		//for LTR or SPS,PPS ID update
		for iIndexD = 0; int32(iIndexD) < pNewParam.ISpatialLayerNum; iIndexD++ {
			(*ppCtx).pSvcParam.sDependencyLayers[iIndexD].uiIdrPicId = uiMaxIdrPicId
		}

		//for sEncoderStatistics
		(*ppCtx).sEncoderStatistics = sTempEncoderStatistics
		(*ppCtx).uiStartTimestamp = uiStartTimestamp
		(*ppCtx).iStatisticsLogInterval = iStatisticsLogInterval
		(*ppCtx).iLastStatisticsLogTs = iLastStatisticsLogTs
		//for sEncoderStatistics

		//load back the needed structure for eSpsPpsIdStrategy
		if ((api.CONSTANT_ID != iOldSpsPpsIdStrategy) && (api.CONSTANT_ID != pNewParam.ESpsPpsIdStrategy)) ||
			((api.SPS_PPS_LISTING == iOldSpsPpsIdStrategy) &&
				(api.SPS_PPS_LISTING == pNewParam.ESpsPpsIdStrategy)) {
			(*ppCtx).pFuncList.pParametersetStrategy.LoadPreviousStructure(&sTmpPsoVariable, &iTmpPpsIdList)
		}
	} else {
		/* maybe adjustment introduced in bitrate or little settings adjustment and so on.. */
		iNumRefUpper := int32(MAX_REFERENCE_PICTURE_COUNT_NUM_SCREEN)
		if pNewParam.IUsageType == api.CAMERA_VIDEO_REAL_TIME {
			iNumRefUpper = MAX_REFERENCE_PICTURE_COUNT_NUM_CAMERA
		}
		pNewParam.INumRefFrame = common.WELS_CLIP3(pNewParam.INumRefFrame, MIN_REF_PIC_COUNT, iNumRefUpper)
		pNewParam.ILoopFilterDisableIdc = common.WELS_CLIP3(pNewParam.ILoopFilterDisableIdc, 0, 6)
		pNewParam.ILoopFilterAlphaC0Offset = common.WELS_CLIP3(pNewParam.ILoopFilterAlphaC0Offset, -6, 6)
		pNewParam.ILoopFilterBetaOffset = common.WELS_CLIP3(pNewParam.ILoopFilterBetaOffset, -6, 6)
		pNewParam.FMaxFrameRate = common.WELS_CLIP3(pNewParam.FMaxFrameRate, MIN_FRAME_RATE, MAX_FRAME_RATE)

		// we can not use direct struct based memcpy due some fields need keep unchanged as before
		pOldParam.FMaxFrameRate = pNewParam.FMaxFrameRate     // maximal frame rate [Hz / fps]
		pOldParam.IComplexityMode = pNewParam.IComplexityMode // color space of input sequence
		pOldParam.UiIntraPeriod = pNewParam.UiIntraPeriod     // intra period (multiple of GOP size as desired)
		pOldParam.ESpsPpsIdStrategy = pNewParam.ESpsPpsIdStrategy
		pOldParam.BPrefixNalAddingCtrl = pNewParam.BPrefixNalAddingCtrl
		pOldParam.INumRefFrame = pNewParam.INumRefFrame // number of reference frame used
		pOldParam.uiGopSize = pNewParam.uiGopSize
		if pOldParam.ITemporalLayerNum != pNewParam.ITemporalLayerNum {
			pOldParam.ITemporalLayerNum = pNewParam.ITemporalLayerNum
			for iIndexD := 0; iIndexD < MAX_DEPENDENCY_LAYER; iIndexD++ {
				pOldParam.sDependencyLayers[iIndexD].iCodingIndex = 0
			}
		}
		pOldParam.iDecompStages = pNewParam.iDecompStages
		/* denoise control */
		pOldParam.BEnableDenoise = pNewParam.BEnableDenoise

		/* background detection control */
		pOldParam.BEnableBackgroundDetection = pNewParam.BEnableBackgroundDetection

		/* adaptive quantization control */
		pOldParam.BEnableAdaptiveQuant = pNewParam.BEnableAdaptiveQuant

		/* int32_t term reference control */
		pOldParam.BEnableLongTermReference = pNewParam.BEnableLongTermReference
		pOldParam.ILtrMarkPeriod = pNewParam.ILtrMarkPeriod

		// keep below values unchanged as before
		pOldParam.BEnableSSEI = pNewParam.BEnableSSEI
		pOldParam.BSimulcastAVC = pNewParam.BSimulcastAVC
		pOldParam.BEnableFrameCroppingFlag = pNewParam.BEnableFrameCroppingFlag // enable frame cropping flag

		/* Motion search */

		/* Deblocking loop filter */
		pOldParam.ILoopFilterDisableIdc =
			pNewParam.ILoopFilterDisableIdc // 0: on, 1: off, 2: on except for slice boundaries
		pOldParam.ILoopFilterAlphaC0Offset = pNewParam.ILoopFilterAlphaC0Offset // AlphaOffset: valid range [-6, 6], default 0
		pOldParam.ILoopFilterBetaOffset =
			pNewParam.ILoopFilterBetaOffset // BetaOffset:  valid range [-6, 6], default 0

		/* Rate Control */
		pOldParam.IRCMode = pNewParam.IRCMode
		pOldParam.ITargetBitrate =
			pNewParam.ITargetBitrate // overall target bitrate introduced in RC module
		pOldParam.IPaddingFlag = pNewParam.IPaddingFlag

		/* Layer definition */
		pOldParam.BPrefixNalAddingCtrl = pNewParam.BPrefixNalAddingCtrl

		// d
		iIndexD = 0
		for {
			pOldDlpInternal := &pOldParam.sDependencyLayers[iIndexD]
			pNewDlpInternal := &pNewParam.sDependencyLayers[iIndexD]

			pOldDlp := &pOldParam.SSpatialLayers[iIndexD]
			pNewDlp := &pNewParam.SSpatialLayers[iIndexD]

			pOldDlpInternal.fInputFrameRate = pNewDlpInternal.fInputFrameRate   // input frame rate
			pOldDlpInternal.fOutputFrameRate = pNewDlpInternal.fOutputFrameRate // output frame rate
			pOldDlp.ISpatialBitrate = pNewDlp.ISpatialBitrate
			pOldDlp.IMaxSpatialBitrate = pNewDlp.IMaxSpatialBitrate
			pOldDlp.UiProfileIdc =
				pNewDlp.UiProfileIdc // value of profile IDC (0 for auto-detection)
			pOldDlp.IDLayerQp = pNewDlp.IDLayerQp

			/* Derived variants below */
			pOldDlpInternal.iTemporalResolution = pNewDlpInternal.iTemporalResolution
			pOldDlpInternal.iDecompositionStages = pNewDlpInternal.iDecompositionStages
			pOldDlpInternal.uiCodingIdx2TemporalId = pNewDlpInternal.uiCodingIdx2TemporalId // confirmed_safe_unsafe_usage
			iIndexD++
			if !(int32(iIndexD) < pOldParam.ISpatialLayerNum) {
				break
			}
		}
	}

	/* Any else initialization/reset for rate control here? */

	return 0
}

func WelsEncoderApplyLTR(pLogCtx *common.SLogContext, ppCtx **sWelsEncCtx, pLTRValue *api.SLTRConfig) int32 {
	var sConfig SWelsSvcCodingParam
	iNumRefFrame := int32(1)
	iRet := int32(0)
	sConfig = *(*ppCtx).pSvcParam
	sConfig.BEnableLongTermReference = pLTRValue.BEnableLongTermReference
	sConfig.ILTRRefNum = pLTRValue.ILTRRefNum
	uiGopSize := int32(1) << uint32(sConfig.ITemporalLayerNum-1)
	if sConfig.IUsageType == api.SCREEN_CONTENT_REAL_TIME {
		if sConfig.BEnableLongTermReference {
			sConfig.ILTRRefNum = LONG_TERM_REF_NUM_SCREEN //WELS_CLIP3 (sConfig.iLTRRefNum, 1, LONG_TERM_REF_NUM_SCREEN);
			iNumRefFrame = common.WELS_MAX(1, common.WELS_LOG2(uint32(uiGopSize))) + sConfig.ILTRRefNum
		} else {
			sConfig.ILTRRefNum = 0
			iNumRefFrame = common.WELS_MAX(1, uiGopSize>>1)
		}
	} else {
		if sConfig.BEnableLongTermReference {
			sConfig.ILTRRefNum = LONG_TERM_REF_NUM //WELS_CLIP3 (sConfig.iLTRRefNum, 1, LONG_TERM_REF_NUM);
		} else {
			sConfig.ILTRRefNum = 0
		}
		if (uiGopSize >> 1) > 1 {
			iNumRefFrame = (uiGopSize >> 1) + sConfig.ILTRRefNum
		} else {
			iNumRefFrame = MIN_REF_PIC_COUNT + sConfig.ILTRRefNum
		}
		iNumRefFrame = common.WELS_CLIP3(iNumRefFrame, MIN_REF_PIC_COUNT, MAX_REFERENCE_PICTURE_COUNT_NUM_CAMERA)
	}
	if iNumRefFrame > sConfig.iMaxNumRefFrame {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			" CWelsH264SVCEncoder::SetOption LTR flag = %d and number = %d: Required number of reference increased to %d and iMaxNumRefFrame is adjusted (from %d)",
			encExtB2I(sConfig.BEnableLongTermReference), sConfig.ILTRRefNum, iNumRefFrame, sConfig.iMaxNumRefFrame)
		sConfig.iMaxNumRefFrame = iNumRefFrame
	}

	if sConfig.INumRefFrame < iNumRefFrame {
		common.WelsLog(pLogCtx, api.WELS_LOG_WARNING,
			" CWelsH264SVCEncoder::SetOption LTR flag = %d and number = %d, Required number of reference increased from Old = %d to New = %d because of LTR setting",
			encExtB2I(sConfig.BEnableLongTermReference), sConfig.ILTRRefNum, sConfig.INumRefFrame, iNumRefFrame)
		sConfig.INumRefFrame = iNumRefFrame
	}
	common.WelsLog(pLogCtx, api.WELS_LOG_INFO, "CWelsH264SVCEncoder::SetOption enable LTR = %d,ltrnum = %d",
		encExtB2I(sConfig.BEnableLongTermReference), sConfig.ILTRRefNum)
	iRet = WelsEncoderParamAdjust(ppCtx, &sConfig)
	return iRet
}

// DynSliceRealloc reallocates the frame bitstream info and the slice buffer
// of the current layer. pLayerBsInfo: the current layer, an element of
// pFrameBsInfo.SLayerInfo (compared by address).
func DynSliceRealloc(pCtx *sWelsEncCtx, pFrameBsInfo *api.SFrameBSInfo, pLayerBsInfo *api.SLayerBSInfo) int32 {
	iRet := int32(0)

	iRet = FrameBsRealloc(pCtx, pFrameBsInfo, pLayerBsInfo, pCtx.pCurDqLayer.iMaxSliceNum)
	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	iRet = ReallocSliceBuffer(pCtx)
	if ENC_RETURN_SUCCESS != iRet {
		return iRet
	}

	return iRet
}

func WelsCodeOnePicPartition(pCtx *sWelsEncCtx, pFrameBSInfo *api.SFrameBSInfo, pLayerBsInfo *api.SLayerBSInfo, pNalIdxInLayer *int32, pLayerSize *int32, iFirstMbIdxInPartition int32, iEndMbIdxInPartition int32, iStartSliceIdx int32) int32 {
	pCurLayer := pCtx.pCurDqLayer
	uSlcBuffIdx := 0
	pStartSlice := &pCurLayer.sSliceBufferInfo[uSlcBuffIdx].pSliceBuffer[iStartSliceIdx]
	iNalIdxInLayer := *pNalIdxInLayer
	iSliceIdx := iStartSliceIdx
	kiSliceStep := int32(pCtx.iActiveThreadsNum)
	kiPartitionId := iStartSliceIdx % kiSliceStep
	iPartitionBsSize := int32(0)
	iAnyMbLeftInPartition := iEndMbIdxInPartition - iFirstMbIdxInPartition + 1
	keNalType := pCtx.eNalType
	keNalRefIdc := pCtx.eNalPriority
	kbNeedPrefix := pCtx.bNeedPrefixNalFlag
	kiSliceIdxStep := int32(pCtx.iActiveThreadsNum)
	iReturn := int32(ENC_RETURN_SUCCESS)

	pStartSlice.sSliceHeaderExt.sSliceHeader.iFirstMbInSlice = iFirstMbIdxInPartition

	for iAnyMbLeftInPartition > 0 {
		iSliceSize := int32(0)
		iPayloadSize := int32(0)
		var pCurSlice *SSlice

		if iSliceIdx >= (pCurLayer.sSliceBufferInfo[uSlcBuffIdx].iMaxSliceNum -
			kiSliceIdxStep) { // insufficient memory in pSliceInLayer[]
			if pCtx.iActiveThreadsNum == 1 {
				//only single thread support re-alloc now
				if DynSliceRealloc(pCtx, pFrameBSInfo, pLayerBsInfo) != 0 {
					common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
						"CWelsH264SVCEncoder::WelsCodeOnePicPartition: DynSliceRealloc not successful")
					return ENC_RETURN_MEMALLOCERR
				}
			} else if iSliceIdx >= pCurLayer.iMaxSliceNum {
				common.WelsLog(&pCtx.sLogCtx, api.WELS_LOG_ERROR,
					"CWelsH264SVCEncoder::WelsCodeOnePicPartition: iSliceIdx(%d) over iMaxSliceNum(%d)", iSliceIdx,
					pCurLayer.iMaxSliceNum)
				return ENC_RETURN_MEMALLOCERR
			}
		}

		if kbNeedPrefix {
			iReturn = AddPrefixNal(pCtx, pLayerBsInfo, pLayerBsInfo.PNalLengthInByte, &iNalIdxInLayer, keNalType, keNalRefIdc,
				&iPayloadSize)
			if iReturn != ENC_RETURN_SUCCESS {
				return iReturn
			}
			iPartitionBsSize += iPayloadSize
		}

		WelsLoadNal(pCtx.pOut, int32(keNalType), int32(keNalRefIdc))
		pCurSlice = &pCtx.pCurDqLayer.sSliceBufferInfo[uSlcBuffIdx].pSliceBuffer[iSliceIdx]
		pCurSlice.iSliceIdx = iSliceIdx

		iReturn = WelsCodeOneSlice(pCtx, pCurSlice, int32(keNalType))
		if iReturn != ENC_RETURN_SUCCESS {
			return iReturn
		}
		WelsUnloadNal(pCtx.pOut)

		iReturn = WelsEncodeNal(&pCtx.pOut.sNalList[pCtx.pOut.iNalIndex-1],
			&pCtx.pCurDqLayer.sLayerInfo.sNalHeaderExt,
			pCtx.iFrameBsSize-pCtx.iPosBsBuffer,
			pCtx.pFrameBs[pCtx.iPosBsBuffer:],
			&pLayerBsInfo.PNalLengthInByte[iNalIdxInLayer])
		if iReturn != ENC_RETURN_SUCCESS {
			return iReturn
		}
		iSliceSize = pLayerBsInfo.PNalLengthInByte[iNalIdxInLayer]

		pCtx.iPosBsBuffer += iSliceSize
		iPartitionBsSize += iSliceSize

		iNalIdxInLayer++
		iSliceIdx += kiSliceStep //if iSliceIdx is not continuous
		iAnyMbLeftInPartition = iEndMbIdxInPartition - pCurLayer.LastCodedMbIdxOfPartition[kiPartitionId]
	}

	*pLayerSize = iPartitionBsSize
	*pNalIdxInLayer = iNalIdxInLayer

	// slice based packing???
	pLayerBsInfo.UiLayerType = uint8(api.VIDEO_CODING_LAYER)
	pLayerBsInfo.UiSpatialId = pCtx.uiDependencyId
	pLayerBsInfo.UiTemporalId = pCtx.uiTemporalId
	pLayerBsInfo.UiQualityId = 0
	pLayerBsInfo.INalCount = iNalIdxInLayer
	return ENC_RETURN_SUCCESS
}
