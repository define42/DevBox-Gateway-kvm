//go:build integration && linux

package rdp

import (
	"slices"
	"testing"
	"time"
)

// TestGraphicsACKFreeRDP exercises an independent decoder and repaint path.
// Timing assertions live in graphics_ack_test.go; machine-dependent latency is
// measured here without a brittle absolute performance threshold.
//
//nolint:paralleltest // Run clients serially to avoid distorting ACK latency with CPU contention.
func TestGraphicsACKFreeRDP(t *testing.T) {
	for _, workload := range []string{"Scroll", "WindowDrag"} {
		t.Run(workload, func(t *testing.T) {
			client := newGraphicsACKClient(t, graphicsACKWidth, graphicsACKHeight)
			fixture := newGraphicsACKFixture(t, workload)
			var measurements graphicsACKMeasurements
			for frame := range 12 {
				measurement := measureGraphicsACKFrame(t, client, fixture, frame)
				if frame >= 4 {
					measurements.add(measurement)
				}
			}
			measurements.validate(t)
			mean, p50, p95 := measurements.latencies()
			t.Logf("ACK mean=%s p50=%s p95=%s samples=%d raced=%d",
				mean, p50, p95, len(measurements.samples), measurements.raced)
		})
	}
}

// BenchmarkGraphicsACKFreeRDP sends one frame at a time over loopback TLS to
// FreeRDP on a private Xvfb display. ns/op includes encoding, writing and waiting;
// ack_*_ms uses the production handler's write-completion-to-ACK timestamps.
// It measures client decoding/repaint, not a physical display or WAN latency.
func BenchmarkGraphicsACKFreeRDP(b *testing.B) {
	for _, workload := range []string{"Scroll", "WindowDrag"} {
		b.Run(workload, func(b *testing.B) {
			client := newGraphicsACKClient(b, graphicsACKWidth, graphicsACKHeight)
			fixture := newGraphicsACKFixture(b, workload)
			const warmup = 10
			for frame := range warmup {
				measureGraphicsACKFrame(b, client, fixture, frame)
			}
			measurements := graphicsACKMeasurements{samples: make([]time.Duration, 0, 1024)}
			frame := warmup
			for b.Loop() {
				measurements.add(measureGraphicsACKFrame(b, client, fixture, frame))
				frame++
			}
			measurements.validate(b)
			mean, p50, p95 := measurements.latencies()
			b.ReportMetric(float64(mean)/float64(time.Millisecond), "ack_mean_ms")
			b.ReportMetric(float64(p50)/float64(time.Millisecond), "ack_p50_ms")
			b.ReportMetric(float64(p95)/float64(time.Millisecond), "ack_p95_ms")
			b.ReportMetric(float64(len(measurements.samples)), "ack_samples")
			b.ReportMetric(float64(measurements.raced)/float64(b.N), "ack_raced/frame")
			b.ReportMetric(float64(measurements.write)/float64(b.N)/float64(time.Millisecond), "write_mean_ms")
			b.ReportMetric(float64(measurements.writeACK)/float64(b.N)/float64(time.Millisecond), "write_ack_mean_ms")
			b.ReportMetric(float64(measurements.bytes)/float64(b.N), "bytes/frame")
			b.ReportMetric(float64(measurements.regions)/float64(b.N), "regions/frame")
		})
	}
}

type graphicsACKMeasurement struct {
	ack, write, writeACK time.Duration
	bytes, regions       int
}

type graphicsACKMeasurements struct {
	samples        []time.Duration
	total, write   time.Duration
	writeACK       time.Duration
	bytes, regions int64
	raced          int
}

func (m *graphicsACKMeasurements) add(sample graphicsACKMeasurement) {
	if sample.ack > 0 {
		m.samples = append(m.samples, sample.ack)
		m.total += sample.ack
	} else {
		m.raced++
	}
	m.write += sample.write
	m.writeACK += sample.writeACK
	m.bytes += int64(sample.bytes)
	m.regions += int64(sample.regions)
}

func (m *graphicsACKMeasurements) validate(tb testing.TB) {
	tb.Helper()
	if len(m.samples) == 0 {
		tb.Fatal("no post-write ACK timing samples; client may be acknowledging before writes complete")
	}
}

func (m *graphicsACKMeasurements) latencies() (mean, p50, p95 time.Duration) {
	slices.Sort(m.samples)
	count := len(m.samples)

	return m.total / time.Duration(count), m.samples[(count*50+99)/100-1], m.samples[(count*95+99)/100-1]
}

func measureGraphicsACKFrame(
	tb testing.TB, client *graphicsACKClient, fixture *graphicsACKFixture, step int,
) graphicsACKMeasurement {
	tb.Helper()
	frame := fixture.encode(tb, step)
	session := client.session
	g := session.graphics
	g.mu.Lock()
	invalid := g.ackOff || g.closed || !g.ready || len(g.inFlight) != 0
	// Benchmark-only reset: with one outstanding frame, the first EWMA sample
	// equals its raw turnaround. Preserve the actual production timestamps and
	// handling of ACKs that race write completion; report those as unsampled.
	g.ackTurnaround = 0
	g.mu.Unlock()
	if invalid {
		tb.Fatal("graphics channel unavailable, ACKs disabled, or previous frame still outstanding")
	}
	started := time.Now()
	sent, err := session.WriteAVC420Damage(frame.data, frame.regions)
	written := time.Since(started)
	if err != nil || !sent {
		tb.Fatalf("send frame %d: sent=%t error=%v", step, sent, err)
	}
	client.wait(tb, func() bool { return session.GraphicsStats().OutstandingACKs == 0 })
	elapsed := time.Since(started)
	g.mu.Lock()
	ack, invalid := g.ackTurnaround, g.ackOff || g.closed || !g.ready
	g.mu.Unlock()
	if invalid {
		tb.Fatal("graphics channel closed or client suspended frame acknowledgments")
	}

	return graphicsACKMeasurement{
		ack: ack, write: written, writeACK: elapsed,
		bytes: len(frame.data), regions: max(1, len(frame.regions)),
	}
}
