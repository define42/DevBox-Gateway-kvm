package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/unix"
)

const (
	protocolTLS                  = uint32(1)
	maxAbstractUnixSocketNameLen = len(unix.RawSockaddrUnix{}.Path)
)

func (s *Server) handleRDP(ctx context.Context, raw net.Conn) {
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	packet, err := readConnectionPacket(raw)
	if err != nil {
		return
	}
	token, protocols, err := parseConnectionRequest(packet)
	if err != nil {
		return
	}
	if protocols&protocolTLS == 0 {
		_ = writePacket(raw, connectionConfirm(3))

		return
	}
	if err := writePacket(raw, connectionConfirm(2)); err != nil {
		return
	}
	frontConfig := s.tls.Clone()
	frontConfig.NextProtos = nil
	front := tls.Server(raw, frontConfig)
	if err := front.HandshakeContext(ctx); err != nil {
		return
	}
	publicHost, _, _ := net.SplitHostPort(s.config.PublicAddress)
	if name := front.ConnectionState().ServerName; name != "" && !strings.EqualFold(name, publicHost) {
		return
	}
	ip := clientIP(raw.RemoteAddr().String())
	connCtx, owner, vm, release, ok := s.store.pendingGrant(ctx, token, ip)
	if !ok {
		return
	}
	defer release()
	stopSession := context.AfterFunc(connCtx, func() { _ = raw.Close() })
	defer stopSession()
	setupCtx, cancelSetup := context.WithTimeout(connCtx, 10*time.Second)
	defer cancelSetup()
	// Backend.RDP checks ownership on the selected VM without listing unrelated desktops.
	endpoint, err := s.config.Backend.RDP(setupCtx, owner, vm)
	if err != nil {
		return
	}
	backend, err := dialRDPBackend(setupCtx, endpoint)
	if err != nil {
		return
	}
	defer backend.NetConn().Close()
	stopBackend := context.AfterFunc(connCtx, func() { _ = backend.NetConn().Close() })
	defer stopBackend()
	if connCtx.Err() != nil {
		return
	}
	authenticate := func(authCtx context.Context, credentials rdpCredentials) error {
		if err := s.authenticateRDP(authCtx, ip, owner, credentials); err != nil {
			return err
		}
		if !s.store.claimGrant(token, ip) {
			return ErrAuthentication
		}
		if !s.promoteConnection(trackedConnection(raw)) {
			return ErrAuthentication
		}

		return nil
	}
	if err := verifyRDPClient(setupCtx, front, backend, authenticate); err != nil {
		return
	}
	cancelSetup()
	_ = front.SetDeadline(time.Time{})
	_ = backend.SetDeadline(time.Time{})
	proxyRDP(front, backend)
}

// authenticateRDP validates credentials afresh with the web login authenticator,
// including LDAP access-group checks. A routing token never authenticates a user.
func (s *Server) authenticateRDP(ctx context.Context, ip, owner string, credentials rdpCredentials) error {
	if ctx.Err() != nil || credentials.Username == "" || len(credentials.Username) > 128 ||
		credentials.Password == "" || len(credentials.Password) > 1024 || !s.store.allowLogin(ip) {
		return ErrAuthentication
	}
	select {
	case s.authSlots <- struct{}{}:
		defer func() { <-s.authSlots }()
	default:
		return ErrAuthentication
	}
	// The configured authenticator selects the directory realm, as it does for
	// web login. The client's RDP Domain hint cannot select another directory.
	identity, err := authenticateIdentity(ctx, s.config.Authenticator, credentials.Username, credentials.Password)
	if err != nil || identity.Username != owner || ctx.Err() != nil {
		return ErrAuthentication
	}

	return nil
}

// readConnectionPacket bounds initial X.224 frames by their one-byte length indicator.
func readConnectionPacket(r io.Reader) ([]byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, err
	}
	size := int(binary.BigEndian.Uint16(header[2:]))
	if header[0] != 3 || header[1] != 0 || size < 11 || size > 260 {
		return nil, fmt.Errorf("%w: invalid RDP connection frame", ErrInvalid)
	}
	packet := make([]byte, size)
	copy(packet, header[:])
	if _, err := io.ReadFull(r, packet[4:]); err != nil {
		return nil, err
	}

	return packet, nil
}

func parseConnectionRequest(packet []byte) (string, uint32, error) {
	bad := func() (string, uint32, error) { return "", 0, fmt.Errorf("%w: invalid RDP negotiation", ErrInvalid) }
	if len(
		packet,
	) < 19 ||
		packet[0] != 3 ||
		packet[1] != 0 ||
		int(
			binary.BigEndian.Uint16(
				packet[2:4],
			),
		) != len(
			packet,
		) {
		return bad()
	}
	payload := packet[4:]
	if int(payload[0])+1 != len(payload) || payload[1] != 0xe0 || payload[6] != 0 {
		return bad()
	}
	data := payload[7:]
	token := ""
	if len(data) > 0 && data[0] != 1 {
		end := bytes.Index(data, []byte("\r\n"))
		if end < 0 {
			return bad()
		}
		token = string(data[:end])
		if _, ok := tokenKey(token); !ok {
			return bad()
		}
		data = data[end+2:]
	}
	if len(data) < 8 || data[0] != 1 || binary.LittleEndian.Uint16(data[2:4]) != 8 {
		return bad()
	}
	if data[1]&8 == 0 {
		if len(data) != 8 {
			return bad()
		}
	} else if len(data) != 44 || data[8] != 6 || data[9] != 0 || binary.LittleEndian.Uint16(data[10:12]) != 36 {
		return bad()
	}

	return token, binary.LittleEndian.Uint32(data[4:8]), nil
}

func connectionConfirm(kind byte) []byte {
	packet := []byte{3, 0, 0, 19, 14, 0xd0, 0, 0, 0x12, 0x34, 0, kind, 0, 8, 0, 0, 0, 0, 0}
	// Result 1 selects TLS (response) or requires TLS (failure).
	binary.LittleEndian.PutUint32(packet[15:], protocolTLS)

	return packet
}

func connectionRequest(token string) []byte {
	packet := []byte{3, 0, 0, 0, 0, 0xe0, 0, 0, 0, 0, 0}
	if token != "" {
		packet = append(packet, token...)
		packet = append(packet, '\r', '\n')
	}
	packet = append(packet, 1, 0, 8, 0, 1, 0, 0, 0)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))
	packet[4] = byte(len(packet) - 5)

	return packet
}

func writePacket(w io.Writer, packet []byte) error {
	for len(packet) > 0 {
		n, err := w.Write(packet)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		packet = packet[n:]
	}

	return nil
}

func dialRDPBackend(ctx context.Context, endpoint Endpoint) (*tls.Conn, error) {
	if !validRDPBackendAddress(endpoint.Address) || endpoint.ServerName == "" {
		return nil, fmt.Errorf("%w: backend must have an abstract Unix address and TLS server name", ErrInvalid)
	}
	roots := x509.NewCertPool()
	block, _ := pem.Decode(endpoint.CertificatePEM)
	if block == nil || block.Type != "CERTIFICATE" || !roots.AppendCertsFromPEM(endpoint.CertificatePEM) {
		return nil, fmt.Errorf("%w: missing backend TLS certificate", ErrInvalid)
	}
	pin := sha256.Sum256(block.Bytes)
	config := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: endpoint.ServerName,
		RootCAs:    roots,
		VerifyConnection: func(
			state tls.ConnectionState,
		) error {
			if len(state.PeerCertificates) == 0 || sha256.Sum256(state.PeerCertificates[0].Raw) != pin {
				return fmt.Errorf("%w: backend TLS identity changed", ErrAuthentication)
			}

			return nil
		},
	}
	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "unix", endpoint.Address)
	if err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = raw.Close()
		}
	}()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	deadline := time.Now().Add(10 * time.Second)
	if parent, ok := ctx.Deadline(); ok {
		deadline = minTime(deadline, parent)
	}
	_ = raw.SetDeadline(deadline)
	if err := writePacket(raw, connectionRequest("")); err != nil {
		return nil, err
	}
	response, err := readConnectionPacket(raw)
	if err != nil {
		return nil, err
	}
	if len(
		response,
	) != 19 ||
		response[4] != 14 ||
		response[5] != 0xd0 ||
		response[10] != 0 ||
		response[11] != 2 ||
		binary.LittleEndian.Uint16(
			response[13:15],
		) != 8 ||
		binary.LittleEndian.Uint32(
			response[15:],
		) != protocolTLS {
		return nil, fmt.Errorf("%w: backend did not select RDP TLS", ErrAuthentication)
	}
	secure := tls.Client(raw, config)
	if err := secure.HandshakeContext(ctx); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	keep = true

	return secure, nil
}

func validRDPBackendAddress(address string) bool {
	return len(address) >= 2 &&
		len(address) <= maxAbstractUnixSocketNameLen &&
		address[0] == '@' &&
		!strings.ContainsRune(address, '\x00')
}

type proxyConn struct{ net.Conn }

func (c proxyConn) Read(b []byte) (int, error) {
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Minute))

	return c.Conn.Read(b)
}

func (c proxyConn) Write(b []byte) (int, error) {
	_ = c.SetWriteDeadline(time.Now().Add(30 * time.Second))

	return c.Conn.Write(b)
}

func proxyRDP(front, backend *tls.Conn) {
	var once sync.Once
	closeBoth := func() { once.Do(func() { _ = front.NetConn().Close(); _ = backend.NetConn().Close() }) }
	var copies sync.WaitGroup
	copies.Go(func() { _, _ = io.Copy(proxyConn{Conn: backend}, proxyConn{Conn: front}); closeBoth() })
	_, _ = io.Copy(proxyConn{Conn: front}, proxyConn{Conn: backend})
	closeBoth()
	copies.Wait()
}
