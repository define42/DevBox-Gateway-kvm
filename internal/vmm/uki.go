package vmm

import (
	"fmt"
	"log"

	"github.com/define42/DevBox-Gateway-kvm/internal/uki"
)

func (v *VMM) validateBootSource() error {
	if err := v.validateUEFIBootSource(); err != nil {
		return err
	}
	if v.UKI != "" && (v.ISO != "" || v.Kernel != "" || v.Initrd != "") {
		return errUKIBootSource
	}

	return nil
}

func (v *VMM) bootParams(embedded string) string {
	if v.ParamsSet {
		return v.consoleBootParams(v.Params)
	}

	return v.consoleBootParams(isoBootParams(embedded))
}

func (v *VMM) setupUKI() error {
	file, cleanup, err := openBootSource(v.UKI)
	if err != nil {
		return fmt.Errorf("open UKI: %w", err)
	}
	// UKIs carry their entire initramfs. After loading, neither the source
	// file nor an attached disk is needed for the guest's lifetime.
	defer cleanup()

	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("stat UKI: %w", err)
	}
	image, err := uki.Open(file, info.Size())
	if err != nil {
		return fmt.Errorf("read UKI: %w", err)
	}
	log.Printf("UKI direct Linux boot: kernel=%d bytes initrd=%d bytes", image.Kernel.Size(), image.Initrd.Size())
	if err := v.LoadLinux(image.Kernel, image.Initrd, v.bootParams(image.Cmdline)); err != nil {
		return fmt.Errorf("load UKI: %w", err)
	}

	return nil
}
