package uki_test

import (
	"bytes"
	"debug/pe"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/define42/gokvm/internal/uki"
)

const (
	peOffset      = 0x80
	optionalStart = peOffset + 24
	sectionTable  = optionalStart + 240
)

type fixtureSection struct {
	name string
	data []byte
}

func TestOpenNetDeskLayout(t *testing.T) {
	t.Parallel()

	sections := netDeskSections()
	data := makePE(sections)
	// Check that the fixture has a real PE32+ section-table layout understood
	// independently by the standard library, including file-alignment padding.
	peFile, err := pe.NewFile(bytes.NewReader(data))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := peFile.Close(); err != nil {
			t.Error(err)
		}
	})
	if peFile.Section(".linux").Size <= peFile.Section(".linux").VirtualSize {
		t.Fatal("fixture must include raw padding after the Linux payload")
	}

	image, err := uki.Open(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		reader *io.SectionReader
		want   []byte
	}{
		{name: "kernel", reader: image.Kernel, want: sections[3].data},
		{name: "initrd", reader: image.Initrd, want: sections[4].data},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			got, err := io.ReadAll(tc.reader)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(got, tc.want) {
				t.Fatalf("payload differs: got %d bytes, want %d", len(got), len(tc.want))
			}
			if tc.reader.Size() != int64(len(tc.want)) {
				t.Fatalf("reader size = %d, want %d", tc.reader.Size(), len(tc.want))
			}
		})
	}
	if want := "rdinit=/init console=tty0 console=ttyS0,115200n8 loglevel=4"; image.Cmdline != want {
		t.Fatalf("Cmdline = %q, want %q", image.Cmdline, want)
	}
}

func TestOpenCmdline(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		data    []byte
		missing bool
		want    string
		wantErr string
	}{
		{name: "missing", missing: true},
		{name: "empty"},
		{name: "unterminated", data: []byte("rdinit=/init"), want: "rdinit=/init"},
		{name: "trailing padding", data: []byte("a=1\r\n\x00\x00"), want: "a=1"},
		{name: "quoted spaces", data: []byte(`key="two words" other=1`), want: `key="two words" other=1`},
		{name: "embedded NUL", data: []byte("a=1\x00b=2"), wantErr: "embedded NUL"},
		{name: "invalid UTF-8", data: []byte{0xff}, wantErr: "UTF-8"},
		{name: "too large", data: bytes.Repeat([]byte{'x'}, 64*1024+1), wantErr: "64 KiB"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			sections := netDeskSections()
			if tc.missing {
				sections = sections[:len(sections)-1]
			} else {
				sections[len(sections)-1].data = tc.data
			}
			data := makePE(sections)
			image, err := uki.Open(bytes.NewReader(data), int64(len(data)))
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("Open() error = %v, want %q", err, tc.wantErr)
				}

				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if image.Cmdline != tc.want {
				t.Fatalf("Cmdline = %q, want %q", image.Cmdline, tc.want)
			}
		})
	}
}

func TestOpenMalformed(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name    string
		mutate  func([]byte)
		wantErr string
	}{
		{
			name: "DOS signature", wantErr: "DOS signature",
			mutate: func(b []byte) { b[0] = 0 },
		},
		{
			name: "PE signature", wantErr: "PE signature",
			mutate: func(b []byte) { b[peOffset] = 0 },
		},
		{
			name: "PE offset in DOS header", wantErr: "overlaps DOS",
			mutate: func(b []byte) { put32(b, 60, 0) },
		},
		{
			name: "PE offset out of file", wantErr: "PE header",
			mutate: func(b []byte) { put32(b, 60, 0xffffffff) },
		},
		{
			name: "ARM64", wantErr: "need x86-64",
			mutate: func(b []byte) { put16(b, peOffset+4, 0xaa64) },
		},
		{
			name: "PE32", wantErr: "PE32+",
			mutate: func(b []byte) { put16(b, optionalStart, 0x10b) },
		},
		{
			name: "EFI driver", wantErr: "EFI application",
			mutate: func(b []byte) { put16(b, optionalStart+68, 11) },
		},
		{
			name: "no sections", wantErr: "header dimensions",
			mutate: func(b []byte) { put16(b, peOffset+6, 0) },
		},
		{
			name: "too many sections", wantErr: "header dimensions",
			mutate: func(b []byte) { put16(b, peOffset+6, 65535) },
		},
		{
			name: "short optional header", wantErr: "header dimensions",
			mutate: func(b []byte) { put16(b, peOffset+20, 70) },
		},
		{
			name: "section table out of file", wantErr: "section table",
			mutate: func(b []byte) { put16(b, peOffset+20, 65535) },
		},
		{
			name: "headers smaller than table", wantErr: "headers size",
			mutate: func(b []byte) { put32(b, optionalStart+60, 1) },
		},
		{
			name: "headers larger than file", wantErr: "headers size",
			mutate: func(b []byte) { put32(b, optionalStart+60, 0xffffffff) },
		},
		{
			name: "missing kernel", wantErr: "missing .linux",
			mutate: func(b []byte) { renameSection(b, 3, ".unused") },
		},
		{
			name: "missing initrd", wantErr: "missing .initrd",
			mutate: func(b []byte) { renameSection(b, 4, ".unused") },
		},
		{
			name: "duplicate kernel", wantErr: "duplicate .linux",
			mutate: func(b []byte) { renameSection(b, 5, ".linux") },
		},
		{
			name: "duplicate initrd", wantErr: "duplicate .initrd",
			mutate: func(b []byte) { renameSection(b, 5, ".initrd") },
		},
		{
			name: "duplicate cmdline", wantErr: "duplicate .cmdline",
			mutate: func(b []byte) { renameSection(b, 0, ".cmdline") },
		},
		{
			name: "multiple profiles", wantErr: "multi-profile",
			mutate: func(b []byte) { renameSection(b, 0, ".profile") },
		},
		{
			name: "empty kernel", wantErr: "invalid .linux payload size",
			mutate: func(b []byte) { put32(b, sectionTable+3*40+8, 0) },
		},
		{
			name: "empty initrd", wantErr: "invalid .initrd payload size",
			mutate: func(b []byte) { put32(b, sectionTable+4*40+8, 0) },
		},
		{
			name: "virtual kernel beyond raw data", wantErr: "invalid .linux payload size",
			mutate: func(b []byte) { put32(b, sectionTable+3*40+8, 0xffffffff) },
		},
		{
			name: "section offset wraps uint32", wantErr: "outside image",
			mutate: func(b []byte) { put32(b, sectionTable+4*40+20, 0xffffff00) },
		},
		{
			name: "raw size wraps uint32", wantErr: "outside image",
			mutate: func(b []byte) { put32(b, sectionTable+4*40+16, 0xffffffff) },
		},
		{
			name: "raw section in headers", wantErr: "overlaps PE headers",
			mutate: func(b []byte) { put32(b, sectionTable+3*40+20, 0) },
		},
		{
			name: "overlapping payloads", wantErr: "sections overlap",
			mutate: func(b []byte) { put32(b, sectionTable+4*40+20, sectionOffset(b, 3)) },
		},
		{
			name: "non Linux kernel", wantErr: "not a supported bzImage",
			mutate: func(b []byte) { b[int(sectionOffset(b, 3))+0x202] = 0 },
		},
		{
			name: "old Linux protocol", wantErr: "old protocol version",
			mutate: func(b []byte) { put16(b, int(sectionOffset(b, 3))+0x206, 0x205) },
		},
		{
			name: "short Linux header", wantErr: "not a supported bzImage",
			mutate: func(b []byte) { put32(b, sectionTable+3*40+8, 0x206) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := makePE(netDeskSections())
			tc.mutate(data)
			image, err := uki.Open(bytes.NewReader(data), int64(len(data)))
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) || image != nil {
				t.Fatalf("Open() = (%v, %v), want nil image and error containing %q", image, err, tc.wantErr)
			}
		})
	}
}

func TestOpenTruncated(t *testing.T) {
	t.Parallel()

	data := makePE(netDeskSections())
	for _, length := range []int{0, 1, 63, 64, peOffset + 23, optionalStart + 111, sectionTable + 39, len(data) - 1} {
		t.Run(fmt.Sprintf("size_%d", length), func(t *testing.T) {
			t.Parallel()
			if _, err := uki.Open(bytes.NewReader(data[:length]), int64(length)); err == nil {
				t.Fatal("Open() accepted a truncated image")
			}
		})
	}
	if _, err := uki.Open(bytes.NewReader(data[:len(data)-1]), int64(len(data))); err == nil {
		t.Fatal("Open() accepted a backing reader shorter than the declared size")
	}
	if _, err := uki.Open(nil, 4096); err == nil {
		t.Fatal("Open() accepted a nil reader")
	}
	if _, err := uki.Open(bytes.NewReader(data), -1); err == nil {
		t.Fatal("Open() accepted a negative size")
	}
}

func TestOpenStreamsLargeInitrd(t *testing.T) {
	t.Parallel()

	// A sparse ReaderAt models a 2 GiB initramfs without allocating it. Any
	// attempt to read the payload eagerly fails the test immediately.
	data := makePE(netDeskSections()[:5])
	const initrdSize = 2 * 1024 * 1024 * 1024
	put32(data, sectionTable+4*40+8, initrdSize)
	put32(data, sectionTable+4*40+16, initrdSize)
	size := int64(sectionOffset(data, 4)) + initrdSize
	reader := &sparseReader{header: data, size: size}
	image, err := uki.Open(reader, size)
	if err != nil {
		t.Fatal(err)
	}
	if image.Initrd.Size() != initrdSize {
		t.Fatalf("Initrd.Size() = %d, want %d", image.Initrd.Size(), initrdSize)
	}
	if reader.bytesRead > 4096 {
		t.Fatalf("Open() read %d bytes for a 2 GiB image", reader.bytesRead)
	}
	if _, err := image.Initrd.Seek(-1, io.SeekEnd); err != nil {
		t.Fatal(err)
	}
	var last [2]byte
	if n, err := image.Initrd.Read(last[:]); n != 1 || err != nil {
		t.Fatalf("read final byte = (%d, %v), want (1, nil)", n, err)
	}
	if n, err := image.Initrd.Read(last[:]); n != 0 || !errors.Is(err, io.EOF) {
		t.Fatalf("read past payload = (%d, %v), want (0, EOF)", n, err)
	}
}

func FuzzOpen(f *testing.F) {
	f.Add(makePE(netDeskSections()))
	f.Add([]byte("MZ"))
	f.Fuzz(func(t *testing.T, data []byte) {
		image, err := uki.Open(bytes.NewReader(data), int64(len(data)))
		if err != nil {
			return
		}
		if image.Kernel.Size() <= 0 || image.Initrd.Size() <= 0 {
			t.Fatal("successful Open() returned an empty payload")
		}
		for _, reader := range []*io.SectionReader{image.Kernel, image.Initrd} {
			n, err := io.Copy(io.Discard, reader)
			if err != nil || n != reader.Size() {
				t.Fatalf("payload read = (%d, %v), want %d bytes", n, err, reader.Size())
			}
		}
	})
}

func netDeskSections() []fixtureSection {
	kernel := make([]byte, 4097)
	put16(kernel, 0x1fe, 0xaa55)
	copy(kernel[0x202:], "HdrS")
	put16(kernel, 0x206, 0x20f)

	return []fixtureSection{
		{name: ".text", data: []byte("EFI stub bytes")},
		{name: ".osrel", data: []byte("ID=alpine\n")},
		{name: ".uname", data: []byte("6.18.1-lts\x00")},
		{name: ".linux", data: kernel},
		{name: ".initrd", data: []byte{0x1f, 0x8b, 8, 0, 0, 0, 0, 0, 0, 3}},
		{name: ".cmdline", data: []byte("rdinit=/init console=tty0 console=ttyS0,115200n8 loglevel=4\n\x00")},
	}
}

func makePE(sections []fixtureSection) []byte {
	align := func(n int) int { return (n + 511) &^ 511 }
	headerSize := align(sectionTable + 40*len(sections))
	size := headerSize
	for _, s := range sections {
		size += align(len(s.data))
	}
	data := make([]byte, size)
	copy(data, "MZ")
	put32(data, 60, peOffset)
	copy(data[peOffset:], "PE\x00\x00")
	put16(data, peOffset+4, 0x8664)
	put16(data, peOffset+6, uint16(len(sections)))
	put16(data, peOffset+20, 240)
	put16(data, peOffset+22, 0x202)
	put16(data, optionalStart, 0x20b)
	put32(data, optionalStart+32, 4096)
	put32(data, optionalStart+36, 512)
	put32(data, optionalStart+60, uint32(headerSize))
	put16(data, optionalStart+68, 10)
	put32(data, optionalStart+108, 16)
	offset := headerSize
	for i, s := range sections {
		header := sectionTable + i*40
		copy(data[header:header+8], s.name)
		put32(data, header+8, uint32(len(s.data)))
		put32(data, header+12, uint32((i+1)*8192))
		put32(data, header+16, uint32(align(len(s.data))))
		put32(data, header+20, uint32(offset))
		put32(data, header+36, 0x40000040)
		copy(data[offset:], s.data)
		// Distinct nonzero padding makes accidental exposure observable.
		for j := offset + len(s.data); j < offset+align(len(s.data)); j++ {
			data[j] = 0xcc
		}
		offset += align(len(s.data))
	}

	return data
}

func renameSection(data []byte, i int, name string) {
	header := data[sectionTable+i*40 : sectionTable+i*40+8]
	clear(header)
	copy(header, name)
}

func sectionOffset(data []byte, i int) uint32 {
	return binary.LittleEndian.Uint32(data[sectionTable+i*40+20:])
}

func put16(data []byte, offset int, value uint16) {
	binary.LittleEndian.PutUint16(data[offset:], value)
}

func put32(data []byte, offset int, value uint32) {
	binary.LittleEndian.PutUint32(data[offset:], value)
}

var errEagerPayloadRead = errors.New("unexpected eager payload read")

type sparseReader struct {
	header    []byte
	size      int64
	bytesRead int
}

func (r *sparseReader) ReadAt(p []byte, offset int64) (int, error) {
	if len(p) > 4096 {
		return 0, errEagerPayloadRead
	}
	if offset < 0 || offset >= r.size {
		return 0, io.EOF
	}
	n := min(int64(len(p)), r.size-offset)
	clear(p[:n])
	if offset < int64(len(r.header)) {
		copy(p[:n], r.header[offset:])
	}
	r.bytesRead += int(n)
	if n < int64(len(p)) {
		return int(n), io.EOF
	}

	return int(n), nil
}
