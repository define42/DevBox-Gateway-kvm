package machine

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"sync"
	"syscall"
)

const uefiFlashBlockSize = 4096

var (
	errUEFIVarsImage     = errors.New("invalid UEFI variable store: expected a separate, page-aligned OVMF_VARS file")
	errUEFIVarsHeader    = errors.New("invalid UEFI variable store: missing firmware volume header")
	errUEFIFlashBoundary = errors.New("UEFI flash access crosses the variable store boundary")
)

// uefiFlash implements the byte-program command set used by OVMF's QemuFlash
// driver. Code is immutable; only the explicitly supplied per-VM variable file
// is writable. The file is locked for the complete lifetime of the VM.
type uefiFlash struct {
	mu      sync.Mutex
	file    *os.File
	data    []byte
	base    uint64
	command byte
	status  byte
	dirty   bool
	closed  bool
}

func openUEFIVars(path string, codeInfo os.FileInfo) (*uefiFlash, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("open UEFI variable store (use a writable copy of OVMF_VARS): %w", err)
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
		}
	}()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if os.SameFile(codeInfo, info) || !info.Mode().IsRegular() || info.Size() < uefiFlashBlockSize ||
		info.Size() > 16<<20 || info.Size()%uefiFlashBlockSize != 0 {
		return nil, errUEFIVarsImage
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		return nil, fmt.Errorf("lock UEFI variable store: %w", err)
	}
	data := make([]byte, int(info.Size()))
	if _, err := f.ReadAt(data, 0); err != nil {
		return nil, err
	}
	if !bytes.Equal(data[40:44], []byte("_FVH")) {
		return nil, errUEFIVarsHeader
	}
	ok = true

	return &uefiFlash{file: f, data: data, status: 0x80}, nil
}

func (f *uefiFlash) access(addr uint64, data []byte, write bool) (bool, error) {
	if addr < f.base || addr-f.base >= uint64(len(f.data)) {
		return false, nil
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	off := addr - f.base
	if uint64(len(data)) > uint64(len(f.data))-off {
		return true, errUEFIFlashBoundary
	}
	if f.closed {
		return true, os.ErrClosed
	}
	if !write {
		for i := range data {
			switch f.command {
			case 0x70:
				data[i] = f.status
			case 0x90:
				data[i] = 0
			default:
				data[i] = f.data[int(off)+i]
			}
		}

		return true, nil
	}
	for i, value := range data {
		pos := int(off) + i
		switch f.command {
		case 0x10, 0x40:
			f.data[pos] &= value
			f.dirty = true
			f.status = 0x80
			f.command = 0x70
		case 0x20:
			if value == 0xd0 {
				start := pos &^ (uefiFlashBlockSize - 1)
				for j := start; j < start+uefiFlashBlockSize; j++ {
					f.data[j] = 0xff
				}
				f.dirty = true
				f.status = 0x80
			} else {
				f.status = 0xb0
			}
			f.command = 0x70
		default:
			switch value {
			case 0xff:
				f.command = 0
				if err := f.syncLocked(); err != nil {
					return true, err
				}
			case 0x50:
				f.status = 0
				f.command = 0
			case 0x10, 0x40, 0x20, 0x70, 0x90:
				f.command = value
			default:
				f.status = 0x90
				f.command = 0x70
			}
		}
	}

	return true, nil
}

func (f *uefiFlash) syncLocked() error {
	if !f.dirty {
		return nil
	}
	if _, err := f.file.WriteAt(f.data, 0); err != nil {
		return fmt.Errorf("write UEFI variables: %w", err)
	}
	if err := f.file.Sync(); err != nil {
		return fmt.Errorf("sync UEFI variables: %w", err)
	}
	f.dirty = false

	return nil
}

func (f *uefiFlash) Close() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		return nil
	}
	f.closed = true

	return errors.Join(f.syncLocked(), f.file.Close())
}
