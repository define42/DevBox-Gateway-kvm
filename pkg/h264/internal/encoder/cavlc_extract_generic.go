//go:build !amd64

package encoder

func cavlcExtractSupported(_ []int16, _ int32) bool { return false }

// The constant-false capability check removes this call on non-amd64 targets.
func cavlcExtractSSSE3(coeff *[16]int16, run *[16]uint8, level *[16]int16, total *int32, end int32) int32 {
	return CavlcParamCal_c(coeff[:], run[:], level[:], total, end)
}
