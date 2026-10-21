package main

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestManagedWorkerRejectsMissingDiskSize(t *testing.T) {
	t.Parallel()
	for _, command := range []string{"worker", "worker-boot"} {
		t.Run(command, func(t *testing.T) {
			t.Parallel()
			// This deliberately invalid descriptor must never be adopted: a
			// missing disk capacity is rejected before confinement or VM setup.
			args := []string{"devbox-kvm", command}
			if command == "worker" {
				args = append(args, "boot")
			}
			args = append(args, "-uefi", "firmware", "-uefi-vars", "vars", "-d", "guest.qcow2", "-rdp-fd=2147483647")
			if err := run(t.Context(), args); !errors.Is(err, errWorkerDiskSize) {
				t.Fatalf("managed entry point returned %v, want missing disk capacity", err)
			}
		})
	}
}

func TestManagedWorkerRequiresPositiveDiskSize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		size  int64
		valid bool
	}{
		{name: "positive capacity", size: 4096, valid: true},
		{name: "missing capacity"},
		{name: "negative capacity", size: -1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			boot := &flag.BootArgs{UEFI: "firmware", Disk: "guest.qcow2", RDPFD: 3, DiskSizeBytes: tc.size}
			err := validateWorkerBoot(boot)
			if tc.valid {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, errWorkerDiskSize) {
				t.Fatalf("invalid disk capacity returned %v", err)
			}
		})
	}
}
