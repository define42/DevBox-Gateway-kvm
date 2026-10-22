package encoder

// quantScanFour4x4Separate is the pre-fusion sequence. Keep it for unsupported
// CPUs and overlapping output buffers, whose sequential write order matters.
func quantScanFour4x4Separate(dct, levels, ff, mf, maxima []int16, masks []uint16, ac bool) {
	_ = levels[63]
	_ = masks[3]
	WelsQuantFour4x4Max_sse2(dct, ff, mf, maxima)
	for block := 0; block < 4; block++ {
		offset := block * 16
		if maxima[block] == 0 {
			clear(levels[offset : offset+16])
			masks[block] = 0
		} else {
			masks[block] = coefficientScanMask(levels[offset:], dct[offset:], ac)
		}
	}
}
