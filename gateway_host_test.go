package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
	"github.com/define42/DevBox-Gateway-kvm/internal/workersandbox"
)

func supportedServeHost() serveHostProbes {
	return serveHostProbes{
		operatingSystem: "linux", architecture: "amd64", kernel: "6.12.0-test", effectiveUID: 995,
		sandbox: func() error { return nil },
		kvm:     func(string) error { return nil },
		cgroup:  func(string) error { return nil },
	}
}

func TestValidateServeHost(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		change  func(*serveHostProbes)
		root    string
		wantErr error
		message string
	}{
		{name: "supported", root: "/delegated/workers"},
		{
			name: "features determine support, not release string", root: "/delegated/workers",
			change: func(host *serveHostProbes) { host.kernel = "6.8.0-backported" },
		},
		{
			name: "wrong architecture", root: "/delegated/workers", wantErr: errUnsupportedPlatform,
			change: func(host *serveHostProbes) { host.architecture = "arm64" }, message: "linux/arm64",
		},
		{
			name: "wrong operating system", root: "/delegated/workers", wantErr: errUnsupportedPlatform,
			change: func(host *serveHostProbes) { host.operatingSystem = "darwin" }, message: "darwin/amd64",
		},
		{
			name: "root cannot launch workers", root: "/delegated/workers", wantErr: workersandbox.ErrRoot,
			change: func(host *serveHostProbes) { host.effectiveUID = 0 }, message: "unprivileged account",
		},
		{
			name: "unsupported kernel", root: "/delegated/workers", wantErr: workersandbox.ErrUnavailable,
			change: func(host *serveHostProbes) {
				host.kernel = "6.8.0-142-generic"
				host.sandbox = func() error { return workersandbox.ErrUnavailable }
			},
			message: "kernel 6.8.0-142-generic",
		},
		{
			name: "KVM access denied", root: "/delegated/workers", wantErr: syscall.EACCES,
			change: func(host *serveHostProbes) {
				host.kvm = func(device string) error { return fmt.Errorf("open %s: %w", device, syscall.EACCES) }
			},
			message: "open /dev/kvm",
		},
		{
			name: "cgroup delegation missing", wantErr: gateway.ErrCgroupUnavailable,
			message: "VM_CGROUP_ROOT is required",
		},
		{
			name: "cgroup delegation invalid", root: "/delegated/workers", wantErr: gateway.ErrCgroupUnavailable,
			change: func(host *serveHostProbes) {
				host.cgroup = func(string) error { return gateway.ErrCgroupUnavailable }
			},
			message: `VM_CGROUP_ROOT "/delegated/workers"`,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			host := supportedServeHost()
			if test.change != nil {
				test.change(&host)
			}
			err := validateServeHost(test.root, host)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("error = %v, want %v", err, test.wantErr)
			}
			if test.wantErr != nil {
				if !errors.Is(err, errHostRequirements) || !strings.Contains(err.Error(), test.message) {
					t.Fatalf("missing startup failure context: %v", err)
				}
			}
		})
	}
}

func TestValidateServeHostReportsIndependentFailures(t *testing.T) {
	t.Parallel()
	host := supportedServeHost()
	host.sandbox = func() error { return workersandbox.ErrUnavailable }
	host.kvm = func(string) error { return syscall.EACCES }
	host.cgroup = func(string) error {
		t.Fatal("empty cgroup path should not be probed")

		return nil
	}
	err := validateServeHost("", host)
	for _, cause := range []error{
		errHostRequirements, workersandbox.ErrUnavailable, syscall.EACCES, gateway.ErrCgroupUnavailable,
	} {
		if !errors.Is(err, cause) {
			t.Fatalf("failure %v omitted from %v", cause, err)
		}
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeHostFailurePrecedesInitialization(t *testing.T) {
	clearServeEnvironment(t)
	state := filepath.Join(t.TempDir(), "uninitialized-state")
	cause := fmt.Errorf("%w: %w", errHostRequirements, workersandbox.ErrUnavailable)
	checked := false
	check := func(root string) error {
		checked = true
		if root != "/delegated/workers" {
			t.Fatalf("configured cgroup root was not checked: %q", root)
		}

		return cause
	}
	args := []string{
		"-ldap-url", "ldaps://directory.example.com", "-ldap-base-dn", "dc=example,dc=com",
		"-state", state, "-vm-cgroup-root", "/delegated/workers",
	}
	ctx := t.Context()
	err := runServeWithHostCheck(ctx, args, io.Discard, check)
	if !checked || !errors.Is(err, cause) {
		t.Fatalf("startup did not fail on host requirements: checked=%v error=%v", checked, err)
	}
	if _, err := os.Stat(state); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("failed preflight created application state or TLS identity: %v", err)
	}
}
