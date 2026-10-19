package telemetry

import (
	"math"
	"sync/atomic"
	"time"
)

// PipelineSnapshot contains cumulative pre-encode service times and work totals.
// A missing pipeline means an older worker, not zero work. Times are sums, not
// percentiles or end-to-end latency. Capture/convert and ACK waits sum viewers.
// Conversion includes bitmap comparison/packing when AVC is not active.
// FullRefreshCount counts forced initialization after scanout/geometry changes.
// Dimensions are unknown (zero) unless exactly one viewer is connected.
type PipelineSnapshot struct {
	TransferNanos    uint64 `json:"transferNanos"`
	TransferCount    uint64 `json:"transferCount"`
	TransferBytes    uint64 `json:"transferBytes"`
	FlushNanos       uint64 `json:"flushNanos"`
	FlushCount       uint64 `json:"flushCount"`
	FlushPixels      uint64 `json:"flushPixels"`
	FullRefreshCount uint64 `json:"fullRefreshCount"`
	ScanoutSwitches  uint64 `json:"scanoutSwitches"`
	CaptureNanos     uint64 `json:"captureNanos"`
	CaptureCount     uint64 `json:"captureCount"`
	ConvertNanos     uint64 `json:"convertNanos"`
	ConvertCount     uint64 `json:"convertCount"`
	ACKBlockedNanos  uint64 `json:"ackBlockedNanos"`
	OutstandingACKs  uint32 `json:"outstandingAcks"`
	SourceWidth      int    `json:"sourceWidth"`
	SourceHeight     int    `json:"sourceHeight"`
	OutputWidth      int    `json:"outputWidth"`
	OutputHeight     int    `json:"outputHeight"`
}

// Validate bounds values without imposing relationships on separately sampled
// atomic counters. Saturating counters never wrap into a false negative delta.
func (p *PipelineSnapshot) Validate() error {
	if p == nil {
		return nil
	}
	for _, counter := range p.counters() {
		if counter > math.MaxInt64 {
			return ErrSnapshot
		}
	}
	if p.OutstandingACKs > maxConnections*2 || !validSize(p.SourceWidth, p.SourceHeight) ||
		!validSize(p.OutputWidth, p.OutputHeight) {
		return ErrSnapshot
	}

	return nil
}

// MonotonicAfter rejects resets/rollbacks within a single child generation.
// Gauges may decrease. Pipeline support may appear, but must not disappear.
func (p *PipelineSnapshot) MonotonicAfter(previous *PipelineSnapshot) bool {
	if previous == nil {
		return true
	}
	if p == nil {
		return false
	}
	before := previous.counters()
	for i, counter := range p.counters() {
		if counter < before[i] {
			return false
		}
	}

	return true
}

func (p *PipelineSnapshot) counters() [13]uint64 {
	return [13]uint64{
		p.TransferNanos, p.TransferCount, p.TransferBytes, p.FlushNanos, p.FlushCount, p.FlushPixels,
		p.FullRefreshCount, p.ScanoutSwitches, p.CaptureNanos, p.CaptureCount,
		p.ConvertNanos, p.ConvertCount, p.ACKBlockedNanos,
	}
}

type pipelineCounters struct {
	transferNanos, transferCount, transferBytes            atomic.Uint64
	flushNanos, flushCount, flushPixels                    atomic.Uint64
	fullRefreshCount, scanoutSwitches                      atomic.Uint64
	captureNanos, captureCount, convertNanos, convertCount atomic.Uint64
	source                                                 atomic.Uint64
}

func (p *pipelineCounters) snapshot() *PipelineSnapshot {
	return &PipelineSnapshot{
		TransferNanos: p.transferNanos.Load(), TransferCount: p.transferCount.Load(),
		TransferBytes: p.transferBytes.Load(), FlushNanos: p.flushNanos.Load(),
		FlushCount: p.flushCount.Load(), FlushPixels: p.flushPixels.Load(),
		FullRefreshCount: p.fullRefreshCount.Load(), ScanoutSwitches: p.scanoutSwitches.Load(),
		CaptureNanos: p.captureNanos.Load(), CaptureCount: p.captureCount.Load(),
		ConvertNanos: p.convertNanos.Load(), ConvertCount: p.convertCount.Load(),
	}
}

// Begin and Elapsed avoid reading the clock when collection is disabled.
func (c *Collector) Begin() time.Time {
	if c == nil {
		return time.Time{}
	}

	return time.Now()
}

func (c *Collector) Elapsed(start time.Time) time.Duration {
	if c == nil || start.IsZero() {
		return 0
	}

	return time.Since(start)
}

// Transfer records one validated guest-to-host transfer and bytes actually
// copied, excluding missing or invalid backing ranges.
func (c *Collector) Transfer(elapsed time.Duration, bytes uint64) {
	if c != nil {
		addBounded(&c.pipeline.transferNanos, uint64(max(0, elapsed)))
		addBounded(&c.pipeline.transferBytes, bytes)
		addBounded(&c.pipeline.transferCount, 1)
	}
}

// Flush records conversion and synchronous publication of a visible rectangle.
func (c *Collector) Flush(elapsed time.Duration, pixels uint64, fullRefresh bool) {
	if c != nil {
		addBounded(&c.pipeline.flushNanos, uint64(max(0, elapsed)))
		addBounded(&c.pipeline.flushPixels, pixels)
		addBounded(&c.pipeline.flushCount, 1)
		if fullRefresh {
			addBounded(&c.pipeline.fullRefreshCount, 1)
		}
	}
}

func (c *Collector) ScanoutSwitch() {
	if c != nil {
		addBounded(&c.pipeline.scanoutSwitches, 1)
	}
}

func (c *Collector) SourceSize(width, height int) {
	if c != nil && validSize(width, height) {
		c.pipeline.source.Store(packSize(width, height))
	}
}

func (c *Connection) Captured(elapsed time.Duration) {
	if c != nil && !c.closed.Load() {
		addBounded(&c.collector.pipeline.captureNanos, uint64(max(0, elapsed)))
		addBounded(&c.collector.pipeline.captureCount, 1)
	}
}

func (c *Connection) Converted(elapsed time.Duration) {
	if c != nil && !c.closed.Load() && elapsed > 0 {
		addBounded(&c.collector.pipeline.convertNanos, uint64(elapsed))
		addBounded(&c.collector.pipeline.convertCount, 1)
	}
}

func (c *Connection) Geometry(sourceWidth, sourceHeight, outputWidth, outputHeight int) {
	if c != nil && !c.closed.Load() && validSize(sourceWidth, sourceHeight) && validSize(outputWidth, outputHeight) {
		c.source.Store(packSize(sourceWidth, sourceHeight))
		c.output.Store(packSize(outputWidth, outputHeight))
	}
}

func (c *Connection) OutstandingACKs(count uint32) {
	if c != nil && !c.closed.Load() {
		c.outstanding.Store(min(count, 2))
	}
}

// WaitingACK tracks a pending frame blocked by the ACK window, excluding the
// ordinary pacing deadline. Snapshot includes an ongoing interval even when the
// writer receives no events, and Close preserves the completed lifetime total.
func (c *Connection) WaitingACK(blocked bool, nextFrame time.Time) {
	if c == nil || c.closed.Load() {
		return
	}
	c.collector.mu.Lock()
	defer c.collector.mu.Unlock()
	if c.closed.Load() {
		return
	}
	if blocked == !c.blockedSince.IsZero() {
		return // An unchanged wait state needs neither clock sampling nor accounting.
	}
	now := time.Now()
	if !blocked {
		c.collector.ackBlocked = boundedSum(c.collector.ackBlocked, waitNanos(c.blockedSince, now))
		c.blockedSince = time.Time{}
	} else if c.blockedSince.IsZero() {
		c.blockedSince = now
		if nextFrame.After(now) {
			c.blockedSince = nextFrame
		}
	}
}

func waitNanos(start, now time.Time) uint64 {
	if start.IsZero() {
		return 0
	}

	return uint64(max(0, now.Sub(start)))
}

func validSize(width, height int) bool {
	return width >= 0 && height >= 0 && width <= 4096 && height <= 4096 && (width == 0) == (height == 0)
}

func packSize(width, height int) uint64 { return uint64(width)<<32 | uint64(height) }

func unpackSize(value uint64) (int, int) { return int(value >> 32), int(uint32(value)) }

func boundedSum(a, b uint64) uint64 { return a + min(b, math.MaxInt64-a) }

func addBounded(counter *atomic.Uint64, value uint64) {
	for {
		previous := counter.Load()
		if counter.CompareAndSwap(previous, boundedSum(previous, value)) {
			return
		}
	}
}
