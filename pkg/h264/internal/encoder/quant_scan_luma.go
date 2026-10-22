package encoder

// Preserve the original four-call ordering for unsupported CPUs and aliases.
// In particular, each group snapshots FF/MF after previous groups have written.
func quantScanLumaSeparate(dct, levels, ff, mf, maxima []int16, masks []uint16) {
	_ = dct[255]
	_ = levels[255]
	_ = ff[7]
	_ = mf[7]
	_ = maxima[15]
	_ = masks[15]
	for group := 0; group < 4; group++ {
		offset := group * 64
		quantScanFour4x4(dct[offset:], levels[offset:], ff, mf, maxima[group*4:], masks[group*4:], false)
	}
}
