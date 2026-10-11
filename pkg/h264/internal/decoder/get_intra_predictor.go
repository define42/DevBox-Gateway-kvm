// Port of codec/decoder/core/src/get_intra_predictor.cpp.
//
// All functions take pPred as a (slice, offset) pair; the neighbouring
// samples (top row at -kiStride, left column at -1) are read from the same
// slice at negative offsets relative to iPredOff.

package decoder

import (
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

const (
	I4x4_COUNT   = 4
	I8x8_COUNT   = 8
	I16x16_COUNT = 16
)

// gipFill4 is ST32A4 (pDst, 0x01010101U * v).
func gipFill4(p []uint8, off int, v uint8) {
	d := p[off : off+4]
	d[0], d[1], d[2], d[3] = v, v, v, v
}

// gipFill8 is ST64A8 (pDst, 0x0101010101010101ULL * v).
func gipFill8(p []uint8, off int, v uint8) {
	d := p[off : off+8]
	for k := range d {
		d[k] = v
	}
}

// gipPx reads pPred[off+k] widened to int32 (C integer promotion).
func gipPx(p []uint8, off int, k int) int32 {
	return int32(p[off+k])
}

// WelsI4x4LumaPredV_c ports void WelsI4x4LumaPredV_c (uint8_t* pPred, const int32_t kiStride).
func WelsI4x4LumaPredV_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	src := iPredOff - s
	copy(pPred[iPredOff:iPredOff+4], pPred[src:src+4])
	copy(pPred[iPredOff+s:iPredOff+s+4], pPred[src:src+4])
	copy(pPred[iPredOff+(s<<1):iPredOff+(s<<1)+4], pPred[src:src+4])
	copy(pPred[iPredOff+(s<<1)+s:iPredOff+(s<<1)+s+4], pPred[src:src+4])
}

// WelsI4x4LumaPredH_c ports void WelsI4x4LumaPredH_c (uint8_t* pPred, const int32_t kiStride).
func WelsI4x4LumaPredH_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	s3 := s2 + s
	kuiL0 := pPred[iPredOff-1]
	kuiL1 := pPred[iPredOff-1+s]
	kuiL2 := pPred[iPredOff-1+s2]
	kuiL3 := pPred[iPredOff-1+s3]

	gipFill4(pPred, iPredOff, kuiL0)
	gipFill4(pPred, iPredOff+s, kuiL1)
	gipFill4(pPred, iPredOff+s2, kuiL2)
	gipFill4(pPred, iPredOff+s3, kuiL3)
}

// WelsI4x4LumaPredDc_c ports void WelsI4x4LumaPredDc_c (uint8_t* pPred, const int32_t kiStride).
func WelsI4x4LumaPredDc_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	s3 := s2 + s
	p, o := pPred, iPredOff
	kuiMean := uint8((gipPx(p, o, -1) + gipPx(p, o, -1+s) + gipPx(p, o, -1+s2) + gipPx(p, o, -1+s3) +
		gipPx(p, o, -s) + gipPx(p, o, -s+1) + gipPx(p, o, -s+2) + gipPx(p, o, -s+3) + 4) >> 3)

	gipFill4(p, o, kuiMean)
	gipFill4(p, o+s, kuiMean)
	gipFill4(p, o+s2, kuiMean)
	gipFill4(p, o+s3, kuiMean)
}

// WelsI4x4LumaPredDcLeft_c ports void WelsI4x4LumaPredDcLeft_c (uint8_t* pPred, const int32_t kiStride).
func WelsI4x4LumaPredDcLeft_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	s3 := s2 + s
	p, o := pPred, iPredOff
	kuiMean := uint8((gipPx(p, o, -1) + gipPx(p, o, -1+s) + gipPx(p, o, -1+s2) + gipPx(p, o, -1+s3) + 2) >> 2)

	gipFill4(p, o, kuiMean)
	gipFill4(p, o+s, kuiMean)
	gipFill4(p, o+s2, kuiMean)
	gipFill4(p, o+s3, kuiMean)
}

// WelsI4x4LumaPredDcTop_c ports void WelsI4x4LumaPredDcTop_c (uint8_t* pPred, const int32_t kiStride).
func WelsI4x4LumaPredDcTop_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	s3 := s2 + s
	p, o := pPred, iPredOff
	kuiMean := uint8((gipPx(p, o, -s) + gipPx(p, o, -s+1) + gipPx(p, o, -s+2) + gipPx(p, o, -s+3) + 2) >> 2)

	gipFill4(p, o, kuiMean)
	gipFill4(p, o+s, kuiMean)
	gipFill4(p, o+s2, kuiMean)
	gipFill4(p, o+s3, kuiMean)
}

// WelsI4x4LumaPredDcNA_c ports void WelsI4x4LumaPredDcNA_c (uint8_t* pPred, const int32_t kiStride).
func WelsI4x4LumaPredDcNA_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	gipFill4(pPred, iPredOff, 0x80)
	gipFill4(pPred, iPredOff+s, 0x80)
	gipFill4(pPred, iPredOff+(s<<1), 0x80)
	gipFill4(pPred, iPredOff+(s<<1)+s, 0x80)
}

// gipStore4Rows does ST32A4 (pPred + r*kiStride, LD32 (kuiList + idx[r])) for r = 0..3.
func gipStore4Rows(pPred []uint8, iPredOff int, s int, kuiList []uint8, idx [4]int) {
	for r := 0; r < 4; r++ {
		copy(pPred[iPredOff+r*s:iPredOff+r*s+4], kuiList[idx[r]:idx[r]+4])
	}
}

// WelsI4x4LumaPredDDL_c ports void WelsI4x4LumaPredDDL_c (uint8_t* pPred, const int32_t kiStride).
// down pLeft
func WelsI4x4LumaPredDDL_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	/*get pTop*/
	ptop := iPredOff - s
	kuiT0 := int32(pPred[ptop])
	kuiT1 := int32(pPred[ptop+1])
	kuiT2 := int32(pPred[ptop+2])
	kuiT3 := int32(pPred[ptop+3])
	kuiT4 := int32(pPred[ptop+4])
	kuiT5 := int32(pPred[ptop+5])
	kuiT6 := int32(pPred[ptop+6])
	kuiT7 := int32(pPred[ptop+7])
	kuiDDL0 := uint8((2 + kuiT0 + kuiT2 + (kuiT1 << 1)) >> 2)
	kuiDDL1 := uint8((2 + kuiT1 + kuiT3 + (kuiT2 << 1)) >> 2)
	kuiDDL2 := uint8((2 + kuiT2 + kuiT4 + (kuiT3 << 1)) >> 2)
	kuiDDL3 := uint8((2 + kuiT3 + kuiT5 + (kuiT4 << 1)) >> 2)
	kuiDDL4 := uint8((2 + kuiT4 + kuiT6 + (kuiT5 << 1)) >> 2)
	kuiDDL5 := uint8((2 + kuiT5 + kuiT7 + (kuiT6 << 1)) >> 2)
	kuiDDL6 := uint8((2 + kuiT6 + kuiT7 + (kuiT7 << 1)) >> 2)
	kuiList := [8]uint8{kuiDDL0, kuiDDL1, kuiDDL2, kuiDDL3, kuiDDL4, kuiDDL5, kuiDDL6, 0}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{0, 1, 2, 3})
}

// WelsI4x4LumaPredDDLTop_c ports void WelsI4x4LumaPredDDLTop_c (uint8_t* pPred, const int32_t kiStride).
// down pLeft
func WelsI4x4LumaPredDDLTop_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	/*get pTop*/
	ptop := iPredOff - s
	kuiT0 := int32(pPred[ptop])
	kuiT1 := int32(pPred[ptop+1])
	kuiT2 := int32(pPred[ptop+2])
	kuiT3 := int32(pPred[ptop+3])
	kuiT01 := 1 + kuiT0 + kuiT1
	kuiT12 := 1 + kuiT1 + kuiT2
	kuiT23 := 1 + kuiT2 + kuiT3
	kuiT33 := 1 + (kuiT3 << 1)
	kuiDLT0 := uint8((kuiT01 + kuiT12) >> 2)
	kuiDLT1 := uint8((kuiT12 + kuiT23) >> 2)
	kuiDLT2 := uint8((kuiT23 + kuiT33) >> 2)
	kuiDLT3 := uint8(kuiT33 >> 1)
	kuiList := [8]uint8{kuiDLT0, kuiDLT1, kuiDLT2, kuiDLT3, kuiDLT3, kuiDLT3, kuiDLT3, kuiDLT3}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{0, 1, 2, 3})
}

// WelsI4x4LumaPredDDR_c ports void WelsI4x4LumaPredDDR_c (uint8_t* pPred, const int32_t kiStride).
// down right
func WelsI4x4LumaPredDDR_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	s3 := s + s2
	ptopleft := iPredOff - (s + 1)
	pleft := iPredOff - 1
	kuiLT := int32(pPred[ptopleft])
	/*get pLeft and pTop*/
	kuiL0 := int32(pPred[pleft])
	kuiL1 := int32(pPred[pleft+s])
	kuiL2 := int32(pPred[pleft+s2])
	kuiL3 := int32(pPred[pleft+s3])
	kuiT0 := int32(pPred[ptopleft+1])
	kuiT1 := int32(pPred[ptopleft+2])
	kuiT2 := int32(pPred[ptopleft+3])
	kuiT3 := int32(pPred[ptopleft+4])
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
	kuiList := [8]uint8{kuiDDR6, kuiDDR5, kuiDDR4, kuiDDR0, kuiDDR1, kuiDDR2, kuiDDR3, 0}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{3, 2, 1, 0})
}

// WelsI4x4LumaPredVL_c ports void WelsI4x4LumaPredVL_c (uint8_t* pPred, const int32_t kiStride).
// vertical pLeft
func WelsI4x4LumaPredVL_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	ptopleft := iPredOff - (s + 1)
	/*get pTop*/
	kuiT0 := int32(pPred[ptopleft+1])
	kuiT1 := int32(pPred[ptopleft+2])
	kuiT2 := int32(pPred[ptopleft+3])
	kuiT3 := int32(pPred[ptopleft+4])
	kuiT4 := int32(pPred[ptopleft+5])
	kuiT5 := int32(pPred[ptopleft+6])
	kuiT6 := int32(pPred[ptopleft+7])
	kuiT01 := 1 + kuiT0 + kuiT1
	kuiT12 := 1 + kuiT1 + kuiT2
	kuiT23 := 1 + kuiT2 + kuiT3
	kuiT34 := 1 + kuiT3 + kuiT4
	kuiT45 := 1 + kuiT4 + kuiT5
	kuiT56 := 1 + kuiT5 + kuiT6
	kuiVL0 := uint8(kuiT01 >> 1)
	kuiVL1 := uint8(kuiT12 >> 1)
	kuiVL2 := uint8(kuiT23 >> 1)
	kuiVL3 := uint8(kuiT34 >> 1)
	kuiVL4 := uint8(kuiT45 >> 1)
	kuiVL5 := uint8((kuiT01 + kuiT12) >> 2)
	kuiVL6 := uint8((kuiT12 + kuiT23) >> 2)
	kuiVL7 := uint8((kuiT23 + kuiT34) >> 2)
	kuiVL8 := uint8((kuiT34 + kuiT45) >> 2)
	kuiVL9 := uint8((kuiT45 + kuiT56) >> 2)
	kuiList := [10]uint8{kuiVL0, kuiVL1, kuiVL2, kuiVL3, kuiVL4, kuiVL5, kuiVL6, kuiVL7, kuiVL8, kuiVL9}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{0, 5, 1, 6})
}

// WelsI4x4LumaPredVLTop_c ports void WelsI4x4LumaPredVLTop_c (uint8_t* pPred, const int32_t kiStride).
// vertical pLeft
func WelsI4x4LumaPredVLTop_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	ptopleft := iPredOff - (s + 1)
	/*get pTop*/
	kuiT0 := int32(pPred[ptopleft+1])
	kuiT1 := int32(pPred[ptopleft+2])
	kuiT2 := int32(pPred[ptopleft+3])
	kuiT3 := int32(pPred[ptopleft+4])
	kuiT01 := 1 + kuiT0 + kuiT1
	kuiT12 := 1 + kuiT1 + kuiT2
	kuiT23 := 1 + kuiT2 + kuiT3
	kuiT33 := 1 + (kuiT3 << 1)
	kuiVL0 := uint8(kuiT01 >> 1)
	kuiVL1 := uint8(kuiT12 >> 1)
	kuiVL2 := uint8(kuiT23 >> 1)
	kuiVL3 := uint8(kuiT33 >> 1)
	kuiVL4 := uint8((kuiT01 + kuiT12) >> 2)
	kuiVL5 := uint8((kuiT12 + kuiT23) >> 2)
	kuiVL6 := uint8((kuiT23 + kuiT33) >> 2)
	kuiVL7 := kuiVL3
	kuiList := [10]uint8{kuiVL0, kuiVL1, kuiVL2, kuiVL3, kuiVL3, kuiVL4, kuiVL5, kuiVL6, kuiVL7, kuiVL7}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{0, 5, 1, 6})
}

// WelsI4x4LumaPredVR_c ports void WelsI4x4LumaPredVR_c (uint8_t* pPred, const int32_t kiStride).
// vertical right
func WelsI4x4LumaPredVR_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	p, o := pPred, iPredOff
	kuiLT := gipPx(p, o, -s-1)
	/*get pLeft and pTop*/
	kuiL0 := gipPx(p, o, -1)
	kuiL1 := gipPx(p, o, s-1)
	kuiL2 := gipPx(p, o, s2-1)
	kuiT0 := gipPx(p, o, -s)
	kuiT1 := gipPx(p, o, 1-s)
	kuiT2 := gipPx(p, o, 2-s)
	kuiT3 := gipPx(p, o, 3-s)
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
	kuiList := [10]uint8{kuiVR8, kuiVR0, kuiVR1, kuiVR2, kuiVR3, kuiVR9, kuiVR4, kuiVR5, kuiVR6, kuiVR7}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{1, 6, 0, 5})
}

// WelsI4x4LumaPredHU_c ports void WelsI4x4LumaPredHU_c (uint8_t* pPred, const int32_t kiStride).
// horizontal up
func WelsI4x4LumaPredHU_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	s3 := s + s2
	p, o := pPred, iPredOff
	/*get pLeft*/
	kuiL0 := gipPx(p, o, -1)
	kuiL1 := gipPx(p, o, s-1)
	kuiL2 := gipPx(p, o, s2-1)
	kuiL3 := gipPx(p, o, s3-1)
	kuiL01 := 1 + kuiL0 + kuiL1
	kuiL12 := 1 + kuiL1 + kuiL2
	kuiL23 := 1 + kuiL2 + kuiL3
	kuiHU0 := uint8(kuiL01 >> 1)
	kuiHU1 := uint8((kuiL01 + kuiL12) >> 2)
	kuiHU2 := uint8(kuiL12 >> 1)
	kuiHU3 := uint8((kuiL12 + kuiL23) >> 2)
	kuiHU4 := uint8(kuiL23 >> 1)
	kuiHU5 := uint8((1 + kuiL23 + (kuiL3 << 1)) >> 2)
	l3 := uint8(kuiL3)
	kuiList := [10]uint8{kuiHU0, kuiHU1, kuiHU2, kuiHU3, kuiHU4, kuiHU5, l3, l3, l3, l3}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{0, 2, 4, 6})
}

// WelsI4x4LumaPredHD_c ports void WelsI4x4LumaPredHD_c (uint8_t* pPred, const int32_t kiStride).
// horizontal down
func WelsI4x4LumaPredHD_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	s2 := s << 1
	s3 := s + s2
	p, o := pPred, iPredOff
	kuiLT := gipPx(p, o, -(s + 1))
	/*get pLeft and pTop*/
	kuiL0 := gipPx(p, o, -1)
	kuiL1 := gipPx(p, o, -1+s)
	kuiL2 := gipPx(p, o, -1+s2)
	kuiL3 := gipPx(p, o, -1+s3)
	kuiT0 := gipPx(p, o, -s)
	kuiT1 := gipPx(p, o, -s+1)
	kuiT2 := gipPx(p, o, -s+2)
	kuiTL0 := 1 + kuiLT + kuiL0
	kuiLT0 := 1 + kuiLT + kuiT0
	kuiT01 := 1 + kuiT0 + kuiT1
	kuiT12 := 1 + kuiT1 + kuiT2
	kuiL01 := 1 + kuiL0 + kuiL1
	kuiL12 := 1 + kuiL1 + kuiL2
	kuiL23 := 1 + kuiL2 + kuiL3
	kuiHD0 := uint8(kuiTL0 >> 1)
	kuiHD1 := uint8((kuiTL0 + kuiLT0) >> 2)
	kuiHD2 := uint8((kuiLT0 + kuiT01) >> 2)
	kuiHD3 := uint8((kuiT01 + kuiT12) >> 2)
	kuiHD4 := uint8(kuiL01 >> 1)
	kuiHD5 := uint8((kuiTL0 + kuiL01) >> 2)
	kuiHD6 := uint8(kuiL12 >> 1)
	kuiHD7 := uint8((kuiL01 + kuiL12) >> 2)
	kuiHD8 := uint8(kuiL23 >> 1)
	kuiHD9 := uint8((kuiL12 + kuiL23) >> 2)
	kuiList := [10]uint8{kuiHD8, kuiHD9, kuiHD6, kuiHD7, kuiHD4, kuiHD5, kuiHD0, kuiHD1, kuiHD2, kuiHD3}

	gipStore4Rows(pPred, iPredOff, s, kuiList[:], [4]int{6, 4, 2, 0})
}

// gipStrides8 fills iStride[i] = i*kiStride.
func gipStrides8(kiStride int32) (iStride [8]int) {
	for i := 1; i < 8; i++ {
		iStride[i] = iStride[i-1] + int(kiStride)
	}
	return
}

// gipFilterT computes uiPixelFilterT[0..7] (8-89 style top filtering).
func gipFilterT(pPred []uint8, o int, s int, bTLAvail, bTRAvail bool) (t [8]uint8) {
	px := func(k int) int32 { return int32(pPred[o+k]) }
	if bTLAvail {
		t[0] = uint8((px(-1-s) + (px(-s) << 1) + px(1-s) + 2) >> 2)
	} else {
		t[0] = uint8((px(-s)*3 + px(1-s) + 2) >> 2)
	}
	for i := 1; i < 7; i++ {
		t[i] = uint8((px(i-1-s) + (px(i-s) << 1) + px(i+1-s) + 2) >> 2)
	}
	if bTRAvail {
		t[7] = uint8((px(6-s) + (px(7-s) << 1) + px(8-s) + 2) >> 2)
	} else {
		t[7] = uint8((px(6-s) + px(7-s)*3 + 2) >> 2)
	}
	return
}

// gipFilterL computes uiPixelFilterL[0..7] (left filtering).
func gipFilterL(pPred []uint8, o int, s int, iStride *[8]int, bTLAvail bool) (l [8]uint8) {
	px := func(k int) int32 { return int32(pPred[o+k]) }
	if bTLAvail {
		l[0] = uint8((px(-1-s) + (px(-1) << 1) + px(-1+iStride[1]) + 2) >> 2)
	} else {
		l[0] = uint8((px(-1)*3 + px(-1+iStride[1]) + 2) >> 2)
	}
	for i := 1; i < 7; i++ {
		l[i] = uint8((px(-1+iStride[i-1]) + (px(-1+iStride[i]) << 1) + px(-1+iStride[i+1]) + 2) >> 2)
	}
	l[7] = uint8((px(-1+iStride[6]) + px(-1+iStride[7])*3 + 2) >> 2)
	return
}

// gipFilterTL computes uiPixelFilterTL.
func gipFilterTL(pPred []uint8, o int, s int) uint8 {
	return uint8((int32(pPred[o-1]) + (int32(pPred[o-1-s]) << 1) + int32(pPred[o-s]) + 2) >> 2)
}

// WelsI8x8LumaPredV_c ports void WelsI8x8LumaPredV_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
func WelsI8x8LumaPredV_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	uiPixelFilterT := gipFilterT(pPred, iPredOff, s, bTLAvail, bTRAvail)

	// 8-89
	for i := 0; i < 8; i++ {
		copy(pPred[iPredOff+s*i:iPredOff+s*i+8], uiPixelFilterT[:])
	}
}

// WelsI8x8LumaPredH_c ports void WelsI8x8LumaPredH_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
func WelsI8x8LumaPredH_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	iStride := gipStrides8(kiStride)
	uiPixelFilterL := gipFilterL(pPred, iPredOff, s, &iStride, bTLAvail)

	// 8-90
	for i := 0; i < 8; i++ {
		gipFill8(pPred, iPredOff+iStride[i], uiPixelFilterL[i])
	}
}

// WelsI8x8LumaPredDc_c ports void WelsI8x8LumaPredDc_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
func WelsI8x8LumaPredDc_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	iStride := gipStrides8(kiStride)
	uiPixelFilterL := gipFilterL(pPred, iPredOff, s, &iStride, bTLAvail)
	uiPixelFilterT := gipFilterT(pPred, iPredOff, s, bTLAvail, bTRAvail)
	var uiTotal uint16

	// 8-91
	for i := 0; i < 8; i++ {
		uiTotal += uint16(uiPixelFilterL[i])
		uiTotal += uint16(uiPixelFilterT[i])
	}

	kuiMean := uint8((int32(uiTotal) + 8) >> 4)

	for i := 0; i < 8; i++ {
		gipFill8(pPred, iPredOff+iStride[i], kuiMean)
	}
}

// WelsI8x8LumaPredDcLeft_c ports void WelsI8x8LumaPredDcLeft_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
func WelsI8x8LumaPredDcLeft_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	iStride := gipStrides8(kiStride)
	uiPixelFilterL := gipFilterL(pPred, iPredOff, s, &iStride, bTLAvail)
	var uiTotal uint16

	// 8-92
	for i := 0; i < 8; i++ {
		uiTotal += uint16(uiPixelFilterL[i])
	}

	kuiMean := uint8((int32(uiTotal) + 4) >> 3)

	for i := 0; i < 8; i++ {
		gipFill8(pPred, iPredOff+iStride[i], kuiMean)
	}
}

// WelsI8x8LumaPredDcTop_c ports void WelsI8x8LumaPredDcTop_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
func WelsI8x8LumaPredDcTop_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	iStride := gipStrides8(kiStride)
	uiPixelFilterT := gipFilterT(pPred, iPredOff, s, bTLAvail, bTRAvail)
	var uiTotal uint16

	// 8-93
	for i := 0; i < 8; i++ {
		uiTotal += uint16(uiPixelFilterT[i])
	}

	kuiMean := uint8((int32(uiTotal) + 4) >> 3)

	for i := 0; i < 8; i++ {
		gipFill8(pPred, iPredOff+iStride[i], kuiMean)
	}
}

// WelsI8x8LumaPredDcNA_c ports void WelsI8x8LumaPredDcNA_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
func WelsI8x8LumaPredDcNA_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	// for normal 8 bit depth, 8-94
	iStride := gipStrides8(kiStride)
	for i := 0; i < 8; i++ {
		gipFill8(pPred, iPredOff+iStride[i], 0x80)
	}
}

// gipFilterT16 computes uiPixelFilterT[0..15] with top-right available.
func gipFilterT16(pPred []uint8, o int, s int, bTLAvail bool) (t [16]uint8) {
	px := func(k int) int32 { return int32(pPred[o+k]) }
	if bTLAvail {
		t[0] = uint8((px(-1-s) + (px(-s) << 1) + px(1-s) + 2) >> 2)
	} else {
		t[0] = uint8((px(-s)*3 + px(1-s) + 2) >> 2)
	}
	for i := 1; i < 15; i++ {
		t[i] = uint8((px(i-1-s) + (px(i-s) << 1) + px(i+1-s) + 2) >> 2)
	}
	t[15] = uint8((px(14-s) + px(15-s)*3 + 2) >> 2)
	return
}

// gipFilterT16Top computes uiPixelFilterT[0..15] with top-right unavailable
// (p[x, -1] x=8...15 are replaced with p[7, -1]).
func gipFilterT16Top(pPred []uint8, o int, s int, bTLAvail bool) (t [16]uint8) {
	px := func(k int) int32 { return int32(pPred[o+k]) }
	if bTLAvail {
		t[0] = uint8((px(-1-s) + (px(-s) << 1) + px(1-s) + 2) >> 2)
	} else {
		t[0] = uint8((px(-s)*3 + px(1-s) + 2) >> 2)
	}
	for i := 1; i < 7; i++ {
		t[i] = uint8((px(i-1-s) + (px(i-s) << 1) + px(i+1-s) + 2) >> 2)
	}
	t[7] = uint8((px(6-s) + px(7-s)*3 + 2) >> 2)
	for i := 8; i < 16; i++ {
		t[i] = pPred[o+7-s]
	}
	return
}

func gipDDL8x8(pPred []uint8, o int, iStride *[8]int, t *[16]uint8) {
	for i := 0; i < 8; i++ { // y
		for j := 0; j < 8; j++ { // x
			if i == 7 && j == 7 { // 8-95
				pPred[o+j+iStride[i]] = uint8((int32(t[14]) + 3*int32(t[15]) + 2) >> 2)
			} else { // 8-96
				pPred[o+j+iStride[i]] = uint8((int32(t[i+j]) + (int32(t[i+j+1]) << 1) + int32(t[i+j+2]) + 2) >> 2)
			}
		}
	}
}

// WelsI8x8LumaPredDDL_c ports void WelsI8x8LumaPredDDL_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// down pLeft; Top and Top-right available
func WelsI8x8LumaPredDDL_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	iStride := gipStrides8(kiStride)
	uiPixelFilterT := gipFilterT16(pPred, iPredOff, int(kiStride), bTLAvail)
	gipDDL8x8(pPred, iPredOff, &iStride, &uiPixelFilterT)
}

// WelsI8x8LumaPredDDLTop_c ports void WelsI8x8LumaPredDDLTop_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// down pLeft; Top available and Top-right unavailable
func WelsI8x8LumaPredDDLTop_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	iStride := gipStrides8(kiStride)
	uiPixelFilterT := gipFilterT16Top(pPred, iPredOff, int(kiStride), bTLAvail)
	gipDDL8x8(pPred, iPredOff, &iStride, &uiPixelFilterT)
}

// WelsI8x8LumaPredDDR_c ports void WelsI8x8LumaPredDDR_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// down right; The TopLeft, Top, Left are all available under this mode
func WelsI8x8LumaPredDDR_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	o := iPredOff
	iStride := gipStrides8(kiStride)
	tl := int32(gipFilterTL(pPred, o, s))
	l := gipFilterL(pPred, o, s, &iStride, true)
	t := gipFilterT(pPred, o, s, true, bTRAvail)
	L := func(k int) int32 { return int32(l[k]) }
	T := func(k int) int32 { return int32(t[k]) }

	for i := 0; i < 8; i++ { // y
		// 8-98, x < y-1
		for j := 0; j < (i - 1); j++ {
			pPred[o+j+iStride[i]] = uint8((L(i-j-2) + (L(i-j-1) << 1) + L(i-j) + 2) >> 2)
		}
		// 8-98, special case, x == y-1
		if i >= 1 {
			j := i - 1
			pPred[o+j+iStride[i]] = uint8((tl + (L(0) << 1) + L(1) + 2) >> 2)
		}
		// 8-99, x==y
		j := i
		pPred[o+j+iStride[i]] = uint8((T(0) + (tl << 1) + L(0) + 2) >> 2)
		// 8-97, special case, x == y+1
		if i < 7 {
			j = i + 1
			pPred[o+j+iStride[i]] = uint8((tl + (T(0) << 1) + T(1) + 2) >> 2)
		}
		for j = i + 2; j < 8; j++ { // 8-97, x > y+1
			pPred[o+j+iStride[i]] = uint8((T(j-i-2) + (T(j-i-1) << 1) + T(j-i) + 2) >> 2)
		}
	}
}

func gipVL8x8(pPred []uint8, o int, iStride *[8]int, t *[16]uint8) {
	for i := 0; i < 8; i++ { // y
		if (i & 0x01) == 0 { // 8-108
			for j := 0; j < 8; j++ { // x
				pPred[o+j+iStride[i]] = uint8((int32(t[j+(i>>1)]) + int32(t[j+(i>>1)+1]) + 1) >> 1)
			}
		} else { // 8-109
			for j := 0; j < 8; j++ { // x
				pPred[o+j+iStride[i]] = uint8((int32(t[j+(i>>1)]) + (int32(t[j+(i>>1)+1]) << 1) + int32(t[j+(i>>1)+2]) + 2) >> 2)
			}
		}
	}
}

// WelsI8x8LumaPredVL_c ports void WelsI8x8LumaPredVL_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// vertical pLeft; Top and Top-right available
func WelsI8x8LumaPredVL_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	iStride := gipStrides8(kiStride)
	uiPixelFilterT := gipFilterT16(pPred, iPredOff, int(kiStride), bTLAvail)
	gipVL8x8(pPred, iPredOff, &iStride, &uiPixelFilterT)
}

// WelsI8x8LumaPredVLTop_c ports void WelsI8x8LumaPredVLTop_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// vertical pLeft; Top available and Top-right unavailable
func WelsI8x8LumaPredVLTop_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	iStride := gipStrides8(kiStride)
	uiPixelFilterT := gipFilterT16Top(pPred, iPredOff, int(kiStride), bTLAvail)
	gipVL8x8(pPred, iPredOff, &iStride, &uiPixelFilterT)
}

// WelsI8x8LumaPredVR_c ports void WelsI8x8LumaPredVR_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// vertical right; The TopLeft, Top, Left are always available under this mode
func WelsI8x8LumaPredVR_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	o := iPredOff
	iStride := gipStrides8(kiStride)
	tl := int32(gipFilterTL(pPred, o, s))
	l := gipFilterL(pPred, o, s, &iStride, true)
	t := gipFilterT(pPred, o, s, true, bTRAvail)
	L := func(k int) int32 { return int32(l[k]) }
	T := func(k int) int32 { return int32(t[k]) }

	for i := 0; i < 8; i++ { // y
		for j := 0; j < 8; j++ { // x
			izVR := (j << 1) - i // 2 * x - y
			izVRDiv := j - (i >> 1)
			var v int32
			if izVR >= 0 {
				if (izVR & 0x01) == 0 { // 8-100
					if izVRDiv > 0 {
						v = (T(izVRDiv-1) + T(izVRDiv) + 1) >> 1
					} else {
						v = (tl + T(0) + 1) >> 1
					}
				} else { // 8-101
					if izVRDiv > 1 {
						v = (T(izVRDiv-2) + (T(izVRDiv-1) << 1) + T(izVRDiv) + 2) >> 2
					} else {
						v = (tl + (T(0) << 1) + T(1) + 2) >> 2
					}
				}
			} else if izVR == -1 { // 8-102
				v = (L(0) + (tl << 1) + T(0) + 2) >> 2
			} else if izVR < -2 { // 8-103
				v = (L(-izVR-1) + (L(-izVR-2) << 1) + L(-izVR-3) + 2) >> 2
			} else { // izVR==-2, 8-103, special case
				v = (L(1) + (L(0) << 1) + tl + 2) >> 2
			}
			pPred[o+j+iStride[i]] = uint8(v)
		}
	}
}

// WelsI8x8LumaPredHU_c ports void WelsI8x8LumaPredHU_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// horizontal up
func WelsI8x8LumaPredHU_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	o := iPredOff
	iStride := gipStrides8(kiStride)
	l := gipFilterL(pPred, o, s, &iStride, bTLAvail)
	L := func(k int) int32 { return int32(l[k]) }

	for i := 0; i < 8; i++ { // y
		for j := 0; j < 8; j++ { // x
			izHU := j + (i << 1) // x + 2 * y
			var v int32
			if izHU < 13 {
				if (izHU & 0x01) == 0 { // 8-110
					v = (L(izHU>>1) + L(1+(izHU>>1)) + 1) >> 1
				} else { // 8-111
					v = (L(izHU>>1) + (L(1+(izHU>>1)) << 1) + L(2+(izHU>>1)) + 2) >> 2
				}
			} else if izHU == 13 { // 8-112
				v = (L(6) + 3*L(7) + 2) >> 2
			} else { // 8-113
				v = L(7)
			}
			pPred[o+j+iStride[i]] = uint8(v)
		}
	}
}

// WelsI8x8LumaPredHD_c ports void WelsI8x8LumaPredHD_c (uint8_t* pPred, const int32_t kiStride, bool bTLAvail, bool bTRAvail).
// horizontal down; The TopLeft, Top, Left are all available under this mode
func WelsI8x8LumaPredHD_c(pPred []uint8, iPredOff int, kiStride int32, bTLAvail bool, bTRAvail bool) {
	s := int(kiStride)
	o := iPredOff
	iStride := gipStrides8(kiStride)
	tl := int32(gipFilterTL(pPred, o, s))
	l := gipFilterL(pPred, o, s, &iStride, true)
	t := gipFilterT(pPred, o, s, true, bTRAvail)
	L := func(k int) int32 { return int32(l[k]) }
	T := func(k int) int32 { return int32(t[k]) }

	for i := 0; i < 8; i++ { // y
		for j := 0; j < 8; j++ { // x
			izHD := (i << 1) - j // 2*y - x
			izHDDiv := i - (j >> 1)
			var v int32
			if izHD >= 0 {
				if (izHD & 0x01) == 0 { // 8-104
					if izHDDiv == 0 {
						v = (tl + L(0) + 1) >> 1
					} else {
						v = (L(izHDDiv-1) + L(izHDDiv) + 1) >> 1
					}
				} else { // 8-105
					if izHDDiv == 1 {
						v = (tl + (L(0) << 1) + L(1) + 2) >> 2
					} else {
						v = (L(izHDDiv-2) + (L(izHDDiv-1) << 1) + L(izHDDiv) + 2) >> 2
					}
				}
			} else if izHD == -1 { // 8-106
				v = (L(0) + (tl << 1) + T(0) + 2) >> 2
			} else if izHD < -2 { // 8-107
				v = (T(-izHD-1) + (T(-izHD-2) << 1) + T(-izHD-3) + 2) >> 2
			} else { // 8-107 special case, izHD==-2
				v = (T(1) + (T(0) << 1) + tl + 2) >> 2
			}
			pPred[o+j+iStride[i]] = uint8(v)
		}
	}
}

// WelsIChromaPredV_c ports void WelsIChromaPredV_c (uint8_t* pPred, const int32_t kiStride).
func WelsIChromaPredV_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	src := pPred[iPredOff-s : iPredOff-s+8]
	for r := 0; r < 8; r++ {
		copy(pPred[iPredOff+r*s:iPredOff+r*s+8], src)
	}
}

// WelsIChromaPredH_c ports void WelsIChromaPredH_c (uint8_t* pPred, const int32_t kiStride).
func WelsIChromaPredH_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 3) - s
	for i := 0; i < 8; i++ {
		kuiVal8 := pPred[iPredOff+iTmp-1]
		gipFill8(pPred, iPredOff+iTmp, kuiVal8)
		iTmp -= s
	}
}

// WelsIChromaPredPlane_c ports void WelsIChromaPredPlane_c (uint8_t* pPred, const int32_t kiStride).
func WelsIChromaPredPlane_c(pPred []uint8, iPredOff int, kiStride int32) {
	var a, b, c, H, V int32
	s := int(kiStride)
	pTop := iPredOff - s
	pLeft := iPredOff - 1

	for i := 0; i < 4; i++ {
		H += int32(i+1) * (int32(pPred[pTop+4+i]) - int32(pPred[pTop+2-i]))
		V += int32(i+1) * (int32(pPred[pLeft+(4+i)*s]) - int32(pPred[pLeft+(2-i)*s]))
	}

	a = (int32(pPred[pLeft+7*s]) + int32(pPred[pTop+7])) << 4
	b = (17*H + 16) >> 5
	c = (17*V + 16) >> 5

	o := iPredOff
	for i := int32(0); i < 8; i++ {
		for j := int32(0); j < 8; j++ {
			iTmp := (a + b*(j-3) + c*(i-3) + 16) >> 5
			pPred[o+int(j)] = common.WelsClip1(iTmp)
		}
		o += s
	}
}

// WelsIChromaPredDc_c ports void WelsIChromaPredDc_c (uint8_t* pPred, const int32_t kiStride).
func WelsIChromaPredDc_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	p, o := pPred, iPredOff
	kiL1 := s - 1
	kiL2 := kiL1 + s
	kiL3 := kiL2 + s
	kiL4 := kiL3 + s
	kiL5 := kiL4 + s
	kiL6 := kiL5 + s
	kiL7 := kiL6 + s
	/*caculate the kMean value*/
	kuiM1 := uint8((gipPx(p, o, -s) + gipPx(p, o, 1-s) + gipPx(p, o, 2-s) + gipPx(p, o, 3-s) +
		gipPx(p, o, -1) + gipPx(p, o, kiL1) + gipPx(p, o, kiL2) + gipPx(p, o, kiL3) + 4) >> 3)
	kuiSum2 := uint32(gipPx(p, o, 4-s) + gipPx(p, o, 5-s) + gipPx(p, o, 6-s) + gipPx(p, o, 7-s))
	kuiSum3 := uint32(gipPx(p, o, kiL4) + gipPx(p, o, kiL5) + gipPx(p, o, kiL6) + gipPx(p, o, kiL7))
	kuiM2 := uint8((kuiSum2 + 2) >> 2)
	kuiM3 := uint8((kuiSum3 + 2) >> 2)
	kuiM4 := uint8((kuiSum2 + kuiSum3 + 4) >> 3)
	kuiMUP := [8]uint8{kuiM1, kuiM1, kuiM1, kuiM1, kuiM2, kuiM2, kuiM2, kuiM2}
	kuiMDown := [8]uint8{kuiM3, kuiM3, kuiM3, kuiM3, kuiM4, kuiM4, kuiM4, kuiM4}

	copy(p[o:o+8], kuiMUP[:])
	copy(p[o+kiL1+1:o+kiL1+9], kuiMUP[:])
	copy(p[o+kiL2+1:o+kiL2+9], kuiMUP[:])
	copy(p[o+kiL3+1:o+kiL3+9], kuiMUP[:])
	copy(p[o+kiL4+1:o+kiL4+9], kuiMDown[:])
	copy(p[o+kiL5+1:o+kiL5+9], kuiMDown[:])
	copy(p[o+kiL6+1:o+kiL6+9], kuiMDown[:])
	copy(p[o+kiL7+1:o+kiL7+9], kuiMDown[:])
}

// WelsIChromaPredDcLeft_c ports void WelsIChromaPredDcLeft_c (uint8_t* pPred, const int32_t kiStride).
func WelsIChromaPredDcLeft_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	p, o := pPred, iPredOff
	kiL1 := -1 + s
	kiL2 := kiL1 + s
	kiL3 := kiL2 + s
	kiL4 := kiL3 + s
	kiL5 := kiL4 + s
	kiL6 := kiL5 + s
	kiL7 := kiL6 + s
	/*caculate the kMean value*/
	kuiMUP := uint8((gipPx(p, o, -1) + gipPx(p, o, kiL1) + gipPx(p, o, kiL2) + gipPx(p, o, kiL3) + 2) >> 2)
	kuiMDown := uint8((gipPx(p, o, kiL4) + gipPx(p, o, kiL5) + gipPx(p, o, kiL6) + gipPx(p, o, kiL7) + 2) >> 2)

	gipFill8(p, o, kuiMUP)
	gipFill8(p, o+kiL1+1, kuiMUP)
	gipFill8(p, o+kiL2+1, kuiMUP)
	gipFill8(p, o+kiL3+1, kuiMUP)
	gipFill8(p, o+kiL4+1, kuiMDown)
	gipFill8(p, o+kiL5+1, kuiMDown)
	gipFill8(p, o+kiL6+1, kuiMDown)
	gipFill8(p, o+kiL7+1, kuiMDown)
}

// WelsIChromaPredDcTop_c ports void WelsIChromaPredDcTop_c (uint8_t* pPred, const int32_t kiStride).
func WelsIChromaPredDcTop_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	p, o := pPred, iPredOff
	iTmp := (s << 3) - s
	/*caculate the kMean value*/
	kuiM1 := uint8((gipPx(p, o, -s) + gipPx(p, o, 1-s) + gipPx(p, o, 2-s) + gipPx(p, o, 3-s) + 2) >> 2)
	kuiM2 := uint8((gipPx(p, o, 4-s) + gipPx(p, o, 5-s) + gipPx(p, o, 6-s) + gipPx(p, o, 7-s) + 2) >> 2)
	kuiM := [8]uint8{kuiM1, kuiM1, kuiM1, kuiM1, kuiM2, kuiM2, kuiM2, kuiM2}

	for i := 0; i < 8; i++ {
		copy(p[o+iTmp:o+iTmp+8], kuiM[:])
		iTmp -= s
	}
}

// WelsIChromaPredDcNA_c ports void WelsIChromaPredDcNA_c (uint8_t* pPred, const int32_t kiStride).
func WelsIChromaPredDcNA_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 3) - s
	for i := 0; i < 8; i++ {
		gipFill8(pPred, iPredOff+iTmp, 0x80)
		iTmp -= s
	}
}

// WelsI16x16LumaPredV_c ports void WelsI16x16LumaPredV_c (uint8_t* pPred, const int32_t kiStride).
func WelsI16x16LumaPredV_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 4) - s
	src := pPred[iPredOff-s : iPredOff-s+16]
	for i := 0; i < 16; i++ {
		copy(pPred[iPredOff+iTmp:iPredOff+iTmp+16], src)
		iTmp -= s
	}
}

// WelsI16x16LumaPredH_c ports void WelsI16x16LumaPredH_c (uint8_t* pPred, const int32_t kiStride).
func WelsI16x16LumaPredH_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 4) - s
	for i := 0; i < 16; i++ {
		kuiVal8 := pPred[iPredOff+iTmp-1]
		gipFill8(pPred, iPredOff+iTmp, kuiVal8)
		gipFill8(pPred, iPredOff+iTmp+8, kuiVal8)
		iTmp -= s
	}
}

// WelsI16x16LumaPredPlane_c ports void WelsI16x16LumaPredPlane_c (uint8_t* pPred, const int32_t kiStride).
func WelsI16x16LumaPredPlane_c(pPred []uint8, iPredOff int, kiStride int32) {
	var a, b, c, H, V int32
	s := int(kiStride)
	pTop := iPredOff - s
	pLeft := iPredOff - 1

	for i := 0; i < 8; i++ {
		H += int32(i+1) * (int32(pPred[pTop+8+i]) - int32(pPred[pTop+6-i]))
		V += int32(i+1) * (int32(pPred[pLeft+(8+i)*s]) - int32(pPred[pLeft+(6-i)*s]))
	}

	a = (int32(pPred[pLeft+15*s]) + int32(pPred[pTop+15])) << 4
	b = (5*H + 32) >> 6
	c = (5*V + 32) >> 6

	o := iPredOff
	for i := int32(0); i < 16; i++ {
		for j := int32(0); j < 16; j++ {
			iTmp := (a + b*(j-7) + c*(i-7) + 16) >> 5
			pPred[o+int(j)] = common.WelsClip1(iTmp)
		}
		o += s
	}
}

// WelsI16x16LumaPredDc_c ports void WelsI16x16LumaPredDc_c (uint8_t* pPred, const int32_t kiStride).
func WelsI16x16LumaPredDc_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 4) - s
	var iSum int32

	/*caculate the kMean value*/
	for i := 15; i >= 0; i-- {
		iSum += int32(pPred[iPredOff-1+iTmp]) + int32(pPred[iPredOff-s+i])
		iTmp -= s
	}
	uiMean := uint8((16 + iSum) >> 5)

	iTmp = (s << 4) - s
	for i := 0; i < 16; i++ {
		gipFill8(pPred, iPredOff+iTmp, uiMean)
		gipFill8(pPred, iPredOff+iTmp+8, uiMean)
		iTmp -= s
	}
}

// WelsI16x16LumaPredDcTop_c ports void WelsI16x16LumaPredDcTop_c (uint8_t* pPred, const int32_t kiStride).
func WelsI16x16LumaPredDcTop_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 4) - s
	var iSum int32

	/*caculate the kMean value*/
	for i := 15; i >= 0; i-- {
		iSum += int32(pPred[iPredOff-s+i])
	}
	uiMean := uint8((8 + iSum) >> 4)

	for i := 0; i < 16; i++ {
		gipFill8(pPred, iPredOff+iTmp, uiMean)
		gipFill8(pPred, iPredOff+iTmp+8, uiMean)
		iTmp -= s
	}
}

// WelsI16x16LumaPredDcLeft_c ports void WelsI16x16LumaPredDcLeft_c (uint8_t* pPred, const int32_t kiStride).
func WelsI16x16LumaPredDcLeft_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 4) - s
	var iSum int32

	/*caculate the kMean value*/
	for i := 0; i < 16; i++ {
		iSum += int32(pPred[iPredOff-1+iTmp])
		iTmp -= s
	}
	uiMean := uint8((8 + iSum) >> 4)

	iTmp = (s << 4) - s
	for i := 0; i < 16; i++ {
		gipFill8(pPred, iPredOff+iTmp, uiMean)
		gipFill8(pPred, iPredOff+iTmp+8, uiMean)
		iTmp -= s
	}
}

// WelsI16x16LumaPredDcNA_c ports void WelsI16x16LumaPredDcNA_c (uint8_t* pPred, const int32_t kiStride).
func WelsI16x16LumaPredDcNA_c(pPred []uint8, iPredOff int, kiStride int32) {
	s := int(kiStride)
	iTmp := (s << 4) - s
	for i := 0; i < 16; i++ {
		gipFill8(pPred, iPredOff+iTmp, 0x80)
		gipFill8(pPred, iPredOff+iTmp+8, 0x80)
		iTmp -= s
	}
}
