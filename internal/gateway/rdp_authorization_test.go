package gateway

import (
	"io"
	"net"
	"testing"
)

func TestRDPAuthorizationDoesNotDependOnInventory(t *testing.T) {
	t.Parallel()
	endpoint := startTestBackend(t, func(conn net.Conn) bool { return proofBackendHandshake(conn, nil) })
	s, backend, cert, raw, _ := startGatewayForTest(t, endpoint)
	backend.mu.Lock()
	backend.listErr = ErrConflict
	backend.mu.Unlock()
	key, _, _ := s.store.session(raw, "127.0.0.1")
	token, proof, err := s.store.grant(key, "mine")
	if err != nil {
		t.Fatal(err)
	}
	front := dialFrontRDP(t, s, cert, token)
	if err := proofClientHandshake(front, proof); err != nil {
		t.Fatal(err)
	}
	if _, err := front.Write([]byte("ready")); err != nil {
		t.Fatal(err)
	}
	var response [5]byte
	if _, err := io.ReadFull(front, response[:]); err != nil || string(response[:]) != "ready" {
		t.Fatalf("RDP depended on unavailable inventory: %q, %v", response, err)
	}
}
