package gateway

import "github.com/define42/DevBox-Gateway-kvm/internal/telemetry"

// VMPipelinePerformance separates pre-encode work from transport backpressure.
// Durations are interval service-time means, not CPU time or percentiles. Rates
// aggregate work, so ACKBlockedMsPerSecond can exceed 1000 with multiple viewers.
// A nil Pipeline means the worker does not supply these measurements.
type VMPipelinePerformance struct {
	TransferMs               *float64 `json:"transferMs"`
	TransferBytesPerSecond   *float64 `json:"transferBytesPerSecond"`
	FlushMs                  *float64 `json:"flushMs"`
	FlushesPerSecond         *float64 `json:"flushesPerSecond"`
	FlushedPixelsPerSecond   *float64 `json:"flushedPixelsPerSecond"`
	FullRefreshesPerSecond   *float64 `json:"fullRefreshesPerSecond"`
	ScanoutSwitchesPerSecond *float64 `json:"scanoutSwitchesPerSecond"`
	CaptureMs                *float64 `json:"captureMs"`
	ConvertMs                *float64 `json:"convertMs"`
	ACKBlockedMsPerSecond    *float64 `json:"ackBlockedMsPerSecond"`
	OutstandingACKs          uint32   `json:"outstandingAcks"`
	SourceWidth              int      `json:"sourceWidth"`
	SourceHeight             int      `json:"sourceHeight"`
	OutputWidth              int      `json:"outputWidth"`
	OutputHeight             int      `json:"outputHeight"`
}

func pipelinePerformance(previous, current telemetry.Snapshot) *VMPipelinePerformance {
	now, before := current.Pipeline, previous.Pipeline
	if now == nil {
		return nil
	}
	result := &VMPipelinePerformance{
		OutstandingACKs: now.OutstandingACKs,
		SourceWidth:     now.SourceWidth, SourceHeight: now.SourceHeight,
		OutputWidth: now.OutputWidth, OutputHeight: now.OutputHeight,
	}
	if before == nil || previous.Sequence == 0 || !now.MonotonicAfter(before) {
		return result
	}
	elapsed := current.ElapsedSeconds - previous.ElapsedSeconds
	if elapsed <= 0 {
		return result
	}
	result.TransferMs = performanceMean(now.TransferNanos-before.TransferNanos, now.TransferCount-before.TransferCount)
	result.FlushMs = performanceMean(now.FlushNanos-before.FlushNanos, now.FlushCount-before.FlushCount)
	result.CaptureMs = performanceMean(now.CaptureNanos-before.CaptureNanos, now.CaptureCount-before.CaptureCount)
	result.ConvertMs = performanceMean(now.ConvertNanos-before.ConvertNanos, now.ConvertCount-before.ConvertCount)
	result.TransferBytesPerSecond = pipelineRate(now.TransferBytes-before.TransferBytes, elapsed)
	result.FlushesPerSecond = pipelineRate(now.FlushCount-before.FlushCount, elapsed)
	result.FlushedPixelsPerSecond = pipelineRate(now.FlushPixels-before.FlushPixels, elapsed)
	result.FullRefreshesPerSecond = pipelineRate(now.FullRefreshCount-before.FullRefreshCount, elapsed)
	result.ScanoutSwitchesPerSecond = pipelineRate(now.ScanoutSwitches-before.ScanoutSwitches, elapsed)
	result.ACKBlockedMsPerSecond = pipelineRate(now.ACKBlockedNanos-before.ACKBlockedNanos, elapsed*1e6)

	return result
}

func pipelineRate(delta uint64, elapsed float64) *float64 {
	rate := float64(delta) / elapsed

	return &rate
}
