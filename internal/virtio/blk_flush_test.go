package virtio

import (
	"encoding/binary"
	"errors"
	"io"
	"slices"
	"sync"
	"testing"
	"testing/synctest"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

var errTestBlkIRQ = errors.New("inject block IRQ")

type flushImage struct {
	batchImage
	events   []string
	writeErr error
	closeErr error
}

func (d *flushImage) WriteAt(p []byte, off int64) (int, error) {
	n, err := d.batchImage.WriteAt(p, off)
	d.events = append(d.events, "write")
	if d.writeErr != nil {
		return n, d.writeErr
	}

	return n, err
}

func (d *flushImage) Sync() error {
	d.events = append(d.events, "sync")

	return d.batchImage.Sync()
}

func (d *flushImage) Close() error {
	d.events = append(d.events, "close")

	return d.closeErr
}

func newFlushTestDevice(t *testing.T, backing disk.Image, readOnly bool) (*Blk, *SplitQueue, []byte) {
	t.Helper()
	mem := make([]byte, 4*1024)
	v := newBlk(backing, readOnly, 10, benchIRQInjector{}, mem)
	t.Cleanup(func() { _ = v.Close() })
	q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
	v.QueueReady(blkQueue, q)

	return v, q, mem
}

func negotiateBlkFlush(v *Blk) {
	writeCfg(v.ModernTransport, 8, 0, 4)
	writeCfg(v.ModernTransport, 12, blkFeatureFlush, 4)
	writeCfg(v.ModernTransport, 8, 1, 4)
	writeCfg(v.ModernTransport, 12, 1, 4) // VIRTIO_F_VERSION_1
	writeCfg(v.ModernTransport, 20, 0x0f, 1)
}

// addFlushTestRequest reserves three descriptor slots for each request, while
// FLUSH itself uses only a header and a writable status descriptor.
func addFlushTestRequest(q *SplitQueue, mem []byte, index int, typ uint32) int {
	addr, head := uint64(index*1024), uint16(index*3)
	status := int(addr) + 16 + SectorSize
	binary.LittleEndian.PutUint32(mem[addr:], typ)
	if typ != 4 {
		binary.LittleEndian.PutUint64(mem[addr+8:], uint64(index))
	}
	q.Desc[head] = SplitDesc{Addr: addr, Len: 16, Flags: descFNext, Next: head + 1}
	statusID := head + 1
	if typ != 4 {
		flags := uint16(descFNext)
		if typ == 0 {
			flags |= descFWrite
		}
		q.Desc[head+1] = SplitDesc{Addr: addr + 16, Len: SectorSize, Flags: flags, Next: head + 2}
		mem[addr+16] = byte(index + 1)
		statusID++
	}
	q.Desc[statusID] = SplitDesc{Addr: uint64(status), Len: 1, Flags: descFWrite}
	mem[status] = 0xff
	q.Avail.Ring[index] = head
	q.Avail.Idx = uint16(index + 1)

	return status
}

func TestBlkRequestsProceedDuringFlush(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, statuses := newConcurrentBlkDevice(t, []uint32{0, 4, 0, 1}, true)
		q := v.VirtQueue[blkQueue]
		done := startConcurrentBlkIO(t, v, d)
		synctest.Wait()
		if d.syncs.Load() != 0 || q.Used.Idx != 0 {
			t.Fatal("FLUSH started before the preceding read finished")
		}
		d.reads[0].release()
		synctest.Wait()
		if d.syncs.Load() != 1 || !d.started[2].Load() {
			t.Fatal("trailing read waited for the FLUSH's Sync")
		}
		d.reads[2].release()
		d.writeGate.release()
		synctest.Wait()
		if d.writes.Load() != 1 || q.Used.Idx != 3 {
			t.Fatalf("requests after FLUSH: writes=%d used=%d, want both complete during Sync",
				d.writes.Load(), q.Used.Idx)
		}
		if v.Mem[statuses[1]] != 0xff {
			t.Fatal("FLUSH completed before its Sync returned")
		}
		select {
		case err := <-done:
			t.Fatalf("IO returned before its FLUSH completed: %v", err)
		default:
		}
		d.syncGate.release()
		if err := <-done; err != nil {
			t.Fatal(err)
		}
		if q.Used.Idx != 4 || q.Used.Ring[3] != (SplitUsedElem{ID: 3, Len: 1}) {
			t.Fatalf("FLUSH completion: used=%d last=%+v", q.Used.Idx, q.Used.Ring[3])
		}
		for i, status := range statuses {
			if v.Mem[status] != 0 {
				t.Fatalf("request %d: status=%d", i, v.Mem[status])
			}
		}
	})
}

func TestBlkWritebackInterruptFailureDrainsBatch(t *testing.T) {
	t.Parallel()

	backing := &batchImage{data: make([]byte, 2*SectorSize)}
	v, q, mem := newFlushTestDevice(t, backing, false)
	negotiateBlkFlush(v)
	irqErr := errTestBlkIRQ
	v.SetIRQLine(func(bool) error { return irqErr })
	statuses := []int{
		addFlushTestRequest(q, mem, 0, 0),
		addFlushTestRequest(q, mem, 1, 0),
	}

	if err := v.IO(); !errors.Is(err, irqErr) {
		t.Fatalf("IO error=%v, want %v", err, irqErr)
	}
	if q.Used.Idx != 2 || v.LastAvailIdx[blkQueue] != 2 || backing.reads != 2 {
		t.Fatalf("drain after IRQ failure: used=%d last avail=%d reads=%d",
			q.Used.Idx, v.LastAvailIdx[blkQueue], backing.reads)
	}
	for i, status := range statuses {
		if mem[status] != 0 {
			t.Fatalf("request %d: status=%d, want success despite IRQ failure", i, mem[status])
		}
	}
	ids := []uint32{q.Used.Ring[0].ID, q.Used.Ring[1].ID}
	slices.Sort(ids)
	if !slices.Equal(ids, []uint32{0, 3}) {
		t.Fatalf("completed IDs=%v, want each read exactly once", ids)
	}
}

func TestBlkFlushNegotiation(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name       string
		accept     bool
		featuresOK bool
		writeback  bool
	}{
		{name: "offered_only", featuresOK: true},
		{name: "features_not_finalized", accept: true},
		{name: "negotiated", accept: true, featuresOK: true, writeback: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backing := &batchImage{data: make([]byte, 4*SectorSize)}
			v, q, mem := newFlushTestDevice(t, backing, false)
			if offered := readCfg(v.ModernTransport, 4, 4); offered&blkFeatureFlush == 0 {
				t.Fatal("device does not advertise FLUSH")
			}
			if tc.accept {
				writeCfg(v.ModernTransport, 12, blkFeatureFlush, 4)
			}
			status := uint64(0x03)
			if tc.featuresOK {
				status = 0x0f
			}
			writeCfg(v.ModernTransport, 20, status, 1)
			writeStatus := addFlushTestRequest(q, mem, 0, 1)
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			wantSyncs := 1
			if tc.writeback {
				wantSyncs = 0
			}
			if backing.syncs != wantSyncs || mem[writeStatus] != 0 || q.Used.Idx != 1 {
				t.Fatalf("write: syncs=%d, status=%d, used=%d", backing.syncs, mem[writeStatus], q.Used.Idx)
			}
			flushStatus := addFlushTestRequest(q, mem, 1, 4)
			backing.onSync = func() {
				if q.Used.Idx != 1 || mem[flushStatus] != 0xff {
					t.Error("FLUSH completion published before Sync finished")
				}
			}
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			wantStatus := byte(2)
			if tc.writeback {
				wantSyncs++
				wantStatus = 0
				if q.Used.Ring[1].Len != 1 {
					t.Fatalf("FLUSH used length=%d, want 1", q.Used.Ring[1].Len)
				}
			}
			if backing.syncs != wantSyncs || mem[flushStatus] != wantStatus || q.Used.Idx != 2 {
				t.Fatalf("flush: syncs=%d, status=%d, used=%d", backing.syncs, mem[flushStatus], q.Used.Idx)
			}
			if backing.reads != 0 || backing.writes != 1 {
				t.Fatalf("FLUSH performed data I/O: reads=%d, writes=%d", backing.reads, backing.writes)
			}
		})
	}
}

func TestBlkFlushReadOnly(t *testing.T) {
	t.Parallel()
	backing := &batchImage{data: make([]byte, 4*SectorSize)}
	v, q, mem := newFlushTestDevice(t, backing, true)
	if features := v.DeviceFeatures(); features&(blkFeatureRO|blkFeatureFlush) != blkFeatureRO|blkFeatureFlush {
		t.Fatalf("read-only device features=%#x", features)
	}
	negotiateBlkFlush(v)
	status := addFlushTestRequest(q, mem, 0, 4)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if backing.syncs != 1 || backing.reads != 0 || backing.writes != 0 || mem[status] != 0 {
		t.Fatalf("read-only FLUSH: syncs=%d, reads=%d, writes=%d, status=%d",
			backing.syncs, backing.reads, backing.writes, mem[status])
	}
}

func TestBlkFlushFailureRetry(t *testing.T) {
	t.Parallel()
	backing := &batchImage{data: make([]byte, 4*SectorSize)}
	v, q, mem := newFlushTestDevice(t, backing, false)
	negotiateBlkFlush(v)
	addFlushTestRequest(q, mem, 0, 1)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	backing.syncErr = io.ErrClosedPipe
	status := addFlushTestRequest(q, mem, 1, 4)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if mem[status] != 1 || backing.syncs != 1 {
		t.Fatalf("failed FLUSH: status=%d, syncs=%d", mem[status], backing.syncs)
	}
	backing.syncErr = nil
	status = addFlushTestRequest(q, mem, 2, 4)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if mem[status] != 0 || backing.syncs != 2 {
		t.Fatalf("retried FLUSH: status=%d, syncs=%d", mem[status], backing.syncs)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if backing.syncs != 2 {
		t.Fatal("Close redundantly synced already-flushed writes")
	}
}

func TestBlkFlushOrderAndClose(t *testing.T) {
	t.Parallel()
	backing := &flushImage{batchImage: batchImage{data: make([]byte, 4*SectorSize)}}
	// Hold the trailing write until the FLUSH's Sync has begun, so that Sync
	// cannot cover it and Close must persist it.
	syncing := make(chan struct{})
	backing.onSync = sync.OnceFunc(func() { close(syncing) })
	writes := 0
	backing.onWrite = func() {
		if writes++; writes == 2 {
			<-syncing
		}
	}
	v, q, mem := newFlushTestDevice(t, backing, false)
	negotiateBlkFlush(v)
	statuses := []int{
		addFlushTestRequest(q, mem, 0, 1),
		addFlushTestRequest(q, mem, 1, 4),
		addFlushTestRequest(q, mem, 2, 1),
	}
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(backing.events, []string{"write", "sync", "write"}) {
		t.Fatalf("I/O order=%v", backing.events)
	}
	for _, status := range statuses {
		if mem[status] != 0 {
			t.Fatalf("request status=%d", mem[status])
		}
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(backing.events, []string{"write", "sync", "write", "sync", "close"}) {
		t.Fatalf("Close did not persist the write after FLUSH: %v", backing.events)
	}
}

func TestBlkFlushReset(t *testing.T) {
	t.Parallel()
	backing := &batchImage{data: make([]byte, 4*SectorSize)}
	v, q, mem := newFlushTestDevice(t, backing, false)
	negotiateBlkFlush(v)
	addFlushTestRequest(q, mem, 0, 1)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	writeCfg(v.ModernTransport, 20, 0, 1)
	if got := v.negotiatedFeatures(); got != 0 {
		t.Fatalf("reset retained negotiated features=%#x", got)
	}
	if err := v.IO(); !errors.Is(err, ErrVQNotInit) {
		t.Fatalf("IO after reset=%v", err)
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if backing.syncs != 1 {
		t.Fatalf("reset lost pending durability work: syncs=%d", backing.syncs)
	}
}

func TestBlkFlushMalformedChain(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		mutate func(*SplitQueue)
	}{
		{name: "status_not_writable", mutate: func(q *SplitQueue) { q.Desc[1].Flags = 0 }},
		{name: "empty_status", mutate: func(q *SplitQueue) { q.Desc[1].Len = 0 }},
		{name: "status_out_of_range", mutate: func(q *SplitQueue) { q.Desc[1].Addr = ^uint64(0) }},
		{name: "header_terminates_chain", mutate: func(q *SplitQueue) { q.Desc[0].Flags = 0 }},
		{name: "descriptor_loop", mutate: func(q *SplitQueue) {
			q.Desc[1].Flags |= descFNext
			q.Desc[1].Next = 0
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backing := &batchImage{data: make([]byte, 4*SectorSize)}
			v, q, mem := newFlushTestDevice(t, backing, false)
			negotiateBlkFlush(v)
			status := addFlushTestRequest(q, mem, 0, 4)
			tc.mutate(q)
			if err := v.IO(); !errors.Is(err, ErrBlkDesc) {
				t.Fatalf("IO=%v, want ErrBlkDesc", err)
			}
			if q.Used.Idx != 0 || mem[status] != 0xff || backing.syncs != 0 || backing.reads != 0 || backing.writes != 0 {
				t.Fatal("malformed FLUSH performed I/O or published a completion")
			}
		})
	}
}

func TestBlkFlushCloseErrors(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name      string
		writeErr  error
		flushFail bool
	}{
		{name: "dirty_write"},
		{name: "failed_write", writeErr: io.ErrShortWrite},
		{name: "failed_flush", flushFail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			syncErr := io.ErrClosedPipe
			closeErr := io.ErrUnexpectedEOF
			backing := &flushImage{
				batchImage: batchImage{data: make([]byte, 4*SectorSize), syncErr: syncErr},
				writeErr:   tc.writeErr,
				closeErr:   closeErr,
			}
			v, q, mem := newFlushTestDevice(t, backing, false)
			negotiateBlkFlush(v)
			status := addFlushTestRequest(q, mem, 0, 1)
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			if tc.writeErr != nil && mem[status] != 1 {
				t.Fatalf("failed write status=%d", mem[status])
			}
			wantEvents := []string{"write"}
			if tc.flushFail {
				addFlushTestRequest(q, mem, 1, 4)
				if err := v.IO(); err != nil {
					t.Fatal(err)
				}
				wantEvents = append(wantEvents, "sync")
			}
			wantEvents = append(wantEvents, "sync", "close")
			for i := 0; i < 2; i++ {
				if err := v.Close(); !errors.Is(err, syncErr) || !errors.Is(err, closeErr) {
					t.Fatalf("Close=%v, want both Sync and Close failures", err)
				}
			}
			if !slices.Equal(backing.events, wantEvents) {
				t.Fatalf("Close order=%v, want %v", backing.events, wantEvents)
			}
		})
	}
}

func TestBlkFlushCloseWaitsForWrite(t *testing.T) {
	t.Parallel()
	entered, release := make(chan struct{}), make(chan struct{})
	backing := &flushImage{batchImage: batchImage{data: make([]byte, 4*SectorSize)}}
	backing.onWrite = func() {
		close(entered)
		<-release
	}
	v, q, mem := newFlushTestDevice(t, backing, false)
	negotiateBlkFlush(v)
	addFlushTestRequest(q, mem, 0, 1)
	ioDone := make(chan error, 1)
	go func() { ioDone <- v.IO() }()
	<-entered
	closeDone := make(chan error, 1)
	go func() { closeDone <- v.Close() }()
	<-v.done
	select {
	case err := <-closeDone:
		close(release)
		<-ioDone
		t.Fatalf("Close returned before active write finished: %v", err)
	case <-time.After(10 * time.Millisecond):
	}
	close(release)
	for _, done := range []<-chan error{ioDone, closeDone} {
		select {
		case err := <-done:
			if err != nil {
				t.Fatal(err)
			}
		case <-time.After(5 * time.Second):
			t.Fatal("write and Close did not finish")
		}
	}
	if !slices.Equal(backing.events, []string{"write", "sync", "close"}) {
		t.Fatalf("Close order=%v", backing.events)
	}
	if err := v.IO(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("IO after Close=%v", err)
	}
}

// Lifecycle changes wait for a running Sync. Reset first publishes the FLUSH to
// the queue it came from; Close drops it but keeps the image open until then.
func TestBlkFlushDrainsBeforeLifecycleChange(t *testing.T) {
	t.Parallel()
	for _, action := range []string{"close", "reset"} {
		t.Run(action, func(t *testing.T) {
			t.Parallel()
			synctest.Test(t, func(t *testing.T) {
				v, d, statuses := newConcurrentBlkDevice(t, []uint32{4, 0}, true)
				q := v.VirtQueue[blkQueue]
				q.Avail.Idx = 1
				done := startConcurrentBlkIO(t, v, d)
				synctest.Wait()
				// A read posted during the Sync makes IO return to take it.
				q.Avail.Idx = 2
				v.Notify(blkQueue)
				if err := <-done; err != nil {
					t.Fatal(err)
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
				synctest.Wait()
				select {
				case err := <-changed:
					t.Fatalf("%s finished during Sync: %v", action, err)
				default:
				}
				if d.closes.Load() != 0 {
					t.Fatal("Close closed the image during Sync")
				}
				d.syncGate.release()
				if err := <-changed; err != nil {
					t.Fatal(err)
				}
				if d.started[1].Load() {
					t.Fatal("lifecycle change serviced the read posted after the FLUSH")
				}
				if action == "reset" {
					if v.Mem[statuses[0]] != 0 || q.Used.Idx != 1 || v.VirtQueue[blkQueue] != nil {
						t.Fatal("Reset did not publish the FLUSH before discarding its queue")
					}
				} else if v.Mem[statuses[0]] != 0xff || q.Used.Idx != 0 || d.closes.Load() != 1 {
					t.Fatal("Close published the FLUSH or did not close the image")
				}
			})
		})
	}
}

// A guest reposting one FLUSH chain can keep more FLUSH requests outstanding
// than the ring holds. They must all complete without blocking the device.
func TestBlkFlushBacklogExceedsRing(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		v, d, statuses := newConcurrentBlkDevice(t, []uint32{4}, true)
		q := v.VirtQueue[blkQueue]
		q.Avail.Idx = QueueSize // Every ring entry names the FLUSH at head 0.
		first := startConcurrentBlkIO(t, v, d)
		synctest.Wait()
		if d.syncs.Load() != 1 {
			t.Fatalf("syncs=%d, want one running Sync", d.syncs.Load())
		}
		q.Avail.Idx += QueueSize
		v.Notify(blkQueue)
		if err := <-first; err != nil {
			t.Fatal(err)
		}
		second := make(chan error, 1)
		go func() { second <- v.IO() }()
		synctest.Wait()
		// The bound keeps the flush channels from filling, which could block
		// the queue owner and flush worker on each other.
		if v.flushing != QueueSize {
			t.Fatalf("outstanding FLUSH requests=%d, want at most %d", v.flushing, QueueSize)
		}
		d.syncGate.release()
		if err := <-second; err != nil {
			t.Fatal(err)
		}
		if q.Used.Idx != 2*QueueSize || v.Mem[statuses[0]] != 0 || d.syncs.Load() < 2 {
			t.Fatalf("used=%d status=%d syncs=%d", q.Used.Idx, v.Mem[statuses[0]], d.syncs.Load())
		}
	})
}
