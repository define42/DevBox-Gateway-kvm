//go:build integration

package vmm

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/machine"
)

func TestRDPWaitCancellationPrecedesVCPURun(t *testing.T) {
	t.Parallel()
	conf := withGatewayRDP(t, Config{
		Dev: "/dev/kvm", NCPUs: 1, MemSize: machine.MinMemSize,
		RDPWait: true,
	})
	v := New(conf)
	if err := v.Init(); errors.Is(err, os.ErrNotExist) || errors.Is(err, os.ErrPermission) {
		t.Skipf("KVM unavailable: %v", err)
	} else if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = v.Close() })
	// Running these diagnostic instructions would produce an immediate CPU failure.
	// The readiness barrier must prevent that execution before cancellation.
	if _, err := v.WriteAt([]byte(machine.Poison), 0x100000); err != nil {
		t.Fatal(err)
	}
	if err := v.SetupRegs(0x100000, 0x10000, false); err != nil {
		t.Fatal(err)
	}
	addr := v.rdpDisplay.Addr()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Millisecond)
	defer cancel()
	if err := v.BootContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("CPU started before an RDP session was ready: %v", err)
	}
	if _, err := v.RunOnce(0); !errors.Is(err, machine.ErrMachineStopped) {
		t.Fatalf("cancelled wait did not close the machine: %v", err)
	}
	if err := conf.RDPListenerFile.Close(); err != nil {
		t.Fatal(err)
	}
	assertRDPSocketReleased(t, addr)
}
