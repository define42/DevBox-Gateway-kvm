package encoder

import (
	"math/rand/v2"
	"testing"
)

func welsHadamardQuant2x2Reference(pRs []int16, iFF int16, iMF int16, pDct []int16, pBlock []int16) int32 {
	var s [4]int16
	iDcNzc := int32(0)
	s[0] = pRs[0] + pRs[32]
	s[1] = pRs[0] - pRs[32]
	s[2] = pRs[16] + pRs[48]
	s[3] = pRs[16] - pRs[48]
	pRs[0], pRs[16], pRs[32], pRs[48] = 0, 0, 0, 0
	pDct[0] = s[0] + s[2]
	pDct[1] = s[0] - s[2]
	pDct[2] = s[1] + s[3]
	pDct[3] = s[1] - s[3]
	for i := range 4 {
		pDct[i] = wels_NEW_QUANT(pDct[i], iFF, iMF)
	}
	copy(pBlock[:4], pDct[:4])
	for i := 0; i < 4; i++ {
		if pBlock[i] != 0 {
			iDcNzc++
		}
	}
	return iDcNzc
}

func TestWelsHadamardQuant2x2Parity(t *testing.T) {
	rng := rand.New(rand.NewPCG(2, 2))
	for test := 0; test < 200000; test++ {
		var rsA, rsB [64]int16
		for _, i := range []int{0, 16, 32, 48} {
			rsA[i] = int16(rng.Uint32())
			if test%2 == 0 {
				rsA[i] = int16(rng.IntN(4096) - 2048)
			}
		}
		rsB = rsA
		ff, mf := int16(rng.IntN(1<<14)), int16(rng.IntN(1<<14))
		var dctA, dctB, blockA, blockB [4]int16
		a := WelsHadamardQuant2x2_c(rsA[:], ff, mf, dctA[:], blockA[:])
		b := welsHadamardQuant2x2Reference(rsB[:], ff, mf, dctB[:], blockB[:])
		if a != b || rsA != rsB || dctA != dctB || blockA != blockB {
			t.Fatalf("test %d: got %d %v %v, want %d %v %v", test, a, dctA, blockA, b, dctB, blockB)
		}
	}
}

func TestAnalysisVaaInfoIntraParity(t *testing.T) {
	rng := rand.New(rand.NewPCG(16, 16))
	for test := 0; test < 20000; test++ {
		stride := 16 + rng.IntN(64)
		off := rng.IntN(32)
		buf := make([]byte, off+15*stride+16+rng.IntN(3))
		for i := range buf {
			switch test % 3 {
			case 0:
				buf[i] = byte(rng.Uint32())
			case 1:
				buf[i] = byte(255 - rng.IntN(3))
			default:
				buf[i] = byte(rng.IntN(16))
			}
		}
		got := AnalysisVaaInfoIntra_c(buf, off, int32(stride))
		want := analysisVaaInfoIntraScalar(buf, off, int32(stride))
		if got != want {
			t.Fatalf("test %d stride %d off %d: %d, want %d", test, stride, off, got, want)
		}
	}
}
