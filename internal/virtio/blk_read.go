package virtio

import "io"

// Limit outstanding host reads independently of guest queue size. Writes,
// allocation and durability barriers stay on the queue owner goroutine.
const blkReadWorkers = 4

// independentBlkReads selects consecutive reads with disjoint guest output
// buffers. Reads may overlap on disk, but concurrent writes to aliased guest
// data/status bytes would race. Fall back to the ordered path at an alias.
func independentBlkReads(q *SplitQueue, pending []blkPendingRequest) int {
	for i := range pending {
		c := &pending[i]
		if c.result != 0 || c.write || c.flush {
			return i
		}
		if blkReadOverlapsQueue(q, c) {
			return i
		}
		for j := range i {
			if blkReadBuffersOverlap(c, &pending[j]) {
				return i
			}
		}
	}

	return len(pending)
}

func blkReadBuffersOverlap(a, b *blkPendingRequest) bool {
	return blkRangesOverlap(a.dataAddr, uint64(len(a.data)), b.dataAddr, uint64(len(b.data))) ||
		blkRangesOverlap(a.dataAddr, uint64(len(a.data)), b.statusAddr, 1) ||
		blkRangesOverlap(a.statusAddr, 1, b.dataAddr, uint64(len(b.data))) ||
		a.statusAddr == b.statusAddr
}

// Addresses and lengths have already passed guestmem.ValidRange, so their sums
// cannot overflow. Empty data buffers do not alias any bytes.
func blkRangesOverlap(a, aLen, b, bLen uint64) bool {
	return aLen != 0 && bLen != 0 && a < b+bLen && b < a+aLen
}

func (v *Blk) readRequest(c *blkPendingRequest) error {
	n, err := v.image.ReadAt(c.data, c.offset)
	if err == nil && n != len(c.data) {
		return io.ErrUnexpectedEOF
	}

	return err
}

// readRequests runs only independent reads. queueMu remains held by IO until
// every read finishes, so writes, FLUSH, Reset and Close cannot overtake them.
// Workers are reused across batches and joined by Close. In-flight file I/O is
// drained, not abandoned: guest memory and the file must remain valid until done.
func (v *Blk) readRequests(q *SplitQueue, queueSize uint16, pending []blkPendingRequest, writeback bool) error {
	v.startReadWorkers()
	for i := range pending {
		v.readJobs <- &pending[i]
	}

	var interruptErr error
	for remaining := len(pending); remaining > 0; {
		var ready [blkReadWorkers]blkPendingRequest
		ready[0] = *<-v.readDone
		count := 1
		// Combine only completions already ready. Never delay a fast read's
		// notification while waiting for a slower outstanding read.
	drain:
		for count < min(remaining, len(ready)) {
			select {
			case c := <-v.readDone:
				ready[count] = *c
				count++
			default:
				break drain
			}
		}
		if writeback {
			if err := v.completeRequests(q, queueSize, ready[:count]); err != nil && interruptErr == nil {
				interruptErr = err
			}
		}
		remaining -= count
	}

	return interruptErr
}

func (v *Blk) startReadWorkers() {
	if v.readJobs != nil {
		return
	}
	// QueueSize bounds the snapshot. Buffering both channels lets the queue
	// owner enqueue a batch without blocking behind completion publication.
	v.readJobs = make(chan *blkPendingRequest, QueueSize)
	v.readDone = make(chan *blkPendingRequest, QueueSize)
	for range blkReadWorkers {
		v.readWorkers.Go(func() {
			for c := range v.readJobs {
				if err := v.readRequest(c); err != nil {
					c.result = 1 // VIRTIO_BLK_S_IOERR
				}
				v.readDone <- c
			}
		})
	}
}
