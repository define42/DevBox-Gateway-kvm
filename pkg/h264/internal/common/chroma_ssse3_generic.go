//go:build !amd64

package common

func initMcChromaSSSE3(_ *SMcFunc, _ uint32) {}

func McChroma_ssse3(src []byte, srcOff int, srcStride int32, dst []byte, dstOff int, dstStride int32,
	mvX, mvY int16, width, height int32) {
	McChroma_sse2(src, srcOff, srcStride, dst, dstOff, dstStride, mvX, mvY, width, height)
}
