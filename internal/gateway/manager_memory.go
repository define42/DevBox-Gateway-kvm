package gateway

import (
	"errors"
	"fmt"
)

const (
	// These are admission allowances, not enforced process memory limits.
	hostMemoryHeadroom = uint64(1 << 30)
	vmMemoryOverhead   = uint64(256 << 20)
)

var errMemoryCheck = errors.New("cannot check host memory")

// checkMemory runs with m.mu held immediately before reserving an active slot.
// Charging full guest RAM prevents untouched mmap pages and concurrent starts
// from appearing free. Reservations remain until child exit, including failed
// starts and shutdowns still waiting for the child to stop.
func (m *Manager) checkMemory() error {
	charge := uint64(m.config.MemoryMiB)<<20 + vmMemoryOverhead
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
