//go:build amd64

package encoder

//go:noescape
func hadamardT4DcSSE2(dc *[16]int16, residual *[241]int16)

//go:noescape
func dequantIHadamard4x4SSE2(residual *[16]int16, mf uint64)

func hadamardT4Dc(dc, residual []int16) {
	// The scalar routine gathers every DC before its first output store, so
	// overlapping input/output remains valid. Short slices retain its exact
	// read/write and panic order, including partial column-ordered output.
	if len(dc) < 16 || len(residual) < 241 {
		WelsHadamardT4Dc_c(dc, residual)
		return
	}
	hadamardT4DcSSE2((*[16]int16)(dc), (*[241]int16)(residual))
}

func dequantIHadamard4x4(residual []int16, mf uint16) {
	if len(residual) < 16 {
		WelsDequantIHadamard4x4_c(residual, mf)
		return
	}
	dequantIHadamard4x4SSE2((*[16]int16)(residual), uint64(mf))
}
