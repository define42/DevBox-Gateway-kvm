//go:build !amd64

package encoder

func coefficientNonzeroMask16(levels []int16) uint16 {
	return coefficientNonzeroMask16Scalar(levels)
}

func coefficientScanMask(levels, dct []int16, ac bool) uint16 {
	return coefficientScanMaskScalar(levels, dct, ac)
}
