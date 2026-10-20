//go:build integration

package vmm

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

// TestUEFIQCOW2Boot builds a disk containing its own EFI boot program. OVMF
// must discover the virtio QCOW2 disk, load EFI/BOOT/BOOTX64.EFI and execute it.
// The program reports success over COM1 and requests a real firmware shutdown.
func TestUEFIQCOW2Boot(t *testing.T) {
	t.Parallel()
	if runtime.GOARCH != "amd64" {
		uefiPrerequisiteUnavailablef(t, "the UEFI smoke fixture requires amd64")
	}
	for _, tool := range []string{"gcc", "ld", "objcopy", "mkfs.vfat", "mmd", "mcopy", "qemu-img"} {
		if _, err := exec.LookPath(tool); err != nil {
			uefiPrerequisiteUnavailablef(t, "%s is required: %v", tool, err)
		}
	}
	for _, path := range []string{
		"/usr/include/efi/efi.h", "/usr/lib/crt0-efi-x86_64.o",
		"/usr/lib/elf_x86_64_efi.lds", "/usr/lib/libefi.a", "/usr/lib/libgnuefi.a",
	} {
		if _, err := os.Stat(path); err != nil {
			uefiPrerequisiteUnavailablef(t, "install gnu-efi (%s): %v", path, err)
		}
	}
	code := uefiTestFirmware(t, "DEVBOX_KVM_TEST_OVMF_CODE", "/usr/share/OVMF/OVMF_CODE_4M.fd")
	varsTemplate := uefiTestFirmware(t, "DEVBOX_KVM_TEST_OVMF_VARS", "/usr/share/OVMF/OVMF_VARS_4M.fd")
	kvm, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		uefiPrerequisiteUnavailablef(t, "KVM unavailable: %v", err)
	}
	if err := kvm.Close(); err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	disk := buildUEFISmokeDisk(t, dir)
	vars := filepath.Join(dir, "guest_VARS.fd")
	data, err := os.ReadFile(varsTemplate)
	if err != nil {
		t.Fatal(err)
	}
	// #nosec G703 -- The destination is inside this test's own temporary directory.
	if err := os.WriteFile(vars, data, 0o600); err != nil {
		t.Fatal(err)
	}

	v := New(Config{
		Dev: "/dev/kvm", NCPUs: 1, MemSize: 256 << 20, Network: "none",
		UEFI: code, UEFIVars: vars, Disk: disk,
	})
	if err := v.Init(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := v.Close(); err != nil {
			t.Error(err)
		}
	})
	if err := v.Setup(); err != nil {
		t.Fatal(err)
	}
	var serial bytes.Buffer
	v.GetSerial().SetOutput(&serial)
	ctx, cancel := context.WithTimeout(t.Context(), 90*time.Second)
	defer cancel()
	// BootContext joins the vCPU workers before returning, so reading serial
	// below cannot race with writes from the guest.
	if err := v.BootContext(ctx); err != nil {
		t.Fatalf("QCOW2 UEFI boot/shutdown failed: %v\nserial:\n%s", err, serial.String())
	}
	if !bytes.Contains(serial.Bytes(), []byte("DEVBOX_KVM_QCOW2_UEFI_BOOT_OK\r\n")) {
		t.Fatalf("firmware stopped without executing the QCOW2 boot program; serial:\n%s", serial.String())
	}
	t.Log("OVMF loaded the QCOW2 EFI boot program and completed firmware shutdown")
}

func uefiPrerequisiteUnavailablef(t *testing.T, format string, args ...any) {
	t.Helper()
	if os.Getenv("DEVBOX_KVM_REQUIRE_UEFI_TEST") == "1" {
		t.Fatalf(format, args...)
	}
	t.Skipf(format, args...)
}

func uefiTestFirmware(t *testing.T, variable, fallback string) string {
	t.Helper()
	path := os.Getenv(variable)
	if path == "" {
		path = fallback
	}
	// #nosec G703 -- The test operator explicitly selects the local firmware to read.
	if _, err := os.Stat(path); err != nil {
		uefiPrerequisiteUnavailablef(t, "set %s to an OVMF firmware file: %v", variable, err)
	}

	return path
}

func buildUEFISmokeDisk(t *testing.T, dir string) string {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	object := filepath.Join(dir, "smoke.o")
	shared := filepath.Join(dir, "smoke.so")
	app := filepath.Join(dir, "BOOTX64.EFI")
	raw := filepath.Join(dir, "boot.raw")
	disk := filepath.Join(dir, "boot.qcow2")
	commands := [][]string{
		{
			"gcc", "-I/usr/include/efi", "-I/usr/include/efi/x86_64", "-fpic", "-fshort-wchar",
			"-mno-red-zone", "-fno-stack-protector", "-DEFI_FUNCTION_WRAPPER", "-Wall", "-Wextra", "-Werror",
			"-c", "testdata/uefi-smoke.c", "-o", object,
		},
		{
			"ld", "-nostdlib", "-znocombreloc", "-T", "/usr/lib/elf_x86_64_efi.lds", "-shared", "-Bsymbolic",
			"/usr/lib/crt0-efi-x86_64.o", object, "-L/usr/lib", "-lefi", "-lgnuefi", "-o", shared,
		},
		{
			"objcopy", "-j", ".text", "-j", ".sdata", "-j", ".data", "-j", ".dynamic", "-j", ".dynsym",
			//nolint:misspell // These are ELF section names.
			"-j", ".rel", "-j", ".rela", "-j", ".reloc", "--target=efi-app-x86_64", shared, app,
		},
		{"mkfs.vfat", "-C", "-F", "32", raw, "65536"},
		{"mmd", "-i", raw, "::/EFI", "::/EFI/BOOT"},
		{"mcopy", "-i", raw, app, "::/EFI/BOOT/BOOTX64.EFI"},
		{"qemu-img", "convert", "-f", "raw", "-O", "qcow2", raw, disk},
	}
	for _, args := range commands {
		// #nosec G204 -- Commands are fixed above; arguments reference only test fixtures.
		if output, err := exec.CommandContext(ctx, args[0], args[1:]...).CombinedOutput(); err != nil {
			t.Fatalf("build UEFI fixture: %v: %v\n%s", args, err, output)
		}
	}

	return disk
}
