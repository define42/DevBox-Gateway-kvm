package cert

import (
	"bytes"
	"context"
	"crypto"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/pem"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"slices"
	"sync/atomic"
	"testing"
	"time"

	"github.com/caddyserver/certmagic"
)

func TestGeneratedIdentityPersistsAndCoversLANHosts(t *testing.T) {
	t.Parallel()
	for _, host := range []string{"localhost", "gateway.lan", "192.168.1.42", "::1"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			opts := Options{Hostname: host, StateDir: filepath.Join(t.TempDir(), "tls")}
			first, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			first.Close()
			original := bytes.Clone(first.fallback.Certificate[0])
			again, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer again.Close()
			if !bytes.Equal(original, again.fallback.Certificate[0]) {
				t.Fatal("restart replaced the identity")
			}
			if err := again.fallback.Leaf.VerifyHostname(host); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"gateway.crt", "gateway.key"} {
				info, err := os.Stat(filepath.Join(opts.StateDir, name))
				if err != nil {
					t.Fatal(err)
				}
				if info.Mode().Perm() != 0o600 {
					t.Fatalf("%s permissions: %v", name, info.Mode())
				}
			}
		})
	}
}

func TestGeneratedIdentityUpdatesHostnameAndExpiryWithoutReplacingKey(t *testing.T) {
	t.Parallel()
	for _, update := range []string{"hostname", "expiry"} {
		t.Run(update, func(t *testing.T) {
			t.Parallel()
			opts := Options{Hostname: "localhost", StateDir: filepath.Join(t.TempDir(), "tls")}
			first, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			first.Close()
			before := readFile(t, filepath.Join(opts.StateDir, "gateway.key"))
			if update == "hostname" {
				opts.Hostname = "desktop.example.com"
			} else {
				leaf := *first.fallback.Leaf
				leaf.NotBefore, leaf.NotAfter = time.Now().Add(-2*time.Hour), time.Now().Add(-time.Hour)
				key := first.fallback.PrivateKey.(crypto.Signer)
				der, err := x509.CreateCertificate(rand.Reader, &leaf, &leaf, key.Public(), key)
				if err != nil {
					t.Fatal(err)
				}
				certificate := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
				writeFile(t, filepath.Join(opts.StateDir, "gateway.crt"), certificate)
			}
			renewed, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			defer renewed.Close()
			if !bytes.Equal(before, readFile(t, filepath.Join(opts.StateDir, "gateway.key"))) {
				t.Fatal("private key changed")
			}
			if err := renewed.fallback.Leaf.VerifyHostname(opts.Hostname); err != nil {
				t.Fatal(err)
			}
			if !renewed.fallback.Leaf.NotAfter.After(time.Now().Add(300 * 24 * time.Hour)) {
				t.Fatal("certificate was not renewed")
			}
		})
	}
}

func TestSuppliedIdentityIsValidatedWithoutModification(t *testing.T) {
	t.Parallel()
	dir := filepath.Join(t.TempDir(), "tls")
	generated, err := New(Options{Hostname: "gateway.test", StateDir: dir})
	if err != nil {
		t.Fatal(err)
	}
	generated.Close()
	opts := Options{
		Hostname: "gateway.test",
		CertFile: filepath.Join(dir, "gateway.crt"),
		KeyFile:  filepath.Join(dir, "gateway.key"),
	}
	original := readFile(t, opts.CertFile)
	supplied, err := New(opts)
	if err != nil {
		t.Fatal(err)
	}
	supplied.Close()
	opts.Hostname = "elsewhere.test"
	if manager, err := New(opts); err == nil {
		manager.Close()
		t.Fatal("mismatched explicit certificate accepted")
	}
	if !bytes.Equal(original, readFile(t, opts.CertFile)) {
		t.Fatal("supplied certificate was changed")
	}
}

func TestInvalidCertificateOptions(t *testing.T) {
	t.Parallel()
	cases := []struct {
		name    string
		options Options
	}{
		{"empty host", Options{}},
		{"URL", Options{Hostname: "https://gateway.example.com"}},
		{"wildcard", Options{Hostname: "*.example.com"}},
		{"port", Options{Hostname: "gateway.example.com:443"}},
		{"invalid label", Options{Hostname: "-gateway.example.com"}},
		{"missing key", Options{Hostname: "gateway.example.com", CertFile: "cert"}},
		{"mixed modes", Options{Hostname: "gateway.example.com", CertFile: "cert", KeyFile: "key", ACME: true}},
		{"ACME IP", Options{Hostname: "8.8.8.8", ACME: true}},
		{"ACME LAN", Options{Hostname: "gateway.local", ACME: true}},
		{"ACME single label", Options{Hostname: "gateway", ACME: true}},
		{"invalid email", Options{Hostname: "gateway.example.com", ACME: true, ACMEEmail: "Admin <admin@example.com>"}},
		{
			"insecure CA",
			Options{Hostname: "gateway.example.com", ACME: true, ACMECA: "http://ca.example.com/directory"},
		},
		{
			"CA credentials",
			Options{Hostname: "gateway.example.com", ACME: true, ACMECA: "https://user@ca.example.com/directory"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			tc.options.StateDir = filepath.Join(t.TempDir(), "tls")
			if manager, err := New(tc.options); err == nil {
				manager.Close()
				t.Fatal("invalid options accepted")
			}
		})
	}
}

func TestUnsafeOrIncompleteIdentityFailsWithoutReplacingFiles(t *testing.T) {
	t.Parallel()
	mutations := []string{"missing key", "corrupt cert", "mismatched key", "public key", "symlink", "public directory"}
	for _, mutation := range mutations {
		t.Run(mutation, func(t *testing.T) {
			t.Parallel()
			opts := Options{Hostname: "gateway.test", StateDir: filepath.Join(t.TempDir(), "tls")}
			first, err := New(opts)
			if err != nil {
				t.Fatal(err)
			}
			first.Close()
			certFile, keyFile := filepath.Join(
				opts.StateDir,
				"gateway.crt",
			), filepath.Join(
				opts.StateDir,
				"gateway.key",
			)
			switch mutation {
			case "missing key":
				if err := os.Remove(keyFile); err != nil {
					t.Fatal(err)
				}
			case "corrupt cert":
				writeFile(t, certFile, []byte("broken certificate"))
			case "mismatched key":
				_, key, err := newIdentity("gateway.test")
				if err != nil {
					t.Fatal(err)
				}
				writeFile(t, keyFile, key)
			case "public key":
				if err := os.Chmod(keyFile, 0o644); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Rename(keyFile, keyFile+".real"); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(keyFile+".real", keyFile); err != nil {
					t.Fatal(err)
				}
			case "public directory":
				if err := os.Chmod(opts.StateDir, 0o755); err != nil {
					t.Fatal(err)
				}
			}
			original := readFile(t, certFile)
			if manager, err := New(opts); err == nil {
				manager.Close()
				t.Fatal("unsafe/incomplete identity accepted")
			}
			if !bytes.Equal(original, readFile(t, certFile)) {
				t.Fatal("certificate replaced after failed validation")
			}
		})
	}
}

func TestTLSSelectsOnlyConfiguredHostAndCompatibleAlgorithms(t *testing.T) {
	t.Parallel()
	for _, acme := range []bool{false, true} {
		for _, tc := range []struct {
			name, serverName string
			protos           []string
			cipher           uint16
			ok               bool
		}{
			{name: "HTTPS", serverName: "gateway.example.com", protos: []string{"http/1.1"}, ok: true},
			{name: "RDP without SNI", ok: true},
			{name: "foreign SNI", serverName: "other.example.com"},
			{name: "RSA only", serverName: "gateway.example.com", cipher: tls.TLS_ECDHE_RSA_WITH_AES_128_GCM_SHA256},
			{name: "unavailable challenge", serverName: "gateway.example.com", protos: []string{ChallengeProtocol}},
			{name: "challenge without SNI", protos: []string{ChallengeProtocol}},
			{
				name: "mixed challenge protocols", serverName: "gateway.example.com",
				protos: []string{ChallengeProtocol, "http/1.1"},
			},
		} {
			t.Run(tc.name+map[bool]string{false: " static", true: " ACME"}[acme], func(t *testing.T) {
				t.Parallel()
				manager, err := New(
					Options{
						Hostname: "gateway.example.com",
						StateDir: filepath.Join(t.TempDir(), "tls"),
						ACME:     acme,
						ACMECA:   map[bool]string{true: "http://127.0.0.1:1/directory"}[acme],
					},
				)
				if err != nil {
					t.Fatal(err)
				}
				defer manager.Close()
				//nolint:gosec // Test certificate selection, including missing SNI.
				clientConfig := &tls.Config{
					MinVersion:         tls.VersionTLS12,
					MaxVersion:         tls.VersionTLS12,
					ServerName:         tc.serverName,
					NextProtos:         tc.protos,
					InsecureSkipVerify: true,
				}
				if tc.cipher != 0 {
					clientConfig.CipherSuites = []uint16{tc.cipher}
				}
				state, err := handshake(t, manager.TLSConfig(), clientConfig)
				if (err == nil) != tc.ok {
					t.Fatalf("handshake error %v; want success %v", err, tc.ok)
				}
				if tc.ok && !bytes.Equal(state.PeerCertificates[0].Raw, manager.fallback.Certificate[0]) {
					t.Fatal("unexpected TLS certificate")
				}
			})
		}
	}
}

func TestACMEStartsAfterListenerAndStopsIdempotently(t *testing.T) {
	t.Parallel()
	var requests atomic.Int32
	requested := make(chan struct{}, 1)
	ca := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case requested <- struct{}{}:
		default:
		}
		http.Error(w, "test CA unavailable", http.StatusServiceUnavailable)
	}))
	defer ca.Close()
	manager, err := New(
		Options{
			Hostname: "gateway.example.com",
			StateDir: filepath.Join(t.TempDir(), "tls"),
			ACME:     true,
			ACMECA:   ca.URL + "/directory",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if requests.Load() != 0 {
		t.Fatal("New contacted the CA before the listener was ready")
	}
	if err := manager.Start(t.Context(), nil); err == nil {
		t.Fatal("ACME started without a bound listener")
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	if err := manager.Start(t.Context(), listener.Addr()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-requested:
	case <-time.After(3 * time.Second):
		t.Fatal("ACME did not start asynchronously")
	}
	manager.Close()
	manager.Close()
	if err := manager.Start(t.Context(), listener.Addr()); err == nil {
		t.Fatal("closed manager restarted")
	}
}

func TestACMECAAndTLSConfiguration(t *testing.T) {
	t.Parallel()
	cases := map[string]string{
		"":                                 certmagic.LetsEncryptProductionCA,
		"prod":                             certmagic.LetsEncryptProductionCA,
		"production":                       certmagic.LetsEncryptProductionCA,
		"staging":                          certmagic.LetsEncryptStagingCA,
		"https://ca.example.com/directory": "https://ca.example.com/directory",
	}
	for name, want := range cases {
		if got, err := acmeCA(name); err != nil || got != want {
			t.Fatalf("CA %q: %q, %v", name, got, err)
		}
	}
	manager, err := New(
		Options{Hostname: "gateway.example.com", StateDir: filepath.Join(t.TempDir(), "tls"), ACME: true},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	config := manager.TLSConfig()
	if config.MinVersion != tls.VersionTLS12 ||
		!slices.Equal(config.NextProtos, []string{"http/1.1", ChallengeProtocol}) {
		t.Fatal("unexpected TLS protocol configuration")
	}
	if manager.magic.OnDemand != nil || !manager.issuer.DisableHTTPChallenge {
		t.Fatal("unrestricted issuance or HTTP challenge enabled")
	}
	config.NextProtos[0] = "changed"
	if manager.TLSConfig().NextProtos[0] != "http/1.1" {
		t.Fatal("TLS configs share mutable protocol slice")
	}
}

func handshake(t *testing.T, serverConfig, clientConfig *tls.Config) (tls.ConnectionState, error) {
	t.Helper()
	serverRaw, clientRaw := net.Pipe()
	defer clientRaw.Close()
	server := tls.Server(serverRaw, serverConfig)
	client := tls.Client(clientRaw, clientConfig)
	ctx, cancel := context.WithTimeout(t.Context(), time.Second)
	defer cancel()
	finished := make(chan error, 1)
	go func() { defer serverRaw.Close(); finished <- server.HandshakeContext(ctx) }()
	err := client.HandshakeContext(ctx)
	<-finished

	return client.ConnectionState(), err
}

func writeFile(t *testing.T, path string, contents []byte) {
	t.Helper()
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
}

func readFile(t *testing.T, path string) []byte {
	t.Helper()
	contents, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}

	return contents
}

func TestDormantACMESettingsDoNotAffectStaticTLS(t *testing.T) {
	t.Parallel()
	manager, err := New(
		Options{
			Hostname:  "localhost",
			StateDir:  filepath.Join(t.TempDir(), "tls"),
			ACMECA:    "production",
			ACMEEmail: "unused@example.com",
		},
	)
	if err != nil {
		t.Fatal(err)
	}
	defer manager.Close()
	if manager.magic != nil || slices.Contains(manager.TLSConfig().NextProtos, ChallengeProtocol) {
		t.Fatal("disabled ACME was initialized")
	}
}
