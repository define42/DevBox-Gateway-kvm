package virtio

import (
	"bytes"
	"encoding/binary"
	"errors"
	"testing"
)

// blkTestSegment is a guest data buffer of a test request.
type blkTestSegment struct {
	addr uint64
	len  uint32
}

// addSegmentedRequest publishes a header, data and status chain through ids,
// one descriptor index per buffer; the indices need not be consecutive.
func addSegmentedRequest(q *SplitQueue, mem []byte, ids []uint16, typ uint32, sector, header, status uint64,
	segments []blkTestSegment,
) {
	binary.LittleEndian.PutUint32(mem[header:], typ)
	binary.LittleEndian.PutUint64(mem[header+8:], sector)
	dataFlags := uint16(descFNext)
	if typ == 0 {
		dataFlags |= descFWrite
	}
	chain := []SplitDesc{{Addr: header, Len: 16, Flags: descFNext}}
	for _, segment := range segments {
		chain = append(chain, SplitDesc{Addr: segment.addr, Len: segment.len, Flags: dataFlags})
	}
	chain = append(chain, SplitDesc{Addr: status, Len: 1, Flags: descFWrite})
	for i, desc := range chain {
		if i+1 < len(ids) {
			desc.Next = ids[i+1]
		}
		q.Desc[ids[i]] = desc
	}
	mem[status] = 0xff
	q.Avail.Ring[q.Avail.Idx%QueueSize] = ids[0]
	q.Avail.Idx++
}

func TestBlkAdvertisesSegmentLimit(t *testing.T) {
	t.Parallel()

	v := newBlk(&batchImage{data: make([]byte, 4*SectorSize)}, false, 10, benchIRQInjector{}, nil)
	t.Cleanup(func() { _ = v.Close() })
	if v.DeviceFeatures()&blkFeatureSegMax == 0 {
		t.Fatal("VIRTIO_BLK_F_SEG_MAX is not offered")
	}
	if blkReadWorkers*(blkMaxSegments+2) > QueueSize {
		t.Fatal("the ring cannot hold one seg_max request per read worker")
	}
	if v.DeviceConfigLen() != 16 {
		t.Fatalf("device configuration length = %d, want through seg_max", v.DeviceConfigLen())
	}
	var cfg [16]byte
	v.MMIO(deviceCfgOffset, cfg[:8], false)
	v.MMIO(deviceCfgOffset+8, cfg[8:], false)
	le := binary.LittleEndian
	if le.Uint64(cfg[:]) != 4 || le.Uint32(cfg[8:]) != 0 || le.Uint32(cfg[12:]) != blkMaxSegments {
		t.Fatalf("virtio_blk_config = %x", cfg)
	}
}

func TestBlkScatteredSegments(t *testing.T) {
	t.Parallel()

	mem := make([]byte, 64*1024)
	backing := &batchImage{data: make([]byte, 8*SectorSize)}
	v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
	t.Cleanup(func() { _ = v.Close() })
	q := NewSplitQueue(QueueSize)
	v.QueueReady(blkQueue, q)

	writes := []blkTestSegment{{0x1000, 512}, {0x3000, 1024}, {0x2000, 512}}
	var want []byte
	for i, segment := range writes {
		buffer := mem[segment.addr : segment.addr+uint64(segment.len)]
		for j := range buffer {
			buffer[j] = byte(i*64 + j%61)
		}
		want = append(want, buffer...)
	}
	addSegmentedRequest(q, mem, []uint16{0, 1, 5, 3, 7}, 1, 1, 0x100, 0x200, writes)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if mem[0x200] != 0 || q.Used.Idx != 1 || q.Used.Ring[0].Len != 16+2048+1 {
		t.Fatalf("segmented write: status=%d used=%d len=%d", mem[0x200], q.Used.Idx, q.Used.Ring[0].Len)
	}
	if !bytes.Equal(backing.data[SectorSize:SectorSize+2048], want) {
		t.Fatal("segments were not written at consecutive disk offsets")
	}

	reads := []blkTestSegment{{0x8000, 1536}, {0x6000, 512}}
	addSegmentedRequest(q, mem, []uint16{9, 10, 11, 12}, 0, 1, 0x300, 0x400, reads)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	got := append(bytes.Clone(mem[0x8000:0x8000+1536]), mem[0x6000:0x6000+512]...)
	if mem[0x400] != 0 || !bytes.Equal(got, want) {
		t.Fatalf("segmented read: status=%d, data matches=%v", mem[0x400], bytes.Equal(got, want))
	}
}

// seg_max limits what drivers send, but the device accepts any request whose
// descriptors fit the ring.
func TestBlkLargestRequestFillsRing(t *testing.T) {
	t.Parallel()

	const ringSegments = QueueSize - 2
	mem := make([]byte, 0x1000+ringSegments*SectorSize)
	backing := &batchImage{data: make([]byte, ringSegments*SectorSize)}
	v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
	t.Cleanup(func() { _ = v.Close() })
	q := NewSplitQueue(QueueSize)
	v.QueueReady(blkQueue, q)
	ids := make([]uint16, QueueSize)
	segments := make([]blkTestSegment, ringSegments)
	for i := range ids {
		ids[i] = uint16(i)
	}
	for i := range segments {
		segments[i] = blkTestSegment{addr: 0x1000 + uint64(i)*SectorSize, len: SectorSize}
		mem[segments[i].addr] = byte(i + 1)
	}
	addSegmentedRequest(q, mem, ids, 1, 0, 0x100, 0x200, segments)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if mem[0x200] != 0 || backing.writes != ringSegments {
		t.Fatalf("ring-sized request: status=%d writes=%d", mem[0x200], backing.writes)
	}
	for i := range segments {
		if backing.data[i*SectorSize] != byte(i+1) {
			t.Fatalf("segment %d landed at the wrong sector", i)
		}
	}

	// The same request starting one sector later ends past the capacity.
	addSegmentedRequest(q, mem, ids, 1, 1, 0x100, 0x200, segments)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if mem[0x200] != 1 || backing.writes != ringSegments {
		t.Fatalf("request past the capacity: status=%d writes=%d", mem[0x200], backing.writes)
	}

	// Two available entries naming the same chain claim more buffers than
	// the ring holds; the batch is malformed and must not perform I/O.
	addSegmentedRequest(q, mem, ids, 1, 0, 0x100, 0x200, segments)
	addSegmentedRequest(q, mem, ids, 1, 0, 0x100, 0x200, segments)
	if err := v.IO(); !errors.Is(err, ErrBlkDesc) || backing.writes != ringSegments {
		t.Fatalf("batch sharing descriptors: IO=%v writes=%d", err, backing.writes)
	}
}

func TestBlkParallelReadsCheckEverySegment(t *testing.T) {
	t.Parallel()

	q := NewSplitQueue(QueueSize)
	read := func(status uint64, segments ...blkTestSegment) blkPendingRequest {
		c := blkPendingRequest{status: new(byte), statusAddr: status}
		for _, segment := range segments {
			c.segments = append(c.segments, blkSegment{data: make([]byte, segment.len), addr: segment.addr})
		}
		c.setBounds()

		return c
	}
	for _, tc := range []struct {
		name   string
		second blkPendingRequest
		want   int
	}{
		{name: "disjoint", second: read(0x901, blkTestSegment{0x2000, 512}, blkTestSegment{0x4000, 512}), want: 2},
		{
			name:   "later_segments_alias",
			second: read(0x901, blkTestSegment{0x2000, 512}, blkTestSegment{0x3100, 512}),
			want:   1,
		},
		{name: "status_in_segment", second: read(0x1010, blkTestSegment{0x2000, 512}), want: 1},
		{name: "segment_covers_status", second: read(0x901, blkTestSegment{0x800, 0x200}), want: 1},
	} {
		first := read(0x900, blkTestSegment{0x1000, 512}, blkTestSegment{0x3000, 512})
		if got := independentBlkReads(q, []blkPendingRequest{first, tc.second}); got != tc.want {
			t.Fatalf("%s: independent reads = %d, want %d", tc.name, got, tc.want)
		}
	}

	// Disjoint segmented reads complete correctly through the parallel path.
	mem := make([]byte, 64*1024)
	backing := &batchImage{data: make([]byte, 8*SectorSize)}
	for i := range backing.data {
		backing.data[i] = byte(i * 7)
	}
	v := newBlk(backing, false, 10, benchIRQInjector{}, mem)
	t.Cleanup(func() { _ = v.Close() })
	ring := NewSplitQueue(QueueSize)
	v.QueueReady(blkQueue, ring)
	addSegmentedRequest(ring, mem, []uint16{0, 1, 2, 3}, 0, 0, 0x100, 0x200,
		[]blkTestSegment{{0x1000, 512}, {0x5000, 1024}})
	addSegmentedRequest(ring, mem, []uint16{4, 5, 6, 7}, 0, 4, 0x300, 0x400,
		[]blkTestSegment{{0x3000, 1024}, {0x7000, 512}})
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	first := append(bytes.Clone(mem[0x1000:0x1200]), mem[0x5000:0x5400]...)
	second := append(bytes.Clone(mem[0x3000:0x3400]), mem[0x7000:0x7200]...)
	if mem[0x200] != 0 || mem[0x400] != 0 || ring.Used.Idx != 2 ||
		!bytes.Equal(first, backing.data[:1536]) || !bytes.Equal(second, backing.data[4*SectorSize:4*SectorSize+1536]) {
		t.Fatal("parallel segmented reads returned the wrong data")
	}
	if backing.reads != 4 {
		t.Fatalf("backend reads = %d, want one per segment", backing.reads)
	}
}
