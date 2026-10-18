package telemetry

import (
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

func TestCollectorReconnectAndFallback(t *testing.T) {
	t.Parallel()
	collector := New()
	connection := collector.Connect()
	connection.Encoded("nvenc", 2*time.Millisecond)
	connection.Written(3 * time.Millisecond)
	connection.Sent(100)
	connection.Acknowledge(20 * time.Millisecond)
	first := collector.Snapshot()
	if first.Connections != 1 || first.Encoder != "nvenc" || first.ACKNanos != uint64(20*time.Millisecond) {
		t.Fatalf("initial connection gauges: %+v", first)
	}
	connection.Encoded("software", 4*time.Millisecond)
	connection.Sent(200)
	if got := collector.Snapshot().Encoder; got != "software" {
		t.Fatalf("fallback backend = %q", got)
	}
	connection.Close()
	connection.Close()
	connection.Encoded("nvenc", time.Second)
	connection.Written(time.Second)
	connection.Sent(999)
	connection.Acknowledge(time.Second)
	idle := collector.Snapshot()
	if idle.Connections != 0 || idle.Encoder != "" || idle.ACKNanos != 0 {
		t.Fatalf("stale disconnected gauges: %+v", idle)
	}
	if idle.FramesSent != 2 || idle.PayloadBytes != 300 || idle.EncodeCount != 2 ||
		idle.EncodeNanos != uint64(6*time.Millisecond) || idle.WriteCount != 1 ||
		idle.WriteNanos != uint64(3*time.Millisecond) {
		t.Fatalf("cumulative counters lost or duplicated: %+v", idle)
	}
	next := collector.Connect()
	defer next.Close()
	next.Sent(50)
	next.Encoded("bitmap", 0)
	reconnected := collector.Snapshot()
	if reconnected.Encoder != "bitmap" || reconnected.FramesSent != 3 ||
		reconnected.PayloadBytes != 350 || reconnected.EncodeCount != idle.EncodeCount {
		t.Fatalf("reconnect reset process counters: %+v", reconnected)
	}
	if err := reconnected.Validate(); err != nil {
		t.Fatalf("collector generated invalid snapshot: %v", err)
	}
}

func TestCollectorMembershipBoundAndMixedBackend(t *testing.T) {
	t.Parallel()
	collector := New()
	connections := make([]*Connection, maxConnections)
	for i := range connections {
		connections[i] = collector.Connect()
		if connections[i] == nil {
			t.Fatalf("connection %d rejected before limit", i)
		}
		defer connections[i].Close()
	}
	if collector.Connect() != nil {
		t.Fatal("connection state exceeded bound")
	}
	connections[0].Encoded("nvenc", 0)
	connections[0].Acknowledge(10 * time.Millisecond)
	connections[1].Acknowledge(30 * time.Millisecond)
	mixed := collector.Snapshot()
	if mixed.Connections != maxConnections || mixed.Encoder != "mixed" ||
		mixed.ACKNanos != uint64(30*time.Millisecond) {
		t.Fatalf("mixed connection gauges: %+v", mixed)
	}
	connections[1].Acknowledge(-1)
	if got := collector.Snapshot().ACKNanos; got != uint64(10*time.Millisecond) {
		t.Fatalf("reset ACK remained in aggregate: %d", got)
	}
	connections[0].Close()
	replacement := collector.Connect()
	if replacement == nil {
		t.Fatal("closed connection did not release membership")
	}
	defer replacement.Close()
	if got := collector.Snapshot().Encoder; got != "bitmap" {
		t.Fatalf("closed backend remained in aggregate: %q", got)
	}
}

func TestCollectorConcurrentCounters(t *testing.T) {
	t.Parallel()
	collector := New()
	connection := collector.Connect()
	defer connection.Close()
	const workers, frames = 8, 100
	var workersDone sync.WaitGroup
	for range workers {
		workersDone.Go(func() {
			for range frames {
				connection.Encoded("software", time.Microsecond)
				connection.Written(time.Microsecond)
				connection.Sent(100)
				connection.Acknowledge(time.Millisecond)
			}
		})
	}
	for range 10 {
		if err := collector.Snapshot().Validate(); err != nil {
			t.Fatalf("concurrent snapshot invalid: %v", err)
		}
	}
	workersDone.Wait()
	snapshot := collector.Snapshot()
	if snapshot.FramesSent != workers*frames || snapshot.PayloadBytes != workers*frames*100 ||
		snapshot.EncodeCount != workers*frames || snapshot.WriteCount != workers*frames ||
		snapshot.EncodeNanos != workers*frames*uint64(time.Microsecond) ||
		snapshot.WriteNanos != workers*frames*uint64(time.Microsecond) {
		t.Fatalf("concurrent counters: %+v", snapshot)
	}
}

func TestCollectorDisabledAndInvalidObservations(t *testing.T) {
	t.Parallel()
	var collector *Collector
	connection := collector.Connect()
	connection.Encoded("software", time.Second)
	connection.Written(time.Second)
	connection.Sent(1)
	connection.Acknowledge(time.Second)
	connection.Close()
	collector = New()
	connection = collector.Connect()
	defer connection.Close()
	connection.Encoded("unknown", time.Second)
	connection.Encoded("bitmap", -1)
	connection.Written(-1)
	connection.Sent(-1)
	snapshot := collector.Snapshot()
	if snapshot.Encoder != "bitmap" || snapshot.EncodeCount != 0 || snapshot.WriteCount != 0 || snapshot.FramesSent != 0 {
		t.Fatalf("invalid observations changed counters: %+v", snapshot)
	}
}

func TestSnapshotValidation(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		mutate func(*Snapshot)
	}{
		{name: "version", mutate: func(s *Snapshot) { s.Version = 2 }},
		{name: "sequence", mutate: func(s *Snapshot) { s.Sequence = 0 }},
		{name: "negative elapsed", mutate: func(s *Snapshot) { s.ElapsedSeconds = -1 }},
		{name: "NaN elapsed", mutate: func(s *Snapshot) { s.ElapsedSeconds = math.NaN() }},
		{name: "infinite elapsed", mutate: func(s *Snapshot) { s.ElapsedSeconds = math.Inf(1) }},
		{name: "oversized elapsed", mutate: func(s *Snapshot) { s.ElapsedSeconds = 1e10 }},
		{name: "negative CPU", mutate: func(s *Snapshot) { value := -1.0; s.CPUSeconds = &value }},
		{name: "NaN CPU", mutate: func(s *Snapshot) { value := math.NaN(); s.CPUSeconds = &value }},
		{name: "infinite CPU", mutate: func(s *Snapshot) { value := math.Inf(1); s.CPUSeconds = &value }},
		{name: "oversized CPU", mutate: func(s *Snapshot) { value := 1e13; s.CPUSeconds = &value }},
		{name: "unknown encoder", mutate: func(s *Snapshot) { s.Encoder = "unknown" }},
		{name: "connection bound", mutate: func(s *Snapshot) { s.Connections = maxConnections + 1 }},
		{name: "idle backend", mutate: func(s *Snapshot) { s.Encoder = "bitmap" }},
		{name: "idle ACK", mutate: func(s *Snapshot) { s.ACKNanos = 1 }},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := Snapshot{Version: Version, Sequence: 1}
			test.mutate(&snapshot)
			if !errors.Is(snapshot.Validate(), ErrSnapshot) {
				t.Fatalf("accepted invalid snapshot: %+v", snapshot)
			}
		})
	}
	// Null resource readings mean unavailable, not zero. Atomic counter pairs
	// can straddle an observation and therefore are not validated as a unit.
	valid := Snapshot{Version: Version, Sequence: 1, EncodeNanos: 1, WriteNanos: 1}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid partial measurement rejected: %v", err)
	}
}
