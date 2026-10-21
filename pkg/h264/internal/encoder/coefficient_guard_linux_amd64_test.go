//go:build linux && amd64

package encoder

import (
	"testing"
	"unsafe"
)

func TestCoefficientSIMDGuardPages(t *testing.T) {
	words := func(count int) []int16 {
		buf := idctGuardedBuffer(t, count*2)
		return unsafe.Slice((*int16)(unsafe.Pointer(&buf[0])), count)
	}
	input, output := words(16), words(16)
	for i := range input {
		input[i] = int16(i*733 - 20000)
	}
	coefficientNonzeroMask16(input)
	coefficientScanMask(output, input, false)
	coefficientScanMask(output, input, true)
	mfBytes := idctGuardedBuffer(t, 16)
	mf := unsafe.Slice((*uint16)(unsafe.Pointer(&mfBytes[0])), 8)
	for i := range mf {
		mf[i] = uint16(65535 - i*199)
	}
	WelsDequant4x4_sse2(words(16), mf)
	WelsDequantFour4x4_sse2(words(64), mf)
}
