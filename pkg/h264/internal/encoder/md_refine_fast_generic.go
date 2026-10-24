//go:build !amd64

package encoder

func meRefineFracPixelFast(*sWelsEncCtx, []uint8, int, *SWelsME, *SMeRefinePointer, int32, int32) bool {
	return false
}
