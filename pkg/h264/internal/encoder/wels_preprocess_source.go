package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"

// Original pictures rotate independently of reconstructed references. A source
// buffer usually contains frame N-2, so copying only frame N's damage would lose
// changes made in N-1. Each resident buffer owns the union of damage since it was
// last refreshed. Caller-provided masks are consumed synchronously, never saved.
type sourceCopyBuffer struct {
	picture *SPicture
	planes  [3]*byte
	offsets [3]int
	strides [3]int32
	dirty   []bool
	valid   bool
}

type sourceCopyState struct {
	// The supported single-temporal-layer configuration allocates three original
	// pictures and rotates two of them. An unexpected fourth picture falls back.
	buffers       [3]sourceCopyBuffer
	reference     *SPicture
	current       *SPicture
	poc           int32
	width, height int32
	qp            int32
	sliceMode     api.SliceModeEnum
	slices        uint32
	partial       bool
}

// Retain mask capacities across failures and no-hint frames. Full-motion input
// must not add an allocation per frame just because its content is invalidated.
func (s *sourceCopyState) invalidate() {
	for i := range s.buffers {
		dirty := s.buffers[i].dirty
		s.buffers[i] = sourceCopyBuffer{dirty: dirty}
	}
	s.reference, s.current, s.partial = nil, nil, false
}

// finish runs after VAA's independent original-reference validation. Only a
// successfully published source can establish the next input generation.
func (s *sourceCopyState) finish(p *CWelsPreProcess, output *api.SFrameBSInfo) {
	if output == nil || s.current == nil || s.current != p.vaaUnchanged.reference {
		s.invalidate()
		return
	}
	s.reference, s.poc = s.current, p.vaaUnchanged.poc
	if output.EFrameType == api.VideoFrameTypeIDR {
		for i := range s.buffers {
			if s.buffers[i].picture != s.current {
				s.buffers[i].valid = false
			}
		}
	}
	s.current = nil
}

// tryCopySource returns true when it has copied only pending dirty runs. False
// leaves copying to WelsMoveMemory_c; the current resident record is committed
// only if the enclosing encode subsequently publishes this original picture.
func (p *CWelsPreProcess) tryCopySource(param *SWelsSvcCodingParam, dst *SPicture, src *api.SSourcePicture, width, height int32) bool {
	s := &p.sourceCopy
	v := &p.vaaUnchanged
	ctx := p.m_pEncCtx
	if ctx == nil || ctx.pVpp != p || !v.enabled || !unchangedConfiguration(param) ||
		src.IColorFormat != int32(api.VideoFormatI420) || p.m_sScaledPicture.pScaledInputPicture != nil ||
		width != src.IPicWidth || height != src.IPicHeight || width < 16 || height < 16 || width&1 != 0 || height&1 != 0 ||
		width != param.IPicWidth || height != param.IPicHeight ||
		width != param.sDependencyLayers[0].iActualWidth || height != param.sDependencyLayers[0].iActualHeight ||
		param.SUsedPicRect != (SUsedPicRect{iWidth: width, iHeight: height}) ||
		dst == nil || dst.iWidthInPixel != (width+15)&^15 || dst.iHeightInPixel != (height+15)&^15 {
		s.invalidate()
		return false
	}
	var planes [3]*byte
	var offsets [3]int
	var strides [3]int32
	for plane := range 3 {
		w, h := int64(width), int64(height)
		if plane != 0 {
			w, h = w/2, h/2
		}
		srcStride, dstStride := int64(src.IStride[plane]), int64(dst.iLineSize[plane])
		dstOff := int64(dst.iDataOff[plane])
		if srcStride < w || dstStride < w || dstOff < 0 ||
			(h-1)*srcStride+w > int64(len(src.PData[plane])) ||
			dstOff+(h-1)*dstStride+w > int64(len(dst.pData[plane])) {
			s.invalidate()
			return false
		}
		planes[plane], offsets[plane], strides[plane] = &dst.pData[plane][0], dst.iDataOff[plane], dst.iLineSize[plane]
	}

	cols, rows := int((width+15)/16), int((height+15)/16)
	count := cols * rows
	layer := &param.SSpatialLayers[0]
	mask := v.mask
	validChain := s.reference != nil && s.reference == v.reference && s.reference == p.m_pLastSpatialPicture[0][0] &&
		s.reference.iFramePoc == s.poc && s.poc == v.poc && dst != s.reference &&
		s.width == width && s.height == height && s.qp == layer.IDLayerQp &&
		s.sliceMode == layer.SSliceArgument.UiSliceMode && s.slices == layer.SSliceArgument.UiSliceNum && len(mask) == count &&
		!param.sDependencyLayers[0].bEncCurFrmAsIdrFlag && (ctx.pVaa == nil || !ctx.pVaa.bIdrPeriodFlag)
	if !validChain {
		s.invalidate()
	}
	s.width, s.height, s.qp = width, height, layer.IDLayerQp
	s.sliceMode, s.slices = layer.SSliceArgument.UiSliceMode, layer.SSliceArgument.UiSliceNum
	s.current, s.partial = dst, false

	var buffer *sourceCopyBuffer
	for i := range s.buffers {
		if s.buffers[i].picture == dst {
			buffer = &s.buffers[i]
			break
		}
	}
	if buffer == nil {
		for i := range s.buffers {
			if s.buffers[i].picture == nil {
				buffer = &s.buffers[i]
				break
			}
		}
	}
	if buffer == nil {
		// Unexpected source-buffer lifecycle: preserve correctness with a full
		// refresh and forget every older generation.
		s.invalidate()
		s.current = dst
		buffer = &s.buffers[0]
	}
	if buffer.planes != planes || buffer.offsets != offsets || buffer.strides != strides || len(buffer.dirty) != count {
		buffer.valid = false
	}
	buffer.picture, buffer.planes, buffer.offsets, buffer.strides = dst, planes, offsets, strides
	if cap(buffer.dirty) < count {
		buffer.dirty = make([]bool, count)
	} else {
		buffer.dirty = buffer.dirty[:count]
	}
	if validChain {
		for i := range s.buffers {
			resident := &s.buffers[i]
			if resident.valid {
				for mb, unchanged := range mask {
					if !unchanged {
						resident.dirty[mb] = true
					}
				}
			}
		}
	}
	if buffer.valid {
		dirtyCount := 0
		for _, dirty := range buffer.dirty {
			if dirty {
				dirtyCount++
			}
		}
		if dirtyCount < count {
			copySourceDirtyRuns(dst, src, buffer.dirty, cols, rows)
			s.partial = true
		}
	}
	clear(buffer.dirty)
	buffer.valid = true
	return s.partial
}

func copySourceDirtyRuns(dst *SPicture, src *api.SSourcePicture, dirty []bool, cols, rows int) {
	for mbY := range rows {
		row := dirty[mbY*cols : (mbY+1)*cols]
		for mbX := 0; mbX < cols; {
			if !row[mbX] {
				mbX++
				continue
			}
			start := mbX
			for mbX < cols && row[mbX] {
				mbX++
			}
			x0, x1 := start*16, min(mbX*16, int(src.IPicWidth))
			y0, y1 := mbY*16, min((mbY+1)*16, int(src.IPicHeight))
			for plane := range 3 {
				shift := 0
				if plane != 0 {
					shift = 1
				}
				left, right := x0>>shift, x1>>shift
				for y := y0 >> shift; y < y1>>shift; y++ {
					srcOff := y*int(src.IStride[plane]) + left
					dstOff := dst.iDataOff[plane] + y*int(dst.iLineSize[plane]) + left
					copy(dst.pData[plane][dstOff:dstOff+right-left], src.PData[plane][srcOff:srcOff+right-left])
				}
			}
		}
	}
}
