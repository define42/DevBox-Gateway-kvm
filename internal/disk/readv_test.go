package disk

import (
	"bytes"
	"errors"
	"io"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// splitBuffers divides data into a random list of buffers, including empty
// ones, that alias it in order.
func splitBuffers(data []byte, rng *rand.Rand) [][]byte {
	var buffers [][]byte
	for len(data) > 0 {
		n := min(len(data), rng.IntN(1500))
		buffers = append(buffers, data[:n])
		data = data[n:]
	}

	return append(buffers, nil)
}

// assertReadvMatchesReadAt compares ReadvAt with ReadAt of the same range,
// including its count and error at the end of the image.
func assertReadvMatchesReadAt(t *testing.T, img Image, off int64, length int, rng *rand.Rand) {
	t.Helper()
	want := make([]byte, length)
	wantN, wantErr := img.ReadAt(want, off)
	got := make([]byte, length)
	buffers := splitBuffers(got, rng)
	lengths := make([]int, len(buffers))
	for i, b := range buffers {
		lengths[i] = len(b)
	}
	n, err := img.(VectorReader).ReadvAt(buffers, off)
	if n != wantN || (err == nil) != (wantErr == nil) || errors.Is(wantErr, io.EOF) != errors.Is(err, io.EOF) {
		t.Fatalf("ReadvAt(%d, %d) = (%d, %v), ReadAt = (%d, %v)", off, length, n, err, wantN, wantErr)
	}
	if !bytes.Equal(got[:n], want[:wantN]) {
		t.Fatalf("ReadvAt(%d, %d) returned different bytes from ReadAt", off, length)
	}
	for i, b := range buffers {
		if len(b) != lengths[i] {
			t.Fatal("ReadvAt changed the caller's buffer list")
		}
	}
}

func TestQCOW2ReadvMatchesReadAt(t *testing.T) {
	t.Parallel()
	q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
	const clusterSize = 512
	// Allocating out of guest order breaks contiguous host runs; clusters 0, 2
	// and 10 onward stay unallocated and read as zero.
	for _, cluster := range []int{9, 3, 4, 5, 6, 7, 8, 1} {
		data := make([]byte, clusterSize)
		for i := range data {
			data[i] = byte(cluster*31 + i)
		}
		mustWriteAt(t, q, data, int64(cluster*clusterSize))
	}
	rng := rand.New(rand.NewPCG(1, 2)) //nolint:gosec // Reproducible test offsets.
	for range 300 {
		assertReadvMatchesReadAt(t, q, rng.Int64N(12*clusterSize), 1+rng.IntN(10*clusterSize), rng)
	}
	// Reads ending at and running past the end of the virtual disk.
	end := q.Size()
	assertReadvMatchesReadAt(t, q, end-700, 700, rng)
	assertReadvMatchesReadAt(t, q, end-700, 2000, rng)
	assertReadvMatchesReadAt(t, q, end, 10, rng)
}

func TestRawReadvMatchesReadAt(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "disk.raw")
	data := make([]byte, 64*1024)
	for i := range data {
		data[i] = byte(i * 7)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	img, err := OpenRawReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = img.Close() })
	rng := rand.New(rand.NewPCG(3, 4)) //nolint:gosec // Reproducible test offsets.
	for range 100 {
		assertReadvMatchesReadAt(t, img, rng.Int64N(int64(len(data))), 1+rng.IntN(8192), rng)
	}
	assertReadvMatchesReadAt(t, img, int64(len(data))-100, 300, rng)
}

func TestReadvInvalidRequests(t *testing.T) {
	t.Parallel()
	q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
	path := filepath.Join(t.TempDir(), "disk.raw")
	if err := os.WriteFile(path, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	raw, err := OpenRawReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = raw.Close() })
	for _, img := range []Image{q, raw} {
		reader := img.(VectorReader)
		if n, err := reader.ReadvAt([][]byte{make([]byte, 8)}, -1); n != 0 || err == nil {
			t.Fatalf("negative offset = (%d, %v), want an error", n, err)
		}
		if n, err := reader.ReadvAt([][]byte{nil, {}}, 0); n != 0 || err != nil {
			t.Fatalf("empty buffers = (%d, %v), want (0, nil)", n, err)
		}
	}
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := q.ReadvAt([][]byte{make([]byte, 8)}, 0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed ReadvAt = %v, want closed", err)
	}
}
