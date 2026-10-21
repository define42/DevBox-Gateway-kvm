//go:build !amd64

package encoder

func WelsDequant4x4_sse2(dct []int16, mf []uint16) {
	WelsDequant4x4_c(dct, mf)
}

func WelsDequantFour4x4_sse2(dct []int16, mf []uint16) {
	WelsDequantFour4x4_c(dct, mf)
}
