//go:build !amd64

package encoder

func dctLumaMbFast(_ []int16, _ []uint8, _ int, _ int32, _ []uint8, _ int) bool { return false }
