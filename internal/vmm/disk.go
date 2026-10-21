package vmm

import (
	"errors"
	"fmt"
	"os"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

var errDiskConfig = errors.New("invalid primary QCOW2 disk capacity")

// ResolveDiskSize pins a QCOW2 disk's capacity before the first standalone boot.
// Explicit values are verified. Callers recreating a VMM after guest resets must
// retain the resolved configuration so every reopen checks the same capacity.
func (c *Config) ResolveDiskSize() (resolveErr error) {
	if c.DiskSizeBytes < 0 || (c.Disk == "" && c.DiskSizeBytes != 0) {
		return errDiskConfig
	}
	if c.Disk == "" {
		return nil
	}
	file, err := os.Open(c.Disk)
	if err != nil {
		return fmt.Errorf("inspect primary QCOW2 disk: %w", err)
	}
	defer func() { resolveErr = errors.Join(resolveErr, file.Close()) }()
	if c.DiskSizeBytes > 0 {
		return disk.ValidateQCOW2Size(file, c.DiskSizeBytes)
	}
	size, err := disk.InspectQCOW2(file)
	if err != nil {
		return err
	}
	c.DiskSizeBytes = size

	return nil
}
