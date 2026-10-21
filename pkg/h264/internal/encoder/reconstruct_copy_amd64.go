//go:build amd64

package encoder

// All supported amd64 CPUs provide SSE2. The slice arguments keep both picture
// buffers live; the wrappers validate the complete blocks before entering asm.
//
//go:noescape
func copyReconstructed16x16SSE2(dst []byte, dstStride int, src []byte, srcStride int)

//go:noescape
func copyReconstructed8x8SSE2(dst []byte, dstStride int, src []byte, srcStride int)

func copyReconstructed16x16(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	dstBlock := reconstructedBlockSpan(dst, dstOff, dstStride, 16)
	srcBlock := reconstructedBlockSpan(src, srcOff, srcStride, 16)
	copyReconstructed16x16SSE2(dstBlock, int(dstStride), srcBlock, int(srcStride))
}

func copyReconstructed8x8(dst []byte, dstOff int, dstStride int32, src []byte, srcOff int, srcStride int32) {
	dstBlock := reconstructedBlockSpan(dst, dstOff, dstStride, 8)
	srcBlock := reconstructedBlockSpan(src, srcOff, srcStride, 8)
	copyReconstructed8x8SSE2(dstBlock, int(dstStride), srcBlock, int(srcStride))
}
