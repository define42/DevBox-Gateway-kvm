package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestManagerInventoryAndEndpointRemainAvailableDuringCreation(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	running := createManagerVM(t, m, "alice", "running")
	if err := m.Start(t.Context(), running.Owner, running.ID); err != nil {
		t.Fatal(err)
	}
	creation := pauseManagerCreation(t, m, "alice", DefaultImageID)
	var list []VM
	managerComplete(t, func() error {
		var err error
		list, err = m.List(t.Context(), "alice")

		return err
	})
	if len(list) != 2 || list[0].State != "running" || list[1].State != "creating" {
		t.Fatalf("visible states during clone = %+v", list)
	}
	managerComplete(t, func() error {
		all, err := m.ListAll(t.Context())
		if err == nil && len(all) != 2 {
			return fmt.Errorf("inventory has %d VMs: %w", len(all), ErrInvalid)
		}

		return err
	})
	managerComplete(t, func() error {
		vm, err := m.GetAny(t.Context(), list[1].ID)
		if err == nil && vm.State != "creating" {
			return fmt.Errorf("creation state = %q: %w", vm.State, ErrInvalid)
		}

		return err
	})
	managerComplete(t, func() error {
		_, err := m.RDP(t.Context(), running.Owner, running.ID)

		return err
	})
	creation.release()
	created := awaitManagerCreation(t, creation.done)
	if created.err != nil {
		t.Fatal(created.err)
	}
	vm, err := m.GetAny(t.Context(), created.vm.ID)
	if err != nil || vm.State != "stopped" {
		t.Fatalf("completed creation state = %+v, %v", vm, err)
	}
}

func TestManagerLifecycleWaitHonorsCancellation(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	creation := pauseManagerCreation(t, m, "alice", DefaultImageID)
	list, err := m.List(t.Context(), "alice")
	if err != nil || len(list) != 1 {
		t.Fatalf("creation inventory = %+v, %v", list, err)
	}
	for _, test := range []struct {
		name string
		op   func(context.Context, string, string) error
	}{
		{name: "start", op: m.Start},
		{name: "stop", op: m.Stop},
		{name: "delete", op: m.Delete},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
			defer cancel()
			managerComplete(t, func() error {
				err := test.op(ctx, "alice", list[0].ID)
				if !errors.Is(err, context.DeadlineExceeded) {
					return fmt.Errorf("unexpected lifecycle wait result: %w", errors.Join(err, ErrInvalid))
				}

				return nil
			})
		})
	}
	// Parent cleanup releases the clone after parallel subtests finish.
	t.Cleanup(func() {
		creation.release()
		if result := awaitManagerCreation(t, creation.done); result.err != nil && !errors.Is(result.err, context.Canceled) {
			t.Error(result.err)
		}
	})
}

func TestManagerCreationLimit(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name        string
		limit, want int
		invalid     bool
	}{
		{name: "default", want: 2},
		{name: "configured", limit: 1, want: 1},
		{name: "negative", limit: -1, invalid: true},
		{name: "too large", limit: 65, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := managerTestConfig(t, "graceful")
			config.MaxCreations = test.limit
			m, err := NewManager(config)
			if test.invalid {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid creation limit returned %v", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer m.Close()
			if got := cap(m.creations); got != test.want {
				t.Fatalf("creation capacity = %d, want %d", got, test.want)
			}
		})
	}
}

func TestManagerCreationAdmissionBoundsWorkAndCancelsQueue(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	for slot := range cap(m.creations) {
		pauseManagerCreation(t, m, fmt.Sprintf("user%d", slot), DefaultImageID)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	progress := make(chan struct{}, 1)
	done := make(chan managerCreationResult, 1)
	go func() {
		vm, err := m.CreateWithProgress(ctx, "queued", "waiting", managerTestPasswordHash(), DefaultImageID,
			func(CreateProgress) {
				select {
				case progress <- struct{}{}:
				default:
				}
			})
		done <- managerCreationResult{vm: vm, err: err}
	}()
	waitManagerCreationVisible(t, m, "queued")
	select {
	case <-progress:
		t.Fatal("queued creation performed provisioning work beyond the global limit")
	default:
	}
	cancel()
	if result := awaitManagerCreation(t, done); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("queued creation cancellation = %v", result.err)
	}
	list, err := m.List(t.Context(), "queued")
	if err != nil || len(list) != 0 {
		t.Fatalf("canceled queued creation retained its reservation: %+v, %v", list, err)
	}
	if len(m.creations) != cap(m.creations) {
		t.Fatal("canceling a queued request released another creation's slot")
	}
}

func TestManagerShutdownCancelsQueuedCreations(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	m.config.StopTimeout = time.Second
	var paused []pausedManagerCreation
	for slot := range cap(m.creations) {
		paused = append(paused, pauseManagerCreation(t, m, fmt.Sprintf("user%d", slot), DefaultImageID))
	}
	done := make(chan managerCreationResult, 1)
	go func() {
		vm, err := m.Create(t.Context(), "queued", "waiting", managerTestPasswordHash())
		done <- managerCreationResult{vm: vm, err: err}
	}()
	waitManagerCreationVisible(t, m, "queued")
	closed := make(chan error, 1)
	go func() { closed <- m.Close() }()
	if result := awaitManagerCreation(t, done); !errors.Is(result.err, context.Canceled) {
		t.Fatalf("shutdown did not cancel queued creation: %v", result.err)
	}
	for _, creation := range paused {
		creation.release()
		if result := awaitManagerCreation(t, creation.done); !errors.Is(result.err, context.Canceled) {
			t.Fatalf("shutdown did not cancel active creation: %v", result.err)
		}
	}
	select {
	case err := <-closed:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("manager shutdown retained creation workers")
	}
	if len(m.creations) != 0 {
		t.Fatal("manager shutdown retained creation slots")
	}
}

func TestManagerImageDeletionPreservesPinnedClone(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	contents := catalogQCOW2("pinned image contents")
	image, err := m.ImportImage(t.Context(), "selected.qcow2", bytes.NewReader(contents))
	if err != nil {
		t.Fatal(err)
	}
	creation := pauseManagerCreation(t, m, "alice", image.ID)
	managerComplete(t, func() error { return m.DeleteImage(t.Context(), image.ID) })
	managerComplete(t, func() error {
		images, err := m.Images(t.Context())
		if err == nil && (len(images) != 1 || images[0].ID != DefaultImageID) {
			return fmt.Errorf("catalog after deletion = %+v: %w", images, ErrInvalid)
		}

		return err
	})
	managerComplete(t, func() error {
		_, err := m.ImportImage(t.Context(), "unrelated.qcow2", bytes.NewReader(catalogQCOW2("unrelated")))

		return err
	})
	if _, err := os.Stat(m.imageDirectory(image.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("deleted catalog directory remains: %v", err)
	}
	creation.release()
	created := awaitManagerCreation(t, creation.done)
	if created.err != nil {
		t.Fatalf("deleting template interrupted its pinned clone: %v", created.err)
	}
	copied, err := os.ReadFile(filepath.Join(m.directory(created.vm.ID), "disk.img"))
	if err != nil || !bytes.Equal(copied, contents) {
		t.Fatalf("pinned clone content changed after catalog deletion: %v", err)
	}
}

type managerCreationResult struct {
	vm  VM
	err error
}

type pausedManagerCreation struct {
	release func()
	done    <-chan managerCreationResult
}

func managerTestPasswordHash() string { return "$6$testsalt$" + strings.Repeat("a", 85) + "." }

func pauseManagerCreation(t *testing.T, m *Manager, owner, imageID string) pausedManagerCreation {
	t.Helper()
	paused := make(chan struct{})
	released := make(chan struct{})
	done := make(chan managerCreationResult, 1)
	var once sync.Once
	release := func() { once.Do(func() { close(released) }) }
	t.Cleanup(release)
	go func() {
		vm, err := m.CreateWithProgress(t.Context(), owner, "copying", managerTestPasswordHash(), imageID,
			func(progress CreateProgress) {
				if progress.Phase == "copying" && progress.CopiedBytes == 0 {
					close(paused)
					<-released
				}
			})
		done <- managerCreationResult{vm: vm, err: err}
	}()
	select {
	case <-paused:
	case result := <-done:
		t.Fatalf("creation ended before copying: %v", result.err)
	case <-time.After(3 * time.Second):
		t.Fatal("creation did not reach copying")
	}

	return pausedManagerCreation{release: release, done: done}
}

func awaitManagerCreation(t *testing.T, done <-chan managerCreationResult) managerCreationResult {
	t.Helper()
	select {
	case result := <-done:
		return result
	case <-time.After(3 * time.Second):
		t.Fatal("creation did not finish")

		return managerCreationResult{}
	}
}

func managerComplete(t *testing.T, operation func() error) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- operation() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("operation waited for unrelated provisioning")
	}
}

func waitManagerCreationVisible(t *testing.T, m *Manager, owner string) {
	t.Helper()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()
	for {
		list, err := m.List(t.Context(), owner)
		if err != nil {
			t.Fatal(err)
		}
		if len(list) == 1 && list[0].State == "creating" {
			return
		}
		select {
		case <-ticker.C:
		case <-timer.C:
			t.Fatal("queued creation was not visible")
		}
	}
}
