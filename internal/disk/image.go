package disk

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// Image is a random-access virtual disk backing store. ReadAt calls may run
// concurrently with non-overlapping destination buffers; their completion order
// is unspecified. Callers must serialize WriteAt, Sync and Close against other
// operations to preserve request ordering, flush durability and handle lifetime.
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

func Open(path string) (Image, error) {
	return open(path, os.O_RDWR, false)
}

func OpenReadOnly(path string) (Image, error) {
	return open(path, os.O_RDONLY, true)
}

func open(path string, flag int, readOnly bool) (Image, error) {
	file, err := os.OpenFile(path, flag, 0o644)
	if err != nil {
		return nil, err
	}

	var magic [4]byte
	n, err := file.ReadAt(magic[:], 0)
	if err != nil && !errors.Is(err, io.EOF) {
		_ = file.Close()

		return nil, err
	}

	if n == len(magic) && bytes.Equal(magic[:], []byte(qcow2Magic)) {
		return openQCOW2(file)
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
