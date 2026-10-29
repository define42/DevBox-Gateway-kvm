//go:build windows

package main

import (
	"net"
	"os"
	"syscall"
	"testing"
	"time"
)

func TestWindowsSocketResetCompletion(t *testing.T) {
	t.Parallel()
	result := report{Mode: "bitmap", Outcome: "failed", ActualEncoder: "bitmap", FramesSent: 60}
	err := &net.OpError{Op: "write", Err: &os.SyscallError{Syscall: "wsasend", Err: syscall.WSAECONNRESET}}
	finishSession(&result, 1, 30*time.Second, err)
	if result.Outcome != "completed" {
		t.Fatalf("Windows socket reset not recognized: %+v", result)
	}
	if platformConnectionReset(os.ErrDeadlineExceeded) {
		t.Fatal("socket timeout must not be classified as peer closure")
	}
}
