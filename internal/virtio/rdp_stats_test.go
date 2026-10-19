package virtio

import (
	"image"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/rdp"
	"github.com/define42/DevBox-Gateway-kvm/internal/rdp/avc"
	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func TestRDPStatsWaitWindows(t *testing.T) {
	t.Parallel()
	var wait rdpWaitTime
	start := time.Unix(100, 0)
	wait.update(true, start)
	wait.update(true, start.Add(2*time.Second))
	if wait.total != 2*time.Second {
		t.Fatalf("continuing wait=%v", wait.total)
	}
	// Reporting consumes the interval but carries the outstanding wait into
	// the next window, with no double counting when the client acknowledges.
	wait.total = 0
	wait.update(false, start.Add(3*time.Second))
	if wait.total != time.Second || !wait.start.IsZero() {
		t.Fatalf("next window wait=%v start=%v", wait.total, wait.start)
	}
	wait.update(false, start.Add(5*time.Second))
	if wait.total != time.Second {
		t.Fatal("idle time was counted as client backpressure")
	}
}

func TestRDPTelemetryWithoutLogs(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	connection := collector.Connect()
	defer connection.Close()
	stats := rdpFrameStats{telemetry: connection}
	if stats.begin().IsZero() {
		t.Fatal("managed telemetry did not enable timing independently of logs")
	}
	stats.encoded("nvenc", 2*time.Millisecond)
	stats.copied(time.Millisecond)
	stats.converted(4 * time.Millisecond)
	stats.written(3 * time.Millisecond)
	stats.sent(100)
	stats.captured("nvenc", image.Pt(1280, 720), image.Pt(1280, 720),
		rdp.GraphicsStats{ACKTurnaround: 20 * time.Millisecond})
	stats.report(1, true)
	stats.encoded("software", 4*time.Millisecond)
	stats.sent(200)
	snapshot := collector.Snapshot()
	if snapshot.Encoder != "software" || snapshot.FramesSent != 2 || snapshot.PayloadBytes != 300 ||
		snapshot.EncodeCount != 2 || snapshot.EncodeNanos != uint64(6*time.Millisecond) ||
		snapshot.WriteCount != 1 || snapshot.WriteNanos != uint64(3*time.Millisecond) ||
		snapshot.ACKNanos != uint64(20*time.Millisecond) {
		t.Fatalf("managed telemetry lost measurements or fallback: %+v", snapshot)
	}
	if stats.enabled || stats.frames != 0 {
		t.Fatal("telemetry enabled log output or local log counters")
	}
	if p := snapshot.Pipeline; p == nil || p.CaptureCount != 1 || p.CaptureNanos != uint64(time.Millisecond) ||
		p.ConvertCount != 1 || p.ConvertNanos != uint64(4*time.Millisecond) ||
		p.SourceWidth != 1280 || p.SourceHeight != 720 || p.OutputWidth != 1280 || p.OutputHeight != 720 {
		t.Fatalf("pre-encode measurements missing with logging disabled: %+v", p)
	}
	connection.Close()
	if snapshot := collector.Snapshot(); snapshot.Connections != 0 || snapshot.Encoder != "" {
		t.Fatalf("disconnected RDP telemetry still active: %+v", snapshot)
	}
}

func TestRDPTelemetrySurvivesLogWindowReset(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	connection := collector.Connect()
	defer connection.Close()
	stats := rdpFrameStats{enabled: true, telemetry: connection}
	stats.report(1, true)
	stats.encoded("software", time.Millisecond)
	stats.sent(100)
	if stats.telemetry != connection || collector.Snapshot().FramesSent != 1 {
		t.Fatal("log window reset detached process telemetry")
	}
}

func TestRDPTelemetryMeasuresRealEncoderAcrossWriters(t *testing.T) {
	t.Parallel()
	collector := telemetry.New()
	for pass := range 2 {
		writer := &rdpFrameWriter{
			session: &rdp.Session{Width: 16, Height: 16}, graphics: true,
			backend: avc.BackendSoftware, stats: rdpFrameStats{telemetry: collector.Connect()},
		}
		defer writer.shutdown()
		// A transport-less session runs the actual software codec but cannot
		// send a frame: encoder attempts and successful sends are distinct.
		sent, err := writer.write(displayFrame{width: 16, height: 16}, nil, true)
		if err != nil || sent {
			t.Fatalf("transport-less encode: sent=%v err=%v", sent, err)
		}
		snapshot := collector.Snapshot()
		if snapshot.Encoder != "software" || snapshot.EncodeCount != uint64(pass+1) ||
			snapshot.EncodeNanos == 0 || snapshot.FramesSent != 0 || snapshot.PayloadBytes != 0 {
			t.Fatalf("real codec timing missing with logs disabled: %+v", snapshot)
		}
		writer.shutdown()
		if idle := collector.Snapshot(); idle.Connections != 0 || idle.Encoder != "" {
			t.Fatalf("writer shutdown retained gauges: %+v", idle)
		}
	}
}

func TestRDPStatsDisabled(t *testing.T) {
	t.Parallel()
	var stats rdpFrameStats
	if !stats.begin().IsZero() || stats.elapsed(time.Time{}) != 0 {
		t.Fatal("disabled stats sampled the clock")
	}
	stats.waiting(true, time.Time{})
	stats.captured(
		"nvenc",
		image.Pt(1280, 720),
		image.Pt(1024, 768),
		rdp.GraphicsStats{FrameInterval: time.Second / 60, ACKTurnaround: time.Millisecond},
	)
	stats.sent(1024)
	stats.report(2, true)
	if stats != (rdpFrameStats{}) {
		t.Fatal("disabled statistics retained samples")
	}
}

func TestRDPStatsACKWaitExcludesPacing(t *testing.T) {
	t.Parallel()
	start := time.Unix(100, 0)
	nextFrame := start.Add(17 * time.Millisecond)
	wait := rdpWaitTime{start: nextFrame}
	// ACKs received before the next permitted frame do not delay rendering.
	wait.update(true, start.Add(time.Millisecond))
	wait.update(false, start.Add(5*time.Millisecond))
	if wait.total != 0 || !wait.start.IsZero() {
		t.Fatalf("pacing was attributed to client delay: %+v", wait)
	}
	wait.start = nextFrame
	wait.update(false, start.Add(20*time.Millisecond))
	if wait.total != 3*time.Millisecond {
		t.Fatalf("only time past the frame deadline should count: %v", wait.total)
	}
}

func TestRDPStatsCaptureTracksBackendAndGeometryChanges(t *testing.T) {
	t.Parallel()
	stats := rdpFrameStats{enabled: true}
	stats.captured(
		"nvenc",
		image.Pt(1280, 720),
		image.Pt(1024, 768),
		rdp.GraphicsStats{FrameInterval: 20 * time.Millisecond, ACKTurnaround: 10 * time.Millisecond},
	)
	// A fallback and resize update the reported configuration while pacing
	// retains both samples for the reporting window.
	stats.captured(
		"software",
		image.Pt(1920, 1080),
		image.Pt(1280, 720),
		rdp.GraphicsStats{FrameInterval: 40 * time.Millisecond, ACKTurnaround: 30 * time.Millisecond},
	)
	if stats.encoder != "software" ||
		stats.source != image.Pt(1920, 1080) || stats.output != image.Pt(1280, 720) {
		t.Fatalf("stale encoder or geometry after fallback/resize: %+v", stats)
	}
	if got := stats.pacing.String(); got != "30.00/40.00ms" {
		t.Fatalf("pacing avg/max = %s, want 30.00/40.00ms", got)
	}
	if stats.ackTurnaround != 30*time.Millisecond {
		t.Fatalf("ACK turnaround is not the latest snapshot: %v", stats.ackTurnaround)
	}
}
