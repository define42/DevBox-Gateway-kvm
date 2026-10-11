// Port of codec/encoder/core/src/deblocking.cpp.
//
// SINGLE_REF_FRAME is defined (as264_common.h), so the reference index
// comparison in DeblockingBSMarginalMBAvcbase is compiled out as in C.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

var g_kuiAlphaTable = [52 + 12]uint8{ //this table refers to Table 8-16 in H.264/AVC standard
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 4, 4, 5, 6,
	7, 8, 9, 10, 12, 13, 15, 17, 20, 22,
	25, 28, 32, 36, 40, 45, 50, 56, 63, 71,
	80, 90, 101, 113, 127, 144, 162, 182, 203, 226,
	255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
}

var g_kiBetaTable = [52 + 12]int8{ //this table refers to Table 8-16 in H.264/AVC standard
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 2, 2, 2, 3,
	3, 3, 3, 4, 4, 4, 6, 6, 7, 7,
	8, 8, 9, 9, 10, 10, 11, 11, 12, 12,
	13, 13, 14, 14, 15, 15, 16, 16, 17, 17,
	18, 18,
	18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18,
}

var g_kiTc0Table = [52 + 12][4]int8{ //this table refers Table 8-17 in H.264/AVC standard
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 1},
	{-1, 0, 0, 1}, {-1, 0, 0, 1}, {-1, 0, 0, 1}, {-1, 0, 1, 1}, {-1, 0, 1, 1}, {-1, 1, 1, 1},
	{-1, 1, 1, 1}, {-1, 1, 1, 1}, {-1, 1, 1, 1}, {-1, 1, 1, 2}, {-1, 1, 1, 2}, {-1, 1, 1, 2},
	{-1, 1, 1, 2}, {-1, 1, 2, 3}, {-1, 1, 2, 3}, {-1, 2, 2, 3}, {-1, 2, 2, 4}, {-1, 2, 3, 4},
	{-1, 2, 3, 4}, {-1, 3, 3, 5}, {-1, 3, 4, 6}, {-1, 3, 4, 6}, {-1, 4, 5, 7}, {-1, 4, 5, 8},
	{-1, 4, 6, 9}, {-1, 5, 7, 10}, {-1, 6, 8, 11}, {-1, 6, 8, 13}, {-1, 7, 10, 14}, {-1, 8, 11, 16},
	{-1, 9, 12, 18}, {-1, 10, 13, 20}, {-1, 11, 15, 23}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
	{-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25}, {-1, 13, 17, 25},
}

var g_kuiTableBIdx = [2][8]uint8{
	{
		0, 4, 8, 12, // g_kuiTableBIdx
		3, 7, 11, 15,
	}, // table_bn_idx

	{
		0, 1, 2, 3, // g_kuiTableBIdx
		12, 13, 14, 15,
	}, // table_bn_idx
}

// dbkLd32 loads 4 int8 values as a little-endian uint32 (C *(uint32_t*)(pNnzTab + off)).
func dbkLd32(p []int8) uint32 {
	return uint32(uint8(p[0])) | uint32(uint8(p[1]))<<8 | uint32(uint8(p[2]))<<16 | uint32(uint8(p[3]))<<24
}

// dbkSt32 stores a uint32 little-endian into 4 bytes (C *(uint32_t*)p = v).
func dbkSt32(p *[4]uint8, v uint32) {
	p[0] = uint8(v)
	p[1] = uint8(v >> 8)
	p[2] = uint8(v >> 16)
	p[3] = uint8(v >> 24)
}

// dbkNonZero4 is C `*(uint32_t*)p != 0`.
func dbkNonZero4(p *[4]uint8) bool {
	return p[0]|p[1]|p[2]|p[3] != 0
}

// MB_BS_MV
func MB_BS_MV(sCurMv []SMVUnitXY, sNeighMv []SMVUnitXY, uiBIdx int, uiBnIdx int) bool {
	return common.WELS_ABS(int32(sCurMv[uiBIdx].iMvX)-int32(sNeighMv[uiBnIdx].iMvX)) >= 4 ||
		common.WELS_ABS(int32(sCurMv[uiBIdx].iMvY)-int32(sNeighMv[uiBnIdx].iMvY)) >= 4
}

// SMB_EDGE_MV
func SMB_EDGE_MV(sMotionVector []SMVUnitXY, uiBIdx int, uiBnIdx int) int32 {
	v := (common.WELS_ABS(int32(sMotionVector[uiBIdx].iMvX)-int32(sMotionVector[uiBnIdx].iMvX)) &^ 3) |
		(common.WELS_ABS(int32(sMotionVector[uiBIdx].iMvY)-int32(sMotionVector[uiBnIdx].iMvY)) &^ 3)
	if v != 0 {
		return 1
	}
	return 0
}

// BS_EDGE
func BS_EDGE(bsx1 uint8, sMotionVector []SMVUnitXY, uiBIdx int, uiBnIdx int) uint8 {
	var sh uint
	if bsx1 != 0 {
		sh = 1
	}
	return uint8((int32(bsx1) | SMB_EDGE_MV(sMotionVector, uiBIdx, uiBnIdx)) << sh)
}

// GET_ALPHA_BETA_FROM_QP
func dbkGetAlphaBetaFromQp(QP int32, iAlphaOffset int32, iBetaOffset int32) (iIdexA int32, iAlpha int32, iBeta int32) {
	iIdexA = QP + iAlphaOffset
	iIdexA = common.CLIP3_QP_0_51(iIdexA)
	iAlpha = int32(g_kuiAlphaTable[iIdexA])
	iBeta = int32(g_kiBetaTable[common.CLIP3_QP_0_51(QP+iBetaOffset)])
	return
}

// TC0_TBL_LOOKUP
func dbkTc0TblLookup(iTc []int8, iIdexA int32, pBS []uint8, bchroma int8) {
	iTc[0] = g_kiTc0Table[iIdexA][pBS[0]] + bchroma
	iTc[1] = g_kiTc0Table[iIdexA][pBS[1]] + bchroma
	iTc[2] = g_kiTc0Table[iIdexA][pBS[2]] + bchroma
	iTc[3] = g_kiTc0Table[iIdexA][pBS[3]] + bchroma
}

// (void inline in C) uiBS: uint8_t uiBS[2][4][4].
func DeblockingBSInsideMBAvsbase(pNnzTab []int8, uiBS *[2][4][4]uint8, iLShiftFactor int32) {
	var uiNnz32b0, uiNnz32b1, uiNnz32b2, uiNnz32b3 uint32
	sh := uint(iLShiftFactor)
	nz := func(a, b int) uint8 { return uint8((int32(pNnzTab[a]) | int32(pNnzTab[b])) << sh) }

	uiNnz32b0 = dbkLd32(pNnzTab[0:])
	uiNnz32b1 = dbkLd32(pNnzTab[4:])
	uiNnz32b2 = dbkLd32(pNnzTab[8:])
	uiNnz32b3 = dbkLd32(pNnzTab[12:])

	uiBS[0][1][0] = nz(0, 1)
	uiBS[0][2][0] = nz(1, 2)
	uiBS[0][3][0] = nz(2, 3)

	uiBS[0][1][1] = nz(4, 5)
	uiBS[0][2][1] = nz(5, 6)
	uiBS[0][3][1] = nz(6, 7)
	dbkSt32(&uiBS[1][1], (uiNnz32b0|uiNnz32b1)<<sh)

	uiBS[0][1][2] = nz(8, 9)
	uiBS[0][2][2] = nz(9, 10)
	uiBS[0][3][2] = nz(10, 11)
	dbkSt32(&uiBS[1][2], (uiNnz32b1|uiNnz32b2)<<sh)

	uiBS[0][1][3] = nz(12, 13)
	uiBS[0][2][3] = nz(13, 14)
	uiBS[0][3][3] = nz(14, 15)
	dbkSt32(&uiBS[1][3], (uiNnz32b2|uiNnz32b3)<<sh)
}

func DeblockingBSInsideMBNormal(pCurMb *SMB, uiBS *[2][4][4]uint8, pNnzTab []int8) {
	var uiNnz32b0, uiNnz32b1, uiNnz32b2, uiNnz32b3 uint32
	var uiBsx4 [4]uint8
	sMv := pCurMb.sMv

	uiNnz32b0 = dbkLd32(pNnzTab[0:])
	uiNnz32b1 = dbkLd32(pNnzTab[4:])
	uiNnz32b2 = dbkLd32(pNnzTab[8:])
	uiNnz32b3 = dbkLd32(pNnzTab[12:])

	for r := 0; r < 4; r++ {
		b := 4 * r
		for i := 0; i < 3; i++ {
			uiBsx4[i] = uint8(pNnzTab[b+i] | pNnzTab[b+i+1])
		}
		uiBS[0][1][r] = BS_EDGE(uiBsx4[0], sMv, b+1, b+0)
		uiBS[0][2][r] = BS_EDGE(uiBsx4[1], sMv, b+2, b+1)
		uiBS[0][3][r] = BS_EDGE(uiBsx4[2], sMv, b+3, b+2)
	}

	//horizontal
	dbkSt32(&uiBsx4, uiNnz32b0|uiNnz32b1)
	uiBS[1][1][0] = BS_EDGE(uiBsx4[0], sMv, 4, 0)
	uiBS[1][1][1] = BS_EDGE(uiBsx4[1], sMv, 5, 1)
	uiBS[1][1][2] = BS_EDGE(uiBsx4[2], sMv, 6, 2)
	uiBS[1][1][3] = BS_EDGE(uiBsx4[3], sMv, 7, 3)

	dbkSt32(&uiBsx4, uiNnz32b1|uiNnz32b2)
	uiBS[1][2][0] = BS_EDGE(uiBsx4[0], sMv, 8, 4)
	uiBS[1][2][1] = BS_EDGE(uiBsx4[1], sMv, 9, 5)
	uiBS[1][2][2] = BS_EDGE(uiBsx4[2], sMv, 10, 6)
	uiBS[1][2][3] = BS_EDGE(uiBsx4[3], sMv, 11, 7)

	dbkSt32(&uiBsx4, uiNnz32b2|uiNnz32b3)
	uiBS[1][3][0] = BS_EDGE(uiBsx4[0], sMv, 12, 8)
	uiBS[1][3][1] = BS_EDGE(uiBsx4[1], sMv, 13, 9)
	uiBS[1][3][2] = BS_EDGE(uiBsx4[2], sMv, 14, 10)
	uiBS[1][3][3] = BS_EDGE(uiBsx4[3], sMv, 15, 11)
}

func DeblockingBSMarginalMBAvcbase(pCurMb *SMB, pNeighMb *SMB, iEdge int32) uint32 {
	var pBS [4]uint8
	pBIdx := g_kuiTableBIdx[iEdge][0:4]
	pBnIdx := g_kuiTableBIdx[iEdge][4:8]

	for i := 0; i < 4; i++ {
		if pCurMb.pNonZeroCount[pBIdx[i]]|pNeighMb.pNonZeroCount[pBnIdx[i]] != 0 {
			pBS[i] = 2
		} else {
			if MB_BS_MV(pCurMb.sMv, pNeighMb.sMv, int(pBIdx[i]), int(pBnIdx[i])) {
				pBS[i] = 1
			} else {
				pBS[i] = 0
			}
		}
	}
	return uint32(pBS[0]) | uint32(pBS[1])<<8 | uint32(pBS[2])<<16 | uint32(pBS[3])<<24
}

// The Go port currently installs only the generic deblocking kernels. Calling
// them directly lets escape analysis keep the per-edge scratch arrays on stack.

// pPix: picture pixel pointer -> (slice, offset); pBS: uint8_t* to the 4 BS values of an edge (uiBS[d][e][:]).
func FilteringEdgeLumaH(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	var iTc [4]int8
	iIdexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiLumaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(iTc[:], iIdexA, pBS, 0)
		common.DeblockLumaLt4V_c(pPix, iPixOff, iStride, iAlpha, iBeta, iTc[:])
	}
}

func FilteringEdgeLumaV(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	var iTc [4]int8
	iIdexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiLumaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(iTc[:], iIdexA, pBS, 0)
		common.DeblockLumaLt4H_c(pPix, iPixOff, iStride, iAlpha, iBeta, iTc[:])
	}
}

func FilteringEdgeLumaIntraH(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiLumaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		common.DeblockLumaEq4V_c(pPix, iPixOff, iStride, iAlpha, iBeta)
	}
}

func FilteringEdgeLumaIntraV(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiLumaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		common.DeblockLumaEq4H_c(pPix, iPixOff, iStride, iAlpha, iBeta)
	}
}

func FilteringEdgeChromaH(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	var iTc [4]int8
	iIdexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiChromaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(iTc[:], iIdexA, pBS, 1)
		common.DeblockChromaLt4V_c(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta, iTc[:])
	}
}

func FilteringEdgeChromaV(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	var iTc [4]int8
	iIdexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiChromaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(iTc[:], iIdexA, pBS, 1)
		common.DeblockChromaLt4H_c(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta, iTc[:])
	}
}

func FilteringEdgeChromaIntraH(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiChromaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		common.DeblockChromaEq4V_c(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta)
	}
}

func FilteringEdgeChromaIntraV(pfDeblocking *DeblockingFunc, pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiChromaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))

	if iAlpha|iBeta != 0 {
		common.DeblockChromaEq4H_c(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta)
	}
}

// dbkNeighborFlags computes iLeftFlag / iTopFlag (bLeftBsValid / bTopBsValid[uiFilterIdc]).
func dbkNeighborFlags(pCurMb *SMB, iMbStride int32, uiFilterIdc uint8) (iLeftFlag bool, iTopFlag bool) {
	iMbX := int32(pCurMb.iMbX)
	iMbY := int32(pCurMb.iMbY)

	bLeftBsValid := [2]bool{iMbX > 0, (iMbX > 0) && (pCurMb.uiSliceIdc == pCurMb.Add(-1).uiSliceIdc)}
	bTopBsValid := [2]bool{iMbY > 0, (iMbY > 0) && (pCurMb.uiSliceIdc == pCurMb.Add(-iMbStride).uiSliceIdc)}

	return bLeftBsValid[uiFilterIdc], bTopBsValid[uiFilterIdc]
}

func DeblockingInterMb(pfDeblocking *DeblockingFunc, pCurMb *SMB, pFilter *SDeblockingFilter, uiBS *[2][4][4]uint8) {
	iCurLumaQp := int8(pCurMb.uiLumaQp)
	iCurChromaQp := int8(pCurMb.uiChromaQp)
	iLineSize := pFilter.iCsStride[0]
	iLineSizeUV := pFilter.iCsStride[1]
	iMbStride := int32(pFilter.iMbStride)

	iLeftFlag, iTopFlag := dbkNeighborFlags(pCurMb, iMbStride, pFilter.uiFilterIdc)

	pDestY, pDestCb, pDestCr := pFilter.pCsData[0], pFilter.pCsData[1], pFilter.pCsData[2]
	oY, oCb, oCr := pFilter.iCsDataOff[0], pFilter.iCsDataOff[1], pFilter.iCsDataOff[2]

	if iLeftFlag {
		pLeft := pCurMb.Add(-1)
		pFilter.uiLumaQP = uint8((int32(iCurLumaQp) + int32(pLeft.uiLumaQp) + 1) >> 1)
		pFilter.uiChromaQP = uint8((int32(iCurChromaQp) + int32(pLeft.uiChromaQp) + 1) >> 1)

		if uiBS[0][0][0] == 0x04 {
			FilteringEdgeLumaIntraV(pfDeblocking, pFilter, pDestY, oY, iLineSize, nil)
			FilteringEdgeChromaIntraV(pfDeblocking, pFilter, pDestCb, oCb, pDestCr, oCr, iLineSizeUV, nil)
		} else {
			if dbkNonZero4(&uiBS[0][0]) {
				FilteringEdgeLumaV(pfDeblocking, pFilter, pDestY, oY, iLineSize, uiBS[0][0][:])
				FilteringEdgeChromaV(pfDeblocking, pFilter, pDestCb, oCb, pDestCr, oCr, iLineSizeUV, uiBS[0][0][:])
			}
		}
	}

	pFilter.uiLumaQP = uint8(iCurLumaQp)
	pFilter.uiChromaQP = uint8(iCurChromaQp)

	if dbkNonZero4(&uiBS[0][1]) {
		FilteringEdgeLumaV(pfDeblocking, pFilter, pDestY, oY+(1<<2), iLineSize, uiBS[0][1][:])
	}

	if dbkNonZero4(&uiBS[0][2]) {
		FilteringEdgeLumaV(pfDeblocking, pFilter, pDestY, oY+(2<<2), iLineSize, uiBS[0][2][:])
		FilteringEdgeChromaV(pfDeblocking, pFilter, pDestCb, oCb+(2<<1), pDestCr, oCr+(2<<1), iLineSizeUV, uiBS[0][2][:])
	}

	if dbkNonZero4(&uiBS[0][3]) {
		FilteringEdgeLumaV(pfDeblocking, pFilter, pDestY, oY+(3<<2), iLineSize, uiBS[0][3][:])
	}

	if iTopFlag {
		pTop := pCurMb.Add(-iMbStride)
		pFilter.uiLumaQP = uint8((int32(iCurLumaQp) + int32(pTop.uiLumaQp) + 1) >> 1)
		pFilter.uiChromaQP = uint8((int32(iCurChromaQp) + int32(pTop.uiChromaQp) + 1) >> 1)

		if uiBS[1][0][0] == 0x04 {
			FilteringEdgeLumaIntraH(pfDeblocking, pFilter, pDestY, oY, iLineSize, nil)
			FilteringEdgeChromaIntraH(pfDeblocking, pFilter, pDestCb, oCb, pDestCr, oCr, iLineSizeUV, nil)
		} else {
			if dbkNonZero4(&uiBS[1][0]) {
				FilteringEdgeLumaH(pfDeblocking, pFilter, pDestY, oY, iLineSize, uiBS[1][0][:])
				FilteringEdgeChromaH(pfDeblocking, pFilter, pDestCb, oCb, pDestCr, oCr, iLineSizeUV, uiBS[1][0][:])
			}
		}
	}

	pFilter.uiLumaQP = uint8(iCurLumaQp)
	pFilter.uiChromaQP = uint8(iCurChromaQp)

	if dbkNonZero4(&uiBS[1][1]) {
		FilteringEdgeLumaH(pfDeblocking, pFilter, pDestY, oY+int((1<<2)*iLineSize), iLineSize, uiBS[1][1][:])
	}

	if dbkNonZero4(&uiBS[1][2]) {
		FilteringEdgeLumaH(pfDeblocking, pFilter, pDestY, oY+int((2<<2)*iLineSize), iLineSize, uiBS[1][2][:])
		FilteringEdgeChromaH(pfDeblocking, pFilter, pDestCb, oCb+int((2<<1)*iLineSizeUV), pDestCr, oCr+int((2<<1)*iLineSizeUV),
			iLineSizeUV, uiBS[1][2][:])
	}

	if dbkNonZero4(&uiBS[1][3]) {
		FilteringEdgeLumaH(pfDeblocking, pFilter, pDestY, oY+int((3<<2)*iLineSize), iLineSize, uiBS[1][3][:])
	}
}

func FilteringEdgeLumaHV(pfDeblocking *DeblockingFunc, pCurMb *SMB, pFilter *SDeblockingFilter) {
	iLineSize := pFilter.iCsStride[0]
	iMbStride := int32(pFilter.iMbStride)

	iLeftFlag, iTopFlag := dbkNeighborFlags(pCurMb, iMbStride, pFilter.uiFilterIdc)

	var iTc [4]int8
	uiBSx4 := [4]uint8{3, 3, 3, 3} // 0x03030303

	pDestY := pFilter.pCsData[0]
	oY := pFilter.iCsDataOff[0]
	iCurQp := int8(pCurMb.uiLumaQp)

	// luma v
	if iLeftFlag {
		pFilter.uiLumaQP = uint8((int32(iCurQp) + int32(pCurMb.Add(-1).uiLumaQp) + 1) >> 1)
		FilteringEdgeLumaIntraV(pfDeblocking, pFilter, pDestY, oY, iLineSize, nil)
	}

	pFilter.uiLumaQP = uint8(iCurQp)
	iIdexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiLumaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))
	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(iTc[:], iIdexA, uiBSx4[:], 0)
		common.DeblockLumaLt4H_c(pDestY, oY+(1<<2), iLineSize, iAlpha, iBeta, iTc[:])
		common.DeblockLumaLt4H_c(pDestY, oY+(2<<2), iLineSize, iAlpha, iBeta, iTc[:])
		common.DeblockLumaLt4H_c(pDestY, oY+(3<<2), iLineSize, iAlpha, iBeta, iTc[:])
	}

	// luma h
	if iTopFlag {
		pFilter.uiLumaQP = uint8((int32(iCurQp) + int32(pCurMb.Add(-iMbStride).uiLumaQp) + 1) >> 1)
		FilteringEdgeLumaIntraH(pfDeblocking, pFilter, pDestY, oY, iLineSize, nil)
	}

	pFilter.uiLumaQP = uint8(iCurQp)
	if iAlpha|iBeta != 0 {
		common.DeblockLumaLt4V_c(pDestY, oY+int((1<<2)*iLineSize), iLineSize, iAlpha, iBeta, iTc[:])
		common.DeblockLumaLt4V_c(pDestY, oY+int((2<<2)*iLineSize), iLineSize, iAlpha, iBeta, iTc[:])
		common.DeblockLumaLt4V_c(pDestY, oY+int((3<<2)*iLineSize), iLineSize, iAlpha, iBeta, iTc[:])
	}
}

func FilteringEdgeChromaHV(pfDeblocking *DeblockingFunc, pCurMb *SMB, pFilter *SDeblockingFilter) {
	iLineSize := pFilter.iCsStride[1]
	iMbStride := int32(pFilter.iMbStride)

	iLeftFlag, iTopFlag := dbkNeighborFlags(pCurMb, iMbStride, pFilter.uiFilterIdc)

	var iTc [4]int8
	uiBSx4 := [4]uint8{3, 3, 3, 3} // 0x03030303

	pDestCb, pDestCr := pFilter.pCsData[1], pFilter.pCsData[2]
	oCb, oCr := pFilter.iCsDataOff[1], pFilter.iCsDataOff[2]
	iCurQp := int8(pCurMb.uiChromaQp)

	// chroma v
	if iLeftFlag {
		pFilter.uiChromaQP = uint8((int32(iCurQp) + int32(pCurMb.Add(-1).uiChromaQp) + 1) >> 1)
		FilteringEdgeChromaIntraV(pfDeblocking, pFilter, pDestCb, oCb, pDestCr, oCr, iLineSize, nil)
	}

	pFilter.uiChromaQP = uint8(iCurQp)
	iIdexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(int32(pFilter.uiChromaQP), int32(pFilter.iSliceAlphaC0Offset), int32(pFilter.iSliceBetaOffset))
	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(iTc[:], iIdexA, uiBSx4[:], 1)
		common.DeblockChromaLt4H_c(pDestCb, oCb+(2<<1), pDestCr, oCr+(2<<1), iLineSize, iAlpha, iBeta, iTc[:])
	}

	// chroma h
	if iTopFlag {
		pFilter.uiChromaQP = uint8((int32(iCurQp) + int32(pCurMb.Add(-iMbStride).uiChromaQp) + 1) >> 1)
		FilteringEdgeChromaIntraH(pfDeblocking, pFilter, pDestCb, oCb, pDestCr, oCr, iLineSize, nil)
	}

	pFilter.uiChromaQP = uint8(iCurQp)
	if iAlpha|iBeta != 0 {
		common.DeblockChromaLt4V_c(pDestCb, oCb+int((2<<1)*iLineSize), pDestCr, oCr+int((2<<1)*iLineSize), iLineSize, iAlpha,
			iBeta, iTc[:])
	}
}

// merge h&v lookup table operation to save performance
func DeblockingIntraMb(pfDeblocking *DeblockingFunc, pCurMb *SMB, pFilter *SDeblockingFilter) {
	FilteringEdgeLumaHV(pfDeblocking, pCurMb, pFilter)
	FilteringEdgeChromaHV(pfDeblocking, pCurMb, pFilter)
}

func DeblockingBSCalc_c(pFunc *SWelsFuncPtrList, pCurMb *SMB, uiBS *[2][4][4]uint8, uiCurMbType Mb_Type, iMbStride int32, iLeftFlag int32, iTopFlag int32) {
	if iLeftFlag != 0 {
		if common.IS_INTRA(pCurMb.Add(-1).uiMbType) {
			dbkSt32(&uiBS[0][0], 0x04040404)
		} else {
			dbkSt32(&uiBS[0][0], DeblockingBSMarginalMBAvcbase(pCurMb, pCurMb.Add(-1), 0))
		}
	} else {
		dbkSt32(&uiBS[0][0], 0)
	}
	if iTopFlag != 0 {
		if common.IS_INTRA(pCurMb.Add(-iMbStride).uiMbType) {
			dbkSt32(&uiBS[1][0], 0x04040404)
		} else {
			dbkSt32(&uiBS[1][0], DeblockingBSMarginalMBAvcbase(pCurMb, pCurMb.Add(-iMbStride), 1))
		}
	} else {
		dbkSt32(&uiBS[1][0], 0)
	}
	//SKIP MB_16x16 or others
	if uiCurMbType != common.MB_TYPE_SKIP {
		pFunc.pfSetNZCZero(pCurMb.pNonZeroCount) // set all none-zero nzc to 1; dbk can be opti!

		if uiCurMbType == common.MB_TYPE_16x16 {
			DeblockingBSInsideMBAvsbase(pCurMb.pNonZeroCount, uiBS, 1)
		} else {
			DeblockingBSInsideMBNormal(pCurMb, uiBS, pCurMb.pNonZeroCount)
		}
	} else {
		uiBS[0][1] = [4]uint8{}
		uiBS[0][2] = [4]uint8{}
		uiBS[0][3] = [4]uint8{}
		uiBS[1][1] = [4]uint8{}
		uiBS[1][2] = [4]uint8{}
		uiBS[1][3] = [4]uint8{}
	}
}

func DeblockingMbAvcbase(pFunc *SWelsFuncPtrList, pCurMb *SMB, pFilter *SDeblockingFilter) {
	var uiBS [2][4][4]uint8

	uiCurMbType := pCurMb.uiMbType
	iMbStride := int32(pFilter.iMbStride)

	bLeft, bTop := dbkNeighborFlags(pCurMb, iMbStride, pFilter.uiFilterIdc)
	var iLeftFlag, iTopFlag int32
	if bLeft {
		iLeftFlag = 1
	}
	if bTop {
		iTopFlag = 1
	}

	switch uiCurMbType {
	case common.MB_TYPE_INTRA4x4, common.MB_TYPE_INTRA16x16, common.MB_TYPE_INTRA_PCM:
		DeblockingIntraMb(&pFunc.pfDeblocking, pCurMb, pFilter)
	default:
		DeblockingBSCalc_c(pFunc, pCurMb, &uiBS, uiCurMbType, iMbStride, iLeftFlag, iTopFlag)
		DeblockingInterMb(&pFunc.pfDeblocking, pCurMb, pFilter, &uiBS)
	}
}

func DeblockingFilterFrameAvcbase(pCurDq *SDqLayer, pFunc *SWelsFuncPtrList) {
	kiMbWidth := int32(pCurDq.iMbWidth)
	kiMbHeight := int32(pCurDq.iMbHeight)
	pMbList := pCurDq.sMbDataP
	iCurrentMbBlock := 0
	sSliceHeaderExt := &pCurDq.ppSliceInLayer[0].sSliceHeaderExt
	var pFilter SDeblockingFilter

	/* Step1: parameters set */
	if sSliceHeaderExt.sSliceHeader.uiDisableDeblockingFilterIdc == 1 {
		return
	}

	if sSliceHeaderExt.sSliceHeader.uiDisableDeblockingFilterIdc != 0 {
		pFilter.uiFilterIdc = 1
	} else {
		pFilter.uiFilterIdc = 0
	}

	pDecPic := pCurDq.pDecPic
	pFilter.iCsStride[0] = pDecPic.iLineSize[0]
	pFilter.iCsStride[1] = pDecPic.iLineSize[1]
	pFilter.iCsStride[2] = pDecPic.iLineSize[2]

	pFilter.iSliceAlphaC0Offset = sSliceHeaderExt.sSliceHeader.iSliceAlphaC0Offset
	pFilter.iSliceBetaOffset = sSliceHeaderExt.sSliceHeader.iSliceBetaOffset

	pFilter.iMbStride = int16(kiMbWidth)

	pFilter.pCsData[0] = pDecPic.pData[0]
	pFilter.pCsData[1] = pDecPic.pData[1]
	pFilter.pCsData[2] = pDecPic.pData[2]

	for j := int32(0); j < kiMbHeight; j++ {
		pFilter.iCsDataOff[0] = pDecPic.iDataOff[0] + int((j*pFilter.iCsStride[0])<<4)
		pFilter.iCsDataOff[1] = pDecPic.iDataOff[1] + int((j*pFilter.iCsStride[1])<<3)
		pFilter.iCsDataOff[2] = pDecPic.iDataOff[2] + int((j*pFilter.iCsStride[2])<<3)
		for i := int32(0); i < kiMbWidth; i++ {
			DeblockingMbAvcbase(pFunc, &pMbList[iCurrentMbBlock], &pFilter)
			iCurrentMbBlock++
			pFilter.iCsDataOff[0] += common.MB_WIDTH_LUMA
			pFilter.iCsDataOff[1] += common.MB_WIDTH_CHROMA
			pFilter.iCsDataOff[2] += common.MB_WIDTH_CHROMA
		}
	}
}

func DeblockingFilterSliceAvcbase(pCurDq *SDqLayer, pFunc *SWelsFuncPtrList, pSlice *SSlice) {
	pMbList := pCurDq.sMbDataP
	sSliceHeaderExt := &pSlice.sSliceHeaderExt
	var pCurrentMbBlock *SMB

	kiMbWidth := int32(pCurDq.iMbWidth)
	kiMbHeight := int32(pCurDq.iMbHeight)
	kiTotalNumMb := kiMbWidth * kiMbHeight
	var iCurMbIdx, iNextMbIdx, iNumMbFiltered int32

	/* Step1: parameters set */
	if sSliceHeaderExt.sSliceHeader.uiDisableDeblockingFilterIdc == 1 {
		return
	}

	var pFilter SDeblockingFilter

	if sSliceHeaderExt.sSliceHeader.uiDisableDeblockingFilterIdc != 0 {
		pFilter.uiFilterIdc = 1
	} else {
		pFilter.uiFilterIdc = 0
	}
	pDecPic := pCurDq.pDecPic
	pFilter.iCsStride[0] = pDecPic.iLineSize[0]
	pFilter.iCsStride[1] = pDecPic.iLineSize[1]
	pFilter.iCsStride[2] = pDecPic.iLineSize[2]
	pFilter.iSliceAlphaC0Offset = sSliceHeaderExt.sSliceHeader.iSliceAlphaC0Offset
	pFilter.iSliceBetaOffset = sSliceHeaderExt.sSliceHeader.iSliceBetaOffset
	pFilter.iMbStride = int16(kiMbWidth)

	pFilter.pCsData[0] = pDecPic.pData[0]
	pFilter.pCsData[1] = pDecPic.pData[1]
	pFilter.pCsData[2] = pDecPic.pData[2]

	iNextMbIdx = sSliceHeaderExt.sSliceHeader.iFirstMbInSlice

	for {
		iCurMbIdx = iNextMbIdx
		pCurrentMbBlock = &pMbList[iCurMbIdx]

		iMbX := int32(pCurrentMbBlock.iMbX)
		iMbY := int32(pCurrentMbBlock.iMbY)
		pFilter.iCsDataOff[0] = pDecPic.iDataOff[0] + int((iMbX+iMbY*pFilter.iCsStride[0])<<4)
		pFilter.iCsDataOff[1] = pDecPic.iDataOff[1] + int((iMbX+iMbY*pFilter.iCsStride[1])<<3)
		pFilter.iCsDataOff[2] = pDecPic.iDataOff[2] + int((iMbX+iMbY*pFilter.iCsStride[2])<<3)

		DeblockingMbAvcbase(pFunc, pCurrentMbBlock, &pFilter)

		iNumMbFiltered++
		iNextMbIdx = WelsGetNextMbOfSlice(pCurDq, iCurMbIdx)
		//whether all of MB in current slice filtered or not
		if iNextMbIdx == -1 || iNextMbIdx >= kiTotalNumMb || iNumMbFiltered >= kiTotalNumMb {
			break
		}
	}
}

func DeblockingFilterSliceAvcbaseNull(pCurDq *SDqLayer, pFunc *SWelsFuncPtrList, pSlice *SSlice) {
}

func PerformDeblockingFilter(pEnc *sWelsEncCtx) {
	pCurLayer := pEnc.pCurDqLayer
	var pSlice *SSlice

	if pCurLayer.iLoopFilterDisableIdc == 0 {
		DeblockingFilterFrameAvcbase(pCurLayer, pEnc.pFuncList)
	} else if pCurLayer.iLoopFilterDisableIdc == 2 {
		var iSliceCount int32
		var iSliceIdx int32

		iSliceCount = GetCurrentSliceNum(pCurLayer)
		for {
			pSlice = pCurLayer.ppSliceInLayer[iSliceIdx]
			if pSlice == nil {
				panic("PerformDeblockingFilter: NULL slice")
			}
			DeblockingFilterSliceAvcbase(pCurLayer, pEnc.pFuncList, pSlice)
			iSliceIdx++
			if !(iSliceIdx < iSliceCount) {
				break
			}
		}
	}
}

func WelsBlockFuncInit(pfSetNZCZero *PSetNoneZeroCountZeroFunc, iCpu int32) {
	*pfSetNZCZero = common.WelsNonZeroCount_c
}

func DeblockingInit(pFunc *DeblockingFunc, iCpu int32) {
	pFunc.pfLumaDeblockingLT4Ver = common.DeblockLumaLt4V_c
	pFunc.pfLumaDeblockingEQ4Ver = common.DeblockLumaEq4V_c
	pFunc.pfLumaDeblockingLT4Hor = common.DeblockLumaLt4H_c
	pFunc.pfLumaDeblockingEQ4Hor = common.DeblockLumaEq4H_c

	pFunc.pfChromaDeblockingLT4Ver = common.DeblockChromaLt4V_c
	pFunc.pfChromaDeblockingEQ4Ver = common.DeblockChromaEq4V_c
	pFunc.pfChromaDeblockingLT4Hor = common.DeblockChromaLt4H_c
	pFunc.pfChromaDeblockingEQ4Hor = common.DeblockChromaEq4H_c

	pFunc.pfDeblockingBSCalc = DeblockingBSCalc_c
}
