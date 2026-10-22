// Port of codec/encoder/core/src/mv_pred.cpp.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

func mvPredBool2I32(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// basic pMv prediction unit for pMv width (4, 2, 1)
func PredMv(kpMvComp *SMVComponentUnit, iPartIdx int8, iPartW int8, iRef int32, sMvp *SMVUnitXY) {
	kuiLeftIdx := int(uint8(common.G_kuiCache30ScanIdx[iPartIdx] - 1))
	kuiTopIdx := int(uint8(common.G_kuiCache30ScanIdx[iPartIdx] - 6))

	var iMatchRef int32
	iLeftRef := int32(kpMvComp.iRefIndexCache[kuiLeftIdx])
	iTopRef := int32(kpMvComp.iRefIndexCache[kuiTopIdx])
	iRightTopRef := int32(kpMvComp.iRefIndexCache[kuiTopIdx+int(iPartW)])
	var iDiagonalRef int32
	sMvA := kpMvComp.sMotionVectorCache[kuiLeftIdx]
	sMvB := kpMvComp.sMotionVectorCache[kuiTopIdx]
	var sMvC SMVUnitXY

	if common.REF_NOT_AVAIL == iRightTopRef {
		iDiagonalRef = int32(kpMvComp.iRefIndexCache[kuiTopIdx-1]) // left_top;
		sMvC = kpMvComp.sMotionVectorCache[kuiTopIdx-1]
	} else {
		iDiagonalRef = iRightTopRef // right_top;
		sMvC = kpMvComp.sMotionVectorCache[kuiTopIdx+int(iPartW)]
	}

	if (common.REF_NOT_AVAIL == iTopRef) && (common.REF_NOT_AVAIL == iDiagonalRef) && iLeftRef != common.REF_NOT_AVAIL {
		*sMvp = sMvA
		return
	}

	// b2[diag] b1[top] b0[left] is available!
	iMatchRef = mvPredBool2I32(iRef == iLeftRef) << MB_LEFT_BIT
	iMatchRef |= mvPredBool2I32(iRef == iTopRef) << MB_TOP_BIT
	iMatchRef |= mvPredBool2I32(iRef == iDiagonalRef) << MB_TOPRIGHT_BIT
	switch iMatchRef {
	case LEFT_MB_POS: // A
		*sMvp = sMvA
	case TOP_MB_POS: // B
		*sMvp = sMvB
	case TOPRIGHT_MB_POS: // C or D
		*sMvp = sMvC
	default:
		sMvp.iMvX = int16(common.WelsMedian(int32(sMvA.iMvX), int32(sMvB.iMvX), int32(sMvC.iMvX)))
		sMvp.iMvY = int16(common.WelsMedian(int32(sMvA.iMvY), int32(sMvB.iMvY), int32(sMvC.iMvY)))
	}
}

func PredInter8x16Mv(pMbCache *SMbCache, iPartIdx int32, iRef int8, sMvp *SMVUnitXY) {
	kpMvComp := &pMbCache.sMvComponents
	if 0 == iPartIdx {
		kiLeftRef := kpMvComp.iRefIndexCache[6]
		if iRef == kiLeftRef {
			*sMvp = kpMvComp.sMotionVectorCache[6]
			return
		}
	} else { // 1 == iPartIdx
		iDiagonalRef := kpMvComp.iRefIndexCache[5] //top-right
		iIndex := 5
		if common.REF_NOT_AVAIL == iDiagonalRef {
			iDiagonalRef = kpMvComp.iRefIndexCache[2] //top-left for 8*8 block(iIndex 1)
			iIndex = 2
		}
		if iRef == iDiagonalRef {
			*sMvp = kpMvComp.sMotionVectorCache[iIndex]
			return
		}
	}

	PredMv(kpMvComp, int8(iPartIdx), 2, int32(iRef), sMvp)
}

func PredInter16x8Mv(pMbCache *SMbCache, iPartIdx int32, iRef int8, sMvp *SMVUnitXY) {
	kpMvComp := &pMbCache.sMvComponents
	if 0 == iPartIdx {
		kiTopRef := kpMvComp.iRefIndexCache[1]
		if iRef == kiTopRef {
			*sMvp = kpMvComp.sMotionVectorCache[1]
			return
		}
	} else { // 8 == iPartIdx
		kiLeftRef := kpMvComp.iRefIndexCache[18]
		if iRef == kiLeftRef {
			*sMvp = kpMvComp.sMotionVectorCache[18]
			return
		}
	}

	PredMv(kpMvComp, int8(iPartIdx), 4, int32(iRef), sMvp)
}

func mvPredIsZeroMv(mv SMVUnitXY) bool { return mv.iMvX == 0 && mv.iMvY == 0 }

func PredSkipMv(pMbCache *SMbCache, sMvp *SMVUnitXY) {
	kpMvComp := &pMbCache.sMvComponents
	kiLeftRef := kpMvComp.iRefIndexCache[6] //A
	kiTopRef := kpMvComp.iRefIndexCache[1]  //B

	if common.REF_NOT_AVAIL == kiLeftRef || common.REF_NOT_AVAIL == kiTopRef ||
		(0 == kiLeftRef && mvPredIsZeroMv(kpMvComp.sMotionVectorCache[6])) ||
		(0 == kiTopRef && mvPredIsZeroMv(kpMvComp.sMotionVectorCache[1])) {
		*sMvp = SMVUnitXY{}
		return
	}

	PredMv(kpMvComp, 0, 4, 0, sMvp)
}

// update pMv and uiRefIndex cache for current MB, only for P_16*16 (SKIP inclusive)
func UpdateP16x16MotionInfo(pMbCache *SMbCache, pCurMb *SMB, kiRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kMv := *pMv

	// Fixed-size views check each slice once; constant cache indexes need
	// no checks at all.
	*(*[4]int8)(pCurMb.pRefIndex) = [4]int8{kiRef, kiRef, kiRef, kiRef}
	// update pMv range from 0~15
	mv := (*[16]SMVUnitXY)(pCurMb.sMv)
	for i := range mv {
		mv[i] = kMv
	}

	/*
	 * blocks 0: 7~10, 1: 13~16, 2: 19~22, 3: 25~28
	 */
	ref, cache := &pMvComp.iRefIndexCache, &pMvComp.sMotionVectorCache
	for k := 0; k < 4; k++ {
		ref[7+k], ref[13+k], ref[19+k], ref[25+k] = kiRef, kiRef, kiRef, kiRef
		cache[7+k], cache[13+k], cache[19+k], cache[25+k] = kMv, kMv, kMv, kMv
	}
}

// update uiRefIndex and pMv of both SMB and Mb_cache, only for P16x8
func UpdateP16x8MotionInfo(pMbCache *SMbCache, pCurMb *SMB, kiPartIdx int32, kiRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kMv := *pMv
	kiScan4Idx := int(common.G_kuiMbCountScan4Idx[kiPartIdx])
	kiCacheIdx := int(common.G_kuiCache30ScanIdx[kiPartIdx])

	pCurMb.pRefIndex[kiPartIdx>>2] = kiRef
	pCurMb.pRefIndex[(kiPartIdx>>2)+1] = kiRef
	for i := 0; i < 8; i++ {
		pCurMb.sMv[kiScan4Idx+i] = kMv
	}

	/*
	 * blocks 0: g_kuiCache30ScanIdx[iPartIdx]~g_kuiCache30ScanIdx[iPartIdx]+3, 1: g_kuiCache30ScanIdx[iPartIdx]+6~g_kuiCache30ScanIdx[iPartIdx]+9
	 */
	for _, base := range [2]int{kiCacheIdx, kiCacheIdx + 6} {
		for k := 0; k < 4; k++ {
			pMvComp.iRefIndexCache[base+k] = kiRef
			pMvComp.sMotionVectorCache[base+k] = kMv
		}
	}
}

// update uiRefIndex and pMv of both SMB and Mb_cache, only for P8x16
func update_P8x16_motion_info(pMbCache *SMbCache, pCurMb *SMB, kiPartIdx int32, kiRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kMv := *pMv
	kiScan4Idx := int(common.G_kuiMbCountScan4Idx[kiPartIdx])
	kiCacheIdx := int(common.G_kuiCache30ScanIdx[kiPartIdx])
	kiBlkIdx := int(kiPartIdx >> 2)

	pCurMb.pRefIndex[kiBlkIdx] = kiRef
	pCurMb.pRefIndex[2+kiBlkIdx] = kiRef
	for _, base := range [4]int{0, 4, 8, 12} {
		pCurMb.sMv[base+kiScan4Idx] = kMv
		pCurMb.sMv[base+kiScan4Idx+1] = kMv
	}

	/*
	 * blocks 0: g_kuiCache30ScanIdx[iPartIdx]~g_kuiCache30ScanIdx[iPartIdx]+3, 1: g_kuiCache30ScanIdx[iPartIdx]+6~g_kuiCache30ScanIdx[iPartIdx]+9
	 */
	for _, base := range [2]int{kiCacheIdx, kiCacheIdx + 12} {
		for k := 0; k < 4; k++ {
			pMvComp.iRefIndexCache[base+k] = kiRef
			pMvComp.sMotionVectorCache[base+k] = kMv
		}
	}
}

// update uiRefIndex and pMv of both SMB and Mb_cache, only for P8x8
func UpdateP8x8MotionInfo(pMbCache *SMbCache, pCurMb *SMB, kiPartIdx int32, kiRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kMv := *pMv
	kiScan4Idx := int(common.G_kuiMbCountScan4Idx[kiPartIdx])
	kiCacheIdx := int(common.G_kuiCache30ScanIdx[kiPartIdx])

	//mb
	pCurMb.sMv[kiScan4Idx] = kMv
	pCurMb.sMv[kiScan4Idx+1] = kMv
	pCurMb.sMv[4+kiScan4Idx] = kMv
	pCurMb.sMv[4+kiScan4Idx+1] = kMv

	//cache
	for _, k := range [4]int{0, 1, 6, 7} {
		pMvComp.iRefIndexCache[kiCacheIdx+k] = kiRef
		pMvComp.sMotionVectorCache[kiCacheIdx+k] = kMv
	}
}

// update uiRefIndex and pMv of both SMB and Mb_cache, only for P4x4
func UpdateP4x4MotionInfo(pMbCache *SMbCache, pCurMb *SMB, kiPartIdx int32, kiRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kiScan4Idx := int(common.G_kuiMbCountScan4Idx[kiPartIdx])
	kiCacheIdx := int(common.G_kuiCache30ScanIdx[kiPartIdx])

	//mb
	pCurMb.sMv[kiScan4Idx] = *pMv
	//cache
	pMvComp.iRefIndexCache[kiCacheIdx] = kiRef
	pMvComp.sMotionVectorCache[kiCacheIdx] = *pMv
}

// update uiRefIndex and pMv of both SMB and Mb_cache, only for P8x4
func UpdateP8x4MotionInfo(pMbCache *SMbCache, pCurMb *SMB, kiPartIdx int32, kiRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kiScan4Idx := int(common.G_kuiMbCountScan4Idx[kiPartIdx])
	kiCacheIdx := int(common.G_kuiCache30ScanIdx[kiPartIdx])

	//mb
	pCurMb.sMv[kiScan4Idx] = *pMv
	pCurMb.sMv[1+kiScan4Idx] = *pMv
	//cache
	pMvComp.iRefIndexCache[kiCacheIdx] = kiRef
	pMvComp.iRefIndexCache[1+kiCacheIdx] = kiRef
	pMvComp.sMotionVectorCache[kiCacheIdx] = *pMv
	pMvComp.sMotionVectorCache[1+kiCacheIdx] = *pMv
}

// update uiRefIndex and pMv of both SMB and Mb_cache, only for P4x8
func UpdateP4x8MotionInfo(pMbCache *SMbCache, pCurMb *SMB, kiPartIdx int32, kiRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kiScan4Idx := int(common.G_kuiMbCountScan4Idx[kiPartIdx])
	kiCacheIdx := int(common.G_kuiCache30ScanIdx[kiPartIdx])

	//mb
	pCurMb.sMv[kiScan4Idx] = *pMv
	pCurMb.sMv[4+kiScan4Idx] = *pMv
	//cache
	pMvComp.iRefIndexCache[kiCacheIdx] = kiRef
	pMvComp.iRefIndexCache[6+kiCacheIdx] = kiRef
	pMvComp.sMotionVectorCache[kiCacheIdx] = *pMv
	pMvComp.sMotionVectorCache[6+kiCacheIdx] = *pMv
}

//=========================update motion info(MV and ref_idx) into Mb_cache==========================

func updateMotion2Cache8x8(pMvComp *SMVComponentUnit, kuiCacheIdx int, iRef int8, kMv SMVUnitXY) {
	for _, k := range [4]int{0, 1, 6, 7} {
		pMvComp.iRefIndexCache[kuiCacheIdx+k] = iRef
		pMvComp.sMotionVectorCache[kuiCacheIdx+k] = kMv
	}
}

// update uiRefIndex and pMv of only Mb_cache, only for P16x8
func UpdateP16x8Motion2Cache(pMbCache *SMbCache, iPartIdx int32, iRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	for i := 0; i < 2; i, iPartIdx = i+1, iPartIdx+4 {
		//cache
		kuiCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
		updateMotion2Cache8x8(pMvComp, kuiCacheIdx, iRef, *pMv)
	}
}

// update uiRefIndex and pMv of only Mb_cache, only for P8x16
func UpdateP8x16Motion2Cache(pMbCache *SMbCache, iPartIdx int32, iRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	for i := 0; i < 2; i, iPartIdx = i+1, iPartIdx+8 {
		//cache
		kuiCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
		updateMotion2Cache8x8(pMvComp, kuiCacheIdx, iRef, *pMv)
	}
}

// update uiRefIndex and pMv of only Mb_cache, only for P8x8
func UpdateP8x8Motion2Cache(pMbCache *SMbCache, iPartIdx int32, pRef int8, pMv *SMVUnitXY) {
	kuiCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])
	updateMotion2Cache8x8(&pMbCache.sMvComponents, kuiCacheIdx, pRef, *pMv)
}

// update uiRefIndex and pMv of only Mb_cache, for P4x4
func UpdateP4x4Motion2Cache(pMbCache *SMbCache, iPartIdx int32, pRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kuiCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])

	pMvComp.iRefIndexCache[kuiCacheIdx] = pRef
	pMvComp.sMotionVectorCache[kuiCacheIdx] = *pMv
}

// update uiRefIndex and pMv of only Mb_cache, for P8x4
func UpdateP8x4Motion2Cache(pMbCache *SMbCache, iPartIdx int32, pRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kuiCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])

	pMvComp.iRefIndexCache[kuiCacheIdx] = pRef
	pMvComp.iRefIndexCache[1+kuiCacheIdx] = pRef
	pMvComp.sMotionVectorCache[kuiCacheIdx] = *pMv
	pMvComp.sMotionVectorCache[1+kuiCacheIdx] = *pMv
}

// update uiRefIndex and pMv of only Mb_cache, for P4x8
func UpdateP4x8Motion2Cache(pMbCache *SMbCache, iPartIdx int32, pRef int8, pMv *SMVUnitXY) {
	pMvComp := &pMbCache.sMvComponents
	kuiCacheIdx := int(common.G_kuiCache30ScanIdx[iPartIdx])

	pMvComp.iRefIndexCache[kuiCacheIdx] = pRef
	pMvComp.iRefIndexCache[6+kuiCacheIdx] = pRef
	pMvComp.sMotionVectorCache[kuiCacheIdx] = *pMv
	pMvComp.sMotionVectorCache[6+kuiCacheIdx] = *pMv
}
