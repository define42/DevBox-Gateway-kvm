//go:build !amd64

package encoder

func dctLumaMbFast(coeff []int16, source []byte, sourceOffset int, stride int32,
	prediction []byte, predictionOffset int, transform PDctFunc) bool {
	return false
}
