package usernet

import (
	"io"
	"net"
	"net/netip"
	"strconv"
	"sync"
	"time"

	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
	"gvisor.dev/gvisor/pkg/waiter"
)

// destination maps the virtual gateway to host loopback. Broadcast/multicast
// packets belong to the virtual link and must not be sent through host sockets.
func (n *Network) destination(id stack.TransportEndpointID) (string, bool) {
	address := id.LocalAddress
	if address == gatewayAddress {
		address = tcpip.AddrFrom4([4]byte{127, 0, 0, 1})
	}
	ip, ok := netip.AddrFromSlice(address.AsSlice())
	// Check the translated IP before opening either TCP or UDP host sockets.
	// DNS responses never grant an exemption to this destination check.
	if !ok || !n.policy.permits(ip) {
		return "", false
	}

	return net.JoinHostPort(address.String(), strconv.Itoa(int(id.LocalPort))), true
}

func (n *Network) reserve() bool {
	select {
	case n.slots <- struct{}{}:
		return true
	default:
		return false
	}
}

func (n *Network) track(c io.Closer) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		_ = c.Close()

		return false
	}
	n.connections[c] = struct{}{}

	return true
}

func (n *Network) release(c io.Closer) {
	_ = c.Close()
	n.mu.Lock()
	delete(n.connections, c)
	n.mu.Unlock()
}

func (n *Network) forwardTCP(request *tcp.ForwarderRequest) {
	address, allowed := n.destination(request.ID())
	if !allowed || !n.reserve() {
		request.Complete(true)

		return
	}
	if !n.run(func() {
		defer func() { <-n.slots }()
		dialer := net.Dialer{Timeout: 10 * time.Second}
		host, err := dialer.DialContext(n.ctx, "tcp4", address)
		if err != nil {
			request.Complete(true)

			return
		}
		if !n.track(host) {
			request.Complete(true)

			return
		}
		defer n.release(host)
		var queue waiter.Queue
		endpoint, endpointErr := request.CreateEndpoint(&queue)
		request.Complete(endpointErr != nil)
		if endpointErr != nil {
			return
		}
		guest := gonet.NewTCPConn(&queue, endpoint)
		if !n.track(guest) {
			return
		}
		defer n.release(guest)
		done := make(chan struct{})
		go func() {
			defer close(done)
			_, copyErr := io.Copy(host, guest)
			if copyErr != nil {
				_ = guest.Close()
				_ = host.Close()

				return
			}
			if tcpHost, ok := host.(*net.TCPConn); ok {
				_ = tcpHost.CloseWrite()
			}
		}()
		_, copyErr := io.Copy(guest, host)
		if copyErr != nil {
			_ = guest.Close()
			_ = host.Close()
		} else {
			_ = guest.CloseWrite()
		}
		// Preserve TCP half-close: EOF ends only one direction. An I/O error
		// or Network.Close closes both sockets and releases the other copier.
		<-done
	}) {
		<-n.slots
		request.Complete(true)
	}
}

func (n *Network) forwardUDP(id stack.TransportEndpointID, packet *stack.PacketBuffer) bool {
	address, allowed := n.destination(id)
	if !allowed || !n.reserve() {
		return false
	}
	var queue waiter.Queue
	// The callback consumes the request synchronously, so the original packet
	// remains valid and no extra packet reference needs to outlive this call.
	request := udp.NewForwarderRequest(n.stack, id, packet)
	endpoint, err := request.CreateEndpoint(&queue)
	if err != nil {
		<-n.slots

		return false
	}
	guest := gonet.NewUDPConn(&queue, endpoint)
	if !n.track(guest) {
		<-n.slots

		return false
	}
	if !n.run(func() {
		defer func() { <-n.slots }()
		defer n.release(guest)
		dialer := net.Dialer{}
		host, dialErr := dialer.DialContext(n.ctx, "udp4", address)
		if dialErr != nil {
			return
		}
		if !n.track(host) {
			return
		}
		defer n.release(host)
		relayUDP(guest, host, time.Minute)
	}) {
		n.release(guest)
		<-n.slots

		return false
	}

	return true
}

// Any traffic refreshes the whole UDP mapping. Independent deadlines on the
// two directions would incorrectly expire continuously active one-way flows.
func relayUDP(guest, host net.Conn, idleTimeout time.Duration) {
	var activityMu sync.Mutex
	activity := func() {
		activityMu.Lock()
		defer activityMu.Unlock()
		deadline := time.Now().Add(idleTimeout)
		_ = guest.SetReadDeadline(deadline)
		_ = host.SetReadDeadline(deadline)
	}
	activity()
	done := make(chan struct{})
	go func() {
		defer close(done)
		relayDatagrams(host, guest, idleTimeout, activity)
		_ = host.Close()
		_ = guest.Close()
	}()
	relayDatagrams(guest, host, idleTimeout, activity)
	_ = host.Close()
	_ = guest.Close()
	<-done
}

func relayDatagrams(dst, src net.Conn, writeTimeout time.Duration, activity func()) {
	data := make([]byte, 65535)
	for {
		count, err := src.Read(data)
		if err != nil {
			return
		}
		activity()
		_ = dst.SetWriteDeadline(time.Now().Add(writeTimeout))
		if _, err := dst.Write(data[:count]); err != nil {
			return
		}
	}
}
