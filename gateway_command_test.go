package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/cert"
	"github.com/define42/DevBox-Gateway-kvm/internal/cloudinit"
	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
)

func TestAuthUserPrivateAndUsable(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	auth := filepath.Join(dir, "auth.json")
	var output bytes.Buffer
	if err := runAuthUser([]string{"-user", "developer", "-out", auth}, strings.NewReader(""), &output); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(auth + ".credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	var credentials localCredential
	if err := json.Unmarshal(data, &credentials); err != nil {
		t.Fatal(err)
	}
	authenticator, err := gateway.NewStaticAuthenticator(auth)
	if err != nil {
		t.Fatal(err)
	}
	owner, err := authenticator.Authenticate(t.Context(), credentials.Username, credentials.Password)
	if err != nil || owner != "developer" {
		t.Fatalf("authenticate: owner %q, error %v", owner, err)
	}
	if strings.Contains(output.String(), credentials.Password) {
		t.Fatal("password printed in command output")
	}
	for _, path := range []string{auth, auth + ".credentials.json"} {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Fatalf("%s permissions %v", path, info.Mode())
		}
	}
	before := append([]byte(nil), data...)
	err = runAuthUser([]string{"-user", "developer", "-out", auth}, strings.NewReader(""), io.Discard)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite: %v", err)
	}
	after, err := os.ReadFile(auth + ".credentials.json")
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing credentials changed")
	}
}

func TestSeedRawAndGenerated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	for name, data := range map[string]string{"user": "#cloud-config\nusers: []\n", "meta": "instance-id: stable-test\n"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	raw := filepath.Join(dir, "raw.iso")
	rawArgs := []string{"-out", raw, "-user-data", filepath.Join(dir, "user"), "-meta-data", filepath.Join(dir, "meta")}
	if err := runSeed(rawArgs, strings.NewReader(""), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cloudinit.ValidateISO(raw); err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(dir, "desktop.iso")
	generatedArgs := []string{"-out", generated, "-user", "rocky", "-password-stdin"}
	if err := runSeed(generatedArgs, strings.NewReader("test-desktop-password\n"), io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := cloudinit.ValidateISO(generated); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(generated + ".credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	var credential localCredential
	if err := json.Unmarshal(data, &credential); err != nil {
		t.Fatal(err)
	}
	if credential.Username != "rocky" || credential.Password != "test-desktop-password" {
		t.Fatal("incorrect generated credentials")
	}
}

func TestPublishFailurePreservesExistingFiles(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	existing := filepath.Join(dir, "exists")
	if err := os.WriteFile(existing, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	credentials := filepath.Join(dir, "credentials")
	credential := localCredential{Username: "test", Password: "secret"}
	err := publishWithCredentials(existing, []byte("replace"), credentials, credential, io.Discard)
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("got %v", err)
	}
	if _, err := os.Stat(credentials); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("credentials left after failure: %v", err)
	}
	data, err := os.ReadFile(existing)
	if err != nil || string(data) != "keep" {
		t.Fatal("existing file changed")
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestGatewayCommandRejectsUnsafeConfiguration(t *testing.T) {
	clearServeEnvironment(t)
	for _, args := range [][]string{
		{},
		{"-auth-file", "missing", "-ldap-url", "ldaps://example.com"},
		{"-auth-file", "missing", "-listen", "0.0.0.0:8443"},
		{"unexpected"},
	} {
		if err := runServe(context.Background(), args, io.Discard); !errors.Is(err, errCommandUsage) {
			t.Fatalf("%v: %v", args, err)
		}
	}
	for _, password := range []string{"short\n", strings.Repeat("a", 73), "test-password\nnext"} {
		if _, err := commandPassword(true, strings.NewReader(password)); !errors.Is(err, errCommandUsage) {
			t.Fatalf("invalid password accepted: %v", err)
		}
	}
}

func TestLocalTLSIdentityPersists(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := gateway.Config{ListenAddr: "127.0.0.1:8443", PublicAddress: "localhost:8443"}
	if err := serveIdentity(dir, &config, cert.Options{ACMECA: "production"}); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(filepath.Join(dir, "gateway.key"))
	if err != nil {
		t.Fatal(err)
	}
	config.TLSManager.Close()
	if err := serveIdentity(dir, &config, cert.Options{ACMECA: "production"}); err != nil {
		t.Fatal(err)
	}
	defer config.TLSManager.Close()
	after, err := os.ReadFile(filepath.Join(dir, "gateway.key"))
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("TLS identity changed")
	}
}

func TestRemoteTLSIdentityGenerated(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	if err := os.Chmod(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	config := gateway.Config{ListenAddr: "0.0.0.0:8443", PublicAddress: "gateway.example.com:8443"}
	if err := serveIdentity(dir, &config, cert.Options{ACMECA: "production"}); err != nil {
		t.Fatalf("generate remote self-signed identity: %v", err)
	}
	defer config.TLSManager.Close()
	if config.TLSManager == nil {
		t.Fatal("no TLS manager configured")
	}
}

func TestGatewayTLSModesRejectConflicts(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, certificate, key string
		acme                   bool
	}{
		{name: "missing key", certificate: "server.crt"},
		{name: "missing certificate", key: "server.key"},
		{name: "static and ACME", certificate: "server.crt", key: "server.key", acme: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			config := gateway.Config{PublicAddress: "gateway.example.com:443", CertFile: test.certificate, KeyFile: test.key}
			if err := serveIdentity(t.TempDir(), &config, cert.Options{ACME: test.acme}); !errors.Is(err, errCommandUsage) {
				t.Fatalf("invalid TLS mode accepted: %v", err)
			}
		})
	}
}

//nolint:paralleltest // clearServeEnvironment changes the process environment.
func TestServeHelpDoesNotRequireHostSupport(t *testing.T) {
	clearServeEnvironment(t)
	var output bytes.Buffer
	check := func(string) error {
		t.Fatal("help must not probe host requirements")

		return errHostRequirements
	}
	err := runServeWithHostCheck(t.Context(), []string{"-help"}, &output, check)
	if !errors.Is(err, flag.ErrHelp) || !strings.Contains(output.String(), "Usage: devbox-kvm serve") {
		t.Fatalf("help failed: %v, output %q", err, output.String())
	}
}
