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

// blkReadBuffersOverlap compares every buffer of two requests. The ring bounds
// the total number of buffers in a batch, so the pairwise loops stay small.
func blkReadBuffersOverlap(a, b *blkPendingRequest) bool {
	if a.hi <= b.lo || b.hi <= a.lo {
		return false
	}
	if a.statusAddr == b.statusAddr {
		return true
	}
	for _, x := range a.segments {
		if blkRangesOverlap(x.addr, uint64(len(x.data)), b.statusAddr, 1) {
			return true
		}
		for _, y := range b.segments {
			if blkRangesOverlap(x.addr, uint64(len(x.data)), y.addr, uint64(len(y.data))) {
				return true
			}
		}
	}
	for _, y := range b.segments {
		if blkRangesOverlap(a.statusAddr, 1, y.addr, uint64(len(y.data))) {
			return true
		}
	}

	return false
}

// Addresses and lengths have already passed guestmem.ValidRange, so their sums
// cannot overflow. Empty data buffers do not alias any bytes.
func blkRangesOverlap(a, aLen, b, bLen uint64) bool {
	return aLen != 0 && bLen != 0 && a < b+bLen && b < a+aLen
}

// readRequest fills the data buffers from consecutive disk offsets. An image
// with vectored reads takes several buffers in one call, using the caller's
// scratch list.
func (v *Blk) readRequest(c *blkPendingRequest, scratch *[][]byte) error {
	if v.vector != nil && len(c.segments) > 1 {
		buffers, total := (*scratch)[:0], 0
		for _, segment := range c.segments {
			buffers = append(buffers, segment.data)
			total += len(segment.data)
		}
		n, err := v.vector.ReadvAt(buffers, c.offset)
		clear(buffers) // Do not retain guest buffers between requests.
		*scratch = buffers[:0]
		if n < 0 || n > total {
			return io.ErrUnexpectedEOF
		}
		c.length = uint32(n) // The validated chain bounds the writable prefix.
		if err != nil {
			return err
		}
		if n != total {
			return io.ErrUnexpectedEOF
		}

		return nil
	}
	offset := c.offset
	for _, segment := range c.segments {
		n, err := v.image.ReadAt(segment.data, offset)
		if n < 0 || n > len(segment.data) {
			return io.ErrUnexpectedEOF
		}
		c.length += uint32(n) // Include bytes returned together with an error.
		if err != nil {
			return err
		}
		if n != len(segment.data) {
			return io.ErrUnexpectedEOF
		}
		offset += int64(n)
	}

	return nil
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
			var scratch [][]byte
			for c := range v.readJobs {
				if err := v.readRequest(c, &scratch); err != nil {
					c.result = 1 // VIRTIO_BLK_S_IOERR
				}
				v.readDone <- c
			}
		})
	}
}
