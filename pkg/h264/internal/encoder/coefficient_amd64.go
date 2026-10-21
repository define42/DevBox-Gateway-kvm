//go:build amd64

package encoder

import (
	"unsafe"

	"golang.org/x/sys/cpu"
)

//go:noescape
func coefficientMaskSSE2(levels []int16) uint32

//go:noescape
func coefficientScanMaskSSSE3(levels, dct []int16, ac int32) uint32

// coefficientNonzeroMask16 returns one bit per coefficient in the supplied
// order. Its SSE2 loads cover exactly 16 coefficients, including unaligned data.
func coefficientNonzeroMask16(levels []int16) uint16 {
	_ = levels[15]
	return uint16(coefficientMaskSSE2(levels))
}

func coefficientScanMask(levels, dct []int16, ac bool) uint16 {
	_ = levels[15]
	_ = dct[15]
	// Existing scalar scans have sequential alias semantics. Keep them when
	// buffers overlap; normal encoding uses separate DCT and level storage.
	out := uintptr(unsafe.Pointer(&levels[0]))
	in := uintptr(unsafe.Pointer(&dct[0]))
	if !cpu.X86.HasSSSE3 || (out < in+32 && in < out+32) {
		return coefficientScanMaskScalar(levels, dct, ac)
	}
	acFlag := int32(0)
	if ac {
		acFlag = 1
	}
	return uint16(coefficientScanMaskSSSE3(levels, dct, acFlag))
}
