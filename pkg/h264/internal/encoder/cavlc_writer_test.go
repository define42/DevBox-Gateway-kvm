package encoder

import (
	"bytes"
	"fmt"
	"math/rand/v2"
	"reflect"
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
				// Include high stale bits: the original writer preserves them
				// until it emits a complete word.
				bits := rng.Uint32()
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
	// Main deliberately uses its direct extraction kernels, even when a
	// custom callback is present. Batching must not change that dispatch.
	functions.pfCavlcParamCal = func([]int16, []uint8, []int16, *int32, int32) int32 {
		panic("unexpected custom extraction callback")
	}
	types := []Mb_Type{common.MB_TYPE_INTRA16x16, common.MB_TYPE_INTRA4x4, common.MB_TYPE_16x16}
	for test := 0; test < 20000; test++ {
		var dct SDCTCoeff
		var cache SMbCache
		cache.pDct = &dct
		mb := SMB{uiMbType: types[rng.IntN(len(types))], uiCbp: uint8(rng.IntN(48))}
		for i := range cache.iNonZeroCoeffCount {
			cache.iNonZeroCoeffCount[i] = int8(rng.IntN(18) - 1)
		}
		fill := func(values []int16) {
			for i := range values {
				if rng.IntN(3) != 0 {
					continue
				}
				switch test % 6 {
				case 0:
					// Entirely zero macroblocks, including explicit DC blocks.
				case 1:
					values[i] = int16(rng.IntN(3) - 1)
				case 2:
					values[i] = int16(rng.IntN(32) - 16)
				case 3:
					values[i] = int16(rng.IntN(512) - 256)
				case 4:
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
		if test%7 == 0 {
			gotBits.UiCurBits = rng.Uint32()
			wantBits.UiCurBits = gotBits.UiCurBits
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

func TestCavlcMbOverflowState(t *testing.T) {
	t.Parallel()
	for _, block := range []int{0, 1, 8, 15, 16, 17, 18, 19, 26} {
		for prefix := 0; prefix < 32; prefix++ {
			var dct SDCTCoeff
			cache := SMbCache{pDct: &dct}
			for i := range cache.iNonZeroCoeffCount {
				cache.iNonZeroCoeffCount[i] = 16
			}
			blocks := [][]int16{dct.iLumaI16x16Dc[:]}
			for i := 0; i < 16; i++ {
				blocks = append(blocks, dct.iLumaBlock[i*16:i*16+15])
			}
			blocks = append(blocks, dct.iChromaDc[0][:], dct.iChromaDc[1][:])
			for i := 0; i < 8; i++ {
				blocks = append(blocks, dct.iChromaBlock[i*16:i*16+15])
			}
			for _, coefficients := range blocks {
				for i := range coefficients {
					coefficients[i] = 2
				}
			}
			blocks[block][0] = -32768
			mb := SMB{uiMbType: common.MB_TYPE_INTRA16x16, uiCbp: 0x2f}
			got := bytes.Repeat([]byte{0xa5}, 4096)
			want := slices.Clone(got)
			gotBits := common.SBitStringAux{PBuf: got, PCurBuf: 4, ILeftBits: int32(32 - prefix), UiCurBits: 0x12345}
			wantBits := gotBits
			wantBits.PBuf = want
			gotResult := WelsWriteMbResidual(nil, &cache, &mb, &gotBits)
			wantResult := welsWriteMbResidualReference(nil, &cache, &mb, &wantBits)
			if gotResult != ENC_RETURN_VLCOVERFLOWFOUND || gotResult != wantResult || !reflect.DeepEqual(gotBits, wantBits) {
				t.Fatalf("block%d prefix%d: return %d/%d cursor%d/%d bits%x/%x left%d/%d bytesEqual=%t",
					block, prefix, gotResult, wantResult, gotBits.PCurBuf, wantBits.PCurBuf,
					gotBits.UiCurBits, wantBits.UiCurBits, gotBits.ILeftBits, wantBits.ILeftBits, bytes.Equal(got, want))
			}
		}
	}
}

func TestCavlcMbShortBufferState(t *testing.T) {
	t.Parallel()
	var dct SDCTCoeff
	cache := SMbCache{pDct: &dct}
	for i := range cache.iNonZeroCoeffCount {
		cache.iNonZeroCoeffCount[i] = 16
	}
	for i := range dct.iLumaBlock {
		dct.iLumaBlock[i] = int16(i%11 + 2)
	}
	mb := SMB{uiMbType: common.MB_TYPE_16x16, uiCbp: 0x2f}
	observe := func(writer func(*SWelsFuncPtrList, *SMbCache, *SMB, *common.SBitStringAux) int32, bs *common.SBitStringAux) (failure string) {
		defer func() {
			if value := recover(); value != nil {
				failure = fmt.Sprintf("%T: %v", value, value)
			}
		}()
		writer(nil, &cache, &mb, bs)
		return
	}
	for size := 0; size < 128; size++ {
		for _, left := range []int32{-3, 0, 1, 17, 32, 64} {
			got := bytes.Repeat([]byte{0xa5}, size)
			want := slices.Clone(got)
			gotBits := common.SBitStringAux{PBuf: got, PCurBuf: 3, ILeftBits: left, UiCurBits: 0xa59d2bc1}
			wantBits := gotBits
			wantBits.PBuf = want
			gotFailure := observe(WelsWriteMbResidual, &gotBits)
			wantFailure := observe(welsWriteMbResidualReference, &wantBits)
			if gotFailure != wantFailure || !reflect.DeepEqual(gotBits, wantBits) {
				t.Fatalf("size%d left%d: failure %q/%q cursor%d/%d bits%x/%x left%d/%d bytesEqual=%t",
					size, left, gotFailure, wantFailure, gotBits.PCurBuf, wantBits.PCurBuf,
					gotBits.UiCurBits, wantBits.UiCurBits, gotBits.ILeftBits, wantBits.ILeftBits, bytes.Equal(got, want))
			}
		}
	}
}

// Invalid neighbors may fail only after earlier blocks have successfully
// committed. Batching must retain that state and the original bounds panic.
func TestCavlcMbMalformedCacheState(t *testing.T) {
	t.Parallel()
	for _, mbType := range []Mb_Type{common.MB_TYPE_16x16, common.MB_TYPE_INTRA4x4, common.MB_TYPE_INTRA16x16} {
		for _, cbp := range []uint8{1, 0x2f} {
			for index := 0; index < 48; index++ {
				for _, count := range []int8{-128, -2, 17, 32, 64, 127} {
					var dct SDCTCoeff
					cache := SMbCache{pDct: &dct}
					cache.iNonZeroCoeffCount[index] = count
					mb := SMB{uiMbType: mbType, uiCbp: cbp}
					observe := func(writer func(*SWelsFuncPtrList, *SMbCache, *SMB, *common.SBitStringAux) int32, bs *common.SBitStringAux) (result int32, failure string) {
						defer func() {
							if value := recover(); value != nil {
								failure = fmt.Sprintf("%T: %v", value, value)
							}
						}()
						result = writer(nil, &cache, &mb, bs)
						return
					}
					got := bytes.Repeat([]byte{0xa5}, 4096)
					want := slices.Clone(got)
					gotBits := common.SBitStringAux{PBuf: got, PCurBuf: 4, ILeftBits: 32}
					wantBits := gotBits
					wantBits.PBuf = want
					gotResult, gotFailure := observe(WelsWriteMbResidual, &gotBits)
					wantResult, wantFailure := observe(welsWriteMbResidualReference, &wantBits)
					if gotResult != wantResult || gotFailure != wantFailure || !reflect.DeepEqual(gotBits, wantBits) {
						t.Fatalf("type%d cbp%x cache[%d]=%d: result%d/%d failure %q/%q cursor%d/%d bits%x/%x left%d/%d",
							mbType, cbp, index, count, gotResult, wantResult, gotFailure, wantFailure,
							gotBits.PCurBuf, wantBits.PCurBuf, gotBits.UiCurBits, wantBits.UiCurBits, gotBits.ILeftBits, wantBits.ILeftBits)
					}
					if mbType == common.MB_TYPE_16x16 && cbp == 1 && index == 2 && count == -128 {
						if wantFailure == "" || wantBits.ILeftBits != 31 || wantBits.UiCurBits != 1 {
							t.Fatalf("regression fixture must fail after one completed zero block: failure %q bits%x left%d", wantFailure, wantBits.UiCurBits, wantBits.ILeftBits)
						}
					}
				}
			}
		}
	}
}
