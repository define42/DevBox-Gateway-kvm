package virtio

import (
	"encoding/binary"
	"io"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"
)

type blkTestGate struct {
	wait    chan struct{}
	release func()
}

func newBlkTestGate() blkTestGate {
	wait := make(chan struct{})

	return blkTestGate{wait: wait, release: sync.OnceFunc(func() { close(wait) })}
}

// Each read has an independent gate so tests can hold the oldest request while
// later requests finish without relying on sleeps or scheduler timing.
type concurrentBlkImage struct {
	reads       []blkTestGate
	readErrors  []error
	shortReads  []bool
	shortWrites []bool
	started     []atomic.Bool
	entered     chan int
	writeGate   blkTestGate
	syncGate    blkTestGate
	active      atomic.Int32
	maximum     atomic.Int32
	writes      atomic.Int32
	syncs       atomic.Int32
	closes      atomic.Int32
	overlapped  atomic.Bool
}

func newConcurrentBlkImage(count int) *concurrentBlkImage {
	d := &concurrentBlkImage{
		reads:       make([]blkTestGate, count),
		readErrors:  make([]error, count),
		shortReads:  make([]bool, count),
		shortWrites: make([]bool, count),
		started:     make([]atomic.Bool, count),
		entered:     make(chan int, count),
		writeGate:   newBlkTestGate(),
		syncGate:    newBlkTestGate(),
	}
	for i := range d.reads {
		d.reads[i] = newBlkTestGate()
	}

	return d
}

func (d *concurrentBlkImage) ReadAt(p []byte, off int64) (int, error) {
	index := int(off / SectorSize)
	active := d.active.Add(1)
	defer d.active.Add(-1)
	for maximum := d.maximum.Load(); active > maximum; maximum = d.maximum.Load() {
		if d.maximum.CompareAndSwap(maximum, active) {
			break
		}
	}
	d.started[index].Store(true)
	d.entered <- index
	<-d.reads[index].wait
	if err := d.readErrors[index]; err != nil {
		return 0, err
	}
	for i := range p {
		p[i] = byte(index + 1)
	}
	if d.shortReads[index] {
		return len(p) - 1, nil
	}

	return len(p), nil
}

func (d *concurrentBlkImage) WriteAt(p []byte, off int64) (int, error) {
	if d.active.Load() != 0 {
		d.overlapped.Store(true)
	}
	d.writes.Add(1)
	<-d.writeGate.wait
	if d.shortWrites[int(off/SectorSize)] {
		return len(p) - 1, nil
	}

	return len(p), nil
}

// Sync may overlap reads and writes, unlike WriteAt and Close.
func (d *concurrentBlkImage) Sync() error {
	d.syncs.Add(1)
	<-d.syncGate.wait

	return nil
}

func (d *concurrentBlkImage) Size() int64 { return int64(len(d.reads) * SectorSize) }

func (d *concurrentBlkImage) Close() error {
	if d.active.Load() != 0 {
		d.overlapped.Store(true)
	}
	d.closes.Add(1)

	return nil
}

func (d *concurrentBlkImage) releaseAll() {
	for _, gate := range d.reads {
		gate.release()
	}
	d.writeGate.release()
	d.syncGate.release()
}

func newConcurrentBlkDevice(t *testing.T, kinds []uint32, writeback bool) (*Blk, *concurrentBlkImage, []int) {
	t.Helper()
	d := newConcurrentBlkImage(len(kinds))
	mem := make([]byte, len(kinds)*1024)
	v := newBlk(d, false, 10, benchIRQInjector{}, mem)
	q := NewSplitQueue(QueueSize)
	v.VirtQueue[blkQueue] = q
	if writeback {
		negotiateBlkFlush(v)
	}
	statuses := make([]int, len(kinds))
	for i, kind := range kinds {
		statuses[i] = addFlushTestRequest(q, mem, i, kind)
	}
	t.Cleanup(func() {
		d.releaseAll()
		if err := v.Close(); err != nil {
			t.Error(err)
		}
	})

	return v, d, statuses
}

func startConcurrentBlkIO(t *testing.T, v *Blk, d *concurrentBlkImage) <-chan error {
	t.Helper()
	done := make(chan error, 1)
	go func() {
		done <- v.IO()
		close(done)
	}()
	t.Cleanup(func() {
		d.releaseAll()
		if err := <-done; err != nil {
			t.Error(err)
		}
	})

	return done
}

func TestBlkReadsCompleteIndependently(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, statuses := newConcurrentBlkDevice(t, []uint32{0, 0, 0}, true)
		q := v.VirtQueue[blkQueue]
		d.reads[1].release()
		d.reads[2].release()
		done := startConcurrentBlkIO(t, v, d)
		synctest.Wait()
		if !d.started[0].Load() || q.Used.Idx != 2 {
			t.Fatalf("blocked read started=%v; completed=%d, want two later reads", d.started[0].Load(), q.Used.Idx)
		}
		ids := []uint32{q.Used.Ring[0].ID, q.Used.Ring[1].ID}
		slices.Sort(ids)
		if !slices.Equal(ids, []uint32{3, 6}) {
			t.Fatalf("completed IDs=%v, want [3 6]", ids)
		}
		if v.Mem[statuses[0]] != 0xff || v.Mem[statuses[1]] != 0 || v.Mem[statuses[2]] != 0 {
			t.Fatal("status bytes did not follow independently completed reads")
		}
		if !v.interruptPending() {
			t.Fatal("later read completions did not notify the guest")
		}
		d.reads[0].release()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if q.Used.Idx != 3 || q.Used.Ring[2].ID != 0 || v.LastAvailIdx[blkQueue] != 3 {
			t.Fatal("oldest read did not complete exactly once after its release")
		}
	})
}

func TestBlkReadConcurrencyBounded(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, _ := newConcurrentBlkDevice(t, make([]uint32, 9), true)
		done := startConcurrentBlkIO(t, v, d)
		synctest.Wait()
		if d.active.Load() != 4 || d.maximum.Load() != 4 || d.started[4].Load() {
			t.Fatalf("active=%d maximum=%d fifth started=%v, want four bounded reads",
				d.active.Load(), d.maximum.Load(), d.started[4].Load())
		}
		d.reads[0].release()
		synctest.Wait()
		if !d.started[4].Load() || d.started[5].Load() || d.active.Load() != 4 {
			t.Fatal("freeing one worker did not admit exactly one next read")
		}
		d.releaseAll()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if d.maximum.Load() != 4 || v.VirtQueue[blkQueue].Used.Idx != 9 {
			t.Fatal("read batch exceeded its bound or lost completions")
		}
	})
}

// FLUSH is not a barrier for later requests; TestBlkRequestsProceedDuringFlush
// covers it.
func TestBlkReadWriteBarrier(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, statuses := newConcurrentBlkDevice(t, []uint32{0, 0, 1, 0}, true)
		done := startConcurrentBlkIO(t, v, d)
		d.reads[1].release()
		synctest.Wait()
		if d.writes.Load() != 0 || d.started[3].Load() {
			t.Fatal("write or trailing read overtook the blocked leading read")
		}
		d.reads[0].release()
		synctest.Wait()
		if d.writes.Load() != 1 || d.overlapped.Load() {
			t.Fatal("write did not wait for every preceding read")
		}
		if d.started[3].Load() || v.Mem[statuses[2]] != 0xff || v.VirtQueue[blkQueue].Used.Idx != 2 {
			t.Fatal("trailing read or write completion overtook the blocked write")
		}
		d.writeGate.release()
		synctest.Wait()
		if !d.started[3].Load() || v.Mem[statuses[2]] != 0 {
			t.Fatal("trailing read did not start after write completion")
		}
		d.reads[3].release()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestBlkConcurrentReadsPreserveWritethrough(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, statuses := newConcurrentBlkDevice(t, []uint32{0, 0, 1, 0}, false)
		d.writeGate.release()
		done := startConcurrentBlkIO(t, v, d)
		synctest.Wait()
		if d.active.Load() != 2 {
			t.Fatalf("leading active reads=%d, want 2", d.active.Load())
		}
		for _, gate := range d.reads {
			gate.release()
		}
		synctest.Wait()
		if d.syncs.Load() != 1 || d.overlapped.Load() || v.VirtQueue[blkQueue].Used.Idx != 0 {
			t.Fatal("writethrough published completions before all I/O and Sync finished")
		}
		for _, status := range statuses {
			if v.Mem[status] != 0xff {
				t.Fatal("writethrough status was published before Sync finished")
			}
		}
		d.syncGate.release()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if v.VirtQueue[blkQueue].Used.Idx != 4 {
			t.Fatal("writethrough batch did not publish all durable completions")
		}
	})
}

func TestBlkConcurrentReadErrorsArePerRequest(t *testing.T) {
	t.Parallel()
	for _, failure := range []string{"read_error", "short_read_without_error"} {
		t.Run(failure, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				v, d, statuses := newConcurrentBlkDevice(t, []uint32{0, 0, 0}, true)
				if failure == "read_error" {
					d.readErrors[1] = io.ErrUnexpectedEOF
				} else {
					d.shortReads[1] = true
				}
				d.reads[1].release()
				d.reads[2].release()
				done := startConcurrentBlkIO(t, v, d)
				synctest.Wait()
				if v.Mem[statuses[1]] != 1 || v.Mem[statuses[2]] != 0 || v.Mem[statuses[0]] != 0xff {
					t.Fatal("one failed read affected another request's completion")
				}
				d.reads[0].release()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if v.Mem[statuses[0]] != 0 || v.VirtQueue[blkQueue].Used.Idx != 3 {
					t.Fatal("read failure prevented the remaining read from completing")
				}
			})
		})
	}
}

func TestBlkRejectedRequestRemainsAnOrderedBarrier(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		kind   uint32
		status byte
	}{
		{name: "unsupported", kind: 8, status: 2},
		{name: "out_of_capacity", kind: 0, status: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				v, d, statuses := newConcurrentBlkDevice(t, []uint32{0, tc.kind, 0}, true)
				if tc.kind == 0 {
					binary.LittleEndian.PutUint64(v.Mem[1024+8:], uint64(len(d.reads)+1))
				}
				d.reads[2].release()
				done := startConcurrentBlkIO(t, v, d)
				synctest.Wait()
				if d.started[2].Load() || v.Mem[statuses[1]] != 0xff {
					t.Fatal("rejected request or trailing read overtook its leading read")
				}
				d.reads[0].release()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				q := v.VirtQueue[blkQueue]
				if d.started[1].Load() || v.Mem[statuses[1]] != tc.status || q.Used.Idx != 3 {
					t.Fatal("rejected request performed backend I/O or did not complete with its own status")
				}
				for i := range statuses {
					if q.Used.Ring[i].ID != uint32(i*3) {
						t.Fatal("rejected request did not preserve the completion barrier")
					}
				}
			})
		})
	}
}

func TestBlkWritesRemainSerial(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, _ := newConcurrentBlkDevice(t, []uint32{0, 0, 1, 1, 0}, true)
		d.reads[0].release()
		d.reads[1].release()
		done := startConcurrentBlkIO(t, v, d)
		synctest.Wait()
		if d.writes.Load() != 1 || d.started[4].Load() {
			t.Fatal("second write or following read overtook the blocked first write")
		}
		d.writeGate.release()
		synctest.Wait()
		if d.writes.Load() != 2 || !d.started[4].Load() || d.overlapped.Load() {
			t.Fatal("write barrier did not drain in order before the trailing read")
		}
		d.releaseAll()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
	})
}

func TestBlkShortWriteReturnsIOError(t *testing.T) {
	t.Parallel()
	for _, mode := range []string{"writeback", "writethrough"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			v, d, statuses := newConcurrentBlkDevice(t, []uint32{1, 1}, mode == "writeback")
			d.shortWrites[0] = true
			d.releaseAll()
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			if v.Mem[statuses[0]] != 1 || v.Mem[statuses[1]] != 0 || v.VirtQueue[blkQueue].Used.Idx != 2 {
				t.Fatal("short write did not return IOERR independently of the next write")
			}
		})
	}
}

func TestBlkNotificationsCoalesceWithoutLosingNextKick(t *testing.T) {
	t.Parallel()
	v, _, _ := newConcurrentBlkDevice(t, []uint32{0}, true)
	for range 1000 {
		v.Notify(blkQueue)
	}
	if len(v.kick) != 1 {
		t.Fatalf("pending kicks=%d, want one coalesced wakeup", len(v.kick))
	}
	<-v.kick
	v.Notify(blkQueue)
	if len(v.kick) != 1 {
		t.Fatal("kick arriving after the previous wake was consumed was lost")
	}
}

func TestBlkAliasedReadBuffersStaySerial(t *testing.T) {
	t.Parallel()
	for _, alias := range []string{"data_data", "data_status", "status_data", "status_status"} {
		t.Run(alias, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				v, d, _ := newConcurrentBlkDevice(t, []uint32{0, 0}, true)
				q := v.VirtQueue[blkQueue]
				switch alias {
				case "data_data":
					q.Desc[4].Addr = q.Desc[1].Addr
				case "data_status":
					q.Desc[4].Addr = q.Desc[2].Addr
				case "status_data":
					q.Desc[5].Addr = q.Desc[1].Addr
				case "status_status":
					q.Desc[5].Addr = q.Desc[2].Addr
				}
				d.reads[1].release()
				done := startConcurrentBlkIO(t, v, d)
				synctest.Wait()
				if !d.started[0].Load() || d.started[1].Load() || q.Used.Idx != 0 {
					t.Fatal("aliased guest buffers were accessed concurrently or out of order")
				}
				d.reads[0].release()
				if err := <-done; err != nil {
					t.Fatal(err)
				}
				if d.maximum.Load() != 1 || q.Used.Idx != 2 || q.Used.Ring[0].ID != 0 || q.Used.Ring[1].ID != 3 {
					t.Fatal("aliased reads did not retain serial completion order")
				}
			})
		})
	}
}

func TestBlkConcurrentReadRingWrap(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, _ := newConcurrentBlkDevice(t, []uint32{0, 0, 0, 0}, true)
		q := NewSplitQueue(16)
		copy(q.Desc, v.VirtQueue[blkQueue].Desc)
		v.VirtQueue[blkQueue] = q
		const start = uint16(65534)
		v.LastAvailIdx[blkQueue], q.Used.Idx = start, start
		for i := uint16(0); i < 4; i++ {
			q.Avail.Ring[(start+i)%q.Size] = i * 3
		}
		q.Avail.Idx = 2
		for i := 1; i < 4; i++ {
			d.reads[i].release()
		}
		done := startConcurrentBlkIO(t, v, d)
		synctest.Wait()
		if q.Used.Idx != 1 {
			t.Fatalf("used index before oldest completion=%d, want wrapped index 1", q.Used.Idx)
		}
		d.reads[0].release()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if q.Used.Idx != 2 || v.LastAvailIdx[blkQueue] != 2 {
			t.Fatal("used or consumed index did not wrap correctly")
		}
		ids := make([]uint32, 4)
		for i := uint16(0); i < 4; i++ {
			ids[i] = q.Used.Ring[(start+i)%q.Size].ID
		}
		slices.Sort(ids)
		if !slices.Equal(ids, []uint32{0, 3, 6, 9}) {
			t.Fatalf("wrapped completion IDs=%v, want each request exactly once", ids)
		}
	})
}

func TestBlkReadWorkersDrainBeforeLifecycleChange(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"close", "reset"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			v, d, _ := newConcurrentBlkDevice(t, []uint32{0, 0}, true)
			done := startConcurrentBlkIO(t, v, d)
			for range 2 {
				select {
				case <-d.entered:
				case <-time.After(5 * time.Second):
					t.Fatal("concurrent reads did not reach the image")
				}
			}
			if v.queueMu.TryLock() {
				v.queueMu.Unlock()
				t.Fatal("active reads did not retain the queue's lifecycle lock")
			}
			changed := make(chan error, 1)
			go func() {
				if action == "close" {
					changed <- v.Close()

					return
				}
				v.Reset()
				changed <- nil
			}()
			d.releaseAll()
			if err := <-done; err != nil {
				t.Fatal(err)
			}
			select {
			case err := <-changed:
				if err != nil {
					t.Fatal(err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("lifecycle change did not finish after reads drained")
			}
			if d.active.Load() != 0 || d.overlapped.Load() {
				t.Fatal("lifecycle change closed the image while a read still owned its buffers")
			}
			if action == "reset" && (v.VirtQueue[blkQueue] != nil || v.LastAvailIdx[blkQueue] != 0) {
				t.Fatal("reset did not discard the queue after reads drained")
			}
		})
	}
}
