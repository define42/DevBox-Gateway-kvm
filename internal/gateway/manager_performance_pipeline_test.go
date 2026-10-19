package gateway

import (
	"math"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func performancePipelineSample() *telemetry.PipelineSnapshot {
	return &telemetry.PipelineSnapshot{
		TransferNanos: 10_000_000, TransferCount: 10, TransferBytes: 1000,
		FlushNanos: 20_000_000, FlushCount: 10, FlushPixels: 2000,
		FullRefreshCount: 2, ScanoutSwitches: 2,
		CaptureNanos: 30_000_000, CaptureCount: 10, ConvertNanos: 40_000_000, ConvertCount: 10,
		ACKBlockedNanos: 1_000_000_000, OutstandingACKs: 2,
		SourceWidth: 1920, SourceHeight: 1080, OutputWidth: 1280, OutputHeight: 720,
	}
}

func TestPipelinePerformanceIntervals(t *testing.T) {
	t.Parallel()
	p := &processPerformance{}
	now := time.Now()
	first := performanceTestSample()
	first.Pipeline = performancePipelineSample()
	if !p.decode(performanceTestJSON(t, first), now) {
		t.Fatal("pipeline sample rejected")
	}
	initial := p.snapshot(now).Pipeline
	if initial == nil || initial.TransferMs != nil || initial.TransferBytesPerSecond != nil ||
		initial.OutstandingACKs != 2 || initial.SourceWidth != 1920 || initial.OutputWidth != 1280 {
		t.Fatalf("first sample invented rates or lost gauges: %+v", initial)
	}
	second := first
	second.Sequence, second.ElapsedSeconds = 4, 11
	second.Pipeline = &telemetry.PipelineSnapshot{
		TransferNanos: 30_000_000, TransferCount: 20, TransferBytes: 2000,
		FlushNanos: 50_000_000, FlushCount: 20, FlushPixels: 4000,
		FullRefreshCount: 3, ScanoutSwitches: 4,
		CaptureNanos: 70_000_000, CaptureCount: 20, ConvertNanos: 90_000_000, ConvertCount: 20,
		ACKBlockedNanos: 6_000_000_000, OutstandingACKs: 1,
		SourceWidth: 1920, SourceHeight: 1080, OutputWidth: 1280, OutputHeight: 720,
	}
	// Delayed/lost exports must use worker elapsed time, not receipt time.
	if !p.decode(performanceTestJSON(t, second), now.Add(20*time.Second)) {
		t.Fatal("advancing pipeline sample rejected")
	}
	got := p.snapshot(now.Add(20 * time.Second)).Pipeline
	for name, test := range map[string]struct {
		got  *float64
		want float64
	}{
		"transfer": {got.TransferMs, 2}, "bytes/s": {got.TransferBytesPerSecond, 100},
		"flush": {got.FlushMs, 3}, "flushes/s": {got.FlushesPerSecond, 1},
		"pixels/s": {got.FlushedPixelsPerSecond, 200}, "refreshes/s": {got.FullRefreshesPerSecond, 0.1},
		"switches/s": {got.ScanoutSwitchesPerSecond, 0.2}, "capture": {got.CaptureMs, 4},
		"convert": {got.ConvertMs, 5}, "ACK blocked ms/s": {got.ACKBlockedMsPerSecond, 500},
	} {
		if test.got == nil || *test.got != test.want {
			t.Errorf("%s = %v, want %v", name, test.got, test.want)
		}
	}
	*got.TransferMs, got.SourceWidth = 0, 0
	if again := p.snapshot(now).Pipeline; *again.TransferMs != 2 || again.SourceWidth != 1920 {
		t.Fatal("returned pipeline aliases retained sample")
	}
	idle := second
	idle.Sequence, idle.ElapsedSeconds = 5, 16
	idle.Pipeline = new(telemetry.PipelineSnapshot)
	*idle.Pipeline = *second.Pipeline
	idle.Connections, idle.Encoder, idle.ACKNanos = 0, "", 0
	idle.Pipeline.OutstandingACKs = 0
	idle.Pipeline.SourceWidth, idle.Pipeline.SourceHeight = 0, 0
	idle.Pipeline.OutputWidth, idle.Pipeline.OutputHeight = 0, 0
	if !p.decode(performanceTestJSON(t, idle), now.Add(25*time.Second)) {
		t.Fatal("disconnected sample rejected")
	}
	stale := p.snapshot(now.Add(41 * time.Second))
	if stale.State != "stale" || stale.Pipeline.TransferMs != nil || stale.Pipeline.ConvertMs != nil ||
		stale.Pipeline.FlushesPerSecond == nil || *stale.Pipeline.FlushesPerSecond != 0 ||
		stale.Pipeline.OutstandingACKs != 0 || *stale.Pipeline.ACKBlockedMsPerSecond != 0 {
		t.Fatalf("idle/stale pipeline invented work: %+v", stale.Pipeline)
	}
}

func TestPipelinePerformanceLegacyWorker(t *testing.T) {
	t.Parallel()
	p := &processPerformance{}
	now := time.Now()
	first := performanceTestSample()
	if !p.decode(performanceTestJSON(t, first), now) || p.snapshot(now).Pipeline != nil {
		t.Fatal("legacy worker must remain supported with null pipeline")
	}
	first.Sequence, first.ElapsedSeconds = 2, 6
	first.Pipeline = performancePipelineSample()
	if !p.decode(performanceTestJSON(t, first), now.Add(5*time.Second)) {
		t.Fatal("newly available pipeline rejected")
	}
	if got := p.snapshot(now).Pipeline; got == nil || got.CaptureMs != nil || got.ACKBlockedMsPerSecond != nil {
		t.Fatalf("new pipeline incorrectly used absent baseline: %+v", got)
	}
}

func TestPipelinePerformanceRejectsRollback(t *testing.T) {
	t.Parallel()
	for name, change := range map[string]func(*telemetry.PipelineSnapshot){
		"transfer time":    func(p *telemetry.PipelineSnapshot) { p.TransferNanos-- },
		"transfer count":   func(p *telemetry.PipelineSnapshot) { p.TransferCount-- },
		"transfer bytes":   func(p *telemetry.PipelineSnapshot) { p.TransferBytes-- },
		"flush time":       func(p *telemetry.PipelineSnapshot) { p.FlushNanos-- },
		"flush count":      func(p *telemetry.PipelineSnapshot) { p.FlushCount-- },
		"flush pixels":     func(p *telemetry.PipelineSnapshot) { p.FlushPixels-- },
		"refreshes":        func(p *telemetry.PipelineSnapshot) { p.FullRefreshCount-- },
		"scanout switches": func(p *telemetry.PipelineSnapshot) { p.ScanoutSwitches-- },
		"capture time":     func(p *telemetry.PipelineSnapshot) { p.CaptureNanos-- },
		"capture count":    func(p *telemetry.PipelineSnapshot) { p.CaptureCount-- },
		"convert time":     func(p *telemetry.PipelineSnapshot) { p.ConvertNanos-- },
		"convert count":    func(p *telemetry.PipelineSnapshot) { p.ConvertCount-- },
		"ACK wait":         func(p *telemetry.PipelineSnapshot) { p.ACKBlockedNanos-- },
		"overflow":         func(p *telemetry.PipelineSnapshot) { p.TransferNanos = math.MaxUint64 },
		"ACK window":       func(p *telemetry.PipelineSnapshot) { p.OutstandingACKs = 3 },
		"geometry":         func(p *telemetry.PipelineSnapshot) { p.SourceWidth = -1 },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			p := &processPerformance{}
			now := time.Now()
			first := performanceTestSample()
			first.Pipeline = performancePipelineSample()
			if !p.decode(performanceTestJSON(t, first), now) {
				t.Fatal("first sample rejected")
			}
			next := first
			next.Sequence, next.ElapsedSeconds = 2, 6
			next.Pipeline = performancePipelineSample()
			change(next.Pipeline)
			if p.decode(performanceTestJSON(t, next), now.Add(5*time.Second)) {
				t.Fatal("invalid pipeline sample accepted")
			}
			next.Pipeline = nil
			if p.decode(performanceTestJSON(t, next), now.Add(5*time.Second)) {
				t.Fatal("pipeline support disappeared within worker generation")
			}
			if got := p.snapshot(now); got.Pipeline == nil || !got.SampledAt.Equal(now) {
				t.Fatal("invalid sample replaced previous data")
			}
		})
	}
}
