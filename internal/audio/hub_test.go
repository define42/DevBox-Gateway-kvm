package audio

import (
	"bytes"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestHubCopiesAndBoundsPlayback(t *testing.T) {
	t.Parallel()
	h := NewHub()
	defer h.Close()
	fast, slow := h.Subscribe(), h.Subscribe()
	defer fast.Close()
	defer slow.Close()
	for i := byte(0); i < 20; i++ {
		data := bytes.Repeat([]byte{i}, PacketBytes)
		h.WritePCM(data)
		clear(data)
		p := <-fast.Packets
		if !bytes.Equal(p.PCM, bytes.Repeat([]byte{i}, PacketBytes)) {
			t.Fatal("guest storage was retained or fast listener lost a packet")
		}
	}
	if len(slow.queue) != queuePackets {
		t.Fatalf("slow listener retained %d packets", len(slow.queue))
	}
	for i := byte(20 - queuePackets); i < 20; i++ {
		if p := <-slow.Packets; p.PCM[0] != i {
			t.Fatalf("slow listener kept stale audio: got %d, want %d", p.PCM[0], i)
		}
	}
}

func TestHubPacketsPreserveSampleOrder(t *testing.T) {
	t.Parallel()
	h := NewHub()
	defer h.Close()
	s := h.Subscribe()
	data := make([]byte, 3*PacketBytes)
	for i := range data {
		data[i] = byte(i)
	}
	h.WritePCM(data)
	var got []byte
	var first uint32
	for i := range 3 {
		p := <-s.Packets
		got = append(got, p.PCM...)
		if i == 0 {
			first = p.Timestamp
		} else if p.Timestamp != first+uint32(i*10) {
			t.Fatalf("sample timestamps: got %d, first %d", p.Timestamp, first)
		}
	}
	if !bytes.Equal(got, data) {
		t.Fatal("packetization changed PCM samples")
	}
	h.WritePCM([]byte{1, 2, 3})
	if len(s.queue) != 0 {
		t.Fatal("accepted an incomplete PCM frame")
	}
}

func TestHubCoalescesGuestPeriodTails(t *testing.T) {
	t.Parallel()
	h := NewHub()
	defer h.Close()
	s := h.Subscribe()
	start := h.started.Add(time.Hour) // Keep the real worker outside the test's clock.
	const period = 2048 * BytesPerFrame
	source := make([]byte, 7*period)
	for i := range source {
		source[i] = byte(i*17 + 1)
	}
	var got []byte
	var last time.Time
	for offset := 0; offset < len(source); {
		// Match virtio-snd's 2048-frame ALSA period: four 1920-byte chunks,
		// then a 512-byte tail. The next period must continue that tail.
		n := min(PacketBytes, period-offset%period)
		last = start.Add(time.Duration(offset) * time.Second / (SampleRate * BytesPerFrame))
		borrowed := append([]byte(nil), source[offset:offset+n]...)
		h.writePCMAt(borrowed, last)
		clear(borrowed)
		offset += n
		got = append(got, drainHubPCM(t, s)...)
	}
	full := len(source) / PacketBytes * PacketBytes
	if len(got) != full || !bytes.Equal(got, source[:full]) {
		t.Fatal("coalescing changed samples or added silence at a period boundary")
	}
	h.flush(last.Add(tailIdle - time.Nanosecond))
	if len(s.queue) != 0 {
		t.Fatal("final tail flushed before its idle deadline")
	}
	h.flush(last.Add(tailIdle))
	got = append(got, drainHubPCM(t, s)...)
	want := append(append([]byte(nil), source...), make([]byte, PacketBytes-len(source)%PacketBytes)...)
	if !bytes.Equal(got, want) {
		t.Fatal("final packet did not preserve source samples followed only by EOF silence")
	}
	h.flush(last.Add(time.Second))
	if len(s.queue) != 0 {
		t.Fatal("idle tail was published more than once")
	}
}

func TestHubPendingTimestampAndIdleBoundary(t *testing.T) {
	t.Parallel()
	h := NewHub()
	defer h.Close()
	s := h.Subscribe()
	start := h.started.Add(time.Hour)
	h.writePCMAt(bytes.Repeat([]byte{1}, 512), start)
	h.writePCMAt(bytes.Repeat([]byte{2}, PacketBytes-512), start.Add(3*time.Millisecond))
	packet := <-s.Packets
	if packet.Timestamp != uint32(time.Hour/time.Millisecond) || len(packet.PCM) != PacketBytes {
		t.Fatal("coalesced packet did not retain its first sample's timestamp")
	}
	h.writePCMAt(bytes.Repeat([]byte{3}, 512), start.Add(10*time.Millisecond))
	h.writePCMAt(nil, start.Add(15*time.Millisecond)) // Empty writes must not postpone EOF.
	h.flush(start.Add(30 * time.Millisecond))
	packet = <-s.Packets
	if packet.Timestamp != uint32(time.Hour/time.Millisecond)+10 ||
		!bytes.Equal(packet.PCM[:512], bytes.Repeat([]byte{3}, 512)) ||
		!bytes.Equal(packet.PCM[512:], make([]byte, PacketBytes-512)) {
		t.Fatal("idle tail lost timestamp, samples, or zero padding")
	}
	// A new write after the deadline must flush the old stream even if the
	// worker has not run yet, instead of joining separated source streams.
	h.writePCMAt(bytes.Repeat([]byte{4}, 512), start.Add(time.Second))
	h.writePCMAt(bytes.Repeat([]byte{5}, PacketBytes), start.Add(time.Second+tailIdle))
	old, fresh := <-s.Packets, <-s.Packets
	if !bytes.Equal(old.PCM[:512], bytes.Repeat([]byte{4}, 512)) ||
		!bytes.Equal(old.PCM[512:], make([]byte, PacketBytes-512)) ||
		!bytes.Equal(fresh.PCM, bytes.Repeat([]byte{5}, PacketBytes)) {
		t.Fatal("new source data was mixed with an expired partial packet")
	}
}

func TestHubDisconnectClearsPartialAndCloseStopsTail(t *testing.T) {
	t.Parallel()
	h := NewHub()
	start := h.started.Add(time.Hour)
	s := h.Subscribe()
	h.writePCMAt(bytes.Repeat([]byte{1}, 512), start)
	s.Close()
	if len(h.pending) != 0 {
		t.Fatal("last listener left pending audio behind")
	}
	h.writePCMAt(bytes.Repeat([]byte{2}, 512), start.Add(time.Millisecond))
	fresh := h.Subscribe()
	h.writePCMAt(bytes.Repeat([]byte{3}, 512), start.Add(2*time.Millisecond))
	h.flush(start.Add(2*time.Millisecond + tailIdle))
	packet := <-fresh.Packets
	if !bytes.Equal(packet.PCM[:512], bytes.Repeat([]byte{3}, 512)) ||
		!bytes.Equal(packet.PCM[512:], make([]byte, PacketBytes-512)) {
		t.Fatal("new listener received stale audio from an earlier connection")
	}
	h.writePCMAt(bytes.Repeat([]byte{4}, 512), start.Add(time.Second))
	h.Close()
	h.Close()
	if h.tail.Stop() {
		t.Fatal("closed hub kept its tail flush scheduled")
	}
	if len(h.pending) != 0 {
		t.Fatal("closed hub retained a partial packet")
	}
	if _, ok := <-fresh.Packets; ok {
		t.Fatal("closed hub published pending samples")
	}
	h.writePCMAt(make([]byte, PacketBytes), start.Add(2*time.Second))
	h.flush(start.Add(3 * time.Second))
	fresh.Close()
}

func TestHubFlushesIdleTailWithoutPolling(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		h := NewHub()
		defer h.Close()
		s := h.Subscribe()
		defer s.Close()
		// Each write postpones the tail flush; ongoing playback is never cut.
		h.WritePCM(bytes.Repeat([]byte{1}, 512))
		time.Sleep(tailIdle / 2)
		h.WritePCM(bytes.Repeat([]byte{2}, 512))
		time.Sleep(tailIdle - time.Nanosecond)
		synctest.Wait()
		if len(s.queue) != 0 {
			t.Fatal("tail flushed before the source was idle")
		}
		time.Sleep(time.Nanosecond)
		synctest.Wait()
		if len(s.queue) != 1 {
			t.Fatalf("idle tail published %d packets, want 1", len(s.queue))
		}
		packet := <-s.Packets
		want := append(bytes.Repeat([]byte{1}, 512), bytes.Repeat([]byte{2}, 512)...)
		if !bytes.Equal(packet.PCM[:1024], want) || !bytes.Equal(packet.PCM[1024:], make([]byte, PacketBytes-1024)) {
			t.Fatal("idle tail lost samples or zero padding")
		}
		time.Sleep(time.Second)
		synctest.Wait()
		if len(s.queue) != 0 {
			t.Fatal("idle hub published again without new samples")
		}
	})
}

func drainHubPCM(t *testing.T, s *Subscription) []byte {
	t.Helper()
	var data []byte
	for {
		select {
		case packet := <-s.Packets:
			if len(packet.PCM) != PacketBytes {
				t.Fatalf("published %d PCM bytes, want fixed 10ms (%d)", len(packet.PCM), PacketBytes)
			}
			data = append(data, packet.PCM...)
		default:
			return data
		}
	}
}

func TestHubConcurrentDisconnect(t *testing.T) {
	t.Parallel()
	h := NewHub()
	var wg sync.WaitGroup
	ready := make(chan struct{}, 8)
	start := make(chan struct{})
	for range 8 {
		wg.Go(func() {
			s := h.Subscribe()
			ready <- struct{}{}
			<-start
			defer s.Close()
			for range 100 {
				s := h.Subscribe()
				h.WritePCM(make([]byte, PacketBytes))
				s.Close()
				s.Close()
			}
		})
	}
	for range 8 {
		<-ready
	}
	close(start)
	h.Close()
	wg.Wait()
	h.Close()
	s := h.Subscribe()
	if _, ok := <-s.Packets; ok {
		t.Fatal("closed hub accepted a new listener")
	}
	s.Close()
}
