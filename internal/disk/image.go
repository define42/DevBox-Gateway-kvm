package disk

import (
	"errors"
	"io"
	"os"
)

// Image is a random-access virtual disk backing store. ReadAt calls may run
// concurrently with non-overlapping destination buffers; their completion order
// is unspecified. Sync may also run concurrently with ReadAt and WriteAt: it
// makes every write that returned before Sync was called durable. Callers must
// serialize WriteAt against reads and other writes, and Close against every
// operation, to preserve request ordering and handle lifetime.
type Image interface {
	io.ReaderAt
	io.WriterAt
	Size() int64
	Sync() error
	Close() error
}

const qcow2Magic = "QFI\xfb"

var errRawWriteOutOfBounds = errors.New("raw: write exceeds disk size")

type rawImage struct {
	file     *os.File
	size     int64
	readOnly bool
}

// Open opens a writable QCOW2 image whose metadata can cover its full capacity.
// Raw data and invalid or unsupported QCOW2 layouts are rejected.
func Open(path string) (Image, error) {
	return open(path, os.O_RDWR)
}

// OpenReadOnly opens a read-only QCOW2 image using the same layout and growth
// validation as Open, so accepted templates can later be used as writable disks.
func OpenReadOnly(path string) (Image, error) {
	return open(path, os.O_RDONLY)
}

func open(path string, flag int) (Image, error) {
	file, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, err
	}
	if _, err := InspectQCOW2(file); err != nil {
		_ = file.Close()

		return nil, err
	}

	return openQCOW2(file)
}

// OpenRaw opens writable raw storage without interpreting any header bytes.
// Production VM boot disks use Open or OpenQCOW2WithSize instead.
func OpenRaw(path string) (Image, error) {
	return openRaw(path, os.O_RDWR, false)
}

// OpenRawReadOnly opens raw read-only media, including cloud-init seed ISOs,
// without interpreting any header bytes.
func OpenRawReadOnly(path string) (Image, error) {
	return openRaw(path, os.O_RDONLY, true)
}

func openRaw(path string, flag int, readOnly bool) (Image, error) {
	file, err := os.OpenFile(path, flag, 0)
	if err != nil {
		return nil, err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()

		return nil, err
	}

	return &rawImage{
		file:     file,
		size:     info.Size(),
		readOnly: readOnly,
	}, nil
}

func (r *rawImage) ReadAt(b []byte, off int64) (int, error) {
	return r.file.ReadAt(b, off)
}

func (r *rawImage) WriteAt(b []byte, off int64) (int, error) {
	if len(b) == 0 {
		return 0, nil
	}
	if off < 0 || off > r.size || int64(len(b)) > r.size-off {
		return 0, errRawWriteOutOfBounds
	}

	return r.file.WriteAt(b, off)
}

func (r *rawImage) Size() int64 {
	return r.size
}

func (r *rawImage) Sync() error {
	if r.readOnly {
		return nil
	}

	return r.file.Sync()
}

func (r *rawImage) Close() error {
	return r.file.Close()
}
