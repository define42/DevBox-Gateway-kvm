// Package gateway serves the authenticated web dashboard and RDP gateway.
package gateway

import (
	"context"
	"errors"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/cert"
)

var (
	ErrNotFound       = errors.New("virtual machine not found")
	ErrConflict       = errors.New("virtual machine operation conflicts with its current state")
	ErrMemoryCapacity = errors.New("insufficient host memory to start desktop")
	ErrInvalid        = errors.New("invalid gateway configuration or request")
	ErrAuthentication = errors.New("authentication failed")
)

// VM describes a persistent desktop and its owner.
type VM struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
	ImageID   string    `json:"imageId,omitempty"`
	ImageName string    `json:"imageName,omitempty"`
	// QCOW2 capacity is fixed at creation, outside guest-writable storage.
	DiskSizeBytes int64 `json:"diskSizeBytes"`
}

// Endpoint identifies a gateway-only private RDP server and the certificate that authenticates it.
type Endpoint struct {
	Address, ServerName string
	CertificatePEM      []byte
}

// Backend manages virtual machines and enforces ownership on every operation.
type Backend interface {
	List(context.Context, string) ([]VM, error)
	Create(ctx context.Context, owner, name, passwordHash string) (VM, error)
	Start(ctx context.Context, owner, id string) error
	Stop(ctx context.Context, owner, id string) error
	Delete(ctx context.Context, owner, id string) error
	RDP(ctx context.Context, owner, id string) (Endpoint, error)
}

// Authenticator validates host credentials and returns a stable canonical owner.
type Authenticator interface {
	Authenticate(ctx context.Context, username, password string) (canonicalUser string, err error)
}

// Identity is the canonical account and the privileges established at login.
// Administrator access is never inferred from an account's name.
type Identity struct {
	Username string
	IsAdmin  bool
}

// IdentityAuthenticator optionally extends authentication with directory roles.
// Authenticators implementing only Authenticator always create ordinary sessions.
type IdentityAuthenticator interface {
	AuthenticateIdentity(ctx context.Context, username, password string) (Identity, error)
}

// Config supplies the public listener, TLS identity, authentication and VM backend.
type Config struct {
	// TLSManager supplies a live certificate shared by HTTPS and RDP.
	TLSManager *cert.Manager
	// Presentation describes the configured desktop resources and login access group.
	DesktopImage, RequiredGroup                  string
	DesktopCPUs, DesktopMemoryMiB                int
	ListenAddr, PublicAddress, CertFile, KeyFile string
	Authenticator                                Authenticator
	Backend                                      Backend
	SessionTTL                                   time.Duration
	MaxImageUploadBytes                          int64
	UploadTimeout, CreationTimeout               time.Duration
	MaxSessions, MaxConnections                  int
	// Pending connections cannot consume the authenticated MaxConnections pool.
	MaxHandshakes int
	// MaxConnectionsPerIP includes pending and authenticated connections;
	// MaxHandshakesPerIP counts only pending connections.
	MaxConnectionsPerIP, MaxHandshakesPerIP int
	// Admission rates are per second, each with a burst equal to its rate.
	ConnectionRateLimit, ConnectionRateLimitPerIP int
	// HandshakeTimeout covers TCP acceptance through application authentication.
	HandshakeTimeout time.Duration
}
