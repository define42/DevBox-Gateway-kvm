package virtio_test

import (
	"bytes"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
)

type mockInjector struct {
	called bool
}

func (m *mockInjector) InjectVirtioNetIRQ() error {
	m.called = true

	return nil
}

func (m *mockInjector) InjectVirtioBlkIRQ() error {
	m.called = true

	return nil
}

func (m *mockInjector) InjectVirtioGPUIRQ() error {
	m.called = true

	return nil
}

// newSplitQueue builds a stand-alone split virtqueue for tests. The rings live
// in ordinary Go memory; only descriptor data addresses point into mem.
func newSplitQueue() *virtio.SplitQueue {
	return &virtio.SplitQueue{
		Desc:  &[virtio.QueueSize]virtio.SplitDesc{},
		Avail: &virtio.SplitAvail{},
		Used:  &virtio.SplitUsed{},
	}
}

func TestNetGetDeviceHeader(t *testing.T) {
	t.Parallel()

	v := virtio.NewNet(9, &mockInjector{}, bytes.NewBuffer([]byte{}), []byte{})

	// 0x1041 is the non-transitional (modern) virtio-net device id.
	if id := v.GetDeviceHeader().DeviceID; id != 0x1041 {
		t.Fatalf("DeviceID: expected 0x1041, actual 0x%x", id)
	}

	hdr := v.GetDeviceHeader()
	if hdr.ClassCode != 0x02 || hdr.Subclass != 0x00 {
		t.Fatalf("class: got %#x/%#x, want 0x02/0x00", hdr.ClassCode, hdr.Subclass)
	}

	if hdr.Status&0x10 == 0 {
		t.Fatal("capabilities-list status bit not set")
	}

	if hdr.CapabilitiesPointer != 0x40 {
		t.Fatalf("CapabilitiesPointer: expected 0x40, actual 0x%x", hdr.CapabilitiesPointer)
	}
}

func TestNetMMIOSize(t *testing.T) {
	t.Parallel()

	v := virtio.NewNet(9, &mockInjector{}, bytes.NewBuffer([]byte{}), []byte{})

	if sz := v.MMIOSize(); sz != 0x8000 {
		t.Fatalf("MMIOSize: expected 0x8000, actual 0x%x", sz)
	}

	if idx := v.MMIOBARIndex(); idx != 0 {
		t.Fatalf("MMIOBARIndex: expected 0, actual %d", idx)
	}
}

func TestTx(t *testing.T) {
	t.Parallel()

	expected := []byte{0xaa, 0xbb, 0xcc, 0xdd}
	b := bytes.NewBuffer([]byte{})

	mem := make([]byte, 0x1000000)
	v := virtio.NewNet(9, &mockInjector{}, b, mem)

	// Size of struct virtio_net_hdr_v1.
	const K = 12

	copy(mem[0x100+K:0x100+K+2], []byte{0xaa, 0xbb})
	copy(mem[0x200:0x200+2], []byte{0xcc, 0xdd})

	q := newSplitQueue()
	q.Desc[0].Addr = 0x100
	q.Desc[0].Len = K + 2
	q.Desc[0].Flags = 0x1 // VRING_DESC_F_NEXT
	q.Desc[0].Next = 0x1

	q.Desc[1].Addr = 0x200
	q.Desc[1].Len = 2

	q.Avail.Idx = 1
	v.VirtQueue[1] = q

	if err := v.Tx(); err != nil {
		t.Fatalf("err: %v\n", err)
	}

	if !v.IRQInjector.(*mockInjector).called {
		t.Fatalf("irqInjected = false\n")
	}

	if !bytes.Equal(expected, b.Bytes()) {
		t.Fatalf("expected: %v, actual: %v", expected, b.Bytes())
	}
}

func TestRx(t *testing.T) {
	t.Parallel()

	expected := []byte{0xaa, 0xbb}
	mem := make([]byte, 0x1000000)
	v := virtio.NewNet(9, &mockInjector{}, bytes.NewBuffer(expected), mem)

	q := newSplitQueue()
	q.Avail.Idx = 1
	q.Desc[0].Addr = 0x100
	q.Desc[0].Len = 0x200
	q.Desc[0].Flags = 0x2 // VRING_DESC_F_WRITE
	v.VirtQueue[0] = q

	// Size of struct virtio_net_hdr_v1.
	const K = 12

	if err := v.Rx(); err != nil {
		t.Fatalf("err: %v\n", err)
	}

	if !v.IRQInjector.(*mockInjector).called {
		t.Fatalf("irqInjected = false\n")
	}

	actual := mem[0x100+K : 0x100+K+2]
	if !bytes.Equal(expected, actual) {
		t.Fatalf("expected: %v, actual: %v", expected, actual)
	}

	// num_buffers (header bytes 10:12) must be 1 when MRG_RXBUF is off.
	if mem[0x100+10] != 1 || mem[0x100+11] != 0 {
		t.Fatalf("num_buffers: expected 1, actual %d",
			uint16(mem[0x100+10])|uint16(mem[0x100+11])<<8)
	}
}

func TestNetNotifyTxKick(t *testing.T) {
	t.Parallel()

	tap := &mockTapCloser{}
	mem := make([]byte, 0x10000)
	v := virtio.NewNet(9, &mockInjector{}, tap, mem)

	defer v.Close()

	// Notifying the TX queue twice must never block.
	for i := 0; i < 2; i++ {
		done := make(chan struct{})

		go func() {
			v.Notify(1) // TX
			close(done)
		}()

		select {
		case <-done:
		case <-time.After(1 * time.Second):
			t.Fatalf("Notify(TX) #%d blocked", i)
		}
	}
}

func TestNetNotifyRxDoesNotTransmit(t *testing.T) {
	t.Parallel()

	tap := &mockTapCloser{}
	mem := make([]byte, 0x10000)
	v := virtio.NewNet(9, &mockInjector{}, tap, mem)

	defer v.Close()

	var wg sync.WaitGroup

	wg.Add(1)

	go func() {
		defer wg.Done()
		v.TxThreadEntry()
	}()

	// Notifying the RX queue must never reach the TX path.
	v.Notify(0) // RX

	time.Sleep(50 * time.Millisecond)

	v.Close()
	wg.Wait()

	if tap.Len() != 0 {
		t.Fatalf("tap had %d bytes; want 0", tap.Len())
	}
}

// mockTapCloser implements io.ReadWriteCloser for testing Net.Close().
type mockTapCloser struct {
	bytes.Buffer
	closed bool
	closes int
}

func (m *mockTapCloser) Close() error {
	m.closed = true
	m.closes++

	return nil
}

func TestNetClose(t *testing.T) {
	t.Parallel()

	tap := &mockTapCloser{}
	v := virtio.NewNet(9, &mockInjector{}, tap, []byte{})

	if err := v.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}

	if !tap.closed {
		t.Fatal("tap was not closed")
	}
	if err := v.Close(); err != nil {
		t.Fatal(err)
	}
	if tap.closes != 1 {
		t.Fatalf("backend closed %d times; want once", tap.closes)
	}
}

func TestNetCloseNonCloser(t *testing.T) {
	t.Parallel()

	// Use a plain io.ReadWriter (no Close method).
	var buf bytes.Buffer
	v := virtio.NewNet(9, &mockInjector{}, io.ReadWriter(&buf), []byte{})

	if err := v.Close(); err != nil {
		t.Fatalf("Close: got %v, want nil", err)
	}
}

func TestNetThreadsExitOnClose(t *testing.T) {
	t.Parallel()

	tap := &mockTapCloser{}
	mem := make([]byte, 0x10000)
	v := virtio.NewNet(9, &mockInjector{}, tap, mem)

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		v.TxThreadEntry()
	}()

	go func() {
		defer wg.Done()
		v.RxThreadEntry()
	}()

	if err := v.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("thread entries did not exit after Close")
	}
}

func TestNetNotifyAfterClose(t *testing.T) {
	t.Parallel()

	tap := &mockTapCloser{}
	mem := make([]byte, 0x10000)
	v := virtio.NewNet(9, &mockInjector{}, tap, mem)

	if err := v.Close(); err != nil {
		t.Fatal(err)
	}

	// TX kick after Close must not panic.
	v.Notify(1)
}

func TestNetConcurrentCloseAndNotify(t *testing.T) {
	t.Parallel()

	tap := &mockTapCloser{}
	mem := make([]byte, 0x10000)
	v := virtio.NewNet(9, &mockInjector{}, tap, mem)

	var wg sync.WaitGroup

	wg.Add(2)

	go func() {
		defer wg.Done()
		v.Close()
	}()

	go func() {
		defer wg.Done()

		for i := 0; i < 100; i++ {
			v.Notify(1)
		}
	}()

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("concurrent Close+Notify deadlocked")
	}
}

// packetBackend models an Ethernet backend whose reads never block and whose
// readiness notification may be coalesced for multiple queued packets.
type packetBackend struct {
	packets chan []byte
	ready   chan struct{}
	reads   atomic.Int32
	closes  atomic.Int32
}

func newPacketBackend() *packetBackend {
	return &packetBackend{packets: make(chan []byte, 4), ready: make(chan struct{}, 1)}
}

func (b *packetBackend) Ready() <-chan struct{} { return b.ready }

func (b *packetBackend) Read(p []byte) (int, error) {
	b.reads.Add(1)
	select {
	case packet := <-b.packets:
		return copy(p, packet), nil
	default:
		return 0, syscall.EAGAIN
	}
}

func (b *packetBackend) Write(p []byte) (int, error) { return len(p), nil }

func (b *packetBackend) Close() error {
	b.closes.Add(1)

	return nil
}

type netIRQNotifier struct {
	mockInjector
	interrupts chan struct{}
}

func (n *netIRQNotifier) InjectVirtioNetIRQ() error {
	n.interrupts <- struct{}{}

	return nil
}

func awaitNetEvent(t *testing.T, event <-chan struct{}) {
	t.Helper()
	select {
	case <-event:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for network worker")
	}
}

func startNetRX(t *testing.T, v *virtio.Net) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		defer close(done)
		v.RxThreadEntry()
	}()
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Error(err)
		}
		awaitNetEvent(t, done)
	})
}

func TestNetBackendReadyDrainsPackets(t *testing.T) {
	t.Parallel()
	b := newPacketBackend()
	irq := &netIRQNotifier{interrupts: make(chan struct{}, 4)}
	mem := make([]byte, 4096)
	v := virtio.NewNet(9, irq, b, mem)
	q := newSplitQueue()
	q.Desc[0] = virtio.SplitDesc{Addr: 0x100, Len: 0x100, Flags: 2}
	q.Desc[1] = virtio.SplitDesc{Addr: 0x200, Len: 0x100, Flags: 2}
	q.Avail.Ring[1] = 1
	q.Avail.Idx = 2
	v.VirtQueue[0] = q
	startNetRX(t, v)

	b.packets <- []byte{0xaa}
	b.packets <- []byte{0xbb}
	b.ready <- struct{}{} // one readiness event must drain both packets
	awaitNetEvent(t, irq.interrupts)
	awaitNetEvent(t, irq.interrupts)

	if mem[0x100+12] != 0xaa || mem[0x200+12] != 0xbb {
		t.Fatalf("received payloads %#x/%#x; want 0xaa/0xbb", mem[0x100+12], mem[0x200+12])
	}
	if got := b.reads.Load(); got != 2 {
		t.Fatalf("read backend %d times; want 2 (no read without a guest buffer)", got)
	}
}

func TestNetRXBuffersPreservePendingPacket(t *testing.T) {
	t.Parallel()
	b := newPacketBackend()
	irq := &netIRQNotifier{interrupts: make(chan struct{}, 4)}
	mem := make([]byte, 4096)
	v := virtio.NewNet(9, irq, b, mem)
	t.Cleanup(func() { _ = v.Close() })
	b.packets <- []byte{0xaa, 0xbb}
	if err := v.Rx(); !errors.Is(err, virtio.ErrVQNotInit) {
		t.Fatalf("Rx before queue setup: %v", err)
	}
	q := newSplitQueue()
	v.VirtQueue[0] = q
	if err := v.Rx(); !errors.Is(err, virtio.ErrNoRxBuf) {
		t.Fatalf("Rx without guest buffers: %v", err)
	}
	if got := b.reads.Load(); got != 0 {
		t.Fatalf("read backend %d times without a guest buffer", got)
	}

	q.Desc[0] = virtio.SplitDesc{Addr: 0x100, Len: 0x100, Flags: 2}
	q.Avail.Idx = 1
	startNetRX(t, v)
	v.Notify(0) // the backend sends no new readiness event for this packet
	awaitNetEvent(t, irq.interrupts)
	if !bytes.Equal(mem[0x100+12:0x100+14], []byte{0xaa, 0xbb}) {
		t.Fatal("RX queue notification did not deliver the pending packet")
	}
}

func TestNetRXDescriptorChain(t *testing.T) {
	t.Parallel()
	payload := []byte{0xaa, 0xbb, 0xcc, 0xdd}
	mem := make([]byte, 4096)
	v := virtio.NewNet(9, &mockInjector{}, bytes.NewBuffer(payload), mem)
	t.Cleanup(func() { _ = v.Close() })
	q := newSplitQueue()
	q.Desc[3] = virtio.SplitDesc{Addr: 0x100, Len: 13, Flags: 3, Next: 7}
	q.Desc[7] = virtio.SplitDesc{Addr: 0x200, Len: 100, Flags: 2}
	q.Avail.Ring[0] = 3
	q.Avail.Idx = 1
	v.VirtQueue[0] = q
	original := *q.Desc
	if err := v.Rx(); err != nil {
		t.Fatal(err)
	}
	got := append([]byte{mem[0x100+12]}, mem[0x200:0x203]...)
	if !bytes.Equal(got, payload) {
		t.Fatalf("descriptor chain payload: %x; want %x", got, payload)
	}
	if *q.Desc != original {
		t.Fatal("RX changed guest descriptor metadata")
	}
	if q.Used.Idx != 1 || q.Used.Ring[0].ID != 3 || q.Used.Ring[0].Len != 16 || v.LastAvailIdx[0] != 1 {
		t.Fatalf("incorrect completion: used=%+v, consumed=%d", q.Used.Ring[0], v.LastAvailIdx[0])
	}
}

func TestNetRXShortBufferDoesNotMergePackets(t *testing.T) {
	t.Parallel()
	mem := bytes.Repeat([]byte{0xff}, 4096)
	v := virtio.NewNet(9, &mockInjector{}, bytes.NewBuffer([]byte{0xaa, 0xbb}), mem)
	t.Cleanup(func() { _ = v.Close() })
	q := newSplitQueue()
	q.Desc[0] = virtio.SplitDesc{Addr: 0x100, Len: 13, Flags: 2}
	q.Desc[1] = virtio.SplitDesc{Addr: 0x200, Len: 100, Flags: 2}
	q.Avail.Ring[1] = 1
	q.Avail.Idx = 2
	v.VirtQueue[0] = q
	if err := v.Rx(); err != nil {
		t.Fatal(err)
	}
	if v.LastAvailIdx[0] != 1 || q.Used.Idx != 1 || q.Used.Ring[0].Len != 0 {
		t.Fatalf("short chain consumed another buffer: consumed=%d, used=%+v", v.LastAvailIdx[0], q.Used.Ring[0])
	}
	if mem[0x200] != 0xff {
		t.Fatal("RX wrote an unrelated available buffer")
	}
}

func TestNetRXRejectsInvalidDescriptors(t *testing.T) {
	t.Parallel()
	for name, desc := range map[string]virtio.SplitDesc{
		"read only":    {Addr: 0x100, Len: 100},
		"outside RAM":  {Addr: 0xfff, Len: 100, Flags: 2},
		"address wrap": {Addr: ^uint64(0), Len: 100, Flags: 2},
		"loop":         {Addr: 0x100, Len: 100, Flags: 3, Next: 0},
		"bad next":     {Addr: 0x100, Len: 100, Flags: 3, Next: virtio.QueueSize},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			b := newPacketBackend()
			v := virtio.NewNet(9, &mockInjector{}, b, make([]byte, 4096))
			t.Cleanup(func() { _ = v.Close() })
			q := newSplitQueue()
			q.Desc[0] = desc
			q.Avail.Idx = 1
			v.VirtQueue[0] = q
			if err := v.Rx(); !errors.Is(err, virtio.ErrNetDesc) {
				t.Fatalf("Rx: %v; want invalid descriptor error", err)
			}
			if b.reads.Load() != 0 || q.Used.Idx != 0 {
				t.Fatal("invalid descriptor consumed a packet")
			}
		})
	}
}

func TestNetTxInvalidChains(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		setup func(*virtio.SplitQueue)
	}{
		{name: "short_header", setup: func(q *virtio.SplitQueue) { q.Desc[0].Len = 11 }},
		{name: "cycle", setup: func(q *virtio.SplitQueue) { q.Desc[0].Flags, q.Desc[0].Next = 1, 0 }},
		{
			name:  "descriptor_out_of_range",
			setup: func(q *virtio.SplitQueue) { q.Desc[0].Flags, q.Desc[0].Next = 1, virtio.QueueSize },
		},
		{name: "memory_out_of_range", setup: func(q *virtio.SplitQueue) { q.Desc[0].Addr = 4096 }},
		{name: "writable_payload", setup: func(q *virtio.SplitQueue) { q.Desc[0].Flags = 2 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var backend bytes.Buffer
			v := virtio.NewNet(9, &mockInjector{}, &backend, make([]byte, 4096))
			t.Cleanup(func() { _ = v.Close() })
			q := newSplitQueue()
			q.Desc[0].Len = 128
			q.Avail.Idx = 1
			tc.setup(q)
			v.VirtQueue[1] = q
			if err := v.Tx(); !errors.Is(err, virtio.ErrNetDesc) {
				t.Fatalf("Tx = %v, want ErrNetDesc", err)
			}
			if backend.Len() != 0 || q.Used.Idx != 0 {
				t.Fatal("invalid packet was sent or completed")
			}
		})
	}
}

type shortNetWriter struct{}

func (shortNetWriter) Read([]byte) (int, error)    { return 0, io.EOF }
func (shortNetWriter) Write(p []byte) (int, error) { return len(p) - 1, nil }

func TestNetTxShortWrite(t *testing.T) {
	t.Parallel()

	v := virtio.NewNet(9, &mockInjector{}, shortNetWriter{}, make([]byte, 4096))
	t.Cleanup(func() { _ = v.Close() })
	q := newSplitQueue()
	q.Desc[0].Len, q.Avail.Idx = 128, 1
	v.VirtQueue[1] = q
	if err := v.Tx(); !errors.Is(err, io.ErrShortWrite) {
		t.Fatalf("Tx = %v, want ErrShortWrite", err)
	}
	if q.Used.Idx != 0 {
		t.Fatal("incomplete packet was completed")
	}
}

func TestNetResetDiscardsQueues(t *testing.T) {
	t.Parallel()

	v := virtio.NewNet(9, &mockInjector{}, &bytes.Buffer{}, make([]byte, 4096))
	t.Cleanup(func() { _ = v.Close() })
	for i := range 2 {
		v.QueueReady(i, newSplitQueue())
		v.LastAvailIdx[i] = 7
	}
	v.Reset()
	if err := v.Tx(); !errors.Is(err, virtio.ErrVQNotInit) {
		t.Fatalf("Tx after reset: %v", err)
	}
	if err := v.Rx(); !errors.Is(err, virtio.ErrVQNotInit) {
		t.Fatalf("Rx after reset: %v", err)
	}
	if v.LastAvailIdx != [2]uint16{} {
		t.Fatal("reset retained consumed queue indices")
	}
}

func TestNetRxNegotiatedQueueWrap(t *testing.T) {
	t.Parallel()

	backend := newPacketBackend()
	backend.packets <- []byte{0xaa, 0xbb, 0xcc}
	mem := make([]byte, 4096)
	v := virtio.NewNet(9, &mockInjector{}, backend, mem)
	t.Cleanup(func() { _ = v.Close() })
	q := newSplitQueue()
	q.Size = 4
	q.Desc[3] = virtio.SplitDesc{Addr: 256, Len: 64, Flags: 2}
	q.Avail.Ring[0] = 3
	q.Avail.Idx, q.Used.Idx = 5, 4
	v.VirtQueue[0], v.LastAvailIdx[0] = q, 4
	if err := v.Rx(); err != nil {
		t.Fatal(err)
	}
	if q.Used.Idx != 5 || q.Used.Ring[0].ID != 3 || q.Used.Ring[0].Len != 15 {
		t.Fatalf("wrapped completion = %+v, idx=%d", q.Used.Ring[0], q.Used.Idx)
	}
	if !bytes.Equal(mem[256+12:256+15], []byte{0xaa, 0xbb, 0xcc}) {
		t.Fatal("wrapped packet copied to wrong buffer")
	}
	if q.Used.Ring[4] != (virtio.SplitUsedElem{}) {
		t.Fatal("completion written outside negotiated ring")
	}
}

func TestNetRxNegotiatedQueueBounds(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		setup func(*virtio.SplitQueue)
	}{
		{name: "head_out_of_range", setup: func(q *virtio.SplitQueue) { q.Avail.Ring[0] = 4 }},
		{name: "next_out_of_range", setup: func(q *virtio.SplitQueue) { q.Desc[0].Flags, q.Desc[0].Next = 3, 4 }},
		{name: "available_overrun", setup: func(q *virtio.SplitQueue) { q.Avail.Idx = 5 }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			backend := newPacketBackend()
			v := virtio.NewNet(9, &mockInjector{}, backend, make([]byte, 4096))
			t.Cleanup(func() { _ = v.Close() })
			q := newSplitQueue()
			q.Size, q.Avail.Idx = 4, 1
			q.Desc[0] = virtio.SplitDesc{Len: 64, Flags: 2}
			q.Desc[4] = virtio.SplitDesc{Addr: 256, Len: 64, Flags: 2}
			tc.setup(q)
			v.VirtQueue[0] = q
			if err := v.Rx(); !errors.Is(err, virtio.ErrNetDesc) {
				t.Fatalf("Rx = %v, want ErrNetDesc", err)
			}
			if backend.reads.Load() != 0 || q.Used.Idx != 0 {
				t.Fatal("invalid queue consumed a packet or published a completion")
			}
		})
	}
}
