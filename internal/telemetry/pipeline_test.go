package telemetry

import (
	"context"
	"encoding/json"
	"math"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

func TestPipelineCountersAndGeometry(t *testing.T) {
	t.Parallel()
	c := New()
	connection := c.Connect()
	defer connection.Close()
	c.Transfer(2*time.Millisecond, 128)
	c.Flush(3*time.Millisecond, 32, true)
	c.ScanoutSwitch()
	c.SourceSize(1280, 720)
	connection.Captured(time.Millisecond)
	connection.Converted(4 * time.Millisecond)
	connection.Geometry(1024, 768, 640, 480)
	connection.OutstandingACKs(2)
	p := c.Snapshot().Pipeline
	if p == nil || p.TransferNanos != uint64(2*time.Millisecond) || p.TransferCount != 1 || p.TransferBytes != 128 ||
		p.FlushNanos != uint64(3*time.Millisecond) || p.FlushCount != 1 || p.FlushPixels != 32 ||
		p.FullRefreshCount != 1 || p.ScanoutSwitches != 1 || p.CaptureNanos != uint64(time.Millisecond) ||
		p.CaptureCount != 1 || p.ConvertNanos != uint64(4*time.Millisecond) || p.ConvertCount != 1 ||
		p.OutstandingACKs != 2 || p.SourceWidth != 1280 || p.SourceHeight != 720 ||
		p.OutputWidth != 640 || p.OutputHeight != 480 {
		t.Fatalf("incorrect pipeline counters: %+v", p)
	}
	other := c.Connect()
	other.Geometry(1920, 1080, 1920, 1080)
	other.OutstandingACKs(1)
	mixed := c.Snapshot()
	if mixed.Pipeline.SourceWidth != 0 || mixed.Pipeline.OutputHeight != 0 || mixed.Pipeline.OutstandingACKs != 3 {
		t.Fatalf("ambiguous dimensions or missing ACKs: %+v", mixed.Pipeline)
	}
	if err := mixed.Validate(); err != nil {
		t.Fatal(err)
	}
	other.Close()
	connection.Close()
	idle := c.Snapshot()
	if idle.Pipeline.OutstandingACKs != 0 || idle.Pipeline.SourceWidth != 0 ||
		idle.Pipeline.OutputHeight != 0 || idle.Pipeline.TransferBytes != 128 {
		t.Fatalf("disconnect lost counters or retained gauges: %+v", idle.Pipeline)
	}
}

func TestPipelineACKHeartbeatAndClose(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		c := New()
		connection := c.Connect()
		connection.WaitingACK(true, time.Now().Add(2*time.Second))
		connection.OutstandingACKs(2)
		packets := make(chan []byte, 4)
		done := make(chan struct{})
		go func() {
			defer close(done)
			c.run(ctx, func(data []byte) { packets <- data })
		}()
		first := decodeSnapshot(t, <-packets)
		if first.Pipeline.ACKBlockedNanos != 0 {
			t.Fatal("pacing counted as ACK blockage")
		}
		time.Sleep(Interval)
		synctest.Wait()
		second := decodeSnapshot(t, <-packets)
		if second.Pipeline.ACKBlockedNanos != uint64(3*time.Second) || second.Pipeline.OutstandingACKs != 2 {
			t.Fatalf("idle heartbeat did not include ongoing ACK wait: %+v", second.Pipeline)
		}
		time.Sleep(Interval)
		synctest.Wait()
		third := decodeSnapshot(t, <-packets)
		if third.Pipeline.ACKBlockedNanos != uint64(8*time.Second) {
			t.Fatalf("ongoing wait did not advance: %+v", third.Pipeline)
		}
		connection.Close()
		time.Sleep(Interval)
		synctest.Wait()
		idle := decodeSnapshot(t, <-packets)
		if idle.Pipeline.ACKBlockedNanos != third.Pipeline.ACKBlockedNanos || idle.Pipeline.OutstandingACKs != 0 {
			t.Fatalf("disconnect lost or extended ACK wait: %+v", idle.Pipeline)
		}
		cancel()
		<-done
	})
}

func TestPipelineACKWaitExcludesPacingAndIdle(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		c := New()
		connection := c.Connect()
		defer connection.Close()
		connection.WaitingACK(true, time.Now().Add(2*time.Second))
		time.Sleep(time.Second)
		connection.WaitingACK(false, time.Time{})
		time.Sleep(3 * time.Second)
		if c.Snapshot().Pipeline.ACKBlockedNanos != 0 {
			t.Fatal("ACK released during pacing was counted as blockage")
		}
		connection.WaitingACK(true, time.Time{})
		time.Sleep(time.Second)
		connection.WaitingACK(true, time.Time{})
		time.Sleep(time.Second)
		connection.WaitingACK(false, time.Time{})
		time.Sleep(time.Second)
		if got := c.Snapshot().Pipeline.ACKBlockedNanos; got != uint64(2*time.Second) {
			t.Fatalf("ACK wait repeated or idle counted: %d", got)
		}
	})
}

func TestPipelineValidationAndMonotonicity(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		value PipelineSnapshot
	}{
		{name: "overflow", value: PipelineSnapshot{TransferNanos: math.MaxUint64}},
		{name: "negative dimension", value: PipelineSnapshot{SourceWidth: -1}},
		{name: "oversized dimension", value: PipelineSnapshot{OutputWidth: 4097, OutputHeight: 720}},
		{name: "partial geometry", value: PipelineSnapshot{SourceWidth: 10}},
		{name: "ACK bound", value: PipelineSnapshot{OutstandingACKs: maxConnections*2 + 1}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			if err := test.value.Validate(); err == nil {
				t.Fatalf("accepted invalid pipeline: %+v", test.value)
			}
		})
	}
	previous := &PipelineSnapshot{TransferBytes: 20, ACKBlockedNanos: 40, OutstandingACKs: 2}
	current := &PipelineSnapshot{TransferBytes: 21, ACKBlockedNanos: 40}
	if !current.MonotonicAfter(previous) || !current.MonotonicAfter(nil) {
		t.Fatal("counter growth or decreasing gauge rejected")
	}
	current.TransferBytes = 19
	if current.MonotonicAfter(previous) || (*PipelineSnapshot)(nil).MonotonicAfter(previous) {
		t.Fatal("counter rollback or disappearing instrumentation accepted")
	}
	partial := &PipelineSnapshot{CaptureNanos: 10, FullRefreshCount: 1}
	if err := partial.Validate(); err != nil {
		t.Fatalf("independent atomic observations rejected: %v", err)
	}
}

func TestPipelineSaturationBoundedPayloadAndDisabled(t *testing.T) {
	t.Parallel()
	var disabled *Collector
	if !disabled.Begin().IsZero() || disabled.Elapsed(time.Time{}) != 0 {
		t.Fatal("disabled instrumentation read the clock")
	}
	disabled.Transfer(time.Second, 100)
	disabled.Flush(time.Second, 100, true)
	disabled.ScanoutSwitch()
	disabled.SourceSize(1280, 720)
	connection := disabled.Connect()
	connection.Captured(time.Second)
	connection.Converted(time.Second)
	connection.Geometry(1280, 720, 1280, 720)
	connection.OutstandingACKs(2)
	connection.WaitingACK(true, time.Time{})
	c := New()
	c.Transfer(time.Duration(math.MaxInt64), math.MaxUint64)
	c.Transfer(time.Second, 1)
	p := c.Snapshot()
	if p.Pipeline.TransferNanos != math.MaxInt64 || p.Pipeline.TransferBytes != math.MaxInt64 {
		t.Fatalf("counters wrapped: %+v", p.Pipeline)
	}
	if err := p.Validate(); err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(p)
	if err != nil || len(data) > MaxMessageSize {
		t.Fatalf("pipeline exceeds bounded packet: bytes=%d err=%v", len(data), err)
	}
}

func TestPipelineConcurrentCounters(t *testing.T) {
	t.Parallel()
	c := New()
	connection := c.Connect()
	defer connection.Close()
	var done sync.WaitGroup
	for range 4 {
		done.Go(func() {
			for range 100 {
				c.Transfer(time.Microsecond, 4)
				c.Flush(time.Microsecond, 1, false)
				connection.Captured(time.Microsecond)
				connection.Converted(time.Microsecond)
				connection.WaitingACK(true, time.Time{})
				connection.WaitingACK(false, time.Time{})
			}
		})
	}
	for range 10 {
		if err := c.Snapshot().Validate(); err != nil {
			t.Fatal(err)
		}
	}
	done.Wait()
	p := c.Snapshot().Pipeline
	if p.TransferCount != 400 || p.TransferBytes != 1600 || p.FlushCount != 400 ||
		p.CaptureCount != 400 || p.ConvertCount != 400 {
		t.Fatalf("concurrent counters lost observations: %+v", p)
	}
}
