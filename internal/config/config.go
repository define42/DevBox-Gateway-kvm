// Package config defines the gateway's environment-backed runtime settings.
// Settings registers defaults and captures environment overrides; the command
// layer applies flags and validates the resulting runtime configuration.
package config

import (
	"errors"
	"fmt"
	"math"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Environment-backed configuration keys follow DevBox-Gateway's naming convention.
const (
	DATA_ROOT_DIR              = "DATA_ROOT_DIR"
	BASE_IMAGE                 = "BASE_IMAGE"
	VM_BINARY                  = "VM_BINARY"
	VM_GPU_3D                  = "VM_GPU_3D"
	VM_NETWORK_POLICY          = "VM_NETWORK_POLICY"
	VM_NETWORK_ALLOW_CIDRS     = "VM_NETWORK_ALLOW_CIDRS"
	UEFI_CODE                  = "UEFI_CODE"
	UEFI_VARS_TEMPLATE         = "UEFI_VARS_TEMPLATE"
	H264_ENCODER               = "H264_ENCODER"
	H264_QP                    = "H264_QP"
	RDP_PROFILE                = "RDP_PROFILE"
	GATEWAY_VM_PROFILING       = "GATEWAY_VM_PROFILING"
	VM_KEYBOARD                = "VM_KEYBOARD"
	VM_VCPU_COUNT              = "VM_VCPU_COUNT"
	VM_MEMORY_MIB              = "VM_MEMORY_MIB"
	VM_MEMORY_BUDGET_MIB       = "VM_MEMORY_BUDGET_MIB"
	VM_MEMORY_OVERHEAD_MIB     = "VM_MEMORY_OVERHEAD_MIB"
	VM_CGROUP_ROOT             = "VM_CGROUP_ROOT"
	VM_DISK_MAX_SIZE           = "VM_DISK_MAX_SIZE"
	MAX_VDI_PER_USER           = "MAX_VDI_PER_USER"
	MAX_CONCURRENT_CREATIONS   = "MAX_CONCURRENT_CREATIONS"
	MAX_RUNNING_VMS            = "MAX_RUNNING_VMS"
	IMAGE_UPLOAD_MAX_BYTES     = "IMAGE_UPLOAD_MAX_BYTES"
	IMAGE_UPLOAD_TIMEOUT       = "IMAGE_UPLOAD_TIMEOUT"
	VM_CREATE_TIMEOUT          = "VM_CREATE_TIMEOUT"
	VM_START_TIMEOUT           = "VM_START_TIMEOUT"
	VM_STOP_TIMEOUT            = "VM_STOP_TIMEOUT"
	LISTEN_ADDR                = "LISTEN_ADDR"
	PUBLIC_ADDRESS             = "PUBLIC_ADDRESS"
	CERT_FILE                  = "CERT_FILE"
	KEY_FILE                   = "KEY_FILE"
	ACME_ENABLE                = "ACME_ENABLE"
	ACME_EMAIL                 = "ACME_EMAIL"
	ACME_CA                    = "ACME_CA"
	AUTH_FILE                  = "AUTH_FILE"
	LDAP_URL                   = "LDAP_URL"
	LDAP_BASE_DN               = "LDAP_BASE_DN"
	LDAP_USER_DOMAIN           = "LDAP_USER_DOMAIN"
	LDAP_USER_FILTER           = "LDAP_USER_FILTER"
	LDAP_USERNAME_ATTRIBUTE    = "LDAP_USERNAME_ATTRIBUTE"
	LDAP_REQUIRED_GROUP        = "LDAP_REQUIRED_GROUP"
	ADMIN_GROUP                = "ADMIN_GROUP"
	LDAP_CA_FILE               = "LDAP_CA_FILE"
	LDAP_INSECURE_LOOPBACK     = "LDAP_INSECURE_LOOPBACK"
	LDAP_AUTH_TIMEOUT          = "LDAP_AUTH_TIMEOUT"
	SESSION_TTL                = "SESSION_TTL"
	MAX_SESSIONS               = "MAX_SESSIONS"
	MAX_CONCURRENT_CONNECTIONS = "MAX_CONCURRENT_CONNECTIONS"

	MAX_HANDSHAKES               = "MAX_HANDSHAKES"
	MAX_CONNECTIONS_PER_IP       = "MAX_CONNECTIONS_PER_IP"
	MAX_HANDSHAKES_PER_IP        = "MAX_HANDSHAKES_PER_IP"
	CONNECTION_RATE_LIMIT        = "CONNECTION_RATE_LIMIT"
	CONNECTION_RATE_LIMIT_PER_IP = "CONNECTION_RATE_LIMIT_PER_IP"
	HANDSHAKE_TIMEOUT            = "HANDSHAKE_TIMEOUT"
)

// DefaultMaxImageUploadBytes bounds uploaded and normalized catalog files.
const DefaultMaxImageUploadBytes int64 = 20 << 30

// DefaultVMDiskMaxSize is the default maximum virtual capacity of a managed disk.
const DefaultVMDiskMaxSize = "500GB"

var errInvalidByteSize = errors.New("invalid byte size")

// Kind identifies the underlying value type stored in a setting.
type Kind uint8

// Kind values supported by Settings.
const (
	KindString Kind = iota
	KindInt
	KindInt64
	KindBool
	KindDuration
)

// Setting stores a single configuration entry and its parsed value.
type Setting struct {
	Description string
	Kind        Kind
	Raw         string
	S           string
	I           int
	I64         int64
	B           bool
	D           time.Duration
}

// Settings holds a snapshot of the process configuration. Construct it with
// NewSettings and finish registration before sharing it between goroutines;
// setters are not synchronized. Getters never read the environment again.
type Settings struct {
	m map[string]*Setting
}

// NewSettings registers gateway defaults and applies environment overrides.
// Invalid integer, boolean, and duration overrides retain their defaults.
// Strings preserve explicit empty values; surrounding whitespace is removed.
// It does not parse command-line flags or validate runtime constraints.
func NewSettings() *Settings {
	s := &Settings{m: make(map[string]*Setting)}

	s.SetString(DATA_ROOT_DIR, "Private persistent VM state directory", ".devbox-kvm-gateway")
	s.SetString(BASE_IMAGE, "Optional immutable standalone uncompressed QCOW2 default image", "")
	s.SetString(VM_BINARY, "VM worker executable; empty uses the current executable", "")
	s.SetBool(VM_GPU_3D, "Enable guest OpenGL through virglrenderer (requires a virgl build and host EGL)", false)
	s.SetString(VM_NETWORK_POLICY,
		"Managed IPv4 access: internet (public destinations) or allowlist (explicit CIDRs only)", "internet")
	s.SetString(VM_NETWORK_ALLOW_CIDRS, "Comma-separated IPv4 CIDRs allowed in addition to the managed network policy; "+
		"empty denies LAN/host access", "")
	s.SetString(UEFI_CODE, "OVMF CODE firmware", "/usr/share/OVMF/OVMF_CODE_4M.fd")
	s.SetString(UEFI_VARS_TEMPLATE, "Matching pristine OVMF variables template", "/usr/share/OVMF/OVMF_VARS_4M.fd")
	s.SetString(H264_ENCODER, "H.264 encoder: auto, nvenc, software", "auto")
	s.SetInt(H264_QP, "H.264 quantizer: min 0, max 51; lower = higher quality/more bandwidth, "+
		"higher = more compression", 20)
	s.SetString(RDP_PROFILE, "Desktop graphics profile: office (30 FPS), smooth (60 FPS)", "office")
	s.SetBool(GATEWAY_VM_PROFILING, "Enable private, on-demand VM worker CPU profiling sockets", false)
	s.SetString(VM_KEYBOARD, "Guest keyboard layout", "us")
	s.SetInt(VM_VCPU_COUNT, "vCPUs per desktop", 2)
	s.SetInt(VM_MEMORY_MIB, "MiB per desktop", 4096)
	s.SetInt(VM_MEMORY_BUDGET_MIB, "Maximum MiB reserved for running desktops including overhead (0: automatic)", 0)
	s.SetInt(VM_MEMORY_OVERHEAD_MIB, "Per-worker host memory allowance above guest RAM, enforced by cgroup", 512)
	s.SetString(VM_CGROUP_ROOT, "Delegated cgroup v2 directory for memory-confined VM workers; required to start VMs", "")
	s.SetString(VM_DISK_MAX_SIZE, "Maximum virtual disk capacity per template and desktop", DefaultVMDiskMaxSize)
	s.SetInt(MAX_VDI_PER_USER, "Desktop quota per account", 3)
	s.SetInt(MAX_CONCURRENT_CREATIONS, "Maximum simultaneous desktop provisioning operations", 2)
	s.SetInt(MAX_RUNNING_VMS, "Global running desktop limit", 4)
	s.SetInt64(IMAGE_UPLOAD_MAX_BYTES, "Maximum uploaded qcow2 file size in bytes", DefaultMaxImageUploadBytes)
	s.SetDuration(IMAGE_UPLOAD_TIMEOUT, "Maximum image upload and import duration", 30*time.Minute)
	s.SetDuration(VM_CREATE_TIMEOUT, "Maximum desktop creation duration including queue wait", 30*time.Minute)
	s.SetDuration(VM_START_TIMEOUT, "Maximum time to wait for a desktop to start", 30*time.Second)
	s.SetDuration(VM_STOP_TIMEOUT, "Maximum time to wait for a desktop to stop", 30*time.Second)

	s.SetString(LISTEN_ADDR, "Shared HTTPS and RDP TCP listener", "127.0.0.1:8443")
	s.SetString(PUBLIC_ADDRESS, "Hostname:port written to downloaded RDP files", "localhost:8443")
	s.SetString(CERT_FILE, "Optional PEM server certificate; otherwise generate a certificate or use ACME", "")
	s.SetString(KEY_FILE, "PEM private key matching CERT_FILE", "")
	s.SetBool(ACME_ENABLE, "Obtain and renew a certificate for the public hostname using ACME TLS-ALPN-01", false)
	s.SetString(ACME_EMAIL, "ACME account email address", "")
	s.SetString(ACME_CA, "ACME CA: production, staging, or directory URL", "production")

	s.SetString(AUTH_FILE, "Private bcrypt JSON file for loopback development; excludes LDAP", "")
	s.SetString(LDAP_URL, "LDAP URL; ldap:// upgrades with StartTLS", "")
	s.SetString(LDAP_BASE_DN, "LDAP search base DN", "")
	s.SetString(LDAP_USER_DOMAIN, "Optional UPN domain for user binding", "")
	s.SetString(LDAP_USER_FILTER, "LDAP filter with one %s placeholder for the bind identifier", "")
	s.SetString(LDAP_USERNAME_ATTRIBUTE, "Canonical account attribute: bare Linux name or name@LDAP_USER_DOMAIN", "uid")
	s.SetString(LDAP_REQUIRED_GROUP, "Optional required memberOf group DN", "")
	s.SetString(ADMIN_GROUP, "Direct memberOf group name or DN granting administrator access; empty disables admins", "")
	s.SetString(LDAP_CA_FILE, "Private LDAP CA PEM file", "")
	s.SetBool(LDAP_INSECURE_LOOPBACK, "Allow plaintext LDAP only on a loopback IP for tests", false)
	s.SetDuration(LDAP_AUTH_TIMEOUT, "Maximum duration of an LDAP authentication attempt", 10*time.Second)
	s.SetDuration(SESSION_TTL, "Authenticated session lifetime", 8*time.Hour)
	s.SetInt(MAX_SESSIONS, "Maximum number of authenticated sessions", 1024)
	s.SetInt(MAX_CONCURRENT_CONNECTIONS, "Maximum simultaneous authenticated HTTPS and RDP connections", 128)
	s.SetInt(MAX_HANDSHAKES, "Maximum simultaneous pre-authentication connections", 64)
	s.SetInt(MAX_CONNECTIONS_PER_IP, "Maximum simultaneous connections per IPv4 address or IPv6 /64", 32)
	s.SetInt(MAX_HANDSHAKES_PER_IP, "Maximum simultaneous pre-authentication connections per IPv4 address or IPv6 /64", 8)
	s.SetInt(CONNECTION_RATE_LIMIT, "New TCP connections admitted per second globally, with an equal burst", 128)
	s.SetInt(CONNECTION_RATE_LIMIT_PER_IP,
		"New TCP connections admitted per second per IPv4 address or IPv6 /64, with an equal burst", 8)
	s.SetDuration(HANDSHAKE_TIMEOUT, "Maximum connection lifetime before application authentication", 10*time.Second)

	return s
}

// SetString registers a string, using a present environment value even if empty.
func (s *Settings) SetString(id, description, defaultValue string) {
	raw := environmentValue(id, defaultValue)
	s.m[id] = &Setting{Description: description, Kind: KindString, Raw: raw, S: raw}
}

// SetInt registers an integer, retaining its default when parsing fails.
func (s *Settings) SetInt(id, description string, defaultValue int) {
	raw := environmentValue(id, strconv.Itoa(defaultValue))
	value, err := strconv.Atoi(raw)
	if err != nil {
		value, raw = defaultValue, strconv.Itoa(defaultValue)
	}
	s.m[id] = &Setting{Description: description, Kind: KindInt, Raw: raw, I: value}
}

// SetInt64 registers a 64-bit integer, retaining its default when parsing fails.
func (s *Settings) SetInt64(id, description string, defaultValue int64) {
	raw := environmentValue(id, strconv.FormatInt(defaultValue, 10))
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		value, raw = defaultValue, strconv.FormatInt(defaultValue, 10)
	}
	s.m[id] = &Setting{Description: description, Kind: KindInt64, Raw: raw, I64: value}
}

// SetBool registers a boolean, retaining its default when parsing fails.
func (s *Settings) SetBool(id, description string, defaultValue bool) {
	raw := environmentValue(id, strconv.FormatBool(defaultValue))
	value, err := strconv.ParseBool(raw)
	if err != nil {
		value, raw = defaultValue, strconv.FormatBool(defaultValue)
	}
	s.m[id] = &Setting{Description: description, Kind: KindBool, Raw: raw, B: value}
}

// SetDuration registers a duration, retaining its default when parsing fails.
func (s *Settings) SetDuration(id, description string, defaultValue time.Duration) {
	raw := environmentValue(id, defaultValue.String())
	value, err := time.ParseDuration(raw)
	if err != nil {
		value, raw = defaultValue, defaultValue.String()
	}
	s.m[id] = &Setting{Description: description, Kind: KindDuration, Raw: raw, D: value}
}

// Has reports whether the named setting is registered.
func (s *Settings) Has(id string) bool {
	_, ok := s.m[id]

	return ok
}

// Get returns String(id), including an empty string for an unknown setting.
func (s *Settings) Get(id string) string { return s.String(id) }

// String formats any registered setting as a string. Unknown settings return an
// empty string.
func (s *Settings) String(id string) string {
	st, ok := s.m[id]
	if !ok {
		return ""
	}
	switch st.Kind {
	case KindString:
		return st.S
	case KindInt:
		return strconv.Itoa(st.I)
	case KindInt64:
		return strconv.FormatInt(st.I64, 10)
	case KindBool:
		return strconv.FormatBool(st.B)
	case KindDuration:
		return st.D.String()
	default:
		return st.Raw
	}
}

// Int returns an integer setting or parses another kind's raw value.
// Unknown settings and failed conversions return zero.
func (s *Settings) Int(id string) int {
	st, ok := s.m[id]
	if !ok {
		return 0
	}
	if st.Kind == KindInt {
		return st.I
	}
	value, err := strconv.Atoi(st.Raw)
	if err != nil {
		return 0
	}

	return value
}

// Int64 returns a 64-bit integer setting or parses another kind's raw value.
// Unknown settings and failed conversions return zero.
func (s *Settings) Int64(id string) int64 {
	st, ok := s.m[id]
	if !ok {
		return 0
	}
	if st.Kind == KindInt64 {
		return st.I64
	}
	value, err := strconv.ParseInt(st.Raw, 10, 64)
	if err != nil {
		return 0
	}

	return value
}

// Bool returns a boolean setting or parses another kind's raw value.
// Unknown settings and failed conversions return false.
func (s *Settings) Bool(id string) bool {
	st, ok := s.m[id]
	if !ok {
		return false
	}
	if st.Kind == KindBool {
		return st.B
	}
	value, err := strconv.ParseBool(st.Raw)

	return err == nil && value
}

// Duration returns a duration setting or parses another kind's raw value.
// Unknown settings and failed conversions return zero.
func (s *Settings) Duration(id string) time.Duration {
	st, ok := s.m[id]
	if !ok {
		return 0
	}
	if st.Kind == KindDuration {
		return st.D
	}
	value, err := time.ParseDuration(st.Raw)
	if err != nil {
		return 0
	}

	return value
}

// Description returns a setting's help text, or an empty string for an unknown ID.
func (s *Settings) Description(id string) string {
	if st, ok := s.m[id]; ok {
		return st.Description
	}

	return ""
}

// Keys returns registered environment names in alphabetical order.
func (s *Settings) Keys() []string {
	keys := make([]string, 0, len(s.m))
	for id := range s.m {
		keys = append(keys, id)
	}
	sort.Strings(keys)

	return keys
}

// ParseByteSize parses a positive integer byte count. SI units use powers of
// 1000, IEC units use powers of 1024, and a value without a unit means bytes.
func ParseByteSize(value string) (int64, error) {
	if value == "" || value[0] < '0' || value[0] > '9' {
		return 0, fmt.Errorf("%w %q: must start with a positive integer", errInvalidByteSize, value)
	}

	unitStart := 0
	for unitStart < len(value) && value[unitStart] >= '0' && value[unitStart] <= '9' {
		unitStart++
	}

	var multiplier int64
	switch strings.ToUpper(value[unitStart:]) {
	case "", "B":
		multiplier = 1
	case "KB":
		multiplier = 1_000
	case "MB":
		multiplier = 1_000_000
	case "GB":
		multiplier = 1_000_000_000
	case "TB":
		multiplier = 1_000_000_000_000
	case "KIB":
		multiplier = 1 << 10
	case "MIB":
		multiplier = 1 << 20
	case "GIB":
		multiplier = 1 << 30
	case "TIB":
		multiplier = 1 << 40
	default:
		return 0, fmt.Errorf("%w %q: unsupported unit", errInvalidByteSize, value)
	}

	number, err := strconv.ParseInt(value[:unitStart], 10, 64)
	if err != nil || number <= 0 {
		return 0, fmt.Errorf("%w %q: must contain a positive integer", errInvalidByteSize, value)
	}
	if number > math.MaxInt64/multiplier {
		return 0, fmt.Errorf("%w %q: exceeds the supported range", errInvalidByteSize, value)
	}

	return number * multiplier, nil
}

func environmentValue(id, fallback string) string {
	if value, ok := os.LookupEnv(id); ok {
		return strings.TrimSpace(value)
	}

	return strings.TrimSpace(fallback)
}
