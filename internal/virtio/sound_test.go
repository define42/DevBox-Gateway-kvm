package virtio

import (
	"bytes"
	"encoding/binary"
	"sync"
	"testing"
	"time"
)

type soundTestSink struct {
	data  []byte
	sizes []int
}

func (s *soundTestSink) WritePCM(pcm []byte) {
	s.data = append(s.data, pcm...)
	s.sizes = append(s.sizes, len(pcm))
}

func newTestSound(t *testing.T) (*Sound, *soundTestSink) {
	t.Helper()
	sink := &soundTestSink{}
	s := NewSound(7, func() error { return nil }, make([]byte, 1024*1024), sink)
	t.Cleanup(func() { _ = s.Close() })

	return s, sink
}

func soundTestQueue() *SplitQueue {
	return &SplitQueue{Desc: new([QueueSize]SplitDesc), Avail: new(SplitAvail), Used: new(SplitUsed)}
}

func soundRequest(code uint32, values ...uint32) []byte {
	data := make([]byte, 4*(1+len(values)))
	binary.LittleEndian.PutUint32(data, code)
	for i, value := range values {
		binary.LittleEndian.PutUint32(data[4*(i+1):], value)
	}

	return data
}

func soundParams(period, buffer uint32) []byte {
	request := soundRequest(soundSetParams, 0, buffer, period, 0, 0)
	request[20], request[21], request[22] = 2, soundFormat16, soundRate48K

	return request
}

func soundConfigure(t *testing.T, s *Sound, period, buffer uint32) {
	t.Helper()
	for _, request := range [][]byte{soundParams(period, buffer), soundRequest(soundPrepare, 0)} {
		if got := binary.LittleEndian.Uint32(s.control(request, 4)); got != soundOK {
			t.Fatalf("control %x: status %x", request, got)
		}
	}
}

// publish creates a fragmented readable/writable chain with private storage.
func soundPublish(s *Sound, q *SplitQueue, head uint16, address uint64, request []byte, responseLen int) []byte {
	copy(s.Mem[address:], request)
	responseAddress := address + uint64(len(request)) + 16
	q.Desc[head] = SplitDesc{Addr: address, Len: uint32(len(request)), Flags: descFNext, Next: head + 1}
	q.Desc[head+1] = SplitDesc{Addr: responseAddress, Len: uint32(responseLen), Flags: descFWrite}
	q.Avail.Ring[q.Avail.Idx%soundQueueSize(q)] = head
	q.Avail.Idx++

	return s.Mem[responseAddress : responseAddress+uint64(responseLen)]
}

func TestSoundConfigurationAndQueries(t *testing.T) {
	t.Parallel()
	s, _ := newTestSound(t)
	var cfg [12]byte
	s.ReadDeviceConfig(0, cfg[:])
	if binary.LittleEndian.Uint32(cfg[:]) != 0 || binary.LittleEndian.Uint32(cfg[4:]) != 1 ||
		binary.LittleEndian.Uint32(cfg[8:]) != 1 {
		t.Fatalf("invalid device configuration: %x", cfg)
	}
	header := s.GetDeviceHeader()
	if header.DeviceID != 0x1059 || header.BAR[0] != SoundMMIOBase || s.NumQueues() != 4 {
		t.Fatalf("invalid PCI sound device: %+v", header)
	}
	pcm := s.control(soundRequest(soundPCMInfo, 0, 1, 32), 36)
	if len(pcm) != 36 || binary.LittleEndian.Uint32(pcm) != soundOK || binary.LittleEndian.Uint32(pcm[4:]) != 0 ||
		binary.LittleEndian.Uint64(pcm[12:]) != 1<<soundFormat16 ||
		binary.LittleEndian.Uint64(pcm[20:]) != 1<<soundRate48K ||
		!bytes.Equal(pcm[28:31], []byte{0, 2, 2}) {
		t.Fatalf("invalid PCM info: %x", pcm)
	}
	chmap := s.control(soundRequest(soundChmapInfo, 0, 1, 24), 28)
	if len(chmap) != 28 || binary.LittleEndian.Uint32(chmap[4:]) != 0 ||
		!bytes.Equal(chmap[8:12], []byte{0, 2, 3, 4}) {
		t.Fatalf("invalid channel map: %x", chmap)
	}
	for _, request := range [][]byte{
		soundRequest(soundPCMInfo, 1, 1, 32),
		soundRequest(soundPCMInfo, 0xffffffff, 2, 32),
		soundRequest(soundPCMInfo, 0, 1, 31),
		soundRequest(soundJackInfo, 0, 1, 24),
	} {
		if got := binary.LittleEndian.Uint32(s.control(request, 36)); got != soundBadMsg {
			t.Fatalf("invalid query %x accepted: %x", request, got)
		}
	}
	if got := binary.LittleEndian.Uint32(s.control(soundRequest(soundPCMInfo, 0, 1, 32), 35)); got != soundBadMsg {
		t.Fatalf("undersized response accepted: %x", got)
	}
}

func TestSoundControlAndParameterValidation(t *testing.T) {
	t.Parallel()
	s, _ := newTestSound(t)
	if got := s.control(soundRequest(soundStart, 0), 4); binary.LittleEndian.Uint32(got) != soundBadMsg {
		t.Fatal("start accepted before preparation")
	}
	for _, values := range [][2]uint32{
		{0, 3840},
		{1921, 3842},
		{1920, 1921},
		{soundMaxPeriod + 4, soundMaxBuffer},
		{1920, soundMaxBuffer + 4},
	} {
		if got := s.control(soundParams(values[0], values[1]), 4); binary.LittleEndian.Uint32(got) != soundBadMsg {
			t.Fatalf("invalid period/buffer %v accepted", values)
		}
	}
	for _, offset := range []int{16, 20, 21, 22} {
		request := soundParams(1920, 3840)
		request[offset] = 255
		if got := s.control(request, 4); binary.LittleEndian.Uint32(got) != soundNotSupp {
			t.Fatalf("unsupported parameter at %d accepted", offset)
		}
	}
	soundConfigure(t, s, 1920, 3840)
	q := soundTestQueue()
	s.QueueReady(soundControlQueue, q)
	response := soundPublish(s, q, 0, 4096, soundRequest(soundStart, 0), 4)
	s.process(time.Now())
	if q.Used.Idx != 1 || q.Used.Ring[0].Len != 4 ||
		binary.LittleEndian.Uint32(response) != soundOK || s.state != soundRunning {
		t.Fatalf("START response=%x used=%+v state=%v", response, q.Used, s.state)
	}
}

func TestSoundPlaybackClockAndOwnership(t *testing.T) {
	t.Parallel()
	s, sink := newTestSound(t)
	soundConfigure(t, s, 3840, 7680) // Two 20 ms periods.
	q := soundTestQueue()
	s.QueueReady(soundTXQueue, q)
	pcm := bytes.Repeat([]byte{0x01, 0x02, 0x03, 0x04}, 960)
	request := append(soundRequest(0), pcm...)
	response := soundPublish(s, q, 0, 4096, request, 8)
	start := time.Unix(100, 0)
	s.process(start) // Prepared prebuffering must neither play nor complete.
	if q.Used.Idx != 0 || len(sink.data) != 0 {
		t.Fatal("played prebuffer before START")
	}
	s.control(soundRequest(soundStart, 0), 4)
	s.process(start)
	// A consumed chunk is independent of guest RAM, while future chunks
	// remain live DMA data and may still be filled by guest userspace.
	clear(s.Mem[4100 : 4100+len(pcm)])
	clear(pcm[1920:])
	s.process(start.Add(9 * time.Millisecond))
	if q.Used.Idx != 0 || len(sink.data) != 1920 {
		t.Fatalf("period completed early: used=%d bytes=%d", q.Used.Idx, len(sink.data))
	}
	s.process(start.Add(10 * time.Millisecond))
	s.process(start.Add(19 * time.Millisecond))
	if q.Used.Idx != 0 || !bytes.Equal(sink.data, pcm) {
		t.Fatalf("PCM not preserved or completed early: used=%d bytes=%d", q.Used.Idx, len(sink.data))
	}
	s.process(start.Add(20 * time.Millisecond))
	if q.Used.Idx != 1 || q.Used.Ring[0].Len != 8 || binary.LittleEndian.Uint32(response) != soundOK {
		t.Fatalf("period did not complete: used=%+v response=%x", q.Used, response)
	}
	if !bytes.Equal(sink.data, pcm) || len(sink.sizes) != 2 || sink.sizes[0] != 1920 || sink.sizes[1] != 1920 {
		t.Fatalf("unexpected sink chunks: %v", sink.sizes)
	}
}

func TestSoundPauseReleaseAndReconnect(t *testing.T) {
	t.Parallel()
	s, sink := newTestSound(t)
	soundConfigure(t, s, 3840, 7680)
	q := soundTestQueue()
	control := soundTestQueue()
	s.QueueReady(soundTXQueue, q)
	s.QueueReady(soundControlQueue, control)
	pcm := bytes.Repeat([]byte{1, 2, 3, 4}, 960)
	request := append(soundRequest(0), pcm...)
	soundPublish(s, q, 0, 4096, request, 8)
	start := time.Unix(100, 0)
	s.control(soundRequest(soundStart, 0), 4)
	s.process(start)
	s.control(soundRequest(soundStop, 0), 4)
	s.process(start.Add(time.Second))
	if q.Used.Idx != 0 || len(sink.data) != 1920 {
		t.Fatal("paused stream kept playing")
	}
	// RELEASE must drain both accepted and not-yet-accepted requests before
	// reporting completion on controlq.
	soundPublish(s, q, 2, 16384, request, 8)
	response := soundPublish(s, control, 0, 32768, soundRequest(soundRelease, 0), 4)
	s.process(start.Add(2 * time.Second))
	if q.Used.Idx != 2 || control.Used.Idx != 1 || binary.LittleEndian.Uint32(response) != soundOK || len(s.pending) != 0 {
		t.Fatalf("RELEASE did not drain TX: tx=%d control=%d response=%x", q.Used.Idx, control.Used.Idx, response)
	}

	// Missing clients are a silent sink; the guest clock continues and a
	// subsequent client can receive fresh samples without reopening ALSA.
	s.control(soundRequest(soundPrepare, 0), 4)
	s.soundTestDisconnect(t, q, request, start.Add(3*time.Second))
}

func (s *Sound) soundTestDisconnect(t *testing.T, q *SplitQueue, request []byte, now time.Time) {
	t.Helper()
	s.sink = nil
	soundPublish(s, q, 4, 49152, request, 8)
	s.control(soundRequest(soundStart, 0), 4)
	s.process(now)
	s.process(now.Add(20 * time.Millisecond))
	if q.Used.Idx != 3 {
		t.Fatalf("disconnected client stalled the guest: used=%d", q.Used.Idx)
	}
	sink := &soundTestSink{}
	s.sink = sink
	soundPublish(s, q, 6, 65536, request, 8)
	s.process(now.Add(30 * time.Millisecond))
	s.process(now.Add(50 * time.Millisecond))
	if q.Used.Idx != 4 || !bytes.Equal(sink.data, request[4:]) {
		t.Fatal("reconnected client did not receive fresh audio")
	}
}

func TestSoundMalformedDescriptors(t *testing.T) {
	t.Parallel()
	for _, name := range []string{
		"cycle", "address", "overflow", "next", "indirect", "too_large", "write_then_read", "small_status", "bad_stream",
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			s, sink := newTestSound(t)
			soundConfigure(t, s, 1920, 3840)
			q := soundTestQueue()
			s.QueueReady(soundTXQueue, q)
			response := soundPublish(s, q, 0, 4096, append(soundRequest(0), make([]byte, 1920)...), 8)
			switch name {
			case "cycle":
				q.Desc[1].Flags |= descFNext
				q.Desc[1].Next = 0
			case "address":
				q.Desc[0].Addr = uint64(len(s.Mem))
			case "overflow":
				q.Desc[0].Addr = ^uint64(0) - 1
			case "next":
				q.Desc[0].Next = QueueSize
			case "indirect":
				q.Desc[0].Flags |= 4
			case "too_large":
				q.Desc[0].Len = soundMaxPeriod + 8
			case "write_then_read":
				q.Desc[0].Flags |= descFWrite
				q.Desc[1].Flags = 0
			case "small_status":
				q.Desc[1].Len = 4
			case "bad_stream":
				binary.LittleEndian.PutUint32(s.Mem[4096:], 1)
			}
			s.process(time.Now())
			if q.Used.Idx != 1 || len(sink.data) != 0 || len(s.pending) != 0 {
				t.Fatalf("malformed chain stalled or played: used=%d", q.Used.Idx)
			}
			if name == "bad_stream" && binary.LittleEndian.Uint32(response) != soundIOErr {
				t.Fatalf("bad stream response=%x", response)
			}
		})
	}
}

func TestSoundResetAndCloseConcurrent(t *testing.T) {
	t.Parallel()
	s, _ := newTestSound(t)
	soundConfigure(t, s, 1920, 3840)
	q := soundTestQueue()
	s.QueueReady(soundTXQueue, q)
	soundPublish(s, q, 0, 4096, append(soundRequest(0), make([]byte, 1920)...), 8)
	s.process(time.Now())
	s.Reset()
	if len(s.pending) != 0 || s.state != soundUnconfigured || s.queues[soundTXQueue] != nil {
		t.Fatal("reset retained guest memory or playback state")
	}
	s.process(time.Now().Add(time.Second))
	if q.Used.Idx != 0 {
		t.Fatal("reset touched abandoned descriptors")
	}

	finished := make(chan struct{})
	go func() {
		s.IOThreadEntry()
		close(finished)
	}()
	var workers sync.WaitGroup
	for range 3 {
		workers.Go(func() {
			for range 100 {
				s.Reset()
				s.QueueReady(soundControlQueue, soundTestQueue())
				s.Notify(soundTXQueue)
			}
		})
	}
	workers.Wait()
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-finished:
	case <-time.After(time.Second):
		t.Fatal("IO thread did not stop")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestSoundSmallQueueAndWrap(t *testing.T) {
	t.Parallel()
	s, _ := newTestSound(t)
	q := soundTestQueue()
	q.Size = 4
	q.Avail.Idx, q.Used.Idx = 0xffff, 0xffff
	s.QueueReady(soundControlQueue, q)
	s.lastAvail[soundControlQueue] = 0xffff
	soundPublish(s, q, 0, 4096, soundRequest(soundPCMInfo, 0, 1, 32), 36)
	s.process(time.Now())
	if q.Used.Idx != 0 || q.Used.Ring[3].Len != 36 || s.lastAvail[soundControlQueue] != 0 {
		t.Fatalf("small ring wrap failed: %+v", q.Used)
	}
	q.Avail.Idx = 9 // Invalid producer overrun is bounded and ignored.
	s.process(time.Now())
	if q.Used.Idx != 0 {
		t.Fatal("processed an overrun ring")
	}
}

func TestSoundPauseResumeAndHostStall(t *testing.T) {
	t.Parallel()
	s, sink := newTestSound(t)
	soundConfigure(t, s, 7680, 15360)
	q := soundTestQueue()
	s.QueueReady(soundTXQueue, q)
	pcm := bytes.Repeat([]byte{1, 2, 3, 4}, 1920)
	soundPublish(s, q, 0, 4096, append(soundRequest(0), pcm...), 8)
	now := time.Unix(100, 0)
	s.control(soundRequest(soundStart, 0), 4)
	s.process(now)
	s.control(soundRequest(soundStop, 0), 4)
	s.process(now.Add(time.Second))
	s.control(soundRequest(soundStart, 0), 4)
	s.process(now.Add(time.Second))
	if q.Used.Idx != 0 || len(sink.data) != 3840 {
		t.Fatal("resume did not retain the paused period")
	}
	// A long host stall must not cause a burst of the remaining audio.
	s.process(now.Add(2 * time.Second))
	if q.Used.Idx != 0 || len(sink.data) != 5760 {
		t.Fatal("host stall drained playback ahead of its sample clock")
	}
	s.process(now.Add(2*time.Second + 10*time.Millisecond))
	s.process(now.Add(2*time.Second + 20*time.Millisecond))
	if q.Used.Idx != 1 || !bytes.Equal(sink.data, pcm) {
		t.Fatal("resume or stall recovery corrupted audio")
	}
}

func TestSoundCyclicDMARequeueBeforeRefill(t *testing.T) {
	t.Parallel()
	s, sink := newTestSound(t)
	soundConfigure(t, s, 3840, 7680)
	q := soundTestQueue()
	s.QueueReady(soundTXQueue, q)
	first := bytes.Repeat([]byte{1, 2, 3, 4}, 960)
	second := bytes.Repeat([]byte{5, 6, 7, 8}, 960)
	third := bytes.Repeat([]byte{9, 10, 11, 12}, 960)
	soundPublish(s, q, 0, 4096, append(soundRequest(0), first...), 8)
	soundPublish(s, q, 2, 16384, append(soundRequest(0), second...), 8)
	s.control(soundRequest(soundStart, 0), 4)
	now := time.Unix(100, 0)
	for ms := 0; ms <= 20; ms += 10 {
		s.process(now.Add(time.Duration(ms) * time.Millisecond))
	}
	if q.Used.Idx != 1 {
		t.Fatal("first DMA period was not returned to the guest")
	}
	// Linux's virtio-snd interrupt callback requeues the completed period
	// before scheduling snd_pcm_period_elapsed. ALSA userspace fills it only
	// afterward; eagerly copying on queue submission captures stale samples.
	q.Avail.Ring[q.Avail.Idx%QueueSize] = 0
	q.Avail.Idx++
	s.process(now.Add(21 * time.Millisecond))
	copy(s.Mem[4100:], third)
	for ms := 30; ms <= 60; ms += 10 {
		s.process(now.Add(time.Duration(ms) * time.Millisecond))
	}
	want := bytes.Join([][]byte{first, second, third}, nil)
	if q.Used.Idx != 3 || !bytes.Equal(sink.data, want) {
		t.Fatalf("cyclic DMA used stale audio: completed=%d bytes=%d", q.Used.Idx, len(sink.data))
	}
}

func TestSoundFragmentedPCMReadAtPlayback(t *testing.T) {
	t.Parallel()
	s, sink := newTestSound(t)
	soundConfigure(t, s, 1920, 3840)
	q := soundTestQueue()
	s.QueueReady(soundTXQueue, q)
	// Deliberately split both stream_id and a sample frame across descriptors.
	pcm := bytes.Repeat([]byte{1, 2, 3, 4}, 480)
	request := append(soundRequest(0), pcm...)
	lengths := []int{2, 101, 500, len(request) - 603}
	offset := 0
	for i, length := range lengths {
		address := uint64(4096 * (i + 1))
		copy(s.Mem[address:], request[offset:offset+length])
		q.Desc[i] = SplitDesc{Addr: address, Len: uint32(length), Flags: descFNext, Next: uint16(i + 1)}
		offset += length
	}
	q.Desc[4] = SplitDesc{Addr: 32768, Len: 8, Flags: descFWrite}
	q.Avail.Idx = 1
	s.control(soundRequest(soundStart, 0), 4)
	now := time.Unix(100, 0)
	s.process(now)
	s.process(now.Add(10 * time.Millisecond))
	if q.Used.Idx != 1 || !bytes.Equal(sink.data, pcm) {
		t.Fatal("fragmented DMA playback corrupted PCM")
	}
}
