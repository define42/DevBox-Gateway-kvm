package machine

import (
	"errors"
	"fmt"
	"syscall"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/ebda"
	"github.com/define42/DevBox-Gateway-kvm/kvm"
)

func TestNewRejectsCPUCountBeforeOpeningKVM(t *testing.T) {
	t.Parallel()
	for _, count := range []int{-1, 0, ebda.MaxVCPUs + 1} {
		t.Run(fmt.Sprint(count), func(t *testing.T) {
			t.Parallel()
			m, err := New("/does/not/exist", count, MinMemSize)
			if m != nil || !errors.Is(err, ErrBadCPU) {
				t.Fatalf("New returned %v, %v", m, err)
			}
		})
	}
}

func TestCPUToFDBounds(t *testing.T) {
	t.Parallel()
	m := &Machine{vcpuFds: []uintptr{7, 9}}
	for _, cpu := range []int{-1, 2, 3} {
		t.Run(fmt.Sprint(cpu), func(t *testing.T) {
			t.Parallel()
			if _, err := m.CPUToFD(cpu); !errors.Is(err, ErrBadCPU) {
				t.Fatalf("got %v", err)
			}
		})
	}
	if fd, err := m.CPUToFD(1); err != nil || fd != 9 {
		t.Fatalf("got %d, %v", fd, err)
	}
}

func TestRunOnceDoesNotReplayIOAfterRunFailure(t *testing.T) {
	t.Parallel()
	run := &kvm.RunData{ExitReason: uint32(kvm.EXITIO)}
	m := &Machine{vcpuFds: []uintptr{^uintptr(0)}, runs: []*kvm.RunData{run}}
	called := false
	m.ioportHandlers[0][0] = func(uint64, []byte) error {
		called = true

		return nil
	}
	again, err := m.RunOnce(0)
	if again || !errors.Is(err, syscall.EBADF) {
		t.Fatalf("got %v, %v", again, err)
	}
	if called {
		t.Fatal("replayed stale I/O")
	}
}

func TestRunOnceAfterStop(t *testing.T) {
	t.Parallel()
	m := &Machine{vcpuFds: []uintptr{^uintptr(0)}, runs: []*kvm.RunData{{}}}
	if err := m.Stop(); err != nil {
		t.Fatal(err)
	}
	again, err := m.RunOnce(0)
	if again || !errors.Is(err, ErrMachineStopped) {
		t.Fatalf("got %v, %v", again, err)
	}
}
