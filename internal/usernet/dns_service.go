package usernet

import (
	"context"
	"encoding/binary"
	"net"
	"sync"
	"time"

	"github.com/miekg/dns"
)

const (
	maxDNSQueries    = 64
	maxDNSTCPClients = 16
	dnsQueryTimeout  = 5 * time.Second
	dnsIOTimeout     = 2 * time.Second
)

type dnsUDPResponse struct {
	data []byte
	peer net.Addr
}

// dnsService admits UDP work before allocating a packet copy or goroutine.
// TCP clients are capped before dns.Server starts their reader goroutines;
// TCP and UDP queries share the upstream budget without waiting for a slot.
type dnsService struct {
	udp       net.PacketConn
	listener  *dnsListener
	tcp       *dns.Server
	servers   []string
	queries   chan struct{}
	responses chan dnsUDPResponse
}

func newDNSService(ctx context.Context, udp net.PacketConn, listener net.Listener, servers []string) *dnsService {
	s := &dnsService{
		udp: udp, listener: &dnsListener{Listener: listener, clients: make(map[*dnsClientConn]struct{})},
		servers: append([]string(nil), servers...), queries: make(chan struct{}, maxDNSQueries),
		responses: make(chan dnsUDPResponse, maxDNSQueries),
	}
	s.tcp = &dns.Server{
		Listener: s.listener, ReadTimeout: dnsIOTimeout, WriteTimeout: dnsIOTimeout,
		IdleTimeout: func() time.Duration { return dnsIOTimeout },
		Handler: dns.HandlerFunc(func(w dns.ResponseWriter, request *dns.Msg) {
			if !s.reserve(ctx) {
				_ = w.WriteMsg(dnsFailure(request))

				return
			}
			defer func() { <-s.queries }()
			_ = w.WriteMsg(s.resolve(ctx, request))
		}),
	}

	return s
}

func (s *dnsService) reserve(ctx context.Context) bool {
	if ctx.Err() != nil {
		return false
	}
	select {
	case s.queries <- struct{}{}:
		return true
	default:
		return false
	}
}

func (s *dnsService) serveUDP(ctx context.Context, run func(func()) bool) {
	data := make([]byte, dns.MaxMsgSize)
	for {
		count, peer, err := s.udp.ReadFrom(data)
		if err != nil {
			return
		}
		if count < 12 || !s.reserve(ctx) {
			continue
		}
		packet := append([]byte(nil), data[:count]...)
		if !run(func() {
			defer func() { <-s.queries }()
			s.handleUDP(ctx, packet, peer)
		}) {
			<-s.queries

			return
		}
	}
}

func (s *dnsService) handleUDP(ctx context.Context, packet []byte, peer net.Addr) {
	// Keep dns.Server's admission policy before parsing variable-length records.
	header := dns.Header{
		Id: binary.BigEndian.Uint16(packet[:2]), Bits: binary.BigEndian.Uint16(packet[2:4]),
		Qdcount: binary.BigEndian.Uint16(packet[4:6]), Ancount: binary.BigEndian.Uint16(packet[6:8]),
		Nscount: binary.BigEndian.Uint16(packet[8:10]), Arcount: binary.BigEndian.Uint16(packet[10:12]),
	}
	action := dns.DefaultMsgAcceptFunc(header)
	if action == dns.MsgIgnore {
		return
	}
	request := new(dns.Msg)
	var reply *dns.Msg
	if action == dns.MsgAccept && request.Unpack(packet) == nil {
		reply = s.resolve(ctx, request)
	} else {
		// Do not copy unparsed sections or attacker-controlled counts to a reply.
		reply = &dns.Msg{MsgHdr: dns.MsgHdr{
			Id: header.Id, Response: true, Opcode: int(header.Bits>>11) & 15,
			Rcode: dns.RcodeFormatError,
		}}
		if action == dns.MsgRejectNotImplemented {
			reply.Rcode = dns.RcodeNotImplemented
		}
	}
	data, err := reply.Pack()
	if err != nil || ctx.Err() != nil {
		return
	}
	select {
	case s.responses <- dnsUDPResponse{data: data, peer: peer}:
	default:
		// A guest that stops receiving must not retain upstream query slots.
	}
}

// One writer owns the shared socket's deadline. Both its queue and every write
// are bounded even if the guest stops consuming replies.
func (s *dnsService) writeUDP(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case response := <-s.responses:
			_ = s.udp.SetWriteDeadline(time.Now().Add(dnsIOTimeout))
			_, _ = s.udp.WriteTo(response.data, response.peer)
		}
	}
}

func (s *dnsService) resolve(ctx context.Context, request *dns.Msg) *dns.Msg {
	ctx, cancel := context.WithTimeout(ctx, dnsQueryTimeout)
	defer cancel()
	for _, server := range s.servers {
		reply, err := exchangeDNS(ctx, request, server, "udp")
		if err != nil {
			continue
		}
		if reply.Truncated {
			reply, err = exchangeDNS(ctx, request, server, "tcp")
			if err != nil {
				continue
			}
		}

		return reply
	}

	return dnsFailure(request)
}

func exchangeDNS(ctx context.Context, request *dns.Msg, address, network string) (*dns.Msg, error) {
	client := dns.Client{Net: network, Timeout: dnsIOTimeout}
	conn, err := client.DialContext(ctx, address)
	if err != nil {
		return nil, err
	}
	defer conn.Close()
	// ExchangeWithConnContext applies deadlines but does not interrupt an
	// established socket when its context is cancelled before the deadline.
	stop := context.AfterFunc(ctx, func() { _ = conn.Close() })
	defer stop()
	reply, _, err := client.ExchangeWithConnContext(ctx, request, conn)

	return reply, err
}

func dnsFailure(request *dns.Msg) *dns.Msg {
	return new(dns.Msg).SetRcode(request, dns.RcodeServerFailure)
}

func (s *dnsService) Close() error {
	_ = s.udp.Close()
	_ = s.listener.Close()
	ctx, cancel := context.WithTimeout(context.Background(), dnsIOTimeout)
	defer cancel()

	return s.tcp.ShutdownContext(ctx)
}

// dnsListener refuses excess clients inside Accept, before dns.Server can
// create a goroutine for each accepted connection. Close also wakes idle peers.
type dnsListener struct {
	net.Listener
	mu      sync.Mutex
	closed  bool
	clients map[*dnsClientConn]struct{}
}

func (l *dnsListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		l.mu.Lock()
		if l.closed || len(l.clients) >= maxDNSTCPClients {
			closed := l.closed
			l.mu.Unlock()
			_ = conn.Close()
			if closed {
				return nil, net.ErrClosed
			}

			continue
		}
		client := &dnsClientConn{Conn: conn, listener: l}
		l.clients[client] = struct{}{}
		l.mu.Unlock()

		return client, nil
	}
}

func (l *dnsListener) Close() error {
	l.mu.Lock()
	l.closed = true
	clients := make([]*dnsClientConn, 0, len(l.clients))
	for client := range l.clients {
		clients = append(clients, client)
	}
	l.mu.Unlock()
	err := l.Listener.Close()
	for _, client := range clients {
		_ = client.Close()
	}

	return err
}

type dnsClientConn struct {
	net.Conn
	listener *dnsListener
	once     sync.Once
}

func (c *dnsClientConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(func() {
		c.listener.mu.Lock()
		delete(c.listener.clients, c)
		c.listener.mu.Unlock()
	})

	return err
}
