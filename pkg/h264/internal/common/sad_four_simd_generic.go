//go:build !amd64

package common

func WelsSampleSadFour16x16_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSSE2(WelsSampleSad16x16_sse2, a, aOff, aStride, b, bOff, bStride, sad)
}

func WelsSampleSadFour16x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSSE2(WelsSampleSad16x8_sse2, a, aOff, aStride, b, bOff, bStride, sad)
}

func WelsSampleSadFour8x16_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSSE2(WelsSampleSad8x16_sse2, a, aOff, aStride, b, bOff, bStride, sad)
}

func WelsSampleSadFour8x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSSE2(WelsSampleSad8x8_sse2, a, aOff, aStride, b, bOff, bStride, sad)
}

func WelsSampleSadFour8x4_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSSE2(WelsSampleSad8x4_sse2, a, aOff, aStride, b, bOff, bStride, sad)
}

func WelsSampleSadFour4x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSSE2(WelsSampleSad4x8_sse2, a, aOff, aStride, b, bOff, bStride, sad)
}

func WelsSampleSadFour4x4_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSSE2(WelsSampleSad4x4_sse2, a, aOff, aStride, b, bOff, bStride, sad)
}
