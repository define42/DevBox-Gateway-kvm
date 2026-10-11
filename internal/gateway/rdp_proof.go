package gateway

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"crypto/tls"
	"encoding/binary"
	"fmt"
	"io"
	"time"
)

type rdpProofKey struct{}

// verifyRDPClient relays only the MCS establishment messages that precede Client
// Info. The second bearer proof is sent inside TLS as the RDP username, never in
// the plaintext X.224 routing token. No Client Info or desktop traffic reaches
// the backend until this proof is verified.
func verifyRDPClient(ctx context.Context, front, backend *tls.Conn) error {
	expected, ok := ctx.Value(rdpProofKey{}).([sha256.Size]byte)
	if !ok {
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
	gate := proofHandshake{front: front, backend: backend, remaining: 256 << 10}
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
			if err := verifyClientInfoProof(mcs, expected); err != nil {
				return err
			}
			if err := ctx.Err(); err != nil {
				return err
			}

			return writePacket(backend, packet)
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

type proofHandshake struct {
	front, backend io.ReadWriter
	remaining      int
}

func (g *proofHandshake) read(r io.Reader) ([]byte, []byte, error) {
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

func (g *proofHandshake) connect() error {
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

func (g *proofHandshake) attach() error {
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

func verifyClientInfoProof(mcs []byte, expected [sha256.Size]byte) error {
	if len(mcs) < 7 || mcs[0] != 0x64 || binary.BigEndian.Uint16(mcs[1:3]) != 0 ||
		binary.BigEndian.Uint16(mcs[3:5]) != 1003 || mcs[5] != 0x70 {
		return ErrAuthentication
	}
	n, off := int(mcs[6]), 7
	if n&0x80 != 0 {
		if len(mcs) < 8 {
			return ErrAuthentication
		}
		n, off = (n&0x7f)<<8|int(mcs[7]), 8
	}
	if n != len(mcs)-off {
		return ErrAuthentication
	}
	info := mcs[off:]
	if len(info) < 22 || binary.LittleEndian.Uint32(info[:4]) != 0x40 {
		return ErrAuthentication
	}
	width := 1
	if binary.LittleEndian.Uint32(info[8:12])&0x10 != 0 {
		width = 2
	}
	var username []byte
	off = 22
	for field := 0; field < 5; field++ {
		n := int(binary.LittleEndian.Uint16(info[12+2*field : 14+2*field]))
		if n%width != 0 || n+width > len(info)-off {
			return ErrAuthentication
		}
		if field == 1 {
			username = info[off : off+n]
		}
		off += n
		for _, b := range info[off : off+width] {
			if b != 0 {
				return ErrAuthentication
			}
		}
		off += width
	}
	if len(username) != 64*width {
		return ErrAuthentication
	}
	proof := make([]byte, 64)
	for i := range proof {
		proof[i] = username[i*width]
		if width == 2 && username[i*width+1] != 0 {
			return ErrAuthentication
		}
	}
	actual, valid := tokenKey(string(proof))
	if !valid || subtle.ConstantTimeCompare(actual[:], expected[:]) != 1 {
		return fmt.Errorf("RDP client proof: %w", ErrAuthentication)
	}

	return nil
}
