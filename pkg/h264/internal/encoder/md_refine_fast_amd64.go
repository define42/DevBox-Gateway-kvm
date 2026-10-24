//go:build amd64

package encoder

import (
	"reflect"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Filter callbacks lack an existing batching capability. Comparing their
// entry points preserves custom callbacks without imposing a new contract.
var refineFilterEntries = [3]uintptr{
	reflect.ValueOf(common.McHorVer20_avx2).Pointer(),
	reflect.ValueOf(common.McHorVer02_avx2).Pointer(),
	reflect.ValueOf(common.McHorVer22_avx2).Pointer(),
}

// meRefineFracPixelFast performs MeRefineFracPixel with one validation of
// every span it reads or writes, then runs the AVX2 half-pixel filters and the
// SATD kernels on pointers. It requires the configuration that the checked
// path would dispatch to the same kernels: the AVX2 half-pixel functions and
// SATD, pfMeCost bound to pfSampleSatd, the averaged-SATD kernel, a block at
// least eight pixels wide, and the InitMeRefinePointer scratch layout.
//
// Main's paired half-pixel and four-candidate quarter-pixel kernels are retained.
// Quarter-pixel candidates are scored like meRefineQuarPixelFused, without
// materializing each average. That is exact because the scratch layout and the
// validated reference and source spans prove that no output overlaps an input.
//
// It reports false, having changed nothing, when any condition does not hold.
func meRefineFracPixelFast(pEncCtx *sWelsEncCtx, pMemPredInterMb []uint8, iMemPredInterMbOff int, pMe *SWelsME,
	pMeRefine *SMeRefinePointer, iWidth int32, iHeight int32) bool {
	pFunc := pEncCtx.pFuncList
	samples := &pFunc.sSampleDealingFuncs
	if pMe.uiBlockSize > BLOCK_8x8 || !samples.bWholeFractionalRefine || !samples.bSampleHalfSatdPair || !samples.bSampleAvgSatdFour ||
		samples.pfSampleAvgSatd == nil || samples.pfSampleAvgSatdValidated == nil || samples.pfMeCost != &samples.pfSampleSatd {
		return false
	}
	if reflect.ValueOf(pFunc.sMcFuncs.PfLumaHalfpelHor).Pointer() != refineFilterEntries[0] ||
		reflect.ValueOf(pFunc.sMcFuncs.PfLumaHalfpelVer).Pointer() != refineFilterEntries[1] ||
		reflect.ValueOf(pFunc.sMcFuncs.PfLumaHalfpelCen).Pointer() != refineFilterEntries[2] ||
		reflect.ValueOf(pFunc.sMcFuncs.PfSampleAveraging).Pointer() != reflect.ValueOf(common.PixelAvg_sse2).Pointer() {
		return false
	}
	w, h := meBlockDims[pMe.uiBlockSize][0], meBlockDims[pMe.uiBlockSize][1]
	if int(iWidth) != w || int(iHeight) != h {
		return false
	}

	// Scratch layout: H, V, best and tmp at offset s of four disjoint regions.
	arena, s := pMeRefine.arena, pMeRefine.arenaOff
	if len(arena) != 4*refineRegion || s < 0 || s > refineRegion-(h+1)*ME_REFINE_BUF_STRIDE ||
		len(pMeRefine.pHalfPixH) < h*ME_REFINE_BUF_STRIDE+w+1 ||
		len(pMeRefine.pHalfPixV) < h*ME_REFINE_BUF_STRIDE+w+1 ||
		len(pMeRefine.pQuarPixBest) < (h-1)*ME_REFINE_BUF_STRIDE+w ||
		len(pMeRefine.pQuarPixTmp) < (h-1)*ME_REFINE_BUF_STRIDE+w {
		return false
	}
	arenaBase := unsafe.Pointer(unsafe.SliceData(arena))
	region := func(i int) unsafe.Pointer { return unsafe.Add(arenaBase, i*refineRegion+s) }
	ptrOf := func(b []byte) unsafe.Pointer { return unsafe.Pointer(unsafe.SliceData(b)) }
	best, tmp := ptrOf(pMeRefine.pQuarPixBest), ptrOf(pMeRefine.pQuarPixTmp)
	if ptrOf(pMeRefine.pHalfPixH) != region(0) || ptrOf(pMeRefine.pHalfPixV) != region(1) ||
		!(best == region(2) && tmp == region(3) || best == region(3) && tmp == region(2)) {
		return false
	}

	// Source block and reference window: the six-tap filters read rows
	// [-3, h+2] and columns [-3, w+2] around the integer-pixel block.
	encStride := int(pEncCtx.pCurDqLayer.iEncStride[0])
	refStrideI32 := pEncCtx.pCurDqLayer.pRefPic.iLineSize[0]
	refStride := int(refStrideI32)
	encOff, refOff := pMe.iEncMbOff, pMe.iRefMbOff
	if encStride < w || refStride < w+6 || encOff < 0 || encOff > len(pMe.pEncMb)-(h-1)*encStride-w {
		return false
	}
	if refOff < 3*refStride+3 || refOff > len(pMe.pRefMb)-(h+2)*refStride-w-3 {
		return false
	}
	lo, hi := refOff-3*refStride-3, refOff+(h+2)*refStride+w+3
	if lo < 0 || hi > len(pMe.pRefMb) {
		return false
	}
	outside := func(p unsafe.Pointer, n int) bool {
		return uintptr(p)+uintptr(n) <= uintptr(arenaBase) || uintptr(p) >= uintptr(arenaBase)+4*refineRegion
	}
	encBase := unsafe.Add(ptrOf(pMe.pEncMb), encOff)
	refBase := unsafe.Add(ptrOf(pMe.pRefMb), lo)
	if !outside(encBase, (h-1)*encStride+w) || !outside(refBase, hi-lo) {
		return false
	}

	// MVD costs: half- and quarter-pixel steps stay within ±3 of the
	// integer-pixel vector.
	iMvx, iMvy := int32(pMe.sMv.iMvX), int32(pMe.sMv.iMvY)
	// Legacy half-pixel updates use int16. Avoid crossing their wrap boundary.
	if iMvx < -32765 || iMvx > 32764 || iMvy < -32765 || iMvy > 32764 {
		return false
	}
	iMvpX, iMvpY := int32(pMe.sMvp.iMvX), int32(pMe.sMvp.iMvY)
	costOff := pMe.iMvdCostOff
	if costOff < 0 || costOff >= len(pMe.pMvdCost) {
		return false
	}
	dLo := min(iMvx-3-iMvpX, iMvy-3-iMvpY)
	dHi := max(iMvx+3-iMvpX, iMvy+3-iMvpY)
	if costOff < -int(dLo) || costOff > len(pMe.pMvdCost)-1-int(dHi) {
		return false
	}
	costBase := unsafe.Add(unsafe.Pointer(unsafe.SliceData(pMe.pMvdCost)), 2*costOff)
	cost := func(dx, dy int32) int32 {
		return int32(*(*uint16)(unsafe.Add(costBase, 2*int(dx)))) + int32(*(*uint16)(unsafe.Add(costBase, 2*int(dy))))
	}

	enc := (*byte)(encBase)
	ref := unsafe.Add(ptrOf(pMe.pRefMb), refOff)
	refAt := func(dx, dy int) *byte { return (*byte)(unsafe.Add(ref, dy*refStride+dx)) }
	halfH, halfV := region(0), region(1)
	at := func(p unsafe.Pointer, off int) *byte { return (*byte)(unsafe.Add(p, off)) }
	satd := func(p *byte, stride int) int32 {
		return int32(satdBlockAVX2(enc, encStride, p, stride, w, h))
	}

	var iBestCost int32
	if pEncCtx.pCurDqLayer.bSatdInMdFlag {
		iBestCost = int32(pMe.uSadPredISatd + uint32(cost(iMvx-iMvpX, iMvy-iMvpY)))
	} else {
		iBestCost = satd(refAt(0, 0), refStride) + cost(iMvx-iMvpX, iMvy-iMvpY)
	}
	iBestHalfPix := int32(REFINE_ME_NO_BEST_HALF_PIXEL)
	pBestPredInter, iBestPredInterOff := pMe.pRefMb, refOff
	iInterBlk4Stride := int32(ME_REFINE_BUF_STRIDE)

	// Vertical half-pixel rows -1..h-1 into V; (0,-2) and (0,2).
	common.RefineHalfpelVer((*byte)(halfV), ME_REFINE_BUF_STRIDE, refAt(0, -1), refStride, w, h+1)
	first, second := satdTwoAVX2(enc, encStride, at(halfV, 0), at(halfV, ME_REFINE_BUF_STRIDE), ME_REFINE_BUF_STRIDE, w, h)
	if c := first + cost(iMvx-iMvpX, iMvy-2-iMvpY); c < iBestCost {
		iBestCost, iBestHalfPix = c, REFINE_ME_HALF_PIXEL_TOP
		pBestPredInter, iBestPredInterOff = pMeRefine.pHalfPixV, 0
	}
	if c := second + cost(iMvx-iMvpX, iMvy+2-iMvpY); c < iBestCost {
		iBestCost, iBestHalfPix = c, REFINE_ME_HALF_PIXEL_BOTTOM
		pBestPredInter, iBestPredInterOff = pMeRefine.pHalfPixV, ME_REFINE_BUF_STRIDE
	}
	// Horizontal half-pixel columns -1..w-1 into H; (-2,0) and (2,0).
	common.RefineHalfpelHor((*byte)(halfH), ME_REFINE_BUF_STRIDE, refAt(-1, 0), refStride, w+1, h)
	first, second = satdTwoAVX2(enc, encStride, at(halfH, 0), at(halfH, 1), ME_REFINE_BUF_STRIDE, w, h)
	if c := first + cost(iMvx-2-iMvpX, iMvy-iMvpY); c < iBestCost {
		iBestCost, iBestHalfPix = c, REFINE_ME_HALF_PIXEL_LEFT
		pBestPredInter, iBestPredInterOff = pMeRefine.pHalfPixH, 0
	}
	if c := second + cost(iMvx+2-iMvpX, iMvy-iMvpY); c < iBestCost {
		iBestCost, iBestHalfPix = c, REFINE_ME_HALF_PIXEL_RIGHT
		pBestPredInter, iBestPredInterOff = pMeRefine.pHalfPixH, 1
	}

	// Quarter-pixel candidates: the average of a (stride 32 scratch) and b.
	iHalfMvx, iHalfMvy := iMvx, iMvy
	var a [4]*byte
	var b [4]*byte
	var strideB [4]int
	cen := func(p unsafe.Pointer) {
		common.RefineHalfpelCen((*byte)(p), ME_REFINE_BUF_STRIDE, refAt(-1, -1), refStride, w+1, h+1)
	}
	switch iBestHalfPix {
	case REFINE_ME_NO_BEST_HALF_PIXEL:
		a = [4]*byte{at(halfV, 0), at(halfV, ME_REFINE_BUF_STRIDE), at(halfH, 0), at(halfH, 1)}
		b = [4]*byte{refAt(0, 0), refAt(0, 0), refAt(0, 0), refAt(0, 0)}
		strideB = [4]int{refStride, refStride, refStride, refStride}
	case REFINE_ME_HALF_PIXEL_LEFT, REFINE_ME_HALF_PIXEL_RIGHT:
		pMeRefine.pHalfPixHV = pMeRefine.pHalfPixV // reuse the V buffer, only H and HV are needed
		cen(halfV)
		x := 0
		if iBestHalfPix == REFINE_ME_HALF_PIXEL_LEFT {
			iHalfMvx -= 2
		} else {
			iHalfMvx += 2
			x = 1
		}
		a = [4]*byte{at(halfH, x), at(halfH, x), at(halfH, x), at(halfH, x)}
		b = [4]*byte{at(halfV, x), at(halfV, x+ME_REFINE_BUF_STRIDE), refAt(x-1, 0), refAt(x, 0)}
		strideB = [4]int{ME_REFINE_BUF_STRIDE, ME_REFINE_BUF_STRIDE, refStride, refStride}
	default: // REFINE_ME_HALF_PIXEL_TOP or REFINE_ME_HALF_PIXEL_BOTTOM
		pMeRefine.pHalfPixHV = pMeRefine.pHalfPixH // reuse the H buffer, only V and HV are needed
		cen(halfH)
		y := 0
		if iBestHalfPix == REFINE_ME_HALF_PIXEL_TOP {
			iHalfMvy -= 2
		} else {
			iHalfMvy += 2
			y = ME_REFINE_BUF_STRIDE
		}
		dy := y / ME_REFINE_BUF_STRIDE
		a = [4]*byte{at(halfV, y), at(halfV, y), at(halfV, y), at(halfV, y)}
		b = [4]*byte{refAt(0, dy-1), refAt(0, dy), at(halfH, y), at(halfH, y+1)}
		strideB = [4]int{refStride, refStride, ME_REFINE_BUF_STRIDE, ME_REFINE_BUF_STRIDE}
	}
	lms := [4]int32{
		cost(iHalfMvx-iMvpX, iHalfMvy-1-iMvpY),
		cost(iHalfMvx-iMvpX, iHalfMvy+1-iMvpY),
		cost(iHalfMvx-1-iMvpX, iHalfMvy-iMvpY),
		cost(iHalfMvx+1-iMvpX, iHalfMvy-iMvpY),
	}
	modes := [4]int32{ME_QUAR_PIXEL_TOP, ME_QUAR_PIXEL_BOTTOM, ME_QUAR_PIXEL_LEFT, ME_QUAR_PIXEL_RIGHT}
	iBestQuarPix := int32(ME_NO_BEST_QUAR_PIXEL)
	quarCost, winner := iBestCost, -1
	var candidates [4]sampleAvgSatdCandidate
	for i := range candidates {
		candidates[i] = sampleAvgSatdCandidate{a[i], b[i], ME_REFINE_BUF_STRIDE, strideB[i]}
	}
	var costs [4]int32
	satdAvgFourAVX2(enc, encStride, &candidates, w, h, &costs)
	for i := 0; i < 4; i++ {
		c := costs[i] + lms[i]
		if c < quarCost {
			quarCost, iBestQuarPix, winner = c, modes[i], i
			pMeRefine.pQuarPixBest, pMeRefine.pQuarPixTmp = pMeRefine.pQuarPixTmp, pMeRefine.pQuarPixBest
		}
	}
	if winner >= 0 {
		// Materialize only the winning average, like meRefineQuarPixelFused.
		dst := ptrOf(pMeRefine.pQuarPixBest)
		// Eligibility proves the built-in averaging callback is installed.
		// Materialize only the winner, after scoring all candidates.
		pFunc.sMcFuncs.PfSampleAveraging(unsafe.Slice((*byte)(dst), (h-1)*ME_REFINE_BUF_STRIDE+w), 0, ME_REFINE_BUF_STRIDE,
			unsafe.Slice(a[winner], (h-1)*ME_REFINE_BUF_STRIDE+w), 0, ME_REFINE_BUF_STRIDE,
			unsafe.Slice(b[winner], (h-1)*strideB[winner]+w), 0, int32(strideB[winner]), iWidth, iHeight)
		pBestPredInter, iBestPredInterOff = pMeRefine.pQuarPixBest, 0
		iBestCost = quarCost
	}

	iMvQuarAddX := [10]int32{0, 0, -1, 1, 0, 0, 0, -1, 1, 0}
	pMe.sMv.iMvX = int16(iHalfMvx + iMvQuarAddX[iBestQuarPix])
	pMe.sMv.iMvY = int16(iHalfMvy + iMvQuarAddX[iBestQuarPix+3])
	pMe.uiSatdCost = uint32(iBestCost)
	if iBestHalfPix+iBestQuarPix == NO_BEST_FRAC_PIX {
		pBestPredInter, iBestPredInterOff = pMe.pRefMb, refOff
		iInterBlk4Stride = refStrideI32
	}
	pMeRefine.pfCopyBlockByMode(pMemPredInterMb, iMemPredInterMbOff, common.MB_WIDTH_LUMA, pBestPredInter, iBestPredInterOff,
		iInterBlk4Stride)
	return true
}
