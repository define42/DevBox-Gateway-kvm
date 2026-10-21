//go:build amd64

package common

import "unsafe"

//go:noescape
func mcChromaSSE2(src []byte, srcStride int, dst []byte, dstStride, width, height int, weights uint64)

// McChroma_sse2 interpolates the common chroma block widths with a single
// rounding step, matching McChroma_c. Integer motion retains the copy path.
func McChroma_sse2(src []byte, srcOff int, srcStride int32, dst []byte, dstOff int, dstStride int32, mvX, mvY int16, width, height int32) {
	if width <= 0 || height <= 0 {
		return
	}
	if (mvX&7 == 0 && mvY&7 == 0) || (width != 2 && width != 4 && width != 8) {
		McChroma_c(src, srcOff, srcStride, dst, dstOff, dstStride, mvX, mvY, width, height)
		return
	}
	// Validate against length, including the extra source column and row.
	// Checking before slicing also handles a short slice with excess capacity.
	first, last := int64(srcOff), int64(srcOff)+int64(height)*int64(srcStride)
	if last < first {
		first, last = last, first
	}
	checkSIMDSpan(src, first, last+int64(width))
	srcFirst, srcLast := first, last+int64(width)
	first, last = int64(dstOff), int64(dstOff)+int64(height-1)*int64(dstStride)
	if last < first {
		first, last = last, first
	}
	checkSIMDSpan(dst, first, last+int64(width)-1)
	// Shifted aliases depend on the scalar pixel-by-pixel access order.
	srcStart := uintptr(unsafe.Pointer(&src[srcFirst]))
	srcEnd := uintptr(unsafe.Pointer(&src[srcLast]))
	dstStart := uintptr(unsafe.Pointer(&dst[first]))
	dstEnd := uintptr(unsafe.Pointer(&dst[last+int64(width)-1]))
	if srcStart <= dstEnd && dstStart <= srcEnd {
		McChroma_c(src, srcOff, srcStride, dst, dstOff, dstStride, mvX, mvY, width, height)
		return
	}
	w := G_kuiABCD[mvY&7][mvX&7]
	weights := uint64(w[0]) | uint64(w[1])<<16 | uint64(w[2])<<32 | uint64(w[3])<<48
	mcChromaSSE2(src[srcOff:], int(srcStride), dst[dstOff:], int(dstStride), int(width), int(height), weights)
}
