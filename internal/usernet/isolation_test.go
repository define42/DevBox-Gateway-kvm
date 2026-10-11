package usernet

import (
	"context"
	"net"
	"net/netip"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/miekg/dns"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

func TestIsolatedDestinationPolicy(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		address  string
		regular  bool
		isolated bool
	}{
		{address: "10.0.2.2", regular: true},
		{address: "127.0.0.1", regular: true},
		{address: "127.55.66.77", regular: true},
		{address: "::1", regular: true},
		{address: "::ffff:127.0.0.1", regular: true},
		{address: "::ffff:127.9.8.7", regular: true},
		{address: "192.168.1.7", regular: true, isolated: true},
		{address: "8.8.8.8", regular: true, isolated: true},
		{address: "0.0.0.0"},
		{address: "255.255.255.255"},
		{address: "224.0.0.1"},
		{address: "169.254.169.254"},
	} {
		t.Run(tc.address, func(t *testing.T) {
			t.Parallel()
			ip := netip.MustParseAddr(tc.address)
			id := stack.TransportEndpointID{LocalAddress: tcpip.AddrFromSlice(ip.AsSlice()), LocalPort: 3389}
			for _, isolated := range []bool{false, true} {
				n := &Network{denyHostLoopback: isolated}
				address, allowed := n.destination(id)
				want := tc.regular
				if isolated {
					want = tc.isolated
				}
				if allowed != want {
					t.Fatalf("isolated=%t destination=%q allowed=%t, want %t", isolated, address, allowed, want)
				}
				if !isolated && tc.address == Gateway && address != "127.0.0.1:3389" {
					t.Fatalf("ordinary user networking lost its host alias: %s", address)
				}
			}
		})
	}
}

func isolatedTestNetwork(t *testing.T, servers ...string) *Network {
	t.Helper()
	n, err := newNetworkWithIsolation(servers, true)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := n.Close(); err != nil {
			t.Error(err)
		}
	})

	return n
}

func TestIsolatedGuestCannotReachHostRDPPort(t *testing.T) {
	t.Parallel()
	n := isolatedTestNetwork(t)
	guest := guestStack(t, n)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(listener.Addr().(*net.TCPAddr).Port)}
	conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
	if err == nil {
		_ = conn.Close()
		t.Fatal("isolated guest connected to a private host loopback endpoint")
	}
	if ctx.Err() != nil {
		t.Fatal("guest connection timed out rather than being rejected by forwarding policy")
	}
}

func TestIsolatedGuestCannotForwardHostUDP(t *testing.T) {
	t.Parallel()
	n := isolatedTestNetwork(t)
	guest := guestStack(t, n)
	host, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer host.Close()
	address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(host.LocalAddr().(*net.UDPAddr).Port)}
	conn, err := gonet.DialUDP(guest, nil, &address, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	if _, err := conn.Write([]byte("must not reach the host")); err != nil {
		t.Fatal(err)
	}
	if err := host.SetReadDeadline(time.Now().Add(100 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := host.ReadFrom(make([]byte, 128)); err == nil {
		t.Fatal("isolated guest reached a host loopback UDP socket")
	}
}

func TestIsolatedDHCPAndDNS(t *testing.T) {
	t.Parallel()
	upstream, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer upstream.Close()
	server := &dns.Server{PacketConn: upstream, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
		reply := new(dns.Msg)
		reply.SetReply(request)
		reply.Answer = []dns.RR{&dns.A{
			Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
			A:   net.IPv4(192, 0, 2, 7),
		}}
		_ = w.WriteMsg(reply)
	})}
	go func() { _ = server.ActivateAndServe() }()
	n := isolatedTestNetwork(t, upstream.LocalAddr().String())
	discover, err := dhcpv4.NewDiscovery(net.HardwareAddr{2, 0, 0, 0, 0, 15})
	if err != nil {
		t.Fatal(err)
	}
	sendDHCP(t, n, discover)
	offer := receiveDHCP(t, n)
	if offer.MessageType() != dhcpv4.MessageTypeOffer || offer.YourIPAddr.String() != "10.0.2.15" {
		t.Fatalf("isolated DHCP offer = %s", offer.Summary())
	}
	request, err := dhcpv4.NewRequestFromOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	sendDHCP(t, n, request)
	if ack := receiveDHCP(t, n); ack.MessageType() != dhcpv4.MessageTypeAck {
		t.Fatal("isolated DHCP did not acknowledge the lease")
	}
	guest := guestStack(t, n)
	conn, err := gonet.DialUDP(guest, nil, &tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: 53}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	query := new(dns.Msg)
	query.SetQuestion("fixture.test.", dns.TypeA)
	client := dns.Client{}
	reply, _, err := client.ExchangeWithConn(query, &dns.Conn{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Answer) != 1 || reply.Answer[0].(*dns.A).A.String() != "192.0.2.7" {
		t.Fatalf("isolated DNS answer = %v", reply.Answer)
	}
}
