package avc

import (
	"image"
	"runtime"
	"sync"
)

// convertBGRXI420Region reads borrowed BGRX pixels synchronously. Each worker
// owns complete 2x2 chroma blocks, and all workers finish before the caller may
// release or modify the source. region uses aligned output coordinates.
func (e *Encoder) convertBGRXI420Region(
	pix []byte, stride, sourceWidth, sourceHeight int, region image.Rectangle,
) bool {
	if e.threads <= 1 ||
		region.Dx()*region.Dy() < 2*i420MinPixelsPerWorker {
		return scaleBGRXI420Region(e.i420, e.width, e.height, pix, stride, sourceWidth, sourceHeight, region)
	}
	workers := i420ConversionWorkers(e.threads, runtime.GOMAXPROCS(0), region)
	if workers == 1 {
		return scaleBGRXI420Region(e.i420, e.width, e.height, pix, stride, sourceWidth, sourceHeight, region)
	}

	var changed [i420MaxWorkers]bool
	var pending sync.WaitGroup
	pairs := region.Dy() / 2
	for worker := 1; worker < workers; worker++ {
		band := region
		band.Min.Y += 2 * (pairs * worker / workers)
		band.Max.Y = region.Min.Y + 2*(pairs*(worker+1)/workers)
		pending.Go(func() {
			changed[worker] = scaleBGRXI420Region(e.i420, e.width, e.height, pix, stride, sourceWidth, sourceHeight, band)
		})
	}
	band := region
	band.Max.Y = region.Min.Y + 2*(pairs/workers)
	changed[0] = scaleBGRXI420Region(e.i420, e.width, e.height, pix, stride, sourceWidth, sourceHeight, band)
	pending.Wait()

	for _, value := range changed[:workers] {
		if value {
			return true
		}
	}

	return false
}

// scaleBGRXI420Region matches scaleI420Region's full-range BT.709 arithmetic and
// nearest-neighbor scaling, reading B,G,R,X bytes directly into retained I420.
// The caller validates the source and clips/aligns region to 2x2 output blocks.
func scaleBGRXI420Region(
	dst []byte, width, height int, pix []byte, stride, sourceWidth, sourceHeight int, region image.Rectangle,
) bool {
	if region.Empty() {
		return false
	}
	if sourceWidth == width && sourceHeight == height {
		if changed, ok := scaleBGRXI420RegionNative(dst, width, height, pix, stride, region); ok {
			return changed
		}
	} else if changed, ok := scaleBGRXI420RegionScaled(
		dst, width, height, pix, stride, sourceWidth, sourceHeight, region,
	); ok {
		return changed
	}

	return scaleBGRXI420RegionScalar(dst, width, height, pix, stride, sourceWidth, sourceHeight, region)
}

func scaleBGRXI420RegionNative(
	dst []byte, width, height int, pix []byte, stride int, region image.Rectangle,
) (bool, bool) {
	luma := width * height
	chroma := luma / 4
	changed := false
	for y := region.Min.Y; y < region.Max.Y; y += 2 {
		chromaStart := y/2*(width/2) + region.Min.X/2
		chromaEnd := chromaStart + region.Dx()/2
		x0, x1 := region.Min.X, region.Max.X
		rowChanged, ok := bgrxToI420AVX2(
			dst[y*width+x0:y*width+x1],
			dst[(y+1)*width+x0:(y+1)*width+x1],
			dst[luma+chromaStart:luma+chromaEnd],
			dst[luma+chroma+chromaStart:luma+chroma+chromaEnd],
			pix[y*stride+x0*4:y*stride+x1*4],
			pix[(y+1)*stride+x0*4:(y+1)*stride+x1*4],
		)
		if !ok {
			return false, false
		}
		changed = rowChanged || changed
	}

	return changed, true
}

func scaleBGRXI420RegionScalar(
	dst []byte, width, height int, pix []byte, stride, sourceWidth, sourceHeight int, region image.Rectangle,
) bool {
	luma := width * height
	chroma := luma / 4
	var columns [maxDimension]int
	for x := region.Min.X; x < region.Max.X; x++ {
		columns[x] = x * sourceWidth / width * 4
	}
	changed := false
	for y := region.Min.Y; y < region.Max.Y; y += 2 {
		pos := y/2*(width/2) + region.Min.X/2
		row0 := pix[(y*sourceHeight/height)*stride:]
		row1 := pix[((y+1)*sourceHeight/height)*stride:]
		luma0 := dst[y*width : (y+1)*width]
		luma1 := dst[(y+1)*width : (y+2)*width]
		for x := region.Min.X; x < region.Max.X; x += 2 {
			p00 := row0[columns[x]:][:3]
			p01 := row0[columns[x+1]:][:3]
			p10 := row1[columns[x]:][:3]
			p11 := row1[columns[x+1]:][:3]
			r00, g00, b00 := int(p00[2]), int(p00[1]), int(p00[0])
			r01, g01, b01 := int(p01[2]), int(p01[1]), int(p01[0])
			r10, g10, b10 := int(p10[2]), int(p10[1]), int(p10[0])
			r11, g11, b11 := int(p11[2]), int(p11[1]), int(p11[0])
			y00 := byte((54*r00 + 183*g00 + 18*b00) >> 8)
			y01 := byte((54*r01 + 183*g01 + 18*b01) >> 8)
			y10 := byte((54*r10 + 183*g10 + 18*b10) >> 8)
			y11 := byte((54*r11 + 183*g11 + 18*b11) >> 8)
			red := (r00 + r01 + r10 + r11) / 4
			green := (g00 + g01 + g10 + g11) / 4
			blue := (b00 + b01 + b10 + b11) / 4
			u := byte(((-29*red - 99*green + 128*blue) >> 8) + 128)
			v := byte(((128*red - 116*green - 12*blue) >> 8) + 128)
			changed = changed || luma0[x] != y00 || luma0[x+1] != y01 ||
				luma1[x] != y10 || luma1[x+1] != y11 || dst[luma+pos] != u || dst[luma+chroma+pos] != v
			luma0[x], luma0[x+1], luma1[x], luma1[x+1] = y00, y01, y10, y11
			dst[luma+pos], dst[luma+chroma+pos] = u, v
			pos++
		}
	}

	return changed
}
