package encoder

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestCavlcPackedLevelTable(t *testing.T) {
	for adjustment := range cavlcLevelTable {
		for suffix := 0; suffix < 7; suffix++ {
			for index, packed := range cavlcLevelTable[adjustment][suffix] {
				value := int32(index - cavlcLevelLimit)
				if value == 0 || (adjustment != 0 && (value == 1 || value == -1)) {
					if packed != 0 {
						t.Fatalf("unused entry adjustment%d suffix%d value%d: %#x", adjustment, suffix, value, packed)
					}
					continue
				}
				want, ok := cavlcLevelSymbolSlow(value, int32(suffix), adjustment != 0)
				got := cavlcLevelSymbol{code: packed & 0xffff, length: uint8(packed >> 16), nextSuffix: uint8(packed >> 24)}
				if !ok || got != want {
					t.Fatalf("adjustment%d suffix%d value%d: %+v want %+v, valid=%t", adjustment, suffix, value, got, want, ok)
				}
			}
		}
		if cavlcLevelTable[adjustment][7] != [2 * cavlcLevelLimit]uint32{} {
			t.Fatalf("adjustment%d: padding suffix row must remain unused", adjustment)
		}
	}
}

// The packed writer also passes successful escape results through the same
// word layout. Exhaust all signed coefficient values and every internal suffix
// to check the packing bounds and the invariant behind the suffix index mask.
func TestCavlcPackedLevelDomain(t *testing.T) {
	for adjustment := 0; adjustment <= 1; adjustment++ {
		for suffix := int32(0); suffix <= 6; suffix++ {
			for value := int32(-32768); value <= 32767; value++ {
				if value == 0 || (adjustment != 0 && (value == 1 || value == -1)) {
					continue
				}
				symbol, ok := cavlcLevelSymbolSlow(value, suffix, adjustment != 0)
				if !ok {
					continue
				}
				if symbol.code >= 1<<13 || symbol.length > 28 || symbol.nextSuffix > 6 {
					t.Fatalf("packing bounds adjustment%d suffix%d value%d: %+v", adjustment, suffix, value, symbol)
				}
				packed := packCavlcLevelSymbol(symbol)
				if packed&0xffff != symbol.code || uint8(packed>>16) != symbol.length || uint8(packed>>24) != symbol.nextSuffix {
					t.Fatalf("packing loses bits adjustment%d suffix%d value%d: %+v to %#x", adjustment, suffix, value, symbol, packed)
				}
			}
		}
	}
}

func TestCavlcPackedWriterTransitions(t *testing.T) {
	patterns := [][16]int16{
		{2, -2, 3, -3, 6, -6, 12, -12, 24, -24, 48, -48, 96, -96, 192, -192},
		{63, -64, 64, -65, 62, -63, 65, -66, 3, -4, 6, -7, 12, -13, 24, -25},
		{32767, -32768, 2, -3, 6, -7, 12, -13, 24, -25, 48, -49, 63, -64, 64, -65},
	}
	var functions SWelsFuncPtrList
	for _, end := range []int32{3, 14, 15} {
		property, nc := int32(LUMA_4x4), int8(3)
		if end == 3 {
			property, nc = int32(CHROMA_DC), CHROMA_DC_NC_OFFSET
		} else if end == 14 {
			property = int32(LUMA_AC)
		}
		for _, stride := range []int32{1, 2} {
			for _, pattern := range patterns {
				for trailing := int32(0); trailing <= 3; trailing++ {
					var coefficients [16]int16
					for index := int32(0); index <= end; index += stride {
						coefficients[index] = pattern[index]
					}
					for i := int32(0); i < trailing; i++ {
						coefficients[end-i] = int16(1 - 2*(i&1))
					}
					for prefix := 0; prefix < 32; prefix++ {
						assertCavlcWriterParity(t, &functions, coefficients[:], end, 1, property, nc, prefix)
						// End 14 with an exact-length input exercises scalar extraction
						// as well as the full-length SIMD input above.
						if end == 14 {
							assertCavlcWriterParity(t, &functions, coefficients[:15], end, 1, property, nc, prefix)
						}
					}
				}
			}
		}
	}
}

// Only the internally produced suffix is masked. Invalid external indices and
// partial output failures must keep their existing panic and writer state.
func TestCavlcPackedWriterFailureParity(t *testing.T) {
	coefficients := []int16{63, -64, 64, -65, 2, -3, 6, -7, 12, -13, 24, -25, 48, -49, 511, -1023, 2}
	type failureCase struct {
		name   string
		length int
		end    int32
		flag   int32
		nc     int8
		output int
	}
	cases := []failureCase{
		{"negative-nc", 16, 15, 1, -1, 512},
		{"large-nc", 16, 15, 1, 18, 512},
		{"short-input", 15, 15, 1, 3, 512},
		{"too-many-coefficients", 17, 16, 1, 3, 512},
		{"negative-end", 16, -1, 1, 3, 512},
		{"ignored-input", 0, 100, 0, 3, 512},
		{"ignored-input-invalid-nc", 0, 100, 0, -1, 512},
	}
	for size := 0; size < 12; size++ {
		cases = append(cases, failureCase{fmt.Sprintf("short-output-%d", size), 16, 15, 1, 3, size})
	}
	var functions SWelsFuncPtrList
	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			for prefix := 0; prefix < 32; prefix++ {
				gotBuffer := bytes.Repeat([]byte{0xa5}, test.output)
				wantBuffer := bytes.Clone(gotBuffer)
				var gotBits, wantBits common.SBitStringAux
				common.InitBits(&gotBits, gotBuffer, 0, int32(test.output))
				common.InitBits(&wantBits, wantBuffer, 0, int32(test.output))
				if prefix != 0 {
					value := uint32((uint64(1) << uint(prefix)) - 1)
					common.BsWriteBits(&gotBits, int32(prefix), value)
					common.BsWriteBits(&wantBits, int32(prefix), value)
				}
				gotResult, gotPanic := cavlcPackedCallResult(func() int32 {
					return WriteBlockResidualCavlc(&functions, coefficients[:test.length], test.end, test.flag, int32(LUMA_4x4), test.nc, &gotBits)
				})
				wantResult, wantPanic := cavlcPackedCallResult(func() int32 {
					return writeBlockResidualCavlcReference(&functions, coefficients[:test.length], test.end, test.flag, int32(LUMA_4x4), test.nc, &wantBits)
				})
				if gotResult != wantResult || gotPanic != wantPanic || gotBits.PCurBuf != wantBits.PCurBuf || gotBits.UiCurBits != wantBits.UiCurBits || gotBits.ILeftBits != wantBits.ILeftBits || !bytes.Equal(gotBuffer, wantBuffer) {
					t.Fatalf("prefix%d: return%d/%d panic%q/%q cursor%d/%d bits%x/%x left%d/%d output%x/%x", prefix, gotResult, wantResult, gotPanic, wantPanic, gotBits.PCurBuf, wantBits.PCurBuf, gotBits.UiCurBits, wantBits.UiCurBits, gotBits.ILeftBits, wantBits.ILeftBits, gotBuffer, wantBuffer)
				}
			}
		})
	}
}

func cavlcPackedCallResult(call func() int32) (result int32, panicText string) {
	defer func() {
		if value := recover(); value != nil {
			panicText = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	result = call()
	return result, ""
}
