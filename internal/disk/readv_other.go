//go:build !linux

package disk

import "os"

// preadv reads only the first buffer on systems without a vectored read; the
// caller continues with the rest.
func preadv(file *os.File, buffers [][]byte, off int64) (int, error) {
	return file.ReadAt(buffers[0], off)
}
