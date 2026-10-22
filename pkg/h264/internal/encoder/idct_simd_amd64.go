//go:build amd64

package encoder

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func idctT4RecSSE2(rec []byte, recStride int, pred []byte, predStride int, coeff []int16)

//go:noescape
func idctFourT4RecSSE2(rec []byte, recStride int, pred []byte, predStride int, coeff []int16)

//go:noescape
func idctFourT4RecAVX2(rec []byte, recStride int, pred []byte, predStride int, coeff []int16)

// WelsIDctT4Rec_sse2 preserves the scalar transform's wrapping horizontal
// int16 intermediates and its widened vertical arithmetic.
func WelsIDctT4Rec_sse2(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, coeff []int16) {
	_ = coeff[15]
	checkSampleSIMDBlock(rec, recOff, recStride, 4, 4)
	checkSampleSIMDBlock(pred, predOff, predStride, 4, 4)
	if idctNeedsScalarAlias(rec, recOff, recStride, pred, predOff, predStride, 4) {
		WelsIDctT4Rec_c(rec, recOff, recStride, pred, predOff, predStride, coeff)
		return
	}
	idctT4RecSSE2(rec[recOff:], int(recStride), pred[predOff:], int(predStride), coeff)
}

// WelsIDctFourT4Rec_sse2 reconstructs an 8x8 block, with coefficients ordered
// top-left, top-right, bottom-left, bottom-right.
func WelsIDctFourT4Rec_sse2(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, coeff []int16) {
	_ = coeff[63]
	checkSampleSIMDBlock(rec, recOff, recStride, 8, 8)
	checkSampleSIMDBlock(pred, predOff, predStride, 8, 8)
	if idctNeedsScalarAlias(rec, recOff, recStride, pred, predOff, predStride, 8) {
		WelsIDctFourT4Rec_c(rec, recOff, recStride, pred, predOff, predStride, coeff)
		return
	}
	if cpu.X86.HasAVX2 {
		idctFourT4RecAVX2(rec[recOff:], int(recStride), pred[predOff:], int(predStride), coeff)
		return
	}
	idctFourT4RecSSE2(rec[recOff:], int(recStride), pred[predOff:], int(predStride), coeff)
}

//go:noescape
func idctI16x16DcRecSSE2(rec []byte, recStride int, pred []byte, predStride int, coeff []int16)

func WelsIDctRecI16x16Dc_sse2(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, coeff []int16) {
	_ = coeff[15]
	checkSampleSIMDBlock(rec, recOff, recStride, 16, 16)
	checkSampleSIMDBlock(pred, predOff, predStride, 16, 16)
	if idctNeedsScalarAlias(rec, recOff, recStride, pred, predOff, predStride, 16) {
		WelsIDctRecI16x16Dc_c(rec, recOff, recStride, pred, predOff, predStride, coeff)
		return
	}
	idctI16x16DcRecSSE2(rec[recOff:], int(recStride), pred[predOff:], int(predStride), coeff)
}

// Preserve scalar access order for overlapping rows or shifted prediction
// aliases. The hot reconstruction case uses the identical block in place.
func idctNeedsScalarAlias(rec []byte, recOff int, recStride int32, pred []byte, predOff int, predStride int32, size int) bool {
	if recStride < int32(size) || predStride < int32(size) {
		return true
	}
	r := uintptr(unsafe.Pointer(&rec[recOff]))
	p := uintptr(unsafe.Pointer(&pred[predOff]))
	if r == p && recStride == predStride {
		return false
	}
	rEnd := r + uintptr((size-1)*int(recStride)+size)
	pEnd := p + uintptr((size-1)*int(predStride)+size)
	return r < pEnd && p < rEnd
}
