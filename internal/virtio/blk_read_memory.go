package virtio

import (
	"runtime"
	"unsafe"
)

// blkReadOverlapsQueue excludes buffers aliasing queue control memory from
// parallel reads. Another request's completion can update the used ring while
// a read is in flight, and the queue owner still inspects notification flags.
func blkReadOverlapsQueue(q *SplitQueue, c *blkPendingRequest) bool {
	for _, region := range [...]struct {
		ptr  unsafe.Pointer
		size uintptr
	}{
		{ptr: unsafe.Pointer(q.Desc), size: unsafe.Sizeof(*q.Desc)},
		{ptr: unsafe.Pointer(q.Avail), size: unsafe.Sizeof(*q.Avail)},
		{ptr: unsafe.Pointer(q.Used), size: unsafe.Sizeof(*q.Used)},
	} {
		for _, segment := range c.segments {
			if blkPointerRangesOverlap(
				unsafe.Pointer(unsafe.SliceData(segment.data)),
				uintptr(len(segment.data)),
				region.ptr,
				region.size,
			) {
				return true
			}
		}
		if blkPointerRangesOverlap(
			unsafe.Pointer(c.status),
			1,
			region.ptr,
			region.size,
		) {
			return true
		}
	}

	return false
}

// blkPointerRangesOverlap compares live object addresses, never reconstructing
// a pointer from an integer. Difference-based comparisons cannot overflow.
func blkPointerRangesOverlap(a unsafe.Pointer, aLen uintptr, b unsafe.Pointer, bLen uintptr) bool {
	if a == nil || b == nil || aLen == 0 || bLen == 0 {
		return false
	}
	aAddr, bAddr := uintptr(a), uintptr(b)
	var overlap bool
	if aAddr <= bAddr {
		overlap = bAddr-aAddr < aLen
	} else {
		overlap = aAddr-bAddr < bLen
	}
	runtime.KeepAlive(a)
	runtime.KeepAlive(b)

	return overlap
}
