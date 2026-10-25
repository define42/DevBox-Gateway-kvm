package main

import (
	"errors"
	"fmt"
	"os"
	"runtime"

	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
	"github.com/define42/DevBox-Gateway-kvm/internal/workersandbox"
	"github.com/define42/DevBox-Gateway-kvm/kvm"
	"golang.org/x/sys/unix"
)

const hostRequirementsExitCode = 78

var (
	errHostRequirements    = errors.New("host requirements not met")
	errUnsupportedPlatform = errors.New("a Linux amd64 host is required")
)

// Per-call probes keep validation testable without a flag that bypasses checks.
type serveHostProbes struct {
	operatingSystem, architecture, kernel string
	effectiveUID                          int
	sandbox                               func() error
	kvm                                   func(string) error
	cgroup                                func(string) error
}

func checkServeHost(cgroupRoot string) error {
	kernel := "unknown"
	var uname unix.Utsname
	if err := unix.Uname(&uname); err == nil {
		kernel = unix.ByteSliceToString(uname.Release[:])
	}

	return validateServeHost(cgroupRoot, serveHostProbes{
		operatingSystem: runtime.GOOS, architecture: runtime.GOARCH, kernel: kernel,
		effectiveUID: os.Geteuid(), sandbox: workersandbox.CheckSupport,
		kvm: kvm.CheckHost, cgroup: gateway.ValidateCgroupRoot,
	})
}

func validateServeHost(cgroupRoot string, host serveHostProbes) error {
	failures := make([]error, 0, 5)
	if host.operatingSystem != "linux" || host.architecture != "amd64" {
		failures = append(failures, fmt.Errorf(
			"%w; detected %s/%s", errUnsupportedPlatform, host.operatingSystem, host.architecture,
		))
	}
	if host.effectiveUID == 0 {
		failures = append(failures, fmt.Errorf(
			"%w; start the packaged devbox-kvm service or use an unprivileged account with KVM access",
			workersandbox.ErrRoot,
		))
	}
	if err := host.sandbox(); err != nil {
		failures = append(failures, fmt.Errorf("VM worker sandbox: %w", err))
	}
	if err := host.kvm("/dev/kvm"); err != nil {
		failures = append(failures, err)
	}
	if cgroupRoot == "" {
		failures = append(failures, fmt.Errorf(
			"%w: VM_CGROUP_ROOT is required; start the packaged systemd service or configure "+
				"a private delegated cgroup v2 subtree",
			gateway.ErrCgroupUnavailable,
		))
	} else if err := host.cgroup(cgroupRoot); err != nil {
		failures = append(failures, fmt.Errorf("VM_CGROUP_ROOT %q: %w", cgroupRoot, err))
	}
	if err := errors.Join(failures...); err != nil {
		return fmt.Errorf("%w (kernel %s):\n%w", errHostRequirements, host.kernel, err)
	}

	return nil
}
