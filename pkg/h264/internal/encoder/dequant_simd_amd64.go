//go:build amd64

package encoder

import "unsafe"

//go:noescape
func dequant4x4SSE2(dct []int16, mf []uint16)

//go:noescape
func dequantFour4x4SSE2(dct []int16, mf []uint16)

func dequantOverlaps(dct []int16, mf []uint16, count uintptr) bool {
	x := uintptr(unsafe.Pointer(&dct[0]))
	y := uintptr(unsafe.Pointer(&mf[0]))
	return x < y+16 && y < x+count*2
}

func WelsDequant4x4_sse2(dct []int16, mf []uint16) {
	_ = dct[15]
	_ = mf[7]
	if dequantOverlaps(dct, mf, 16) {
		WelsDequant4x4_c(dct, mf)
		return
	}
	dequant4x4SSE2(dct, mf)
}

func WelsDequantFour4x4_sse2(dct []int16, mf []uint16) {
	_ = dct[63]
	_ = mf[7]
	if dequantOverlaps(dct, mf, 64) {
		WelsDequantFour4x4_c(dct, mf)
		return
	}
	dequantFour4x4SSE2(dct, mf)
}
