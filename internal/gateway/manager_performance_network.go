package gateway

import "github.com/define42/DevBox-Gateway-kvm/internal/telemetry"

// VMNetworkPerformance measures RDP transport traffic across all connections to
// the current VM worker. Sent means VM to client (download); received means
// client to VM (upload). Totals survive reconnects and reset on VM restart.
// Rates are decimal megabits per second; nil means no sampling baseline yet.
// A nil Network means these measurements are unavailable.
type VMNetworkPerformance struct {
	SentBytes     uint64   `json:"sentBytes"`
	ReceivedBytes uint64   `json:"receivedBytes"`
	SentMbps      *float64 `json:"sentMbps"`
	ReceivedMbps  *float64 `json:"receivedMbps"`
}

func networkAdvances(previous, current *telemetry.NetworkSnapshot) bool {
	return previous == nil || (current != nil &&
		current.SentBytes >= previous.SentBytes && current.ReceivedBytes >= previous.ReceivedBytes)
}

func networkPerformance(previous, current telemetry.Snapshot) *VMNetworkPerformance {
	now, before := current.Network, previous.Network
	if now == nil {
		return nil
	}
	result := &VMNetworkPerformance{SentBytes: now.SentBytes, ReceivedBytes: now.ReceivedBytes}
	elapsed := current.ElapsedSeconds - previous.ElapsedSeconds
	if before == nil || previous.Sequence == 0 || elapsed <= 0 || !networkAdvances(before, now) {
		return result
	}
	sent := float64(now.SentBytes-before.SentBytes) * 8 / elapsed / 1e6
	received := float64(now.ReceivedBytes-before.ReceivedBytes) * 8 / elapsed / 1e6
	result.SentMbps, result.ReceivedMbps = &sent, &received

	return result
}
