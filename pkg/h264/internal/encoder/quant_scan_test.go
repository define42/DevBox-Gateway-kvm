package encoder

import (
	"fmt"
	"math/rand"
	"testing"
	"unsafe"
)

func TestQuantScanParity(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(0x2645ca))
	for qp := 0; qp < 52; qp++ {
		for fixture := 0; fixture < 64; fixture++ {
			var input [64]int16
			for i := range input {
				switch fixture % 5 {
				case 0: // zero blocks must clear stale scanned levels and masks
				case 1:
					if i%11 == 0 {
						input[i] = int16(rng.Intn(129) - 64)
					}
				case 2:
					input[i] = int16(rng.Uint32())
				case 3:
					input[i] = [...]int16{-32768, -32767, -1, 0, 1, 32766, 32767}[i%7]
				default:
					input[i] = int16(rng.Intn(8193) - 4096)
				}
			}
			for _, ac := range []bool{false, true} {
				ff, mf := g_kiQuantInterFF[qp], g_kiQuantMF[qp]
				if fixture >= 32 {
					// Include the entire nonnegative FF/MF domain, not just QP tables.
					for i := range ff {
						ff[i], mf[i] = int16(rng.Intn(32768)), int16(rng.Intn(32768))
					}
				}
				gotDCT, wantDCT := input, input
				var gotLevels, wantLevels [68]int16
				var gotMax, wantMax [8]int16
				var gotMask, wantMask [8]uint16
				for i := range gotLevels {
					gotLevels[i], wantLevels[i] = -12345, -12345
				}
				for i := range gotMax {
					gotMax[i], wantMax[i] = -23456, -23456
					gotMask[i], wantMask[i] = 0xbeef, 0xbeef
				}
				// An independent scalar reference retains the original stage boundaries.
				WelsQuantFour4x4Max_c(wantDCT[:], ff[:], mf[:], wantMax[:])
				for block := 0; block < 4; block++ {
					offset := block * 16
					if wantMax[block] == 0 {
						clear(wantLevels[offset : offset+16])
					} else if ac {
						WelsScan4x4Ac_c(wantLevels[offset:], wantDCT[offset:])
					} else {
						WelsScan4x4DcAc_c(wantLevels[offset:], wantDCT[offset:])
					}
					wantMask[block] = coefficientNonzeroMask16Scalar(wantLevels[offset:])
				}
				quantScanFour4x4(gotDCT[:], gotLevels[:], ff[:], mf[:], gotMax[:], gotMask[:], ac)
				if gotDCT != wantDCT || gotLevels != wantLevels || gotMax != wantMax || gotMask != wantMask {
					t.Fatalf("QP=%d fixture=%d AC=%t: coefficient, level, maximum, mask or guard mismatch", qp, fixture, ac)
				}
			}
		}
	}
}

func TestQuantScanOverlappingOutputs(t *testing.T) {
	t.Parallel()
	// dct, levels, maxima, masks, FF, MF offsets in one backing array.
	for _, offsets := range [][6]int{
		{0, 0, 160, 168, 176, 184}, {0, 8, 160, 168, 176, 184},
		{0, 80, 0, 168, 176, 184}, {0, 80, 80, 168, 176, 184},
		{0, 80, 160, 0, 176, 184}, {0, 80, 160, 80, 176, 184},
		{0, 80, 160, 160, 176, 184}, {0, 80, 160, 168, 0, 8},
		{0, 80, 160, 168, 80, 88},
	} {
		for _, ac := range []bool{false, true} {
			var got [200]int16
			for i := range got {
				got[i] = int16(i*1701 + 13)
			}
			copy(got[offsets[4]:], g_kiQuantInterFF[20][:])
			copy(got[offsets[5]:], g_kiQuantMF[20][:])
			want := got
			run := func(buffer []int16, fn func([]int16, []int16, []int16, []int16, []int16, []uint16, bool)) {
				masks := unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[offsets[3]])), 4)
				fn(buffer[offsets[0]:], buffer[offsets[1]:], buffer[offsets[4]:], buffer[offsets[5]:],
					buffer[offsets[2]:], masks, ac)
			}
			run(want[:], quantScanFour4x4Separate)
			run(got[:], quantScanFour4x4)
			if got != want {
				t.Fatalf("offsets=%v AC=%t: overlapping buffer semantics changed", offsets, ac)
			}
		}
	}
}

func TestQuantScanBounds(t *testing.T) {
	t.Parallel()
	for field := 0; field < 6; field++ {
		t.Run(fmt.Sprint(field), func(t *testing.T) {
			t.Parallel()
			inputs := [][]int16{make([]int16, 64), make([]int16, 64), make([]int16, 8), make([]int16, 8), make([]int16, 4)}
			masks := make([]uint16, 4)
			if field == 5 {
				masks = masks[:3]
			} else {
				inputs[field] = inputs[field][:len(inputs[field])-1]
			}
			defer func() {
				if recover() == nil {
					t.Fatal("short buffer did not panic")
				}
			}()
			quantScanFour4x4(inputs[0], inputs[1], inputs[2], inputs[3], inputs[4], masks, false)
		})
	}
}

func BenchmarkQuantScanFour4x4(b *testing.B) {
	for _, density := range []string{"Zero", "Sparse", "Dense"} {
		for _, ac := range []bool{false, true} {
			for _, fused := range []bool{false, true} {
				b.Run(fmt.Sprintf("%s/AC%t/Fused%t", density, ac, fused), func(b *testing.B) {
					var input, dct, levels [64]int16
					for i := range input {
						if density == "Dense" || (density == "Sparse" && i%11 == 0) {
							input[i] = int16(i*71 - 1800)
						}
					}
					var maxima [4]int16
					var masks [4]uint16
					ff, mf := g_kiQuantInterFF[20][:], g_kiQuantMF[20][:]
					b.ReportAllocs()
					for b.Loop() {
						copy(dct[:], input[:])
						if fused {
							quantScanFour4x4(dct[:], levels[:], ff, mf, maxima[:], masks[:], ac)
						} else {
							quantScanFour4x4Separate(dct[:], levels[:], ff, mf, maxima[:], masks[:], ac)
						}
					}
				})
			}
		}
	}
}
