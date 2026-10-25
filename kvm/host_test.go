package kvm

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"golang.org/x/sys/unix"
)

func TestCheckHostMissingDevice(t *testing.T) {
	t.Parallel()

	device := filepath.Join(t.TempDir(), "missing-kvm")
	err := CheckHost(device)
	if !errors.Is(err, os.ErrNotExist) || !strings.Contains(err.Error(), "load the kvm_intel or kvm_amd module") {
		t.Fatalf("missing device error = %v", err)
	}
}

func TestCheckHostNonKVMFile(t *testing.T) {
	t.Parallel()

	device := filepath.Join(t.TempDir(), "not-kvm")
	if err := os.WriteFile(device, nil, 0o600); err != nil {
		t.Fatal(err)
	}
	err := CheckHost(device)
	if !errors.Is(err, unix.ENOTTY) || !strings.Contains(err.Error(), "query KVM API version") {
		t.Fatalf("regular file error = %v", err)
	}
}

func TestCheckHostOperations(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		openError     error
		apiVersion    uintptr
		apiError      error
		createError   error
		vmCloseError  error
		devCloseError error
		wantErrors    []error
		wantMessage   string
		wantCreate    bool
		wantClosed    []int
	}{
		{
			name: "success", apiVersion: 12, wantCreate: true, wantClosed: []int{101, 100},
		},
		{
			name: "missing device", openError: unix.ENOENT,
			wantErrors: []error{unix.ENOENT}, wantMessage: "load the kvm_intel or kvm_amd module",
		},
		{
			name: "permission denied", openError: unix.EACCES,
			wantErrors: []error{unix.EACCES}, wantMessage: "grant the service user access through the kvm group",
		},
		{
			name: "other open error", openError: unix.EIO,
			wantErrors: []error{unix.EIO}, wantMessage: "open KVM device",
		},
		{
			name: "API query failed", apiError: unix.ENOTTY,
			wantErrors: []error{unix.ENOTTY}, wantMessage: "query KVM API version", wantClosed: []int{100},
		},
		{
			name: "unsupported API", apiVersion: 11,
			wantErrors: []error{errHostAPIVersion}, wantMessage: "reports 11, require 12", wantClosed: []int{100},
		},
		{
			name: "VM creation failed", apiVersion: 12, createError: unix.ENODEV, wantCreate: true,
			wantErrors: []error{unix.ENODEV}, wantMessage: "check hardware or nested virtualization", wantClosed: []int{100},
		},
		{
			name: "VM close failed", apiVersion: 12, wantCreate: true, vmCloseError: unix.EIO,
			wantErrors: []error{unix.EIO}, wantMessage: "close temporary KVM VM", wantClosed: []int{101, 100},
		},
		{
			name: "device close failed", apiVersion: 12, wantCreate: true, devCloseError: unix.EBADF,
			wantErrors: []error{unix.EBADF}, wantMessage: "close KVM device", wantClosed: []int{101, 100},
		},
		{
			name: "both closes failed", apiVersion: 12, wantCreate: true,
			vmCloseError: unix.EIO, devCloseError: unix.EBADF,
			wantErrors: []error{unix.EIO, unix.EBADF}, wantClosed: []int{101, 100},
		},
		{
			name: "API and cleanup failed", apiError: unix.ENOTTY, devCloseError: unix.EBADF,
			wantErrors: []error{unix.ENOTTY, unix.EBADF}, wantClosed: []int{100},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			const device = "/test/kvm"
			closed := []int{}
			created := false
			operations := hostOperations{
				open: func(path string, flags int, mode uint32) (int, error) {
					if path != device || flags != unix.O_RDWR|unix.O_CLOEXEC || mode != 0 {
						t.Errorf("open(%q, %#x, %#o)", path, flags, mode)
					}

					return 100, test.openError
				},
				apiVersion: func(fd uintptr) (uintptr, error) {
					if test.openError != nil || fd != 100 {
						t.Fatalf("API query after failed open or with wrong fd: %d", fd)
					}

					return test.apiVersion, test.apiError
				},
				createVM: func(fd uintptr) (uintptr, error) {
					if fd != 100 {
						t.Errorf("createVM fd = %d", fd)
					}
					created = true

					return 101, test.createError
				},
				close: func(fd int) error {
					closed = append(closed, fd)
					if fd == 101 {
						return test.vmCloseError
					}

					return test.devCloseError
				},
			}
			err := checkHost(device, operations)
			if len(test.wantErrors) == 0 && err != nil {
				t.Fatalf("checkHost() = %v", err)
			}
			for _, target := range test.wantErrors {
				if !errors.Is(err, target) {
					t.Errorf("checkHost() = %v; want wrapped %v", err, target)
				}
			}
			if test.wantMessage != "" && (err == nil || !strings.Contains(err.Error(), test.wantMessage)) {
				t.Errorf("checkHost() = %v; want message containing %q", err, test.wantMessage)
			}
			if created != test.wantCreate || !slices.Equal(closed, test.wantClosed) {
				t.Errorf("createVM called = %t, closed = %v; want %t, %v", created, closed, test.wantCreate, test.wantClosed)
			}
		})
	}
}
