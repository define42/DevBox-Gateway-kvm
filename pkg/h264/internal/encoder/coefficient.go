package encoder

import "math/bits"

// coefficientScoreMask is the JVT-O079 score of a block in scan order. Only
// zero runs matter; coefficient signs and magnitudes do not affect this score.
func coefficientScoreMask(mask uint16) int32 {
	var score int32
	previous := -1
	for mask != 0 {
		position := bits.TrailingZeros16(mask)
		score += kiTRunTable[position-previous-1]
		previous = position
		mask &= mask - 1
	}
	return score
}

func coefficientNonzeroMask16Scalar(levels []int16) uint16 {
	_ = levels[15]
	var mask uint16
	for i, value := range levels[:16] {
		if value != 0 {
			mask |= 1 << i
		}
	}
	return mask
}

func coefficientScanMaskScalar(levels, dct []int16, ac bool) uint16 {
	if ac {
		WelsScan4x4Ac_c(levels, dct)
	} else {
		WelsScan4x4DcAc_c(levels, dct)
	}
	return coefficientNonzeroMask16Scalar(levels)
}

func coefficientNonzeroCount(levels []int16) int32 {
	return int32(bits.OnesCount16(coefficientNonzeroMask16(levels)))
}
