package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestCavlcFastWriterParity(t *testing.T) {
	rng := rand.New(rand.NewSource(0x264ca))
	var functions SWelsFuncPtrList
	InitCoeffFunc(&functions, 0, 0)
	for test := 0; test < 30000; test++ {
		var coefficients [16]int16
		end, property, nc := int32(15), int32(LUMA_4x4), int8(rng.Intn(17))
		switch test % 3 {
		case 1:
			end, property = 14, int32(LUMA_AC)
		case 2:
			end, property, nc = 3, int32(CHROMA_DC), CHROMA_DC_NC_OFFSET
		}
		for i := range coefficients {
			if rng.Intn(4) == 0 {
				continue
			}
			switch test % 4 {
			case 0:
				coefficients[i] = int16(rng.Intn(5) - 2)
			case 1:
				coefficients[i] = int16(rng.Intn(128) - 64)
			case 2:
				coefficients[i] = int16(rng.Intn(4096) - 2048)
			default:
				coefficients[i] = int16(rng.Uint32())
			}
		}
		flag := int32(1)
		if test%13 == 0 {
			flag = 0
		}
		assertCavlcWriterParity(t, &functions, coefficients[:end+1], end, flag, property, nc, test%32)
	}
	// Exhaust the signed coefficient domain through both a first-level
	// adjustment and a context with three trailing ones (no adjustment).
	for value := -32768; value <= 32767; value++ {
		if value == 0 {
			continue
		}
		for _, trailing := range []int{0, 3} {
			var coefficients [16]int16
			coefficients[0] = int16(value)
			for i := 1; i <= trailing; i++ {
				coefficients[i] = 1
			}
			assertCavlcWriterParity(t, &functions, coefficients[:], 15, 1, int32(LUMA_4x4), 0, value&31)
		}
	}
}

func assertCavlcWriterParity(t *testing.T, functions *SWelsFuncPtrList, coefficients []int16, end, flag, property int32, nc int8, prefix int) {
	t.Helper()
	want := bytes.Repeat([]byte{0xa5}, 512)
	got := slices.Clone(want)
	var gotBits, wantBits common.SBitStringAux
	common.InitBits(&gotBits, got, 0, int32(len(got)))
	common.InitBits(&wantBits, want, 0, int32(len(want)))
	if prefix != 0 {
		value := uint32((uint64(1) << uint(prefix)) - 1)
		common.BsWriteBits(&gotBits, int32(prefix), value)
		common.BsWriteBits(&wantBits, int32(prefix), value)
	}
	gotResult := WriteBlockResidualCavlc(functions, coefficients, end, flag, property, nc, &gotBits)
	wantResult := writeBlockResidualCavlcReference(functions, coefficients, end, flag, property, nc, &wantBits)
	if gotResult != wantResult || gotBits.PCurBuf != wantBits.PCurBuf || gotBits.UiCurBits != wantBits.UiCurBits || gotBits.ILeftBits != wantBits.ILeftBits || !bytes.Equal(got, want) {
		t.Fatalf("end%d nc%d prefix%d coeffs%v: return%d/%d cursor%d/%d bits%x/%x left%d/%d", end, nc, prefix, coefficients, gotResult, wantResult, gotBits.PCurBuf, wantBits.PCurBuf, gotBits.UiCurBits, wantBits.UiCurBits, gotBits.ILeftBits, wantBits.ILeftBits)
	}
}

func BenchmarkCavlcResidual(b *testing.B) {
	var functions SWelsFuncPtrList
	InitCoeffFunc(&functions, 0, 0)
	for _, sparse := range []bool{true, false} {
		var coefficients [16]int16
		for i := range coefficients {
			if !sparse || i%5 == 0 {
				coefficients[i] = int16(i%9 - 4)
			}
		}
		for _, original := range []bool{true, false} {
			b.Run(fmt.Sprintf("sparse=%t/reference=%t", sparse, original), func(b *testing.B) {
				var buffer [512]byte
				var bits common.SBitStringAux
				b.ReportAllocs()
				for b.Loop() {
					common.InitBits(&bits, buffer[:], 0, int32(len(buffer)))
					if original {
						writeBlockResidualCavlcReference(&functions, coefficients[:], 15, 1, int32(LUMA_4x4), 3, &bits)
					} else {
						WriteBlockResidualCavlc(&functions, coefficients[:], 15, 1, int32(LUMA_4x4), 3, &bits)
					}
				}
			})
		}
	}
}
