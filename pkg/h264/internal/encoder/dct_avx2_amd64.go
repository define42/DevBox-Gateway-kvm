//go:build amd64

package encoder

import "unsafe"

// welsDctFourT4AVX2 transforms four 4x4 blocks with one packed AVX2 pass.
// Callers must validate the complete 8x8 input spans and exclude output aliases.
//
//go:noescape
func welsDctFourT4AVX2(dct *[64]int16, pixel1 *byte, stride1 int, pixel2 *byte, stride2 int)

// The previous SSE2 implementation stores the top two blocks before reading
// the bottom two. Preserve that access order when differently typed buffers
// alias. Pointer differences also reject a span that contains the other start.
// The pointers stay tracked across any stack growth; uintptr values are used
// only in the comparisons, never saved and later converted back into pointers.
func dctFourT4Aliases(dct *[64]int16, pixel1 *byte, stride1 int, pixel2 *byte, stride2 int) bool {
	return uintptr(unsafe.Pointer(dct))-uintptr(unsafe.Pointer(pixel1)) < uintptr(7*stride1+8) ||
		uintptr(unsafe.Pointer(pixel1))-uintptr(unsafe.Pointer(dct)) < 128 ||
		uintptr(unsafe.Pointer(dct))-uintptr(unsafe.Pointer(pixel2)) < uintptr(7*stride2+8) ||
		uintptr(unsafe.Pointer(pixel2))-uintptr(unsafe.Pointer(dct)) < 128
}
