//go:build !amd64

package common

func WelsCopy8x16_sse2(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	WelsCopy8x16_c(dst, dstOff, dstStride, src, srcOff, srcStride)
}

func WelsCopy16x16_sse2(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	WelsCopy16x16_c(dst, dstOff, dstStride, src, srcOff, srcStride)
}
