//go:build !amd64

package encoder

func initSampleSadTwoSIMD(samples *SSampleDealingFunc, flags uint32) {
	samples.bSampleSadTwo = false
}

func meInitialSadTwo(me *SWelsME, initial, mvc SMVUnitXY, initialOff int,
	encStride, refStride int32) (SMVUnitXY, int, int32, bool) {
	return SMVUnitXY{}, 0, 0, false
}
