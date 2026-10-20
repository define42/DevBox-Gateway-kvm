package gateway

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// These opt-in tests exercise the copied GLAuth server, including certificate
// verification, mail binding, canonical owners, and the required access group.
// Run with make gateway-ldap-test; ordinary Go tests require no Docker daemon.
func bundledLDAPConfig(t *testing.T) LDAPConfig {
	t.Helper()
	address := os.Getenv("DEVBOX_KVM_TEST_LDAP_URL")
	if address == "" {
		t.Skip("run make gateway-ldap-test to test the bundled GLAuth directory")
	}
	ca := os.Getenv("DEVBOX_KVM_TEST_LDAP_CA")
	if ca == "" {
		t.Fatal("DEVBOX_KVM_TEST_LDAP_CA is required with DEVBOX_KVM_TEST_LDAP_URL")
	}

	return LDAPConfig{
		URL:               address,
		BaseDN:            "dc=glauth,dc=com",
		UserDomain:        "example.com",
		UserFilter:        "(mail=%s)",
		UsernameAttribute: "mail",
		RequiredGroup:     "ou=team10_r,ou=groups,dc=glauth,dc=com",
		AdminGroup:        "devbox-admins",
		CAFile:            ca,
	}
}

func TestBundledLDAPAuthentication(t *testing.T) {
	t.Parallel()
	config := bundledLDAPConfig(t)
	for _, tc := range []struct {
		name, user, password, owner string
		withoutGroup                bool
	}{
		{name: "primary account", user: "johndoe", password: "dogood", owner: "johndoe"},
		{name: "test account", user: "testuser", password: "dogood", owner: "testuser"},
		{name: "administrator account", user: "administrator", password: "dogood", owner: "administrator"},
		{name: "wrong password", user: "johndoe", password: "incorrect"},
		{name: "empty password", user: "johndoe"},
		{name: "unknown account", user: "missing", password: "dogood"},
		{name: "qualified login", user: "johndoe@example.com", password: "dogood"},
		{name: "missing required group", user: "serviceuser", password: "mysecret"},
		{
			name: "service account without group restriction", user: "serviceuser",
			password: "mysecret", owner: "serviceuser", withoutGroup: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config
			if tc.withoutGroup {
				cfg.RequiredGroup = ""
			}
			auth, err := NewLDAPAuthenticator(cfg)
			if err != nil {
				t.Fatal(err)
			}
			owner, err := auth.Authenticate(t.Context(), tc.user, tc.password)
			if tc.owner == "" {
				if !errors.Is(err, ErrAuthentication) || owner != "" {
					t.Fatalf("unauthorized account accepted: owner %q, error %v", owner, err)
				}

				return
			}
			if err != nil || owner != tc.owner {
				t.Fatalf("owner %q, error %v; want owner %q", owner, err, tc.owner)
			}
		})
	}
}

func TestBundledLDAPRequiresTrustedCertificate(t *testing.T) {
	t.Parallel()
	config := bundledLDAPConfig(t)
	cert, _, err := NewTLSIdentity("unrelated-directory.test")
	if err != nil {
		t.Fatal(err)
	}
	config.CAFile = filepath.Join(t.TempDir(), "other-ca.pem")
	if err := os.WriteFile(config.CAFile, cert, 0o600); err != nil {
		t.Fatal(err)
	}
	auth, err := NewLDAPAuthenticator(config)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := auth.Authenticate(t.Context(), "johndoe", "dogood")
	if !errors.Is(err, ErrAuthentication) || owner != "" {
		t.Fatalf("untrusted directory accepted: owner %q, error %v", owner, err)
	}
}

func TestBundledLDAPAdministratorRole(t *testing.T) {
	t.Parallel()
	config := bundledLDAPConfig(t)
	for _, tc := range []struct {
		name, username, adminGroup string
		wantAdmin                  bool
	}{
		{name: "administrator", username: "administrator", adminGroup: "devbox-admins", wantAdmin: true},
		{name: "ordinary user", username: "johndoe", adminGroup: "devbox-admins"},
		{name: "other groups", username: "testuser", adminGroup: "devbox-admins"},
		{name: "administrator role disabled", username: "administrator"},
		{
			name: "full administrator DN", username: "administrator",
			adminGroup: "ou=devbox-admins,ou=groups,dc=glauth,dc=com", wantAdmin: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			cfg := config
			cfg.AdminGroup = tc.adminGroup
			auth, err := NewLDAPAuthenticator(cfg)
			if err != nil {
				t.Fatal(err)
			}
			identity, err := authenticateIdentity(t.Context(), auth, tc.username, "dogood")
			if err != nil || identity.Username != tc.username || identity.IsAdmin != tc.wantAdmin {
				t.Fatalf("identity = %#v, %v; want %q, admin %v", identity, err, tc.username, tc.wantAdmin)
			}
		})
	}
}
