package encoder

// Ports of test/encoder/EncUT_Cavlc.cpp and EncUT_ExpGolomb.cpp (C parts only).

import (
	"math"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// ---------------------------------------------------------------------------
// EncUT_Cavlc.cpp
// ---------------------------------------------------------------------------

func cavlcParamCal_ref(pCoffLevel []int16, pRun []uint8, pLevel []int16, pTotalCoeff *int32, iLastIndex int32) int32 {
	iTotalZeros := int32(0)
	iTotalCoeffs := int32(0)
	for iLastIndex >= 0 && pCoffLevel[iLastIndex] == 0 {
		iLastIndex--
	}
	for iLastIndex >= 0 {
		iCountZero := int32(0)
		pLevel[iTotalCoeffs] = pCoffLevel[iLastIndex]
		iLastIndex--
		for iLastIndex >= 0 && pCoffLevel[iLastIndex] == 0 {
			iCountZero++
			iLastIndex--
		}
		iTotalZeros += iCountZero
		pRun[iTotalCoeffs] = uint8(iCountZero)
		iTotalCoeffs++
	}
	*pTotalCoeff = iTotalCoeffs
	return iTotalZeros
}

func testCavlcParamCalWithEndIdx(t *testing.T, r *rand.Rand, fn PCavlcParamCalFunc, endIdx int32, allZero, allNonZero bool) {
	t.Helper()
	var coeffLevel, level, levelRef [16]int16
	var run, runRef [16]uint8
	var totalCoeffs, totalCoeffsRef int32
	for i := int32(0); i < 16; i++ {
		rv := cRandInt(r)
		if allZero || (i > endIdx && endIdx > 7) {
			coeffLevel[i] = 0
		} else if allNonZero {
			v := rv%0xFFFF - 0x8000
			if v != 0 {
				coeffLevel[i] = int16(v)
			} else {
				coeffLevel[i] = 0x7FFF
			}
		} else {
			coeffLevel[i] = int16((rv >> 16 & 1) * ((rv & 0xFFFF) - 0x8000))
		}
	}
	totalZerosRef := cavlcParamCal_ref(coeffLevel[:], runRef[:], levelRef[:], &totalCoeffsRef, endIdx)
	totalZeros := fn(coeffLevel[:], run[:], level[:], &totalCoeffs, endIdx)
	if totalCoeffs != totalCoeffsRef {
		t.Fatalf("totalCoeffs %d want %d", totalCoeffs, totalCoeffsRef)
	}
	if totalCoeffs > 0 && totalZeros != totalZerosRef {
		t.Fatalf("totalZeros %d want %d", totalZeros, totalZerosRef)
	}
	for i := int32(0); i < totalCoeffsRef; i++ {
		if level[i] != levelRef[i] {
			t.Fatalf("level[%d]", i)
		}
	}
	for i := int32(0); i < totalCoeffsRef-1; i++ {
		if run[i] != runRef[i] {
			t.Fatalf("run[%d]", i)
		}
	}
}

func TestCavlcTest_CavlcParamCal_c(t *testing.T) {
	r := rand.New(rand.NewSource(41))
	endIdxes := []int32{3, 14, 15}
	const numTestRepetitions = 10000
	for _, e := range endIdxes {
		for count := 0; count < numTestRepetitions; count++ {
			testCavlcParamCalWithEndIdx(t, r, CavlcParamCal_c, e, count == 0, count == 1)
		}
	}
}

// TestCavlcTest_WriteBlockResidualCavlc checks a few hand-computed CAVLC
// residual blocks (H.264 9.2) written by WriteBlockResidualCavlc.
func TestCavlcTest_WriteBlockResidualCavlc(t *testing.T) {
	var sFuncList SWelsFuncPtrList
	InitCoeffFunc(&sFuncList, 0, 0)
	write := func(coeffs []int16, iEndIdx int32, iResidualProperty int32, iNC int8) []uint8 {
		buf := make([]uint8, 64)
		var bs common.SBitStringAux
		common.InitBits(&bs, buf, 0, int32(len(buf)))
		if ret := WriteBlockResidualCavlc(&sFuncList, coeffs, iEndIdx, 1, iResidualProperty, iNC, &bs); ret != ENC_RETURN_SUCCESS {
			t.Fatalf("ret %d", ret)
		}
		common.BsFlush(&bs)
		return buf[:bs.PCurBuf]
	}
	// all zero block, nC = 0: coeff_token "1" -> 0x80
	var zero [16]int16
	if got := write(zero[:], 15, int32(LUMA_4x4), 0); len(got) != 1 || got[0] != 0x80 {
		t.Fatalf("zero block: %x", got)
	}
	// Example of the standard text (Richardson): 0,3,-1,0 / 0,-1,1,0 / 1,0,0,0 / 0,0,0,0
	// in zig-zag order: 0 3 0 1 -1 -1 0 1 0 0 0 0 0 0 0 0, nC = 0
	// -> 000010001110010111101101
	coeffs := []int16{0, 3, 0, 1, -1, -1, 0, 1, 0, 0, 0, 0, 0, 0, 0, 0}
	got := write(coeffs, 15, int32(LUMA_4x4), 0)
	want := []uint8{0x08, 0xE5, 0xED}
	if len(got) != len(want) {
		t.Fatalf("example: %x want %x", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("example: %x want %x", got, want)
		}
	}
}

// ---------------------------------------------------------------------------
// EncUT_ExpGolomb.cpp
// ---------------------------------------------------------------------------

var g_kdLog2Factor = 1.0 / math.Log(2.0)

func TestUeExpGolombTest_TestBsSizeUeLt256(t *testing.T) {
	for uiInVal := uint32(0); uiInVal < 256; uiInVal++ {
		uiActVal := BsSizeUE(uiInVal)
		m := int32(math.Log(float64(uiInVal+1)*1.0)*g_kdLog2Factor + 1e-6)
		uiExpVal := uint32((m << 1) + 1)
		if uiActVal != uiExpVal {
			t.Fatalf("%d: %d want %d", uiInVal, uiActVal, uiExpVal)
		}
	}
}

func TestUeExpGolombTest_TestBsSizeUeRangeFrom256To65534(t *testing.T) {
	for uiInVal := uint32(0x100); uiInVal < 0xFFFF; uiInVal++ {
		uiActVal := BsSizeUE(uiInVal)
		m := int32(math.Log(float64(uiInVal+1)*1.0)*g_kdLog2Factor + 1e-6)
		uiExpVal := uint32((m << 1) + 1)
		if uiActVal != uiExpVal {
			t.Fatalf("%d: %d want %d", uiInVal, uiActVal, uiExpVal)
		}
	}
}

func TestUeExpGolombTest_TestBsSizeUeRangeFrom65535ToPlus256(t *testing.T) {
	uiInVal := uint32(0xFFFF)
	uiInValEnd := uiInVal + 256
	for ; uiInVal < uiInValEnd; uiInVal++ {
		uiActVal := BsSizeUE(uiInVal)
		m := common.WELS_LOG2(1 + uiInVal)
		uiExpVal := uint32((m << 1) + 1)
		if uiActVal != uiExpVal {
			t.Fatalf("%d: %d want %d", uiInVal, uiActVal, uiExpVal)
		}
	}
}

func TestBsFlushTest_TestBsFlushUb(t *testing.T) {
	buffer := make([]uint8, 16)
	var bs common.SBitStringAux
	bs.PBuf = buffer
	bs.PStartBuf = 0
	bs.PEndBuf = 16
	bs.PCurBuf = 0
	bs.UiCurBits = 123
	bs.ILeftBits = 32
	common.BsFlush(&bs)
}
