package virtio

const blkAvailNoInterrupt = 1

// notifyCompletion checks the driver's split-ring suppression hint only after
// the caller has published all completed buffers and the updated used index.
// EVENT_IDX is not offered: used_event must not influence this decision.
func (v *Blk) notifyCompletion(q *SplitQueue) error {
	if blkNotificationSuppression {
		// The guest reenables notifications and then checks Used.Idx. A full
		// Store-to-Load fence here pairs with the guest's barrier: it must not
		// see an old used index while we see its old suppression flag.
		blkNotificationBarrier()
		if LoadU16(&q.Avail.Flags)&blkAvailNoInterrupt != 0 {
			// An older IRQ may still be pending. Suppressing this completion
			// must neither acknowledge it nor prevent its recovery retry.
			return nil
		}
	}

	return v.Interrupt()
}
