package virtio

import (
	"errors"
	"testing"
)

func TestBlkCompletionNotificationSuppression(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		writeback bool
		flags     uint16
	}{
		{name: "writethrough_enabled"},
		{name: "writethrough_suppressed", flags: availNoInterrupt},
		{name: "writeback_enabled", writeback: true},
		{name: "writeback_suppressed", writeback: true, flags: availNoInterrupt},
		{name: "reserved_flag_not_suppressed", flags: 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			backing := &batchImage{data: make([]byte, 4*SectorSize)}
			v, q, mem := newFlushTestDevice(t, backing, false)
			if tc.writeback {
				negotiateBlkFlush(v)
			}
			q.Avail.Flags = tc.flags
			// Unnegotiated EVENT_IDX must not suppress the ordinary flag path.
			q.Avail.UsedEvent = 0xffff
			statuses := []int{
				addFlushTestRequest(q, mem, 0, 0),
				addFlushTestRequest(q, mem, 1, 0),
			}
			var assertions int
			v.SetIRQLine(func(high bool) error {
				if !high {
					return nil
				}
				assertions++
				if q.Used.Idx == 0 {
					t.Error("interrupt preceded used-index publication")
				}
				for i := range q.Used.Idx {
					id := q.Used.Ring[i].ID
					if id > 3 || id%3 != 0 || mem[statuses[id/3]] != 0 {
						t.Error("interrupt preceded status publication")
					}
				}

				return nil
			})

			if err := v.IO(); err != nil {
				t.Fatal(err)
			}
			if q.Used.Idx != 2 || v.LastAvailIdx[blkQueue] != 2 {
				t.Fatalf("completion indices: used=%d available=%d", q.Used.Idx, v.LastAvailIdx[blkQueue])
			}
			var seen [2]bool
			for i, status := range statuses {
				id := q.Used.Ring[i].ID
				if mem[status] != 0 || id > 3 || id%3 != 0 || seen[id/3] {
					t.Fatalf("request %d: status=%d used ID=%d", i, mem[status], q.Used.Ring[i].ID)
				}
				seen[id/3] = true
			}
			suppressed := notificationSuppression && tc.flags&availNoInterrupt != 0
			if suppressed {
				if assertions != 0 || v.interruptPending() || len(v.irqWake) != 0 {
					t.Fatalf("suppressed completion raised an interrupt: assertions=%d", assertions)
				}
			} else if assertions == 0 || !v.interruptPending() {
				t.Fatal("completion requiring notification did not raise an interrupt")
			}
		})
	}
}

func TestBlkCompletionNotificationReenabled(t *testing.T) {
	t.Parallel()

	backing := &batchImage{data: make([]byte, 4*SectorSize)}
	v, q, mem := newFlushTestDevice(t, backing, false)
	negotiateBlkFlush(v)
	q.Avail.Flags = availNoInterrupt
	addFlushTestRequest(q, mem, 0, 0)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	_ = v.clearInterrupt() // The conservative fallback is allowed to notify.
	q.Avail.Flags = 0
	status := addFlushTestRequest(q, mem, 1, 0)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if q.Used.Idx != 2 || mem[status] != 0 || !v.interruptPending() {
		t.Fatal("reenabling notifications lost the subsequent completion")
	}
	if got := v.clearInterrupt(); got != 1 {
		t.Fatalf("interrupt status=%d, want queue interrupt", got)
	}
	if v.interruptPending() {
		t.Fatal("acknowledged queue interrupt remained pending")
	}
}

func TestBlkSuppressedCompletionPreservesPendingInterrupt(t *testing.T) {
	t.Parallel()

	backing := &batchImage{data: make([]byte, 4*SectorSize)}
	v, q, mem := newFlushTestDevice(t, backing, false)
	q.Avail.Flags = availNoInterrupt
	if err := v.Interrupt(blkQueue); err != nil {
		t.Fatal(err)
	}
	if err := v.ConfigChanged(); err != nil {
		t.Fatal(err)
	}
	addFlushTestRequest(q, mem, 0, 0)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if got := v.clearInterrupt(); got != 3 {
		t.Fatalf("suppressed completion changed pending queue/config interrupts: ISR=%d", got)
	}
	if err := v.ConfigChanged(); err != nil {
		t.Fatal(err)
	}
	if got := v.clearInterrupt(); got != 2 {
		t.Fatalf("queue suppression affected configuration interrupt: ISR=%d", got)
	}
}

func TestBlkSuppressedCompletionPreservesInterruptRecovery(t *testing.T) {
	t.Parallel()

	backing := &batchImage{data: make([]byte, 4*SectorSize)}
	v, q, mem := newFlushTestDevice(t, backing, false)
	var assertions int
	v.SetIRQLine(func(high bool) error {
		if high {
			assertions++
			if assertions == 1 {
				return errTestBlkIRQ
			}
		}

		return nil
	})
	addFlushTestRequest(q, mem, 0, 0)
	if err := v.IO(); !errors.Is(err, errTestBlkIRQ) {
		t.Fatalf("first completion IRQ error=%v, want %v", err, errTestBlkIRQ)
	}
	q.Avail.Flags = availNoInterrupt
	status := addFlushTestRequest(q, mem, 1, 0)
	if err := v.IO(); err != nil {
		t.Fatal(err)
	}
	if q.Used.Idx != 2 || mem[status] != 0 || !v.interruptPending() {
		t.Fatal("suppressed completion discarded the failed pending interrupt")
	}
	before := assertions
	if err := v.ReinjectIfPending(); err != nil {
		t.Fatal(err)
	}
	if assertions != before+1 {
		t.Fatal("suppression prevented retrying the pending interrupt")
	}
	_ = v.clearInterrupt()
	before = assertions
	if err := v.ReinjectIfPending(); err != nil {
		t.Fatal(err)
	}
	if assertions != before {
		t.Fatal("recovery retried an acknowledged interrupt")
	}
}
