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
	changed     int // false entries in macroblocks
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

	// WelsMdInterMbLoop stamps each finished macroblock with codingStamp when
	// its final coded MV makes it a zero-motion P_SKIP, else with zero. Copy
	// elision stamps residentStamps when it leaves a block unwritten. A
	// successful frame publishes codingStamp as finalStamp, so capturing the
	// proof needs no pass over the macroblocks. Stamps written by other frames
	// never equal the current one; the counter clears both arrays on wrap.
	zeroSkipStamps     []uint32
	residentStamps     []uint32
	stampCounter       uint32
	codingStamp        uint32
	finalStamp         uint32
	finalZeroSkipCount int
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
	s.macroblocks, s.changed = nil, 0
	s.reuseDestination = nil
	s.codingStamp = 0
	if source == nil || !unchangedConfiguration(ctx.pSvcParam) {
		return
	}
	s.beginStamps(ctx)
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
		for _, unchanged := range s.macroblocks {
			if !unchanged {
				s.changed++
			}
		}
		s.reuseDestination = s.reusableSource()
	}
}

// skipCandidate reports whether index may be coded as a zero-motion skip,
// given a valid reference: an unchanged block whose reference block was itself
// a zero-motion skip. sMvList is heuristic metadata, not
// necessarily the final coded MV; copy elision separately requires proof.
func (s *unchangedFrameState) skipCandidate(reference *SPicture, index int) bool {
	return s.macroblocks[index] && reference.uiRefMbType[index] == common.MB_TYPE_SKIP &&
		reference.sMvList[index] == (SMVUnitXY{})
}

// beginStamps sizes the stamp arrays on the calling goroutine, before any
// slice worker can record into them.
func (s *unchangedFrameState) beginStamps(ctx *sWelsEncCtx) {
	if len(ctx.ppDqLayerList) == 0 || ctx.ppDqLayerList[0] == nil {
		return
	}
	layer := ctx.ppDqLayerList[0]
	count := int(layer.iMbWidth) * int(layer.iMbHeight)
	s.stampCounter++
	if s.stampCounter == 0 || len(s.zeroSkipStamps) != count {
		s.zeroSkipStamps, s.residentStamps = make([]uint32, count), make([]uint32, count)
		s.finalZeroSkipCount, s.stampCounter = 0, 1
	}
	s.codingStamp = s.stampCounter
}

// stampsFor returns where WelsMdInterMbLoop records this picture's finished
// macroblocks, or nil when it records nothing.
func (s *unchangedFrameState) stampsFor(layer *SDqLayer) []uint32 {
	if s.codingStamp == 0 || len(s.zeroSkipStamps) < int(layer.iMbWidth)*int(layer.iMbHeight) {
		return nil
	}

	return s.zeroSkipStamps
}

// resident reports whether index already holds its zero-motion skip
// reconstruction: the recycled destination keeps the generation the previous
// frame predicted from, and that frame coded the block as a zero-motion skip.
func (s *unchangedFrameState) resident(destination *SPicture, index int) bool {
	return s.reuseDestination != nil && destination == s.reuseDestination &&
		index < s.finalZeroSkipCount && s.zeroSkipStamps[index] == s.finalStamp
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
	if index < 0 || index >= len(state.macroblocks) || !state.macroblocks[index] ||
		!unchangedReferenceValid(ctx, mb) {
		return false
	}
	reference := ctx.pCurDqLayer.pRefPic
	// Unchanged blocks keep the existing hint policy, a previous P_SKIP with a
	// zero motion-search vector, so their output matches unhinted encoding.
	// First P frames after IDR still take the full path.
	return reference.iPictureType == common.P_SLICE && state.skipCandidate(reference, index)
}

// unchangedReferenceValid reports whether mb's reference is the picture the
// frame's hints describe, coded under the configuration they assume.
func unchangedReferenceValid(ctx *sWelsEncCtx, mb *SMB) bool {
	state := &ctx.unchanged
	layer := ctx.pCurDqLayer
	reference := layer.pRefPic
	validReference := ctx.eSliceType == common.P_SLICE && ctx.iNumRef0 == 1 &&
		reference != nil && reference == state.reference && reference == ctx.pRefList0[0] &&
		reference.iFrameNum == state.frameNum && reference.iFramePoc == state.poc

	return validReference && layer.iLoopFilterDisableIdc == 1 && int32(mb.uiLumaQp) == state.qp
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
	if state.resident(layer.pDecPic, index) {
		if index < len(state.residentStamps) {
			state.residentStamps[index] = state.codingStamp
		}
	} else {
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

// unchangedRunsEnabled reports whether WelsMdInterMbLoop may code runs of
// early-skipped macroblocks with encodeUnchangedRun; it is constant for the
// picture. It hoists the picture-wide half of unchangedMacroblockEligible.
// unchangedConfiguration also fixes the per-macroblock steps a run replaces
// with their known effects: RC_OFF_MODE selects WelsRcMbInitDisable, whose QP
// is the clipped global QP without adaptive quantization, and the no-op
// WelsRcMbInfoUpdateDisable; CAVLC writes a skip as a run count only; and both
// background update functions perform WelsMdUpdateBGDInfo.
func unchangedRunsEnabled(ctx *sWelsEncCtx) bool {
	state := &ctx.unchanged
	layer := ctx.pCurDqLayer
	if state.macroblocks == nil || layer == nil || !unchangedConfiguration(ctx.pSvcParam) {
		return false
	}
	if mode := layer.sSliceEncCtx.uiSliceMode; mode != api.SM_SINGLE_SLICE && mode != api.SM_FIXEDSLCNUM_SLICE {
		return false
	}
	reference := layer.pRefPic
	validReference := ctx.eSliceType == common.P_SLICE && ctx.iNumRef0 == 1 &&
		reference != nil && reference == state.reference && reference == ctx.pRefList0[0] &&
		reference.iFrameNum == state.frameNum && reference.iFramePoc == state.poc

	return validReference && layer.iLoopFilterDisableIdc == 1 && reference.iPictureType == common.P_SLICE &&
		ctx.pRefPic != nil && common.WELS_CLIP3(ctx.iGlobalQp, 0, 51) == state.qp &&
		len(state.macroblocks) == int(layer.iMbWidth)*int(layer.iMbHeight)
}

// encodeUnchangedRun codes the run of macroblocks from index that
// tryUnchangedMacroblockEarly would skip, at most limit, and leaves the same
// macroblock, picture, slice and cache state as the per-macroblock path of
// WelsMdInterMbLoop. It requires unchangedRunsEnabled. It returns the run
// length and the slice's next macroblock, or -1 after the slice's last one.
func encodeUnchangedRun(ctx *sWelsEncCtx, md *SWelsMD, slice *SSlice, index, limit int32) (int32, int32) {
	state := &ctx.unchanged
	layer := ctx.pCurDqLayer
	reference, decoded := layer.pRefPic, layer.pDecPic
	blocks := layer.sMbDataP
	width := int32(layer.iMbWidth)
	total := width * int32(layer.iMbHeight)
	// Skips take the slice's last QP (WelsMdInterUpdatePskip, WelsSpatialWriteMbSyn).
	qp := slice.uiLastMbQp
	chromaQp := chromaQpOf(layer, qp)
	// WelsMdUpdateBGDInfo keeps the reference's QP for a zero-motion block
	// without residual unless the encoder's reference picture is intra.
	intraReference := ctx.pRefPic.iPictureType == common.I_SLICE
	stamps := state.stampsFor(layer)
	csY, csUV := layer.iCsStride[0], layer.iCsStride[1]
	refY, refUV := reference.iLineSize[0], reference.iLineSize[1]

	// Find the run first. It ends where the slice's next macroblock is not the
	// adjacent one, so every per-macroblock array below covers one range; the
	// next call continues from there exactly as the per-block path would.
	count, next := int32(0), index
	for count < limit && next >= 0 && next == index+count && next < total {
		if !state.skipCandidate(reference, int(next)) {
			break
		}
		// After the run's first block, the previous block is either the left
		// neighbor, now a zero-motion skip, or the end of the row above, which
		// leaves no left neighbor. Both make the skip predictor zero.
		if count == 0 && !unchangedSkipPredictorIsZero(&blocks[next], width) {
			break
		}
		count++
		next = WelsGetNextMbOfSlice(layer, next)
	}
	if count == 0 {
		return 0, index
	}
	end := index + count
	last := end - 1

	// encodeUnchangedMacroblock, with the pointers of initIntraPicturePointers
	// and initInterPicturePointers. Residency reads the zero-skip stamps that
	// the run overwrites below.
	reusable := state.reuseDestination != nil && decoded == state.reuseDestination
	for i := index; i < end; i++ {
		mb := &blocks[i]
		if reusable && int(i) < state.finalZeroSkipCount && state.zeroSkipStamps[i] == state.finalStamp {
			if int(i) < len(state.residentStamps) {
				state.residentStamps[i] = state.codingStamp
			}
		} else {
			x, y := int32(mb.iMbX), int32(mb.iMbY)
			copyReconstructed16x16(layer.pCsData[0], layer.iCsDataOff[0]+int((x+y*csY)<<4), csY,
				reference.pData[0], reference.iDataOff[0]+int((x+y*refY)<<4), refY)
			for plane := 1; plane < 3; plane++ {
				copyReconstructed8x8(layer.pCsData[plane], layer.iCsDataOff[plane]+int((x+y*csUV)<<3), layer.iCsStride[plane],
					reference.pData[plane], reference.iDataOff[plane]+int((x+y*refUV)<<3), reference.iLineSize[plane])
			}
		}
		mb.uiMbType, mb.uiCbp = common.MB_TYPE_SKIP, 0
		mb.sP16x16Mv = SMVUnitXY{}
		mb.uiLumaQp, mb.uiChromaQp = qp, chromaQp
	}
	// InitMbInfo gives consecutive macroblocks adjacent entries, so each
	// block's slice extends over the rest of the run.
	first := &blocks[index]
	clear(first.pRefIndex[:count*common.MB_BLOCK8x8_NUM])
	clear(first.sMv[:count*common.MB_BLOCK4x4_NUM])
	clear(first.pNonZeroCount[:count*MB_LUMA_CHROMA_BLOCK4x4_NUM])
	clear(decoded.sMvList[index:end])
	// WelsMdInterSaveSadAndRefMbType and WelsMdUpdateBGDInfo.
	copy(decoded.pMbSkipSad[index:end], reference.pMbSkipSad[index:end])
	fillUnchanged(decoded.uiRefMbType[index:end], common.MB_TYPE_SKIP)
	if intraReference {
		fillUnchanged(decoded.pRefMbQp[index:end], qp)
	} else {
		copy(decoded.pRefMbQp[index:end], reference.pRefMbQp[index:end])
	}
	// WelsSpatialWriteMbSyn and the zero-skip stamp of WelsMdInterMbLoop.
	slice.iMbSkipRun += count
	if stamps != nil {
		fillUnchanged(stamps[index:end], state.codingStamp)
	}
	index = next

	// Leave the slice cache and mode decision as the last skip would have;
	// the next macroblock advances these picture pointers incrementally.
	mb := &blocks[last]
	x, y := int32(mb.iMbX), int32(mb.iMbY)
	cache := &slice.sMbCacheInfo
	data := &cache.SPicData
	encY, encUV := layer.iEncStride[0], layer.iEncStride[1]
	decY, decUV := decoded.iLineSize[0], decoded.iLineSize[1]
	for plane := range 3 {
		shift, enc, cs, dec, ref := int32(4), encY, csY, decY, refY
		if plane > 0 {
			shift, enc, cs, dec, ref = 3, encUV, csUV, decUV, refUV
		}
		data.pEncMb[plane], data.iEncMbOff[plane] = layer.pEncData[plane], layer.iEncDataOff[plane]+int((x+y*enc)<<shift)
		data.pCsMb[plane], data.iCsMbOff[plane] = layer.pCsData[plane], layer.iCsDataOff[plane]+int((x+y*cs)<<shift)
		data.pDecMb[plane], data.iDecMbOff[plane] = decoded.pData[plane], decoded.iDataOff[plane]+int((x+y*dec)<<shift)
		data.pRefMb[plane], data.iRefMbOff[plane] = reference.pData[plane], reference.iDataOff[plane]+int((x+y*ref)<<shift)
	}
	cache.pEncSad, cache.iEncSadOff = decoded.pMbSkipSad, int(last)
	cache.uiRefMbType = reference.uiRefMbType[last]
	cache.bCollocatedPredFlag = true
	UpdateNonZeroCountCache(mb, cache)
	md.iCostLuma = *mb.pSadCost
	md.iCostSkipMb = reference.pMbSkipSad[last]

	return count, index
}

func fillUnchanged[T any](s []T, value T) {
	for i := range s {
		s[i] = value
	}
}

// unchangedPreferCallerThread reports whether so few macroblocks fall outside
// unchanged runs that coding the slices on the calling goroutine costs less
// than waking slice workers. Only hinted blocks over zero-motion skips can join
// a run, so count the others, stopping once they exceed the limit.
func unchangedPreferCallerThread(ctx *sWelsEncCtx) bool {
	state := &ctx.unchanged
	limit := len(state.macroblocks) / 32
	if state.changed > limit || !unchangedRunsEnabled(ctx) {
		return false
	}
	reference := ctx.pCurDqLayer.pRefPic
	full := 0
	for index := range state.macroblocks {
		if !state.skipCandidate(reference, index) {
			full++
			if full > limit {
				return false
			}
		}
	}

	return true
}
