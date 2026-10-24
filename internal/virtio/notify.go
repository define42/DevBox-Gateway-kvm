package virtio

const availNoInterrupt = 1 // VRING_AVAIL_F_NO_INTERRUPT

// notifyUsed signals used buffers on queue unless the driver suppressed it.
// Call it only after publishing every completed buffer and the used index.
// EVENT_IDX is not offered: used_event must not influence this decision.
//
// Honoring the hint matters most under MSI-X, where every call sends a
// message: a driver polling the ring (e.g. NAPI) sets the flag rather than
// take an interrupt per buffer, which INTx used to coalesce implicitly.
func (t *ModernTransport) notifyUsed(queue int, q *SplitQueue) error {
	if notificationSuppression {
		// The guest reenables notifications and then checks Used.Idx. A full
		// Store-to-Load fence here pairs with the guest's barrier: it must not
		// see an old used index while we see its old suppression flag.
		notificationBarrier()
		if LoadU16(&q.Avail.Flags)&availNoInterrupt != 0 {
			// An older IRQ may still be pending. Suppressing this completion
			// must neither acknowledge it nor prevent its recovery retry.
			return nil
		}
	}

	return t.Interrupt(queue)
}
