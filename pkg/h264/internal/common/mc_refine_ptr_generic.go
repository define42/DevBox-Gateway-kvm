//go:build !amd64

package common

func RefineHalfpelVer(_ *byte, _ int, _ *byte, _, _, _ int) {
	panic("h264: RefineHalfpelVer unsupported")
}

func RefineHalfpelHor(_ *byte, _ int, _ *byte, _, _, _ int) {
	panic("h264: RefineHalfpelHor unsupported")
}

func RefineHalfpelCen(_ *byte, _ int, _ *byte, _, _, _ int) {
	panic("h264: RefineHalfpelCen unsupported")
}

func PixelAvgPtr(_ *byte, _ int, _ *byte, _ int, _ *byte, _ int, _, _ int) {
	panic("h264: PixelAvgPtr unsupported")
}
