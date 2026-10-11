package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/define42/gokvm/internal/machine"
	"github.com/define42/gokvm/internal/vmm"
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
