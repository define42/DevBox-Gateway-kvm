package main

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/machine"
	"github.com/define42/DevBox-Gateway-kvm/internal/vmm"
	"golang.org/x/sys/unix"
)

func TestGuestResetPreservesOtherFailures(t *testing.T) {
	t.Parallel()

	wrapped := fmt.Errorf("CPU 0: %w", machine.ErrUEFIReset)
	for _, tc := range []struct {
		name string
		err  error
		want bool
	}{
		{name: "shutdown", err: nil, want: false},
		{name: "firmware reset", err: machine.ErrUEFIReset, want: true},
		{name: "wrapped reset", err: wrapped, want: true},
		{name: "successful cleanup", err: errors.Join(wrapped, nil), want: true},
		{name: "disk flush failed", err: errors.Join(wrapped, os.ErrPermission), want: false},
		{name: "cancelled", err: errors.Join(wrapped, context.Canceled), want: false},
		{name: "unrelated failure", err: os.ErrNotExist, want: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := isGuestReset(tc.err); got != tc.want {
				t.Fatalf("restart for %v = %v, want %v", tc.err, got, tc.want)
			}
		})
	}
}

func TestCancelledBootDoesNotCreateMachine(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	conf := vmm.Config{Dev: "/nonexistent-kvm", UEFI: "OVMF_CODE.fd", UEFIVars: "guest_VARS.fd", Disk: "guest.qcow2"}
	if err := runBoot(ctx, conf); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled boot returned %v", err)
	}
}

func TestCancelledBootClosesInheritedRDPListener(t *testing.T) {
	t.Parallel()
	listener, err := net.ListenTCP("tcp4", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	address := listener.Addr().String()
	file, err := listener.File()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	// Transfer a raw duplicate to run, which becomes its sole owner. Wrapping
	// it in another os.File here would risk closing a reused descriptor later.
	fd, err := unix.FcntlInt(file.Fd(), unix.F_DUPFD_CLOEXEC, 3)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	args := []string{"gokvm", "boot", "-rdp", address, "-rdp-fd", fmt.Sprint(fd)}
	if err := run(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled boot with inherited listener returned %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}

	replacement, err := net.Listen("tcp", address)
	if err != nil {
		t.Fatalf("cancelled boot retained its inherited RDP listener: %v", err)
	}
	if err := replacement.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestBootRejectsInvalidInheritedRDPDescriptorBeforeInitialization(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// This descriptor exceeds the process limit without closing a real file
	// whose descriptor could be reused by another parallel test.
	const invalidFD = 1<<31 - 1
	args := []string{"gokvm", "boot", "-rdp", "127.0.0.1:3389", "-rdp-fd", fmt.Sprint(invalidFD)}
	if err := run(ctx, args); !errors.Is(err, unix.EBADF) {
		t.Fatalf("invalid inherited descriptor returned %v, want EBADF before boot cancellation", err)
	}
}

func TestCancellationPreservesCleanupFailure(t *testing.T) {
	t.Parallel()
	if !isOnlyError(errors.Join(context.Canceled), context.Canceled) {
		t.Fatal("plain cancellation not recognized")
	}
	cleanup := os.ErrPermission
	if isOnlyError(errors.Join(context.Canceled, cleanup), context.Canceled) {
		t.Fatal("disk persistence error hidden by cancellation")
	}
}
