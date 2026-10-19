package gateway

import (
	"errors"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func profilingTestDirectory(t *testing.T) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "gwprof-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(dir); err != nil {
			t.Error(err)
		}
	})

	return dir
}

func TestManagerProfilingLifecycle(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		enabled bool
	}{
		{name: "disabled"},
		{name: "enabled", enabled: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t, "graceful")
			config.DataDir = profilingTestDirectory(t)
			config.VMProfiling = test.enabled
			m, err := NewManager(config)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = m.Close() })
			m.cgroups = testCgroupFactory{}
			m.readResident = func(int) (uint64, error) { return 0, nil }
			m.readMemory = func() (hostMemory, error) {
				return hostMemory{Total: 64 << 30, Available: 32 << 30}, nil
			}
			vm := createManagerVM(t, m, "alice", "desktop")
			var previous string
			for range 2 {
				if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
					t.Fatal(err)
				}
				v, err := m.lookup(vm.Owner, vm.ID)
				if err != nil {
					t.Fatal(err)
				}
				run := v.view.Load().run
				index := slices.Index(run.cmd.Args, "-profile-socket")
				if (index >= 0) != test.enabled || (run.profiling != nil) != test.enabled {
					t.Fatalf("profiling enabled=%t, args=%v", test.enabled, run.cmd.Args)
				}
				var directory string
				if test.enabled {
					directory = run.profiling.directory
					if directory == previous || filepath.Dir(directory) != m.directory(vm.ID) {
						t.Fatalf("unexpected per-launch directory: %q", directory)
					}
					if run.cmd.Args[index+1] != filepath.Join(directory, "cpu.sock") {
						t.Fatal("child did not receive its own socket path")
					}
					if err := checkPrivateDirectory(directory); err != nil {
						t.Fatal(err)
					}
					previous = directory
				}
				if err := m.Stop(t.Context(), vm.Owner, vm.ID); err != nil {
					t.Fatal(err)
				}
				if test.enabled {
					if _, err := os.Lstat(directory); !errors.Is(err, os.ErrNotExist) {
						t.Fatalf("stopped child retained profiling directory: %v", err)
					}
				}
			}
		})
	}
}

func TestManagerProfilingLaunchFailureCleanup(t *testing.T) {
	t.Parallel()
	config := managerTestConfig(t, "graceful")
	config.DataDir = profilingTestDirectory(t)
	config.VMProfiling = true
	m, err := NewManager(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = m.Close() })
	m.readMemory = func() (hostMemory, error) {
		return hostMemory{Total: 64 << 30, Available: 32 << 30}, nil
	}
	vm := createManagerVM(t, m, "alice", "desktop")
	m.config.Binary = "/nonexistent-vm-worker"
	if err := m.Start(t.Context(), vm.Owner, vm.ID); err == nil {
		t.Fatal("invalid child executable unexpectedly started")
	}
	entries, err := os.ReadDir(m.directory(vm.ID))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), "profile-") {
			t.Fatalf("failed launch left %s", entry.Name())
		}
	}
}

func TestPrepareProfilingUnavailable(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing directory", "path too long"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := &Manager{config: ManagerConfig{VMProfiling: true}}
			path := filepath.Join(t.TempDir(), strings.Repeat("long", 30))
			if name == "path too long" {
				if err := os.Mkdir(path, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			if profile := m.prepareProfiling(path); profile != nil {
				profile.close()
				t.Fatal("unusable profile path accepted")
			}
			if name == "path too long" {
				entries, err := os.ReadDir(path)
				if err != nil || len(entries) != 0 {
					t.Fatalf("unusable profiling directory retained: %v, %v", entries, err)
				}
			}
		})
	}
}

func TestProcessProfilingCleanup(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"empty directory", "stale socket", "regular file", "symlink", "replaced directory"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := &Manager{config: ManagerConfig{VMProfiling: true}}
			profile := m.prepareProfiling(profilingTestDirectory(t))
			if profile == nil {
				t.Fatal("profiling directory unavailable")
			}
			switch name {
			case "stale socket":
				listener, err := net.ListenUnix("unix", &net.UnixAddr{Name: profile.socket, Net: "unix"})
				if err != nil {
					t.Fatal(err)
				}
				listener.SetUnlinkOnClose(false)
				if err := os.Chmod(profile.socket, 0o600); err != nil {
					t.Fatal(err)
				}
				if err := listener.Close(); err != nil {
					t.Fatal(err)
				}
			case "regular file":
				if err := os.WriteFile(profile.socket, []byte("preserve"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink("target", profile.socket); err != nil {
					t.Fatal(err)
				}
			case "replaced directory":
				if err := os.Rename(profile.directory, profile.directory+"-old"); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(profile.directory, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			profile.close()
			profile.close()
			_, err := os.Lstat(profile.directory)
			if name == "empty directory" || name == "stale socket" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("owned directory not removed: %v", err)
				}
			} else if err != nil {
				t.Fatalf("cleanup removed unrelated state: %v", err)
			}
		})
	}
}
