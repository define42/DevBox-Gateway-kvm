package encoder

import (
	"bytes"
	"math/rand/v2"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// cavlcPut must store the same words and leave the same SBitStringAux state as
// the original 32-bit writer for every pending-bit count and symbol length.
// CAVLC symbols are at most 28 bits; the reference sign-extends 32-bit values.
func TestCavlcPutParity(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(264, 64))
	for left := int32(1); left <= 32; left++ {
		for n := uint(0); n <= 31; n++ {
			for trial := 0; trial < 32; trial++ {
				var gotBuffer, wantBuffer [16]byte
				bits := rng.Uint32()
				if left < 32 {
					bits &= 1<<(32-left) - 1
				} else {
					bits = 0
				}
				value := uint64(rng.Uint32()) & (1<<n - 1)
				want := cavlcWriterReference{sCavlcBs{pBuf: wantBuffer[:], pBufPtr: 4, uiCurBits: bits, iLeftBits: left}}
				want.write(int32(n), int32(uint32(value)))
				gotBits := common.SBitStringAux{PBuf: gotBuffer[:], PCurBuf: 4, UiCurBits: bits, ILeftBits: left}
				pos, acc, cnt := cavlcLoad(&gotBits)
				pos, acc, cnt = cavlcPut(&gotBits.PBuf, pos, acc, cnt, n, value)
				cavlcStore(&gotBits, pos, acc, cnt)
				if gotBits.PCurBuf != want.pBufPtr || gotBits.UiCurBits != want.uiCurBits || gotBits.ILeftBits != want.iLeftBits || gotBuffer != wantBuffer {
					t.Fatalf("left=%d n=%d value=%x: state %d/%x/%d, want %d/%x/%d", left, n, value,
						gotBits.PCurBuf, gotBits.UiCurBits, gotBits.ILeftBits, want.pBufPtr, want.uiCurBits, want.iLeftBits)
				}
			}
		}
	}
}

// The macroblock writer must match the original per-block loop, including the
// state left behind by an overflowing block.
func TestWelsWriteMbResidualParity(t *testing.T) {
	t.Parallel()
	rng := rand.New(rand.NewPCG(0x264, 0xcaf))
	var functions SWelsFuncPtrList
	InitCoeffFunc(&functions, 0, 0)
	types := []Mb_Type{common.MB_TYPE_INTRA16x16, common.MB_TYPE_INTRA4x4, common.MB_TYPE_16x16}
	for test := 0; test < 20000; test++ {
		var dct SDCTCoeff
		var cache SMbCache
		cache.pDct = &dct
		mb := SMB{uiMbType: types[test%3], uiCbp: uint8(rng.IntN(48))}
		for i := range cache.iNonZeroCoeffCount {
			cache.iNonZeroCoeffCount[i] = int8(rng.IntN(18) - 1)
		}
		fill := func(values []int16) {
			for i := range values {
				if rng.IntN(3) != 0 {
					continue
				}
				switch test % 5 {
				case 0:
					values[i] = int16(rng.IntN(3) - 1)
				case 1:
					values[i] = int16(rng.IntN(32) - 16)
				case 2:
					values[i] = int16(rng.IntN(512) - 256)
				case 3:
					values[i] = int16(rng.IntN(8192) - 4096)
				default:
					values[i] = int16(rng.Uint32()) // includes overflowing levels
				}
			}
		}
		fill(dct.iLumaBlock[:])
		fill(dct.iLumaI16x16Dc[:])
		fill(dct.iChromaBlock[:])
		fill(dct.iChromaDc[0][:])
		fill(dct.iChromaDc[1][:])

		want := bytes.Repeat([]byte{0x5a}, 4096)
		got := slices.Clone(want)
		var gotBits, wantBits common.SBitStringAux
		common.InitBits(&gotBits, got, 0, int32(len(got)))
		common.InitBits(&wantBits, want, 0, int32(len(want)))
		if prefix := test % 32; prefix != 0 {
			common.BsWriteBits(&gotBits, int32(prefix), 1)
			common.BsWriteBits(&wantBits, int32(prefix), 1)
		}
		gotResult := WelsWriteMbResidual(&functions, &cache, &mb, &gotBits)
		wantResult := welsWriteMbResidualReference(&functions, &cache, &mb, &wantBits)
		if gotResult != wantResult || gotBits.PCurBuf != wantBits.PCurBuf || gotBits.UiCurBits != wantBits.UiCurBits || gotBits.ILeftBits != wantBits.ILeftBits || !bytes.Equal(got, want) {
			t.Fatalf("test %d type %d cbp %#x: return %d/%d cursor %d/%d bits %x/%x left %d/%d",
				test, mb.uiMbType, mb.uiCbp, gotResult, wantResult, gotBits.PCurBuf, wantBits.PCurBuf,
				gotBits.UiCurBits, wantBits.UiCurBits, gotBits.ILeftBits, wantBits.ILeftBits)
		}
	}
}

func BenchmarkWelsWriteMbResidual(b *testing.B) {
	var functions SWelsFuncPtrList
	InitCoeffFunc(&functions, 0, 0)
	rng := rand.New(rand.NewPCG(1, 2))
	var dct SDCTCoeff
	var cache SMbCache
	cache.pDct = &dct
	for i := range dct.iLumaBlock {
		if rng.IntN(4) == 0 {
			dct.iLumaBlock[i] = int16(rng.IntN(9) - 4)
		}
	}
	for i := range dct.iChromaBlock {
		if rng.IntN(8) == 0 {
			dct.iChromaBlock[i] = int16(rng.IntN(5) - 2)
		}
	}
	dct.iChromaDc[0] = [4]int16{3, -1, 0, 1}
	dct.iChromaDc[1] = [4]int16{-2, 0, 0, 1}
	for i := range cache.iNonZeroCoeffCount {
		cache.iNonZeroCoeffCount[i] = int8(rng.IntN(6))
	}
	mb := SMB{uiMbType: common.MB_TYPE_16x16, uiCbp: 0x2f}
	buffer := make([]byte, 4096)
	for _, reference := range []bool{true, false} {
		name := "new"
		if reference {
			name = "reference"
		}
		b.Run(name, func(b *testing.B) {
			var bits common.SBitStringAux
			for b.Loop() {
				common.InitBits(&bits, buffer, 0, int32(len(buffer)))
				if reference {
					welsWriteMbResidualReference(&functions, &cache, &mb, &bits)
				} else {
					WelsWriteMbResidual(&functions, &cache, &mb, &bits)
				}
			}
		})
	}
}
