// Port of codec/encoder/core/src/get_intra_predictor.cpp.

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

const (
	I4x4_COUNT   = 4
	I8x8_COUNT   = 8
	I16x16_COUNT = 16

	I4x4_PRED_STRIDE  = 4
	I4x4_PRED_STRIDE2 = 8
	I4x4_PRED_STRIDE3 = 12

	I8x8_PRED_STRIDE = 8
)

// WelsFillingPred8to16_c: 8 bytes of pSrc repeated twice.
func WelsFillingPred8to16_c(pPred []uint8, iPredOff int, pSrc []uint8) {
	copy(pPred[iPredOff:iPredOff+8], pSrc[:8])
	copy(pPred[iPredOff+8:iPredOff+16], pSrc[:8])
}

// WelsFillingPred8x2to16_c: 16 bytes of pSrc.
func WelsFillingPred8x2to16_c(pPred []uint8, iPredOff int, pSrc []uint8) {
	copy(pPred[iPredOff:iPredOff+16], pSrc[:16])
}

// WelsFillingPred1to16_c: 16 copies of kuiSrc.
func WelsFillingPred1to16_c(pPred []uint8, iPredOff int, kuiSrc uint8) {
	intraPredFillBytes(pPred[iPredOff:iPredOff+16], kuiSrc)
}

func intraPredFillBytes(p []uint8, v uint8) {
	for i := range p {
		p[i] = v
	}
}

func WelsI4x4LumaPredV_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	var uiSrcx2 [8]uint8
	top := iRefOff - int(kiStride)
	copy(uiSrcx2[0:4], pRef[top:top+4])
	copy(uiSrcx2[4:8], pRef[top:top+4])

	WelsFillingPred8to16_c(pPred, iPredOff, uiSrcx2[:])
}

func WelsI4x4LumaPredH_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	kiStridex2Left := int((kiStride << 1) - 1)
	kiStridex3Left := int(kiStride) + kiStridex2Left
	kuiHor1 := pRef[iRefOff-1]
	kuiHor2 := pRef[iRefOff+int(kiStride)-1]
	kuiHor3 := pRef[iRefOff+kiStridex2Left]
	kuiHor4 := pRef[iRefOff+kiStridex3Left]
	var uiSrc [16]uint8
	intraPredFillBytes(uiSrc[0:4], kuiHor1)
	intraPredFillBytes(uiSrc[4:8], kuiHor2)
	intraPredFillBytes(uiSrc[8:12], kuiHor3)
	intraPredFillBytes(uiSrc[12:16], kuiHor4)

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

func WelsI4x4LumaPredDc_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiDcValue := uint8((r(-1) + r(s-1) + r((s<<1)-1) + r((s<<1)+s-1) +
		r(-s) + r(1-s) + r(2-s) + r(3-s) + 4) >> 3)

	WelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

func WelsI4x4LumaPredDcLeft_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiDcValue := uint8((r(-1) + r(s-1) + r((s<<1)-1) + r((s<<1)+s-1) + 2) >> 2)

	WelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

func WelsI4x4LumaPredDcTop_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiDcValue := uint8((r(-s) + r(1-s) + r(2-s) + r(3-s) + 2) >> 2)

	WelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

func WelsI4x4LumaPredDcNA_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	const kuiDcValue = 0x80

	WelsFillingPred1to16_c(pPred, iPredOff, kuiDcValue)
}

/*down pLeft*/
func WelsI4x4LumaPredDDL_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	/*get pTop*/
	kuiT0 := r(-s)
	kuiT1 := r(1 - s)
	kuiT2 := r(2 - s)
	kuiT3 := r(3 - s)
	kuiT4 := r(4 - s)
	kuiT5 := r(5 - s)
	kuiT6 := r(6 - s)
	kuiT7 := r(7 - s)
	kuiDDL0 := uint8((2 + kuiT0 + kuiT2 + (kuiT1 << 1)) >> 2) // uiDDL0
	kuiDDL1 := uint8((2 + kuiT1 + kuiT3 + (kuiT2 << 1)) >> 2) // uiDDL1
	kuiDDL2 := uint8((2 + kuiT2 + kuiT4 + (kuiT3 << 1)) >> 2) // uiDDL2
	kuiDDL3 := uint8((2 + kuiT3 + kuiT5 + (kuiT4 << 1)) >> 2) // uiDDL3
	kuiDDL4 := uint8((2 + kuiT4 + kuiT6 + (kuiT5 << 1)) >> 2) // uiDDL4
	kuiDDL5 := uint8((2 + kuiT5 + kuiT7 + (kuiT6 << 1)) >> 2) // uiDDL5
	kuiDDL6 := uint8((2 + kuiT6 + kuiT7 + (kuiT7 << 1)) >> 2) // uiDDL6
	var uiSrc [16]uint8
	uiSrc[0] = kuiDDL0
	uiSrc[1], uiSrc[4] = kuiDDL1, kuiDDL1
	uiSrc[2], uiSrc[5], uiSrc[8] = kuiDDL2, kuiDDL2, kuiDDL2
	uiSrc[3], uiSrc[6], uiSrc[9], uiSrc[12] = kuiDDL3, kuiDDL3, kuiDDL3, kuiDDL3
	uiSrc[7], uiSrc[10], uiSrc[13] = kuiDDL4, kuiDDL4, kuiDDL4
	uiSrc[11], uiSrc[14] = kuiDDL5, kuiDDL5
	uiSrc[15] = kuiDDL6

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

/*down pLeft*/
func WelsI4x4LumaPredDDLTop_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	/*get pTop*/
	kuiT0 := r(-s)
	kuiT1 := r(1 - s)
	kuiT2 := r(2 - s)
	kuiT3 := r(3 - s)
	kuiDLT0 := uint8((2 + kuiT0 + kuiT2 + (kuiT1 << 1)) >> 2) // uiDLT0
	kuiDLT1 := uint8((2 + kuiT1 + kuiT3 + (kuiT2 << 1)) >> 2) // uiDLT1
	kuiDLT2 := uint8((2 + kuiT2 + kuiT3 + (kuiT3 << 1)) >> 2) // uiDLT2
	kuiDLT3 := uint8((2 + (kuiT3 << 2)) >> 2)                 // uiDLT3
	var uiSrc [16]uint8
	intraPredFillBytes(uiSrc[6:16], kuiDLT3)
	uiSrc[0] = kuiDLT0
	uiSrc[1], uiSrc[4] = kuiDLT1, kuiDLT1
	uiSrc[2], uiSrc[5], uiSrc[8] = kuiDLT2, kuiDLT2, kuiDLT2
	uiSrc[3] = kuiDLT3

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

/*down right*/
func WelsI4x4LumaPredDDR_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kiStridex2 := s << 1
	kiStridex3 := s + kiStridex2
	kuiLT := r(-s - 1) // pTop-pLeft
	/*get pLeft and pTop*/
	kuiL0 := r(-1)
	kuiL1 := r(s - 1)
	kuiL2 := r(kiStridex2 - 1)
	kuiL3 := r(kiStridex3 - 1)
	kuiT0 := r(-s)
	kuiT1 := r(1 - s)
	kuiT2 := r(2 - s)
	kuiT3 := r(3 - s)
	kuiTL0 := 1 + kuiLT + kuiL0
	kuiLT0 := 1 + kuiLT + kuiT0
	kuiT01 := 1 + kuiT0 + kuiT1
	kuiT12 := 1 + kuiT1 + kuiT2
	kuiT23 := 1 + kuiT2 + kuiT3
	kuiL01 := 1 + kuiL0 + kuiL1
	kuiL12 := 1 + kuiL1 + kuiL2
	kuiL23 := 1 + kuiL2 + kuiL3
	kuiDDR0 := uint8((kuiTL0 + kuiLT0) >> 2)
	kuiDDR1 := uint8((kuiLT0 + kuiT01) >> 2)
	kuiDDR2 := uint8((kuiT01 + kuiT12) >> 2)
	kuiDDR3 := uint8((kuiT12 + kuiT23) >> 2)
	kuiDDR4 := uint8((kuiTL0 + kuiL01) >> 2)
	kuiDDR5 := uint8((kuiL01 + kuiL12) >> 2)
	kuiDDR6 := uint8((kuiL12 + kuiL23) >> 2)
	var uiSrc [16]uint8
	uiSrc[0], uiSrc[5], uiSrc[10], uiSrc[15] = kuiDDR0, kuiDDR0, kuiDDR0, kuiDDR0
	uiSrc[1], uiSrc[6], uiSrc[11] = kuiDDR1, kuiDDR1, kuiDDR1
	uiSrc[2], uiSrc[7] = kuiDDR2, kuiDDR2
	uiSrc[3] = kuiDDR3
	uiSrc[4], uiSrc[9], uiSrc[14] = kuiDDR4, kuiDDR4, kuiDDR4
	uiSrc[8], uiSrc[13] = kuiDDR5, kuiDDR5
	uiSrc[12] = kuiDDR6

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

/*vertical pLeft*/
func WelsI4x4LumaPredVL_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	/*get pTop*/
	kuiT0 := r(-s)
	kuiT1 := r(1 - s)
	kuiT2 := r(2 - s)
	kuiT3 := r(3 - s)
	kuiT4 := r(4 - s)
	kuiT5 := r(5 - s)
	kuiT6 := r(6 - s)
	kuiVL0 := uint8((1 + kuiT0 + kuiT1) >> 1)                // uiVL0
	kuiVL1 := uint8((1 + kuiT1 + kuiT2) >> 1)                // uiVL1
	kuiVL2 := uint8((1 + kuiT2 + kuiT3) >> 1)                // uiVL2
	kuiVL3 := uint8((1 + kuiT3 + kuiT4) >> 1)                // uiVL3
	kuiVL4 := uint8((1 + kuiT4 + kuiT5) >> 1)                // uiVL4
	kuiVL5 := uint8((2 + kuiT0 + (kuiT1 << 1) + kuiT2) >> 2) // uiVL5
	kuiVL6 := uint8((2 + kuiT1 + (kuiT2 << 1) + kuiT3) >> 2) // uiVL6
	kuiVL7 := uint8((2 + kuiT2 + (kuiT3 << 1) + kuiT4) >> 2) // uiVL7
	kuiVL8 := uint8((2 + kuiT3 + (kuiT4 << 1) + kuiT5) >> 2) // uiVL8
	kuiVL9 := uint8((2 + kuiT4 + (kuiT5 << 1) + kuiT6) >> 2) // uiVL9
	var uiSrc [16]uint8
	uiSrc[0] = kuiVL0
	uiSrc[1], uiSrc[8] = kuiVL1, kuiVL1
	uiSrc[2], uiSrc[9] = kuiVL2, kuiVL2
	uiSrc[3], uiSrc[10] = kuiVL3, kuiVL3
	uiSrc[4] = kuiVL5
	uiSrc[5], uiSrc[12] = kuiVL6, kuiVL6
	uiSrc[6], uiSrc[13] = kuiVL7, kuiVL7
	uiSrc[7], uiSrc[14] = kuiVL8, kuiVL8
	uiSrc[11] = kuiVL4
	uiSrc[15] = kuiVL9

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

/*vertical pLeft*/
func WelsI4x4LumaPredVLTop_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	pTopLeft := iRefOff - int(kiStride) - 1 // pTop-pLeft
	/*get pTop*/
	kuiT0 := int32(pRef[pTopLeft+1])
	kuiT1 := int32(pRef[pTopLeft+2])
	kuiT2 := int32(pRef[pTopLeft+3])
	kuiT3 := int32(pRef[pTopLeft+4])
	kuiVLT0 := uint8((1 + kuiT0 + kuiT1) >> 1)                // uiVLT0
	kuiVLT1 := uint8((1 + kuiT1 + kuiT2) >> 1)                // uiVLT1
	kuiVLT2 := uint8((1 + kuiT2 + kuiT3) >> 1)                // uiVLT2
	kuiVLT3 := uint8((1 + (kuiT3 << 1)) >> 1)                 // uiVLT3
	kuiVLT4 := uint8((2 + kuiT0 + (kuiT1 << 1) + kuiT2) >> 2) // uiVLT4
	kuiVLT5 := uint8((2 + kuiT1 + (kuiT2 << 1) + kuiT3) >> 2) // uiVLT5
	kuiVLT6 := uint8((2 + kuiT2 + (kuiT3 << 1) + kuiT3) >> 2) // uiVLT6
	kuiVLT7 := uint8((2 + (kuiT3 << 2)) >> 2)                 // uiVLT7
	var uiSrc [16]uint8
	uiSrc[0] = kuiVLT0
	uiSrc[1], uiSrc[8] = kuiVLT1, kuiVLT1
	uiSrc[2], uiSrc[9] = kuiVLT2, kuiVLT2
	uiSrc[3], uiSrc[10], uiSrc[11] = kuiVLT3, kuiVLT3, kuiVLT3
	uiSrc[4] = kuiVLT4
	uiSrc[5], uiSrc[12] = kuiVLT5, kuiVLT5
	uiSrc[6], uiSrc[13] = kuiVLT6, kuiVLT6
	uiSrc[7], uiSrc[14], uiSrc[15] = kuiVLT7, kuiVLT7, kuiVLT7

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

/*vertical right*/
func WelsI4x4LumaPredVR_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kiStridex2 := s << 1
	kuiLT := r(-s - 1) // pTop-pLeft
	/*get pLeft and pTop*/
	kuiL0 := r(-1)
	kuiL1 := r(s - 1)
	kuiL2 := r(kiStridex2 - 1)
	kuiT0 := r(-s)
	kuiT1 := r(1 - s)
	kuiT2 := r(2 - s)
	kuiT3 := r(3 - s)
	kuiVR0 := uint8((1 + kuiLT + kuiT0) >> 1)
	kuiVR1 := uint8((1 + kuiT0 + kuiT1) >> 1)
	kuiVR2 := uint8((1 + kuiT1 + kuiT2) >> 1)
	kuiVR3 := uint8((1 + kuiT2 + kuiT3) >> 1)
	kuiVR4 := uint8((2 + kuiL0 + (kuiLT << 1) + kuiT0) >> 2)
	kuiVR5 := uint8((2 + kuiLT + (kuiT0 << 1) + kuiT1) >> 2)
	kuiVR6 := uint8((2 + kuiT0 + (kuiT1 << 1) + kuiT2) >> 2)
	kuiVR7 := uint8((2 + kuiT1 + (kuiT2 << 1) + kuiT3) >> 2)
	kuiVR8 := uint8((2 + kuiLT + (kuiL0 << 1) + kuiL1) >> 2)
	kuiVR9 := uint8((2 + kuiL0 + (kuiL1 << 1) + kuiL2) >> 2)
	var uiSrc [16]uint8
	uiSrc[0], uiSrc[9] = kuiVR0, kuiVR0
	uiSrc[1], uiSrc[10] = kuiVR1, kuiVR1
	uiSrc[2], uiSrc[11] = kuiVR2, kuiVR2
	uiSrc[3] = kuiVR3
	uiSrc[4], uiSrc[13] = kuiVR4, kuiVR4
	uiSrc[5], uiSrc[14] = kuiVR5, kuiVR5
	uiSrc[6], uiSrc[15] = kuiVR6, kuiVR6
	uiSrc[7] = kuiVR7
	uiSrc[8] = kuiVR8
	uiSrc[12] = kuiVR9

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

/*horizontal up*/
func WelsI4x4LumaPredHU_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kiStridex2 := s << 1
	kiStridex3 := s + kiStridex2
	/*get pLeft*/
	kuiL0 := r(-1)
	kuiL1 := r(s - 1)
	kuiL2 := r(kiStridex2 - 1)
	kuiL3 := r(kiStridex3 - 1)
	kuiL01 := 1 + kuiL0 + kuiL1
	kuiL12 := 1 + kuiL1 + kuiL2
	kuiL23 := 1 + kuiL2 + kuiL3
	kuiHU0 := uint8(kuiL01 >> 1)
	kuiHU1 := uint8((kuiL01 + kuiL12) >> 2)
	kuiHU2 := uint8(kuiL12 >> 1)
	kuiHU3 := uint8((kuiL12 + kuiL23) >> 2)
	kuiHU4 := uint8(kuiL23 >> 1)
	kuiHU5 := uint8((1 + kuiL23 + (kuiL3 << 1)) >> 2)
	var uiSrc [16]uint8
	uiSrc[0] = kuiHU0
	uiSrc[1] = kuiHU1
	uiSrc[2], uiSrc[4] = kuiHU2, kuiHU2
	uiSrc[3], uiSrc[5] = kuiHU3, kuiHU3
	uiSrc[6], uiSrc[8] = kuiHU4, kuiHU4
	uiSrc[7], uiSrc[9] = kuiHU5, kuiHU5
	intraPredFillBytes(uiSrc[10:16], uint8(kuiL3))

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

/*horizontal down*/
func WelsI4x4LumaPredHD_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kiStridex2 := s << 1
	kiStridex3 := s + kiStridex2
	kuiLT := r(-s - 1) // pTop-pLeft
	/*get pLeft and pTop*/
	kuiL0 := r(-1)
	kuiL1 := r(s - 1)
	kuiL2 := r(kiStridex2 - 1)
	kuiL3 := r(kiStridex3 - 1)
	kuiT0 := r(-s)
	kuiT1 := r(1 - s)
	kuiT2 := r(2 - s)
	kuiHD0 := uint8((1 + kuiLT + kuiL0) >> 1)                // uiHD0
	kuiHD1 := uint8((2 + kuiL0 + (kuiLT << 1) + kuiT0) >> 2) // uiHD1
	kuiHD2 := uint8((2 + kuiLT + (kuiT0 << 1) + kuiT1) >> 2) // uiHD2
	kuiHD3 := uint8((2 + kuiT0 + (kuiT1 << 1) + kuiT2) >> 2) // uiHD3
	kuiHD4 := uint8((1 + kuiL0 + kuiL1) >> 1)                // uiHD4
	kuiHD5 := uint8((2 + kuiLT + (kuiL0 << 1) + kuiL1) >> 2) // uiHD5
	kuiHD6 := uint8((1 + kuiL1 + kuiL2) >> 1)                // uiHD6
	kuiHD7 := uint8((2 + kuiL0 + (kuiL1 << 1) + kuiL2) >> 2) // uiHD7
	kuiHD8 := uint8((1 + kuiL2 + kuiL3) >> 1)                // uiHD8
	kuiHD9 := uint8((2 + kuiL1 + (kuiL2 << 1) + kuiL3) >> 2) // uiHD9
	var uiSrc [16]uint8
	uiSrc[0], uiSrc[6] = kuiHD0, kuiHD0
	uiSrc[1], uiSrc[7] = kuiHD1, kuiHD1
	uiSrc[2] = kuiHD2
	uiSrc[3] = kuiHD3
	uiSrc[4], uiSrc[10] = kuiHD4, kuiHD4
	uiSrc[5], uiSrc[11] = kuiHD5, kuiHD5
	uiSrc[8], uiSrc[14] = kuiHD6, kuiHD6
	uiSrc[9], uiSrc[15] = kuiHD7, kuiHD7
	uiSrc[12] = kuiHD8
	uiSrc[13] = kuiHD9

	WelsFillingPred8x2to16_c(pPred, iPredOff, uiSrc[:])
}

func WelsIChromaPredV_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	top := iRefOff - int(kiStride)
	kuiSrc64 := pRef[top : top+8]
	for i := 0; i < 8; i++ {
		copy(pPred[iPredOff+i*8:iPredOff+i*8+8], kuiSrc64)
	}
}

func WelsIChromaPredH_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	iStridex7 := (kiStride << 3) - kiStride
	iI8x8Stridex7 := int32((I8x8_PRED_STRIDE << 3) - I8x8_PRED_STRIDE)
	for i := 0; i < 8; i++ {
		kuiLeft := pRef[iRefOff+int(iStridex7)-1] // pLeft value
		o := iPredOff + int(iI8x8Stridex7)
		intraPredFillBytes(pPred[o:o+8], kuiLeft)

		iStridex7 -= kiStride
		iI8x8Stridex7 -= I8x8_PRED_STRIDE
	}
}

func WelsIChromaPredPlane_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	var iLTshift, iTopshift, iLeftshift, iTopSum, iLeftSum int32
	s := int(kiStride)
	pTop := iRefOff - s
	pLeft := iRefOff - 1

	for i := 0; i < 4; i++ {
		iTopSum += int32(i+1) * (int32(pRef[pTop+4+i]) - int32(pRef[pTop+2-i]))
		iLeftSum += int32(i+1) * (int32(pRef[pLeft+(4+i)*s]) - int32(pRef[pLeft+(2-i)*s]))
	}

	iLTshift = (int32(pRef[pLeft+7*s]) + int32(pRef[pTop+7])) << 4
	iTopshift = (17*iTopSum + 16) >> 5
	iLeftshift = (17*iLeftSum + 16) >> 5

	p := iPredOff
	for i := int32(0); i < 8; i++ {
		for j := int32(0); j < 8; j++ {
			pPred[p+int(j)] = common.WelsClip1((iLTshift + iTopshift*(j-3) + iLeftshift*(i-3) + 16) >> 5)
		}
		p += I8x8_PRED_STRIDE
	}
}

func fillChroma8x8TopBottom(pPred []uint8, iPredOff int, kuiTop [8]uint8, kuiBottom [8]uint8) {
	for i := 0; i < 4; i++ {
		copy(pPred[iPredOff+i*8:iPredOff+i*8+8], kuiTop[:])
	}
	for i := 4; i < 8; i++ {
		copy(pPred[iPredOff+i*8:iPredOff+i*8+8], kuiBottom[:])
	}
}

func WelsIChromaPredDc_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiL1 := s - 1
	kuiL2 := kuiL1 + s
	kuiL3 := kuiL2 + s
	kuiL4 := kuiL3 + s
	kuiL5 := kuiL4 + s
	kuiL6 := kuiL5 + s
	kuiL7 := kuiL6 + s
	/*caculate the iMean value*/
	kuiMean1 := uint8((r(-s) + r(1-s) + r(2-s) + r(3-s) + r(-1) + r(kuiL1) + r(kuiL2) + r(kuiL3) + 4) >> 3)
	kuiSum2 := uint32(r(4-s) + r(5-s) + r(6-s) + r(7-s))
	kuiSum3 := uint32(r(kuiL4) + r(kuiL5) + r(kuiL6) + r(kuiL7))
	kuiMean2 := uint8((kuiSum2 + 2) >> 2)
	kuiMean3 := uint8((kuiSum3 + 2) >> 2)
	kuiMean4 := uint8((kuiSum2 + kuiSum3 + 4) >> 3)

	kuiTopMean := [8]uint8{kuiMean1, kuiMean1, kuiMean1, kuiMean1, kuiMean2, kuiMean2, kuiMean2, kuiMean2}
	kuiBottomMean := [8]uint8{kuiMean3, kuiMean3, kuiMean3, kuiMean3, kuiMean4, kuiMean4, kuiMean4, kuiMean4}
	fillChroma8x8TopBottom(pPred, iPredOff, kuiTopMean, kuiBottomMean)
}

func WelsIChromaPredDcLeft_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	kuiL1 := s - 1
	kuiL2 := kuiL1 + s
	kuiL3 := kuiL2 + s
	kuiL4 := kuiL3 + s
	kuiL5 := kuiL4 + s
	kuiL6 := kuiL5 + s
	kuiL7 := kuiL6 + s
	/*caculate the iMean value*/
	kuiTopMean := uint8((r(-1) + r(kuiL1) + r(kuiL2) + r(kuiL3) + 2) >> 2)
	kuiBottomMean := uint8((r(kuiL4) + r(kuiL5) + r(kuiL6) + r(kuiL7) + 2) >> 2)
	t := [8]uint8{kuiTopMean, kuiTopMean, kuiTopMean, kuiTopMean, kuiTopMean, kuiTopMean, kuiTopMean, kuiTopMean}
	b := [8]uint8{kuiBottomMean, kuiBottomMean, kuiBottomMean, kuiBottomMean, kuiBottomMean, kuiBottomMean, kuiBottomMean, kuiBottomMean}
	fillChroma8x8TopBottom(pPred, iPredOff, t, b)
}

func WelsIChromaPredDcTop_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	s := int(kiStride)
	r := func(k int) int32 { return int32(pRef[iRefOff+k]) }
	/*caculate the iMean value*/
	kuiMean1 := uint8((r(-s) + r(1-s) + r(2-s) + r(3-s) + 2) >> 2)
	kuiMean2 := uint8((r(4-s) + r(5-s) + r(6-s) + r(7-s) + 2) >> 2)
	kuiMean := [8]uint8{kuiMean1, kuiMean1, kuiMean1, kuiMean1, kuiMean2, kuiMean2, kuiMean2, kuiMean2}
	fillChroma8x8TopBottom(pPred, iPredOff, kuiMean, kuiMean)
}

func WelsIChromaPredDcNA_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	intraPredFillBytes(pPred[iPredOff:iPredOff+64], 0x80)
}

func WelsI16x16LumaPredPlane_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	var iLTshift, iTopshift, iLeftshift, iTopSum, iLeftSum int32
	s := int(kiStride)
	pTop := iRefOff - s
	pLeft := iRefOff - 1
	const iPredStride = 16

	for i := 0; i < 8; i++ {
		iTopSum += int32(i+1) * (int32(pRef[pTop+8+i]) - int32(pRef[pTop+6-i]))
		iLeftSum += int32(i+1) * (int32(pRef[pLeft+(8+i)*s]) - int32(pRef[pLeft+(6-i)*s]))
	}

	iLTshift = (int32(pRef[pLeft+15*s]) + int32(pRef[pTop+15])) << 4
	iTopshift = (5*iTopSum + 32) >> 6
	iLeftshift = (5*iLeftSum + 32) >> 6

	p := iPredOff
	for i := int32(0); i < 16; i++ {
		for j := int32(0); j < 16; j++ {
			pPred[p+int(j)] = common.WelsClip1((iLTshift + iTopshift*(j-7) + iLeftshift*(i-7) + 16) >> 5)
		}
		p += iPredStride
	}
}

func WelsI16x16LumaPredDc_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	iStridex15 := (kiStride << 4) - kiStride
	var iSum int32

	/*caculate the iMean value*/
	for i := 15; i >= 0; i-- {
		iSum += int32(pRef[iRefOff-1+int(iStridex15)]) + int32(pRef[iRefOff-int(kiStride)+i])
		iStridex15 -= kiStride
	}
	iMean := uint8((16 + iSum) >> 5)
	intraPredFillBytes(pPred[iPredOff:iPredOff+256], iMean)
}

func WelsI16x16LumaPredDcTop_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	var iSum int32

	/*caculate the iMean value*/
	for i := 15; i >= 0; i-- {
		iSum += int32(pRef[iRefOff-int(kiStride)+i])
	}
	iMean := uint8((8 + iSum) >> 4)
	intraPredFillBytes(pPred[iPredOff:iPredOff+256], iMean)
}

func WelsI16x16LumaPredDcLeft_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	iStridex15 := (kiStride << 4) - kiStride
	var iSum int32

	/*caculate the iMean value*/
	for i := 15; i >= 0; i-- {
		iSum += int32(pRef[iRefOff-1+int(iStridex15)])
		iStridex15 -= kiStride
	}
	iMean := uint8((8 + iSum) >> 4)
	intraPredFillBytes(pPred[iPredOff:iPredOff+256], iMean)
}

func WelsI16x16LumaPredDcNA_c(pPred []uint8, iPredOff int, pRef []uint8, iRefOff int, kiStride int32) {
	intraPredFillBytes(pPred[iPredOff:iPredOff+256], 0x80)
}

func WelsInitIntraPredFuncs(pFuncList *SWelsFuncPtrList, kuiCpuFlag uint32) {
	pFuncList.pfGetLumaI16x16Pred[common.I16_PRED_V] = common.WelsI16x16LumaPredV_c
	pFuncList.pfGetLumaI16x16Pred[common.I16_PRED_H] = common.WelsI16x16LumaPredH_c
	pFuncList.pfGetLumaI16x16Pred[common.I16_PRED_DC] = WelsI16x16LumaPredDc_c
	pFuncList.pfGetLumaI16x16Pred[common.I16_PRED_P] = WelsI16x16LumaPredPlane_c
	pFuncList.pfGetLumaI16x16Pred[common.I16_PRED_DC_L] = WelsI16x16LumaPredDcLeft_c
	pFuncList.pfGetLumaI16x16Pred[common.I16_PRED_DC_T] = WelsI16x16LumaPredDcTop_c
	pFuncList.pfGetLumaI16x16Pred[common.I16_PRED_DC_128] = WelsI16x16LumaPredDcNA_c

	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_V] = WelsI4x4LumaPredV_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_H] = WelsI4x4LumaPredH_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_DC] = WelsI4x4LumaPredDc_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_DC_L] = WelsI4x4LumaPredDcLeft_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_DC_T] = WelsI4x4LumaPredDcTop_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_DC_128] = WelsI4x4LumaPredDcNA_c

	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_DDL] = WelsI4x4LumaPredDDL_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_DDL_TOP] = WelsI4x4LumaPredDDLTop_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_DDR] = WelsI4x4LumaPredDDR_c

	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_VL] = WelsI4x4LumaPredVL_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_VL_TOP] = WelsI4x4LumaPredVLTop_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_VR] = WelsI4x4LumaPredVR_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_HU] = WelsI4x4LumaPredHU_c
	pFuncList.pfGetLumaI4x4Pred[common.I4_PRED_HD] = WelsI4x4LumaPredHD_c

	pFuncList.pfGetChromaPred[common.C_PRED_DC] = WelsIChromaPredDc_c
	pFuncList.pfGetChromaPred[common.C_PRED_H] = WelsIChromaPredH_c
	pFuncList.pfGetChromaPred[common.C_PRED_V] = WelsIChromaPredV_c
	pFuncList.pfGetChromaPred[common.C_PRED_P] = WelsIChromaPredPlane_c
	pFuncList.pfGetChromaPred[common.C_PRED_DC_L] = WelsIChromaPredDcLeft_c
	pFuncList.pfGetChromaPred[common.C_PRED_DC_T] = WelsIChromaPredDcTop_c
	pFuncList.pfGetChromaPred[common.C_PRED_DC_128] = WelsIChromaPredDcNA_c
	initIntraPredSIMD(pFuncList, kuiCpuFlag)
}
