package gateway

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestManagerMemoryAdmission(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name      string
		available uint64
		budgetMiB int
		readErr   error
		wantErr   error
	}{
		{name: "automatic exact capacity", available: 1536 << 20},
		{name: "automatic one byte short", available: (1536 << 20) - 1, wantErr: ErrMemoryCapacity},
		{name: "headroom only", available: 1024 << 20, wantErr: ErrMemoryCapacity},
		{name: "less than headroom", available: 512 << 20, wantErr: ErrMemoryCapacity},
		{name: "explicit exact capacity", available: 4 << 30, budgetMiB: 512},
		{name: "explicit too small", available: 4 << 30, budgetMiB: 511, wantErr: ErrMemoryCapacity},
		{name: "explicit cannot override host", available: 1 << 30, budgetMiB: 8192, wantErr: ErrMemoryCapacity},
		{name: "memory read failure", readErr: os.ErrPermission, wantErr: errMemoryCheck},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			m.config.MemoryMiB, m.config.MemoryBudgetMiB = 256, tt.budgetMiB
			m.readMemory = func() (hostMemory, error) {
				return hostMemory{Total: 8 << 30, Available: tt.available}, tt.readErr
			}
			vm := createManagerVM(t, m, "alice", "desktop")
			err := m.Start(t.Context(), vm.Owner, vm.ID)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Start = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				if _, err := os.Stat(filepath.Join(m.directory(vm.ID), "helper.pid")); !errors.Is(err, os.ErrNotExist) {
					t.Fatalf("rejected start reached child launch: %v", err)
				}
				vms, err := m.List(t.Context(), vm.Owner)
				if err != nil || len(vms) != 1 || vms[0].State != "stopped" {
					t.Fatalf("rejected start changed VM state: %+v, %v", vms, err)
				}
			}
		})
	}
}

func TestManagerConcurrentMemoryReservations(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	m.config.MemoryMiB, m.config.MemoryBudgetMiB, m.config.MaxRunning = 256, 512, 4
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
	var running, rejected VM
	for range 2 {
		got := <-results
		switch {
		case got.err == nil:
			if running.ID != "" {
				t.Fatal("memory budget allowed two children")
			}
			running = got.vm
		case errors.Is(got.err, ErrMemoryCapacity):
			rejected = got.vm
		default:
			t.Fatal(got.err)
		}
	}
	if running.ID == "" || rejected.ID == "" {
		t.Fatal("expected one running and one rejected desktop")
	}
	if err := m.Stop(t.Context(), running.Owner, running.ID); err != nil {
		t.Fatal(err)
	}
	if err := m.Start(t.Context(), rejected.Owner, rejected.ID); err != nil {
		t.Fatalf("child exit did not release memory reservation: %v", err)
	}
}

func TestManagerMemoryPressureAndIdleRefresh(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	m.config.MemoryMiB, m.config.MaxRunning = 256, 4
	available, total := uint64(2<<30), uint64(8<<30)
	m.readMemory = func() (hostMemory, error) {
		return hostMemory{Total: total, Available: available}, nil
	}
	first := createManagerVM(t, m, "alice", "first")
	second := createManagerVM(t, m, "bob", "second")
	third := createManagerVM(t, m, "carol", "third")
	if err := m.Start(t.Context(), first.Owner, first.ID); err != nil {
		t.Fatal(err)
	}
	available = 1400 << 20
	if err := m.Start(t.Context(), second.Owner, second.ID); !errors.Is(err, ErrMemoryCapacity) {
		t.Fatalf("current host pressure ignored: %v", err)
	}
	available = 2 << 30
	total = 1400 << 20
	if err := m.Start(t.Context(), second.Owner, second.ID); !errors.Is(err, ErrMemoryCapacity) {
		t.Fatalf("reduced effective memory limit ignored: %v", err)
	}
	total = 8 << 30
	if err := m.Start(t.Context(), second.Owner, second.ID); err != nil {
		t.Fatal(err)
	}
	// Neither helper touches guest RAM. Its reservation must still consume the
	// original budget even when the host reports more available memory later.
	available = 8 << 30
	if err := m.Start(t.Context(), third.Owner, third.ID); !errors.Is(err, ErrMemoryCapacity) {
		t.Fatalf("untouched guest pages bypassed reservation: %v", err)
	}
	for _, vm := range []VM{first, second} {
		if err := m.Stop(t.Context(), vm.Owner, vm.ID); err != nil {
			t.Fatal(err)
		}
	}
	// An idle gateway refreshes its automatic budget from current availability.
	for _, vm := range []VM{first, second, third} {
		if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
			t.Fatalf("idle budget was not refreshed: %v", err)
		}
	}
}

func TestManagerFailedLaunchReleasesMemory(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "graceful")
	m.config.MemoryMiB, m.config.MemoryBudgetMiB = 256, 512
	vm := createManagerVM(t, m, "alice", "desktop")
	binary := m.workerBinary
	m.workerBinary = filepath.Join(t.TempDir(), "missing")
	if err := m.Start(t.Context(), vm.Owner, vm.ID); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected executable launch failure, got %v", err)
	}
	m.workerBinary = binary
	if err := m.Start(t.Context(), vm.Owner, vm.ID); err != nil {
		t.Fatalf("failed launch leaked reservation: %v", err)
	}
}

func TestManagerStartupReservesMemoryUntilExit(t *testing.T) {
	t.Parallel()
	m := newTestManager(t, "no-listener")
	m.config.MemoryMiB, m.config.MemoryBudgetMiB, m.config.MaxRunning = 256, 512, 4
	first := createManagerVM(t, m, "alice", "first")
	second := createManagerVM(t, m, "bob", "second")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	started := make(chan error, 1)
	go func() { started <- m.Start(ctx, first.Owner, first.ID) }()
	managerWaitFile(t, filepath.Join(m.directory(first.ID), "helper.pid"))
	if err := m.Start(t.Context(), second.Owner, second.ID); !errors.Is(err, ErrMemoryCapacity) {
		t.Fatalf("starting desktop had no memory reservation: %v", err)
	}
	cancel()
	if err := <-started; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled startup returned %v", err)
	}
	managerAssertReaped(t, m.directory(first.ID))
	ctx, cancelRetry := context.WithTimeout(t.Context(), 250*time.Millisecond)
	defer cancelRetry()
	if err := m.Start(ctx, second.Owner, second.ID); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("cancelled startup leaked memory reservation: %v", err)
	}
}

func TestMemoryBudgetValidation(t *testing.T) {
	t.Parallel()
	for _, budget := range []int{-1, 1<<30 + 1} {
		t.Run(fmt.Sprint(budget), func(t *testing.T) {
			t.Parallel()
			config := ManagerConfig{MemoryBudgetMiB: budget}
			if err := normalizeManagerLimits(&config); !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid budget accepted: %v", err)
			}
		})
	}
}

func TestMemoryAdmissionErrorsAreReadable(t *testing.T) {
	t.Parallel()
	status, message := backendError(fmt.Errorf("%w: private host details", ErrMemoryCapacity))
	if status != http.StatusConflict || !strings.Contains(message, "Not enough host memory") ||
		strings.Contains(message, "private") {
		t.Fatalf("capacity error response = %d, %q", status, message)
	}
	status, message = backendError(fmt.Errorf("%w: /private/path", errMemoryCheck))
	if status != http.StatusServiceUnavailable || !strings.Contains(message, "could not be checked") ||
		strings.Contains(message, "path") {
		t.Fatalf("memory check error response = %d, %q", status, message)
	}
}

func TestManagerAccountsForUncommittedMemoryUnderPressure(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name     string
		resident uint64
		readErr  error
		wantErr  error
	}{
		{name: "untouched RAM remains reserved", wantErr: ErrMemoryCapacity},
		{name: "resident RAM avoids double counting", resident: 3584 << 20},
		{name: "one byte short", resident: (3584 << 20) - 1, wantErr: ErrMemoryCapacity},
		{name: "credit capped at reservation", resident: 16 << 30},
		{
			name:     "unreadable child receives no credit",
			resident: 16 << 30, readErr: os.ErrPermission, wantErr: ErrMemoryCapacity,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			m := newTestManager(t, "graceful")
			m.config.MaxRunning = 4
			available := uint64(10 << 30)
			m.readMemory = func() (hostMemory, error) {
				return hostMemory{Total: 16 << 30, Available: available}, nil
			}
			m.readResident = func(int) (uint64, error) { return tt.resident, tt.readErr }
			first := createManagerVM(t, m, "alice", "first")
			second := createManagerVM(t, m, "bob", "second")
			if err := m.Start(t.Context(), first.Owner, first.ID); err != nil {
				t.Fatal(err)
			}
			// Other applications consume memory after the automatic budget was
			// captured. Enough remains for one VM, but not two untouched guests.
			available = 6 << 30
			if err := m.Start(t.Context(), second.Owner, second.ID); !errors.Is(err, tt.wantErr) {
				t.Fatalf("Start with %d resident bytes = %v, want %v", tt.resident, err, tt.wantErr)
			}
		})
	}
}
