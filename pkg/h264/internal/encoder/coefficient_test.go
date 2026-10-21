package encoder

import (
	"fmt"
	"math/bits"
	"math/rand"
	"slices"
	"testing"
)

func TestCoefficientMaskAndScoreExhaustive(t *testing.T) {
	var levels [16]int16
	for pattern := 0; pattern < 1<<16; pattern++ {
		for i := range levels {
			levels[i] = 0
			if pattern&(1<<i) != 0 {
				levels[i] = []int16{-32768, -1, 1, 32767}[i&3]
			}
		}
		mask := coefficientNonzeroMask16(levels[:])
		if mask != uint16(pattern) {
			t.Fatalf("mask=%#x want=%#x", mask, pattern)
		}
		if got, want := coefficientScoreMask(mask), WelsCalculateSingleCtr4x4_c(levels[:]); got != want {
			t.Fatalf("pattern=%#x score=%d want=%d", pattern, got, want)
		}
		if got, want := coefficientNonzeroCount(levels[:]), WelsGetNoneZeroCount_c(levels[:]); got != want {
			t.Fatalf("pattern=%#x count=%d want=%d", pattern, got, want)
		}
	}
}

func TestCoefficientScanMask(t *testing.T) {
	rng := rand.New(rand.NewSource(814))
	for _, ac := range []bool{false, true} {
		for offset := 0; offset < 8; offset++ {
			for iteration := 0; iteration < 200; iteration++ {
				source := make([]int16, 16+offset)
				for i := range source {
					if rng.Intn(3) != 0 {
						source[i] = int16(rng.Uint32())
					}
				}
				got, want := make([]int16, 18+offset), make([]int16, 18+offset)
				for i := range got {
					got[i], want[i] = 31415, 31415
				}
				mask := coefficientScanMask(got[offset:], source[offset:], ac)
				wantMask := coefficientScanMaskScalar(want[offset:], source[offset:], ac)
				if !slices.Equal(got, want) || mask != wantMask {
					t.Fatalf("ac=%v offset=%d iteration=%d mask=%#x want=%#x", ac, offset, iteration, mask, wantMask)
				}
				if ac && (got[offset+15] != 0 || mask&0x8000 != 0) {
					t.Fatal("AC scan must exclude DC and clear final coefficient")
				}
			}
		}
	}
}

func TestCoefficientScanOverlappingBuffers(t *testing.T) {
	for _, ac := range []bool{false, true} {
		for delta := -15; delta <= 15; delta++ {
			got := make([]int16, 64)
			for i := range got {
				got[i] = int16(i*i - 4)
			}
			want := slices.Clone(got)
			mask := coefficientScanMask(got[16+delta:], got[16:], ac)
			wantMask := coefficientScanMaskScalar(want[16+delta:], want[16:], ac)
			if !slices.Equal(got, want) || mask != wantMask {
				t.Fatalf("ac=%v delta=%d alias semantics differ", ac, delta)
			}
		}
	}
}

func TestCoefficientShortBuffersPanic(t *testing.T) {
	for _, fn := range []func(){
		func() { coefficientNonzeroMask16(make([]int16, 15)) },
		func() { coefficientScanMask(make([]int16, 15), make([]int16, 16), false) },
		func() { coefficientScanMask(make([]int16, 16), make([]int16, 15), true) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Error("short coefficient buffer did not panic")
				}
			}()
			fn()
		}()
	}
}

var coefficientBenchmarkSink int32

func BenchmarkCoefficientMetadata(b *testing.B) {
	for _, mask := range []uint16{0x0021, 0x4125, 0xffff} {
		var input, output [16]int16
		for i := range input {
			if mask&(1<<i) != 0 {
				input[i] = 1
			}
		}
		b.Run(fmt.Sprintf("Nonzero%d", bits.OnesCount16(mask)), func(b *testing.B) {
			b.Run("Separate", func(b *testing.B) {
				for b.Loop() {
					WelsScan4x4DcAc_c(output[:], input[:])
					coefficientBenchmarkSink = WelsGetNoneZeroCount_c(output[:]) + WelsCalculateSingleCtr4x4_c(output[:])
				}
			})
			b.Run("Fused", func(b *testing.B) {
				for b.Loop() {
					mask := coefficientScanMask(output[:], input[:], false)
					coefficientBenchmarkSink = int32(bits.OnesCount16(mask)) + coefficientScoreMask(mask)
				}
			})
		})
	}
}
