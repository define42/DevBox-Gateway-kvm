package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/binary"
	"io"
	"time"
	"unicode/utf16"
	"unicode/utf8"
)

type rdpCredentials struct {
	Username string
	Password string
	Domain   string
}

// verifyRDPClient relays only the MCS establishment messages that precede Client
// Info. Credentials arrive inside TLS and must authenticate before Client Info
// or desktop traffic reaches the backend. The backend never receives the password.
func verifyRDPClient(
	ctx context.Context, front, backend *tls.Conn, authenticate func(context.Context, rdpCredentials) error,
) error {
	if authenticate == nil {
		return ErrAuthentication
	}
	deadline := time.Now().Add(10 * time.Second)
	if parent, ok := ctx.Deadline(); ok {
		deadline = minTime(deadline, parent)
	}
	_ = front.SetDeadline(deadline)
	_ = backend.SetDeadline(deadline)
	stop := context.AfterFunc(ctx, func() {
		_ = front.NetConn().Close()
		_ = backend.NetConn().Close()
	})
	defer stop()
	gate := rdpHandshake{front: front, backend: backend, remaining: 256 << 10}
	if err := gate.connect(); err != nil {
		return err
	}
	if err := gate.attach(); err != nil {
		return err
	}
	joined := make(map[uint16]bool)
	for packets := 0; packets < 34; packets++ {
		packet, mcs, err := gate.read(front)
		if err != nil {
			return err
		}
		if mcs[0] == 0x64 {
			if !joined[1001] || !joined[1003] {
				return ErrAuthentication
			}
			sanitized, err := authenticatedClientInfo(ctx, mcs, authenticate)
			if err != nil {
				return err
			}

			return writePacket(backend, sanitized)
		}
		if len(mcs) != 5 || mcs[0] != 0x38 || binary.BigEndian.Uint16(mcs[1:3]) != 0 {
			return ErrAuthentication
		}
		channel := binary.BigEndian.Uint16(mcs[3:5])
		if joined[channel] {
			return ErrAuthentication
		}
		joined[channel] = true
		if err := writePacket(backend, packet); err != nil {
			return err
		}
		reply, body, err := gate.read(backend)
		if err != nil {
			return err
		}
		want := append([]byte{0x3e, 0}, mcs[1:]...)
		want = append(want, mcs[3:5]...)
		if !bytes.Equal(body, want) {
			return ErrAuthentication
		}
		if err := writePacket(front, reply); err != nil {
			return err
		}
	}

	return ErrAuthentication
}

func authenticatedClientInfo(
	ctx context.Context, mcs []byte, authenticate func(context.Context, rdpCredentials) error,
) ([]byte, error) {
	credentials, sanitized, err := parseClientInfo(mcs)
	if err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := authenticate(ctx, credentials); err != nil {
		return nil, ErrAuthentication
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	return sanitized, nil
}

type rdpHandshake struct {
	front, backend io.ReadWriter
	remaining      int
}

func (g *rdpHandshake) read(r io.Reader) ([]byte, []byte, error) {
	var header [4]byte
	if _, err := io.ReadFull(r, header[:]); err != nil {
		return nil, nil, err
	}
	n := int(binary.BigEndian.Uint16(header[2:4]))
	if header[0] != 3 || header[1] != 0 || n < 8 || n > g.remaining {
		return nil, nil, ErrAuthentication
	}
	g.remaining -= n
	packet := make([]byte, n)
	copy(packet, header[:])
	if _, err := io.ReadFull(r, packet[4:]); err != nil {
		return nil, nil, err
	}
	if !bytes.Equal(packet[4:7], []byte{2, 0xf0, 0x80}) {
		return nil, nil, ErrAuthentication
	}

	return packet, packet[7:], nil
}

func (g *rdpHandshake) connect() error {
	request, body, err := g.read(g.front)
	if err != nil {
		return err
	}
	if !validMCSConnect(body, 0x65) {
		return ErrAuthentication
	}
	if err := writePacket(g.backend, request); err != nil {
		return err
	}
	reply, body, err := g.read(g.backend)
	if err != nil {
		return err
	}
	if !validMCSConnect(body, 0x66) {
		return ErrAuthentication
	}

	return writePacket(g.front, reply)
}

func validMCSConnect(mcs []byte, kind byte) bool {
	if len(mcs) < 3 || mcs[0] != 0x7f || mcs[1] != kind {
		return false
	}
	n, off := int(mcs[2]), 3
	if n&0x80 != 0 {
		count := n & 0x7f
		if count == 0 || count > 2 || len(mcs) < off+count {
			return false
		}
		n = 0
		for _, b := range mcs[off : off+count] {
			n = n<<8 | int(b)
		}
		off += count
	}

	return n == len(mcs)-off
}

func (g *rdpHandshake) attach() error {
	for _, want := range [][]byte{{4, 1, 0, 1, 0}, {0x28}} {
		request, body, err := g.read(g.front)
		if err != nil {
			return err
		}
		if !bytes.Equal(body, want) {
			return ErrAuthentication
		}
		if err := writePacket(g.backend, request); err != nil {
			return err
		}
	}
	reply, body, err := g.read(g.backend)
	if err != nil {
		return err
	}
	if !bytes.Equal(body, []byte{0x2e, 0, 0, 0}) {
		return ErrAuthentication
	}

	return writePacket(g.front, reply)
}

// parseClientInfo validates credentials and returns a password-free TPKT packet.
// Standard Security, smart-card PINs, ambiguous ANSI encodings and malformed
// Unicode are rejected; this listener accepts password authentication over TLS.
func parseClientInfo(mcs []byte) (rdpCredentials, []byte, error) {
	info, err := clientInfoData(mcs)
	if err != nil {
		return rdpCredentials{}, nil, err
	}
	flags := binary.LittleEndian.Uint32(info[8:12])
	if flags&0x00040000 != 0 { // INFO_PASSWORD_IS_SC_PIN
		return rdpCredentials{}, nil, ErrAuthentication
	}
	width := 1
	if flags&0x10 != 0 { // INFO_UNICODE
		width = 2
	}
	var fields [5][]byte
	var passwordOffset int
	off := 22
	for field := range fields {
		n := int(binary.LittleEndian.Uint16(info[12+2*field : 14+2*field]))
		if n%width != 0 || n+width > len(info)-off {
			return rdpCredentials{}, nil, ErrAuthentication
		}
		fields[field] = info[off : off+n]
		if field == 2 {
			passwordOffset = off
		}
		off += n
		for _, b := range info[off : off+width] {
			if b != 0 {
				return rdpCredentials{}, nil, ErrAuthentication
			}
		}
		off += width
	}
	var values [3]string
	for i, limit := range []int{128, 128, 1024} {
		values[i], err = decodeRDPCredential(fields[i], width, limit)
		if err != nil {
			return rdpCredentials{}, nil, err
		}
	}
	if values[1] == "" || values[2] == "" {
		return rdpCredentials{}, nil, ErrAuthentication
	}
	credentials := rdpCredentials{Domain: values[0], Username: values[1], Password: values[2]}
	sanitized := stripClientInfoPassword(mcs, info, passwordOffset, len(fields[2]))
	clear(fields[2])

	return credentials, sanitized, nil
}

func clientInfoData(mcs []byte) ([]byte, error) {
	if len(mcs) < 7 || mcs[0] != 0x64 || binary.BigEndian.Uint16(mcs[1:3]) != 0 ||
		binary.BigEndian.Uint16(mcs[3:5]) != 1003 || mcs[5] != 0x70 {
		return nil, ErrAuthentication
	}
	n, off := int(mcs[6]), 7
	if n&0x80 != 0 {
		if len(mcs) < 8 {
			return nil, ErrAuthentication
		}
		n, off = (n&0x7f)<<8|int(mcs[7]), 8
	}
	if n != len(mcs)-off {
		return nil, ErrAuthentication
	}
	info := mcs[off:]
	if len(info) < 22 || binary.LittleEndian.Uint32(info[:4]) != 0x40 {
		return nil, ErrAuthentication
	}

	return info, nil
}

func decodeRDPCredential(encoded []byte, width, limit int) (string, error) {
	if len(encoded) > limit*width {
		return "", ErrAuthentication
	}
	decoded := make([]byte, 0, len(encoded))
	for off := 0; off < len(encoded); {
		r, n, err := rdpCredentialRune(encoded[off:], width)
		if err != nil {
			return "", err
		}
		off += n
		if r == 0 {
			return "", ErrAuthentication
		}
		decoded = utf8.AppendRune(decoded, r)
		if len(decoded) > limit {
			return "", ErrAuthentication
		}
	}

	return string(decoded), nil
}

func rdpCredentialRune(encoded []byte, width int) (rune, int, error) {
	if width == 1 {
		if encoded[0] >= utf8.RuneSelf {
			return 0, 0, ErrAuthentication
		}

		return rune(encoded[0]), 1, nil
	}
	r := rune(binary.LittleEndian.Uint16(encoded[:2]))
	if !utf16.IsSurrogate(r) {
		return r, 2, nil
	}
	if r > 0xdbff || len(encoded) < 4 {
		return 0, 0, ErrAuthentication
	}
	low := rune(binary.LittleEndian.Uint16(encoded[2:4]))
	if low < 0xdc00 || low > 0xdfff {
		return 0, 0, ErrAuthentication
	}

	return utf16.DecodeRune(r, low), 4, nil
}

func stripClientInfoPassword(mcs, info []byte, passwordOffset, passwordLength int) []byte {
	cleanInfo := make([]byte, len(info)-passwordLength)
	copy(cleanInfo, info[:passwordOffset])
	copy(cleanInfo[passwordOffset:], info[passwordOffset+passwordLength:])
	binary.LittleEndian.PutUint16(cleanInfo[16:18], 0)
	flags := binary.LittleEndian.Uint32(cleanInfo[8:12])
	flags &^= 0x8 | 0x00100000 // INFO_AUTOLOGON | INFO_USING_SAVED_CREDS
	binary.LittleEndian.PutUint32(cleanInfo[8:12], flags)

	packet := append([]byte{3, 0, 0, 0, 2, 0xf0, 0x80}, mcs[:6]...)
	if len(cleanInfo) >= 128 {
		packet = append(packet, byte(len(cleanInfo)>>8)|0x80, byte(len(cleanInfo)))
	} else {
		packet = append(packet, byte(len(cleanInfo)))
	}
	packet = append(packet, cleanInfo...)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))

	return packet
}
