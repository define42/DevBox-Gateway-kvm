package disk

import "errors"

var errQCOW2Capacity = errors.New(
	"qcow2: refcount table cannot cover the full virtual disk; normalize the image through the gateway image catalog",
)

// validateGrowthCapacity proves that every missing data cluster and L2 table can
// be appended without expanding the refcount table. Existing file space cannot
// be reused by this allocator. Each absent refcount block also consumes a cluster
// in the range it describes, sometimes the last cluster before the next range.
// Work is bounded by the refcount table, not the number of guest clusters.
func (q *qcow2Image) validateGrowthCapacity(fileSize, missing uint64) error {
	if missing == 0 {
		return nil
	}
	next := (fileSize + q.clusterSize - 1) / q.clusterSize
	// Data and L2 offsets must fit their on-disk field as well as signed host I/O.
	limit := min(uint64(maxInt64)/q.clusterSize, qcow2OffsetMask/q.clusterSize+1)
	for missing > 0 {
		index := next / q.refcountBlockEntries
		if index >= uint64(len(q.refcountTable)) || next >= limit {
			return errQCOW2Capacity
		}
		end := min((index+1)*q.refcountBlockEntries, limit)
		if q.refcountTable[index] == 0 {
			next++ // The allocator first appends this range's refcount block.
		}
		count := min(missing, end-next)
		next += count
		missing -= count
	}

	return nil
}
