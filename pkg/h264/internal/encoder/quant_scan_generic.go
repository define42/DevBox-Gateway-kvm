//go:build !amd64

package encoder

func quantScanFour4x4(dct, levels, ff, mf, maxima []int16, masks []uint16, ac bool) {
	quantScanFour4x4Separate(dct, levels, ff, mf, maxima, masks, ac)
}
