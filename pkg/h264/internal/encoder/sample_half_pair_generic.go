//go:build !amd64

package encoder

func initSampleHalfSatdPairSIMD(samples *SSampleDealingFunc, flags uint32) {
	samples.bSampleHalfSatdPair = false
}

func sampleHalfSatdPair(enc []byte, encOff int, encStride int32, prediction []byte,
	secondOff int, width, height int32) (int32, int32, bool) {
	return 0, 0, false
}
