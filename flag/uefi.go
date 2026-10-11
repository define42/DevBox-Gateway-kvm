package flag

import (
	"errors"
	"flag"
)

var ErrUEFIBootSource = errors.New("-uefi cannot be combined with -k, -i, -uki, -iso, or -p")

var ErrUEFIVars = errors.New("-uefi and -uefi-vars must be provided together")

var ErrUEFIDisk = errors.New("-uefi requires a boot disk supplied with -d")

func selectUEFIBootSource(cmd *flag.FlagSet, c *BootArgs) error {
	if (c.UEFI == "") != (c.UEFIVars == "") {
		return ErrUEFIVars
	}
	if c.UEFI == "" {
		return nil
	}
	var directBootFlag bool
	cmd.Visit(func(f *flag.Flag) {
		switch f.Name {
		case "k", "i", "uki", "iso", "p":
			directBootFlag = true
		}
	})
	if directBootFlag {
		return ErrUEFIBootSource
	}
	if c.Disk == "" {
		return ErrUEFIDisk
	}
	// Firmware loads the disk's bootloader and command line. Direct Linux
	// defaults must not leak into this mode.
	c.Kernel = ""
	c.Params = ""

	return nil
}
