// Port of codec/decoder/core/src/deblocking.cpp.
//
// Boundary strength derivation and loop filtering of the decoder. Only the
// C loop filter implementations of package common are installed by
// DeblockingInit.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const (
	NO_SUPPORTED_FILTER_IDX = -1
	LEFT_FLAG_BIT           = 0
	TOP_FLAG_BIT            = 1
	LEFT_FLAG_MASK          = 0x01
	TOP_FLAG_MASK           = 0x02
)

// SAME_MB_DIFF_REFIDX is defined in the C source: the SMB_EDGE_MV /
// IN_SMB_EDGE_MV variants that compare reference pictures are ported.

var g_kuiAlphaTable = [52 + 24]uint8{ //this table refers to Table 8-16 in H.264/AVC standard
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 4, 4, 5, 6,
	7, 8, 9, 10, 12, 13, 15, 17, 20, 22,
	25, 28, 32, 36, 40, 45, 50, 56, 63, 71,
	80, 90, 101, 113, 127, 144, 162, 182, 203, 226,
	255, 255,
	255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255,
}

var g_kiBetaTable = [52 + 24]int8{ //this table refers to Table 8-16 in H.264/AVC standard
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 0, 0, 0, 0,
	0, 0, 0, 0, 0, 0, 2, 2, 2, 3,
	3, 3, 3, 4, 4, 4, 6, 6, 7, 7,
	8, 8, 9, 9, 10, 10, 11, 11, 12, 12,
	13, 13, 14, 14, 15, 15, 16, 16, 17, 17,
	18, 18,
	18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18, 18,
}

var g_kiTc0Table = [52 + 24][4]int8{ //this table refers Table 8-17 in H.264/AVC standard
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
	{-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0}, {-1, 0, 0, 0},
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
		0, 4, 8, 12,
		3, 7, 11, 15,
	},

	{
		0, 1, 2, 3,
		12, 13, 14, 15,
	},
}

var g_kuiTableB8x8Idx = [2][16]uint8{
	{
		0, 1, 4, 5, 8, 9, 12, 13, // 0   1 |  2  3
		2, 3, 6, 7, 10, 11, 14, 15, // 4   5 |  6  7
	}, // ------------
	// 8   9 | 10 11
	{
		// 12 13 | 14 15
		0, 1, 4, 5, 2, 3, 6, 7,
		8, 9, 12, 13, 10, 11, 14, 15,
	},
}

// GET_ALPHA_BETA_FROM_QP (iQp, iAlphaOffset, iBetaOffset, iIndex, iAlpha, iBeta)
func dbkGetAlphaBetaFromQp(iQp, iAlphaOffset, iBetaOffset int8) (iIndex, iAlpha, iBeta int32) {
	iIndex = int32(iQp) + int32(iAlphaOffset)
	iAlpha = int32(g_kuiAlphaTable[iIndex+12])
	iBeta = int32(g_kiBetaTable[int32(iQp)+int32(iBetaOffset)+12])
	return
}

// TC0_TBL_LOOKUP (tc, iIndexA, pBS, bChroma). fix Bugzilla 1486223
func dbkTc0TblLookup(tc *[4]int8, iIndexA int32, pBS []uint8, bChroma int32) {
	t := &g_kiTc0Table[iIndexA+12]
	tc[0] = int8(int32(t[pBS[0]&3]) + bChroma)
	tc[1] = int8(int32(t[pBS[1]&3]) + bChroma)
	tc[2] = int8(int32(t[pBS[2]&3]) + bChroma)
	tc[3] = int8(int32(t[pBS[3]&3]) + bChroma)
}

// dbkLd32 is the little-endian load *(uint32_t*)p of 4 int8/uint8 values.
func dbkLd32(p []int8) uint32 {
	return uint32(uint8(p[0])) | uint32(uint8(p[1]))<<8 | uint32(uint8(p[2]))<<16 | uint32(uint8(p[3]))<<24
}

// dbkSt32 is the little-endian store *(uint32_t*)p = v.
func dbkSt32(p *[4]uint8, v uint32) {
	p[0] = uint8(v)
	p[1] = uint8(v >> 8)
	p[2] = uint8(v >> 16)
	p[3] = uint8(v >> 24)
}

// dbkIsNonZero32 is *(uint32_t*)p != 0.
func dbkIsNonZero32(p *[4]uint8) bool {
	return p[0]|p[1]|p[2]|p[3] != 0
}

func dbkAbs(x int32) int32 {
	if x > 0 {
		return x
	}
	return -x
}

func dbkB2i(b bool) int32 {
	if b {
		return 1
	}
	return 0
}

// dbkRefPic returns pRefPics[iRefIdx] when iRefIdx > REF_NOT_IN_LIST, NULL otherwise.
func dbkRefPic(pRefPics []*SPicture, iRefIdx int8) *SPicture {
	if iRefIdx > REF_NOT_IN_LIST && int(iRefIdx) < len(pRefPics) {
		return pRefPics[iRefIdx]
	}
	return nil
}

// MB_BS_MV (pRefPic0, pRefPic1, iMotionVector, iMbXy, iMbBn, iIndex, iNeighIndex)
func dbkMbBsMv(pRefPic0, pRefPic1 *SPicture, iMotionVector [][common.MB_BLOCK4x4_NUM][common.MV_A]int16,
	iMbXy, iMbBn int32, iIndex, iNeighIndex uint8) uint8 {
	return uint8(dbkB2i((pRefPic0 != pRefPic1) ||
		(dbkAbs(int32(iMotionVector[iMbXy][iIndex][0])-int32(iMotionVector[iMbBn][iNeighIndex][0])) >= 4) ||
		(dbkAbs(int32(iMotionVector[iMbXy][iIndex][1])-int32(iMotionVector[iMbBn][iNeighIndex][1])) >= 4)))
}

// ON_MB_BS_MV_DIFF (iMV_A, iMV_B, iMbXy, iMbBn, iIndex, iNeighIndex)
func dbkOnMbBsMvDiff(iMV_A, iMV_B [][common.MB_BLOCK4x4_NUM][common.MV_A]int16, iMbXy, iMbBn int32,
	iIndex, iNeighIndex uint8) bool {
	return (dbkAbs(int32(iMV_A[iMbXy][iIndex][0])-int32(iMV_B[iMbBn][iNeighIndex][0])) >= 4) ||
		(dbkAbs(int32(iMV_A[iMbXy][iIndex][1])-int32(iMV_B[iMbBn][iNeighIndex][1])) >= 4)
}

// IN_MB_BS_MV_DIFF (iMV_A, iMV_B, iMbXy, iIndex, iNeighIndex)
func dbkInMbBsMvDiff(iMV_A, iMV_B [][common.MB_BLOCK4x4_NUM][common.MV_A]int16, iMbXy int32,
	iIndex, iNeighIndex int32) bool {
	return (dbkAbs(int32(iMV_A[iMbXy][iIndex][0])-int32(iMV_B[iMbXy][iNeighIndex][0])) >= 4) ||
		(dbkAbs(int32(iMV_A[iMbXy][iIndex][1])-int32(iMV_B[iMbXy][iNeighIndex][1])) >= 4)
}

// ON_MB_BS: On MB Boundary strength, apply for B_SLICE.
func dbkOnMbBs(ref_p0, ref_q0, ref_p1, ref_q1 *SPicture, mv0, mv1 [][common.MB_BLOCK4x4_NUM][common.MV_A]int16,
	iMbXy, iMbBn int32, iIndex, iNeighIndex uint8) uint8 {
	d := func(a, b [][common.MB_BLOCK4x4_NUM][common.MV_A]int16) bool {
		return dbkOnMbBsMvDiff(a, b, iMbXy, iMbBn, iIndex, iNeighIndex)
	}
	var r bool
	if ref_p0 != ref_p1 {
		if ref_p0 == ref_q0 {
			r = d(mv0, mv0) || d(mv1, mv1)
		} else {
			r = d(mv0, mv1) || d(mv1, mv0)
		}
	} else {
		r = (d(mv0, mv0) || d(mv1, mv1)) && (d(mv0, mv1) || d(mv1, mv0))
	}
	return uint8(dbkB2i(r))
}

// SMB_EDGE_MV (pRefPics, iMotionVector, iIndex, iNeighIndex) (SAME_MB_DIFF_REFIDX variant)
func dbkSmbEdgeMv(pRefPics *[common.MB_BLOCK4x4_NUM]*SPicture, iMotionVector *[common.MB_BLOCK4x4_NUM][common.MV_A]int16,
	iIndex, iNeighIndex int32) int32 {
	return dbkB2i((pRefPics[iIndex] != pRefPics[iNeighIndex]) ||
		((dbkAbs(int32(iMotionVector[iIndex][0])-int32(iMotionVector[iNeighIndex][0]))&(^3))|
			(dbkAbs(int32(iMotionVector[iIndex][1])-int32(iMotionVector[iNeighIndex][1]))&(^3))) != 0)
}

// IN_SMB_EDGE_MV (refs, mv, iMbXy, iIndex, iNeigborIndex) (SAME_MB_DIFF_REFIDX variant)
func dbkInSmbEdgeMv(refs *[common.LIST_A][common.MB_BLOCK4x4_NUM]*SPicture,
	mv *[common.LIST_A][][common.MB_BLOCK4x4_NUM][common.MV_A]int16, iMbXy int32, iIndex, iNeigborIndex int32) int32 {
	r0, r1 := &refs[common.LIST_0], &refs[common.LIST_1]
	m0, m1 := mv[common.LIST_0], mv[common.LIST_1]
	d := func(a, b [][common.MB_BLOCK4x4_NUM][common.MV_A]int16) bool {
		return dbkInMbBsMvDiff(a, b, iMbXy, iIndex, iNeigborIndex)
	}
	if ((r0[iIndex] == r0[iNeigborIndex]) && (r1[iIndex] == r1[iNeigborIndex])) ||
		((r0[iIndex] == r1[iNeigborIndex]) && (r1[iIndex] == r0[iNeigborIndex])) {
		if r0[iIndex] != r1[iIndex] {
			if r0[iIndex] == r0[iNeigborIndex] {
				return dbkB2i(d(m0, m0) || d(m1, m1))
			}
			return dbkB2i(d(m0, m1) || d(m1, m0))
		}
		return dbkB2i((d(m0, m0) || d(m1, m1)) && (d(m0, m1) || d(m1, m0)))
	}
	return 1
}

// BS_EDGE (bsx1, pRefPics, iMotionVector, iIndex, iNeighIndex)
func dbkBsEdge(bsx1 int32, pRefPics *[common.MB_BLOCK4x4_NUM]*SPicture,
	iMotionVector *[common.MB_BLOCK4x4_NUM][common.MV_A]int16, iIndex, iNeighIndex int32) uint8 {
	return uint8((bsx1 | dbkSmbEdgeMv(pRefPics, iMotionVector, iIndex, iNeighIndex)) << uint(dbkB2i(bsx1 != 0)))
}

// IN_BS_EDGE (bsx1, refs, mv, iMbXy, iIndex, iNeigborIndex): Inside MB Boundary strength, apply for B_SLICE.
func dbkInBsEdge(bsx1 int32, refs *[common.LIST_A][common.MB_BLOCK4x4_NUM]*SPicture,
	mv *[common.LIST_A][][common.MB_BLOCK4x4_NUM][common.MV_A]int16, iMbXy int32, iIndex, iNeigborIndex int32) uint8 {
	return uint8((bsx1 | dbkInSmbEdgeMv(refs, mv, iMbXy, iIndex, iNeigborIndex)) << uint(dbkB2i(bsx1 != 0)))
}

// DeblockingBSInsideMBAvsbase ports void inline DeblockingBSInsideMBAvsbase (int8_t* pNnzTab, uint8_t nBS[2][4][4], int32_t
// iLShiftFactor).
func DeblockingBSInsideMBAvsbase(pNnzTab []int8, nBS *[2][4][4]uint8, iLShiftFactor int32) {
	var uiNnz32b0, uiNnz32b1, uiNnz32b2, uiNnz32b3 uint32
	p := func(k int) int32 { return int32(pNnzTab[k]) }
	s := uint(iLShiftFactor)

	uiNnz32b0 = dbkLd32(pNnzTab[0:])
	uiNnz32b1 = dbkLd32(pNnzTab[4:])
	uiNnz32b2 = dbkLd32(pNnzTab[8:])
	uiNnz32b3 = dbkLd32(pNnzTab[12:])

	nBS[0][1][0] = uint8((p(0) | p(1)) << s)
	nBS[0][2][0] = uint8((p(1) | p(2)) << s)
	nBS[0][3][0] = uint8((p(2) | p(3)) << s)

	nBS[0][1][1] = uint8((p(4) | p(5)) << s)
	nBS[0][2][1] = uint8((p(5) | p(6)) << s)
	nBS[0][3][1] = uint8((p(6) | p(7)) << s)
	dbkSt32(&nBS[1][1], (uiNnz32b0|uiNnz32b1)<<s)

	nBS[0][1][2] = uint8((p(8) | p(9)) << s)
	nBS[0][2][2] = uint8((p(9) | p(10)) << s)
	nBS[0][3][2] = uint8((p(10) | p(11)) << s)
	dbkSt32(&nBS[1][2], (uiNnz32b1|uiNnz32b2)<<s)

	nBS[0][1][3] = uint8((p(12) | p(13)) << s)
	nBS[0][2][3] = uint8((p(13) | p(14)) << s)
	nBS[0][3][3] = uint8((p(14) | p(15)) << s)
	dbkSt32(&nBS[1][3], (uiNnz32b2|uiNnz32b3)<<s)
}

// dbkI8x8NnzTab computes i8x8NnzTab[4] from the 4x4 non-zero counts.
func dbkI8x8NnzTab(pNnzTab []int8) (i8x8NnzTab [4]int8) {
	sc := &common.G_kuiMbCountScan4Idx
	for i := 0; i < 4; i++ {
		iBlkIdx := i << 2
		i8x8NnzTab[i] = pNnzTab[sc[iBlkIdx]] | pNnzTab[sc[iBlkIdx+1]] |
			pNnzTab[sc[iBlkIdx+2]] | pNnzTab[sc[iBlkIdx+3]]
	}
	return
}

// DeblockingBSInsideMBAvsbase8x8 ports void inline DeblockingBSInsideMBAvsbase8x8 (int8_t* pNnzTab, uint8_t nBS[2][4][4], int32_t
// iLShiftFactor).
func DeblockingBSInsideMBAvsbase8x8(pNnzTab []int8, nBS *[2][4][4]uint8, iLShiftFactor int32) {
	i8x8NnzTab := dbkI8x8NnzTab(pNnzTab)
	s := uint(iLShiftFactor)
	n := func(a, b int) uint8 { return uint8((int32(i8x8NnzTab[a]) | int32(i8x8NnzTab[b])) << s) }

	//vertical
	nBS[0][2][0] = n(0, 1)
	nBS[0][2][1] = nBS[0][2][0]
	nBS[0][2][2] = n(2, 3)
	nBS[0][2][3] = nBS[0][2][2]
	//horizontal
	nBS[1][2][0] = n(0, 2)
	nBS[1][2][1] = nBS[1][2][0]
	nBS[1][2][2] = n(1, 3)
	nBS[1][2][3] = nBS[1][2][2]
}

func DeblockingBSInsideMBNormal(pFilter *SDeblockingFilter, pCurDqLayer *SDqLayer, nBS *[2][4][4]uint8,
	pNnzTab []int8, iMbXy int32) {
	iRefIdx := &pCurDqLayer.pDec.pRefIndex[common.LIST_0][iMbXy]
	var iRefs [common.MB_BLOCK4x4_NUM]*SPicture
	var uiBsx4 [4]uint8
	pMv := &pCurDqLayer.pDec.pMv[common.LIST_0][iMbXy]

	/* Look up each reference picture based on indices */
	for i := 0; i < common.MB_BLOCK4x4_NUM; i++ {
		iRefs[i] = dbkRefPic(pFilter.pRefPics[common.LIST_0], iRefIdx[i])
	}

	if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
		i8x8NnzTab := dbkI8x8NnzTab(pNnzTab)
		sc := &common.G_kuiMbCountScan4Idx
		e := func(a, b int, idx, nidx uint8) uint8 {
			return dbkBsEdge(int32(i8x8NnzTab[a])|int32(i8x8NnzTab[b]), &iRefs, pMv, int32(idx), int32(nidx))
		}
		//vertical
		nBS[0][2][0] = e(0, 1, sc[1<<2], sc[0])
		nBS[0][2][1] = nBS[0][2][0]
		nBS[0][2][2] = e(2, 3, sc[3<<2], sc[2<<2])
		nBS[0][2][3] = nBS[0][2][2]

		//horizontal
		nBS[1][2][0] = e(0, 2, sc[2<<2], sc[0])
		nBS[1][2][1] = nBS[1][2][0]
		nBS[1][2][2] = e(1, 3, sc[3<<2], sc[1<<2])
		nBS[1][2][3] = nBS[1][2][2]
	} else {
		uiNnz32b0 := dbkLd32(pNnzTab[0:])
		uiNnz32b1 := dbkLd32(pNnzTab[4:])
		uiNnz32b2 := dbkLd32(pNnzTab[8:])
		uiNnz32b3 := dbkLd32(pNnzTab[12:])
		e := func(k int, idx, nidx int32) uint8 {
			return dbkBsEdge(int32(uiBsx4[k]), &iRefs, pMv, idx, nidx)
		}

		for r := 0; r < 4; r++ {
			b := r << 2
			for i := 0; i < 3; i++ {
				uiBsx4[i] = uint8(pNnzTab[b+i] | pNnzTab[b+i+1])
			}
			nBS[0][1][r] = e(0, int32(b+1), int32(b))
			nBS[0][2][r] = e(1, int32(b+2), int32(b+1))
			nBS[0][3][r] = e(2, int32(b+3), int32(b+2))
		}

		// horizontal
		dbkSt32(&uiBsx4, uiNnz32b0|uiNnz32b1)
		nBS[1][1][0] = e(0, 4, 0)
		nBS[1][1][1] = e(1, 5, 1)
		nBS[1][1][2] = e(2, 6, 2)
		nBS[1][1][3] = e(3, 7, 3)

		dbkSt32(&uiBsx4, uiNnz32b1|uiNnz32b2)
		nBS[1][2][0] = e(0, 8, 4)
		nBS[1][2][1] = e(1, 9, 5)
		nBS[1][2][2] = e(2, 10, 6)
		nBS[1][2][3] = e(3, 11, 7)

		dbkSt32(&uiBsx4, uiNnz32b2|uiNnz32b3)
		nBS[1][3][0] = e(0, 12, 8)
		nBS[1][3][1] = e(1, 13, 9)
		nBS[1][3][2] = e(2, 14, 10)
		nBS[1][3][3] = e(3, 15, 11)
	}
}

func DeblockingBSliceBSInsideMBNormal(pFilter *SDeblockingFilter, pCurDqLayer *SDqLayer,
	nBS *[2][4][4]uint8, pNnzTab []int8, iMbXy int32) {
	var iRefs [common.LIST_A][common.MB_BLOCK4x4_NUM]*SPicture
	var uiBsx4 [4]uint8
	pMv := &pCurDqLayer.pDec.pMv

	for l := 0; l < common.LIST_A; l++ {
		iRefIdx := &pCurDqLayer.pDec.pRefIndex[l][iMbXy]
		/* Look up each reference picture based on indices */
		for i := 0; i < common.MB_BLOCK4x4_NUM; i++ {
			iRefs[l][i] = dbkRefPic(pFilter.pRefPics[l], iRefIdx[i])
		}
	}

	if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
		i8x8NnzTab := dbkI8x8NnzTab(pNnzTab)
		sc := &common.G_kuiMbCountScan4Idx
		e := func(a, b int, idx, nidx uint8) uint8 {
			return dbkInBsEdge(int32(i8x8NnzTab[a])|int32(i8x8NnzTab[b]), &iRefs, pMv, iMbXy,
				int32(int8(idx)), int32(int8(nidx)))
		}
		//vertical
		nBS[0][2][0] = e(0, 1, sc[1<<2], sc[0])
		nBS[0][2][1] = nBS[0][2][0]
		nBS[0][2][2] = e(2, 3, sc[3<<2], sc[2<<2])
		nBS[0][2][3] = nBS[0][2][2]

		//horizontal
		nBS[1][2][0] = e(0, 2, sc[2<<2], sc[0])
		nBS[1][2][1] = nBS[1][2][0]
		nBS[1][2][2] = e(1, 3, sc[3<<2], sc[1<<2])
		nBS[1][2][3] = nBS[1][2][2]
	} else {
		uiNnz32b0 := dbkLd32(pNnzTab[0:])
		uiNnz32b1 := dbkLd32(pNnzTab[4:])
		uiNnz32b2 := dbkLd32(pNnzTab[8:])
		uiNnz32b3 := dbkLd32(pNnzTab[12:])
		e := func(k int, idx, nidx int32) uint8 {
			return dbkInBsEdge(int32(uiBsx4[k]), &iRefs, pMv, iMbXy, idx, nidx)
		}

		for r := 0; r < 4; r++ {
			b := r << 2
			for i := 0; i < 3; i++ {
				uiBsx4[i] = uint8(pNnzTab[b+i] | pNnzTab[b+i+1])
			}
			nBS[0][1][r] = e(0, int32(b+1), int32(b))
			nBS[0][2][r] = e(1, int32(b+2), int32(b+1))
			nBS[0][3][r] = e(2, int32(b+3), int32(b+2))
		}

		// horizontal
		dbkSt32(&uiBsx4, uiNnz32b0|uiNnz32b1)
		nBS[1][1][0] = e(0, 4, 0)
		nBS[1][1][1] = e(1, 5, 1)
		nBS[1][1][2] = e(2, 6, 2)
		nBS[1][1][3] = e(3, 7, 3)

		dbkSt32(&uiBsx4, uiNnz32b1|uiNnz32b2)
		nBS[1][2][0] = e(0, 8, 4)
		nBS[1][2][1] = e(1, 9, 5)
		nBS[1][2][2] = e(2, 10, 6)
		nBS[1][2][3] = e(3, 11, 7)

		dbkSt32(&uiBsx4, uiNnz32b2|uiNnz32b3)
		nBS[1][3][0] = e(0, 12, 8)
		nBS[1][3][1] = e(1, 13, 9)
		nBS[1][3][2] = e(2, 14, 10)
		nBS[1][3][3] = e(3, 15, 11)
		// (the C code ends with a no-op loop "if (nBS > 1) nBS = nBS")
	}
}

// dbkPack4 packs pBS[0..3] into the uint32 the C code returns through
// uint8_t* pBS = (uint8_t*)&uiBSx4 (little-endian byte order).
func dbkPack4(pBS *[4]uint8) uint32 {
	return uint32(pBS[0]) | uint32(pBS[1])<<8 | uint32(pBS[2])<<16 | uint32(pBS[3])<<24
}

// DeblockingBsMarginalMBAvcbase ports uint32_t DeblockingBsMarginalMBAvcbase (PDeblockingFilter pFilter, PDqLayer pCurDqLayer, int32_t
// iEdge, int32_t iNeighMb, int32_t iMbXy).
//
// The returned uint32 holds the 4 boundary strengths in little-endian byte order.
func DeblockingBsMarginalMBAvcbase(pFilter *SDeblockingFilter, pCurDqLayer *SDqLayer, iEdge int32, iNeighMb int32, iMbXy int32) uint32 {
	var pBS [4]uint8
	pBIdx := g_kuiTableBIdx[iEdge][0:]
	pBnIdx := g_kuiTableBIdx[iEdge][4:]
	pB8x8Idx := g_kuiTableB8x8Idx[iEdge][0:]
	pBn8x8Idx := g_kuiTableB8x8Idx[iEdge][8:]
	var iRefIdx [][common.MB_BLOCK4x4_NUM]int8
	var pMvSel [][common.MB_BLOCK4x4_NUM][common.MV_A]int16
	if pCurDqLayer.pDec != nil {
		iRefIdx = pCurDqLayer.pDec.pRefIndex[common.LIST_0]
		pMvSel = pCurDqLayer.pDec.pMv[common.LIST_0]
	} else {
		iRefIdx = pCurDqLayer.pRefIndex[common.LIST_0]
		pMvSel = pCurDqLayer.pMv[common.LIST_0]
	}
	pRefPics0 := pFilter.pRefPics[common.LIST_0]
	pNzcCur := GetPNzc(pCurDqLayer, iMbXy)
	pNzcNeigh := GetPNzc(pCurDqLayer, iNeighMb)

	if pCurDqLayer.pTransformSize8x8Flag[iMbXy] && pCurDqLayer.pTransformSize8x8Flag[iNeighMb] {
		for i := 0; i < 2; i++ {
			var uiNzc uint8
			for j := 0; uiNzc == 0 && j < 4; j++ {
				uiNzc |= uint8(pNzcCur[pB8x8Idx[j]] | pNzcNeigh[pBn8x8Idx[j]])
			}
			if uiNzc != 0 {
				pBS[i<<1] = 2
				pBS[1+(i<<1)] = 2
			} else {
				ref0 := dbkRefPic(pRefPics0, iRefIdx[iMbXy][pB8x8Idx[0]])
				ref1 := dbkRefPic(pRefPics0, iRefIdx[iNeighMb][pBn8x8Idx[0]])
				v := dbkMbBsMv(ref0, ref1, pCurDqLayer.pDec.pMv[common.LIST_0], iMbXy, iNeighMb, pB8x8Idx[0], pBn8x8Idx[0])
				pBS[i<<1] = v
				pBS[1+(i<<1)] = v
			}
			pB8x8Idx = pB8x8Idx[4:]
			pBn8x8Idx = pBn8x8Idx[4:]
		}
	} else if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
		for i := 0; i < 2; i++ {
			var uiNzc uint8
			for j := 0; uiNzc == 0 && j < 4; j++ {
				uiNzc |= uint8(pNzcCur[pB8x8Idx[j]])
			}
			for j := 0; j < 2; j++ {
				if int32(uiNzc)|int32(pNzcNeigh[pBnIdx[0]]) != 0 {
					pBS[j+(i<<1)] = 2
				} else {
					ref0 := dbkRefPic(pRefPics0, iRefIdx[iMbXy][pB8x8Idx[0]])
					ref1 := dbkRefPic(pRefPics0, iRefIdx[iNeighMb][pBnIdx[0]])
					pBS[j+(i<<1)] = dbkMbBsMv(ref0, ref1, pMvSel, iMbXy, iNeighMb, pB8x8Idx[0], pBnIdx[0])
				}
				pBnIdx = pBnIdx[1:]
			}
			pB8x8Idx = pB8x8Idx[4:]
		}
	} else if pCurDqLayer.pTransformSize8x8Flag[iNeighMb] {
		for i := 0; i < 2; i++ {
			var uiNzc uint8
			for j := 0; uiNzc == 0 && j < 4; j++ {
				uiNzc |= uint8(pNzcNeigh[pBn8x8Idx[j]])
			}
			for j := 0; j < 2; j++ {
				if int32(uiNzc)|int32(pNzcCur[pBIdx[0]]) != 0 {
					pBS[j+(i<<1)] = 2
				} else {
					ref0 := dbkRefPic(pRefPics0, iRefIdx[iMbXy][pBIdx[0]])
					ref1 := dbkRefPic(pRefPics0, iRefIdx[iNeighMb][pBn8x8Idx[0]])
					pBS[j+(i<<1)] = dbkMbBsMv(ref0, ref1, pMvSel, iMbXy, iNeighMb, pBIdx[0], pBn8x8Idx[0])
				}
				pBIdx = pBIdx[1:]
			}
			pBn8x8Idx = pBn8x8Idx[4:]
		}
	} else {
		// only 4x4 transform
		for i := 0; i < 4; i++ {
			if pNzcCur[pBIdx[0]]|pNzcNeigh[pBnIdx[0]] != 0 {
				pBS[i] = 2
			} else {
				ref0 := dbkRefPic(pRefPics0, iRefIdx[iMbXy][pBIdx[0]])
				ref1 := dbkRefPic(pRefPics0, iRefIdx[iNeighMb][pBnIdx[0]])
				pBS[i] = dbkMbBsMv(ref0, ref1, pMvSel, iMbXy, iNeighMb, pBIdx[0], pBnIdx[0])
			}
			pBIdx = pBIdx[1:]
			pBnIdx = pBnIdx[1:]
		}
	}

	return dbkPack4(&pBS)
}

// DeblockingBSliceBsMarginalMBAvcbase ports uint32_t DeblockingBSliceBsMarginalMBAvcbase (PDeblockingFilter pFilter, PDqLayer pCurDqLayer,
// int32_t iEdge, int32_t iNeighMb, int32_t iMbXy).
//
// Signature of the definition (the header declaration lacks pFilter).
func DeblockingBSliceBsMarginalMBAvcbase(pFilter *SDeblockingFilter, pCurDqLayer *SDqLayer, iEdge int32, iNeighMb int32, iMbXy int32) uint32 {
	var pBS [4]uint8
	pBIdx := g_kuiTableBIdx[iEdge][0:]
	pBnIdx := g_kuiTableBIdx[iEdge][4:]
	pB8x8Idx := g_kuiTableB8x8Idx[iEdge][0:]
	pBn8x8Idx := g_kuiTableB8x8Idx[iEdge][8:]
	iRefIdx0 := pCurDqLayer.pDec.pRefIndex[common.LIST_0]
	iRefIdx1 := pCurDqLayer.pDec.pRefIndex[common.LIST_1]
	pRefPics0 := pFilter.pRefPics[common.LIST_0]
	pRefPics1 := pFilter.pRefPics[common.LIST_1]
	var pMv0, pMv1 [][common.MB_BLOCK4x4_NUM][common.MV_A]int16
	if pCurDqLayer.pDec != nil {
		pMv0 = pCurDqLayer.pDec.pMv[common.LIST_0]
		pMv1 = pCurDqLayer.pDec.pMv[common.LIST_1]
	} else {
		pMv0 = pCurDqLayer.pMv[common.LIST_0]
		pMv1 = pCurDqLayer.pMv[common.LIST_1]
	}
	pNzcCur := GetPNzc(pCurDqLayer, iMbXy)
	pNzcNeigh := GetPNzc(pCurDqLayer, iNeighMb)

	// bs computes the boundary strength of a non-coded edge between block
	// p of the current MB and block q of the neighbour MB.
	bs := func(p, q uint8) uint8 {
		ref_p0 := dbkRefPic(pRefPics0, iRefIdx0[iMbXy][p])
		ref_q0 := dbkRefPic(pRefPics0, iRefIdx0[iNeighMb][q])
		ref_p1 := dbkRefPic(pRefPics1, iRefIdx1[iMbXy][p])
		ref_q1 := dbkRefPic(pRefPics1, iRefIdx1[iNeighMb][q])
		if ((ref_p0 == ref_q0) && (ref_p1 == ref_q1)) || ((ref_p0 == ref_q1) && (ref_p1 == ref_q0)) {
			return dbkOnMbBs(ref_p0, ref_q0, ref_p1, ref_q1, pMv0, pMv1, iMbXy, iNeighMb, p, q)
		}
		return 1
	}

	if pCurDqLayer.pTransformSize8x8Flag[iMbXy] && pCurDqLayer.pTransformSize8x8Flag[iNeighMb] {
		for i := 0; i < 2; i++ {
			var uiNzc uint8
			for j := 0; uiNzc == 0 && j < 4; j++ {
				uiNzc |= uint8(pNzcCur[pB8x8Idx[j]] | pNzcNeigh[pBn8x8Idx[j]])
			}
			if uiNzc != 0 {
				pBS[i<<1] = 2
				pBS[1+(i<<1)] = 2
			} else {
				v := bs(pB8x8Idx[0], pBn8x8Idx[0])
				pBS[i<<1] = v
				pBS[1+(i<<1)] = v
			}
			pB8x8Idx = pB8x8Idx[4:]
			pBn8x8Idx = pBn8x8Idx[4:]
		}
	} else if pCurDqLayer.pTransformSize8x8Flag[iMbXy] {
		for i := 0; i < 2; i++ {
			var uiNzc uint8
			for j := 0; uiNzc == 0 && j < 4; j++ {
				uiNzc |= uint8(pNzcCur[pB8x8Idx[j]])
			}
			for j := 0; j < 2; j++ {
				if int32(uiNzc)|int32(pNzcNeigh[pBnIdx[0]]) != 0 {
					pBS[j+(i<<1)] = 2
				} else {
					pBS[j+(i<<1)] = bs(pB8x8Idx[0], pBnIdx[0])
				}
				pBnIdx = pBnIdx[1:]
			}
			pB8x8Idx = pB8x8Idx[4:]
		}
	} else if pCurDqLayer.pTransformSize8x8Flag[iNeighMb] {
		for i := 0; i < 2; i++ {
			var uiNzc uint8
			for j := 0; uiNzc == 0 && j < 4; j++ {
				uiNzc |= uint8(pNzcNeigh[pBn8x8Idx[j]])
			}
			for j := 0; j < 2; j++ {
				if int32(uiNzc)|int32(pNzcCur[pBIdx[0]]) != 0 {
					pBS[j+(i<<1)] = 2
				} else {
					pBS[j+(i<<1)] = bs(pBIdx[0], pBn8x8Idx[0])
				}
				pBIdx = pBIdx[1:]
			}
			pBn8x8Idx = pBn8x8Idx[4:]
		}
	} else {
		// only 4x4 transform
		for i := 0; i < 4; i++ {
			if pNzcCur[pBIdx[0]]|pNzcNeigh[pBnIdx[0]] != 0 {
				pBS[i] = 2
			} else {
				pBS[i] = bs(pBIdx[0], pBnIdx[0])
			}
			pBIdx = pBIdx[1:]
			pBnIdx = pBnIdx[1:]
		}
	}

	return dbkPack4(&pBS)
}

// DeblockingAvailableNoInterlayer ports int32_t DeblockingAvailableNoInterlayer (PDqLayer pCurDqLayer, int32_t iFilterIdc).
func DeblockingAvailableNoInterlayer(pCurDqLayer *SDqLayer, iFilterIdc int32) int32 {
	iMbY := pCurDqLayer.iMbY
	iMbX := pCurDqLayer.iMbX
	iMbXy := pCurDqLayer.iMbXyIndex
	bLeftFlag := false
	bTopFlag := false

	if 2 == iFilterIdc {
		bLeftFlag = (iMbX > 0) && (pCurDqLayer.pSliceIdc[iMbXy] == pCurDqLayer.pSliceIdc[iMbXy-1])
		bTopFlag = (iMbY > 0) && (pCurDqLayer.pSliceIdc[iMbXy] == pCurDqLayer.pSliceIdc[iMbXy-pCurDqLayer.iMbWidth])
	} else { //if ( 0 == iFilterIdc )
		bLeftFlag = (iMbX > 0)
		bTopFlag = (iMbY > 0)
	}
	return (dbkB2i(bLeftFlag) << LEFT_FLAG_BIT) | (dbkB2i(bTopFlag) << TOP_FLAG_BIT)
}

// FilteringEdgeLumaH ports void FilteringEdgeLumaH (SDeblockingFilter* pFilter, uint8_t* pPix, int32_t iStride, uint8_t* pBS).
//
// pPix: (slice, offset) pair. pBS: sub-slice (4 boundary strengths), may be nil.
func FilteringEdgeLumaH(pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	var tc [4]int8

	iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iLumaQP, pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(&tc, iIndexA, pBS, 0)
		pFilter.pLoopf.pfLumaDeblockingLT4Ver(pPix, iPixOff, iStride, iAlpha, iBeta, tc[:])
	}
}

// FilteringEdgeLumaV ports void FilteringEdgeLumaV (SDeblockingFilter* pFilter, uint8_t* pPix, int32_t iStride, uint8_t* pBS).
//
// pPix: (slice, offset) pair. pBS: sub-slice (4 boundary strengths), may be nil.
func FilteringEdgeLumaV(pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	var tc [4]int8

	iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iLumaQP, pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(&tc, iIndexA, pBS, 0)
		pFilter.pLoopf.pfLumaDeblockingLT4Hor(pPix, iPixOff, iStride, iAlpha, iBeta, tc[:])
	}
}

// FilteringEdgeLumaIntraH ports void FilteringEdgeLumaIntraH (SDeblockingFilter* pFilter, uint8_t* pPix, int32_t iStride, uint8_t*
// pBS).
//
// pPix: (slice, offset) pair. pBS: sub-slice, may be nil.
func FilteringEdgeLumaIntraH(pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iLumaQP, pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

	if iAlpha|iBeta != 0 {
		pFilter.pLoopf.pfLumaDeblockingEQ4Ver(pPix, iPixOff, iStride, iAlpha, iBeta)
	}
}

// FilteringEdgeLumaIntraV ports void FilteringEdgeLumaIntraV (SDeblockingFilter* pFilter, uint8_t* pPix, int32_t iStride, uint8_t*
// pBS).
//
// pPix: (slice, offset) pair. pBS: sub-slice, may be nil.
func FilteringEdgeLumaIntraV(pFilter *SDeblockingFilter, pPix []uint8, iPixOff int, iStride int32, pBS []uint8) {
	_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iLumaQP, pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

	if iAlpha|iBeta != 0 {
		pFilter.pLoopf.pfLumaDeblockingEQ4Hor(pPix, iPixOff, iStride, iAlpha, iBeta)
	}
}

// FilteringEdgeChromaH ports void FilteringEdgeChromaH (SDeblockingFilter* pFilter, uint8_t* pPixCb, uint8_t* pPixCr, int32_t
// iStride, uint8_t* pBS).
//
// pPixCb / pPixCr: (slice, offset) pairs. pBS: sub-slice, may be nil.
func FilteringEdgeChromaH(pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	var tc [4]int8
	if pFilter.iChromaQP[0] == pFilter.iChromaQP[1] {
		iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[0], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

		if iAlpha|iBeta != 0 {
			dbkTc0TblLookup(&tc, iIndexA, pBS, 1)
			pFilter.pLoopf.pfChromaDeblockingLT4Ver(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta, tc[:])
		}
	} else {
		for i := 0; i < 2; i++ {
			iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[i], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

			if iAlpha|iBeta != 0 {
				pPixCbCr, iPixCbCrOff := pPixCb, iPixCbOff
				if i != 0 {
					pPixCbCr, iPixCbCrOff = pPixCr, iPixCrOff
				}
				dbkTc0TblLookup(&tc, iIndexA, pBS, 1)
				pFilter.pLoopf.pfChromaDeblockingLT4Ver2(pPixCbCr, iPixCbCrOff, iStride, iAlpha, iBeta, tc[:])
			}
		}
	}
}

// FilteringEdgeChromaV ports void FilteringEdgeChromaV (SDeblockingFilter* pFilter, uint8_t* pPixCb, uint8_t* pPixCr, int32_t
// iStride, uint8_t* pBS).
//
// pPixCb / pPixCr: (slice, offset) pairs. pBS: sub-slice, may be nil.
func FilteringEdgeChromaV(pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	var tc [4]int8
	if pFilter.iChromaQP[0] == pFilter.iChromaQP[1] {
		iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[0], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

		if iAlpha|iBeta != 0 {
			dbkTc0TblLookup(&tc, iIndexA, pBS, 1)
			pFilter.pLoopf.pfChromaDeblockingLT4Hor(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta, tc[:])
		}
	} else {
		for i := 0; i < 2; i++ {
			iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[i], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

			if iAlpha|iBeta != 0 {
				pPixCbCr, iPixCbCrOff := pPixCb, iPixCbOff
				if i != 0 {
					pPixCbCr, iPixCbCrOff = pPixCr, iPixCrOff
				}
				dbkTc0TblLookup(&tc, iIndexA, pBS, 1)
				pFilter.pLoopf.pfChromaDeblockingLT4Hor2(pPixCbCr, iPixCbCrOff, iStride, iAlpha, iBeta, tc[:])
			}
		}
	}
}

// FilteringEdgeChromaIntraH ports void FilteringEdgeChromaIntraH (SDeblockingFilter* pFilter, uint8_t* pPixCb, uint8_t* pPixCr,
// int32_t iStride, uint8_t* pBS).
//
// pPixCb / pPixCr: (slice, offset) pairs. pBS: sub-slice, may be nil.
func FilteringEdgeChromaIntraH(pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	if pFilter.iChromaQP[0] == pFilter.iChromaQP[1] {
		_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[0], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

		if iAlpha|iBeta != 0 {
			pFilter.pLoopf.pfChromaDeblockingEQ4Ver(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta)
		}
	} else {
		for i := 0; i < 2; i++ {
			_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[i], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)

			if iAlpha|iBeta != 0 {
				pPixCbCr, iPixCbCrOff := pPixCb, iPixCbOff
				if i != 0 {
					pPixCbCr, iPixCbCrOff = pPixCr, iPixCrOff
				}
				pFilter.pLoopf.pfChromaDeblockingEQ4Ver2(pPixCbCr, iPixCbCrOff, iStride, iAlpha, iBeta)
			}
		}
	}
}

// FilteringEdgeChromaIntraV ports void FilteringEdgeChromaIntraV (SDeblockingFilter* pFilter, uint8_t* pPixCb, uint8_t* pPixCr,
// int32_t iStride, uint8_t* pBS).
//
// pPixCb / pPixCr: (slice, offset) pairs. pBS: sub-slice, may be nil.
func FilteringEdgeChromaIntraV(pFilter *SDeblockingFilter, pPixCb []uint8, iPixCbOff int, pPixCr []uint8, iPixCrOff int, iStride int32, pBS []uint8) {
	if pFilter.iChromaQP[0] == pFilter.iChromaQP[1] { // QP of cb and cr are the same
		_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[0], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)
		if iAlpha|iBeta != 0 {
			pFilter.pLoopf.pfChromaDeblockingEQ4Hor(pPixCb, iPixCbOff, pPixCr, iPixCrOff, iStride, iAlpha, iBeta)
		}
	} else {
		for i := 0; i < 2; i++ {
			_, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[i], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)
			if iAlpha|iBeta != 0 {
				pPixCbCr, iPixCbCrOff := pPixCb, iPixCbOff
				if i != 0 {
					pPixCbCr, iPixCbCrOff = pPixCr, iPixCrOff
				}
				pFilter.pLoopf.pfChromaDeblockingEQ4Hor2(pPixCbCr, iPixCbCrOff, iStride, iAlpha, iBeta)
			}
		}
	}
}

func DeblockingInterMb(pCurDqLayer *SDqLayer, pFilter *SDeblockingFilter, nBS *[2][4][4]uint8,
	iBoundryFlag int32) {
	iMbXyIndex := pCurDqLayer.iMbXyIndex
	iMbX := pCurDqLayer.iMbX
	iMbY := pCurDqLayer.iMbY

	iCurLumaQp := int32(pCurDqLayer.pLumaQp[iMbXyIndex])
	pCurChromaQp := &pCurDqLayer.pChromaQp[iMbXyIndex]
	iLineSize := pFilter.iCsStride[0]
	iLineSizeUV := pFilter.iCsStride[1]
	ls := int(iLineSize)
	lsUV := int(iLineSizeUV)

	pDestY := pFilter.pCsData[0]
	pDestCb := pFilter.pCsData[1]
	pDestCr := pFilter.pCsData[2]
	iDestY := pFilter.iCsDataOff[0] + int((iMbY*iLineSize+iMbX)<<4)
	iDestCb := pFilter.iCsDataOff[1] + int((iMbY*iLineSizeUV+iMbX)<<3)
	iDestCr := pFilter.iCsDataOff[2] + int((iMbY*iLineSizeUV+iMbX)<<3)
	bT8x8 := pCurDqLayer.pTransformSize8x8Flag[iMbXyIndex]

	//Vertical margin
	if iBoundryFlag&LEFT_FLAG_MASK != 0 {
		iLeftXyIndex := iMbXyIndex - 1
		pFilter.iLumaQP = int8((iCurLumaQp + int32(pCurDqLayer.pLumaQp[iLeftXyIndex]) + 1) >> 1)
		for i := 0; i < 2; i++ {
			pFilter.iChromaQP[i] = int8((int32(pCurChromaQp[i]) + int32(pCurDqLayer.pChromaQp[iLeftXyIndex][i]) + 1) >> 1)
		}
		if nBS[0][0][0] == 0x04 {
			FilteringEdgeLumaIntraV(pFilter, pDestY, iDestY, iLineSize, nil)
			FilteringEdgeChromaIntraV(pFilter, pDestCb, iDestCb, pDestCr, iDestCr, iLineSizeUV, nil)
		} else {
			if dbkIsNonZero32(&nBS[0][0]) {
				FilteringEdgeLumaV(pFilter, pDestY, iDestY, iLineSize, nBS[0][0][:])
				FilteringEdgeChromaV(pFilter, pDestCb, iDestCb, pDestCr, iDestCr, iLineSizeUV, nBS[0][0][:])
			}
		}
	}

	pFilter.iLumaQP = int8(iCurLumaQp)
	pFilter.iChromaQP[0] = pCurChromaQp[0]
	pFilter.iChromaQP[1] = pCurChromaQp[1]

	if dbkIsNonZero32(&nBS[0][1]) && !bT8x8 {
		FilteringEdgeLumaV(pFilter, pDestY, iDestY+(1<<2), iLineSize, nBS[0][1][:])
	}

	if dbkIsNonZero32(&nBS[0][2]) {
		FilteringEdgeLumaV(pFilter, pDestY, iDestY+(2<<2), iLineSize, nBS[0][2][:])
		FilteringEdgeChromaV(pFilter, pDestCb, iDestCb+(2<<1), pDestCr, iDestCr+(2<<1), iLineSizeUV, nBS[0][2][:])
	}

	if dbkIsNonZero32(&nBS[0][3]) && !bT8x8 {
		FilteringEdgeLumaV(pFilter, pDestY, iDestY+(3<<2), iLineSize, nBS[0][3][:])
	}

	if iBoundryFlag&TOP_FLAG_MASK != 0 {
		iTopXyIndex := iMbXyIndex - pCurDqLayer.iMbWidth
		pFilter.iLumaQP = int8((iCurLumaQp + int32(pCurDqLayer.pLumaQp[iTopXyIndex]) + 1) >> 1)
		for i := 0; i < 2; i++ {
			pFilter.iChromaQP[i] = int8((int32(pCurChromaQp[i]) + int32(pCurDqLayer.pChromaQp[iTopXyIndex][i]) + 1) >> 1)
		}

		if nBS[1][0][0] == 0x04 {
			FilteringEdgeLumaIntraH(pFilter, pDestY, iDestY, iLineSize, nil)
			FilteringEdgeChromaIntraH(pFilter, pDestCb, iDestCb, pDestCr, iDestCr, iLineSizeUV, nil)
		} else {
			if dbkIsNonZero32(&nBS[1][0]) {
				FilteringEdgeLumaH(pFilter, pDestY, iDestY, iLineSize, nBS[1][0][:])
				FilteringEdgeChromaH(pFilter, pDestCb, iDestCb, pDestCr, iDestCr, iLineSizeUV, nBS[1][0][:])
			}
		}
	}

	pFilter.iLumaQP = int8(iCurLumaQp)
	pFilter.iChromaQP[0] = pCurChromaQp[0]
	pFilter.iChromaQP[1] = pCurChromaQp[1]

	if dbkIsNonZero32(&nBS[1][1]) && !bT8x8 {
		FilteringEdgeLumaH(pFilter, pDestY, iDestY+(1<<2)*ls, iLineSize, nBS[1][1][:])
	}

	if dbkIsNonZero32(&nBS[1][2]) {
		FilteringEdgeLumaH(pFilter, pDestY, iDestY+(2<<2)*ls, iLineSize, nBS[1][2][:])
		FilteringEdgeChromaH(pFilter, pDestCb, iDestCb+(2<<1)*lsUV, pDestCr, iDestCr+(2<<1)*lsUV, iLineSizeUV,
			nBS[1][2][:])
	}

	if dbkIsNonZero32(&nBS[1][3]) && !bT8x8 {
		FilteringEdgeLumaH(pFilter, pDestY, iDestY+(3<<2)*ls, iLineSize, nBS[1][3][:])
	}
}

// FilteringEdgeLumaHV ports void FilteringEdgeLumaHV (PDqLayer pCurDqLayer, PDeblockingFilter pFilter, int32_t iBoundryFlag).
func FilteringEdgeLumaHV(pCurDqLayer *SDqLayer, pFilter *SDeblockingFilter, iBoundryFlag int32) {
	iMbXyIndex := pCurDqLayer.iMbXyIndex
	iMbX := pCurDqLayer.iMbX
	iMbY := pCurDqLayer.iMbY
	iMbWidth := pCurDqLayer.iMbWidth
	iLineSize := pFilter.iCsStride[0]
	ls := int(iLineSize)

	var iTc [4]int8
	uiBSx4 := [4]uint8{3, 3, 3, 3} // *(uint32_t*)uiBSx4 = 0x03030303

	pDestY := pFilter.pCsData[0]
	iDestY := pFilter.iCsDataOff[0] + int((iMbY*iLineSize+iMbX)<<4)
	iCurQp := int32(pCurDqLayer.pLumaQp[iMbXyIndex])
	bT8x8 := pCurDqLayer.pTransformSize8x8Flag[iMbXyIndex]

	// luma v
	if iBoundryFlag&LEFT_FLAG_MASK != 0 {
		pFilter.iLumaQP = int8((iCurQp + int32(pCurDqLayer.pLumaQp[iMbXyIndex-1]) + 1) >> 1)
		FilteringEdgeLumaIntraV(pFilter, pDestY, iDestY, iLineSize, nil)
	}

	pFilter.iLumaQP = int8(iCurQp)
	iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iLumaQP, pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)
	if iAlpha|iBeta != 0 {
		dbkTc0TblLookup(&iTc, iIndexA, uiBSx4[:], 0)

		if !bT8x8 {
			pFilter.pLoopf.pfLumaDeblockingLT4Hor(pDestY, iDestY+(1<<2), iLineSize, iAlpha, iBeta, iTc[:])
		}

		pFilter.pLoopf.pfLumaDeblockingLT4Hor(pDestY, iDestY+(2<<2), iLineSize, iAlpha, iBeta, iTc[:])

		if !bT8x8 {
			pFilter.pLoopf.pfLumaDeblockingLT4Hor(pDestY, iDestY+(3<<2), iLineSize, iAlpha, iBeta, iTc[:])
		}
	}

	// luma h
	if iBoundryFlag&TOP_FLAG_MASK != 0 {
		pFilter.iLumaQP = int8((iCurQp + int32(pCurDqLayer.pLumaQp[iMbXyIndex-iMbWidth]) + 1) >> 1)
		FilteringEdgeLumaIntraH(pFilter, pDestY, iDestY, iLineSize, nil)
	}

	pFilter.iLumaQP = int8(iCurQp)
	if iAlpha|iBeta != 0 {
		if !bT8x8 {
			pFilter.pLoopf.pfLumaDeblockingLT4Ver(pDestY, iDestY+(1<<2)*ls, iLineSize, iAlpha, iBeta, iTc[:])
		}

		pFilter.pLoopf.pfLumaDeblockingLT4Ver(pDestY, iDestY+(2<<2)*ls, iLineSize, iAlpha, iBeta, iTc[:])

		if !bT8x8 {
			pFilter.pLoopf.pfLumaDeblockingLT4Ver(pDestY, iDestY+(3<<2)*ls, iLineSize, iAlpha, iBeta, iTc[:])
		}
	}
}

// FilteringEdgeChromaHV ports void FilteringEdgeChromaHV (PDqLayer pCurDqLayer, PDeblockingFilter pFilter, int32_t iBoundryFlag).
func FilteringEdgeChromaHV(pCurDqLayer *SDqLayer, pFilter *SDeblockingFilter, iBoundryFlag int32) {
	iMbXyIndex := pCurDqLayer.iMbXyIndex
	iMbX := pCurDqLayer.iMbX
	iMbY := pCurDqLayer.iMbY
	iMbWidth := pCurDqLayer.iMbWidth
	iLineSize := pFilter.iCsStride[1]
	ls := int(iLineSize)

	var iTc [4]int8
	uiBSx4 := [4]uint8{3, 3, 3, 3} // *(uint32_t*)uiBSx4 = 0x03030303

	pDestCb := pFilter.pCsData[1]
	pDestCr := pFilter.pCsData[2]
	iDestCb := pFilter.iCsDataOff[1] + int((iMbY*iLineSize+iMbX)<<3)
	iDestCr := pFilter.iCsDataOff[2] + int((iMbY*iLineSize+iMbX)<<3)
	pCurQp := &pCurDqLayer.pChromaQp[iMbXyIndex]

	// chroma v
	if iBoundryFlag&LEFT_FLAG_MASK != 0 {
		for i := 0; i < 2; i++ {
			pFilter.iChromaQP[i] = int8((int32(pCurQp[i]) + int32(pCurDqLayer.pChromaQp[iMbXyIndex-1][i]) + 1) >> 1)
		}
		FilteringEdgeChromaIntraV(pFilter, pDestCb, iDestCb, pDestCr, iDestCr, iLineSize, nil)
	}

	pFilter.iChromaQP[0] = pCurQp[0]
	pFilter.iChromaQP[1] = pCurQp[1]
	if pFilter.iChromaQP[0] == pFilter.iChromaQP[1] {
		iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[0], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)
		if iAlpha|iBeta != 0 {
			dbkTc0TblLookup(&iTc, iIndexA, uiBSx4[:], 1)
			pFilter.pLoopf.pfChromaDeblockingLT4Hor(pDestCb, iDestCb+(2<<1), pDestCr, iDestCr+(2<<1), iLineSize, iAlpha, iBeta, iTc[:])
		}
	} else {
		for i := 0; i < 2; i++ {
			iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[i], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)
			if iAlpha|iBeta != 0 {
				pDestCbCr, iDestCbCr := pDestCb, iDestCb+(2<<1)
				if i != 0 {
					pDestCbCr, iDestCbCr = pDestCr, iDestCr+(2<<1)
				}
				dbkTc0TblLookup(&iTc, iIndexA, uiBSx4[:], 1)
				pFilter.pLoopf.pfChromaDeblockingLT4Hor2(pDestCbCr, iDestCbCr, iLineSize, iAlpha, iBeta, iTc[:])
			}
		}
	}

	// chroma h

	if iBoundryFlag&TOP_FLAG_MASK != 0 {
		for i := 0; i < 2; i++ {
			pFilter.iChromaQP[i] = int8((int32(pCurQp[i]) + int32(pCurDqLayer.pChromaQp[iMbXyIndex-iMbWidth][i]) + 1) >> 1)
		}
		FilteringEdgeChromaIntraH(pFilter, pDestCb, iDestCb, pDestCr, iDestCr, iLineSize, nil)
	}

	pFilter.iChromaQP[0] = pCurQp[0]
	pFilter.iChromaQP[1] = pCurQp[1]

	if pFilter.iChromaQP[0] == pFilter.iChromaQP[1] {
		iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[0], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)
		if iAlpha|iBeta != 0 {
			dbkTc0TblLookup(&iTc, iIndexA, uiBSx4[:], 1)
			pFilter.pLoopf.pfChromaDeblockingLT4Ver(pDestCb, iDestCb+(2<<1)*ls, pDestCr, iDestCr+(2<<1)*ls, iLineSize,
				iAlpha, iBeta, iTc[:])
		}
	} else {
		for i := 0; i < 2; i++ {
			iIndexA, iAlpha, iBeta := dbkGetAlphaBetaFromQp(pFilter.iChromaQP[i], pFilter.iSliceAlphaC0Offset, pFilter.iSliceBetaOffset)
			if iAlpha|iBeta != 0 {
				dbkTc0TblLookup(&iTc, iIndexA, uiBSx4[:], 1)
				pDestCbCr, iDestCbCr := pDestCb, iDestCb+(2<<1)*ls
				if i != 0 {
					pDestCbCr, iDestCbCr = pDestCr, iDestCr+(2<<1)*ls
				}
				pFilter.pLoopf.pfChromaDeblockingLT4Ver2(pDestCbCr, iDestCbCr, iLineSize,
					iAlpha, iBeta, iTc[:])
			}
		}
	}
}

// DeblockingIntraMb: merge h&v lookup table operation to save performance.
func DeblockingIntraMb(pCurDqLayer *SDqLayer, pFilter *SDeblockingFilter, iBoundryFlag int32) {
	FilteringEdgeLumaHV(pCurDqLayer, pFilter, iBoundryFlag)
	FilteringEdgeChromaHV(pCurDqLayer, pFilter, iBoundryFlag)
}

// WelsDeblockingMb ports void WelsDeblockingMb (PDqLayer pCurDqLayer, PDeblockingFilter pFilter, int32_t iBoundryFlag).
func WelsDeblockingMb(pCurDqLayer *SDqLayer, pFilter *SDeblockingFilter, iBoundryFlag int32) {
	var nBS [2][4][4]uint8

	iMbXyIndex := pCurDqLayer.iMbXyIndex
	mbType := func(i int32) uint32 {
		if pCurDqLayer.pDec != nil {
			return pCurDqLayer.pDec.pMbType[i]
		}
		return pCurDqLayer.pMbType[i]
	}
	iCurMbType := mbType(iMbXyIndex)
	var iMbNb int32

	pSlice := &pCurDqLayer.sLayerInfo.sSliceInLayer
	pSliceHeader := &pSlice.sSliceHeaderExt.sSliceHeader
	bBSlice := pSliceHeader.eSliceType == common.B_SLICE

	switch iCurMbType {
	case common.MB_TYPE_INTRA4x4, common.MB_TYPE_INTRA8x8, common.MB_TYPE_INTRA16x16, common.MB_TYPE_INTRA_PCM:
		DeblockingIntraMb(pCurDqLayer, pFilter, iBoundryFlag)
	default:
		if iBoundryFlag&LEFT_FLAG_MASK != 0 {
			iMbNb = iMbXyIndex - 1
			uiMbType := mbType(iMbNb)
			var v uint32
			if common.IS_INTRA(uiMbType) {
				v = 0x04040404
			} else if bBSlice {
				v = DeblockingBSliceBsMarginalMBAvcbase(pFilter, pCurDqLayer, 0, iMbNb, iMbXyIndex)
			} else {
				v = DeblockingBsMarginalMBAvcbase(pFilter, pCurDqLayer, 0, iMbNb, iMbXyIndex)
			}
			dbkSt32(&nBS[0][0], v)
		} else {
			dbkSt32(&nBS[0][0], 0)
		}
		if iBoundryFlag&TOP_FLAG_MASK != 0 {
			iMbNb = iMbXyIndex - pCurDqLayer.iMbWidth
			uiMbType := mbType(iMbNb)
			var v uint32
			if common.IS_INTRA(uiMbType) {
				v = 0x04040404
			} else if bBSlice {
				v = DeblockingBSliceBsMarginalMBAvcbase(pFilter, pCurDqLayer, 1, iMbNb, iMbXyIndex)
			} else {
				v = DeblockingBsMarginalMBAvcbase(pFilter, pCurDqLayer, 1, iMbNb, iMbXyIndex)
			}
			dbkSt32(&nBS[1][0], v)
		} else {
			dbkSt32(&nBS[1][0], 0)
		}
		//SKIP MB_16x16 or others
		if common.IS_SKIP(iCurMbType) {
			nBS[0][1], nBS[0][2], nBS[0][3] = [4]uint8{}, [4]uint8{}, [4]uint8{}
			nBS[1][1], nBS[1][2], nBS[1][3] = [4]uint8{}, [4]uint8{}, [4]uint8{}
		} else {
			if common.IS_INTER_16x16(iCurMbType) {
				if !pCurDqLayer.pTransformSize8x8Flag[pCurDqLayer.iMbXyIndex] {
					DeblockingBSInsideMBAvsbase(GetPNzc(pCurDqLayer, iMbXyIndex), &nBS, 1)
				} else {
					DeblockingBSInsideMBAvsbase8x8(GetPNzc(pCurDqLayer, iMbXyIndex), &nBS, 1)
				}
			} else {
				if bBSlice {
					DeblockingBSliceBSInsideMBNormal(pFilter, pCurDqLayer, &nBS, GetPNzc(pCurDqLayer, iMbXyIndex), iMbXyIndex)
				} else {
					DeblockingBSInsideMBNormal(pFilter, pCurDqLayer, &nBS, GetPNzc(pCurDqLayer, iMbXyIndex), iMbXyIndex)
				}
			}
		}
		DeblockingInterMb(pCurDqLayer, pFilter, &nBS, iBoundryFlag)
	}
}

// dbkSetupFilter fills the filter parameters (Step1 of WelsDeblockingFilterSlice /
// WelsDeblockingInitFilter) after the memset.
func dbkSetupFilter(pCtx *SWelsDecoderContext, pFilter *SDeblockingFilter) {
	pCurDqLayer := pCtx.pCurDqLayer
	pSliceHeaderExt := &pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt

	*pFilter = SDeblockingFilter{}

	/* Step1: parameters set */
	for i := 0; i < 3; i++ {
		pFilter.pCsData[i] = pCtx.pDec.pData[i]
		pFilter.iCsDataOff[i] = pCtx.pDec.iDataOff[i]
	}

	pFilter.iCsStride[0] = pCtx.pDec.iLinesize[0]
	pFilter.iCsStride[1] = pCtx.pDec.iLinesize[1]

	pFilter.eSliceType = common.EWelsSliceType(pCurDqLayer.sLayerInfo.sSliceInLayer.eSliceType)

	pFilter.iSliceAlphaC0Offset = int8(pSliceHeaderExt.sSliceHeader.iSliceAlphaC0Offset)
	pFilter.iSliceBetaOffset = int8(pSliceHeaderExt.sSliceHeader.iSliceBetaOffset)

	pFilter.pLoopf = &pCtx.sDeblockingFunc
	pFilter.pRefPics[0] = pCtx.sRefPic.pRefList[0][:]
	pFilter.pRefPics[1] = pCtx.sRefPic.pRefList[1][:]
}

// WelsDeblockingFilterSlice ports void WelsDeblockingFilterSlice (PWelsDecoderContext pCtx, PDeblockingFilterMbFunc pDeblockMb).
//
// AVC slice deblocking filtering target layer.
func WelsDeblockingFilterSlice(pCtx *SWelsDecoderContext, pDeblockMb PDeblockingFilterMbFunc) {
	pCurDqLayer := pCtx.pCurDqLayer
	pSliceHeaderExt := &pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt
	iMbWidth := pCurDqLayer.iMbWidth
	iTotalMbCount := int32(pSliceHeaderExt.sSliceHeader.pSps.uiTotalMbCount)

	var pFilter SDeblockingFilter
	pFmo := pCtx.pFmo
	var iNextMbXyIndex int32
	iTotalNumMb := pCurDqLayer.sLayerInfo.sSliceInLayer.iTotalMbInCurSlice
	iCountNumMb := int32(0)
	var iBoundryFlag int32
	iFilterIdc := int32(pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.uiDisableDeblockingFilterIdc)

	/* Step1: parameters set */
	dbkSetupFilter(pCtx, &pFilter)

	/* Step2: macroblock deblocking */
	if 0 == iFilterIdc || 2 == iFilterIdc {
		iNextMbXyIndex = pSliceHeaderExt.sSliceHeader.iFirstMbInSlice
		pCurDqLayer.iMbX = iNextMbXyIndex % iMbWidth
		pCurDqLayer.iMbY = iNextMbXyIndex / iMbWidth
		pCurDqLayer.iMbXyIndex = iNextMbXyIndex

		for {
			iBoundryFlag = DeblockingAvailableNoInterlayer(pCurDqLayer, iFilterIdc)

			pDeblockMb(pCurDqLayer, &pFilter, iBoundryFlag)

			iCountNumMb++
			if iCountNumMb >= iTotalNumMb {
				break
			}

			if pSliceHeaderExt.sSliceHeader.pPps.uiNumSliceGroups > 1 {
				iNextMbXyIndex = FmoNextMb(pFmo, iNextMbXyIndex)
			} else {
				iNextMbXyIndex++
			}
			if -1 == iNextMbXyIndex || iNextMbXyIndex >= iTotalMbCount { // slice group boundary or end of a frame
				break
			}

			pCurDqLayer.iMbX = iNextMbXyIndex % iMbWidth
			pCurDqLayer.iMbY = iNextMbXyIndex / iMbWidth
			pCurDqLayer.iMbXyIndex = iNextMbXyIndex
		}
	}
}

// WelsDeblockingInitFilter ports void WelsDeblockingInitFilter (PWelsDecoderContext pCtx, SDeblockingFilter& pFilter, int32_t&
// iFilterIdc).
//
// AVC slice init deblocking filtering target layer. C++ references
// SDeblockingFilter& / int32_t& -> pointers.
func WelsDeblockingInitFilter(pCtx *SWelsDecoderContext, pFilter *SDeblockingFilter, iFilterIdc *int32) {
	pCurDqLayer := pCtx.pCurDqLayer

	*pFilter = SDeblockingFilter{}

	*iFilterIdc = int32(pCurDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.uiDisableDeblockingFilterIdc)

	/* Step1: parameters set */
	dbkSetupFilter(pCtx, pFilter)
}

// WelsDeblockingFilterMB ports void WelsDeblockingFilterMB (PDqLayer pCurDqLayer, SDeblockingFilter& pFilter, int32_t& iFilterIdc,
// PDeblockingFilterMbFunc pDeblockMb).
//
// AVC MB deblocking filtering target layer. C++ references -> pointers.
func WelsDeblockingFilterMB(pCurDqLayer *SDqLayer, pFilter *SDeblockingFilter, iFilterIdc *int32, pDeblockMb PDeblockingFilterMbFunc) {
	/* macroblock deblocking */
	if 0 == *iFilterIdc || 2 == *iFilterIdc {
		iBoundryFlag := DeblockingAvailableNoInterlayer(pCurDqLayer, *iFilterIdc)
		pDeblockMb(pCurDqLayer, pFilter, iBoundryFlag)
	}
}

// DeblockingInit ports void DeblockingInit (SDeblockingFunc* pFunc, int32_t iCpu).
//
// Only the _c functions of package common are installed.
func DeblockingInit(pFunc *SDeblockingFunc, iCpu int32) {
	pFunc.pfLumaDeblockingLT4Ver = common.DeblockLumaLt4V_c
	pFunc.pfLumaDeblockingEQ4Ver = common.DeblockLumaEq4V_c
	pFunc.pfLumaDeblockingLT4Hor = common.DeblockLumaLt4H_c
	pFunc.pfLumaDeblockingEQ4Hor = common.DeblockLumaEq4H_c

	pFunc.pfChromaDeblockingLT4Ver = common.DeblockChromaLt4V_c
	pFunc.pfChromaDeblockingEQ4Ver = common.DeblockChromaEq4V_c
	pFunc.pfChromaDeblockingLT4Hor = common.DeblockChromaLt4H_c
	pFunc.pfChromaDeblockingEQ4Hor = common.DeblockChromaEq4H_c

	pFunc.pfChromaDeblockingLT4Ver2 = common.DeblockChromaLt4V2_c
	pFunc.pfChromaDeblockingEQ4Ver2 = common.DeblockChromaEq4V2_c
	pFunc.pfChromaDeblockingLT4Hor2 = common.DeblockChromaLt4H2_c
	pFunc.pfChromaDeblockingEQ4Hor2 = common.DeblockChromaEq4H2_c
}
