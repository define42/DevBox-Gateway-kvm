package kvm

import (
	"errors"
	"fmt"
	"os"

	"golang.org/x/sys/unix"
)

const hostAPIVersion = 12

var errHostAPIVersion = errors.New("unsupported KVM API version")

type hostOperations struct {
	open       func(string, int, uint32) (int, error)
	apiVersion func(uintptr) (uintptr, error)
	createVM   func(uintptr) (uintptr, error)
	close      func(int) error
}

// CheckHost verifies that the current process can open device, use the stable
// KVM API, and create a VM. The temporary VM has no memory or virtual CPUs and
// is closed before returning; no guest code runs.
func CheckHost(device string) error {
	return checkHost(device, hostOperations{
		open:       unix.Open,
		apiVersion: GetAPIVersion,
		createVM:   CreateVM,
		close:      unix.Close,
	})
}

func checkHost(device string, operations hostOperations) (result error) {
	fd, err := operations.open(device, unix.O_RDWR|unix.O_CLOEXEC, 0)
	if err != nil {
		switch {
		case errors.Is(err, os.ErrNotExist):
			return fmt.Errorf("open KVM device %q: %w; enable virtualization and load the kvm_intel or kvm_amd module",
				device, err)
		case errors.Is(err, os.ErrPermission):
			return fmt.Errorf("open KVM device %q for read/write: %w; grant the service user access through the kvm group",
				device, err)
		default:
			return fmt.Errorf("open KVM device %q for read/write: %w", device, err)
		}
	}
	defer func() {
		if err := operations.close(fd); err != nil {
			result = errors.Join(result, fmt.Errorf("close KVM device %q: %w", device, err))
		}
	}()

	version, err := operations.apiVersion(uintptr(fd))
	if err != nil {
		return fmt.Errorf("query KVM API version from %q (must be a usable KVM device): %w", device, err)
	}
	if version != hostAPIVersion {
		return fmt.Errorf("%w: %q reports %d, require %d", errHostAPIVersion, device, version, hostAPIVersion)
	}

	vm, err := operations.createVM(uintptr(fd))
	if err != nil {
		return fmt.Errorf("create temporary KVM VM using %q: %w; "+
			"check hardware or nested virtualization and available host resources", device, err)
	}
	if err := operations.close(int(vm)); err != nil {
		return fmt.Errorf("close temporary KVM VM: %w", err)
	}

	return nil
}
