//go:build amd64

package common

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
