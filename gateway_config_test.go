package main

import (
	"bytes"
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
)

func clearServeEnvironment(t *testing.T) {
	t.Helper()
	for _, key := range config.NewSettings(false).Keys() {
		// Setenv registers restoration of the original environment at cleanup.
		t.Setenv(key, "")
		if err := os.Unsetenv(key); err != nil {
			t.Fatal(err)
		}
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeEnvironmentConfiguration(t *testing.T) {
	clearServeEnvironment(t)
	for key, value := range map[string]string{
		"DATA_ROOT_DIR": "/srv/desktops", "BASE_IMAGE": "/srv/images/desktop.qcow2", "VM_BINARY": "/opt/gokvm",
		"UEFI_CODE": "/firmware/code.fd", "UEFI_VARS_TEMPLATE": "/firmware/vars.fd",
		"H264_ENCODER": "software", "VM_KEYBOARD": "dk", "VM_VCPU_COUNT": "6", "VM_MEMORY_MIB": "8192",
		"VM_MEMORY_BUDGET_MIB": "32768", "MAX_VDI_PER_USER": "5", "MAX_RUNNING_VMS": "8", "MAX_CONCURRENT_CREATIONS": "3",
		"IMAGE_UPLOAD_MAX_BYTES": "42949672960", "IMAGE_UPLOAD_TIMEOUT": "45m",
		"VM_CREATE_TIMEOUT": "40m", "VM_START_TIMEOUT": "50s", "VM_STOP_TIMEOUT": "25s", "SESSION_TTL": "4h",
		"MAX_SESSIONS": "250", "MAX_CONCURRENT_CONNECTIONS": "64", "LISTEN_ADDR": "0.0.0.0:9443",
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
	want := serveOptions{
		manager: gateway.ManagerConfig{
			DataDir: "/srv/desktops", Binary: "/opt/gokvm", BaseImage: "/srv/images/desktop.qcow2",
			Firmware: "/firmware/code.fd", VarsTemplate: "/firmware/vars.fd", Encoder: "software", Keyboard: "dk",
			CPUs: 6, MemoryMiB: 8192, MemoryBudgetMiB: 32768, MaxVMsPerUser: 5, MaxRunning: 8, MaxCreations: 3,
			MaxImageUploadBytes: 40 << 30, StartTimeout: 50 * time.Second, StopTimeout: 25 * time.Second,
		},
		gateway: gateway.Config{
			ListenAddr: "0.0.0.0:9443", PublicAddress: "desktop.example.com:443",
			CertFile: "/tls/cert.pem", KeyFile: "/tls/key.pem", DesktopImage: "desktop.qcow2",
			DesktopCPUs: 6, DesktopMemoryMiB: 8192, MaxImageUploadBytes: 40 << 30,
			CreationTimeout: 40 * time.Minute,
			UploadTimeout:   45 * time.Minute, SessionTTL: 4 * time.Hour, MaxSessions: 250, MaxConnections: 64,
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
	options, err := readServeOptions(nil, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if !options.ldap.AllowInsecure || !options.certificates.ACME {
		t.Fatalf("boolean environment settings not applied: %#v", options)
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeFlagsOverrideEnvironment(t *testing.T) {
	clearServeEnvironment(t)
	for key, value := range map[string]string{
		"LDAP_URL": "ldaps://directory.example.com", "AUTH_FILE": "",
		"VM_VCPU_COUNT": "8", "VM_MEMORY_BUDGET_MIB": "32768", "ACME_ENABLE": "true",
		"IMAGE_UPLOAD_MAX_BYTES": "42949672960", "IMAGE_UPLOAD_TIMEOUT": "45m", "ADMIN_GROUP": "admins",
	} {
		t.Setenv(key, value)
	}
	options, err := readServeOptions([]string{
		"-ldap-url=", "-auth-file=local.json", "-cpus=3", "-memory-budget=0", "-acme=false",
		"-image-upload-limit=10737418240", "-image-upload-timeout=20m", "-ldap-admin-group=",
	}, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if options.ldap.URL != "" || options.authFile != "local.json" || options.ldap.AdminGroup != "" {
		t.Fatalf("string overrides not applied: %#v", options)
	}
	if options.manager.CPUs != 3 || options.manager.MemoryBudgetMiB != 0 || options.certificates.ACME {
		t.Fatalf("integer/boolean overrides not applied: %#v", options)
	}
	if options.manager.MaxImageUploadBytes != 10<<30 || options.gateway.UploadTimeout != 20*time.Minute {
		t.Fatalf("byte/duration overrides not applied: %#v", options)
	}
	if options.gateway.DesktopCPUs != 3 || options.gateway.MaxImageUploadBytes != 10<<30 {
		t.Fatalf("derived settings ignored explicit flags: %#v", options.gateway)
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
	for _, key := range config.NewSettings(false).Keys() {
		if !strings.Contains(output.String(), "["+key+"]") {
			t.Errorf("help omits %s", key)
		}
	}
}
