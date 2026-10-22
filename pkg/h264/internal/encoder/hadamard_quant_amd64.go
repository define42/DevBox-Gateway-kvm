//go:build amd64

package encoder

//go:noescape
func hadamardQuant2x2SSE2(rs *[49]int16, ff, mf int64, dct, block *[4]int16) int32

func hadamardQuant2x2(rs []int16, ff, mf int16, dct, block []int16) int32 {
	// The packed unsigned arithmetic matches the scalar quantizer throughout
	// its nonnegative FF/MF domain. Retain signed-parameter behavior and the
	// original partial writes/panic order for short input or output slices.
	if len(rs) < 49 || len(dct) < 4 || len(block) < 4 || ff < 0 || mf < 0 {
		return WelsHadamardQuant2x2_c(rs, ff, mf, dct, block)
	}
	return hadamardQuant2x2SSE2((*[49]int16)(rs), int64(ff), int64(mf), (*[4]int16)(dct), (*[4]int16)(block))
}
