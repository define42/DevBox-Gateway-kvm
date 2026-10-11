package machine

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/bootparam"
	"github.com/define42/DevBox-Gateway-kvm/internal/guestmem"
)

func bootTestMachine(t *testing.T, size int) *Machine {
	t.Helper()
	mapping, mem, err := mapGuestMemory(size)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := syscall.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})

	return &Machine{mem: mem, vcpuFds: []uintptr{0}}
}

func testBzImage(t *testing.T, mutate func(*bootparam.SetupHeader)) []byte {
	t.Helper()
	hdr := bootparam.SetupHeader{
		SetupSects: 4, Header: bootparam.MagicSignature, Version: 0x20f,
		SysSize: 256, LoadFlags: bootparam.LoadedHigh, InitrdAddrMax: 0x7fffffff,
		KernelAlignment: 2 << 20, RelocatableKernel: 1, CmdlineSize: 2047,
		PrefAddress: 16 << 20, InitSize: 8 << 20,
	}
	if mutate != nil {
		mutate(&hdr)
	}
	var buf bytes.Buffer
	if err := binary.Write(&buf, binary.LittleEndian, hdr); err != nil {
		t.Fatal(err)
	}
	image := make([]byte, 5*512+4096)
	copy(image[0x1f1:], buf.Bytes())
	for i := 5 * 512; i < len(image); i++ {
		image[i] = byte(i)
	}

	return image
}

func bootParams(t *testing.T, m *Machine) bootparam.BootParam {
	t.Helper()
	var bp bootparam.BootParam
	if err := binary.Read(bytes.NewReader(m.mem[bootParamAddr:]), binary.LittleEndian, &bp); err != nil {
		t.Fatal(err)
	}

	return bp
}

func TestLinuxBootPlacement(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 64<<20)
	kernel := testBzImage(t, func(hdr *bootparam.SetupHeader) { hdr.SetupSects = 0 })
	initrd := bytes.Repeat([]byte{0x5a}, 2<<20)
	entry, amd64, err := m.prepareLinuxBoot(bytes.NewReader(kernel), bytes.NewReader(initrd), "console=ttyS0")
	if err != nil {
		t.Fatal(err)
	}
	bp := bootParams(t, m)
	if entry != 16<<20 || amd64 || bp.Hdr.Code32Start != uint32(entry) {
		t.Fatalf("entry %#x, 64-bit %v, code32_start %#x", entry, amd64, bp.Hdr.Code32Start)
	}
	if !bytes.Equal(m.mem[entry:entry+4096], kernel[5*512:]) {
		t.Fatal("setup_sects=0 did not use four setup sectors")
	}
	start, size := uint64(bp.Hdr.RamdiskImage), uint64(bp.Hdr.RamdiskSize)
	if start != 62<<20 || size != uint64(len(initrd)) || !bytes.Equal(m.mem[start:start+size], initrd) {
		t.Fatalf("initrd placement %#x+%#x does not preserve image", start, size)
	}
	if bp.Hdr.CmdlineSize != 2047 || string(m.mem[cmdlineAddr:cmdlineAddr+14]) != "console=ttyS0\x00" {
		t.Fatal("command line or kernel-advertised limit changed")
	}
}

func TestLinuxInitrdAddressLimitAndRuntime(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 64<<20)
	kernel := testBzImage(t, func(hdr *bootparam.SetupHeader) { hdr.InitrdAddrMax = 32<<20 - 1 })
	// 12 MiB cannot fit above the 16..24 MiB kernel runtime below 32 MiB;
	// the loader must place it in the free range below the kernel instead.
	initrd := bytes.NewReader(make([]byte, 12<<20))
	if _, _, err := m.prepareLinuxBoot(bytes.NewReader(kernel), initrd, ""); err != nil {
		t.Fatal(err)
	}
	bp := bootParams(t, m)
	end := uint64(bp.Hdr.RamdiskImage) + uint64(bp.Hdr.RamdiskSize)
	if bp.Hdr.RamdiskImage != 4<<20 || end > uint64(bp.Hdr.InitrdAddrMax)+1 {
		t.Fatalf("initrd placement exceeds address limit or kernel runtime: %#x+%#x", bp.Hdr.RamdiskImage, bp.Hdr.RamdiskSize)
	}
}

func TestLinuxVESAPlacement(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 64<<20)
	m.vesaEnabled = true
	kernel := testBzImage(t, nil)
	entry, _, err := m.prepareLinuxBoot(bytes.NewReader(kernel), nil, "")
	if err != nil {
		t.Fatal(err)
	}
	if entry != vesaFramebufferEnd {
		t.Fatalf("kernel entry %#x overlaps VESA memory", entry)
	}
}

func TestLinuxBootRejectsInvalidImages(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name    string
		mutate  func(*bootparam.SetupHeader)
		params  string
		initrd  io.ReaderAt
		wantErr string
	}{
		{name: "kernel too large", mutate: func(h *bootparam.SetupHeader) { h.InitSize = 128 << 20 }, wantErr: "runtime"},
		{name: "kernel wraps", mutate: func(h *bootparam.SetupHeader) { h.PrefAddress = ^uint64(0) }, wantErr: "runtime"},
		{name: "bad alignment", mutate: func(h *bootparam.SetupHeader) { h.KernelAlignment = 3 }, wantErr: "alignment"},
		{name: "truncated payload", mutate: func(h *bootparam.SetupHeader) { h.SysSize = 1 << 20 }, wantErr: "truncated"},
		{name: "no room for initrd", initrd: fakeSizedReader{size: 64 << 20}, wantErr: "does not fit"},
		{name: "initrd exceeds low RAM", initrd: fakeSizedReader{size: 1 << 32}, wantErr: "exceeds"},
		{
			name: "initrd read failure", initrd: fakeSizedReader{size: 4096, err: io.ErrUnexpectedEOF},
			wantErr: "unexpected EOF",
		},
		{name: "long command", params: strings.Repeat("x", 2048), wantErr: "command line"},
		{name: "NUL command", params: "quiet\x00bad", wantErr: "NUL"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			m := bootTestMachine(t, 64<<20)
			_, _, err := m.prepareLinuxBoot(bytes.NewReader(testBzImage(t, tc.mutate)), tc.initrd, tc.params)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("got %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

type fakeSizedReader struct {
	size int64
	err  error
}

func (r fakeSizedReader) Size() int64 { return r.size }
func (r fakeSizedReader) ReadAt(p []byte, off int64) (int, error) {
	if r.err != nil {
		return 0, r.err
	}

	return 0, io.EOF
}

func TestReadBootImageRejectsShortReadAndHole(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 6<<30)
	image := bootImage{reader: bytes.NewReader([]byte{1}), addr: highMemBase, size: 2, memory: 2}
	if err := m.readBootImage(image); !errors.Is(err, io.ErrUnexpectedEOF) {
		t.Fatalf("short read: %v", err)
	}
	if err := m.readBootImage(bootImage{addr: guestmem.MMIOStart - 1, memory: 2}); err == nil {
		t.Fatal("accepted image crossing into MMIO hole")
	}
	if err := m.readBootImage(bootImage{addr: ^uint64(0), memory: 2}); err == nil {
		t.Fatal("accepted image with overflowing address")
	}
}

func TestELFImagesClearBSSAndRejectHole(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 6<<30)
	program := &elf.Prog{
		ProgHeader: elf.ProgHeader{Type: elf.PT_LOAD, Paddr: guestmem.HighRAMStart, Filesz: 1, Memsz: 2},
		ReaderAt:   bytes.NewReader([]byte{42}),
	}
	file := &elf.File{FileHeader: elf.FileHeader{Machine: elf.EM_X86_64}, Progs: []*elf.Prog{program}}
	images, _, err := m.elfImages(file)
	if err != nil {
		t.Fatal(err)
	}
	m.mem[guestmem.HighRAMStart+1] = 99
	if err := m.readBootImage(images[0]); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(m.mem[guestmem.HighRAMStart:guestmem.HighRAMStart+2], []byte{42, 0}) {
		t.Fatal("ELF data or zero-fill is incorrect")
	}
	program.Paddr = guestmem.MMIOStart - 1
	if _, _, err := m.elfImages(file); err == nil {
		t.Fatal("accepted ELF segment crossing into MMIO hole")
	}
}
