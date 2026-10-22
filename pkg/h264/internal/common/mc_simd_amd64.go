//go:build amd64

package common

// The assembly entry points use the Go slice ABI so their backing arrays stay
// live for the duration of each kernel.

//go:noescape
func filterInput8bitWithStrideSSE2(src []byte, stride int) int

//go:noescape
func pixelAvgSSE2(dst []byte, dstStride int, srcA []byte, srcAStride int,
	srcB []byte, srcBStride, width, height int)

//go:noescape
func mcHorVer20SSE2(dst []byte, dstStride int, src []byte, srcStride, width, height int)

//go:noescape
func mcHorVer02SSE2(dst []byte, dstStride int, src []byte, srcStride, width, height int)

//go:noescape
func mcVerticalRawSSE2(dst []int16, dstStride int, src []byte, srcStride, width, height int)

//go:noescape
func mcHorizontalRawToU8SSE2(dst []byte, dstStride int, src []int16, srcStride, width, height int)

// FilterInput8bitWithStride_sse2 is the bit-exact SSE2 equivalent of
// FilterInput8bitWithStride_c. The block kernels below amortize the assembly
// transition and should be preferred in hot loops.
func FilterInput8bitWithStride_sse2(pSrc []uint8, iOff int, kiOffset int32) int32 {
	first := int64(iOff) - 2*int64(kiOffset)
	last := int64(iOff) + 3*int64(kiOffset)
	if last < first {
		first, last = last, first
	}
	checkSIMDSpan(pSrc, first, last)
	return int32(filterInput8bitWithStrideSSE2(pSrc[iOff:], int(kiOffset)))
}

// PixelAvg_sse2 averages a block with H.264's rounded unsigned average. It
// supports every positive width; the codec's common widths (4, 8, and 16) are
// handled entirely with packed SSE2 operations.
func PixelAvg_sse2(pDst []uint8, iDstOff int, iDstStride int32, pSrcA []uint8, iSrcAOff int,
	iSrcAStride int32, pSrcB []uint8, iSrcBOff int, iSrcBStride int32, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	width, height := int(iWidth), int(iHeight)
	dstBlock := simdBlockSlice(pDst, iDstOff, iDstStride, width, height)
	srcABlock := simdBlockSlice(pSrcA, iSrcAOff, iSrcAStride, width, height)
	srcBBlock := simdBlockSlice(pSrcB, iSrcBOff, iSrcBStride, width, height)
	pixelAvgSSE2(dstBlock, int(iDstStride), srcABlock, int(iSrcAStride),
		srcBBlock, int(iSrcBStride), width, height)
}

func PixelAvgWidthEq16_sse2(pDst []uint8, iDstOff int, iDstStride int32, pSrcA []uint8, iSrcAOff int,
	iSrcAStride int32, pSrcB []uint8, iSrcBOff int, iSrcBStride int32, iHeight int32) {
	PixelAvg_sse2(pDst, iDstOff, iDstStride, pSrcA, iSrcAOff, iSrcAStride,
		pSrcB, iSrcBOff, iSrcBStride, 16, iHeight)
}

func PixelAvgWidthEq8_sse2(pDst []uint8, iDstOff int, iDstStride int32, pSrcA []uint8, iSrcAOff int,
	iSrcAStride int32, pSrcB []uint8, iSrcBOff int, iSrcBStride int32, iHeight int32) {
	PixelAvg_sse2(pDst, iDstOff, iDstStride, pSrcA, iSrcAOff, iSrcAStride,
		pSrcB, iSrcBOff, iSrcBStride, 8, iHeight)
}

func PixelAvgWidthEq4_sse2(pDst []uint8, iDstOff int, iDstStride int32, pSrcA []uint8, iSrcAOff int,
	iSrcAStride int32, pSrcB []uint8, iSrcBOff int, iSrcBStride int32, iHeight int32) {
	PixelAvg_sse2(pDst, iDstOff, iDstStride, pSrcA, iSrcAOff, iSrcAStride,
		pSrcB, iSrcBOff, iSrcBStride, 4, iHeight)
}

// McHorVer20_sse2 performs horizontal six-tap half-pel interpolation. The
// source offset points at the first output sample and therefore needs two
// readable pixels on the left and three on the right, matching the C kernel.
func McHorVer20_sse2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int,
	iDstStride, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	if iSrcStride < 0 {
		panic(simdBoundsPanic)
	}
	width, height := int(iWidth), int(iHeight)
	dstBlock := simdBlockSlice(pDst, iDstOff, iDstStride, width, height)
	srcStart := iSrcOff - 2
	srcEnd := iSrcOff + (height-1)*int(iSrcStride) + width + 3
	checkSIMDSpan(pSrc, int64(srcStart), int64(srcEnd)-1)
	srcBlock := pSrc[srcStart:srcEnd]
	mcHorVer20SSE2(dstBlock, int(iDstStride), srcBlock[2:], int(iSrcStride), width, height)
}

// McHorVer02_sse2 performs vertical six-tap half-pel interpolation. The source
// offset needs two readable rows above and three below, matching the C kernel.
func McHorVer02_sse2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int,
	iDstStride, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	if iSrcStride < 0 {
		panic(simdBoundsPanic)
	}
	width, height, stride := int(iWidth), int(iHeight), int(iSrcStride)
	dstBlock := simdBlockSlice(pDst, iDstOff, iDstStride, width, height)
	srcStart := iSrcOff - 2*stride
	srcEnd := iSrcOff + (height+2)*stride + width
	checkSIMDSpan(pSrc, int64(srcStart), int64(srcEnd)-1)
	srcBlock := pSrc[srcStart:srcEnd]
	mcHorVer02SSE2(dstBlock, int(iDstStride), srcBlock[2*stride:], stride, width, height)
}

// McHorVer22_sse2 performs the separable horizontal and vertical six-tap
// filters without clipping the intermediate samples. OpenH264's half-pel
// workspace contract is at most 17 by 17; larger blocks retain the scalar
// implementation.
func McHorVer22_sse2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int,
	iDstStride, iWidth, iHeight int32) {
	if iWidth <= 0 || iHeight <= 0 {
		return
	}
	if iSrcStride < 0 {
		panic(simdBoundsPanic)
	}
	if iWidth > 17 || iHeight > 17 {
		McHorVer22_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
		return
	}
	width, height, stride := int(iWidth), int(iHeight), int(iSrcStride)
	dstBlock := simdBlockSlice(pDst, iDstOff, iDstStride, width, height)
	srcStart := iSrcOff - 2*stride - 2
	srcEnd := iSrcOff + (height+2)*stride + width + 3
	checkSIMDSpan(pSrc, int64(srcStart), int64(srcEnd)-1)
	srcBlock := pSrc[srcStart:srcEnd]

	const tapStride = 24
	var taps [17 * tapStride]int16
	mcVerticalRawSSE2(taps[:], tapStride, srcBlock[2*stride:], stride, width+5, height)
	mcHorizontalRawToU8SSE2(dstBlock, int(iDstStride), taps[:], tapStride, width, height)
}

// McLuma_sse2 accelerates integer, half-pel, and quarter-pel luma positions.
// Quarter-pel positions compose the SIMD half-pel and averaging kernels.
func McLuma_sse2(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iMvX, iMvY int16, iWidth, iHeight int32) {
	switch (uint16(iMvX)&3)<<2 | (uint16(iMvY) & 3) {
	case 0:
		McCopy_c(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	case 2 << 2:
		McHorVer20_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	case 2:
		McHorVer02_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	case 2<<2 | 2:
		McHorVer22_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride, iWidth, iHeight)
	default:
		mcLumaQuarterpel_sse2(pSrc, iSrcOff, iSrcStride, pDst, iDstOff, iDstStride,
			iMvX, iMvY, iWidth, iHeight)
	}
}
