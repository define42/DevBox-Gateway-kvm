package virtio

import (
	"encoding/binary"
	"errors"
	"io"
	"testing"
)

type batchImage struct {
	data    []byte
	syncs   int
	reads   int
	writes  int
	syncErr error
	onSync  func()
	onWrite func()
}

func (d *batchImage) ReadAt(p []byte, off int64) (int, error) {
	d.reads++
	if off < 0 || off+int64(len(p)) > int64(len(d.data)) {
		return 0, io.EOF
	}

	return copy(p, d.data[off:]), nil
}

func (d *batchImage) WriteAt(p []byte, off int64) (int, error) {
	d.writes++
	if d.onWrite != nil {
		d.onWrite()
	}
	if off < 0 || off+int64(len(p)) > int64(len(d.data)) {
		return 0, io.ErrShortWrite
	}

	return copy(d.data[off:], p), nil
}
func (d *batchImage) Size() int64  { return int64(len(d.data)) }
func (d *batchImage) Close() error { return nil }
func (d *batchImage) Sync() error {
	d.syncs++
	if d.onSync != nil {
		d.onSync()
	}

	return d.syncErr
}

func TestBlkBatchDurability(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		syncErr error
	}{
		{name: "durable"},
		{name: "sync_failure", syncErr: io.ErrClosedPipe},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mem := make([]byte, 3*1024)
			backing := &batchImage{data: make([]byte, 3*SectorSize), syncErr: tc.syncErr}
			v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
			t.Cleanup(func() { _ = v.Close() })
			q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
			for i := 0; i < 3; i++ {
				addr, head := uint64(i*1024), uint16(i*3)
				if i != 1 {
					binary.LittleEndian.PutUint32(mem[addr:], 1)
				}
				binary.LittleEndian.PutUint64(mem[addr+8:], uint64(i))
				q.Desc[head] = SplitDesc{Addr: addr, Len: 16, Next: head + 1}
				q.Desc[head+1] = SplitDesc{Addr: addr + 16, Len: SectorSize, Next: head + 2}
				q.Desc[head+2] = SplitDesc{Addr: addr + 16 + SectorSize, Len: 1}
				mem[addr+16], mem[addr+16+SectorSize] = byte(i+1), 0xff
				q.Avail.Ring[i] = head
			}
			q.Avail.Idx = 3
			v.VirtQueue[blkQueue] = q
			backing.onSync = func() {
				if q.Used.Idx != 0 {
					t.Fatal("published a completion before Sync finished")
				}
				for i := 0; i < 3; i++ {
					if mem[i*1024+16+SectorSize] != 0xff {
						t.Fatal("published status before Sync finished")
					}
				}
			}
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			if q.Used.Idx != 3 {
				t.Fatalf("completed %d requests, want 3", q.Used.Idx)
			}
			if backing.syncs != 1 {
				t.Fatalf("syncs = %d, want one durable batch", backing.syncs)
			}
			for i := 0; i < 3; i++ {
				want := byte(0)
				if tc.syncErr != nil && i != 1 {
					want = 1
				}
				if got := mem[i*1024+16+SectorSize]; got != want {
					t.Fatalf("request %d status = %d, want %d", i, got, want)
				}
				if got := q.Used.Ring[i].ID; got != uint32(i*3) {
					t.Fatalf("request %d used ID = %d", i, got)
				}
			}
			if backing.data[0] != 1 || backing.data[2*SectorSize] != 3 {
				t.Fatal("writes missing from backing image")
			}
		})
	}
}

func TestBlkBatchSnapshot(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 2048)
	backing := &batchImage{data: make([]byte, 2*SectorSize)}
	v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
	t.Cleanup(func() { _ = v.Close() })
	q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
	for i := 0; i < 2; i++ {
		addr, head := uint64(i*1024), uint16(i*3)
		binary.LittleEndian.PutUint32(mem[addr:], 1)
		q.Desc[head] = SplitDesc{Addr: addr, Len: 16, Next: head + 1}
		q.Desc[head+1] = SplitDesc{Addr: addr + 16, Len: SectorSize, Next: head + 2}
		q.Desc[head+2] = SplitDesc{Addr: addr + 16 + SectorSize, Len: 1}
		q.Avail.Ring[i] = head
	}
	q.Avail.Idx = 1
	v.VirtQueue[blkQueue] = q
	backing.onSync = func() { q.Avail.Idx = 2 }
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if q.Used.Idx != 1 {
		t.Fatal("request arriving during Sync was completed without its own durability boundary")
	}
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if q.Used.Idx != 2 || backing.syncs != 2 {
		t.Fatal("request arriving during Sync was not processed on the next batch")
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if err := v.IO(); !errors.Is(err, io.ErrClosedPipe) {
		t.Fatalf("IO after Close: %v", err)
	}
}

func TestBlkResetDiscardsQueue(t *testing.T) {
	t.Parallel()

	v := newBlk(&batchImage{}, false, 10, benchIRQInjector{}, nil)
	t.Cleanup(func() { _ = v.Close() })
	v.QueueReady(blkQueue, &SplitQueue{})
	v.LastAvailIdx[blkQueue] = 7
	v.Reset()
	if err := v.IO(); !errors.Is(err, ErrVQNotInit) {
		t.Fatalf("IO after reset: %v", err)
	}
	if v.LastAvailIdx[blkQueue] != 0 {
		t.Fatal("reset retained consumed queue index")
	}
}

func TestBlkBatchSnapshotsDescriptorsBeforeIO(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 2048)
	backing := &batchImage{data: make([]byte, 2*SectorSize)}
	v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
	t.Cleanup(func() { _ = v.Close() })
	q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
	for i := 0; i < 2; i++ {
		addr, head := uint64(i*1024), uint16(i*3)
		binary.LittleEndian.PutUint32(mem[addr:], 1)
		binary.LittleEndian.PutUint64(mem[addr+8:], uint64(i))
		q.Desc[head] = SplitDesc{Addr: addr, Len: 16, Next: head + 1}
		q.Desc[head+1] = SplitDesc{Addr: addr + 16, Len: SectorSize, Next: head + 2}
		q.Desc[head+2] = SplitDesc{Addr: addr + 16 + SectorSize, Len: 1}
		mem[addr+16], mem[addr+16+SectorSize] = byte(i+1), 0xff
		q.Avail.Ring[i] = head
	}
	q.Avail.Idx = 2
	v.VirtQueue[blkQueue] = q
	backing.onWrite = func() {
		backing.onWrite = nil
		// Emulate a guest changing the second request while the first request
		// is in the backend. Validated addresses and command fields must win.
		q.Desc[3] = SplitDesc{Addr: ^uint64(0), Len: 16, Next: QueueSize}
		q.Desc[4] = SplitDesc{Addr: ^uint64(0), Len: SectorSize, Next: QueueSize}
		q.Desc[5] = SplitDesc{Addr: ^uint64(0), Len: 1}
		binary.LittleEndian.PutUint32(mem[1024:], 0)
		binary.LittleEndian.PutUint64(mem[1032:], ^uint64(0))
	}
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if backing.data[0] != 1 || backing.data[SectorSize] != 2 {
		t.Fatal("backend did not use the validated request snapshot")
	}
	if q.Used.Idx != 2 || mem[16+SectorSize] != 0 || mem[1024+16+SectorSize] != 0 {
		t.Fatal("snapshot requests did not complete successfully")
	}
	if backing.syncs != 1 {
		t.Fatalf("Sync count = %d, want 1", backing.syncs)
	}
}

func TestBlkUnsupportedRequests(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		typ  uint32
		data bool
	}{
		{name: "flush", typ: 4},
		{name: "get_id", typ: 8, data: true},
		{name: "unknown_odd_type", typ: 11, data: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mem := make([]byte, 1024)
			backing := &batchImage{data: make([]byte, SectorSize)}
			v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
			t.Cleanup(func() { _ = v.Close() })
			q := &SplitQueue{Desc: &[QueueSize]SplitDesc{}, Avail: &SplitAvail{}, Used: &SplitUsed{}}
			binary.LittleEndian.PutUint32(mem, tc.typ)
			q.Desc[0] = SplitDesc{Addr: 0, Len: 16, Flags: descFNext, Next: 1}
			statusID := 1
			if tc.data {
				q.Desc[1] = SplitDesc{Addr: 32, Len: SectorSize, Flags: descFWrite | descFNext, Next: 2}
				statusID = 2
			}
			q.Desc[statusID] = SplitDesc{Addr: 900, Len: 1, Flags: descFWrite}
			mem[32], mem[900] = 0x7f, 0xff
			q.Avail.Idx = 1
			v.VirtQueue[blkQueue] = q
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			if mem[900] != 2 || q.Used.Idx != 1 {
				t.Fatal("unsupported request did not return VIRTIO_BLK_S_UNSUPP")
			}
			if backing.syncs != 0 || backing.reads != 0 || backing.writes != 0 {
				t.Fatal("unsupported request performed backend I/O")
			}
		})
	}
}
