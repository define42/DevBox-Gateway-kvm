package config_test

import (
	"bytes"
	"errors"
	"io"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/config"
)

//nolint:paralleltest // unsetEnvironment changes process-wide environment variables.
func TestNewSettingsDefaults(t *testing.T) {
	for _, key := range config.NewSettings(false).Keys() {
		unsetEnvironment(t, key)
	}
	settings := config.NewSettings(false)
	want := map[string]string{
		config.DATA_ROOT_DIR:              ".gokvm-gateway",
		config.BASE_IMAGE:                 "",
		config.VM_BINARY:                  "",
		config.UEFI_CODE:                  "/usr/share/OVMF/OVMF_CODE_4M.fd",
		config.UEFI_VARS_TEMPLATE:         "/usr/share/OVMF/OVMF_VARS_4M.fd",
		config.H264_ENCODER:               "auto",
		config.VM_KEYBOARD:                "us",
		config.VM_VCPU_COUNT:              "2",
		config.VM_MEMORY_MIB:              "4096",
		config.VM_MEMORY_BUDGET_MIB:       "0",
		config.MAX_VDI_PER_USER:           "3",
		config.MAX_RUNNING_VMS:            "4",
		config.IMAGE_UPLOAD_MAX_BYTES:     "21474836480",
		config.IMAGE_UPLOAD_TIMEOUT:       "30m0s",
		config.VM_START_TIMEOUT:           "30s",
		config.VM_STOP_TIMEOUT:            "30s",
		config.LISTEN_ADDR:                "127.0.0.1:8443",
		config.PUBLIC_ADDRESS:             "localhost:8443",
		config.CERT_FILE:                  "",
		config.KEY_FILE:                   "",
		config.ACME_ENABLE:                "false",
		config.ACME_EMAIL:                 "",
		config.ACME_CA:                    "production",
		config.AUTH_FILE:                  "",
		config.LDAP_URL:                   "",
		config.LDAP_BASE_DN:               "",
		config.LDAP_USER_DOMAIN:           "",
		config.LDAP_USER_FILTER:           "",
		config.LDAP_USERNAME_ATTRIBUTE:    "uid",
		config.LDAP_REQUIRED_GROUP:        "",
		config.ADMIN_GROUP:                "",
		config.LDAP_CA_FILE:               "",
		config.LDAP_INSECURE_LOOPBACK:     "false",
		config.LDAP_AUTH_TIMEOUT:          "10s",
		config.SESSION_TTL:                "8h0m0s",
		config.MAX_SESSIONS:               "1024",
		config.MAX_CONCURRENT_CONNECTIONS: "128",
	}
	if len(settings.Keys()) != len(want) {
		t.Fatalf("registered %d settings, want %d", len(settings.Keys()), len(want))
	}
	for key, value := range want {
		t.Run(key, func(t *testing.T) {
			if !settings.Has(key) {
				t.Fatal("setting is not registered")
			}
			if got := settings.Get(key); got != value {
				t.Errorf(
					"Get(%q) = %q, want %q",
					key,
					got,
					value,
				)
			}
			if settings.Description(key) == "" {
				t.Error("setting has no help text")
			}
		})
	}
}

func TestNewSettingsEnvironmentOverrides(t *testing.T) {
	t.Setenv(config.DATA_ROOT_DIR, "  /srv/desktops \n")
	t.Setenv(config.VM_VCPU_COUNT, "\t6 ")
	t.Setenv(config.IMAGE_UPLOAD_MAX_BYTES, " 42949672960 ")
	t.Setenv(config.ACME_ENABLE, " TRUE ")
	t.Setenv(config.VM_START_TIMEOUT, " 1m15s ")
	settings := config.NewSettings(false)
	if got := settings.String(config.DATA_ROOT_DIR); got != "/srv/desktops" {
		t.Errorf("data root = %q, want /srv/desktops", got)
	}
	if got := settings.Int(config.VM_VCPU_COUNT); got != 6 {
		t.Errorf("CPU count = %d, want 6", got)
	}
	if got := settings.Int64(config.IMAGE_UPLOAD_MAX_BYTES); got != 40<<30 {
		t.Errorf("upload limit = %d, want 40 GiB", got)
	}
	if !settings.Bool(config.ACME_ENABLE) {
		t.Error("ACME must be enabled")
	}
	if got := settings.Duration(config.VM_START_TIMEOUT); got != 75*time.Second {
		t.Errorf("start timeout = %s, want 1m15s", got)
	}
	if got := settings.Get(config.ACME_ENABLE); got != "true" {
		t.Errorf("formatted boolean = %q, want true", got)
	}
}

func TestNewSettingsInvalidOverridesRetainDefaults(t *testing.T) {
	tests := []struct {
		name  string
		value string
	}{
		{name: "invalid", value: "invalid"},
		{name: "empty", value: ""},
		{name: "whitespace", value: " \t\n "},
		{name: "integer overflow", value: "9223372036854775808"},
		{name: "duration overflow", value: "9223372036854775808h"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.VM_VCPU_COUNT, tt.value)
			t.Setenv(config.IMAGE_UPLOAD_MAX_BYTES, tt.value)
			t.Setenv(config.ACME_ENABLE, tt.value)
			t.Setenv(config.VM_START_TIMEOUT, tt.value)
			settings := config.NewSettings(false)
			if got := settings.Int(config.VM_VCPU_COUNT); got != 2 {
				t.Errorf("CPU count = %d, want default 2", got)
			}
			if got := settings.Int64(config.IMAGE_UPLOAD_MAX_BYTES); got != 20<<30 {
				t.Errorf("upload limit = %d, want default 20 GiB", got)
			}
			if settings.Bool(config.ACME_ENABLE) {
				t.Error("invalid ACME override must retain false")
			}
			if got := settings.Duration(config.VM_START_TIMEOUT); got != 30*time.Second {
				t.Errorf("start timeout = %s, want default 30s", got)
			}
			if got := settings.Get(config.VM_VCPU_COUNT); got != "2" {
				t.Errorf("formatted CPU count = %q, want default 2", got)
			}
		})
	}
}

func TestNewSettingsSnapshot(t *testing.T) {
	t.Setenv(config.DATA_ROOT_DIR, "/first")
	t.Setenv(config.VM_VCPU_COUNT, "3")
	first := config.NewSettings(false)
	t.Setenv(config.DATA_ROOT_DIR, "/second")
	t.Setenv(config.VM_VCPU_COUNT, "8")
	second := config.NewSettings(false)
	if first.Get(config.DATA_ROOT_DIR) != "/first" || first.Int(config.VM_VCPU_COUNT) != 3 {
		t.Error("environment changes modified an existing snapshot")
	}
	if second.Get(config.DATA_ROOT_DIR) != "/second" || second.Int(config.VM_VCPU_COUNT) != 8 {
		t.Error("new snapshot did not read the updated environment")
	}
}

func TestSettingsSetString(t *testing.T) {
	tests := []struct {
		name  string
		value string
		want  string
	}{
		{name: "empty override", value: "", want: ""},
		{name: "whitespace override", value: " \t\n", want: ""},
		{name: "trimmed override", value: " example \n", want: "example"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv(config.DATA_ROOT_DIR, tt.value)
			if got := config.NewSettings(false).Get(config.DATA_ROOT_DIR); got != tt.want {
				t.Errorf("Get(DATA_ROOT_DIR) = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("trimmed default", func(t *testing.T) {
		unsetEnvironment(t, "CONFIG_TEST_STRING")
		settings := config.NewSettings(false)
		settings.SetString("CONFIG_TEST_STRING", "Example", " default \n")
		if got := settings.Get("CONFIG_TEST_STRING"); got != "default" {
			t.Errorf("default = %q, want default", got)
		}
	})
}

func TestSettingsConversions(t *testing.T) {
	tests := []struct {
		name      string
		value     string
		integer   int
		integer64 int64
		boolean   bool
		duration  time.Duration
	}{
		{name: "integer", value: "42", integer: 42, integer64: 42},
		{name: "boolean", value: "true", boolean: true},
		{name: "duration", value: "90s", duration: 90 * time.Second},
		{name: "empty", value: ""},
		{name: "invalid", value: "unknown"},
		{name: "overflow", value: "99999999999999999999999999999"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Setenv("CONFIG_TEST_VALUE", tt.value)
			settings := config.NewSettings(false)
			settings.SetString("CONFIG_TEST_VALUE", "Example", "")
			if got := settings.Int("CONFIG_TEST_VALUE"); got != tt.integer {
				t.Errorf("Int = %d, want %d", got, tt.integer)
			}
			if got := settings.Int64("CONFIG_TEST_VALUE"); got != tt.integer64 {
				t.Errorf("Int64 = %d, want %d", got, tt.integer64)
			}
			if got := settings.Bool("CONFIG_TEST_VALUE"); got != tt.boolean {
				t.Errorf("Bool = %t, want %t", got, tt.boolean)
			}
			if got := settings.Duration("CONFIG_TEST_VALUE"); got != tt.duration {
				t.Errorf("Duration = %s, want %s", got, tt.duration)
			}
		})
	}
}

func TestSettingsUnknown(t *testing.T) {
	t.Parallel()

	settings := config.NewSettings(false)
	const unknown = "CONFIG_TEST_UNREGISTERED"
	if settings.Has(unknown) {
		t.Error("Has must be false for an unregistered setting")
	}
	if settings.Get(unknown) != "" || settings.String(unknown) != "" {
		t.Error("unknown strings must be empty")
	}
	if settings.Int(unknown) != 0 || settings.Int64(unknown) != 0 {
		t.Error("unknown integers must be zero")
	}
	if settings.Bool(unknown) {
		t.Error("unknown booleans must be false")
	}
	if settings.Duration(unknown) != 0 {
		t.Error("unknown durations must be zero")
	}
	if settings.Description(unknown) != "" {
		t.Error("unknown descriptions must be empty")
	}
}

func TestSettingsPrint(t *testing.T) {
	t.Setenv("CONFIG_TEST_SECRET", " hidden-credential ")
	t.Setenv(config.DATA_ROOT_DIR, "/srv/desktops")
	settings := config.NewSettings(false)
	settings.SetSecretString("CONFIG_TEST_SECRET", "Test credential", "")
	var output bytes.Buffer
	if err := settings.Print(&output); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "hidden-credential") {
		t.Fatal("Print exposed a secret")
	}
	if !strings.Contains(output.String(), "******") || !strings.Contains(output.String(), "/srv/desktops") {
		t.Error("Print must include masked secrets and effective non-secret values")
	}
	if got := settings.Get("CONFIG_TEST_SECRET"); got != "hidden-credential" {
		t.Errorf("Get(secret) = %q, want the unmasked credential", got)
	}
	keys := settings.Keys()
	if !sort.StringsAreSorted(keys) {
		t.Error("Keys must be sorted")
	}
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != len(keys)+1 {
		t.Fatalf("Print emitted %d lines, want %d", len(lines), len(keys)+1)
	}
	for index, key := range keys {
		if got := strings.Fields(lines[index+1])[0]; got != key {
			t.Errorf(
				"printed row %d = %q, want %q",
				index,
				got,
				key,
			)
		}
	}
	keys[0] = "changed"
	if reflect.DeepEqual(keys, settings.Keys()) {
		t.Error("modifying Keys changed the settings")
	}

	wantErr := io.ErrClosedPipe
	if err := settings.Print(errorWriter{err: wantErr}); !errors.Is(err, wantErr) {
		t.Errorf("Print error = %v, want %v", err, wantErr)
	}
}

type errorWriter struct {
	err error
}

func (w errorWriter) Write([]byte) (int, error) {
	return 0, w.err
}

func unsetEnvironment(t *testing.T, key string) {
	t.Helper()
	// Setenv arranges restoration, including for originally absent variables.
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}
