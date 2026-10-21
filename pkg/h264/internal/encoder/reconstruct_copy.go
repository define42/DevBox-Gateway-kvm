package encoder

// reconstructedBlockSpan checks the entire positive-stride block before any
// assembly reads or writes. Use wide arithmetic so offsets and strides cannot
// wrap into an apparently valid span, including on 32-bit fallback builds.
func reconstructedBlockSpan(buf []byte, offset int, stride int32, size int) []byte {
	span := int64(size-1)*int64(stride) + int64(size)
	if offset < 0 || offset > len(buf) || stride < 0 || span > int64(len(buf)-offset) {
		panic("h264: reconstruction copy span out of range")
	}
	return buf[offset : offset+int(span)]
}
