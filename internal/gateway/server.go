package gateway

import (
	"bufio"
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/cert"
)

// Server serves HTTPS and TLS-protected RDP on one TCP listener.
type Server struct {
	config      Config
	tls         *tls.Config
	store       sessionStore
	authSlots   chan struct{}
	served      atomic.Bool
	uploading   atomic.Bool
	mu          sync.Mutex
	listener    net.Listener
	connections map[*trackedConn]struct{}
	admission   connectionAdmission
	requests    sync.WaitGroup
	draining    bool
}

// New validates configuration and loads the HTTPS/RDP certificate.
func New(c Config) (*Server, error) {
	if c.Authenticator == nil || c.Backend == nil || (c.TLSManager == nil && (c.CertFile == "" || c.KeyFile == "")) {
		return nil, fmt.Errorf("%w: authentication, backend, and TLS certificate/key are required", ErrInvalid)
	}
	if c.ListenAddr == "" {
		c.ListenAddr = "127.0.0.1:8443"
	}
	if _, _, err := net.SplitHostPort(c.ListenAddr); err != nil {
		return nil, fmt.Errorf("%w: listen address needs host:port", ErrInvalid)
	}
	host, port, err := net.SplitHostPort(c.PublicAddress)
	portNumber, portErr := strconv.Atoi(port)
	if err != nil ||
		host == "" ||
		portErr != nil ||
		portNumber < 1 ||
		portNumber > 65535 ||
		strings.ContainsAny(
			c.PublicAddress,
			"\r\n\t /\\@?#",
		) {
		return nil, fmt.Errorf("%w: public address needs a hostname and port", ErrInvalid)
	}
	if c.SessionTTL == 0 {
		c.SessionTTL = 8 * time.Hour
	}
	if c.SessionTTL < time.Minute || c.SessionTTL > 24*time.Hour {
		return nil, fmt.Errorf("%w: session lifetime must be one minute to 24 hours", ErrInvalid)
	}
	if c.MaxSessions == 0 {
		c.MaxSessions = 1024
	}
	if c.MaxConnections == 0 {
		c.MaxConnections = 128
	}
	if c.MaxSessions < 1 || c.MaxSessions > 10000 || c.MaxConnections < 1 || c.MaxConnections > 4096 {
		return nil, fmt.Errorf("%w: invalid session/connection limit", ErrInvalid)
	}
	if err := validateProvisioningConfig(&c); err != nil {
		return nil, err
	}
	if err := validateConnectionConfig(&c); err != nil {
		return nil, err
	}
	tlsConfig, err := serverTLSConfig(c)
	if err != nil {
		return nil, err
	}

	return &Server{
		config: c,
		tls:    tlsConfig,
		store:  newSessionStore(),
		authSlots: make(
			chan struct{},
			8,
		),
		connections: make(
			map[*trackedConn]struct{},
		),
	}, nil
}

func serverTLSConfig(c Config) (*tls.Config, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, NextProtos: []string{"http/1.1"}}
	if c.TLSManager != nil {
		if c.CertFile != "" || c.KeyFile != "" {
			return nil, fmt.Errorf("%w: TLS manager and certificate files cannot be combined", ErrInvalid)
		}
		tlsConfig = c.TLSManager.TLSConfig()
	} else {
		certificate, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("load gateway TLS identity: %w", err)
		}
		tlsConfig.Certificates = []tls.Certificate{certificate}
	}

	return tlsConfig, nil
}

// Addr returns the bound listener address once Serve has started.
func (s *Server) Addr() net.Addr {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.listener == nil {
		return nil
	}

	return s.listener.Addr()
}

// Serve runs until cancellation, then closes listeners, HTTP requests and RDP proxies.
func (s *Server) Serve(ctx context.Context) error {
	if !s.served.CompareAndSwap(false, true) {
		return fmt.Errorf("%w: server already started", ErrConflict)
	}
	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", s.config.ListenAddr)
	if err != nil {
		if s.config.TLSManager != nil {
			s.config.TLSManager.Close()
		}

		return err
	}

	return s.serve(ctx, ln)
}

func (s *Server) serve(ctx context.Context, ln net.Listener) error {
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	s.mu.Lock()
	s.listener = ln
	s.mu.Unlock()
	if s.config.TLSManager != nil {
		defer s.config.TLSManager.Close()
		if err := s.config.TLSManager.Start(ctx, ln.Addr()); err != nil {
			_ = ln.Close()

			return err
		}
	}
	https := &channelListener{addr: ln.Addr(), incoming: make(chan net.Conn), done: make(chan struct{})}
	httpServer := &http.Server{
		Handler:           s.routes(),
		ConnContext:       connectionContext,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      2 * time.Minute,
		IdleTimeout:       time.Minute,
		MaxHeaderBytes:    16 << 10,
		BaseContext: func(
			net.Listener,
		) context.Context {
			return ctx
		},
		ErrorLog: log.New(
			discardLog{},
			"",
			0,
		),
	}
	httpDone := make(chan struct{})
	go func() {
		defer close(httpDone)
		_ = httpServer.Serve(https)
		cancel()
		_ = ln.Close()
	}()
	shutdownDone := make(chan struct{})
	stop := context.AfterFunc(ctx, func() {
		defer close(shutdownDone)
		_ = ln.Close()
		_ = https.Close()
		s.mu.Lock()
		s.draining = true
		all := make([]*trackedConn, 0, len(s.connections))
		for conn := range s.connections {
			all = append(all, conn)
		}
		s.mu.Unlock()
		for _, conn := range all {
			_ = conn.Close()
		}
		_ = httpServer.Close()
	})
	defer stop()
	var workers sync.WaitGroup
	defer func() {
		cancel()
		_ = ln.Close()
		_ = https.Close()
		_ = httpServer.Close()
		workers.Wait()
		<-httpDone
		<-shutdownDone
		s.requests.Wait()
		s.store.mu.Lock()
		for key := range s.store.sessions {
			s.store.revokeLocked(key)
		}
		s.store.mu.Unlock()
	}()
	workers.Go(func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case now := <-ticker.C:
				s.store.mu.Lock()
				s.store.pruneLocked(now)
				s.store.mu.Unlock()
				s.mu.Lock()
				s.pruneAdmissionLocked(now)
				s.mu.Unlock()
			}
		}
	})
	for {
		raw, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}

			return err
		}
		conn := s.admitConnection(ctx, raw)
		if conn == nil {
			_ = raw.Close()

			continue
		}
		workers.Go(func() { s.dispatch(conn.ctx, conn, https) })
	}
}

// HTTP TLS handshake diagnostics may contain client-controlled data; request errors
// are rendered explicitly instead of logging credentials or routing tokens.
type discardLog struct{}

func (discardLog) Write(b []byte) (int, error) { return len(b), nil }

func (s *Server) dispatch(ctx context.Context, raw net.Conn, https *channelListener) {
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	handedOff := false
	defer func() {
		if !handedOff {
			_ = raw.Close()
		}
	}()
	_ = raw.SetDeadline(time.Now().Add(s.config.HandshakeTimeout))
	reader := bufio.NewReaderSize(raw, 4096)
	prefix, err := reader.Peek(1)
	if err != nil {
		return
	}
	buffered := &bufferedConn{Conn: raw, reader: reader}
	if prefix[0] == 3 {
		s.handleRDP(ctx, buffered)

		return
	}
	if prefix[0] != 0x16 {
		return
	}
	secure := tls.Server(buffered, s.tls)
	if err := secure.HandshakeContext(ctx); err != nil {
		return
	}
	if secure.ConnectionState().NegotiatedProtocol == cert.ChallengeProtocol {
		return
	}
	_ = secure.SetDeadline(time.Time{})
	select {
	case https.incoming <- secure:
		handedOff = true
	case <-ctx.Done():
	case <-https.done:
	}
}

type bufferedConn struct {
	net.Conn
	reader *bufio.Reader
}

func (c *bufferedConn) Read(b []byte) (int, error) { return c.reader.Read(b) }

type channelListener struct {
	addr     net.Addr
	incoming chan net.Conn
	done     chan struct{}
	once     sync.Once
}

func (l *channelListener) Accept() (net.Conn, error) {
	select {
	case conn := <-l.incoming:
		return conn, nil
	case <-l.done:
		return nil, net.ErrClosed
	}
}

func (l *channelListener) Close() error {
	l.once.Do(func() { close(l.done) })

	return nil
}
func (l *channelListener) Addr() net.Addr { return l.addr }

func backendError(err error) (int, string) {
	switch {
	case errors.Is(err, ErrNotFound):
		return http.StatusNotFound, "That desktop could not be found."
	case errors.Is(err, ErrCgroupUnavailable):
		return http.StatusServiceUnavailable, "The host cannot enforce this desktop's memory and process limits. " +
			"Ask your administrator to configure the delegated VM cgroup."
	case errors.Is(err, syscall.ENOSPC), errors.Is(err, syscall.EDQUOT):
		return http.StatusInsufficientStorage, "Host storage is full or its quota has been reached. " +
			"Contact your administrator; the operation could not be completed."
	case errors.Is(err, ErrDiskTooLarge):
		return http.StatusConflict, "The virtual disk exceeds the maximum size allowed per VM. " +
			"Choose a smaller base image or contact your administrator."
	case errors.Is(err, ErrDiskChanged):
		return http.StatusConflict, "The virtual disk no longer matches its expected format and capacity. " +
			"Contact your administrator."
	case errors.Is(err, ErrMemoryCapacity):
		return http.StatusConflict, "Not enough host memory to start this desktop. " +
			"Stop another desktop or contact your administrator."
	case errors.Is(err, errMemoryCheck):
		return http.StatusServiceUnavailable, "Host memory could not be checked. Please contact your administrator."
	case errors.Is(err, ErrStartupFailed):
		return http.StatusInternalServerError, "The desktop failed to start. Please contact your administrator."
	case errors.Is(err, ErrConflict):
		return http.StatusConflict, "That desktop is busy or has reached a configured limit. Please try again later."
	case errors.Is(err, ErrInvalid):
		return http.StatusBadRequest, "Check the desktop name and try again."
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusRequestTimeout, "The operation timed out. Check the desktop status before trying again."
	default:
		return http.StatusInternalServerError, "The operation could not be completed."
	}
}

func validateProvisioningConfig(c *Config) error {
	if c.MaxImageUploadBytes == 0 {
		c.MaxImageUploadBytes = DefaultMaxImageUploadBytes
	}
	if c.UploadTimeout == 0 {
		c.UploadTimeout = 30 * time.Minute
	}
	if c.MaxImageUploadBytes < 1 || c.MaxImageUploadBytes > 1<<40 ||
		c.UploadTimeout < time.Minute || c.UploadTimeout > 24*time.Hour {
		return fmt.Errorf("%w: invalid image upload limit or timeout", ErrInvalid)
	}
	if c.CreationTimeout == 0 {
		c.CreationTimeout = 30 * time.Minute
	}
	if c.CreationTimeout < time.Minute || c.CreationTimeout > 24*time.Hour {
		return fmt.Errorf("%w: creation timeout must be one minute to 24 hours", ErrInvalid)
	}

	return nil
}
