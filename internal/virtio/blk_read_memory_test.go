package virtio

import (
	"testing"
	"unsafe"
)

func TestBlkReadOverlapsQueue(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		region string
		status bool
		tail   bool
	}{
		{name: "descriptor_data", region: "descriptor"},
		{name: "available_data", region: "available"},
		{name: "used_data", region: "used"},
		{name: "used_data_tail", region: "used", tail: true},
		{name: "descriptor_status", region: "descriptor", status: true},
		{name: "available_status", region: "available", status: true},
		{name: "used_status", region: "used", status: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			q := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
			var region []byte
			switch tc.region {
			case "descriptor":
				region = unsafe.Slice((*byte)(unsafe.Pointer(q.Desc)), int(unsafe.Sizeof(*q.Desc)))
			case "available":
				region = unsafe.Slice((*byte)(unsafe.Pointer(q.Avail)), int(unsafe.Sizeof(*q.Avail)))
			case "used":
				region = unsafe.Slice((*byte)(unsafe.Pointer(q.Used)), int(unsafe.Sizeof(*q.Used)))
			}
			if tc.tail {
				region = region[len(region)-1:]
			}
			c := blkPendingRequest{data: make([]byte, 8), status: new(byte)}
			if tc.status {
				c.status = &region[0]
			} else {
				c.data = region
			}
			if !blkReadOverlapsQueue(q, &c) {
				t.Fatal("read aliasing queue memory was eligible for parallel I/O")
			}
		})
	}
}

func TestBlkReadSeparateQueueMemory(t *testing.T) {
	t.Parallel()

	q := &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
	c := blkPendingRequest{data: make([]byte, 8), status: new(byte)}
	if blkReadOverlapsQueue(q, &c) {
		t.Fatal("separate read buffers unexpectedly overlap queue memory")
	}
	c.data = unsafe.Slice((*byte)(unsafe.Pointer(q.Used)), 0)
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
