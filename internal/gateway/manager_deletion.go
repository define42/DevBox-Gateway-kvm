package gateway

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/sys/unix"
)

// vmDeletionFilesystem permits deterministic durability and partial-removal
// failures in tests without changing the production filesystem operations.
type vmDeletionFilesystem interface {
	SyncDirectory(string) error
	RemoveAll(string) error
}

type localVMDeletionFilesystem struct{}

func (localVMDeletionFilesystem) SyncDirectory(path string) error {
	return syncImageDirectory(path)
}

func (localVMDeletionFilesystem) RemoveAll(path string) error {
	//nolint:gosec // Validated deletion staging directory below the private VM root.
	return os.RemoveAll(path)
}

// Delete removes an owned, stopped desktop from inventory before reclaiming its
// files. Cleanup errors may leave a staging directory for startup to retry.
func (m *Manager) Delete(ctx context.Context, owner, id string) error {
	ctx, finish, err := m.operation(ctx)
	if err != nil {
		return err
	}
	defer finish()

	v, err := m.lookup(owner, id)
	if err != nil {
		return err
	}
	if err := v.lock(ctx); err != nil {
		return err
	}
	defer v.unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	if v.run != nil || v.record.State != "stopped" {
		return ErrConflict
	}
	dir := m.directory(id)
	if err := checkPrivateDirectory(dir); err != nil {
		return err
	}
	staging := filepath.Join(filepath.Dir(dir), ".deleting-"+id)
	if err := unix.Renameat2(unix.AT_FDCWD, dir, unix.AT_FDCWD, staging, unix.RENAME_NOREPLACE); err != nil {
		return fmt.Errorf("stage desktop deletion: %w", err)
	}
	// Once renamed, no queued lifecycle operation may use the old path, even
	// if syncing the rename or reclaiming files fails.
	m.mu.Lock()
	delete(m.vms, id)
	m.mu.Unlock()
	v.record.State = "deleted"
	if err := m.cleanupDeletedVM(staging); err != nil {
		return fmt.Errorf("desktop removed from inventory; file cleanup pending: %w", err)
	}

	return nil
}

func (m *Manager) cleanupDeletedVM(dir string) error {
	if err := checkPrivateDirectory(dir); err != nil {
		return err
	}
	files := m.deletionFS
	if files == nil {
		files = localVMDeletionFilesystem{}
	}
	root := filepath.Dir(dir)
	// Persist deletion intent before removing any contents. This also applies
	// during startup: a process restart does not make an earlier rename durable.
	if err := files.SyncDirectory(root); err != nil {
		return fmt.Errorf("persist desktop deletion: %w", err)
	}
	removeErr := files.RemoveAll(dir)

	return errors.Join(removeErr, files.SyncDirectory(root))
}
