package serial

import (
	"context"
	"errors"
	"io"
	"os"

	"golang.org/x/sys/unix"
)

// StartContext reads terminal input until EOF, Ctrl-A x, or cancellation. Polling
// an eventfd alongside the input lets shutdown join this reader without closing
// the caller's terminal or leaving a goroutine blocked in Read.
func (s *Serial) StartContext(ctx context.Context, in *os.File, irqInject func() error) error {
	wake, err := unix.Eventfd(0, unix.EFD_CLOEXEC|unix.EFD_NONBLOCK)
	if err != nil {
		return err
	}
	defer unix.Close(wake)
	awakened := make(chan struct{})
	stopWake := context.AfterFunc(ctx, func() {
		defer close(awakened)
		// The fd remains open until this callback has finished.
		_, _ = unix.Write(wake, []byte{1, 0, 0, 0, 0, 0, 0, 0})
	})
	defer func() {
		if !stopWake() {
			<-awakened
		}
	}()
	polls := []unix.PollFd{{Fd: int32(in.Fd()), Events: unix.POLLIN}, {Fd: int32(wake), Events: unix.POLLIN}}
	var before byte
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		if _, err := unix.Poll(polls, -1); err != nil {
			if errors.Is(err, unix.EINTR) {
				continue
			}

			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if polls[0].Revents&unix.POLLNVAL != 0 {
			return os.ErrClosed
		}
		if polls[0].Revents&(unix.POLLIN|unix.POLLHUP|unix.POLLERR) == 0 {
			continue
		}
		var buf [1]byte
		n, err := unix.Read(int(in.Fd()), buf[:])
		if errors.Is(err, unix.EINTR) || errors.Is(err, unix.EAGAIN) {
			continue
		}
		if err != nil {
			return err
		}
		if n == 0 {
			return io.EOF
		}
		select {
		case s.inputChan <- buf[0]:
		case <-ctx.Done():
			return ctx.Err()
		}
		if err := irqInject(); err != nil {
			return err
		}
		if before == 1 && buf[0] == 'x' {
			return io.EOF
		}
		before = buf[0]
	}
}
