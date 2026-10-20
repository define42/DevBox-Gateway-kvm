package pvh_test

import (
	"bytes"
	"debug/elf"
	"encoding/binary"
	"errors"
	"io"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/internal/pvh"
)

func TestStartInfoWireLayout(t *testing.T) {
	t.Parallel()
	info := pvh.NewStartInfo(0x12345678, 0x98765432)
	info.NrModules = 1
	info.ModlistPAddr = pvh.PVHModlistStart
	info.MemMapEntries = 2
	got, err := info.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x78, 0xc5, 0x6e, 0x33, 1, 0, 0, 0, // magic, version
		0, 0, 0, 0, 1, 0, 0, 0, // flags, number of modules
		0x40, 0x60, 0, 0, 0, 0, 0, 0, // module list address
		0x32, 0x54, 0x76, 0x98, 0, 0, 0, 0, // command line address
		0x78, 0x56, 0x34, 0x12, 0, 0, 0, 0, // RSDP address
		0, 0x70, 0, 0, 0, 0, 0, 0, // memory map address
		2, 0, 0, 0, 0, 0, 0, 0, // memory map entries, reserved
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("start info = %x, want %x", got, want)
	}
}

func TestModListWireLayout(t *testing.T) {
	t.Parallel()
	entry := pvh.NewModListEntry(0x123456789a, 0x87654321, 0xfedcba98)
	got, err := entry.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0x9a, 0x78, 0x56, 0x34, 0x12, 0, 0, 0,
		0x21, 0x43, 0x65, 0x87, 0, 0, 0, 0,
		0x98, 0xba, 0xdc, 0xfe, 0, 0, 0, 0,
		0, 0, 0, 0, 0, 0, 0, 0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("module entry = %x, want %x", got, want)
	}
}

func TestMemMapWireLayout(t *testing.T) {
	t.Parallel()
	entry := pvh.NewMemMapTableEntry(0x100000000, 0x40000000, 1)
	got, err := entry.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	want := []byte{
		0, 0, 0, 0, 1, 0, 0, 0,
		0, 0, 0, 0x40, 0, 0, 0, 0,
		1, 0, 0, 0, 0, 0, 0, 0,
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("memory map = %x, want %x", got, want)
	}
}

func TestGDTWireLayout(t *testing.T) {
	t.Parallel()
	gdt := pvh.CreateGDT()
	got := gdt.Bytes()
	if len(got) != 32 {
		t.Fatalf("GDT length = %d, want 32", len(got))
	}
	for i, entry := range gdt {
		if decoded := binary.LittleEndian.Uint64(got[i*8:]); decoded != entry {
			t.Fatalf("GDT[%d] = %#x, want %#x", i, decoded, entry)
		}
	}
}

func TestParsePVHEntry(t *testing.T) {
	t.Parallel()
	valid := pvhNote("Xen\x00", 18, []byte{0x78, 0x56, 0x34, 0x12})
	for _, tc := range []struct {
		name string
		note []byte
		fail bool
	}{
		{name: "entry", note: valid},
		{name: "skip_other_note_with_padding", note: append(pvhNote("AB\x00", 1, []byte{1, 2, 3}), valid...)},
		{name: "skip_other_vendor", note: append(pvhNote("XYZ\x00", 18, []byte{1, 2, 3, 4}), valid...)},
		{name: "empty", fail: true},
		{name: "missing", note: pvhNote("GNU\x00", 3, []byte{1, 2, 3, 4}), fail: true},
		{name: "short_header", note: valid[:8], fail: true},
		{name: "short_name", note: valid[:14], fail: true},
		{name: "short_address", note: valid[:18], fail: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			// Put notes at a nonzero file offset to catch segment-relative addressing errors.
			data := append(make([]byte, 32), tc.note...)
			prog := &elf.Prog{ProgHeader: elf.ProgHeader{Off: 32, Filesz: uint64(len(tc.note))}}
			entry, err := pvh.ParsePVHEntry(bytes.NewReader(data), prog)
			if tc.fail {
				if err == nil {
					t.Fatalf("invalid note returned entry %#x", entry)
				}

				return
			}
			if err != nil || entry != 0x12345678 {
				t.Fatalf("entry = %#x, %v; want %#x", entry, err, 0x12345678)
			}
		})
	}
}

func TestCheckPVH(t *testing.T) {
	t.Parallel()
	valid := pvhNote("Xen\x00", 18, []byte{0, 0, 0x10, 0})
	for _, tc := range []struct {
		name string
		note []byte
		want bool
		err  error
	}{
		{name: "entry", note: valid, want: true},
		{name: "after_padded_note", note: append(pvhNote("AB\x00", 1, []byte{1}), valid...), want: true},
		{name: "other_vendor", note: pvhNote("XYZ\x00", 18, []byte{0, 0, 0, 0})},
		{name: "no_notes"},
		{name: "truncated_header", note: valid[:8], err: io.EOF},
		{name: "truncated_vendor", note: valid[:14], err: io.EOF},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			image := pvhELF(t, tc.note)
			got, err := pvh.CheckPVH(bytes.NewReader(image))
			if got != tc.want || !errors.Is(err, tc.err) {
				t.Fatalf("CheckPVH = %t, %v; want %t, %v", got, err, tc.want, tc.err)
			}
		})
	}
	if got, err := pvh.CheckPVH(bytes.NewReader([]byte("not an ELF image"))); got || err != nil {
		t.Fatalf("non-ELF image = %t, %v; want false, nil", got, err)
	}
}

func pvhNote(name string, typ uint32, desc []byte) []byte {
	nameSize := (len(name) + 3) &^ 3
	descSize := (len(desc) + 3) &^ 3
	note := make([]byte, 12+nameSize+descSize)
	binary.LittleEndian.PutUint32(note, uint32(len(name)))
	binary.LittleEndian.PutUint32(note[4:], uint32(len(desc)))
	binary.LittleEndian.PutUint32(note[8:], typ)
	copy(note[12:], name)
	copy(note[12+nameSize:], desc)

	return note
}

func pvhELF(t *testing.T, note []byte) []byte {
	t.Helper()
	var image bytes.Buffer
	header := elf.Header64{
		Ident: [16]byte{0x7f, 'E', 'L', 'F', byte(elf.ELFCLASS64), byte(elf.ELFDATA2LSB), byte(elf.EV_CURRENT)},
		Type:  uint16(elf.ET_EXEC), Machine: uint16(elf.EM_X86_64), Version: uint32(elf.EV_CURRENT),
		Phoff: 64, Ehsize: 64, Phentsize: 56, Phnum: 1,
	}
	prog := elf.Prog64{Type: uint32(elf.PT_NOTE), Off: 120, Filesz: uint64(len(note)), Align: 4}
	if err := binary.Write(&image, binary.LittleEndian, header); err != nil {
		t.Fatal(err)
	}
	if err := binary.Write(&image, binary.LittleEndian, prog); err != nil {
		t.Fatal(err)
	}
	image.Write(note)

	return image.Bytes()
}
