package machine

import (
	"debug/elf"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/define42/DevBox-Gateway-kvm/internal/bootparam"
	"github.com/define42/DevBox-Gateway-kvm/internal/ebda"
	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
)

var errBootImage = errors.New("invalid boot image")

const maxBootCommandLine = pageTableBase - cmdlineAddr - 1

type bootRange struct {
	addr uint64
	size uint64
}

func (r bootRange) overlaps(other bootRange) bool {
	return r.size != 0 && other.size != 0 && r.addr < other.addr+other.size && other.addr < r.addr+r.size
}

type bootImage struct {
	reader io.ReaderAt
	offset int64
	addr   uint64
	size   uint64
	memory uint64
}

// readerSize keeps sized inputs (files and UKI sections) streaming without
// allocating another copy. The bounded fallback preserves the ReaderAt API.
func readerSize(r io.ReaderAt, limit uint64) (uint64, error) {
	if r == nil {
		return 0, fmt.Errorf("%w: missing image", errBootImage)
	}
	var size int64
	switch r := r.(type) {
	case interface{ Size() int64 }:
		size = r.Size()
	case interface{ Stat() (os.FileInfo, error) }:
		info, err := r.Stat()
		if err != nil {
			return 0, fmt.Errorf("image size: %w", err)
		}
		size = info.Size()
	default:
		var err error
		size, err = io.Copy(io.Discard, io.NewSectionReader(r, 0, int64(limit)+1))
		if err != nil {
			return 0, fmt.Errorf("measure image: %w", err)
		}
	}
	if size < 0 || uint64(size) > limit {
		return 0, fmt.Errorf("%w: image size %d exceeds available boot memory (%d bytes)", errBootImage, size, limit)
	}

	return uint64(size), nil
}

func (m *Machine) readBootImage(image bootImage) error {
	if image.memory < image.size || !guestmem.ValidRange(m.mem, image.addr, image.memory) {
		return fmt.Errorf("%w: image at %#x (%#x bytes) is outside guest RAM", errBootImage, image.addr, image.memory)
	}
	if image.size != 0 {
		dst := m.mem[image.addr : image.addr+image.size]
		if _, err := io.ReadFull(io.NewSectionReader(image.reader, image.offset, int64(image.size)), dst); err != nil {
			return fmt.Errorf("read image at %#x: %w", image.addr, err)
		}
	}
	clear(m.mem[image.addr+image.size : image.addr+image.memory])

	return nil
}

func validateCommandLine(params string, limit uint64) error {
	if strings.IndexByte(params, 0) >= 0 {
		return fmt.Errorf("%w: kernel command line contains a NUL byte", errBootImage)
	}
	if uint64(len(params)) > min(uint64(maxBootCommandLine), limit) {
		return fmt.Errorf("%w: kernel command line is %d bytes; maximum is %d",
			errBootImage, len(params), min(uint64(maxBootCommandLine), limit))
	}

	return nil
}

func (m *Machine) initrdImage(r io.ReaderAt, maxAddr uint64, occupied []bootRange) (bootImage, error) {
	if r == nil {
		return bootImage{}, nil
	}
	low, _ := m.ramSizes()
	limit := min(low, min(maxAddr, guestmem.HighRAMStart-1)+1)
	size, err := readerSize(r, limit)
	if err != nil {
		return bootImage{}, fmt.Errorf("initrd: %w", err)
	}
	if size == 0 {
		return bootImage{}, fmt.Errorf("%w: initrd is empty", errBootImage)
	}
	if m.vesaEnabled {
		occupied = append(occupied, bootRange{vesaFramebufferBase, vesaFramebufferReserveSize})
	}
	// Find the highest page-aligned contiguous range below initrd_addr_max.
	// The 32-bit Linux entry protocol keeps the initrd below 4 GiB.
	for size <= limit {
		addr := (limit - size) &^ uint64(4095)
		if addr < highMemBase {
			break
		}
		candidate := bootRange{addr, size}
		conflict := false
		for _, reserved := range occupied {
			if candidate.overlaps(reserved) {
				limit = min(limit, reserved.addr)
				conflict = true
			}
		}
		if !conflict {
			return bootImage{reader: r, addr: addr, size: size, memory: size}, nil
		}
	}

	return bootImage{}, fmt.Errorf("%w: initrd (%d bytes) does not fit below %#x without overlapping "+
		"the kernel or reserved memory",
		errBootImage, size, maxAddr)
}

func (m *Machine) elfImages(k *elf.File) ([]bootImage, []bootRange, error) {
	if k.Machine != elf.EM_X86_64 && k.Machine != elf.EM_386 {
		return nil, nil, fmt.Errorf("%w: unsupported ELF architecture %v", errBootImage, k.Machine)
	}
	var images []bootImage
	var occupied []bootRange
	for i, p := range k.Progs {
		if p.Type != elf.PT_LOAD || p.Memsz == 0 {
			continue
		}
		if p.Filesz > p.Memsz || p.Paddr < highMemBase || !guestmem.ValidRange(m.mem, p.Paddr, p.Memsz) {
			return nil, nil, fmt.Errorf("%w: ELF segment %d at %#x (%#x bytes) does not fit in guest RAM",
				errBootImage, i, p.Paddr, p.Memsz)
		}
		r := bootRange{p.Paddr, p.Memsz}
		if m.vesaEnabled && r.overlaps(bootRange{vesaFramebufferBase, vesaFramebufferReserveSize}) {
			return nil, nil, fmt.Errorf("%w: ELF segment %d overlaps the VESA framebuffer", errBootImage, i)
		}
		for _, previous := range occupied {
			if r.overlaps(previous) {
				return nil, nil, fmt.Errorf("%w: ELF segment %d overlaps another loadable segment", errBootImage, i)
			}
		}
		images = append(images, bootImage{reader: p, addr: p.Paddr, size: p.Filesz, memory: p.Memsz})
		occupied = append(occupied, r)
	}
	if len(images) == 0 {
		return nil, nil, ErrZeroSizeKernel
	}

	return images, occupied, nil
}

func (m *Machine) bzImage(kernel io.ReaderAt, hdr *bootparam.SetupHeader) (bootImage, []bootRange, error) {
	low, _ := m.ramSizes()
	size, err := readerSize(kernel, low)
	if err != nil {
		return bootImage{}, nil, fmt.Errorf("kernel: %w", err)
	}
	sects := uint64(hdr.SetupSects)
	if sects == 0 {
		sects = 4
	}
	setupSize := (sects + 1) * 512
	if size <= setupSize {
		return bootImage{}, nil, fmt.Errorf("kernel has no protected-mode payload: %w", ErrZeroSizeKernel)
	}
	size -= setupSize
	if uint64(hdr.SysSize)*16 > size {
		return bootImage{}, nil, fmt.Errorf("%w: kernel protected-mode payload is truncated: have %d, need %d bytes",
			errBootImage, size, uint64(hdr.SysSize)*16)
	}
	image := bootImage{reader: kernel, offset: int64(setupSize), addr: highMemBase, size: size, memory: size}
	image, occupied, err := m.kernelRuntime(image, hdr)
	if err != nil {
		return bootImage{}, nil, err
	}

	if image.addr > uint64(^uint32(0)) || !guestmem.ValidRange(m.mem, image.addr, image.memory) {
		return bootImage{}, nil, fmt.Errorf("%w: kernel does not fit in low RAM", errBootImage)
	}
	if m.vesaEnabled {
		for _, r := range occupied {
			if r.overlaps(bootRange{vesaFramebufferBase, vesaFramebufferReserveSize}) {
				return bootImage{}, nil, fmt.Errorf("%w: kernel overlaps the VESA framebuffer", errBootImage)
			}
		}
	}
	hdr.Code32Start = uint32(image.addr)

	return image, occupied, nil
}

func (m *Machine) kernelRuntime(image bootImage, hdr *bootparam.SetupHeader) (bootImage, []bootRange, error) {
	low, _ := m.ramSizes()
	occupied := []bootRange{{image.addr, image.size}}
	if hdr.Version < 0x020a || hdr.InitSize == 0 {
		return image, occupied, nil
	}
	runtimeStart := hdr.PrefAddress
	if runtimeStart == 0 {
		runtimeStart = highMemBase
	}
	if hdr.RelocatableKernel != 0 {
		alignment := uint64(hdr.KernelAlignment)
		if alignment == 0 || alignment&(alignment-1) != 0 {
			return bootImage{}, nil, fmt.Errorf("%w: invalid kernel alignment %#x", errBootImage, alignment)
		}
		runtimeStart = max(runtimeStart, uint64(highMemBase))
		runtimeWindow := bootRange{runtimeStart, max(image.size, uint64(hdr.InitSize))}
		framebuffer := bootRange{vesaFramebufferBase, vesaFramebufferReserveSize}
		if m.vesaEnabled && runtimeWindow.overlaps(framebuffer) {
			runtimeStart = max(runtimeStart, vesaFramebufferEnd)
		}
		if runtimeStart > low || alignment > low {
			return bootImage{}, nil, fmt.Errorf("%w: kernel runtime address or alignment exceeds low RAM", errBootImage)
		}
		runtimeStart = (runtimeStart + alignment - 1) &^ (alignment - 1)
		image.addr = runtimeStart
		occupied[0] = bootRange{image.addr, image.size}
	}
	runtime := bootRange{runtimeStart, uint64(hdr.InitSize)}
	if runtimeStart < highMemBase || runtimeStart > low || runtime.size > low-runtimeStart {
		return bootImage{}, nil, fmt.Errorf("%w: kernel runtime at %#x (%#x bytes) does not fit in guest RAM",
			errBootImage, runtime.addr, runtime.size)
	}
	occupied = append(occupied, runtime)

	return image, occupied, nil
}

type linuxBootPlan struct {
	params           *bootparam.BootParam
	images           []bootImage
	occupied         []bootRange
	entry            uint64
	amd64            bool
	maxInitrdAddr    uint64
	commandLineLimit uint64
}

func (m *Machine) planLinuxBoot(kernel io.ReaderAt) (linuxBootPlan, error) {
	plan := linuxBootPlan{
		params: &bootparam.BootParam{}, maxInitrdAddr: uint64(^uint32(0)),
		commandLineLimit: maxBootCommandLine,
	}
	k, err := elf.NewFile(kernel)
	if err == nil {
		plan.images, plan.occupied, err = m.elfImages(k)
		if err != nil {
			return plan, err
		}
		plan.entry, plan.amd64 = k.Entry, k.Class == elf.ELFCLASS64
		if plan.entry >= guestmem.MMIOStart || !entryInImages(plan.entry, plan.images) {
			return plan, fmt.Errorf("%w: ELF entry %#x is outside bootable low RAM", errBootImage, plan.entry)
		}

		return plan, nil
	}
	plan.params, err = bootparam.New(kernel)
	if err != nil {
		return plan, fmt.Errorf("kernel boot header: %w", err)
	}
	image, ranges, err := m.bzImage(kernel, &plan.params.Hdr)
	if err != nil {
		return plan, err
	}
	plan.images, plan.occupied, plan.entry = []bootImage{image}, ranges, image.addr
	plan.maxInitrdAddr = uint64(plan.params.Hdr.InitrdAddrMax)
	if plan.maxInitrdAddr == 0 {
		plan.maxInitrdAddr = 0x37ffffff
	}
	plan.commandLineLimit = uint64(plan.params.Hdr.CmdlineSize)
	if plan.commandLineLimit == 0 {
		plan.commandLineLimit = 255
	}

	return plan, nil
}

// prepareLinuxBoot validates and loads all images before touching any vCPU.
func (m *Machine) prepareLinuxBoot(kernel, initrd io.ReaderAt, params string) (uint64, bool, error) {
	if len(m.mem) < MinMemSize {
		return 0, false, ErrMemTooSmall
	}
	if kernel == nil {
		return 0, false, fmt.Errorf("%w: missing kernel image", errBootImage)
	}
	plan, err := m.planLinuxBoot(kernel)
	if err != nil {
		return 0, false, err
	}
	bp := plan.params

	if err := validateCommandLine(params, plan.commandLineLimit); err != nil {
		return 0, false, err
	}
	rd, err := m.initrdImage(initrd, plan.maxInitrdAddr, plan.occupied)
	if err != nil {
		return 0, false, err
	}
	for _, image := range plan.images {
		if err := m.readBootImage(image); err != nil {
			return 0, false, fmt.Errorf("kernel: %w", err)
		}
	}
	if initrd != nil {
		if err := m.readBootImage(rd); err != nil {
			return 0, false, fmt.Errorf("initrd: %w", err)
		}
	}
	for _, r := range m.memoryMap() {
		bp.AddE820Entry(r.Addr, r.Size, r.Type)
	}
	bp.Hdr.VidMode = 0xffff
	bp.Hdr.TypeOfLoader = 0xff
	bp.Hdr.RamdiskImage = uint32(rd.addr)
	bp.Hdr.RamdiskSize = uint32(rd.size)
	bp.Hdr.LoadFlags |= bootparam.CanUseHeap | bootparam.LoadedHigh | bootparam.KeepSegments
	bp.Hdr.HeapEndPtr = 0xfe00
	bp.Hdr.ExtLoaderVer = 0
	bp.Hdr.CmdlinePtr = cmdlineAddr
	// CmdlineSize is a kernel-advertised maximum, not a loader output field.
	data, err := bp.Bytes()
	if err != nil {
		return 0, false, err
	}
	copy(m.mem[bootParamAddr:], data)
	copy(m.mem[cmdlineAddr:], params)
	m.mem[cmdlineAddr+len(params)] = 0
	e, err := ebda.New(len(m.vcpuFds))
	if err != nil {
		return 0, false, err
	}
	data, err = e.Bytes()
	if err != nil {
		return 0, false, err
	}
	copy(m.mem[bootparam.EBDAStart:], data)

	return plan.entry, plan.amd64, nil
}

func entryInImages(entry uint64, images []bootImage) bool {
	for _, image := range images {
		if entry >= image.addr && entry-image.addr < image.memory {
			return true
		}
	}

	return false
}
