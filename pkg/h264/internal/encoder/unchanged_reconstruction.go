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

func (s *unchangedFrameState) captureFinalSkipMotion(layer *SDqLayer, validSource bool) {
	count := int(layer.iMbWidth) * int(layer.iMbHeight)
	if !validSource || count < 1 || count > len(layer.sMbDataP) {
		s.finalZeroSkips = s.finalZeroSkips[:0]
		return
	}
	// Called only after a successful frame and after every slice worker joined.
	// Restrict the capture to this frame's complete macroblock grid.
	blocks := layer.sMbDataP[:count]
	if cap(s.finalZeroSkips) < len(blocks) {
		s.finalZeroSkips = make([]bool, len(blocks))
	} else {
		s.finalZeroSkips = s.finalZeroSkips[:len(blocks)]
	}
	for i := range blocks {
		mb := &blocks[i]
		// sMvList is an integer-search heuristic. A final refined 16x16 mode
		// can become P_SKIP without updating it, so only the final coded MV
		// proves that a previous skip reproduced the colocated source block.
		s.finalZeroSkips[i] = mb.uiMbType == common.MB_TYPE_SKIP &&
			len(mb.sMv) != 0 && mb.sMv[0] == (SMVUnitXY{})
	}
}
