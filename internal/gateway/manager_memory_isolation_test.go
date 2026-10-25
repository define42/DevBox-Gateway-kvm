package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"syscall"
	"testing"
)

// Lifecycle helper processes deliberately do not use or modify host cgroups.
// Production always uses linuxVMCgroupFactory; only package tests inject this seam.
type testCgroupFactory struct {
	prepare func(string, string, uint64) (workerCgroup, error)
}

func (testCgroupFactory) Validate(string) error { return nil }

func (f testCgroupFactory) Prepare(root, id string, memory uint64) (workerCgroup, error) {
	if f.prepare != nil {
		return f.prepare(root, id, memory)
	}

	return &testWorkerCgroup{}, nil
}

type testWorkerCgroup struct {
	applyErr, closeErr, cleanupErr error
	applies, closes                atomic.Int32
	cleanups                       atomic.Int32
}

func (c *testWorkerCgroup) Apply(_ *syscall.SysProcAttr) error {
	c.applies.Add(1)

	return c.applyErr
}

func (c *testWorkerCgroup) CloseFD() error {
	c.closes.Add(1)

	return c.closeErr
}

func (c *testWorkerCgroup) Cleanup() error {
	c.cleanups.Add(1)

	return c.cleanupErr
}

func TestManagerRequiresCgroupIsolation(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	m.cgroups = linuxVMCgroupFactory{}
	vm := createManagerVM(t, m, "alice", "desktop")
	if err := m.Start(t.Context(), vm.Owner, vm.ID); !errors.Is(err, ErrCgroupUnavailable) {
		t.Fatalf("unconfigured worker isolation error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(m.directory(vm.ID), "helper.pid")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unconfined child was started: %v", err)
	}
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active != 0 {
		t.Fatalf("failed isolation retained %d running slots", active)
	}
}

func TestManagerRejectsUnsafeConfiguredCgroupRoot(t *testing.T) {
	t.Parallel()
	config := managerTestConfig(t)
	config.CgroupRoot = t.TempDir()
	if manager, err := NewManager(config); !errors.Is(err, ErrCgroupUnavailable) {
		if manager != nil {
			_ = manager.Close()
		}
		t.Fatalf("ordinary directory accepted as cgroup root: %v", err)
	}
	config.CgroupRoot = ""
	manager, err := NewManager(config)
	if err != nil {
		t.Fatalf("failed cgroup verification retained state lock: %v", err)
	}
	if err := manager.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestManagerCgroupCleanupFailureBlocksNewWorkers(t *testing.T) {
	t.Parallel()
	for _, failLaunch := range []bool{false, true} {
		name := "after worker exit"
		if failLaunch {
			name = "during failed launch"
		}
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			manager, err := NewManager(managerTestConfig(t))
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = manager.Close() })
			manager.workerBinary = managerTestWorker(t, "graceful")
			group := &testWorkerCgroup{cleanupErr: syscall.EBUSY}
			if failLaunch {
				group.applyErr = os.ErrPermission
			}
			manager.cgroups = testCgroupFactory{prepare: func(string, string, uint64) (workerCgroup, error) {
				return group, nil
			}}
			manager.readMemory = func() (hostMemory, error) {
				return hostMemory{Total: 64 << 30, Available: 32 << 30}, nil
			}
			vm := createManagerVM(t, manager, "alice", "desktop")
			err = manager.Start(t.Context(), vm.Owner, vm.ID)
			if !failLaunch {
				if err != nil {
					t.Fatal(err)
				}
				err = manager.Stop(t.Context(), vm.Owner, vm.ID)
			}
			if !errors.Is(err, errWorkerCleanup) {
				t.Fatalf("cleanup failure = %v", err)
			}
			manager.mu.Lock()
			active := manager.active
			manager.mu.Unlock()
			if active != 1 {
				t.Fatalf("unverified cleanup released memory reservation: active=%d", active)
			}
			other := createManagerVM(t, manager, "bob", "other")
			if err := manager.Start(t.Context(), other.Owner, other.ID); !errors.Is(err, ErrCgroupUnavailable) {
				t.Fatalf("start after unverified cleanup = %v", err)
			}
			if err := manager.Close(); !errors.Is(err, errWorkerCleanup) {
				t.Fatalf("shutdown omitted cleanup failure: %v", err)
			}
		})
	}
}

func TestManagerCgroupLifecycle(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"success", "apply failure", "exec failure", "close failure"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			group := &testWorkerCgroup{}
			m.config.MemoryOverheadMiB = 768
			m.config.CgroupRoot = "/delegated/workers"
			vm := createManagerVM(t, m, "alice", "desktop")
			m.cgroups = testCgroupFactory{prepare: func(root, id string, memory uint64) (workerCgroup, error) {
				if root != m.config.CgroupRoot || id != vm.ID || memory != uint64(m.config.MemoryMiB+768)<<20 {
					t.Errorf("cgroup request = %q, %q, %d", root, id, memory)
				}

				return group, nil
			}}
			switch name {
			case "apply failure":
				group.applyErr = os.ErrPermission
			case "exec failure":
				m.workerBinary = filepath.Join(t.TempDir(), "missing-worker")
			case "close failure":
				group.closeErr = os.ErrInvalid
			}
			err := m.Start(t.Context(), vm.Owner, vm.ID)
			if name == "exec failure" && errors.Is(err, ErrCgroupUnavailable) {
				t.Fatal("ordinary exec failure was misreported as missing cgroup isolation")
			}
			if name == "success" {
				if err != nil {
					t.Fatal(err)
				}
				if group.cleanups.Load() != 0 || group.closes.Load() != 1 {
					t.Fatal("running worker's cgroup was prematurely removed")
				}
				if err := m.Stop(t.Context(), vm.Owner, vm.ID); err != nil {
					t.Fatal(err)
				}
			} else if err == nil {
				t.Fatal("worker isolation failure unexpectedly succeeded")
			}
			if group.applies.Load() != 1 || group.cleanups.Load() != 1 {
				t.Fatalf("cgroup calls: apply=%d cleanup=%d", group.applies.Load(), group.cleanups.Load())
			}
			m.mu.Lock()
			active := m.active
			m.mu.Unlock()
			if active != 0 {
				t.Fatalf("worker exit retained %d running slots", active)
			}
		})
	}
}

func TestWorkerMemoryConfiguration(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name     string
		overhead int
		root     string
		invalid  bool
	}{
		{name: "default"},
		{name: "custom", overhead: 1024, root: "/delegated/workers"},
		{name: "negative", overhead: -1, invalid: true},
		{name: "too small", overhead: 63, invalid: true},
		{name: "too large", overhead: 16385, invalid: true},
		{name: "relative path", root: "workers", invalid: true},
		{name: "unclean path", root: "/delegated/../workers", invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := ManagerConfig{MemoryMiB: 4096, MemoryOverheadMiB: test.overhead, CgroupRoot: test.root}
			err := normalizeWorkerMemoryConfig(&config)
			if test.invalid {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid worker limits error = %v", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if workerMemoryLimit(config) != uint64(4096+config.MemoryOverheadMiB)<<20 {
				t.Fatal("admission and cgroup memory bounds disagree")
			}
		})
	}
}
