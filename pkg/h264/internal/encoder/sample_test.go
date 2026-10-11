// Port of test/encoder/EncUT_Sample.cpp (C paths only; the SIMD-vs-C
// comparisons are replaced by checks of the C combined-intra functions
// against the individual predictors and cost functions).

package encoder

import (
	"math"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const samplePixelStride = 32

type sadSatdFixture struct {
	r                  *rand.Rand
	pPixSrcA, pPixSrcB []uint8
	iStrideA, iStrideB int32
}

func newSadSatdFixture(seed int64) *sadSatdFixture {
	f := &sadSatdFixture{r: rand.New(rand.NewSource(seed))}
	f.iStrideA = int32(f.r.Intn(256) + samplePixelStride)
	f.iStrideB = int32(f.r.Intn(256) + samplePixelStride)
	f.pPixSrcA = make([]uint8, f.iStrideA<<5)
	f.pPixSrcB = make([]uint8, f.iStrideB<<5)
	for i := range f.pPixSrcA {
		f.pPixSrcA[i] = uint8(f.r.Intn(256))
	}
	for i := range f.pPixSrcB {
		f.pPixSrcB[i] = uint8(f.r.Intn(256))
	}
	return f
}

// satd4x4Ref is the reference from EncUT_Sample.cpp (TEST_F WelsSampleSatd4x4_c).
func satd4x4Ref(pPixA []uint8, offA int, strideA int32, pPixB []uint8, offB int, strideB int32) int32 {
	var W, T, Y [16]int32
	k := 0
	for i := 0; i < 4; i++ {
		for j := 0; j < 4; j++ {
			W[k] = int32(pPixA[offA+j]) - int32(pPixB[offB+j])
			k++
		}
		offA += int(strideA)
		offB += int(strideB)
	}
	for c := 0; c < 4; c++ {
		T[c] = W[c] + W[4+c] + W[8+c] + W[12+c]
		T[4+c] = W[c] + W[4+c] - W[8+c] - W[12+c]
		T[8+c] = W[c] - W[4+c] - W[8+c] + W[12+c]
		T[12+c] = W[c] - W[4+c] + W[8+c] - W[12+c]
	}
	for r := 0; r < 4; r++ {
		b := 4 * r
		Y[b+0] = T[b] + T[b+1] + T[b+2] + T[b+3]
		Y[b+1] = T[b] + T[b+1] - T[b+2] - T[b+3]
		Y[b+2] = T[b] - T[b+1] - T[b+2] + T[b+3]
		Y[b+3] = T[b] - T[b+1] + T[b+2] - T[b+3]
	}
	var iSumSatd int32
	for i := 0; i < 16; i++ {
		iSumSatd += common.WELS_ABS(Y[i])
	}
	return (iSumSatd + 1) >> 1
}

func TestSadSatdCFuncTest_WelsSampleSatd4x4_c(t *testing.T) {
	for it := 0; it < 50; it++ {
		f := newSadSatdFixture(int64(100 + it))
		want := satd4x4Ref(f.pPixSrcA, 0, f.iStrideA, f.pPixSrcB, 0, f.iStrideB)
		if got := WelsSampleSatd4x4_c(f.pPixSrcA, 0, f.iStrideA, f.pPixSrcB, 0, f.iStrideB); got != want {
			t.Fatalf("WelsSampleSatd4x4_c = %d, want %d", got, want)
		}
	}
}

func TestSadSatdCFuncTest_WelsSampleSatdNxM_c(t *testing.T) {
	f := newSadSatdFixture(7)
	var sFuncList SWelsFuncPtrList
	WelsInitSampleSadFunc(&sFuncList, 0)
	dims := [BLOCK_SIZE_ALL][2]int{
		BLOCK_16x16: {16, 16}, BLOCK_16x8: {16, 8}, BLOCK_8x16: {8, 16}, BLOCK_8x8: {8, 8},
		BLOCK_4x4: {4, 4}, BLOCK_8x4: {8, 4}, BLOCK_4x8: {4, 8},
	}
	for b := 0; b < BLOCK_SIZE_ALL; b++ {
		var want int32
		for y := 0; y < dims[b][1]; y += 4 {
			for x := 0; x < dims[b][0]; x += 4 {
				want += satd4x4Ref(f.pPixSrcA, y*int(f.iStrideA)+x, f.iStrideA, f.pPixSrcB, y*int(f.iStrideB)+x, f.iStrideB)
			}
		}
		if got := sFuncList.sSampleDealingFuncs.pfSampleSatd[b](f.pPixSrcA, 0, f.iStrideA, f.pPixSrcB, 0, f.iStrideB); got != want {
			t.Fatalf("satd block %d = %d, want %d", b, got, want)
		}
	}
}

func TestSadSatdCFuncTest_WelsSampleSad_c(t *testing.T) {
	f := newSadSatdFixture(8)
	var sFuncList SWelsFuncPtrList
	WelsInitSampleSadFunc(&sFuncList, 0)
	dims := [BLOCK_SIZE_ALL][2]int{
		BLOCK_16x16: {16, 16}, BLOCK_16x8: {16, 8}, BLOCK_8x16: {8, 16}, BLOCK_8x8: {8, 8},
		BLOCK_4x4: {4, 4}, BLOCK_8x4: {8, 4}, BLOCK_4x8: {4, 8},
	}
	for b := 0; b < BLOCK_SIZE_ALL; b++ {
		var iSumSad int32
		for i := 0; i < dims[b][1]; i++ {
			for j := 0; j < dims[b][0]; j++ {
				iSumSad += common.WELS_ABS(int32(f.pPixSrcA[i*int(f.iStrideA)+j]) - int32(f.pPixSrcB[i*int(f.iStrideB)+j]))
			}
		}
		if got := sFuncList.sSampleDealingFuncs.pfSampleSad[b](f.pPixSrcA, 0, f.iStrideA, f.pPixSrcB, 0, f.iStrideB); got != iSumSad {
			t.Fatalf("sad block %d = %d, want %d", b, got, iSumSad)
		}

		// WelsSampleSadFour*: up, down, left, right neighbours summed (pPixB at +stride).
		pPixB := int(f.iStrideB)
		var iSumSad4 int32
		for i := 0; i < dims[b][1]; i++ {
			for j := 0; j < dims[b][0]; j++ {
				a := int32(f.pPixSrcA[i*int(f.iStrideA)+j])
				o := pPixB + i*int(f.iStrideB) + j
				iSumSad4 += common.WELS_ABS(a - int32(f.pPixSrcB[o-1]))
				iSumSad4 += common.WELS_ABS(a - int32(f.pPixSrcB[o+1]))
				iSumSad4 += common.WELS_ABS(a - int32(f.pPixSrcB[o-int(f.iStrideB)]))
				iSumSad4 += common.WELS_ABS(a - int32(f.pPixSrcB[o+int(f.iStrideB)]))
			}
		}
		var pSad [4]int32
		sFuncList.sSampleDealingFuncs.pfSample4Sad[b](f.pPixSrcA, 0, f.iStrideA, f.pPixSrcB, pPixB, f.iStrideB, pSad[:])
		if got := pSad[0] + pSad[1] + pSad[2] + pSad[3]; got != iSumSad4 {
			t.Fatalf("sad four block %d = %d, want %d", b, got, iSumSad4)
		}
	}
}

// pickBest mirrors the "first strictly smaller wins" selection of the
// combined functions, evaluated in the C order of the candidate list.
func pickBest(costs []int32, modes []int32) (int32, int32) {
	best, mode := int32(math.MaxInt32), int32(-1)
	for i, c := range costs {
		if c < best {
			best, mode = c, modes[i]
		}
	}
	return best, mode
}

func TestIntraSadSatdFuncTest_Combined3_c(t *testing.T) {
	r := rand.New(rand.NewSource(30))
	const iLineSizeDec, iLineSizeEnc = 32, 32
	for it := 0; it < 100; it++ {
		pDec := make([]uint8, iLineSizeDec<<5)
		pEnc := make([]uint8, iLineSizeEnc<<5)
		pDecCr := make([]uint8, iLineSizeDec<<5)
		pEncCr := make([]uint8, iLineSizeEnc<<5)
		for i := range pDec {
			pDec[i] = uint8(r.Intn(256))
			pDecCr[i] = uint8(r.Intn(256))
		}
		for i := range pEnc {
			pEnc[i] = uint8(r.Intn(256))
			pEncCr[i] = uint8(r.Intn(256))
		}
		iLambda := int32(50)
		dec := 128 // pDec + 128

		// 16x16
		for _, sad := range []bool{false, true} {
			cost := WelsSampleSatd16x16_c
			fn := WelsSampleSatdIntra16x16Combined3_c
			if sad {
				cost = common.WelsSampleSad16x16_c
				fn = WelsSampleSadIntra16x16Combined3_c
			}
			var tmp [256]uint8
			common.WelsI16x16LumaPredV_c(tmp[:], 0, pDec, dec, iLineSizeDec)
			cV := cost(tmp[:], 0, 16, pEnc, 0, iLineSizeEnc)
			common.WelsI16x16LumaPredH_c(tmp[:], 0, pDec, dec, iLineSizeDec)
			cH := cost(tmp[:], 0, 16, pEnc, 0, iLineSizeEnc) + iLambda*2
			WelsI16x16LumaPredDc_c(tmp[:], 0, pDec, dec, iLineSizeDec)
			cDc := cost(tmp[:], 0, 16, pEnc, 0, iLineSizeEnc) + iLambda*2
			wantCost, wantMode := pickBest([]int32{cV, cH, cDc}, []int32{0, 1, 2})
			pDst := make([]uint8, 512)
			var iBestMode int32
			if got := fn(pDec, dec, iLineSizeDec, pEnc, 0, iLineSizeEnc, &iBestMode, iLambda, pDst, 0); got != wantCost || iBestMode != wantMode {
				t.Fatalf("16x16 sad=%v: (%d,%d) want (%d,%d)", sad, got, iBestMode, wantCost, wantMode)
			}
		}

		// chroma 8x8
		for _, sad := range []bool{false, true} {
			cost := WelsSampleSatd8x8_c
			fn := WelsSampleSatdIntra8x8Combined3_c
			if sad {
				cost = common.WelsSampleSad8x8_c
				fn = WelsSampleSadIntra8x8Combined3_c
			}
			var tCb, tCr [64]uint8
			eval := func(pred PGetIntraPredFunc) int32 {
				pred(tCb[:], 0, pDec, dec, iLineSizeDec)
				pred(tCr[:], 0, pDecCr, dec, iLineSizeDec)
				return cost(tCb[:], 0, 8, pEnc, 0, iLineSizeEnc) + cost(tCr[:], 0, 8, pEncCr, 0, iLineSizeEnc)
			}
			cV := eval(WelsIChromaPredV_c) + iLambda*2
			cH := eval(WelsIChromaPredH_c) + iLambda*2
			cDc := eval(WelsIChromaPredDc_c)
			wantCost, wantMode := pickBest([]int32{cV, cH, cDc}, []int32{2, 1, 0})
			pDst := make([]uint8, 512)
			var iBestMode int32
			if got := fn(pDec, dec, iLineSizeDec, pEnc, 0, iLineSizeEnc, &iBestMode, iLambda, pDst, 0, pDecCr, dec, pEncCr, 0); got != wantCost || iBestMode != wantMode {
				t.Fatalf("8x8 sad=%v: (%d,%d) want (%d,%d)", sad, got, iBestMode, wantCost, wantMode)
			}
		}

		// 4x4
		lambda := [2]int32{iLambda << 2, iLambda}
		iPredMode := r.Intn(3)
		b2i := func(b bool) int {
			if b {
				return 1
			}
			return 0
		}
		l2, l1, l0 := lambda[b2i(iPredMode == 2)], lambda[b2i(iPredMode == 1)], lambda[b2i(iPredMode == 0)]
		var pr [3][16]uint8
		WelsI4x4LumaPredDc_c(pr[2][:], 0, pDec, dec, iLineSizeDec)
		WelsI4x4LumaPredH_c(pr[1][:], 0, pDec, dec, iLineSizeDec)
		WelsI4x4LumaPredV_c(pr[0][:], 0, pDec, dec, iLineSizeDec)
		cDc := WelsSampleSatd4x4_c(pr[2][:], 0, 4, pEnc, 0, iLineSizeEnc) + l2
		cH := WelsSampleSatd4x4_c(pr[1][:], 0, 4, pEnc, 0, iLineSizeEnc) + l1
		cV := WelsSampleSatd4x4_c(pr[0][:], 0, 4, pEnc, 0, iLineSizeEnc) + l0
		wantCost, wantMode := pickBest([]int32{cDc, cH, cV}, []int32{2, 1, 0})
		pDst := make([]uint8, 512)
		var iBestMode int32
		if got := WelsSampleSatdIntra4x4Combined3_c(pDec, dec, iLineSizeDec, pEnc, 0, iLineSizeEnc, pDst, 0, &iBestMode, l2, l1, l0); got != wantCost || iBestMode != wantMode {
			t.Fatalf("4x4: (%d,%d) want (%d,%d)", got, iBestMode, wantCost, wantMode)
		}
		for i := 0; i < 16; i++ {
			if pDst[i] != pr[wantMode][i] {
				t.Fatalf("4x4: pDst[%d] = %d want %d", i, pDst[i], pr[wantMode][i])
			}
		}
	}
}
