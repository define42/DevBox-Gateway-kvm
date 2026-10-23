//go:build amd64

package avc

import "golang.org/x/sys/cpu"

// rgbaToI420AVX2 converts two rows of a positive even-width, unscaled RGBA span to
// I420. The destination slices must start at the corresponding luma and chroma
// positions. ok reports whether the input shape and CPU support the fast path.
func rgbaToI420AVX2(dstY0, dstY1, dstU, dstV, src0, src1 []byte) (changed, ok bool) {
	return rgb32ToI420AVX2(dstY0, dstY1, dstU, dstV, src0, src1, 0)
}

// bgrxToI420AVX2 reads guest BGRX pixels directly, without a channel-swap buffer.
func bgrxToI420AVX2(dstY0, dstY1, dstU, dstV, src0, src1 []byte) (changed, ok bool) {
	return rgb32ToI420AVX2(dstY0, dstY1, dstU, dstV, src0, src1, 2)
}

// redIndex is zero for RGBA and two for BGRX. The assembly chooses coefficients
// and channel shuffles once per row pair, keeping the pixel loop identical.
func rgb32ToI420AVX2(dstY0, dstY1, dstU, dstV, src0, src1 []byte, redIndex int) (changed, ok bool) {
	width := len(dstY0)
	if !cpu.X86.HasAVX2 || width == 0 || width > maxDimension || width&1 != 0 ||
		len(dstY1) != width || len(dstU) != width/2 || len(dstV) != width/2 ||
		len(src0) != width*4 || len(src1) != width*4 {
		return false, false
	}

	// Spans narrower than one wide block, such as single-macroblock damage,
	// keep the eight-pixel kernel below.
	if width >= 32 {
		blocks := width &^ 31
		changed = rgb32ToI420AVX2Wide(
			&dstY0[0], &dstY1[0], &dstU[0], &dstV[0],
			&src0[0], &src1[0], blocks, redIndex,
		)
		// Finish with one overlapping block. Re-converting already written
		// pixels stores identical bytes, so it cannot report a false change.
		if tail := width - 32; blocks != width {
			changed = rgb32ToI420AVX2Wide(
				&dstY0[tail], &dstY1[tail], &dstU[tail/2], &dstV[tail/2],
				&src0[tail*4], &src1[tail*4], 32, redIndex,
			) || changed
		}

		return changed, true
	}

	vectorWidth := width &^ 7
	if vectorWidth != 0 {
		changed = rgb32ToI420AVX2Kernel(
			&dstY0[0], &dstY1[0], &dstU[0], &dstV[0],
			&src0[0], &src1[0], vectorWidth, redIndex,
		)
	}

	// Keep the assembly loop branch-free by finishing its zero-to-six-pixel
	// suffix here. The arithmetic deliberately mirrors scaleI420Region.
	blueIndex := 2 - redIndex
	for x := vectorWidth; x < width; x += 2 {
		offset := x * 4
		r00, g00, b00 := int(src0[offset+redIndex]), int(src0[offset+1]), int(src0[offset+blueIndex])
		r01, g01, b01 := int(src0[offset+4+redIndex]), int(src0[offset+5]), int(src0[offset+4+blueIndex])
		r10, g10, b10 := int(src1[offset+redIndex]), int(src1[offset+1]), int(src1[offset+blueIndex])
		r11, g11, b11 := int(src1[offset+4+redIndex]), int(src1[offset+5]), int(src1[offset+4+blueIndex])

		y00 := byte((54*r00 + 183*g00 + 18*b00) >> 8)
		y01 := byte((54*r01 + 183*g01 + 18*b01) >> 8)
		y10 := byte((54*r10 + 183*g10 + 18*b10) >> 8)
		y11 := byte((54*r11 + 183*g11 + 18*b11) >> 8)
		red := (r00 + r01 + r10 + r11) / 4
		green := (g00 + g01 + g10 + g11) / 4
		blue := (b00 + b01 + b10 + b11) / 4
		u := byte(((-29*red - 99*green + 128*blue) >> 8) + 128)
		v := byte(((128*red - 116*green - 12*blue) >> 8) + 128)
		chroma := x / 2

		if dstY0[x] != y00 || dstY0[x+1] != y01 ||
			dstY1[x] != y10 || dstY1[x+1] != y11 ||
			dstU[chroma] != u || dstV[chroma] != v {
			changed = true
		}
		dstY0[x], dstY0[x+1] = y00, y01
		dstY1[x], dstY1[x+1] = y10, y11
		dstU[chroma], dstV[chroma] = u, v
	}

	return changed, true
}

//go:noescape
func rgb32ToI420AVX2Kernel(dstY0, dstY1, dstU, dstV, src0, src1 *byte, width, redIndex int) bool

// rgb32ToI420AVX2Wide converts 32 pixels per iteration; width is a multiple of 32.
//
//go:noescape
func rgb32ToI420AVX2Wide(dstY0, dstY1, dstU, dstV, src0, src1 *byte, width, redIndex int) bool
