package virtio

import (
	"cmp"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestInputRejectsMalformedEventChains(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		size  uint16
		head  uint16
		descs []SplitDesc
	}{
		{name: "head beyond table", head: QueueSize},
		{name: "maximum head", head: ^uint16(0)},
		{name: "head beyond negotiated size", size: 2, head: 2},
		{name: "next beyond table", descs: []SplitDesc{{Flags: descFNext, Next: QueueSize}}},
		{name: "maximum next", descs: []SplitDesc{{Flags: descFNext, Next: ^uint16(0)}}},
		{name: "next beyond negotiated size", size: 2, descs: []SplitDesc{{Flags: descFNext, Next: 2}}},
		{name: "self cycle", descs: []SplitDesc{{Flags: descFNext, Next: 0}}},
		{name: "two descriptor cycle", descs: []SplitDesc{
			{Flags: descFNext, Next: 1},
			{Flags: descFNext, Next: 0},
		}},
		{name: "partially writable cycle", descs: []SplitDesc{{Addr: 0x100, Len: 4, Flags: descFWrite | descFNext}}},
		{name: "complete event before cycle", descs: []SplitDesc{
			{Addr: 0x100, Len: inputEventLen, Flags: descFWrite | descFNext},
		}},
		{name: "partial event before invalid next", descs: []SplitDesc{
			{Addr: 0x100, Len: 4, Flags: descFWrite | descFNext, Next: QueueSize},
		}},
		{name: "complete event before invalid next", descs: []SplitDesc{
			{Addr: 0x100, Len: inputEventLen, Flags: descFWrite | descFNext, Next: QueueSize},
		}},
		{name: "partial event before invalid memory", descs: []SplitDesc{
			{Addr: 0x100, Len: 4, Flags: descFWrite | descFNext, Next: 1},
			{Addr: ^uint64(0), Len: 4, Flags: descFWrite},
		}},
		{name: "indirect descriptor", descs: []SplitDesc{{Addr: 0x100, Len: inputEventLen, Flags: descFWrite | 4}}},
		{name: "buffer extends beyond memory", descs: []SplitDesc{{Addr: 0x100, Len: ^uint32(0), Flags: descFWrite}}},
		{name: "short writable buffer", descs: []SplitDesc{{Addr: 0x100, Len: 4, Flags: descFWrite}}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			mem := make([]byte, 0x1000)
			v := NewInputKeyboard(5, func() error { return nil }, mem)
			q := newInputSplitQueue(cmp.Or(tc.size, QueueSize))
			copy(q.Desc, tc.descs)
			q.Avail.Ring[0] = tc.head
			q.Avail.Idx = 1
			v.QueueReady(inputEventQueue, q)
			v.KeyEvent(true, 'a')
			pending := slices.Clone(v.pending)

			finished := make(chan error, 1)
			go func() { finished <- v.flushEvents() }()
			select {
			case err := <-finished:
				if !errors.Is(err, errNoInputEvent) {
					t.Fatalf("malformed chain returned %v, want no event delivery", err)
				}
			case <-time.After(time.Second):
				t.Fatal("malformed chain blocked event processing")
			}

			if q.Used.Idx != 0 || v.LastAvailIdx[inputEventQueue] != 0 {
				t.Fatal("malformed chain was completed")
			}
			if !slices.Equal(v.pending, pending) {
				t.Fatalf("pending events changed: got %v, want %v", v.pending, pending)
			}
			if !slices.Equal(mem, make([]byte, len(mem))) {
				t.Fatal("malformed chain modified guest memory")
			}

			// Repair the guest buffer and ensure the original key event is retried.
			q.Avail.Ring[0] = 0
			q.Desc[0] = SplitDesc{Addr: 0x100, Len: inputEventLen, Flags: descFWrite}
			if err := v.flushEvents(); err != nil {
				t.Fatalf("flush repaired chain: %v", err)
			}
			assertInputEvent(t, mem[0x100:0x108], evKey, keyA, 1)

			go func() { finished <- v.Close() }()
			select {
			case err := <-finished:
				if err != nil {
					t.Fatalf("close after malformed chain: %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("malformed chain blocked Close")
			}
		})
	}
}

func TestInputEventDescriptorChains(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		size uint16
	}{
		{name: "maximum queue", size: QueueSize},
		{name: "negotiated queue", size: 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := make([]byte, 0x1000)
			v := NewInputKeyboard(5, func() error { return nil }, mem)
			q := newInputSplitQueue(tc.size)
			size := tc.size
			// Walk every descriptor, including zero-length entries, before the
			// two writable segments that hold one complete event.
			for i := range size - 1 {
				q.Desc[i] = SplitDesc{Flags: descFNext, Next: i + 1}
			}
			q.Desc[size-2] = SplitDesc{Addr: 0x100, Len: 3, Flags: descFWrite | descFNext, Next: size - 1}
			q.Desc[size-1] = SplitDesc{Addr: 0x200, Len: 5, Flags: descFWrite}
			q.Avail.Idx = 1
			v.QueueReady(inputEventQueue, q)
			v.KeyEvent(true, 'a')
			if err := v.flushEvents(); err != nil {
				t.Fatalf("flush fragmented event: %v", err)
			}
			raw := append(slices.Clone(mem[0x100:0x103]), mem[0x200:0x205]...)
			assertInputEvent(t, raw, evKey, keyA, 1)
			if q.Used.Idx != 1 || q.Used.Ring[0].ID != 0 || q.Used.Ring[0].Len != inputEventLen {
				t.Fatalf("fragmented event completion: index=%d, entry=%+v", q.Used.Idx, q.Used.Ring[0])
			}
		})
	}
}

func TestInputRejectsQueueOverrun(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		idx  int
	}{
		{name: "events", idx: inputEventQueue},
		{name: "status", idx: inputStatusQueue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := make([]byte, 0x1000)
			v := NewInputKeyboard(5, func() error { return nil }, mem)
			q := newInputSplitQueue(2)
			q.Desc[0] = SplitDesc{Addr: 0x100, Len: inputEventLen, Flags: descFWrite}
			q.Avail.Idx = q.Size + 1
			v.QueueReady(tc.idx, q)
			v.KeyEvent(true, 'a')
			var err error
			if tc.idx == inputEventQueue {
				err = v.flushEvents()
			} else {
				err = v.drainStatusQueue()
			}
			if !errors.Is(err, errNoInputEvent) {
				t.Fatalf("overrun returned %v, want no event delivery", err)
			}
			if q.Used.Idx != 0 || v.LastAvailIdx[tc.idx] != 0 || len(v.pending) != 2 {
				t.Fatal("overrun consumed a buffer or pending event")
			}
		})
	}
}

func TestInputNegotiatedQueueWrap(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		idx  int
	}{
		{name: "events", idx: inputEventQueue},
		{name: "status", idx: inputStatusQueue},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			mem := make([]byte, 0x1000)
			v := NewInputKeyboard(5, func() error { return nil }, mem)
			q := newInputSplitQueue(2)
			v.QueueReady(tc.idx, q)
			for i := range uint16(5) {
				head := i % q.Size
				q.Desc[head] = SplitDesc{Addr: 0x100, Len: inputEventLen, Flags: descFWrite}
				q.Avail.Ring[i%q.Size] = head
				q.Avail.Idx++
				q.Used.Ring[i%q.Size] = SplitUsedElem{ID: ^uint32(0)}
				var err error
				if tc.idx == inputEventQueue {
					v.enqueue(inputEvent{typ: evKey, code: keyA, value: int32(i)})
					err = v.flushEvents()
					assertInputEvent(t, mem[0x100:0x108], evKey, keyA, int32(i))
				} else {
					err = v.drainStatusQueue()
				}
				if err != nil {
					t.Fatalf("process ring entry %d: %v", i, err)
				}
				if q.Used.Idx != i+1 || q.Used.Ring[i%q.Size].ID != uint32(head) {
					t.Fatalf("ring entry %d completed in wrong slot: index=%d, entry=%+v", i, q.Used.Idx, q.Used.Ring[i%q.Size])
				}
			}
		})
	}
}
