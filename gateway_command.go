package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/cert"
	"github.com/define42/DevBox-Gateway-kvm/internal/config"
	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
)

var errCommandUsage = errors.New("invalid command arguments")

func runServe(ctx context.Context, args []string, output io.Writer) (serveErr error) {
	options, err := readServeOptions(args, output)
	if err != nil {
		return err
	}
	manager, config, ldap := options.manager, options.gateway, options.ldap
	certificates, authFile := options.certificates, options.authFile
	if authFile != "" {
		config.Authenticator, err = gateway.NewStaticAuthenticator(authFile)
	} else {
		config.RequiredGroup = ldap.RequiredGroup
		config.Authenticator, err = gateway.NewLDAPAuthenticator(ldap)
	}
	if err != nil {
		return err
	}
	if err := serveIdentity(manager.DataDir, &config, certificates); err != nil {
		return err
	}
	defer config.TLSManager.Close()
	backend, err := gateway.NewManager(manager)
	if err != nil {
		return err
	}
	defer func() { serveErr = errors.Join(serveErr, backend.Close()) }()
	config.Backend = backend
	server, err := gateway.New(config)
	if err != nil {
		return err
	}
	log.Printf("Gateway: https://%s (HTTPS and RDP share %s)", config.PublicAddress, config.ListenAddr)

	return server.Serve(ctx)
}

// serveOptions keeps environment loading and argument parsing separate from startup I/O.
type serveOptions struct {
	manager      gateway.ManagerConfig
	gateway      gateway.Config
	ldap         gateway.LDAPConfig
	certificates cert.Options
	authFile     string
}

// readServeOptions applies defaults, then environment values, then explicit flags.
func readServeOptions(args []string, output io.Writer) (serveOptions, error) {
	s := config.NewSettings()
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(output)
	flags.Usage = func() {
		fmt.Fprintln(output, "Usage: gokvm serve [flags]")
		fmt.Fprintln(output, "Configuration is read from environment variables; explicit flags override them.")
		flags.PrintDefaults()
	}
	var options serveOptions
	maxDiskSize := s.Get(config.VM_DISK_MAX_SIZE)
	bindServeFlags(flags, s, &options, &maxDiskSize)
	if err := parseCommand(flags, args); err != nil {
		return serveOptions{}, err
	}
	maxDiskBytes, err := config.ParseByteSize(maxDiskSize)
	if err != nil {
		return serveOptions{}, fmt.Errorf(
			"VM_DISK_MAX_SIZE (-vm-disk-max-size): %s: %w",
			err.Error(),
			errCommandUsage,
		)
	}
	options.manager.MaxDiskBytes = maxDiskBytes
	if (options.authFile == "") == (options.ldap.URL == "") {
		return serveOptions{}, fmt.Errorf(
			"choose either LDAP_URL (-ldap-url) or AUTH_FILE (-auth-file): %w", errCommandUsage,
		)
	}
	if options.authFile != "" && !loopbackAddress(options.gateway.ListenAddr) {
		return serveOptions{}, fmt.Errorf("AUTH_FILE (-auth-file) is restricted to a loopback listener: %w", errCommandUsage)
	}
	if options.manager.BaseImage != "" {
		options.gateway.DesktopImage = filepath.Base(options.manager.BaseImage)
	}
	options.gateway.DesktopCPUs = options.manager.CPUs
	options.gateway.DesktopMemoryMiB = options.manager.MemoryMiB
	options.gateway.MaxImageUploadBytes = options.manager.MaxImageUploadBytes

	return options, nil
}

// Keep legacy flags as overrides; defaults and descriptions live in internal/config.
func bindServeFlags(flags *flag.FlagSet, s *config.Settings, options *serveOptions, maxDiskSize *string) {
	description := func(key string) string { return s.Description(key) + " [" + key + "]" }
	stringFlag := func(target *string, name, key string) {
		flags.StringVar(target, name, s.Get(key), description(key))
	}
	intFlag := func(target *int, name, key string) {
		flags.IntVar(target, name, s.Int(key), description(key))
	}
	boolFlag := func(target *bool, name, key string) {
		flags.BoolVar(target, name, s.Bool(key), description(key))
	}
	durationFlag := func(target *time.Duration, name, key string) {
		flags.DurationVar(target, name, s.Duration(key), description(key))
	}
	stringFlag(&options.manager.DataDir, "state", config.DATA_ROOT_DIR)
	stringFlag(&options.manager.BaseImage, "image", config.BASE_IMAGE)
	stringFlag(&options.manager.Binary, "vm-binary", config.VM_BINARY)
	stringFlag(&options.manager.Firmware, "uefi", config.UEFI_CODE)
	stringFlag(&options.manager.VarsTemplate, "uefi-vars-template", config.UEFI_VARS_TEMPLATE)
	stringFlag(&options.manager.Encoder, "encoder", config.H264_ENCODER)
	stringFlag(&options.manager.RDPProfile, "rdp-profile", config.RDP_PROFILE)
	boolFlag(&options.manager.VMProfiling, "vm-profiling", config.GATEWAY_VM_PROFILING)
	stringFlag(&options.manager.Keyboard, "keyboard", config.VM_KEYBOARD)
	intFlag(&options.manager.CPUs, "cpus", config.VM_VCPU_COUNT)
	intFlag(&options.manager.MemoryMiB, "memory", config.VM_MEMORY_MIB)
	intFlag(&options.manager.MemoryBudgetMiB, "memory-budget", config.VM_MEMORY_BUDGET_MIB)
	intFlag(&options.manager.MemoryOverheadMiB, "memory-overhead", config.VM_MEMORY_OVERHEAD_MIB)
	stringFlag(&options.manager.CgroupRoot, "vm-cgroup-root", config.VM_CGROUP_ROOT)
	stringFlag(maxDiskSize, "vm-disk-max-size", config.VM_DISK_MAX_SIZE)
	intFlag(&options.manager.MaxVMsPerUser, "max-vms-per-user", config.MAX_VDI_PER_USER)
	intFlag(&options.manager.MaxCreations, "max-creations", config.MAX_CONCURRENT_CREATIONS)
	intFlag(&options.manager.MaxRunning, "max-running", config.MAX_RUNNING_VMS)
	flags.Int64Var(
		&options.manager.MaxImageUploadBytes,
		"image-upload-limit",
		s.Int64(config.IMAGE_UPLOAD_MAX_BYTES),
		description(config.IMAGE_UPLOAD_MAX_BYTES),
	)
	durationFlag(&options.gateway.CreationTimeout, "vm-create-timeout", config.VM_CREATE_TIMEOUT)
	durationFlag(&options.manager.StartTimeout, "vm-start-timeout", config.VM_START_TIMEOUT)
	durationFlag(&options.manager.StopTimeout, "vm-stop-timeout", config.VM_STOP_TIMEOUT)
	durationFlag(&options.gateway.UploadTimeout, "image-upload-timeout", config.IMAGE_UPLOAD_TIMEOUT)
	durationFlag(&options.gateway.SessionTTL, "session-ttl", config.SESSION_TTL)
	intFlag(&options.gateway.MaxSessions, "max-sessions", config.MAX_SESSIONS)
	intFlag(&options.gateway.MaxConnections, "max-connections", config.MAX_CONCURRENT_CONNECTIONS)
	intFlag(&options.gateway.MaxHandshakes, "max-handshakes", config.MAX_HANDSHAKES)
	intFlag(&options.gateway.MaxConnectionsPerIP, "max-connections-per-ip", config.MAX_CONNECTIONS_PER_IP)
	intFlag(&options.gateway.MaxHandshakesPerIP, "max-handshakes-per-ip", config.MAX_HANDSHAKES_PER_IP)
	intFlag(&options.gateway.ConnectionRateLimit, "connection-rate-limit", config.CONNECTION_RATE_LIMIT)
	intFlag(&options.gateway.ConnectionRateLimitPerIP, "connection-rate-limit-per-ip", config.CONNECTION_RATE_LIMIT_PER_IP)
	durationFlag(&options.gateway.HandshakeTimeout, "handshake-timeout", config.HANDSHAKE_TIMEOUT)
	stringFlag(&options.gateway.ListenAddr, "listen", config.LISTEN_ADDR)
	stringFlag(&options.gateway.PublicAddress, "public-address", config.PUBLIC_ADDRESS)
	stringFlag(&options.gateway.CertFile, "tls-cert", config.CERT_FILE)
	stringFlag(&options.gateway.KeyFile, "tls-key", config.KEY_FILE)
	boolFlag(&options.certificates.ACME, "acme", config.ACME_ENABLE)
	stringFlag(&options.certificates.ACMEEmail, "acme-email", config.ACME_EMAIL)
	stringFlag(&options.certificates.ACMECA, "acme-ca", config.ACME_CA)
	stringFlag(&options.authFile, "auth-file", config.AUTH_FILE)
	stringFlag(&options.ldap.URL, "ldap-url", config.LDAP_URL)
	stringFlag(&options.ldap.BaseDN, "ldap-base-dn", config.LDAP_BASE_DN)
	stringFlag(&options.ldap.UserDomain, "ldap-domain", config.LDAP_USER_DOMAIN)
	stringFlag(&options.ldap.UserFilter, "ldap-filter", config.LDAP_USER_FILTER)
	stringFlag(&options.ldap.UsernameAttribute, "ldap-username-attribute", config.LDAP_USERNAME_ATTRIBUTE)
	stringFlag(&options.ldap.RequiredGroup, "ldap-required-group", config.LDAP_REQUIRED_GROUP)
	stringFlag(&options.ldap.AdminGroup, "ldap-admin-group", config.ADMIN_GROUP)
	stringFlag(&options.ldap.CAFile, "ldap-ca", config.LDAP_CA_FILE)
	boolFlag(&options.ldap.AllowInsecure, "ldap-insecure-loopback", config.LDAP_INSECURE_LOOPBACK)
	durationFlag(&options.ldap.Timeout, "ldap-timeout", config.LDAP_AUTH_TIMEOUT)
}

func parseCommand(flags *flag.FlagSet, args []string) error {
	if err := flags.Parse(args); err != nil {
		return err
	}
	if flags.NArg() != 0 {
		return fmt.Errorf("unexpected positional arguments: %w", errCommandUsage)
	}

	return nil
}

func loopbackAddress(address string) bool {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return false
	}
	ip := net.ParseIP(host)

	return host == "localhost" || ip != nil && ip.IsLoopback()
}

func serveIdentity(state string, c *gateway.Config, options cert.Options) error {
	if (c.CertFile == "") != (c.KeyFile == "") {
		return fmt.Errorf("CERT_FILE (-tls-cert) and KEY_FILE (-tls-key) must be supplied together: %w", errCommandUsage)
	}
	if options.ACME && c.CertFile != "" {
		return fmt.Errorf("choose ACME or a supplied TLS certificate/key pair: %w", errCommandUsage)
	}
	host, _, err := net.SplitHostPort(c.PublicAddress)
	if err != nil {
		return fmt.Errorf("public address needs a hostname and port: %w", errCommandUsage)
	}
	options.Hostname, options.StateDir = host, state
	options.CertFile, options.KeyFile = c.CertFile, c.KeyFile
	c.TLSManager, err = cert.New(options)
	if err != nil {
		return fmt.Errorf("configure gateway TLS: %w", err)
	}
	if options.ACME {
		log.Printf("Gateway TLS: ACME for %s; public TCP 443 must reach this listener", host)
	} else if options.CertFile == "" {
		log.Printf("Gateway TLS: persistent self-signed certificate in %s", state)
	}
	c.CertFile, c.KeyFile = "", ""

	return nil
}

// createPrivateFile publishes a complete private file without replacing a path.
func createPrivateFile(path string, data []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".gokvm-*")
	if err != nil {
		return err
	}
	defer func() { _ = file.Close(); _ = os.Remove(file.Name()) }()
	if _, err := file.Write(data); err != nil {
		return err
	}
	if err := file.Sync(); err != nil {
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}

	return os.Link(file.Name(), path)
}
