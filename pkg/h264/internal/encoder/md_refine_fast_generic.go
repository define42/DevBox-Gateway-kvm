//go:build !amd64

package encoder

const satdPtrSupported = false

func meRefineFracPixelFast(_ *sWelsEncCtx, _ []uint8, _ int, _ *SWelsME, _ *SMeRefinePointer, _, _ int32) bool {
	return false
}
