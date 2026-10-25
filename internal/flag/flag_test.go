package flag_test

import (
	"errors"
	"strconv"
	"strings"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/ebda"
	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestParsesize(t *testing.T) { // nolint:paralleltest
	for _, tt := range []struct {
		name string
		unit string
		m    string
		amt  int
		err  error
	}{
		{name: "badsuffix", m: "1T", amt: -1, err: strconv.ErrSyntax},
		{name: "1G", m: "1G", amt: 1 << 30, err: nil},
		{name: "1g", m: "1g", amt: 1 << 30, err: nil},
		{name: "1M", m: "1M", amt: 1 << 20, err: nil},
		{name: "1m", m: "1m", amt: 1 << 20, err: nil},
		{name: "1K", m: "1K", amt: 1 << 10, err: nil},
		{name: "1k", m: "1k", amt: 1 << 10, err: nil},
		{name: "1 with unit k", m: "1", unit: "k", amt: 1 << 10, err: nil},
		{name: "1 with unit \"\"", m: "1", unit: "", amt: 1, err: nil},
		{name: "8192m", m: "8192m", amt: 8192 << 20, err: nil},
		{name: "bogusgarbage", m: "123411;3413234134", amt: -1, err: strconv.ErrSyntax},
		{name: "bogusgarbagemsuffix", m: "123411;3413234134m", amt: -1, err: strconv.ErrSyntax},
		{name: "bogustoobig", m: "0xfffffffffffffffffffffff", amt: -1, err: strconv.ErrRange},
	} {
		amt, err := flag.ParseSize(tt.m, tt.unit)
		if !errors.Is(err, tt.err) || amt != tt.amt {
			t.Errorf("%s:parseMemSize(%s): got (%d, %v), want (%d, %v)", tt.name, tt.m, amt, err, tt.amt, tt.err)
		}
	}
}

func TestParseBootArgs(t *testing.T) {
	t.Parallel()

	args := []string{
		"devbox-kvm",
		"boot",
		"-i",
		"initrd_path",
		"-k",
		"kernel_path",
		"-p",
		"params",
		"-t",
		"tap_if_name",
		"-c",
		"2",
		"-d",
		"disk_path",
		"-rdp-fd", "3",
		"-rdp-cert", "console.crt",
		"-rdp-key", "console.key",
		"-rdp-h264",
		"-rdp-stats",
		"-m",
		"1G",
		"-T",
		"1M",
	}

	c, _, err := flag.ParseArgs(args)
	if err != nil {
		t.Fatal(err)
	}

	if c.Dev != "/dev/kvm" {
		t.Error("invalid kvm  path")
	}

	if c.Kernel != "kernel_path" {
		t.Error("invalid kernel image path")
	}

	if c.Initrd != "initrd_path" {
		t.Error("invalid initrd path")
	}

	if c.Params != "params" {
		t.Error("invalid kernel command-line parameters")
	}

	if !c.ParamsSet {
		t.Error("kernel command-line should be marked explicitly set")
	}

	if c.TapIfName != "tap_if_name" {
		t.Error("invalid name of tap interface")
	}

	if c.Disk != "disk_path" {
		t.Errorf("invalid path of disk file: got %v, want %v", c.Disk, "disk_path")
	}

	if c.RDPFD != 3 || c.RDPCert != "console.crt" || c.RDPKey != "console.key" || !c.RDPH264 {
		t.Errorf("invalid RDP options: %+v", c)
	}
	if !c.RDPStats {
		t.Errorf("invalid RDP performance options: %+v", c)
	}

	if c.NCPUs != 2 {
		t.Error("invalid number of vcpus")
	}

	if c.MemSize != 1<<30 {
		t.Errorf("msize: got %#x, want %#x", c.MemSize, 1<<30)
	}

	if c.TraceCount != 1<<20 {
		t.Errorf("trace: got %#x, want %#x", c.TraceCount, 1<<20)
	}
}

func TestParseBootArgsWithDefaults(t *testing.T) {
	t.Parallel()

	args := []string{
		"devbox-kvm",
		"boot",
	}

	c, _, err := flag.ParseArgs(args)
	if err != nil {
		t.Fatal(err)
	}

	if c.Dev != "/dev/kvm" {
		t.Error("invalid kvm path")
	}

	if c.Kernel != "./bzImage" {
		t.Error("invalid kernel image path")
	}

	if c.Initrd != "" {
		t.Error("invalid initrd path")
	}

	if c.Params != `console=tty0 console=ttyS0 earlyprintk=serial `+
		`noapic noacpi nowatchdog nmi_watchdog=0 mitigations=off lapic `+
		`pci=realloc=off `+
		`virtio_pci.force_legacy=1 rdinit=/init init=/init `+
		`devbox-kvm.ipv4_addr=192.168.20.1/24` {
		t.Error("invalid kernel command-line parameters")
	}

	if c.TapIfName != "" {
		t.Error("invalid name of tap interface")
	}

	if c.ParamsSet {
		t.Error("kernel command-line should not be marked explicitly set")
	}

	if c.Disk != "" {
		t.Errorf("invalid path of disk file: got %v, want %v", c.Disk, "disk_path")
	}

	if c.RDPFD != 0 || c.RDPCert != "" || c.RDPKey != "" || c.RDPH264 {
		t.Errorf("RDP must be disabled by default: %+v", c)
	}
	if c.RDPStats {
		t.Errorf("RDP statistics must be disabled by default: %+v", c)
	}

	if c.NCPUs != 1 {
		t.Error("invalid number of vcpus")
	}

	if c.MemSize != 1<<30 {
		t.Errorf("msize: got %#x, want %#x", c.MemSize, 1<<30)
	}

	if c.TraceCount != 0 {
		t.Errorf("trace: got %#x, want %#x", c.TraceCount, 1<<20)
	}
}

func TestParseBootArgsRejectsRemovedDisplayFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		flag  string
		value string
	}{
		{name: "slices", flag: "-rdp-h264-threads", value: "4"},
		{name: "profile", flag: "-rdp-profile", value: "smooth"},
		{name: "png", flag: "-g", value: "framebuffer.png"},
		{name: "vnc", flag: "-vnc", value: "127.0.0.1:5900"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", tc.flag, tc.value})
			if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: "+tc.flag) {
				t.Fatalf("removed display flag %s returned %v, want unknown flag", tc.flag, err)
			}
		})
	}
}

func TestParseBootArgsRejectsRemovedMediaFlags(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		flag  string
		value string
	}{
		{name: "iso path", flag: "-iso", value: "installer.iso"},
		{name: "iso url", flag: "-iso", value: "https://example.test/installer.iso"},
		{name: "uki path", flag: "-uki", value: "guest.efi"},
		{name: "uki url", flag: "-uki", value: "https://example.test/guest.efi"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			_, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", tc.flag, tc.value})
			if err == nil || !strings.Contains(err.Error(), "flag provided but not defined: "+tc.flag) {
				t.Fatalf("removed media flag %s returned %v, want unknown flag", tc.flag, err)
			}
		})
	}
}

func TestParseBootArgsPreservesExplicitEmptyParams(t *testing.T) {
	t.Parallel()
	c, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-k", "vmlinux", "-p", ""})
	if err != nil {
		t.Fatal(err)
	}
	if c.Kernel != "vmlinux" || !c.ParamsSet || c.Params != "" {
		t.Fatalf("explicit empty command line was lost: %+v", c)
	}
}

func TestParseBootArgsCPUCount(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name  string
		count int
		valid bool
	}{
		{name: "negative", count: -1},
		{name: "zero", count: 0},
		{name: "one", count: 1, valid: true},
		{name: "maximum", count: ebda.MaxVCPUs, valid: true},
		{name: "too-many", count: ebda.MaxVCPUs + 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			c, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-c", strconv.Itoa(tc.count)})
			if !tc.valid {
				if !errors.Is(err, flag.ErrCPUCount) {
					t.Fatalf("got %v, want %v", err, flag.ErrCPUCount)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if c.NCPUs != tc.count {
				t.Fatalf("got %d CPUs, want %d", c.NCPUs, tc.count)
			}
		})
	}
}

func TestParseBootArgsPreservesExplicitCPUParams(t *testing.T) {
	t.Parallel()
	const params = `notsc tsc_early_khz=2000 debug apic=debug show_lapic=all nosmp dyndbg="file smpboot.c +p"`
	c, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-p", params})
	if err != nil {
		t.Fatal(err)
	}
	if c.Params != params || !c.ParamsSet {
		t.Fatalf("explicit parameters changed: %+v", c)
	}
}

func TestRDPRequiresCertificatePair(t *testing.T) {
	t.Parallel()
	for _, args := range [][]string{
		{"-rdp-fd", "3", "-rdp-cert", "console.crt"},
		{"-rdp-fd", "3", "-rdp-key", "console.key"},
		{"-rdp-cert", "console.crt", "-rdp-key", "console.key"},
	} {
		if _, _, err := flag.ParseArgs(append([]string{"devbox-kvm", "boot"}, args...)); !errors.Is(err, flag.ErrRDPOptions) {
			t.Errorf("args %v: got %v, want %v", args, err, flag.ErrRDPOptions)
		}
	}
}

func TestRDPH264RequiresListener(t *testing.T) {
	t.Parallel()
	if _, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-rdp-h264"}); !errors.Is(err, flag.ErrRDPH264) {
		t.Fatalf("H.264 without RDP listener: got %v, want %v", err, flag.ErrRDPH264)
	}
}

func TestRDPStatisticsOptions(t *testing.T) {
	t.Parallel()
	if _, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-rdp-stats"}); !errors.Is(err, flag.ErrRDPStats) {
		t.Fatalf("statistics without RDP listener: got %v, want %v", err, flag.ErrRDPStats)
	}
	c, _, err := flag.ParseArgs([]string{"devbox-kvm", "boot", "-rdp-fd", "3", "-rdp-stats"})
	if err != nil || !c.RDPStats || c.RDPH264 {
		t.Fatalf("statistics should work for bitmap RDP: config %+v, error %v", c, err)
	}
}

func TestParseProbeArgs(t *testing.T) {
	t.Parallel()

	args := []string{
		"devbox-kvm",
		"probe",
	}

	_, probeConfig, err := flag.ParseArgs(args)
	if err != nil {
		t.Fatal(err)
	}

	if probeConfig == nil {
		t.Fatal("probeConfig is nil")
	}
}
