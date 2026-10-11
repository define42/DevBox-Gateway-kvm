package virtio

import (
	"image"
	"testing"
	"time"

	"github.com/define42/gokvm/internal/rdp"
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
