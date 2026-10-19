//go:build amd64 && !purego

package virtio

import "golang.org/x/sys/cpu"

// bgrToRGBARow converts eight pixels per AVX2 iteration. Restrict the assembly
// to complete vectors so it never reads or writes outside a dirty row; small
// rows, the final zero-to-seven pixels, and older CPUs use the Go fallback.
func bgrToRGBARow(dst, src []byte, alpha uint32) {
	vectorBytes := len(src) &^ 31
	if cpu.X86.HasAVX2 && vectorBytes != 0 {
		// Bounds-check before crossing into assembly, which has no slice checks.
		_ = dst[vectorBytes-1]
		bgrToRGBAAVX2(&dst[0], &src[0], vectorBytes, alpha)
		dst, src = dst[vectorBytes:], src[vectorBytes:]
	}
	if len(src) != 0 {
		bgrToRGBARowGeneric(dst, src, alpha)
	}
}

// bgrToRGBAAVX2 requires AVX2, non-overlapping buffers of at least n bytes, and
// a positive n divisible by 32. It retains no pointers and accesses only n bytes.
//
//go:noescape
func bgrToRGBAAVX2(dst, src *byte, n int, alpha uint32)
