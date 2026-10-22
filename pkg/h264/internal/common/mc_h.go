package common

// Port of codec/common/inc/mc.h.
//
// All pixel pointers are (slice, offset) pairs: the slice is the whole
// underlying buffer and the offset is where the C pointer points.

// PWelsMcFunc: luma / chroma motion compensation. pSrc has already been
// offset by the integer part of the mv; iMvX/iMvY carry the fractional part
// in their low bits.
type PWelsMcFunc func(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iMvX, iMvY int16, iWidth, iHeight int32)

// PWelsLumaHalfpelMcFunc: half-pel luma interpolation (hor / ver / center).
type PWelsLumaHalfpelMcFunc func(pSrc []uint8, iSrcOff int, iSrcStride int32, pDst []uint8, iDstOff int, iDstStride int32,
	iWidth, iHeight int32)

// PWelsSampleAveragingFunc: (pDst, iDstStride, pSrcA, iSrcAStride, pSrcB,
// iSrcBStride, iWidth, iHeight).
type PWelsSampleAveragingFunc func(pDst []uint8, iDstOff int, iDstStride int32, pSrcA []uint8, iSrcAOff int, iSrcAStride int32,
	pSrcB []uint8, iSrcBOff int, iSrcBStride int32, iWidth, iHeight int32)

// SMcFunc mirrors TagMcFunc.
type SMcFunc struct {
	PfLumaHalfpelHor  PWelsLumaHalfpelMcFunc
	PfLumaHalfpelVer  PWelsLumaHalfpelMcFunc
	PfLumaHalfpelCen  PWelsLumaHalfpelMcFunc
	PMcChromaFunc     PWelsMcFunc
	PMcLumaFunc       PWelsMcFunc
	PfSampleAveraging PWelsSampleAveragingFunc

	// BRefineAVX2 records that the half-pixel functions are the AVX2 ones,
	// whose refinement shapes the RefineHalfpel* entries reproduce.
	BRefineAVX2 bool
}
