//go:build amd64

package common

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func mcHorVer20AVX2(dst []byte, dstStride int, src []byte, srcStride, width, height int)

//go:noescape
func mcHorVer02AVX2(dst []byte, dstStride int, src []byte, srcStride, width, height int)

//go:noescape
func mcVerticalRawAVX2(dst []int16, dstStride int, src []byte, srcStride, width, height int)

//go:noescape
func mcHorizontalRawToU8AVX2(dst []byte, dstStride int, src []int16, srcStride, width, height int)

func initMcAVX2(funcs *SMcFunc, flags uint32) {
	if flags&WELS_CPU_AVX2 == 0 || !cpu.X86.HasAVX2 {
		return
	}
	funcs.PfLumaHalfpelHor = McHorVer20_avx2
	funcs.PfLumaHalfpelVer = McHorVer02_avx2
	funcs.PfLumaHalfpelCen = McHorVer22_avx2
	funcs.PMcLumaFunc = McLuma_avx2
}

// mcAVX2BlocksOverlap compares live pointers only within this expression. In
// particular, no uintptr address survives a call that could grow the Go stack.
func mcAVX2BlocksOverlap(a, b []byte) bool {
	return uintptr(unsafe.Pointer(unsafe.SliceData(a))) < uintptr(unsafe.Pointer(unsafe.SliceData(b)))+uintptr(len(b)) &&
		uintptr(unsafe.Pointer(unsafe.SliceData(b))) < uintptr(unsafe.Pointer(unsafe.SliceData(a)))+uintptr(len(a))
}

// McHorVer20_avx2 uses packed horizontal tap pairs, with exact-width AVX2
// paths for the extra column needed by 4-, 8- and 16-wide refinement blocks.
func McHorVer20_avx2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int,
	iDstStride, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	if iSrcStride < 0 || iDstStride < 0 {
		McHorVer20_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		return
	}
	if !cpu.X86.HasAVX2 {
		McHorVer20_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		return
	}
	width, height, stride := int(iWidth), int(iHeight), int(iSrcStride)
	dst := simdBlockSlice(pDst, iDstOff, iDstStride, width, height)
	srcStart, srcEnd := iSrcOff-2, iSrcOff+(height-1)*stride+width+3
	checkSIMDSpan(pSrc, int64(srcStart), int64(srcEnd)-1)
	src := pSrc[srcStart:srcEnd]
	// Preserve the narrower SSE2 read/write order for in-place callers.
	if mcAVX2BlocksOverlap(src, dst) {
		mcHorVer20SSE2(dst, int(iDstStride), src[2:], stride, width, height)
		return
	}
	if width == 5 || width == 9 || width == 17 {
		mcHorRefineAVX2(&dst[0], int(iDstStride), &src[2], stride, width, height)
		return
	}
	if width < 16 {
		mcHorVer20SSE2(dst, int(iDstStride), src[2:], stride, width, height)
		return
	}
	mcHorVer20AVX2(dst, int(iDstStride), src[2:], stride, width, height)
}

// McHorVer02_avx2 applies the vertical six-tap filter with exactly the same
// two-row upper and three-row lower halo as the scalar and SSE2 versions.
func McHorVer02_avx2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int,
	iDstStride, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	if iSrcStride < 0 || iDstStride < 0 {
		McHorVer02_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		return
	}
	if !cpu.X86.HasAVX2 {
		McHorVer02_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		return
	}
	width, height, stride := int(iWidth), int(iHeight), int(iSrcStride)
	dst := simdBlockSlice(pDst, iDstOff, iDstStride, width, height)
	srcStart, srcEnd := iSrcOff-2*stride, iSrcOff+(height+2)*stride+width
	checkSIMDSpan(pSrc, int64(srcStart), int64(srcEnd)-1)
	src := pSrc[srcStart:srcEnd]
	if mcAVX2BlocksOverlap(src, dst) {
		mcHorVer02SSE2(dst, int(iDstStride), src[2*stride:], stride, width, height)
		return
	}
	// The row-reuse kernel writes wide blocks in column groups. Preserve the
	// original ordering when destination rows overlap, including zero stride.
	if stride > 0 && int(iDstStride) >= width {
		switch width {
		case 4, 5, 8, 9, 16, 17:
			mcVerReuseAVX2(&dst[0], int(iDstStride), &src[2*stride], stride, width, height)
			return
		}
	}
	if width < 16 {
		mcHorVer02SSE2(dst, int(iDstStride), src[2*stride:], stride, width, height)
		return
	}
	mcHorVer02AVX2(dst, int(iDstStride), src[2*stride:], stride, width, height)
}

// McHorVer22_avx2 preserves signed first-pass samples and widens them to
// 32-bit lanes before the second filter. Clipping occurs only at the end.
func McHorVer22_avx2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int,
	iDstStride, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	if iSrcStride < 0 || iDstStride < 0 {
		McHorVer22_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		return
	}
	if iWidth > 17 || iHeight > 17 || !cpu.X86.HasAVX2 {
		McHorVer22_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		return
	}
	width, height, stride := int(iWidth), int(iHeight), int(iSrcStride)
	dst := simdBlockSlice(pDst, iDstOff, iDstStride, width, height)
	srcStart, srcEnd := iSrcOff-2*stride-2, iSrcOff+(height+2)*stride+width+3
	checkSIMDSpan(pSrc, int64(srcStart), int64(srcEnd)-1)
	src := pSrc[srcStart:srcEnd]
	if width == 4 || width == 5 || width == 8 || width == 9 || width == 16 || width == 17 {
		// The combined leaf owns its compact scratch, writes every tap before
		// reading it, and completes all source reads before destination stores.
		mcCenterRefineAVX2(&dst[0], int(iDstStride), &src[2*stride], stride, width, height)
		return
	}
	mcCenterLegacyAVX2(dst, int(iDstStride), src, stride, width, height)
}

// Keep the legacy scratch frame off the refinement path. This helper retains
// the previous kernels and ordering for other positive center-filter widths.
func mcCenterLegacyAVX2(dst []byte, dstStride int, src []byte, stride, width, height int) {
	const tapStride = 24
	var taps [17 * tapStride]int16
	switch width {
	case 4, 5, 8, 9, 16, 17:
		// Scratch rows are disjoint and contain the exact width+5 first-pass
		// samples. Row reuse therefore also applies to the center filter.
		mcVerRawReuseAVX2(&taps[0], tapStride, &src[2*stride], stride, width+5, height)
	default:
		if width < 8 {
			mcVerticalRawSSE2(taps[:], tapStride, src[2*stride:], stride, width+5, height)
		} else {
			mcVerticalRawAVX2(taps[:], tapStride, src[2*stride:], stride, width+5, height)
		}
	}
	if width < 8 {
		mcHorizontalRawToU8SSE2(dst, dstStride, taps[:], tapStride, width, height)
	} else {
		mcHorizontalRawToU8AVX2(dst, dstStride, taps[:], tapStride, width, height)
	}
}

// McLuma_avx2 composes quarter-pel samples from the AVX2 half-pel filters and
// the existing exact-width SSE2 average. Small blocks retain the SSE2 path.
func McLuma_avx2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iMvX, iMvY int16, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	if iSrcStride < 0 || iDstStride < 0 {
		McLuma_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iMvX, iMvY, iWidth, iHeight)
		return
	}
	if iWidth < 8 || !cpu.X86.HasAVX2 {
		McLuma_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iMvX, iMvY, iWidth, iHeight)
		return
	}
	x, y := iMvX&3, iMvY&3
	switch x<<2 | y {
	case 0:
		if iHeight == 16 && (iWidth == 8 || iWidth == 16) {
			mcCopySSE2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		} else {
			McCopy_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		}
	case 8:
		McHorVer20_avx2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	case 2:
		McHorVer02_avx2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	case 10:
		McHorVer22_avx2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	default:
		mcLumaQuarterpelAVX2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, x, y, iWidth, iHeight)
	}
}

func mcLumaQuarterpelAVX2(pSrc []byte, iSrcOff int, iSrcStride int32,
	pDst []byte, iDstOff int, iDstStride int32, x, y int16, iWidth, iHeight int32) {
	var a, b [16 * 16]byte
	const stride = 16
	xOff, yOff := 0, 0
	if x == 3 {
		xOff = 1
	}
	if y == 3 {
		yOff = int(iSrcStride)
	}
	switch {
	case y == 0:
		McHorVer20_avx2(pSrc, iSrcOff, iSrcStride, a[:], 0, stride, iWidth, iHeight)
		PixelAvg_sse2(pDst, iDstOff, iDstStride, a[:], 0, stride,
			pSrc, iSrcOff+xOff, iSrcStride, iWidth, iHeight)
		return
	case x == 0:
		McHorVer02_avx2(pSrc, iSrcOff, iSrcStride, a[:], 0, stride, iWidth, iHeight)
		PixelAvg_sse2(pDst, iDstOff, iDstStride, a[:], 0, stride,
			pSrc, iSrcOff+yOff, iSrcStride, iWidth, iHeight)
		return
	case y == 2:
		McHorVer22_avx2(pSrc, iSrcOff, iSrcStride, a[:], 0, stride, iWidth, iHeight)
		McHorVer02_avx2(pSrc, iSrcOff+xOff, iSrcStride, b[:], 0, stride, iWidth, iHeight)
	case x == 2:
		McHorVer22_avx2(pSrc, iSrcOff, iSrcStride, a[:], 0, stride, iWidth, iHeight)
		McHorVer20_avx2(pSrc, iSrcOff+yOff, iSrcStride, b[:], 0, stride, iWidth, iHeight)
	default:
		McHorVer20_avx2(pSrc, iSrcOff+yOff, iSrcStride, a[:], 0, stride, iWidth, iHeight)
		McHorVer02_avx2(pSrc, iSrcOff+xOff, iSrcStride, b[:], 0, stride, iWidth, iHeight)
	}
	PixelAvg_sse2(pDst, iDstOff, iDstStride, a[:], 0, stride, b[:], 0, stride, iWidth, iHeight)
}
