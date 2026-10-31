// Package audio distributes guest PCM playback to remote listeners.
package audio

import (
	"sync"
	"time"
)

const (
	SampleRate    = 48000
	Channels      = 2
	BytesPerFrame = Channels * 2                     // Signed 16-bit little-endian samples.
	PacketBytes   = SampleRate * BytesPerFrame / 100 // 10 ms.
	queuePackets  = 5
	tailIdle      = 20 * time.Millisecond
)

// Packet contains immutable PCM and the source timestamp in milliseconds.
// Listeners must not change PCM, which can be shared with other listeners.
type Packet struct {
	PCM       []byte
	Timestamp uint32
}

// Hub keeps a bounded queue per listener. A slow or absent listener must never
// stop the virtual sound card's clock or retain unbounded guest audio.
type Hub struct {
	mu        sync.Mutex
	listeners map[*Subscription]struct{}
	started   time.Time
	closed    bool
	pending   []byte
	stamp     time.Duration
	lastWrite time.Time
	tail      *time.Timer // Flushes a partial packet once the source goes idle.
}

type Subscription struct {
	Packets <-chan Packet
	queue   chan Packet
	hub     *Hub
}

// NewHub returns a hub with no background polling: each write that leaves a
// partial packet postpones a single tail flush until tailIdle after it.
func NewHub() *Hub {
	h := &Hub{listeners: make(map[*Subscription]struct{}), started: time.Now()}
	h.tail = time.AfterFunc(tailIdle, func() { h.flush(time.Now()) })
	h.tail.Stop()

	return h
}

func (h *Hub) Subscribe() *Subscription {
	h.mu.Lock()
	defer h.mu.Unlock()
	queue := make(chan Packet, queuePackets)
	s := &Subscription{Packets: queue, queue: queue, hub: h}
	if h.closed {
		close(queue)
	} else {
		h.listeners[s] = struct{}{}
	}

	return s
}

// Close removes a listener. It is safe to call concurrently or more than once.
func (s *Subscription) Close() {
	h := s.hub
	h.mu.Lock()
	defer h.mu.Unlock()
	if _, ok := h.listeners[s]; ok {
		delete(h.listeners, s)
		close(s.queue)
		if len(h.listeners) == 0 {
			h.pending = nil
			h.lastWrite = time.Time{}
		}
	}
}

// WritePCM copies borrowed guest audio before returning. It never waits for a
// listener; when a queue fills, its oldest packet is discarded to limit delay.
func (h *Hub) WritePCM(pcm []byte) {
	h.writePCMAt(pcm, time.Now())
}

func (h *Hub) writePCMAt(pcm []byte, now time.Time) {
	if len(pcm)%BytesPerFrame != 0 {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.closed || len(h.listeners) == 0 {
		h.pending = nil
		h.lastWrite = time.Time{}

		return
	}
	if len(pcm) == 0 {
		return
	}
	h.flushLocked(now)
	h.lastWrite = now
	stamp := now.Sub(h.started)
	offset := 0
	for len(pcm) > 0 {
		if len(h.pending) == 0 {
			h.pending = make([]byte, 0, PacketBytes)
			h.stamp = stamp + time.Duration(offset)*time.Second/(SampleRate*BytesPerFrame)
		}
		n := min(len(pcm), PacketBytes-len(h.pending))
		h.pending = append(h.pending, pcm[:n]...)
		if len(h.pending) == PacketBytes {
			h.publishPending()
		}
		pcm = pcm[n:]
		offset += n
	}
	if len(h.pending) != 0 {
		// The flush rechecks the deadline, so a stale run is harmless.
		h.tail.Reset(tailIdle)
	}
}

func (h *Hub) flush(now time.Time) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.flushLocked(now)
}

// Keep packet duration constant across guest period boundaries. Some clients
// use the current packet's duration to detect overruns and drop short tails.
// Once the source stops, pad its last partial packet with silence so it plays.
func (h *Hub) flushLocked(now time.Time) {
	if h.closed || len(h.pending) == 0 || now.Sub(h.lastWrite) < tailIdle {
		return
	}
	n := len(h.pending)
	h.pending = h.pending[:PacketBytes]
	clear(h.pending[n:])
	h.publishPending()
}

func (h *Hub) publishPending() {
	packet := Packet{PCM: h.pending, Timestamp: uint32(h.stamp / time.Millisecond)}
	h.pending = nil // Published storage belongs to listeners and stays immutable.
	for listener := range h.listeners {
		select {
		case listener.queue <- packet:
		default:
			select {
			case <-listener.queue:
			default:
			}
			listener.queue <- packet // Only this mutex's owner sends.
		}
	}
}

// Close stops the tail flush. A flush already waiting for the mutex observes
// the closed hub and publishes nothing.
func (h *Hub) Close() {
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.closed {
		h.closed = true
		h.pending = nil
		h.tail.Stop()
		for listener := range h.listeners {
			close(listener.queue)
			delete(h.listeners, listener)
		}
	}
}
