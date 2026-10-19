package gateway

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
)

const (
	// Headroom protects the gateway; the worker allowance is also cgroup-enforced.
	hostMemoryHeadroom = uint64(1 << 30)
	vmMemoryOverhead   = uint64(512 << 20)
)

var (
	errMemoryCheck   = errors.New("cannot check host memory")
	errWorkerCleanup = errors.New("cannot verify worker cgroup cleanup")
)

// checkMemory runs with m.mu held immediately before reserving an active slot.
// Charging full guest RAM prevents untouched mmap pages and concurrent starts
// from appearing free. Reservations remain until child exit, including failed
// starts and shutdowns still waiting for the child to stop.
func (m *Manager) checkMemory() error {
	if m.isolationErr != nil {
		return m.isolationErr
	}
	charge := workerMemoryLimit(m.config)
	// Only resident anonymous memory backs a reservation. File-backed mappings,
	// shared-memory mappings, swapped-out pages and virtual address space do not.
	// Read resident pages before host availability to avoid crediting newly
	// committed pages against an earlier free-memory snapshot.
	var resident uint64
	for run := range m.processes {
		used, err := m.readResident(run.cmd.Process.Pid)
		if err == nil {
			resident += min(used, charge)
		}
		// If a child exits or its accounting cannot be read, credit no pages.
		// Its full reservation remains until watch observes process exit.
	}
	memory, err := m.readMemory()
	if err != nil {
		return fmt.Errorf("%w: %w", errMemoryCheck, err)
	}
	if m.active == 0 {
		// Snapshot the capacity when starting an idle group of desktops. Keep the
		// total reservation cap stable while any child is active; the separate
		// availability check below accounts for their resident pages.
		m.memoryBudget = 0
		if memory.Available > hostMemoryHeadroom {
			m.memoryBudget = memory.Available - hostMemoryHeadroom
		}
		if m.config.MemoryBudgetMiB > 0 {
			m.memoryBudget = min(m.memoryBudget, uint64(m.config.MemoryBudgetMiB)<<20)
		}
	}
	// Also respect a reduced cgroup limit and memory pressure since the initial
	// snapshot. This is admission control; unrelated allocations can still race it.
	budget := m.memoryBudget
	if memory.Total <= hostMemoryHeadroom {
		budget = 0
	} else {
		budget = min(budget, memory.Total-hostMemoryHeadroom)
	}
	if uint64(m.active) >= budget/charge {
		return fmt.Errorf("%w: desktop needs %d MiB including overhead; %d active desktop(s), budget %d MiB",
			ErrMemoryCapacity, charge>>20, m.active, budget>>20)
	}
	// The budget division above also proves this multiplication cannot overflow.
	uncommitted := (uint64(m.active)+1)*charge - resident
	if memory.Available < hostMemoryHeadroom || memory.Available-hostMemoryHeadroom < uncommitted {
		return fmt.Errorf("%w: desktops need %d MiB of uncommitted reservations plus %d MiB host headroom; %d MiB available",
			ErrMemoryCapacity, uncommitted>>20, hostMemoryHeadroom>>20, memory.Available>>20)
	}

	return nil
}

func normalizeWorkerMemoryConfig(config *ManagerConfig) error {
	if config.MemoryOverheadMiB == 0 {
		config.MemoryOverheadMiB = int(vmMemoryOverhead >> 20)
	}
	if config.MemoryOverheadMiB < 64 || config.MemoryOverheadMiB > 16<<10 {
		return fmt.Errorf("worker memory overhead must be between 64 and 16384 MiB: %w", ErrInvalid)
	}
	if config.CgroupRoot != "" &&
		(!filepath.IsAbs(config.CgroupRoot) || filepath.Clean(config.CgroupRoot) != config.CgroupRoot) {
		return fmt.Errorf("VM cgroup root must be a clean absolute path: %w", ErrInvalid)
	}

	return nil
}

func workerMemoryLimit(config ManagerConfig) uint64 {
	overhead := vmMemoryOverhead
	if config.MemoryOverheadMiB > 0 {
		overhead = uint64(config.MemoryOverheadMiB) << 20
	}

	return uint64(config.MemoryMiB)<<20 + overhead
}

// startConfinedWorker never starts a child before its memory limit is installed.
// The syscall attribute attaches the child at clone time, not in a racing PID move.
func (m *Manager) startConfinedWorker(cmd *exec.Cmd, vmID string) (result workerCgroup, startErr error) {
	cgroup, err := m.cgroups.Prepare(m.config.CgroupRoot, vmID, workerMemoryLimit(m.config))
	if err != nil {
		return nil, err
	}
	defer func() {
		if result == nil {
			startErr = errors.Join(startErr, m.cleanupWorker(cgroup))
		}
	}()
	if err := cgroup.Apply(cmd.SysProcAttr); err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start memory-confined worker: %w", err)
	}
	if err := cgroup.CloseFD(); err != nil {
		killErr := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		waitErr := cmd.Wait()

		return nil, errors.Join(err, killErr, waitErr)
	}

	return cgroup, nil
}

// cleanupWorker preserves admission when the kernel cannot confirm isolation
// cleanup. Management remains available, but recovery requires operator action.
func (m *Manager) cleanupWorker(cgroup workerCgroup) error {
	if err := cgroup.Cleanup(); err != nil {
		failure := errors.Join(ErrCgroupUnavailable, errWorkerCleanup, err)
		m.mu.Lock()
		m.isolationErr = errors.Join(m.isolationErr, failure)
		m.mu.Unlock()

		return failure
	}

	return nil
}
