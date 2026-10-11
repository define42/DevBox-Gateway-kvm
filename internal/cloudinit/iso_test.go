package cloudinit

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/kdomanski/iso9660"
)

func TestValidateISO(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name  string
		label string
		files map[string][]byte
		valid bool
	}{
		{name: "lowercase label", label: "cidata", files: validSeedFiles(), valid: true},
		{name: "uppercase label", label: "CIDATA", files: validSeedFiles(), valid: true},
		{name: "wrong label", label: "desktop", files: validSeedFiles()},
		{name: "mixed case label", label: "CiData", files: validSeedFiles()},
		{name: "missing user-data", label: "cidata", files: map[string][]byte{"meta-data": []byte("instance-id: vm\n")}},
		{name: "missing meta-data", label: "cidata", files: map[string][]byte{"user-data": []byte("#cloud-config\n")}},
		{
			name: "nested documents", label: "cidata",
			files: map[string][]byte{"seed/user-data": []byte("#cloud-config\n"), "seed/meta-data": []byte("instance-id: vm\n")},
		},
		{
			name: "wrong filename", label: "cidata",
			files: map[string][]byte{"user_data": []byte("#cloud-config\n"), "meta-data": []byte("instance-id: vm\n")},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := testISO(t, tc.label, tc.files)
			path := writeTestISO(t, data)
			err := ValidateISO(path)
			if (err == nil) != tc.valid {
				t.Fatalf("ValidateISO valid=%v: %v", tc.valid, err)
			}
			after, err := os.ReadFile(path)
			if err != nil || !bytes.Equal(after, data) {
				t.Fatalf("validation changed the seed image: %v", err)
			}
		})
	}
}

func TestValidateISORejectsMalformedImage(t *testing.T) {
	t.Parallel()

	original := testISO(t, "cidata", validSeedFiles())
	for _, tc := range []struct {
		name   string
		mutate func([]byte) []byte
	}{
		{name: "empty", mutate: func([]byte) []byte { return nil }},
		{name: "truncated descriptor", mutate: func(data []byte) []byte { return data[:16*isoSectorSize+100] }},
		{
			name: "root outside image",
			mutate: func(data []byte) []byte {
				writeDual32(data[16*isoSectorSize+158:], ^uint32(0))

				return data
			},
		},
		{
			name: "huge root length",
			mutate: func(data []byte) []byte {
				writeDual32(data[16*isoSectorSize+166:], ^uint32(0))

				return data
			},
		},
		{
			name: "short directory record",
			mutate: func(data []byte) []byte {
				data[testRootOffset(data)] = 1

				return data
			},
		},
		{
			name: "oversized identifier",
			mutate: func(data []byte) []byte {
				data[testRootOffset(data)+32] = 255

				return data
			},
		},
		{
			name: "root endian mismatch",
			mutate: func(data []byte) []byte {
				data[16*isoSectorSize+162] ^= 1

				return data
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := tc.mutate(bytes.Clone(original))
			if err := ValidateISO(writeTestISO(t, data)); err == nil {
				t.Fatal("malformed ISO was accepted")
			}
		})
	}
}

func TestValidateISOLimits(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "oversized.iso")
	file, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := file.Truncate(MaxISOSize + 1); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := ValidateISO(path); err == nil {
		t.Fatal("oversized image was accepted")
	}
	if err := ValidateISO(t.TempDir()); err == nil {
		t.Fatal("directory was accepted")
	}
	files := validSeedFiles()
	files["network-config"] = bytes.Repeat([]byte("x"), MaxSeedDataSize)
	if err := ValidateISO(writeTestISO(t, testISO(t, "cidata", files))); err == nil {
		t.Fatal("oversized document total was accepted")
	}
}

func validSeedFiles() map[string][]byte {
	return map[string][]byte{"user-data": []byte("#cloud-config\n"), "meta-data": []byte("instance-id: vm-test\n")}
}

func testISO(t *testing.T, label string, files map[string][]byte) []byte {
	t.Helper()
	writer, err := iso9660.NewWriter()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = writer.Cleanup() })
	for name, data := range files {
		if err := writer.AddFile(bytes.NewReader(data), name); err != nil {
			t.Fatal(err)
		}
	}
	var output bytes.Buffer
	if err := writer.WriteTo(&output, label); err != nil {
		t.Fatal(err)
	}

	return output.Bytes()
}

func writeTestISO(t *testing.T, data []byte) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "seed.iso")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}

	return path
}

func testRootOffset(data []byte) int {
	return int(binary.LittleEndian.Uint32(data[16*isoSectorSize+158:])) * isoSectorSize
}

func writeDual32(data []byte, value uint32) {
	binary.LittleEndian.PutUint32(data[:4], value)
	binary.BigEndian.PutUint32(data[4:8], value)
}
