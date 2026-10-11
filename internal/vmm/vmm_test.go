package vmm

import (
	"bytes"
	"compress/gzip"
	"io"
	"strings"
	"testing"
)

func TestISOBootParamsAddsOnlyDirectLinuxDefaults(t *testing.T) {
	t.Parallel()

	cmdline := isoBootParams("loglevel=3 cde")

	for _, want := range []string{
		"console=tty0",
		"console=ttyS0",
		"earlyprintk=serial",
		"noapic",
		"noacpi",
		"nortc",
		"pci=realloc=off",
		"virtio_pci.force_legacy=1",
		"loglevel=3",
		"cde",
	} {
		if !hasField(cmdline, want) {
			t.Fatalf("cmdline %q is missing %q", cmdline, want)
		}
	}

	for _, unwanted := range []string{
		"nosmp",
		"maxcpus=0",
		"maxcpus=1",
		"nr_cpus=1",
		"desktop=flwm",
		"icons=wbar",
		"notsc",
		"tsc_early_khz=2000",
		"xvesa=1024x768x32",
	} {
		if hasField(cmdline, unwanted) {
			t.Fatalf("cmdline %q should not add %q", cmdline, unwanted)
		}
	}
}

func TestISOBootParamsPreservesExplicitCPULimits(t *testing.T) {
	t.Parallel()

	for _, limit := range []string{"nosmp", "maxcpus=1", "nr_cpus=1"} {
		t.Run(limit, func(t *testing.T) {
			t.Parallel()
			cmdline := isoBootParams("loglevel=3 " + limit)
			if got := countField(cmdline, limit); got != 1 {
				t.Fatalf("explicit CPU limit %q appears %d times in %q", limit, got, cmdline)
			}
		})
	}
}

func TestISOBootParamsDoesNotDuplicateExistingParams(t *testing.T) {
	t.Parallel()

	cmdline := isoBootParams("console=ttyS0 pci=nomsi custom=1")

	if got := countField(cmdline, "console=ttyS0"); got != 1 {
		t.Fatalf("console=ttyS0 count: got %d, want 1 in %q", got, cmdline)
	}

	if hasField(cmdline, "pci=realloc=off") {
		t.Fatalf("cmdline %q should not add a second pci= parameter", cmdline)
	}

	if !hasField(cmdline, "pci=nomsi") {
		t.Fatalf("cmdline %q should keep ISO pci= parameter", cmdline)
	}
}

func TestAddTinyCoreVNCAutostart(t *testing.T) {
	t.Parallel()

	var raw bytes.Buffer
	writeNewcEntry(&raw, "TRAILER!!!", 0, nil)

	var compressed bytes.Buffer
	zw := gzip.NewWriter(&compressed)
	if _, err := zw.Write(raw.Bytes()); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}

	patched, err := addTinyCoreVNCAutostart(compressed.Bytes())
	if err != nil {
		t.Fatal(err)
	}

	zr, err := gzip.NewReader(bytes.NewReader(patched))
	if err != nil {
		t.Fatal(err)
	}
	defer zr.Close()

	data, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}

	if !bytes.Contains(data, []byte("sbin/autologin")) {
		t.Fatalf("patched initramfs does not contain autologin overlay")
	}

	if !bytes.Contains(data, []byte("echo flwm > /etc/sysconfig/desktop")) {
		t.Fatalf("patched initramfs does not seed TinyCore desktop")
	}

	if !bytes.Contains(data, []byte("echo wbar > /etc/sysconfig/icons")) {
		t.Fatalf("patched initramfs does not seed TinyCore icons")
	}

	if !bytes.Contains(data, []byte("echo Xvesa > /etc/sysconfig/Xserver")) {
		t.Fatalf("patched initramfs does not seed TinyCore X server")
	}

	if bytes.Contains(data, []byte("Xvesa -listmodes")) {
		t.Fatalf("patched initramfs still contains diagnostic Xvesa mode probe")
	}

	if !bytes.Contains(data, []byte("-mouse /dev/input/mice,5")) {
		t.Fatalf("patched initramfs does not use TinyCore's mouse input protocol")
	}

	if !bytes.Contains(data, []byte("-a 1 -t 0")) {
		t.Fatalf("patched initramfs does not disable Xvesa mouse acceleration")
	}

	if !bytes.Contains(data, []byte("/tmp/gokvm-vnc-session")) {
		t.Fatalf("patched initramfs does not create the VNC session script")
	}

	if !bytes.Contains(data, []byte("flwm >/tmp/flwm.log")) {
		t.Fatalf("patched initramfs does not start flwm")
	}

	if !bytes.Contains(data, []byte("wbar.sh >/tmp/wbar.log")) {
		t.Fatalf("patched initramfs does not start wbar")
	}
}

func hasField(s, want string) bool {
	for _, field := range strings.Fields(s) {
		if field == want {
			return true
		}
	}

	return false
}

func countField(s, want string) int {
	count := 0
	for _, field := range strings.Fields(s) {
		if field == want {
			count++
		}
	}

	return count
}
