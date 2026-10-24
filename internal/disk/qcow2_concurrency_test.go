package disk

import (
	"bytes"
	"errors"
	"os"
	"sync"
	"testing"
	"time"
)

type qcow2DataReaderFunc func([]byte, int64) (int, error)

func (read qcow2DataReaderFunc) ReadAt(data []byte, offset int64) (int, error) {
	return read(data, offset)
}

func TestQCOW2ConcurrentWarmReads(t *testing.T) {
	t.Parallel()
	q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
	want := bytes.Repeat([]byte{0x59}, 512)
	mustWriteAt(t, q, want, 0)
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	q.dataReader = qcow2DataReaderFunc(func(data []byte, off int64) (int, error) {
		entered <- struct{}{}
		<-release

		return q.file.ReadAt(data, off)
	})
	var readers sync.WaitGroup
	defer readers.Wait()
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	for range 2 {
		readers.Go(func() { checkConcurrentQCOW2Read(t, q, want, 0) })
	}
	// Both data reads must reach the backing store before either is released.
	for range 2 {
		waitQCOW2Signal(t, entered)
	}
	unblock()
}

func TestQCOW2ConcurrentColdReads(t *testing.T) {
	t.Parallel()
	path := createEmptyCacheTestQCOW2(t)
	q := openWriteTestQCOW2(t, path)
	// This spans three lazy L2 tables in the small-cluster fixture.
	want := bytes.Repeat([]byte{0x59, 0xab, 0x10, 0xe4}, 18*1024)
	mustWriteAt(t, q, want, 17)
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	q = openWriteTestQCOW2(t, path)
	start := make(chan struct{})
	var readers sync.WaitGroup
	for worker := range 16 {
		readers.Go(func() {
			<-start
			offset := worker * 4093
			checkConcurrentQCOW2Read(t, q, want[offset:offset+4096], int64(offset+17))
		})
	}
	close(start)
	readers.Wait()
	assertReadAt(t, q, want, 17)
}

func TestQCOW2ConcurrentReadsAndWrites(t *testing.T) {
	t.Parallel()
	q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
	first := bytes.Repeat([]byte{0x59}, 64*1024)
	second := bytes.Repeat([]byte{0xab}, len(first))
	mustWriteAt(t, q, first, 0)
	start := make(chan struct{})
	var workers sync.WaitGroup
	for range 4 {
		workers.Go(func() {
			<-start
			got := make([]byte, len(first))
			for range 32 {
				n, err := q.ReadAt(got, 0)
				if err != nil || n != len(got) {
					t.Errorf("ReadAt = (%d, %v), want (%d, nil)", n, err, len(got))

					return
				}
				if !bytes.Equal(got, first) && !bytes.Equal(got, second) {
					t.Error("read observed a partially completed multi-cluster write")

					return
				}
			}
		})
	}
	workers.Go(func() {
		<-start
		for iteration := range 32 {
			data := first
			if iteration%2 == 0 {
				data = second
			}
			if n, err := q.WriteAt(data, 0); err != nil || n != len(data) {
				t.Errorf("WriteAt = (%d, %v), want (%d, nil)", n, err, len(data))

				return
			}
		}
	})
	close(start)
	workers.Wait()
}

func TestQCOW2ExclusiveOperationsDrainRead(t *testing.T) {
	t.Parallel()
	for _, operation := range []string{"write", "close"} {
		t.Run(operation, func(t *testing.T) {
			t.Parallel()
			q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
			want := []byte("before")
			mustWriteAt(t, q, want, 0)
			entered := make(chan struct{}, 1)
			release := make(chan struct{})
			q.dataReader = qcow2DataReaderFunc(func(data []byte, off int64) (int, error) {
				entered <- struct{}{}
				<-release

				return q.file.ReadAt(data, off)
			})
			var workers sync.WaitGroup
			defer workers.Wait()
			unblock := sync.OnceFunc(func() { close(release) })
			defer unblock()
			workers.Go(func() { checkConcurrentQCOW2Read(t, q, want, 0) })
			waitQCOW2Signal(t, entered)
			started := make(chan struct{})
			finished := make(chan error, 1)
			workers.Go(func() {
				close(started)
				switch operation {
				case "write":
					_, err := q.WriteAt([]byte("after!"), 0)
					finished <- err
				case "close":
					finished <- q.Close()
				}
			})
			waitQCOW2Signal(t, started)
			assertQCOW2OperationBlocked(t, finished)
			unblock()
			waitQCOW2Operation(t, finished)
		})
	}
}

// A guest flush must not stall reads, so Sync does not wait for one either.
func TestQCOW2SyncDoesNotWaitForRead(t *testing.T) {
	t.Parallel()
	q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
	want := []byte("before")
	mustWriteAt(t, q, want, 0)
	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	q.dataReader = qcow2DataReaderFunc(func(data []byte, off int64) (int, error) {
		entered <- struct{}{}
		<-release

		return q.file.ReadAt(data, off)
	})
	var workers sync.WaitGroup
	defer workers.Wait()
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	workers.Go(func() { checkConcurrentQCOW2Read(t, q, want, 0) })
	waitQCOW2Signal(t, entered)
	synced := make(chan error, 1)
	workers.Go(func() { synced <- q.Sync() })
	waitQCOW2Operation(t, synced)
}

func TestQCOW2CloseDrainsWrite(t *testing.T) {
	t.Parallel()
	q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
	entered := make(chan struct{})
	release := make(chan struct{})
	block := sync.OnceFunc(func() {
		close(entered)
		<-release
	})
	q.metadataWriter = qcow2MetadataWriterFunc(func(data []byte, off int64) (int, error) {
		block()

		return q.file.WriteAt(data, off)
	})
	var workers sync.WaitGroup
	defer workers.Wait()
	unblock := sync.OnceFunc(func() { close(release) })
	defer unblock()
	written := make(chan error, 1)
	workers.Go(func() {
		_, err := q.WriteAt([]byte("new allocation"), 0)
		written <- err
	})
	waitQCOW2Signal(t, entered)
	started := make(chan struct{})
	closed := make(chan error, 1)
	workers.Go(func() {
		close(started)
		closed <- q.Close()
	})
	waitQCOW2Signal(t, started)
	assertQCOW2OperationBlocked(t, closed)
	unblock()
	waitQCOW2Operation(t, written)
	waitQCOW2Operation(t, closed)
}

func TestQCOW2ClosedOperations(t *testing.T) {
	t.Parallel()
	q := openWriteTestQCOW2(t, createEmptyCacheTestQCOW2(t))
	mustWriteAt(t, q, []byte("cached"), 0)
	if err := q.Close(); err != nil {
		t.Fatal(err)
	}
	for _, read := range []struct {
		name   string
		offset int64
		length int
	}{
		{name: "cached", length: 6},
		{name: "sparse", offset: 128 * 1024, length: 6},
		{name: "empty"},
	} {
		t.Run(read.name, func(t *testing.T) {
			t.Parallel()
			if n, err := q.ReadAt(make([]byte, read.length), read.offset); n != 0 || !errors.Is(err, os.ErrClosed) {
				t.Errorf("closed ReadAt = (%d, %v), want (0, closed)", n, err)
			}
		})
	}
	if n, err := q.WriteAt(nil, 0); n != 0 || !errors.Is(err, os.ErrClosed) {
		t.Errorf("closed WriteAt = (%d, %v), want (0, closed)", n, err)
	}
	if err := q.Sync(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("closed Sync = %v, want closed", err)
	}
	if err := q.Close(); !errors.Is(err, os.ErrClosed) {
		t.Errorf("second Close = %v, want closed", err)
	}
}

func checkConcurrentQCOW2Read(t *testing.T, q *qcow2Image, want []byte, off int64) {
	t.Helper()
	got := make([]byte, len(want))
	if n, err := q.ReadAt(got, off); n != len(got) || err != nil {
		t.Errorf("ReadAt = (%d, %v), want (%d, nil)", n, err, len(got))

		return
	}
	if !bytes.Equal(got, want) {
		t.Errorf("ReadAt(%d) returned unexpected data", off)
	}
}

func waitQCOW2Signal(t *testing.T, signal <-chan struct{}) {
	t.Helper()
	select {
	case <-signal:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for QCOW2 operation")
	}
}

func assertQCOW2OperationBlocked(t *testing.T, finished <-chan error) {
	t.Helper()
	// This is a negative assertion while a controlled read/write remains blocked,
	// not a latency requirement: only completion before release is a failure.
	select {
	case err := <-finished:
		t.Fatalf("operation completed before active I/O drained: %v", err)
	case <-time.After(20 * time.Millisecond):
	}
}

func waitQCOW2Operation(t *testing.T, finished <-chan error) {
	t.Helper()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for QCOW2 completion")
	}
}
