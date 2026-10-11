package vmm

import (
	"errors"
	"fmt"
	"log"
)

var errUEFIBootSource = errors.New("UEFI cannot be combined with kernel, initrd, UKI, ISO, or kernel parameters")

var errUEFIVars = errors.New("UEFI code and a writable per-VM variables file must be provided together")

var errUEFIDisk = errors.New("UEFI requires a boot disk")

func (v *VMM) validateUEFIBootSource() error {
	if (v.UEFI == "") != (v.UEFIVars == "") {
		return errUEFIVars
	}
	if v.UEFI == "" {
		return nil
	}
	if v.Kernel != "" || v.Initrd != "" || v.UKI != "" || v.ISO != "" || v.Params != "" || v.ParamsSet {
		return errUEFIBootSource
	}
	if v.Disk == "" {
		return errUEFIDisk
	}

	return nil
}

func (v *VMM) setupUEFI() error {
	if err := v.LoadUEFI(v.UEFI, v.UEFIVars); err != nil {
		return fmt.Errorf("load UEFI firmware: %w", err)
	}
	v.attachSerialOutput()
	log.Printf("UEFI disk boot: firmware=%s variables=%s disk=%s", v.UEFI, v.UEFIVars, v.Disk)

	return nil
}
