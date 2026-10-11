package gateway

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/cloudinit"
	"github.com/goccy/go-yaml"
	"golang.org/x/sys/unix"
)

// Some desktop images omit XRDP entirely. Only disable units that exist, then
// mask all known guest RDP and serial-console services to prevent later startup.
// All shell tokens are fixed; no account or password data is interpolated here.
const disableGuestConsoleServices = `set -eu
for unit in xrdp.service xrdp-sesman.service serial-getty@ttyS0.service; do
    if systemctl cat "$unit" >/dev/null 2>&1; then
        systemctl disable --now "$unit"
    fi
done`

var sha512PasswordPattern = regexp.MustCompile(
	`^\$6\$(?:rounds=([1-9][0-9]{3,8})\$)?[./0-9A-Za-z]{1,16}\$[./0-9A-Za-z]{85}[./01]$`,
)

func validPasswordHash(hash string) bool {
	parts := sha512PasswordPattern.FindStringSubmatch(hash)
	if parts == nil {
		return false
	}
	if parts[1] == "" {
		return true
	}
	rounds, err := strconv.ParseUint(parts[1], 10, 32)

	return err == nil && rounds >= 1000 && rounds <= 999999999
}

// DesktopSeed provisions a password-protected desktop account without guest
// XRDP. The caller supplies a salted SHA-512 crypt hash and a persistent instance
// ID; neither credentials nor instance identity are generated implicitly.
// The seed keeps root locked, disables SSH password login, and configures DHCP
// for Ethernet interfaces with either predictable en* or traditional eth* names.
func DesktopSeed(username, passwordHash, hostname, instanceID, keyboard string) (cloudinit.Seed, error) {
	if !validOwner(username) || username == "root" || !validPasswordHash(passwordHash) ||
		!seedValidDNSName(hostname) || !seedValidInstanceID(instanceID) {
		return cloudinit.Seed{}, fmt.Errorf("invalid desktop account, hostname, instance ID or password hash: %w", ErrInvalid)
	}
	if keyboard == "" {
		keyboard = "us"
	}
	if !regexp.MustCompile(`^[a-zA-Z0-9_-]{1,32}$`).MatchString(keyboard) {
		return cloudinit.Seed{}, fmt.Errorf("invalid desktop keyboard layout: %w", ErrInvalid)
	}
	userData := map[string]any{
		"disable_root": true,
		"ssh_pwauth":   false,
		"keyboard":     map[string]string{"layout": keyboard},
		"users": []map[string]any{{
			"name": username, "shell": "/bin/bash", "lock_passwd": false,
			"passwd": passwordHash, "sudo": "ALL=(ALL) ALL",
		}},
		"runcmd": [][]string{
			{"usermod", "--lock", "root"},
			{"sh", "-c", disableGuestConsoleServices},
			{"systemctl", "mask", "xrdp.service", "xrdp-sesman.service", "serial-getty@ttyS0.service"},
			{"systemctl", "set-default", "graphical.target"},
			{"systemctl", "start", "--no-block", "graphical.target"},
		},
	}
	network := map[string]any{
		"version": 2,
		"ethernets": map[string]any{
			"all": map[string]any{
				"match": map[string]string{"name": "e*"}, "dhcp4": true, "dhcp6": false, "accept-ra": false,
			},
		},
	}
	user, err := yaml.Marshal(userData)
	if err != nil {
		return cloudinit.Seed{}, fmt.Errorf("encode desktop user-data: %w", err)
	}
	metadata, err := yaml.Marshal(map[string]string{"instance-id": instanceID, "local-hostname": hostname})
	if err != nil {
		return cloudinit.Seed{}, fmt.Errorf("encode desktop meta-data: %w", err)
	}
	networkData, err := yaml.Marshal(network)
	if err != nil {
		return cloudinit.Seed{}, fmt.Errorf("encode desktop network-config: %w", err)
	}

	return cloudinit.Seed{
		UserData: append([]byte("#cloud-config\n"), user...), MetaData: metadata, NetworkConfig: networkData,
	}, nil
}

func seedValidInstanceID(id string) bool {
	return len(id) <= 128 && regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*$`).MatchString(id)
}

func seedValidDNSName(host string) bool {
	if host == "" || len(host) > 253 {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, c := range label {
			if c != '-' && (c < 'a' || c > 'z') && (c < 'A' || c > 'Z') && (c < '0' || c > '9') {
				return false
			}
		}
	}

	return true
}

// NewTLSIdentity makes a self-signed ECDSA server identity for a DNS name or IP.
// Callers persist the returned private key with mode 0600 and distribute only
// the certificate to peers that pin this identity. The certificate lasts ten
// years; it provides encryption and identity pinning, not public-CA trust.
func NewTLSIdentity(host string) ([]byte, []byte, error) {
	ip := net.ParseIP(host)
	if ip == nil && !seedValidDNSName(host) {
		return nil, nil, fmt.Errorf("invalid TLS identity hostname: %w", ErrInvalid)
	}
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, fmt.Errorf("generate TLS key: %w", err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, nil, fmt.Errorf("generate TLS certificate serial: %w", err)
	}
	serial.Add(serial, big.NewInt(1))
	now := time.Now()
	certificate := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: host},
		NotBefore: now.Add(-5 * time.Minute), NotAfter: now.AddDate(10, 0, 0),
		KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip != nil {
		certificate.IPAddresses = []net.IP{ip}
	} else {
		certificate.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, certificate, certificate, &key.PublicKey, key)
	if err != nil {
		return nil, nil, fmt.Errorf("create TLS certificate: %w", err)
	}
	private, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, fmt.Errorf("encode TLS private key: %w", err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}),
		pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: private}), nil
}

func (m *Manager) prepare(ctx context.Context, record VM, passwordHash string) error {
	return m.prepareImage(ctx, record, passwordHash, m.config.BaseImage)
}

func (m *Manager) prepareImage(ctx context.Context, record VM, passwordHash, imagePath string) error {
	return m.prepareImageWithProgress(ctx, record, passwordHash, imagePath, nil)
}

func (m *Manager) prepareImageWithProgress(
	ctx context.Context, record VM, passwordHash, imagePath string, report func(CreateProgress),
) error {
	input, err := os.Open(imagePath) //nolint:gosec // Validated operator image path.
	if err != nil {
		return err
	}
	defer input.Close()

	return m.prepareOpenImageWithProgress(ctx, record, passwordHash, input, report)
}

func (m *Manager) prepareOpenImageWithProgress(
	ctx context.Context, record VM, passwordHash string, imageFile *os.File, report func(CreateProgress),
) error {
	if !validID(record.ID) {
		return fmt.Errorf("invalid VM identity: %w", ErrInvalid)
	}
	if err := reportCreationProgress(ctx, report, CreateProgress{Phase: "preparing"}); err != nil {
		return err
	}
	seed, err := DesktopSeed(record.Owner, passwordHash, record.Name, "vm-"+record.ID, m.config.Keyboard)
	if err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	root := filepath.Join(m.config.DataDir, "vms")
	staging := filepath.Join(root, ".creating-"+record.ID)
	//nolint:gosec // State root is operator-owned; the VM ID was strictly validated above.
	if err := os.Mkdir(staging, 0o700); err != nil {
		return fmt.Errorf("create VM staging directory: %w", err)
	}
	defer func() {
		_ = os.RemoveAll(staging) //nolint:gosec // Private staging path uses the strictly validated VM ID.
	}()
	if err := cloneOpenFileWithProgress(ctx, imageFile, filepath.Join(staging, "disk.img"), report); err != nil {
		return fmt.Errorf("clone VM base image: %w", err)
	}
	if err := reportCreationProgress(ctx, report, CreateProgress{Phase: "configuring"}); err != nil {
		return err
	}
	if err := cloneFile(ctx, m.config.VarsTemplate, filepath.Join(staging, "vars.fd")); err != nil {
		return fmt.Errorf("clone VM firmware variables: %w", err)
	}
	if err := writePreparedSeed(staging, seed, record); err != nil {
		return err
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := reportCreationProgress(ctx, report, CreateProgress{Phase: "finalizing"}); err != nil {
		return err
	}
	if err := unix.Renameat2(
		unix.AT_FDCWD, staging, unix.AT_FDCWD, m.directory(record.ID), unix.RENAME_NOREPLACE,
	); err != nil {
		return fmt.Errorf("publish VM directory: %w", err)
	}
	directory, err := os.Open(root) //nolint:gosec // Private operator-configured state root.
	if err != nil {
		_ = os.RemoveAll(m.directory(record.ID)) //nolint:gosec // Validated ID; only this newly published VM.

		return fmt.Errorf("open VM state directory: %w", err)
	}
	defer func() { _ = directory.Close() }()
	if err := directory.Sync(); err != nil {
		_ = os.RemoveAll(m.directory(record.ID)) //nolint:gosec // Validated ID; only this newly published VM.

		return fmt.Errorf("sync VM state directory: %w", err)
	}

	return nil
}

func writePreparedSeed(dir string, seed cloudinit.Seed, record VM) error {
	image, err := cloudinit.CreateSeedISO(seed)
	if err != nil {
		return err
	}
	certificate, key, err := NewTLSIdentity("vm-" + record.ID)
	if err != nil {
		return err
	}
	metadata, err := json.MarshalIndent(record, "", "  ")
	if err != nil {
		return fmt.Errorf("encode VM record: %w", err)
	}
	for _, file := range []struct {
		name string
		data []byte
	}{
		{name: "seed.iso", data: image},
		{name: "rdp.crt", data: certificate},
		{name: "rdp.key", data: key},
		{name: "vm.json", data: metadata},
	} {
		if err := writeAtomic(dir, file.name, file.data); err != nil {
			return fmt.Errorf("write VM %s: %w", file.name, err)
		}
	}

	return nil
}
