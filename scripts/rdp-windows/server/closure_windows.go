//go:build windows

package main

import (
	"errors"
	"syscall"
)

func platformConnectionReset(err error) bool {
	// Windows socket errors use Winsock codes, not Go's synthetic POSIX
	// syscall.ECONNRESET value. A client disconnect can race the frame writer.
	return errors.Is(err, syscall.WSAECONNRESET)
}
