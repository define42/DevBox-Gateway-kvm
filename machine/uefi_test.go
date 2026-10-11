package machine

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestUEFIRejectsInvalidCodeBeforeKVM(t *testing.T) {
	t.Parallel()
	for _, size := range []int{0, 4095, 4096, 4097} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "CODE.fd")
			if err := os.WriteFile(path, make([]byte, size), 0o600); err != nil {
				t.Fatal(err)
			}
			m := &Machine{}
			if err := m.LoadUEFI(path, "missing-vars"); !errors.Is(err, errUEFICodeImage) {
				t.Fatalf("invalid code returned %v", err)
			}
		})
	}
}

func TestUEFIInvalidPairReleasesVariableStore(t *testing.T) {
	t.Parallel()
	vars, codeInfo := testUEFIVars(t)
	codePath := filepath.Join(filepath.Dir(vars), "CODE.fd")
	code := make([]byte, 4096)
	copy(code[40:], "_FVH")
	if err := os.WriteFile(codePath, code, 0o600); err != nil {
		t.Fatal(err)
	}
	m := &Machine{}
	// 4 KiB CODE plus 8 KiB VARS is not a supported firmware pair.
	if err := m.LoadUEFI(codePath, vars); !errors.Is(err, errUEFIFirmwarePair) {
		t.Fatalf("invalid firmware pair returned %v", err)
	}
	f, err := openUEFIVars(vars, codeInfo)
	if err != nil {
		t.Fatalf("invalid pair retained variable-store lock: %v", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestUEFICodeMMIOIsImmutable(t *testing.T) {
	t.Parallel()
	code := []byte{0x12, 0x34, 0x56, 0x78}
	m := &Machine{
		uefiCode:     bytes.Clone(code),
		uefiCodeBase: 0xfffffffc,
		uefiVars:     &uefiFlash{base: 0xffff0000, data: make([]byte, 4096)},
	}
	if handled, err := m.uefiMMIO(m.uefiCodeBase, []byte{0xff}, true); !handled || err != nil {
		t.Fatalf("code write was not handled: %v, %v", handled, err)
	}
	if !bytes.Equal(m.uefiCode, code) {
		t.Fatal("guest write modified CODE")
	}
	if handled, err := m.uefiMMIO(0x1000, []byte{0}, false); handled || err != nil {
		t.Fatalf("UEFI intercepted unrelated MMIO: %v, %v", handled, err)
	}
}
