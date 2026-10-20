package telemetry

import "math"

// NetworkSnapshot contains cumulative RDP transport bytes from this VM worker's
// perspective. SentBytes flows to the client; ReceivedBytes flows from it.
// Totals include RDP negotiation, TLS and all channels, but exclude TCP/IP
// overhead and guest networking. They survive reconnects and reset on launch.
// A missing network object identifies an older worker, not zero traffic.
type NetworkSnapshot struct {
	SentBytes     uint64 `json:"sentBytes"`
	ReceivedBytes uint64 `json:"receivedBytes"`
}

// Validate checks the domain of saturating process-lifetime counters.
func (n *NetworkSnapshot) Validate() error {
	if n != nil && (n.SentBytes > math.MaxInt64 || n.ReceivedBytes > math.MaxInt64) {
		return ErrSnapshot
	}

	return nil
}

// NetworkSent records bytes actually written, including partial writes with an
// error. Collection is independent of session activation and reconnects.
func (c *Collector) NetworkSent(bytes int) {
	if c != nil && bytes > 0 {
		addBounded(&c.sentBytes, uint64(bytes))
	}
}

// NetworkReceived records bytes actually read, including reads returning data
// with EOF or another error. Both network methods are nil-safe and concurrent.
func (c *Collector) NetworkReceived(bytes int) {
	if c != nil && bytes > 0 {
		addBounded(&c.receivedBytes, uint64(bytes))
	}
}
