//go:build amd64

package common

import "unsafe"

const simdBoundsPanic = "h264: SIMD buffer span out of range"

func checkSIMDSpan(buf []byte, first, last int64) {
	if first < 0 || last < first || last >= int64(len(buf)) {
		panic(simdBoundsPanic)
	}
}

// simdBlockSlice validates and returns the complete positive-stride block span
// passed to an assembly kernel. Codec picture and scratch strides are positive.
func simdBlockSlice(buf []byte, off int, stride int32, width, height int) []byte {
	if stride < 0 {
		panic(simdBoundsPanic)
	}
	end := off + (height-1)*int(stride) + width
	checkSIMDSpan(buf, int64(off), int64(end)-1)
	return buf[off:end]
}

// SIMDBlockPtr validates the complete positive-stride block span passed to an
// assembly kernel and returns its first byte. One combined comparison replaces
// a span check, a slice expression and an index check, and the function
// inlines into the checked wrappers.
func SIMDBlockPtr(buf []byte, off int, stride int32, width, height int) *byte {
	end := off + (height-1)*int(stride) + width
	if stride < 0 || off < 0 || end <= off || end > len(buf) {
		panic(simdBoundsPanic)
	}
	return (*byte)(unsafe.Add(unsafe.Pointer(unsafe.SliceData(buf)), off))
}
