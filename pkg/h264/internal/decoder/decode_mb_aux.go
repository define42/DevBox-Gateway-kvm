// Port of codec/decoder/core/src/decode_mb_aux.cpp.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// IdctResAddPred_c ports void IdctResAddPred_c (uint8_t* pPred, const int32_t kiStride, int16_t* pRs).
//
// pPred: (slice, offset) pair; pRs: coefficient sub-slice.
//
// NOTE::: p_RS should NOT be modified and it will lead to mismatch with JSVM.
// so should allocate kA array to store the temporary value (idct).
func IdctResAddPred_c(pPred []uint8, iPredOff int, kiStride int32, pRs []int16) {
	var iSrc [16]int16

	pDst := pPred
	iDstOff := iPredOff
	kiStride1 := int(kiStride)
	kiStride2 := int(kiStride << 1)
	kiStride3 := int(kiStride) + kiStride2

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

		pDst[iDstOff+i] = common.WelsClip1(kT3 + int32(pPred[iPredOff+i]))
		pDst[iDstOff+i+kiStride3] = common.WelsClip1(kT4 + int32(pPred[iPredOff+i+kiStride3]))

		kT1 = int32(iSrc[i]) - int32(iSrc[i+8])
		kT2 = (int32(iSrc[i+4]) >> 1) - int32(iSrc[i+12])
		pDst[iDstOff+i+kiStride1] = common.WelsClip1(((32 + kT1 + kT2) >> 6) + int32(pDst[iDstOff+i+kiStride1]))
		pDst[iDstOff+i+kiStride2] = common.WelsClip1(((32 + kT1 - kT2) >> 6) + int32(pDst[iDstOff+i+kiStride2]))
	}
}

// IdctResAddPred8x8_c ports void IdctResAddPred8x8_c (uint8_t* pPred, const int32_t kiStride, int16_t* pRs).
//
// pPred: (slice, offset) pair; pRs: coefficient sub-slice.
func IdctResAddPred8x8_c(pPred []uint8, iPredOff int, kiStride int32, pRs []int16) {
	// To make the ASM code easy to write, should using one funciton to apply hor and ver together, such as we did on HEVC
	// Ugly code, just for easy debug, the final version need optimization
	var p, b [8]int16
	var a [4]int16

	var iTmp [64]int16
	var iRes [64]int16

	// i16 helpers: C computes in int and truncates on store to int16_t.
	// Horizontal
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			p[j] = pRs[j+(i<<3)]
		}
		idct8x8_1d(&p, &a, &b)

		iTmp[0+(i<<3)] = int16(int32(b[0]) + int32(b[7]))
		iTmp[1+(i<<3)] = int16(int32(b[2]) - int32(b[5]))
		iTmp[2+(i<<3)] = int16(int32(b[4]) + int32(b[3]))
		iTmp[3+(i<<3)] = int16(int32(b[6]) + int32(b[1]))
		iTmp[4+(i<<3)] = int16(int32(b[6]) - int32(b[1]))
		iTmp[5+(i<<3)] = int16(int32(b[4]) - int32(b[3]))
		iTmp[6+(i<<3)] = int16(int32(b[2]) + int32(b[5]))
		iTmp[7+(i<<3)] = int16(int32(b[0]) - int32(b[7]))
	}

	//Vertical
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			p[j] = iTmp[i+(j<<3)]
		}
		idct8x8_1d(&p, &a, &b)

		iRes[(0<<3)+i] = int16(int32(b[0]) + int32(b[7]))
		iRes[(1<<3)+i] = int16(int32(b[2]) - int32(b[5]))
		iRes[(2<<3)+i] = int16(int32(b[4]) + int32(b[3]))
		iRes[(3<<3)+i] = int16(int32(b[6]) + int32(b[1]))
		iRes[(4<<3)+i] = int16(int32(b[6]) - int32(b[1]))
		iRes[(5<<3)+i] = int16(int32(b[4]) - int32(b[3]))
		iRes[(6<<3)+i] = int16(int32(b[2]) + int32(b[5]))
		iRes[(7<<3)+i] = int16(int32(b[0]) - int32(b[7]))
	}

	pDst := pPred
	for i := 0; i < 8; i++ {
		for j := 0; j < 8; j++ {
			k := iPredOff + i*int(kiStride) + j
			pDst[k] = common.WelsClip1(((32 + int32(iRes[(i<<3)+j])) >> 6) + int32(pDst[k]))
		}
	}
}

// idct8x8_1d is the butterfly shared by the horizontal and vertical passes of
// IdctResAddPred8x8_c (identical code in C), with int16_t truncating stores.
func idct8x8_1d(p *[8]int16, a *[4]int16, b *[8]int16) {
	p0, p1, p2, p3 := int32(p[0]), int32(p[1]), int32(p[2]), int32(p[3])
	p4, p5, p6, p7 := int32(p[4]), int32(p[5]), int32(p[6]), int32(p[7])

	a[0] = int16(p0 + p4)
	a[1] = int16(p0 - p4)
	a[2] = int16(p6 - (p2 >> 1))
	a[3] = int16(p2 + (p6 >> 1))

	b[0] = int16(int32(a[0]) + int32(a[3]))
	b[2] = int16(int32(a[1]) - int32(a[2]))
	b[4] = int16(int32(a[1]) + int32(a[2]))
	b[6] = int16(int32(a[0]) - int32(a[3]))

	a[0] = int16(-p3 + p5 - p7 - (p7 >> 1))
	a[1] = int16(p1 + p7 - p3 - (p3 >> 1))
	a[2] = int16(-p1 + p7 + p5 + (p5 >> 1))
	a[3] = int16(p3 + p5 + p1 + (p1 >> 1))

	b[1] = int16(int32(a[0]) + (int32(a[3]) >> 2))
	b[3] = int16(int32(a[1]) + (int32(a[2]) >> 2))
	b[5] = int16(int32(a[2]) - (int32(a[1]) >> 2))
	b[7] = int16(int32(a[3]) - (int32(a[0]) >> 2))
}

// GetI4LumaIChromaAddrTable ports void GetI4LumaIChromaAddrTable (int32_t* pBlockOffset,
// const int32_t kiYStride, const int32_t kiUVStride).
func GetI4LumaIChromaAddrTable(pBlockOffset []int32, kiYStride int32, kiUVStride int32) {
	pOffset := pBlockOffset
	kuiScan0 := g_kuiScan8[0]

	for i := 0; i < 16; i++ {
		kuiA := uint32(g_kuiScan8[i]) - uint32(kuiScan0)
		kuiX := kuiA & 0x07
		kuiY := kuiA >> 3

		pOffset[i] = int32((kuiX + uint32(kiYStride)*kuiY) << 2)
	}

	for i := 0; i < 4; i++ {
		kuiA := uint32(g_kuiScan8[i]) - uint32(kuiScan0)

		v := int32(((kuiA & 0x07) + uint32(kiUVStride)*(kuiA>>3)) << 2)
		pOffset[20+i] = v
		pOffset[16+i] = v
	}
}
