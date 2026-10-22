//go:build amd64

package encoder

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func welsDctLumaMbAVX2(dct *[256]int16, enc *byte, encStride int, pred *[256]byte)

// dctLumaMbFast transforms a 16x16 luma residual with one validated call. It
// reports false, without writing, unless AVX2 is present, the source has a
// non-negative stride, both spans are in range and the output overlaps
// neither input; WelsDctMb then uses its per-quadrant calls.
func dctLumaMbFast(pRes []int16, pEncMb []uint8, iEncMbOff int, iEncStride int32, pBestPred []uint8, iBestPredOff int) bool {
	if !cpu.X86.HasAVX2 || len(pRes) < 256 || iEncStride < 0 || iEncMbOff < 0 || iBestPredOff < 0 ||
		iEncMbOff+15*int(iEncStride)+16 > len(pEncMb) || iBestPredOff+256 > len(pBestPred) {
		return false
	}
	dct := (*[256]int16)(pRes)
	enc := &pEncMb[iEncMbOff]
	pred := (*[256]byte)(pBestPred[iBestPredOff:])
	if spansOverlap(unsafe.Pointer(dct), 512, unsafe.Pointer(enc), uintptr(15*int(iEncStride)+16)) ||
		spansOverlap(unsafe.Pointer(dct), 512, unsafe.Pointer(pred), 256) {
		return false
	}
	welsDctLumaMbAVX2(dct, enc, int(iEncStride), pred)
	return true
}

// spansOverlap reports whether [a, a+aBytes) and [b, b+bBytes) intersect.
// The pointers are only compared, never converted back.
func spansOverlap(a unsafe.Pointer, aBytes uintptr, b unsafe.Pointer, bBytes uintptr) bool {
	return uintptr(a)-uintptr(b) < bBytes || uintptr(b)-uintptr(a) < aBytes
}
