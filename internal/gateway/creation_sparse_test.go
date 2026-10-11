package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"io"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestCopySparseFilePreservesContentsAndHoles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		offsets []int64
	}{
		{name: "all holes"},
		{name: "leading hole", offsets: []int64{4 << 20}},
		{name: "trailing hole", offsets: []int64{0}},
		{name: "fragmented unaligned", offsets: []int64{17, (2 << 20) + 31, (8 << 20) + 9}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			for _, mode := range []string{"extents", "zero scan"} {
				t.Run(mode, func(t *testing.T) {
					t.Parallel()
					const size = (8 << 20) + 17
					input := newSparseTestFile(t, "source")
					if err := input.Truncate(size); err != nil {
						t.Fatal(err)
					}
					for _, offset := range tc.offsets {
						if _, err := input.WriteAt([]byte("marker"), offset); err != nil {
							t.Fatal(err)
						}
					}
					output := newSparseTestFile(t, "destination")
					// Existing destination bytes must not survive skipped holes.
					if _, err := output.WriteAt(bytes.Repeat([]byte{0xff}, 4096), 1<<20); err != nil {
						t.Fatal(err)
					}
					var last CreateProgress
					report := func(progress CreateProgress) { last = progress }
					copyFile := copyFileWithProgress
					if mode == "zero scan" {
						copyFile = func(ctx context.Context, in io.Reader, out io.Writer, total int64,
							report func(CreateProgress),
						) error {
							return copySparseStream(ctx, in.(*os.File), out.(*os.File), total, report)
						}
					}
					if err := copyFile(t.Context(), input, output, size, report); err != nil {
						t.Fatal(err)
					}
					if last.CopiedBytes != size || last.TotalBytes != size {
						t.Fatalf("incomplete logical progress: %+v", last)
					}
					assertSparseCopy(t, input, output, size)
				})
			}
		})
	}
}

func newSparseTestFile(t *testing.T, name string) *os.File {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), name))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	return file
}

func assertSparseCopy(t *testing.T, input, output *os.File, size int64) {
	t.Helper()
	var hashes [2][]byte
	for i, file := range []*os.File{input, output} {
		info, err := file.Stat()
		if err != nil {
			t.Fatal(err)
		}
		if info.Size() != size {
			t.Fatalf("size = %d, want %d", info.Size(), size)
		}
		hash := sha256.New()
		if _, err := io.Copy(hash, io.NewSectionReader(file, 0, size)); err != nil {
			t.Fatal(err)
		}
		hashes[i] = hash.Sum(nil)
	}
	if !bytes.Equal(hashes[0], hashes[1]) {
		t.Fatal("copy changed image contents")
	}
	source, _ := input.Stat()
	target, _ := output.Stat()
	if source.Sys().(*syscall.Stat_t).Blocks*512 >= size/4 {
		t.Log("filesystem does not expose a sparse source; content verified")

		return
	}
	allocated := target.Sys().(*syscall.Stat_t).Blocks * 512
	t.Logf("logical=%d bytes, source allocation=%d bytes, destination allocation=%d bytes",
		size, source.Sys().(*syscall.Stat_t).Blocks*512, allocated)
	if allocated >= size/4 {
		t.Fatalf("sparse copy allocated %d of %d bytes", allocated, size)
	}
}

func TestCopySparseFileRejectsCancellationAndSizeChange(t *testing.T) {
	t.Parallel()
	input := newSparseTestFile(t, "source")
	output := newSparseTestFile(t, "destination")
	if err := input.Truncate(4096); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if err := copyFileWithProgress(ctx, input, output, 4096, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled copy: %v", err)
	}
	if err := copyFileWithProgress(t.Context(), input, output, 8192, nil); !errors.Is(err, ErrInvalid) {
		t.Fatalf("changed source size: %v", err)
	}
	info, err := output.Stat()
	if err != nil || info.Size() != 0 {
		t.Fatalf("invalid copy modified destination: %v, %v", info, err)
	}
}
