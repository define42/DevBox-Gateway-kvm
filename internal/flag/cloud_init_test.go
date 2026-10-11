package flag_test

import (
	"errors"
	"reflect"
	"testing"

	"github.com/define42/gokvm/internal/flag"
)

func TestCloudInitDefaultsEmpty(t *testing.T) {
	t.Parallel()

	args, _, err := flag.ParseArgs([]string{"gokvm", "boot"})
	if err != nil {
		t.Fatal(err)
	}
	if args.CloudInit != "" {
		t.Fatalf("cloud-init = %q, want empty", args.CloudInit)
	}
}

func TestCloudInitPreservesBootOptions(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "direct defaults"},
		{name: "direct explicit", args: []string{"-k", "vmlinuz", "-i", "initrd", "-p", "console=tty0"}},
		{name: "UKI", args: []string{"-uki", "guest.efi", "-p", ""}},
		{name: "UEFI", args: []string{"-uefi", "OVMF_CODE.fd", "-uefi-vars", "guest_VARS.fd"}},
		{name: "ISO", args: []string{"-iso", "installer.iso"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli := append([]string{
				"gokvm", "boot", "-d", "guest.qcow2", "-m", "2G", "-c", "2",
				"-net", "user", "-rdp", "127.0.0.1:3390", "-rdp-wait",
			}, tc.args...)
			want, _, err := flag.ParseArgs(cli)
			if err != nil {
				t.Fatal(err)
			}
			got, _, err := flag.ParseArgs(append(cli, "-cloud-init", "seed.iso"))
			if err != nil {
				t.Fatal(err)
			}
			if got.CloudInit != "seed.iso" {
				t.Fatalf("cloud-init = %q, want seed.iso", got.CloudInit)
			}
			got.CloudInit = ""
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("cloud-init changed other boot options: got %+v, want %+v", got, want)
			}
		})
	}
}

func TestCloudInitRequiresPrimaryDisk(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "missing disk"},
		{name: "empty disk", args: []string{"-d", ""}},
		{name: "UKI", args: []string{"-uki", "guest.efi"}},
		{name: "UEFI", args: []string{"-uefi", "OVMF_CODE.fd", "-uefi-vars", "guest_VARS.fd"}},
		{name: "ISO", args: []string{"-iso", "installer.iso"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			cli := append([]string{"gokvm", "boot", "-cloud-init", "seed.iso"}, tc.args...)
			if _, _, err := flag.ParseArgs(cli); !errors.Is(err, flag.ErrCloudInitDisk) {
				t.Fatalf("got %v, want ErrCloudInitDisk", err)
			}
		})
	}
}
