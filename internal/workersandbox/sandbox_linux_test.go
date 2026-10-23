//go:build linux && amd64 && integration

package workersandbox

import (
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkerConfinement(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("worker intentionally rejects root")
	}
	if _, err := ABI(); err != nil {
		t.Skip(err)
	}
	dir := t.TempDir()
	own := filepath.Join(dir, "disk.img")
	other := filepath.Join(dir, "other-vm.img")
	secret := filepath.Join(dir, "gateway-secret")
	control := filepath.Join(dir, "cgroup.procs")
	for _, path := range []string{own, other, secret, control} {
		if err := os.WriteFile(path, []byte("private"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// The outside process creates both types of Unix socket. No connections to
	// them are needed for success; opening Unix sockets is denied after lockdown.
	listener, err := net.Listen("unix", filepath.Join(dir, "gateway.sock"))
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	// #nosec G204 G702 -- Re-exec this test binary with private fixtures created above.
	command := exec.Command(os.Args[0], "-test.run=^TestSandboxHelper$", "--", own, other, secret, control)
	command.Env = append(os.Environ(),
		"DEVBOX_SANDBOX_TEST=launch",
		"LDAP_PASSWORD=must-not-leak", "GORACE=atexit_sleep_ms=0")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("confined child: %v\n%s", err, output)
	}
	if !strings.Contains(string(output), "confinement verified") {
		t.Fatalf("missing child verification: %s", output)
	}
}

//nolint:paralleltest // Helper re-execs and restricts its entire process.
func TestSandboxHelper(t *testing.T) {
	stage := os.Getenv("DEVBOX_SANDBOX_TEST")
	if stage == "" {
		return
	}
	var paths []string
	for i, arg := range os.Args {
		if arg == "--" {
			paths = os.Args[i+1:]

			break
		}
	}
	if len(paths) != 4 {
		t.Fatal("missing paths")
	}
	if stage == "launch" {
		env := append(Environment(os.Environ()), "DEVBOX_SANDBOX_TEST=confined",
			"GORACE=atexit_sleep_ms=0")
		if err := Exec(os.Args[0], os.Args, env, Policy{ReadWrite: []string{paths[0]}}); err != nil {
			t.Fatal(err)
		}
		t.Fatal("exec returned")
	}
	if os.Getenv("LDAP_PASSWORD") != "" {
		t.Fatal("gateway secret inherited")
	}
	if err := LockRuntime(); err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- The parent test passes only its own temporary fixture paths.
	if err := os.WriteFile(paths[0], []byte("own disk usable"), 0o600); err != nil {
		t.Fatalf("own resource: %v", err)
	}
	for _, path := range paths[1:] {
		// #nosec G703 -- Deliberately attempt the parent test fixture to verify denial.
		if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("read %s = %v", path, err)
		}
		// #nosec G703 -- Deliberately attempt the parent test fixture to verify denial.
		if err := os.WriteFile(path, []byte("attacker"), 0o600); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("write %s = %v", path, err)
		}
		// #nosec G703 -- Deliberately attempt a parent fixture to verify denial.
		if err := os.Chmod(path, 0o777); !errors.Is(err, os.ErrPermission) {
			t.Fatalf("chmod %s = %v", path, err)
		}
	}
	if err := unix.Kill(os.Getppid(), 0); !errors.Is(err, unix.EPERM) {
		t.Fatalf("signal parent = %v", err)
	}
	if err := unix.Exec(os.Args[0], os.Args, os.Environ()); !errors.Is(err, unix.EPERM) {
		t.Fatalf("exec = %v", err)
	}
	if fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0); !errors.Is(err, unix.EPERM) {
		if err == nil {
			unix.Close(fd)
		}
		t.Fatalf("Unix socket = %v", err)
	}
	if fd, err := unix.Socket(unix.AF_INET, unix.SOCK_RAW, unix.IPPROTO_TCP); !errors.Is(err, unix.EPERM) {
		if err == nil {
			unix.Close(fd)
		}
		t.Fatalf("raw socket = %v", err)
	}
	for _, number := range []uintptr{
		unix.SYS_PTRACE, unix.SYS_PROCESS_VM_READV, unix.SYS_PIDFD_GETFD, unix.SYS_IO_URING_SETUP,
	} {
		if _, _, err := unix.Syscall(number, 0, 0, 0); err != unix.EPERM {
			t.Fatalf("syscall %d = %v", number, err)
		}
	}
	if _, err := net.InterfaceAddrs(); err != nil {
		t.Fatalf("interface discovery: %v", err)
	}
	for _, kind := range []int{unix.SOCK_STREAM, unix.SOCK_DGRAM} {
		fd, err := unix.Socket(unix.AF_INET, kind|unix.SOCK_CLOEXEC, 0)
		if err != nil {
			t.Fatalf("host socket %d: %v", kind, err)
		}
		unix.Close(fd)
	}
	// Check existing and newly-created runtime threads, not just the installer.
	var wg sync.WaitGroup
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			checkThreadNameAccess(t)
			// #nosec G703 -- Deliberately attempt a parent fixture to verify denial.
			if _, err := os.ReadFile(paths[1]); !errors.Is(err, os.ErrPermission) {
				t.Errorf("thread escaped filesystem restrictions: %v", err)
			}
			if fd, err := unix.Socket(unix.AF_UNIX, unix.SOCK_STREAM, 0); !errors.Is(err, unix.EPERM) {
				if err == nil {
					unix.Close(fd)
				}
				t.Errorf("thread escaped syscall restrictions: %v", err)
			}
		}()
	}
	wg.Wait()
	fmt.Println("confinement verified")
}

// checkThreadNameAccess runs on a locked OS thread, including threads created
// after re-exec. Process task files must remain inaccessible.
func checkThreadNameAccess(t *testing.T) {
	t.Helper()
	path := fmt.Sprintf("/proc/self/task/%d/comm", unix.Gettid())
	// #nosec G703 -- This path names only the current test thread.
	err := os.WriteFile(path, []byte("sandbox-test"), 0o600)
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("write own thread name = %v, want permission denied", err)
	}
	// #nosec G703 -- Verify that process task files remain unreadable.
	if _, err := os.ReadFile(path); !errors.Is(err, os.ErrPermission) {
		t.Errorf("read own thread name = %v, want permission denied", err)
	}
	parent := os.Getppid()
	path = fmt.Sprintf("/proc/%d/task/%d/comm", parent, parent)
	// #nosec G703 -- Open without truncation so a regression cannot change the parent.
	file, err := os.OpenFile(path, os.O_WRONLY, 0)
	if err == nil {
		file.Close()
	}
	if !errors.Is(err, os.ErrPermission) {
		t.Errorf("write parent thread name = %v, want permission denied", err)
	}
}
