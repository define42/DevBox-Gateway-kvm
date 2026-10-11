package gateway

import (
	"context"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"net/url"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/go-ldap/ldap/v3"
	"github.com/tredoe/osutil/user/crypt/sha512_crypt"
	"golang.org/x/crypto/bcrypt"
)

// LDAPConfig configures direct user binding followed by a unique directory search.
// UserFilter contains one %s placeholder for the escaped bind identifier.
// ldap:// uses StartTLS; plaintext is restricted to explicit loopback testing.
type LDAPConfig struct {
	URL, BaseDN, UserDomain, UserFilter, UsernameAttribute, RequiredGroup, CAFile string
	AllowInsecure                                                                 bool
	Timeout                                                                       time.Duration
}

type ldapAuthenticator struct {
	config  LDAPConfig
	address string
	tls     *tls.Config
	ldaps   bool
}

// NewLDAPAuthenticator validates LDAP configuration without contacting the server.
func NewLDAPAuthenticator(c LDAPConfig) (Authenticator, error) {
	u, err := url.Parse(c.URL)
	if err != nil ||
		(u.Scheme != "ldap" &&
			u.Scheme != "ldaps") ||
		u.Hostname() == "" ||
		u.User != nil ||
		u.RawQuery != "" ||
		u.Fragment != "" ||
		(u.Path != "" &&
			u.Path != "/") ||
		c.BaseDN == "" {
		return nil, fmt.Errorf("%w: LDAP needs an ldap:// or ldaps:// URL and base DN", ErrInvalid)
	}
	if c.AllowInsecure {
		ip, err := netip.ParseAddr(u.Hostname())
		if err != nil || !ip.IsLoopback() || u.Scheme != "ldap" {
			return nil, fmt.Errorf("%w: insecure LDAP is restricted to a loopback IP", ErrInvalid)
		}
	}
	if strings.ContainsAny(c.UserDomain, "@/\\ \t\r\n") {
		return nil, fmt.Errorf("%w: invalid LDAP user domain", ErrInvalid)
	}
	if c.UsernameAttribute == "" {
		c.UsernameAttribute = "uid"
	}
	if !regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9-]*$`).MatchString(c.UsernameAttribute) {
		return nil, fmt.Errorf("%w: invalid LDAP username attribute", ErrInvalid)
	}
	if c.UserFilter == "" {
		c.UserFilter = "(uid=%s)"
		if c.UserDomain != "" {
			c.UserFilter = "(mail=%s)"
		}
	}
	if strings.Count(c.UserFilter, "%s") != 1 {
		return nil, fmt.Errorf("%w: LDAP user filter needs one %%s placeholder", ErrInvalid)
	}
	if _, err := ldap.CompileFilter(strings.Replace(c.UserFilter, "%s", "probe", 1)); err != nil {
		return nil, fmt.Errorf("%w: invalid LDAP user filter", ErrInvalid)
	}
	if c.Timeout == 0 {
		c.Timeout = 10 * time.Second
	}
	if c.Timeout < time.Second || c.Timeout > time.Minute {
		return nil, fmt.Errorf("%w: LDAP timeout must be between one second and one minute", ErrInvalid)
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12, ServerName: u.Hostname()}
	if c.CAFile != "" {
		pem, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("read LDAP CA: %w", err)
		}
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(pem) {
			return nil, fmt.Errorf("%w: LDAP CA file has no certificates", ErrInvalid)
		}
		tlsConfig.RootCAs = roots
	}
	port := u.Port()
	if port == "" {
		port = "389"
		if u.Scheme == "ldaps" {
			port = "636"
		}
	}

	return &ldapAuthenticator{
		config: c,
		address: net.JoinHostPort(
			u.Hostname(),
			port,
		),
		tls:   tlsConfig,
		ldaps: u.Scheme == "ldaps",
	}, nil
}

func (a *ldapAuthenticator) Authenticate(ctx context.Context, username, password string) (string, error) {
	if !validOwner(strings.ToLower(username)) || password == "" || len(password) > 1024 {
		return "", ErrAuthentication
	}
	ctx, cancel := context.WithTimeout(ctx, a.config.Timeout)
	defer cancel()
	var dialer net.Dialer
	raw, err := dialer.DialContext(ctx, "tcp", a.address)
	if err != nil {
		return "", ErrAuthentication
	}
	defer raw.Close()
	stop := context.AfterFunc(ctx, func() { _ = raw.Close() })
	defer stop()
	if deadline, ok := ctx.Deadline(); ok {
		_ = raw.SetDeadline(deadline)
	}
	transport := raw
	if a.ldaps {
		secure := tls.Client(raw, a.tls)
		if err := secure.HandshakeContext(ctx); err != nil {
			return "", ErrAuthentication
		}
		transport = secure
	}
	conn := ldap.NewConn(transport, a.ldaps)
	conn.Start()
	defer func() {
		_ = raw.Close()
		_ = conn.Close()
	}()
	conn.SetTimeout(a.config.Timeout)
	if !a.ldaps && !a.config.AllowInsecure {
		if err := conn.StartTLS(a.tls); err != nil {
			return "", ErrAuthentication
		}
	}
	identifier := username
	if a.config.UserDomain != "" {
		identifier += "@" + a.config.UserDomain
	}
	if err := conn.Bind(identifier, password); err != nil {
		return "", ErrAuthentication
	}
	filter := strings.Replace(a.config.UserFilter, "%s", ldap.EscapeFilter(identifier), 1)
	result, err := conn.Search(
		ldap.NewSearchRequest(
			a.config.BaseDN,
			ldap.ScopeWholeSubtree,
			ldap.NeverDerefAliases,
			2,
			int(
				a.config.Timeout.Seconds(),
			),
			false,
			filter,
			[]string{
				a.config.UsernameAttribute,
				"memberOf",
			},
			nil,
		),
	)
	if err != nil || len(result.Entries) != 1 || ctx.Err() != nil {
		return "", ErrAuthentication
	}
	entry := result.Entries[0]
	if a.config.RequiredGroup != "" {
		matched := false
		for _, group := range entry.GetAttributeValues("memberOf") {
			if strings.EqualFold(group, a.config.RequiredGroup) {
				matched = true
			}
		}
		if !matched {
			return "", ErrAuthentication
		}
	}

	return canonicalLDAPUsername(entry, a.config)
}

// canonicalLDAPUsername derives ownership solely from a unique directory value.
// Attribute and domain names are case-insensitive, but account names are not:
// folding a directory name could merge distinct owners. Unsupported Linux
// account names are rejected instead of silently rewritten.
func canonicalLDAPUsername(entry *ldap.Entry, config LDAPConfig) (string, error) {
	var values []string
	if entry != nil {
		for _, attribute := range entry.Attributes {
			if strings.EqualFold(attribute.Name, config.UsernameAttribute) {
				values = append(values, attribute.Values...)
			}
		}
	}
	if len(values) != 1 || values[0] == "" {
		return "", ErrAuthentication
	}
	name := values[0]
	if name != strings.TrimSpace(name) {
		return "", ErrAuthentication
	}
	if local, domain, qualified := strings.Cut(name, "@"); qualified {
		if domain == "" || !strings.EqualFold(domain, config.UserDomain) {
			return "", ErrAuthentication
		}
		name = local
	}
	if !validOwner(name) {
		return "", fmt.Errorf("%w: directory username must be a supported Linux account name", ErrInvalid)
	}

	return name, nil
}

type staticAuthenticator struct {
	hashes map[string][]byte
	dummy  []byte
}

// NewStaticAuthenticator loads a mode-0600 JSON map of usernames to bcrypt hashes.
// It is intended for explicitly configured local development, never an auth fallback.
func NewStaticAuthenticator(path string) (Authenticator, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 || info.Size() > 1<<20 {
		return nil, fmt.Errorf("%w: password file must be a private regular file (mode 0600)", ErrInvalid)
	}
	var entries map[string]string
	decoder := json.NewDecoder(io.LimitReader(f, (1<<20)+1))
	if err := decoder.Decode(&entries); err != nil {
		return nil, fmt.Errorf("%w: invalid password file", ErrInvalid)
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) || len(entries) == 0 || len(entries) > 1000 {
		return nil, fmt.Errorf("%w: invalid password file", ErrInvalid)
	}
	a := &staticAuthenticator{hashes: make(map[string][]byte, len(entries))}
	for user, hash := range entries {
		cost, err := bcrypt.Cost([]byte(hash))
		if !validOwner(user) || err != nil || cost < bcrypt.DefaultCost || cost > 16 {
			return nil, fmt.Errorf(
				"%w: password file needs valid usernames and bcrypt hashes (cost 10..16)",
				ErrInvalid,
			)
		}
		a.hashes[user] = []byte(hash)
		if a.dummy == nil {
			a.dummy = []byte(hash)
		}
	}

	return a, nil
}

func (a *staticAuthenticator) Authenticate(ctx context.Context, username, password string) (string, error) {
	if ctx.Err() != nil || password == "" || len(password) > 72 {
		return "", ErrAuthentication
	}
	hash, ok := a.hashes[username]
	if !ok {
		hash = a.dummy
	}
	err := bcrypt.CompareHashAndPassword(hash, []byte(password))
	if !ok || err != nil || ctx.Err() != nil {
		return "", ErrAuthentication
	}

	return username, nil
}

func validOwner(s string) bool {
	if len(s) == 0 || len(s) > 32 || (s[0] != '_' && (s[0] < 'a' || s[0] > 'z')) {
		return false
	}
	for _, c := range s {
		if c != '_' && c != '.' && c != '-' && (c < 'a' || c > 'z') && (c < '0' || c > '9') {
			return false
		}
	}

	return true
}

// GuestPasswordHash derives a salted SHA-512 crypt hash for a cloud-init guest account.
// The plaintext is never stored by the gateway.
func GuestPasswordHash(password string) (string, error) {
	if password == "" || len(password) > 1024 {
		return "", fmt.Errorf("%w: guest password must contain 1–1024 bytes", ErrInvalid)
	}
	var salt [8]byte
	if _, err := rand.Read(salt[:]); err != nil {
		return "", err
	}

	return sha512_crypt.New().Generate([]byte(password), []byte("$6$rounds=100000$"+hex.EncodeToString(salt[:])+"$"))
}
