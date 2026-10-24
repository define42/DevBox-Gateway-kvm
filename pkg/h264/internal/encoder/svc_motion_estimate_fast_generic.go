//go:build !amd64

package encoder

func welsMotionEstimateIntegerFast(*SWelsFuncPtrList, *SWelsME, *SSlice, int32, int32) bool {
	return false
}
