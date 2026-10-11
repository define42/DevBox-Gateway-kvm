// Port of the C-reference parts of test/decoder/DecUT_IdctResAddPred.cpp
// (the SIMD comparisons and the WelsNonZeroCount test of package common are
// not ported here).

package decoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func IdctResAddPred_ref(pPred []uint8, o int, kiStride int32, pRs []int16) {
	var iSrc [16]int16
	s := int(kiStride)
	kiStride2 := s << 1
	kiStride3 := s + kiStride2

	for i := 0; i < 4; i++ {
		kiY := i << 2
		kiT0 := int32(pRs[kiY]) + int32(pRs[kiY+2])
		kiT1 := int32(pRs[kiY]) - int32(pRs[kiY+2])
		kiT2 := (int32(pRs[kiY+1]) >> 1) - int32(pRs[kiY+3])
		kiT3 := int32(pRs[kiY+1]) + (int32(pRs[kiY+3]) >> 1)
		iSrc[kiY] = int16(kiT0 + kiT3)
		iSrc[kiY+1] = int16(kiT1 + kiT2)
		iSrc[kiY+2] = int16(kiT1 - kiT2)
		iSrc[kiY+3] = int16(kiT0 - kiT3)
	}
	for i := 0; i < 4; i++ {
		kT1 := int32(iSrc[i]) + int32(iSrc[i+8])
		kT2 := int32(iSrc[i+4]) + (int32(iSrc[i+12]) >> 1)
		kT3 := (32 + kT1 + kT2) >> 6
		kT4 := (32 + kT1 - kT2) >> 6
		pPred[o+i] = common.WelsClip1(kT3 + int32(pPred[o+i]))
		pPred[o+i+kiStride3] = common.WelsClip1(kT4 + int32(pPred[o+i+kiStride3]))
		kT1 = int32(iSrc[i]) - int32(iSrc[i+8])
		kT2 = (int32(iSrc[i+4]) >> 1) - int32(iSrc[i+12])
		pPred[o+i+s] = common.WelsClip1(((32 + kT1 + kT2) >> 6) + int32(pPred[o+i+s]))
		pPred[o+i+kiStride2] = common.WelsClip1(((32 + kT1 - kT2) >> 6) + int32(pPred[o+i+kiStride2]))
	}
}

// IdctResAddPred8x8_ref uses plain int16 arithmetic: every intermediate of
// the C reference is stored to int16_t and only additions, subtractions and
// arithmetic shifts of stored values occur, so wrapping int16 arithmetic
// gives the same result (an independent check of the int32 port).
func IdctResAddPred8x8_ref(pPred []uint8, o int, kiStride int32, pRs []int16) {
	var p, b [8]int16
	var a [4]int16
	var iTmp, iRes [64]int16
	butterfly := func() {
		a[0] = p[0] + p[4]
		a[1] = p[0] - p[4]
		a[2] = p[6] - (p[2] >> 1)
		a[3] = p[2] + (p[6] >> 1)
		b[0] = a[0] + a[3]
		b[2] = a[1] - a[2]
		b[4] = a[1] + a[2]
		b[6] = a[0] - a[3]
		a[0] = -p[3] + p[5] - p[7] - (p[7] >> 1)
		a[1] = p[1] + p[7] - p[3] - (p[3] >> 1)
		a[2] = -p[1] + p[7] + p[5] + (p[5] >> 1)
		a[3] = p[3] + p[5] + p[1] + (p[1] >> 1)
		b[1] = a[0] + (a[3] >> 2)
		b[3] = a[1] + (a[2] >> 2)
		b[5] = a[2] - (a[1] >> 2)
		b[7] = a[3] - (a[0] >> 2)
	}
	out := func(dst []int16, idx func(k int) int) {
		dst[idx(0)] = b[0] + b[7]
		dst[idx(1)] = b[2] - b[5]
		dst[idx(2)] = b[4] + b[3]
		dst[idx(3)] = b[6] + b[1]
		dst[idx(4)] = b[6] - b[1]
		dst[idx(5)] = b[4] - b[3]
		dst[idx(6)] = b[2] + b[5]
		dst[idx(7)] = b[0] - b[7]
	}
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			p[j] = pRs[j+(i<<3)]
		}
		butterfly()
		out(iTmp[:], func(k int) int { return k + (i << 3) })
	}
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			p[j] = iTmp[i+(j<<3)]
		}
		butterfly()
		out(iRes[:], func(k int) int { return (k << 3) + i })
	}
	s := int(kiStride)
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			k := o + i*s + j
			pPred[k] = common.WelsClip1(((32 + int32(iRes[(i<<3)+j])) >> 6) + int32(pPred[k]))
		}
	}
}

func TestDecoderDecodeMbAux_IdctResAddPred_c(t *testing.T) {
	const kiStride = 32
	const iBits = 12
	const iMask = (1 << iBits) - 1
	const iOffset = 1 << (iBits - 1)
	iRS := make([]int16, 16)
	iRefRS := make([]int16, 16)
	uiPred := make([]uint8, 16*kiStride)
	uiRefPred := make([]uint8, 16*kiStride)
	for iRunTimes := 0; iRunTimes < 1000; iRunTimes++ {
		for i := 0; i < 16; i++ {
			v := int16((rand.Int() & iMask) - iOffset)
			iRefRS[i], iRS[i] = v, v
		}
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				v := uint8(rand.Intn(256))
				uiRefPred[i*kiStride+j], uiPred[i*kiStride+j] = v, v
			}
		}
		IdctResAddPred_c(uiPred, 0, kiStride, iRS)
		IdctResAddPred_ref(uiRefPred, 0, kiStride, iRefRS)
		for i := 0; i < 4; i++ {
			for j := 0; j < 4; j++ {
				if uiRefPred[i*kiStride+j] != uiPred[i*kiStride+j] {
					t.Fatalf("mismatch at (%d,%d)", j, i)
				}
			}
		}
		for i := 0; i < 16; i++ {
			if iRS[i] != iRefRS[i] {
				t.Fatalf("IdctResAddPred_c modified pRs")
			}
		}
	}
}

func TestDecoderDecodeMbAux_IdctResAddPred8x8_c(t *testing.T) {
	const kiStride = 32
	const iBits = 12
	const iMask = (1 << iBits) - 1
	const iOffset = 1 << (iBits - 1)
	iRS := make([]int16, 64)
	iRefRS := make([]int16, 64)
	uiPred := make([]uint8, 64*kiStride)
	uiRefPred := make([]uint8, 64*kiStride)
	for iRunTimes := 0; iRunTimes < 1000; iRunTimes++ {
		for i := 0; i < 64; i++ {
			v := int16((rand.Int() & iMask) - iOffset)
			iRefRS[i], iRS[i] = v, v
		}
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				v := uint8(rand.Intn(256))
				uiRefPred[i*kiStride+j], uiPred[i*kiStride+j] = v, v
			}
		}
		IdctResAddPred8x8_c(uiPred, 0, kiStride, iRS)
		IdctResAddPred8x8_ref(uiRefPred, 0, kiStride, iRefRS)
		for i := 0; i < 8; i++ {
			for j := 0; j < 8; j++ {
				if uiRefPred[i*kiStride+j] != uiPred[i*kiStride+j] {
					t.Fatalf("mismatch at (%d,%d)", j, i)
				}
			}
		}
	}
}

// GetI4LumaIChromaAddrTable has no C unit test; check it against the
// raster position of each 4x4 block (scan8 order) directly.
func TestDecoderDecodeMbAux_GetI4LumaIChromaAddrTable(t *testing.T) {
	const kiYStride, kiUVStride = 64, 40
	var pOffset [24]int32
	GetI4LumaIChromaAddrTable(pOffset[:], kiYStride, kiUVStride)
	// luma blocks in decoding (scan4) order: 8x8 quadrant z-order, then 4x4 z-order
	for i := 0; i < 16; i++ {
		x := ((i>>2)&1)*2 + (i & 1)
		y := ((i>>3)&1)*2 + ((i >> 1) & 1)
		if want := int32(4*x + 4*y*kiYStride); pOffset[i] != want {
			t.Errorf("luma %d: got %d want %d", i, pOffset[i], want)
		}
	}
	for i := 0; i < 4; i++ {
		x, y := i&1, i>>1
		want := int32(4*x + 4*y*kiUVStride)
		if pOffset[16+i] != want || pOffset[20+i] != want {
			t.Errorf("chroma %d: got %d/%d want %d", i, pOffset[16+i], pOffset[20+i], want)
		}
	}
}
