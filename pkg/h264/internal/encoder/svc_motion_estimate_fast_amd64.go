//go:build amd64

package encoder

import (
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// meBlockDims holds the width and height of each motion-estimation block size.
var meBlockDims = [MAX_BLOCK_TYPE][2]int{{16, 16}, {16, 8}, {8, 16}, {8, 8}, {4, 4}, {8, 4}, {4, 8}}

// welsMotionEstimateIntegerFast performs WelsMotionEstimateInitialPoint and,
// unless that stops early, WelsDiamondSearch and MeEndIntepelSearch. It
// validates once every span the search can read: integer motion vectors stay
// within [sMvStartMin-1, sMvStartMax] in each dimension (initial candidates are
// clipped to the closed range, diamond centers to the half-open range, and the
// four-candidate SAD reads one pixel around each center). The SAD kernels and
// MVD cost table are then read without further checks, and every cost,
// comparison and state update matches the checked functions.
//
// It reports false, having changed nothing, unless built-in SSE2 SAD,
// camera-content directional handling and diamond search are installed and
// the geometry validates. The capability is cleared for custom callbacks.
func welsMotionEstimateIntegerFast(pFuncList *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, iStrideEnc, iStrideRef int32) bool {
	if !pFuncList.bWholeIntegerSearch || !pFuncList.sSampleDealingFuncs.bSampleSadTwo || int(pMe.uiBlockSize) >= MAX_BLOCK_TYPE ||
		int(pSlice.uiMvcNum) > len(pSlice.sMvc) {
		return false
	}
	w, h := meBlockDims[pMe.uiBlockSize][0], meBlockDims[pMe.uiBlockSize][1]
	encStride, refStride := int(iStrideEnc), int(iStrideRef)
	mvMin, mvMax := pSlice.sMvStartMin, pSlice.sMvStartMax
	if encStride < w || refStride < w || mvMin.iMvX > mvMax.iMvX || mvMin.iMvY > mvMax.iMvY {
		return false
	}
	encOff, refOff := pMe.iEncMbOff, pMe.iRefMbOff
	if encOff < 0 || encOff > len(pMe.pEncMb)-(h-1)*encStride-w {
		return false
	}
	// The original path calculates row offsets in int32. Decline windows
	// where those operations could wrap before conversion to int.
	lowDelta := (int64(mvMin.iMvY)-1)*int64(refStride) + int64(mvMin.iMvX) - 1
	highDelta := int64(mvMax.iMvY)*int64(refStride) + int64(mvMax.iMvX)
	if lowDelta < -1<<31 || highDelta > 1<<31-1 || refOff < 0 || refOff > len(pMe.pRefMb) {
		return false
	}
	lo := int64(refOff) + lowDelta
	hi := int64(refOff) + highDelta + int64(h-1)*int64(refStride) + int64(w)
	if lo < 0 || hi > int64(len(pMe.pRefMb)) {
		return false
	}
	mvp := pMe.sMvp
	costOff := pMe.iMvdCostOff
	if costOff < 0 || costOff >= len(pMe.pMvdCost) {
		return false
	}
	dLo := min((int(mvMin.iMvX)-1)*4-int(mvp.iMvX), (int(mvMin.iMvY)-1)*4-int(mvp.iMvY))
	dHi := max(int(mvMax.iMvX)*4-int(mvp.iMvX), int(mvMax.iMvY)*4-int(mvp.iMvY))
	if costOff < -dLo || costOff > len(pMe.pMvdCost)-1-dHi {
		return false
	}

	enc := (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(pMe.pEncMb)), encOff))
	refBase := unsafe.Pointer(unsafe.SliceData(pMe.pRefMb))
	costBase := unsafe.Add(unsafe.Pointer(unsafe.SliceData(pMe.pMvdCost)), 2*costOff)
	cost := func(dx, dy int32) int32 {
		return int32(*(*uint16)(unsafe.Add(costBase, 2*int(dx)))) + int32(*(*uint16)(unsafe.Add(costBase, 2*int(dy))))
	}
	sad := func(off int) int32 {
		return common.SadPtr(w, h, enc, encStride, (*byte)(unsafe.Add(refBase, off)), refStride)
	}

	// Step 1: initial point prediction (WelsMotionEstimateInitialPoint).
	mvpX, mvpY := int32(mvp.iMvX), int32(mvp.iMvY)
	var sMv SMVUnitXY
	sMv.iMvX = int16(common.WELS_CLIP3((2+mvpX)>>2, int32(mvMin.iMvX), int32(mvMax.iMvX)))
	sMv.iMvY = int16(common.WELS_CLIP3((2+mvpY)>>2, int32(mvMin.iMvY), int32(mvMax.iMvY)))
	pRefMb := pMe.pRefMb
	iRefMbOff := refOff + int(sMv.iMvY)*refStride + int(sMv.iMvX)
	var iBestSadCost int32
	mvc := &pSlice.sMvc
	count := int(pSlice.uiMvcNum)
	// Preserve main's shared-load two-candidate kernel. The whole search
	// window has already been validated, so this entry needs no extra checks.
	if count == 1 && (pMe.uiBlockSize <= BLOCK_8x8 || pMe.uiBlockSize == BLOCK_8x4) {
		candidate := SMVUnitXY{
			iMvX: int16(common.WELS_CLIP3((2+int32(mvc[0].iMvX))>>2, int32(mvMin.iMvX), int32(mvMax.iMvX))),
			iMvY: int16(common.WELS_CLIP3((2+int32(mvc[0].iMvY))>>2, int32(mvMin.iMvY), int32(mvMax.iMvY))),
		}
		if candidate != sMv {
			off := refOff + int(candidate.iMvY)*refStride + int(candidate.iMvX)
			first, second := sadTwoSSE2(enc, encStride, (*byte)(unsafe.Add(refBase, iRefMbOff)),
				(*byte)(unsafe.Add(refBase, off)), refStride, w, h)
			iBestSadCost = first + cost(int32(sMv.iMvX)*4-mvpX, int32(sMv.iMvY)*4-mvpY)
			second += cost(int32(candidate.iMvX)*4-mvpX, int32(candidate.iMvY)*4-mvpY)
			if second < iBestSadCost {
				sMv, iRefMbOff, iBestSadCost = candidate, off, second
			}
		} else {
			iBestSadCost = sad(iRefMbOff) + cost(int32(sMv.iMvX)*4-mvpX, int32(sMv.iMvY)*4-mvpY)
		}
		count = 0
	} else {
		iBestSadCost = sad(iRefMbOff) + cost(int32(sMv.iMvX)*4-mvpX, int32(sMv.iMvY)*4-mvpY)
	}
	for i := 0; i < count; i++ {
		iMvc0 := int16(common.WELS_CLIP3((2+int32(mvc[i].iMvX))>>2, int32(mvMin.iMvX), int32(mvMax.iMvX)))
		iMvc1 := int16(common.WELS_CLIP3((2+int32(mvc[i].iMvY))>>2, int32(mvMin.iMvY), int32(mvMax.iMvY)))
		if iMvc0 != sMv.iMvX || iMvc1 != sMv.iMvY {
			off := refOff + int(iMvc1)*refStride + int(iMvc0)
			if c := sad(off) + cost(int32(iMvc0)*4-mvpX, int32(iMvc1)*4-mvpY); c < iBestSadCost {
				sMv.iMvX, sMv.iMvY = iMvc0, iMvc1
				iRefMbOff = off
				iBestSadCost = c
			}
		}
	}

	// bWholeIntegerSearch is installed only with CheckDirectionalMvFalse.
	// Screen-content and instrumented callbacks keep the checked path.
	UpdateMeResults(sMv, uint32(iBestSadCost), pRefMb, iRefMbOff, pMe)
	if iBestSadCost < int32(pMe.uSadPredISatd) {
		MeEndIntepelSearch(pMe)
		return true
	}

	// Step 2: diamond search (WelsDiamondSearch) around the initial point.
	iMvDx := int32(pMe.sMv.iMvX)*4 - mvpX
	iMvDy := int32(pMe.sMv.iMvY)*4 - mvpY
	iBestCost := int32(pMe.uiSadCost)
	sads := &pSlice.sMbCacheInfo.iDiamondSad
	for iTimeThreshold := int32(ITERATIVE_TIMES); iTimeThreshold != 0; {
		iTimeThreshold--
		pMe.sMv.iMvX = int16((iMvDx + mvpX) >> 2)
		pMe.sMv.iMvY = int16((iMvDy + mvpY) >> 2)
		if !CheckMvInRange(pMe.sMv, mvMin, mvMax) {
			continue
		}
		common.SadFourPtr(w, h, enc, encStride, (*byte)(unsafe.Add(refBase, iRefMbOff)), refStride, sads)

		input := iBestCost
		var iX, iY int32
		if c := sads[0] + cost(iMvDx, iMvDy-4); c < iBestCost {
			iBestCost, iX, iY = c, 0, 1
		}
		if c := sads[1] + cost(iMvDx, iMvDy+4); c < iBestCost {
			iBestCost, iX, iY = c, 0, -1
		}
		if c := sads[2] + cost(iMvDx-4, iMvDy); c < iBestCost {
			iBestCost, iX, iY = c, 1, 0
		}
		if c := sads[3] + cost(iMvDx+4, iMvDy); c < iBestCost {
			iBestCost, iX, iY = c, -1, 0
		}
		if iBestCost == input {
			break
		}
		iMvDx -= iX * 4
		iMvDy -= iY * 4
		iRefMbOff -= int(iX + iY*iStrideRef)
	}
	pMe.sMv.iMvX = int16((iMvDx + mvpX) >> 2)
	pMe.sMv.iMvY = int16((iMvDy + mvpY) >> 2)
	pMe.uiSadCost = uint32(iBestCost)
	pMe.uiSatdCost = pMe.uiSadCost
	pMe.pRefMb = pRefMb
	pMe.iRefMbOff = iRefMbOff
	MeEndIntepelSearch(pMe)
	return true
}
