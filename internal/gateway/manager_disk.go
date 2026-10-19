package gateway

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math"
	"os"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

// DefaultMaxDiskBytes bounds the virtual capacity of every managed VM disk.
const DefaultMaxDiskBytes int64 = 500_000_000_000

// ErrDiskTooLarge reports a virtual disk whose capacity exceeds operator policy.
var ErrDiskTooLarge = errors.New("virtual disk exceeds the configured size limit")

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
// Raw disks use their file length; QCOW2 stores virtual capacity in its header.
// Full QCOW2 layout validation remains at image admission and disk-open sites.
func openDiskVirtualSize(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, err
	}
	if !info.Mode().IsRegular() || info.Size() < 1 {
		return 0, fmt.Errorf("virtual disk must be a nonempty regular file: %w", ErrInvalid)
	}

	var header [32]byte
	n, err := file.ReadAt(header[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		return 0, err
	}
	if n < 4 || string(header[:4]) != "QFI\xfb" {
		return info.Size(), nil
	}
	if n != len(header) {
		return 0, fmt.Errorf("truncated QCOW2 header: %w", ErrInvalid)
	}
	version := binary.BigEndian.Uint32(header[4:8])
	if version != 2 && version != 3 {
		return 0, fmt.Errorf("unsupported QCOW2 version: %w", ErrInvalid)
	}
	size := binary.BigEndian.Uint64(header[24:32])
	if size == 0 || size > math.MaxInt64 {
		return 0, fmt.Errorf("invalid QCOW2 virtual size: %w", ErrInvalid)
	}

	return int64(size), nil
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
