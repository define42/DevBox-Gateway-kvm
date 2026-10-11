package gateway

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"golang.org/x/sys/unix"
)

// copySparseFileWithProgress skips source holes instead of materializing an
// entire virtual disk when reflinks are unavailable. Progress counts logical
// bytes, including holes, while only populated extents consume write bandwidth.
func copySparseFileWithProgress(
	ctx context.Context, input, output *os.File, total int64, report func(CreateProgress),
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := checkCopySize(input, total); err != nil {
		return err
	}
	data, err := unix.Seek(int(input.Fd()), 0, unix.SEEK_DATA)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) {
		return copySparseStream(ctx, input, output, total, report)
	}
	if err != nil && !errors.Is(err, unix.ENXIO) {
		return err
	}
	if err := output.Truncate(0); err != nil {
		return err
	}
	buffer := make([]byte, 1<<20)
	lastReport := time.Now()
	for err == nil && data < total {
		hole, seekErr := unix.Seek(int(input.Fd()), data, unix.SEEK_HOLE)
		if seekErr != nil {
			return seekErr
		}
		if hole <= data || hole > total {
			return fmt.Errorf("source image extent changed during copy: %w", ErrInvalid)
		}
		if err := copyDataExtent(ctx, input, output, buffer, data, hole, func(copied int64) error {
			if time.Since(lastReport) < 100*time.Millisecond {
				return ctx.Err()
			}
			lastReport = time.Now()

			return reportCreationProgress(ctx, report, CreateProgress{
				Phase: "copying", CopiedBytes: copied, TotalBytes: total,
			})
		}); err != nil {
			return err
		}
		data, err = unix.Seek(int(input.Fd()), hole, unix.SEEK_DATA)
	}
	if err != nil && !errors.Is(err, unix.ENXIO) {
		return err
	}
	if err := checkCopySize(input, total); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := output.Truncate(total); err != nil {
		return err
	}

	return reportCreationProgress(ctx, report, CreateProgress{
		Phase: "copying", CopiedBytes: total, TotalBytes: total,
	})
}

func copyDataExtent(
	ctx context.Context, input, output *os.File, buffer []byte, offset, end int64, progress func(int64) error,
) error {
	for offset < end {
		if err := ctx.Err(); err != nil {
			return err
		}
		chunk := buffer[:min(int64(len(buffer)), end-offset)]
		if _, err := input.ReadAt(chunk, offset); err != nil {
			return err
		}
		n, err := output.WriteAt(chunk, offset)
		if err != nil {
			return err
		}
		if n != len(chunk) {
			return io.ErrShortWrite
		}
		offset += int64(n)
		if err := progress(offset); err != nil {
			return err
		}
	}

	return nil
}

func checkCopySize(input *os.File, total int64) error {
	info, err := input.Stat()
	if err != nil {
		return err
	}
	if info.Size() != total {
		return fmt.Errorf("source image size changed during copy: %w", ErrInvalid)
	}

	return nil
}

// Without extent queries the fallback still preserves zero-filled blocks,
// at the cost of reading them.
func copySparseStream(ctx context.Context, input, output *os.File, total int64, report func(CreateProgress)) error {
	if _, err := input.Seek(0, io.SeekStart); err != nil {
		return err
	}
	if err := output.Truncate(0); err != nil {
		return err
	}
	if _, err := output.Seek(0, io.SeekStart); err != nil {
		return err
	}
	writer := sparseCopyWriter{file: output}
	if err := copyStreamWithProgress(ctx, input, &writer, total, report); err != nil {
		return err
	}

	return output.Truncate(total)
}

type sparseCopyWriter struct {
	file *os.File
	zero [4096]byte
}

func (w *sparseCopyWriter) Write(data []byte) (int, error) {
	written := 0
	for len(data) > 0 {
		end := min(len(data), len(w.zero))
		zero := bytes.Equal(data[:end], w.zero[:end])
		for end < len(data) {
			next := min(end+len(w.zero), len(data))
			if bytes.Equal(data[end:next], w.zero[:next-end]) != zero {
				break
			}
			end = next
		}
		if zero {
			if _, err := w.file.Seek(int64(end), io.SeekCurrent); err != nil {
				return written, err
			}
		} else {
			n, err := w.file.Write(data[:end])
			if err != nil {
				return written + n, err
			}
			if n != end {
				return written + n, io.ErrShortWrite
			}
		}
		written += end
		data = data[end:]
	}

	return written, nil
}
