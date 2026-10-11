package encoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestMvdCostInit(t *testing.T) {
	const kiMvdSz = 648*2 + 1
	const kiStride = kiMvdSz
	pTable := make([]uint16, 52*kiStride)
	MvdCostInit(pTable, kiMvdSz)
	kiSz := kiMvdSz >> 1
	for iQp := 0; iQp < 52; iQp++ {
		kiLambda := uint32(g_kiQpCostTable[iQp])
		iCentre := iQp*kiStride + kiSz
		if got := pTable[iCentre]; uint32(got) != kiLambda {
			t.Fatalf("qp %d: centre %d, want %d", iQp, got, kiLambda)
		}
		for _, v := range []int32{1, 2, 3, 7, 100, 647, 648} {
			want := uint16(kiLambda * BsSizeSE(v))
			if got := pTable[iCentre+int(v)]; got != want {
				t.Fatalf("qp %d mvd %d: got %d want %d", iQp, v, got, want)
			}
			want = uint16(kiLambda * BsSizeSE(-v))
			if got := pTable[iCentre-int(v)]; got != want {
				t.Fatalf("qp %d mvd %d: got %d want %d", iQp, -v, got, want)
			}
		}
	}
}

func TestMdInterAnalysisVaaInfo(t *testing.T) {
	if got := MdInterAnalysisVaaInfo_c([]int32{1000, 1000, 1000, 1000}); got != 15 {
		t.Fatalf("flat: got %d", got)
	}
	// top half larger: blocks 0 and 1 above average -> 0x08|0x04
	if got := MdInterAnalysisVaaInfo_c([]int32{4000, 4000, 100, 100}); got != 12 {
		t.Fatalf("hor: got %d", got)
	}
	if got := MdInterAnalysisVaaInfo_c([]int32{4000, 100, 100, 4000}); got != 9 {
		t.Fatalf("cmpx: got %d", got)
	}
}

func TestAnalysisVaaInfoIntra(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	const kiStride = 40
	buf := make([]uint8, kiStride*20)
	for i := range buf {
		buf[i] = uint8(r.Intn(256))
	}
	iOff := kiStride*2 + 3
	// reference computation
	var avg [16]int32
	for by := 0; by < 4; by++ {
		for bx := 0; bx < 4; bx++ {
			s := int32(0)
			for y := 0; y < 4; y++ {
				for x := 0; x < 4; x++ {
					s += int32(buf[iOff+(by*4+y)*kiStride+bx*4+x])
				}
			}
			avg[by*4+bx] = s >> 4
		}
	}
	var iSumAvg, iSumSqr int32
	for _, v := range avg {
		iSumAvg += v
		iSumSqr += v * v
	}
	want := iSumSqr - ((iSumAvg * iSumAvg) >> 4)
	if got := AnalysisVaaInfoIntra_c(buf, iOff, kiStride); got != want {
		t.Fatalf("got %d want %d", got, want)
	}
}

func TestPredictSad(t *testing.T) {
	var ref [30]int8
	sad := []int32{10, 20, 30, 40}
	// only left available
	ref[1], ref[5], ref[0], ref[6] = common.REF_NOT_AVAIL, common.REF_NOT_AVAIL, common.REF_NOT_AVAIL, 0
	var iPred int32
	PredictSad(ref[:], sad, 0, &iPred)
	x := int32(40) << 6
	if want := (x - (x >> 3) + (x >> 5) + 32) >> 6; iPred != want {
		t.Fatalf("got %d want %d", iPred, want)
	}
	// all available, ref 0: median
	ref[1], ref[5], ref[0], ref[6] = 0, 0, 0, 0
	PredictSad(ref[:], sad, 0, &iPred)
	x = common.WelsMedian(40, 20, 30) << 6
	if want := (x - (x >> 3) + (x >> 5) + 32) >> 6; iPred != want {
		t.Fatalf("median: got %d want %d", iPred, want)
	}

	var iSkip int32
	skip := []bool{false, true, false, false}
	PredictSadSkip(ref[:], skip, sad, 0, &iSkip)
	if iSkip != 20 { // only top is a skip MB
		t.Fatalf("skip: got %d", iSkip)
	}
}

func TestPredIntra4x4Mode(t *testing.T) {
	var modes [48]int8
	modes[9-8] = 5
	modes[9-1] = 3
	if got := PredIntra4x4Mode(modes[:], 9); got != 3 {
		t.Fatalf("got %d", got)
	}
	modes[9-1] = -1
	if got := PredIntra4x4Mode(modes[:], 9); got != 2 {
		t.Fatalf("unavail: got %d", got)
	}
}

func TestInitBlkStrideWithRef(t *testing.T) {
	var s [16]int32
	InitBlkStrideWithRef(s[:], 100)
	if s[0] != 0 || s[1] != 4 || s[2] != 400 || s[3] != 404 || s[15] != 12+1200 {
		t.Fatalf("got %v", s)
	}
}

// newRefineTestCtx builds the minimal context MeRefineFracPixel needs.
func newRefineTestCtx(kiStrideEnc, kiStrideRef int32) *sWelsEncCtx {
	pFunc := &SWelsFuncPtrList{}
	common.InitMcFunc(&pFunc.sMcFuncs, 0)
	pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16] = common.WelsSampleSad16x16_c
	pFunc.sSampleDealingFuncs.pfMeCost = &pFunc.sSampleDealingFuncs.pfSampleSad
	pFunc.pfCopy16x16NotAligned = common.WelsCopy16x16_c
	return &sWelsEncCtx{
		pFuncList: pFunc,
		pCurDqLayer: &SDqLayer{
			iEncStride: [3]int32{kiStrideEnc, kiStrideEnc >> 1, kiStrideEnc >> 1},
			pRefPic:    &SPicture{iLineSize: [3]int32{kiStrideRef, kiStrideRef >> 1, kiStrideRef >> 1}},
		},
	}
}

func TestMeRefineFracPixel(t *testing.T) {
	const kiStride = 64
	r := rand.New(rand.NewSource(7))
	pRefPic := make([]uint8, kiStride*64)
	for i := range pRefPic {
		pRefPic[i] = uint8(r.Intn(256))
	}
	iRefOff := 16*kiStride + 16

	const kiMvdSz = 648*2 + 1
	pMvdCost := make([]uint16, 52*kiMvdSz)
	MvdCostInit(pMvdCost, kiMvdSz)
	iMvdOff := 26*kiMvdSz + kiMvdSz>>1

	var sMbCache SMbCache
	sMbCache.pBufferInterPredMe = make([]uint8, 4*640)
	pPred := make([]uint8, 256)

	run := func(pEnc []uint8) *SWelsME {
		pEncCtx := newRefineTestCtx(16, kiStride)
		var sMe SWelsME
		sMe.pMvdCost, sMe.iMvdCostOff = pMvdCost, iMvdOff
		sMe.uiBlockSize = BLOCK_16x16
		sMe.pEncMb, sMe.iEncMbOff = pEnc, 0
		sMe.pRefMb, sMe.iRefMbOff = pRefPic, iRefOff
		var sMeRefine SMeRefinePointer
		InitMeRefinePointer(&sMeRefine, &sMbCache, 0)
		sMeRefine.pfCopyBlockByMode = pEncCtx.pFuncList.pfCopy16x16NotAligned
		MeRefineFracPixel(pEncCtx, pPred, 0, &sMe, &sMeRefine, 16, 16)
		return &sMe
	}

	// 1) source equals the integer position: MV stays (0,0)
	pEnc := make([]uint8, 256)
	common.WelsCopy16x16_c(pEnc, 0, 16, pRefPic, iRefOff, kiStride)
	sMe := run(pEnc)
	if sMe.sMv != (SMVUnitXY{}) {
		t.Fatalf("integer: mv %+v", sMe.sMv)
	}
	if want := uint32(COST_MVD(pMvdCost, iMvdOff, 0, 0)); sMe.uiSatdCost != want {
		t.Fatalf("integer: cost %d want %d", sMe.uiSatdCost, want)
	}
	for i := range pEnc {
		if pPred[i] != pEnc[i] {
			t.Fatalf("integer: pred mismatch at %d", i)
		}
	}

	// 2) source equals the horizontal half-pel position to the right: MV (2,0)
	common.McHorVer20_c(pRefPic, iRefOff, kiStride, pEnc, 0, 16, 16, 16)
	sMe = run(pEnc)
	if sMe.sMv != (SMVUnitXY{2, 0}) {
		t.Fatalf("half: mv %+v", sMe.sMv)
	}
	for i := range pEnc {
		if pPred[i] != pEnc[i] {
			t.Fatalf("half: pred mismatch at %d", i)
		}
	}
}

func TestCheckBorderAndStatic(t *testing.T) {
	if !CheckBorder(0, 0, -1, 0, 10, 10) || CheckBorder(1, 1, -16, -16, 10, 10) || !CheckBorder(9, 0, 1, 0, 10, 10) {
		t.Fatal("CheckBorder")
	}
	blk := []int32{1, 1, 1, 1}
	if !IsMbCollocatedStatic(blk) || IsMbScrolledStatic(blk) || IsMbStatic(nil, 1) {
		t.Fatal("IsMbStatic")
	}
}
