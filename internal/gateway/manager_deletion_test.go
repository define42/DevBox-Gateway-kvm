package gateway

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestManagerDeleteCleanupFailureDoesNotBlockRestart(t *testing.T) {
	t.Parallel()
	if os.Geteuid() == 0 {
		t.Skip("requires an unprivileged user to reproduce filesystem permission failures")
	}
	m := newTestManager(t, "graceful")
	victim := createManagerVM(t, m, "alice", "victim")
	healthy := createManagerVM(t, m, "alice", "healthy")
	live := m.directory(victim.ID)
	staged := filepath.Join(filepath.Dir(live), ".deleting-"+victim.ID)
	blocked := filepath.Join(live, "blocked")
	if err := os.Mkdir(blocked, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(blocked, "remaining"), []byte("pending cleanup"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		for _, path := range []string{blocked, filepath.Join(staged, "blocked")} {
			if err := os.Chmod(path, 0o700); err != nil && !errors.Is(err, os.ErrNotExist) {
				t.Error(err)
			}
		}
	})
	if err := os.Chmod(blocked, 0); err != nil {
		t.Fatal(err)
	}
	if err := m.Delete(t.Context(), victim.Owner, victim.ID); !errors.Is(err, os.ErrPermission) {
		t.Fatalf("Delete with inaccessible nested directory = %v, want permission error", err)
	}
	if _, err := m.GetAny(t.Context(), victim.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted VM remains in inventory: %v", err)
	}
	if _, err := os.Lstat(live); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted VM remains at its live path: %v", err)
	}
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("failed cleanup did not retain the deletion directory: %v", err)
	}
	if _, err := os.Stat(filepath.Join(staged, "vm.json")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("permission failure did not exercise partial metadata removal: %v", err)
	}

	reopened := reopenDeletionTestManager(t, m)
	assertDeletionTestInventory(t, reopened, healthy, victim)
	if _, err := os.Stat(staged); err != nil {
		t.Fatalf("persistent cleanup failure did not retain the deletion directory: %v", err)
	}
	if err := os.Chmod(filepath.Join(staged, "blocked"), 0o700); err != nil {
		t.Fatal(err)
	}
	recovered := reopenDeletionTestManager(t, reopened)
	assertDeletionTestInventory(t, recovered, healthy, victim)
	if _, err := os.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("restart did not retry the repaired deletion directory: %v", err)
	}
}

func TestManagerReloadRecoversInterruptedDeletion(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"complete tree", "metadata removed", "empty directory"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			victim := createManagerVM(t, m, "alice", "victim")
			healthy := createManagerVM(t, m, "alice", "healthy")
			live := m.directory(victim.ID)
			staged := filepath.Join(filepath.Dir(live), ".deleting-"+victim.ID)
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			if err := os.Rename(live, staged); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "metadata removed":
				if err := os.Remove(filepath.Join(staged, "vm.json")); err != nil {
					t.Fatal(err)
				}
			case "empty directory":
				if err := os.RemoveAll(staged); err != nil {
					t.Fatal(err)
				}
				if err := os.Mkdir(staged, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			reopened := reopenDeletionTestManager(t, m)
			assertDeletionTestInventory(t, reopened, healthy, victim)
			if _, err := os.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("interrupted deletion remains after restart: %v", err)
			}
		})
	}
}

func TestManagerDeleteRejectsExistingStagingDirectory(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"empty", "nonempty"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			vm := createManagerVM(t, m, "alice", "desktop")
			live := m.directory(vm.ID)
			metadata := filepath.Join(live, "vm.json")
			before, err := os.ReadFile(metadata)
			if err != nil {
				t.Fatal(err)
			}
			staged := filepath.Join(filepath.Dir(live), ".deleting-"+vm.ID)
			if err := os.Mkdir(staged, 0o700); err != nil {
				t.Fatal(err)
			}
			if name == "nonempty" {
				if err := os.WriteFile(filepath.Join(staged, "keep"), []byte("existing state"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := m.Delete(t.Context(), vm.Owner, vm.ID); err == nil {
				t.Fatal("Delete replaced an existing deletion directory")
			}
			loaded, err := m.GetAny(t.Context(), vm.ID)
			if err != nil || loaded.ID != vm.ID || loaded.State != "stopped" {
				t.Fatalf("failed staging changed the live VM: %+v, %v", loaded, err)
			}
			after, err := os.ReadFile(metadata)
			if err != nil || !bytes.Equal(after, before) {
				t.Fatalf("failed staging changed live metadata: %v", err)
			}
			entries, err := os.ReadDir(staged)
			if err != nil {
				t.Fatal(err)
			}
			if name == "empty" && len(entries) != 0 {
				t.Fatalf("existing empty deletion directory changed: %v", entries)
			}
			if name == "nonempty" {
				content, err := os.ReadFile(filepath.Join(staged, "keep"))
				if err != nil || string(content) != "existing state" || len(entries) != 1 {
					t.Fatalf("existing deletion directory changed: %q, %v; entries %v", content, err, entries)
				}
			}
		})
	}
}

func TestManagerReloadDeletionRecoveryRejectsMalformedState(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing metadata", "malformed metadata", "invalid deletion name"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			vm := createManagerVM(t, m, "alice", "desktop")
			metadata := filepath.Join(m.directory(vm.ID), "vm.json")
			invalid := filepath.Join(filepath.Dir(m.directory(vm.ID)), ".deleting-invalid")
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			switch name {
			case "missing metadata":
				if err := os.Remove(metadata); err != nil {
					t.Fatal(err)
				}
			case "malformed metadata":
				if err := os.WriteFile(metadata, []byte("{"), 0o600); err != nil {
					t.Fatal(err)
				}
			case "invalid deletion name":
				if err := os.Mkdir(invalid, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			reopened, err := NewManager(m.config)
			if reopened != nil {
				if closeErr := reopened.Close(); closeErr != nil {
					t.Error(closeErr)
				}
			}
			if err == nil {
				t.Fatal("deletion recovery accepted malformed live state")
			}
			if name == "invalid deletion name" {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid deletion name error = %v, want ErrInvalid", err)
				}
				if _, err := os.Stat(invalid); err != nil {
					t.Fatalf("invalid deletion directory was removed: %v", err)
				}
			}
		})
	}
}

func TestManagerReloadDeletionDoesNotFollowNestedSymlink(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	victim := createManagerVM(t, m, "alice", "victim")
	healthy := createManagerVM(t, m, "alice", "healthy")
	outside := t.TempDir()
	sentinel := filepath.Join(outside, "keep")
	if err := os.WriteFile(sentinel, []byte("outside VM state"), 0o600); err != nil {
		t.Fatal(err)
	}
	live := m.directory(victim.ID)
	if err := os.Symlink(outside, filepath.Join(live, "external")); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	staged := filepath.Join(filepath.Dir(live), ".deleting-"+victim.ID)
	if err := os.Rename(live, staged); err != nil {
		t.Fatal(err)
	}
	reopened := reopenDeletionTestManager(t, m)
	assertDeletionTestInventory(t, reopened, healthy, victim)
	content, err := os.ReadFile(sentinel)
	if err != nil || string(content) != "outside VM state" {
		t.Fatalf("deletion recovery modified a symlink target: %q, %v", content, err)
	}
	if _, err := os.Lstat(staged); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deletion directory containing a symlink remains: %v", err)
	}
}

func reopenDeletionTestManager(t *testing.T, previous *Manager) *Manager {
	t.Helper()
	if err := previous.Close(); err != nil {
		t.Fatal(err)
	}
	m, err := NewManager(previous.config)
	if err != nil {
		t.Fatalf("restart after staged deletion: %v", err)
	}
	t.Cleanup(func() {
		if err := m.Close(); err != nil {
			t.Error(err)
		}
	})

	return m
}

func assertDeletionTestInventory(t *testing.T, m *Manager, healthy, victim VM) {
	t.Helper()
	list, err := m.List(t.Context(), healthy.Owner)
	if err != nil || len(list) != 1 || list[0].ID != healthy.ID || list[0].State != "stopped" {
		t.Fatalf("healthy VM unavailable after deletion recovery: %+v, %v", list, err)
	}
	if _, err := m.GetAny(t.Context(), victim.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("deleted VM reappeared after restart: %v", err)
	}
}
