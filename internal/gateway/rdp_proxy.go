package gateway

import (
	"crypto/tls"
	"io"
	"net"
	"sync"
	"time"
)

// rdpProxyError records the first observed I/O error, not necessarily the peer
// that initiated disconnection. Cancellation and simultaneous failures can also
// terminate a relay. Never include the wrapped error's text in diagnostics.
type rdpProxyError struct {
	operation string
	err       error
}

func (e *rdpProxyError) Error() string { return "RDP session relay ended" }

func (e *rdpProxyError) Unwrap() error { return e.err }

type proxyConn struct {
	net.Conn
	readOperation  string
	writeOperation string
	observe        func(string, error)
}

func (c proxyConn) Read(b []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Minute))
	n, err := c.Conn.Read(b)
	if err != nil {
		c.observe(c.readOperation, err)
	}

	return n, err
}

func (c proxyConn) Write(b []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))
	n, err := c.Conn.Write(b)
	if err == nil && n >= 0 && n < len(b) {
		// io.Copy would otherwise synthesize this after losing the endpoint.
		err = io.ErrShortWrite
	}
	if err != nil {
		c.observe(c.writeOperation, err)
	}

	return n, err
}

func proxyRDP(front, backend *tls.Conn) error {
	return proxyRDPConnections(front, backend, func() {
		// Close raw sockets: TLS Close can block sending close_notify while the
		// other copy is waiting for a peer that has already stopped responding.
		_ = front.NetConn().Close()
		_ = backend.NetConn().Close()
	})
}

func proxyRDPConnections(front, backend net.Conn, closeConnections func()) error {
	var first, closing sync.Once
	var firstError error
	observe := func(operation string, err error) {
		first.Do(func() {
			firstError = &rdpProxyError{operation: operation, err: err}
		})
	}
	frontProxy := proxyConn{
		Conn: front, readOperation: "front_read", writeOperation: "front_write", observe: observe,
	}
	backendProxy := proxyConn{
		Conn: backend, readOperation: "backend_read", writeOperation: "backend_write", observe: observe,
	}
	// Record errors in Read/Write before io.Copy swallows EOF or cleanup creates
	// secondary errors. Do not close on observation: a Read may return data and
	// EOF together, and io.Copy must still forward those final bytes.
	copyTo := func(dst, src proxyConn) {
		if _, err := io.Copy(dst, src); err != nil {
			// Read/Write normally recorded the error already. This also covers
			// errors io.Copy synthesizes for invalid Writer return counts.
			observe(dst.writeOperation, err)
		}
		closing.Do(closeConnections)
	}
	var copies sync.WaitGroup
	copies.Go(func() { copyTo(backendProxy, frontProxy) })
	copyTo(frontProxy, backendProxy)
	copies.Wait()

	return firstError
}
