package gateway

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

func performanceRDPSession(sequence uint64, timestamp time.Time) telemetry.RDPSessionEvent {
	return telemetry.RDPSessionEvent{
		Sequence: sequence, Timestamp: timestamp.UTC(), Stage: "session_read",
		Outcome: "closed", Error: "peer_closed", DurationMs: 1,
	}
}

func TestProcessPerformanceRDPSessionLifetimeAndCopy(t *testing.T) {
	t.Parallel()

	p := &processPerformance{}
	now := time.Now().UTC()
	first := performanceTestSample()
	first.RDPSessions = []telemetry.RDPSessionEvent{performanceRDPSession(1, now)}
	if !p.decode(performanceTestJSON(t, first), now) {
		t.Fatal("first RDP session sample rejected")
	}
	got := p.snapshot(now)
	if len(got.RDPSessions) != 1 || got.RDPSessions[0].Sequence != 1 {
		t.Fatalf("RDP sessions missing: %+v", got.RDPSessions)
	}
	got.RDPSessions[0].Stage = "mutated"
	if p.snapshot(now).RDPSessions[0].Stage != "session_read" {
		t.Fatal("VM performance RDP sessions alias retained telemetry")
	}

	idle := first
	idle.Sequence, idle.ElapsedSeconds = 2, 6
	idle.Connections, idle.Encoder, idle.ACKNanos = 0, "", 0
	if !p.decode(performanceTestJSON(t, idle), now.Add(5*time.Second)) {
		t.Fatal("idle heartbeat with retained RDP session rejected")
	}
	if got := p.snapshot(now.Add(5 * time.Second)); len(got.RDPSessions) != 1 || got.RDPSessions[0].Sequence != 1 {
		t.Fatalf("idle heartbeat lost RDP session: %+v", got.RDPSessions)
	}

	next := idle
	next.Sequence, next.ElapsedSeconds = 3, 11
	next.RDPSessions = append(cloneRDPSessionEvents(idle.RDPSessions), performanceRDPSession(2, now.Add(6*time.Second)))
	if !p.decode(performanceTestJSON(t, next), now.Add(10*time.Second)) {
		t.Fatal("advancing RDP session history rejected")
	}
	if got := p.snapshot(now.Add(26 * time.Second)); got.State != "stale" || len(got.RDPSessions) != 2 {
		t.Fatalf("stale performance lost RDP session history: %+v", got)
	}
}

func TestProcessPerformanceRejectsRDPHistoryMutation(t *testing.T) {
	t.Parallel()

	p := &processPerformance{}
	now := time.Now().UTC()
	first := performanceTestSample()
	first.RDPSessions = []telemetry.RDPSessionEvent{performanceRDPSession(1, now)}
	if !p.decode(performanceTestJSON(t, first), now) {
		t.Fatal("first sample rejected")
	}
	for name, mutate := range map[string]func(*telemetry.Snapshot){
		"rewritten": func(sample *telemetry.Snapshot) { sample.RDPSessions[0].Stage = "tls" },
		"removed":   func(sample *telemetry.Snapshot) { sample.RDPSessions = nil },
		"unbounded": func(sample *telemetry.Snapshot) {
			for sequence := uint64(2); sequence <= 5; sequence++ {
				sample.RDPSessions = append(sample.RDPSessions, performanceRDPSession(sequence, now))
			}
		},
		"raw error": func(sample *telemetry.Snapshot) { sample.RDPSessions[0].Error = "private packet" },
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			sample := first
			sample.RDPSessions = cloneRDPSessionEvents(first.RDPSessions)
			sample.Sequence, sample.ElapsedSeconds = 2, 6
			mutate(&sample)
			if p.decode(performanceTestJSON(t, sample), now.Add(5*time.Second)) {
				t.Fatal("invalid RDP history accepted")
			}
		})
	}
}

func TestProcessPerformanceRDPSessionReasonIngress(t *testing.T) {
	t.Parallel()

	p := &processPerformance{}
	now := time.Now().UTC()
	first := performanceTestSample()
	event := performanceRDPSession(1, now)
	event.Stage, event.Outcome, event.Error = "dynamic_channels", "failed", "protocol_or_internal_error"
	event.Reason = "dvc_caps_version"
	first.RDPSessions = []telemetry.RDPSessionEvent{event}
	if !p.decode(performanceTestJSON(t, first), now) {
		t.Fatal("allowlisted failure reason rejected")
	}
	got := p.snapshot(now)
	if len(got.RDPSessions) != 1 || got.RDPSessions[0].Reason != event.Reason {
		t.Fatalf("failure reason lost: %+v", got.RDPSessions)
	}
	got.RDPSessions[0].Reason = "mutated"
	if p.snapshot(now).RDPSessions[0].Reason != event.Reason {
		t.Fatal("snapshot reason aliases retained telemetry")
	}
	for _, reason := range []string{"private packet", "dvc_caps_header", ""} {
		sample := first
		sample.Sequence, sample.ElapsedSeconds = 2, 6
		sample.RDPSessions = cloneRDPSessionEvents(first.RDPSessions)
		sample.RDPSessions[0].Reason = reason
		if p.decode(performanceTestJSON(t, sample), now.Add(5*time.Second)) {
			t.Fatalf("accepted invalid/rewritten reason %q", reason)
		}
	}
	bad := first
	bad.RDPSessions = cloneRDPSessionEvents(first.RDPSessions)
	bad.RDPSessions[0].Reason = "private packet"
	if new(processPerformance).decode(performanceTestJSON(t, bad), now) {
		t.Fatal("raw reason accepted on first sample")
	}
}

func TestManagerPerformanceRDPSessionOwnershipAndLaunchLifetime(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	record := VM{ID: "aaaaaaaaaaaaaaaaaaaaaaaaaa", Owner: "alice", State: "running"}
	v := newManagedVM(record)
	m := &Manager{ctx: ctx, config: ManagerConfig{MemoryMiB: 1024}, vms: map[string]*managedVM{record.ID: v}}
	p := &processPerformance{}
	received := time.Now().Add(-performanceStaleAfter - time.Second).UTC()
	sample := performanceTestSample()
	sample.RDPSessions = []telemetry.RDPSessionEvent{performanceRDPSession(1, received)}
	if !p.decode(performanceTestJSON(t, sample), received) {
		t.Fatal("RDP telemetry sample rejected")
	}
	v.run = &managedProcess{done: make(chan struct{}), performance: p}
	v.publish()
	if _, err := m.Performance(t.Context(), "bob", record.ID); !errors.Is(err, ErrNotFound) {
		t.Fatalf("foreign RDP performance visible: %v", err)
	}
	got, err := m.Performance(t.Context(), "alice", record.ID)
	if err != nil || got.State != "stale" || len(got.RDPSessions) != 1 {
		t.Fatalf("owner stale RDP performance = %+v, %v", got, err)
	}
	got.RDPSessions[0].Stage = "mutated"
	again, _ := m.Performance(t.Context(), "alice", record.ID)
	if again.RDPSessions[0].Stage != "session_read" {
		t.Fatal("manager response aliases RDP session telemetry")
	}
	close(v.run.done)
	if got, _ := m.Performance(t.Context(), "alice", record.ID); got.State != "stopped" || len(got.RDPSessions) != 0 {
		t.Fatalf("exited worker retained RDP sessions: %+v", got)
	}
	v.run = &managedProcess{done: make(chan struct{}), performance: &processPerformance{}}
	v.publish()
	if got, _ := m.Performance(t.Context(), "alice", record.ID); got.State != "unavailable" || len(got.RDPSessions) != 0 {
		t.Fatalf("replacement worker inherited RDP sessions: %+v", got)
	}
}
