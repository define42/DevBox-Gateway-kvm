//go:build amd64

package avc

import "golang.org/x/sys/cpu"

// rgb32ToI420Damage converts two unscaled rows starting on a 16-pixel boundary.
// Each unchanged bit covers 16 luma pixels and their eight U/V samples. The
// caller initializes bits before processing all row pairs of a macroblock row;
// this function only clears bits, and compares samples before overwriting them.
// Unsupported geometry or CPUs return ok=false without touching the buffers.
func rgb32ToI420Damage(
	dstY0, dstY1, dstU, dstV, src0, src1 []byte, unchanged []bool, redIndex int,
) (changed, ok bool) {
	width := len(dstY0)
	if !cpu.X86.HasAVX2 || width == 0 || width > maxDimension || width%32 != 0 ||
		len(dstY1) != width || len(dstU) != width/2 || len(dstV) != width/2 ||
		len(src0) != width*4 || len(src1) != width*4 || len(unchanged) != width/16 ||
		(redIndex != 0 && redIndex != 2) {
		return false, false
	}

	return rgb32ToI420DamageAVX2(
		&dstY0[0], &dstY1[0], &dstU[0], &dstV[0], &src0[0], &src1[0],
		&unchanged[0], width, redIndex,
	), true
}

//go:noescape
func rgb32ToI420DamageAVX2(
	dstY0, dstY1, dstU, dstV, src0, src1 *byte, unchanged *bool, width, redIndex int,
) bool
