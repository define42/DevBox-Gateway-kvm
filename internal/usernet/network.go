// Package usernet connects a guest Ethernet device to host sockets without TAP,
// elevated privileges, or changes to the host's network configuration.
package usernet

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"syscall"

	"github.com/containers/gvisor-tap-vsock/pkg/services/dhcp"
	"github.com/containers/gvisor-tap-vsock/pkg/tap"
	"github.com/containers/gvisor-tap-vsock/pkg/types"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/ethernet"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/icmp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

const (
	Gateway        = "10.0.2.2"
	Subnet         = "10.0.2.0/24"
	MTU            = 1500
	queueSize      = 256
	maxConnections = 256
	gatewayMAC     = "02:00:00:00:02:02"
)

var errStack = errors.New("configure userspace network") //nolint:gochecknoglobals

var gatewayAddress = tcpip.AddrFrom4([4]byte{10, 0, 2, 2}) //nolint:gochecknoglobals

// Network is a nonblocking, packet-oriented Ethernet device. Each Read or Write
// transfers one complete frame. Ready signals that Read may have work available.
type Network struct {
	stack       *stack.Stack
	link        *channel.Endpoint
	incoming    chan []byte
	ready       chan struct{}
	ctx         context.Context //nolint:containedctx // The network owns its goroutines and cancellation lifetime.
	cancel      context.CancelFunc
	workers     sync.WaitGroup
	closeOnce   sync.Once
	mu          sync.Mutex
	closed      bool
	connections map[io.Closer]struct{}
	services    []io.Closer
	slots       chan struct{}
	policy      *destinationPolicy
}

func newNetwork(dnsServers []string) (*Network, error) {
	return newNetworkWithIsolation(dnsServers, false)
}

func newNetworkWithIsolation(dnsServers []string, isolated bool) (*Network, error) {
	if isolated {
		return newNetworkWithPolicy(dnsServers, "internet", "")
	}

	return newNetworkConfigured(dnsServers, nil)
}

func newNetworkConfigured(dnsServers []string, policy *destinationPolicy) (_ *Network, resultErr error) {
	ctx, cancel := context.WithCancel(context.Background())
	n := &Network{
		incoming: make(chan []byte, queueSize), ready: make(chan struct{}, 1),
		ctx: ctx, cancel: cancel, connections: make(map[io.Closer]struct{}),
		slots: make(chan struct{}, maxConnections), policy: policy,
	}
	defer func() {
		if resultErr != nil {
			_ = n.Close()
		}
	}()
	mac, _ := net.ParseMAC(gatewayMAC)
	n.link = channel.New(queueSize, MTU+header.EthernetMinimumSize, tcpip.LinkAddress(mac))
	n.link.AddNotify(n)
	n.stack = stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol, icmp.NewProtocol4},
	})
	if err := n.stack.CreateNIC(1, ethernet.New(n.link)); err != nil {
		return nil, fmt.Errorf("%w: %s", errStack, err.String())
	}
	if err := n.stack.AddProtocolAddress(1, tcpip.ProtocolAddress{
		Protocol: ipv4.ProtocolNumber, AddressWithPrefix: tcpip.AddressWithPrefix{Address: gatewayAddress, PrefixLen: 24},
	}, stack.AddressProperties{}); err != nil {
		return nil, fmt.Errorf("%w: %s", errStack, err.String())
	}
	if err := n.stack.SetSpoofing(1, true); err != nil {
		return nil, fmt.Errorf("%w: %s", errStack, err.String())
	}
	if err := n.stack.SetPromiscuousMode(1, true); err != nil {
		return nil, fmt.Errorf("%w: %s", errStack, err.String())
	}
	n.stack.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, NIC: 1}})
	tcpForwarder := tcp.NewForwarder(n.stack, 0, maxConnections, n.forwardTCP)
	n.stack.SetTransportProtocolHandler(tcp.ProtocolNumber, tcpForwarder.HandlePacket)
	n.stack.SetTransportProtocolHandler(udp.ProtocolNumber, n.forwardUDP)
	if err := n.startDHCP(); err != nil {
		return nil, fmt.Errorf("start DHCP: %w", err)
	}
	if err := n.startDNS(dnsServers); err != nil {
		return nil, fmt.Errorf("start DNS: %w", err)
	}
	n.run(n.receive)

	return n, nil
}

func (n *Network) startDHCP() error {
	pool, err := tap.NewIPPool(Subnet)
	if err != nil {
		return err
	}
	// Reserve addresses before the guest range and the subnet broadcast address.
	for i := 1; i < 15; i++ {
		if err := pool.Reserve(fmt.Sprintf("10.0.2.%d", i), gatewayMAC); err != nil {
			return err
		}
	}
	if err := pool.Reserve("10.0.2.255", gatewayMAC); err != nil {
		return err
	}
	configuration := &types.Configuration{
		Subnet: Subnet, GatewayIP: Gateway, GatewayMacAddress: gatewayMAC, MTU: MTU,
	}
	server, err := dhcp.New(configuration, n.stack, pool)
	if err != nil {
		return err
	}
	n.services = append(n.services, server.Underlying)
	n.run(func() { _ = server.Serve() })

	return nil
}

func (n *Network) run(f func()) bool {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.closed {
		return false
	}
	n.workers.Add(1)
	go func() { defer n.workers.Done(); f() }()

	return true
}

func (n *Network) receive() {
	for {
		select {
		case <-n.ctx.Done():
			return
		case frame := <-n.incoming:
			// Netstack answers ICMP locally. Do not make an external destination
			// appear reachable when no host ICMP forwarding is implemented.
			if len(frame) >= header.EthernetMinimumSize+header.IPv4MinimumSize &&
				header.Ethernet(frame).Type() == ipv4.ProtocolNumber {
				ip := header.IPv4(frame[header.EthernetMinimumSize:])
				if ip.Protocol() == uint8(icmp.ProtocolNumber4) && ip.DestinationAddress() != gatewayAddress {
					continue
				}
			}
			packet := stack.NewPacketBuffer(stack.PacketBufferOptions{Payload: buffer.MakeWithData(frame)})
			n.link.InjectInbound(0, packet)
			packet.DecRef()
		}
	}
}

// Ready returns edge notifications; callers should drain Read until EAGAIN.
func (n *Network) Ready() <-chan struct{} { return n.ready }

// WriteNotify implements channel.Notification.
func (n *Network) WriteNotify() {
	select {
	case n.ready <- struct{}{}:
	default:
	}
}

func (n *Network) Read(dst []byte) (int, error) {
	if n.ctx.Err() != nil {
		return 0, net.ErrClosed
	}
	for {
		packet := n.link.Read()
		if packet == nil {
			return 0, syscall.EAGAIN
		}
		view := packet.ToView()
		frame := view.AsSlice()
		// Promiscuous mode accepts routed traffic for any destination. Restrict ARP
		// replies to our gateway so guest duplicate-address detection still works.
		valid := true
		if len(frame) >= header.EthernetMinimumSize+header.ARPSize && header.Ethernet(frame).Type() == arp.ProtocolNumber {
			a := header.ARP(frame[header.EthernetMinimumSize:])
			valid = a.Op() != header.ARPReply || tcpip.AddrFrom4Slice(a.ProtocolAddressSender()) == gatewayAddress
		}
		size := len(frame)
		if valid && len(dst) >= size {
			copy(dst, frame)
		}
		view.Release()
		packet.DecRef()
		if !valid {
			continue
		}
		if n.link.NumQueued() > 0 {
			n.WriteNotify()
		}
		if len(dst) < size {
			return 0, io.ErrShortBuffer
		}

		return size, nil
	}
}

func (n *Network) Write(frame []byte) (int, error) {
	if n.ctx.Err() != nil {
		return 0, net.ErrClosed
	}
	// Invalid and saturated packets are dropped like a physical NIC would do.
	if len(frame) < header.EthernetMinimumSize || len(frame) > MTU+header.EthernetMinimumSize {
		return len(frame), nil
	}
	select {
	case n.incoming <- append([]byte(nil), frame...):
	default:
	}

	return len(frame), nil
}

func (n *Network) Close() error {
	n.closeOnce.Do(func() {
		n.mu.Lock()
		n.closed = true
		n.cancel()
		for c := range n.connections {
			_ = c.Close()
		}
		n.mu.Unlock()
		for _, c := range n.services {
			_ = c.Close()
		}
		// Stop packet delivery before destroying the stack. Cancellation also closes
		// active host sockets and interrupts pending TCP connection attempts.
		if n.stack != nil {
			n.stack.Close()
		}
		n.workers.Wait()
		if n.stack != nil {
			n.stack.Destroy()
		}
		if n.link != nil {
			n.link.Close()
		}
	})

	return nil
}
