//go:build amd64

package encoder

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestIDctFourPackedAVX2Parity(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewPCG(264, 416))
	extremes := [...]int16{-32768, -32767, -16385, -16384, -8193, -8192, -65, -64, -33, -32, -1, 1, 31, 32, 63, 64, 8191, 8192, 16383, 16384, 32766, 32767}
	for run := range 4096 {
		var coeff [64]int16
		if run < len(coeff)*len(extremes) {
			coeff[run/len(extremes)] = extremes[run%len(extremes)]
		} else {
			for i := range coeff {
				if run%2 == 0 {
					coeff[i] = extremes[rng.IntN(len(extremes))]
				} else {
					coeff[i] = int16(rng.Uint32())
				}
			}
		}
		before := coeff
		recStride, predStride := 8+run%29, 8+(run/3)%31
		recOffset, predOffset := run%17, (run/5)%19
		pred := make([]byte, predOffset+7*predStride+8)
		for i := range pred {
			pred[i] = byte(rng.Uint32())
		}
		predBefore := slices.Clone(pred)
		got := bytes.Repeat([]byte{0xa5}, recOffset+7*recStride+8+19)
		want := slices.Clone(got)
		if run%3 == 0 {
			for i := range got {
				got[i] = byte(rng.Uint32())
			}
			copy(want, got)
			WelsIDctFourT4Rec_c(want, recOffset, int32(recStride), want, recOffset, int32(recStride), coeff[:])
			idctFourT4RecPackedAVX2(&got[recOffset], recStride, &got[recOffset], recStride, &coeff)
		} else {
			WelsIDctFourT4Rec_c(want, recOffset, int32(recStride), pred, predOffset, int32(predStride), coeff[:])
			idctFourT4RecPackedAVX2(&got[recOffset], recStride, &pred[predOffset], predStride, &coeff)
		}
		if !bytes.Equal(got, want) || !bytes.Equal(pred, predBefore) || coeff != before {
			t.Fatalf("run %d: packed reconstruction differs from scalar", run)
		}
	}
}

// Loading all four blocks up front must not change the previous paired
// kernel's order when byte output shares storage with later coefficients.
func TestIDctFourPackedCoefficientAliases(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, recOffset := range []int{384, 416, 448, 480, 512, 544, 576, 608, 640, 672} {
		for _, inPlace := range []bool{false, true} {
			got := make([]byte, 1024)
			for i := range got {
				got[i] = byte(i*37 + 11)
			}
			want := slices.Clone(got)
			gotCoeff := unsafe.Slice((*int16)(unsafe.Pointer(&got[512])), 64)
			wantCoeff := unsafe.Slice((*int16)(unsafe.Pointer(&want[512])), 64)
			pred := make([]byte, 256)
			for i := range pred {
				pred[i] = byte(i*13 + 7)
			}
			if inPlace {
				idctFourT4RecAVX2(want[recOffset:], 16, want[recOffset:], 16, wantCoeff)
				WelsIDctFourT4Rec_sse2(got, recOffset, 16, got, recOffset, 16, gotCoeff)
			} else {
				idctFourT4RecAVX2(want[recOffset:], 16, pred, 31, wantCoeff)
				WelsIDctFourT4Rec_sse2(got, recOffset, 16, pred, 0, 31, gotCoeff)
			}
			if !bytes.Equal(got, want) {
				t.Fatalf("output offset %d, in place %t: coefficient alias changed", recOffset, inPlace)
			}
		}
	}
}
