package encoder

import (
	"fmt"
	"math/rand"
	"testing"
)

func TestCavlcExtractMasks(t *testing.T) {
	rng := rand.New(rand.NewSource(0x264e))
	for _, end := range []int32{14, 15} {
		t.Run(fmt.Sprintf("end%d", end), func(t *testing.T) {
			for mask := 0; mask < 1<<16; mask++ {
				// The odd word offset also exercises unaligned vector loads.
				var storage [17]int16
				coeff := storage[1:]
				for i := range coeff {
					if mask&(1<<i) != 0 {
						coeff[i] = int16(rng.Uint32())
						if coeff[i] == 0 {
							coeff[i] = -32768
						}
					}
				}
				assertCavlcExtractParity(t, coeff, end)
			}
		})
	}
}

func TestCavlcExtractLengthsAndEnds(t *testing.T) {
	rng := rand.New(rand.NewSource(0x264ed))
	for end := int32(-1); end <= 15; end++ {
		t.Run(fmt.Sprintf("end%d", end), func(t *testing.T) {
			for length := int(end + 1); length <= 16; length++ {
				var coeff [16]int16
				for repeat := 0; repeat < 64; repeat++ {
					for i := range coeff {
						coeff[i] = int16(rng.Uint32())
						if rng.Intn(4) != 0 {
							coeff[i] = 0
						}
					}
					// A large capacity must never authorize wider input reads.
					assertCavlcExtractParity(t, coeff[:length], end)
				}
			}
		})
	}
}

func TestCavlcExtractCoefficientDomain(t *testing.T) {
	for value := -32768; value <= 32767; value++ {
		var coeff [16]int16
		coeff[0], coeff[7], coeff[15] = int16(value), int16(value), int16(value)
		assertCavlcExtractParity(t, coeff[:], 15)
	}
}

func assertCavlcExtractParity(t *testing.T, coeff []int16, end int32) {
	t.Helper()
	var gotLevel, wantLevel [16]int16
	var gotRun, wantRun [16]uint8
	for i := range gotLevel {
		gotLevel[i], wantLevel[i] = int16(0x4321+i), int16(0x4321+i)
		gotRun[i], wantRun[i] = uint8(0xa0+i), uint8(0xa0+i)
	}
	var gotCount, wantCount int32
	wantZeros := cavlcParamCal_ref(coeff, wantRun[:], wantLevel[:], &wantCount, end)
	gotZeros := cavlcExtract(coeff, &gotRun, &gotLevel, &gotCount, end)
	if gotZeros != wantZeros || gotCount != wantCount || gotRun != wantRun || gotLevel != wantLevel {
		t.Fatalf("end=%d coeff=%v: zeros %d/%d count %d/%d run %v/%v level %v/%v",
			end, coeff, gotZeros, wantZeros, gotCount, wantCount, gotRun, wantRun, gotLevel, wantLevel)
	}
}

// Exercise the same dispatch as the writer while keeping production's tiny
// scalar fallback free of an additional Go call.
func cavlcExtract(coeff []int16, run *[16]uint8, level *[16]int16, total *int32, end int32) int32 {
	if cavlcExtractSupported(coeff, end) {
		return cavlcExtractSSSE3((*[16]int16)(coeff), run, level, total, end)
	}
	return CavlcParamCal_c(coeff, run[:], level[:], total, end)
}
