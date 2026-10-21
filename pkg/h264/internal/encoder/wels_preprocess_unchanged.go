package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// VAA compares ORIGINAL source pictures, unlike mode decision, which compares
// reconstructed pictures. Track its successful source independently; a recycled
// source buffer, failed call or different VAA reference must not reuse hints.
type vaaUnchangedFrameState struct {
	reference     *SPicture
	current       *SPicture
	poc           int32
	width, height int32
	mask          []bool
	enabled       bool
}

func (s *vaaUnchangedFrameState) begin(ctx *sWelsEncCtx, source *api.SSourcePicture) {
	s.mask, s.current, s.enabled = nil, nil, false
	if source == nil || ctx == nil || !unchangedConfiguration(ctx.pSvcParam) ||
		source.IColorFormat != int32(api.VideoFormatI420) || ctx.pVpp == nil ||
		ctx.pVpp.m_sScaledPicture.pScaledInputPicture != nil {
		*s = vaaUnchangedFrameState{}
		return
	}
	param := ctx.pSvcParam
	if param.SUsedPicRect.iLeft != 0 || param.SUsedPicRect.iTop != 0 ||
		param.SUsedPicRect.iWidth != source.IPicWidth || param.SUsedPicRect.iHeight != source.IPicHeight ||
		source.IPicWidth < 16 || source.IPicHeight < 16 ||
		source.IPicWidth != param.IPicWidth || source.IPicHeight != param.IPicHeight ||
		source.IPicWidth != param.sDependencyLayers[0].iActualWidth ||
		source.IPicHeight != param.sDependencyLayers[0].iActualHeight {
		*s = vaaUnchangedFrameState{}
		return
	}
	if s.width != source.IPicWidth || s.height != source.IPicHeight {
		s.reference = nil
	}
	s.width, s.height, s.enabled = source.IPicWidth, source.IPicHeight, true
	count := ((int64(s.width) + 15) / 16) * ((int64(s.height) + 15) / 16)
	if s.reference != nil && int64(len(source.UnchangedMacroblocks)) == count {
		s.mask = source.UnchangedMacroblocks
	}
}

func (s *vaaUnchangedFrameState) maskFor(ctx *sWelsEncCtx, current, reference *SPicture) []bool {
	if !s.enabled || len(s.mask) == 0 || ctx == nil || ctx.pVpp == nil ||
		ctx.eSliceType != common.P_SLICE || !unchangedConfiguration(ctx.pSvcParam) ||
		current == nil || reference == nil || current == reference || current != s.current ||
		reference != s.reference || reference.iFramePoc != s.poc ||
		reference != ctx.pVpp.m_pLastSpatialPicture[0][0] {
		return nil
	}
	// Padding is constant (Y=0, U/V=128), so an unchanged partial edge block
	// also has equal padded source samples. Require the exact padded geometry.
	width, height := (int64(s.width)+15)&^15, (int64(s.height)+15)&^15
	if int64(current.iWidthInPixel) != width || int64(current.iHeightInPixel) != height ||
		current.iWidthInPixel != reference.iWidthInPixel || current.iHeightInPixel != reference.iHeightInPixel ||
		current.iLineSize[0] != reference.iLineSize[0] {
		return nil
	}
	return s.mask
}

func (s *vaaUnchangedFrameState) finish(ctx *sWelsEncCtx, output *api.SFrameBSInfo, success bool) {
	current, width, height, enabled := s.current, s.width, s.height, s.enabled
	*s = vaaUnchangedFrameState{}
	if !enabled || !success || ctx == nil || ctx.pVpp == nil || output == nil ||
		ctx.iEncoderError != ENC_RETURN_SUCCESS || !unchangedConfiguration(ctx.pSvcParam) ||
		output.IFrameSizeInBytes <= 0 ||
		(output.EFrameType != api.VideoFrameTypeP && output.EFrameType != api.VideoFrameTypeIDR) ||
		current == nil || current != ctx.pEncPic || current != ctx.pVpp.m_pLastSpatialPicture[0][0] {
		return
	}
	s.reference, s.poc = current, current.iFramePoc
	s.width, s.height = width, height
}
