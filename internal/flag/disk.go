package flag

import "errors"

var ErrDiskOptions = errors.New("disk capacity must be nonnegative and requires a primary QCOW2 disk")

func validateDiskOptions(c *BootArgs) error {
	if c.DiskSizeBytes < 0 || (c.Disk == "" && c.DiskSizeBytes != 0) {
		return ErrDiskOptions
	}

	return nil
}
