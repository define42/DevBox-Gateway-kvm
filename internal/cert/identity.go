package cert

import (
	"bytes"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log"
	"math/big"
	"net"
	"os"
	"path/filepath"
	"syscall"
	"time"

	"golang.org/x/sys/unix"
)

func privateOwned(info fs.FileInfo) bool {
	stat, ok := info.Sys().(*syscall.Stat_t)

	return ok && stat.Uid == uint32(os.Geteuid()) && info.Mode().Perm()&0o077 == 0
}

func privateDirectory(path string) error {
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create TLS state directory: %w", err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		return err
	}
	if !info.IsDir() || !privateOwned(info) {
		return fmt.Errorf(
			"%w: TLS state directory must be private (0700) and owned by the current user: %s",
			ErrInvalid,
			path,
		)
	}

	return nil
}

func checkStorage(path string) error {
	return filepath.WalkDir(path, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if (!info.IsDir() && !info.Mode().IsRegular()) || !privateOwned(info) {
			return fmt.Errorf(
				"%w: ACME storage must contain only private directories and regular files owned by the current user: %s",
				ErrInvalid,
				path,
			)
		}

		return nil
	})
}

func readIdentityFile(path string, key bool) ([]byte, error) {
	// Operator-selected paths are read without following a final symlink or blocking on special files.
	file, err := os.OpenFile(path, os.O_RDONLY|unix.O_NOFOLLOW|unix.O_NONBLOCK, 0) //nolint:gosec
	if err != nil {
		return nil, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > 1<<20 || (key && !privateOwned(info)) {
		return nil, fmt.Errorf(
			"%w: TLS files must be regular; private keys must be private (0600) and owned by the current user: %s",
			ErrInvalid,
			path,
		)
	}

	return io.ReadAll(io.LimitReader(file, 1<<20))
}

func loadPair(certFile, keyFile string) (tls.Certificate, error) {
	certificate, err := readIdentityFile(certFile, false)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read TLS certificate: %w", err)
	}
	key, err := readIdentityFile(keyFile, true)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("read TLS private key: %w", err)
	}
	pair, err := tls.X509KeyPair(certificate, key)
	if err != nil {
		return tls.Certificate{}, fmt.Errorf("load matching TLS certificate and key: %w", err)
	}
	if pair.Leaf == nil {
		pair.Leaf, err = x509.ParseCertificate(pair.Certificate[0])
		if err != nil {
			return tls.Certificate{}, fmt.Errorf("parse TLS certificate: %w", err)
		}
	}

	return pair, nil
}

func loadIdentity(certFile, keyFile, host string) (tls.Certificate, error) {
	pair, err := loadPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	if err := pair.Leaf.VerifyHostname(host); err != nil {
		return tls.Certificate{}, fmt.Errorf(
			"TLS certificate does not cover %s; supply a matching identity: %w",
			host,
			err,
		)
	}
	now := time.Now()
	if now.Before(pair.Leaf.NotBefore) || !now.Before(pair.Leaf.NotAfter) {
		return tls.Certificate{}, fmt.Errorf(
			"%w: TLS certificate is expired or not yet valid; supply a valid identity",
			ErrInvalid,
		)
	}
	if len(pair.Leaf.ExtKeyUsage) > 0 {
		allowed := false
		for _, usage := range pair.Leaf.ExtKeyUsage {
			if usage == x509.ExtKeyUsageServerAuth || usage == x509.ExtKeyUsageAny {
				allowed = true
			}
		}
		if !allowed {
			return tls.Certificate{}, fmt.Errorf(
				"%w: TLS certificate does not permit server authentication",
				ErrInvalid,
			)
		}
	}

	return pair, nil
}

func persistentIdentity(dir, host string) (tls.Certificate, error) {
	certFile, keyFile := filepath.Join(dir, "gateway.crt"), filepath.Join(dir, "gateway.key")
	_, certErr := os.Lstat(certFile)
	_, keyErr := os.Lstat(keyFile)
	if errors.Is(certErr, fs.ErrNotExist) && errors.Is(keyErr, fs.ErrNotExist) {
		certificate, key, err := newIdentity(host)
		if err != nil {
			return tls.Certificate{}, err
		}
		if err := writeNewIdentityFile(keyFile, key); err != nil {
			return tls.Certificate{}, err
		}
		if err := writeNewIdentityFile(certFile, certificate); err != nil {
			return tls.Certificate{}, err
		}
	} else if certErr != nil || keyErr != nil {
		return tls.Certificate{}, fmt.Errorf(
			"incomplete TLS identity; restore gateway.crt and gateway.key or move both aside: %w",
			errors.Join(certErr, keyErr),
		)
	}
	pair, err := loadPair(certFile, keyFile)
	if err != nil {
		return tls.Certificate{}, err
	}
	if pair.Leaf.VerifyHostname(host) != nil || !time.Now().Before(pair.Leaf.NotAfter) {
		leaf := pair.Leaf
		if !bytes.Equal(leaf.RawIssuer, leaf.RawSubject) ||
			leaf.CheckSignature(leaf.SignatureAlgorithm, leaf.RawTBSCertificate, leaf.Signature) != nil {
			return tls.Certificate{}, fmt.Errorf(
				"%w: stored TLS identity is not self-signed; replace it with a valid certificate for %s",
				ErrInvalid,
				host,
			)
		}
		key, ok := pair.PrivateKey.(crypto.Signer)
		if !ok {
			return tls.Certificate{}, fmt.Errorf("%w: stored TLS private key cannot sign certificates", ErrInvalid)
		}
		certificate, err := certificateForKey(host, key)
		if err != nil {
			return tls.Certificate{}, err
		}
		if err := replaceCertificate(certFile, certificate); err != nil {
			return tls.Certificate{}, err
		}
		log.Printf("renewed generated TLS certificate for %s, preserving its private key", host)
	}

	return loadIdentity(certFile, keyFile, host)
}

func replaceCertificate(path string, contents []byte) error {
	file, err := os.CreateTemp(filepath.Dir(path), ".gateway-cert-*")
	if err != nil {
		return err
	}
	defer os.Remove(file.Name())
	_, writeErr := file.Write(contents)
	if err := errors.Join(writeErr, file.Sync(), file.Close()); err != nil {
		return err
	}

	return os.Rename(file.Name(), path)
}

func writeNewIdentityFile(path string, contents []byte) error {
	//nolint:gosec // New private TLS file in a checked state directory.
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|unix.O_NOFOLLOW, 0o600)
	if err != nil {
		return fmt.Errorf("create TLS identity: %w", err)
	}
	_, writeErr := file.Write(contents)
	syncErr := file.Sync()

	return errors.Join(writeErr, syncErr, file.Close())
}

func newIdentity(host string) ([]byte, []byte, error) {
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, nil, err
	}
	certificate, err := certificateForKey(host, key)
	if err != nil {
		return nil, nil, err
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return nil, nil, err
	}

	return certificate, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), nil
}

func certificateForKey(host string, key crypto.Signer) ([]byte, error) {
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 128))
	if err != nil {
		return nil, err
	}
	identity := &x509.Certificate{
		SerialNumber: serial, Subject: pkix.Name{CommonName: host},
		NotBefore: time.Now().Add(-5 * time.Minute), NotAfter: time.Now().AddDate(1, 0, 0),
		KeyUsage:              x509.KeyUsageDigitalSignature,
		ExtKeyUsage:           []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		BasicConstraintsValid: true,
	}
	if ip := net.ParseIP(host); ip != nil {
		identity.IPAddresses = []net.IP{ip}
	} else {
		identity.DNSNames = []string{host}
	}
	der, err := x509.CreateCertificate(rand.Reader, identity, identity, key.Public(), key)
	if err != nil {
		return nil, err
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), nil
}
