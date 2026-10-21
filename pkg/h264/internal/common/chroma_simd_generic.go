//go:build !amd64

package common

func McChroma_sse2(src []byte, srcOff int, srcStride int32, dst []byte, dstOff int, dstStride int32, mvX, mvY int16, width, height int32) {
	McChroma_c(src, srcOff, srcStride, dst, dstOff, dstStride, mvX, mvY, width, height)
}
