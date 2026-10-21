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

	// Picture generations describe completed reconstruction writes, independently
	// of frame numbers/POC, which the reference manager overwrites when recycling.
	generation       uint64
	pictures         [2]reconstructedPictureVersion
	source           reconstructedPictureVersion
	reuseDestination *SPicture
	finalZeroSkips   []bool // owned proof from final coded MVs, not the search heuristic
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
	s.reuseDestination = nil
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
		s.reuseDestination = s.reusableSource()
	}
}

func (s *unchangedFrameState) finish(
	ctx *sWelsEncCtx,
	source *api.SSourcePicture,
	output *api.SFrameBSInfo,
	success bool,
) {
	s.macroblocks = nil
	s.reuseDestination = nil
	if !success || source == nil || output == nil || !unchangedConfiguration(ctx.pSvcParam) {
		*s = unchangedFrameState{}
		return
	}
	encodedPicture := output.IFrameSizeInBytes > 0 &&
		(output.EFrameType == api.VideoFrameTypeP || output.EFrameType == api.VideoFrameTypeIDR)
	if !encodedPicture || ctx.iEncoderError != ENC_RETURN_SUCCESS || ctx.pCurDqLayer == nil {
		*s = unchangedFrameState{}
		return
	}
	// ctx.pDecPic already points at the NEXT free buffer after UpdateRefList;
	// the layer retains the reconstructed picture that was just published.
	reference := ctx.pCurDqLayer.pDecPic
	if reference == nil || !reference.bUsedAsRef || reference.bIsLongRef {
		*s = unchangedFrameState{}
		return
	}
	s.finishReconstruction(ctx, reference, output.EFrameType)
	s.reference = reference
	s.frameNum, s.poc = reference.iFrameNum, reference.iFramePoc
	s.qp = ctx.pSvcParam.SSpatialLayers[0].IDLayerQp
	s.width, s.height = source.IPicWidth, source.IPicHeight
}

func unchangedMacroblockEligible(ctx *sWelsEncCtx, mb *SMB) bool {
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
	// Preserve the existing hint policy: previous P_SKIP and a zero motion-search
	// vector. sMvList is heuristic metadata, not necessarily the final coded MV.
	// Copy elision separately requires final-zero-MV proof. First P frames after
	// IDR still take the full path.
	previousSkip := reference.iPictureType == common.P_SLICE &&
		reference.uiRefMbType[index] == common.MB_TYPE_SKIP && reference.sMvList[index] == (SMVUnitXY{})
	if !previousSkip {
		return false
	}
	return true
}

// The early path only proves the zero-predictor cases that need A and B. Any
// other case uses the full neighbor cache and median predictor below.
func tryUnchangedMacroblockEarly(ctx *sWelsEncCtx, md *SWelsMD, slice *SSlice, mb *SMB) bool {
	if !unchangedMacroblockEligible(ctx, mb) || !unchangedSkipPredictorIsZero(mb, int32(ctx.pCurDqLayer.iMbWidth)) {
		return false
	}
	encodeUnchangedMacroblock(ctx, md, slice, mb)
	return true
}

func unchangedSkipPredictorIsZero(mb *SMB, width int32) bool {
	available := uint32(mb.uiNeighborAvail)
	if available&LEFT_MB_POS == 0 || available&TOP_MB_POS == 0 {
		return true
	}
	left := mb.Add(-1)
	if common.IS_SVC_INTER(left.uiMbType) && left.pRefIndex[1] == 0 && left.sMv[3] == (SMVUnitXY{}) {
		return true
	}
	top := mb.Add(-width)
	return common.IS_SVC_INTER(top.uiMbType) && top.pRefIndex[2] == 0 && top.sMv[12] == (SMVUnitXY{})
}

func tryUnchangedMacroblock(ctx *sWelsEncCtx, md *SWelsMD, slice *SSlice, mb *SMB) bool {
	if !unchangedMacroblockEligible(ctx, mb) {
		return false
	}
	var predicted SMVUnitXY
	PredSkipMv(&slice.sMbCacheInfo, &predicted)
	if predicted != (SMVUnitXY{}) {
		return false
	}
	encodeUnchangedMacroblock(ctx, md, slice, mb)
	return true
}

func encodeUnchangedMacroblock(ctx *sWelsEncCtx, md *SWelsMD, slice *SSlice, mb *SMB) {
	layer := ctx.pCurDqLayer
	reference := layer.pRefPic
	index := int(mb.iMbXY)
	cache := &slice.sMbCacheInfo

	// Copy reconstructed reference samples, never source pixels. The decoder
	// infers this same zero-motion prediction from P_SKIP. Copying directly to
	// reconstruction avoids motion-compensation scratch and a second copy.
	// When the recycled destination still contains the source generation used
	// by the previous skip, its samples are already identical. Otherwise copy
	// with validated, exact-width strided loads.
	state := &ctx.unchanged
	resident := state.reuseDestination != nil && layer.pDecPic == state.reuseDestination &&
		index < len(state.finalZeroSkips) && state.finalZeroSkips[index]
	if !resident {
		data := &cache.SPicData
		copyReconstructed16x16(data.pCsMb[0], data.iCsMbOff[0], layer.iCsStride[0],
			data.pRefMb[0], data.iRefMbOff[0], reference.iLineSize[0])
		for plane := 1; plane < 3; plane++ {
			copyReconstructed8x8(data.pCsMb[plane], data.iCsMbOff[plane], layer.iCsStride[plane],
				data.pRefMb[plane], data.iRefMbOff[plane], reference.iLineSize[plane])
		}
	}

	mb.uiMbType, mb.uiCbp = common.MB_TYPE_SKIP, 0
	setRefIndexZero(mb)
	ctx.pFuncList.pfUpdateMbMv(mb.sMv, SMVUnitXY{})
	mb.sP16x16Mv = SMVUnitXY{}
	layer.pDecPic.sMvList[index] = SMVUnitXY{}
	clear(mb.pNonZeroCount[:24])
	// Init leaves this MB's previous luma decision cost intact. Preserve that
	// heuristic metadata; reconstruction correctness does not depend on its value.
	md.iCostLuma = *mb.pSadCost
	md.iCostSkipMb = reference.pMbSkipSad[index]
	WelsMdInterUpdatePskip(layer, slice, mb, cache)
}
