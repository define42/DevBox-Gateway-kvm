package virtio

import "encoding/binary"

// bgrToRGBARowGeneric swaps B and R in complete four-byte pixels. The caller
// supplies equal-length, non-overlapping rows with lengths divisible by four.
// alpha is zero to preserve BGRA/cursor alpha, or 0xff000000 for opaque BGRX.
func bgrToRGBARowGeneric(dst, src []byte, alpha uint32) {
	for i := 0; i < len(src); i += gpuBytesPerPixel {
		pixel := binary.LittleEndian.Uint32(src[i:])
		rgba := pixel&0xff00ff00 | (pixel>>16)&0xff | (pixel&0xff)<<16 | alpha
		binary.LittleEndian.PutUint32(dst[i:], rgba)
	}
}
