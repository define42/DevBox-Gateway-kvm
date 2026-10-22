//go:build !amd64

package encoder

func hadamardQuant2x2(rs []int16, ff, mf int16, dct, block []int16) int32 {
	return WelsHadamardQuant2x2_c(rs, ff, mf, dct, block)
}
