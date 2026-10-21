package encoder

import "unsafe"

type sampleSatdFunc func([]uint8, int, int32, []uint8, int, int32) int32

func sampleSatd8x4SIMD(fn sampleSatdFunc, a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return fn(a, aOff, aStride, ref, refOff, refStride) +
		fn(a, aOff+4, aStride, ref, refOff+4, refStride)
}

func sampleSatd4x8SIMD(fn sampleSatdFunc, a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return fn(a, aOff, aStride, ref, refOff, refStride) +
		fn(a, aOff+int(aStride)*4, aStride, ref, refOff+int(refStride)*4, refStride)
}

func sampleSatd16x8SIMD(fn sampleSatdFunc, a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return fn(a, aOff, aStride, ref, refOff, refStride) +
		fn(a, aOff+8, aStride, ref, refOff+8, refStride)
}

func sampleSatd8x16SIMD(fn sampleSatdFunc, a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return fn(a, aOff, aStride, ref, refOff, refStride) +
		fn(a, aOff+int(aStride)*8, aStride, ref, refOff+int(refStride)*8, refStride)
}

func sampleSatd16x16SIMD(fn sampleSatdFunc, a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	a8, ref8 := int(aStride)*8, int(refStride)*8
	return fn(a, aOff, aStride, ref, refOff, refStride) +
		fn(a, aOff+8, aStride, ref, refOff+8, refStride) +
		fn(a, aOff+a8, aStride, ref, refOff+ref8, refStride) +
		fn(a, aOff+a8+8, aStride, ref, refOff+ref8+8, refStride)
}

func WelsSampleSatd8x4_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatd8x4SIMD(WelsSampleSatd4x4_sse41, a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd4x8_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatd4x8SIMD(WelsSampleSatd4x4_sse41, a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd16x8_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatd16x8SIMD(WelsSampleSatd8x8_sse41, a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd8x16_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatd8x16SIMD(WelsSampleSatd8x8_sse41, a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd16x16_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return sampleSatd16x16SIMD(WelsSampleSatd8x8_sse41, a, aOff, aStride, ref, refOff, refStride)
}

// sampleAvgSatdFunc evaluates SATD against the rounded average of two
// predictions without materializing the intermediate prediction.
type sampleAvgSatdFunc func(enc []byte, encOff int, encStride int32,
	a []byte, aOff int, aStride int32, b []byte, bOff int, bStride int32,
	width, height int32) int32

// Keep addresses as pointers across calls: stack-backed input arrays may move
// when a helper grows the goroutine stack.
type sampleMemoryRange struct {
	start unsafe.Pointer
	span  uintptr
}

// sampleBlockMemoryRange conservatively includes row padding in its range.
// Invalid or backwards/overlapping rows use the existing refinement path.
func sampleBlockMemoryRange(buf []byte, off int, stride, width, height int32) (sampleMemoryRange, bool) {
	if off < 0 || stride < width || width <= 0 || height <= 0 {
		return sampleMemoryRange{}, false
	}
	span := int64(height-1)*int64(stride) + int64(width)
	if int64(off) > int64(len(buf)) || span > int64(len(buf))-int64(off) {
		return sampleMemoryRange{}, false
	}
	return sampleMemoryRange{start: unsafe.Pointer(&buf[off]), span: uintptr(span)}, true
}

func sampleMemoryRangesOverlap(a, b sampleMemoryRange) bool {
	aStart, bStart := uintptr(a.start), uintptr(b.start)
	return aStart < bStart+b.span && bStart < aStart+a.span
}

// Keep the original sequential averaging behavior if any output can modify
// a later input. Normal refinement buffers occupy disjoint regions of the
// same allocation, so compare the accessed spans, not whole trailing slices.
func quarterRefineBuffersDisjoint(me *SWelsME, refine *SMeRefinePointer,
	params *SQuarRefineParams, encStride, width, height int32) bool {
	best, valid := sampleBlockMemoryRange(refine.pQuarPixBest, 0, ME_REFINE_BUF_STRIDE, width, height)
	if !valid {
		return false
	}
	tmp, valid := sampleBlockMemoryRange(refine.pQuarPixTmp, 0, ME_REFINE_BUF_STRIDE, width, height)
	if !valid || sampleMemoryRangesOverlap(best, tmp) {
		return false
	}
	overlapsOutput := func(buf []byte, off int, stride int32) bool {
		input, valid := sampleBlockMemoryRange(buf, off, stride, width, height)
		return !valid || sampleMemoryRangesOverlap(input, best) || sampleMemoryRangesOverlap(input, tmp)
	}
	if overlapsOutput(me.pEncMb, me.iEncMbOff, encStride) {
		return false
	}
	for i := 0; i < 4; i++ {
		stride := params.iStrideA
		if i >= 2 {
			stride = params.iStrideB
		}
		if overlapsOutput(params.pSrcA[i], params.iSrcAOff[i], ME_REFINE_BUF_STRIDE) ||
			overlapsOutput(params.pSrcB[i], params.iSrcBOff[i], stride) {
			return false
		}
	}
	return true
}

func meRefineQuarPixelFused(funcs *SWelsFuncPtrList, me *SWelsME, refine *SMeRefinePointer,
	width, height int32, params *SQuarRefineParams, encStride int32) bool {
	samples := &funcs.sSampleDealingFuncs
	if samples.pfSampleAvgSatd == nil || samples.pfMeCost != &samples.pfSampleSatd ||
		width < 8 || me.uiBlockSize >= MAX_BLOCK_TYPE {
		return false
	}
	sizes := [MAX_BLOCK_TYPE][2]int32{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {4, 4}, {8, 4}, {4, 8}}
	if sizes[me.uiBlockSize] != [2]int32{width, height} ||
		!quarterRefineBuffersDisjoint(me, refine, params, encStride, width, height) {
		return false
	}
	modes := [4]int32{ME_QUAR_PIXEL_TOP, ME_QUAR_PIXEL_BOTTOM, ME_QUAR_PIXEL_LEFT, ME_QUAR_PIXEL_RIGHT}
	winner := -1
	for i := 0; i < 4; i++ {
		stride := params.iStrideA
		if i >= 2 {
			stride = params.iStrideB
		}
		cost := samples.pfSampleAvgSatd(me.pEncMb, me.iEncMbOff, encStride,
			params.pSrcA[i], params.iSrcAOff[i], ME_REFINE_BUF_STRIDE,
			params.pSrcB[i], params.iSrcBOff[i], stride, width, height) + params.iLms[i]
		if cost < params.iBestCost {
			params.iBestCost = cost
			params.iBestQuarPix = modes[i]
			// Preserve the original buffer swaps and tie order, but defer
			// prediction stores until the final winner is known. Losing
			// scratch pixels are not consumed by subsequent refinement.
			refine.pQuarPixBest, refine.pQuarPixTmp = refine.pQuarPixTmp, refine.pQuarPixBest
			winner = i
		}
	}
	if winner >= 0 {
		stride := params.iStrideA
		if winner >= 2 {
			stride = params.iStrideB
		}
		funcs.sMcFuncs.PfSampleAveraging(refine.pQuarPixBest, 0, ME_REFINE_BUF_STRIDE,
			params.pSrcA[winner], params.iSrcAOff[winner], ME_REFINE_BUF_STRIDE,
			params.pSrcB[winner], params.iSrcBOff[winner], stride, width, height)
	}
	return true
}
