//go:build !amd64

package encoder

func analysisVaaInfoIntraFast(_ []uint8, _ int, _ int32) (int32, bool) { return 0, false }
