package disk

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestCreateQCOW2(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		size int64
	}{
		{name: "one byte", size: 1},
		{name: "partial cluster", size: 65536 + 17},
		{name: "multiple L2 tables", size: 1<<30 + 17},
		{name: "multiple refcount table clusters", size: 16 << 40},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "disk.qcow2")
			image, err := CreateQCOW2(path, tc.size)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = image.Close() })
			if image.Size() != tc.size {
				t.Fatalf("Size = %d, want %d", image.Size(), tc.size)
			}
			info, err := os.Stat(path)
			if err != nil {
				t.Fatal(err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("permissions = %o, want 600", info.Mode().Perm())
			}
			if info.Size() > 1<<20 {
				t.Fatalf("empty image size = %d, want sparse metadata below 1 MiB", info.Size())
			}
			zero := make([]byte, min(tc.size, 4096))
			assertReadAt(t, image, zero, 0)
			assertReadAt(t, image, zero, tc.size-int64(len(zero)))
			mustWriteAt(t, image, []byte{0xa5}, 0)
			if tc.size > 1 {
				mustWriteAt(t, image, []byte{0x5a}, tc.size-1)
			}
			if err := image.Sync(); err != nil {
				t.Fatal(err)
			}
			if err := image.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertReadAt(t, reopened, []byte{0xa5}, 0)
			if tc.size > 1 {
				assertReadAt(t, reopened, []byte{0x5a}, tc.size-1)
			}
		})
	}
}

func TestCreateQCOW2FullCapacity(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "disk.qcow2")
	want := bytes.Repeat([]byte{0x5a, 0xa5, 0xff}, 700001)
	image, err := CreateQCOW2(path, int64(len(want)))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = image.Close() })
	mustWriteAt(t, image, want, 0)
	if err := image.Sync(); err != nil {
		t.Fatal(err)
	}
	if err := image.Close(); err != nil {
		t.Fatal(err)
	}
	reopened, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	assertReadAt(t, reopened, want, 0)
}

func TestCreateQCOW2PreservesExistingFile(t *testing.T) {
	t.Parallel()
	path := filepath.Join(t.TempDir(), "disk.qcow2")
	want := []byte("existing image contents")
	if err := os.WriteFile(path, want, 0o600); err != nil {
		t.Fatal(err)
	}
	image, err := CreateQCOW2(path, 1<<20)
	if image != nil {
		_ = image.Close()
		t.Fatal("CreateQCOW2 returned an image for an existing path")
	}
	if !errors.Is(err, os.ErrExist) {
		t.Fatalf("CreateQCOW2 = %v, want file-exists error", err)
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("existing contents changed: got %q, want %q", got, want)
	}
}

func TestCreateQCOW2InvalidSize(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		size int64
	}{
		{name: "negative", size: -1},
		{name: "zero", size: 0},
		{name: "refcount table limit", size: 2 << 50},
		{name: "L1 table limit", size: maxInt64},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "disk.qcow2")
			image, err := CreateQCOW2(path, tc.size)
			if image != nil {
				_ = image.Close()
				t.Fatal("CreateQCOW2 returned an image for an invalid size")
			}
			if err == nil {
				t.Fatal("CreateQCOW2 accepted an invalid size")
			}
			if tc.size <= 0 && !errors.Is(err, ErrInvalidSize) {
				t.Fatalf("CreateQCOW2 = %v, want invalid-size error", err)
			}
			if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("invalid creation left a file: %v", err)
			}
		})
	}
}
