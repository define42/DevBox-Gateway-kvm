package encoder

// Ports of test/encoder/EncUT_EncoderMb.cpp, EncUT_EncoderMbAux.cpp and
// EncUT_MBCopy.cpp (C parts only).

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// ---------------------------------------------------------------------------
// EncUT_EncoderMb.cpp
// ---------------------------------------------------------------------------

var g_kiQuantInterFFCompare = [104][8]int16{
	/* 0*/ {0, 1, 0, 1, 1, 1, 1, 1},
	/* 1*/ {0, 1, 0, 1, 1, 1, 1, 1},
	/* 2*/ {1, 1, 1, 1, 1, 1, 1, 1},
	/* 3*/ {1, 1, 1, 1, 1, 2, 1, 2},
	/* 4*/ {1, 1, 1, 1, 1, 2, 1, 2},
	/* 5*/ {1, 1, 1, 1, 1, 2, 1, 2},
	/* 6*/ {1, 1, 1, 1, 1, 2, 1, 2},
	/* 7*/ {1, 2, 1, 2, 2, 2, 2, 2},
	/* 8*/ {1, 2, 1, 2, 2, 3, 2, 3},
	/* 9*/ {1, 2, 1, 2, 2, 3, 2, 3},
	/*10*/ {1, 2, 1, 2, 2, 3, 2, 3},
	/*11*/ {2, 2, 2, 2, 2, 4, 2, 4},
	/*12*/ {2, 3, 2, 3, 3, 4, 3, 4},
	/*13*/ {2, 3, 2, 3, 3, 5, 3, 5},
	/*14*/ {2, 3, 2, 3, 3, 5, 3, 5},
	/*15*/ {2, 4, 2, 4, 4, 6, 4, 6},
	/*16*/ {3, 4, 3, 4, 4, 7, 4, 7},
	/*17*/ {3, 5, 3, 5, 5, 8, 5, 8},
	/*18*/ {3, 6, 3, 6, 6, 9, 6, 9},
	/*19*/ {4, 6, 4, 6, 6, 10, 6, 10},
	/*20*/ {4, 7, 4, 7, 7, 11, 7, 11},
	/*21*/ {5, 8, 5, 8, 8, 12, 8, 12},
	/*22*/ {6, 9, 6, 9, 9, 13, 9, 13},
	/*23*/ {6, 10, 6, 10, 10, 16, 10, 16},
	/*24*/ {7, 11, 7, 11, 11, 17, 11, 17},
	/*25*/ {8, 12, 8, 12, 12, 19, 12, 19},
	/*26*/ {9, 14, 9, 14, 14, 21, 14, 21},
	/*27*/ {10, 15, 10, 15, 15, 25, 15, 25},
	/*28*/ {11, 17, 11, 17, 17, 27, 17, 27},
	/*29*/ {12, 20, 12, 20, 20, 31, 20, 31},
	/*30*/ {14, 22, 14, 22, 22, 34, 22, 34},
	/*31*/ {15, 24, 15, 24, 24, 39, 24, 39},
	/*32*/ {18, 27, 18, 27, 27, 43, 27, 43},
	/*33*/ {19, 31, 19, 31, 31, 49, 31, 49},
	/*34*/ {22, 34, 22, 34, 34, 54, 34, 54},
	/*35*/ {25, 40, 25, 40, 40, 62, 40, 62},
	/*36*/ {27, 45, 27, 45, 45, 69, 45, 69},
	/*37*/ {30, 48, 30, 48, 48, 77, 48, 77},
	/*38*/ {36, 55, 36, 55, 55, 86, 55, 86},
	/*39*/ {38, 62, 38, 62, 62, 99, 62, 99},
	/*40*/ {44, 69, 44, 69, 69, 107, 69, 107},
	/*41*/ {49, 79, 49, 79, 79, 125, 79, 125},
	/*42*/ {55, 89, 55, 89, 89, 137, 89, 137},
	/*43*/ {61, 96, 61, 96, 96, 154, 96, 154},
	/*44*/ {71, 110, 71, 110, 110, 171, 110, 171},
	/*45*/ {77, 124, 77, 124, 124, 198, 124, 198},
	/*46*/ {88, 137, 88, 137, 137, 217, 137, 217},
	/*47*/ {99, 159, 99, 159, 159, 250, 159, 250},
	/*48*/ {110, 179, 110, 179, 179, 275, 179, 275},
	/*49*/ {121, 191, 121, 191, 191, 313, 191, 313},
	/*50*/ {143, 221, 143, 221, 221, 341, 221, 341},
	/*51*/ {154, 245, 154, 245, 245, 402, 245, 402},
	//from here below is intra
	/* 0*/ {1, 1, 1, 1, 1, 2, 1, 2},
	/* 1*/ {1, 1, 1, 1, 1, 2, 1, 2},
	/* 2*/ {1, 2, 1, 2, 2, 3, 2, 3},
	/* 3*/ {1, 2, 1, 2, 2, 3, 2, 3},
	/* 4*/ {1, 2, 1, 2, 2, 3, 2, 3},
	/* 5*/ {1, 2, 1, 2, 2, 4, 2, 4},
	/* 6*/ {2, 3, 2, 3, 3, 4, 3, 4},
	/* 7*/ {2, 3, 2, 3, 3, 5, 3, 5},
	/* 8*/ {2, 3, 2, 3, 3, 5, 3, 5},
	/* 9*/ {2, 4, 2, 4, 4, 6, 4, 6},
	/*10*/ {3, 4, 3, 4, 4, 6, 4, 6},
	/*11*/ {3, 5, 3, 5, 5, 7, 5, 7},
	/*12*/ {3, 5, 3, 5, 5, 8, 5, 8},
	/*13*/ {4, 6, 4, 6, 6, 9, 6, 9},
	/*14*/ {4, 7, 4, 7, 7, 10, 7, 10},
	/*15*/ {5, 7, 5, 7, 7, 12, 7, 12},
	/*16*/ {5, 8, 5, 8, 8, 13, 8, 13},
	/*17*/ {6, 9, 6, 9, 9, 15, 9, 15},
	/*18*/ {7, 11, 7, 11, 11, 16, 11, 16},
	/*19*/ {7, 11, 7, 11, 11, 18, 11, 18},
	/*20*/ {9, 13, 9, 13, 13, 20, 13, 20},
	/*21*/ {9, 15, 9, 15, 15, 24, 15, 24},
	/*22*/ {11, 16, 11, 16, 16, 26, 16, 26},
	/*23*/ {12, 19, 12, 19, 19, 30, 19, 30},
	/*24*/ {13, 21, 13, 21, 21, 33, 21, 33},
	/*25*/ {14, 23, 14, 23, 23, 37, 23, 37},
	/*26*/ {17, 26, 17, 26, 26, 41, 26, 41},
	/*27*/ {18, 30, 18, 30, 30, 47, 30, 47},
	/*28*/ {21, 33, 21, 33, 33, 51, 33, 51},
	/*29*/ {24, 38, 24, 38, 38, 59, 38, 59},
	/*30*/ {26, 43, 26, 43, 43, 66, 43, 66},
	/*31*/ {29, 46, 29, 46, 46, 74, 46, 74},
	/*32*/ {34, 52, 34, 52, 52, 82, 52, 82},
	/*33*/ {37, 59, 37, 59, 59, 94, 59, 94},
	/*34*/ {42, 66, 42, 66, 66, 102, 66, 102},
	/*35*/ {47, 75, 47, 75, 75, 119, 75, 119},
	/*36*/ {52, 85, 52, 85, 85, 131, 85, 131},
	/*37*/ {58, 92, 58, 92, 92, 147, 92, 147},
	/*38*/ {68, 105, 68, 105, 105, 164, 105, 164},
	/*39*/ {73, 118, 73, 118, 118, 189, 118, 189},
	/*40*/ {84, 131, 84, 131, 131, 205, 131, 205},
	/*41*/ {94, 151, 94, 151, 151, 239, 151, 239},
	/*42*/ {105, 171, 105, 171, 171, 262, 171, 262},
	/*43*/ {116, 184, 116, 184, 184, 295, 184, 295},
	/*44*/ {136, 211, 136, 211, 211, 326, 211, 326},
	/*45*/ {147, 236, 147, 236, 236, 377, 236, 377},
	/*46*/ {168, 262, 168, 262, 262, 414, 262, 414},
	/*47*/ {189, 303, 189, 303, 303, 478, 303, 478},
	/*48*/ {211, 341, 211, 341, 341, 524, 341, 524},
	/*49*/ {231, 364, 231, 364, 364, 597, 364, 597},
	/*50*/ {272, 422, 272, 422, 422, 652, 422, 652},
	/*51*/ {295, 467, 295, 467, 467, 768, 467, 768},
}

const thValue = 2

func randomPixelDataGenerator(r *rand.Rand, p []uint8, iWidth, iHeight, iStride int) {
	for i := 0; i < iHeight; i++ {
		for j := 0; j < iWidth; j++ {
			p[i*iStride+j] = uint8(r.Intn(256))
		}
	}
}

func checkQuantDelta(t *testing.T, qp uint32, pDct, pDctCompare []int16, n int) {
	t.Helper()
	for i := 0; i < n; i++ {
		iDeta := common.WELS_ABS(int32(pDct[i]) - int32(pDctCompare[i]))
		if iDeta < thValue {
			iDeta = 0
		}
		if iDeta != 0 {
			t.Fatalf("qp %d idx %d: %d vs %d", qp, i, pDct[i], pDctCompare[i])
		}
	}
}

func testQuant(t *testing.T, r *rand.Rand, qp uint32, pSrc, pPred []uint8, pDct, pDctCompare []int16, iWidth, iHeight int) {
	pMf := g_kiQuantMF[qp][:]
	pFfCompareI := g_kiQuantInterFFCompare[52+qp][:]
	pFfCompareP := g_kiQuantInterFFCompare[qp][:]
	pFfI := g_kiQuantInterFF[6+qp][:]
	pFfP := g_kiQuantInterFF[qp][:]

	//quant4x4  Intra MB
	randomPixelDataGenerator(r, pSrc, iWidth, iHeight, iWidth)
	randomPixelDataGenerator(r, pPred, iWidth, iHeight, iWidth)
	for i := 0; i < 16; i++ {
		pDct[i] = int16(int32(pSrc[i]) - int32(pPred[i]))
		pDctCompare[i] = pDct[i]
	}
	WelsQuant4x4_c(pDct, pFfI, pMf)
	WelsQuant4x4_c(pDctCompare, pFfCompareI, pMf)
	checkQuantDelta(t, qp, pDct, pDctCompare, 16)

	//quant4x4 DC
	randomPixelDataGenerator(r, pSrc, iWidth, iHeight, iWidth)
	randomPixelDataGenerator(r, pPred, iWidth, iHeight, iWidth)
	for i := 0; i < 16; i++ {
		pDct[i] = int16(int32(pSrc[i]) - int32(pPred[i]))
		pDctCompare[i] = pDct[i]
	}
	WelsQuant4x4Dc_c(pDct, int16(int32(pFfI[0])<<1), pMf[0]>>1)
	WelsQuant4x4Dc_c(pDctCompare, int16(int32(pFfCompareI[0])<<1), pMf[0]>>1)
	checkQuantDelta(t, qp, pDct, pDctCompare, 16)

	//quant4x4 Inter MB
	randomPixelDataGenerator(r, pSrc, iWidth, iHeight, iWidth)
	randomPixelDataGenerator(r, pPred, iWidth, iHeight, iWidth)
	for i := 0; i < 64; i++ {
		pDct[i] = int16(int32(pSrc[i]) - int32(pPred[i]))
		pDctCompare[i] = pDct[i]
	}
	WelsQuantFour4x4_c(pDct, pFfP, pMf)
	WelsQuantFour4x4_c(pDctCompare, pFfCompareP, pMf)
	checkQuantDelta(t, qp, pDct, pDctCompare, 64)
}

func TestEncoderMbTest_TestQuantTable(t *testing.T) {
	r := rand.New(rand.NewSource(1))
	iWidth, iHeight := 16, 16
	pSrc := make([]uint8, iWidth*iHeight)
	pPred := make([]uint8, iWidth*iHeight)
	pDct := make([]int16, 64)
	pDctCompare := make([]int16, 64)
	for iQP := uint32(0); iQP < 51; iQP++ {
		testQuant(t, r, iQP, pSrc, pPred, pDct, pDctCompare, iWidth, iHeight)
	}
}

// ---------------------------------------------------------------------------
// EncUT_EncoderMbAux.cpp
// ---------------------------------------------------------------------------

func TestEncodeMbAuxTest_TestScan_4x4_ac_c(t *testing.T) {
	r := rand.New(rand.NewSource(2))
	iLevel := make([]int16, 16)
	iDctA := make([]int16, 16)
	iDctB := make([]int16, 16)
	for i := 0; i < 16; i++ {
		iDctA[i] = int16(r.Intn(256) + 1)
		iDctB[i] = iDctA[i]
	}
	WelsScan4x4Ac_c(iLevel, iDctA)
	exp := []int{1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	for i, k := range exp {
		if iLevel[i] != iDctB[k] {
			t.Fatalf("iLevel[%d]=%d want %d", i, iLevel[i], iDctB[k])
		}
	}
	if iLevel[15] != 0 {
		t.Fatalf("iLevel[15]=%d want 0", iLevel[15])
	}
}

func TestEncodeMbAuxTest_TestScan_4x4_dcc(t *testing.T) {
	r := rand.New(rand.NewSource(3))
	iLevel := make([]int16, 16)
	iDctA := make([]int16, 16)
	iDctB := make([]int16, 16)
	for i := 0; i < 16; i++ {
		iDctA[i] = int16(r.Intn(256) + 1)
		iDctB[i] = iDctA[i]
	}
	WelsScan4x4Dc(iLevel, iDctA)
	exp := []int{0, 1, 4, 8, 5, 2, 3, 6, 9, 12, 13, 10, 7, 11, 14, 15}
	for i, k := range exp {
		if iLevel[i] != iDctB[k] {
			t.Fatalf("iLevel[%d]=%d want %d", i, iLevel[i], iDctB[k])
		}
	}
	// WelsScan4x4DcAc_c is the same scan.
	iLevel2 := make([]int16, 16)
	WelsScan4x4DcAc_c(iLevel2, iDctA)
	for i := range iLevel {
		if iLevel[i] != iLevel2[i] {
			t.Fatalf("DcAc scan mismatch at %d", i)
		}
	}
}

const (
	fENC_STRIDE = 16
	fDEC_STRIDE = 32
)

func pixelSubWH(iDiff []int16, iSize int, pPix1 []uint8, iStride1 int, pPix2 []uint8, iStride2 int) {
	o1, o2 := 0, 0
	for y := 0; y < iSize; y++ {
		for x := 0; x < iSize; x++ {
			iDiff[x+y*iSize] = int16(int32(pPix1[o1+x]) - int32(pPix2[o2+x]))
		}
		o1 += iStride1
		o2 += iStride2
	}
}

func sub4x4DctAnchor(iDct *[4][4]int16, pPix1 []uint8, pPix2 []uint8) {
	var iDiff [16]int16
	var tmp [4][4]int16
	pixelSubWH(iDiff[:], 4, pPix1, fENC_STRIDE, pPix2, fDEC_STRIDE)
	for i := 0; i < 4; i++ {
		a03 := int32(iDiff[i*4+0]) + int32(iDiff[i*4+3])
		a12 := int32(iDiff[i*4+1]) + int32(iDiff[i*4+2])
		s03 := int32(iDiff[i*4+0]) - int32(iDiff[i*4+3])
		s12 := int32(iDiff[i*4+1]) - int32(iDiff[i*4+2])
		tmp[0][i] = int16(a03 + a12)
		tmp[1][i] = int16(2*s03 + s12)
		tmp[2][i] = int16(a03 - a12)
		tmp[3][i] = int16(s03 - 2*s12)
	}
	for i := 0; i < 4; i++ {
		a03 := int32(tmp[i][0]) + int32(tmp[i][3])
		a12 := int32(tmp[i][1]) + int32(tmp[i][2])
		s03 := int32(tmp[i][0]) - int32(tmp[i][3])
		s12 := int32(tmp[i][1]) - int32(tmp[i][2])
		iDct[i][0] = int16(a03 + a12)
		iDct[i][1] = int16(2*s03 + s12)
		iDct[i][2] = int16(a03 - a12)
		iDct[i][3] = int16(s03 - 2*s12)
	}
}

func TestEncodeMbAuxTest_WelsDctT4_c(t *testing.T) {
	r := rand.New(rand.NewSource(4))
	for run := 0; run < 100; run++ {
		var iDctRef [4][4]int16
		uiPix1 := make([]uint8, 16*fENC_STRIDE)
		uiPix2 := make([]uint8, 16*fDEC_STRIDE)
		iDct := make([]int16, 16)
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				uiPix1[i*fENC_STRIDE+j] = uint8(r.Intn(256))
				uiPix2[i*fDEC_STRIDE+j] = uint8(r.Intn(256))
			}
		}
		sub4x4DctAnchor(&iDctRef, uiPix1, uiPix2)
		WelsDctT4_c(iDct, uiPix1, 0, fENC_STRIDE, uiPix2, 0, fDEC_STRIDE)
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				if iDctRef[j][i] != iDct[i*4+j] {
					t.Fatalf("run %d (%d,%d): %d vs %d", run, i, j, iDctRef[j][i], iDct[i*4+j])
				}
			}
		}
	}
}

func TestEncodeMbAuxTest_WelsDctFourT4_c(t *testing.T) {
	r := rand.New(rand.NewSource(5))
	for run := 0; run < 100; run++ {
		var iDctRef [4][4][4]int16
		uiPix1 := make([]uint8, 16*fENC_STRIDE)
		uiPix2 := make([]uint8, 16*fDEC_STRIDE)
		iDct := make([]int16, 16*4)
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				uiPix1[i*fENC_STRIDE+j] = uint8(r.Intn(256))
				uiPix2[i*fDEC_STRIDE+j] = uint8(r.Intn(256))
			}
		}
		sub4x4DctAnchor(&iDctRef[0], uiPix1[0:], uiPix2[0:])
		sub4x4DctAnchor(&iDctRef[1], uiPix1[4:], uiPix2[4:])
		sub4x4DctAnchor(&iDctRef[2], uiPix1[4*fENC_STRIDE:], uiPix2[4*fDEC_STRIDE:])
		sub4x4DctAnchor(&iDctRef[3], uiPix1[4*fENC_STRIDE+4:], uiPix2[4*fDEC_STRIDE+4:])
		WelsDctFourT4_c(iDct, uiPix1, 0, fENC_STRIDE, uiPix2, 0, fDEC_STRIDE)
		for k := 0; k < 4; k++ {
			for i := 0; i < 4; i++ {
				for j := 0; j < 4; j++ {
					if iDctRef[k][j][i] != iDct[k*16+i*4+j] {
						t.Fatalf("run %d k %d (%d,%d)", run, k, i, j)
					}
				}
			}
		}
	}
}

func testCopyFunc(t *testing.T, r *rand.Rand, width, height int, function PCopyFunc) {
	t.Helper()
	const iSStride = 64
	const iDStride = 64
	refSrc := make([]uint8, iSStride*height)
	refDst := make([]uint8, iDStride*height)
	dst := make([]uint8, iDStride*height)
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			refSrc[i*iSStride+j] = uint8(r.Intn(256))
		}
	}
	function(dst, 0, iDStride, refSrc, 0, iSStride)
	for i := 0; i < height; i++ {
		copy(refDst[i*iDStride:i*iDStride+width], refSrc[i*iSStride:i*iSStride+width])
	}
	for i := 0; i < height; i++ {
		for j := 0; j < width; j++ {
			if refDst[i*iDStride+j] != dst[i*iDStride+j] {
				t.Fatalf("%dx%d mismatch at (%d,%d)", width, height, i, j)
			}
		}
	}
}

func TestEncodeMbAuxTest_WelsCopy(t *testing.T) {
	r := rand.New(rand.NewSource(6))
	testCopyFunc(t, r, 4, 4, common.WelsCopy4x4_c)
	testCopyFunc(t, r, 8, 4, common.WelsCopy8x4_c)
	testCopyFunc(t, r, 4, 8, common.WelsCopy4x8_c)
	testCopyFunc(t, r, 8, 8, common.WelsCopy8x8_c)
	testCopyFunc(t, r, 8, 16, common.WelsCopy8x16_c)
	testCopyFunc(t, r, 16, 8, common.WelsCopy16x8_c)
	testCopyFunc(t, r, 16, 16, common.WelsCopy16x16_c)
}

// cRandInt mimics the range of the C rand() (0..RAND_MAX with RAND_MAX = 2^31-1).
func cRandInt(r *rand.Rand) int32 {
	return int32(r.Int31())
}

func TestEncodeMbAuxTest_WelsGetNoneZeroCount_c(t *testing.T) {
	r := rand.New(rand.NewSource(7))
	pLevel := make([]int16, 16)
	const numTestRuns = 1000
	for run := 0; run < numTestRuns; run++ {
		allZero := run == 0
		allNonzero := run == 1
		result := int32(0)
		for i := 0; i < 16; i++ {
			rv := cRandInt(r)
			if allZero {
				pLevel[i] = 0
			} else if allNonzero {
				v := rv%0xFFFF - 0x8000
				if v != 0 {
					pLevel[i] = int16(v)
				} else {
					pLevel[i] = 0x7FFF
				}
			} else {
				pLevel[i] = int16((rv >> 16 & 1) * ((rv & 0xFFFF) - 0x8000))
			}
			if pLevel[i] != 0 {
				result++
			}
		}
		if nnz := WelsGetNoneZeroCount_c(pLevel); nnz != result {
			t.Fatalf("run %d: nnz %d want %d", run, nnz, result)
		}
	}
}

// anchors (test-local WELS_ABS_LC / NEW_QUANT / WELS_NEW_QUANT)
func welsQuant4x4MaxAnchor(pDct []int16, ff []int16, mf []int16) int16 {
	maxAbs := int16(0)
	for i := 0; i < 16; i++ {
		j := i & 0x07
		sign := int32(pDct[i]) >> 31
		pDct[i] = int16(((int32(ff[j]) + ((sign ^ int32(pDct[i])) - sign)) * int32(mf[j])) >> 16)
		if pDct[i] > maxAbs {
			maxAbs = pDct[i]
		}
		pDct[i] = int16((sign ^ int32(pDct[i])) - sign)
	}
	return maxAbs
}

func welsQuant4x4DcAnchor(pDct []int16, iFF int16, iMF int16) {
	for i := 0; i < 16; i++ {
		sign := int32(pDct[i]) >> 31
		q := ((int32(iFF) + ((sign ^ int32(pDct[i])) - sign)) * int32(iMF)) >> 16
		pDct[i] = int16((sign ^ q) - sign)
	}
}

func TestEncodeMbAuxTest_WelsQuant4x4_c(t *testing.T) {
	r := rand.New(rand.NewSource(8))
	for run := 0; run < 100; run++ {
		ff := make([]int16, 8)
		mf := make([]int16, 8)
		iDctC := make([]int16, 16)
		iDctS := make([]int16, 16)
		for i := 0; i < 8; i++ {
			ff[i] = int16(cRandInt(r) & 32767)
			mf[i] = int16(cRandInt(r) & 32767)
		}
		for i := 0; i < 16; i++ {
			iDctC[i] = int16((cRandInt(r) & 65535) - 32768)
			iDctS[i] = iDctC[i]
		}
		welsQuant4x4MaxAnchor(iDctC, ff, mf)
		WelsQuant4x4_c(iDctS, ff, mf)
		for i := 0; i < 16; i++ {
			if iDctC[i] != iDctS[i] {
				t.Fatalf("run %d idx %d: %d vs %d", run, i, iDctC[i], iDctS[i])
			}
		}
	}
}

func TestEncodeMbAuxTest_WelsQuant4x4Dc_c(t *testing.T) {
	r := rand.New(rand.NewSource(9))
	for run := 0; run < 100; run++ {
		ff := int16(cRandInt(r) & 32767)
		mf := int16(cRandInt(r) & 32767)
		iDctC := make([]int16, 16)
		iDctS := make([]int16, 16)
		for i := 0; i < 16; i++ {
			iDctC[i] = int16((cRandInt(r) & 65535) - 32768)
			iDctS[i] = iDctC[i]
		}
		welsQuant4x4DcAnchor(iDctC, ff, mf)
		WelsQuant4x4Dc_c(iDctS, ff, mf)
		for i := 0; i < 16; i++ {
			if iDctC[i] != iDctS[i] {
				t.Fatalf("run %d idx %d: %d vs %d", run, i, iDctC[i], iDctS[i])
			}
		}
	}
}

func TestEncodeMbAuxTest_WelsQuantFour4x4_c(t *testing.T) {
	r := rand.New(rand.NewSource(10))
	for run := 0; run < 100; run++ {
		ff := make([]int16, 8)
		mf := make([]int16, 8)
		iDctC := make([]int16, 64)
		iDctS := make([]int16, 64)
		for i := 0; i < 8; i++ {
			ff[i] = int16(cRandInt(r) & 32767)
			mf[i] = int16(cRandInt(r) & 32767)
		}
		for i := 0; i < 64; i++ {
			iDctC[i] = int16((cRandInt(r) & 65535) - 32768)
			iDctS[i] = iDctC[i]
		}
		for i := 0; i < 4; i++ {
			welsQuant4x4MaxAnchor(iDctC[16*i:], ff, mf)
		}
		WelsQuantFour4x4_c(iDctS, ff, mf)
		for i := 0; i < 64; i++ {
			if iDctC[i] != iDctS[i] {
				t.Fatalf("run %d idx %d: %d vs %d", run, i, iDctC[i], iDctS[i])
			}
		}
	}
}

func TestEncodeMbAuxTest_WelsQuantFour4x4Max_c(t *testing.T) {
	r := rand.New(rand.NewSource(11))
	for run := 0; run < 100; run++ {
		ff := make([]int16, 8)
		mf := make([]int16, 8)
		iDctC := make([]int16, 64)
		iDctS := make([]int16, 64)
		iMaxC := make([]int16, 16)
		iMaxS := make([]int16, 16)
		for i := 0; i < 8; i++ {
			ff[i] = int16(cRandInt(r) & 32767)
			mf[i] = int16(cRandInt(r) & 32767)
		}
		for i := 0; i < 64; i++ {
			iDctC[i] = int16((cRandInt(r) & 65535) - 32767)
			iDctS[i] = iDctC[i]
		}
		for i := 0; i < 4; i++ {
			iMaxC[i] = welsQuant4x4MaxAnchor(iDctC[16*i:], ff, mf)
		}
		WelsQuantFour4x4Max_c(iDctS, ff, mf, iMaxS)
		for i := 0; i < 64; i++ {
			if iDctC[i] != iDctS[i] {
				t.Fatalf("run %d idx %d: %d vs %d", run, i, iDctC[i], iDctS[i])
			}
		}
		for i := 0; i < 4; i++ {
			if iMaxC[i] != iMaxS[i] {
				t.Fatalf("run %d max %d: %d vs %d", run, i, iMaxC[i], iMaxS[i])
			}
		}
	}
}

func welsHadamardQuant2x2SkipAnchor(rs []int16, ff int16, mf int16) int32 {
	var pDct, s [4]int16
	threshold := int16(((1<<16)-1)/int32(mf) - int32(ff))
	s[0] = rs[0] + rs[32]
	s[1] = rs[0] - rs[32]
	s[2] = rs[16] + rs[48]
	s[3] = rs[16] - rs[48]
	pDct[0] = s[0] + s[2]
	pDct[1] = s[0] - s[2]
	pDct[2] = s[1] + s[3]
	pDct[3] = s[1] - s[3]
	for i := 0; i < 4; i++ {
		if common.WELS_ABS(int32(pDct[i])) > int32(threshold) {
			return 1
		}
	}
	return 0
}

func TestEncodeMbAuxTest_WelsHadamardQuant2x2Skip_c(t *testing.T) {
	r := rand.New(rand.NewSource(12))
	for run := 0; run < 100; run++ {
		iRS := make([]int16, 64)
		for i := 0; i < 64; i++ {
			iRS[i] = int16((cRandInt(r) & 32767) - 16384)
		}
		ff := int16(cRandInt(r) & 32767)
		mf := int16(cRandInt(r)&32767) | 1 // avoid division by zero
		if a, b := WelsHadamardQuant2x2Skip_c(iRS, ff, mf), welsHadamardQuant2x2SkipAnchor(iRS, ff, mf); a != b {
			t.Fatalf("run %d: %d vs %d", run, a, b)
		}
	}
}

func welsHadamardQuant2x2Anchor(rs []int16, ff int16, mf int16, pDct []int16, block []int16) int32 {
	var s [4]int16
	dcNzc := int32(0)
	s[0] = rs[0] + rs[32]
	s[1] = rs[0] - rs[32]
	s[2] = rs[16] + rs[48]
	s[3] = rs[16] - rs[48]
	rs[0] = 0
	rs[16] = 0
	rs[32] = 0
	rs[48] = 0
	pDct[0] = s[0] + s[2]
	pDct[1] = s[0] - s[2]
	pDct[2] = s[1] + s[3]
	pDct[3] = s[1] - s[3]
	for i := 0; i < 4; i++ {
		sign := int32(pDct[i]) >> 31
		q := ((int32(ff) + ((sign ^ int32(pDct[i])) - sign)) * int32(mf)) >> 16
		pDct[i] = int16((sign ^ q) - sign)
	}
	copy(block[:4], pDct[:4])
	for i := 0; i < 4; i++ {
		if block[i] != 0 {
			dcNzc++
		}
	}
	return dcNzc
}

func TestEncodeMbAuxTest_WelsHadamardQuant2x2_c(t *testing.T) {
	r := rand.New(rand.NewSource(13))
	for run := 0; run < 100; run++ {
		iRsC := make([]int16, 64)
		iRsA := make([]int16, 64)
		iBlockA := make([]int16, 16)
		iBlockC := make([]int16, 16)
		iDctA := make([]int16, 4)
		iDctC := make([]int16, 4)
		for i := 0; i < 64; i++ {
			iRsA[i] = int16((cRandInt(r) & 32767) - 16384)
			iRsC[i] = iRsA[i]
		}
		for i := 0; i < 4; i++ {
			iDctA[i] = int16((cRandInt(r) & 32767) - 16384)
			iDctC[i] = iDctA[i]
		}
		ff := int16(cRandInt(r) & 32767)
		mf := int16(cRandInt(r) & 32767)
		iRetA := welsHadamardQuant2x2Anchor(iRsA, ff, mf, iDctA, iBlockA)
		iRetC := WelsHadamardQuant2x2_c(iRsC, ff, mf, iDctC, iBlockC)
		if iRetA != iRetC {
			t.Fatalf("run %d: ret %d vs %d", run, iRetA, iRetC)
		}
		for i := 0; i < 4; i++ {
			if iDctA[i] != iDctC[i] || iBlockA[i] != iBlockC[i] {
				t.Fatalf("run %d idx %d", run, i)
			}
		}
		for i := 0; i < 64; i++ {
			if iRsA[i] != iRsC[i] {
				t.Fatalf("run %d rs idx %d", run, i)
			}
		}
	}
}

func welsHadamardT4DcAnchor(pLumaDc []int16, pDct []int16) {
	var p [16]int32
	var s [4]int32
	for i := 0; i < 16; i += 4 {
		iIdx := ((i & 0x08) << 4) + ((i & 0x04) << 3)
		s[0] = int32(pDct[iIdx]) + int32(pDct[iIdx+80])
		s[3] = int32(pDct[iIdx]) - int32(pDct[iIdx+80])
		s[1] = int32(pDct[iIdx+16]) + int32(pDct[iIdx+64])
		s[2] = int32(pDct[iIdx+16]) - int32(pDct[iIdx+64])
		p[i] = s[0] + s[1]
		p[i+2] = s[0] - s[1]
		p[i+1] = s[3] + s[2]
		p[i+3] = s[3] - s[2]
	}
	for i := 0; i < 4; i++ {
		s[0] = p[i] + p[i+12]
		s[3] = p[i] - p[i+12]
		s[1] = p[i+4] + p[i+8]
		s[2] = p[i+4] - p[i+8]
		pLumaDc[i] = int16(common.WELS_CLIP3((s[0]+s[1]+1)>>1, -32768, 32767))
		pLumaDc[i+8] = int16(common.WELS_CLIP3((s[0]-s[1]+1)>>1, -32768, 32767))
		pLumaDc[i+4] = int16(common.WELS_CLIP3((s[3]+s[2]+1)>>1, -32768, 32767))
		pLumaDc[i+12] = int16(common.WELS_CLIP3((s[3]-s[2]+1)>>1, -32768, 32767))
	}
}

func TestEncodeMbAuxTest_WelsHadamardT4Dc_c(t *testing.T) {
	r := rand.New(rand.NewSource(14))
	for run := 0; run < 100; run++ {
		iDct := make([]int16, 128*16)
		iLumaDcR := make([]int16, 16)
		iLumaDcC := make([]int16, 16)
		for i := range iDct {
			iDct[i] = int16((cRandInt(r) & 32767) - 16384)
		}
		welsHadamardT4DcAnchor(iLumaDcR, iDct)
		WelsHadamardT4Dc_c(iLumaDcC, iDct)
		for i := 0; i < 16; i++ {
			if iLumaDcR[i] != iLumaDcC[i] {
				t.Fatalf("run %d idx %d", run, i)
			}
		}
	}
}

func TestEncodeMbAuxTest_WelsCalculateSingleCtr4x4_c(t *testing.T) {
	// JVT-O079 run table {3, 2, 2, 1, 1, 1, 0, ...} indexed by the number of
	// zeros preceding each non-zero coefficient.
	cases := []struct {
		nz   []int
		want int32
	}{
		{nil, 0},
		{[]int{0}, 3},
		{[]int{0, 1}, 6},
		{[]int{15}, 0},
		{[]int{0, 15}, 3},
		{[]int{2}, 2},
		{[]int{4, 1}, 2 + 2},
	}
	for _, c := range cases {
		var pDct [16]int16
		for _, k := range c.nz {
			pDct[k] = -1
		}
		if v := WelsCalculateSingleCtr4x4_c(pDct[:]); v != c.want {
			t.Fatalf("%v: got %d want %d", c.nz, v, c.want)
		}
	}
}

// ---------------------------------------------------------------------------
// EncUT_MBCopy.cpp
// ---------------------------------------------------------------------------

func testMbCopy(t *testing.T, ref PCopyFunc, fn PCopyFunc, srcOff int) {
	t.Helper()
	r := rand.New(rand.NewSource(15))
	pSrcAlign := make([]uint8, 16*64+1)
	pDst0 := make([]uint8, 16*32+16)
	pDst1 := make([]uint8, 16*32+16)
	for k := 0; k < 1000; k++ {
		clear(pDst0[:16*32+1])
		clear(pDst1[:16*32+1])
		for i := range pSrcAlign {
			pSrcAlign[i] = uint8(r.Intn(256))
		}
		ref(pDst0, 0, 32, pSrcAlign, srcOff, 64)
		fn(pDst1, 0, 32, pSrcAlign, srcOff, 64)
		for i := 0; i < 16*32+1; i++ {
			if pDst0[i] != pDst1[i] {
				t.Fatalf("k %d idx %d", k, i)
			}
		}
	}
}

func TestMBCopyFunTest(t *testing.T) {
	var sFuncPtrList SWelsFuncPtrList
	WelsInitEncodingFuncs(&sFuncPtrList, 0)
	testMbCopy(t, common.WelsCopy8x8_c, sFuncPtrList.pfCopy8x8Aligned, 0)
	testMbCopy(t, common.WelsCopy8x16_c, sFuncPtrList.pfCopy8x16Aligned, 0)
	testMbCopy(t, common.WelsCopy16x16_c, sFuncPtrList.pfCopy16x16Aligned, 0)
	testMbCopy(t, common.WelsCopy16x8_c, sFuncPtrList.pfCopy16x8NotAligned, 1)
	testMbCopy(t, common.WelsCopy16x16_c, sFuncPtrList.pfCopy16x16NotAligned, 1)
}
