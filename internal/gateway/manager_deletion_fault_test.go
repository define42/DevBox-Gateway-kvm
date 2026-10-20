package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestManagerDeletionFilesystemFailures(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		failSync    int
		partial     bool
		wantResidue bool
	}{
		{name: "persist intent", failSync: 1, wantResidue: true},
		{name: "partial removal", partial: true, wantResidue: true},
		{name: "persist removal", failSync: 2},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			victim := createManagerVM(t, m, "alice", "delete-me")
			healthy := createManagerVM(t, m, "bob", "keep-me")
			root := filepath.Dir(m.directory(victim.ID))
			staging := filepath.Join(root, ".deleting-"+victim.ID)
			var syncs, removals int
			intentDurable := false
			files := deletionFaultFilesystem{
				syncDirectory: func(path string) error {
					if path != root {
						t.Fatalf("synced %q instead of VM parent %q", path, root)
					}
					syncs++
					if syncs == test.failSync {
						return syscall.EIO
					}
					if err := syncImageDirectory(path); err != nil {
						return err
					}
					intentDurable = true

					return nil
				},
				removeAll: func(path string) error {
					removals++
					if !intentDurable || path != staging {
						t.Fatalf("removed %q before persisting deletion intent", path)
					}
					if test.partial {
						if err := os.Remove(filepath.Join(path, "vm.json")); err != nil {
							return err
						}

						return syscall.EIO
					}

					return os.RemoveAll(path)
				},
			}
			m.deletionFS = files
			if err := m.Delete(t.Context(), victim.Owner, victim.ID); !errors.Is(err, syscall.EIO) {
				t.Fatalf("Delete = %v, want filesystem failure", err)
			}
			if _, err := m.GetAny(t.Context(), victim.ID); !errors.Is(err, ErrNotFound) {
				t.Fatalf("deleted VM is still visible: %v", err)
			}
			if _, err := os.Stat(m.directory(victim.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("live VM directory survived deletion: %v", err)
			}
			_, err := os.Stat(staging)
			if test.wantResidue && err != nil || !test.wantResidue && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("deletion residue: %v, want present=%t", err, test.wantResidue)
			}
			if test.failSync == 1 {
				if removals != 0 {
					t.Fatal("failed intent sync allowed file removal")
				}
				for _, name := range []string{"vm.json", "disk.img", "seed.iso"} {
					if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
						t.Fatalf("failed intent sync lost %s: %v", name, err)
					}
				}
			}
			if test.failSync != 1 && syncs != 2 {
				t.Fatalf("cleanup did not persist directory changes: syncs=%d", syncs)
			}
			if err := m.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := NewManager(m.config)
			if err != nil {
				t.Fatalf("deletion failure blocked restart: %v", err)
			}
			t.Cleanup(func() {
				if err := reopened.Close(); err != nil {
					t.Error(err)
				}
			})
			all, err := reopened.ListAll(t.Context())
			if err != nil || len(all) != 1 || all[0].ID != healthy.ID {
				t.Fatalf("healthy inventory after restart = %+v, %v", all, err)
			}
			if _, err := os.Stat(staging); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("startup did not finish deletion: %v", err)
			}
		})
	}
}

func TestManagerDeletionRecoverySyncFailurePreservesFiles(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	victim := createManagerVM(t, m, "alice", "delete-me")
	healthy := createManagerVM(t, m, "bob", "keep-me")
	staging := filepath.Join(filepath.Dir(m.directory(victim.ID)), ".deleting-"+victim.ID)
	if err := os.Rename(m.directory(victim.ID), staging); err != nil {
		t.Fatal(err)
	}
	if err := m.Close(); err != nil {
		t.Fatal(err)
	}
	// Exercise the same loader used by NewManager, with a failed startup fsync.
	loader := &Manager{
		config: m.config, vms: make(map[string]*managedVM), images: m.images,
		deletionFS: deletionFaultFilesystem{
			syncDirectory: func(string) error { return syscall.EIO },
			removeAll: func(string) error {
				t.Fatal("startup removed files without persisting deletion intent")

				return nil
			},
		},
	}
	if err := loader.load(); err != nil {
		t.Fatalf("pending deletion blocked healthy inventory: %v", err)
	}
	if len(loader.vms) != 1 || loader.vms[healthy.ID] == nil {
		t.Fatalf("startup loaded %d VMs without the healthy desktop", len(loader.vms))
	}
	for _, name := range []string{"vm.json", "disk.img", "seed.iso"} {
		if _, err := os.Stat(filepath.Join(staging, name)); err != nil {
			t.Fatalf("startup sync failure lost %s: %v", name, err)
		}
	}
}

type deletionFaultFilesystem struct {
	syncDirectory func(string) error
	removeAll     func(string) error
}

func (f deletionFaultFilesystem) SyncDirectory(path string) error {
	return f.syncDirectory(path)
}

func (f deletionFaultFilesystem) RemoveAll(path string) error {
	return f.removeAll(path)
}
