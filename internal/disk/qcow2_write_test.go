package disk

import (
	"bytes"
	"errors"
	"io"
	"os"
	"syscall"
	"testing"
)

type qcow2MetadataWriterFunc func([]byte, int64) (int, error)

func (write qcow2MetadataWriterFunc) WriteAt(data []byte, offset int64) (int, error) {
	return write(data, offset)
}

func TestQCOW2FailedPointerPublicationCanRetry(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"l1", "l2"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			for _, failure := range []struct {
				name    string
				written int
				err     error
				wantErr error
			}{
				{name: "no bytes", err: syscall.EIO, wantErr: syscall.EIO},
				{name: "partial write", written: 7, err: syscall.EIO, wantErr: syscall.EIO},
				{name: "short write", written: 7, wantErr: io.ErrShortWrite},
			} {
				t.Run(failure.name, func(t *testing.T) {
					t.Parallel()
					path := createEmptyCacheTestQCOW2(t)
					q := openWriteTestQCOW2(t, path)
					guestOffset := int64(0)
					pointerOffset := int64(q.l1TableOffset)
					if table == "l2" {
						mustWriteAt(t, q, []byte("existing data"), 0)
						guestOffset = int64(q.clusterSize)
						pointerOffset = int64(clusterOffset(q.l1[0]) + 8)
					}
					before := readWriteTestFile(t, path)
					injectMetadataFailure(q, pointerOffset, failure.written, failure.err)
					if n, err := q.WriteAt([]byte("failed write"), guestOffset); n != 0 || !errors.Is(err, failure.wantErr) {
						t.Fatalf("failed WriteAt = (%d, %v), want (0, %v)", n, err, failure.wantErr)
					}
					if !bytes.Equal(readWriteTestFile(t, path), before) {
						t.Fatal("failed pointer publication changed the image after recovery")
					}
					if table == "l1" && (q.l1[0] != 0 || q.l2Cache[0] != nil) {
						t.Fatal("unpublished l2 table remained cached")
					}
					assertReadAt(t, q, make([]byte, 12), guestOffset)
					if err := q.Sync(); err != nil {
						t.Fatal(err)
					}
					// Reopen the recovered file before retrying on the original
					// handle: both disk metadata and cached state must be safe.
					recovered := openWriteTestQCOW2(t, path)
					assertReadAt(t, recovered, make([]byte, 12), guestOffset)
					if err := recovered.Close(); err != nil {
						t.Fatal(err)
					}
					runCommand(t, "qemu-img", "check", path)
					want := []byte("successful retry")
					mustWriteAt(t, q, want, guestOffset)
					if err := q.Close(); err != nil {
						t.Fatal(err)
					}
					reopened := openWriteTestQCOW2(t, path)
					assertReadAt(t, reopened, want, guestOffset)
					if table == "l2" {
						assertReadAt(t, reopened, []byte("existing data"), 0)
					}
					if err := reopened.Close(); err != nil {
						t.Fatal(err)
					}
					runCommand(t, "qemu-img", "check", path)
				})
			}
		})
	}
}

func TestQCOW2FailedRefcountAllocationCanRetry(t *testing.T) {
	t.Parallel()

	path := createEmptyCacheTestQCOW2(t)
	q := openWriteTestQCOW2(t, path)
	before := readWriteTestFile(t, path)
	clusterOffset := uint64(len(before))
	blockOffset, index, _, err := q.refcountLocation(clusterOffset)
	if err != nil {
		t.Fatal(err)
	}
	// The entire refcount reaches disk despite the write returning an error.
	injectMetadataFailure(q, int64(blockOffset+index*2), 2, syscall.EIO)
	if _, err := q.WriteAt([]byte{1}, 0); !errors.Is(err, syscall.EIO) {
		t.Fatalf("WriteAt returned %v, want injected failure", err)
	}
	if !bytes.Equal(readWriteTestFile(t, path), before) {
		t.Fatal("failed refcount allocation changed the image after recovery")
	}
	if err := q.Sync(); err != nil {
		t.Fatal(err)
	}
	runCommand(t, "qemu-img", "check", path)
	mustWriteAt(t, q, []byte("retry after refcount failure"), 0)
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	runCommand(t, "qemu-img", "check", path)
}

func TestQCOW2FailedRefcountBlockPublicationCanRetry(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"block", "table"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			const oldSize = 128 * 1024
			if err := os.Truncate(path, oldSize); err != nil {
				t.Fatal(err)
			}
			q := openWriteTestQCOW2(t, path)
			before := readWriteTestFile(t, path)
			offset := int64(oldSize)
			if target == "table" {
				offset = int64(q.refcountTableOffset + 8)
			}
			injectMetadataFailure(q, offset, 7, syscall.EIO)
			if _, err := q.WriteAt([]byte{1}, 0); !errors.Is(err, syscall.EIO) {
				t.Fatalf("WriteAt returned %v, want injected failure", err)
			}
			if !bytes.Equal(readWriteTestFile(t, path), before) {
				t.Fatal("failed refcount block publication changed the image after recovery")
			}
			if q.refcountTable[1] != 0 {
				t.Fatal("unpublished refcount block remained cached")
			}
			if err := q.Sync(); err != nil {
				t.Fatal(err)
			}
			runCommand(t, "qemu-img", "check", path)
			want := []byte("retry after new refcount block failure")
			mustWriteAt(t, q, want, 0)
			if err := q.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openWriteTestQCOW2(t, path)
			assertReadAt(t, reopened, want, 0)
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			runCommand(t, "qemu-img", "check", path)
		})
	}
}

func TestQCOW2FailedPublicationPreservesNewRefcountBlock(t *testing.T) {
	t.Parallel()

	for _, table := range []string{"l1", "l2"} {
		t.Run(table, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			q := openWriteTestQCOW2(t, path)
			guestOffset := int64(0)
			pointerOffset := int64(q.l1TableOffset)
			if table == "l2" {
				mustWriteAt(t, q, []byte("existing data"), 0)
				guestOffset = int64(q.clusterSize)
				pointerOffset = int64(clusterOffset(q.l1[0]) + 8)
			}
			const blockOffset = 128 * 1024
			if err := q.file.Truncate(blockOffset); err != nil {
				t.Fatal(err)
			}
			injectMetadataFailure(q, pointerOffset, 7, syscall.EIO)
			if _, err := q.WriteAt([]byte{1}, guestOffset); !errors.Is(err, syscall.EIO) {
				t.Fatalf("WriteAt returned %v, want injected failure", err)
			}
			if q.refcountTable[1] != blockOffset {
				t.Fatal("recovery removed a successfully published refcount block")
			}
			if got := len(readWriteTestFile(t, path)); got != blockOffset+int(q.clusterSize) {
				t.Fatalf("recovered file size = %d, want refcount block only", got)
			}
			if err := q.Sync(); err != nil {
				t.Fatal(err)
			}
			runCommand(t, "qemu-img", "check", path)
			want := []byte("retry keeps the new refcount block")
			mustWriteAt(t, q, want, guestOffset)
			if err := q.Close(); err != nil {
				t.Fatal(err)
			}
			reopened := openWriteTestQCOW2(t, path)
			assertReadAt(t, reopened, want, guestOffset)
			if table == "l2" {
				assertReadAt(t, reopened, []byte("existing data"), 0)
			}
			if err := reopened.Close(); err != nil {
				t.Fatal(err)
			}
			runCommand(t, "qemu-img", "check", path)
		})
	}
}

func TestQCOW2FailedMetadataRecoveryDisablesWrites(t *testing.T) {
	t.Parallel()

	for _, target := range []string{"l1", "l2", "refcount", "refcount table"} {
		t.Run(target, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			if target == "refcount table" {
				if err := os.Truncate(path, 128*1024); err != nil {
					t.Fatal(err)
				}
			}
			q := openWriteTestQCOW2(t, path)
			guestOffset := int64(0)
			pointerOffset := int64(q.l1TableOffset)
			if target == "l2" {
				mustWriteAt(t, q, []byte("existing data"), 0)
				guestOffset = int64(q.clusterSize)
				pointerOffset = int64(clusterOffset(q.l1[0]) + 8)
			}
			beforeSize := len(readWriteTestFile(t, path))
			if target == "refcount" {
				blockOffset, index, _, err := q.refcountLocation(uint64(beforeSize))
				if err != nil {
					t.Fatal(err)
				}
				pointerOffset = int64(blockOffset + index*2)
			}
			if target == "refcount table" {
				pointerOffset = int64(q.refcountTableOffset + 8)
			}
			// Fail the attempted update and its restoration. A possibly live
			// pointer must retain its target, and the handle must stop writing.
			failed := false
			q.metadataWriter = qcow2MetadataWriterFunc(func(data []byte, offset int64) (int, error) {
				if offset != pointerOffset {
					return q.file.WriteAt(data, offset)
				}
				if failed {
					return 0, syscall.EIO
				}
				failed = true
				n, err := q.file.WriteAt(data, offset)
				if err != nil {
					return n, err
				}

				return n, syscall.EIO
			})
			if _, err := q.WriteAt([]byte{1}, guestOffset); !errors.Is(err, syscall.EIO) {
				t.Fatalf("WriteAt returned %v, want injected failure", err)
			}
			if q.writeFault == nil {
				t.Fatal("failed metadata recovery did not disable writes")
			}
			after := readWriteTestFile(t, path)
			if len(after) < beforeSize+int(q.clusterSize) {
				t.Fatal("failed recovery removed the possibly referenced target")
			}
			q.metadataWriter = nil
			if n, err := q.WriteAt([]byte("must not write"), guestOffset); n != 0 || !errors.Is(err, syscall.EIO) {
				t.Fatalf("WriteAt after failed recovery = (%d, %v)", n, err)
			}
			if err := q.Sync(); !errors.Is(err, syscall.EIO) {
				t.Fatalf("Sync after failed recovery returned %v", err)
			}
			if !bytes.Equal(readWriteTestFile(t, path), after) {
				t.Fatal("disabled writer changed the image")
			}
		})
	}
}

func openWriteTestQCOW2(t *testing.T, path string) *qcow2Image {
	t.Helper()
	img, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = img.Close() })
	q, ok := img.(*qcow2Image)
	if !ok {
		t.Fatal("expected qcow2 image")
	}

	return q
}

func readWriteTestFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return data
}

// Write the requested prefix before returning the injected error. Returning a
// nil error with a prefix also exercises a malformed backend's short write.
func injectMetadataFailure(q *qcow2Image, offset int64, written int, failure error) {
	failed := false
	q.metadataWriter = qcow2MetadataWriterFunc(func(data []byte, writeOffset int64) (int, error) {
		if writeOffset != offset || failed {
			return q.file.WriteAt(data, writeOffset)
		}
		failed = true
		if written > 0 {
			n, err := q.file.WriteAt(data[:written], writeOffset)
			if err != nil {
				return n, err
			}
		}

		return written, failure
	})
}
