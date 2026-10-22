//go:build amd64

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

func initSampleSadTwoSIMD(samples *SSampleDealingFunc, flags uint32) {
	samples.bSampleSadTwo = flags&common.WELS_CPU_SSE2 != 0
}

//go:noescape
func sadTwoSSE2(enc *byte, encStride int, first, second *byte, refStride, width, height int) (firstCost, secondCost int32)

// The existing checked SAD wrappers reject negative strides. Returning false
// preserves their original panic order for malformed inputs, including slice
// lengths shorter than capacity. Zero and overlapping rows remain valid.
func initialSadBlockValid(buf []byte, off int, stride int32, width, height int) bool {
	span := int64(height-1)*int64(stride) + int64(width)
	return stride >= 0 && off >= 0 && int64(off) <= int64(len(buf))-span
}

// meInitialSadTwo has no indirect calls: all pointers passed to assembly stay
// tracked and noescape, including a caller's stack-local motion state. It is
// only called for one already-clipped, distinct MVC and the unmodified built-in
// SAD table capability.
func meInitialSadTwo(me *SWelsME, initial, mvc SMVUnitXY, initialOff int,
	encStride, refStride int32) (best SMVUnitXY, bestOff int, bestCost int32, ok bool) {
	width, height := 8, 8
	switch me.uiBlockSize {
	case BLOCK_16x16:
		width, height = 16, 16
	case BLOCK_16x8:
		width, height = 16, 8
	case BLOCK_8x16:
		height = 16
	case BLOCK_8x8:
	case BLOCK_8x4:
		height = 4
	default:
		return SMVUnitXY{}, 0, 0, false
	}
	// Preserve the legacy int32 multiplication/addition before conversion to int.
	mvcOff := me.iRefMbOff + int(int32(mvc.iMvY)*refStride+int32(mvc.iMvX))
	if !initialSadBlockValid(me.pEncMb, me.iEncMbOff, encStride, width, height) ||
		!initialSadBlockValid(me.pRefMb, initialOff, refStride, width, height) ||
		!initialSadBlockValid(me.pRefMb, mvcOff, refStride, width, height) {
		return SMVUnitXY{}, 0, 0, false
	}
	first, second := sadTwoSSE2(&me.pEncMb[me.iEncMbOff], int(encStride),
		&me.pRefMb[initialOff], &me.pRefMb[mvcOff], int(refStride), width, height)
	// Read MVD entries in the same order as the two original candidates. All
	// pixel reads above are validated and read-only, so table panics retain the
	// original observable state. A tie always retains the initial predictor.
	first += COST_MVD(me.pMvdCost, me.iMvdCostOff,
		int32(initial.iMvX)*4-int32(me.sMvp.iMvX), int32(initial.iMvY)*4-int32(me.sMvp.iMvY))
	second += COST_MVD(me.pMvdCost, me.iMvdCostOff,
		int32(mvc.iMvX)*4-int32(me.sMvp.iMvX), int32(mvc.iMvY)*4-int32(me.sMvp.iMvY))
	if second < first {
		return mvc, mvcOff, second, true
	}
	return initial, initialOff, first, true
}
