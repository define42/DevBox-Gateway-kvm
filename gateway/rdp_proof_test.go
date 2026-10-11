package gateway

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"errors"
	"net"
	"sync/atomic"
	"testing"
	"time"
)

func mcsPacket(body []byte) []byte {
	packet := append([]byte{3, 0, 0, 0, 2, 0xf0, 0x80}, body...)
	binary.BigEndian.PutUint16(packet[2:4], uint16(len(packet)))

	return packet
}

func proofInfoPacket(proof string, unicode bool) []byte {
	width := 1
	if unicode {
		width = 2
	}
	info := make([]byte, 22+5*width+len(proof)*width)
	info[0] = 0x40
	if unicode {
		info[8] = 0x10
	}
	binary.LittleEndian.PutUint16(info[14:16], uint16(len(proof)*width))
	for i := range len(proof) {
		info[22+width+i*width] = proof[i]
	}
	mcs := []byte{0x64, 0, 0, 3, 0xeb, 0x70}
	if len(info) >= 128 {
		mcs = append(mcs, byte(len(info)>>8)|0x80, byte(len(info)))
	} else {
		mcs = append(mcs, byte(len(info)))
	}

	return mcsPacket(append(mcs, info...))
}

func proofClientHandshake(conn net.Conn, proof string) error {
	g := proofHandshake{remaining: 256 << 10}
	if err := writePacket(conn, mcsPacket([]byte{0x7f, 0x65, 0})); err != nil {
		return err
	}
	if _, _, err := g.read(conn); err != nil {
		return err
	}
	for _, body := range [][]byte{{4, 1, 0, 1, 0}, {0x28}} {
		if err := writePacket(conn, mcsPacket(body)); err != nil {
			return err
		}
	}
	if _, _, err := g.read(conn); err != nil {
		return err
	}
	for _, channel := range []uint16{1001, 1003} {
		body := []byte{0x38, 0, 0, byte(channel >> 8), byte(channel)}
		if err := writePacket(conn, mcsPacket(body)); err != nil {
			return err
		}
		if _, _, err := g.read(conn); err != nil {
			return err
		}
	}

	return writePacket(conn, proofInfoPacket(proof, true))
}

func proofBackendHandshake(conn net.Conn, received *atomic.Bool) bool {
	g := proofHandshake{remaining: 256 << 10}
	if _, _, err := g.read(conn); err != nil {
		return false
	}
	if err := writePacket(conn, mcsPacket([]byte{0x7f, 0x66, 0})); err != nil {
		return false
	}
	for range 2 {
		if _, _, err := g.read(conn); err != nil {
			return false
		}
	}
	if err := writePacket(conn, mcsPacket([]byte{0x2e, 0, 0, 0})); err != nil {
		return false
	}
	for range 2 {
		_, body, err := g.read(conn)
		if err != nil || len(body) != 5 {
			return false
		}
		response := append([]byte{0x3e, 0}, body[1:]...)
		response = append(response, body[3:5]...)
		if err := writePacket(conn, mcsPacket(response)); err != nil {
			return false
		}
	}
	_, body, err := g.read(conn)
	if err != nil || len(body) < 7 || body[0] != 0x64 {
		return false
	}
	if received != nil {
		received.Store(true)
	}

	return true
}

func TestClientInfoProofBoundsAndEncoding(t *testing.T) {
	t.Parallel()
	proof, err := randomToken()
	if err != nil {
		t.Fatal(err)
	}
	expected, _ := tokenKey(proof)
	for _, unicode := range []bool{false, true} {
		t.Run(map[bool]string{false: "ANSI", true: "Unicode"}[unicode], func(t *testing.T) {
			t.Parallel()
			packet := proofInfoPacket(proof, unicode)
			mcs := packet[7:]
			if err := verifyClientInfoProof(mcs, expected); err != nil {
				t.Fatal(err)
			}
			for i := range len(mcs) {
				if err := verifyClientInfoProof(mcs[:i], expected); err == nil {
					t.Fatalf("accepted truncation at %d", i)
				}
			}
			wrong := sha256.Sum256([]byte("different-proof"))
			if err := verifyClientInfoProof(mcs, wrong); !errors.Is(err, ErrAuthentication) {
				t.Fatal("wrong proof accepted")
			}
			for _, offset := range []int{0, 1, 3, 5, 6} {
				altered := bytes.Clone(mcs)
				altered[offset] ^= 1
				if err := verifyClientInfoProof(altered, expected); err == nil {
					t.Fatalf("accepted bad MCS byte %d", offset)
				}
			}
		})
	}
}

func TestRoutingTokenWithoutEncryptedProofNeverReachesDesktop(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"missing", "wrong"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			var received atomic.Bool
			endpoint := startTestBackend(t, func(conn net.Conn) bool { return proofBackendHandshake(conn, &received) })
			s, _, cert, raw, _ := startGatewayForTest(t, endpoint)
			key, _, _ := s.store.session(raw, "127.0.0.1")
			token, _, err := s.store.grant(key, "mine")
			if err != nil {
				t.Fatal(err)
			}
			proof := ""
			if name == "wrong" {
				proof, err = randomToken()
				if err != nil {
					t.Fatal(err)
				}
			}
			front := dialFrontRDP(t, s, cert, token)
			if err := proofClientHandshake(front, proof); err != nil {
				t.Fatal(err)
			}
			// Even a pipelined attempt to send desktop input must remain behind the gate.
			_, _ = front.Write([]byte("attempted desktop input"))
			_ = front.SetReadDeadline(time.Now().Add(time.Second))
			if _, err := front.Read(make([]byte, 1)); err == nil {
				t.Fatal("missing proof opened desktop")
			}
			if received.Load() {
				t.Fatal("backend received ClientInfo before proof verification")
			}
		})
	}
}

func TestProofGateRejectsBackendDesktopBeforeAuthentication(t *testing.T) {
	t.Parallel()
	endpoint := startTestBackend(t, func(conn net.Conn) bool {
		g := proofHandshake{remaining: 256 << 10}
		if _, _, err := g.read(conn); err != nil {
			return false
		}
		_ = writePacket(conn, mcsPacket([]byte{0x68, 0, 0, 3, 0xeb, 0x70, 4, 'd', 'e', 's', 'k'}))

		return false
	})
	s, _, cert, raw, _ := startGatewayForTest(t, endpoint)
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, _, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	front := dialFrontRDP(t, s, cert, token)
	if err := writePacket(front, mcsPacket([]byte{0x7f, 0x65, 0})); err != nil {
		t.Fatal(err)
	}
	_ = front.SetReadDeadline(time.Now().Add(time.Second))
	if n, err := front.Read(make([]byte, 100)); err == nil || n != 0 {
		t.Fatal("pre-authentication desktop bytes escaped")
	}
}

func TestProofPacketReaderRejectsFastPathAndBoundsAllocation(t *testing.T) {
	t.Parallel()
	for _, data := range [][]byte{
		{0, 0, 0, 8, 2, 0xf0, 0x80, 0},
		{3, 0, 0, 7, 2, 0xf0, 0x80},
		{3, 0, 0xff, 0xff},
		{3, 0, 0, 8, 2, 0xf0, 0, 0},
	} {
		g := proofHandshake{remaining: 64}
		if _, _, err := g.read(bytes.NewReader(data)); err == nil {
			t.Fatal("invalid pre-auth packet accepted")
		}
	}
	g := proofHandshake{remaining: 16}
	p := mcsPacket([]byte{0x28})
	if _, _, err := g.read(bytes.NewReader(p)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.read(bytes.NewReader(p)); err != nil {
		t.Fatal(err)
	}
	if _, _, err := g.read(bytes.NewReader(p)); err == nil {
		t.Fatal("handshake byte budget ignored")
	}
}
