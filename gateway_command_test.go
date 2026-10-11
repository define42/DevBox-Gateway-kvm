package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/define42/gokvm/internal/cloudinit"
	"github.com/define42/gokvm/internal/gateway"
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

func TestGatewayCommandRejectsUnsafeConfiguration(t *testing.T) {
	t.Parallel()
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
	if err := serveIdentity(dir, &config); err != nil {
		t.Fatal(err)
	}
	before, err := os.ReadFile(config.KeyFile)
	if err != nil {
		t.Fatal(err)
	}
	config.CertFile, config.KeyFile = "", ""
	if err := serveIdentity(dir, &config); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(config.KeyFile)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("TLS identity changed")
	}
	remote := gateway.Config{ListenAddr: "0.0.0.0:8443", PublicAddress: "gateway.example.com:8443"}
	if err := serveIdentity(dir, &remote); !errors.Is(err, errCommandUsage) {
		t.Fatalf("remote self-signed default: %v", err)
	}
}
