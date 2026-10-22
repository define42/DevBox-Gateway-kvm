//go:build amd64

package common

import "golang.org/x/sys/cpu"

// Frozen round9 checked entry. Deliberately independent of the new helper.
func mcCenterStackOriginal(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int,
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
	if width == 5 || width == 9 || width == 17 {
		mcHorRawRefineAVX2(&dst[0], int(iDstStride), &taps[0], tapStride, width, height)
	} else if width < 8 {
		mcHorizontalRawToU8SSE2(dst, int(iDstStride), taps[:], tapStride, width, height)
	} else {
		mcHorizontalRawToU8AVX2(dst, int(iDstStride), taps[:], tapStride, width, height)
	}
}
