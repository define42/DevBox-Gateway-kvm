package encoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// reconstructedPictureVersion records the last successful write to a recycled
// reconstruction allocation. Pixel identity includes origins and strides;
// reference-management metadata such as POC deliberately does not participate.
type reconstructedPictureVersion struct {
	picture    *SPicture
	generation uint64
	planes     [3]*byte
	lengths    [3]int
	offsets    [3]int
	strides    [3]int32
}

func pictureVersion(picture *SPicture, generation uint64) reconstructedPictureVersion {
	version := reconstructedPictureVersion{picture: picture, generation: generation}
	if picture == nil {
		return version
	}
	version.offsets, version.strides = picture.iDataOff, picture.iLineSize
	for plane, data := range picture.pData {
		version.lengths[plane] = len(data)
		if len(data) != 0 {
			version.planes[plane] = &data[0]
		}
	}
	return version
}

func (s *unchangedFrameState) reusableSource() *SPicture {
	if s.source.picture == nil || s.source.generation == 0 ||
		s.source != pictureVersion(s.source.picture, s.source.generation) {
		return nil
	}
	for _, version := range s.pictures {
		if version == s.source {
			return version.picture
		}
	}
	return nil
}

func (s *unchangedFrameState) finishReconstruction(
	ctx *sWelsEncCtx, reference *SPicture, frameType api.EVideoFrameType,
) {
	// A previous zero-motion skip is identical to the picture used to predict
	// it. If that source allocation remains at the same generation, a matching
	// block in the next destination already has the required reconstruction.
	// Each MB is written once, and the guarded configuration disables filtering.
	var source reconstructedPictureVersion
	if frameType == api.VideoFrameTypeP && ctx.pCurDqLayer.pRefPic == s.reference {
		for _, version := range s.pictures {
			if version.picture == s.reference && version == pictureVersion(s.reference, version.generation) {
				source = version
				break
			}
		}
	}
	if frameType == api.VideoFrameTypeIDR || s.width != ctx.pSvcParam.IPicWidth ||
		s.height != ctx.pSvcParam.IPicHeight || s.qp != ctx.pSvcParam.SSpatialLayers[0].IDLayerQp {
		s.pictures = [2]reconstructedPictureVersion{}
		source = reconstructedPictureVersion{}
	}
	s.generation++
	if s.generation == 0 {
		s.pictures = [2]reconstructedPictureVersion{}
		source = reconstructedPictureVersion{}
		s.generation = 1
	}
	// Only one short-term reference is allowed, so there are two allocations.
	// Replace the destination's previous version, retaining its source version.
	index := 0
	if s.pictures[0].picture == source.picture && source.picture != nil {
		index = 1
	}
	s.pictures[index] = pictureVersion(reference, s.generation)
	s.source = source
	s.captureFinalSkipMotion(ctx.pCurDqLayer, source.picture != nil)
}

// captureFinalSkipMotion publishes this frame's zero-skip stamps. It is called
// only after a successful frame and after every slice worker joined. A
// macroblock coded outside WelsMdInterMbLoop keeps an older stamp, which only
// forgoes reuse.
func (s *unchangedFrameState) captureFinalSkipMotion(layer *SDqLayer, validSource bool) {
	count := int(layer.iMbWidth) * int(layer.iMbHeight)
	if !validSource || s.codingStamp == 0 || count < 1 || count > len(s.zeroSkipStamps) {
		s.finalZeroSkipCount = 0
		return
	}
	s.finalStamp, s.finalZeroSkipCount = s.codingStamp, count
}

// recordFinalSkipMotion stamps a finished macroblock. sMvList is an
// integer-search heuristic: a final refined 16x16 mode can become P_SKIP
// without updating it, so only the final coded MV proves that a previous skip
// reproduced the colocated source block.
func recordFinalSkipMotion(stamps []uint32, stamp uint32, mb *SMB) {
	if mb.uiMbType == common.MB_TYPE_SKIP && len(mb.sMv) != 0 && mb.sMv[0] == (SMVUnitXY{}) {
		stamps[mb.iMbXY] = stamp
	} else {
		stamps[mb.iMbXY] = 0
	}
}

// expandChangedBorder pads only the border macroblocks this frame wrote. The
// reused destination still holds the complete, already padded write that copy
// elision relies on, and a resident block's edge samples are unchanged, so its
// padding is current. The union of per-macroblock padding over the border is
// the whole-picture expansion. It reports false when the caller must pad the
// whole picture.
func (s *unchangedFrameState) expandChangedBorder(ctx *sWelsEncCtx, picture *SPicture) bool {
	layer := ctx.pCurDqLayer
	if s.reuseDestination == nil || picture != s.reuseDestination || layer == nil || layer.pDecPic != picture ||
		s.codingStamp == 0 {
		return false
	}
	width, height := int32(layer.iMbWidth), int32(layer.iMbHeight)
	if width < 2 || height < 2 || int(width)*int(height) > len(s.residentStamps) ||
		picture.iWidthInPixel != width*16 || picture.iHeightInPixel != height*16 {
		return false
	}
	pad := func(x, y int32) {
		if s.residentStamps[y*width+x] == s.codingStamp {
			return
		}
		common.PadMBLuma_c(picture.pData[0], picture.iDataOff[0], picture.iLineSize[0],
			picture.iWidthInPixel, picture.iHeightInPixel, x, y, width, height)
		for plane := 1; plane < 3; plane++ {
			common.PadMBChroma_c(picture.pData[plane], picture.iDataOff[plane], picture.iLineSize[plane],
				picture.iWidthInPixel/2, picture.iHeightInPixel/2, x, y, width, height)
		}
	}
	for x := range width {
		pad(x, 0)
		pad(x, height-1)
	}
	for y := int32(1); y < height-1; y++ {
		pad(0, y)
		pad(width-1, y)
	}

	return true
}
