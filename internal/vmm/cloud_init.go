package vmm

import (
	"errors"
	"fmt"
	"os"

	"github.com/define42/DevBox-Gateway-kvm/internal/cloudinit"
)

var errCloudInitDisk = errors.New("cloud-init seed requires a boot disk")

var errCloudInitBootDisk = errors.New("cloud-init seed must be a separate file from the writable boot disk")

func (v *VMM) validateCloudInit() error {
	if v.CloudInit == "" {
		return nil
	}
	if v.Disk == "" {
		return errCloudInitDisk
	}
	if err := cloudinit.ValidateISO(v.CloudInit); err != nil {
		return fmt.Errorf("cloud-init seed: %w", err)
	}

	seed, err := os.Stat(v.CloudInit)
	if err != nil {
		return fmt.Errorf("stat cloud-init seed: %w", err)
	}
	disk, err := os.Stat(v.Disk)
	if err != nil {
		return fmt.Errorf("stat boot disk: %w", err)
	}
	if os.SameFile(seed, disk) {
		return errCloudInitBootDisk
	}

	return nil
}
