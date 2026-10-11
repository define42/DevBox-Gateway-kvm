package main

import (
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/define42/gokvm/internal/cloudinit"
	"github.com/define42/gokvm/internal/gateway"
	"golang.org/x/crypto/bcrypt"
)

type localCredential struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

func runAuthUser(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("auth-user", flag.ContinueOnError)
	flags.SetOutput(output)
	username := flags.String("user", "developer", "local development account name")
	path := flags.String("out", "", "new private bcrypt JSON authentication file")
	credentials := flags.String("credentials", "", "new private credentials file (default: OUT.credentials.json)")
	stdin := flags.Bool("password-stdin", false, "read password from standard input instead of generating one")
	if err := parseCommand(flags, args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("-out is required: %w", errCommandUsage)
	}
	// DesktopSeed uses the same username validation as the manager and login path.
	password, err := commandPassword(*stdin, input)
	if err != nil {
		return err
	}
	hash, err := gateway.GuestPasswordHash(password)
	if err != nil {
		return err
	}
	if _, err := gateway.DesktopSeed(*username, hash, "desktop", "validation", "us"); err != nil {
		return err
	}
	bcryptHash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return err
	}
	encoded, err := json.MarshalIndent(map[string]string{*username: string(bcryptHash)}, "", "  ")
	if err != nil {
		return err
	}
	if *credentials == "" {
		*credentials = *path + ".credentials.json"
	}
	credential := localCredential{Username: *username, Password: password}

	return publishWithCredentials(*path, encoded, *credentials, credential, output)
}

func runSeed(args []string, input io.Reader, output io.Writer) error {
	flags := flag.NewFlagSet("seed", flag.ContinueOnError)
	flags.SetOutput(output)
	path := flags.String("out", "", "new CIDATA ISO; existing files are never replaced")
	userData := flags.String("user-data", "", "raw user-data file (requires -meta-data)")
	metaData := flags.String("meta-data", "", "raw meta-data file with stable instance-id")
	network := flags.String("network-config", "", "optional raw network-config file")
	username := flags.String("user", "", "generate a desktop account instead of supplying raw documents")
	hostname := flags.String("hostname", "desktop", "guest hostname for generated account seed")
	instance := flags.String("instance-id", "", "stable instance identity (default: newly generated)")
	keyboard := flags.String("keyboard", "us", "guest keyboard layout")
	hashPath := flags.String("password-hash-file", "", "SHA-512 crypt hash file instead of generating a password")
	credentials := flags.String("credentials", "", "private generated credentials file (default: OUT.credentials.json)")
	stdin := flags.Bool("password-stdin", false, "read guest password from standard input")
	if err := parseCommand(flags, args); err != nil {
		return err
	}
	if *path == "" {
		return fmt.Errorf("-out is required: %w", errCommandUsage)
	}
	var seed cloudinit.Seed
	var credential localCredential
	var err error
	accountMode := *username != ""
	if accountMode && (*userData != "" || *metaData != "" || *network != "" || *stdin && *hashPath != "") {
		return fmt.Errorf("account and raw-document options are mutually exclusive: %w", errCommandUsage)
	}
	if !accountMode && (*userData == "" || *metaData == "" || *stdin || *hashPath != "" || *credentials != "") {
		return fmt.Errorf("supply -user or both -user-data and -meta-data: %w", errCommandUsage)
	}
	if *hashPath != "" && *credentials != "" {
		return fmt.Errorf("a hash does not supply plaintext credentials: %w", errCommandUsage)
	}
	if accountMode {
		account := seedAccount{username: *username, hostname: *hostname, instance: *instance, keyboard: *keyboard}
		seed, credential, err = account.generate(*hashPath, *stdin, input)
	} else {
		seed, err = readRawSeed(*userData, *metaData, *network)
	}
	if err != nil {
		return err
	}
	if credential.Username != "" && *credentials == "" {
		*credentials = *path + ".credentials.json"
	}
	image, err := cloudinit.CreateSeedISO(seed)
	if err != nil {
		return err
	}
	if credential.Username != "" {
		return publishWithCredentials(*path, image, *credentials, credential, output)
	}
	if err := createPrivateFile(*path, image); err != nil {
		return err
	}
	_, err = fmt.Fprintf(output, "Created NoCloud seed: %s\n", *path)

	return err
}

func readRawSeed(userPath, metaPath, networkPath string) (cloudinit.Seed, error) {
	var seed cloudinit.Seed
	var err error
	seed.UserData, err = readCommandFile(userPath, 8<<20)
	if err != nil {
		return seed, err
	}
	seed.MetaData, err = readCommandFile(metaPath, 64<<10)
	if err != nil {
		return seed, err
	}
	if networkPath != "" {
		seed.NetworkConfig, err = readCommandFile(networkPath, 8<<20)
	}

	return seed, err
}

type seedAccount struct {
	username, hostname, instance, keyboard string
}

func (a seedAccount) generate(hashPath string, stdin bool, input io.Reader) (cloudinit.Seed, localCredential, error) {
	var credential localCredential
	var hash string
	if hashPath != "" {
		data, err := readCommandFile(hashPath, 1024)
		if err != nil {
			return cloudinit.Seed{}, credential, err
		}
		hash = strings.TrimSpace(string(data))
	} else {
		password, err := commandPassword(stdin, input)
		if err != nil {
			return cloudinit.Seed{}, credential, err
		}
		hash, err = gateway.GuestPasswordHash(password)
		if err != nil {
			return cloudinit.Seed{}, credential, err
		}
		credential = localCredential{Username: a.username, Password: password}
	}
	if a.instance == "" {
		a.instance = "gokvm-" + strings.ToLower(rand.Text())
	}
	seed, err := gateway.DesktopSeed(a.username, hash, a.hostname, a.instance, a.keyboard)

	return seed, credential, err
}

func commandPassword(stdin bool, input io.Reader) (string, error) {
	if !stdin {
		return rand.Text(), nil
	}
	data, err := io.ReadAll(io.LimitReader(input, 74))
	if err != nil {
		return "", err
	}
	password := strings.TrimSuffix(strings.TrimSuffix(string(data), "\n"), "\r")
	if len(password) < 12 || len(password) > 72 || strings.ContainsAny(password, "\x00\r\n") {
		return "", fmt.Errorf("password must contain 12..72 bytes without line breaks: %w", errCommandUsage)
	}

	return password, nil
}

func publishWithCredentials(
	path string, data []byte, credentials string, credential localCredential, output io.Writer,
) error {
	// The explicit credentials output is a private 0600 file, never an HTTP response or log.
	encoded, err := json.MarshalIndent(credential, "", "  ") //nolint:gosec // G117: intentional local credential export.
	if err != nil {
		return err
	}
	if err := createPrivateFile(credentials, encoded); err != nil {
		return err
	}
	if err := createPrivateFile(path, data); err != nil {
		_ = os.Remove(credentials)

		return err
	}
	_, err = fmt.Fprintf(output, "Created %s\nLogin credentials saved to %s (0600).\n", path, credentials)

	return err
}

func readCommandFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > limit {
		return nil, fmt.Errorf("input must be a bounded regular file: %w", errCommandUsage)
	}
	data, err := io.ReadAll(io.LimitReader(file, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("input file too large: %w", errCommandUsage)
	}

	return data, nil
}
