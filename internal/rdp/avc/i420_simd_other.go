//go:build !amd64

package avc

func rgbaToI420AVX2(dstY0, dstY1, dstU, dstV, src0, src1 []byte) (changed, ok bool) {
	return false, false
}

func bgrxToI420AVX2(dstY0, dstY1, dstU, dstV, src0, src1 []byte) (changed, ok bool) {
	return false, false
}
