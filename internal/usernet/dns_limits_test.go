package usernet

import (
	"context"
	"errors"
	"fmt"
	"net"
	"runtime"
	"testing"
	"time"

	"github.com/miekg/dns"
	"gvisor.dev/gvisor/pkg/tcpip"
	"gvisor.dev/gvisor/pkg/tcpip/adapters/gonet"
	"gvisor.dev/gvisor/pkg/tcpip/network/ipv4"
	"gvisor.dev/gvisor/pkg/tcpip/stack"
)

type dnsStubRequest struct {
	data []byte
	peer net.Addr
}

type dnsStub struct {
	conn     net.PacketConn
	requests chan dnsStubRequest
}

func newDNSStub(t *testing.T) *dnsStub {
	t.Helper()
	conn, err := net.ListenPacket("udp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	s := &dnsStub{conn: conn, requests: make(chan dnsStubRequest, maxDNSQueries*2)}
	done := make(chan struct{})
	go func() {
		defer close(done)
		data := make([]byte, dns.MaxMsgSize)
		for {
			count, peer, err := conn.ReadFrom(data)
			if err != nil {
				return
			}
			select {
			case s.requests <- dnsStubRequest{data: append([]byte(nil), data[:count]...), peer: peer}:
			case <-t.Context().Done():
				return
			}
		}
	}()
	t.Cleanup(func() { _ = conn.Close(); <-done })

	return s
}

func (s *dnsStub) next(t *testing.T) dnsStubRequest {
	t.Helper()
	select {
	case request := <-s.requests:
		return request
	case <-time.After(3 * time.Second):
		t.Fatal("upstream DNS query did not arrive")

		return dnsStubRequest{}
	}
}

func (s *dnsStub) reply(t *testing.T, pending dnsStubRequest) {
	t.Helper()
	if err := s.writeReply(pending); err != nil {
		t.Fatal(err)
	}
}

func (s *dnsStub) writeReply(pending dnsStubRequest) error {
	request := new(dns.Msg)
	if err := request.Unpack(pending.data); err != nil {
		return fmt.Errorf("unpack DNS stub request: %w", err)
	}
	data, err := dnsAnswer(request).Pack()
	if err != nil {
		return fmt.Errorf("pack DNS stub reply: %w", err)
	}
	if _, err := s.conn.WriteTo(data, pending.peer); err != nil {
		return fmt.Errorf("write DNS stub reply: %w", err)
	}

	return nil
}

func (s *dnsStub) replyQueued(t *testing.T) {
	t.Helper()
	stop := make(chan struct{})
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			select {
			case <-stop:
				return
			case pending := <-s.requests:
				if err := s.writeReply(pending); err != nil {
					t.Errorf("upstream DNS responder: %v", err)

					return
				}
			}
		}
	}()
	t.Cleanup(func() { close(stop); <-done })
}

func dnsAnswer(request *dns.Msg) *dns.Msg {
	reply := new(dns.Msg).SetReply(request)
	reply.Answer = []dns.RR{&dns.A{
		Hdr: dns.RR_Header{Name: request.Question[0].Name, Rrtype: dns.TypeA, Class: dns.ClassINET, Ttl: 60},
		A:   net.IPv4(192, 0, 2, 7),
	}}

	return reply
}

func dnsServiceForTest(t *testing.T, n *Network) *dnsService {
	t.Helper()
	for _, service := range n.services {
		if s, ok := service.(*dnsService); ok {
			return s
		}
	}
	t.Fatal("network DNS service missing")

	return nil
}

func dnsGuestUDP(t *testing.T, guest *stack.Stack) *gonet.UDPConn {
	t.Helper()
	conn, err := gonet.DialUDP(guest, nil, &tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: 53}, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func dnsGuestTCP(t *testing.T, guest *stack.Stack) *gonet.TCPConn {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
	defer cancel()
	address := tcpip.FullAddress{NIC: 1, Addr: gatewayAddress, Port: 53}
	conn, err := gonet.DialContextTCP(ctx, guest, address, ipv4.ProtocolNumber)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	return conn
}

func dnsQuery(id uint16) *dns.Msg {
	request := new(dns.Msg).SetQuestion(fmt.Sprintf("query-%d.test.", id), dns.TypeA)
	request.Id = id

	return request
}

func waitDNS(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for !condition() {
		if time.Now().After(deadline) {
			t.Fatal("DNS resource state did not settle")
		}
		time.Sleep(time.Millisecond)
	}
}

// Keep this test sequential so a sustained flood's goroutine measurement is not
// affected by unrelated parallel tests. The fake resolver never contacts DNS.
//
//nolint:paralleltest // Measures process-wide goroutine growth during a bounded flood.
func TestDNSSustainedFloodBoundedAndRecovers(t *testing.T) {
	stub := newDNSStub(t)
	n := isolatedTestNetwork(t, stub.conn.LocalAddr().String())
	service := dnsServiceForTest(t, n)
	guest := guestStack(t, n)
	conn := dnsGuestUDP(t, guest)
	client := &dns.Conn{Conn: conn}
	pending := make([]dnsStubRequest, 0, maxDNSQueries)
	peers := make(map[string]struct{})
	for i := range maxDNSQueries {
		if err := client.WriteMsg(dnsQuery(uint16(i))); err != nil {
			t.Fatal(err)
		}
		request := stub.next(t)
		pending = append(pending, request)
		peers[request.peer.String()] = struct{}{}
	}
	if len(peers) != maxDNSQueries {
		t.Fatalf("live upstream sockets = %d, want %d", len(peers), maxDNSQueries)
	}
	before := runtime.NumGoroutine()
	packet, err := dnsQuery(1000).Pack()
	if err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(100 * time.Millisecond)
	packets := 0
	for time.Now().Before(deadline) {
		if _, err := conn.Write(packet); err != nil {
			t.Fatal(err)
		}
		packets++
		if packets%128 == 0 {
			runtime.Gosched()
		}
	}
	if packets < maxDNSQueries*2 {
		t.Fatalf("too few flood packets: %d", packets)
	}
	if extra := runtime.NumGoroutine() - before; extra > 8 {
		t.Fatalf("flood spawned %d additional goroutines while all DNS slots were full", extra)
	}
	select {
	case <-stub.requests:
		t.Fatal("overloaded DNS opened an upstream socket beyond its budget")
	default:
	}
	if got := len(service.queries); got != maxDNSQueries {
		t.Fatalf("active DNS queries = %d, want %d", got, maxDNSQueries)
	}
	// The same ceiling applies to TCP queries; failure is immediate, not queued.
	tcp := &dns.Conn{Conn: dnsGuestTCP(t, guest)}
	_ = tcp.SetDeadline(time.Now().Add(time.Second))
	if err := tcp.WriteMsg(dnsQuery(2000)); err != nil {
		t.Fatal(err)
	}
	reply, err := tcp.ReadMsg()
	if err != nil || reply.Rcode != dns.RcodeServerFailure {
		t.Fatalf("overloaded TCP DNS reply = %v, %v", reply, err)
	}
	for _, request := range pending {
		stub.reply(t, request)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	var received [maxDNSQueries]bool
	for count := 0; count < len(pending); {
		reply, err := client.ReadMsg()
		if err != nil {
			t.Fatal(err)
		}
		if reply.Id == 1000 {
			continue
		}
		if reply.Id >= maxDNSQueries || received[reply.Id] || reply.Rcode != dns.RcodeSuccess || len(reply.Answer) != 1 {
			t.Fatalf("initial DNS reply = %v", reply)
		}
		received[reply.Id] = true
		count++
	}
	// Writes and an empty query budget do not drain the guest's UDP queues.
	// Answer late flood requests as slots reopen, and use a fresh socket so
	// their replies cannot be mistaken for the recovery query's response.
	stub.replyQueued(t)
	recovery := &dns.Conn{Conn: dnsGuestUDP(t, guest)}
	resolver := &dns.Client{Timeout: 100 * time.Millisecond}
	waitDNS(t, func() bool {
		reply, _, err := resolver.ExchangeWithConn(dnsQuery(3000), recovery)
		if err != nil {
			var timeout net.Error
			if !errors.As(err, &timeout) || !timeout.Timeout() {
				t.Fatalf("DNS recovery query failed: %v", err)
			}

			// A recovery datagram can still be dropped behind queued flood work.
			return false
		}
		if reply.Id != 3000 || reply.Rcode != dns.RcodeSuccess || len(reply.Answer) != 1 {
			t.Fatalf("DNS failed to recover after overload: %v", reply)
		}

		return true
	})
	waitDNS(t, func() bool { return len(service.queries) == 0 })
	t.Logf("dropped sustained overload of %d packets with %d upstream sockets", packets, len(peers))
}

func TestDNSTCPClientLimitAndIdleRecovery(t *testing.T) {
	t.Parallel()
	n := isolatedTestNetwork(t)
	service := dnsServiceForTest(t, n)
	guest := guestStack(t, n)
	active := func() int {
		service.listener.mu.Lock()
		defer service.listener.mu.Unlock()

		return len(service.listener.clients)
	}
	clients := make([]*gonet.TCPConn, 0, maxDNSTCPClients)
	for range maxDNSTCPClients {
		clients = append(clients, dnsGuestTCP(t, guest))
	}
	waitDNS(t, func() bool { return active() == maxDNSTCPClients })
	extra := dnsGuestTCP(t, guest)
	_ = extra.SetReadDeadline(time.Now().Add(time.Second))
	if _, err := extra.Read(make([]byte, 1)); err == nil {
		t.Fatal("excess idle DNS client was not closed")
	} else if timeout, ok := errors.AsType[net.Error](err); ok && timeout.Timeout() {
		t.Fatal("excess DNS client waited instead of being rejected")
	}
	_ = clients[0].Close()
	waitDNS(t, func() bool { return active() == maxDNSTCPClients-1 })
	_ = dnsGuestTCP(t, guest)
	waitDNS(t, func() bool { return active() == maxDNSTCPClients })
	waitDNS(t, func() bool { return active() == 0 })
	_ = dnsGuestTCP(t, guest)
	waitDNS(t, func() bool { return active() == 1 })
}

func TestDNSCancellationClosesActiveUpstreams(t *testing.T) {
	t.Parallel()
	stub := newDNSStub(t)
	n := isolatedTestNetwork(t, stub.conn.LocalAddr().String())
	service := dnsServiceForTest(t, n)
	guest := guestStack(t, n)
	for _, conn := range []net.Conn{dnsGuestUDP(t, guest), dnsGuestTCP(t, guest)} {
		if err := (&dns.Conn{Conn: conn}).WriteMsg(dnsQuery(1)); err != nil {
			t.Fatal(err)
		}
		_ = stub.next(t)
	}
	start := time.Now()
	if err := n.Close(); err != nil {
		t.Fatal(err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("DNS cancellation waited for upstream timeout: %s", elapsed)
	}
	if len(service.queries) != 0 {
		t.Fatalf("cancelled DNS retained %d query slots", len(service.queries))
	}
}

func TestDNSTimeoutReleasesQuerySlot(t *testing.T) {
	t.Parallel()
	stub := newDNSStub(t)
	n := isolatedTestNetwork(t, stub.conn.LocalAddr().String())
	service := dnsServiceForTest(t, n)
	guest := guestStack(t, n)
	conn := dnsGuestUDP(t, guest)
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	client := &dns.Conn{Conn: conn}
	if err := client.WriteMsg(dnsQuery(1)); err != nil {
		t.Fatal(err)
	}
	_ = stub.next(t)
	reply, err := client.ReadMsg()
	if err != nil || reply.Rcode != dns.RcodeServerFailure {
		t.Fatalf("timed-out DNS reply = %v, %v", reply, err)
	}
	waitDNS(t, func() bool { return len(service.queries) == 0 })
	if err := client.WriteMsg(dnsQuery(2)); err != nil {
		t.Fatal(err)
	}
	stub.reply(t, stub.next(t))
	reply, err = client.ReadMsg()
	if err != nil || reply.Id != 2 || len(reply.Answer) != 1 {
		t.Fatalf("DNS failed after timeout: %v, %v", reply, err)
	}
}

func TestDNSTruncatedUDPUsesTCPUpstream(t *testing.T) {
	t.Parallel()
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	udp, err := net.ListenPacket("udp4", listener.Addr().String())
	if err != nil {
		_ = listener.Close()
		t.Fatal(err)
	}
	for _, server := range []*dns.Server{
		{PacketConn: udp, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
			reply := new(dns.Msg).SetReply(request)
			reply.Truncated = true
			_ = w.WriteMsg(reply)
		})},
		{Listener: listener, Handler: dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
			_ = w.WriteMsg(dnsAnswer(request))
		})},
	} {
		done := make(chan struct{})
		go func() { defer close(done); _ = server.ActivateAndServe() }()
		t.Cleanup(func() { _ = server.Shutdown(); <-done })
	}
	t.Cleanup(func() { _ = udp.Close(); _ = listener.Close() })
	n := isolatedTestNetwork(t, udp.LocalAddr().String())
	guest := guestStack(t, n)
	for _, conn := range []net.Conn{dnsGuestUDP(t, guest), dnsGuestTCP(t, guest)} {
		_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
		client := dns.Client{}
		reply, _, err := client.ExchangeWithConn(dnsQuery(1), &dns.Conn{Conn: conn})
		if err != nil || len(reply.Answer) != 1 || reply.Truncated {
			t.Fatalf("DNS TCP fallback = %v, %v", reply, err)
		}
	}
}

func TestDNSMalformedRequestsReleaseBudget(t *testing.T) {
	t.Parallel()
	stub := newDNSStub(t)
	n := isolatedTestNetwork(t, stub.conn.LocalAddr().String())
	service := dnsServiceForTest(t, n)
	guest := guestStack(t, n)
	conn := dnsGuestUDP(t, guest)
	_ = conn.SetDeadline(time.Now().Add(3 * time.Second))
	client := &dns.Conn{Conn: conn}
	// A question whose name points back to itself must get FORMERR and never reach the resolver.
	malformed := []byte{0, 1, 0, 0, 0, 1, 0, 0, 0, 0, 0, 0, 0xc0, 0x0c}
	for range maxDNSQueries * 2 {
		if _, err := conn.Write(malformed); err != nil {
			t.Fatal(err)
		}
		reply, err := client.ReadMsg()
		if err != nil || reply.Rcode != dns.RcodeFormatError {
			t.Fatalf("malformed DNS reply = %v, %v", reply, err)
		}
	}
	// Short packets and responses are silently ignored.
	for _, packet := range [][]byte{{1, 2, 3}, {0, 1, 128, 0, 0, 1, 0, 0, 0, 0, 0, 0}} {
		if _, err := conn.Write(packet); err != nil {
			t.Fatal(err)
		}
	}
	waitDNS(t, func() bool { return len(service.queries) == 0 })
	select {
	case <-stub.requests:
		t.Fatal("malformed DNS reached the host resolver")
	default:
	}
	if err := client.WriteMsg(dnsQuery(2)); err != nil {
		t.Fatal(err)
	}
	stub.reply(t, stub.next(t))
	reply, err := client.ReadMsg()
	if err != nil || reply.Id != 2 || len(reply.Answer) != 1 {
		t.Fatalf("DNS did not recover after malformed packets: %v, %v", reply, err)
	}
}
