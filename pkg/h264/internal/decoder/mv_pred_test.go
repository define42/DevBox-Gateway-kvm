// Port of test/decoder/DecUT_PredMv.cpp.

package decoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const testRandMax = 0x7FFFFFFF

// Anchor functions

// cache element equal to 30
var g_kuiAnchorCache30ScanIdx = [16]uint8{ //mv or ref_index cache scan index, 4*4 block as basic unit
	7, 8, 13, 14,
	9, 10, 15, 16,
	19, 20, 25, 26,
	21, 22, 27, 28,
}

type SAnchorMvPred struct {
	iMvArray     [2][30][2]int16
	iRefIdxArray [2][30]int8
	iPartIdx     int32
	iPartWidth   int32
	iRef         int32
	iMvp         [2]int16
}

func AnchorPredMv(iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8,
	iPartIdx int32, iPartWidth int32, iRef int8, iMVP *[2]int16) {
	kuiLeftIdx := g_kuiAnchorCache30ScanIdx[iPartIdx] - 1
	kuiTopIdx := g_kuiAnchorCache30ScanIdx[iPartIdx] - 6
	kuiRightTopIdx := kuiTopIdx + uint8(iPartWidth)
	kuiLeftTopIdx := kuiTopIdx - 1
	kiLeftRef := iRefIndex[0][kuiLeftIdx]
	kiTopRef := iRefIndex[0][kuiTopIdx]
	kiRightTopRef := iRefIndex[0][kuiRightTopIdx]
	kiLeftTopRef := iRefIndex[0][kuiLeftTopIdx]
	iDiagonalRef := kiRightTopRef
	var iMatchRef int8

	iAMV := iMotionVector[0][kuiLeftIdx]
	iBMV := iMotionVector[0][kuiTopIdx]
	iCMV := iMotionVector[0][kuiRightTopIdx]

	if REF_NOT_AVAIL == iDiagonalRef {
		iDiagonalRef = kiLeftTopRef
		iCMV = iMotionVector[0][kuiLeftTopIdx]
	}

	iMatchRef = b2i8(iRef == kiLeftRef) + b2i8(iRef == kiTopRef) + b2i8(iRef == iDiagonalRef)

	if (REF_NOT_AVAIL == kiTopRef) && (REF_NOT_AVAIL == iDiagonalRef) && (kiLeftRef >= REF_NOT_IN_LIST) {
		*iMVP = iAMV
		return
	}

	if 1 == iMatchRef {
		if iRef == kiLeftRef {
			*iMVP = iAMV
		} else if iRef == kiTopRef {
			*iMVP = iBMV
		} else {
			*iMVP = iCMV
		}
	} else {
		iMVP[0] = int16(common.WelsMedian(int32(iAMV[0]), int32(iBMV[0]), int32(iCMV[0])))
		iMVP[1] = int16(common.WelsMedian(int32(iAMV[1]), int32(iBMV[1]), int32(iCMV[1])))
	}
}

func AnchorPredInter8x16Mv(iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8,
	iPartIdx int32, iRef int8, iMVP *[2]int16) {
	if 0 == iPartIdx {
		kiLeftRef := iRefIndex[0][6]
		if iRef == kiLeftRef {
			*iMVP = iMotionVector[0][6]
			return
		}
	} else { // 4 == iPartIdx
		iDiagonalRef := iRefIndex[0][5] //top-right
		index := 5
		if REF_NOT_AVAIL == iDiagonalRef {
			iDiagonalRef = iRefIndex[0][2] //top-left for 8*8 block(index 1)
			index = 2
		}
		if iRef == iDiagonalRef {
			*iMVP = iMotionVector[0][index]
			return
		}
	}

	AnchorPredMv(iMotionVector, iRefIndex, iPartIdx, 2, iRef, iMVP)
}

func AnchorPredInter16x8Mv(iMotionVector *[common.LIST_A][30][common.MV_A]int16, iRefIndex *[common.LIST_A][30]int8,
	iPartIdx int32, iRef int8, iMVP *[2]int16) {
	if 0 == iPartIdx {
		kiTopRef := iRefIndex[0][1]
		if iRef == kiTopRef {
			*iMVP = iMotionVector[0][1]
			return
		}
	} else { // 8 == iPartIdx
		kiLeftRef := iRefIndex[0][18]
		if iRef == kiLeftRef {
			*iMVP = iMotionVector[0][18]
			return
		}
	}

	AnchorPredMv(iMotionVector, iRefIndex, iPartIdx, 4, iRef, iMVP)
}

// Input structure for test
type SWelsMvPred = SAnchorMvPred

// mok input data
func AssignMvInputData(rnd *rand.Rand, pAncMvPred *SAnchorMvPred) {
	//fill MV data and refIdx
	for i := 0; i < 2; i++ {
		for j := 0; j < 30; j++ {
			for k := 0; k < 2; k++ {
				pAncMvPred.iMvArray[i][j][k] = int16(rnd.Int31() - testRandMax/2)
			}
			pAncMvPred.iRefIdxArray[i][j] = int8((rnd.Int31() % 18) - 2) //-2 ~ 15. 8x8 may have different values, but it matters nothing
		}
	}
}

func CopyMvInputData(pDstMvPred *SWelsMvPred, pSrcMvPred *SAnchorMvPred) {
	pDstMvPred.iMvArray = pSrcMvPred.iMvArray
	pDstMvPred.iRefIdxArray = pSrcMvPred.iRefIdxArray
}

func TestPredMvTest_PredMv(t *testing.T) {
	rnd := rand.New(rand.NewSource(1))
	var sWelsMvPred SWelsMvPred
	var sAncMvPred SAnchorMvPred
	const kiRandTime = 100

	test := func(iIndex, iBlockWidth int32, iRef int8) {
		AssignMvInputData(rnd, &sAncMvPred)
		CopyMvInputData(&sWelsMvPred, &sAncMvPred)
		AnchorPredMv(&sAncMvPred.iMvArray, &sAncMvPred.iRefIdxArray, iIndex, iBlockWidth, iRef, &sAncMvPred.iMvp)
		PredMv(&sWelsMvPred.iMvArray, &sWelsMvPred.iRefIdxArray, common.LIST_0, iIndex, iBlockWidth, iRef, &sWelsMvPred.iMvp)
		if sAncMvPred.iMvp != sWelsMvPred.iMvp {
			t.Fatalf("PredMv mismatch idx=%d w=%d ref=%d: anchor %v, wels %v", iIndex, iBlockWidth, iRef, sAncMvPred.iMvp, sWelsMvPred.iMvp)
		}
	}
	randRef := func() int8 { return int8((rnd.Int31() % 18) - 2) } //-2~15

	//test specific input: 16x16
	for i := 0; i < kiRandTime; i++ {
		test(0, 4, randRef())
	}
	//test specific input: 16x8
	for i := 0; i < kiRandTime; i++ {
		iIndex := (rnd.Int31() & 1) << 3 //0,8
		test(iIndex, 4, randRef())
	}
	//test specific input: 8x16
	for i := 0; i < kiRandTime; i++ {
		iIndex := (rnd.Int31() & 1) << 2 //0,4
		test(iIndex, 2, randRef())
	}
	//test specific input: 8x8
	for i := 0; i < kiRandTime; i++ {
		iIndex := (rnd.Int31() & 3) << 2 //0,4,8,12
		test(iIndex, 2, randRef())
	}
	//test specific input: 4x4
	for i := 0; i < kiRandTime; i++ {
		iIndex := rnd.Int31() & 0x0f //0~15
		test(iIndex, 1, randRef())
	}
}

func TestPredMvTest_PredInter16x8Mv(t *testing.T) {
	rnd := rand.New(rand.NewSource(2))
	var sWelsMvPred SWelsMvPred
	var sAncMvPred SAnchorMvPred
	const kiRandTime = 100

	for i := 0; i < kiRandTime; i++ {
		iIndex := (rnd.Int31() & 1) << 3     //0, 8
		iRef := int8((rnd.Int31() % 18) - 2) //-2~15
		AssignMvInputData(rnd, &sAncMvPred)
		CopyMvInputData(&sWelsMvPred, &sAncMvPred)
		AnchorPredInter16x8Mv(&sAncMvPred.iMvArray, &sAncMvPred.iRefIdxArray, iIndex, iRef, &sAncMvPred.iMvp)
		PredInter16x8Mv(&sWelsMvPred.iMvArray, &sWelsMvPred.iRefIdxArray, common.LIST_0, iIndex, iRef, &sWelsMvPred.iMvp)
		if sAncMvPred.iMvp != sWelsMvPred.iMvp {
			t.Fatalf("PredInter16x8Mv mismatch: anchor %v, wels %v", sAncMvPred.iMvp, sWelsMvPred.iMvp)
		}
	}
}

func TestPredMvTest_PredInter8x16Mv(t *testing.T) {
	rnd := rand.New(rand.NewSource(3))
	var sWelsMvPred SWelsMvPred
	var sAncMvPred SAnchorMvPred
	const kiRandTime = 100

	for i := 0; i < kiRandTime; i++ {
		iIndex := (rnd.Int31() & 1) << 2     //0, 4
		iRef := int8((rnd.Int31() % 18) - 2) //-2~15
		AssignMvInputData(rnd, &sAncMvPred)
		CopyMvInputData(&sWelsMvPred, &sAncMvPred)
		AnchorPredInter8x16Mv(&sAncMvPred.iMvArray, &sAncMvPred.iRefIdxArray, iIndex, iRef, &sAncMvPred.iMvp)
		PredInter8x16Mv(&sWelsMvPred.iMvArray, &sWelsMvPred.iRefIdxArray, common.LIST_0, iIndex, iRef, &sWelsMvPred.iMvp)
		if sAncMvPred.iMvp != sWelsMvPred.iMvp {
			t.Fatalf("PredInter8x16Mv mismatch: anchor %v, wels %v", sAncMvPred.iMvp, sWelsMvPred.iMvp)
		}
	}
}

func AnchorPredPSkipMvFromNeighbor(pCurLayer *SDqLayer, iMvp *[2]int16) {
	var bTopAvail, bLeftTopAvail, bRightTopAvail, bLeftAvail bool

	var iCurSliceIdc, iTopSliceIdc, iLeftTopSliceIdc, iRightTopSliceIdc, iLeftSliceIdc int32
	var iLeftTopType, iRightTopType, iTopType, iLeftType int32
	var iCurX, iCurY, iCurXy, iLeftXy, iTopXy, iLeftTopXy, iRightTopXy int32

	var iLeftRef, iTopRef, iRightTopRef, iLeftTopRef, iDiagonalRef, iMatchRef int8
	var iMvA, iMvB, iMvC, iMvD [2]int16

	iCurXy = pCurLayer.iMbXyIndex
	iCurX = pCurLayer.iMbX
	iCurY = pCurLayer.iMbY
	iCurSliceIdc = pCurLayer.pSliceIdc[iCurXy]

	if iCurX != 0 {
		iLeftXy = iCurXy - 1
		iLeftSliceIdc = pCurLayer.pSliceIdc[iLeftXy]
		bLeftAvail = (iLeftSliceIdc == iCurSliceIdc)
	} else {
		bLeftAvail = false
		bLeftTopAvail = false
	}

	if iCurY != 0 {
		iTopXy = iCurXy - pCurLayer.iMbWidth
		iTopSliceIdc = pCurLayer.pSliceIdc[iTopXy]
		bTopAvail = (iTopSliceIdc == iCurSliceIdc)
		if iCurX != 0 {
			iLeftTopXy = iTopXy - 1
			iLeftTopSliceIdc = pCurLayer.pSliceIdc[iLeftTopXy]
			bLeftTopAvail = (iLeftTopSliceIdc == iCurSliceIdc)
		} else {
			bLeftTopAvail = false
		}
		if iCurX != (pCurLayer.iMbWidth - 1) {
			iRightTopXy = iTopXy + 1
			iRightTopSliceIdc = pCurLayer.pSliceIdc[iRightTopXy]
			bRightTopAvail = (iRightTopSliceIdc == iCurSliceIdc)
		} else {
			bRightTopAvail = false
		}
	} else {
		bTopAvail = false
		bLeftTopAvail = false
		bRightTopAvail = false
	}

	if iCurX != 0 && bLeftAvail {
		iLeftType = int32(pCurLayer.pMbType[iLeftXy])
	}
	if iCurY != 0 && bTopAvail {
		iTopType = int32(pCurLayer.pMbType[iTopXy])
	}
	if iCurX != 0 && iCurY != 0 && bLeftTopAvail {
		iLeftTopType = int32(pCurLayer.pMbType[iLeftTopXy])
	}
	if iCurX != pCurLayer.iMbWidth-1 && iCurY != 0 && bRightTopAvail {
		iRightTopType = int32(pCurLayer.pMbType[iRightTopXy])
	}

	/*get neb mv&iRefIdxArray*/
	/*left*/
	if bLeftAvail && common.IS_INTER(iLeftType) {
		iMvA = pCurLayer.pMv[0][iLeftXy][3]
		iLeftRef = pCurLayer.pRefIndex[0][iLeftXy][3]
	} else {
		iMvA = [2]int16{}
		if !bLeftAvail { //not available
			iLeftRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iLeftRef = REF_NOT_IN_LIST
		}
	}
	if REF_NOT_AVAIL == iLeftRef ||
		(0 == iLeftRef && iMvA[0] == 0 && iMvA[1] == 0) {
		*iMvp = [2]int16{}
		return
	}

	/*top*/
	if bTopAvail && common.IS_INTER(iTopType) {
		iMvB = pCurLayer.pMv[0][iTopXy][12]
		iTopRef = pCurLayer.pRefIndex[0][iTopXy][12]
	} else {
		iMvB = [2]int16{}
		if !bTopAvail { //not available
			iTopRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iTopRef = REF_NOT_IN_LIST
		}
	}
	if REF_NOT_AVAIL == iTopRef ||
		(0 == iTopRef && iMvB[0] == 0 && iMvB[1] == 0) {
		*iMvp = [2]int16{}
		return
	}

	/*right_top*/
	if bRightTopAvail && common.IS_INTER(iRightTopType) {
		iMvC = pCurLayer.pMv[0][iRightTopXy][12]
		iRightTopRef = pCurLayer.pRefIndex[0][iRightTopXy][12]
	} else {
		iMvC = [2]int16{}
		if !bRightTopAvail { //not available
			iRightTopRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iRightTopRef = REF_NOT_IN_LIST
		}
	}

	/*left_top*/
	if bLeftTopAvail && common.IS_INTER(iLeftTopType) {
		iMvD = pCurLayer.pMv[0][iLeftTopXy][15]
		iLeftTopRef = pCurLayer.pRefIndex[0][iLeftTopXy][15]
	} else {
		iMvD = [2]int16{}
		if !bLeftTopAvail { //not available
			iLeftTopRef = REF_NOT_AVAIL
		} else { //available but is intra mb type
			iLeftTopRef = REF_NOT_IN_LIST
		}
	}

	iDiagonalRef = iRightTopRef
	if REF_NOT_AVAIL == iDiagonalRef {
		iDiagonalRef = iLeftTopRef
		iMvC = iMvD
	}

	if REF_NOT_AVAIL == iTopRef && REF_NOT_AVAIL == iDiagonalRef && iLeftRef >= REF_NOT_IN_LIST {
		*iMvp = iMvA
		return
	}

	iMatchRef = b2i8(0 == iLeftRef) + b2i8(0 == iTopRef) + b2i8(0 == iDiagonalRef)
	if 1 == iMatchRef {
		if 0 == iLeftRef {
			*iMvp = iMvA
		} else if 0 == iTopRef {
			*iMvp = iMvB
		} else {
			*iMvp = iMvC
		}
	} else {
		iMvp[0] = int16(common.WelsMedian(int32(iMvA[0]), int32(iMvB[0]), int32(iMvC[0])))
		iMvp[1] = int16(common.WelsMedian(int32(iMvA[1]), int32(iMvB[1]), int32(iMvC[1])))
	}
}

func AllocLayerData(pDqLayer *SDqLayer) {
	n := pDqLayer.iMbWidth * pDqLayer.iMbHeight
	pDqLayer.pSliceIdc = make([]int32, n)
	pDqLayer.pMbType = make([]uint32, n)
	pDqLayer.pMv[0] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, n)
	pDqLayer.pRefIndex[0] = make([][common.MB_BLOCK4x4_NUM]int8, n)
}

func InitRandomLayerSliceIdc(rnd *rand.Rand, pDqLayer *SDqLayer) {
	iTotalMbNum := pDqLayer.iMbWidth * pDqLayer.iMbHeight
	iMbFirstSliceEnd := rnd.Int31() % (iTotalMbNum - 1) //assure 2 slices
	var i int32
	for i = 0; i <= iMbFirstSliceEnd; i++ {
		pDqLayer.pSliceIdc[i] = 0 //to keep simple value here
	}
	for ; i < iTotalMbNum; i++ {
		pDqLayer.pSliceIdc[i] = 1 //to keep simple value here
	}
}

func InitRandomLayerMbType(rnd *rand.Rand, pDqLayer *SDqLayer) {
	for i := int32(0); i < pDqLayer.iMbWidth*pDqLayer.iMbHeight; i++ {
		pDqLayer.pMbType[i] = 1 << (rnd.Int31() % 11) //2^(1 ~ 10)
	}
}

func InitRandomLayerMvData(rnd *rand.Rand, pDqLayer *SDqLayer) {
	for i := int32(0); i < pDqLayer.iMbWidth*pDqLayer.iMbHeight; i++ {
		for j := 0; j < common.MB_BLOCK4x4_NUM; j++ {
			for k := 0; k < common.MV_A; k++ {
				pDqLayer.pMv[0][i][j][k] = int16(rnd.Int31() - testRandMax/2)
			}
		}
	}
}

func InitRandomLayerRefIdxData(rnd *rand.Rand, pDqLayer *SDqLayer) {
	for i := int32(0); i < pDqLayer.iMbWidth*pDqLayer.iMbHeight; i++ {
		for j := 0; j < common.MB_BLOCK4x4_NUM; j++ {
			pDqLayer.pRefIndex[0][i][j] = int8(rnd.Int31()%18 - 2) //-2 ~ 15
		}
	}
}

func InitRandomLayerData(rnd *rand.Rand, pDqLayer *SDqLayer) {
	InitRandomLayerSliceIdc(rnd, pDqLayer)
	InitRandomLayerMbType(rnd, pDqLayer)
	InitRandomLayerMvData(rnd, pDqLayer)
	InitRandomLayerRefIdxData(rnd, pDqLayer)
}

func setAllRefIdx(pDqLayer *SDqLayer, v int8) {
	for i := range pDqLayer.pRefIndex[0] {
		for j := range pDqLayer.pRefIndex[0][i] {
			pDqLayer.pRefIndex[0][i][j] = v
		}
	}
}

func setAllMbType(pDqLayer *SDqLayer, v uint32) {
	for i := range pDqLayer.pMbType {
		pDqLayer.pMbType[i] = v
	}
}

func TestPredMvTest_PredSkipMvFromNeighbor(t *testing.T) {
	rnd := rand.New(rand.NewSource(4))
	const kiRandTime = 100
	var sDqLayer SDqLayer
	var iAncMvp, iWelsMvp [2]int16

	testSkip := func(name string) {
		t.Helper()
		PredPSkipMvFromNeighbor(&sDqLayer, &iWelsMvp)
		if iWelsMvp != iAncMvp {
			t.Errorf("%s: expect %v, got %v", name, iAncMvp, iWelsMvp)
		}
	}

	//Assume the input data as 352x288 size
	//allocate the data
	sDqLayer.iMbWidth = 11
	sDqLayer.iMbHeight = 9
	AllocLayerData(&sDqLayer)
	InitRandomLayerData(rnd, &sDqLayer) //init MV data, as it would not affect the following logic test

	CURR_MB_IDX := func() int32 { return sDqLayer.iMbXyIndex }
	LEFT_MB_IDX := func() int32 { return sDqLayer.iMbXyIndex - 1 }
	const LEFT_MB_BLK = 3
	TOP_MB_IDX := func() int32 { return sDqLayer.iMbXyIndex - sDqLayer.iMbWidth }
	const TOP_MB_BLK = 12
	LEFT_TOP_MB_IDX := func() int32 { return sDqLayer.iMbXyIndex - sDqLayer.iMbWidth - 1 }
	const LEFT_TOP_MB_BLK = 15
	RIGHT_TOP_MB_IDX := func() int32 { return sDqLayer.iMbXyIndex - sDqLayer.iMbWidth + 1 }
	const RIGHT_TOP_MB_BLK = 12
	setPos := func(x, y int32) {
		sDqLayer.iMbX = x
		sDqLayer.iMbY = y
		sDqLayer.iMbXyIndex = sDqLayer.iMbY*sDqLayer.iMbWidth + sDqLayer.iMbX
	}

	//CASE 1: test MB [0,0], expect mvp = (0,0)
	setPos(0, 0)
	iAncMvp = [2]int16{} //expect anchor result to 0
	testSkip("CASE 1")
	//CASE 2: test MB [ANY, 0], expect mvp = (0,0)
	setPos(rnd.Int31()%sDqLayer.iMbWidth, 0)
	iAncMvp = [2]int16{}
	testSkip("CASE 2")
	//CASE 3: test MB [0, ANY], expect mvp = (0,0)
	setPos(0, rnd.Int31()%sDqLayer.iMbHeight)
	iAncMvp = [2]int16{}
	testSkip("CASE 3")
	//CASE 4.1: test MB [RIGHT_SIDE, ANY]
	setPos(sDqLayer.iMbWidth-1, rnd.Int31()%(sDqLayer.iMbHeight-1)+1) //not equal to 0
	//CASE 4.1.1: same slice_idc, assume = 0
	for i := range sDqLayer.pSliceIdc {
		sDqLayer.pSliceIdc[i] = 0
	}
	//CASE 4.1.1.1: ALL P modes
	setAllMbType(&sDqLayer, common.MB_TYPE_16x16)
	//CASE 4.1.1.1.1: ref_idx = 0, left MV = 0, top MV != 0, expect mvp = (0,0)
	setAllRefIdx(&sDqLayer, 0)
	InitRandomLayerMvData(rnd, &sDqLayer)                        //reset Mv data
	sDqLayer.pMv[0][LEFT_MB_IDX()][LEFT_MB_BLK] = [2]int16{0, 0} //left_mv = 0
	sDqLayer.pMv[0][TOP_MB_IDX()][TOP_MB_BLK] = [2]int16{1, 1}   //top_mv != 0
	iAncMvp = [2]int16{}
	testSkip("CASE 4.1.1.1.1")
	//CASE 4.1.1.1.2: ref_idx = 0, left MV != 0, top MV = 0, expect mvp = (0,0)
	setAllRefIdx(&sDqLayer, 0)
	InitRandomLayerMvData(rnd, &sDqLayer)                        //reset Mv data
	sDqLayer.pMv[0][LEFT_MB_IDX()][LEFT_MB_BLK] = [2]int16{1, 1} //left_mv != 0
	sDqLayer.pMv[0][TOP_MB_IDX()][TOP_MB_BLK] = [2]int16{0, 0}   //top_mv = 0
	iAncMvp = [2]int16{}
	testSkip("CASE 4.1.1.1.2")
	//CASE 4.1.1.1.3: ref_idx top = 0, others = 1, expect mvp = top mv
	InitRandomLayerMvData(rnd, &sDqLayer)                         //reset Mv data
	sDqLayer.pRefIndex[0][TOP_MB_IDX()][TOP_MB_BLK] = 0           //top ref_idx = 0
	sDqLayer.pRefIndex[0][LEFT_MB_IDX()][LEFT_MB_BLK] = 1         //left ref_idx = 1
	sDqLayer.pRefIndex[0][LEFT_TOP_MB_IDX()][LEFT_TOP_MB_BLK] = 1 //left_top ref_idx = 1
	iAncMvp = sDqLayer.pMv[0][TOP_MB_IDX()][TOP_MB_BLK]
	testSkip("CASE 4.1.1.1.3")
	//CASE 4.1.1.1.4: ref_idx left = 0, others = 1, expect mvp = left mv
	sDqLayer.pRefIndex[0][TOP_MB_IDX()][TOP_MB_BLK] = 1           //top ref_idx = 1
	sDqLayer.pRefIndex[0][LEFT_MB_IDX()][LEFT_MB_BLK] = 0         //left ref_idx = 0
	sDqLayer.pRefIndex[0][LEFT_TOP_MB_IDX()][LEFT_TOP_MB_BLK] = 1 //left_top ref_idx = 1
	iAncMvp = sDqLayer.pMv[0][LEFT_MB_IDX()][LEFT_MB_BLK]
	testSkip("CASE 4.1.1.1.4")
	//CASE 4.1.1.2: All I
	setAllMbType(&sDqLayer, common.MB_TYPE_INTRA16x16)
	//CASE 4.1.1.2.1: left P, expect mvp = left mv
	sDqLayer.pMbType[LEFT_MB_IDX()] = common.MB_TYPE_16x16 //left P
	iAncMvp = sDqLayer.pMv[0][LEFT_MB_IDX()][LEFT_MB_BLK]
	testSkip("CASE 4.1.1.2.1")
	//CASE 4.1.1.3: only top P, top ref_idx = 0, expect mvp = top mv
	setAllMbType(&sDqLayer, common.MB_TYPE_INTRA16x16)    // All I MB
	setAllRefIdx(&sDqLayer, 1)                            // All ref_idx = 1
	sDqLayer.pMbType[TOP_MB_IDX()] = common.MB_TYPE_16x16 //top P
	sDqLayer.pRefIndex[0][TOP_MB_IDX()][TOP_MB_BLK] = 0   //top ref_idx = 0
	iAncMvp = sDqLayer.pMv[0][TOP_MB_IDX()][TOP_MB_BLK]
	testSkip("CASE 4.1.1.3")
	//CASE 4.1.1.4: only left_top P, left_top ref_idx = 0, expect mvp = 0
	setPos((rnd.Int31()%(sDqLayer.iMbWidth-2))+1, (rnd.Int31()%(sDqLayer.iMbHeight-2))+1)
	setAllMbType(&sDqLayer, common.MB_TYPE_INTRA16x16) // All I MB
	setAllRefIdx(&sDqLayer, 1)                         // All ref_idx = 1
	sDqLayer.pMbType[LEFT_TOP_MB_IDX()] = common.MB_TYPE_16x16
	sDqLayer.pRefIndex[0][LEFT_TOP_MB_IDX()][LEFT_TOP_MB_BLK] = 0
	iAncMvp = [2]int16{}
	testSkip("CASE 4.1.1.4")
	//CASE 4.1.1.5: only right_top P, right_top ref_idx = 0, expect mvp = right_top mv
	setPos((rnd.Int31()%(sDqLayer.iMbWidth-2))+1, (rnd.Int31()%(sDqLayer.iMbHeight-2))+1)
	setAllMbType(&sDqLayer, common.MB_TYPE_INTRA16x16) // All I MB
	setAllRefIdx(&sDqLayer, 1)                         // All ref_idx = 1
	sDqLayer.pMbType[RIGHT_TOP_MB_IDX()] = common.MB_TYPE_16x16
	sDqLayer.pRefIndex[0][RIGHT_TOP_MB_IDX()][RIGHT_TOP_MB_BLK] = 0
	iAncMvp = sDqLayer.pMv[0][RIGHT_TOP_MB_IDX()][RIGHT_TOP_MB_BLK]
	testSkip("CASE 4.1.1.5")
	//CASE 4.1.2: different neighbor slice idc for all P and ref_idx = 0, expect mvp = 0
	setAllMbType(&sDqLayer, common.MB_TYPE_16x16) // All P MB
	setAllRefIdx(&sDqLayer, 0)
	setPos((rnd.Int31()%(sDqLayer.iMbWidth-2))+1, (rnd.Int31()%(sDqLayer.iMbHeight-2))+1)
	sDqLayer.pSliceIdc[CURR_MB_IDX()] = 5
	sDqLayer.pSliceIdc[LEFT_MB_IDX()] = 0
	sDqLayer.pSliceIdc[TOP_MB_IDX()] = 1
	sDqLayer.pSliceIdc[LEFT_TOP_MB_IDX()] = 2
	sDqLayer.pSliceIdc[RIGHT_TOP_MB_IDX()] = 3
	iAncMvp = [2]int16{}
	testSkip("CASE 4.1.2")

	//normal tests
	for i := 0; i < kiRandTime; i++ {
		InitRandomLayerData(rnd, &sDqLayer)
		AnchorPredPSkipMvFromNeighbor(&sDqLayer, &iAncMvp)
		testSkip("random")
	}
}

func TestPredMvTest_GetColocatedMbNullRefPicReturnsErrorBeforeWait(t *testing.T) {
	var sCtx SWelsDecoderContext
	var sDqLayer SDqLayer
	var sThreadCtx SWelsDecoderThreadCTX
	uiMbType := []uint32{common.MB_TYPE_16x16}
	var mbType MbType = common.MB_TYPE_16x16
	var subMbType SubMbType = common.SUB_MB_TYPE_8x8

	sDqLayer.iMbXyIndex = 0
	sDqLayer.iMbY = 1
	sDqLayer.pMbType = uiMbType
	sCtx.pCurDqLayer = &sDqLayer

	sThreadCtx.sThreadInfo.uiThrMaxNum = 2
	sCtx.pThreadCtx = &sThreadCtx
	sCtx.lastReadyHeightOffset[1][0] = 0
	sCtx.sRefPic.pRefList[common.LIST_1][0] = nil

	iRet := GetColocatedMb(&sCtx, &mbType, &subMbType)
	if want := GENERATE_ERROR_NO(ERR_LEVEL_SLICE_DATA, ERR_INFO_REFERENCE_PIC_LOST); iRet != want {
		t.Fatalf("GetColocatedMb = %d, want %d", iRet, want)
	}
}

// Extra (not in the C suite): the rectangle helpers used by GetColocatedMb.
func TestPredMvTest_RectBlockHelpers(t *testing.T) {
	var src, dst [16][2]int16
	for i := range src {
		src[i] = [2]int16{int16(i), int16(-i)}
	}
	CopyRectBlock4Cols(&dst, &src, 16, 16, 4, 4)
	if dst != src {
		t.Fatalf("CopyRectBlock4Cols mv: got %v", dst)
	}
	var srcR, dstR [16]int8
	for i := range srcR {
		srcR[i] = int8(i - 3)
	}
	CopyRectBlock4Cols(&dstR, &srcR, 4, 4, 4, 1)
	if dstR != srcR {
		t.Fatalf("CopyRectBlock4Cols ref: got %v", dstR)
	}

	var mv [16][2]int16
	setRectBlockMv(&mv, 2, 2, 2, 16, [2]int16{7, -7})
	for i := range mv {
		want := [2]int16{}
		if i == 2 || i == 3 || i == 6 || i == 7 {
			want = [2]int16{7, -7}
		}
		if mv[i] != want {
			t.Fatalf("setRectBlockMv[%d] = %v, want %v", i, mv[i], want)
		}
	}
	var r [16]int8
	setRectBlockInt8(&r, 8, 2, 2, 4, -1)
	for i := range r {
		var want int8
		if i == 8 || i == 9 || i == 12 || i == 13 {
			want = -1
		}
		if r[i] != want {
			t.Fatalf("setRectBlockInt8[%d] = %d, want %d", i, r[i], want)
		}
	}
}

// Extra (not in the C suite): B-slice direct prediction smoke tests on a
// 2x1-MB picture, current MB = 1, colocated MB is a P 16x16 L0 block.
func newBDirectTestCtx(t *testing.T) (*SWelsDecoderContext, *SPicture) {
	t.Helper()
	ctx := &SWelsDecoderContext{pParam: &api.SDecodingParam{}}
	sps := &SSps{}
	sps.bDirect8x8InferenceFlag = true
	ctx.pSps = sps
	pDec := AllocPicture(ctx, 32, 16)
	colPic := AllocPicture(ctx, 32, 16)
	refPic0 := AllocPicture(ctx, 32, 16)
	refPic0.iFramePoc = 2
	colPic.iFramePoc = 8
	colPic.pMbType[1] = common.MB_TYPE_16x16 | common.MB_TYPE_P0L0
	for k := 0; k < 16; k++ {
		colPic.pMv[0][1][k] = [2]int16{8, 4}
		colPic.pRefIndex[0][1][k] = 0
		colPic.pRefIndex[1][1][k] = REF_NOT_IN_LIST
	}
	colPic.pRefPic[0][0] = refPic0
	ctx.sRefPic.pRefList[common.LIST_0][0] = refPic0
	ctx.sRefPic.pRefList[common.LIST_1][0] = colPic
	ctx.sRefPic.uiRefCount[0] = 1
	ctx.sRefPic.uiRefCount[1] = 1

	dq := &SDqLayer{}
	dq.iMbWidth, dq.iMbHeight = 2, 1
	dq.iMbX, dq.iMbY, dq.iMbXyIndex = 1, 0, 1
	dq.pDec = pDec
	dq.pSliceIdc = make([]int32, 2)
	dq.pSubMbType = make([][MB_SUB_PARTITION_SIZE]uint32, 2)
	dq.pDirect = make([][common.MB_BLOCK4x4_NUM]int8, 2)
	for l := 0; l < common.LIST_A; l++ {
		dq.pMvd[l] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, 2)
	}
	dq.sLayerInfo.sSliceInLayer.iMvScale[0][0] = 128
	dq.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.uiRefCount[0] = 1
	ctx.pCurDqLayer = dq

	// left neighbour (MB 0): P 16x16, L0 ref 0 mv (6,-2); not in L1
	pDec.pMbType[0] = common.MB_TYPE_16x16 | common.MB_TYPE_P0L0
	for k := 0; k < 16; k++ {
		pDec.pMv[0][0][k] = [2]int16{6, -2}
		pDec.pRefIndex[0][0][k] = 0
		pDec.pRefIndex[1][0][k] = REF_NOT_IN_LIST
	}
	pDec.pMbType[1] = common.MB_TYPE_SKIP | common.MB_TYPE_DIRECT
	return ctx, pDec
}

func TestPredMvTest_PredBDirectTemporal16x16(t *testing.T) {
	ctx, pDec := newBDirectTestCtx(t)
	var iMvp [common.LIST_A][2]int16
	var ref [common.LIST_A]int8
	var subMbType SubMbType
	if ret := PredBDirectTemporal(ctx, &iMvp, &ref, &subMbType); ret != ERR_NONE {
		t.Fatalf("ret = %d", ret)
	}
	if iMvp[0] != [2]int16{4, 2} || iMvp[1] != [2]int16{-4, -2} || ref != [2]int8{0, 0} {
		t.Fatalf("mvp %v ref %v", iMvp, ref)
	}
	for k := 0; k < 16; k++ {
		if pDec.pMv[0][1][k] != [2]int16{4, 2} || pDec.pMv[1][1][k] != [2]int16{-4, -2} ||
			pDec.pRefIndex[0][1][k] != 0 || pDec.pRefIndex[1][1][k] != 0 {
			t.Fatalf("block %d: mv %v %v ref %d %d", k, pDec.pMv[0][1][k], pDec.pMv[1][1][k],
				pDec.pRefIndex[0][1][k], pDec.pRefIndex[1][1][k])
		}
	}
	if !common.IS_INTER_16x16(pDec.pMbType[1]) {
		t.Fatalf("mb type %x", pDec.pMbType[1])
	}
}

func TestPredMvTest_PredMvBDirectSpatial16x16(t *testing.T) {
	ctx, pDec := newBDirectTestCtx(t)
	var iMvp [common.LIST_A][2]int16
	var ref [common.LIST_A]int8
	var subMbType SubMbType
	if ret := PredMvBDirectSpatial(ctx, &iMvp, &ref, &subMbType); ret != ERR_NONE {
		t.Fatalf("ret = %d", ret)
	}
	if iMvp[0] != [2]int16{6, -2} || iMvp[1] != [2]int16{} || ref != [2]int8{0, REF_NOT_IN_LIST} {
		t.Fatalf("mvp %v ref %v", iMvp, ref)
	}
	if pDec.pMbType[1]&common.MB_TYPE_L1 != 0 || !common.IS_INTER_16x16(pDec.pMbType[1]) {
		t.Fatalf("mb type %x", pDec.pMbType[1])
	}
	for k := 0; k < 16; k++ {
		if pDec.pMv[0][1][k] != [2]int16{6, -2} || pDec.pRefIndex[0][1][k] != 0 || pDec.pRefIndex[1][1][k] != REF_NOT_IN_LIST {
			t.Fatalf("block %d: mv %v ref %d %d", k, pDec.pMv[0][1][k], pDec.pRefIndex[0][1][k], pDec.pRefIndex[1][1][k])
		}
	}
}
