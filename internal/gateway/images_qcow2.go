package gateway

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/define42/gokvm/internal/disk"
)

// These checks run before any external image tool can interpret filenames in an
// uploaded header. See https://www.qemu.org/docs/master/interop/qcow2.html.
func validateUploadedQCOW2(path string) (int64, error) {
	file, err := openPrivateFile(path, os.O_RDONLY)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	var header [104]byte
	if _, err := io.ReadFull(file, header[:72]); err != nil || string(header[:4]) != "QFI\xfb" {
		return 0, fmt.Errorf("upload must be a QCOW2 disk image: %w", ErrInvalid)
	}
	version := binary.BigEndian.Uint32(header[4:8])
	if version != 2 && version != 3 {
		return 0, fmt.Errorf("unsupported QCOW2 version: %w", ErrInvalid)
	}
	if binary.BigEndian.Uint64(header[8:16]) != 0 || binary.BigEndian.Uint32(header[16:20]) != 0 {
		return 0, fmt.Errorf("upload must not reference a backing file: %w", ErrInvalid)
	}
	clusterBits := binary.BigEndian.Uint32(header[20:24])
	if clusterBits < 9 || clusterBits > 21 {
		return 0, fmt.Errorf("invalid QCOW2 cluster size: %w", ErrInvalid)
	}
	virtualSize := binary.BigEndian.Uint64(header[24:32])
	if virtualSize == 0 || virtualSize > uint64(maxImageVirtualSize) {
		return 0, fmt.Errorf("virtual image size must be between 1 byte and 1 TiB: %w", ErrInvalid)
	}
	if binary.BigEndian.Uint32(header[32:36]) != 0 {
		return 0, fmt.Errorf("encrypted images are unsupported: %w", ErrInvalid)
	}
	if binary.BigEndian.Uint32(header[60:64]) != 0 || binary.BigEndian.Uint64(header[64:72]) != 0 {
		return 0, fmt.Errorf("images with internal snapshots are unsupported: %w", ErrInvalid)
	}
	headerLength := uint32(72)
	if version == 3 {
		if _, err := io.ReadFull(file, header[72:]); err != nil {
			return 0, fmt.Errorf("truncated QCOW2 header: %w", ErrInvalid)
		}
		// Only compression type and extended L2 entries may need conversion.
		// Dirty, corrupt, external data and unknown incompatible features fail closed.
		if binary.BigEndian.Uint64(header[72:80]) & ^uint64((1<<3)|(1<<4)) != 0 {
			return 0, fmt.Errorf("dirty, corrupt or externally backed images are unsupported: %w", ErrInvalid)
		}
		if binary.BigEndian.Uint64(header[88:96]) & ^uint64(1) != 0 {
			return 0, fmt.Errorf("external data or unknown QCOW2 features are unsupported: %w", ErrInvalid)
		}
		headerLength = binary.BigEndian.Uint32(header[100:104])
		if headerLength < 104 || headerLength%8 != 0 {
			return 0, fmt.Errorf("invalid QCOW2 header length: %w", ErrInvalid)
		}
	}
	clusterSize := uint32(1) << clusterBits
	if headerLength > clusterSize-8 {
		return 0, fmt.Errorf("invalid QCOW2 header bounds: %w", ErrInvalid)
	}
	if err := validateQCOW2Extensions(file, int64(headerLength), int64(clusterSize)); err != nil {
		return 0, err
	}

	return int64(virtualSize), nil //nolint:gosec // Bounded to 1 TiB above.
}

func validateQCOW2Extensions(file io.ReaderAt, offset, end int64) error {
	var extension [8]byte
	for offset <= end-8 {
		if _, err := file.ReadAt(extension[:], offset); err != nil {
			return fmt.Errorf("truncated QCOW2 extensions: %w", ErrInvalid)
		}
		kind := binary.BigEndian.Uint32(extension[:4])
		length := int64(binary.BigEndian.Uint32(extension[4:]))
		if kind == 0 {
			if length != 0 {
				return fmt.Errorf("invalid QCOW2 extension terminator: %w", ErrInvalid)
			}

			return nil
		}
		switch kind {
		case 0xe2792aca, 0x44415441, 0x0537be77:
			return fmt.Errorf("backing file, external data and encryption extensions are unsupported: %w", ErrInvalid)
		}
		next := offset + 8 + ((length + 7) &^ 7)
		if next > end {
			return fmt.Errorf("invalid QCOW2 extension bounds: %w", ErrInvalid)
		}
		offset = next
	}

	return fmt.Errorf("missing QCOW2 extension terminator: %w", ErrInvalid)
}

func normalizeUploadedQCOW2(ctx context.Context, source, destination string, sizeLimit int64) error {
	opened, err := disk.OpenReadOnly(source)
	if err == nil {
		if err := opened.Close(); err != nil {
			return err
		}
		// Supported images are already standalone and do not need an external tool.
		return os.Rename(source, destination) //nolint:gosec // Fixed filenames in the same private import directory.
	}
	binary, err := exec.LookPath("qemu-img")
	if err != nil {
		return ErrImageToolUnavailable
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := imageTool(ctx, binary, "check", "-f", "qcow2", source); err != nil {
		return err
	}
	if err := convertUploadedQCOW2(ctx, binary, source, destination, sizeLimit); err != nil {
		return err
	}
	//nolint:gosec // Fixed output file in the private import directory.
	if err := os.Chmod(destination, 0o600); err != nil {
		return err
	}
	opened, err = disk.OpenReadOnly(destination)
	if err != nil {
		return fmt.Errorf("image could not be converted to a supported QCOW2 layout: %w", ErrInvalid)
	}
	if err := opened.Close(); err != nil {
		return err
	}
	file, err := openPrivateFile(destination, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer file.Close()
	if err := file.Sync(); err != nil {
		return err
	}

	return os.Remove(source) //nolint:gosec // Fixed uploaded file in the private import directory.
}

func imageTool(ctx context.Context, binary string, args ...string) error {
	//nolint:gosec // Located operator tool; fixed options and private generated paths, never a shell.
	cmd := exec.CommandContext(ctx, binary, args...)
	cmd.Stdout, cmd.Stderr = io.Discard, io.Discard
	cmd.SysProcAttr = &syscall.SysProcAttr{Pdeathsig: syscall.SIGKILL}
	if err := cmd.Run(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("QCOW2 validation or conversion failed: %w", ErrInvalid)
	}

	return nil
}

func convertUploadedQCOW2(ctx context.Context, binary, source, destination string, sizeLimit int64) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	finished := make(chan error, 1)
	go func() {
		finished <- imageTool(ctx, binary, "convert", "-f", "qcow2", "-O", "qcow2", "-o",
			"compat=1.1,lazy_refcounts=off,refcount_bits=16,cluster_size=65536", source, destination)
	}()
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case err := <-finished:
			if err != nil {
				return err
			}
			info, err := os.Stat(destination) //nolint:gosec // Fixed conversion output path.
			if err != nil {
				return err
			}
			if info.Size() > sizeLimit {
				return ErrImageTooLarge
			}

			return nil
		case <-ticker.C:
			info, err := os.Stat(destination) //nolint:gosec // Monitor only the private conversion output.
			if err != nil && !errors.Is(err, os.ErrNotExist) {
				cancel()
				<-finished

				return err
			}
			if err == nil && info.Size() > sizeLimit {
				cancel()
				<-finished

				return ErrImageTooLarge
			}
		}
	}
}
