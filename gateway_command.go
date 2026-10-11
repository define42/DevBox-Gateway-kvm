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
	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
)

var errCommandUsage = errors.New("invalid command arguments")

func runServe(ctx context.Context, args []string, output io.Writer) (serveErr error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(output)
	var manager gateway.ManagerConfig
	var config gateway.Config
	var ldap gateway.LDAPConfig
	var certificates cert.Options
	var authFile string
	flags.StringVar(&manager.DataDir, "state", ".gokvm-gateway", "private persistent VM state directory")
	flags.StringVar(&manager.BaseImage, "image", "",
		"optional immutable raw or uncompressed qcow2 default image; otherwise use admin uploads")
	flags.StringVar(&manager.Firmware, "uefi", "/usr/share/OVMF/OVMF_CODE_4M.fd", "OVMF CODE firmware")
	flags.StringVar(&manager.VarsTemplate, "uefi-vars-template", "/usr/share/OVMF/OVMF_VARS_4M.fd",
		"matching pristine OVMF variables template")
	flags.StringVar(&manager.Encoder, "encoder", "auto", "H.264 encoder: auto, nvenc, software")
	flags.StringVar(&manager.Keyboard, "keyboard", "us", "guest keyboard layout")
	flags.IntVar(&manager.CPUs, "cpus", 2, "vCPUs per desktop")
	flags.IntVar(&manager.MemoryMiB, "memory", 4096, "MiB per desktop")
	flags.IntVar(&manager.MemoryBudgetMiB, "memory-budget", 0,
		"maximum MiB reserved for running desktops including overhead (0: automatic)")
	flags.IntVar(&manager.MaxVMsPerUser, "max-vms-per-user", 3, "desktop quota per account")
	flags.IntVar(&manager.MaxRunning, "max-running", 4, "global running desktop limit")
	flags.Int64Var(&manager.MaxImageUploadBytes, "image-upload-limit", gateway.DefaultMaxImageUploadBytes,
		"maximum uploaded qcow2 file size in bytes")
	flags.DurationVar(&config.UploadTimeout, "image-upload-timeout", 30*time.Minute,
		"maximum image upload and import duration")
	flags.StringVar(&config.ListenAddr, "listen", "127.0.0.1:8443", "shared HTTPS and RDP TCP listener")
	flags.StringVar(&config.PublicAddress, "public-address", "localhost:8443",
		"hostname:port written to downloaded RDP files")
	flags.StringVar(
		&config.CertFile, "tls-cert", "",
		"optional PEM server certificate; otherwise generate a self-signed certificate or use ACME",
	)
	flags.StringVar(&config.KeyFile, "tls-key", "", "PEM private key matching -tls-cert")
	flags.BoolVar(
		&certificates.ACME, "acme", false,
		"obtain and renew a certificate for the public hostname using ACME TLS-ALPN-01",
	)
	flags.StringVar(&certificates.ACMEEmail, "acme-email", "", "ACME account email address")
	flags.StringVar(&certificates.ACMECA, "acme-ca", "production", "ACME CA: production, staging, or directory URL")
	flags.StringVar(&authFile, "auth-file", "", "private bcrypt JSON file for loopback development; excludes LDAP")
	flags.StringVar(&ldap.URL, "ldap-url", "", "LDAP URL; ldap:// upgrades with StartTLS")
	flags.StringVar(&ldap.BaseDN, "ldap-base-dn", "", "LDAP search base DN")
	flags.StringVar(&ldap.UserDomain, "ldap-domain", "", "optional UPN domain for user binding")
	flags.StringVar(&ldap.UserFilter, "ldap-filter", "", "LDAP filter with one %s placeholder for the bind identifier")
	flags.StringVar(&ldap.UsernameAttribute, "ldap-username-attribute", "uid",
		"canonical account attribute: bare Linux name or name@ldap-domain")
	flags.StringVar(&ldap.RequiredGroup, "ldap-required-group", "", "optional required memberOf group DN")
	flags.StringVar(&ldap.AdminGroup, "ldap-admin-group", "",
		"direct memberOf group name or DN granting administrator access; empty disables admins")
	flags.StringVar(&ldap.CAFile, "ldap-ca", "", "private LDAP CA PEM file")
	flags.BoolVar(&ldap.AllowInsecure, "ldap-insecure-loopback", false,
		"allow plaintext LDAP only on a loopback IP for tests")
	if err := parseCommand(flags, args); err != nil {
		return err
	}
	if (authFile == "") == (ldap.URL == "") {
		return fmt.Errorf("choose either -ldap-url or -auth-file: %w", errCommandUsage)
	}
	if manager.BaseImage != "" {
		config.DesktopImage = filepath.Base(manager.BaseImage)
	}
	config.DesktopCPUs, config.DesktopMemoryMiB = manager.CPUs, manager.MemoryMiB
	config.MaxImageUploadBytes = manager.MaxImageUploadBytes
	var err error
	if authFile != "" {
		if !loopbackAddress(config.ListenAddr) {
			return fmt.Errorf("-auth-file is restricted to a loopback listener: %w", errCommandUsage)
		}
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
		return fmt.Errorf("-tls-cert and -tls-key must be supplied together: %w", errCommandUsage)
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
