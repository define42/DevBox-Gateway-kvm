// Port of the decoder-specific parts of test/decoder/DecUT_DeblockCommon.cpp
// (the DeblockingCommon tests of the C loop filters live in
// internal/common; test/decoder/DecUT_Deblock.cpp only compares SIMD code
// against C and is not ported).

package decoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

/* NULL functions, for null call */
func UT_DeblockingFuncInterface(pCurDqLayer *SDqLayer, filter *SDeblockingFilter, boundry_flag int32) {
}

func UT_DeblockingFuncLumaLT4Func(iSampleY []uint8, iSampleYOff int, iStride int32, iAlpha int32, iBeta int32, iTc []int8) {
	if iAlpha > 0 || iBeta > 0 {
		iSampleY[iSampleYOff]++
	}
}

func UT_DeblockingFuncLumaEQ4Func(iSampleY []uint8, iSampleYOff int, iStride int32, iAlpha int32, iBeta int32) {
	if iAlpha > 0 || iBeta > 0 {
		iSampleY[iSampleYOff]++
	}
}

func UT_DeblockingFuncChromaLT4Func(iSampleCb []uint8, iSampleCbOff int, iSampleCr []uint8, iSampleCrOff int, iStride int32,
	iAlpha int32, iBeta int32, iTc []int8) {
	if iAlpha > 0 || iBeta > 0 {
		iSampleCb[iSampleCbOff]++
		iSampleCr[iSampleCrOff]++
	}
}

func UT_DeblockingFuncChromaEQ4Func(iSampleCb []uint8, iSampleCbOff int, iSampleCr []uint8, iSampleCrOff int, iStride int32,
	iAlpha int32, iBeta int32) {
	if iAlpha > 0 || iBeta > 0 {
		iSampleCb[iSampleCbOff]++
		iSampleCr[iSampleCrOff]++
	}
}

func TestDecoderDeblocking_DeblockingAvailableNoInterlayer(t *testing.T) {
	var sLayer SDqLayer
	var iFilterIdc int32
	iSliceIdc := make([]int32, 9)

	sLayer.pSliceIdc = iSliceIdc

	/* iFilterIdc only support 0 and 2, which is related with the encode configuration */
	/* Using 3x3 grids to simulate the different situations */
	setPos := func(iX, iY int32) {
		sLayer.iMbX = iX
		sLayer.iMbY = iY
		sLayer.iMbXyIndex = sLayer.iMbX + sLayer.iMbY*3
		sLayer.iMbWidth = 3
	}
	idc0 := func(iX, iY, iExpect int32) {
		iFilterIdc = 0
		setPos(iX, iY)
		if got := DeblockingAvailableNoInterlayer(&sLayer, iFilterIdc); got != iExpect {
			t.Errorf("idc0 (%d,%d): got %#x want %#x", iX, iY, got, iExpect)
		}
	}
	idc2Same := func(iX, iY, iExpect int32) {
		iFilterIdc = 2
		setPos(iX, iY)
		iSliceIdc[0] = int32(rand.Intn(10))
		for i := 1; i < 9; i++ {
			iSliceIdc[i] = iSliceIdc[0]
		}
		if got := DeblockingAvailableNoInterlayer(&sLayer, iFilterIdc); got != iExpect {
			t.Errorf("Same Slice (%d,%d): got %#x want %#x", iX, iY, got, iExpect)
		}
	}
	idc2Diff := func(iX, iY, iExpect int32) {
		iFilterIdc = 2
		setPos(iX, iY)
		for i := 0; i < 9; i++ {
			iSliceIdc[i] = int32(i)
		}
		if got := DeblockingAvailableNoInterlayer(&sLayer, iFilterIdc); got != iExpect {
			t.Errorf("Different Slice (%d,%d): got %#x want %#x", iX, iY, got, iExpect)
		}
	}

	// (1) idc==0
	idc0(0, 0, 0x00)
	idc0(0, 1, 0x02)
	idc0(0, 2, 0x02)
	idc0(1, 0, 0x01)
	idc0(1, 1, 0x03)
	idc0(1, 2, 0x03)
	idc0(2, 0, 0x01)
	idc0(2, 1, 0x03)
	idc0(2, 2, 0x03)

	// (2) idc==2, same slice
	idc2Same(0, 0, 0x00)
	idc2Same(0, 1, 0x02)
	idc2Same(0, 2, 0x02)
	idc2Same(1, 0, 0x01)
	idc2Same(1, 1, 0x03)
	idc2Same(1, 2, 0x03)
	idc2Same(2, 0, 0x01)
	idc2Same(2, 1, 0x03)
	idc2Same(2, 2, 0x03)

	// (3) idc==2, diff slice
	for x := int32(0); x < 3; x++ {
		for y := int32(0); y < 3; y++ {
			idc2Diff(x, y, 0x00)
		}
	}
}

func TestDecoderDeblocking_DeblockingInit(t *testing.T) {
	var sDBFunc SDeblockingFunc
	DeblockingInit(&sDBFunc, 0x00000000)
	if sDBFunc.pfLumaDeblockingLT4Ver == nil || sDBFunc.pfLumaDeblockingEQ4Ver == nil ||
		sDBFunc.pfLumaDeblockingLT4Hor == nil || sDBFunc.pfLumaDeblockingEQ4Hor == nil ||
		sDBFunc.pfChromaDeblockingLT4Ver == nil || sDBFunc.pfChromaDeblockingEQ4Ver == nil ||
		sDBFunc.pfChromaDeblockingLT4Hor == nil || sDBFunc.pfChromaDeblockingEQ4Hor == nil ||
		sDBFunc.pfChromaDeblockingLT4Ver2 == nil || sDBFunc.pfChromaDeblockingEQ4Ver2 == nil ||
		sDBFunc.pfChromaDeblockingLT4Hor2 == nil || sDBFunc.pfChromaDeblockingEQ4Hor2 == nil {
		t.Fatalf("DeblockingInit left a nil function pointer")
	}

	// Go func values cannot be compared, so check behaviour against the C
	// functions on random data instead.
	const n = 16 * 17
	buf1 := make([]uint8, n)
	buf2 := make([]uint8, n)
	tc := []int8{3, 5, 7, 9}
	for k := range buf1 {
		buf1[k] = uint8(rand.Intn(256))
	}
	copy(buf2, buf1)
	sDBFunc.pfLumaDeblockingLT4Ver(buf1, 16*8, 16, 40, 10, tc)
	common.DeblockLumaLt4V_c(buf2, 16*8, 16, 40, 10, tc)
	for k := range buf1 {
		if buf1[k] != buf2[k] {
			t.Fatalf("pfLumaDeblockingLT4Ver is not DeblockLumaLt4V_c")
		}
	}
}

func TestDecoderDeblocking_WelsDeblockingFilterSlice(t *testing.T) {
	/* NOT support FMO now */
	var sCtx SWelsDecoderContext
	var sDqLayer SDqLayer
	var sSPS SSps
	var sPPS SPps
	var sDec SPicture
	var pDeblockMb PDeblockingFilterMbFunc = UT_DeblockingFuncInterface

	/* NOT do actual deblocking process, set related parameters to null */
	sCtx.pDec = &sDec

	/* As no FMO in encoder now, the multi slicegroups has not been set */
	sCtx.pFmo = nil

	sCtx.pCurDqLayer = &sDqLayer
	pSlice := &sDqLayer.sLayerInfo.sSliceInLayer
	pSh := &pSlice.sSliceHeaderExt.sSliceHeader
	/* As void return, using iMbXyIndex to reflect whether the all MBs have been passed. */
	pSh.iFirstMbInSlice = 0
	pSlice.iTotalMbInCurSlice = 0

	// whether disable Deblocking Filter Idc
	pSh.uiDisableDeblockingFilterIdc = 0
	pSh.iSliceAlphaC0Offset = 0
	pSh.iSliceBetaOffset = 0

	pSh.pSps = &sSPS
	pSh.pSps.uiTotalMbCount = 0

	pSh.pPps = &sPPS
	/* Only test one slicegroup, not reflect the FMO func */
	pSh.pPps.uiNumSliceGroups = 1

	// (1) Normal case, the iTotalMbInCurSlice == pSps->uiTotalMbCount
	sDqLayer.iMbX, sDqLayer.iMbY = 0, 0
	sDqLayer.iMbXyIndex = 0
	pSlice.iTotalMbInCurSlice = 1 + int32(rand.Intn(256)) // at least one MB
	pSh.pSps.uiTotalMbCount = uint32(pSlice.iTotalMbInCurSlice)
	sDqLayer.iMbWidth = 1 + int32(rand.Intn(128))
	WelsDeblockingFilterSlice(&sCtx, pDeblockMb)
	if sDqLayer.iMbXyIndex+1 != pSlice.iTotalMbInCurSlice {
		t.Errorf("(1) %d %d", sDqLayer.iMbXyIndex, pSlice.iTotalMbInCurSlice)
	}

	// (2) Normal case, multi slices, iTotalMbInCurSlice <= pSps->uiTotalMbCount
	sDqLayer.iMbX, sDqLayer.iMbY = 0, 0
	sDqLayer.iMbXyIndex = 0
	pSlice.iTotalMbInCurSlice = 1 + int32(rand.Intn(256))
	pSh.pSps.uiTotalMbCount = uint32(pSlice.iTotalMbInCurSlice) + uint32(rand.Intn(256))
	sDqLayer.iMbWidth = 1 + int32(rand.Intn(128))
	WelsDeblockingFilterSlice(&sCtx, pDeblockMb)
	if sDqLayer.iMbXyIndex+1 != pSlice.iTotalMbInCurSlice {
		t.Errorf("(2) %d %d", sDqLayer.iMbXyIndex, pSlice.iTotalMbInCurSlice)
	}

	// (3) Special case, iTotalMbInCurSlice >= pSps->uiTotalMbCount, JUST FOR TEST
	sDqLayer.iMbX, sDqLayer.iMbY = 0, 0
	sDqLayer.iMbXyIndex = 0
	pSh.pSps.uiTotalMbCount = 1 + uint32(rand.Intn(256))
	pSlice.iTotalMbInCurSlice = int32(pSh.pSps.uiTotalMbCount) + int32(rand.Intn(256))
	sDqLayer.iMbWidth = 1 + int32(rand.Intn(128))
	WelsDeblockingFilterSlice(&sCtx, pDeblockMb)
	if uint32(sDqLayer.iMbXyIndex+1) != pSh.pSps.uiTotalMbCount {
		t.Errorf("(3) %d %d", sDqLayer.iMbXyIndex, pSh.pSps.uiTotalMbCount)
	}

	// (4) Special case, uiDisableDeblockingFilterIdc==1, disable deblocking
	sDqLayer.iMbX, sDqLayer.iMbY = 0, 0
	sDqLayer.iMbXyIndex = 0
	pSh.uiDisableDeblockingFilterIdc = 1
	pSlice.iTotalMbInCurSlice = 1 + int32(rand.Intn(256))
	pSh.pSps.uiTotalMbCount = uint32(pSlice.iTotalMbInCurSlice)
	sDqLayer.iMbWidth = 1 + int32(rand.Intn(128))
	WelsDeblockingFilterSlice(&sCtx, pDeblockMb)
	if sDqLayer.iMbXyIndex != 0 {
		t.Errorf("(4) %d %d", sDqLayer.iMbXyIndex, pSlice.iTotalMbInCurSlice)
	}
}

func TestDecoderDeblocking_FilteringEdgeChromaHV(t *testing.T) {
	var sDqLayer SDqLayer
	var sFilter SDeblockingFilter
	var iBoundryFlag int32

	var sDBFunc SDeblockingFunc
	sFilter.pLoopf = &sDBFunc
	sFilter.pLoopf.pfChromaDeblockingLT4Hor = UT_DeblockingFuncChromaLT4Func
	sFilter.pLoopf.pfChromaDeblockingLT4Ver = UT_DeblockingFuncChromaLT4Func
	sFilter.pLoopf.pfChromaDeblockingEQ4Hor = UT_DeblockingFuncChromaEQ4Func
	sFilter.pLoopf.pfChromaDeblockingEQ4Ver = UT_DeblockingFuncChromaEQ4Func

	iChromaQP := make([][2]int8, 9)
	sDqLayer.pChromaQp = iChromaQP

	iCb := make([]uint8, 9)
	iCr := make([]uint8, 9)
	sFilter.pCsData[1] = iCb
	sFilter.pCsData[2] = iCr
	sFilter.iCsStride[0], sFilter.iCsStride[1] = 2, 2

	sDqLayer.iMbX = 0
	sDqLayer.iMbY = 0       //Only for test easy
	sDqLayer.iMbXyIndex = 1 // this function has NO iMbXyIndex validation

	test := func(iFlag int32, iQP int, iV0, iV1, iV2 uint8) {
		iBoundryFlag = iFlag
		for k := range iChromaQP {
			iChromaQP[k] = [2]int8{int8(iQP), int8(iQP)}
		}
		clear(iCb)
		clear(iCr)
		FilteringEdgeChromaHV(&sDqLayer, &sFilter, iBoundryFlag)
		s := int(sFilter.iCsStride[1])
		if !(iCb[0] == iV0 && iCr[0] == iV0) ||
			!(iCb[2<<1] == iV1 && iCr[2<<1] == iV1) ||
			!(iCb[(2<<1)*s] == iV2 && iCr[(2<<1)*s] == iV2) {
			t.Errorf("flag %#x qp %d: got (%d,%d,%d) want (%d,%d,%d)", iFlag, iQP,
				iCb[0], iCb[2<<1], iCb[(2<<1)*s], iV0, iV1, iV2)
		}
	}

	// QP<=15, iAlpha == iBeta == 0, TOP & LEFT
	test(0x03, rand.Intn(16), 0, 0, 0)
	// QP>=16, iAlpha>0 && iBeta>0, TOP & LEFT
	test(0x03, 16+rand.Intn(35), 2, 1, 1)
	// QP<=15, iAlpha == iBeta == 0, TOP | LEFT
	test(0x01, rand.Intn(16), 0, 0, 0)
	test(0x02, rand.Intn(16), 0, 0, 0)
	// QP>=16, iAlpha>0 && iBeta>0, TOP | LEFT
	test(0x01, 16+rand.Intn(35), 1, 1, 1)
	test(0x02, 16+rand.Intn(35), 1, 1, 1)
	// QP<=15, iAlpha == iBeta == 0, !TOP & !LEFT
	test(0x00, rand.Intn(16), 0, 0, 0)
	// QP>=16, iAlpha>0 && iBeta>0, !TOP & !LEFT
	test(0x00, 16+rand.Intn(35), 0, 1, 1)
}

func TestDecoderDeblocking_FilteringEdgeLumaHV(t *testing.T) {
	var sDqLayer SDqLayer
	var sFilter SDeblockingFilter
	var iBoundryFlag int32

	var sDBFunc SDeblockingFunc
	sFilter.pLoopf = &sDBFunc
	sFilter.pLoopf.pfLumaDeblockingLT4Hor = UT_DeblockingFuncLumaLT4Func
	sFilter.pLoopf.pfLumaDeblockingEQ4Hor = UT_DeblockingFuncLumaEQ4Func
	sFilter.pLoopf.pfLumaDeblockingLT4Ver = UT_DeblockingFuncLumaLT4Func
	sFilter.pLoopf.pfLumaDeblockingEQ4Ver = UT_DeblockingFuncLumaEQ4Func

	iLumaQP := make([]int8, 50)
	sDqLayer.pLumaQp = iLumaQP

	iY := make([]uint8, 50)
	sFilter.pCsData[0] = iY
	sFilter.iCsStride[0], sFilter.iCsStride[1] = 4, 4

	sDqLayer.iMbX = 0
	sDqLayer.iMbY = 0       //Only for test easy
	sDqLayer.iMbXyIndex = 1 // this function has NO iMbXyIndex validation

	bTSize8x8Flag := make([]bool, 50)
	sDqLayer.pTransformSize8x8Flag = bTSize8x8Flag
	sDqLayer.pTransformSize8x8Flag[sDqLayer.iMbXyIndex] = false

	test := func(iFlag int32, iQP int, iV0, iV1, iV2 uint8) {
		iBoundryFlag = iFlag
		for k := range iLumaQP {
			iLumaQP[k] = int8(iQP)
		}
		clear(iY)
		FilteringEdgeLumaHV(&sDqLayer, &sFilter, iBoundryFlag)
		s := int(sFilter.iCsStride[0])
		if iY[0] != iV0 ||
			!(iY[1<<2] == iV1 && iY[2<<2] == iV1 && iY[3<<2] == iV1) ||
			!(iY[(1<<2)*s] == iV2 && iY[(2<<2)*s] == iV2 && iY[(3<<2)*s] == iV2) {
			t.Errorf("flag %#x qp %d: got (%d,%d,%d) want (%d,%d,%d)", iFlag, iQP,
				iY[0], iY[1<<2], iY[(1<<2)*s], iV0, iV1, iV2)
		}
	}

	// QP<=15, iAlpha == iBeta == 0, TOP & LEFT
	test(0x03, rand.Intn(16), 0, 0, 0)
	// QP>=16, iAlpha>0 && iBeta>0, TOP & LEFT
	test(0x03, 16+rand.Intn(35), 2, 1, 1)
	// QP<=15, iAlpha == iBeta == 0, TOP | LEFT
	test(0x01, rand.Intn(16), 0, 0, 0)
	test(0x02, rand.Intn(16), 0, 0, 0)
	// QP>=16, iAlpha>0 && iBeta>0, TOP | LEFT
	test(0x01, 16+rand.Intn(35), 1, 1, 1)
	test(0x02, 16+rand.Intn(35), 1, 1, 1)
	// QP<=15, iAlpha == iBeta == 0, !TOP & !LEFT
	test(0x00, rand.Intn(16), 0, 0, 0)
	// QP>=16, iAlpha>0 && iBeta>0, !TOP & !LEFT
	test(0x00, 16+rand.Intn(35), 0, 1, 1)
}

// Bs calculation functions
func TestDecoderDeblocking_DeblockingBsMarginalMBAvcbase(t *testing.T) {
	/* Calculate the Bs equal to 2 or 1 */
	var sDqLayer SDqLayer
	var sFilter SDeblockingFilter

	// Only define 2 MBs here
	iNoZeroCount := make([][24]int8, 2)                          // (*pNzc)[24]
	var iLayerRefIndex [2][][common.MB_BLOCK4x4_NUM]int8         // (*pRefIndex[LIST_A])[MB_BLOCK4x4_NUM];
	var iLayerMv [2][][common.MB_BLOCK4x4_NUM][common.MV_A]int16 //(*pMv[LIST_A])[MB_BLOCK4x4_NUM][MV_A];
	var iFilterPics [2][MAX_DPB_COUNT]*SPicture                  // Dummy reference pictures list
	for l := 0; l < 2; l++ {
		iLayerRefIndex[l] = make([][common.MB_BLOCK4x4_NUM]int8, 2)
		iLayerMv[l] = make([][common.MB_BLOCK4x4_NUM][common.MV_A]int16, 2)
	}

	sDqLayer.pNzc = iNoZeroCount
	sDqLayer.pRefIndex[0] = iLayerRefIndex[0]
	sDqLayer.pRefIndex[1] = iLayerRefIndex[1]
	sDqLayer.pMv[0] = iLayerMv[0]
	sDqLayer.pMv[1] = iLayerMv[1]

	bTSize8x8Flag := make([]bool, 50)
	sDqLayer.pTransformSize8x8Flag = bTSize8x8Flag
	// Dummy picture list pointers; they only need to be different
	for i := 0; i < MAX_DPB_COUNT; i++ {
		p := new(SPicture)
		iFilterPics[0][i] = p
		iFilterPics[1][i] = p
	}

	sFilter.pRefPics[0] = iFilterPics[0][:]
	sFilter.pRefPics[1] = iFilterPics[1][:]
	sDqLayer.pDec = nil

	clean := func() {
		clear(iNoZeroCount)
		for l := 0; l < 2; l++ {
			clear(iLayerRefIndex[l])
			clear(iLayerMv[l])
		}
	}
	refValue := func(value uint8, pos int) uint32 {
		return uint32(value) << (8 * pos)
	}

	for iEdge := int32(0); iEdge < 2; iEdge++ { // Vertical and Horizontal
		for iPos := 0; iPos < 4; iPos++ { // Four different blocks on the edge
			var iCurrBlock, iNeighborBlock int
			if iEdge == 0 {
				iCurrBlock, iNeighborBlock = 4*iPos, 3+iPos*4
			} else {
				iCurrBlock, iNeighborBlock = iPos, 12+iPos
			}
			check := func(want uint32, msg string) {
				t.Helper()
				if got := DeblockingBsMarginalMBAvcbase(&sFilter, &sDqLayer, iEdge, 1, 0); got != want {
					t.Errorf("edge %d pos %d %s: got %#08x want %#08x", iEdge, iPos, msg, got, want)
				}
			}

			// (1) current block NoZeroCount != 0
			clean()
			iNoZeroCount[0][iCurrBlock] = 1
			check(refValue(2, iPos), "NoZeroCount!=0")

			// (2) neighbor block NoZeroCount != 0
			clean()
			iNoZeroCount[1][iNeighborBlock] = 1
			check(refValue(2, iPos), "NoZeroCount!=0")

			// (3) reference idx diff
			clean()
			iLayerRefIndex[0][0][iCurrBlock] = 0
			iLayerRefIndex[0][1][iNeighborBlock] = 1
			check(refValue(1, iPos), "Ref idx diff")

			// (4) abs(mv diff) < 4
			clean()
			iLayerMv[0][0][iCurrBlock][0] = int16(rand.Intn(4))
			check(0, "diff_mv < 4")
			clean()
			iLayerMv[0][0][iCurrBlock][1] = int16(rand.Intn(4))
			check(0, "diff_mv < 4")
			clean()
			iLayerMv[0][1][iNeighborBlock][0] = int16(rand.Intn(4))
			check(0, "diff_mv < 4")
			clean()
			iLayerMv[0][1][iNeighborBlock][1] = int16(rand.Intn(4))
			check(0, "diff_mv < 4")

			// (5) abs(mv diff) >= 4
			clean()
			iLayerMv[0][0][iCurrBlock][0] = 4
			check(refValue(1, iPos), "diff_mv == 4")
			clean()
			iLayerMv[0][0][iCurrBlock][1] = 4
			check(refValue(1, iPos), "diff_mv == 4")
			clean()
			iLayerMv[0][1][iNeighborBlock][0] = 4
			check(refValue(1, iPos), "diff_mv == 4")
			clean()
			iLayerMv[0][1][iNeighborBlock][1] = 4
			check(refValue(1, iPos), "diff_mv == 4")

			clean()
			iLayerMv[0][0][iCurrBlock][0] = -2048
			iLayerMv[0][1][iNeighborBlock][0] = 2047
			check(refValue(1, iPos), "diff_mv == maximum")
			clean()
			iLayerMv[0][0][iCurrBlock][1] = -2048
			iLayerMv[0][1][iNeighborBlock][1] = 2047
			check(refValue(1, iPos), "diff_mv == maximum")
		}
	}
}

func TestDeblocking_WelsDeblockingMb(t *testing.T) {
	/* Deblock one MB, calculate the Bs inside the function, only consider the intra / intra block */
	var sDqLayer SDqLayer
	sDqLayer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader.eSliceType = common.P_SLICE
	sDqLayer.pDec = nil
	var sFilter SDeblockingFilter
	var sDBFunc SDeblockingFunc
	sFilter.pLoopf = &sDBFunc
	sFilter.pLoopf.pfChromaDeblockingLT4Hor = UT_DeblockingFuncChromaLT4Func
	sFilter.pLoopf.pfChromaDeblockingLT4Ver = UT_DeblockingFuncChromaLT4Func
	sFilter.pLoopf.pfChromaDeblockingEQ4Hor = UT_DeblockingFuncChromaEQ4Func
	sFilter.pLoopf.pfChromaDeblockingEQ4Ver = UT_DeblockingFuncChromaEQ4Func
	sFilter.pLoopf.pfLumaDeblockingLT4Hor = UT_DeblockingFuncLumaLT4Func
	sFilter.pLoopf.pfLumaDeblockingEQ4Hor = UT_DeblockingFuncLumaEQ4Func
	sFilter.pLoopf.pfLumaDeblockingLT4Ver = UT_DeblockingFuncLumaLT4Func
	sFilter.pLoopf.pfLumaDeblockingEQ4Ver = UT_DeblockingFuncLumaEQ4Func
	sFilter.pRefPics[0], sFilter.pRefPics[1] = nil, nil // Don't need Ref pics for intra tests

	sDqLayer.iMbX, sDqLayer.iMbY = 0, 0
	sDqLayer.iMbXyIndex = 1
	sDqLayer.iMbWidth = 1

	sDqLayer.pTransformSize8x8Flag = make([]bool, 50)
	sDqLayer.pNzc = make([][24]int8, 2)

	iY := make([]uint8, 50)
	sFilter.pCsData[0] = iY
	sFilter.iCsStride[0] = 4

	iCb := make([]uint8, 9)
	iCr := make([]uint8, 9)
	sFilter.pCsData[1] = iCb
	sFilter.pCsData[2] = iCr
	sFilter.iCsStride[1] = 2

	iLumaQP := make([]int8, 50)
	iChromaQP := make([][2]int8, 9)
	sDqLayer.pLumaQp = iLumaQP
	sDqLayer.pChromaQp = iChromaQP

	iMbType := make([]uint32, 2)
	sDqLayer.pMbType = iMbType
	sDqLayer.pMbType[0] = common.MB_TYPE_INTRA4x4
	sDqLayer.pMbType[1] = common.MB_TYPE_INTRA4x4

	sFilter.iSliceAlphaC0Offset = 0
	sFilter.iSliceBetaOffset = 0

	test := func(iBoundFlag int32, iQP int, iLumaV0, iLumaV1, iLumaV2, iChromaV0, iChromaV1, iChromaV2 uint8) {
		t.Helper()
		for k := range iLumaQP {
			iLumaQP[k] = int8(iQP)
		}
		for k := range iChromaQP {
			iChromaQP[k] = [2]int8{int8(iQP), int8(iQP)}
		}
		clear(iY)
		clear(iCb)
		clear(iCr)
		WelsDeblockingMb(&sDqLayer, &sFilter, iBoundFlag)
		ls := int(sFilter.iCsStride[0])
		lsUV := int(sFilter.iCsStride[1])
		ok := iY[0] == iLumaV0 &&
			iY[1<<2] == iLumaV1 && iY[2<<2] == iLumaV1 && iY[3<<2] == iLumaV1 &&
			iY[(1<<2)*ls] == iLumaV2 && iY[(2<<2)*ls] == iLumaV2 && iY[(3<<2)*ls] == iLumaV2 &&
			iCb[0] == iChromaV0 && iCr[0] == iChromaV0 &&
			iCb[2<<1] == iChromaV1 && iCr[2<<1] == iChromaV1 &&
			iCb[(2<<1)*lsUV] == iChromaV2 && iCr[(2<<1)*lsUV] == iChromaV2
		if !ok {
			t.Errorf("qp %d mbtype %#x: Y %v Cb %v Cr %v", iQP, sDqLayer.pMbType[1], iY, iCb, iCr)
		}
	}

	// QP>16, LEFT & TOP, Intra mode MB_TYPE_INTRA4x4
	sDqLayer.pMbType[1] = common.MB_TYPE_INTRA4x4
	test(0x03, 16+rand.Intn(35), 2, 1, 1, 2, 1, 1)

	// QP>16, LEFT & TOP, Intra mode MB_TYPE_INTRA16x16
	sDqLayer.pMbType[1] = common.MB_TYPE_INTRA16x16
	test(0x03, 16+rand.Intn(35), 2, 1, 1, 2, 1, 1)

	// MbType==0x03, Intra8x8 has not been supported now.

	// QP>16, LEFT & TOP, Intra mode MB_TYPE_INTRA_PCM
	sDqLayer.pMbType[1] = common.MB_TYPE_INTRA_PCM
	test(0x03, 16+rand.Intn(35), 2, 1, 1, 2, 1, 1)

	// QP>16, LEFT & TOP, neighbor is Intra
	sDqLayer.pMbType[0] = common.MB_TYPE_INTRA16x16
	sDqLayer.pMbType[1] = common.MB_TYPE_SKIP // Internal SKIP, Bs==0
	test(0x03, 16+rand.Intn(35), 2, 0, 0, 2, 0, 0)

	// QP<15, no output
	sDqLayer.pMbType[1] = common.MB_TYPE_INTRA_PCM
	test(0x03, rand.Intn(16), 0, 0, 0, 0, 0, 0)
}
