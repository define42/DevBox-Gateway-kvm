package main

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/iotest"

	"github.com/define42/DevBox-Gateway-kvm/internal/cloudinit"
	"github.com/define42/DevBox-Gateway-kvm/internal/gateway"
)

func TestSeedRejectsInvalidOptionsWithoutPublishing(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name, parseError string
		args             []string
	}{
		{name: "missing output", args: []string{"-user", "developer"}},
		{name: "unknown flag", args: []string{"-unknown"}, parseError: "flag provided but not defined: -unknown"},
		{name: "positional argument", args: []string{"unexpected"}},
		{name: "missing documents", args: []string{"-out", "OUTPUT"}},
		{name: "mixed account and documents", args: []string{"-out", "OUTPUT", "-user", "developer", "-user-data", "user"}},
		{
			name: "mixed password sources",
			args: []string{"-out", "OUTPUT", "-user", "developer", "-password-stdin", "-password-hash-file", "hash"},
		},
		{
			name: "credentials from hash",
			args: []string{
				"-out", "OUTPUT", "-user", "developer", "-password-hash-file", "hash", "-credentials", "credentials",
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			args := append([]string(nil), test.args...)
			for i, arg := range args {
				if arg == "OUTPUT" {
					args[i] = filepath.Join(dir, "seed.iso")
				}
			}
			err := runSeed(args, strings.NewReader(""), io.Discard)
			if test.parseError != "" {
				if err == nil || !strings.Contains(err.Error(), test.parseError) {
					t.Fatalf("invalid flag: got %v, want %q", err, test.parseError)
				}
			} else if !errors.Is(err, errCommandUsage) {
				t.Fatalf("invalid options: got %v, want command usage error", err)
			}
			entries, err := os.ReadDir(dir)
			if err != nil || len(entries) != 0 {
				t.Fatalf("invalid command published files: %v, %v", entries, err)
			}
		})
	}
}

func TestAuthUserRejectsInvalidInput(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name string
		args []string
	}{
		{name: "missing output"},
		{name: "unknown flag", args: []string{"-unknown"}},
		{name: "invalid password", args: []string{"-password-stdin"}},
		{name: "invalid username", args: []string{"-user", "../invalid"}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "auth.json")
			args := append([]string(nil), test.args...)
			if test.name != "missing output" {
				args = append(args, "-out", path)
			}
			if err := runAuthUser(args, strings.NewReader("short"), io.Discard); err == nil {
				t.Fatal("invalid account input accepted")
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid command published authentication file: %v", err)
			}
		})
	}
}

func TestReadCommandFileLimits(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "input")
	if err := os.WriteFile(path, []byte("1234"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, path string
		limit      int64
		want       error
	}{
		{name: "exact limit", path: path, limit: 4},
		{name: "too large", path: path, limit: 3, want: errCommandUsage},
		{name: "directory", path: dir, limit: 1 << 20, want: errCommandUsage},
		{name: "missing", path: filepath.Join(dir, "missing"), limit: 4, want: os.ErrNotExist},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			data, err := readCommandFile(test.path, test.limit)
			if !errors.Is(err, test.want) {
				t.Fatalf("read: got %v, want %v", err, test.want)
			}
			if test.want == nil && string(data) != "1234" {
				t.Fatalf("file data changed: %q", data)
			}
		})
	}
}

func TestReadRawSeedDocuments(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	paths := []string{filepath.Join(dir, "user"), filepath.Join(dir, "meta"), filepath.Join(dir, "network")}
	documents := []string{"#cloud-config\nusers: []\n", "instance-id: raw-test\n", "version: 2\n"}
	for i, path := range paths {
		if err := os.WriteFile(path, []byte(documents[i]), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	seed, err := readRawSeed(paths[0], paths[1], paths[2])
	if err != nil {
		t.Fatal(err)
	}
	if string(seed.UserData) != documents[0] || string(seed.MetaData) != documents[1] ||
		string(seed.NetworkConfig) != documents[2] {
		t.Fatalf("raw documents changed: %+v", seed)
	}
	for i, name := range []string{"missing user", "missing metadata", "missing network"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			missing := append([]string(nil), paths...)
			missing[i] = filepath.Join(dir, "missing")
			if _, err := readRawSeed(missing[0], missing[1], missing[2]); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing document: %v", err)
			}
		})
	}
}

func TestSeedFromPasswordHash(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	hash, err := gateway.GuestPasswordHash("test-desktop-password")
	if err != nil {
		t.Fatal(err)
	}
	hashPath := filepath.Join(dir, "hash")
	if err := os.WriteFile(hashPath, []byte("  "+hash+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "seed.iso")
	args := []string{"-out", path, "-user", "developer", "-instance-id", "stable-test", "-password-hash-file", hashPath}
	var output bytes.Buffer
	if err := runSeed(args, iotest.ErrReader(io.ErrUnexpectedEOF), &output); err != nil {
		t.Fatal(err)
	}
	if err := cloudinit.ValidateISO(path); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path + ".credentials.json"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("hash mode should not publish plaintext credentials: %v", err)
	}
	if strings.Contains(output.String(), hash) || !strings.Contains(output.String(), path) {
		t.Fatalf("unexpected command output: %q", output.String())
	}
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := runSeed(args, strings.NewReader(""), io.Discard); !errors.Is(err, os.ErrExist) {
		t.Fatalf("overwrite existing seed: %v", err)
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(before, after) {
		t.Fatal("existing seed changed")
	}
	account := seedAccount{username: "developer", hostname: "desktop", instance: "stable-test", keyboard: "us"}
	_, _, err = account.generate(filepath.Join(dir, "missing"), false, strings.NewReader(""))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing hash file: %v", err)
	}
	_, _, err = account.generate("", true, iotest.ErrReader(io.ErrUnexpectedEOF))
	if !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("password read failure: %v", err)
	}
}

func TestCommandPasswordLineEndings(t *testing.T) {
	t.Parallel()
	for name, suffix := range map[string]string{"bare": "", "LF": "\n", "CRLF": "\r\n"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			want := strings.Repeat("p", 72)
			got, err := commandPassword(true, strings.NewReader(want+suffix))
			if err != nil || got != want {
				t.Fatalf("password: got %q, %v; want 72 bytes unchanged", got, err)
			}
		})
	}
}
