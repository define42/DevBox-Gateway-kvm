package telemetry

import (
	"encoding/json"
	"errors"
	"math"
	"sync"
	"testing"
)

func TestNetworkSnapshotOptionalAndValidation(t *testing.T) {
	t.Parallel()
	legacy := Snapshot{Version: Version, Sequence: 1}
	data, err := json.Marshal(legacy)
	if err != nil {
		t.Fatal(err)
	}
	var decoded Snapshot
	if err := json.Unmarshal(data, &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded.Network != nil || decoded.Validate() != nil {
		t.Fatalf("legacy snapshot must retain unavailable network counters: %+v", decoded)
	}
	fresh := New().Snapshot()
	if fresh.Network == nil || fresh.Network.SentBytes != 0 || fresh.Network.ReceivedBytes != 0 {
		t.Fatalf("new worker must publish measured zero traffic: %+v", fresh.Network)
	}
	for _, network := range []*NetworkSnapshot{
		{SentBytes: math.MaxUint64}, {ReceivedBytes: math.MaxUint64},
	} {
		legacy.Network = network
		if !errors.Is(legacy.Validate(), ErrSnapshot) {
			t.Fatalf("accepted overflowing network counters: %+v", network)
		}
	}
}

func TestCollectorNetworkReconnectAndReset(t *testing.T) {
	t.Parallel()
	collector := New()
	// Negotiation contributes traffic before a session becomes active.
	collector.NetworkSent(19)
	collector.NetworkReceived(19)
	connection := collector.Connect()
	collector.NetworkSent(100)
	collector.NetworkReceived(20)
	connection.Close()
	reconnected := collector.Connect()
	collector.NetworkSent(200)
	collector.NetworkReceived(30)
	reconnected.Close()
	network := collector.Snapshot().Network
	if network.SentBytes != 319 || network.ReceivedBytes != 69 {
		t.Fatalf("reconnect lost transport counters: %+v", network)
	}
	fresh := New().Snapshot().Network
	if fresh.SentBytes != 0 || fresh.ReceivedBytes != 0 {
		t.Fatalf("new launch retained old transport counters: %+v", fresh)
	}
}

func TestCollectorNetworkConcurrent(t *testing.T) {
	t.Parallel()
	collector := New()
	const workers, packets = 8, 100
	var done sync.WaitGroup
	for range workers {
		done.Go(func() {
			for range packets {
				collector.NetworkSent(50)
				collector.NetworkReceived(7)
			}
		})
	}
	for range 10 {
		if err := collector.Snapshot().Validate(); err != nil {
			t.Fatalf("concurrent snapshot invalid: %v", err)
		}
	}
	done.Wait()
	network := collector.Snapshot().Network
	if network.SentBytes != workers*packets*50 || network.ReceivedBytes != workers*packets*7 {
		t.Fatalf("concurrent transport counters: %+v", network)
	}
}

func TestCollectorNetworkDisabledInvalidAndSaturation(t *testing.T) {
	t.Parallel()
	var disabled *Collector
	disabled.NetworkSent(1)
	disabled.NetworkReceived(1)
	collector := New()
	collector.NetworkSent(-1)
	collector.NetworkSent(0)
	collector.NetworkReceived(-1)
	collector.NetworkReceived(0)
	if network := collector.Snapshot().Network; network.SentBytes != 0 || network.ReceivedBytes != 0 {
		t.Fatalf("invalid observations changed counters: %+v", network)
	}
	collector.sentBytes.Store(math.MaxInt64 - 1)
	collector.receivedBytes.Store(math.MaxInt64 - 1)
	for range 2 {
		collector.NetworkSent(7)
		collector.NetworkReceived(7)
	}
	snapshot := collector.Snapshot()
	if snapshot.Network.SentBytes != math.MaxInt64 || snapshot.Network.ReceivedBytes != math.MaxInt64 {
		t.Fatalf("transport counters wrapped: %+v", snapshot.Network)
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("saturated counters invalid: %v", err)
	}
}

func TestSnapshotMaximumJSONSize(t *testing.T) {
	t.Parallel()
	cpu, resident := 1e12, uint64(math.MaxUint64)
	snapshot := Snapshot{
		Version: Version, Sequence: math.MaxUint64, ElapsedSeconds: 1e9,
		CPUSeconds: &cpu, ResidentBytes: &resident, Connections: 1, Encoder: "software",
		FramesSent: math.MaxUint64, PayloadBytes: math.MaxUint64,
		EncodeNanos: math.MaxUint64, EncodeCount: math.MaxUint64,
		WriteNanos: math.MaxUint64, WriteCount: math.MaxUint64, ACKNanos: math.MaxUint64,
		Network: &NetworkSnapshot{SentBytes: math.MaxInt64, ReceivedBytes: math.MaxInt64},
		Pipeline: &PipelineSnapshot{
			TransferNanos: math.MaxInt64, TransferCount: math.MaxInt64, TransferBytes: math.MaxInt64,
			FlushNanos: math.MaxInt64, FlushCount: math.MaxInt64, FlushPixels: math.MaxInt64,
			FullRefreshCount: math.MaxInt64, ScanoutSwitches: math.MaxInt64,
			CaptureNanos: math.MaxInt64, CaptureCount: math.MaxInt64,
			ConvertNanos: math.MaxInt64, ConvertCount: math.MaxInt64, ACKBlockedNanos: math.MaxInt64,
			OutstandingACKs: 2, SourceWidth: 4096, SourceHeight: 4096, OutputWidth: 4096, OutputHeight: 4096,
		},
	}
	if err := snapshot.Validate(); err != nil {
		t.Fatalf("maximal valid snapshot rejected: %v", err)
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxMessageSize {
		t.Fatalf("maximal telemetry JSON uses %d bytes, limit %d", len(data), MaxMessageSize)
	}
	t.Logf("maximal telemetry JSON: %d/%d bytes", len(data), MaxMessageSize)
}
