// Package gateway serves the authenticated web dashboard and RDP gateway.
package gateway

import (
	"context"
	"errors"
	"time"
)

var (
	ErrNotFound       = errors.New("virtual machine not found")
	ErrConflict       = errors.New("virtual machine operation conflicts with its current state")
	ErrInvalid        = errors.New("invalid gateway configuration or request")
	ErrAuthentication = errors.New("authentication failed")
)

// VM describes a virtual machine visible to its owner.
type VM struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Owner     string    `json:"owner"`
	State     string    `json:"state"`
	Error     string    `json:"error,omitempty"`
	CreatedAt time.Time `json:"createdAt"`
}

// Endpoint identifies a private RDP server and the certificate that authenticates it.
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

// Config supplies the public listener, TLS identity, authentication and VM backend.
type Config struct {
	// Presentation describes the configured desktop resources and login access group.
	DesktopImage, RequiredGroup                  string
	DesktopCPUs, DesktopMemoryMiB                int
	ListenAddr, PublicAddress, CertFile, KeyFile string
	Authenticator                                Authenticator
	Backend                                      Backend
	SessionTTL                                   time.Duration
	MaxSessions, MaxConnections                  int
}
