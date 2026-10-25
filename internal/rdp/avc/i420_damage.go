package avc

import (
	"image"
	"runtime"
	"sync"
)

type i420DamageSource struct {
	pix                             []byte
	stride, width, height, redIndex int
}

// Each worker owns whole macroblock rows, including their change bits. Splitting
// only on chroma rows would let two workers clear the same bool concurrently.
func (e *Encoder) convertI420DamageRegion(source i420DamageSource, region image.Rectangle) bool {
	if region.Empty() {
		return false
	}
	first, last := region.Min.Y/16, (region.Max.Y+15)/16
	workers := min(i420ConversionWorkers(e.threads, runtime.GOMAXPROCS(0), region), last-first)
	if workers <= 1 {
		return scaleI420DamageRegion(e.i420, e.width, e.height, source, region, e.unchanged)
	}
	var changed [i420MaxWorkers]bool
	var pending sync.WaitGroup
	for worker := 1; worker < workers; worker++ {
		band := region
		band.Min.Y = (first + (last-first)*worker/workers) * 16
		if worker+1 < workers {
			band.Max.Y = (first + (last-first)*(worker+1)/workers) * 16
		}
		pending.Go(func() {
			changed[worker] = scaleI420DamageRegion(e.i420, e.width, e.height, source, band, e.unchanged)
		})
	}
	band := region
	band.Max.Y = (first + (last-first)/workers) * 16
	changed[0] = scaleI420DamageRegion(e.i420, e.width, e.height, source, band, e.unchanged)
	pending.Wait()
	for _, value := range changed[:workers] {
		if value {
			return true
		}
	}

	return false
}

func scaleI420DamageRegion(
	dst []byte, width, height int, source i420DamageSource, region image.Rectangle, unchanged []bool,
) bool {
	if source.width != width || source.height != height {
		return scaleI420DamageRegionScaled(dst, width, height, source, region, unchanged)
	}
	changed := false
	for y := region.Min.Y; y < region.Max.Y; y += 2 {
		row0 := source.pix[y*source.stride:][:width*4]
		row1 := source.pix[(y+1)*source.stride:][:width*4]
		changed = convertI420DamageRows(dst, width, height, y, row0, row1, region, unchanged, source.redIndex) || changed
	}

	return changed
}

// Gather nearest-neighbor samples before using the same row conversion as the
// native-size path. Bounded scratch is local to each worker and never retained.
func scaleI420DamageRegionScaled(
	dst []byte, width, height int, source i420DamageSource, region image.Rectangle, unchanged []bool,
) bool {
	var columns [maxDimension]int
	var pixels [2][maxDimension * 4]byte
	for x := region.Min.X; x < region.Max.X; x++ {
		columns[x] = x * source.width / width * 4
	}
	row0, row1 := pixels[0][:width*4], pixels[1][:width*4]
	changed := false
	for y := region.Min.Y; y < region.Max.Y; y += 2 {
		src0 := source.pix[(y*source.height/height)*source.stride:]
		src1 := source.pix[((y+1)*source.height/height)*source.stride:]
		for x := region.Min.X; x < region.Max.X; x++ {
			offset := columns[x]
			copy(row0[x*4:x*4+4], src0[offset:offset+4])
			copy(row1[x*4:x*4+4], src1[offset:offset+4])
		}
		changed = convertI420DamageRows(dst, width, height, y, row0, row1, region, unchanged, source.redIndex) || changed
	}

	return changed
}

func convertI420DamageRows(
	dst []byte, width, height, y int, src0, src1 []byte, region image.Rectangle, unchanged []bool, redIndex int,
) bool {
	luma, chroma := width*height, width*height/4
	y0, y1 := dst[y*width:(y+1)*width], dst[(y+1)*width:(y+2)*width]
	start := y / 2 * (width / 2)
	u, v := dst[luma+start:luma+start+width/2], dst[luma+chroma+start:luma+chroma+start+width/2]
	columns := (width + 15) / 16
	mask := unchanged[y/16*columns : (y/16+1)*columns]
	x0, x1 := region.Min.X, region.Max.X
	left := min((x0+15)&^15, x1)
	right := left + (x1-left)/32*32
	// Vector groups must begin at an output macroblock boundary. Scalar edges
	// handle partial blocks without touching any samples outside the damage.
	if right > left {
		changed, ok := rgb32ToI420Damage(
			y0[left:right], y1[left:right], u[left/2:right/2], v[left/2:right/2],
			src0[left*4:right*4], src1[left*4:right*4], mask[left/16:right/16], redIndex,
		)
		if ok {
			changed = rgb32ToI420DamageEdge(y0, y1, u, v, src0, src1, mask, x0, left, redIndex) || changed

			return rgb32ToI420DamageEdge(y0, y1, u, v, src0, src1, mask, right, x1, redIndex) || changed
		}
	}

	return rgb32ToI420DamageEdge(y0, y1, u, v, src0, src1, mask, x0, x1, redIndex)
}

// Edges retain the existing narrow SIMD kernel while stopping at each global
// macroblock boundary, so a row-wide change flag still identifies one block.
func rgb32ToI420DamageEdge(
	y0, y1, u, v, src0, src1 []byte, unchanged []bool, left, right, redIndex int,
) bool {
	changed := false
	for left < right {
		end := min((left+16)&^15, right)
		var different, ok bool
		if redIndex == 2 {
			different, ok = bgrxToI420AVX2(
				y0[left:end], y1[left:end], u[left/2:end/2], v[left/2:end/2],
				src0[left*4:end*4], src1[left*4:end*4],
			)
		} else {
			different, ok = rgbaToI420AVX2(
				y0[left:end], y1[left:end], u[left/2:end/2], v[left/2:end/2],
				src0[left*4:end*4], src1[left*4:end*4],
			)
		}
		if !ok {
			return rgb32ToI420DamageScalar(y0, y1, u, v, src0, src1, unchanged, left, right, redIndex) || changed
		}
		if different {
			unchanged[left/16] = false
			changed = true
		}
		left = end
	}

	return changed
}

func rgb32ToI420DamageScalar(
	y0, y1, u, v, src0, src1 []byte, unchanged []bool, left, right, redIndex int,
) bool {
	changed := false
	blueIndex := 2 - redIndex
	for x := left; x < right; x += 2 {
		offset := x * 4
		r00, g00, b00 := int(src0[offset+redIndex]), int(src0[offset+1]), int(src0[offset+blueIndex])
		r01, g01, b01 := int(src0[offset+4+redIndex]), int(src0[offset+5]), int(src0[offset+4+blueIndex])
		r10, g10, b10 := int(src1[offset+redIndex]), int(src1[offset+1]), int(src1[offset+blueIndex])
		r11, g11, b11 := int(src1[offset+4+redIndex]), int(src1[offset+5]), int(src1[offset+4+blueIndex])
		y00 := byte((54*r00 + 183*g00 + 18*b00) >> 8)
		y01 := byte((54*r01 + 183*g01 + 18*b01) >> 8)
		y10 := byte((54*r10 + 183*g10 + 18*b10) >> 8)
		y11 := byte((54*r11 + 183*g11 + 18*b11) >> 8)
		red, green, blue := (r00+r01+r10+r11)/4, (g00+g01+g10+g11)/4, (b00+b01+b10+b11)/4
		cb := byte(((-29*red - 99*green + 128*blue) >> 8) + 128)
		cr := byte(((128*red - 116*green - 12*blue) >> 8) + 128)
		chroma := x / 2
		if y0[x] != y00 || y0[x+1] != y01 || y1[x] != y10 || y1[x+1] != y11 || u[chroma] != cb || v[chroma] != cr {
			unchanged[x/16] = false
			changed = true
		}
		y0[x], y0[x+1], y1[x], y1[x+1] = y00, y01, y10, y11
		u[chroma], v[chroma] = cb, cr
	}

	return changed
}
