package virtio

import (
	"runtime"
	"unsafe"
)

// blkReadOverlapsQueue excludes buffers aliasing queue control memory from
// parallel reads. Another request's completion can update the used ring while
// a read is in flight, and the queue owner still inspects notification flags.
func blkReadOverlapsQueue(q *SplitQueue, c *blkPendingRequest) bool {
	var avail, used *splitRingHeader
	var availRing []uint16
	var usedRing []SplitUsedElem
	if q.Avail != nil {
		avail, availRing = q.Avail.splitRingHeader, q.Avail.Ring
	}
	if q.Used != nil {
		used, usedRing = q.Used.splitRingHeader, q.Used.Ring
	}
	for _, region := range [...]struct {
		ptr  unsafe.Pointer
		size uintptr
	}{
		{ptr: unsafe.Pointer(unsafe.SliceData(q.Desc)), size: uintptr(len(q.Desc)) * unsafe.Sizeof(SplitDesc{})},
		{ptr: unsafe.Pointer(avail), size: unsafe.Sizeof(splitRingHeader{})},
		{ptr: unsafe.Pointer(unsafe.SliceData(availRing)), size: uintptr(len(availRing)) * unsafe.Sizeof(uint16(0))},
		{ptr: unsafe.Pointer(used), size: unsafe.Sizeof(splitRingHeader{})},
		{ptr: unsafe.Pointer(unsafe.SliceData(usedRing)), size: uintptr(len(usedRing)) * unsafe.Sizeof(SplitUsedElem{})},
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
