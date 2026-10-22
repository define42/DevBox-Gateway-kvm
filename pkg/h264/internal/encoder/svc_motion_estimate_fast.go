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
// It reports false, having changed nothing, unless the SSE2 SAD kernels and the
// diamond search are installed and the geometry validates.
func welsMotionEstimateIntegerFast(pFuncList *SWelsFuncPtrList, pMe *SWelsME, pSlice *SSlice, iStrideEnc, iStrideRef int32) bool {
	if !pFuncList.bDiamondSearch || !pFuncList.sSampleDealingFuncs.bSadSIMD || int(pMe.uiBlockSize) >= MAX_BLOCK_TYPE ||
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
	if encOff < 0 || encOff+(h-1)*encStride+w > len(pMe.pEncMb) {
		return false
	}
	lo := refOff + (int(mvMin.iMvY)-1)*refStride + int(mvMin.iMvX) - 1
	hi := refOff + int(mvMax.iMvY)*refStride + int(mvMax.iMvX) + (h-1)*refStride + w
	if lo < 0 || hi > len(pMe.pRefMb) {
		return false
	}
	mvp := pMe.sMvp
	costOff := pMe.iMvdCostOff
	dLo := min((int(mvMin.iMvX)-1)*4-int(mvp.iMvX), (int(mvMin.iMvY)-1)*4-int(mvp.iMvY))
	dHi := max(int(mvMax.iMvX)*4-int(mvp.iMvX), int(mvMax.iMvY)*4-int(mvp.iMvY))
	if costOff+dLo < 0 || costOff+dHi >= len(pMe.pMvdCost) {
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
	iBestSadCost := sad(iRefMbOff) + cost(int32(sMv.iMvX)*4-mvpX, int32(sMv.iMvY)*4-mvpY)

	mvc := &pSlice.sMvc
	for i := 0; i < int(pSlice.uiMvcNum); i++ {
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

	pSad := pFuncList.sSampleDealingFuncs.pfSampleSad[pMe.uiBlockSize]
	directional := pFuncList.pfCheckDirectionalMv(pSad, pMe, mvMin, mvMax, iStrideEnc, iStrideRef, &pMe.iDirectionalSadCost)
	if directional {
		sMv = pMe.sDirectionalMv
		pRefMb = pMe.pColoRefMb
		iRefMbOff = pMe.iColoRefMbOff + int(int32(sMv.iMvY)*iStrideRef+int32(sMv.iMvX))
		iBestSadCost = pMe.iDirectionalSadCost
	}
	UpdateMeResults(sMv, uint32(iBestSadCost), pRefMb, iRefMbOff, pMe)
	if iBestSadCost < int32(pMe.uSadPredISatd) {
		MeEndIntepelSearch(pMe) // initial point early stop
		return true
	}
	if directional {
		// The search continues in the collocated reference, outside the
		// validated window.
		pFuncList.pfSearchMethod[pMe.uiBlockSize](pFuncList, pMe, pSlice, iStrideEnc, iStrideRef)
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
