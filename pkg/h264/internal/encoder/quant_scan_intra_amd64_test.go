//go:build amd64

package encoder

import (
	"fmt"
	"math/rand/v2"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

// Independent unsigned-word model of the selected SSE2 callback, including
// saturating FF addition and signed FF/MF values interpreted as word bits.
func intraACUnsignedReference(dct, levels []int16, ff, mf []int16) (masks [16]uint16) {
	for i, value := range dct[:256] {
		magnitude := int32(value)
		if magnitude < 0 {
			magnitude = -magnitude
		}
		rounded := min(uint32(magnitude)+uint32(uint16(ff[i%8])), uint32(65535))
		quantized := uint16((rounded * uint32(uint16(mf[i%8]))) >> 16)
		if value < 0 {
			quantized = -quantized
		}
		dct[i] = int16(quantized)
	}
	for block := range 16 {
		WelsScan4x4Ac_c(levels[block*16:], dct[block*16:])
		for i, value := range levels[block*16 : block*16+16] {
			if value != 0 {
				masks[block] |= 1 << i
			}
		}
	}
	return masks
}

func TestIntraACRawAndCheckedParity(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 required")
	}
	rng := rand.New(rand.NewPCG(264, 164))
	var funcs SWelsFuncPtrList
	funcs.pfQuantizationFour4x4 = WelsQuantFour4x4_sse2
	for trial := range 4096 {
		var input [260]int16
		var output [262]int16
		var ff, mf [8]int16
		for i := range input {
			input[i] = int16(rng.Uint32())
		}
		for i := range output {
			output[i] = -12345
		}
		if trial < 52 {
			copy(ff[:], g_iQuantIntraFF[trial][:])
			copy(mf[:], g_kiQuantMF[trial][:])
		} else {
			for i := range ff {
				ff[i], mf[i] = int16(rng.Uint32()), int16(rng.Uint32())
			}
		}
		if trial%8 == 0 {
			clear(input[2:258])
		}
		if trial%8 == 1 {
			for i := range ff {
				ff[i], mf[i] = -1, -1
			}
		}
		wantDct, wantLevels := input, output
		wantMask := intraACUnsignedReference(wantDct[2:258], wantLevels[3:259], ff[:], mf[:])
		for _, raw := range []bool{false, true} {
			gotDct, gotLevels := input, output
			gotMask := [18]uint16{0x1234}
			gotMask[17] = 0x5678
			if raw {
				quantScanIntraLumaAVX2((*[256]int16)(gotDct[2:258]), (*[256]int16)(gotLevels[3:259]), &ff, &mf, (*[16]uint16)(gotMask[1:17]))
			} else {
				if !tryQuantScanIntraLuma(&funcs, gotDct[2:258], gotLevels[3:259], ff[:], mf[:], (*[16]uint16)(gotMask[1:17])) {
					t.Fatal("valid built-in call declined")
				}
			}
			if gotDct != wantDct || gotLevels != wantLevels || *(*[16]uint16)(gotMask[1:17]) != wantMask || gotMask[0] != 0x1234 || gotMask[17] != 0x5678 {
				t.Fatalf("trial%d raw%v: raster, complete AC scan, masks or canaries differ", trial, raw)
			}
		}
	}
}

func TestIntraACRawCoefficientDomain(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 required")
	}
	ff := [8]int16{0, 1, 32767, -32768, -1, 17, -1, 0}
	mf := [8]int16{-1, 32767, -1, -32768, 1, 8192, -1, 0}
	for value := -32768; value <= 32767; value++ {
		var got, want [256]int16
		for _, offset := range []int{0, 1, 7, 8, 15, 16, 63, 64, 127, 128, 239, 255} {
			got[offset] = int16(value)
		}
		want = got
		var gotLevels, wantLevels [256]int16
		var gotMask [16]uint16
		wantMask := intraACUnsignedReference(want[:], wantLevels[:], ff[:], mf[:])
		quantScanIntraLumaAVX2(&got, &gotLevels, &ff, &mf, &gotMask)
		if got != want || gotLevels != wantLevels || gotMask != wantMask {
			t.Fatalf("coefficient%d: output differs", value)
		}
	}
}

func TestIntraACPreflightNoMutation(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 required")
	}
	for _, scenario := range []string{"shortDct", "shortLevels", "shortFF", "shortMF", "nilMasks", "nilFuncs", "nilQuant", "scalarQuant", "customQuant", "sameOutput", "futureGroupOutput", "ffDct", "mfDct", "ffLevels", "mfLevels", "masksDct", "masksLevels", "ffMasks", "mfMasks"} {
		t.Run(scenario, func(t *testing.T) {
			var data [1024]int16
			for i := range data {
				data[i] = int16(i*71 - 30000)
			}
			var funcs SWelsFuncPtrList
			funcs.pfQuantizationFour4x4 = WelsQuantFour4x4_sse2
			funcPtr := &funcs
			dct, levels, ff, mf := data[:256], data[256:512], data[512:520], data[520:528]
			masks := (*[16]uint16)(unsafe.Pointer(&data[528]))
			switch scenario {
			case "shortDct":
				dct = dct[:255]
			case "shortLevels":
				levels = levels[:255]
			case "shortFF":
				ff = ff[:7]
			case "shortMF":
				mf = mf[:7]
			case "nilMasks":
				masks = nil
			case "nilFuncs":
				funcPtr = nil
			case "nilQuant":
				funcs.pfQuantizationFour4x4 = nil
			case "scalarQuant":
				funcs.pfQuantizationFour4x4 = WelsQuantFour4x4_c
			case "customQuant":
				funcs.pfQuantizationFour4x4 = func(d, f, m []int16) { WelsQuantFour4x4_sse2(d, f, m) }
			case "sameOutput":
				levels = dct
			case "futureGroupOutput":
				levels = data[192:448]
			case "ffDct":
				ff = data[240:248]
			case "mfDct":
				mf = data[240:248]
			case "ffLevels":
				ff = data[496:504]
			case "mfLevels":
				mf = data[496:504]
			case "masksDct":
				masks = (*[16]uint16)(unsafe.Pointer(&data[240]))
			case "masksLevels":
				masks = (*[16]uint16)(unsafe.Pointer(&data[496]))
			case "ffMasks":
				ff = data[528:536]
			case "mfMasks":
				mf = data[528:536]
			}
			before := data
			if tryQuantScanIntraLuma(funcPtr, dct, levels, ff, mf, masks) || data != before {
				t.Fatal("rejected input performed work")
			}
		})
	}
}

func TestIntraACReadOnlyTableAlias(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 required")
	}
	for _, shift := range []int{0, 1, 7} {
		t.Run(fmt.Sprint(shift), func(t *testing.T) {
			var tables [16]int16
			for i := range tables {
				tables[i] = int16(i*4093 - 32768)
			}
			var got, want, gotLevels, wantLevels [256]int16
			for i := range got {
				got[i] = int16(i*293 - 32768)
			}
			want = got
			before := tables
			wantMasks := intraACUnsignedReference(want[:], wantLevels[:], tables[:8], tables[shift:shift+8])
			var masks [16]uint16
			funcs := SWelsFuncPtrList{pfQuantizationFour4x4: WelsQuantFour4x4_sse2}
			if !tryQuantScanIntraLuma(&funcs, got[:], gotLevels[:], tables[:8], tables[shift:shift+8], &masks) || got != want || gotLevels != wantLevels || masks != wantMasks || tables != before {
				t.Fatal("read-only table alias differs")
			}
		})
	}
}
