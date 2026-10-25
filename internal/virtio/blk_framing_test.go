package virtio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/disk"
)

// postBlkStreams scatters the two logical streams into descriptors, including
// empty descriptors. Every descriptor has separate guest storage.
func postBlkStreams(q *SplitQueue, mem, readable []byte, readSizes, writeSizes []int) [][]byte {
	var outputs [][]byte
	sizes := append(append([]int(nil), readSizes...), writeSizes...)
	for i, size := range sizes {
		addr := uint64(0x100 + i*0x400)
		desc := SplitDesc{Addr: addr, Len: uint32(size)}
		if i+1 < len(sizes) {
			desc.Flags, desc.Next = descFNext, uint16(i+1)
		}
		data := mem[addr : addr+uint64(size)]
		if i < len(readSizes) {
			copy(data, readable[:size])
			readable = readable[size:]
		} else {
			desc.Flags |= descFWrite
			outputs = append(outputs, data)
		}
		q.Desc[i] = desc
	}
	q.Avail.Ring[q.Avail.Idx%q.Size] = 0
	q.Avail.Idx++

	return outputs
}

func TestBlkDescriptorFraming(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name                  string
		typ                   uint32
		readSizes, writeSizes []int
	}{
		{name: "merged_write_header_data", typ: 1, readSizes: []int{528}, writeSizes: []int{1}},
		{name: "split_write_header_data", typ: 1, readSizes: []int{5, 0, 28, 495}, writeSizes: []int{0, 1, 0}},
		{name: "merged_read_data_status", readSizes: []int{16}, writeSizes: []int{513}},
		{name: "split_read_header_data_status", readSizes: []int{3, 0, 13}, writeSizes: []int{17, 0, 496, 0}},
		{name: "split_flush_header", typ: 4, readSizes: []int{7, 9}, writeSizes: []int{0, 1, 0}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			want := bytes.Repeat([]byte{0x5a}, SectorSize)
			backing := &batchImage{data: bytes.Clone(want)}
			if tc.typ == 1 {
				clear(backing.data)
			}
			mem := bytes.Repeat([]byte{0xcc}, 16*1024)
			v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
			t.Cleanup(func() { _ = v.Close() })
			negotiateBlkFlush(v)
			q := NewSplitQueue(QueueSize)
			v.QueueReady(blkQueue, q)
			header := make([]byte, 16)
			binary.LittleEndian.PutUint32(header, tc.typ)
			readable := header
			if tc.typ == 1 {
				readable = append(readable, want...)
			}
			outputs := postBlkStreams(q, mem, readable, tc.readSizes, tc.writeSizes)
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			var got []byte
			for _, data := range outputs {
				got = append(got, data...)
			}
			if q.Used.Idx != 1 || got[len(got)-1] != 0 {
				t.Fatalf("used=%d status=%d", q.Used.Idx, got[len(got)-1])
			}
			wantLen := uint32(1)
			if tc.typ == 0 {
				wantLen += SectorSize
				if !bytes.Equal(got[:len(got)-1], want) {
					t.Fatal("read payload was not filled before its trailing status")
				}
			} else if tc.typ == 1 && !bytes.Equal(backing.data, want) {
				t.Fatal("write completed without storing its complete payload")
			}
			if q.Used.Ring[0].Len != wantLen {
				t.Fatalf("used.len=%d, want %d", q.Used.Ring[0].Len, wantLen)
			}
		})
	}
}

func TestBlkRejectsInvalidDirections(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		typ    uint32
		mutate func(*SplitQueue)
	}{
		{name: "writable_header", mutate: func(q *SplitQueue) { q.Desc[0].Flags |= descFWrite }},
		{name: "short_readable_header", mutate: func(q *SplitQueue) { q.Desc[0].Len = 15 }},
		{name: "readable_read_payload", mutate: func(q *SplitQueue) { q.Desc[1].Flags &^= descFWrite }},
		{name: "readable_status_after_writable", mutate: func(q *SplitQueue) { q.Desc[2].Flags = 0 }},
		{name: "writable_write_payload", typ: 1, mutate: func(q *SplitQueue) { q.Desc[1].Flags |= descFWrite }},
		{name: "missing_writable_status", typ: 1, mutate: func(q *SplitQueue) { q.Desc[2].Flags = 0 }},
		{name: "unnegotiated_indirect", mutate: func(q *SplitQueue) { q.Desc[0].Flags |= 4 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backing := &batchImage{data: bytes.Repeat([]byte{0x5a}, SectorSize)}
			mem := bytes.Repeat([]byte{0xcc}, 4096)
			v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
			t.Cleanup(func() { _ = v.Close() })
			q := NewSplitQueue(QueueSize)
			v.QueueReady(blkQueue, q)
			post := func(typ uint32) {
				addSegmentedRequest(q, mem, []uint16{0, 1, 2}, typ, 0, 0x100, 0x900,
					[]blkTestSegment{{0x400, SectorSize}})
			}
			post(tc.typ)
			tc.mutate(q)
			if err := v.IO(); !errors.Is(err, ErrBlkDesc) {
				t.Fatalf("IO=%v, want ErrBlkDesc", err)
			}
			if backing.reads != 0 || backing.writes != 0 || q.Used.Idx != 0 || mem[0x900] != 0xff {
				t.Fatal("invalid directions performed I/O or completed the request")
			}
			// Replacing the rejected request must not reuse stale payload or
			// completion counts from its partially constructed snapshot.
			q.Avail.Idx = 0
			post(0)
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			if q.Used.Idx != 1 || q.Used.Ring[0].Len != SectorSize+1 ||
				mem[0x900] != 0 || !bytes.Equal(mem[0x400:0x600], backing.data) {
				t.Fatal("valid request after rejection reused stale state")
			}
		})
	}
}

// prefixReadImage returns a fixed prefix, optionally together with an error.
// The request offset selects the remaining bytes across successive segments.
type prefixReadImage struct {
	batchImage
	limit int
	err   error
}

func (d *prefixReadImage) ReadAt(p []byte, off int64) (int, error) {
	n := min(len(p), max(0, d.limit-int(off)))
	copy(p[:n], d.data[off:off+int64(n)])
	if int(off)+n == d.limit {
		return n, d.err
	}

	return n, nil
}

type prefixVectorImage struct{ *prefixReadImage }

func (d prefixVectorImage) ReadvAt(buffers [][]byte, off int64) (int, error) {
	done := 0
	for _, p := range buffers {
		n, err := d.ReadAt(p, off+int64(done))
		done += n
		if err != nil || n != len(p) {
			return done, err
		}
	}

	return done, nil
}

func TestBlkReadCompletionPrefix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		limit int
		err   error
	}{
		{name: "failure_before_data", err: io.ErrUnexpectedEOF},
		{name: "partial_first_segment", limit: 7, err: io.ErrUnexpectedEOF},
		{name: "partial_later_segment", limit: 97, err: io.ErrUnexpectedEOF},
		{name: "short_without_error", limit: 97},
		{name: "full_with_error", limit: SectorSize, err: io.EOF},
		{name: "full", limit: SectorSize},
	} {
		for _, vector := range []bool{false, true} {
			name := tc.name + "/segments"
			if vector {
				name = tc.name + "/vector"
			}
			t.Run(name, func(t *testing.T) {
				t.Parallel()
				backing := &prefixReadImage{
					batchImage: batchImage{data: bytes.Repeat([]byte{0x5a}, SectorSize)}, limit: tc.limit, err: tc.err,
				}
				var image disk.Image = backing
				if vector {
					image = prefixVectorImage{backing}
				}
				mem := bytes.Repeat([]byte{0xcc}, 16*1024)
				v := newBlk(image, false, 10, benchIRQInjector{}, mem)
				t.Cleanup(func() { _ = v.Close() })
				negotiateBlkFlush(v)
				q := NewSplitQueue(QueueSize)
				v.QueueReady(blkQueue, q)
				// Two independent requests also exercise worker completion copies.
				for i := range 2 {
					base := uint64(i * 0x1000)
					head := uint16(i * 4)
					addSegmentedRequest(q, mem, []uint16{head, head + 1, head + 2, head + 3},
						0, 0, base+0x100, base+0x900, []blkTestSegment{{base + 0x400, 17}, {base + 0x500, 495}})
				}
				if err := v.IO(); err != nil {
					t.Fatal(err)
				}
				wantLen, wantStatus := uint32(tc.limit), byte(1)
				if tc.limit == SectorSize {
					wantLen++
					if tc.err == nil {
						wantStatus = 0
					}
				}
				if q.Used.Idx != 2 {
					t.Fatalf("completed=%d, want 2", q.Used.Idx)
				}
				for i := range 2 {
					base := i * 0x1000
					got := append(bytes.Clone(mem[base+0x400:base+0x411]), mem[base+0x500:base+0x500+495]...)
					if mem[base+0x900] != wantStatus || q.Used.Ring[i].Len != wantLen ||
						!bytes.Equal(got[:tc.limit], backing.data[:tc.limit]) ||
						!bytes.Equal(got[tc.limit:], bytes.Repeat([]byte{0xcc}, SectorSize-tc.limit)) {
						t.Fatalf("status=%d used.len=%d, want %d/%d with untouched unread suffix",
							mem[base+0x900], q.Used.Ring[i].Len, wantStatus, wantLen)
					}
				}
			})
		}
	}
}

func TestBlkStatusOnlyCompletionPrefix(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name       string
		typ        uint32
		sector     uint64
		dataLen    uint32
		readOnly   bool
		wantLen    uint32
		wantStatus byte
	}{
		{name: "write", typ: 1, dataLen: SectorSize, wantLen: 1},
		{name: "read_only_write", typ: 1, dataLen: SectorSize, readOnly: true, wantLen: 1, wantStatus: 1},
		{name: "write_capacity", typ: 1, sector: 1, dataLen: SectorSize, wantLen: 1, wantStatus: 1},
		{name: "read_capacity", sector: 1, dataLen: SectorSize, wantStatus: 1},
		{name: "unsupported_status", typ: 11, wantLen: 1, wantStatus: 2},
		{name: "unsupported_writable_payload", typ: 11, dataLen: SectorSize, wantStatus: 2},
		{name: "flush", typ: 4, wantLen: 1},
		{name: "flush_readable_payload", typ: 4, dataLen: SectorSize, wantLen: 1},
		{name: "flush_writable_payload", typ: 4, dataLen: SectorSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			backing := &batchImage{data: make([]byte, SectorSize)}
			mem := bytes.Repeat([]byte{0xcc}, 4096)
			v := newBlk(backing, tc.readOnly, 10, benchIRQInjector{}, mem)
			t.Cleanup(func() { _ = v.Close() })
			negotiateBlkFlush(v)
			q := NewSplitQueue(QueueSize)
			v.QueueReady(blkQueue, q)
			addSegmentedRequest(q, mem, []uint16{0, 1, 2}, tc.typ, tc.sector, 0x100, 0x900,
				[]blkTestSegment{{0x400, tc.dataLen}})
			if tc.typ == 11 || tc.name == "flush_writable_payload" {
				q.Desc[1].Flags |= descFWrite
			}
			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			if q.Used.Idx != 1 || q.Used.Ring[0].Len != tc.wantLen || mem[0x900] != tc.wantStatus {
				t.Fatalf("used=%d len=%d status=%d, want 1/%d/%d",
					q.Used.Idx, q.Used.Ring[0].Len, mem[0x900], tc.wantLen, tc.wantStatus)
			}
			if tc.wantStatus != 0 && !bytes.Equal(mem[0x400:0x400+tc.dataLen], bytes.Repeat([]byte{0xcc}, int(tc.dataLen))) {
				t.Fatal("failed/unsupported request changed payload bytes")
			}
		})
	}
}
