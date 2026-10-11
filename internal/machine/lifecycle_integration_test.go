//go:build integration

package machine

import (
	"errors"
	"os"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/define42/gokvm/kvm"
)

func newLifecycleMachine(t *testing.T, cpus int) *Machine {
	t.Helper()
	m, err := New("/dev/kvm", cpus, MinMemSize)
	if errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) {
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

	return m
}

//nolint:paralleltest // Keep FD-lifetime assertions isolated from other KVM tests.
func TestCloseStopsRunningVCPUs(t *testing.T) {
	for _, tc := range []struct {
		name string
		code []byte
	}{
		{name: "busy", code: []byte{0xfa, 0xeb, 0xfe}},         // cli; jmp .
		{name: "halted", code: []byte{0xfa, 0xf4, 0xeb, 0xfd}}, // cli; hlt; jmp hlt
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newLifecycleMachine(t, 2)
			copy(m.mem[0x1000:], tc.code)
			results := make(chan error, 2)
			for cpu, fd := range m.vcpuFds {
				sregs, err := kvm.GetSregs(fd)
				if err != nil {
					t.Fatal(err)
				}
				sregs.CS.Base, sregs.CS.Selector = 0, 0
				if err := kvm.SetSregs(fd, sregs); err != nil {
					t.Fatal(err)
				}
				if err := kvm.SetRegs(fd, &kvm.Regs{RIP: 0x1000, RFLAGS: 2}); err != nil {
					t.Fatal(err)
				}
				if err := kvm.SetMPState(fd, &kvm.MPState{State: kvm.MPStateRunnable}); err != nil {
					t.Fatal(err)
				}
				go func() { results <- m.RunInfiniteLoop(cpu) }()
			}
			deadline := time.After(2 * time.Second)
			for {
				m.runMu.Lock()
				count := len(m.threads)
				m.runMu.Unlock()
				if count == 2 {
					break
				}
				select {
				case err := <-results:
					t.Fatalf("vCPU stopped before Close: %v", err)
				case <-deadline:
					t.Fatal("vCPUs did not start")
				default:
					time.Sleep(time.Millisecond)
				}
			}
			// Give the registered threads time to enter KVM, including in-kernel HLT.
			time.Sleep(20 * time.Millisecond)
			closed := make(chan error, 1)
			go func() { closed <- m.Close() }()
			select {
			case err := <-closed:
				if err != nil {
					t.Fatal(err)
				}
			case <-deadline:
				t.Fatal("Close did not wake and join vCPUs")
			}
			for range 2 {
				if err := <-results; !errors.Is(err, ErrMachineStopped) {
					t.Errorf("vCPU returned %v", err)
				}
			}
			if _, err := m.RunOnce(0); !errors.Is(err, ErrMachineStopped) {
				t.Fatalf("RunOnce after Close: %v", err)
			}
		})
	}
}

//nolint:paralleltest // Keep FD-lifetime assertions isolated from other KVM tests.
func TestCloseReleasesResources(t *testing.T) {
	m := newLifecycleMachine(t, 2)
	fds := append([]uintptr{m.kvmFd, m.vmFd}, m.vcpuFds...)
	var wg sync.WaitGroup
	for range 4 {
		wg.Go(func() {
			if err := m.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	wg.Wait()
	for _, fd := range fds {
		var stat syscall.Stat_t
		if err := syscall.Fstat(int(fd), &stat); !errors.Is(err, syscall.EBADF) {
			t.Errorf("fd %d still open: %v", fd, err)
		}
	}
	for i, mapping := range append(m.runMappings, m.memMapping) {
		// syscall.Munmap rejects mappings it has already unmapped without touching
		// the old address. If cleanup missed one, this also releases the test leak.
		if err := syscall.Munmap(mapping); !errors.Is(err, syscall.EINVAL) {
			t.Errorf("mapping %d was not released: %v", i, err)
		}
	}
}

//nolint:paralleltest // Keep FD-lifetime assertions isolated from other KVM tests.
func TestRunImmediateExitReturnsEINTR(t *testing.T) {
	m := newLifecycleMachine(t, 1)
	m.runs[0].ImmediateExit = 1
	done := make(chan error, 1)
	go func() { done <- kvm.Run(m.vcpuFds[0]) }()
	select {
	case err := <-done:
		if !errors.Is(err, syscall.EINTR) {
			t.Fatalf("Run: %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("KVM_RUN retried immediate exit")
	}
}
