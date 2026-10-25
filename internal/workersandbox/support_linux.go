//go:build linux

package workersandbox

import (
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// CheckSupport checks the kernel features required by managed VM workers without
// changing the caller's sandbox, capabilities, resource limits, or privileges.
// Worker startup still validates that its individual policy can be installed.
func CheckSupport() error {
	return checkSupport(ABI, seccompSupport)
}

func checkSupport(landlockABI func() (int, error), checkSeccomp func() error) error {
	version, err := landlockABI()
	if err == nil && version < minABI {
		err = ErrUnavailable
	}
	if err != nil {
		if !errors.Is(err, ErrUnavailable) {
			err = fmt.Errorf("%w: %w", ErrUnavailable, err)
		}
		switch {
		case errors.Is(err, unix.ENOSYS):
			return fmt.Errorf("landlock is unavailable or its syscall is blocked; "+
				"use Linux 6.12+ with Landlock enabled and allow its syscalls: %w", err)
		case errors.Is(err, unix.EOPNOTSUPP):
			return fmt.Errorf("landlock is disabled on this kernel; "+
				"enable Landlock in the boot-time LSM configuration (Linux 6.12+ required): %w", err)
		case version > 0 && version < minABI:
			return fmt.Errorf("landlock ABI %d is too old (need %d or newer); "+
				"boot Linux 6.12+ with Landlock enabled: %w", version, minABI, err)
		default:
			return fmt.Errorf("query landlock support: %w", err)
		}
	}
	if err := checkSeccomp(); err != nil {
		return fmt.Errorf("%w: seccomp syscall filtering is unavailable: %w", ErrUnavailable, err)
	}

	return nil
}

func seccompSupport() error {
	return checkSeccompSupport(func(operation, flags uintptr, argument unsafe.Pointer) unix.Errno {
		_, _, errno := unix.Syscall(unix.SYS_SECCOMP, operation, flags, uintptr(argument))

		return errno
	})
}

func checkSeccompSupport(call func(operation, flags uintptr, argument unsafe.Pointer) unix.Errno) error {
	// runtimeFilter uses KILL_PROCESS for an unexpected syscall architecture.
	action := uint32(unix.SECCOMP_RET_KILL_PROCESS)
	errno := call(unix.SECCOMP_GET_ACTION_AVAIL, 0, unsafe.Pointer(&action))
	if errno != 0 {
		return fmt.Errorf("query SECCOMP_RET_KILL_PROCESS: %w", errno)
	}

	// GET_ACTION_AVAIL also exists when CONFIG_SECCOMP_FILTER is disabled.
	// A NULL filter can never be installed: EFAULT proves that the kernel
	// supports filter mode and TSYNC and reached the user-filter copy. This
	// happens before privilege checks or any changes to the calling process.
	errno = call(unix.SECCOMP_SET_MODE_FILTER, unix.SECCOMP_FILTER_FLAG_TSYNC, nil)
	if errno == 0 {
		return fmt.Errorf("%w: seccomp NULL filter probe unexpectedly succeeded", ErrUnavailable)
	}
	if errno != unix.EFAULT {
		return fmt.Errorf("probe seccomp filter and thread synchronization support: %w", errno)
	}

	return nil
}
