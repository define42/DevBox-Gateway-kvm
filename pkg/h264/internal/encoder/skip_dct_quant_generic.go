//go:build !amd64

package encoder

func trySkipDctQuantPixels(_ *sWelsEncCtx, _ *SMbCache, _ []byte, _ int, _ int32, _ uint8) (bool, bool) {
	return false, false
}
