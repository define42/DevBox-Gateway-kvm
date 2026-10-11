// Package cloudinit creates and validates bounded NoCloud seed disks.
// Guest account, network, and instance identity policy belongs to the caller.
package cloudinit

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/goccy/go-yaml"
	"github.com/kdomanski/iso9660"
)

const (
	// MaxSeedDataSize bounds the combined size of the three seed documents.
	MaxSeedDataSize = 8 << 20
	// MaxMetaDataSize bounds the YAML metadata parsed during validation.
	MaxMetaDataSize = 64 << 10
	// MaxISOSize permits filesystem overhead in addition to the seed documents.
	MaxISOSize = 16 << 20
)

var (
	errSeedSize      = errors.New("cloud-init seed documents exceed the 8 MiB size limit")
	errMetaData      = errors.New("cloud-init meta-data must be one YAML mapping with a nonempty string instance-id")
	errEmptyUserData = errors.New("cloud-init user-data must not be empty")
	errMetaDataSize  = errors.New("cloud-init meta-data exceeds the 64 KiB size limit")
)

// Seed contains the caller's original NoCloud documents. UserData may be cloud
// config, a script, or another format understood by the guest's cloud-init.
// MetaData must include a stable instance-id chosen by the caller. NetworkConfig
// is optional. The documents are written unchanged; this package adds no users,
// passwords, network settings, or commands.
//
// The combined documents must fit within MaxSeedDataSize. MetaData additionally
// has the smaller MaxMetaDataSize limit because it must be parsed on attachment.
// Callers must not modify the byte slices while CreateSeedISO is running.
type Seed struct {
	UserData      []byte
	MetaData      []byte
	NetworkConfig []byte
}

// CreateSeedISO returns a cidata-labelled ISO containing the seed's documents.
// No VM, external command, or privileged filesystem mount is needed.
func CreateSeedISO(seed Seed) ([]byte, error) {
	if err := validateSeed(seed); err != nil {
		return nil, err
	}
	writer, err := iso9660.NewWriter()
	if err != nil {
		return nil, fmt.Errorf("create cloud-init ISO writer: %w", err)
	}
	defer func() { _ = writer.Cleanup() }()

	for _, document := range []struct {
		name string
		data []byte
	}{
		{name: "user-data", data: seed.UserData},
		{name: "meta-data", data: seed.MetaData},
		{name: "network-config", data: seed.NetworkConfig},
	} {
		if len(document.data) == 0 {
			continue
		}
		if err := writer.AddFile(bytes.NewReader(document.data), document.name); err != nil {
			return nil, fmt.Errorf("add cloud-init %s: %w", document.name, err)
		}
	}

	var image bytes.Buffer
	if err := writer.WriteTo(&image, "cidata"); err != nil {
		return nil, fmt.Errorf("write cloud-init ISO: %w", err)
	}

	return image.Bytes(), nil
}

func validateSeed(seed Seed) error {
	remaining := MaxSeedDataSize
	for _, document := range [][]byte{seed.UserData, seed.MetaData, seed.NetworkConfig} {
		if len(document) > remaining {
			return errSeedSize
		}
		remaining -= len(document)
	}
	if len(bytes.TrimSpace(seed.UserData)) == 0 {
		return errEmptyUserData
	}
	if len(seed.MetaData) > MaxMetaDataSize {
		return errMetaDataSize
	}

	return validateMetaData(seed.MetaData)
}

func validateMetaData(data []byte) error {
	var metadata map[string]any
	decoder := yaml.NewDecoder(bytes.NewReader(data))
	if err := decoder.Decode(&metadata); err != nil {
		// YAML errors can include the source and credentials. Deliberately
		// return only the document's validation requirement.
		return errMetaData
	}
	instanceID, ok := metadata["instance-id"].(string)
	if !ok || strings.TrimSpace(instanceID) == "" {
		return errMetaData
	}
	var extra any
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		return errMetaData
	}

	return nil
}
