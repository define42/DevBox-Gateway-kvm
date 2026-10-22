//go:build amd64

package encoder

import (
	"math/rand/v2"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestQuantMaxAVX2MatchesSSE2(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewPCG(264, 464))
	edges := [...]int16{-32768, -32767, -1, 0, 1, 16383, 16384, 32766, 32767}
	for fixture := range 1024 {
		var gotDCT [68]int16
		var gotMax [8]int16
		var ff, mf [8]int16
		for i := range gotDCT {
			gotDCT[i] = -12345
		}
		for i := range gotMax {
			gotMax[i] = -23456
		}
		for i := range 64 {
			// Across fixtures, every signed int16 value reaches the kernel.
			gotDCT[i+1] = int16(fixture*64 + i)
		}
		for i := range ff {
			switch fixture % 4 {
			case 0:
				ff[i], mf[i] = g_kiQuantInterFF[fixture%52][i], g_kiQuantMF[fixture%52][i]
			case 1:
				// Preserve the historical SSE2 bit patterns even outside the
				// nonnegative FF/MF domain shared with the scalar quantizer.
				ff[i], mf[i] = int16(rng.Uint32()), int16(rng.Uint32())
			case 2:
				ff[i], mf[i] = edges[(fixture+i)%len(edges)], edges[(fixture/4+i)%len(edges)]
			case 3:
				ff[i], mf[i] = 20000, -1
			}
		}
		wantDCT, wantMax := gotDCT, gotMax
		beforeFF, beforeMF := ff, mf
		quantFour4x4MaxSSE2(wantDCT[1:65], ff[:], mf[:], wantMax[1:5])
		quantFour4x4MaxAVX2((*[64]int16)(gotDCT[1:65]), &ff, &mf, (*[4]int16)(gotMax[1:5]))
		if gotDCT != wantDCT || gotMax != wantMax || ff != beforeFF || mf != beforeMF {
			t.Fatalf("fixture%d: coefficients, maxima, tables or canaries differ", fixture)
		}
	}
}

func TestQuantMaxAVX2ZeroAndSignedMaxima(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, rounding := range []int16{20000, 32767, -1} {
		for _, multiplier := range []int16{32767, -1} {
			var ff, mf [8]int16
			for i := range ff {
				ff[i], mf[i] = rounding, multiplier
			}
			// Zero coefficients can round to nonzero values. Large unsigned
			// magnitudes can also be negative as signed words, making the
			// block maximum zero even though its output is nonzero.
			var got [64]int16
			for i := 16; i < 32; i++ {
				got[i] = 32767
				got[i+16] = -32768
			}
			for i := 48; i < 64; i++ {
				got[i] = int16(1 - 2*(i&1))
			}
			want := got
			var gotMax, wantMax [4]int16
			quantFour4x4MaxSSE2(want[:], ff[:], mf[:], wantMax[:])
			quantFour4x4MaxAVX2(&got, &ff, &mf, &gotMax)
			if got != want || gotMax != wantMax {
				t.Fatalf("FF%d MF%d: zero sign or signed maximum differs", rounding, multiplier)
			}
		}
	}
}

func TestQuantMaxCheckedQPAndCoefficientDomain(t *testing.T) {
	for qp := range 52 {
		ff, mf := g_kiQuantInterFF[qp], g_kiQuantMF[qp]
		for fixture := range 1024 {
			var input [64]int16
			for i := range input {
				input[i] = int16(fixture*64 + i)
			}
			got, want := input, input
			var gotMax, wantMax [4]int16
			WelsQuantFour4x4Max_c(want[:], ff[:], mf[:], wantMax[:])
			WelsQuantFour4x4Max_sse2(got[:], ff[:], mf[:], gotMax[:])
			if got != want || gotMax != wantMax {
				t.Fatalf("QP%d fixture%d: coefficients or maxima differ", qp, fixture)
			}
		}
	}
}

func TestQuantMaxCheckedAliases(t *testing.T) {
	// Coefficients occupy words 32..95. Include maxima before, inside, across,
	// and immediately after that span, with FF/MF also aliasing either output.
	for _, maxOffset := range []int{0, 28, 29, 31, 32, 33, 47, 48, 63, 64, 79, 80, 93, 95, 96, 97, 120} {
		for _, ffOffset := range []int{0, 32, 33, 95, 100} {
			for _, mfOffset := range []int{8, 40, 89, 104} {
				var got [132]int16
				for i := range got {
					got[i] = int16(i*1701 + 13)
				}
				want := got
				quantFour4x4MaxSSE2(want[32:96], want[ffOffset:ffOffset+8], want[mfOffset:mfOffset+8], want[maxOffset:maxOffset+4])
				WelsQuantFour4x4Max_sse2(got[32:96], got[ffOffset:ffOffset+8], got[mfOffset:mfOffset+8], got[maxOffset:maxOffset+4])
				if got != want {
					t.Fatalf("max%d FF%d MF%d: original SSE2 alias semantics changed", maxOffset, ffOffset, mfOffset)
				}
			}
		}
	}
}

func TestQuantMaxCheckedBounds(t *testing.T) {
	for argument, limit := range []int{64, 8, 8, 4} {
		for length := 0; length < limit; length++ {
			var storage [84]int16
			for i := range storage {
				storage[i] = int16(i*1701 + 13)
			}
			before := storage
			args := [4][]int16{storage[:64], storage[64:72], storage[72:80], storage[80:84]}
			args[argument] = args[argument][:length]
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				WelsQuantFour4x4Max_sse2(args[0], args[1], args[2], args[3])
			}()
			if !panicked || storage != before {
				t.Fatalf("argument%d length%d: expected bounds panic before any writes", argument, length)
			}
		}
	}
}
