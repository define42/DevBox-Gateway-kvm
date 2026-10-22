//go:build amd64

package common

import "unsafe"

// The RefineHalfpel* entries run the AVX2 kernels that McHorVer02_avx2,
// McHorVer20_avx2 and McHorVer22_avx2 select for motion-refinement shapes. The
// caller must have validated the complete source spans, including the six-tap
// halo, and the destination spans, and must ensure they do not overlap.

// RefineHalfpelVer is McHorVer02 for width 4, 8 or 16 with positive strides
// and dstStride >= width. src points at the first output position.
func RefineHalfpelVer(dst *byte, dstStride int, src *byte, srcStride, width, height int) {
	mcVerReuseAVX2(dst, dstStride, src, srcStride, width, height)
}

// RefineHalfpelHor is McHorVer20 for width 5, 9 or 17.
func RefineHalfpelHor(dst *byte, dstStride int, src *byte, srcStride, width, height int) {
	mcHorRefineAVX2(dst, dstStride, src, srcStride, width, height)
}

// RefineHalfpelCen is McHorVer22 for width 5, 9 or 17 and height at most 17.
func RefineHalfpelCen(dst *byte, dstStride int, src *byte, srcStride, width, height int) {
	const tapStride = 24
	var taps [17 * tapStride]int16
	mcVerRawReuseAVX2(&taps[0], tapStride, (*byte)(unsafe.Add(unsafe.Pointer(src), -2)), srcStride, width+5, height)
	mcHorRawRefineAVX2(dst, dstStride, &taps[0], tapStride, width, height)
}

// PixelAvgPtr stores the rounded average of two width×height blocks, as
// PixelAvg_sse2 does, on spans the caller validated.
func PixelAvgPtr(dst *byte, dstStride int, a *byte, aStride int, b *byte, bStride int, width, height int) {
	pixelAvgSSE2(unsafe.Slice(dst, (height-1)*dstStride+width), dstStride,
		unsafe.Slice(a, (height-1)*aStride+width), aStride,
		unsafe.Slice(b, (height-1)*bStride+width), bStride, width, height)
}
