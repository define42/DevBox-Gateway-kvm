//go:build amd64

package encoder

import "testing"

func TestIntraDCRawCoefficientDomain(t *testing.T) {
	for value := -32768; value <= 32767; value++ {
		// Vary the active position across the full-domain sweep; separate
		// impulses below exercise every position at all overflow boundaries.
		position := value & 15
		var source [241]int16
		source[intraDCDcOffsets[position]] = int16(value)
		var gotDc, wantDc [16]int16
		hadamardT4DcSSE2(&gotDc, &source)
		WelsHadamardT4Dc_c(wantDc[:], source[:])
		if gotDc != wantDc {
			t.Fatalf("raw forward value%d pos%d: %v want%v", value, position, gotDc, wantDc)
		}
		var got, want [16]int16
		got[position], want[position] = int16(value), int16(value)
		mf := uint16(value*73 + 193)
		dequantIHadamard4x4SSE2(&got, uint64(mf))
		WelsDequantIHadamard4x4_c(want[:], mf)
		if got != want {
			t.Fatalf("raw inverse value%d pos%d mf%d: %v want%v", value, position, mf, got, want)
		}
	}
	for position := range 16 {
		for _, value := range []int16{-32768, -32767, -16384, -1, 0, 1, 16384, 32767} {
			var source [241]int16
			source[intraDCDcOffsets[position]] = value
			var gotDc, wantDc [16]int16
			hadamardT4DcSSE2(&gotDc, &source)
			WelsHadamardT4Dc_c(wantDc[:], source[:])
			if gotDc != wantDc {
				t.Fatalf("raw forward impulse value%d pos%d: %v want%v", value, position, gotDc, wantDc)
			}
			for _, mf := range []uint16{0, 1, 2, 32767, 32768, 65535} {
				var got, want [16]int16
				got[position], want[position] = value, value
				dequantIHadamard4x4SSE2(&got, uint64(mf))
				WelsDequantIHadamard4x4_c(want[:], mf)
				if got != want {
					t.Fatalf("raw inverse impulse value%d pos%d mf%d: %v want%v", value, position, mf, got, want)
				}
			}
		}
	}
}
