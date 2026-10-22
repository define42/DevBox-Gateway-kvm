//go:build amd64

package common

// Checked wrappers pass tracked pointers to private assembly entry points.
// This keeps backing arrays live without passing unused slice length/capacity.

//go:noescape
func sad16x16(src *byte, srcStride int, ref *byte, refStride int) int

//go:noescape
func sad16x8(src *byte, srcStride int, ref *byte, refStride int) int

//go:noescape
func sad8x16(src *byte, srcStride int, ref *byte, refStride int) int

//go:noescape
func sad8x8(src *byte, srcStride int, ref *byte, refStride int) int

//go:noescape
func sad8x4(src *byte, srcStride int, ref *byte, refStride int) int

//go:noescape
func sad4x8(src *byte, srcStride int, ref *byte, refStride int) int

//go:noescape
func sad4x4(src *byte, srcStride int, ref *byte, refStride int) int

func WelsSampleSad16x16_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
	return int32(sad16x16(SIMDBlockPtr(a, aOff, aStride, 16, 16), int(aStride),
		SIMDBlockPtr(b, bOff, bStride, 16, 16), int(bStride)))
}

func WelsSampleSad16x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
	return int32(sad16x8(SIMDBlockPtr(a, aOff, aStride, 16, 8), int(aStride),
		SIMDBlockPtr(b, bOff, bStride, 16, 8), int(bStride)))
}

func WelsSampleSad8x16_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
	return int32(sad8x16(SIMDBlockPtr(a, aOff, aStride, 8, 16), int(aStride),
		SIMDBlockPtr(b, bOff, bStride, 8, 16), int(bStride)))
}

func WelsSampleSad8x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
	return int32(sad8x8(SIMDBlockPtr(a, aOff, aStride, 8, 8), int(aStride),
		SIMDBlockPtr(b, bOff, bStride, 8, 8), int(bStride)))
}

func WelsSampleSad8x4_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
	return int32(sad8x4(SIMDBlockPtr(a, aOff, aStride, 8, 4), int(aStride),
		SIMDBlockPtr(b, bOff, bStride, 8, 4), int(bStride)))
}

func WelsSampleSad4x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
	return int32(sad4x8(SIMDBlockPtr(a, aOff, aStride, 4, 8), int(aStride),
		SIMDBlockPtr(b, bOff, bStride, 4, 8), int(bStride)))
}

func WelsSampleSad4x4_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
	return int32(sad4x4(SIMDBlockPtr(a, aOff, aStride, 4, 4), int(aStride),
		SIMDBlockPtr(b, bOff, bStride, 4, 4), int(bStride)))
}
