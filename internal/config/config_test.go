package config_test

import (
	"math"
	"os"
	"reflect"
	"sort"
	"testing"
	"time"

	"github.com/define42/DevBox-Gateway-kvm/internal/config"
)

//nolint:paralleltest // unsetEnvironment changes process-wide environment variables.
func TestNewSettingsDefaults(t *testing.T) {
	for _, key := range config.NewSettings().Keys() {
		unsetEnvironment(t, key)
	}
	settings := config.NewSettings()
	want := map[string]string{
		config.DATA_ROOT_DIR:              ".devbox-kvm-gateway",
		config.BASE_IMAGE:                 "",
		config.VM_BINARY:                  "",
		config.VM_NETWORK_POLICY:          "internet",
		config.VM_NETWORK_ALLOW_CIDRS:     "",
		config.UEFI_CODE:                  "/usr/share/OVMF/OVMF_CODE_4M.fd",
		config.UEFI_VARS_TEMPLATE:         "/usr/share/OVMF/OVMF_VARS_4M.fd",
		config.H264_ENCODER:               "software",
		config.H264_QP:                    "20",
		config.H264_SLICES:                "1",
		config.RDP_PROFILE:                "office",
		config.RDP_SURFACE_COPIES:         "true",
		config.GATEWAY_VM_PROFILING:       "false",
		config.VM_KEYBOARD:                "us",
		config.VM_VCPU_COUNT:              "2",
		config.VM_MEMORY_MIB:              "4096",
		config.VM_MEMORY_BUDGET_MIB:       "0",
		config.VM_MEMORY_OVERHEAD_MIB:     "512",
		config.VM_CGROUP_ROOT:             "",
		config.VM_DISK_MAX_SIZE:           config.DefaultVMDiskMaxSize,
		config.MAX_VDI_PER_USER:           "3",
		config.MAX_CONCURRENT_CREATIONS:   "2",
		config.MAX_RUNNING_VMS:            "4",
		config.IMAGE_UPLOAD_MAX_BYTES:     "21474836480",
		config.IMAGE_UPLOAD_TIMEOUT:       "30m0s",
		config.VM_CREATE_TIMEOUT:          "30m0s",
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

		config.MAX_HANDSHAKES:               "64",
		config.MAX_CONNECTIONS_PER_IP:       "32",
		config.MAX_HANDSHAKES_PER_IP:        "8",
		config.CONNECTION_RATE_LIMIT:        "128",
		config.CONNECTION_RATE_LIMIT_PER_IP: "8",
		config.HANDSHAKE_TIMEOUT:            "10s",
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
	settings := config.NewSettings()
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
			settings := config.NewSettings()
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

func TestParseByteSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
		want  int64
	}{
		{name: "bare bytes", value: "7", want: 7},
		{name: "bytes", value: "7B", want: 7},
		{name: "kilobytes", value: "2KB", want: 2_000},
		{name: "megabytes", value: "3MB", want: 3_000_000},
		{name: "gigabytes", value: "4GB", want: 4_000_000_000},
		{name: "terabytes", value: "5TB", want: 5_000_000_000_000},
		{name: "kibibytes", value: "2KiB", want: 2 << 10},
		{name: "mebibytes", value: "3MiB", want: 3 << 20},
		{name: "gibibytes", value: "4GiB", want: 4 << 30},
		{name: "tebibytes", value: "5TiB", want: 5 << 40},
		{name: "lowercase unit", value: "6gib", want: 6 << 30},
		{name: "maximum raw bytes", value: "9223372036854775807", want: math.MaxInt64},
		{name: "default", value: config.DefaultVMDiskMaxSize, want: 500_000_000_000},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			got, err := config.ParseByteSize(tt.value)
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("ParseByteSize(%q) = %d, want %d", tt.value, got, tt.want)
			}
		})
	}
}

func TestParseByteSizeRejectsInvalidValues(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		value string
	}{
		{name: "empty", value: ""},
		{name: "whitespace", value: " "},
		{name: "zero bytes", value: "0"},
		{name: "zero gigabytes", value: "0GB"},
		{name: "negative", value: "-1GB"},
		{name: "positive sign", value: "+1GB"},
		{name: "fraction", value: "1.5GB"},
		{name: "leading fraction", value: ".5GB"},
		{name: "space before unit", value: "1 GB"},
		{name: "trailing space", value: "1GB "},
		{name: "missing number", value: "GB"},
		{name: "long unit", value: "1bytes"},
		{name: "petabytes", value: "1PB"},
		{name: "incomplete iec unit", value: "1Gi"},
		{name: "unit suffix", value: "1GBx"},
		{name: "raw overflow", value: "9223372036854775808"},
		{name: "multiplication overflow", value: "8388608TiB"},
		{name: "non-ascii digit", value: "١GB"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			if _, err := config.ParseByteSize(tt.value); err == nil {
				t.Fatalf("ParseByteSize(%q) succeeded", tt.value)
			}
		})
	}
}

func TestNewSettingsSnapshot(t *testing.T) {
	t.Setenv(config.DATA_ROOT_DIR, "/first")
	t.Setenv(config.VM_VCPU_COUNT, "3")
	first := config.NewSettings()
	t.Setenv(config.DATA_ROOT_DIR, "/second")
	t.Setenv(config.VM_VCPU_COUNT, "8")
	second := config.NewSettings()
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
			if got := config.NewSettings().Get(config.DATA_ROOT_DIR); got != tt.want {
				t.Errorf("Get(DATA_ROOT_DIR) = %q, want %q", got, tt.want)
			}
		})
	}
	t.Run("trimmed default", func(t *testing.T) {
		unsetEnvironment(t, "CONFIG_TEST_STRING")
		settings := config.NewSettings()
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
			settings := config.NewSettings()
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

	settings := config.NewSettings()
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

func TestSettingsKeys(t *testing.T) {
	t.Parallel()

	settings := config.NewSettings()
	keys := settings.Keys()
	if !sort.StringsAreSorted(keys) {
		t.Error("Keys must be sorted")
	}
	keys[0] = "changed"
	if reflect.DeepEqual(keys, settings.Keys()) {
		t.Error("modifying Keys changed the settings")
	}
}

func unsetEnvironment(t *testing.T, key string) {
	t.Helper()
	// Setenv arranges restoration, including for originally absent variables.
	t.Setenv(key, "")
	if err := os.Unsetenv(key); err != nil {
		t.Fatal(err)
	}
}
