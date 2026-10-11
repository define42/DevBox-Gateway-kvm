package machine

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func testUEFIVars(t *testing.T) (string, os.FileInfo) {
	t.Helper()
	dir := t.TempDir()
	code := filepath.Join(dir, "CODE.fd")
	if err := os.WriteFile(code, make([]byte, 4096), 0o600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(code)
	if err != nil {
		t.Fatal(err)
	}
	data := bytes.Repeat([]byte{0xff}, 2*uefiFlashBlockSize)
	copy(data[40:], "_FVH")
	path := filepath.Join(dir, "VARS.fd")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path, info
}

func TestUEFIFlashProgramEraseAndPersistence(t *testing.T) {
	t.Parallel()
	path, code := testUEFIVars(t)
	f, err := openUEFIVars(path, code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	f.base = 0xffe00000
	write := func(offset uint64, values ...byte) {
		t.Helper()
		for _, value := range values {
			if handled, err := f.access(f.base+offset, []byte{value}, true); !handled || err != nil {
				t.Fatalf("write %#x: handled=%v err=%v", offset, handled, err)
			}
		}
	}
	read := func(offset uint64) byte {
		t.Helper()
		data := []byte{0}
		if handled, err := f.access(f.base+offset, data, false); !handled || err != nil {
			t.Fatalf("read %#x: handled=%v err=%v", offset, handled, err)
		}

		return data[0]
	}
	// These command sequences are used by OVMF's flash probe and write path.
	write(100, 0x50, 0x70)
	if got := read(100); got != 0 {
		t.Fatalf("clear status = %#x", got)
	}
	write(100, 0xff, 0x10, 0x5a)
	if got := read(100); got != 0x80 {
		t.Fatalf("program status = %#x", got)
	}
	write(100, 0xff)
	if got := read(100); got != 0x5a {
		t.Fatalf("programmed byte = %#x", got)
	}
	data, err := os.ReadFile(path)
	if err != nil || data[100] != 0x5a {
		t.Fatalf("program not persisted: err=%v", err)
	}
	// Flash programming cannot turn cleared bits back on without an erase.
	write(100, 0x40, 0xf0, 0xff)
	if got := read(100); got != 0x50 {
		t.Fatalf("programmed bits = %#x", got)
	}
	write(uefiFlashBlockSize+100, 0x10, 0x12, 0xff)
	write(100, 0x20, 0x00)
	if got := read(100); got != 0xb0 {
		t.Fatalf("invalid erase status = %#x", got)
	}
	write(100, 0xff)
	if got := read(100); got != 0x50 {
		t.Fatalf("invalid erase changed data: %#x", got)
	}
	write(uefiFlashBlockSize+100, 0x20, 0xd0, 0xff)
	if got := read(uefiFlashBlockSize + 100); got != 0xff {
		t.Fatalf("erased byte = %#x", got)
	}
	if got := read(100); got != 0x50 {
		t.Fatalf("erase affected another block: %#x", got)
	}
	// Close flushes a final write even if the guest stops before READ_ARRAY.
	write(101, 0x10, 0x34)
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	data, err = os.ReadFile(path)
	if err != nil || data[100] != 0x50 || data[101] != 0x34 || data[uefiFlashBlockSize+100] != 0xff {
		t.Fatalf("close did not persist flash state: err=%v", err)
	}
	if _, err := f.access(f.base, make([]byte, 1), false); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed access returned %v", err)
	}
}

func TestUEFIFlashExclusiveLockAndBounds(t *testing.T) {
	t.Parallel()
	path, code := testUEFIVars(t)
	f, err := openUEFIVars(path, code)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	if other, err := openUEFIVars(path, code); err == nil {
		_ = other.Close()
		t.Fatal("same variable store accepted by two VMs")
	}
	f.base = 0x10000
	if handled, err := f.access(f.base-1, []byte{0}, false); handled || err != nil {
		t.Fatalf("outside access: %v, %v", handled, err)
	}
	if handled, err := f.access(f.base+uint64(len(f.data))-1, make([]byte, 2), false); !handled || err == nil {
		t.Fatalf("boundary crossing: %v, %v", handled, err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	other, err := openUEFIVars(path, code)
	if err != nil {
		t.Fatalf("close failed to release file lock: %v", err)
	}
	if err := other.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUEFIFlashRejectsInvalidStores(t *testing.T) {
	t.Parallel()
	for _, name := range []string{"same file", "bad size", "bad header", "missing"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			path, code := testUEFIVars(t)
			switch name {
			case "same file":
				var err error
				code, err = os.Stat(path)
				if err != nil {
					t.Fatal(err)
				}
			case "bad size":
				if err := os.Truncate(path, 100); err != nil {
					t.Fatal(err)
				}
			case "bad header":
				if err := os.WriteFile(path, make([]byte, 8192), 0o600); err != nil {
					t.Fatal(err)
				}
			case "missing":
				path += ".missing"
			}
			if f, err := openUEFIVars(path, code); err == nil {
				_ = f.Close()
				t.Fatal("invalid variable store accepted")
			}
		})
	}
}

func TestUEFIResetPort(t *testing.T) {
	t.Parallel()
	for _, value := range []byte{0, 2, 4, 6, 0xe} {
		err := uefiReset(0xcf9, []byte{value})
		if got, want := errors.Is(err, ErrUEFIReset), value&4 != 0; got != want {
			t.Errorf("CF9 %#x: reset=%v want=%v", value, got, want)
		}
	}
}
