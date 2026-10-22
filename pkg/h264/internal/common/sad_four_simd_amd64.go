//go:build amd64

package common

// The kernels share source loads between the four motion candidates. The
// reference pointer starts at the center candidate; the wrapper also validates
// the row above it before passing tracked pointers to assembly.

//go:noescape
func sadFour16SSE2(src *byte, srcStride int, ref *byte, refStride, height int, sad *[4]int32)

//go:noescape
func sadFour8SSE2(src *byte, srcStride int, ref *byte, refStride, height int, sad *[4]int32)

//go:noescape
func sadFour4SSE2(src *byte, srcStride int, ref *byte, refStride, height int, sad *[4]int32)

func sampleSadFourSIMD(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, width, height int, sad []int32) {
	out := (*[4]int32)(sad)
	if aStride < 0 || bStride < 0 {
		panic(simdBoundsPanic)
	}
	aEnd := aOff + (height-1)*int(aStride) + width
	checkSIMDSpan(a, int64(aOff), int64(aEnd-1))
	aBlock := &a[aOff]
	// A zero stride still needs the left and right neighboring pixels. Check
	// len explicitly: a slice expression alone permits reads up to cap.
	padding := max(int(bStride), 1)
	bEnd := bOff + (height-1)*int(bStride) + width + padding
	checkSIMDSpan(b, int64(bOff-padding), int64(bEnd-1))
	bBlock := &b[bOff]
	switch width {
	case 16:
		sadFour16SSE2(aBlock, int(aStride), bBlock, int(bStride), height, out)
	case 8:
		sadFour8SSE2(aBlock, int(aStride), bBlock, int(bStride), height, out)
	case 4:
		sadFour4SSE2(aBlock, int(aStride), bBlock, int(bStride), height, out)
	}
}

func WelsSampleSadFour16x16_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSIMD(a, aOff, aStride, b, bOff, bStride, 16, 16, sad)
}

func WelsSampleSadFour16x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSIMD(a, aOff, aStride, b, bOff, bStride, 16, 8, sad)
}

func WelsSampleSadFour8x16_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSIMD(a, aOff, aStride, b, bOff, bStride, 8, 16, sad)
}

func WelsSampleSadFour8x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSIMD(a, aOff, aStride, b, bOff, bStride, 8, 8, sad)
}

func WelsSampleSadFour8x4_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSIMD(a, aOff, aStride, b, bOff, bStride, 8, 4, sad)
}

func WelsSampleSadFour4x8_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSIMD(a, aOff, aStride, b, bOff, bStride, 4, 8, sad)
}

func WelsSampleSadFour4x4_sse2(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sampleSadFourSIMD(a, aOff, aStride, b, bOff, bStride, 4, 4, sad)
}
