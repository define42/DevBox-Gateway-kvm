//go:build amd64

package encoder

import (
	"bytes"
	"math/rand"
	"slices"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestIDctPairedAVX2MatchesPriorKernel(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewSource(0x2641dc7))
	extremes := [...]int16{-32768, -32767, -16385, -16384, -65, -64, -33, -32, -1, 0, 1, 31, 32, 63, 64, 16383, 16384, 32766, 32767}
	for run := 0; run < 2048; run++ {
		var coeff [64]int16
		if run < len(coeff)*len(extremes) {
			coeff[run/len(extremes)] = extremes[run%len(extremes)]
		} else {
			for i := range coeff {
				coeff[i] = int16(rng.Uint32())
			}
		}
		before := coeff
		recStride, predStride := 8+run%23, 8+(run/3)%29
		offset := run % 17
		pred := make([]byte, offset+7*predStride+8)
		rng.Read(pred)
		predBefore := slices.Clone(pred)
		got := bytes.Repeat([]byte{0xa5}, offset+7*recStride+8+19)
		want := slices.Clone(got)
		if run%2 == 0 {
			copy(got, pred)
			copy(want, pred)
			idctFourT4RecSSE2(want[offset:], recStride, want[offset:], recStride, coeff[:])
			idctFourT4RecAVX2(got[offset:], recStride, got[offset:], recStride, coeff[:])
		} else {
			idctFourT4RecSSE2(want[offset:], recStride, pred[offset:], predStride, coeff[:])
			idctFourT4RecAVX2(got[offset:], recStride, pred[offset:], predStride, coeff[:])
		}
		if !bytes.Equal(got, want) || !bytes.Equal(pred, predBefore) || coeff != before {
			t.Fatalf("run %d: paired kernel differs from four independent transforms", run)
		}
	}
}

func BenchmarkIDctPairedKernel(b *testing.B) {
	if !cpu.X86.HasAVX2 {
		b.Skip("AVX2 unavailable")
	}
	for _, candidate := range []struct {
		name string
		fn   func([]byte, int, []byte, int, []int16)
	}{
		{"PriorFourSSE2", idctFourT4RecSSE2},
		{"PairedAVX2", idctFourT4RecAVX2},
	} {
		b.Run(candidate.name, func(b *testing.B) {
			var coefficients [64]int16
			var prediction, reconstructed [8 * 64]byte
			for i := range coefficients {
				coefficients[i] = int16(i*43 - 1300)
			}
			for i := range prediction {
				prediction[i] = byte(i)
			}
			b.ReportAllocs()
			for b.Loop() {
				candidate.fn(reconstructed[:], 64, prediction[:], 64, coefficients[:])
			}
		})
	}
}
