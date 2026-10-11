package machine

import (
	"debug/elf"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"reflect"
	"runtime"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/internal/bootparam"
	"github.com/define42/DevBox-Gateway-kvm/internal/ebda"
	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
	"github.com/define42/DevBox-Gateway-kvm/internal/iodev"
	"github.com/define42/DevBox-Gateway-kvm/internal/pci"
	"github.com/define42/DevBox-Gateway-kvm/internal/pvh"
	"github.com/define42/DevBox-Gateway-kvm/internal/serial"
	"github.com/define42/DevBox-Gateway-kvm/internal/tap"
	"github.com/define42/DevBox-Gateway-kvm/internal/usernet"
	"github.com/define42/DevBox-Gateway-kvm/internal/virtio"
	"github.com/define42/DevBox-Gateway-kvm/kvm"
	"golang.org/x/arch/x86/x86asm"
)

const (
	bootParamAddr = 0x10000
	cmdlineAddr   = 0x20000

	highMemBase = 0x100000
	bootStack   = 0x80000

	serialIRQ              = 4
	ps2KeyboardIRQ         = 1
	ps2MouseIRQ            = 12
	idePrimaryIRQ          = 14
	virtioNetIRQ           = 9
	virtioBlkIRQ           = 10
	virtioGPUIRQ           = 11
	virtioInputKeyboardIRQ = 5
	virtioInputPointerIRQ  = 6
	virtioSoundIRQ         = 7

	pageTableBase = 0x30_000

	MinMemSize = 1 << 25
)

const (
	// These *could* be in kvm, but we'll see.

	// golangci-lint is completely wrong about these names.
	// Control Register Paging Enable for example:
	// golang style requires all letters in an acronym to be caps.
	// CR0 bits.
	CR0xPE = 1
	CR0xMP = (1 << 1)
	CR0xEM = (1 << 2)
	CR0xTS = (1 << 3)
	CR0xET = (1 << 4)
	CR0xNE = (1 << 5)
	CR0xWP = (1 << 16)
	CR0xAM = (1 << 18)
	CR0xNW = (1 << 29)
	CR0xCD = (1 << 30)
	CR0xPG = (1 << 31)

	// CR4 bits.
	CR4xVME        = 1
	CR4xPVI        = (1 << 1)
	CR4xTSD        = (1 << 2)
	CR4xDE         = (1 << 3)
	CR4xPSE        = (1 << 4)
	CR4xPAE        = (1 << 5)
	CR4xMCE        = (1 << 6)
	CR4xPGE        = (1 << 7)
	CR4xPCE        = (1 << 8)
	CR4xOSFXSR     = (1 << 8)
	CR4xOSXMMEXCPT = (1 << 10)
	CR4xUMIP       = (1 << 11)
	CR4xVMXE       = (1 << 13)
	CR4xSMXE       = (1 << 14)
	CR4xFSGSBASE   = (1 << 16)
	CR4xPCIDE      = (1 << 17)
	CR4xOSXSAVE    = (1 << 18)
	CR4xSMEP       = (1 << 20)
	CR4xSMAP       = (1 << 21)

	EFERxSCE = 1
	EFERxLME = (1 << 8)
	EFERxLMA = (1 << 10)
	EFERxNXE = (1 << 11)

	// 64-bit page * entry bits.
	PDE64xPRESENT  = 1
	PDE64xRW       = (1 << 1)
	PDE64xUSER     = (1 << 2)
	PDE64xACCESSED = (1 << 5)
	PDE64xDIRTY    = (1 << 6)
	PDE64xPS       = (1 << 7)
	PDE64xG        = (1 << 8)
)

const (
	// Poison is a diagnostic x86 instruction sequence. Tests and debuggers may
	// write it explicitly; New leaves guest RAM zero initialized.
	// Disassembly:
	// 0:  b8 be ba fe ca          mov    eax,0xcafebabe
	// 5:  90                      nop
	// 6:  0f 0b                   ud2
	Poison = "\xB8\xBE\xBA\xFE\xCA\x90\x0F\x0B"
)

var ErrZeroSizeKernel = errors.New("kernel is 0 bytes")

// ErrWriteToCF9 indicates a write to cf9, the standard x86 reset port.
var ErrWriteToCF9 = fmt.Errorf("power cycle via 0xcf9")

// ErrBadVA indicates a bad virtual address was used.
var ErrBadVA = fmt.Errorf("bad virtual address")

// ErrBadCPU indicates a cpu number is invalid.
var ErrBadCPU = fmt.Errorf("bad cpu number")

// ErrUnsupported indicates something we do not yet do.
var ErrUnsupported = fmt.Errorf("unsupported")

// ErrMemTooSmall indicates the requested memory size is too small.
var ErrMemTooSmall = fmt.Errorf("mem request must be at least 1<<20")

var ErrNotELF64File = fmt.Errorf("file is not ELF64")

var errPTNoteHasNoFSize = fmt.Errorf("elf programm PT_NOTE has file size equel zero")

// ErrMachineStopped is returned by RunOnce when Machine.Close
// has been called.
var ErrMachineStopped = errors.New("machine stopped")

type Machine struct {
	kvmFd, vmFd    uintptr
	kvmFile        *os.File
	vcpuFds        []uintptr
	mem            []byte
	memMapping     []byte // Original allocation, including guest RAM alignment padding.
	runs           []*kvm.RunData
	pci            *pci.PCI
	serial         *serial.Serial
	serialDisabled bool
	blockCount     uint32
	devices        []iodev.Device
	ioportHandlers [0x10000][2]func(port uint64, bytes []byte) error
	stopped        uint32
	runMu          sync.Mutex
	runWG          sync.WaitGroup
	deviceWG       sync.WaitGroup
	threads        map[int]int
	closeOnce      sync.Once
	closeErr       error
	vmCreated      bool
	runMappings    [][]byte
	vesaEnabled    bool
	uefiCode       []byte
	uefiCodeBase   uint64
	uefiVars       *uefiFlash
	uefiIRQ        bool // Set before attaching devices by EnableUEFIPlatform.
	uefiPower      *uefiPower
	pciIRQMu       sync.Mutex
	pciIRQLines    map[uint8]*sharedIRQLine
}

// New creates a new KVM. This includes opening the kvm device, creating VM, creating
// vCPUs, and attaching memory, disk (if needed), and tap (if needed).
// Guest RAM is zero initialized and demand-paged rather than eagerly filled.
func New(kvmPath string, nCpus int, memSize int) (_ *Machine, initErr error) {
	if nCpus < 1 || nCpus > ebda.MaxVCPUs {
		return nil, fmt.Errorf("cpu count %d outside 1..%d: %w", nCpus, ebda.MaxVCPUs, ErrBadCPU)
	}
	if memSize < MinMemSize {
		return nil, fmt.Errorf("memory size %d:%w", memSize, ErrMemTooSmall)
	}

	m := &Machine{}
	defer func() {
		if initErr != nil {
			initErr = errors.Join(initErr, m.Close())
		}
	}()

	m.pci = pci.New(pci.NewBridge())

	var err error

	err = m.initVMandVCPU(kvmPath, nCpus)
	if err != nil {
		return nil, err
	}

	// initCPUIDs here manually
	for cpuNr := range m.runs {
		if err := m.initCPUID(cpuNr); err != nil {
			return nil, err
		}
	}

	if m.memMapping, m.mem, err = mapGuestMemory(memSize); err != nil {
		return nil, err
	}

	if err := m.registerMemory(); err != nil {
		return nil, err
	}

	return m, nil
}

func (m *Machine) AddTapIf(tapIfName string) error {
	t, err := tap.New(tapIfName)
	if err != nil {
		return err
	}

	m.addNetwork(t)

	return nil
}

// AddUserNet attaches an unprivileged Ethernet backend with DHCP, DNS, and
// outbound connections through the host's ordinary TCP/UDP sockets.
func (m *Machine) AddUserNet() error {
	network, err := usernet.New()
	if err != nil {
		return fmt.Errorf("user-mode network: %w", err)
	}
	m.addNetwork(network)

	return nil
}

// AddIsolatedUserNet provides outbound networking without host-loopback access.
// Built-in guest DHCP/DNS remain available at the virtual gateway.
func (m *Machine) AddIsolatedUserNet() error {
	network, err := usernet.NewIsolated()
	if err != nil {
		return fmt.Errorf("isolated user-mode network: %w", err)
	}
	m.addNetwork(network)

	return nil
}

func (m *Machine) addNetwork(backend io.ReadWriter) {
	v := virtio.NewNet(virtioNetIRQ, m, backend, m.mem)
	m.connectVirtioIRQ(v.ModernTransport, virtioNetIRQ)
	m.deviceWG.Go(v.TxThreadEntry)
	m.deviceWG.Go(v.RxThreadEntry)
	// 00:01.0 for Virtio net
	m.pci.AddDevices(v)
}

func (m *Machine) AddDisk(diskPath string) error {
	v, err := virtio.NewBlk(diskPath, virtioBlkIRQ, m, m.mem)
	if err != nil {
		return err
	}

	m.addBlockDevice(v)

	return nil
}

func (m *Machine) AddReadOnlyDisk(diskPath string) error {
	v, err := virtio.NewReadOnlyBlk(diskPath, virtioBlkIRQ, m, m.mem)
	if err != nil {
		return err
	}

	m.addBlockDevice(v)

	return nil
}

// addBlockDevice reserves a distinct initial BAR for every disk. Direct Linux
// boot can disable PCI resource reassignment, so firmware cannot fix collisions.
func (m *Machine) addBlockDevice(v *virtio.Blk) {
	if m.blockCount != 0 {
		// Keep the original primary-disk BAR. Secondary disks start beyond the
		// fixed network, graphics, input and sound ranges, in 64 KiB slots.
		v.SetInitialMMIOBase(0xd010_0000 + (m.blockCount-1)*0x10000)
	}
	m.blockCount++
	m.connectVirtioIRQ(v.ModernTransport, virtioBlkIRQ)
	m.deviceWG.Go(v.IOThreadEntry)
	m.pci.AddDevices(v)
}

func (m *Machine) AddReadOnlyCDROM(r io.ReaderAt, size int64) {
	m.pci.AddDevices(pci.NewIDEController())

	for _, dev := range iodev.NewATAPICDROM(r, size, m.InjectIDEPrimaryIRQ) {
		m.AddDevice(dev)
	}
}

func (m *Machine) AddGPU(outputPath string) error {
	return m.AddGPUDisplay(virtio.NewPNGDisplay(outputPath))
}

func (m *Machine) AddGPUDisplay(display virtio.Display) error {
	v := virtio.NewGPU(virtioGPUIRQ, m, m.mem, display)
	m.connectVirtioIRQ(v.ModernTransport, virtioGPUIRQ)
	if resizable, ok := display.(interface{ SetResizeHandler(func(int, int) error) }); ok {
		resizable.SetResizeHandler(v.SetDisplaySize)
	}

	m.deviceWG.Go(v.IOThreadEntry)
	// 00:03.0 for Virtio gpu
	m.pci.AddDevices(v)

	return nil
}

// AddSound attaches a clocked virtio-snd playback device to the audio sink.
func (m *Machine) AddSound(sink virtio.SoundSink) {
	v := virtio.NewSound(virtioSoundIRQ, m.InjectVirtioSoundIRQ, m.mem, sink)
	m.connectVirtioIRQ(v.ModernTransport, virtioSoundIRQ)
	m.pci.AddDevices(v)
	m.deviceWG.Go(v.IOThreadEntry)
}

func (m *Machine) AddPS2Input() virtio.VNCInput {
	dev := iodev.NewPS2Controller(m.InjectPS2KeyboardIRQ, m.InjectPS2MouseIRQ)
	m.AddDevice(dev)

	return dev
}

// AddUEFIInput retains PS/2 firmware controls and hands guest pointer input to
// an absolute tablet when the guest driver is ready.
func (m *Machine) AddUEFIInput() virtio.VNCInput {
	legacy := m.AddPS2Input()
	pointer := virtio.NewInputTablet(virtioInputPointerIRQ, m.InjectVirtioInputPointerIRQ, m.mem)
	m.connectVirtioIRQ(pointer.ModernTransport, virtioInputPointerIRQ)
	m.pci.AddDevices(pointer)
	m.deviceWG.Go(pointer.IOThreadEntry)

	return virtio.NewFirmwareInput(legacy, pointer)
}

func (m *Machine) AddVirtioInput() virtio.VNCInput {
	keyboard := virtio.NewInputKeyboard(virtioInputKeyboardIRQ, m.InjectVirtioInputKeyboardIRQ, m.mem)
	pointer := virtio.NewInputTablet(virtioInputPointerIRQ, m.InjectVirtioInputPointerIRQ, m.mem)
	m.connectVirtioIRQ(keyboard.ModernTransport, virtioInputKeyboardIRQ)
	m.connectVirtioIRQ(pointer.ModernTransport, virtioInputPointerIRQ)

	m.deviceWG.Go(keyboard.IOThreadEntry)
	m.deviceWG.Go(pointer.IOThreadEntry)

	m.pci.AddDevices(keyboard, pointer)

	return virtio.NewInputPair(keyboard, pointer)
}

func (m *Machine) StartVGATextFallback(display virtio.ConsoleDisplay) {
	display.StartVGATextFallback(m.mem)
}

// Translate translates a virtual address for all active CPUs
// and returns a []*Translate or error.
func (m *Machine) Translate(vaddr uint64) ([]*kvm.Translation, error) {
	t := make([]*kvm.Translation, 0, len(m.vcpuFds))

	for cpu := range m.vcpuFds {
		tr := &kvm.Translation{
			LinearAddress: vaddr,
		}
		if err := kvm.Translate(m.vcpuFds[cpu], tr); err != nil {
			return t, err
		}

		t = append(t, tr)
	}

	return t, nil
}

// SetupRegs sets up the general purpose registers,
// including a RIP and BP.
func (m *Machine) SetupRegs(rip, bp uint64, amd64 bool) error {
	for _, cpu := range m.vcpuFds {
		if err := m.initRegs(cpu, rip, bp); err != nil {
			return err
		}

		if err := m.initSregs(cpu, amd64); err != nil {
			return err
		}
	}

	return nil
}

// RunData returns the kvm.RunData for the VM.
func (m *Machine) RunData() []*kvm.RunData {
	return m.runs
}

func (m *Machine) LoadPVH(kern, initrd io.ReaderAt, cmdline string) error {
	if kern == nil {
		return fmt.Errorf("%w: missing PVH kernel image", errBootImage)
	}
	if len(m.mem) < MinMemSize {
		return ErrMemTooSmall
	}
	if err := validateCommandLine(cmdline, maxBootCommandLine); err != nil {
		return err
	}
	// Set EDBA-Pointer
	edbaval := uint32(bootparam.EBDAStart >> 4)
	edbabytes := make([]byte, 4)

	// Convert EBDA-Address to bytes
	binary.LittleEndian.PutUint32(edbabytes, edbaval)

	// Copy EBDA-Address to memory at EBDAPointer-Address (0x40e)
	copy(m.mem[pvh.EBDAPointer:], edbabytes)

	// Create EBDA/mptables - Required for booting into Linux with PVH.
	e, err := ebda.New(len(m.vcpuFds))
	if err != nil {
		return err
	}

	// Convert EBDA/mptables to bytes
	eb, err := e.Bytes()
	if err != nil {
		return err
	}

	// Write EBDA/mptables to memory at EBDAStart (0x0009_FC00)
	copy(m.mem[bootparam.EBDAStart:], eb)

	// Create Global Descriptor Table
	gdt := pvh.CreateGDT()

	// Write GDT to memory
	copy(m.mem[pvh.BootGDTStart:], gdt.Bytes())

	// Write IDT to memory
	copy(m.mem[pvh.BootIDTStart:], []byte{0x0})

	// Load firmware as ELF
	fwElf, err := elf.NewFile(kern)
	if err != nil {
		// Abort if no ELF-File
		return err
	}

	images, occupied, err := m.elfImages(fwElf)
	if err != nil {
		return err
	}
	ripAddr := fwElf.Entry
	for _, entry := range fwElf.Progs {
		if entry.Type != elf.PT_NOTE {
			continue
		}
		if entry.Filesz == 0 {
			return errPTNoteHasNoFSize
		}
		if addr, err := pvh.ParsePVHEntry(kern, entry); err == nil {
			ripAddr = uint64(addr)
		}
	}
	if ripAddr >= guestmem.MMIOStart || !entryInImages(ripAddr, images) {
		return fmt.Errorf("%w: PVH entry %#x is outside bootable low RAM", errBootImage, ripAddr)
	}
	rd, err := m.initrdImage(initrd, uint64(^uint32(0)), occupied)
	if err != nil {
		return err
	}
	for _, image := range images {
		if err := m.readBootImage(image); err != nil {
			return fmt.Errorf("PVH kernel: %w", err)
		}
	}
	if initrd != nil {
		if err := m.readBootImage(rd); err != nil {
			return fmt.Errorf("PVH initrd: %w", err)
		}
	}
	copy(m.mem[cmdlineAddr:], cmdline)
	m.mem[cmdlineAddr+len(cmdline)] = 0

	for _, cpu := range m.vcpuFds {
		if err := pvh.InitRegs(cpu, ripAddr); err != nil {
			return err
		}

		if err := pvh.InitSRegs(cpu, gdt); err != nil {
			return err
		}
	}

	pvhstartinfo := pvh.NewStartInfo(bootparam.EBDAStart, cmdlineAddr)

	if initrd != nil {
		ramdiskmod := pvh.NewModListEntry(rd.addr, rd.size, 0)

		pvhstartinfo.NrModules += 1
		pvhstartinfo.ModlistPAddr = pvh.PVHModlistStart

		ramdiskmodbytes, err := ramdiskmod.Bytes()
		if err != nil {
			return err
		}

		copy(m.mem[pvh.PVHModlistStart:], ramdiskmodbytes)

		m.AddDevice(&iodev.Noop{Port: 0x80, Psize: 0x30}) // DMA Page Registers (Commonly 74L612 Chip)
	} else {
		m.AddDevice(&iodev.PostCode{}) // Port 0x80
	}

	memmapentries := make([]*pvh.HVMMemMapTableEntry, 0)
	for _, entry := range m.memoryMap() {
		memmapentries = append(memmapentries, pvh.NewMemMapTableEntry(entry.Addr, entry.Size, entry.Type))
	}

	pvhstartinfo.MemMapEntries = uint32(len(memmapentries))

	memOffset := pvh.PVHMemMapStart

	// Copy the MEMMapEntries to memory one at a time.
	for _, entry := range memmapentries {
		b, err := entry.Bytes()
		if err != nil {
			return err
		}

		copy(m.mem[memOffset:], b)

		memOffset += len(b)
	}

	// Copy the PVHInfoStart struct to memory.
	pvhstartinfob, err := pvhstartinfo.Bytes()
	if err != nil {
		return err
	}

	copy(m.mem[pvh.PVHInfoStart:], pvhstartinfob)

	if err := m.initSerial(); err != nil {
		return err
	}

	m.AddDevice(&iodev.FWDebug{}) // Port 0x402
	low, high := m.ramSizes()
	m.AddDevice(iodev.NewCMOS(low, high))
	m.AddDevice(iodev.NewACPIPMTimer())
	m.initIOPortHandlers()

	return nil
}

// LoadLinux loads a bzImage or ELF file, an optional initrd, and
// optional params.
func (m *Machine) LoadLinux(kernel, initrd io.ReaderAt, params string) error {
	entry, amd64, err := m.prepareLinuxBoot(kernel, initrd, params)
	if err != nil {
		return err
	}
	if err := m.SetupRegs(entry, bootParamAddr, amd64); err != nil {
		return err
	}
	if err := m.initSerial(); err != nil {
		return err
	}
	low, high := m.ramSizes()
	m.AddDevice(iodev.NewCMOS(low, high))
	m.AddDevice(&iodev.Noop{Port: 0x80, Psize: 0xA0})
	m.initIOPortHandlers()
	if m.vesaEnabled {
		m.installVESABIOS()
	}

	return nil
}

// DisableSerial omits the emulated UART. Call it before loading a guest.
func (m *Machine) DisableSerial() {
	m.serialDisabled = true
	m.serial = nil
}

func (m *Machine) initSerial() error {
	if m.serialDisabled {
		return nil
	}
	var err error
	m.serial, err = serial.New(m)

	return err
}

// GetInputChan returns the serial input channel, or nil when no UART is present.
func (m *Machine) GetInputChan() chan<- byte {
	if m.serial == nil {
		return nil
	}

	return m.serial.GetInputChan()
}

// GetRegs gets regs for vCPU.
func (m *Machine) GetRegs(cpu int) (*kvm.Regs, error) {
	fd, err := m.CPUToFD(cpu)
	if err != nil {
		return nil, err
	}

	return kvm.GetRegs(fd)
}

// GetSRegs gets sregs for vCPU.
func (m *Machine) GetSRegs(cpu int) (*kvm.Sregs, error) {
	fd, err := m.CPUToFD(cpu)
	if err != nil {
		return nil, err
	}

	return kvm.GetSregs(fd)
}

// SetRegs sets regs for vCPU.
func (m *Machine) SetRegs(cpu int, r *kvm.Regs) error {
	fd, err := m.CPUToFD(cpu)
	if err != nil {
		return err
	}

	return kvm.SetRegs(fd, r)
}

// SetSRegs sets sregs for vCPU.
func (m *Machine) SetSRegs(cpu int, s *kvm.Sregs) error {
	fd, err := m.CPUToFD(cpu)
	if err != nil {
		return err
	}

	return kvm.SetSregs(fd, s)
}

func (m *Machine) initRegs(vcpufd uintptr, rip, bp uint64) error {
	regs, err := kvm.GetRegs(vcpufd)
	if err != nil {
		return err
	}

	// Clear all FLAGS bits, except bit 1 which is always set.
	regs.RFLAGS = 2
	regs.RIP = rip

	regs.RAX = 0
	regs.RBX = 0
	regs.RCX = 0
	regs.RDX = 0
	regs.RDI = 0
	regs.RBP = 0
	regs.RSP = bootStack
	regs.R8 = 0
	regs.R9 = 0
	regs.R10 = 0
	regs.R11 = 0
	regs.R12 = 0
	regs.R13 = 0
	regs.R14 = 0
	regs.R15 = 0

	// Linux's 32-bit boot protocol receives the boot params address in ESI.
	regs.RSI = bp

	if err := kvm.SetRegs(vcpufd, regs); err != nil {
		return err
	}

	return nil
}

func (m *Machine) initSregs(vcpufd uintptr, amd64 bool) error {
	sregs, err := kvm.GetSregs(vcpufd)
	if err != nil {
		return err
	}

	if !amd64 {
		// set all segment flat
		sregs.CS.Base, sregs.CS.Limit, sregs.CS.G = 0, 0xFFFFFFFF, 1
		sregs.DS.Base, sregs.DS.Limit, sregs.DS.G = 0, 0xFFFFFFFF, 1
		sregs.FS.Base, sregs.FS.Limit, sregs.FS.G = 0, 0xFFFFFFFF, 1
		sregs.GS.Base, sregs.GS.Limit, sregs.GS.G = 0, 0xFFFFFFFF, 1
		sregs.ES.Base, sregs.ES.Limit, sregs.ES.G = 0, 0xFFFFFFFF, 1
		sregs.SS.Base, sregs.SS.Limit, sregs.SS.G = 0, 0xFFFFFFFF, 1

		sregs.CS.DB, sregs.SS.DB = 1, 1
		sregs.CR0 |= 1 // protected mode

		if err := kvm.SetSregs(vcpufd, sregs); err != nil {
			return err
		}

		return nil
	}

	high64k := m.mem[pageTableBase : pageTableBase+0x6000]

	// zero out the page tables.
	// but we might in fact want to poison them?
	// do we really want 1G, for example?
	for i := range high64k {
		high64k[i] = 0
	}

	// Set up page tables for long mode.
	// take the first six pages of an area it should not touch -- PageTableBase
	// present, read/write, page table at 0xffff0000
	// ptes[0] = PageTableBase + 0x1000 | 0x3
	// 3 in lowest 2 bits means present and read/write
	// 0x60 means accessed/dirty
	// 0x80 means the page size bit -- 0x80 | 0x60 = 0xe0
	// 0x10 here is making it point at the next page.
	// another go anti-pattern from golangci-lint.
	// golangci-lint claims this file has not been go-fumpt-ed
	// but it has.
	copy(high64k, []byte{
		0x03,
		0x10 | uint8((pageTableBase>>8)&0xff),
		uint8((pageTableBase >> 16) & 0xff),
		uint8((pageTableBase >> 24) & 0xff), 0, 0, 0, 0,
	})
	// need four pointers to 2M page tables -- PHYSICAL addresses:
	// 0x2000, 0x3000, 0x4000, 0x5000
	// experiment: set PS bit
	// Don't.
	for i := uint64(0); i < 4; i++ {
		ptb := pageTableBase + (i+2)*0x1000
		// Another coding anti-pattern
		copy(high64k[int(i*8)+0x1000:],
			[]byte{
				/*0x80 |*/ 0x63,
				uint8((ptb >> 8) & 0xff),
				uint8((ptb >> 16) & 0xff),
				uint8((ptb >> 24) & 0xff), 0, 0, 0, 0,
			})
	}
	// Now the 2M pages.
	for i := uint64(0); i < 0x1_0000_0000; i += 0x2_00_000 {
		ptb := i | 0xe3
		ix := int((i/0x2_00_000)*8 + 0x2000)
		// another coding anti-pattern from golangci-lint.
		copy(high64k[ix:], []byte{
			uint8(ptb),
			uint8((ptb >> 8) & 0xff),
			uint8((ptb >> 16) & 0xff),
			uint8((ptb >> 24) & 0xff), 0, 0, 0, 0,
		})
	}

	// set to true to debug.
	if false {
		log.Printf("Page tables: %s", hex.Dump(m.mem[pageTableBase:pageTableBase+0x3000]))
	}

	sregs.CR3 = uint64(pageTableBase)
	sregs.CR4 = CR4xPAE
	sregs.CR0 = CR0xPE | CR0xMP | CR0xET | CR0xNE | CR0xWP | CR0xAM | CR0xPG
	sregs.EFER = EFERxLME | EFERxLMA

	seg := kvm.Segment{
		Base:     0,
		Limit:    0xffffffff,
		Selector: 1 << 3,
		Typ:      11, /* Code: execute, read, accessed */
		Present:  1,
		DPL:      0,
		DB:       0,
		S:        1, /* Code/data */
		L:        1,
		G:        1, /* 4KB granularity */
		AVL:      0,
	}

	sregs.CS = seg

	seg.Typ = 3 /* Data: read/write, accessed */
	seg.Selector = 2 << 3
	sregs.DS, sregs.ES, sregs.FS, sregs.GS, sregs.SS = seg, seg, seg, seg, seg

	if err := kvm.SetSregs(vcpufd, sregs); err != nil {
		return err
	}

	return nil
}

// SingleStep enables single stepping the guest.
func (m *Machine) SingleStep(onoff bool) error {
	for cpu := range m.vcpuFds {
		if err := kvm.SingleStep(m.vcpuFds[cpu], onoff); err != nil {
			return fmt.Errorf("single step %d:%w", cpu, err)
		}
	}

	return nil
}

// RunInfiniteLoop runs the guest cpu until there is an error.
// If the error is ErrExitDebug, this function can be called again.
func (m *Machine) RunInfiniteLoop(cpu int) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fd, err := m.beginRun(cpu)
	if err != nil {
		return err
	}
	defer m.endRun(cpu)

	return m.runInfiniteLoop(cpu, fd)
}

func (m *Machine) runInfiniteLoop(cpu int, fd uintptr) error {
	for {
		again, err := m.runOnce(cpu, fd)
		if err != nil || !again {
			return err
		}
	}
}

// RunOnce runs the guest vCPU until it exits.
func (m *Machine) RunOnce(cpu int) (bool, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fd, err := m.beginRun(cpu)
	if err != nil {
		return false, err
	}
	defer m.endRun(cpu)

	return m.runOnce(cpu, fd)
}

func (m *Machine) runOnce(cpu int, fd uintptr) (bool, error) {
	if atomic.LoadUint32(&m.stopped) != 0 {
		return false, ErrMachineStopped
	}
	err := kvm.Run(fd)
	if atomic.LoadUint32(&m.stopped) != 0 {
		return false, ErrMachineStopped
	}
	// An interrupted/retryable run has no valid exit reason. Do not replay the
	// previous I/O exit; return to the loop so cancellation is checked again.
	if errors.Is(err, syscall.EINTR) || errors.Is(err, syscall.EAGAIN) {
		return true, nil
	}
	if err != nil {
		return false, fmt.Errorf("run vCPU %d: %w", cpu, err)
	}

	exit := kvm.ExitType(m.runs[cpu].ExitReason)

	switch exit {
	case kvm.EXITHLT:
		return false, err
	case kvm.EXITIO:
		direction, size, port, count, offset := m.runs[cpu].IO()
		f := m.ioportHandlers[port][direction]
		if os.Getenv("GOKVM_TRACE_IO") != "" {
			dir := "out"
			if direction == kvm.EXITIOIN {
				dir = "in"
			}
			log.Printf("io %s port=%#x size=%d count=%d", dir, port, size, count)
		}

		ioBytes := unsafe.Slice(
			(*byte)(unsafe.Pointer(uintptr(unsafe.Pointer(m.runs[cpu]))+uintptr(offset))),
			int(size)*int(count),
		)
		for i := 0; i < int(count); i++ {
			bytes := ioBytes[i*int(size) : (i+1)*int(size)]
			if err := f(port, bytes); err != nil {
				return false, err
			}
		}

		return true, err
	case kvm.EXITMMIO:
		physAddr, data, isWrite := m.runs[cpu].MMIO()
		if handled, err := m.uefiMMIO(physAddr, data, isWrite); handled {
			return err == nil, err
		}

		if dev, offset, ok := m.pci.LookupMMIO(physAddr); ok {
			dev.MMIO(offset, data, isWrite)
		} else if os.Getenv("GOKVM_TRACE_MMIO") != "" {
			log.Printf("unhandled MMIO @%#x len=%d write=%v",
				physAddr, len(data), isWrite)
		}

		return true, err
	case kvm.EXITINTR:
		// When a signal is sent to the thread hosting the VM it will result in EINTR
		// refs https://gist.github.com/mcastelino/df7e65ade874f6890f618dc51778d83a
		return true, nil
	case kvm.EXITDEBUG:
		return false, kvm.ErrDebug

	case kvm.EXITUNKNOWN,
		kvm.EXITDCR,
		kvm.EXITEXCEPTION,
		kvm.EXITFAILENTRY,
		kvm.EXITHYPERCALL,
		kvm.EXITINTERNALERROR,
		kvm.EXITIRQWINDOWOPEN,
		kvm.EXITNMI,
		kvm.EXITS390RESET,
		kvm.EXITS390SIEIC,
		kvm.EXITSETTPR,
		kvm.EXITSHUTDOWN,
		kvm.EXITTPRACCESS:
		if err != nil {
			return false, err
		}

		return false, fmt.Errorf("%w: %s", kvm.ErrUnexpectedExitReason, exit.String())
	default:
		if err != nil {
			return false, err
		}

		r, _ := m.GetRegs(cpu)
		s, _ := m.GetSRegs(cpu)
		// another coding anti-pattern from golangci-lint.
		return false, fmt.Errorf("%w: %v: regs:\n%s",
			kvm.ErrUnexpectedExitReason,
			kvm.ExitType(m.runs[cpu].ExitReason).String(), show("", &s, &r))
	}
}

func (m *Machine) registerIOPortHandler(
	start, end uint64,
	inHandler, outHandler func(port uint64, bytes []byte) error,
) {
	for i := start; i < end; i++ {
		m.ioportHandlers[i][kvm.EXITIOIN] = inHandler
		m.ioportHandlers[i][kvm.EXITIOOUT] = outHandler
	}
}

func (m *Machine) initIOPortHandlers() {
	funcNone := func(port uint64, bytes []byte) error {
		return nil
	}

	funcInAbsent := func(port uint64, bytes []byte) error {
		for i := range bytes {
			bytes[i] = 0xff
		}

		return nil
	}

	funcError := func(port uint64, bytes []byte) error {
		return fmt.Errorf("%w: unexpected io port 0x%x", kvm.ErrUnexpectedExitReason, port)
	}

	// 0xCF9 port can get three values for three types of reset:
	//
	// Writing 4 to 0xCF9:(INIT) Will INIT the CPU. Meaning it will jump
	// to the initial location of booting but it will keep many CPU
	// elements untouched. Most internal tables, chaches etc will remain
	// unchanged by the Init call (but may change during it).
	//
	// Writing 6 to 0xCF9:(RESET) Will RESET the CPU with all
	// internal tables caches etc cleared to initial state.
	//
	// Writing 0xE to 0xCF9:(RESTART) Will power cycle the mother board
	// with everything that comes with it.
	// For now, we will exit without regard to the value. Should we wish
	// to have more sophisticated cf9 handling, we will need to modify
	// gokvm a bit more.
	funcOutbCF9 := func(port uint64, bytes []byte) error {
		if len(bytes) == 1 && bytes[0] == 0xe {
			return fmt.Errorf("write 0xe to cf9: %w", ErrWriteToCF9)
		}

		return fmt.Errorf("write %#x to cf9: %w", bytes, ErrWriteToCF9)
	}

	// In ubuntu 20.04 on wsl2, the output to IO port 0x64 continued
	// infinitely. To deal with this issue, refer to kvmtool and
	// configure the input to the Status Register of the PS2 controller.
	//
	// refs:
	// https://github.com/kvmtool/kvmtool/blob/0e1882a49f81cb15d328ef83a78849c0ea26eecc/hw/i8042.c#L312
	// https://git.kernel.org/pub/scm/linux/kernel/git/will/kvmtool.git/tree/hw/i8042.c#n312
	// https://wiki.osdev.org/%228042%22_PS/2_Controller
	funcInbPS2 := func(port uint64, bytes []byte) error {
		bytes[0] = 0x20

		return nil
	}

	m.registerIOPortHandler(0, 0x10000, funcError, funcError)    // default handler
	m.registerIOPortHandler(0x00, 0x10, funcNone, funcNone)      // 8237 DMA controller 1
	m.registerIOPortHandler(0xc0, 0xe0, funcNone, funcNone)      // 8237 DMA controller 2
	m.registerIOPortHandler(0xcf9, 0xcfa, funcNone, funcOutbCF9) // CF9
	// Built-in ISA drivers also probe below the ATA range (for example,
	// advansys reads its signature at 0x121). An absent device must return
	// all ones so probing can continue without terminating the vCPU.
	m.registerIOPortHandler(0x120, 0x3f8, funcInAbsent, funcNone) // Absent legacy ISA/gameport/PNP/ATA ports
	m.registerIOPortHandler(0x3c0, 0x3db, funcNone, funcNone)     // VGA
	m.registerIOPortHandler(0x3b4, 0x3b6, funcNone, funcNone)     // VGA
	m.registerIOPortHandler(0xcfe, 0xcff, funcNone, funcNone)     // unknown
	m.registerIOPortHandler(0xcfa, 0xcfc, funcNone, funcNone)     // unknown
	m.registerIOPortHandler(0xc000, 0xd000, funcNone, funcNone)   // PCI Configuration Space Access Mechanism #2
	m.registerIOPortHandler(0x279, 0x27a, funcNone, funcNone)     // ISA Plug and Play address port
	m.registerIOPortHandler(0xa79, 0xa7a, funcNone, funcNone)     // ISA Plug and Play write-data port
	m.registerIOPortHandler(0x60, 0x70, funcInbPS2, funcNone)     // PS/2 Keyboard (Always 8042 Chip)
	m.registerIOPortHandler(0xed, 0xee, funcNone, funcNone)       // 0xed is the new standard delay port.

	// Serial port 1 behaves as absent when no UART is attached.
	if m.serial == nil {
		m.registerIOPortHandler(serial.COM1Addr, serial.COM1Addr+8, funcInAbsent, funcNone)
	} else {
		m.registerIOPortHandler(serial.COM1Addr, serial.COM1Addr+8, m.serial.In, m.serial.Out)
	}

	// PCI configuration
	//
	// 0xcf8 for address register for PCI Config Space
	// 0xcfc + 0xcff for data for PCI Config Space
	// see https://github.com/torvalds/linux/blob/master/arch/x86/pci/direct.c for more detail.
	m.registerIOPortHandler(0xcf8, 0xcf9, m.pci.PciConfAddrIn, m.pci.PciConfAddrOut)
	m.registerIOPortHandler(0xcfc, 0xd00, m.pci.PciConfDataIn, m.pci.PciConfDataOut)

	// IO Devices - non PCI
	for _, dev := range m.devices {
		m.registerIOPortHandler(dev.IOPort(), dev.IOPort()+dev.Size(), dev.Read, dev.Write)
	}

	// PCI devices
	for _, dev := range m.pci.Devices() {
		m.registerIOPortHandler(dev.IOPort(), dev.IOPort()+dev.Size(), dev.Read, dev.Write)
	}
}

// InjectSerialIRQ injects a serial interrupt, or does nothing without a UART.
func (m *Machine) InjectSerialIRQ() error {
	if m.serial == nil {
		return nil
	}
	if err := kvm.IRQLineStatus(m.vmFd, serialIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, serialIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectPS2KeyboardIRQ injects a keyboard controller interrupt.
func (m *Machine) InjectPS2KeyboardIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, ps2KeyboardIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, ps2KeyboardIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectPS2MouseIRQ injects a mouse controller interrupt.
func (m *Machine) InjectPS2MouseIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, ps2MouseIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, ps2MouseIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectIDEPrimaryIRQ injects a primary IDE channel interrupt.
func (m *Machine) InjectIDEPrimaryIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, idePrimaryIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, idePrimaryIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectViortNetIRQ injects a virtio net interrupt.
func (m *Machine) InjectVirtioNetIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, virtioNetIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, virtioNetIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectViortNetIRQ injects a virtio block interrupt.
func (m *Machine) InjectVirtioBlkIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, virtioBlkIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, virtioBlkIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectVirtioGPUIRQ injects a virtio gpu interrupt.
func (m *Machine) InjectVirtioGPUIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, virtioGPUIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, virtioGPUIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectVirtioSoundIRQ injects a virtio-snd interrupt.
func (m *Machine) InjectVirtioSoundIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, virtioSoundIRQ, 0); err != nil {
		return err
	}

	return kvm.IRQLineStatus(m.vmFd, virtioSoundIRQ, 1)
}

// InjectVirtioInputKeyboardIRQ injects a virtio-input keyboard interrupt.
func (m *Machine) InjectVirtioInputKeyboardIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, virtioInputKeyboardIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, virtioInputKeyboardIRQ, 1); err != nil {
		return err
	}

	return nil
}

// InjectVirtioInputPointerIRQ injects a virtio-input pointer interrupt.
func (m *Machine) InjectVirtioInputPointerIRQ() error {
	if err := kvm.IRQLineStatus(m.vmFd, virtioInputPointerIRQ, 0); err != nil {
		return err
	}

	if err := kvm.IRQLineStatus(m.vmFd, virtioInputPointerIRQ, 1); err != nil {
		return err
	}

	return nil
}

// ReadAt implements io.ReadAt for the kvm guest pvh.
func (m *Machine) ReadAt(b []byte, off int64) (int, error) {
	if off < 0 || !guestmem.ValidRange(m.mem, uint64(off), uint64(len(b))) {
		return 0, io.EOF
	}

	return copy(b, m.mem[off:]), nil
}

// WriteAt implements io.WriterAt for guest RAM, excluding MMIO reservations.
func (m *Machine) WriteAt(b []byte, off int64) (int, error) {
	if off < 0 || !guestmem.ValidRange(m.mem, uint64(off), uint64(len(b))) {
		return 0, syscall.EFBIG
	}

	return copy(m.mem[off:], b), nil
}

func showone(indent string, in interface{}) string {
	var ret string

	s := reflect.ValueOf(in).Elem()
	typeOfT := s.Type()

	for i := 0; i < s.NumField(); i++ {
		f := s.Field(i)
		if f.Kind() == reflect.String {
			ret += fmt.Sprintf(indent+"%s %s = %s\n", typeOfT.Field(i).Name, f.Type(), f.Interface())
		} else {
			ret += fmt.Sprintf(indent+"%s %s = %#x\n", typeOfT.Field(i).Name, f.Type(), f.Interface())
		}
	}

	return ret
}

func show(indent string, l ...interface{}) string {
	var ret string
	for _, i := range l {
		ret += showone(indent, i)
	}

	return ret
}

// CPUToFD translates a CPU number to an fd.
func (m *Machine) CPUToFD(cpu int) (uintptr, error) {
	if cpu < 0 || cpu >= len(m.vcpuFds) {
		return 0, fmt.Errorf("cpu %d outside [0, %d): %w", cpu, len(m.vcpuFds), ErrBadCPU)
	}

	return m.vcpuFds[cpu], nil
}

// VtoP returns the physical address for a vCPU virtual address.
func (m *Machine) VtoP(cpu int, vaddr uint64) (int64, error) {
	fd, err := m.CPUToFD(cpu)
	if err != nil {
		return 0, err
	}

	t := &kvm.Translation{
		LinearAddress: vaddr,
	}
	if err := kvm.Translate(fd, t); err != nil {
		return -1, err
	}

	// There can exist a valid translation for memory that does not exist.
	// For now, we call that an error.
	if t.Valid == 0 || !guestmem.ValidRange(m.mem, t.PhysicalAddress, 1) {
		return -1, fmt.Errorf("%#x:valid not set:%w", vaddr, ErrBadVA)
	}

	return int64(t.PhysicalAddress), nil
}

// GetReg gets a pointer to a register in kvm.Regs, given
// a register number from reg. This used to be a comprehensive
// case, but golangci-lint disliked the cyclomatic complexity
// So we only show the few registers we support.
func GetReg(r *kvm.Regs, reg x86asm.Reg) (*uint64, error) {
	if reg == x86asm.RAX {
		return &r.RAX, nil
	}

	if reg == x86asm.RCX {
		return &r.RCX, nil
	}

	if reg == x86asm.RDX {
		return &r.RDX, nil
	}

	if reg == x86asm.RBX {
		return &r.RBX, nil
	}

	if reg == x86asm.RSP {
		return &r.RSP, nil
	}

	if reg == x86asm.RBP {
		return &r.RBP, nil
	}

	if reg == x86asm.RSI {
		return &r.RSI, nil
	}

	if reg == x86asm.RDI {
		return &r.RDI, nil
	}

	if reg == x86asm.R8 {
		return &r.R8, nil
	}

	if reg == x86asm.R9 {
		return &r.R9, nil
	}

	if reg == x86asm.R10 {
		return &r.R10, nil
	}

	if reg == x86asm.R11 {
		return &r.R11, nil
	}

	if reg == x86asm.R12 {
		return &r.R12, nil
	}

	if reg == x86asm.R13 {
		return &r.R13, nil
	}

	if reg == x86asm.R14 {
		return &r.R14, nil
	}

	if reg == x86asm.R15 {
		return &r.R15, nil
	}

	if reg == x86asm.RIP {
		return &r.RIP, nil
	}

	return nil, fmt.Errorf("register %v%w", reg, ErrUnsupported)
}

// initVMandVCPU records each resource immediately, so New can release partial
// initialization through the same Close path used after a successful run.
func (m *Machine) initVMandVCPU(kvmPath string, nCpus int) error {
	var err error
	m.kvmFile, err = os.OpenFile(kvmPath, os.O_RDWR, 0)
	if err != nil {
		return err
	}
	m.kvmFd = m.kvmFile.Fd()
	supported, err := kvm.CheckExtension(m.kvmFd, kvm.CapImmediateExit)
	if err != nil {
		return err
	}
	if supported == 0 {
		return fmt.Errorf("KVM immediate exit: %w", ErrUnsupported)
	}
	m.vmFd, err = kvm.CreateVM(m.kvmFd)
	if err != nil {
		return fmt.Errorf("create VM: %w", err)
	}
	m.vmCreated = true
	if err := kvm.SetTSSAddr(m.vmFd, pvh.KVMTSSStart); err != nil {
		return err
	}
	if err := kvm.SetIdentityMapAddr(m.vmFd, pvh.KVMIdentityMapStart); err != nil {
		return err
	}
	if err := kvm.CreateIRQChip(m.vmFd); err != nil {
		return err
	}
	if err := kvm.CreatePIT2(m.vmFd); err != nil {
		return err
	}
	mmapSize, err := kvm.GetVCPUMMmapSize(m.kvmFd)
	if err != nil {
		return err
	}
	for cpu := range nCpus {
		fd, err := kvm.CreateVCPU(m.vmFd, cpu)
		if err != nil {
			return err
		}
		m.vcpuFds = append(m.vcpuFds, fd)
		mapping, err := syscall.Mmap(int(fd), 0, int(mmapSize), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
		if err != nil {
			return err
		}
		m.runMappings = append(m.runMappings, mapping)
		m.runs = append(m.runs, (*kvm.RunData)(unsafe.Pointer(&mapping[0])))
	}

	return nil
}

func (m *Machine) VCPU(stdout io.Writer, cpu, traceCount int) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	fd, err := m.beginRun(cpu)
	if err != nil {
		return err
	}
	defer m.endRun(cpu)
	trace := traceCount > 0

	// Consider ANOTHER option, maxInsCount, which would
	// exit this loop after a certain number of instructions
	// were run.
	for tc := 0; ; tc++ {
		err = m.runInfiniteLoop(cpu, fd)
		if err == nil {
			return nil
		}

		if !trace || !errors.Is(err, kvm.ErrDebug) {
			return fmt.Errorf("CPU %d: %w", cpu, err)
		}

		if err := m.SingleStep(trace); err != nil {
			fmt.Fprintf(stdout, "Setting trace to %v:%v", trace, err)
		}

		if tc%traceCount != 0 {
			continue
		}

		_, r, s, err := m.Inst(cpu)
		if err != nil {
			fmt.Fprintf(stdout, "disassembling after debug exit:%v", err)
		} else {
			fmt.Fprintf(stdout, "%#x:%s\r\n", r.RIP, s)
		}
	}
}

func (m *Machine) GetSerial() *serial.Serial {
	return m.serial
}

func (m *Machine) AddDevice(dev iodev.Device) {
	m.devices = append(m.devices, dev)
}
