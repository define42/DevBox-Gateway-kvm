package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// unchangedFrameState is deliberately outside the translated codec's normal
// policy. Only the opt-in, fixed-QP single-reference RDP configuration can use
// it. The reference identity includes POC/frame number because picture buffers
// are recycled. A failed/skipped call invalidates the chain; mask storage is
// never retained after EncodeFrame returns.
type unchangedFrameState struct {
	macroblocks []bool
	reference   *SPicture
	frameNum    int32
	poc         int32
	qp          int32
	width       int32
	height      int32
}

func unchangedConfiguration(param *SWelsSvcCodingParam) bool {
	if param == nil {
		return false
	}
	ordinarySingleReference := param.IUsageType == api.CAMERA_VIDEO_REAL_TIME &&
		param.ISpatialLayerNum == 1 && param.ITemporalLayerNum == 1 && param.INumRefFrame == 1
	fixedIndependentCoding := param.IRCMode == api.RC_OFF_MODE && param.IComplexityMode == api.LOW_COMPLEXITY &&
		param.IEntropyCodingModeFlag == 0 && param.ILoopFilterDisableIdc == 1
	noSourceOrReferencePolicy := !param.BEnableDenoise && !param.BEnableAdaptiveQuant &&
		!param.BEnableBackgroundDetection && !param.BEnableLongTermReference && !param.BSimulcastAVC
	mode := param.SSpatialLayers[0].SSliceArgument.UiSliceMode
	fixedSlices := mode == api.SM_SINGLE_SLICE || mode == api.SM_FIXEDSLCNUM_SLICE

	return ordinarySingleReference && fixedIndependentCoding && noSourceOrReferencePolicy && fixedSlices
}

func (s *unchangedFrameState) begin(ctx *sWelsEncCtx, source *api.SSourcePicture) {
	s.macroblocks = nil
	if source == nil || !unchangedConfiguration(ctx.pSvcParam) {
		return
	}
	param := ctx.pSvcParam
	matchingGeometry := source.IPicWidth == param.IPicWidth && source.IPicHeight == param.IPicHeight &&
		source.IPicWidth == param.sDependencyLayers[0].iActualWidth &&
		source.IPicHeight == param.sDependencyLayers[0].iActualHeight
	if !matchingGeometry || source.IPicWidth < 16 || source.IPicHeight < 16 {
		return
	}
	count := ((int64(source.IPicWidth) + 15) / 16) * ((int64(source.IPicHeight) + 15) / 16)
	validPrevious := s.reference != nil && s.width == source.IPicWidth && s.height == source.IPicHeight &&
		s.qp == param.SSpatialLayers[0].IDLayerQp
	if validPrevious && int64(len(source.UnchangedMacroblocks)) == count {
		s.macroblocks = source.UnchangedMacroblocks
	}
}

func (s *unchangedFrameState) finish(
	ctx *sWelsEncCtx,
	source *api.SSourcePicture,
	output *api.SFrameBSInfo,
	success bool,
) {
	*s = unchangedFrameState{}
	if !success || source == nil || output == nil || !unchangedConfiguration(ctx.pSvcParam) {
		return
	}
	encodedPicture := output.IFrameSizeInBytes > 0 &&
		(output.EFrameType == api.VideoFrameTypeP || output.EFrameType == api.VideoFrameTypeIDR)
	if !encodedPicture || ctx.iEncoderError != ENC_RETURN_SUCCESS || ctx.pCurDqLayer == nil {
		return
	}
	// ctx.pDecPic already points at the NEXT free buffer after UpdateRefList;
	// the layer retains the reconstructed picture that was just published.
	reference := ctx.pCurDqLayer.pDecPic
	if reference == nil || !reference.bUsedAsRef || reference.bIsLongRef {
		return
	}
	s.reference = reference
	s.frameNum, s.poc = reference.iFrameNum, reference.iFramePoc
	s.qp = ctx.pSvcParam.SSpatialLayers[0].IDLayerQp
	s.width, s.height = source.IPicWidth, source.IPicHeight
}

func tryUnchangedMacroblock(ctx *sWelsEncCtx, md *SWelsMD, slice *SSlice, mb *SMB) bool {
	state := &ctx.unchanged
	index := int(mb.iMbXY)
	if index < 0 || index >= len(state.macroblocks) || !state.macroblocks[index] {
		return false
	}
	layer := ctx.pCurDqLayer
	reference := layer.pRefPic
	validReference := ctx.eSliceType == common.P_SLICE && ctx.iNumRef0 == 1 &&
		reference != nil && reference == state.reference && reference == ctx.pRefList0[0] &&
		reference.iFrameNum == state.frameNum && reference.iFramePoc == state.poc
	if !validReference || layer.iLoopFilterDisableIdc != 1 || int32(mb.uiLumaQp) != state.qp {
		return false
	}
	// Reuse only a previously accepted zero-motion skip. With unchanged input,
	// a fixed quantizer, and no deblocking, its reconstructed block is unchanged.
	// First P frames after IDR still take the full path.
	previousSkip := reference.iPictureType == common.P_SLICE &&
		reference.uiRefMbType[index] == common.MB_TYPE_SKIP && reference.sMvList[index] == (SMVUnitXY{})
	if !previousSkip {
		return false
	}
	cache := &slice.sMbCacheInfo
	var predicted SMVUnitXY
	PredSkipMv(cache, &predicted)
	if predicted != (SMVUnitXY{}) {
		return false
	}

	// Copy reconstructed reference samples, never source pixels. The decoder
	// infers this same zero-motion prediction from P_SKIP. Copying directly to
	// reconstruction avoids motion-compensation scratch and a second copy.
	// Keep row widths literal so these tiny copies compile to inline loads and
	// stores instead of invoking the general-purpose memmove for every row.
	data := &cache.SPicData
	source, target := data.iRefMbOff[0], data.iCsMbOff[0]
	for range 16 {
		copy(data.pCsMb[0][target:target+16], data.pRefMb[0][source:source+16])
		source += int(reference.iLineSize[0])
		target += int(layer.iCsStride[0])
	}
	for plane := 1; plane < 3; plane++ {
		source, target := data.iRefMbOff[plane], data.iCsMbOff[plane]
		for range 8 {
			copy(data.pCsMb[plane][target:target+8], data.pRefMb[plane][source:source+8])
			source += int(reference.iLineSize[plane])
			target += int(layer.iCsStride[plane])
		}
	}
	mb.uiMbType, mb.uiCbp = common.MB_TYPE_SKIP, 0
	setRefIndexZero(mb)
	ctx.pFuncList.pfUpdateMbMv(mb.sMv, predicted)
	mb.sP16x16Mv = predicted
	layer.pDecPic.sMvList[index] = predicted
	clear(mb.pNonZeroCount[:24])
	// Init leaves this MB's previous luma decision cost intact. Preserve that
	// heuristic metadata; reconstruction correctness does not depend on its value.
	md.iCostLuma = *mb.pSadCost
	md.iCostSkipMb = reference.pMbSkipSad[index]
	WelsMdInterUpdatePskip(layer, slice, mb, cache)

	return true
}
