package virtio

import "io"

// blkFlush is a FLUSH handed to the flush worker. It keeps its own copy of the
// request because the queue owner reuses the batch snapshot.
type blkFlush struct {
	q         *SplitQueue
	queueSize uint16
	request   blkPendingRequest
}

// queueFlush starts c's Sync without waiting for it. A guest reposting
// descriptors can have more FLUSH requests outstanding than the ring holds, so
// a full backlog first publishes the oldest; the channels then never block.
func (v *Blk) queueFlush(q *SplitQueue, queueSize uint16, c *blkPendingRequest) error {
	var interruptErr error
	if v.flushing == QueueSize {
		interruptErr = v.completeFlush(<-v.flushDone)
	}
	v.startFlushWorker()
	v.flushing++
	v.flushJobs <- blkFlush{q: q, queueSize: queueSize, request: *c}

	return interruptErr
}

// startFlushWorker starts the goroutine that syncs FLUSH requests. It never
// touches guest memory: results return to the queue owner for publication.
// Every write a queued FLUSH covers returned before it was queued, so one Sync
// starting afterward serves all the FLUSH requests already waiting.
func (v *Blk) startFlushWorker() {
	if v.flushJobs != nil {
		return
	}
	v.flushJobs = make(chan blkFlush, QueueSize)
	v.flushDone = make(chan blkFlush, QueueSize)
	v.flushWorker.Go(func() {
		var batch [QueueSize]blkFlush
		for f := range v.flushJobs {
			batch[0] = f
			count := 1
		drain:
			for count < len(batch) {
				select {
				case f, ok := <-v.flushJobs:
					if !ok {
						break drain
					}
					batch[count] = f
					count++
				default:
					break drain
				}
			}
			result := byte(0)
			if err := v.syncImage(); err != nil {
				result = 1 // VIRTIO_BLK_S_IOERR
			}
			for i := range batch[:count] {
				batch[i].request.result = result
				v.flushDone <- batch[i]
			}
			clear(batch[:count])
			v.Notify(blkQueue)
		}
	})
}

// completeFlush publishes a FLUSH whose Sync has returned.
func (v *Blk) completeFlush(f blkFlush) error {
	v.flushing--
	completed := [1]blkPendingRequest{f.request}

	return v.completeRequests(f.q, f.queueSize, completed[:])
}

// awaitFlushes publishes FLUSH requests as their Syncs return. It returns early
// once the driver posts more requests, which the next IO call takes while the
// remaining Syncs continue.
func (v *Blk) awaitFlushes(q *SplitQueue) error {
	var interruptErr error
	for v.flushing > 0 {
		select {
		case <-v.done:
			return io.ErrClosedPipe
		case f := <-v.flushDone:
			if err := v.completeFlush(f); err != nil && interruptErr == nil {
				interruptErr = err
			}
		case <-v.kick:
			if LoadU16(&q.Avail.Idx) != v.LastAvailIdx[blkQueue] {
				return interruptErr
			}
		}
	}

	return interruptErr
}

// finishFlushes waits for every outstanding FLUSH and publishes it to the queue
// it came from, before Reset or QueueReady replaces that queue.
func (v *Blk) finishFlushes() {
	for v.flushing > 0 {
		_ = v.completeFlush(<-v.flushDone)
	}
}
