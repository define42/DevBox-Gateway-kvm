//go:build amd64

package common

// SadPtrSupported reports whether SadPtr and SadFourPtr run the SSE2 kernels.
const SadPtrSupported = true

// SadPtr returns the SAD of a width×height block (a pfSampleSad block size).
// The caller must have validated both spans; no bounds are checked here.
func SadPtr(width, height int, src *byte, srcStride int, ref *byte, refStride int) int32 {
	switch width<<8 | height {
	case 16<<8 | 16:
		return int32(sad16x16(src, srcStride, ref, refStride))
	case 16<<8 | 8:
		return int32(sad16x8(src, srcStride, ref, refStride))
	case 8<<8 | 16:
		return int32(sad8x16(src, srcStride, ref, refStride))
	case 8<<8 | 8:
		return int32(sad8x8(src, srcStride, ref, refStride))
	case 8<<8 | 4:
		return int32(sad8x4(src, srcStride, ref, refStride))
	case 4<<8 | 8:
		return int32(sad4x8(src, srcStride, ref, refStride))
	default:
		return int32(sad4x4(src, srcStride, ref, refStride))
	}
}

// SadFourPtr stores the SADs of the candidates one row above, one row below,
// one pixel left and one pixel right of ref, like WelsSampleSadFour*_sse2. The
// caller must have validated the source span and the reference span including
// a one-pixel border on every side; no bounds are checked here.
func SadFourPtr(width, height int, src *byte, srcStride int, ref *byte, refStride int, sad *[4]int32) {
	switch width {
	case 16:
		sadFour16SSE2(src, srcStride, ref, refStride, height, sad)
	case 8:
		sadFour8SSE2(src, srcStride, ref, refStride, height, sad)
	default:
		sadFour4SSE2(src, srcStride, ref, refStride, height, sad)
	}
}
