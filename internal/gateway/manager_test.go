package gateway

import (
	"context"
	"crypto/tls"
	"encoding/binary"
	"encoding/json"
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

	bootflag "github.com/define42/DevBox-Gateway-kvm/internal/flag"
	"golang.org/x/sys/unix"
)

func managerTestConfig(t *testing.T, behavior string) ManagerConfig {
	t.Helper()
	dir := t.TempDir()
	binaryPath := filepath.Join(dir, "helper")
	quote := func(s string) string { return "'" + strings.ReplaceAll(s, "'", "'\\''") + "'" }
	script := "#!/bin/sh\nGORACE=atexit_sleep_ms=0 DEVBOX_KVM_MANAGER_HELPER=" + quote(behavior) +
		" exec " + quote(os.Args[0]) +
		" -test.run='^TestManagerHelperProcess$' -- \"$@\"\n"
	//nolint:gosec // The private test helper must be executable by its owner.
	if err := os.WriteFile(binaryPath, []byte(script), 0o700); err != nil {
		t.Fatal(err)
	}
	config := ManagerConfig{
		DataDir: filepath.Join(dir, "state"), Binary: binaryPath,
		BaseImage: filepath.Join(dir, "base.qcow2"), StartTimeout: 3 * time.Second,
		StopTimeout: 100 * time.Millisecond, MaxVMsPerUser: 2, MaxRunning: 1,
		MemoryOverheadMiB: 256,
	}

	base := catalogQCOW2("manager test base")
	binary.BigEndian.PutUint64(base[24:32], 4096)
	if err := os.WriteFile(config.BaseImage, base, 0o600); err != nil {
		t.Fatal(err)
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
	manager.cgroups = testCgroupFactory{}
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

func TestManagerAudioPlayback(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	vm := createManagerVM(t, m, "alice", "desktop")
	for range 2 {
		if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
			t.Fatal(err)
		}
		audio, err := os.ReadFile(filepath.Join(m.directory(vm.ID), "helper.audio"))
		if err != nil {
			t.Fatal(err)
		}
		if string(audio) != "rdp" {
			t.Fatalf("child audio mode = %q, want rdp", audio)
		}
		if err := m.Stop(t.Context(), vm.Owner, vm.ID); err != nil {
			t.Fatal(err)
		}
	}
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
	legacyLog := filepath.Join(m.directory(other.ID), "console.log")
	if err := os.WriteFile(legacyLog, []byte("legacy child output"), 0o600); err != nil {
		t.Fatal(err)
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
	if _, err := os.Stat(legacyLog); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("legacy console log was not removed: %v", err)
	}
	if err := reopened.Delete(t.Context(), "alice", first.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(reopened.directory(first.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted VM state remains: %v", err)
	}
}

func TestManagerReloadWithoutConfiguredBaseImage(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		legacy bool
	}{
		{name: "saved image metadata"},
		{name: "legacy VM", legacy: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			vm := createManagerVM(t, m, "alice", "existing")
			if test.legacy {
				vm.ImageID, vm.ImageName = "", ""
				data, err := json.Marshal(vm)
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(m.directory(vm.ID), "vm.json"), data, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			config := m.config
			if err := os.Remove(config.BaseImage); err != nil {
				t.Fatal(err)
			}
			config.BaseImage = ""
			reopened, err := NewManager(config)
			if err != nil {
				t.Fatalf("reload VM after removing the default template: %v", err)
			}
			defer reopened.Close()
			loaded, err := reopened.GetAny(t.Context(), vm.ID)
			if err != nil || loaded.ImageID != DefaultImageID || loaded.ImageName != vm.ImageName {
				t.Fatalf("VM source metadata = %+v, %v", loaded, err)
			}
			// The helper child does not allocate guest RAM.
			reopened.cgroups = testCgroupFactory{}
			reopened.readMemory = m.readMemory
			reopened.readResident = m.readResident
			if err := reopened.Start(t.Context(), vm.Owner, vm.ID); err != nil {
				t.Fatalf("start an existing VM without its source template: %v", err)
			}
		})
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

func TestManagerRetainsListenerDuringStartup(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "verify-listener")
	vm := createManagerVM(t, m, "alice", "desktop")
	// The child attempts to bind this abstract address after adopting its descriptor.
	// Startup succeeds only if the reserved listener survived the exec handoff.
	if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatal(err)
	}
	endpoint, err := m.RDP(t.Context(), vm.Owner, vm.ID)
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Stop(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatal(err)
	}
	managerAssertAddressAvailable(t, endpoint.Address)
}

func TestManagerStartReportsEarlyExit(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "startup-exit")
	vm := createManagerVM(t, m, "alice", "desktop")
	err := m.Start(t.Context(), vm.Owner, vm.ID)
	if !errors.Is(err, ErrStartupFailed) {
		t.Fatalf("startup exit did not report a startup failure: %v", err)
	}
	if errors.Is(err, ErrConflict) {
		t.Fatalf("startup exit was classified as a state or capacity conflict: %v", err)
	}
	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) || exitErr.ExitCode() != 2 {
		t.Fatalf("startup exit did not preserve exit status 2: %v", err)
	}
	if !strings.Contains(err.Error(), "desktop process exited during startup") ||
		!strings.Contains(err.Error(), "exit status 2") {
		t.Fatalf("startup failure omitted the process exit diagnostic: %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.directory(vm.ID), "console.log")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("startup created a console log: %v", err)
	}
	managerAssertReaped(t, m.directory(vm.ID))
	data, err := os.ReadFile(filepath.Join(m.directory(vm.ID), "helper.address"))
	if err != nil {
		t.Fatal(err)
	}
	managerAssertAddressAvailable(t, string(data))
}

func managerAssertAddressAvailable(t *testing.T, address string) {
	t.Helper()
	if !validRDPBackendAddress(address) {
		t.Fatalf("child received invalid private RDP address %q", address)
	}
	listener, err := net.Listen("unix", address)
	if err != nil {
		t.Fatalf("stopped child retained its listener at %s: %v", address, err)
	}
	if err := listener.Close(); err != nil {
		t.Fatal(err)
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
	behavior := os.Getenv("DEVBOX_KVM_MANAGER_HELPER")
	if behavior == "" {
		return
	}
	args := make(map[string]string)
	for i, arg := range os.Args {
		if strings.HasPrefix(arg, "-") && i+1 < len(os.Args) {
			args[arg] = os.Args[i+1]
		}
	}
	for i, arg := range os.Args {
		if arg != "boot" {
			continue
		}
		boot, _, err := bootflag.ParseArgs(append([]string{"devbox-kvm"}, os.Args[i:]...))
		if err != nil {
			fmt.Fprintln(os.Stderr, "parse helper boot arguments:", err)
			os.Exit(2)
		}
		args["-rdp-h264-qp"] = strconv.Itoa(boot.RDPH264QP)

		break
	}
	dir := filepath.Dir(args["-d"])
	if err := os.WriteFile(filepath.Join(dir, "helper.rdp-h264-qp"), []byte(args["-rdp-h264-qp"]), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write helper QP:", err)
		os.Exit(2)
	}
	signals := make(chan os.Signal, 4)
	signal.Notify(signals, syscall.SIGUSR1, syscall.SIGTERM)
	if err := os.WriteFile(filepath.Join(dir, "helper.pid"), []byte(strconv.Itoa(os.Getpid())), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.audio"), []byte(args["-audio"]), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write helper audio:", err)
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.net-policy"),
		[]byte(args["-net-policy"]+"\n"+args["-net-allow-cidrs"]), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write helper network policy:", err)
		os.Exit(2)
	}
	if err := os.WriteFile(filepath.Join(dir, "helper.disk-policy"),
		[]byte(args["-disk-size-bytes"]), 0o600); err != nil {
		fmt.Fprintln(os.Stderr, "write helper disk policy:", err)
		os.Exit(2)
	}
	if args["-net"] != "user-isolated" {
		fmt.Fprintln(os.Stderr, "manager helper: expected isolated networking")
		os.Exit(2)
	}
	if err := managerHelperListen(args, behavior); err != nil {
		fmt.Fprintln(os.Stderr, "manager helper:", err)
		os.Exit(2)
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

//nolint:err113 // Standalone test helper diagnostics are logged immediately before exit.
func managerHelperListen(args map[string]string, behavior string) error {
	if _, exists := args["-rdp"]; exists {
		return errors.New("manager exposed the private RDP address in worker arguments")
	}
	fd, err := strconv.Atoi(args["-rdp-fd"])
	if err != nil {
		return fmt.Errorf("parse inherited RDP descriptor: %w", err)
	}
	if fd < 3 {
		return fmt.Errorf("invalid inherited RDP descriptor %d", fd)
	}
	file := os.NewFile(uintptr(fd), "rdp-listener")
	if file == nil {
		return errors.New("inherited RDP descriptor is unavailable")
	}
	defer file.Close()
	listener, err := net.FileListener(file)
	if err != nil {
		return fmt.Errorf("adopt inherited RDP listener: %w", err)
	}
	if _, ok := listener.(*net.UnixListener); !ok || !validRDPBackendAddress(listener.Addr().String()) {
		_ = listener.Close()

		return errors.New("inherited RDP listener is not an abstract Unix socket")
	}
	address := listener.Addr().String()
	dir := filepath.Dir(args["-d"])
	if err := os.WriteFile(filepath.Join(dir, "helper.address"), []byte(address), 0o600); err != nil {
		_ = listener.Close()

		return fmt.Errorf("write helper address: %w", err)
	}
	if behavior == "verify-listener" {
		contender, err := net.Listen("unix", address)
		if err == nil {
			_ = contender.Close()
			_ = listener.Close()

			return errors.New("reserved RDP address became available before child startup")
		}
		if !errors.Is(err, syscall.EADDRINUSE) {
			_ = listener.Close()

			return fmt.Errorf("check reserved RDP address: %w", err)
		}
	}
	if behavior == "no-listener" {
		return listener.Close()
	}
	if behavior == "startup-exit" {
		_ = listener.Close()

		return errors.New("forced startup failure")
	}
	certificate, err := tls.LoadX509KeyPair(args["-rdp-cert"], args["-rdp-key"])
	if err != nil {
		_ = listener.Close()

		return fmt.Errorf("load RDP certificate: %w", err)
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

	return nil
}

func TestManagerH264QPWorkerConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		qp   *int
		want int
	}{
		{name: "default", want: 20},
		{name: "minimum", qp: new(0), want: 0},
		{name: "maximum", qp: new(51), want: 51},
		{name: "custom", qp: new(28), want: 28},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t, "graceful")
			config.H264QP = test.qp
			m, err := NewManager(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := m.Close(); err != nil {
					t.Error(err)
				}
			})
			if config.H264QP != nil {
				// Future workers must use the configuration captured at startup.
				*config.H264QP = 52
			}
			m.cgroups = testCgroupFactory{}
			m.readResident = func(int) (uint64, error) { return 0, nil }
			m.readMemory = func() (hostMemory, error) {
				return hostMemory{Total: 64 << 30, Available: 32 << 30}, nil
			}
			vm := createManagerVM(t, m, "alice", "desktop")
			if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
				t.Fatal(err)
			}
			qp, err := os.ReadFile(filepath.Join(m.directory(vm.ID), "helper.rdp-h264-qp"))
			if err != nil {
				t.Fatal(err)
			}
			if string(qp) != strconv.Itoa(test.want) {
				t.Fatalf("child QP = %q, want %d", qp, test.want)
			}
		})
	}
}

func TestManagerRejectsInvalidH264QP(t *testing.T) {
	t.Parallel()
	for _, qp := range []int{-1, 52} {
		t.Run(strconv.Itoa(qp), func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t, "graceful")
			config.H264QP = &qp
			m, err := NewManager(config)
			if m != nil {
				_ = m.Close()
			}
			if !errors.Is(err, ErrInvalid) || !strings.Contains(err.Error(), "H264_QP") {
				t.Fatalf("invalid QP accepted: %v", err)
			}
			if _, err := os.Stat(config.DataDir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("invalid QP caused state-directory creation")
			}
		})
	}
}
