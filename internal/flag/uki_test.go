package flag_test

import (
	"errors"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/flag"
)

func TestParseUKIBoot(t *testing.T) {
	t.Parallel()

	args := []string{"gokvm", "boot", "-uki", "netdesk.efi", "-m", "6G", "-net", "user"}
	c, _, err := flag.ParseArgs(args)
	if err != nil {
		t.Fatal(err)
	}
	if c.UKI != "netdesk.efi" || c.Kernel != "" || c.Initrd != "" || c.ISO != "" {
		t.Fatalf("incorrect UKI boot source: %+v", c)
	}
	if c.MemSize != 6<<30 || c.Network != "user" || c.ParamsSet {
		t.Fatalf("incorrect UKI boot options: %+v", c)
	}
}

func TestUKIRejectsConflictingBootSources(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		args []string
	}{
		{name: "iso", args: []string{"-iso", "live.iso"}},
		{name: "kernel", args: []string{"-k", "bzImage"}},
		{name: "explicit default kernel", args: []string{"-k", "./bzImage"}},
		{name: "initrd", args: []string{"-i", "initrd"}},
		{name: "explicit empty initrd", args: []string{"-i", ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			args := append([]string{"gokvm", "boot", "-uki", "netdesk.efi"}, tc.args...)
			if _, _, err := flag.ParseArgs(args); !errors.Is(err, flag.ErrUKIBootSource) {
				t.Fatalf("got %v, want ErrUKIBootSource", err)
			}
		})
	}
}

func TestUKIExplicitEmptyCommandLine(t *testing.T) {
	t.Parallel()

	c, _, err := flag.ParseArgs([]string{"gokvm", "boot", "-uki", "netdesk.efi", "-p", ""})
	if err != nil {
		t.Fatal(err)
	}
	if !c.ParamsSet || c.Params != "" {
		t.Fatalf("explicit empty command line was lost: %+v", c)
	}
}
