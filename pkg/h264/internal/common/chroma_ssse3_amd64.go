//go:build amd64

package common

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func mcChroma8SSSE3(src *byte, srcStride int, dst *byte, dstStride, height int, weights uint32)

func initMcChromaSSSE3(funcs *SMcFunc, flags uint32) {
	if flags&WELS_CPU_SSSE3 != 0 && cpu.X86.HasSSSE3 {
		funcs.PMcChromaFunc = McChroma_ssse3
	}
}

// McChroma_ssse3 uses paired byte multiply-adds for width eight. Keep integer
// copies and narrower interpolation in this dispatcher so they do not pay for
// an extra call through the SSE2 wrapper. Span and alias checks match that wrapper.
func McChroma_ssse3(src []byte, srcOff int, srcStride int32, dst []byte, dstOff int, dstStride int32, mvX, mvY int16, width, height int32) {
	if width <= 0 || height <= 0 {
		return
	}
	if mvX&7 == 0 && mvY&7 == 0 {
		if height == 16 && (width == 8 || width == 16) {
			mcCopySSE2(src, srcOff, srcStride, dst, dstOff, dstStride, width, height)
		} else {
			McCopy_c(src, srcOff, srcStride, dst, dstOff, dstStride, width, height)
		}
		return
	}
	if width != 2 && width != 4 && width != 8 {
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
	if width == 8 && cpu.X86.HasSSSE3 {
		weights := uint32(w[0]) | uint32(w[1])<<8 | uint32(w[2])<<16 | uint32(w[3])<<24
		mcChroma8SSSE3(&src[srcOff], int(srcStride), &dst[dstOff], int(dstStride), int(height), weights)
		return
	}
	weights := uint64(w[0]) | uint64(w[1])<<16 | uint64(w[2])<<32 | uint64(w[3])<<48
	mcChromaSSE2(src[srcOff:], int(srcStride), dst[dstOff:], int(dstStride), int(width), int(height), weights)
}
