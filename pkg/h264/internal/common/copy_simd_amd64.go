//go:build amd64

package common

import "unsafe"

// The fixed-shape kernels amortize span validation over the whole block. They
// load and store exactly the row width and accept unaligned pictures.
//
//go:noescape
func copy8x16SSE2(dst *byte, dstStride int, src *byte, srcStride int)

//go:noescape
func copy16x16SSE2(dst *byte, dstStride int, src *byte, srcStride int)

func copySIMDReady(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32, width, height int) bool {
	// Positive, disjoint rows are the encoding case. Validate each full span
	// once, then compare the two starting addresses without indexing row ends.
	if dstStride >= int32(width) && srcStride >= int32(width) {
		dstSpan := (height-1)*int(dstStride) + width
		srcSpan := (height-1)*int(srcStride) + width
		checkSIMDSpan(dst, int64(dstOff), int64(dstOff)+int64(dstSpan)-1)
		checkSIMDSpan(src, int64(srcOff), int64(srcOff)+int64(srcSpan)-1)
		d := uintptr(unsafe.Pointer(&dst[dstOff]))
		s := uintptr(unsafe.Pointer(&src[srcOff]))
		return s-d >= uintptr(dstSpan) && d-s >= uintptr(srcSpan)
	}
	// Scalar routines preserve the row order for negative, zero and
	// overlapping strides, including writes visible to later source rows.
	return false
}

func WelsCopy8x16_sse2(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	if !copySIMDReady(dst, dstOff, dstStride, src, srcOff, srcStride, 8, 16) {
		WelsCopy8x16_c(dst, dstOff, dstStride, src, srcOff, srcStride)
		return
	}
	copy8x16SSE2(&dst[dstOff], int(dstStride), &src[srcOff], int(srcStride))
}

func WelsCopy16x16_sse2(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	if !copySIMDReady(dst, dstOff, dstStride, src, srcOff, srcStride, 16, 16) {
		WelsCopy16x16_c(dst, dstOff, dstStride, src, srcOff, srcStride)
		return
	}
	copy16x16SSE2(&dst[dstOff], int(dstStride), &src[srcOff], int(srcStride))
}

// mcCopySSE2 accelerates sixteen-row integer motion without changing McCopy_c's
// fallback order. Callers route shorter blocks directly to the scalar function:
// their copies are too small to amortize a separate checked assembly call.
func mcCopySSE2(src []byte, srcOff int, srcStride int32, dst []byte, dstOff int, dstStride int32, width, height int32) {
	if (width != 8 && width != 16) || height != 16 ||
		!copySIMDReady(dst, dstOff, dstStride, src, srcOff, srcStride, int(width), int(height)) {
		McCopy_c(src, srcOff, srcStride, dst, dstOff, dstStride, width, height)
		return
	}
	if width == 8 {
		copy8x16SSE2(&dst[dstOff], int(dstStride), &src[srcOff], int(srcStride))
	} else {
		copy16x16SSE2(&dst[dstOff], int(dstStride), &src[srcOff], int(srcStride))
	}
}
