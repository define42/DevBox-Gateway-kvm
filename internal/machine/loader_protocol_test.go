package machine

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/bootparam"
	"github.com/define42/DevBox-Gateway-kvm/internal/pvh"
)

func TestReaderSizeUsesBoundedInputs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		reader io.ReaderAt
		limit  uint64
		want   uint64
		err    error
	}{
		{name: "missing", limit: 4, err: errBootImage},
		{name: "negative size", reader: fakeSizedReader{size: -1}, limit: 4, err: errBootImage},
		{name: "sized", reader: bytes.NewReader([]byte{1, 2, 3}), limit: 3, want: 3},
		{name: "streaming", reader: readerAtOnly{bytes.NewReader([]byte{1, 2, 3})}, limit: 3, want: 3},
		{
			name: "streaming exceeds bound", reader: readerAtOnly{bytes.NewReader(make([]byte, 5))}, limit: 3,
			err: errBootImage,
		},
		{
			name: "stream failure", reader: readerAtOnly{fakeSizedReader{err: io.ErrUnexpectedEOF}}, limit: 4,
			err: io.ErrUnexpectedEOF,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			got, err := readerSize(test.reader, test.limit)
			if got != test.want || !errors.Is(err, test.err) {
				t.Fatalf("reader size = %d, %v; want %d, %v", got, err, test.want, test.err)
			}
		})
	}
	path := filepath.Join(t.TempDir(), "image")
	if err := os.WriteFile(path, []byte{1, 2, 3}, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })
	if got, err := readerSize(file, 3); err != nil || got != 3 {
		t.Fatalf("file size = %d, %v", got, err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := readerSize(file, 3); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed file size = %v", err)
	}
}

func TestLinuxELFLoadsEntryPayloadAndBSS(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 32<<20)
	image := machineELFImage(t, 2<<20+2, nil)
	for index := 2 << 20; index < 2<<20+16; index++ {
		m.mem[index] = 0xa5
	}
	entry, amd64, err := m.prepareLinuxBoot(bytes.NewReader(image), bytes.NewReader([]byte("initrd")), "console=ttyS0")
	if err != nil || !amd64 || entry != 2<<20+2 {
		t.Fatalf("ELF boot = %#x, %t, %v", entry, amd64, err)
	}
	if !bytes.Equal(m.mem[2<<20:2<<20+16], append([]byte{1, 2, 3, 4}, make([]byte, 12)...)) {
		t.Fatal("ELF payload and zero-filled BSS differ from the program header")
	}
	bp := bootParams(t, m)
	if bp.Hdr.RamdiskSize != 6 || string(m.mem[bp.Hdr.RamdiskImage:bp.Hdr.RamdiskImage+6]) != "initrd" {
		t.Fatal("ELF boot did not preserve its initrd")
	}
	if string(m.mem[cmdlineAddr:cmdlineAddr+14]) != "console=ttyS0\x00" {
		t.Fatal("ELF boot command line was not terminated")
	}
}

func TestLinuxELFRejectsInvalidEntryAndSegments(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name  string
		entry uint64
		edit  func([]byte)
		want  error
	}{
		{name: "entry before segment", entry: 2<<20 - 1},
		{name: "entry at segment end", entry: 2<<20 + 16},
		{name: "entry in MMIO", entry: 0xd0000000},
		{name: "unsupported architecture", entry: 2 << 20, edit: func(data []byte) {
			binary.LittleEndian.PutUint16(data[18:], uint16(elf.EM_ARM))
		}},
		{name: "file larger than memory", entry: 2 << 20, edit: func(data []byte) {
			binary.LittleEndian.PutUint64(data[64+40:], 1)
		}},
		{name: "no loadable segment", entry: 2 << 20, want: ErrZeroSizeKernel, edit: func(data []byte) {
			binary.LittleEndian.PutUint32(data[64:], uint32(elf.PT_NULL))
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := bootTestMachine(t, MinMemSize)
			image := machineELFImage(t, test.entry, nil)
			if test.edit != nil {
				test.edit(image)
			}
			want := test.want
			if want == nil {
				want = errBootImage
			}
			if _, _, err := m.prepareLinuxBoot(bytes.NewReader(image), nil, ""); !errors.Is(err, want) {
				t.Fatalf("invalid ELF error = %v, want %v", err, want)
			}
		})
	}
}

func TestPVHLoadsImagesBeforeVCPUSetup(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		entry  uint64
		note   []byte
		initrd io.ReaderAt
	}{
		{name: "ELF entry fallback", entry: 2 << 20},
		{
			name: "PVH note overrides ELF entry", entry: 0, note: machinePVHNote(2<<20 + 2),
			initrd: bytes.NewReader([]byte("seed")),
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := bootTestMachine(t, MinMemSize)
			m.vcpuFds = []uintptr{^uintptr(0)} // Deterministic failure at the first vCPU ioctl, not a real KVM fd.
			image := machineELFImage(t, test.entry, test.note)
			err := m.LoadPVH(bytes.NewReader(image), test.initrd, "console=ttyS0")
			if !errors.Is(err, syscall.EBADF) {
				t.Fatalf("PVH failed before vCPU setup: %v", err)
			}
			if !bytes.Equal(m.mem[2<<20:2<<20+16], append([]byte{1, 2, 3, 4}, make([]byte, 12)...)) {
				t.Fatal("PVH did not load the ELF payload and clear BSS")
			}
			if string(m.mem[cmdlineAddr:cmdlineAddr+14]) != "console=ttyS0\x00" {
				t.Fatal("PVH did not install its command line")
			}
			if binary.LittleEndian.Uint32(m.mem[pvh.EBDAPointer:]) != bootparam.EBDAStart>>4 {
				t.Fatal("PVH EBDA pointer is invalid")
			}
			if !bytes.Equal(m.mem[pvh.BootGDTStart:pvh.BootGDTStart+32], pvh.CreateGDT().Bytes()) {
				t.Fatal("PVH did not install its GDT")
			}
			if test.initrd != nil && string(m.mem[MinMemSize-4096:MinMemSize-4096+4]) != "seed" {
				t.Fatal("PVH initrd was not loaded into the highest free aligned range")
			}
		})
	}
}

func TestPVHRejectsInvalidBootInputs(t *testing.T) {
	t.Parallel()
	for _, test := range []struct {
		name   string
		kernel []byte
		initrd io.ReaderAt
		params string
		want   string
	}{
		{name: "missing kernel", want: "missing"},
		{name: "not ELF", kernel: []byte("not ELF"), want: "EOF"},
		{name: "invalid command", kernel: []byte{1}, params: "quiet\x00bad", want: "NUL"},
		{name: "zero-size note", kernel: machineELFImage(t, 2<<20, []byte{}), want: "PT_NOTE"},
		{name: "entry outside image", kernel: machineELFImage(t, 0, nil), want: "outside"},
		{name: "empty initrd", kernel: machineELFImage(t, 2<<20, nil), initrd: bytes.NewReader(nil), want: "empty"},
		{
			name: "unreadable initrd", kernel: machineELFImage(t, 2<<20, nil),
			initrd: fakeSizedReader{size: 4, err: io.ErrUnexpectedEOF}, want: "PVH initrd",
		},
		{name: "truncated segment", kernel: machineELFImage(t, 2<<20, nil)[:4098], want: "PVH kernel"},
	} {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			m := bootTestMachine(t, MinMemSize)
			var kernel io.ReaderAt
			if test.kernel != nil {
				kernel = bytes.NewReader(test.kernel)
			}
			if err := m.LoadPVH(kernel, test.initrd, test.params); err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("PVH error = %v, want %q", err, test.want)
			}
		})
	}
}

func TestLinuxBootLegacyHeaderDefaultsAndVCPUFailure(t *testing.T) {
	t.Parallel()
	m := bootTestMachine(t, 32<<20)
	image := testBzImage(t, func(header *bootparam.SetupHeader) {
		header.InitrdAddrMax, header.CmdlineSize = 0, 0
	})
	plan, err := m.planLinuxBoot(bytes.NewReader(image))
	if err != nil || plan.maxInitrdAddr != 0x37ffffff || plan.commandLineLimit != 255 {
		t.Fatalf("legacy boot defaults = %+v, %v", plan, err)
	}
	m.vcpuFds = []uintptr{^uintptr(0)}
	if err := m.LoadLinux(bytes.NewReader(image), nil, "quiet"); !errors.Is(err, syscall.EBADF) {
		t.Fatalf("Linux vCPU setup failure = %v", err)
	}
	if bp := bootParams(t, m); bp.Hdr.Code32Start != 16<<20 {
		t.Fatal("Linux did not install its prepared boot parameters")
	}
}

type readerAtOnly struct{ io.ReaderAt }

func machinePVHNote(entry uint32) []byte {
	note := make([]byte, 20)
	binary.LittleEndian.PutUint32(note, 4)
	binary.LittleEndian.PutUint32(note[4:], 4)
	binary.LittleEndian.PutUint32(note[8:], 18)
	copy(note[12:], "Xen\x00")
	binary.LittleEndian.PutUint32(note[16:], entry)

	return note
}

func machineELFImage(t *testing.T, entry uint64, note []byte) []byte {
	t.Helper()
	programs := []elf.Prog64{{
		Type: uint32(elf.PT_LOAD), Flags: uint32(elf.PF_R | elf.PF_X),
		Off: 4096, Vaddr: 2 << 20, Paddr: 2 << 20, Filesz: 4, Memsz: 16, Align: 4096,
	}}
	size := 4100
	if note != nil {
		programs = append(programs, elf.Prog64{Type: uint32(elf.PT_NOTE), Off: 8192, Filesz: uint64(len(note)), Align: 4})
		size = 8192 + len(note)
	}
	header := elf.Header64{
		Ident: [16]byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)},
		Type:  uint16(elf.ET_EXEC), Machine: uint16(elf.EM_X86_64), Version: uint32(elf.EV_CURRENT),
		Entry: entry, Phoff: 64, Ehsize: 64, Phentsize: 56, Phnum: uint16(len(programs)),
	}
	var metadata bytes.Buffer
	if err := binary.Write(&metadata, binary.LittleEndian, header); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&metadata, binary.LittleEndian, programs); err != nil {
		t.Fatal(err)
	}
	image := make([]byte, size)
	copy(image, metadata.Bytes())
	copy(image[4096:], []byte{1, 2, 3, 4})
	if note != nil {
		copy(image[8192:], note)
	}

	return image
}
