package usernet

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"syscall"
	"testing"
	"testing/synctest"
	"time"

	"github.com/google/gopacket"
	"github.com/google/gopacket/layers"
	"github.com/insomniacslk/dhcp/dhcpv4"
	"github.com/miekg/dns"
	"gvisor.dev/gvisor/pkg/buffer"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/header"
	"gvisor.dev/gvisor/pkg/tcpip/link/channel"
	"gvisor.dev/gvisor/pkg/tcpip/link/ethernet"
	"gvisor.dev/gvisor/pkg/tcpip/network/arp"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
	"gvisor.dev/gvisor/pkg/tcpip/transport/tcp"
	"gvisor.dev/gvisor/pkg/tcpip/transport/udp"
)

func testNetwork(tb testing.TB, servers ...string) *Network {
	tb.Helper()
	n, err := newNetwork(servers)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() {
		if err := n.Close(); err != nil {
			tb.Error(err)
		}
	})

	return n
}

func TestDHCP(t *testing.T) {
	t.Parallel()
	n := testNetwork(t)
	mac := net.HardwareAddr{2, 0, 0, 0, 0, 15}
	discover, err := dhcpv4.NewDiscovery(mac)
	if err != nil {
		t.Fatal(err)
	}
	sendDHCP(t, n, discover)
	offer := receiveDHCP(t, n)
	if offer.MessageType() != dhcpv4.MessageTypeOffer || !offer.YourIPAddr.Equal(net.IPv4(10, 0, 2, 15)) {
		t.Fatalf("unexpected offer: %s", offer.Summary())
	}
	request, err := dhcpv4.NewRequestFromOffer(offer)
	if err != nil {
		t.Fatal(err)
	}
	sendDHCP(t, n, request)
	ack := receiveDHCP(t, n)
	if ack.MessageType() != dhcpv4.MessageTypeAck {
		t.Fatalf("unexpected reply: %s", ack.Summary())
	}
	if routers := ack.Router(); len(routers) != 1 || routers[0].String() != Gateway {
		t.Fatalf("router = %v", routers)
	}
	if servers := ack.DNS(); len(servers) != 1 || servers[0].String() != Gateway {
		t.Fatalf("DNS = %v", servers)
	}
	if ack.SubnetMask().String() != "ffffff00" {
		t.Fatalf("subnet mask = %v", ack.SubnetMask())
	}
}

func sendDHCP(t *testing.T, n *Network, message *dhcpv4.DHCPv4) {
	t.Helper()
	eth := &layers.Ethernet{
		SrcMAC: message.ClientHWAddr, DstMAC: net.HardwareAddr{255, 255, 255, 255, 255, 255},
		EthernetType: layers.EthernetTypeIPv4,
	}
	ip := &layers.IPv4{Version: 4, TTL: 64, SrcIP: net.IPv4zero, DstIP: net.IPv4bcast, Protocol: layers.IPProtocolUDP}
	datagram := &layers.UDP{SrcPort: 68, DstPort: 67}
	if err := datagram.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatal(err)
	}
	packet := gopacket.NewSerializeBuffer()
	options := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	payload := gopacket.Payload(message.ToBytes())
	if err := gopacket.SerializeLayers(packet, options, eth, ip, datagram, payload); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Write(packet.Bytes()); err != nil {
		t.Fatal(err)
	}
}

func receiveDHCP(t *testing.T, n *Network) *dhcpv4.DHCPv4 {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	data := make([]byte, 1600)
	for {
		size, err := n.Read(data)
		if err == nil {
			packet := gopacket.NewPacket(data[:size], layers.LayerTypeEthernet, gopacket.Default)
			if layer := packet.Layer(layers.LayerTypeUDP); layer != nil {
				message, parseErr := dhcpv4.FromBytes(layer.(*layers.UDP).Payload)
				if parseErr == nil {
					return message
				}
			}

			continue
		}
		if !errors.Is(err, syscall.EAGAIN) {
			t.Fatal(err)
		}
		select {
		case <-ctx.Done():
			t.Fatal("no DHCP reply")
		case <-n.Ready():
		}
	}
}

func guestStack(tb testing.TB, n *Network) *stack.Stack {
	tb.Helper()
	link := channel.New(queueSize, MTU+header.EthernetMinimumSize, tcpip.LinkAddress("\x02\x00\x00\x00\x00\x0f"))
	guest := stack.New(stack.Options{
		NetworkProtocols:   []stack.NetworkProtocolFactory{ipv4.NewProtocol, arp.NewProtocol},
		TransportProtocols: []stack.TransportProtocolFactory{tcp.NewProtocol, udp.NewProtocol},
	})
	if err := guest.CreateNIC(1, ethernet.New(link)); err != nil {
		tb.Fatal(err)
	}
	address := tcpip.ProtocolAddress{
		Protocol:          ipv4.ProtocolNumber,
		AddressWithPrefix: tcpip.AddressWithPrefix{Address: tcpip.AddrFrom4([4]byte{10, 0, 2, 15}), PrefixLen: 24},
	}
	if err := guest.AddProtocolAddress(1, address, stack.AddressProperties{}); err != nil {
		tb.Fatal(err)
	}
	guest.SetRouteTable([]tcpip.Route{{Destination: header.IPv4EmptySubnet, Gateway: gatewayAddress, NIC: 1}})
	ctx, cancel := context.WithCancel(tb.Context())
	var workers sync.WaitGroup
	workers.Add(2)
	go func() {
		defer workers.Done()
		for {
			packet := link.ReadContext(ctx)
			if packet == nil {
				return
			}
			view := packet.ToView()
			_, _ = n.Write(view.AsSlice())
			view.Release()
			packet.DecRef()
		}
	}()
	go func() {
		defer workers.Done()
		data := make([]byte, 1600)
		for {
			size, err := n.Read(data)
			if err == nil {
				packet := stack.NewPacketBuffer(stack.PacketBufferOptions{
					Payload: buffer.MakeWithData(append([]byte(nil), data[:size]...)),
				})
				link.InjectInbound(0, packet)
				packet.DecRef()

				continue
			}
			select {
			case <-ctx.Done():
				return
			case <-n.Ready():
			}
		}
	}()
	tb.Cleanup(func() { cancel(); workers.Wait(); guest.Destroy(); link.Close() })

	return guest
}

func TestTCPAndUDP(t *testing.T) {
	t.Parallel()
	n := testNetwork(t)
	guest := guestStack(t, n)
	t.Run("TCP", func(t *testing.T) {
		t.Parallel()
		listener, err := net.Listen("tcp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer listener.Close()
		done := make(chan struct{})
		go func() {
			defer close(done)
			conn, acceptErr := listener.Accept()
			if acceptErr != nil {
				return
			}
			defer conn.Close()
			_, _ = io.Copy(conn, conn)
		}()
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()
		address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(listener.Addr().(*net.TCPAddr).Port)}
		conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
		if err != nil {
			t.Fatal(err)
		}
		exchange(t, conn)
		_ = conn.Close()
		select {
		case <-done:
		case <-ctx.Done():
			t.Fatal("TCP relay did not close")
		}
	})
	t.Run("UDP", func(t *testing.T) {
		t.Parallel()
		host, err := net.ListenPacket("udp4", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		defer host.Close()
		go func() {
			data := make([]byte, 1600)
			size, addr, readErr := host.ReadFrom(data)
			if readErr == nil {
				_, _ = host.WriteTo(data[:size], addr)
			}
		}()
		address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(host.LocalAddr().(*net.UDPAddr).Port)}
		conn, err := gonet.DialUDP(guest, nil, &address, ipv4.ProtocolNumber)
		if err != nil {
			t.Fatal(err)
		}
		defer conn.Close()
		exchange(t, conn)
	})
}

func exchange(t *testing.T, conn net.Conn) {
	t.Helper()
	if err := conn.SetDeadline(time.Now().Add(3 * time.Second)); err != nil {
		t.Fatal(err)
	}
	want := []byte("network through host sockets")
	if _, err := conn.Write(want); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := io.ReadFull(conn, got); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("reply = %q", got)
	}
}

func TestDNS(t *testing.T) {
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
	n := testNetwork(t, upstream.LocalAddr().String())
	guest := guestStack(t, n)
	conn, err := gonet.DialUDP(guest, nil, &tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: 53}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	request := new(dns.Msg)
	request.SetQuestion("fixture.test.", dns.TypeA)
	client := dns.Client{}
	reply, _, err := client.ExchangeWithConn(request, &dns.Conn{Conn: conn})
	if err != nil {
		t.Fatal(err)
	}
	if len(reply.Answer) != 1 || reply.Answer[0].(*dns.A).A.String() != "192.0.2.7" {
		t.Fatalf("DNS answer = %v", reply.Answer)
	}
}

func TestCloseAndNonblockingRead(t *testing.T) {
	t.Parallel()
	n := testNetwork(t)
	if _, err := n.Read(make([]byte, 1600)); !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("empty read = %v", err)
	}
	done := make(chan error, 1)
	go func() { done <- n.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("Close blocked")
	}
	if _, err := n.Read(make([]byte, 1600)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed read = %v", err)
	}
	if _, err := n.Write(make([]byte, 64)); !errors.Is(err, net.ErrClosed) {
		t.Fatalf("closed write = %v", err)
	}
}

func TestTCPHalfClose(t *testing.T) {
	t.Parallel()
	n := testNetwork(t)
	guest := guestStack(t, n)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	received := make(chan []byte, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer conn.Close()
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		_, _ = conn.Write([]byte("ready"))
		_ = conn.(*net.TCPConn).CloseWrite()
		data, _ := io.ReadAll(conn)
		received <- data
	}()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: uint16(listener.Addr().(*net.TCPAddr).Port)}
	conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	reply, err := io.ReadAll(conn)
	if err != nil || string(reply) != "ready" {
		t.Fatalf("reply=%q error=%v", reply, err)
	}
	if _, err := conn.Write([]byte("upload after EOF")); err != nil {
		t.Fatal(err)
	}
	if err := conn.CloseWrite(); err != nil {
		t.Fatal(err)
	}
	select {
	case data := <-received:
		if string(data) != "upload after EOF" {
			t.Fatalf("upload=%q", data)
		}
	case <-ctx.Done():
		t.Fatal("half-closed connection stalled")
	}
}

func TestCloseWithIdleDNSClient(t *testing.T) {
	t.Parallel()
	n := testNetwork(t)
	guest := guestStack(t, n)
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: 53}
	conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	defer conn.Close()
	done := make(chan error, 1)
	go func() { done <- n.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Close blocked on idle DNS/TCP client")
	}
}

func TestCloseDuringTCPHandshake(t *testing.T) {
	t.Parallel()
	n := testNetwork(t)
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		conn, acceptErr := listener.Accept()
		if acceptErr == nil {
			accepted <- conn
		}
	}()
	guestMAC := net.HardwareAddr{2, 0, 0, 0, 0, 15}
	gateway, _ := net.ParseMAC(gatewayMAC)
	guestAddress := tcpip.AddrFrom4([4]byte{10, 0, 2, 15})
	if err := n.stack.AddStaticNeighbor(1, ipv4.ProtocolNumber, guestAddress, tcpip.LinkAddress(guestMAC)); err != nil {
		t.Fatal(err)
	}
	eth := &layers.Ethernet{SrcMAC: guestMAC, DstMAC: gateway, EthernetType: layers.EthernetTypeIPv4}
	ip := &layers.IPv4{
		Version: 4, TTL: 64, SrcIP: net.IPv4(10, 0, 2, 15), DstIP: net.ParseIP(Gateway), Protocol: layers.IPProtocolTCP,
	}
	syn := &layers.TCP{
		SrcPort: 12345, DstPort: layers.TCPPort(listener.Addr().(*net.TCPAddr).Port), Seq: 1234, SYN: true, Window: 65535,
	}
	if err := syn.SetNetworkLayerForChecksum(ip); err != nil {
		t.Fatal(err)
	}
	packet := gopacket.NewSerializeBuffer()
	options := gopacket.SerializeOptions{FixLengths: true, ComputeChecksums: true}
	if err := gopacket.SerializeLayers(packet, options, eth, ip, syn); err != nil {
		t.Fatal(err)
	}
	if _, err := n.Write(packet.Bytes()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	select {
	case conn := <-accepted:
		defer conn.Close()
	case <-ctx.Done():
		t.Fatal("host did not accept TCP connection")
	}
	// The guest deliberately never acknowledges the SYN-ACK.
	select {
	case <-n.Ready():
	case <-ctx.Done():
		t.Fatal("no SYN-ACK")
	}
	done := make(chan error, 1)
	go func() { done <- n.Close() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("Close blocked in guest TCP handshake")
	}
}

func TestUDPExpiryUsesActivityInEitherDirection(t *testing.T) {
	t.Parallel()
	synctest.Test(t, testUDPExpiryUsesActivityInEitherDirection)
}

func testUDPExpiryUsesActivityInEitherDirection(t *testing.T) {
	t.Helper()
	guest, client := net.Pipe()
	host, server := net.Pipe()
	defer client.Close()
	defer server.Close()
	done := make(chan struct{})
	go func() { defer close(done); relayUDP(guest, host, 150*time.Millisecond) }()
	// Keep only the guest-to-host direction active for much longer than the idle
	// timeout. The source port/session must survive despite no return traffic.
	for i := 0; i < 12; i++ {
		deadline := time.Now().Add(time.Second)
		_ = client.SetDeadline(deadline)
		_ = server.SetDeadline(deadline)
		if _, err := client.Write([]byte{byte(i)}); err != nil {
			t.Fatalf("active UDP flow expired: %v", err)
		}
		data := make([]byte, 1)
		if _, err := io.ReadFull(server, data); err != nil {
			t.Fatal(err)
		}
		if data[0] != byte(i) {
			t.Fatalf("datagram=%v", data)
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Once both directions become quiet the session still expires.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("idle UDP flow did not expire")
	}
}
