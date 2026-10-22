package encoder

// Only the four built-in AVX2 SATD shapes opt in. Narrow shapes retain their
// original SSE4.1 behavior, including validation and negative-stride failures.
func sampleHalfSatdPairEnabled(samples *SSampleDealingFunc, block uint8, width, height int32) bool {
	if !samples.bSampleHalfSatdPair || samples.pfMeCost != &samples.pfSampleSatd {
		return false
	}
	switch block {
	case BLOCK_16x16:
		return width == 16 && height == 16
	case BLOCK_16x8:
		return width == 16 && height == 8
	case BLOCK_8x16:
		return width == 8 && height == 16
	case BLOCK_8x8:
		return width == 8 && height == 8
	}
	return false
}
