package cloudinit

import (
	"bytes"
	"io"
	"strings"
	"testing"

	"github.com/kdomanski/iso9660"
)

func TestCreateSeedISO(t *testing.T) {
	t.Parallel()

	seed := Seed{
		UserData:      []byte("#!/bin/sh\nprintf '%s\\n' 'literal user-data'\n"),
		MetaData:      []byte("instance-id: desktop-123\nlocal-hostname: desktop\n"),
		NetworkConfig: []byte("version: 2\nethernets:\n  eth0: {dhcp4: true}\n"),
	}
	data, err := CreateSeedISO(seed)
	if err != nil {
		t.Fatal(err)
	}
	image, err := iso9660.OpenImage(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	label, err := image.Label()
	if err != nil || label != "cidata" {
		t.Fatalf("image label = %q, error = %v", label, err)
	}
	root, err := image.RootDir()
	if err != nil {
		t.Fatal(err)
	}
	files, err := root.GetChildren()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string][]byte{
		"user-data": seed.UserData, "meta-data": seed.MetaData, "network-config": seed.NetworkConfig,
	}
	if len(files) != len(want) {
		t.Fatalf("ISO contains %d files, want %d", len(files), len(want))
	}
	for _, file := range files {
		contents, err := io.ReadAll(file.Reader())
		if err != nil {
			t.Fatal(err)
		}
		if expected, ok := want[file.Name()]; !ok || !bytes.Equal(contents, expected) {
			t.Errorf("document %q was renamed, changed, or unexpectedly added", file.Name())
		}
	}
	if err := ValidateISO(writeTestISO(t, data)); err != nil {
		t.Fatalf("generated seed failed validation: %v", err)
	}
}

func TestCreateSeedISORejectsInvalidDocuments(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		seed Seed
	}{
		{name: "missing user-data", seed: Seed{MetaData: []byte("instance-id: vm-1\n")}},
		{name: "blank user-data", seed: Seed{UserData: []byte(" \n\t"), MetaData: []byte("instance-id: vm-1\n")}},
		{name: "missing metadata", seed: Seed{UserData: []byte("#cloud-config\n")}},
		{name: "malformed metadata", seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("instance-id: [")}},
		{name: "missing instance-id", seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("hostname: vm\n")}},
		{name: "blank instance-id", seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("instance-id: '  '\n")}},
		{
			name: "numeric instance-id",
			seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("instance-id: 123\n")},
		},
		{
			name: "boolean instance-id",
			seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("instance-id: true\n")},
		},
		{name: "metadata sequence", seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("- instance-id: vm\n")}},
		{
			name: "duplicate instance-id",
			seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("instance-id: first\ninstance-id: second\n")},
		},
		{
			name: "multiple metadata documents",
			seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte("instance-id: first\n---\ninstance-id: second\n")},
		},
		{
			name: "total size",
			seed: Seed{UserData: bytes.Repeat([]byte("x"), MaxSeedDataSize), MetaData: []byte("instance-id: vm\n")},
		},
		{
			name: "metadata size",
			seed: Seed{UserData: []byte("#cloud-config\n"), MetaData: bytes.Repeat([]byte("x"), MaxMetaDataSize+1)},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if _, err := CreateSeedISO(tc.seed); err == nil {
				t.Fatal("invalid seed was accepted")
			}
		})
	}
}

func TestCreateSeedISOMetadataAndSizeBoundary(t *testing.T) {
	t.Parallel()

	for _, metadata := range []string{
		"instance-id: '123'\n",
		"instance-id: !!str 123\n",
		`{"instance-id":"vm-json"}`,
	} {
		t.Run(metadata, func(t *testing.T) {
			t.Parallel()
			if _, err := CreateSeedISO(Seed{UserData: []byte("#cloud-config\n"), MetaData: []byte(metadata)}); err != nil {
				t.Fatal(err)
			}
		})
	}
	metadata := []byte("instance-id: boundary\n")
	seed := Seed{UserData: bytes.Repeat([]byte("x"), MaxSeedDataSize-len(metadata)), MetaData: metadata}
	data, err := CreateSeedISO(seed)
	if err != nil {
		t.Fatal(err)
	}
	if err := ValidateISO(writeTestISO(t, data)); err != nil {
		t.Fatalf("boundary-sized seed failed validation: %v", err)
	}
}

func TestSeedValidationErrorsDoNotRevealContents(t *testing.T) {
	t.Parallel()

	const secret = "never-include-this-password"
	seed := Seed{
		UserData: []byte("#cloud-config\npassword: " + secret + "\n"),
		MetaData: []byte("instance-id: [" + secret),
	}
	_, err := CreateSeedISO(seed)
	if err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("expected sanitized metadata validation failure, got %v", err)
	}
	data := testISO(t, "cidata", map[string][]byte{"user-data": seed.UserData, "meta-data": seed.MetaData})
	if err := ValidateISO(writeTestISO(t, data)); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("expected sanitized ISO validation failure, got %v", err)
	}
}
