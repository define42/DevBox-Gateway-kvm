package machine

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"log"
	"os"
	"syscall"
	"unsafe"

	"github.com/define42/gokvm/internal/iodev"
	"github.com/define42/gokvm/kvm"
)

// ErrUEFIReset asks the caller to recreate the VM after a guest firmware reset.
var ErrUEFIReset = errors.New("UEFI guest requested reset")

var (
	errUEFIAlreadyLoaded = errors.New("UEFI firmware already loaded")
	errUEFICodeImage     = errors.New("invalid UEFI code image: expected a page-aligned OVMF_CODE file")
	errUEFIFirmwarePair  = errors.New("UEFI CODE and VARS sizes must form a matching power-of-two firmware image")
)

// EnableUEFIPlatform must precede PCI device attachment. OVMF's i440fx platform
// expects a PIIX ISA bridge and power-management function in slot one.
func (m *Machine) EnableUEFIPlatform() error {
	if err := m.pci.AddUEFIChipset(); err != nil {
		return err
	}
	m.uefiIRQ = true

	return nil
}

// LoadUEFI maps an OVMF code image below 4 GiB and its matching, writable
// variable store immediately below it. The guest starts at the x86 reset
// vector; firmware discovers and boots the attached virtio disk itself.
func (m *Machine) LoadUEFI(codePath, varsPath string) error {
	if m.uefiCode != nil || m.uefiVars != nil {
		return errUEFIAlreadyLoaded
	}
	code, err := os.Open(codePath)
	if err != nil {
		return fmt.Errorf("open UEFI firmware: %w", err)
	}
	defer code.Close()
	info, err := code.Stat()
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() || info.Size() < 4096 || info.Size() > 16<<20 || info.Size()%4096 != 0 {
		return errUEFICodeImage
	}
	var signature [4]byte
	if _, err := code.ReadAt(signature[:], 40); err != nil {
		return fmt.Errorf("read UEFI code header: %w", err)
	}
	if string(signature[:]) != "_FVH" {
		return errUEFICodeImage
	}
	vars, err := openUEFIVars(varsPath, info)
	if err != nil {
		return err
	}
	total := uint64(info.Size()) + uint64(len(vars.data))
	if total > 16<<20 || total&(total-1) != 0 {
		return errors.Join(errUEFIFirmwarePair, vars.Close())
	}
	vars.base = uint64(1)<<32 - total
	m.uefiCodeBase = uint64(1)<<32 - uint64(info.Size())
	m.uefiCode, err = syscall.Mmap(int(code.Fd()), 0, int(info.Size()), syscall.PROT_READ, syscall.MAP_PRIVATE)
	if err != nil {
		return errors.Join(fmt.Errorf("map UEFI code: %w", err), vars.Close())
	}
	m.uefiVars = vars
	region := kvm.UserspaceMemoryRegion{
		Slot: 2, GuestPhysAddr: m.uefiCodeBase, MemorySize: uint64(len(m.uefiCode)),
		UserspaceAddr: uint64(uintptr(unsafe.Pointer(&m.uefiCode[0]))),
	}
	region.SetMemReadonly()
	if err := kvm.SetUserMemoryRegion(m.vmFd, &region); err != nil {
		return fmt.Errorf("register UEFI code: %w", err)
	}
	if err := m.uefiDevices(); err != nil {
		return err
	}
	for cpu, fd := range m.vcpuFds {
		sregs, err := kvm.GetSregs(fd)
		if err != nil {
			return err
		}
		// Preserve KVM's architectural reset state, including the BSP APIC bit.
		sregs.CS.Base = 0xffff0000
		sregs.CS.Selector = 0xf000
		if err := kvm.SetSregs(fd, sregs); err != nil {
			return err
		}
		if err := kvm.SetRegs(fd, &kvm.Regs{RIP: 0xfff0, RFLAGS: 2}); err != nil {
			return err
		}
		state := kvm.MPStateUninitialized
		if cpu == 0 {
			state = kvm.MPStateRunnable
		}
		if err := kvm.SetMPState(fd, &kvm.MPState{State: state}); err != nil {
			return err
		}
	}
	log.Printf("UEFI boot: code=%s variables=%s reset=0xfffffff0", codePath, varsPath)

	return nil
}

func (m *Machine) uefiDevices() error {
	if err := m.initSerial(); err != nil {
		return err
	}
	low, high := m.ramSizes()
	m.AddDevice(iodev.NewCMOS(low, high))
	m.AddDevice(&iodev.FWDebug{})
	power := &uefiPower{timer: iodev.NewACPIPMTimer(), stop: m.Stop, irq: m.sharedIRQSource(9)}
	m.uefiPower = power
	m.AddDevice(power)
	entries := map[uint16][]byte{
		iodev.FWCfgRAMSize:  binary.LittleEndian.AppendUint64(nil, low+high),
		iodev.FWCfgNBCPUs:   binary.LittleEndian.AppendUint16(nil, uint16(len(m.vcpuFds))),
		iodev.FWCfgMaxCPUs:  binary.LittleEndian.AppendUint16(nil, uint16(len(m.vcpuFds))),
		iodev.FWCfgBootMenu: {1, 0},
	}
	var e820 bytes.Buffer
	for _, entry := range m.memoryMap() {
		if err := binary.Write(&e820, binary.LittleEndian, entry); err != nil {
			return err
		}
	}
	files := map[string][]byte{
		"etc/e820":           e820.Bytes(),
		"etc/boot-fail-wait": binary.LittleEndian.AppendUint32(nil, 0xffffffff),
	}
	acpi, err := buildUEFITables(len(m.vcpuFds), m.pci.Functions())
	if err != nil {
		return err
	}
	for name, data := range acpi {
		files[name] = data
	}
	fw, err := iodev.NewFWCfg(entries, files)
	if err != nil {
		return err
	}
	m.AddDevice(fw)
	m.initIOPortHandlers()
	// Unimplemented optional PC devices behave as absent, rather than killing
	// a firmware probe. This policy is restricted to the firmware boot mode.
	absent := func(_ uint64, data []byte) error {
		for i := range data {
			data[i] = 0xff
		}

		return nil
	}
	ignore := func(_ uint64, _ []byte) error { return nil }
	for _, r := range [][2]uint64{{0x80, 0xa0}, {0xb2, 0xb4}, {0x600, 0x608}, {0xaf00, 0xaf20}} {
		m.registerIOPortHandler(r[0], r[1], absent, ignore)
	}
	// Distribution kernels probe VMware mouse/backdoor ports even on KVM.
	// No VMware device is exposed here; return the absent-device value.
	m.registerIOPortHandler(0x5658, 0x565a, absent, ignore)
	m.registerIOPortHandler(0xb2, 0xb3, absent, power.smi)
	m.registerIOPortHandler(0xcf9, 0xcfa, absent, uefiReset)

	return nil
}

func (m *Machine) uefiMMIO(addr uint64, data []byte, write bool) (bool, error) {
	if m.uefiVars == nil {
		return false, nil
	}
	if handled, err := m.uefiVars.access(addr, data, write); handled {
		return true, err
	}
	if addr >= m.uefiCodeBase && addr-m.uefiCodeBase < uint64(len(m.uefiCode)) {
		// KVM traps writes to the immutable code image. Firmware cannot modify it.
		return true, nil
	}

	return false, nil
}

func uefiReset(_ uint64, data []byte) error {
	if len(data) > 0 && data[0]&4 != 0 {
		return ErrUEFIReset
	}

	return nil
}
