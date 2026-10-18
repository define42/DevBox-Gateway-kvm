package main

import (
	"context"
	"errors"
	"strconv"
	"testing"

	"golang.org/x/sys/unix"
)

func TestInvalidTelemetryDoesNotPreventBoot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// An invalid optional telemetry descriptor must not replace the ordinary
	// boot result or cause an attempt to initialize the VM after cancellation.
	args := []string{"gokvm", "boot", "-telemetry-fd", "2147483647"}
	if err := run(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("telemetry initialization prevented normal boot handling: %v", err)
	}
}

func TestCancelledBootJoinsTelemetryAndClosesDescriptor(t *testing.T) {
	t.Parallel()
	pair, err := unix.Socketpair(unix.AF_UNIX, unix.SOCK_DGRAM|unix.SOCK_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer unix.Close(pair[0])
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	// The raw child descriptor is owned only by run; no second os.File can
	// accidentally close an unrelated descriptor after the number is reused.
	args := []string{"gokvm", "boot", "-telemetry-fd", strconv.Itoa(pair[1])}
	if err := run(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled boot returned %v", err)
	}
	err = unix.Send(pair[0], []byte("closed"), unix.MSG_DONTWAIT|unix.MSG_NOSIGNAL)
	if !errors.Is(err, unix.ECONNREFUSED) {
		t.Fatalf("boot left telemetry socket open: %v", err)
	}
}
