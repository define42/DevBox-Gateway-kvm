//go:build linux

package disk

import (
	"errors"
	"os"

	"golang.org/x/sys/unix"
)

// preadv issues one vectored read. Control holds a file reference without the
// read lock that RawConn.Read takes, so concurrent reads are not serialized,
// matching os.File.ReadAt.
func preadv(file *os.File, buffers [][]byte, off int64) (int, error) {
	raw, err := file.SyscallConn()
	if err != nil {
		return 0, err
	}
	var n int
	var readErr error
	if err := raw.Control(func(fd uintptr) {
		for {
			n, readErr = unix.Preadv(int(fd), buffers, off)
			if !errors.Is(readErr, unix.EINTR) {
				return
			}
		}
	}); err != nil {
		return 0, err
	}
	if readErr != nil {
		return max(n, 0), &os.PathError{Op: "preadv", Path: file.Name(), Err: readErr}
	}

	return n, nil
}
