//go:build !amd64 || purego

package virtio

func bgrToRGBARow(dst, src []byte, alpha uint32) {
	bgrToRGBARowGeneric(dst, src, alpha)
}
