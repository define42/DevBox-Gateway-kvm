//go:build amd64

package avc

import (
	"image"

	"golang.org/x/sys/cpu"
)

// scaleI420RegionScaled gathers nearest-neighbor samples into two packed RGBA
// rows, so resizing uses the same vector color conversion and change detection
// as native-size frames. Scratch is bounded by the encoder's maximum width and
// stays on the stack; source row padding and pixels outside region are untouched.
func scaleI420RegionScaled(dst []byte, width, height int, img *image.RGBA, region image.Rectangle) (bool, bool) {
	return scaleRGB32I420RegionScaled(dst, width, height, img.Pix, img.Stride, img.Rect.Dx(), img.Rect.Dy(), region, 0)
}

func scaleBGRXI420RegionScaled(
	dst []byte, width, height int, pix []byte, stride, sourceWidth, sourceHeight int, region image.Rectangle,
) (bool, bool) {
	return scaleRGB32I420RegionScaled(dst, width, height, pix, stride, sourceWidth, sourceHeight, region, 2)
}

func scaleRGB32I420RegionScaled(
	dst []byte, width, height int, pix []byte, stride, sourceWidth, sourceHeight int, region image.Rectangle, redIndex int,
) (bool, bool) {
	if !cpu.X86.HasAVX2 || region.Dx() < 8 {
		return false, false
	}

	var columns [maxDimension]int
	var pixels [2][maxDimension * 4]byte
	span := region.Dx()
	row0, row1 := pixels[0][:span*4], pixels[1][:span*4]
	for x := range span {
		columns[x] = (x + region.Min.X) * sourceWidth / width * 4
	}
	luma := width * height
	chroma := luma / 4
	changed := false
	for y := region.Min.Y; y < region.Max.Y; y += 2 {
		src0 := pix[(y*sourceHeight/height)*stride:]
		src1 := pix[((y+1)*sourceHeight/height)*stride:]
		for x, offset := range columns[:span] {
			// Literal four-byte copies lower to single pixel loads/stores and
			// never read past the selected pixel, including the last source row.
			copy(row0[x*4:x*4+4], src0[offset:offset+4])
			copy(row1[x*4:x*4+4], src1[offset:offset+4])
		}
		chromaStart := y/2*(width/2) + region.Min.X/2
		chromaEnd := chromaStart + span/2
		rowChanged, _ := rgb32ToI420AVX2(
			dst[y*width+region.Min.X:y*width+region.Max.X],
			dst[(y+1)*width+region.Min.X:(y+1)*width+region.Max.X],
			dst[luma+chromaStart:luma+chromaEnd],
			dst[luma+chroma+chromaStart:luma+chroma+chromaEnd],
			row0, row1, redIndex,
		)
		changed = rowChanged || changed
	}

	return changed, true
}
