package disk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"testing"
)

func TestInspectQCOW2(t *testing.T) {
	t.Parallel()
	path := createEmptyCacheTestQCOW2(t)
	got, err := InspectQCOW2(openSizeTestFile(t, path))
	if err != nil {
		t.Fatal(err)
	}
	if got != 512*1024 {
		t.Fatalf("InspectQCOW2 = %d, want %d", got, 512*1024)
	}
}

func TestInspectQCOW2RejectsInvalidFiles(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func([]byte) []byte
	}{
		{name: "empty", edit: func([]byte) []byte { return nil }},
		{name: "magic", edit: func(b []byte) []byte {
			b[0] = 0

			return b
		}},
		{name: "version", edit: func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[4:8], 4)

			return b
		}},
		{name: "zero_capacity", edit: func(b []byte) []byte {
			binary.BigEndian.PutUint64(b[24:32], 0)

			return b
		}},
		{name: "capacity_overflow", edit: func(b []byte) []byte {
			binary.BigEndian.PutUint64(b[24:32], uint64(maxInt64)+1)

			return b
		}},
		{name: "short_header", edit: func(b []byte) []byte { return b[:31] }},
		{name: "short_v3_header", edit: func(b []byte) []byte { return b[:72] }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(path, tc.edit(data), 0o600); err != nil {
				t.Fatal(err)
			}
			if size, err := InspectQCOW2(openSizeTestFile(t, path)); err == nil {
				t.Fatalf("InspectQCOW2 = %d, want error", size)
			}
		})
	}
}

func TestInspectQCOW2RejectsDirectory(t *testing.T) {
	t.Parallel()
	if size, err := InspectQCOW2(openSizeTestFile(t, t.TempDir())); err == nil {
		t.Fatalf("InspectQCOW2 directory = %d, want error", size)
	}
}

func TestValidateQCOW2Size(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name     string
		expected int64
		want     error
	}{
		{name: "exact", expected: 512 * 1024},
		{name: "smaller", expected: 512*1024 - 1, want: ErrSpecMismatch},
		{name: "larger", expected: 512*1024 + 1, want: ErrSpecMismatch},
		{name: "zero", want: ErrInvalidSize},
		{name: "negative", expected: -1, want: ErrInvalidSize},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			file := openSizeTestFile(t, path)
			if err := ValidateQCOW2Size(file, tc.expected); !errors.Is(err, tc.want) {
				t.Fatalf("ValidateQCOW2Size = %v, want %v", err, tc.want)
			}
			image, err := OpenQCOW2WithSize(path, tc.expected)
			if image != nil {
				_ = image.Close()
			}
			if !errors.Is(err, tc.want) {
				t.Fatalf("OpenQCOW2WithSize = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestValidateQCOW2(t *testing.T) {
	t.Parallel()
	const capacity = 512 * 1024
	for _, tc := range []struct {
		name      string
		expected  int64
		malformed bool
		want      error
	}{
		{name: "valid", expected: capacity},
		{name: "capacity mismatch", expected: capacity + 1, want: ErrSpecMismatch},
		{name: "zero capacity", want: ErrInvalidSize},
		{name: "negative capacity", expected: -1, want: ErrInvalidSize},
		{name: "overlapping metadata", expected: capacity, malformed: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			before, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.malformed {
				// Keep the fixed header valid, but alias L1 and refcount tables.
				binary.BigEndian.PutUint64(before[40:48], 512)
				//nolint:gosec // The fixed fixture path was created below t.TempDir.
				if err := os.WriteFile(path, before, 0o600); err != nil {
					t.Fatal(err)
				}
			}
			file, err := os.OpenFile(path, os.O_RDWR, 0)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = file.Close() })
			if tc.malformed {
				if err := ValidateQCOW2Size(file, capacity); err != nil {
					t.Fatalf("header-only validation rejected fixture: %v", err)
				}
			}
			err = ValidateQCOW2(file, tc.expected)
			if tc.malformed {
				if err == nil {
					t.Fatal("ValidateQCOW2 accepted overlapping metadata")
				}
			} else if !errors.Is(err, tc.want) {
				t.Fatalf("ValidateQCOW2 = %v, want %v", err, tc.want)
			}
			// Validation must preserve caller ownership on every return path.
			after := make([]byte, len(before))
			if _, err := file.ReadAt(after, 0); err != nil {
				t.Fatalf("validation closed the caller's descriptor: %v", err)
			}
			if !bytes.Equal(after, before) {
				t.Fatal("validation changed the image")
			}
		})
	}
}

func TestOpenQCOW2WithSize(t *testing.T) {
	t.Parallel()
	path := createEmptyCacheTestQCOW2(t)
	const expected = 512 * 1024
	image, err := OpenQCOW2WithSize(path, expected)
	if err != nil {
		t.Fatal(err)
	}
	defer image.Close()
	if image.Size() != expected {
		t.Fatalf("Size = %d, want %d", image.Size(), expected)
	}
	want := []byte("pinned qcow2")
	if _, err := image.WriteAt(want, 512); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, len(want))
	if _, err := image.ReadAt(got, 512); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("ReadAt = %q, want %q", got, want)
	}
}

func openSizeTestFile(t *testing.T, path string) *os.File {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = file.Close() })

	return file
}
