package gateway

import (
	"context"
	"errors"
	"testing"

	"github.com/go-ldap/ldap/v3"
)

func TestLDAPAdministratorRequiresDirectConfiguredGroup(t *testing.T) {
	t.Parallel()
	const adminDN = "ou=devbox-admins,ou=groups,dc=glauth,dc=com"
	const usersDN = "ou=team10_r,ou=groups,dc=glauth,dc=com"
	for _, tc := range []struct {
		name, username, adminGroup string
		groups                     []string
		wantAdmin, denied          bool
	}{
		{
			name: "bare group", username: "alice", adminGroup: "devbox-admins",
			groups: []string{usersDN, adminDN}, wantAdmin: true,
		},
		{name: "full DN", username: "alice", adminGroup: adminDN, groups: []string{usersDN, adminDN}, wantAdmin: true},
		{
			name: "case insensitive DN", username: "alice", adminGroup: "OU=DEVBOX-ADMINS,OU=GROUPS,DC=GLAUTH,DC=COM",
			groups: []string{usersDN, adminDN}, wantAdmin: true,
		},
		{
			name: "trimmed bare group", username: "alice", adminGroup: " DEVBOX-ADMINS ",
			groups: []string{usersDN, " " + adminDN + " "}, wantAdmin: true,
		},
		{name: "disabled", username: "alice", groups: []string{usersDN, adminDN}},
		{name: "wrong group", username: "alice", adminGroup: "other-admins", groups: []string{usersDN, adminDN}},
		{
			name: "account name grants no role", username: "administrator",
			adminGroup: "devbox-admins", groups: []string{usersDN},
		},
		{
			name: "nested group is not direct membership", username: "alice", adminGroup: "devbox-admins",
			groups: []string{usersDN, "cn=operators,ou=devbox-admins,dc=glauth,dc=com"},
		},
		{
			name: "administrator still needs access group", username: "alice", adminGroup: "devbox-admins",
			groups: []string{adminDN}, denied: true,
		},
		{name: "missing memberOf", username: "alice", adminGroup: "devbox-admins", denied: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			entry := ldap.NewEntry("cn="+tc.username+",dc=glauth,dc=com", map[string][]string{
				"mail": {tc.username + "@example.com"}, "MEMBEROF": tc.groups,
			})
			identity, err := ldapIdentity(entry, LDAPConfig{
				UsernameAttribute: "mail", UserDomain: "example.com", RequiredGroup: usersDN,
				AdminGroup: tc.adminGroup,
			})
			if tc.denied {
				if !errors.Is(err, ErrAuthentication) || identity != (Identity{}) {
					t.Fatalf("unauthorized identity accepted: %#v, %v", identity, err)
				}

				return
			}
			if err != nil || identity.Username != tc.username || identity.IsAdmin != tc.wantAdmin {
				t.Fatalf("identity = %#v, %v; want %q, admin %v", identity, err, tc.username, tc.wantAdmin)
			}
		})
	}
}

func TestLDAPAdministratorRejectsAmbiguousIdentity(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		entry *ldap.Entry
	}{
		{name: "missing entry"},
		{
			name: "multiple canonical names",
			entry: ldap.NewEntry("cn=alice,dc=example", map[string][]string{
				"uid": {"alice", "administrator"}, "memberOf": {"cn=admins,dc=example"},
			}),
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			identity, err := ldapIdentity(tc.entry, LDAPConfig{UsernameAttribute: "uid", AdminGroup: "admins"})
			if err == nil || identity != (Identity{}) {
				t.Fatalf("invalid directory identity accepted: %#v, %v", identity, err)
			}
		})
	}
}

type roleTestAuthenticator struct {
	identity Identity
	err      error
}

func (a roleTestAuthenticator) Authenticate(context.Context, string, string) (string, error) {
	return "", ErrAuthentication
}

func (a roleTestAuthenticator) AuthenticateIdentity(context.Context, string, string) (Identity, error) {
	return a.identity, a.err
}

func TestAuthenticatedIdentityFailsClosed(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		auth Authenticator
		want Identity
	}{
		{
			name: "legacy authenticator never grants admin",
			auth: testAuthenticator(func(context.Context, string, string) (string, error) { return "administrator", nil }),
			want: Identity{Username: "administrator"},
		},
		{
			name: "verified role",
			auth: roleTestAuthenticator{identity: Identity{Username: "alice", IsAdmin: true}},
			want: Identity{Username: "alice", IsAdmin: true},
		},
		{
			name: "failed authentication returns no role",
			auth: roleTestAuthenticator{identity: Identity{Username: "alice", IsAdmin: true}, err: ErrAuthentication},
		},
		{
			name: "invalid owner cannot carry role",
			auth: roleTestAuthenticator{identity: Identity{Username: "../alice", IsAdmin: true}},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			identity, err := authenticateIdentity(t.Context(), tc.auth, "irrelevant", "test-password")
			if identity != tc.want || (err != nil) != (tc.want.Username == "") {
				t.Fatalf("identity = %#v, %v; want %#v", identity, err, tc.want)
			}
		})
	}
}
