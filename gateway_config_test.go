package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/cert"
	"github.com/define42/DevBox-Gateway-kvm/internal/config"
	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
	"github.com/define42/DevBox-Gateway-kvm/internal/virgl"
)

func clearServeEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range config.NewSettings().Keys() {
		// Setenv registers restoration of the original environment at cleanup.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeDefaultRDPProfile(t *testing.T) {
	clearServeEnvironment(t)
	options, err := readServeOptions([]string{"-auth-file=local.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.manager.RDPProfile != "office" {
		t.Fatalf("default RDP profile = %q, want office", options.manager.RDPProfile)
	}
	if options.manager.VMProfiling {
		t.Fatal("VM profiling must be disabled by default")
	}
	if options.manager.MemoryOverheadMiB != 512 || options.manager.CgroupRoot != "" {
		t.Fatal("unexpected default worker memory isolation configuration")
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeDefaultVMDiskSize(t *testing.T) {
	clearServeEnvironment(t)
	options, err := readServeOptions([]string{"-auth-file=local.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.manager.MaxDiskBytes != gateway.DefaultMaxDiskBytes {
		t.Fatalf(
			"default VM disk size = %d, want %d",
			options.manager.MaxDiskBytes,
			gateway.DefaultMaxDiskBytes,
		)
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeEnvironmentConfiguration(t *testing.T) {
	clearServeEnvironment(t)
	for key, value := range map[string]string{
		"DATA_ROOT_DIR": "/srv/desktops", "BASE_IMAGE": "/srv/images/desktop.qcow2", "VM_BINARY": "/opt/devbox-kvm",
		"UEFI_CODE": "/firmware/code.fd", "UEFI_VARS_TEMPLATE": "/firmware/vars.fd",
		"H264_ENCODER": "software", "H264_QP": "28", "RDP_PROFILE": "smooth", "VM_KEYBOARD": "dk", "VM_VCPU_COUNT": "6",
		"VM_MEMORY_MIB":        "8192",
		"VM_MEMORY_BUDGET_MIB": "32768", "MAX_VDI_PER_USER": "5", "MAX_RUNNING_VMS": "8", "MAX_CONCURRENT_CREATIONS": "3",
		"VM_DISK_MAX_SIZE":       "750GiB",
		"VM_MEMORY_OVERHEAD_MIB": "1024", "VM_CGROUP_ROOT": "/sys/fs/cgroup/devbox/workers",
		"IMAGE_UPLOAD_MAX_BYTES": "42949672960", "IMAGE_UPLOAD_TIMEOUT": "45m",
		"VM_CREATE_TIMEOUT": "40m", "VM_START_TIMEOUT": "50s", "VM_STOP_TIMEOUT": "25s", "SESSION_TTL": "4h",
		"MAX_SESSIONS": "250", "MAX_CONCURRENT_CONNECTIONS": "64", "LISTEN_ADDR": "0.0.0.0:9443",
		"MAX_HANDSHAKES": "48", "MAX_CONNECTIONS_PER_IP": "24", "MAX_HANDSHAKES_PER_IP": "6",
		"CONNECTION_RATE_LIMIT": "100", "CONNECTION_RATE_LIMIT_PER_IP": "12", "HANDSHAKE_TIMEOUT": "15s",
		"PUBLIC_ADDRESS": "desktop.example.com:443", "CERT_FILE": "/tls/cert.pem", "KEY_FILE": "/tls/key.pem",
		"ACME_ENABLE": "false", "ACME_EMAIL": "admin@example.com", "ACME_CA": "staging", "AUTH_FILE": "",
		"LDAP_URL": "ldaps://ldap.example.com:636", "LDAP_BASE_DN": "dc=example,dc=com",
		"LDAP_USER_DOMAIN": "example.com", "LDAP_USER_FILTER": "(mail=%s)", "LDAP_USERNAME_ATTRIBUTE": "mail",
		"LDAP_REQUIRED_GROUP": "developers", "ADMIN_GROUP": "admins", "LDAP_CA_FILE": "/tls/ldap.pem",
		"LDAP_INSECURE_LOOPBACK": "false", "LDAP_AUTH_TIMEOUT": "20s",
	} {
		t.Setenv(key, value)
	}
	options, err := readServeOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	qp := 28
	want := serveOptions{
		manager: gateway.ManagerConfig{
			MemoryOverheadMiB: 1024, CgroupRoot: "/sys/fs/cgroup/devbox/workers",
			DataDir: "/srv/desktops", Binary: "/opt/devbox-kvm", BaseImage: "/srv/images/desktop.qcow2",
			Firmware: "/firmware/code.fd", VarsTemplate: "/firmware/vars.fd", Encoder: "software", Keyboard: "dk",
			RDPProfile: "smooth", H264QP: &qp, NetworkPolicy: "internet",
			CPUs: 6, MemoryMiB: 8192, MemoryBudgetMiB: 32768, MaxVMsPerUser: 5, MaxRunning: 8, MaxCreations: 3,
			MaxDiskBytes: 750 << 30, MaxImageUploadBytes: 40 << 30,
			StartTimeout: 50 * time.Second, StopTimeout: 25 * time.Second,
		},
		gateway: gateway.Config{
			ListenAddr: "0.0.0.0:9443", PublicAddress: "desktop.example.com:443",
			CertFile: "/tls/cert.pem", KeyFile: "/tls/key.pem", DesktopImage: "desktop.qcow2",
			DesktopCPUs: 6, DesktopMemoryMiB: 8192, MaxImageUploadBytes: 40 << 30,
			CreationTimeout: 40 * time.Minute,
			UploadTimeout:   45 * time.Minute, SessionTTL: 4 * time.Hour, MaxSessions: 250, MaxConnections: 64,
			MaxHandshakes: 48, MaxConnectionsPerIP: 24, MaxHandshakesPerIP: 6,
			ConnectionRateLimit: 100, ConnectionRateLimitPerIP: 12, HandshakeTimeout: 15 * time.Second,
		},
		ldap: gateway.LDAPConfig{
			URL: "ldaps://ldap.example.com:636", BaseDN: "dc=example,dc=com", UserDomain: "example.com",
			UserFilter: "(mail=%s)", UsernameAttribute: "mail", RequiredGroup: "developers", AdminGroup: "admins",
			CAFile: "/tls/ldap.pem", Timeout: 20 * time.Second,
		},
		certificates: cert.Options{ACMEEmail: "admin@example.com", ACMECA: "staging"},
	}
	if !reflect.DeepEqual(options, want) {
		t.Fatalf("environment configuration:\n got %#v\nwant %#v", options, want)
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeEnvironmentEnablesBooleanSettings(t *testing.T) {
	clearServeEnvironment(t)
	t.Setenv("LDAP_URL", "ldap://127.0.0.1:1389")
	t.Setenv("LDAP_BASE_DN", "dc=example,dc=com")
	t.Setenv("LDAP_INSECURE_LOOPBACK", "true")
	t.Setenv("ACME_ENABLE", "true")
	t.Setenv("GATEWAY_VM_PROFILING", "true")
	options, err := readServeOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !options.ldap.AllowInsecure || !options.certificates.ACME || !options.manager.VMProfiling {
		t.Fatalf("boolean environment settings not applied: %#v", options)
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeFlagsOverrideEnvironment(t *testing.T) {
	clearServeEnvironment(t)
	for key, value := range map[string]string{
		"LDAP_URL": "ldaps://directory.example.com", "AUTH_FILE": "",
		"VM_VCPU_COUNT": "8", "VM_MEMORY_BUDGET_MIB": "32768", "ACME_ENABLE": "true", "RDP_PROFILE": "smooth",
		"VM_DISK_MAX_SIZE":       "malformed",
		"VM_MEMORY_OVERHEAD_MIB": "1024", "VM_CGROUP_ROOT": "/delegated/env-workers",
		"IMAGE_UPLOAD_MAX_BYTES": "42949672960", "IMAGE_UPLOAD_TIMEOUT": "45m", "ADMIN_GROUP": "admins",
		"MAX_CONCURRENT_CONNECTIONS": "64", "MAX_HANDSHAKES": "48", "MAX_CONNECTIONS_PER_IP": "24",
		"MAX_HANDSHAKES_PER_IP": "6", "CONNECTION_RATE_LIMIT": "100", "CONNECTION_RATE_LIMIT_PER_IP": "12",
		"HANDSHAKE_TIMEOUT":    "15s",
		"GATEWAY_VM_PROFILING": "true",
	} {
		t.Setenv(key, value)
	}
	options, err := readServeOptions([]string{
		"-ldap-url=", "-auth-file=local.json", "-cpus=3", "-memory-budget=0", "-acme=false", "-rdp-profile=office",
		"-vm-disk-max-size=600GiB",
		"-memory-overhead=768", "-vm-cgroup-root=/delegated/flag-workers",
		"-image-upload-limit=10737418240", "-image-upload-timeout=20m", "-ldap-admin-group=",
		"-max-connections=80", "-max-handshakes=40", "-max-connections-per-ip=20", "-max-handshakes-per-ip=5",
		"-connection-rate-limit=120", "-connection-rate-limit-per-ip=10", "-handshake-timeout=12s",
		"-vm-profiling=false",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.ldap.URL != "" || options.authFile != "local.json" || options.ldap.AdminGroup != "" {
		t.Fatalf("string overrides not applied: %#v", options)
	}
	if options.manager.RDPProfile != "office" {
		t.Fatalf("RDP profile override = %q, want office", options.manager.RDPProfile)
	}
	if options.manager.CPUs != 3 || options.manager.MemoryBudgetMiB != 0 || options.certificates.ACME ||
		options.manager.VMProfiling {
		t.Fatalf("integer/boolean overrides not applied: %#v", options)
	}
	if options.manager.MaxImageUploadBytes != 10<<30 || options.gateway.UploadTimeout != 20*time.Minute {
		t.Fatalf("byte/duration overrides not applied: %#v", options)
	}
	if options.manager.MaxDiskBytes != 600<<30 {
		t.Fatalf("VM disk size override = %d, want %d", options.manager.MaxDiskBytes, int64(600<<30))
	}
	if options.manager.MemoryOverheadMiB != 768 || options.manager.CgroupRoot != "/delegated/flag-workers" {
		t.Fatal("worker memory isolation flags did not override environment")
	}
	if options.gateway.DesktopCPUs != 3 || options.gateway.MaxImageUploadBytes != 10<<30 {
		t.Fatalf("derived settings ignored explicit flags: %#v", options.gateway)
	}
	if options.gateway.MaxConnections != 80 || options.gateway.MaxHandshakes != 40 ||
		options.gateway.MaxConnectionsPerIP != 20 || options.gateway.MaxHandshakesPerIP != 5 ||
		options.gateway.ConnectionRateLimit != 120 || options.gateway.ConnectionRateLimitPerIP != 10 ||
		options.gateway.HandshakeTimeout != 12*time.Second {
		t.Fatalf("connection admission overrides not applied: %#v", options.gateway)
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeRejectsInvalidVMDiskSize(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "blank", value: ""},
		{name: "zero", value: "0"},
		{name: "negative", value: "-1GB"},
		{name: "fraction", value: "1.5GB"},
		{name: "overflow", value: "9223372036854775808"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			clearServeEnvironment(t)
			t.Setenv(config.AUTH_FILE, "local.json")
			t.Setenv(config.VM_DISK_MAX_SIZE, tt.value)

			_, err := readServeOptions(nil, io.Discard)
			if !errors.Is(err, errCommandUsage) {
				t.Fatalf("readServeOptions error = %v, want command usage", err)
			}
			if !strings.Contains(err.Error(), config.VM_DISK_MAX_SIZE) {
				t.Fatalf("readServeOptions error = %q, want setting name", err)
			}
		})
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeEnvironmentRejectsUnsafeAuthentication(t *testing.T) {
	for _, test := range []struct {
		name string
		env  map[string]string
	}{
		{name: "missing authentication", env: map[string]string{}},
		{name: "conflicting authentication", env: map[string]string{
			"AUTH_FILE": "local.json", "LDAP_URL": "ldaps://directory.example.com",
		}},
		{name: "remote static authentication", env: map[string]string{
			"AUTH_FILE": "local.json", "LISTEN_ADDR": "0.0.0.0:8443",
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearServeEnvironment(t)
			for key, value := range test.env {
				t.Setenv(key, value)
			}
			if err := runServe(t.Context(), nil, io.Discard); !errors.Is(err, errCommandUsage) {
				t.Fatalf("unsafe environment accepted: %v", err)
			}
		})
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeHelpDocumentsEnvironment(t *testing.T) {
	clearServeEnvironment(t)
	var output bytes.Buffer
	if _, err := readServeOptions([]string{"-help"}, &output); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help: %v", err)
	}
	if !strings.HasPrefix(output.String(), "Usage: devbox-kvm serve [flags]\n") {
		t.Fatalf("help does not identify the executable: %q", output.String())
	}
	for _, text := range []string{
		"-h264-qp", "min 0, max 51", "lower = higher quality/more bandwidth", "higher = more compression",
	} {
		if !strings.Contains(output.String(), text) {
			t.Errorf("help omits H264_QP description %q", text)
		}
	}
	for _, key := range config.NewSettings().Keys() {
		if !strings.Contains(output.String(), "["+key+"]") {
			t.Errorf("help omits %s", key)
		}
	}
}

//nolint:paralleltest // Verifies environment defaults and explicit flag precedence.
func TestServeGPU3DConfiguration(t *testing.T) {
	for _, test := range []struct {
		name string
		env  string
		args []string
		want bool
	}{
		{name: "disabled default"},
		{name: "enabled environment", env: "true", want: true},
		{name: "disabled environment", env: "false"},
		{name: "malformed environment disables", env: "invalid"},
		{name: "flag disables environment", env: "true", args: []string{"-gpu-3d=false"}},
		{name: "flag enables environment", env: "false", args: []string{"-gpu-3d=true"}, want: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearServeEnvironment(t)
			t.Setenv(config.VM_GPU_3D, test.env)
			options, err := readServeOptions(append([]string{"-auth-file=local.json"}, test.args...), io.Discard)
			if err != nil {
				t.Fatal(err)
			}
			if options.manager.GPU3D != test.want {
				t.Fatalf("GPU3D = %t, want %t", options.manager.GPU3D, test.want)
			}
		})
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeRejectsUnavailableGPU3DBeforeOpeningAuthFile(t *testing.T) {
	if virgl.Available() {
		t.Skip("this build includes virgl support")
	}
	clearServeEnvironment(t)
	t.Setenv(config.VM_GPU_3D, "true")
	err := runServe(context.Background(), []string{"-auth-file=/missing-auth-file"}, io.Discard)
	if !errors.Is(err, virgl.ErrUnavailable) {
		t.Fatalf("startup error = %v, want unavailable renderer before authentication I/O", err)
	}
	if !strings.Contains(err.Error(), "VM_GPU_3D=false") {
		t.Fatalf("startup error must explain how to disable 3D: %v", err)
	}
}

//nolint:paralleltest // Verifies environment defaults and explicit flag precedence.
func TestServeH264QPConfiguration(t *testing.T) {
	for _, test := range []struct {
		name    string
		env     string
		args    []string
		want    int
		invalid bool
	}{
		{name: "default", want: 20},
		{name: "minimum", env: "0", want: 0},
		{name: "maximum", env: "51", want: 51},
		{name: "custom", env: "28", want: 28},
		{name: "trimmed", env: " 28 ", want: 28},
		{name: "malformed uses default", env: "invalid", want: 20},
		{name: "flag minimum overrides environment", env: "28", args: []string{"-h264-qp=0"}, want: 0},
		{name: "flag overrides invalid environment", env: "52", args: []string{"-h264-qp=28"}, want: 28},
		{name: "environment below minimum", env: "-1", invalid: true},
		{name: "environment above maximum", env: "52", invalid: true},
		{name: "flag below minimum", args: []string{"-h264-qp=-1"}, invalid: true},
		{name: "flag above maximum", args: []string{"-h264-qp=52"}, invalid: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			clearServeEnvironment(t)
			if test.env != "" {
				t.Setenv(config.H264_QP, test.env)
			}
			options, err := readServeOptions(append([]string{"-auth-file=local.json"}, test.args...), io.Discard)
			if test.invalid {
				if !errors.Is(err, errCommandUsage) || !strings.Contains(err.Error(), "H264_QP") ||
					!strings.Contains(err.Error(), "51") {
					t.Fatalf("invalid QP error = %v, want named setting and range", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if options.manager.H264QP == nil || *options.manager.H264QP != test.want {
				t.Fatalf("QP = %v, want %d", options.manager.H264QP, test.want)
			}
		})
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeNetworkDestinationPolicy(t *testing.T) {
	clearServeEnvironment(t)
	options, err := readServeOptions([]string{"-auth-file=local.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.manager.NetworkPolicy != "internet" || options.manager.NetworkAllowCIDRs != "" {
		t.Fatal("managed default permits unexpected network destinations")
	}
	t.Setenv(config.VM_NETWORK_POLICY, "allowlist")
	t.Setenv(config.VM_NETWORK_ALLOW_CIDRS, "10.20.0.0/16")
	options, err = readServeOptions([]string{"-auth-file=local.json"}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.manager.NetworkPolicy != "allowlist" || options.manager.NetworkAllowCIDRs != "10.20.0.0/16" {
		t.Fatal("network policy environment was ignored")
	}
	options, err = readServeOptions([]string{
		"-auth-file=local.json", "-vm-network-policy=internet", "-vm-network-allow-cidrs=192.168.2.10/32",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.manager.NetworkPolicy != "internet" || options.manager.NetworkAllowCIDRs != "192.168.2.10/32" {
		t.Fatal("network policy flags did not override environment")
	}
	for _, args := range [][]string{
		{"-vm-network-policy=unrestricted"},
		{"-vm-network-allow-cidrs=::1/128"},
		{"-vm-network-allow-cidrs=0.0.0.0/0"},
		{"-vm-network-allow-cidrs=10.0.0.1/24"},
	} {
		_, err := readServeOptions(append([]string{"-auth-file=local.json"}, args...), io.Discard)
		if !errors.Is(err, errCommandUsage) {
			t.Fatalf("invalid policy accepted: %v; %v", args, err)
		}
	}
}
