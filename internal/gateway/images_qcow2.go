package gateway

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
	"github.com/define42/DevBox-Gateway-kvm/internal/qcow2import"
)

// These checks restrict uploads to standalone images before opening their
// metadata or decoding compressed clusters. See https://www.qemu.org/docs/master/interop/qcow2.html.
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

// normalizeUploadedQCOW2 keeps already-writable layouts intact. Other supported
// standalone layouts are decoded once into the native writer's canonical format.
func normalizeUploadedQCOW2(ctx context.Context, source, destination string, sizeLimit int64) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Minute)
	defer cancel()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkNormalizedImageSize(source, sizeLimit); err != nil {
		return err
	}
	opened, err := disk.OpenReadOnly(source)
	if err == nil {
		if err := opened.Close(); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		// Open verifies room for full virtual-disk allocation, including future
		// metadata. Only those layouts can bypass normalization.
		return os.Rename(source, destination) //nolint:gosec // Fixed private staging paths.
	}
	if err := convertUploadedQCOW2(ctx, source, destination, sizeLimit); err != nil {
		return err
	}
	opened, err = disk.OpenReadOnly(destination)
	if err != nil {
		return fmt.Errorf("normalized QCOW2 failed validation: %w: %w", ErrInvalid, err)
	}
	if err := opened.Close(); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return os.Remove(source) //nolint:gosec // Only the successfully normalized private upload.
}

func convertUploadedQCOW2(ctx context.Context, source, destination string, sizeLimit int64) (convertErr error) {
	input, err := openPrivateFile(source, os.O_RDONLY)
	if err != nil {
		return err
	}
	defer func() { convertErr = errors.Join(convertErr, input.Close()) }()
	reader, err := qcow2import.OpenContext(ctx, input)
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		return fmt.Errorf("read uploaded QCOW2: %w: %w", ErrInvalid, err)
	}
	defer func() { convertErr = errors.Join(convertErr, reader.Close()) }()
	output, err := disk.CreateQCOW2(destination, reader.Size())
	if err != nil {
		return err
	}
	defer func() {
		convertErr = errors.Join(convertErr, output.Close())
		if convertErr != nil {
			_ = os.Remove(destination) //nolint:gosec // Only the output created exclusively above.
		}
	}()
	checkSize := func() error { return checkNormalizedImageSize(destination, sizeLimit) }
	if err := checkSize(); err != nil {
		return err
	}
	if err := copyQCOW2Extents(ctx, reader, output, checkSize); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	return output.Sync()
}

type qcow2ImportSource interface {
	io.ReaderAt
	Size() int64
	Extent(start, length int64) (qcow2import.Extent, error)
}

// copyQCOW2Extents skips source holes and zero data. Writes are serialized because
// disk.Image owns mutable allocation metadata. At most one 64 KiB data write and
// its metadata can be added between output size checks.
func copyQCOW2Extents(ctx context.Context, source qcow2ImportSource, output io.WriterAt, checkSize func() error) error {
	buffer := make([]byte, 2<<20) // Covers the largest accepted compressed cluster.
	zero := make([]byte, 64<<10)
	for offset := int64(0); offset < source.Size(); {
		if err := ctx.Err(); err != nil {
			return err
		}
		extent, err := source.Extent(offset, source.Size()-offset)
		if err != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			return fmt.Errorf("read QCOW2 extent: %w: %w", ErrInvalid, err)
		}
		if extent.Start != offset || extent.Length <= 0 || extent.Length > source.Size()-offset {
			return fmt.Errorf("invalid QCOW2 extent bounds: %w", ErrInvalid)
		}
		end := offset + extent.Length
		if extent.Zero || !extent.Allocated {
			offset = end

			continue
		}
		for offset < end {
			if err := ctx.Err(); err != nil {
				return err
			}
			data := buffer[:min(int64(len(buffer)), end-offset)]
			if n, err := source.ReadAt(data, offset); err != nil || n != len(data) {
				if ctx.Err() != nil {
					return ctx.Err()
				}

				return fmt.Errorf("read QCOW2 data: %w: %w", ErrInvalid, errors.Join(err, io.ErrUnexpectedEOF))
			}
			for len(data) > 0 {
				if err := ctx.Err(); err != nil {
					return err
				}
				chunk := data[:min(len(data), len(zero))]
				if !bytes.Equal(chunk, zero[:len(chunk)]) {
					if n, err := output.WriteAt(chunk, offset); err != nil {
						return err
					} else if n != len(chunk) {
						return io.ErrShortWrite
					}
					if err := checkSize(); err != nil {
						return err
					}
				}
				offset += int64(len(chunk))
				data = data[len(chunk):]
			}
		}
	}

	return ctx.Err()
}

func checkNormalizedImageSize(path string, limit int64) error {
	info, err := os.Stat(path) //nolint:gosec // Server-generated private import path.
	if err != nil {
		return err
	}
	if info.Size() > limit {
		return ErrImageTooLarge
	}

	return nil
}
