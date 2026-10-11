package disk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestQCOW2RefcountCacheAllocationReopen(t *testing.T) {
	t.Parallel()
	path := createEmptyCacheTestQCOW2(t)
	image, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	q := image.(*qcow2Image)
	// Warm the original block before allocating data, then cross several
	// refcount-block boundaries. Both cached and new blocks must stay current.
	if got, err := q.refcount(0); err != nil || got != 1 {
		t.Fatalf("initial refcount: got (%d, %v), want (1, nil)", got, err)
	}
	want := bytes.Repeat([]byte{0x59, 0xab, 0x10, 0xe4}, 64*1024)
	mustWriteAt(t, image, want, 17)
	mustWriteAt(t, image, []byte("overwrite"), 17)
	copy(want, "overwrite")
	if err := image.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertReadAt(t, reopened, want, 17)
}

func TestQCOW2RefcountCacheWriteThrough(t *testing.T) {
	t.Parallel()
	q := createRefcountCacheTestImage(t, 512, 1)
	if _, err := q.refcount(0); err != nil {
		t.Fatal(err)
	}
	if err := q.setRefcount(0, 7); err != nil {
		t.Fatal(err)
	}
	if got, err := q.refcount(0); err != nil || got != 7 {
		t.Fatalf("cached refcount: got (%d, %v), want (7, nil)", got, err)
	}
	// A separately opened handle observes the update without eviction or Sync.
	file, err := os.Open(q.file.Name())
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	var data [2]byte
	if _, err := file.ReadAt(data[:], int64(q.refcountTable[0])); err != nil {
		t.Fatal(err)
	}
	if got := binary.BigEndian.Uint16(data[:]); got != 7 {
		t.Fatalf("on-disk refcount: got %d, want 7", got)
	}
}

func TestQCOW2RefcountCacheWriteFailureInvalidates(t *testing.T) {
	t.Parallel()
	q := createRefcountCacheTestImage(t, 512, 1)
	if _, err := q.refcount(0); err != nil {
		t.Fatal(err)
	}
	path := q.file.Name()
	if err := q.file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := q.setRefcount(0, 7); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("setRefcount: got %v, want closed-file error", err)
	}
	// The failed write must discard the cached value: this read should attempt
	// host I/O and fail too, rather than returning a potentially stale refcount.
	if _, err := q.refcount(0); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("refcount after failed write: got %v, want closed-file error", err)
	}
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	q.file = file
	if got, err := q.refcount(0); err != nil || got != 1 {
		t.Fatalf("refcount after reopen: got (%d, %v), want (1, nil)", got, err)
	}
}

func TestQCOW2RefcountCacheEviction(t *testing.T) {
	t.Parallel()
	const clusterSize = 64 * 1024
	const blockCount = qcow2RefcountCacheBytes/clusterSize + 3
	q := createRefcountCacheTestImage(t, clusterSize, blockCount)
	if _, err := q.refcount(0); err != nil {
		t.Fatal(err)
	}
	if err := q.setRefcount(0, 123); err != nil {
		t.Fatal(err)
	}
	for block := 1; block < blockCount; block++ {
		offset := uint64(block) * q.refcountBlockEntries * q.clusterSize
		if got, err := q.refcount(offset); err != nil || got != uint16(block+1) {
			t.Fatalf("block %d refcount: got (%d, %v), want (%d, nil)", block, got, err, block+1)
		}
	}
	var cachedBytes int
	for _, block := range q.refcountBlocks {
		cachedBytes += len(block.data)
	}
	if cachedBytes > qcow2RefcountCacheBytes {
		t.Fatalf("cached bytes: got %d, limit %d", cachedBytes, qcow2RefcountCacheBytes)
	}
	if _, cached := q.refcountCache[q.refcountTable[0]]; cached {
		t.Fatal("first block remains cached after exceeding the cache budget")
	}
	if got, err := q.refcount(0); err != nil || got != 123 {
		t.Fatalf("reloaded refcount: got (%d, %v), want (123, nil)", got, err)
	}
}

func createRefcountCacheTestImage(t *testing.T, clusterSize, blockCount int) *qcow2Image {
	t.Helper()
	file, err := os.Create(filepath.Join(t.TempDir(), "refcounts"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	q := &qcow2Image{
		file:                 file,
		clusterSize:          uint64(clusterSize),
		refcountBlockEntries: uint64(clusterSize / 2),
		refcountTable:        make([]uint64, blockCount),
	}
	data := make([]byte, clusterSize)
	for block := range blockCount {
		offset := uint64((block + 1) * clusterSize)
		q.refcountTable[block] = offset
		binary.BigEndian.PutUint16(data, uint16(block+1))
		if _, err := file.WriteAt(data, int64(offset)); err != nil {
			t.Fatal(err)
		}
	}

	return q
}

// A small-cluster standalone image makes refcount growth exercise kilobytes
// instead of gigabytes, without requiring qemu-img for these regression tests.
func createEmptyCacheTestQCOW2(t *testing.T) string {
	t.Helper()
	const clusterSize = 512
	metadata := make([]byte, 4*clusterSize)
	copy(metadata, qcow2Magic)
	binary.BigEndian.PutUint32(metadata[4:8], qcow2Version3)
	binary.BigEndian.PutUint32(metadata[20:24], 9)
	binary.BigEndian.PutUint64(metadata[24:32], 512*1024)
	binary.BigEndian.PutUint32(metadata[36:40], 16)
	binary.BigEndian.PutUint64(metadata[40:48], 3*clusterSize)
	binary.BigEndian.PutUint64(metadata[48:56], clusterSize)
	binary.BigEndian.PutUint32(metadata[56:60], 1)
	binary.BigEndian.PutUint32(metadata[96:100], qcow2RefcountOrder16)
	binary.BigEndian.PutUint32(metadata[100:104], qcow2HeaderV3Len)
	binary.BigEndian.PutUint64(metadata[clusterSize:], 2*clusterSize)
	for cluster := range 4 {
		binary.BigEndian.PutUint16(metadata[2*clusterSize+cluster*2:], 1)
	}
	path := filepath.Join(t.TempDir(), "disk.qcow2")
	if err := os.WriteFile(path, metadata, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}
