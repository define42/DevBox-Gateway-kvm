package usernet

import (
	"net"

	"github.com/miekg/dns"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
)

// New starts a private IPv4 network with DHCP, DNS and outbound TCP/UDP. The
// first DHCP client receives 10.0.2.15; 10.0.2.2 also reaches host loopback services.
func New() (*Network, error) {
	return newDefaultNetwork(false)
}

// NewIsolated provides public IPv4 internet access while denying host, private
// and special destinations. DNS can still contact the configured host resolver.
func NewIsolated() (*Network, error) {
	return newDefaultNetwork(true)
}

func newDefaultNetwork(denyHostLoopback bool) (*Network, error) {
	config, err := dns.ClientConfigFromFile("/etc/resolv.conf")
	if err != nil {
		return nil, err
	}
	servers := make([]string, 0, len(config.Servers))
	for _, server := range config.Servers {
		servers = append(servers, net.JoinHostPort(server, config.Port))
	}

	return newNetworkWithIsolation(servers, denyHostLoopback)
}

// startDNS serves DNS inside the guest stack and forwards to the host's resolver
// addresses, including loopback resolvers such as systemd-resolved.
func (n *Network) startDNS(servers []string) error {
	address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: 53}
	conn, err := gonet.DialUDP(n.stack, &address, nil, ipv4.ProtocolNumber)
	if err != nil {
		return err
	}
	listener, err := gonet.ListenTCP(n.stack, address, ipv4.ProtocolNumber)
	if err != nil {
		_ = conn.Close()

		return err
	}
	service := newDNSService(n.ctx, conn, listener, servers)
	n.services = append(n.services, service)
	n.run(func() { service.serveUDP(n.ctx, n.run) })
	n.run(func() { service.writeUDP(n.ctx) })
	n.run(func() { _ = service.tcp.ActivateAndServe() })

	return nil
}
