//go:build amd64

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

//go:noescape
func intraPredFillSSE2(dst []byte, value uint8)

//go:noescape
func intraPredHorizontalSSE2(dst, left []byte, stride, size int)

//go:noescape
func intraPredPlaneSSE2(dst []byte, base, horizontal, vertical int32, size int)

//go:noescape
func intraPredChromaDCSSE2(dst []byte, top, bottom uint64)

//go:noescape
func intraSad16x16SSE2(src []byte, srcStride int, top, left []byte, refStride int, costs *[3]int32)

//go:noescape
func intraSad8x8SSE2(src []byte, srcStride int, top, left []byte, refStride int, dcTop, dcBottom uint64,
	costs *[3]int32)

func initIntraPredSIMD(f *SWelsFuncPtrList, flags uint32) {
	if flags&common.WELS_CPU_SSE2 == 0 {
		return
	}
	f.pfGetLumaI4x4Pred[common.I4_PRED_HD] = WelsI4x4LumaPredHD_sse2
	f.pfGetLumaI4x4Pred[common.I4_PRED_HU] = WelsI4x4LumaPredHU_sse2
	f.pfGetLumaI4x4Pred[common.I4_PRED_VR] = WelsI4x4LumaPredVR_sse2
	f.pfGetLumaI16x16Pred[common.I16_PRED_H] = intra16Horizontal
	f.pfGetLumaI16x16Pred[common.I16_PRED_P] = intra16Plane
	f.pfGetLumaI16x16Pred[common.I16_PRED_DC] = intra16DC
	f.pfGetLumaI16x16Pred[common.I16_PRED_DC_L] = intra16DCLeft
	f.pfGetLumaI16x16Pred[common.I16_PRED_DC_T] = intra16DCTop
	f.pfGetLumaI16x16Pred[common.I16_PRED_DC_128] = intra16DC128
	f.pfGetChromaPred[common.C_PRED_H] = intra8Horizontal
	f.pfGetChromaPred[common.C_PRED_P] = intra8Plane
	f.pfGetChromaPred[common.C_PRED_DC] = intra8DC
	f.pfGetChromaPred[common.C_PRED_DC_L] = intra8DCLeft
	f.pfGetChromaPred[common.C_PRED_DC_T] = intra8DCTop
	f.pfGetChromaPred[common.C_PRED_DC_128] = intra8DC128
}

func initIntraSadSIMD(s *SSampleDealingFunc, flags uint32) {
	if flags&common.WELS_CPU_SSE2 != 0 {
		s.pfIntra16x16Combined3Sad = intra16CombinedSad
		s.pfIntra8x8Combined3Sad = intra8CombinedSad
	}
}

func intra16Horizontal(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	checkSampleSIMDBlock(ref, refOff-1, stride, 1, 16)
	intraPredHorizontalSSE2(dst[dstOff:dstOff+256], ref[refOff-1:], int(stride), 16)
}

func intra8Horizontal(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	checkSampleSIMDBlock(ref, refOff-1, stride, 1, 8)
	intraPredHorizontalSSE2(dst[dstOff:dstOff+64], ref[refOff-1:], int(stride), 8)
}

// Plane coefficients use signed, widened arithmetic exactly as the scalar
// predictors. Every unshifted output fits int16: luma is within [-10038,19648]
// and chroma within [-8114,19016], including rounding. SSE2 therefore preserves
// the arithmetic shift before unsigned saturation.
func intraPlaneCoefficients(ref []byte, off int, stride int32, size int) (int32, int32, int32) {
	s, half := int(stride), size/2
	top, left := off-s, off-1
	var h, v int32
	for i := 0; i < half; i++ {
		h += int32(i+1) * (int32(ref[top+half+i]) - int32(ref[top+half-2-i]))
		v += int32(i+1) * (int32(ref[left+(half+i)*s]) - int32(ref[left+(half-2-i)*s]))
	}
	if size == 16 {
		h, v = (5*h+32)>>6, (5*v+32)>>6
	} else {
		h, v = (17*h+16)>>5, (17*v+16)>>5
	}
	a := (int32(ref[left+(size-1)*s]) + int32(ref[top+size-1])) << 4
	return a - int32(half-1)*(h+v) + 16, h, v
}

func intra16Plane(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	if intraPlaneReferencesFit(ref, refOff, stride, 16) {
		block := dst[dstOff : dstOff+256]
		intraPredPlane16FullSSE2(&block[0], &ref[refOff-int(stride)-1], int(stride))
		return
	}
	base, h, v := intraPlaneCoefficients(ref, refOff, stride, 16)
	intraPredPlaneSSE2(dst[dstOff:dstOff+256], base, h, v, 16)
}

func intra8Plane(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	if intraPlaneReferencesFit(ref, refOff, stride, 8) {
		block := dst[dstOff : dstOff+64]
		intraPredPlane8FullSSE2(&block[0], &ref[refOff-int(stride)-1], int(stride))
		return
	}
	base, h, v := intraPlaneCoefficients(ref, refOff, stride, 8)
	intraPredPlaneSSE2(dst[dstOff:dstOff+64], base, h, v, 8)
}

func intraLeftSum(ref []byte, off int, stride int32, size int) int32 {
	var sum int32
	for i := 0; i < size; i++ {
		sum += int32(ref[off+i*int(stride)-1])
	}
	return sum
}

func intraTopSum(ref []byte, off int, stride int32, size int) int32 {
	var sum int32
	for _, value := range ref[off-int(stride) : off-int(stride)+size] {
		sum += int32(value)
	}
	return sum
}

func intra16DC(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	mean := (intraLeftSum(ref, refOff, stride, 16) + intraTopSum(ref, refOff, stride, 16) + 16) >> 5
	intraPredFillSSE2(dst[dstOff:dstOff+256], uint8(mean))
}

func intra16DCLeft(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	mean := (intraLeftSum(ref, refOff, stride, 16) + 8) >> 4
	intraPredFillSSE2(dst[dstOff:dstOff+256], uint8(mean))
}

func intra16DCTop(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	mean := (intraTopSum(ref, refOff, stride, 16) + 8) >> 4
	intraPredFillSSE2(dst[dstOff:dstOff+256], uint8(mean))
}

func intra16DC128(dst []byte, dstOff int, _ []byte, _ int, _ int32) {
	intraPredFillSSE2(dst[dstOff:dstOff+256], 128)
}

func intraChromaDCRow(left, right int32) uint64 {
	return uint64(uint8(left))*0x01010101 | uint64(uint8(right))*0x0101010100000000
}

func intra8DC(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	t0 := intraTopSum(ref, refOff, stride, 4)
	t1 := intraTopSum(ref, refOff+4, stride, 4)
	l0 := intraLeftSum(ref, refOff, stride, 4)
	l1 := intraLeftSum(ref, refOff+4*int(stride), stride, 4)
	top := intraChromaDCRow((t0+l0+4)>>3, (t1+2)>>2)
	bottom := intraChromaDCRow((l1+2)>>2, (t1+l1+4)>>3)
	intraPredChromaDCSSE2(dst[dstOff:dstOff+64], top, bottom)
}

func intra8DCLeft(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	top := (intraLeftSum(ref, refOff, stride, 4) + 2) >> 2
	bottom := (intraLeftSum(ref, refOff+4*int(stride), stride, 4) + 2) >> 2
	intraPredChromaDCSSE2(dst[dstOff:dstOff+64], intraChromaDCRow(top, top), intraChromaDCRow(bottom, bottom))
}

func intra8DCTop(dst []byte, dstOff int, ref []byte, refOff int, stride int32) {
	left := (intraTopSum(ref, refOff, stride, 4) + 2) >> 2
	right := (intraTopSum(ref, refOff+4, stride, 4) + 2) >> 2
	row := intraChromaDCRow(left, right)
	intraPredChromaDCSSE2(dst[dstOff:dstOff+64], row, row)
}

func intra8DC128(dst []byte, dstOff int, _ []byte, _ int, _ int32) {
	intraPredFillSSE2(dst[dstOff:dstOff+64], 128)
}

// Evaluate the three non-plane candidates without writing rejected predictions.
// The caller evaluates plane next and materializes the selected predictor once.
func intra16CombinedSad(ref []byte, refOff int, refStride int32, src []byte, srcOff int, srcStride int32,
	bestMode *int32, lambda int32, _ []byte, _ int,
) int32 {
	checkSampleSIMDBlock(src, srcOff, srcStride, 16, 16)
	checkSampleSIMDBlock(ref, refOff-1, refStride, 1, 16)
	top := ref[refOff-int(refStride) : refOff-int(refStride)+16]
	var costs [3]int32
	intraSad16x16SSE2(src[srcOff:], int(srcStride), top, ref[refOff-1:], int(refStride), &costs)
	costs[1] += 2 * lambda
	costs[2] += 2 * lambda
	mode := int32(0)
	for candidate := int32(1); candidate < 3; candidate++ {
		if costs[candidate] < costs[mode] {
			mode = candidate
		}
	}
	*bestMode = mode
	return costs[mode]
}

func intra8SadCosts(ref []byte, refOff int, refStride int32, src []byte, srcOff int, srcStride int32) [3]int32 {
	checkSampleSIMDBlock(src, srcOff, srcStride, 8, 8)
	checkSampleSIMDBlock(ref, refOff-1, refStride, 1, 8)
	top := ref[refOff-int(refStride) : refOff-int(refStride)+8]
	t0 := int32(top[0]) + int32(top[1]) + int32(top[2]) + int32(top[3])
	t1 := int32(top[4]) + int32(top[5]) + int32(top[6]) + int32(top[7])
	l0 := intraLeftSum(ref, refOff, refStride, 4)
	l1 := intraLeftSum(ref, refOff+4*int(refStride), refStride, 4)
	dcTop := intraChromaDCRow((t0+l0+4)>>3, (t1+2)>>2)
	dcBottom := intraChromaDCRow((l1+2)>>2, (t1+l1+4)>>3)
	var costs [3]int32
	intraSad8x8SSE2(src[srcOff:], int(srcStride), top, ref[refOff-1:], int(refStride), dcTop, dcBottom, &costs)
	return costs
}

func intra8CombinedSad(refCb []byte, refCbOff int, refStride int32, srcCb []byte, srcCbOff int, srcStride int32,
	bestMode *int32, lambda int32, _ []byte, _ int, refCr []byte, refCrOff int, srcCr []byte, srcCrOff int,
) int32 {
	cb := intra8SadCosts(refCb, refCbOff, refStride, srcCb, srcCbOff, srcStride)
	cr := intra8SadCosts(refCr, refCrOff, refStride, srcCr, srcCrOff, srcStride)
	costs := [3]int32{cb[0] + cr[0] + 2*lambda, cb[1] + cr[1] + 2*lambda, cb[2] + cr[2]}
	mode := 0
	for candidate := 1; candidate < 3; candidate++ {
		if costs[candidate] < costs[mode] {
			mode = candidate
		}
	}
	*bestMode = int32(2 - mode) // The evaluation order is V, H, DC.
	return costs[mode]
}
