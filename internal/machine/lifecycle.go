package machine

import (
	"errors"
	"fmt"
	"io"
	"os"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var ErrCPUAlreadyRunning = errors.New("vCPU already running")

// beginRun registers a thread before it can enter KVM_RUN. runMu serializes
// registration with Stop, so Close cannot miss a new worker when joining.
// The caller must keep its OS thread locked until endRun returns.
func (m *Machine) beginRun(cpu int) (uintptr, error) {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	fd, err := m.CPUToFD(cpu)
	if err != nil {
		return 0, err
	}
	if atomic.LoadUint32(&m.stopped) != 0 {
		return 0, ErrMachineStopped
	}
	if m.threads[cpu] != 0 {
		return 0, fmt.Errorf("cpu %d: %w", cpu, ErrCPUAlreadyRunning)
	}
	if m.threads == nil {
		m.threads = make(map[int]int)
	}
	m.threads[cpu] = syscall.Gettid()
	m.runWG.Add(1)

	return fd, nil
}

func (m *Machine) endRun(cpu int) {
	m.runMu.Lock()
	delete(m.threads, cpu)
	m.runWG.Done()
	m.runMu.Unlock()
}

// Stop prevents new runs and wakes active vCPU threads. It does not wait and is
// safe to call from a vCPU exit handler. Close joins workers and frees resources.
func (m *Machine) Stop() error {
	m.runMu.Lock()
	defer m.runMu.Unlock()
	if atomic.SwapUint32(&m.stopped, 1) != 0 {
		return nil
	}
	for _, run := range m.runs {
		// On x86, immediate_exit is byte 1 of the input header. Set it atomically
		// without overwriting request_interrupt_window or either padding byte.
		atomic.OrUint32((*uint32)(unsafe.Pointer(run)), 1<<8)
	}
	var errs []error
	for _, tid := range m.threads {
		// SIGURG is handled by Go's runtime (also for async preemption). It breaks
		// an in-flight KVM_RUN without installing or replacing process handlers.
		// immediate_exit covers the race where the signal arrives before entry.
		if err := syscall.Tgkill(os.Getpid(), tid, syscall.SIGURG); err != nil && !errors.Is(err, syscall.ESRCH) {
			errs = append(errs, fmt.Errorf("wake vCPU thread %d: %w", tid, err))
		}
	}

	return errors.Join(errs...)
}

// Close stops and joins vCPU execution and device workers before releasing any
// guest RAM. It is idempotent and may be called concurrently. Device attachment
// and boot setup must finish before execution or Close begins.
func (m *Machine) Close() error {
	m.closeOnce.Do(func() {
		var errs []error
		errs = append(errs, m.Stop())
		m.runWG.Wait()
		// No doorbell can ring once the vCPUs are joined. Closing the
		// ioeventfds ends their forwarders, which deviceWG joins below.
		if m.doorbells != nil {
			errs = append(errs, m.doorbells.close())
		}
		if m.pci != nil {
			for _, dev := range m.pci.Devices() {
				if closer, ok := dev.(io.Closer); ok {
					errs = append(errs, closer.Close())
				}
			}
		}
		for _, dev := range m.devices {
			if closer, ok := dev.(io.Closer); ok {
				errs = append(errs, closer.Close())
			}
		}
		m.deviceWG.Wait()
		// Device workers signal MSI-X irqfds until they are joined.
		if m.msiRoutes != nil {
			errs = append(errs, m.msiRoutes.close())
		}
		for _, mapping := range m.runMappings {
			errs = append(errs, syscall.Munmap(mapping))
		}
		for _, fd := range m.vcpuFds {
			errs = append(errs, syscall.Close(int(fd)))
		}
		if m.vmCreated {
			errs = append(errs, syscall.Close(int(m.vmFd)))
		}
		if m.uefiVars != nil {
			errs = append(errs, m.uefiVars.Close())
		}
		if m.uefiCode != nil {
			errs = append(errs, syscall.Munmap(m.uefiCode))
		}
		if m.memMapping != nil {
			errs = append(errs, syscall.Munmap(m.memMapping))
		}
		if m.kvmFile != nil {
			errs = append(errs, m.kvmFile.Close())
		}
		m.closeErr = errors.Join(errs...)
	})

	return m.closeErr
}
