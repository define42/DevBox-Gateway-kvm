package telemetry

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net"
	"strings"
	"sync"
	"testing"
	"testing/synctest"
	"time"
)

type rdpTimeoutError struct{}

func (rdpTimeoutError) Error() string   { return "hostile timeout details" }
func (rdpTimeoutError) Timeout() bool   { return true }
func (rdpTimeoutError) Temporary() bool { return true }

var errRDPTestSecret = errors.New("private credential identity and packet")

func TestRDPSessionClassificationRetentionAndCopy(t *testing.T) {
	t.Parallel()

	var disabled *Collector
	disabled.RecordRDPSession("tls", errRDPTestSecret, time.Millisecond)
	collector := New()
	collector.RecordRDPSession("not-a-stage", errRDPTestSecret, time.Millisecond)
	collector.RecordRDPSession("tls", errRDPTestSecret, -time.Millisecond)
	observations := []struct {
		stage    string
		err      error
		outcome  string
		category string
	}{
		{"negotiation", nil, "closed", ""},
		{"tls", fmt.Errorf("private packet: %w", io.EOF), "closed", "peer_closed"},
		{"mcs_connect", fmt.Errorf("private packet: %w", net.ErrClosed), "closed", "connection_closed"},
		{"activation_client_info", fmt.Errorf("private credential: %w", context.DeadlineExceeded), "failed", "timeout"},
		{"session_read", fmt.Errorf("private credential: %w", rdpTimeoutError{}), "failed", "timeout"},
		{"frame_write", errRDPTestSecret, "failed", "protocol_or_internal_error"},
	}
	for _, observation := range observations {
		collector.RecordRDPSession(observation.stage, observation.err, 10*time.Millisecond)
	}
	events := collector.Snapshot().RDPSessions
	if len(events) != maxRDPSessionEvents {
		t.Fatalf("retained events = %d, want %d", len(events), maxRDPSessionEvents)
	}
	for index, observation := range observations[len(observations)-maxRDPSessionEvents:] {
		event := events[index]
		if event.Sequence != uint64(index+3) || event.Stage != observation.stage ||
			event.Outcome != observation.outcome || event.Error != observation.category || event.DurationMs != 10 {
			t.Errorf("event %d = %+v, want stage=%q outcome=%q error=%q", index, event,
				observation.stage, observation.outcome, observation.category)
		}
		if _, offset := event.Timestamp.Zone(); offset != 0 {
			t.Errorf("event %d timestamp is not UTC: %v", index, event.Timestamp)
		}
	}
	data, err := json.Marshal(events)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"private", "credential", "identity", "packet", "hostile"} {
		if strings.Contains(string(data), secret) {
			t.Fatalf("raw error detail %q leaked in %s", secret, data)
		}
	}

	events[0].Stage = "mutated"
	events[1].Error = "mutated"
	again := collector.Snapshot().RDPSessions
	if again[0].Stage == "mutated" || again[1].Error == "mutated" {
		t.Fatal("snapshot aliases retained RDP session history")
	}
	if got := collector.Snapshot().RDPSessions; len(got) != 4 || got[3].Sequence != 6 {
		t.Fatalf("idle snapshot lost terminal history: %+v", got)
	}
}

func TestRDPSessionValidation(t *testing.T) {
	t.Parallel()

	base := RDPSessionEvent{
		Sequence: 1, Timestamp: time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC),
		Stage: "activation_client_info", Outcome: "failed", Error: "protocol_or_internal_error", DurationMs: 1,
	}
	changed := func(change func(*RDPSessionEvent)) []RDPSessionEvent {
		event := base
		change(&event)

		return []RDPSessionEvent{event}
	}
	pair := func(change func(*RDPSessionEvent)) []RDPSessionEvent {
		event := base
		change(&event)

		return []RDPSessionEvent{base, event}
	}
	tests := []struct {
		name   string
		events []RDPSessionEvent
	}{
		{"too many", append(make([]RDPSessionEvent, maxRDPSessionEvents), base)},
		{"zero sequence", changed(func(event *RDPSessionEvent) { event.Sequence = 0 })},
		{"zero timestamp", changed(func(event *RDPSessionEvent) { event.Timestamp = time.Time{} })},
		{"non UTC", changed(func(event *RDPSessionEvent) {
			event.Timestamp = event.Timestamp.In(time.FixedZone("private", 3600))
		})},
		{"unknown stage", changed(func(event *RDPSessionEvent) { event.Stage = "<script>" })},
		{"unknown outcome", changed(func(event *RDPSessionEvent) { event.Outcome = "partial" })},
		{"unknown error", changed(func(event *RDPSessionEvent) { event.Error = "raw error" })},
		{"closed failure", changed(func(event *RDPSessionEvent) {
			event.Outcome, event.Error = "closed", "timeout"
		})},
		{"failed empty", changed(func(event *RDPSessionEvent) { event.Error = "" })},
		{"negative duration", changed(func(event *RDPSessionEvent) { event.DurationMs = -1 })},
		{"NaN duration", changed(func(event *RDPSessionEvent) { event.DurationMs = math.NaN() })},
		{"infinite duration", changed(func(event *RDPSessionEvent) { event.DurationMs = math.Inf(1) })},
		{"oversized duration", changed(func(event *RDPSessionEvent) {
			event.DurationMs = float64(math.MaxInt64)/float64(time.Millisecond) + 1
		})},
		{"sequence gap", pair(func(event *RDPSessionEvent) { event.Sequence = 3 })},
		{"time rollback", pair(func(event *RDPSessionEvent) {
			event.Sequence, event.Timestamp = 2, base.Timestamp.Add(-time.Second)
		})},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			snapshot := Snapshot{Version: Version, Sequence: 1, RDPSessions: test.events}
			if !errors.Is(snapshot.Validate(), ErrSnapshot) {
				t.Fatalf("accepted invalid RDP session history: %+v", test.events)
			}
		})
	}
	for _, result := range []struct{ outcome, category string }{
		{"closed", ""},
		{"closed", "peer_closed"},
		{"closed", "connection_closed"},
		{"failed", "timeout"},
		{"failed", "protocol_or_internal_error"},
	} {
		event := base
		event.Outcome, event.Error = result.outcome, result.category
		if err := (Snapshot{Version: Version, Sequence: 1, RDPSessions: []RDPSessionEvent{event}}).Validate(); err != nil {
			t.Errorf("valid result %q/%q rejected: %v", result.outcome, result.category, err)
		}
	}
	if err := (Snapshot{Version: Version, Sequence: 1}).Validate(); err != nil {
		t.Fatalf("older snapshot without RDP sessions rejected: %v", err)
	}
}

func TestRDPSessionsMonotonicAfter(t *testing.T) {
	t.Parallel()

	start := time.Date(2026, 10, 29, 10, 0, 0, 0, time.UTC)
	events := make([]RDPSessionEvent, 8)
	for index := range events {
		events[index] = RDPSessionEvent{
			Sequence: uint64(index + 1), Timestamp: start.Add(time.Duration(index) * time.Second),
			Stage: "session_read", Outcome: "closed", Error: "peer_closed", DurationMs: float64(index),
		}
	}
	for name, test := range map[string]struct {
		previous []RDPSessionEvent
		current  []RDPSessionEvent
		want     bool
	}{
		"old optional field": {nil, events[:1], true},
		"unchanged":          {events[:4], events[:4], true},
		"appended":           {events[:3], events[:4], true},
		"short append":       {events[:2], events[:3], true},
		"one evicted":        {events[:4], events[1:5], true},
		"fully evicted":      {events[:4], events[4:8], true},
		"disappeared":        {events[:1], nil, false},
		"rolled back":        {events[2:4], events[1:3], false},
		"missing overlap":    {events[:4], events[2:3], false},
		"premature eviction": {events[:4], events[4:5], false},
		"short discard":      {events[:2], events[1:3], false},
		"rewritten": {events[:4], func() []RDPSessionEvent {
			changed := append([]RDPSessionEvent(nil), events[:4]...)
			changed[3].Stage = "tls"

			return changed
		}(), false},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if got := RDPSessionsMonotonicAfter(test.previous, test.current); got != test.want {
				t.Fatalf("RDPSessionsMonotonicAfter() = %v, want %v", got, test.want)
			}
		})
	}
}

func TestRDPSessionConcurrentRecordAndSnapshot(t *testing.T) {
	t.Parallel()

	collector := New()
	const workers, sessions = 8, 100
	var group sync.WaitGroup
	for range workers {
		group.Go(func() {
			for range sessions {
				collector.RecordRDPSession("session_read", io.EOF, time.Millisecond)
				if err := collector.Snapshot().Validate(); err != nil {
					t.Errorf("concurrent snapshot invalid: %v", err)
				}
			}
		})
	}
	group.Wait()
	events := collector.Snapshot().RDPSessions
	if len(events) != maxRDPSessionEvents || events[len(events)-1].Sequence != workers*sessions {
		t.Fatalf("concurrent retained events: %+v", events)
	}
}

func TestRDPSessionHeartbeatAndWorstCasePacket(t *testing.T) {
	t.Parallel()

	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		collector := New()
		packets := make(chan []byte, 2)
		done := make(chan struct{})
		go func() {
			defer close(done)
			collector.run(ctx, func(data []byte) { packets <- data })
		}()
		if got := decodeSnapshot(t, <-packets).RDPSessions; len(got) != 0 {
			t.Fatalf("initial history = %+v", got)
		}
		collector.RecordRDPSession("activation_confirm_active", errRDPTestSecret, time.Millisecond)
		time.Sleep(Interval)
		synctest.Wait()
		got := decodeSnapshot(t, <-packets).RDPSessions
		if len(got) != 1 || got[0].DurationMs != 1 || got[0].Error != "protocol_or_internal_error" {
			t.Fatalf("short terminal session not retained for heartbeat: %+v", got)
		}
		cancel()
		<-done
	})

	cpu, resident := 1e12, uint64(math.MaxUint64)
	worst := Snapshot{
		Version: Version, Sequence: math.MaxUint64, ElapsedSeconds: 1e9,
		CPUSeconds: &cpu, ResidentBytes: &resident, Connections: maxConnections, Encoder: "mixed",
		FramesSent: math.MaxUint64, PayloadBytes: math.MaxUint64,
		EncodeNanos: math.MaxUint64, EncodeCount: math.MaxUint64,
		WriteNanos: math.MaxUint64, WriteCount: math.MaxUint64, ACKNanos: math.MaxUint64,
		Pipeline: &PipelineSnapshot{
			TransferNanos: math.MaxInt64, TransferCount: math.MaxInt64, TransferBytes: math.MaxInt64,
			FlushNanos: math.MaxInt64, FlushCount: math.MaxInt64, FlushPixels: math.MaxInt64,
			FullRefreshCount: math.MaxInt64, ScanoutSwitches: math.MaxInt64,
			CaptureNanos: math.MaxInt64, CaptureCount: math.MaxInt64,
			ConvertNanos: math.MaxInt64, ConvertCount: math.MaxInt64, ACKBlockedNanos: math.MaxInt64,
			OutstandingACKs: maxConnections * 2,
		},
		Network: &NetworkSnapshot{SentBytes: math.MaxInt64, ReceivedBytes: math.MaxInt64},
	}
	for index := range maxRDPSessionEvents {
		worst.RDPSessions = append(worst.RDPSessions, RDPSessionEvent{
			Sequence:  math.MaxUint64 - uint64(maxRDPSessionEvents-1-index),
			Timestamp: time.Date(9999, 12, 31, 23, 59, 59, 999_999_999, time.UTC),
			Stage:     "activation_confirm_active", Outcome: "failed", Error: "protocol_or_internal_error",
			DurationMs: float64(math.MaxInt64) / float64(time.Millisecond),
		})
	}
	if err := worst.Validate(); err != nil {
		t.Fatalf("worst-case snapshot invalid: %v", err)
	}
	data, err := json.Marshal(worst)
	if err != nil {
		t.Fatal(err)
	}
	if len(data) > MaxMessageSize {
		t.Fatalf("worst-case telemetry packet = %d bytes, limit %d", len(data), MaxMessageSize)
	}
}
