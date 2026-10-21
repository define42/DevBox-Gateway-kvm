package gateway

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

// DefaultMaxDiskBytes bounds the virtual capacity of every managed VM disk.
const DefaultMaxDiskBytes int64 = 500_000_000_000

// ErrDiskTooLarge reports a virtual disk whose capacity exceeds operator policy.
var ErrDiskTooLarge = errors.New("virtual disk exceeds the configured size limit")

// ErrDiskChanged reports a disk that no longer matches its creation-time metadata.
var ErrDiskChanged = errors.New("virtual disk does not match its expected format and capacity")

func normalizeManagerStorageLimits(config *ManagerConfig) error {
	if config.MaxImageUploadBytes == 0 {
		config.MaxImageUploadBytes = DefaultMaxImageUploadBytes
	}
	if config.MaxImageUploadBytes < 1 || config.MaxImageUploadBytes > maxImageVirtualSize {
		return fmt.Errorf("invalid image upload size limit: %w", ErrInvalid)
	}
	if config.MaxDiskBytes == 0 {
		config.MaxDiskBytes = DefaultMaxDiskBytes
	}
	if config.MaxDiskBytes < 0 {
		return fmt.Errorf("virtual disk size limit must not be negative: %w", ErrInvalid)
	}

	return nil
}

func validateDiskSize(actual, limit int64) error {
	if actual < 1 {
		return fmt.Errorf("virtual disk must have a positive size: %w", ErrInvalid)
	}
	if limit == 0 {
		limit = DefaultMaxDiskBytes
	}
	if limit < 0 {
		return fmt.Errorf("virtual disk size limit must not be negative: %w", ErrInvalid)
	}
	if actual > limit {
		return fmt.Errorf(
			"virtual disk size %d bytes exceeds configured limit %d bytes: %w",
			actual, limit, ErrDiskTooLarge,
		)
	}

	return nil
}

func validateAndCloseDiskImage(image disk.Image, limit int64) error {
	return errors.Join(validateDiskSize(image.Size(), limit), image.Close())
}

// openDiskVirtualSize reads capacity from an already pinned file descriptor.
// QCOW2 stores virtual capacity in its header; other disk formats are rejected.
// Full QCOW2 layout validation remains at image admission and disk-open sites.
func openDiskVirtualSize(file *os.File) (int64, error) {
	size, err := disk.InspectQCOW2(file)
	if err != nil {
		return 0, fmt.Errorf("inspect disk: %w: %w", ErrInvalid, err)
	}

	return size, nil
}

func validateOpenDisk(file *os.File, limit int64) (int64, error) {
	size, err := openDiskVirtualSize(file)
	if err != nil {
		return 0, err
	}

	return size, validateDiskSize(size, limit)
}

func (m *Manager) validatePrivateDisk(path string) (size int64, validateErr error) {
	file, err := openPrivateFile(path, os.O_RDONLY)
	if err != nil {
		return 0, err
	}
	defer func() { validateErr = errors.Join(validateErr, file.Close()) }()

	return validateOpenDisk(file, m.config.MaxDiskBytes)
}

func validatePinnedDiskSize(file *os.File, expected, limit int64) error {
	if err := validateDiskSize(expected, limit); err != nil {
		return err
	}
	if err := disk.ValidateQCOW2Size(file, expected); err != nil {
		return fmt.Errorf("validate pinned QCOW2 disk: %w: %w", ErrDiskChanged, err)
	}

	return nil
}

func (m *Manager) validateManagedDisk(path string, record VM) (validateErr error) {
	file, err := openPrivateFile(path, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer func() { validateErr = errors.Join(validateErr, file.Close()) }()

	return validatePinnedDiskSize(file, record.DiskSizeBytes, m.config.MaxDiskBytes)
}

// decodeSavedVM distinguishes pre-capacity records from explicitly invalid
// values. RawMessage also preserves null and encoding/json's case matching.
func decodeSavedVM(data []byte) (VM, bool, error) {
	var saved struct {
		VM
		Capacity json.RawMessage `json:"diskSizeBytes"`
	}
	if err := json.Unmarshal(data, &saved); err != nil {
		return VM{}, false, err
	}
	if len(saved.Capacity) == 0 {
		return saved.VM, true, nil
	}
	if err := json.Unmarshal(saved.Capacity, &saved.DiskSizeBytes); err != nil {
		return VM{}, false, err
	}
	if saved.DiskSizeBytes <= 0 {
		return VM{}, false, fmt.Errorf("invalid saved VM disk capacity: %w", ErrInvalid)
	}

	return saved.VM, false, nil
}

// migrateSavedDiskCapacity pins legacy records to their trusted source template.
// Never infer the expected capacity from a disk that has already run a guest.
// Existing positive pins bypass migration and remain independent of templates.
func (m *Manager) migrateSavedDiskCapacity(record *VM) error {
	image, exists := m.images[record.ImageID]
	if !exists {
		return fmt.Errorf("saved capacity is missing and source image %q is unavailable; "+
			"restore the original template or trusted VM metadata: %w", record.ImageID, ErrInvalid)
	}
	if err := validateDiskSize(image.VirtualSizeBytes, m.config.MaxDiskBytes); err != nil {
		return err
	}
	file, err := openPrivateFile(filepath.Join(m.directory(record.ID), "disk.img"), os.O_RDONLY)
	if err != nil {
		return err
	}
	validationErr := disk.ValidateQCOW2(file, image.VirtualSizeBytes)
	if err := errors.Join(validationErr, file.Close()); err != nil {
		return fmt.Errorf("legacy disk does not match its source template: %w: %w", ErrDiskChanged, err)
	}
	record.DiskSizeBytes = image.VirtualSizeBytes

	return m.save(*record)
}
