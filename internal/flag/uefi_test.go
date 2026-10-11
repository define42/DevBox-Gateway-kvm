package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/gokvm/internal/flag"
)

func TestParseUEFIBoot(t *testing.T) {
	t.Parallel()

	args := []string{
		"gokvm", "boot", "-uefi", "OVMF_CODE.fd", "-uefi-vars", "guest_VARS.fd",
		"-d", "guest.qcow2", "-c", "4", "-m", "2G", "-net", "user", "-rdp", "127.0.0.1:3390",
	}
	c, _, err := flag.ParseArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if c.UEFI != "OVMF_CODE.fd" || c.UEFIVars != "guest_VARS.fd" || c.Disk != "guest.qcow2" {
		t.Fatalf("incorrect UEFI boot source: %+v", c)
	}
	if c.Kernel != "" || c.Initrd != "" || c.UKI != "" || c.ISO != "" || c.Params != "" || c.ParamsSet {
		t.Fatalf("direct boot defaults leaked into UEFI mode: %+v", c)
	}
	if c.NCPUs != 4 || c.MemSize != 2<<30 || c.Network != "user" || c.RDP != "127.0.0.1:3390" {
		t.Fatalf("incorrect UEFI device options: %+v", c)
	}
}

func TestUEFIRejectsConflictingBootFlags(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"k", "i", "uki", "iso", "p"} {
		for _, value := range []string{"", "payload"} {
			t.Run(name+"/"+value, func(t *testing.T) {
				t.Parallel()
				args := []string{
					"gokvm", "boot", "-uefi", "OVMF_CODE.fd", "-uefi-vars", "guest_VARS.fd",
					"-d", "guest.qcow2", "-" + name, value,
				}
				if _, _, err := flag.ParseArgs(args); !errors.Is(err, flag.ErrUEFIBootSource) {
					t.Fatalf("got %v, want ErrUEFIBootSource", err)
				}
			})
		}
	}
}

func TestUEFIRequiresFirmwarePairAndDisk(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
		err  error
	}{
		{name: "missing variables", args: []string{"-uefi", "OVMF_CODE.fd", "-d", "guest.qcow2"}, err: flag.ErrUEFIVars},
		{name: "missing code", args: []string{"-uefi-vars", "guest_VARS.fd"}, err: flag.ErrUEFIVars},
		{name: "missing disk", args: []string{"-uefi", "OVMF_CODE.fd", "-uefi-vars", "guest_VARS.fd"}, err: flag.ErrUEFIDisk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"gokvm", "boot"}, tc.args...)
			if _, _, err := flag.ParseArgs(args); !errors.Is(err, tc.err) {
				t.Fatalf("got %v, want %v", err, tc.err)
			}
		})
	}
}
