package encoder

import (
	"fmt"
	"math/rand/v2"
	"testing"
)

func TestHadamardQuant2x2CoefficientDomain(t *testing.T) {
	for _, quant := range [][2]int16{{0, 0}, {0, 32767}, {32767, 32767}, {32767, 1}} {
		for value := -32768; value <= 32767; value++ {
			var input [64]int16
			for i := range input {
				input[i] = -12345
			}
			// Every DC input independently traverses the entire int16 domain.
			input[0], input[16] = int16(value), int16(value*17+1)
			input[32], input[48] = int16(value*73+7), int16(value*8191+13)
			assertHadamardQuant2x2Parity(t, input, quant[0], quant[1])
		}
	}
}

func TestHadamardQuant2x2ButterflyEdges(t *testing.T) {
	edges := [...]int16{-32768, -32767, -1, 0, 1, 32766, 32767}
	for _, a := range edges {
		for _, b := range edges {
			for _, c := range edges {
				for _, d := range edges {
					var input [64]int16
					input[0], input[16], input[32], input[48] = a, b, c, d
					for _, quant := range [][2]int16{{0, 32767}, {32767, 32767}, {20000, 24000}} {
						assertHadamardQuant2x2Parity(t, input, quant[0], quant[1])
					}
				}
			}
		}
	}
}

func TestHadamardQuant2x2QP(t *testing.T) {
	rng := rand.New(rand.NewPCG(264, 222))
	for qp := range 52 {
		for _, intra := range []int{0, 1} {
			ff := int16(int32(g_kiQuantInterFF[qp+intra*6][0]) << 1)
			mf := g_kiQuantMF[qp][0] >> 1
			for fixture := range 64 {
				var input [64]int16
				for i := range input {
					switch fixture % 4 {
					case 1:
						if i%16 == 0 {
							input[i] = int16(rng.Uint32())
						}
					case 2:
						input[i] = int16(rng.Uint32())
					case 3:
						input[i] = int16(rng.IntN(257) - 128)
					}
				}
				assertHadamardQuant2x2Parity(t, input, ff, mf)
			}
		}
	}
}

func TestHadamardQuant2x2SignedParameters(t *testing.T) {
	rng := rand.New(rand.NewPCG(264, 223))
	for fixture := range 4096 {
		var input [64]int16
		for i := range input {
			input[i] = int16(rng.Uint32())
		}
		ff, mf := int16(rng.Uint32()), int16(rng.Uint32())
		if fixture%4 == 0 {
			ff, mf = -32768, -32768
		}
		assertHadamardQuant2x2Parity(t, input, ff, mf)
	}
}

func assertHadamardQuant2x2Parity(t *testing.T, input [64]int16, ff, mf int16) {
	t.Helper()
	gotRS, wantRS := input, input
	var gotDCT, wantDCT, gotBlock, wantBlock [8]int16
	for i := range gotDCT {
		gotDCT[i], wantDCT[i] = 12345, 12345
		gotBlock[i], wantBlock[i] = -23456, -23456
	}
	want := WelsHadamardQuant2x2_c(wantRS[:], ff, mf, wantDCT[1:5], wantBlock[1:5])
	got := hadamardQuant2x2(gotRS[:], ff, mf, gotDCT[1:5], gotBlock[1:5])
	if got != want || gotRS != wantRS || gotDCT != wantDCT || gotBlock != wantBlock {
		t.Fatalf("DC[%d %d %d %d] FF%d MF%d: count%d/%d DCT%v/%v block%v/%v residual parity=%t", input[0], input[16], input[32], input[48], ff, mf, got, want, gotDCT, wantDCT, gotBlock, wantBlock, gotRS == wantRS)
	}
}

func TestHadamardQuant2x2Aliases(t *testing.T) {
	// Residual DC words are at storage indices 16,32,48,64. Cover coincident
	// and shifted output aliases, including partial dct/block overlap.
	offsets := []int{0, 15, 16, 17, 30, 31, 32, 33, 47, 48, 49, 63, 64, 65, 80, 81, 96}
	for _, dctOffset := range offsets {
		for _, blockOffset := range offsets {
			for _, quant := range [][2]int16{{0, 32767}, {32767, 32767}, {-32768, 32767}} {
				var got [104]int16
				for i := range got {
					got[i] = int16(i*1701 + 13)
				}
				want := got
				wantCount := WelsHadamardQuant2x2_c(want[16:65], quant[0], quant[1], want[dctOffset:dctOffset+4], want[blockOffset:blockOffset+4])
				gotCount := hadamardQuant2x2(got[16:65], quant[0], quant[1], got[dctOffset:dctOffset+4], got[blockOffset:blockOffset+4])
				if gotCount != wantCount || got != want {
					t.Fatalf("DCT%d block%d FF%d MF%d: count%d/%d or aliased storage differs", dctOffset, blockOffset, quant[0], quant[1], gotCount, wantCount)
				}
			}
		}
	}
}

func TestHadamardQuant2x2ShortSliceParity(t *testing.T) {
	for _, layout := range [][3]int{{16, 80, 88}, {16, 16, 17}, {16, 32, 33}} {
		for argument, limit := range []int{49, 4, 4} {
			for length := 0; length < limit; length++ {
				var got [104]int16
				for i := range got {
					got[i] = int16(i*1701 + 13)
				}
				want := got
				lengths := [3]int{49, 4, 4}
				lengths[argument] = length
				wantCount, wantPanic := hadamardQuantCallResult(func() int32 {
					return WelsHadamardQuant2x2_c(want[layout[0]:layout[0]+lengths[0]], 32767, 32767, want[layout[1]:layout[1]+lengths[1]], want[layout[2]:layout[2]+lengths[2]])
				})
				gotCount, gotPanic := hadamardQuantCallResult(func() int32 {
					return hadamardQuant2x2(got[layout[0]:layout[0]+lengths[0]], 32767, 32767, got[layout[1]:layout[1]+lengths[1]], got[layout[2]:layout[2]+lengths[2]])
				})
				if wantPanic == "" || gotPanic != wantPanic || gotCount != wantCount || got != want {
					t.Fatalf("layout%v argument%d length%d: count%d/%d panic%q/%q or partial writes differ", layout, argument, length, gotCount, wantCount, gotPanic, wantPanic)
				}
			}
		}
	}
}

func hadamardQuantCallResult(call func() int32) (result int32, panicText string) {
	defer func() {
		if value := recover(); value != nil {
			panicText = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	result = call()
	return result, ""
}
