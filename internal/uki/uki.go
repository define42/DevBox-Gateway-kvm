// Package uki reads the Linux payloads in an x86-64 Unified Kernel Image.
// It does not execute EFI code or verify Secure Boot signatures.
package uki

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/define42/DevBox-Gateway-kvm/internal/bootparam"
)

const (
	maxSections = 96
	maxCmdline  = 64 * 1024
)

// Image exposes embedded payloads without copying them into host memory.
// The reader supplied to Open must remain available while the payloads are read.
type Image struct {
	Kernel  *io.SectionReader
	Initrd  *io.SectionReader
	Cmdline string
}

// ErrInvalidImage identifies malformed images and unsupported UKI formats.
var ErrInvalidImage = errors.New("uki: invalid or unsupported image")

type peLayout struct {
	count       int64
	tableOffset int64
	headerSize  int64
}

type section struct {
	name   string
	offset int64
	size   int64
	rawEnd int64
}

// Open reads a single-profile x86-64 UKI containing a Linux bzImage and initramfs.
// It validates the container and Linux boot header, but does not decompress or
// otherwise validate the initramfs. The optional .cmdline section is UTF-8 text;
// trailing NUL bytes and line endings are removed.
func Open(r io.ReaderAt, size int64) (*Image, error) {
	if r == nil || size < 64 {
		return nil, fmt.Errorf("%w: missing or truncated DOS header", ErrInvalidImage)
	}

	reader := io.NewSectionReader(r, 0, size)
	layout, err := readHeaders(reader, size)
	if err != nil {
		return nil, err
	}
	sections, err := readSections(reader, size, layout)
	if err != nil {
		return nil, err
	}

	kernel := sections[".linux"]
	initrd := sections[".initrd"]
	image := &Image{
		Kernel: io.NewSectionReader(reader, kernel.offset, kernel.size),
		Initrd: io.NewSectionReader(reader, initrd.offset, initrd.size),
	}
	if _, err := bootparam.New(image.Kernel); err != nil {
		return nil, fmt.Errorf("uki: .linux is not a supported bzImage: %w", err)
	}

	if cmdline, exists := sections[".cmdline"]; exists {
		if cmdline.size > maxCmdline {
			return nil, fmt.Errorf("%w: .cmdline exceeds 64 KiB", ErrInvalidImage)
		}
		data := make([]byte, int(cmdline.size))
		if err := readAt(reader, data, cmdline.offset); err != nil {
			return nil, fmt.Errorf("uki: .cmdline: %w", err)
		}
		image.Cmdline = strings.TrimRight(string(data), "\x00\r\n")
		if strings.ContainsRune(image.Cmdline, '\x00') || !utf8.ValidString(image.Cmdline) {
			return nil, fmt.Errorf("%w: .cmdline must be UTF-8 text without embedded NUL bytes", ErrInvalidImage)
		}
	}

	return image, nil
}

func readHeaders(reader *io.SectionReader, size int64) (*peLayout, error) {
	var dos [64]byte
	if err := readAt(reader, dos[:], 0); err != nil {
		return nil, fmt.Errorf("uki: DOS header: %w", err)
	}
	if string(dos[:2]) != "MZ" {
		return nil, fmt.Errorf("%w: missing DOS signature", ErrInvalidImage)
	}
	peOffset := int64(binary.LittleEndian.Uint32(dos[60:64]))
	if peOffset < int64(len(dos)) {
		return nil, fmt.Errorf("%w: PE header overlaps DOS header", ErrInvalidImage)
	}

	var coff [24]byte
	if err := readAt(reader, coff[:], peOffset); err != nil {
		return nil, fmt.Errorf("uki: PE header: %w", err)
	}
	if string(coff[:4]) != "PE\x00\x00" {
		return nil, fmt.Errorf("%w: missing PE signature", ErrInvalidImage)
	}
	if machine := binary.LittleEndian.Uint16(coff[4:6]); machine != 0x8664 {
		return nil, fmt.Errorf("%w: unsupported PE machine %#x; need x86-64", ErrInvalidImage, machine)
	}
	count := int64(binary.LittleEndian.Uint16(coff[6:8]))
	optionalSize := int64(binary.LittleEndian.Uint16(coff[20:22]))
	if count == 0 || count > maxSections || optionalSize < 112 {
		return nil, fmt.Errorf("%w: invalid PE header dimensions", ErrInvalidImage)
	}
	tableOffset := peOffset + int64(len(coff)) + optionalSize
	tableEnd := tableOffset + count*40
	if tableEnd > size {
		return nil, fmt.Errorf("%w: truncated PE section table", ErrInvalidImage)
	}
	var optional [112]byte
	if err := readAt(reader, optional[:], peOffset+int64(len(coff))); err != nil {
		return nil, fmt.Errorf("uki: optional header: %w", err)
	}
	if binary.LittleEndian.Uint16(optional[:2]) != 0x20b {
		return nil, fmt.Errorf("%w: image is not PE32+", ErrInvalidImage)
	}
	if binary.LittleEndian.Uint16(optional[68:70]) != 10 {
		return nil, fmt.Errorf("%w: image is not an EFI application", ErrInvalidImage)
	}
	headerSize := int64(binary.LittleEndian.Uint32(optional[60:64]))
	if headerSize < tableEnd || headerSize > size {
		return nil, fmt.Errorf("%w: invalid PE headers size", ErrInvalidImage)
	}

	return &peLayout{count: count, tableOffset: tableOffset, headerSize: headerSize}, nil
}

func readSections(reader *io.SectionReader, size int64, layout *peLayout) (map[string]section, error) {
	sections := make(map[string]section, 3)
	ranges := make([]section, 0, layout.count)
	for i := int64(0); i < layout.count; i++ {
		var header [40]byte
		if err := readAt(reader, header[:], layout.tableOffset+i*40); err != nil {
			return nil, fmt.Errorf("uki: section header: %w", err)
		}
		s := section{
			name:   string(bytes.TrimRight(header[:8], "\x00")),
			size:   int64(binary.LittleEndian.Uint32(header[8:12])),
			offset: int64(binary.LittleEndian.Uint32(header[20:24])),
		}
		rawSize := int64(binary.LittleEndian.Uint32(header[16:20]))
		s.rawEnd = s.offset + rawSize
		if s.offset > size || s.rawEnd > size {
			return nil, fmt.Errorf("%w: %s section lies outside image", ErrInvalidImage, s.name)
		}
		if rawSize > 0 {
			if s.offset < layout.headerSize {
				return nil, fmt.Errorf("%w: %s section overlaps PE headers", ErrInvalidImage, s.name)
			}
			for _, previous := range ranges {
				if s.offset < previous.rawEnd && previous.offset < s.rawEnd {
					return nil, fmt.Errorf("%w: %s and %s sections overlap", ErrInvalidImage, s.name, previous.name)
				}
			}
			// Check the backing reader too, without loading the payload. A file
			// might have been truncated after its size was obtained.
			var last [1]byte
			if err := readAt(reader, last[:], s.rawEnd-1); err != nil {
				return nil, fmt.Errorf("uki: truncated %s section: %w", s.name, err)
			}
			ranges = append(ranges, s)
		}

		switch s.name {
		case ".profile":
			return nil, fmt.Errorf("%w: multi-profile images are not supported", ErrInvalidImage)
		case ".linux", ".initrd", ".cmdline":
			if _, exists := sections[s.name]; exists {
				return nil, fmt.Errorf("%w: duplicate %s section", ErrInvalidImage, s.name)
			}
			// VirtualSize is the payload length; SizeOfRawData can contain
			// alignment padding that must not reach the Linux loader.
			if s.size > rawSize || (s.size == 0 && s.name != ".cmdline") {
				return nil, fmt.Errorf("%w: invalid %s payload size", ErrInvalidImage, s.name)
			}
			sections[s.name] = s
		}
	}

	for _, name := range []string{".linux", ".initrd"} {
		if _, exists := sections[name]; !exists {
			return nil, fmt.Errorf("%w: missing %s section", ErrInvalidImage, name)
		}
	}

	return sections, nil
}

func readAt(r io.ReaderAt, data []byte, offset int64) error {
	if len(data) == 0 {
		return nil
	}
	n, err := r.ReadAt(data, offset)
	if n != len(data) && err == nil {
		return io.ErrUnexpectedEOF
	}

	return err
}
