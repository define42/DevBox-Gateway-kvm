package common

type sampleSadFunc func([]uint8, int, int32, []uint8, int, int32) int32

func sampleSadFourSSE2(fn sampleSadFunc, a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32, sad []int32) {
	sad[0] = fn(a, aOff, aStride, b, bOff-int(bStride), bStride)
	sad[1] = fn(a, aOff, aStride, b, bOff+int(bStride), bStride)
	sad[2] = fn(a, aOff, aStride, b, bOff-1, bStride)
	sad[3] = fn(a, aOff, aStride, b, bOff+1, bStride)
}
