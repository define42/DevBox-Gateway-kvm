//go:build !amd64

package encoder

func copyReconstructed16x16(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	dst = reconstructedBlockSpan(dst, dstOff, dstStride, 16)
	src = reconstructedBlockSpan(src, srcOff, srcStride, 16)
	for row := range 16 {
		d, s := row*int(dstStride), row*int(srcStride)
		copy(dst[d:d+16], src[s:s+16])
	}
}

func copyReconstructed8x8(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	dst = reconstructedBlockSpan(dst, dstOff, dstStride, 8)
	src = reconstructedBlockSpan(src, srcOff, srcStride, 8)
	for row := range 8 {
		d, s := row*int(dstStride), row*int(srcStride)
		copy(dst[d:d+8], src[s:s+8])
	}
}
