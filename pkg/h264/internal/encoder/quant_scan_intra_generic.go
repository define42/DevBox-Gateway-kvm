//go:build !amd64

package encoder

func tryQuantScanIntraLuma(funcs *SWelsFuncPtrList, dct, levels, ff, mf []int16, masks *[16]uint16) bool {
	return false
}
