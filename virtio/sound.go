package virtio

import (
	"encoding/binary"
	"log"
	"sync"
	"time"

	"github.com/define42/gokvm/internal/guestmem"
	"github.com/define42/gokvm/pci"
)

// Sound implements the message-based playback subset of virtio 1.2 section
// 5.14. Samples are interleaved stereo S16_LE at 48 kHz. No capture streams,
// physical jacks, shared-memory transport or asynchronous events are advertised.
// https://docs.oasis-open.org/virtio/virtio/v1.2/virtio-v1.2.html
const (
	SoundMMIOBase = 0xd005_0000

	soundControlQueue = 0
	soundEventQueue   = 1
	soundTXQueue      = 2
	soundRXQueue      = 3
	soundNumQueues    = 4

	soundJackInfo  = 0x0001
	soundPCMInfo   = 0x0100
	soundSetParams = 0x0101
	soundPrepare   = 0x0102
	soundRelease   = 0x0103
	soundStart     = 0x0104
	soundStop      = 0x0105
	soundChmapInfo = 0x0200

	soundOK       = 0x8000
	soundBadMsg   = 0x8001
	soundNotSupp  = 0x8002
	soundIOErr    = 0x8003
	soundFormat16 = 5
	soundRate48K  = 7

	soundBytesPerFrame  = 4
	soundBytesPerSecond = 48000 * soundBytesPerFrame
	soundChunkBytes     = soundBytesPerSecond / 100 // 10 ms
	soundMaxBuffer      = 512 * 1024
	soundMaxPeriod      = 64 * 1024
)

// SoundSink consumes 48 kHz stereo S16_LE PCM. WritePCM must not block. The
// bytes are borrowed for the duration of the call; retaining them requires a
// copy. A disconnected sink should drop samples without stopping the clock.
type SoundSink interface {
	WritePCM([]byte)
}

type soundState uint8

const (
	soundUnconfigured soundState = iota
	soundConfigured
	soundPrepared
	soundRunning
	soundStopped
)

type soundBuffer struct {
	head     uint16
	response [][]byte
	request  [][]byte // Validated guest DMA mappings; the first four bytes are the stream ID.
	size     int
	sent     int
}

// Sound is a playback-only modern virtio PCI sound card. mu serializes queue
// setup/reset with the IO thread so no guest descriptors are touched after a
// device reset has returned.
type Sound struct {
	*ModernTransport

	mu          sync.Mutex
	queues      [soundNumQueues]*SplitQueue
	lastAvail   [soundNumQueues]uint16
	state       soundState
	bufferBytes uint32
	periodBytes uint32
	pending     []*soundBuffer
	pendingSize uint32
	next        time.Time
	chunk       [soundChunkBytes]byte
	sink        SoundSink
	irq         uint8
	kick        chan struct{}
	done        chan struct{}
	closed      bool
}

var _ pci.CapsAndMMIO = (*Sound)(nil)

// NewSound creates a sound card. The caller starts IOThreadEntry and closes it
// before unmapping guest RAM. inject raises the card's legacy INTx interrupt.
func NewSound(irq uint8, inject func() error, mem []byte, sink SoundSink) *Sound {
	s := &Sound{irq: irq, sink: sink, kick: make(chan struct{}, 1), done: make(chan struct{})}
	s.ModernTransport = NewModernTransport(s, mem, inject)

	return s
}

func (s *Sound) GetDeviceHeader() pci.DeviceHeader {
	return pci.DeviceHeader{
		DeviceID: 0x1059, VendorID: 0x1af4, ClassCode: 0x04, Subclass: 0x01,
		RevisionID: 1, SubsystemID: 0x1059, Command: 0x6, Status: 0x10, CapabilitiesPointer: capCommonAt,
		BAR: [6]uint32{SoundMMIOBase}, InterruptPin: 1, InterruptLine: s.irq,
	}
}

// The modern PCI card has no legacy IO-port BAR.
func (s *Sound) Read(_ uint64, _ []byte) error  { return nil }
func (s *Sound) Write(_ uint64, _ []byte) error { return nil }
func (s *Sound) IOPort() uint64                 { return 0 }
func (s *Sound) Size() uint64                   { return 0 }

func (s *Sound) DeviceFeatures() uint64               { return 0 }
func (s *Sound) NumQueues() int                       { return soundNumQueues }
func (s *Sound) DeviceConfigLen() int                 { return 12 }
func (s *Sound) WriteDeviceConfig(_ uint64, _ []byte) {}

func (s *Sound) ReadDeviceConfig(offset uint64, data []byte) {
	var cfg [12]byte
	binary.LittleEndian.PutUint32(cfg[4:], 1) // One playback stream.
	binary.LittleEndian.PutUint32(cfg[8:], 1) // One stereo channel map.
	zero(data)
	if offset < uint64(len(cfg)) {
		copy(data, cfg[offset:])
	}
}

func (s *Sound) QueueReady(idx int, q *SplitQueue) {
	if idx < 0 || idx >= soundNumQueues {
		return
	}

	s.mu.Lock()
	if !s.closed {
		s.queues[idx] = q
		s.lastAvail[idx] = 0
	}
	s.mu.Unlock()
	s.Notify(idx)
}

func (s *Sound) Notify(idx int) {
	if idx < 0 || idx >= soundNumQueues {
		return
	}

	select {
	case s.kick <- struct{}{}:
	default:
	}
}

// Reset abandons all queue mappings and pending samples without accessing old
// guest memory. A later queue setup can reuse the device and its IO thread.
func (s *Sound) Reset() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.resetLocked()
}

func (s *Sound) resetLocked() {
	s.queues = [soundNumQueues]*SplitQueue{}
	s.lastAvail = [soundNumQueues]uint16{}
	s.state = soundUnconfigured
	s.bufferBytes, s.periodBytes, s.pendingSize = 0, 0, 0
	s.pending = nil
	s.next = time.Time{}
}

func (s *Sound) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if !s.closed {
		s.closed = true
		s.resetLocked()
		close(s.done)
	}

	return nil
}

func (s *Sound) IOThreadEntry() {
	ticker := time.NewTicker(time.Millisecond)
	defer ticker.Stop()

	for {
		select {
		case <-s.done:
			return
		case <-s.kick:
		case <-ticker.C:
		}
		s.process(time.Now())
	}
}

func (s *Sound) process(now time.Time) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return
	}

	s.processControl()
	s.processTX(false)
	s.processRX()
	s.play(now)
	if err := s.ReinjectIfPending(); err != nil {
		log.Printf("virtio-snd: interrupt: %v", err)
	}
}

func soundQueueSize(q *SplitQueue) uint16 {
	if q.Size == 0 || q.Size > QueueSize {
		return QueueSize
	}

	return q.Size
}

func (s *Sound) pop(idx int) (uint16, bool) {
	q := s.queues[idx]
	if q == nil || q.Desc == nil || q.Avail == nil || q.Used == nil {
		return 0, false
	}

	available := q.Avail.Idx - s.lastAvail[idx]
	if available == 0 {
		return 0, false
	}
	if available > soundQueueSize(q) {
		// An overrun cannot describe valid outstanding buffers. Drop the
		// corrupt publication rather than walking an unbounded index range.
		s.lastAvail[idx] = q.Avail.Idx

		return 0, false
	}

	head := q.Avail.Ring[s.lastAvail[idx]%soundQueueSize(q)]
	s.lastAvail[idx]++

	return head, true
}

func (s *Sound) complete(idx int, head uint16, response [][]byte, data []byte) {
	var written uint32
	for _, segment := range response {
		n := copy(segment, data)
		written += uint32(n)
		data = data[n:]
		if len(data) == 0 {
			break
		}
	}

	q := s.queues[idx]
	q.Used.Ring[q.Used.Idx%soundQueueSize(q)] = SplitUsedElem{ID: uint32(head), Len: written}
	q.Used.Idx++
	if q.Avail.Flags&1 == 0 {
		if err := s.Interrupt(); err != nil {
			log.Printf("virtio-snd: interrupt: %v", err)
		}
	}
}

// chain validates the complete chain before reading PCM or touching response
// buffers. Indirect descriptors are not advertised. A strict readable-then-
// writable order prevents malformed requests from modifying guest input data.
func (s *Sound) chain(q *SplitQueue, head uint16, limit int) ([][]byte, [][]byte, int, int, bool) {
	var readable, writable [][]byte
	var seen [QueueSize]bool
	readBytes, writeBytes := 0, 0
	for {
		if head >= soundQueueSize(q) || seen[head] {
			return nil, nil, 0, 0, false
		}
		seen[head] = true
		d := q.Desc[head]
		if d.Flags & ^uint16(descFNext|descFWrite) != 0 ||
			!guestmem.ValidRange(s.Mem, d.Addr, uint64(d.Len)) {
			return nil, nil, 0, 0, false
		}
		segment := s.Mem[d.Addr : d.Addr+uint64(d.Len)]
		if d.Flags&descFWrite != 0 {
			if uint64(d.Len) > uint64(soundMaxBuffer-writeBytes) {
				return nil, nil, 0, 0, false
			}
			writeBytes += int(d.Len)
			writable = append(writable, segment)
		} else {
			if len(writable) != 0 || uint64(d.Len) > uint64(limit-readBytes) {
				return nil, nil, 0, 0, false
			}
			readBytes += int(d.Len)
			readable = append(readable, segment)
		}
		if d.Flags&descFNext == 0 {
			break
		}
		head = d.Next
	}

	return readable, writable, readBytes, writeBytes, true
}

func soundRead(dst []byte, segments [][]byte, offset int) {
	for _, segment := range segments {
		if offset >= len(segment) {
			offset -= len(segment)

			continue
		}
		n := copy(dst, segment[offset:])
		dst = dst[n:]
		offset = 0
		if len(dst) == 0 {
			return
		}
	}
}

func soundStatus(status uint32) []byte {
	var response [4]byte
	binary.LittleEndian.PutUint32(response[:], status)

	return response[:]
}

func (s *Sound) processControl() {
	for range QueueSize {
		head, ok := s.pop(soundControlQueue)
		if !ok {
			return
		}
		segments, response, size, capacity, valid := s.chain(s.queues[soundControlQueue], head, 64)
		var result []byte
		if valid && capacity >= 4 {
			result = soundStatus(soundBadMsg)
			if size >= 4 {
				request := make([]byte, size)
				soundRead(request, segments, 0)
				result = s.control(request, capacity)
			}
		}
		s.complete(soundControlQueue, head, response, result)
	}
}

func (s *Sound) control(request []byte, capacity int) []byte {
	code := binary.LittleEndian.Uint32(request)
	if code == soundPCMInfo || code == soundChmapInfo || code == soundJackInfo {
		return soundInfo(code, request, capacity)
	}
	if code < soundSetParams || code > soundStop {
		return soundStatus(soundNotSupp)
	}
	if len(request) < 8 || binary.LittleEndian.Uint32(request[4:]) != 0 {
		return soundStatus(soundBadMsg)
	}

	status := uint32(soundOK)
	switch code {
	case soundSetParams:
		status = s.setParams(request)
	case soundPrepare:
		if s.state != soundConfigured && s.state != soundPrepared {
			status = soundBadMsg
		} else {
			s.state = soundPrepared
		}
	case soundStart:
		if s.state != soundPrepared && s.state != soundStopped {
			status = soundBadMsg
		} else {
			s.state = soundRunning
			s.next = time.Time{}
		}
	case soundStop:
		if s.state != soundRunning {
			status = soundBadMsg
		} else {
			s.state = soundStopped
			s.next = time.Time{}
		}
	case soundRelease:
		if s.state != soundPrepared && s.state != soundStopped {
			status = soundBadMsg
		} else {
			// RELEASE must return every TX descriptor before its own ack,
			// including requests published just before the control command.
			for _, buffer := range s.pending {
				s.completePCM(buffer, soundOK)
			}
			s.pending, s.pendingSize = nil, 0
			s.processTX(true)
			s.state = soundConfigured
			s.next = time.Time{}
		}
	}

	return soundStatus(status)
}

func (s *Sound) setParams(request []byte) uint32 {
	if len(request) != 24 || (s.state != soundUnconfigured && s.state != soundConfigured && s.state != soundPrepared) {
		return soundBadMsg
	}
	le := binary.LittleEndian
	buffer, period, features := le.Uint32(request[8:]), le.Uint32(request[12:]), le.Uint32(request[16:])
	if features != 0 || request[20] != 2 || request[21] != soundFormat16 || request[22] != soundRate48K {
		return soundNotSupp
	}
	if buffer == 0 || buffer > soundMaxBuffer || period == 0 || period > soundMaxPeriod ||
		period%soundBytesPerFrame != 0 || buffer%period != 0 || request[23] != 0 || len(s.pending) != 0 {
		return soundBadMsg
	}
	s.bufferBytes, s.periodBytes = buffer, period
	s.state = soundConfigured

	return soundOK
}

func soundInfo(code uint32, request []byte, capacity int) []byte {
	if len(request) != 16 {
		return soundStatus(soundBadMsg)
	}
	le := binary.LittleEndian
	start, count, size := le.Uint32(request[4:]), le.Uint32(request[8:]), le.Uint32(request[12:])
	total, expected := uint32(1), uint32(32)
	switch code {
	case soundChmapInfo:
		expected = 24
	case soundJackInfo:
		total, expected = 0, 24
	}
	if start > total || count > total-start || size != expected || uint64(capacity) < 4+uint64(count)*uint64(size) {
		return soundStatus(soundBadMsg)
	}
	response := make([]byte, 4+count*size)
	le.PutUint32(response, soundOK)
	if count == 0 {
		return response
	}
	info := response[4:]
	// Keep the shared HDA function group at zero: Linux uses this field as
	// the ALSA PCM device number, and default playback opens hw:0,0.
	if code == soundPCMInfo {
		le.PutUint64(info[8:], uint64(1)<<soundFormat16)
		le.PutUint64(info[16:], uint64(1)<<soundRate48K)
		info[25], info[26] = 2, 2
	} else {
		info[5], info[6], info[7] = 2, 3, 4 // FL, FR.
	}

	return response
}

func (s *Sound) processTX(release bool) {
	for range QueueSize {
		head, ok := s.pop(soundTXQueue)
		if !ok {
			return
		}
		request, response, length, capacity, valid := s.chain(s.queues[soundTXQueue], head, soundMaxPeriod+4)
		buffer := &soundBuffer{head: head, response: response}
		var header [4]byte
		soundRead(header[:], request, 0)
		if !valid || capacity < 8 || length < 8 || binary.LittleEndian.Uint32(header[:]) != 0 ||
			(length-4)%soundBytesPerFrame != 0 {
			s.completePCM(buffer, soundIOErr)

			continue
		}
		if release {
			s.completePCM(buffer, soundOK)

			continue
		}
		size := uint32(length - 4)
		if (s.state != soundPrepared && s.state != soundRunning && s.state != soundStopped) ||
			size > s.periodBytes || size > s.bufferBytes-s.pendingSize || len(s.pending) >= QueueSize {
			s.completePCM(buffer, soundIOErr)

			continue
		}
		// Linux immediately requeues completed cyclic DMA periods from its
		// interrupt handler, before userspace refills them. Keep their validated
		// addresses and read samples on the playback clock, like hardware DMA.
		buffer.request, buffer.size = request, int(size)
		s.pendingSize += size
		s.pending = append(s.pending, buffer)
	}
}

func (s *Sound) completePCM(buffer *soundBuffer, status uint32) {
	var response [8]byte
	binary.LittleEndian.PutUint32(response[:], status)
	// No intermediate device playback buffer: chunks are sent at their
	// sample clock, and network/client buffering is outside this device.
	s.complete(soundTXQueue, buffer.head, buffer.response, response[:])
}

func (s *Sound) processRX() {
	// Capture is not advertised. Return invalid RX requests instead of
	// keeping a malicious or confused guest waiting indefinitely.
	for range QueueSize {
		head, ok := s.pop(soundRXQueue)
		if !ok {
			return
		}
		_, response, _, _, _ := s.chain(s.queues[soundRXQueue], head, 4)
		var result [8]byte
		binary.LittleEndian.PutUint32(result[:], soundIOErr)
		s.complete(soundRXQueue, head, response, result[:])
	}
}

func (s *Sound) play(now time.Time) {
	if s.state != soundRunning || len(s.pending) == 0 {
		return
	}
	if s.next.IsZero() || now.Sub(s.next) > 100*time.Millisecond {
		// After a host stall, resume in real time instead of flooding the
		// network and advancing the guest clock by seconds in one pass.
		s.next = now
	}
	for chunks := 0; chunks < QueueSize && !now.Before(s.next); chunks++ {
		buffer := s.pending[0]
		if buffer.sent == buffer.size {
			s.completePCM(buffer, soundOK)
			s.pendingSize -= uint32(buffer.size)
			s.pending[0] = nil
			s.pending = s.pending[1:]
			if len(s.pending) == 0 {
				s.next = time.Time{}

				return
			}
			buffer = s.pending[0]
		}
		n := min(soundChunkBytes, buffer.size-buffer.sent)
		if s.sink != nil {
			// Never expose mutable guest memory to the sink. Its borrowed
			// chunk is owned by Sound until WritePCM returns.
			soundRead(s.chunk[:n], buffer.request, 4+buffer.sent)
			s.sink.WritePCM(s.chunk[:n])
		}
		buffer.sent += n
		s.next = s.next.Add(time.Duration(n) * time.Second / soundBytesPerSecond)
	}
}
