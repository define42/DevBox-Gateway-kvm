package iodev

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
)

// Fixed fw_cfg selectors used by PC firmware. Integer item payloads are
// little-endian; the file directory itself uses big-endian fields.
const (
	FWCfgSignature uint16 = 0x00
	FWCfgID        uint16 = 0x01
	FWCfgRAMSize   uint16 = 0x03
	FWCfgNBCPUs    uint16 = 0x05
	FWCfgBootMenu  uint16 = 0x0e
	FWCfgMaxCPUs   uint16 = 0x0f
	FWCfgFileDir   uint16 = 0x19
	FWCfgFileFirst uint16 = 0x20

	fwCfgSelectorPort = 0x510
	fwCfgDataPort     = 0x511
	fwCfgWriteChannel = 0x4000
	fwCfgArchLocal    = 0x8000
	fwCfgFileNameSize = 56
	fwCfgFileSize     = 64
)

var errFWCfg = errors.New("fw_cfg")

// FWCfg supplies firmware configuration through QEMU's x86 selector/data ports.
// Items are immutable after construction. The device implements the traditional
// byte-stream interface only; it does not advertise DMA support.
//
// See https://www.qemu.org/docs/master/specs/fw_cfg.html.
type FWCfg struct {
	mu       sync.Mutex
	items    map[uint16][]byte
	selected uint16
	offset   int
}

// NewFWCfg builds a read-only firmware configuration device. Signature, feature
// bitmap and file-directory selectors are generated automatically. Caller-owned
// buffers are copied. Generic selectors at or above FWCfgFileFirst are reserved
// for named files; architecture-local selectors can be supplied in entries.
func NewFWCfg(entries map[uint16][]byte, files map[string][]byte) (*FWCfg, error) {
	if len(files) > fwCfgWriteChannel-int(FWCfgFileFirst) {
		return nil, fmt.Errorf("%w: too many files: %d", errFWCfg, len(files))
	}

	items := make(map[uint16][]byte, len(entries)+len(files)+3)
	items[FWCfgSignature] = []byte("QEMU")
	items[FWCfgID] = []byte{1, 0, 0, 0}

	for selector, data := range entries {
		if selector&fwCfgWriteChannel != 0 {
			return nil, fmt.Errorf("%w: selector %#x uses the unsupported write channel", errFWCfg, selector)
		}
		reserved := selector == FWCfgSignature || selector == FWCfgID || selector == FWCfgFileDir
		fileSelector := selector >= FWCfgFileFirst && selector&fwCfgArchLocal == 0
		if reserved || fileSelector {
			return nil, fmt.Errorf("%w: selector %#x is reserved", errFWCfg, selector)
		}
		if uint64(len(data)) > math.MaxUint32 {
			return nil, fmt.Errorf("%w: item %#x exceeds the 32-bit size limit", errFWCfg, selector)
		}
		items[selector] = slices.Clone(data)
	}

	names := make([]string, 0, len(files))
	for name, data := range files {
		if err := validateFWCfgFileName(name); err != nil {
			return nil, err
		}
		if uint64(len(data)) > math.MaxUint32 {
			return nil, fmt.Errorf("%w: file %q exceeds the 32-bit size limit", errFWCfg, name)
		}
		names = append(names, name)
	}
	// Stable selectors make firmware discovery reproducible across boots.
	slices.Sort(names)

	directory := make([]byte, 4+len(names)*fwCfgFileSize)
	binary.BigEndian.PutUint32(directory, uint32(len(names)))
	for i, name := range names {
		selector := FWCfgFileFirst + uint16(i)
		data := files[name]
		entry := directory[4+i*fwCfgFileSize:]
		binary.BigEndian.PutUint32(entry, uint32(len(data)))
		binary.BigEndian.PutUint16(entry[4:], selector)
		copy(entry[8:8+fwCfgFileNameSize], name)
		items[selector] = slices.Clone(data)
	}
	items[FWCfgFileDir] = directory

	return &FWCfg{items: items}, nil
}

func validateFWCfgFileName(name string) error {
	if len(name) == 0 || len(name) >= fwCfgFileNameSize {
		return fmt.Errorf("%w: file name %q must contain 1 to 55 ASCII bytes", errFWCfg, name)
	}
	for i := range len(name) {
		if name[i] < 0x20 || name[i] > 0x7e {
			return fmt.Errorf("%w: file name %q contains a non-printable ASCII byte", errFWCfg, name)
		}
	}

	return nil
}

// Read consumes one byte from the selected item. Unknown items and exhausted
// streams return zero. The write-only selector register reads as zero.
func (f *FWCfg) Read(port uint64, data []byte) error {
	if port == fwCfgSelectorPort {
		if len(data) != 2 {
			return errDataLenInvalid
		}
		clear(data)

		return nil
	}
	if port != fwCfgDataPort {
		return fmt.Errorf("%w: invalid read port %#x", errFWCfg, port)
	}
	if len(data) != 1 {
		return errDataLenInvalid
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	data[0] = 0
	item := f.items[f.selected]
	if f.offset < len(item) {
		data[0] = item[f.offset]
		f.offset++
	}

	return nil
}

// Write selects an item and rewinds its stream. Data-port writes are ignored,
// matching QEMU's read-only traditional interface.
func (f *FWCfg) Write(port uint64, data []byte) error {
	if port == fwCfgDataPort {
		if len(data) != 1 {
			return errDataLenInvalid
		}

		return nil
	}
	if port != fwCfgSelectorPort {
		return fmt.Errorf("%w: invalid write port %#x", errFWCfg, port)
	}
	if len(data) != 2 {
		return errDataLenInvalid
	}

	f.mu.Lock()
	defer f.mu.Unlock()

	// Bit 14 selected the obsolete write channel. QEMU still ignores it
	// when indexing read-only items.
	f.selected = binary.LittleEndian.Uint16(data) &^ fwCfgWriteChannel
	f.offset = 0

	return nil
}

func (f *FWCfg) IOPort() uint64 { return fwCfgSelectorPort }

func (f *FWCfg) Size() uint64 { return 2 }
