//go:build amd64

package encoder

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func quantScanFour4x4SSSE3(dct, levels *[64]int16, ff, mf *[8]int16, maxima *[4]int16, masks *[4]uint16, ac int32)

// Quantize each group once, then scan and extract masks while its coefficients
// are still in SIMD registers. Maxima retain the existing signed-word semantics.
func quantScanFour4x4(dct, levels, ff, mf, maxima []int16, masks []uint16, ac bool) {
	_ = dct[63]
	_ = levels[63]
	_ = ff[7]
	_ = mf[7]
	_ = maxima[3]
	_ = masks[3]
	if !cpu.X86.HasSSSE3 || quantScanOutputsOverlap(unsafe.Pointer(&dct[0]), unsafe.Pointer(&levels[0]), unsafe.Pointer(&maxima[0]), unsafe.Pointer(&masks[0])) {
		quantScanFour4x4Separate(dct, levels, ff, mf, maxima, masks, ac)
		return
	}
	acFlag := int32(0)
	if ac {
		acFlag = 1
	}
	quantScanFour4x4SSSE3((*[64]int16)(dct), (*[64]int16)(levels), (*[8]int16)(ff), (*[8]int16)(mf), (*[4]int16)(maxima), (*[4]uint16)(masks), acFlag)
}

func quantScanOutputsOverlap(d, l, x, m unsafe.Pointer) bool {
	return quantScanRangesOverlap(d, 128, l, 128) ||
		quantScanRangesOverlap(d, 128, x, 8) || quantScanRangesOverlap(d, 128, m, 8) ||
		quantScanRangesOverlap(l, 128, x, 8) || quantScanRangesOverlap(l, 128, m, 8) ||
		quantScanRangesOverlap(x, 8, m, 8)
}

// Keep addresses as tracked pointers across calls, even if this helper stops
// inlining. No uintptr address survives a call that could move a Go stack.
func quantScanRangesOverlap(a unsafe.Pointer, aBytes uintptr, b unsafe.Pointer, bBytes uintptr) bool {
	return uintptr(a) < uintptr(b)+bBytes && uintptr(b) < uintptr(a)+aBytes
}
