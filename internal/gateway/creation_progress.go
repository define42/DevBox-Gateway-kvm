package gateway

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"
)

// CreateProgress reports a creation stage and logical image-file copy progress,
// including bytes preserved as sparse holes. Byte counts apply only to copying;
// phases do not imply percentages.
type CreateProgress struct {
	Phase       string `json:"phase"`
	CopiedBytes int64  `json:"copiedBytes"`
	TotalBytes  int64  `json:"totalBytes"`
}

// CreationProgressBackend adds synchronous VM creation progress notifications.
// It reports preparing, copying, configuring and finalizing; successful completion
// is determined by the returned VM and error, not by an early progress event.
type CreationProgressBackend interface {
	CreateWithProgress(
		ctx context.Context, owner, name, passwordHash, imageID string, report func(CreateProgress),
	) (VM, error)
}

func reportCreationProgress(ctx context.Context, report func(CreateProgress), progress CreateProgress) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if report != nil {
		report(progress)
	}

	return ctx.Err()
}

func copyFileWithProgress(
	ctx context.Context, input io.Reader, output io.Writer, total int64, report func(CreateProgress),
) error {
	if source, ok := input.(*os.File); ok {
		if target, ok := output.(*os.File); ok {
			return copySparseFileWithProgress(ctx, source, target, total, report)
		}
	}

	return copyStreamWithProgress(ctx, input, output, total, report)
}

func copyStreamWithProgress(
	ctx context.Context, input io.Reader, output io.Writer, total int64, report func(CreateProgress),
) error {
	buffer := make([]byte, 1<<20)
	var copied int64
	lastReport := time.Now()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		n, readErr := input.Read(buffer)
		if n > 0 {
			written, err := output.Write(buffer[:n])
			copied += int64(written)
			if err != nil {
				return err
			}
			if written != n {
				return io.ErrShortWrite
			}
			if copied > total {
				return fmt.Errorf("source image size changed during copy: %w", ErrInvalid)
			}
		}
		if n > 0 && report != nil && copied < total && time.Since(lastReport) >= 100*time.Millisecond {
			if err := reportCreationProgress(ctx, report, CreateProgress{
				Phase: "copying", CopiedBytes: copied, TotalBytes: total,
			}); err != nil {
				return err
			}
			lastReport = time.Now()
		}
		if errors.Is(readErr, io.EOF) {
			if copied != total {
				return io.ErrUnexpectedEOF
			}

			return reportCreationProgress(ctx, report, CreateProgress{
				Phase: "copying", CopiedBytes: copied, TotalBytes: total,
			})
		}
		if readErr != nil {
			return readErr
		}
	}
}
