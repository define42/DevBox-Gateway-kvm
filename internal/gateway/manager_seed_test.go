package gateway

import (
	"bytes"
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/define42/gokvm/internal/cloudinit"
	"github.com/goccy/go-yaml"
)

func seedHelperPasswordHash() string {
	return "$6$rounds=100000$0123456789abcdef$" + strings.Repeat("A", 85) + "."
}

func TestValidPasswordHash(t *testing.T) {
	t.Parallel()

	valid := seedHelperPasswordHash()
	for _, tc := range []struct {
		name  string
		hash  string
		valid bool
	}{
		{name: "explicit rounds", hash: valid, valid: true},
		{name: "default rounds", hash: strings.Replace(valid, "rounds=100000$", "", 1), valid: true},
		{name: "minimum rounds", hash: strings.Replace(valid, "100000", "1000", 1), valid: true},
		{name: "maximum rounds", hash: strings.Replace(valid, "100000", "999999999", 1), valid: true},
		{name: "empty"},
		{name: "plain password", hash: "this-is-not-a-hash"},
		{name: "algorithm", hash: strings.Replace(valid, "$6$", "$5$", 1)},
		{name: "low rounds", hash: strings.Replace(valid, "100000", "999", 1)},
		{name: "high rounds", hash: strings.Replace(valid, "100000", "1000000000", 1)},
		{name: "trailing data", hash: valid + "\n"},
		{name: "missing checksum", hash: "$6$salt$"},
		{name: "short checksum", hash: valid[:len(valid)-1]},
		{name: "invalid checksum bits", hash: valid[:len(valid)-1] + "z"},
		{name: "long salt", hash: strings.Replace(valid, "0123456789abcdef", "0123456789abcdefg", 1)},
		{name: "invalid salt", hash: strings.Replace(valid, "0123456789abcdef", "salt:bad", 1)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := validPasswordHash(tc.hash); got != tc.valid {
				t.Fatalf("password hash syntax accepted=%v, want %v", got, tc.valid)
			}
		})
	}
}

func TestDesktopSeedPolicy(t *testing.T) {
	t.Parallel()

	seed, err := DesktopSeed("alice", seedHelperPasswordHash(), "desktop.example.test", "vm-persistent-123", "dk")
	if err != nil {
		t.Fatal(err)
	}
	var user struct {
		DisableRoot bool `yaml:"disable_root"` //nolint:tagliatelle // Required cloud-init field name.
		SSHPwAuth   bool `yaml:"ssh_pwauth"`   //nolint:tagliatelle // Required cloud-init field name.
		Keyboard    struct {
			Layout string `yaml:"layout"`
		} `yaml:"keyboard"`
		Users []struct {
			Name       string `yaml:"name"`
			Passwd     string `yaml:"passwd"`
			Sudo       string `yaml:"sudo"`
			LockPasswd bool   `yaml:"lock_passwd"` //nolint:tagliatelle // Required cloud-init field name.
		} `yaml:"users"`
		RunCmd [][]string `yaml:"runcmd"`
	}
	if err := yaml.Unmarshal(seed.UserData, &user); err != nil {
		t.Fatal("generated user-data is not valid YAML")
	}
	if !bytes.HasPrefix(seed.UserData, []byte("#cloud-config\n")) || !user.DisableRoot || user.SSHPwAuth {
		t.Fatal("seed must identify cloud-config, disable root and disable SSH password authentication")
	}
	if user.Keyboard.Layout != "dk" || len(user.Users) != 1 || user.Users[0].Name != "alice" ||
		user.Users[0].Passwd != seedHelperPasswordHash() || user.Users[0].LockPasswd ||
		user.Users[0].Sudo != "ALL=(ALL) ALL" {
		t.Fatal("desktop account or keyboard policy differs from the requested configuration")
	}
	commands := make(map[string]bool)
	for _, command := range user.RunCmd {
		commands[strings.Join(command, " ")] = true
	}
	for _, command := range []string{
		"usermod --lock root",
		"sh -c " + disableGuestConsoleServices,
		"systemctl mask xrdp.service xrdp-sesman.service serial-getty@ttyS0.service",
		"systemctl set-default graphical.target",
		"systemctl start --no-block graphical.target",
	} {
		if !commands[command] {
			t.Errorf("missing fixed desktop setup command: %s", command)
		}
	}
	var metadata map[string]string
	if err := yaml.Unmarshal(seed.MetaData, &metadata); err != nil || metadata["instance-id"] != "vm-persistent-123" ||
		metadata["local-hostname"] != "desktop.example.test" {
		t.Fatal("seed must preserve the supplied persistent instance identity and hostname")
	}
	var network struct {
		Version   int `yaml:"version"`
		Ethernets map[string]struct {
			Match struct {
				Name string `yaml:"name"`
			} `yaml:"match"`
			DHCP4    bool `yaml:"dhcp4"`
			DHCP6    bool `yaml:"dhcp6"`
			AcceptRA bool `yaml:"accept-ra"` //nolint:tagliatelle // Required cloud-init field name.
		} `yaml:"ethernets"`
	}
	if err := yaml.Unmarshal(seed.NetworkConfig, &network); err != nil {
		t.Fatal("generated network-config is not valid YAML")
	}
	ethernet := network.Ethernets["all"]
	if network.Version != 2 || ethernet.Match.Name != "e*" || !ethernet.DHCP4 || ethernet.DHCP6 || ethernet.AcceptRA {
		t.Fatal("network-config must configure DHCP for both en* and eth* naming without a fixed MAC")
	}
	image, err := cloudinit.CreateSeedISO(seed)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.iso")
	if err := os.WriteFile(path, image, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := cloudinit.ValidateISO(path); err != nil {
		t.Fatal(err)
	}
}

func TestDisableGuestConsoleServices(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, present string
		fail          bool
	}{
		{name: "all optional units absent"},
		{name: "installed service", present: "xrdp.service"},
		{name: "serial console template", present: "serial-getty@ttyS0.service"},
		{name: "real disable failure", present: "xrdp.service", fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			logPath := filepath.Join(dir, "disabled")
			stub := `#!/bin/sh
case "$1" in
cat) [ "$2" = "$PRESENT_UNIT" ] ;;
disable) printf '%s\n' "$3" >> "$UNIT_LOG"; [ "$DISABLE_FAIL" = false ] ;;
*) exit 7 ;;
esac
`
			//nolint:gosec // This fixed test stub must be executable; its directory is private.
			if err := os.WriteFile(filepath.Join(dir, "systemctl"), []byte(stub), 0o700); err != nil {
				t.Fatal(err)
			}
			command := exec.CommandContext(t.Context(), "sh", "-c", disableGuestConsoleServices)
			command.Env = append(os.Environ(), "PATH="+dir, "PRESENT_UNIT="+tc.present, "UNIT_LOG="+logPath,
				"DISABLE_FAIL="+strconv.FormatBool(tc.fail))
			if err := command.Run(); (err != nil) != tc.fail {
				t.Fatalf("optional-service setup failed=%v, want %v", err != nil, tc.fail)
			}
			logData, err := os.ReadFile(logPath)
			if tc.present == "" {
				if !errors.Is(err, os.ErrNotExist) {
					t.Fatal("attempted to disable a nonexistent unit")
				}
			} else if err != nil || strings.TrimSpace(string(logData)) != tc.present {
				t.Fatal("did not disable exactly the installed unit")
			}
		})
	}
}

func TestDesktopSeedRejectsInvalidInput(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name, username, hostname, instanceID, keyboard string
	}{
		{name: "root account", username: "root"},
		{name: "account injection", username: "alice; touch /tmp/bad"},
		{name: "hostname injection", hostname: "host\nbad"},
		{name: "instance path", instanceID: "../../other"},
		{name: "keyboard injection", keyboard: "us;echo bad"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			username, hostname, instanceID, keyboard := "alice", "desktop", "vm-123", "us"
			if tc.username != "" {
				username = tc.username
			}
			if tc.hostname != "" {
				hostname = tc.hostname
			}
			if tc.instanceID != "" {
				instanceID = tc.instanceID
			}
			if tc.keyboard != "" {
				keyboard = tc.keyboard
			}
			_, err := DesktopSeed(username, seedHelperPasswordHash(), hostname, instanceID, keyboard)
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("invalid seed input error = %v, want ErrInvalid", err)
			}
		})
	}
}

func TestNewTLSIdentity(t *testing.T) {
	t.Parallel()

	for _, host := range []string{"vm-abcdefghijklmnopqrstuvwx23", "localhost", "127.0.0.1", "::1"} {
		t.Run(host, func(t *testing.T) {
			t.Parallel()
			certificate, key, err := NewTLSIdentity(host)
			if err != nil {
				t.Fatal(err)
			}
			if _, err := tls.X509KeyPair(certificate, key); err != nil {
				t.Fatal("generated TLS certificate and private key do not match")
			}
			block, _ := pem.Decode(certificate)
			parsed, err := x509.ParseCertificate(block.Bytes)
			if err != nil {
				t.Fatal(err)
			}
			roots := x509.NewCertPool()
			roots.AddCert(parsed)
			if _, err := parsed.Verify(x509.VerifyOptions{DNSName: host, Roots: roots}); err != nil {
				t.Fatalf("pinned certificate identity failed verification: %v", err)
			}
			if parsed.PublicKeyAlgorithm != x509.ECDSA || parsed.IsCA || parsed.SerialNumber.Sign() <= 0 {
				t.Fatal("expected a positive-serial ECDSA server certificate")
			}
			if _, err := parsed.Verify(x509.VerifyOptions{DNSName: "other.example.test", Roots: roots}); err == nil {
				t.Fatal("certificate authenticated a different VM hostname")
			}
		})
	}
}

func TestManagerPreparePublishesPrivateCompleteState(t *testing.T) {
	t.Parallel()

	manager, record := seedHelperManager(t)
	if err := manager.prepare(t.Context(), record, seedHelperPasswordHash()); err != nil {
		t.Fatal(err)
	}
	dir := manager.directory(record.ID)
	for _, name := range []string{"disk.img", "vars.fd", "seed.iso", "rdp.crt", "rdp.key", "vm.json"} {
		info, err := os.Stat(filepath.Join(dir, name))
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Fatalf("prepared %s must exist with mode 0600: %v", name, err)
		}
	}
	if err := cloudinit.ValidateISO(filepath.Join(dir, "seed.iso")); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ path, want string }{
		{manager.config.BaseImage, "immutable base"},
		{filepath.Join(dir, "disk.img"), "immutable base"},
		{manager.config.VarsTemplate, "firmware variables"},
		{filepath.Join(dir, "vars.fd"), "firmware variables"},
	} {
		data, err := os.ReadFile(tc.path)
		if err != nil || string(data) != tc.want {
			t.Fatal("VM preparation changed base data or failed to clone it")
		}
	}
	data, err := os.ReadFile(filepath.Join(dir, "vm.json"))
	if err != nil {
		t.Fatal(err)
	}
	var saved VM
	if err := json.Unmarshal(data, &saved); err != nil || saved.ID != record.ID || saved.Owner != record.Owner {
		t.Fatal("published VM metadata does not identify its owner and stable VM ID")
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(dir), ".creating-"+record.ID)); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("completed preparation left a staging directory")
	}
}

func TestManagerPrepareFailureCleansOnlyStaging(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"canceled", "clone failure", "existing target"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			manager, record := seedHelperManager(t)
			ctx, cancel := context.WithCancel(t.Context())
			defer cancel()
			dir := manager.directory(record.ID)
			switch name {
			case "canceled":
				cancel()
			case "clone failure":
				manager.config.VarsTemplate = filepath.Join(t.TempDir(), "missing.fd")
			case "existing target":
				if err := os.Mkdir(dir, 0o700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(filepath.Join(dir, "keep"), []byte("existing VM"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			if err := manager.prepare(ctx, record, seedHelperPasswordHash()); err == nil {
				t.Fatal("preparation unexpectedly succeeded")
			}
			if _, err := os.Stat(filepath.Join(filepath.Dir(dir), ".creating-"+record.ID)); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed preparation left partial staging data")
			}
			if name == "existing target" {
				data, err := os.ReadFile(filepath.Join(dir, "keep"))
				if err != nil || string(data) != "existing VM" {
					t.Fatal("preparation overwrote or deleted an existing VM")
				}
			} else if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("failed preparation published a VM directory")
			}
		})
	}
}

func seedHelperManager(t *testing.T) (*Manager, VM) {
	t.Helper()
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "vms"), 0o700); err != nil {
		t.Fatal(err)
	}
	base := filepath.Join(root, "base.img")
	variables := filepath.Join(root, "template.fd")
	if err := os.WriteFile(base, []byte("immutable base"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(variables, []byte("firmware variables"), 0o600); err != nil {
		t.Fatal(err)
	}

	return &Manager{config: ManagerConfig{DataDir: root, BaseImage: base, VarsTemplate: variables, Keyboard: "us"}},
		VM{ID: "abcdefghijklmnopqrstuvwx23", Owner: "alice", Name: "desktop", State: "creating"}
}
