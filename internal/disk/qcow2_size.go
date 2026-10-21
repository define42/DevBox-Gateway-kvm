package disk

import (
	"encoding/binary"
	"errors"
	"fmt"
	"os"
)

var (
	ErrInvalidSize  = errors.New("disk: expected capacity must be positive")
	ErrSpecMismatch = errors.New("disk: image does not match pinned capacity")
	errInvalidImage = errors.New("disk: invalid image")
)

// InspectQCOW2 reads the guest-visible capacity of a regular, nonempty QCOW2
// file. Inspection is bounded to its fixed header; opening the image additionally
// validates the complete metadata layout. It never detects or accepts raw data
// and does not close file.
func InspectQCOW2(file *os.File) (int64, error) {
	info, err := file.Stat()
	if err != nil {
		return 0, fmt.Errorf("disk: stat image: %w", err)
	}
	if !info.Mode().IsRegular() || info.Size() <= 0 {
		return 0, fmt.Errorf("%w: expected a regular, nonempty file", errInvalidImage)
	}
	var header [qcow2HeaderV3Len]byte
	if _, err := file.ReadAt(header[:qcow2HeaderV2Len], 0); err != nil {
		return 0, fmt.Errorf("disk: read qcow2 header: %w", err)
	}
	if string(header[:4]) != qcow2Magic {
		return 0, fmt.Errorf("%w: qcow2 magic is missing", errInvalidImage)
	}
	switch version := binary.BigEndian.Uint32(header[4:8]); version {
	case qcow2Version2:
	case qcow2Version3:
		if _, err := file.ReadAt(header[qcow2HeaderV2Len:], qcow2HeaderV2Len); err != nil {
			return 0, fmt.Errorf("disk: read qcow2 v3 header: %w", err)
		}
	default:
		return 0, fmt.Errorf("%w: unsupported qcow2 version %d", errInvalidImage, version)
	}
	size := binary.BigEndian.Uint64(header[24:32])
	if size == 0 || size > uint64(maxInt64) {
		return 0, fmt.Errorf("%w: invalid qcow2 capacity %d", errInvalidImage, size)
	}

	return int64(size), nil
}

// ValidateQCOW2Size checks a QCOW2 disk against its pinned guest-visible capacity.
// It does not close file or validate the complete metadata layout.
func ValidateQCOW2Size(file *os.File, expected int64) error {
	if expected <= 0 {
		return ErrInvalidSize
	}
	observed, err := InspectQCOW2(file)
	if err != nil {
		return err
	}
	if observed != expected {
		return fmt.Errorf("%w: qcow2 capacity is %d, expected %d", ErrSpecMismatch, observed, expected)
	}

	return nil
}

// OpenQCOW2WithSize opens writable QCOW2 storage with a pinned capacity. The
// opened descriptor is validated and then retained by the returned Image.
func OpenQCOW2WithSize(path string, expected int64) (Image, error) {
	if expected <= 0 {
		return nil, ErrInvalidSize
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	if err := ValidateQCOW2Size(file, expected); err != nil {
		_ = file.Close()

		return nil, err
	}
	image, err := openQCOW2(file)
	if err != nil {
		return nil, err
	}
	// The decoder rereads metadata. Its actual capacity must still match if the
	// file was modified after the initial descriptor validation.
	if image.Size() != expected {
		_ = image.Close()

		return nil, fmt.Errorf("%w: qcow2 capacity changed while opening", ErrSpecMismatch)
	}

	return image, nil
}
