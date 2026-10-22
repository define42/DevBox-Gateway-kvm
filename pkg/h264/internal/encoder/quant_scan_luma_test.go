package encoder

import (
	"fmt"
	"math/rand"
	"testing"
	"unsafe"
)

// Independently preserve the previous completed-luma call sequence, including
// each group's snapshot of potentially aliased FF/MF coefficients.
func quantScanLumaFourCalls(dct, levels, ff, mf, maxima []int16, masks []uint16) {
	for group := 0; group < 4; group++ {
		quantScanFour4x4(dct[group*64:], levels[group*64:], ff, mf, maxima[group*4:], masks[group*4:], false)
	}
}

func TestQuantScanLumaParity(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewSource(0x26416))
	for qp := 0; qp <= 51; qp++ {
		for fixture := 0; fixture < 32; fixture++ {
			var gotDCT, wantDCT, gotLevels, wantLevels [260]int16
			var gotMax, wantMax [20]int16
			var gotMask, wantMask [20]uint16
			for i := range gotDCT {
				gotDCT[i] = -12345
				gotLevels[i] = 23456
			}
			for i := 0; i < 256; i++ {
				switch fixture % 5 {
				case 0:
					gotDCT[i+2] = 0
				case 1:
					gotDCT[i+2] = 0
					if i%17 == 0 {
						gotDCT[i+2] = int16(rng.Intn(129) - 64)
					}
				case 2:
					gotDCT[i+2] = int16(rng.Uint32())
				case 3:
					gotDCT[i+2] = [...]int16{-32768, -32767, -1, 0, 1, 32766, 32767}[i%7]
				default:
					gotDCT[i+2] = int16(rng.Intn(8193) - 4096)
				}
			}
			for i := range gotMax {
				gotMax[i], gotMask[i] = -23456, 0xbeef
			}
			wantDCT, wantLevels, wantMax, wantMask = gotDCT, gotLevels, gotMax, gotMask
			ff, mf := g_kiQuantInterFF[qp], g_kiQuantMF[qp]
			if fixture >= 16 {
				for i := range ff {
					ff[i], mf[i] = int16(rng.Intn(32768)), int16(rng.Intn(32768))
				}
			}
			for group := 0; group < 4; group++ {
				offset := 2 + group*64
				WelsQuantFour4x4Max_c(wantDCT[offset:], ff[:], mf[:], wantMax[2+group*4:])
				for block := 0; block < 4; block++ {
					start := offset + block*16
					index := 2 + group*4 + block
					if wantMax[index] == 0 {
						clear(wantLevels[start : start+16])
					} else {
						WelsScan4x4DcAc_c(wantLevels[start:], wantDCT[start:])
					}
					wantMask[index] = coefficientNonzeroMask16Scalar(wantLevels[start:])
				}
			}
			quantScanLuma(gotDCT[2:258], gotLevels[2:258], ff[:], mf[:], gotMax[2:18], gotMask[2:18])
			if gotDCT != wantDCT || gotLevels != wantLevels || gotMax != wantMax || gotMask != wantMask {
				t.Fatalf("QP=%d fixture=%d: raster, levels, maxima, masks or canaries differ", qp, fixture)
			}
		}
	}
}

func TestQuantScanLumaAliasedGroups(t *testing.T) {
	t.Parallel()
	// dct, levels, FF, MF, maxima, masks are word offsets in one backing array.
	base := [6]int{0, 320, 640, 648, 608, 624}
	offsets := [][6]int{base}
	for _, pair := range [][2]int{{0, 1}, {0, 4}, {0, 5}, {1, 4}, {1, 5}, {4, 5}} {
		for _, shift := range []int{0, 4, 64, 128, 192} {
			candidate := base
			candidate[pair[1]] = candidate[pair[0]] + shift
			offsets = append(offsets, candidate)
		}
	}
	// Tables may be overwritten by earlier groups; every output/table pairing
	// and all four raster/level groups must retain the old reload ordering.
	for _, table := range []int{2, 3} {
		for _, output := range []int{0, 1, 4, 5} {
			shifts := []int{0, 8}
			if output < 2 {
				shifts = []int{0, 64, 128, 192}
			}
			for _, shift := range shifts {
				candidate := base
				candidate[table] = candidate[output] + shift
				offsets = append(offsets, candidate)
			}
		}
	}
	sharedTables := base
	sharedTables[3] = sharedTables[2] + 4
	offsets = append(offsets, sharedTables)
	for _, positions := range offsets {
		t.Run(fmt.Sprint(positions), func(t *testing.T) {
			var got [1024]int16
			for i := range got {
				got[i] = int16(i*1701 + 13)
			}
			copy(got[positions[2]:], g_kiQuantInterFF[20][:])
			copy(got[positions[3]:], g_kiQuantMF[20][:])
			want := got
			run := func(buffer []int16, fn func([]int16, []int16, []int16, []int16, []int16, []uint16)) {
				masks := unsafe.Slice((*uint16)(unsafe.Pointer(&buffer[positions[5]])), 16)
				fn(buffer[positions[0]:], buffer[positions[1]:], buffer[positions[2]:], buffer[positions[3]:], buffer[positions[4]:], masks)
			}
			run(want[:], quantScanLumaFourCalls)
			run(got[:], quantScanLuma)
			if got != want {
				t.Fatal("batch changed sequential group alias semantics")
			}
		})
	}
}

func TestQuantScanLumaBounds(t *testing.T) {
	t.Parallel()
	for field := 0; field < 6; field++ {
		t.Run(fmt.Sprint(field), func(t *testing.T) {
			t.Parallel()
			inputs := [][]int16{make([]int16, 256), make([]int16, 256), make([]int16, 8), make([]int16, 8), make([]int16, 16)}
			masks := make([]uint16, 16)
			if field == 5 {
				masks = masks[:15]
			} else {
				inputs[field] = inputs[field][:len(inputs[field])-1]
			}
			defer func() {
				if recover() == nil {
					t.Fatal("slice capacity authorized an out-of-length access")
				}
			}()
			quantScanLuma(inputs[0], inputs[1], inputs[2], inputs[3], inputs[4], masks)
		})
	}
}

func BenchmarkQuantScanLuma(b *testing.B) {
	for _, density := range []string{"Zero", "Sparse", "Dense"} {
		b.Run(density, func(b *testing.B) {
			var input, dct, levels [256]int16
			for i := range input {
				if density == "Dense" || (density == "Sparse" && i%17 == 0) {
					input[i] = int16(i*71 - 9000)
				}
			}
			var maxima [16]int16
			var masks [16]uint16
			ff, mf := g_kiQuantInterFF[20][:], g_kiQuantMF[20][:]
			for _, candidate := range []struct {
				name string
				fn   func([]int16, []int16, []int16, []int16, []int16, []uint16)
			}{{"FourCalls", quantScanLumaFourCalls}, {"Batched", quantScanLuma}} {
				b.Run(candidate.name, func(b *testing.B) {
					b.ReportAllocs()
					for b.Loop() {
						copy(dct[:], input[:])
						candidate.fn(dct[:], levels[:], ff, mf, maxima[:], masks[:])
					}
				})
			}
		})
	}
}
