//go:build integration

package vmm

import (
	"context"
	"errors"
	"net"
	"os"
	"testing"
	"time"

	"github.com/define42/gokvm/internal/machine"
	"github.com/define42/gokvm/internal/term"
	"github.com/define42/gokvm/kvm"
)

func TestHeadlessBootReturnsCPUFailure(t *testing.T) {
	t.Parallel()
	if term.IsTerminal() {
		t.Skip("requires headless stdin")
	}
	m, err := machine.New("/dev/kvm", 2, machine.MinMemSize)
	if errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		t.Skipf("KVM unavailable: %v", err)
	}
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})
	// The poisoned RAM deliberately executes UD2 without an IDT, producing a
	// fatal exit on the BSP while the second CPU is waiting for startup.
	if err := m.SetupRegs(0x100000, 0x10000, false); err != nil {
		t.Fatal(err)
	}
	v := New(Config{NCPUs: 2})
	v.Machine = m
	ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
	defer cancel()
	err = v.BootContext(ctx)
	if !errors.Is(err, kvm.ErrUnexpectedExitReason) || errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("headless Boot returned %v", err)
	}
	if _, err := m.RunOnce(1); !errors.Is(err, machine.ErrMachineStopped) {
		t.Fatalf("sibling CPU not stopped: %v", err)
	}
}

func TestSetupFailureClosesMachineAndConsole(t *testing.T) {
	t.Parallel()

	v := New(Config{
		Dev: "/dev/kvm", NCPUs: 1, MemSize: machine.MinMemSize,
		Kernel: t.TempDir() + "/missing-kernel", VNC: "127.0.0.1:0",
	})
	if err := v.Init(); errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		t.Skipf("KVM unavailable: %v", err)
	} else if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Error(err)
		}
	})
	addr := v.vncDisplay.Addr()
	if err := v.Setup(); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Setup returned %v, want missing kernel error", err)
	}
	if _, err := v.RunOnce(0); !errors.Is(err, machine.ErrMachineStopped) {
		t.Fatalf("failed setup left machine runnable: %v", err)
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("failed setup left console listener open: %v", err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
}
