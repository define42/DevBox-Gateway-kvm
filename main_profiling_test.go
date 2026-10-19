package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestInvalidProfilingDoesNotPreventBoot(t *testing.T) {
	t.Parallel()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	args := []string{"gokvm", "boot", "-profile-socket", "relative.sock"}
	if err := run(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("optional profiling prevented normal boot handling: %v", err)
	}
}

func TestCancelledBootClosesProfiling(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "cpu.sock")
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	args := []string{"gokvm", "boot", "-profile-socket", path}
	if err := run(ctx, args); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled boot returned %v", err)
	}
	if _, err := os.Lstat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("cancelled boot left profiling socket: %v", err)
	}
}
