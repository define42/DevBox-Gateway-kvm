package telemetry

import (
	"context"
	"errors"
	"io"
	"math"
	"net"
	"syscall"
	"time"
)

const maxRDPSessionEvents = 4

// RDPSessionEvent is one bounded terminal summary from the worker's backend
// RDP server. Values are entirely host-generated: no peer identity, packet
// contents, credentials, or raw error text is retained.
type RDPSessionEvent struct {
	Sequence   uint64    `json:"sequence"`
	Timestamp  time.Time `json:"timestamp"`
	Stage      string    `json:"stage"`
	Outcome    string    `json:"outcome"`
	Error      string    `json:"error,omitempty"`
	Reason     string    `json:"reason,omitempty"`
	DurationMs float64   `json:"durationMs"`
}

// RecordRDPSession retains one terminal backend RDP session. A nil collector,
// an unknown stage, or a negative duration disables the observation. Wrapped
// errors are reduced to a fixed category and their text is never retained.
func (c *Collector) RecordRDPSession(stage string, err error, duration time.Duration) {
	if c == nil || !validRDPSessionStage(stage) || duration < 0 {
		return
	}
	outcome, category := classifyRDPSessionError(err)
	reason := rdpSessionReason(stage, outcome, category, err)
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.rdpSequence == math.MaxUint64 {
		return
	}
	c.rdpSequence++
	timestamp := time.Now().UTC()
	if count := len(c.rdpSessions); count > 0 && timestamp.Before(c.rdpSessions[count-1].Timestamp) {
		timestamp = c.rdpSessions[count-1].Timestamp
	}
	event := RDPSessionEvent{
		Sequence: c.rdpSequence, Timestamp: timestamp, Stage: stage,
		Outcome: outcome, Error: category, Reason: reason, DurationMs: float64(duration) / float64(time.Millisecond),
	}
	if len(c.rdpSessions) == maxRDPSessionEvents {
		copy(c.rdpSessions, c.rdpSessions[1:])
		c.rdpSessions[len(c.rdpSessions)-1] = event

		return
	}
	c.rdpSessions = append(c.rdpSessions, event)
}

func classifyRDPSessionError(err error) (outcome, category string) {
	switch {
	case err == nil:
		return "closed", ""
	case errors.Is(err, io.EOF), errors.Is(err, io.ErrUnexpectedEOF):
		return "closed", "peer_closed"
	case errors.Is(err, net.ErrClosed), errors.Is(err, io.ErrClosedPipe),
		errors.Is(err, syscall.EPIPE), errors.Is(err, syscall.ECONNRESET), errors.Is(err, syscall.ECONNABORTED):
		return "closed", "connection_closed"
	case errors.Is(err, context.DeadlineExceeded):
		return "failed", "timeout"
	}
	var netError net.Error
	if errors.As(err, &netError) && netError.Timeout() {
		return "failed", "timeout"
	}

	return "failed", "protocol_or_internal_error"
}

func rdpSessionReason(stage, outcome, category string, err error) string {
	var diagnostic interface{ RDPDiagnosticReason() string }
	if !errors.As(err, &diagnostic) {
		return ""
	}
	reason := diagnostic.RDPDiagnosticReason()
	if !validRDPSessionReason(stage, outcome, category, reason) {
		return ""
	}

	return reason
}

func validRDPSessionReason(stage, outcome, category, reason string) bool {
	if reason == "" {
		return true
	}
	if stage != "dynamic_channels" || outcome != "failed" || category != "protocol_or_internal_error" {
		return false
	}
	switch reason {
	case "dvc_caps_state", "dvc_caps_length", "dvc_caps_header", "dvc_caps_version":
		return true
	default:
		return false
	}
}

func validRDPSessionStage(stage string) bool {
	switch stage {
	case "negotiation", "tls", "mcs_connect",
		"activation_erect_domain", "activation_attach_user", "activation_channel_join",
		"activation_client_info", "activation_license", "activation_demand_active",
		"activation_confirm_active", "activation_finalize", "audio", "dynamic_channels",
		"display_control", "graphics", "pointer", "session_read", "frame_write", "audio_write":
		return true
	default:
		return false
	}
}

func validateRDPSessions(events []RDPSessionEvent) error {
	if len(events) > maxRDPSessionEvents {
		return ErrSnapshot
	}
	for index, event := range events {
		if event.Sequence == 0 || event.Timestamp.IsZero() || !validRDPSessionStage(event.Stage) ||
			!finiteNonnegative(event.DurationMs) || event.DurationMs > float64(math.MaxInt64)/float64(time.Millisecond) ||
			!validRDPSessionResult(event.Outcome, event.Error) ||
			!validRDPSessionReason(event.Stage, event.Outcome, event.Error, event.Reason) {
			return ErrSnapshot
		}
		_, offset := event.Timestamp.Zone()
		if offset != 0 {
			return ErrSnapshot
		}
		if index > 0 && (event.Sequence != events[index-1].Sequence+1 ||
			event.Timestamp.Before(events[index-1].Timestamp)) {
			return ErrSnapshot
		}
	}

	return nil
}

func validRDPSessionResult(outcome, category string) bool {
	switch outcome {
	case "closed":
		return category == "" || category == "peer_closed" || category == "connection_closed"
	case "failed":
		return category == "timeout" || category == "protocol_or_internal_error"
	default:
		return false
	}
}

// RDPSessionsMonotonicAfter verifies that retained terminal history only stays
// unchanged or advances. A missing optional field may gain support, while a
// non-empty history cannot disappear or rewrite an overlapping sequence.
func RDPSessionsMonotonicAfter(previous, current []RDPSessionEvent) bool {
	if validateRDPSessions(previous) != nil || validateRDPSessions(current) != nil {
		return false
	}
	if len(previous) == 0 {
		return true
	}
	if len(current) == 0 || current[len(current)-1].Sequence < previous[len(previous)-1].Sequence {
		return false
	}
	previousNewest := previous[len(previous)-1].Sequence
	currentNewest := current[len(current)-1].Sequence
	added := currentNewest - previousNewest
	expectedCount := maxRDPSessionEvents
	if added < maxRDPSessionEvents {
		expectedCount = min(maxRDPSessionEvents, len(previous)+int(added))
	}
	if len(current) != expectedCount || current[0].Sequence != currentNewest-uint64(expectedCount-1) {
		return false
	}
	for _, before := range previous {
		for _, now := range current {
			if before.Sequence == now.Sequence && !sameRDPSessionEvent(before, now) {
				return false
			}
		}
	}

	return true
}

func sameRDPSessionEvent(left, right RDPSessionEvent) bool {
	return left.Sequence == right.Sequence && left.Timestamp.Equal(right.Timestamp) &&
		left.Stage == right.Stage && left.Outcome == right.Outcome && left.Error == right.Error &&
		left.Reason == right.Reason && left.DurationMs == right.DurationMs
}
