//go:build !amd64

package avc

func rgb32ToI420Damage(
	dstY0, dstY1, dstU, dstV, src0, src1 []byte, unchanged []bool, redIndex int,
) (changed, ok bool) {
	return false, false
}
