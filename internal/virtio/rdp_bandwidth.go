package virtio

import (
	"net"

	"github.com/define42/DevBox-Gateway-kvm/internal/telemetry"
)

// rdpBandwidthConn measures the worker's authorized RDP transport before TLS,
// including negotiation, graphics, audio, input and control traffic. Embedding
// only net.Conn preserves addresses, deadlines and close without exposing
// io.ReaderFrom or io.WriterTo paths that would bypass byte accounting.
type rdpBandwidthConn struct {
	net.Conn
	collector *telemetry.Collector
}

func trackRDPBandwidth(conn net.Conn, collector *telemetry.Collector) net.Conn {
	if collector == nil {
		return conn
	}

	return &rdpBandwidthConn{Conn: conn, collector: collector}
}

func (c *rdpBandwidthConn) Read(data []byte) (int, error) {
	count, err := c.Conn.Read(data)
	c.collector.NetworkReceived(count)

	return count, err
}

func (c *rdpBandwidthConn) Write(data []byte) (int, error) {
	count, err := c.Conn.Write(data)
	c.collector.NetworkSent(count)

	return count, err
}
