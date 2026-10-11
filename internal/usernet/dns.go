package usernet

import (
	"context"
	"net"
	"time"

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

// NewIsolated preserves DHCP, DNS and outbound access while denying forwarded
// host loopback connections, including the virtual gateway's host-service alias.
// The DNS service can still contact the operator-configured host resolver.
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
	n.services = append(n.services, conn)
	listener, err := gonet.ListenTCP(n.stack, address, ipv4.ProtocolNumber)
	if err != nil {
		return err
	}
	n.services = append(n.services, listener)
	handler := dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
		ctx, cancel := context.WithTimeout(n.ctx, 5*time.Second)
		defer cancel()
		client := dns.Client{Net: "udp", Timeout: 2 * time.Second}
		for _, server := range servers {
			reply, _, exchangeErr := client.ExchangeContext(ctx, request, server)
			if exchangeErr != nil {
				continue
			}
			if reply.Truncated {
				client.Net = "tcp"
				reply, _, exchangeErr = client.ExchangeContext(ctx, request, server)
				client.Net = "udp"
				if exchangeErr != nil {
					continue
				}
			}
			_ = w.WriteMsg(reply)

			return
		}
		reply := new(dns.Msg)
		reply.SetRcode(request, dns.RcodeServerFailure)
		_ = w.WriteMsg(reply)
	})
	for _, server := range []*dns.Server{
		{PacketConn: conn, Handler: handler},
		{Listener: listener, Handler: handler},
	} {
		n.services = append(n.services, dnsService{server})
		n.run(func() { _ = server.ActivateAndServe() })
	}

	return nil
}

// Closing the listening socket alone leaves accepted TCP DNS clients alive.
type dnsService struct{ *dns.Server }

func (s dnsService) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	return s.ShutdownContext(ctx)
}
