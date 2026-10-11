package vmm

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestUKIBootParams(t *testing.T) {
	t.Parallel()

	const embedded = "rdinit=/init console=tty0 console=ttyS0,115200n8 loglevel=4"
	v := New(Config{UKI: "netdesk.efi", Params: "unused CLI defaults"})
	got := v.bootParams(embedded)
	if !strings.HasSuffix(got, embedded) {
		t.Fatalf("embedded command line not preserved: %q", got)
	}
	for _, want := range []string{"noacpi", "noapic", "pci=realloc=off", "console=ttyS0,115200n8"} {
		if countField(got, want) != 1 {
			t.Errorf("expected one %q in %q", want, got)
		}
	}
	for _, unwanted := range []string{"console=ttyS0", "init=/init", "gokvm.ipv4_addr=192.168.20.1/24"} {
		if hasField(got, unwanted) {
			t.Errorf("unexpected %q in %q", unwanted, got)
		}
	}

	for _, override := range []string{"", "console=ttyS0 rdinit=/bin/sh"} {
		v.ParamsSet = true
		v.Params = override
		if got := v.bootParams(embedded); got != override {
			t.Errorf("command-line override: got %q, want %q", got, override)
		}
	}
}

func TestUKIConfigRejectsConflictingBootSourcesBeforeKVM(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		conf Config
	}{
		{name: "ISO", conf: Config{UKI: "netdesk.efi", ISO: "live.iso"}},
		{name: "kernel", conf: Config{UKI: "netdesk.efi", Kernel: "bzImage"}},
		{name: "initrd", conf: Config{UKI: "netdesk.efi", Initrd: "initrd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			v := New(tc.conf)
			if err := v.Init(); !errors.Is(err, errUKIBootSource) {
				t.Fatalf("Init returned %v", err)
			}
			if err := v.Setup(); !errors.Is(err, errUKIBootSource) {
				t.Fatalf("Setup returned %v", err)
			}
		})
	}
}

func TestSetupUKIRejectsMalformedImageBeforeLoading(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "invalid.efi")
	if err := os.WriteFile(path, []byte("not a UKI"), 0o600); err != nil {
		t.Fatal(err)
	}
	v := New(Config{UKI: path})
	if err := v.Setup(); err == nil || !strings.Contains(err.Error(), "read UKI") {
		t.Fatalf("Setup returned %v", err)
	}
}

func TestBootSourceDownloadAndCleanup(t *testing.T) {
	t.Parallel()

	const payload = "boot image bytes"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, payload)
	}))
	defer server.Close()

	file, cleanup, err := openBootSource(server.URL + "/netdesk.efi")
	if err != nil {
		t.Fatal(err)
	}
	defer cleanup()
	data, err := io.ReadAll(file)
	if err != nil || string(data) != payload {
		t.Fatalf("downloaded %q, err %v", data, err)
	}
	cleanup()
	if _, err := os.Stat(file.Name()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary image still exists: %v", err)
	}
}

func TestBootSourceRejectsFailedDownload(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.NotFoundHandler())
	defer server.Close()
	file, cleanup, err := openBootSource(server.URL + "/netdesk.efi")
	if !errors.Is(err, errDownloadBootImage) || file != nil || cleanup != nil {
		t.Fatalf("failed download returned file %v, cleanup set %v, err %v", file, cleanup != nil, err)
	}
}

func TestBootSourceCleanupPreservesLocalImage(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "netdesk.efi")
	if err := os.WriteFile(path, []byte("local image"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, cleanup, err := openBootSource(path)
	if err != nil {
		t.Fatal(err)
	}
	cleanup()
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("cleanup removed local source: %v", err)
	}
}
