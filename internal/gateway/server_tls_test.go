package gateway

import (
	"bufio"
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/cert"
)

func TestManagedIdentityServesHTTPSAndRDPWithoutSNI(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "tls")
	manager, err := cert.New(cert.Options{Hostname: "gateway.test", StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	server, err := New(
		Config{
			TLSManager:    manager,
			PublicAddress: "gateway.test:443",
			Backend:       &webBackend{},
			Authenticator: testAuthenticator(
				func(context.Context, string, string) (string, error) { return "alice", nil },
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	addr := runTLSTestServer(t, server)
	certPEM, err := os.ReadFile(filepath.Join(dir, "gateway.crt"))
	if err != nil {
		t.Fatal(err)
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(certPEM) {
		t.Fatal("generated certificate missing")
	}
	config := &tls.Config{
		RootCAs:    roots,
		ServerName: "gateway.test",
		MinVersion: tls.VersionTLS12,
		NextProtos: []string{"http/1.1"},
	}
	https, err := tls.DialWithDialer(&net.Dialer{Timeout: time.Second}, "tcp", addr, config)
	if err != nil {
		t.Fatal(err)
	}
	defer https.Close()
	request := "GET /login HTTP/1.1\r\nHost: gateway.test:443\r\nConnection: close\r\n\r\n"
	if _, err := fmt.Fprint(https, request); err != nil {
		t.Fatal(err)
	}
	response, err := http.ReadResponse(bufio.NewReader(https), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("HTTPS login returned %d", response.StatusCode)
	}
	expected := https.ConnectionState().PeerCertificates[0].Raw

	raw, err := net.DialTimeout("tcp", addr, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if err := raw.SetDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if err := writePacket(raw, connectionRequest("")); err != nil {
		t.Fatal(err)
	}
	if _, err := readConnectionPacket(raw); err != nil {
		t.Fatal(err)
	}
	// Missing SNI is common in RDP. Verify the configured name explicitly instead.
	//nolint:gosec // Equivalent hostname and chain verification below, with no ClientHello SNI.
	rdp := tls.Client(
		raw,
		&tls.Config{
			MinVersion:         tls.VersionTLS12,
			InsecureSkipVerify: true,
			VerifyConnection: func(state tls.ConnectionState) error {
				if len(state.PeerCertificates) == 0 {
					return fmt.Errorf("%w: missing certificate", ErrAuthentication)
				}
				_, err := state.PeerCertificates[0].Verify(x509.VerifyOptions{DNSName: "gateway.test", Roots: roots})

				return err
			},
		},
	)
	if err := rdp.HandshakeContext(t.Context()); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(expected, rdp.ConnectionState().PeerCertificates[0].Raw) {
		t.Fatal("RDP and HTTPS used different identities")
	}
}

func TestACMEChallengeConnectionClosesBeforeHTTPDispatch(t *testing.T) {
	t.Parallel()
	_, _, certFile, keyFile := testTLSIdentity(t, "gateway.test")
	server, err := New(
		Config{
			CertFile:      certFile,
			KeyFile:       keyFile,
			PublicAddress: "gateway.test:443",
			Backend:       &webBackend{},
			Authenticator: testAuthenticator(
				func(context.Context, string, string) (string, error) { return "alice", nil },
			),
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	// This test isolates dispatch; CertMagic challenge certificate selection is
	// exercised by the certificate package and local-CA integration validation.
	server.tls.NextProtos = []string{cert.ChallengeProtocol}
	addr := runTLSTestServer(t, server)
	//nolint:gosec // Isolated dispatch test with a generated test identity.
	connection, err := tls.DialWithDialer(
		&net.Dialer{Timeout: time.Second},
		"tcp",
		addr,
		&tls.Config{
			MinVersion:         tls.VersionTLS12,
			ServerName:         "gateway.test",
			NextProtos:         []string{cert.ChallengeProtocol},
			InsecureSkipVerify: true,
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer connection.Close()
	if connection.ConnectionState().NegotiatedProtocol != cert.ChallengeProtocol {
		t.Fatal("ACME protocol was not negotiated")
	}
	if err := connection.SetReadDeadline(time.Now().Add(time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := connection.Read(make([]byte, 1)); !errors.Is(err, io.EOF) {
		t.Fatalf("challenge connection must close without waiting for HTTP: %v", err)
	}
}

func runTLSTestServer(t *testing.T, server *Server) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan error, 1)
	go func() { done <- server.serve(ctx, listener) }()
	t.Cleanup(func() {
		cancel()
		select {
		case err := <-done:
			if err != nil {
				t.Error(err)
			}
		case <-time.After(3 * time.Second):
			t.Error("TLS test gateway did not stop")
		}
	})

	return listener.Addr().String()
}
