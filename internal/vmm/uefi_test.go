package vmm

import (
	"errors"
	"testing"
)

func TestUEFIConfigRejectsInvalidOptionsBeforeKVM(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		modify func(*Config)
		err    error
	}{
		{name: "kernel", modify: func(c *Config) { c.Kernel = "bzImage" }, err: errUEFIBootSource},
		{name: "initrd", modify: func(c *Config) { c.Initrd = "initrd" }, err: errUEFIBootSource},
		{name: "parameters", modify: func(c *Config) { c.Params = "console=tty0" }, err: errUEFIBootSource},
		{name: "empty parameters", modify: func(c *Config) { c.ParamsSet = true }, err: errUEFIBootSource},
		{name: "missing code", modify: func(c *Config) { c.UEFI = "" }, err: errUEFIVars},
		{name: "missing variables", modify: func(c *Config) { c.UEFIVars = "" }, err: errUEFIVars},
		{name: "missing disk", modify: func(c *Config) { c.Disk = "" }, err: errUEFIDisk},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			conf := Config{Dev: "/no/kvm", UEFI: "OVMF_CODE.fd", UEFIVars: "guest_VARS.fd", Disk: "guest.qcow2"}
			tc.modify(&conf)
			v := New(conf)
			if err := v.Init(); !errors.Is(err, tc.err) {
				t.Fatalf("Init returned %v, want %v", err, tc.err)
			}
			if err := v.Setup(); !errors.Is(err, tc.err) {
				t.Fatalf("Setup returned %v, want %v", err, tc.err)
			}
		})
	}
}

func TestUEFIConfigAcceptsFirmwareAndDisk(t *testing.T) {
	t.Parallel()

	v := New(Config{UEFI: "OVMF_CODE.fd", UEFIVars: "guest_VARS.fd", Disk: "guest.qcow2"})
	if err := v.validateUEFIBootSource(); err != nil {
		t.Fatal(err)
	}
}
