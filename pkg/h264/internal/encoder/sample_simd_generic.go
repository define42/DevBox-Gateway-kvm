//go:build !amd64

package encoder

func WelsSampleSatd4x4_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return WelsSampleSatd4x4_c(a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd8x8_sse41(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return WelsSampleSatd8x8_c(a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd8x8_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return WelsSampleSatd8x8_c(a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd16x8_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return WelsSampleSatd16x8_c(a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd8x16_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return WelsSampleSatd8x16_c(a, aOff, aStride, ref, refOff, refStride)
}

func WelsSampleSatd16x16_avx2(a []uint8, aOff int, aStride int32, ref []uint8, refOff int, refStride int32) int32 {
	return WelsSampleSatd16x16_c(a, aOff, aStride, ref, refOff, refStride)
}

func initSampleAvgSatdSIMD(s *SSampleDealingFunc, flags uint32) {
	s.pfSampleAvgSatd = nil
	s.pfSampleAvgSatdValidated = nil
	s.bSampleAvgSatdFour = false
	s.bWholeFractionalRefine = false
}
