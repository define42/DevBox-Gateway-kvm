package disk

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQCOW2GrowsRefcountBlocks(t *testing.T) {
	t.Parallel()

	path := createSmallClusterQCOW2(t)
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	// A 512-byte cluster has 256 refcounts, so this crosses several 128 KiB
	// host-file boundaries without needing a multi-gigabyte test fixture.
	want := bytes.Repeat([]byte{0x59, 0xab, 0x10, 0xe4}, 256*1024)
	mustWriteAt(t, img, want, 17)
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertReadAt(t, reopened, want, 17)
	if err := reopened.Close(); err != nil {
		t.Fatal(err)
	}
	runCommand(t, "qemu-img", "check", path)
}

func TestQCOW2RefcountBlockAtCoverageBoundary(t *testing.T) {
	t.Parallel()

	path := createSmallClusterQCOW2(t)
	// Place EOF at the last cluster covered by an absent refcount block.
	// Both that block and the following block must account for themselves.
	if err := os.Truncate(path, 2*128*1024-512); err != nil {
		t.Fatal(err)
	}
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer img.Close()
	want := []byte("write after two new refcount blocks")
	mustWriteAt(t, img, want, 33)
	assertReadAt(t, img, want, 33)
	if err := img.Close(); err != nil {
		t.Fatal(err)
	}
	runCommand(t, "qemu-img", "check", path)
}

func TestQCOW2RefcountTableCapacityFailureDoesNotGrowFile(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		remaining int64
	}{
		{name: "full table", remaining: 0},
		{name: "only refcount block fits", remaining: 512},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := createSmallClusterQCOW2(t)
			img, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			q, ok := img.(*qcow2Image)
			if !ok {
				t.Fatal("expected qcow2 image")
			}
			capacity := int64(len(q.refcountTable)) * int64(q.refcountBlockEntries*q.clusterSize)
			before := capacity - tc.remaining
			if err := os.Truncate(path, before); err != nil {
				t.Fatal(err)
			}
			for range 2 {
				_, err := img.WriteAt([]byte{1}, 0)
				if err == nil || !strings.Contains(err.Error(), "refcount table is too small") {
					t.Fatalf("write at refcount capacity returned %v", err)
				}
				info, err := os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
				if info.Size() != before {
					t.Fatalf("failed write grew image from %d to %d", before, info.Size())
				}
			}
			if err := img.Close(); err != nil {
				t.Fatal(err)
			}
			runCommand(t, "qemu-img", "check", path)
		})
	}
}

func createSmallClusterQCOW2(t *testing.T) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "disk.qcow2")
	runCommand(t, "qemu-img", "create", "-f", "qcow2", "-o", "cluster_size=512", path, "2M")

	return path
}
