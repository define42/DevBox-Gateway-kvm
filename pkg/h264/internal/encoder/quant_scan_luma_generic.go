//go:build !amd64

package encoder

func quantScanLuma(dct, levels, ff, mf, maxima []int16, masks []uint16) {
	quantScanLumaSeparate(dct, levels, ff, mf, maxima, masks)
}
