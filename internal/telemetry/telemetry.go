// Package telemetry carries bounded, host-generated VM performance snapshots.
// It never includes guest output, credentials, input events or framebuffer data.
package telemetry

import (
	"errors"
	"math"
	"sync"
	"sync/atomic"
	"time"
)

const (
	Version        = 1
	MaxMessageSize = 4096
	Interval       = 5 * time.Second
	maxConnections = 16
)

var ErrSnapshot = errors.New("invalid VM telemetry snapshot")

// Snapshot contains process-lifetime counters and current connection gauges.
// CPUSeconds and ResidentBytes are nil when the operating-system sample failed.
// CPU time includes guest execution charged to this process, not only Go work.
// Frame counts are server sends and payload bytes exclude wire overhead.
// ACKNanos is send-to-ACK turnaround including client processing, not
// input/display latency.
type Snapshot struct {
	Version        uint32            `json:"version"`
	Sequence       uint64            `json:"sequence"`
	ElapsedSeconds float64           `json:"elapsedSeconds"`
	CPUSeconds     *float64          `json:"cpuSeconds"`
	ResidentBytes  *uint64           `json:"residentBytes"`
	Connections    uint32            `json:"connections"`
	Encoder        string            `json:"encoder"`
	FramesSent     uint64            `json:"framesSent"`
	PayloadBytes   uint64            `json:"payloadBytes"`
	EncodeNanos    uint64            `json:"encodeNanos"`
	EncodeCount    uint64            `json:"encodeCount"`
	WriteNanos     uint64            `json:"writeNanos"`
	WriteCount     uint64            `json:"writeCount"`
	ACKNanos       uint64            `json:"ackNanos"`
	Pipeline       *PipelineSnapshot `json:"pipeline,omitempty"`
	Network        *NetworkSnapshot  `json:"network,omitempty"`
}

// Validate checks the fixed schema's value domain, not cumulative ordering.
// The receiver must separately check launch identity, sequence and elapsed time.
func (s Snapshot) Validate() error {
	if s.Version != Version || s.Sequence == 0 ||
		!finiteNonnegative(s.ElapsedSeconds) || s.ElapsedSeconds > 1e9 {
		return ErrSnapshot
	}
	if s.CPUSeconds != nil && (!finiteNonnegative(*s.CPUSeconds) || *s.CPUSeconds > 1e12) {
		return ErrSnapshot
	}
	if s.Connections > maxConnections {
		return ErrSnapshot
	}
	switch s.Encoder {
	case "", "bitmap", "software", "nvenc", "mixed":
	default:
		return ErrSnapshot
	}
	if s.Connections == 0 && (s.Encoder != "" || s.ACKNanos != 0) {
		return ErrSnapshot
	}
	if err := s.Network.Validate(); err != nil {
		return err
	}
	if err := s.Pipeline.Validate(); err != nil {
		return err
	}
	if s.Pipeline != nil && (s.Pipeline.OutstandingACKs > 2*s.Connections ||
		(s.Connections != 1 && (s.Pipeline.SourceWidth != 0 || s.Pipeline.SourceHeight != 0 ||
			s.Pipeline.OutputWidth != 0 || s.Pipeline.OutputHeight != 0))) {
		return ErrSnapshot
	}

	return nil
}

func finiteNonnegative(value float64) bool {
	return value >= 0 && !math.IsNaN(value) && !math.IsInf(value, 0)
}

// Collector outlives individual displays and RDP connections, including guest
// firmware resets. Work counters are atomic and never perform I/O; membership
// and ACK-wait transitions share a short lock with heartbeat observation.
type Collector struct {
	start         time.Time
	sequence      atomic.Uint64
	frames        atomic.Uint64
	bytes         atomic.Uint64
	encodeNanos   atomic.Uint64
	encodeCount   atomic.Uint64
	writeNanos    atomic.Uint64
	writeCount    atomic.Uint64
	sentBytes     atomic.Uint64
	receivedBytes atomic.Uint64
	mu            sync.Mutex // Membership and ACK waits only; never held over I/O.
	connections   map[*Connection]struct{}
	pipeline      pipelineCounters
	ackBlocked    uint64 // Completed ACK wait intervals, protected by mu.
}

func New() *Collector {
	return &Collector{start: time.Now(), connections: make(map[*Connection]struct{})}
}

// Connect registers an activated RDP session, not an incomplete handshake.
// A nil collector disables collection. Membership is bounded defensively.
func (c *Collector) Connect() *Connection {
	if c == nil {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if len(c.connections) >= maxConnections {
		return nil
	}
	connection := &Connection{collector: c}
	connection.backend.Store(1) // Bitmap until an actual AVC encoder is selected.
	c.connections[connection] = struct{}{}

	return connection
}

// Snapshot samples process resources outside the short membership lock. Counter
// pairs are individually atomic; a concurrent observation can span snapshots.
func (c *Collector) Snapshot() Snapshot {
	snapshot := Snapshot{
		Version: Version, Sequence: c.sequence.Add(1), ElapsedSeconds: time.Since(c.start).Seconds(),
		FramesSent: c.frames.Load(), PayloadBytes: c.bytes.Load(),
		EncodeNanos: c.encodeNanos.Load(), EncodeCount: c.encodeCount.Load(),
		WriteNanos: c.writeNanos.Load(), WriteCount: c.writeCount.Load(),
		Network: &NetworkSnapshot{SentBytes: c.sentBytes.Load(), ReceivedBytes: c.receivedBytes.Load()},
	}
	c.mu.Lock()
	snapshot.Pipeline = c.pipeline.snapshot()
	snapshot.Pipeline.ACKBlockedNanos = c.ackBlocked
	now := time.Now()
	snapshot.Connections = uint32(len(c.connections))
	for connection := range c.connections {
		backend := backendName(connection.backend.Load())
		if snapshot.Encoder == "" {
			snapshot.Encoder = backend
		} else if snapshot.Encoder != backend {
			snapshot.Encoder = "mixed"
		}
		// With multiple viewers report the slowest valid current turnaround.
		snapshot.ACKNanos = max(snapshot.ACKNanos, connection.ack.Load())
		snapshot.Pipeline.OutstandingACKs += connection.outstanding.Load()
		snapshot.Pipeline.ACKBlockedNanos = boundedSum(
			snapshot.Pipeline.ACKBlockedNanos, waitNanos(connection.blockedSince, now),
		)
		if snapshot.Connections == 1 {
			snapshot.Pipeline.SourceWidth, snapshot.Pipeline.SourceHeight = unpackSize(connection.source.Load())
			snapshot.Pipeline.OutputWidth, snapshot.Pipeline.OutputHeight = unpackSize(connection.output.Load())
			if source := c.pipeline.source.Load(); source != 0 {
				snapshot.Pipeline.SourceWidth, snapshot.Pipeline.SourceHeight = unpackSize(source)
			}
		}
	}
	c.mu.Unlock()
	snapshot.CPUSeconds, snapshot.ResidentBytes = processResources()

	return snapshot
}

// Connection owns current backend/ACK gauges. Its methods are nil-safe and may
// be called concurrently, but observation should finish before Close.
type Connection struct {
	collector    *Collector
	backend      atomic.Uint32
	ack          atomic.Uint64
	closed       atomic.Bool
	outstanding  atomic.Uint32
	source       atomic.Uint64
	output       atomic.Uint64
	blockedSince time.Time // Protected by collector.mu; may start after pacing ends.
}

// Encoded records codec service time and its actual (possibly fallback) backend.
// A zero duration updates the backend without recording an encode operation.
func (c *Connection) Encoded(backend string, elapsed time.Duration) {
	if c == nil || c.closed.Load() {
		return
	}
	var value uint32
	switch backend {
	case "bitmap":
		value = 1
	case "software":
		value = 2
	case "nvenc":
		value = 3
	default:
		return
	}
	c.backend.Store(value)
	if elapsed > 0 {
		c.collector.encodeNanos.Add(uint64(elapsed))
		c.collector.encodeCount.Add(1)
	}
}

func (c *Connection) Written(elapsed time.Duration) {
	if c != nil && !c.closed.Load() && elapsed > 0 {
		c.collector.writeNanos.Add(uint64(elapsed))
		c.collector.writeCount.Add(1)
	}
}

func (c *Connection) Sent(payloadBytes int) {
	if c != nil && !c.closed.Load() && payloadBytes > 0 {
		c.collector.frames.Add(1)
		c.collector.bytes.Add(uint64(payloadBytes))
	}
}

// Acknowledge records the current smoothed turnaround; zero clears an unknown
// or reset reference chain's measurement rather than retaining a stale gauge.
func (c *Connection) Acknowledge(turnaround time.Duration) {
	if c != nil && !c.closed.Load() {
		c.ack.Store(uint64(max(0, turnaround)))
	}
}

func (c *Connection) Close() {
	if c == nil || !c.closed.CompareAndSwap(false, true) {
		return
	}
	c.collector.mu.Lock()
	c.collector.ackBlocked = boundedSum(c.collector.ackBlocked, waitNanos(c.blockedSince, time.Now()))
	c.blockedSince = time.Time{}
	delete(c.collector.connections, c)
	c.collector.mu.Unlock()
}

func backendName(value uint32) string {
	switch value {
	case 1:
		return "bitmap"
	case 2:
		return "software"
	case 3:
		return "nvenc"
	default:
		return ""
	}
}
