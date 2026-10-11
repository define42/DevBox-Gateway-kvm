package main

import (
	"context"
	"crypto/tls"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"path/filepath"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
)

var errCommandUsage = errors.New("invalid command arguments")

func runServe(ctx context.Context, args []string, output io.Writer) (serveErr error) {
	flags := flag.NewFlagSet("serve", flag.ContinueOnError)
	flags.SetOutput(output)
	var manager gateway.ManagerConfig
	var config gateway.Config
	var ldap gateway.LDAPConfig
	var authFile string
	flags.StringVar(&manager.DataDir, "state", ".gokvm-gateway", "private persistent VM state directory")
	flags.StringVar(&manager.BaseImage, "image", "", "immutable standalone raw or uncompressed qcow2 base image")
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
	flags.StringVar(&config.CertFile, "tls-cert", "", "public TLS certificate (required for remote access)")
	flags.StringVar(&config.KeyFile, "tls-key", "", "public TLS private key")
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
	config.DesktopImage = filepath.Base(manager.BaseImage)
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
	if err := serveIdentity(manager.DataDir, &config); err != nil {
		return err
	}
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

func serveIdentity(state string, c *gateway.Config) error {
	if (c.CertFile == "") != (c.KeyFile == "") {
		return fmt.Errorf("-tls-cert and -tls-key must be supplied together: %w", errCommandUsage)
	}
	if c.CertFile != "" {
		return nil
	}
	if !loopbackAddress(c.ListenAddr) || !loopbackAddress(c.PublicAddress) {
		return fmt.Errorf("remote access requires -tls-cert and -tls-key: %w", errCommandUsage)
	}
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	info, err := os.Lstat(state)
	if err != nil {
		return err
	}
	if !info.IsDir() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("state directory must be private (0700): %w", errCommandUsage)
	}
	c.CertFile, c.KeyFile = filepath.Join(state, "gateway.crt"), filepath.Join(state, "gateway.key")
	_, certErr := os.Lstat(c.CertFile)
	_, keyErr := os.Lstat(c.KeyFile)
	if certErr == nil && keyErr == nil {
		_, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)

		return err
	}
	if !errors.Is(certErr, os.ErrNotExist) || !errors.Is(keyErr, os.ErrNotExist) {
		return fmt.Errorf("incomplete local TLS identity; supply a matching certificate/key pair: %w", errCommandUsage)
	}
	host, _, err := net.SplitHostPort(c.PublicAddress)
	if err != nil {
		return err
	}
	cert, key, err := gateway.NewTLSIdentity(host)
	if err != nil {
		return err
	}
	if err := createPrivateFile(c.KeyFile, key); err != nil {
		return err
	}
	if err := createPrivateFile(c.CertFile, cert); err != nil {
		_ = os.Remove(c.KeyFile)

		return err
	}
	log.Printf("Created local self-signed TLS identity: %s", c.CertFile)

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
