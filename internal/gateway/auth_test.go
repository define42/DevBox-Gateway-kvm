package gateway

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/go-ldap/ldap/v3"
	"github.com/tredoe/osutil/user/crypt/sha512_crypt"
	"golang.org/x/crypto/bcrypt"
)

func TestStaticAuthenticationRequiresPrivateHashedFile(t *testing.T) {
	t.Parallel()
	hash, err := bcrypt.GenerateFromPassword([]byte("local-test-secret"), bcrypt.DefaultCost)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, user, password string
		mode                 os.FileMode
		fileHash             string
		invalid              bool
		accepted             bool
	}{
		// #nosec G101 -- synthetic password used only by this authentication test.
		{
			name:     "correct",
			user:     "alice",
			password: "local-test-secret",
			mode:     0o600,
			fileHash: string(hash),
			accepted: true,
		},
		{name: "wrong password", user: "alice", password: "incorrect", mode: 0o600, fileHash: string(hash)},
		// #nosec G101 -- synthetic password used only by this authentication test.
		{name: "unknown user", user: "bob", password: "local-test-secret", mode: 0o600, fileHash: string(hash)},
		{name: "empty password", user: "alice", mode: 0o600, fileHash: string(hash)},
		{name: "public file", mode: 0o644, fileHash: string(hash), invalid: true},
		{name: "plaintext file", mode: 0o600, fileHash: "local-test-secret", invalid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "users.json")
			data, err := json.Marshal(map[string]string{"alice": tc.fileHash})
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, data, tc.mode); err != nil {
				t.Fatal(err)
			}
			a, err := NewStaticAuthenticator(path)
			if tc.invalid {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid password file accepted: %v", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			user, err := a.Authenticate(context.Background(), tc.user, tc.password)
			if tc.accepted {
				if err != nil || user != "alice" {
					t.Fatalf("valid login rejected: %s %v", user, err)
				}
			} else if !errors.Is(err, ErrAuthentication) {
				t.Fatal("invalid login accepted")
			}
		})
	}
}

func TestLDAPConfigurationRequiresSecureTransport(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name   string
		config LDAPConfig
		valid  bool
	}{
		{name: "ldaps", config: LDAPConfig{URL: "ldaps://directory.example", BaseDN: "dc=example"}, valid: true},
		{name: "starttls", config: LDAPConfig{URL: "ldap://directory.example", BaseDN: "dc=example"}, valid: true},
		{
			name: "explicit local plaintext",
			config: LDAPConfig{
				URL:           "ldap://127.0.0.1:1389",
				BaseDN:        "dc=example",
				AllowInsecure: true,
			},
			valid: true,
		},
		{
			name: "remote plaintext",
			config: LDAPConfig{
				URL:           "ldap://192.0.2.1",
				BaseDN:        "dc=example",
				AllowInsecure: true,
			},
		},
		{
			name: "unresolved plaintext",
			config: LDAPConfig{
				URL:           "ldap://localhost",
				BaseDN:        "dc=example",
				AllowInsecure: true,
			},
		},
		{name: "missing base", config: LDAPConfig{URL: "ldaps://directory.example"}},
		// #nosec G101 -- deliberately invalid URL used to test credential rejection.
		{name: "url credentials", config: LDAPConfig{URL: "ldaps://user:pass@directory.example", BaseDN: "dc=example"}},
		{name: "wrong scheme", config: LDAPConfig{URL: "http://directory.example", BaseDN: "dc=example"}},
		{
			name: "filter without placeholder",
			config: LDAPConfig{
				URL:        "ldaps://directory.example",
				BaseDN:     "dc=example",
				UserFilter: "(uid=alice)",
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			a, err := NewLDAPAuthenticator(tc.config)
			if !tc.valid {
				if !errors.Is(err, ErrInvalid) {
					t.Fatalf("invalid config accepted: %v", err)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, err := a.Authenticate(context.Background(), "alice", ""); !errors.Is(err, ErrAuthentication) {
				t.Fatal("empty LDAP password was not rejected before dialing")
			}
			if _, err := a.Authenticate(
				context.Background(),
				"alice)(uid=*)",
				"secret",
			); !errors.Is(
				err,
				ErrAuthentication,
			) {
				t.Fatal("invalid login identifier accepted")
			}
		})
	}
}

func TestLDAPCanonicalDirectoryIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, attribute, domain, want string
		values                        map[string][]string
	}{
		{
			name: "mail", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {"johndoe@example.com"}}, want: "johndoe",
		},
		{
			name: "case insensitive attribute and domain", attribute: "mail", domain: "example.com",
			values: map[string][]string{"MAIL": {"johndoe@EXAMPLE.COM"}}, want: "johndoe",
		},
		{
			name: "bare canonical attribute", attribute: "uid",
			values: map[string][]string{"uid": {"johndoe"}}, want: "johndoe",
		},
		{
			name: "configured attribute only", attribute: "uid", domain: "example.com",
			values: map[string][]string{"uid": {"johndoe"}, "mail": {"alias@example.com", "other@example.com"}},
			want:   "johndoe",
		},
		{
			name: "user principal name", attribute: "userPrincipalName", domain: "example.com",
			values: map[string][]string{"userPrincipalName": {"johndoe@example.com"}}, want: "johndoe",
		},
		{
			name: "different domain", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {"johndoe@other.example"}},
		},
		{
			name: "unconfigured domain", attribute: "mail",
			values: map[string][]string{"mail": {"johndoe@example.com"}},
		},
		{
			name: "missing attribute", attribute: "mail", domain: "example.com",
			values: map[string][]string{"uid": {"johndoe"}},
		},
		{name: "nil entry", attribute: "mail", domain: "example.com"},
		{
			name: "no values", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {}},
		},
		{
			name: "empty value", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {""}},
		},
		{
			name: "ambiguous aliases", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {"johndoe@example.com", "alias@example.com"}},
		},
		{
			name: "duplicate values", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {"johndoe@example.com", "johndoe@example.com"}},
		},
		{
			name: "duplicate case insensitive attributes", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {"johndoe@example.com"}, "MAIL": {"other@example.com"}},
		},
		{
			name: "canonical case is never folded", attribute: "mail", domain: "example.com",
			values: map[string][]string{"mail": {"JohnDoe@example.com"}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			var entry *ldap.Entry
			if tc.values != nil {
				entry = ldap.NewEntry("uid=johndoe,dc=example,dc=com", tc.values)
			}
			got, err := canonicalLDAPUsername(entry, LDAPConfig{
				UsernameAttribute: tc.attribute,
				UserDomain:        tc.domain,
			})
			if tc.want == "" {
				if err == nil || got != "" {
					t.Fatalf("ambiguous or invalid identity accepted: %q, %v", got, err)
				}

				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("canonical directory identity = %q, %v; want %q", got, err, tc.want)
			}
		})
	}
}

func TestLDAPCanonicalDirectoryIdentityRejectsUnsafeNames(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, value string
	}{
		{name: "empty domain", value: "johndoe@"},
		{name: "empty local name", value: "@example.com"},
		{name: "multiple separators", value: "johndoe@other@example.com"},
		{name: "leading whitespace", value: " johndoe@example.com"},
		{name: "trailing whitespace", value: "johndoe@example.com "},
		{name: "path traversal", value: "../johndoe@example.com"},
		{name: "path separator", value: "john/doe@example.com"},
		{name: "control character", value: "john\ndoe@example.com"},
		{name: "too long", value: strings.Repeat("a", 33) + "@example.com"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := ldap.NewEntry("uid=johndoe,dc=example,dc=com", map[string][]string{"mail": {tc.value}})
			got, err := canonicalLDAPUsername(entry, LDAPConfig{UsernameAttribute: "mail", UserDomain: "example.com"})
			if err == nil || got != "" {
				t.Fatalf("unsafe directory identity accepted: %q, %v", got, err)
			}
		})
	}
}

func TestGuestPasswordHashUsesFreshSaltAndValidatesInput(t *testing.T) {
	t.Parallel()
	first, err := GuestPasswordHash("desktop-secret")
	if err != nil {
		t.Fatal(err)
	}
	second, err := GuestPasswordHash("desktop-secret")
	if err != nil {
		t.Fatal(err)
	}
	if first == second || !strings.HasPrefix(first, "$6$") {
		t.Fatal("password hashes lack fresh SHA-512 crypt salts")
	}
	if err := sha512_crypt.New().Verify(first, []byte("desktop-secret")); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{"", strings.Repeat("x", 1025)} {
		if _, err := GuestPasswordHash(value); !errors.Is(err, ErrInvalid) {
			t.Fatal("invalid guest password accepted")
		}
	}
}
