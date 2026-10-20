package gateway

import (
	"encoding/json"
	"math"
	"reflect"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func TestProcessPerformanceNetworkRatesAndReconnect(t *testing.T) {
	t.Parallel()
	p := &processPerformance{}
	now := time.Now()
	sample := performanceTestSample()
	sample.Network = &telemetry.NetworkSnapshot{SentBytes: 1000, ReceivedBytes: 2000}
	if !p.decode(performanceTestJSON(t, sample), now) {
		t.Fatal("first sample rejected")
	}
	initial := p.snapshot(now).Network
	if !reflect.DeepEqual(initial, &VMNetworkPerformance{SentBytes: 1000, ReceivedBytes: 2000}) {
		t.Fatalf("first sample lost totals or invented a rate: %+v", initial)
	}
	// Lost/delayed exports must use the worker's ten-second interval, not
	// arrival time or the nominal five-second period. Directions are distinct.
	sample.Sequence, sample.ElapsedSeconds = 4, 11
	sample.Network = &telemetry.NetworkSnapshot{SentBytes: 10_001_000, ReceivedBytes: 502_000}
	if !p.decode(performanceTestJSON(t, sample), now.Add(20*time.Second)) {
		t.Fatal("advancing sample rejected")
	}
	got := p.snapshot(now.Add(20 * time.Second)).Network
	if got == nil || got.SentBytes != 10_001_000 || got.ReceivedBytes != 502_000 ||
		got.SentMbps == nil || *got.SentMbps != 8 || got.ReceivedMbps == nil || *got.ReceivedMbps != 0.4 {
		t.Fatalf("network totals/rates = %+v, want 8 Mbps down and 0.4 Mbps up", got)
	}
	got.SentBytes, got.ReceivedBytes, *got.SentMbps, *got.ReceivedMbps = 0, 0, 0, 0
	again := p.snapshot(now.Add(20 * time.Second)).Network
	if again.SentBytes != 10_001_000 || again.ReceivedBytes != 502_000 ||
		*again.SentMbps != 8 || *again.ReceivedMbps != 0.4 {
		t.Fatal("returned network data aliases retained samples")
	}
	sample.Sequence, sample.ElapsedSeconds = 5, 16
	sample.Connections, sample.Encoder, sample.ACKNanos = 0, "", 0
	if !p.decode(performanceTestJSON(t, sample), now.Add(25*time.Second)) {
		t.Fatal("disconnected sample rejected")
	}
	idle := p.snapshot(now.Add(25 * time.Second)).Network
	zero := 0.0
	wantIdle := &VMNetworkPerformance{SentBytes: 10_001_000, ReceivedBytes: 502_000, SentMbps: &zero, ReceivedMbps: &zero}
	if !reflect.DeepEqual(idle, wantIdle) {
		t.Fatalf("disconnection lost totals or invented traffic: %+v", idle)
	}
	// Bytes from subsequent handshakes/sessions contribute to the same totals.
	sample.Sequence, sample.ElapsedSeconds = 6, 21
	sample.Connections, sample.Encoder = 1, "nvenc"
	sample.Network = &telemetry.NetworkSnapshot{SentBytes: 11_251_000, ReceivedBytes: 502_625}
	if !p.decode(performanceTestJSON(t, sample), now.Add(30*time.Second)) {
		t.Fatal("reconnected sample rejected")
	}
	got = p.snapshot(now.Add(30 * time.Second)).Network
	if got == nil || got.SentMbps == nil || *got.SentMbps != 2 ||
		got.ReceivedMbps == nil || *got.ReceivedMbps != 0.001 {
		t.Fatalf("reconnection used incorrect counters: %+v", got)
	}
	stale := p.snapshot(now.Add(46 * time.Second))
	if stale.State != "stale" || stale.Network == nil || stale.Network.SentBytes != 11_251_000 {
		t.Fatalf("stale sample lost traffic history: %+v", stale)
	}
}

func TestProcessPerformanceNetworkOlderWorker(t *testing.T) {
	t.Parallel()
	p := &processPerformance{}
	now := time.Now()
	sample := performanceTestSample()
	if !p.decode(performanceTestJSON(t, sample), now) || p.snapshot(now).Network != nil {
		t.Fatal("older worker counters were rejected or invented")
	}
	sample.Sequence, sample.ElapsedSeconds = 2, 6
	sample.Network = &telemetry.NetworkSnapshot{SentBytes: 10000, ReceivedBytes: 500}
	if !p.decode(performanceTestJSON(t, sample), now.Add(5*time.Second)) {
		t.Fatal("first network measurement rejected")
	}
	got := p.snapshot(now.Add(5 * time.Second)).Network
	if got == nil || got.SentBytes != 10000 || got.ReceivedBytes != 500 ||
		got.SentMbps != nil || got.ReceivedMbps != nil {
		t.Fatalf("missing previous counters were treated as zero: %+v", got)
	}
}

func TestProcessPerformanceNetworkRejectsRollback(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name    string
		network *telemetry.NetworkSnapshot
	}{
		{name: "missing"},
		{name: "sent decreases", network: &telemetry.NetworkSnapshot{SentBytes: 99, ReceivedBytes: 200}},
		{name: "received decreases", network: &telemetry.NetworkSnapshot{SentBytes: 100, ReceivedBytes: 199}},
		{name: "sent overflow", network: &telemetry.NetworkSnapshot{SentBytes: math.MaxUint64, ReceivedBytes: 200}},
		{name: "received overflow", network: &telemetry.NetworkSnapshot{SentBytes: 100, ReceivedBytes: math.MaxUint64}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			p := &processPerformance{}
			now := time.Now()
			sample := performanceTestSample()
			sample.Network = &telemetry.NetworkSnapshot{SentBytes: 100, ReceivedBytes: 200}
			if !p.decode(performanceTestJSON(t, sample), now) {
				t.Fatal("first sample rejected")
			}
			sample.Sequence, sample.ElapsedSeconds = 2, 6
			sample.Network = test.network
			if p.decode(performanceTestJSON(t, sample), now.Add(5*time.Second)) {
				t.Fatal("invalid counters replaced previous sample")
			}
			got := p.snapshot(now)
			if got.SampledAt == nil || !got.SampledAt.Equal(now) || got.Network == nil ||
				got.Network.SentBytes != 100 || got.Network.ReceivedBytes != 200 || got.Network.SentMbps != nil {
				t.Fatalf("invalid counters altered latest sample: %+v", got)
			}
			if _, err := json.Marshal(got); err != nil {
				t.Fatalf("invalid derived rates: %v", err)
			}
		})
	}
}
