package gateway

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"golang.org/x/sys/unix"
)

func managerTestConfig(t *testing.T, behavior string) ManagerConfig {
	t.Helper()
	dir := t.TempDir()
	binary := filepath.Join(dir, "helper")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nGORACE=atexit_sleep_ms=0 GOKVM_MANAGER_HELPER=" + quote(behavior) +
		" exec " + quote(os.Args[0]) +
		" -test.run='^TestManagerHelperProcess$' -- \"$@\"\n"
	//nolint:gosec // The private test helper must be executable by its owner.
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	config := ManagerConfig{
		DataDir: filepath.Join(dir, "state"), Binary: binary,
		BaseImage: filepath.Join(dir, "base.raw"), Firmware: filepath.Join(dir, "code.fd"),
		VarsTemplate: filepath.Join(dir, "vars.fd"), StartTimeout: 3 * time.Second,
		StopTimeout: 100 * time.Millisecond, MaxVMsPerUser: 2, MaxRunning: 1,
	}
	for _, path := range []string{config.BaseImage, config.Firmware, config.VarsTemplate} {
		if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	return config
}

func newTestManager(t *testing.T, behavior string) *Manager {
	t.Helper()
	manager, err := NewManager(managerTestConfig(t, behavior))
	if err != nil {
		t.Fatal(err)
	}
	// Helper children do not allocate guest RAM; keep lifecycle tests independent
	// of the test host and its cgroup memory limit.
	manager.readResident = func(int) (uint64, error) { return 0, nil }
	manager.readMemory = func() (hostMemory, error) {
		return hostMemory{Total: 64 << 30, Available: 32 << 30}, nil
	}
	t.Cleanup(func() {
		if err := manager.Close(); err != nil {
			t.Error(err)
		}
	})

	return manager
}

func createManagerVM(t *testing.T, manager *Manager, owner, name string) VM {
	t.Helper()
	hash := "$6$testsalt$" + strings.Repeat("a", 85) + "."
	vm, err := manager.Create(t.Context(), owner, name, hash)
	if err != nil {
		t.Fatal(err)
	}

	return vm
}

func TestManagerOwnershipQuotaAndPersistence(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	first := createManagerVM(t, m, "alice", "desktop")
	second := createManagerVM(t, m, "alice", "work")
	other := createManagerVM(t, m, "bob", "desktop")
	hash := "$6$testsalt$" + strings.Repeat("a", 85) + "."
	for _, name := range []string{"desktop", "third"} {
		if _, err := m.Create(t.Context(), "alice", name, hash); !errors.Is(err, ErrConflict) {
			t.Fatalf("duplicate name/quota accepted: %v", err)
		}
	}
	for _, op := range []func(context.Context, string, string) error{m.Start, m.Stop, m.Delete} {
		if err := op(t.Context(), "bob", first.ID); !errors.Is(err, ErrNotFound) {
			t.Fatalf("operation exposed another owner's VM: %v", err)
		}
	}
	if _, err := m.RDP(t.Context(), "bob", first.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("RDP exposed another owner's VM: %v", err)
	}
	list, err := m.List(t.Context(), "alice")
	if err != nil || len(list) != 2 || list[0].ID != first.ID || list[1].ID != second.ID {
		t.Fatalf("owned VM list = %+v, %v", list, err)
	}
	if _, err := NewManager(m.config); err == nil {
		t.Fatal("second manager acquired an active state directory")
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(m.config)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	list, err = reopened.List(t.Context(), "bob")
	if err != nil || len(list) != 1 || list[0].ID != other.ID || list[0].State != "stopped" {
		t.Fatalf("saved VM did not reload: %+v, %v", list, err)
	}
	if err := reopened.Delete(t.Context(), "alice", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reopened.directory(first.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted VM state remains: %v", err)
	}
}

func TestManagerCloseReleasesDuplicatedLock(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	// A child between fork and exec holds the same open file description even
	// when the descriptor is close-on-exec. Keep that reference alive explicitly.
	inherited, err := unix.FcntlInt(m.lock.Fd(), unix.F_DUPFD_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Close(inherited); err != nil {
			t.Error(err)
		}
	})
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := NewManager(m.config)
	if err != nil {
		t.Fatalf("closed manager retained its state lock through an inherited descriptor: %v", err)
	}
	t.Cleanup(func() {
		if err := reopened.Close(); err != nil {
			t.Error(err)
		}
	})
}

func TestManagerConcurrentStartLimitAndStop(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	first := createManagerVM(t, m, "alice", "first")
	second := createManagerVM(t, m, "bob", "second")
	type result struct {
		vm  VM
		err error
	}
	results := make(chan result, 2)
	for _, vm := range []VM{first, second} {
		go func() { results <- result{vm, m.Start(t.Context(), vm.Owner, vm.ID)} }()
	}
	var running VM
	for range 2 {
		result := <-results
		if result.err == nil {
			if running.ID != "" {
				t.Fatal("running quota allowed two children")
			}
			running = result.vm
		} else if !errors.Is(result.err, ErrConflict) {
			t.Fatal(result.err)
		}
	}
	if running.ID == "" {
		t.Fatal("neither desktop started")
	}
	endpoint, err := m.RDP(t.Context(), running.Owner, running.ID)
	if err != nil {
		t.Fatal(err)
	}
	endpoint.CertificatePEM[0] = 'x'
	again, err := m.RDP(t.Context(), running.Owner, running.ID)
	if err != nil || again.CertificatePEM[0] == 'x' {
		t.Fatal("caller could mutate the stored TLS pin")
	}
	if err := m.Delete(t.Context(), running.Owner, running.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("running desktop was deletable: %v", err)
	}
	if err := m.Stop(t.Context(), running.Owner, running.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := m.RDP(t.Context(), running.Owner, running.ID); !errors.Is(err, ErrConflict) {
		t.Fatalf("stopped desktop retained endpoint: %v", err)
	}
}

func TestManagerStopTimeoutDoesNotKill(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "ignore-acpi")
	vm := createManagerVM(t, m, "alice", "desktop")
	if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(t.Context(), vm.Owner, vm.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("unresponsive guest Stop returned %v", err)
	}
	if _, err := m.RDP(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatalf("normal Stop killed or detached an unresponsive guest: %v", err)
	}
	var group sync.WaitGroup
	for range 3 {
		group.Go(func() {
			if err := m.Close(); err != nil {
				t.Error(err)
			}
		})
	}
	group.Wait()
	if m.active != 0 {
		t.Fatalf("Close returned with %d active children", m.active)
	}
}

func TestManagerFailedStartupIsReaped(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "no-listener")
	m.config.StartTimeout = 500 * time.Millisecond
	vm := createManagerVM(t, m, "alice", "desktop")
	start := time.Now()
	if err := m.Start(t.Context(), vm.Owner, vm.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("startup timeout error = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatal("failed startup cleanup was unbounded")
	}
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active != 0 {
		t.Fatalf("startup failure leaked %d active children", active)
	}
	managerAssertReaped(t, m.directory(vm.ID))
}

func TestManagerCloseCancelsStartup(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "no-listener")
	vm := createManagerVM(t, m, "alice", "desktop")
	started := make(chan error, 1)
	go func() { started <- m.Start(t.Context(), vm.Owner, vm.ID) }()
	managerWaitFile(t, filepath.Join(m.directory(vm.ID), "helper.pid"))
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("Close did not cancel startup: %v", err)
	}
	managerAssertReaped(t, m.directory(vm.ID))
}

func TestManagerCloseBoundsUnresponsiveChild(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "stubborn")
	vm := createManagerVM(t, m, "alice", "desktop")
	if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	if time.Since(started) > 3*time.Second {
		t.Fatal("Close did not bound forced child cleanup")
	}
	managerAssertReaped(t, m.directory(vm.ID))
}

func TestManagerStopReportsUnexpectedExit(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "bad-exit")
	vm := createManagerVM(t, m, "alice", "desktop")
	if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatal(err)
	}
	var exitErr *exec.ExitError
	if err := m.Stop(t.Context(), vm.Owner, vm.ID); !errors.As(err, &exitErr) {
		t.Fatalf("unexpected process exit was reported as graceful: %v", err)
	}
	list, err := m.List(t.Context(), vm.Owner)
	if err != nil || len(list) != 1 || list[0].Error == "" || list[0].State != "stopped" {
		t.Fatalf("unexpected exit state = %+v, %v", list, err)
	}
}

func TestManagerRejectsUnsafeState(t *testing.T) {
	t.Parallel()
	for _, kind := range []string{
		"public directory", "lock symlink", "VM directory symlink", "metadata symlink", "metadata FIFO",
	} {
		t.Run(kind, func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t, "graceful")
			m, err := NewManager(config)
			if err != nil {
				t.Fatal(err)
			}
			vm := createManagerVM(t, m, "alice", "desktop")
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			switch kind {
			case "public directory":
				err = os.Chmod(config.DataDir, 0o755)
			case "lock symlink":
				path := filepath.Join(config.DataDir, "manager.lock")
				if err = os.Remove(path); err == nil {
					err = os.Symlink(config.BaseImage, path)
				}
			case "VM directory symlink":
				path := filepath.Join(config.DataDir, "vms")
				if err = os.Rename(path, path+"-elsewhere"); err == nil {
					err = os.Symlink(path+"-elsewhere", path)
				}
			case "metadata symlink", "metadata FIFO":
				path := filepath.Join(m.directory(vm.ID), "vm.json")
				if err = os.Remove(path); err == nil {
					if kind == "metadata symlink" {
						err = os.Symlink(config.BaseImage, path)
					} else {
						err = syscall.Mkfifo(path, 0o600)
					}
				}
			}
			if err != nil {
				t.Fatal(err)
			}
			if unsafe, err := NewManager(config); err == nil {
				_ = unsafe.Close()
				t.Fatal("unsafe persisted state was accepted")
			}
		})
	}
}

func managerWaitFile(t *testing.T, path string) {
	t.Helper()
	deadline := time.NewTimer(3 * time.Second)
	defer deadline.Stop()
	tick := time.NewTicker(10 * time.Millisecond)
	defer tick.Stop()
	for {
		if _, err := os.Stat(path); err == nil {
			return
		}
		select {
		case <-deadline.C:
			t.Fatal("helper process did not initialize")
		case <-tick.C:
		}
	}
}

func managerAssertReaped(t *testing.T, dir string) {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "helper.pid"))
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.Kill(pid, 0); !errors.Is(err, syscall.ESRCH) {
		t.Fatalf("child PID %d still exists: %v", pid, err)
	}
}

// TestManagerHelperProcess runs only in exec'd test children. It provides the
// actual pinned RDP/TLS startup exchange and distinct shutdown failure modes.
func TestManagerHelperProcess(t *testing.T) { //nolint:paralleltest // Runs a standalone signal-driven subprocess.
	behavior := os.Getenv("GOKVM_MANAGER_HELPER")
	if behavior == "" {
		return
	}
	args := make(map[string]string)
	for i, arg := range os.Args {
		if strings.HasPrefix(arg, "-") && i+1 < len(os.Args) {
			args[arg] = os.Args[i+1]
		}
	}
	dir := filepath.Dir(args["-d"])
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM)
	if err := os.WriteFile(filepath.Join(dir, "helper.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if args["-net"] != "user-isolated" {
		os.Exit(2)
	}
	if behavior != "no-listener" {
		managerHelperListen(args)
	}
	for received := range signals {
		if behavior == "no-listener" || behavior == "stubborn" ||
			(behavior == "ignore-acpi" && received == syscall.SIGUSR1) {
			continue
		}
		if behavior == "bad-exit" {
			os.Exit(7)
		}
		os.Exit(0)
	}
}

func managerHelperListen(args map[string]string) {
	certificate, err := tls.LoadX509KeyPair(args["-rdp-cert"], args["-rdp-key"])
	if err != nil {
		os.Exit(2)
	}
	listener, err := net.Listen("tcp", args["-rdp"])
	if err != nil {
		os.Exit(2)
	}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer conn.Close()
				_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
				if _, err := readConnectionPacket(conn); err != nil {
					return
				}
				if err := writePacket(conn, connectionConfirm(2)); err != nil {
					return
				}
				secure := tls.Server(conn, &tls.Config{Certificates: []tls.Certificate{certificate}, MinVersion: tls.VersionTLS12})
				_, _ = io.Copy(io.Discard, secure)
			}()
		}
	}()
}
