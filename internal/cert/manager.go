// Package cert manages the gateway's persistent TLS identity and ACME renewals.
package cert

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/mail"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"

	"github.com/caddyserver/certmagic"
	"go.uber.org/zap"
)

// ChallengeProtocol identifies the ACME TLS-ALPN challenge on the shared listener.
const ChallengeProtocol = "acme-tls/1"

// ErrInvalid identifies an invalid TLS configuration or stored identity.
var ErrInvalid = errors.New("invalid TLS configuration or identity")

// Options selects a supplied identity, a generated identity, or automatic ACME.
// ACME uses TLS-ALPN-01: public TCP port 443 must reach the gateway listener.
type Options struct {
	Hostname, StateDir, CertFile, KeyFile, ACMEEmail, ACMECA string
	ACME                                                     bool
}

// Manager owns an instance's certificate cache and background renewal work.
type Manager struct {
	host            string
	fallback        tls.Certificate
	magic           *certmagic.Config
	cache           *certmagic.Cache
	issuer          *certmagic.ACMEIssuer
	mu              sync.Mutex
	started, closed bool
	cancel          context.CancelFunc
	closeOnce       sync.Once
}

// New validates options and loads or creates an identity without contacting a CA.
func New(opts Options) (*Manager, error) {
	opts.Hostname = strings.ToLower(opts.Hostname)
	if !validHost(opts.Hostname) {
		return nil, fmt.Errorf("%w: TLS hostname must be a hostname or IP address without a port", ErrInvalid)
	}
	if (opts.CertFile == "") != (opts.KeyFile == "") {
		return nil, fmt.Errorf("%w: TLS certificate and key must be supplied together", ErrInvalid)
	}
	if opts.ACME && opts.CertFile != "" {
		return nil, fmt.Errorf("%w: ACME cannot be combined with an explicit TLS certificate and key", ErrInvalid)
	}
	ca, err := validateACME(opts)
	if err != nil {
		return nil, err
	}
	m := &Manager{host: opts.Hostname}
	if opts.CertFile != "" {
		m.fallback, err = loadIdentity(opts.CertFile, opts.KeyFile, opts.Hostname)
	} else {
		if opts.StateDir == "" {
			return nil, fmt.Errorf("%w: TLS state directory is required for automatic certificates", ErrInvalid)
		}
		if err := privateDirectory(opts.StateDir); err != nil {
			return nil, err
		}
		m.fallback, err = persistentIdentity(opts.StateDir, opts.Hostname)
	}
	if err != nil {
		return nil, err
	}
	if opts.ACME {
		storagePath := filepath.Join(opts.StateDir, "acme")
		if err := privateDirectory(storagePath); err != nil {
			return nil, err
		}
		if err := checkStorage(storagePath); err != nil {
			return nil, err
		}
		logger, err := zap.NewProduction()
		if err != nil {
			return nil, fmt.Errorf("initialize certificate logger: %w", err)
		}
		m.cache = certmagic.NewCache(certmagic.CacheOptions{
			GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return m.magic, nil },
			Logger:           logger,
		})
		m.magic = certmagic.New(m.cache, certmagic.Config{
			Storage:           &certmagic.FileStorage{Path: storagePath},
			DefaultServerName: opts.Hostname,
			Logger:            logger,
		})
		m.issuer = certmagic.NewACMEIssuer(m.magic, certmagic.ACMEIssuer{
			CA: ca, Email: opts.ACMEEmail, Agreed: true,
			DisableHTTPChallenge: true, Logger: logger,
		})
		m.magic.Issuers = []certmagic.Issuer{m.issuer}
	}

	return m, nil
}

func validateACME(opts Options) (string, error) {
	if !opts.ACME {
		return "", nil
	}
	if net.ParseIP(opts.Hostname) != nil || !strings.Contains(opts.Hostname, ".") ||
		!certmagic.SubjectQualifiesForPublicCert(opts.Hostname) {
		return "", fmt.Errorf(
			"%w: ACME requires a public DNS hostname; use automatic self-signed TLS for LAN names and IP addresses",
			ErrInvalid,
		)
	}
	ca, err := acmeCA(opts.ACMECA)
	if err != nil {
		return "", err
	}
	if opts.ACMEEmail != "" {
		address, err := mail.ParseAddress(opts.ACMEEmail)
		if err != nil || address.Address != opts.ACMEEmail {
			return "", fmt.Errorf("%w: invalid ACME contact email", ErrInvalid)
		}
	}

	return ca, nil
}

// TLSConfig returns a separate configuration sharing this manager's live identity.
// HTTP/2 is intentionally absent: the gateway dispatches HTTP/1.1 and RDP itself.
func (m *Manager) TLSConfig() *tls.Config {
	protos := []string{"http/1.1"}
	if m.magic != nil {
		protos = append(protos, ChallengeProtocol)
	}

	return &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: protos, GetCertificate: m.getCertificate}
}

func (m *Manager) getCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	if hello.ServerName != "" && !strings.EqualFold(hello.ServerName, m.host) {
		return nil, fmt.Errorf("%w: TLS server name does not match the configured gateway hostname", ErrInvalid)
	}
	challenge := slices.Contains(hello.SupportedProtos, ChallengeProtocol)
	if challenge && (m.magic == nil || hello.ServerName == "" || len(hello.SupportedProtos) != 1) {
		return nil, fmt.Errorf("%w: invalid ACME TLS-ALPN challenge", ErrInvalid)
	}
	// RDP clients often omit SNI. Only the configured hostname may be selected.
	clientHello := *hello
	clientHello.ServerName = m.host
	if m.magic != nil {
		identity, err := m.magic.GetCertificate(&clientHello)
		if err == nil {
			return identity, nil
		}
		// A validation handshake must fail if its challenge is unavailable; an
		// ordinary self-signed certificate can never satisfy ACME validation.
		if challenge {
			return nil, err
		}
	}
	if err := clientHello.SupportsCertificate(&m.fallback); err != nil {
		return nil, fmt.Errorf("TLS client cannot use the gateway certificate: %w", err)
	}

	return &m.fallback, nil
}

// Start begins asynchronous issuance after the shared gateway listener is bound.
// Existing certificates are reused and renewal continues until Close is called.
func (m *Manager) Start(ctx context.Context, addr net.Addr) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return fmt.Errorf("%w: certificate manager is closed", ErrInvalid)
	}
	if m.started {
		return nil
	}
	if m.magic != nil {
		if addr == nil {
			return fmt.Errorf("%w: ACME requires a bound gateway listener", ErrInvalid)
		}
		host, port, err := net.SplitHostPort(addr.String())
		if err != nil {
			return fmt.Errorf("ACME listener address: %w", err)
		}
		portNumber, err := strconv.Atoi(port)
		if err != nil || portNumber < 1 || portNumber > 65535 {
			return fmt.Errorf("%w: invalid ACME listener port", ErrInvalid)
		}
		m.issuer.ListenHost = host
		m.issuer.AltTLSALPNPort = portNumber
		var managedCtx context.Context
		managedCtx, m.cancel = context.WithCancel(ctx)
		if err := m.magic.ManageAsync(managedCtx, []string{m.host}); err != nil {
			m.cancel()

			return fmt.Errorf("start ACME certificate management: %w", err)
		}
	}
	m.started = true

	return nil
}

// Close stops certificate maintenance. It is safe to call repeatedly.
func (m *Manager) Close() {
	m.closeOnce.Do(func() {
		m.mu.Lock()
		m.closed = true
		if m.cancel != nil {
			m.cancel()
		}
		m.mu.Unlock()
		if m.cache != nil {
			m.cache.Stop()
		}
	})
}

func validHost(host string) bool {
	if net.ParseIP(host) != nil {
		return true
	}
	if host == "" || len(host) > 253 {
		return false
	}
	for label := range strings.SplitSeq(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, r := range label {
			if (r < 'a' || r > 'z') && (r < '0' || r > '9') && r != '-' {
				return false
			}
		}
	}

	return true
}

func acmeCA(value string) (string, error) {
	switch value {
	case "", "production", "prod":
		return certmagic.LetsEncryptProductionCA, nil
	case "staging":
		return certmagic.LetsEncryptStagingCA, nil
	default:
		endpoint, err := url.Parse(value)
		if err != nil || endpoint.Host == "" || endpoint.User != nil || endpoint.Fragment != "" ||
			(endpoint.Scheme != "https" && (endpoint.Scheme != "http" || !isLoopback(endpoint.Hostname()))) {
			return "", fmt.Errorf(
				"%w: ACME CA must be production, staging, or an HTTPS directory URL; HTTP is only allowed on loopback",
				ErrInvalid,
			)
		}

		return endpoint.String(), nil
	}
}

func isLoopback(host string) bool {
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)

	return ip != nil && ip.IsLoopback()
}
