package virtio

import (
	"testing"
	"unsafe"
)

// sliceBytes views the memory backing s.
func sliceBytes[T any](s []T) []byte {
	var zero T

	return unsafe.Slice((*byte)(unsafe.Pointer(unsafe.SliceData(s))), uintptr(len(s))*unsafe.Sizeof(zero))
}

// setBlkData replaces a test request's data buffers.
func setBlkData(c *blkPendingRequest, buffers ...[]byte) {
	c.segments = nil
	for _, data := range buffers {
		c.segments = append(c.segments, blkSegment{data: data})
	}
}

func TestBlkReadOverlapsQueue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		region string
		status bool
		tail   bool
	}{
		{name: "descriptor_data", region: "descriptor"},
		{name: "available_header_data", region: "available_header"},
		{name: "available_data", region: "available"},
		{name: "used_header_data", region: "used_header"},
		{name: "used_data", region: "used"},
		{name: "used_data_tail", region: "used", tail: true},
		{name: "descriptor_status", region: "descriptor", status: true},
		{name: "available_status", region: "available", status: true},
		{name: "used_status", region: "used", status: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q := NewSplitQueue(QueueSize)
			var region []byte
			switch tc.region {
			case "descriptor":
				region = sliceBytes(q.Desc)
			case "available_header":
				region = unsafe.Slice((*byte)(unsafe.Pointer(q.Avail.splitRingHeader)), unsafe.Sizeof(splitRingHeader{}))
			case "available":
				region = sliceBytes(q.Avail.Ring)
			case "used_header":
				region = unsafe.Slice((*byte)(unsafe.Pointer(q.Used.splitRingHeader)), unsafe.Sizeof(splitRingHeader{}))
			case "used":
				region = sliceBytes(q.Used.Ring)
			}
			if tc.tail {
				region = region[len(region)-1:]
			}
			c := blkPendingRequest{status: new(byte)}
			setBlkData(&c, make([]byte, 8))
			if tc.status {
				c.status = &region[0]
			} else {
				// Only a later segment aliases the queue.
				setBlkData(&c, make([]byte, 8), region)
			}
			if !blkReadOverlapsQueue(q, &c) {
				t.Fatal("read aliasing queue memory was eligible for parallel I/O")
			}
		})
	}
}

func TestBlkReadSeparateQueueMemory(t *testing.T) {
	t.Parallel()

	q := NewSplitQueue(QueueSize)
	c := blkPendingRequest{status: new(byte)}
	setBlkData(&c, make([]byte, 8), make([]byte, 8))
	if blkReadOverlapsQueue(q, &c) {
		t.Fatal("separate read buffers unexpectedly overlap queue memory")
	}
	setBlkData(&c, sliceBytes(q.Used.Ring)[:0])
	if blkReadOverlapsQueue(q, &c) {
		t.Fatal("empty read buffer overlaps queue memory")
	}
	if blkReadOverlapsQueue(&SplitQueue{}, &c) {
		t.Fatal("unmapped queue overlaps a read buffer")
	}
}

func TestBlkPointerRangesOverlap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name                 string
		aStart, aLen, bStart int
		bLen                 int
		want                 bool
	}{
		{name: "same", aStart: 8, aLen: 8, bStart: 8, bLen: 8, want: true},
		{name: "first_inside_second", aStart: 8, aLen: 1, bStart: 4, bLen: 8, want: true},
		{name: "second_inside_first", aStart: 4, aLen: 8, bStart: 8, bLen: 1, want: true},
		{name: "touching_before", aStart: 4, aLen: 4, bStart: 8, bLen: 4},
		{name: "touching_after", aStart: 8, aLen: 4, bStart: 4, bLen: 4},
		{name: "empty_first", aStart: 4, bStart: 4, bLen: 4},
		{name: "empty_second", aStart: 4, aLen: 4, bStart: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var data [32]byte
			got := blkPointerRangesOverlap(
				unsafe.Pointer(&data[tc.aStart]),
				uintptr(tc.aLen),
				unsafe.Pointer(&data[tc.bStart]),
				uintptr(tc.bLen),
			)
			if got != tc.want {
				t.Fatalf("overlap=%v, want %v", got, tc.want)
			}
		})
	}
}
