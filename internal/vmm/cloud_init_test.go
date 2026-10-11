package vmm

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/define42/gokvm/internal/cloudinit"
	"github.com/define42/gokvm/internal/machine"
)

func writeCloudInitSeed(t *testing.T) string {
	t.Helper()
	data, err := cloudinit.CreateSeedISO(cloudinit.Seed{
		UserData: []byte("#cloud-config\n{}\n"), MetaData: []byte("instance-id: test-vm\n"),
	})
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "seed.iso")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func TestCloudInitRejectedBeforeKVM(t *testing.T) {
	t.Parallel()

	invalidSeed := filepath.Join(t.TempDir(), "not-iso")
	if err := os.WriteFile(invalidSeed, []byte("invalid seed"), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		conf Config
		want error
	}{
		{name: "missing boot disk", conf: Config{CloudInit: "seed.iso"}, want: errCloudInitDisk},
		{name: "missing seed", conf: Config{CloudInit: invalidSeed + ".missing", Disk: "disk.raw"}, want: os.ErrNotExist},
		{name: "invalid seed", conf: Config{CloudInit: invalidSeed, Disk: "disk.raw"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conf := tc.conf
			conf.Dev, conf.NCPUs, conf.MemSize = "/unavailable/kvm", 1, machine.MinMemSize
			v := New(conf)
			err := v.Init()
			if err == nil || !strings.Contains(err.Error(), "cloud-init") {
				t.Fatalf("Init did not reject the seed before KVM: %v", err)
			}
			if tc.want != nil && !errors.Is(err, tc.want) {
				t.Fatalf("Init error = %v, want %v", err, tc.want)
			}
			if v.Machine != nil {
				t.Fatal("invalid seed initialized the machine")
			}
		})
	}
}

func TestCloudInitBootModes(t *testing.T) {
	t.Parallel()

	seed := writeCloudInitSeed(t)
	disk := filepath.Join(t.TempDir(), "boot.raw")
	if err := os.WriteFile(disk, make([]byte, 512), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name string
		conf Config
	}{
		{name: "kernel", conf: Config{Kernel: "bzImage"}},
		{name: "UKI", conf: Config{UKI: "desktop.efi"}},
		{name: "ISO", conf: Config{ISO: "desktop.iso"}},
		{name: "UEFI", conf: Config{UEFI: "OVMF_CODE.fd", UEFIVars: "vm_VARS.fd"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conf := tc.conf
			conf.CloudInit, conf.Disk = seed, disk
			conf.Dev, conf.NCPUs, conf.MemSize = "/unavailable/kvm", 1, machine.MinMemSize
			v := New(conf)
			var pathErr *os.PathError
			if err := v.Init(); !errors.As(err, &pathErr) || pathErr.Path != conf.Dev {
				t.Fatalf("valid seed and boot source did not reach KVM initialization: %v", err)
			}
		})
	}
}

func TestCloudInitCannotBeWritableBootDisk(t *testing.T) {
	t.Parallel()

	seed := writeCloudInitSeed(t)
	alias := filepath.Join(t.TempDir(), "boot.raw")
	if err := os.Link(seed, alias); err != nil {
		t.Fatal(err)
	}
	for _, disk := range []string{seed, alias} {
		if err := New(Config{CloudInit: seed, Disk: disk}).validateCloudInit(); !errors.Is(err, errCloudInitBootDisk) {
			t.Fatalf("seed also attached writable: %v", err)
		}
	}
}
