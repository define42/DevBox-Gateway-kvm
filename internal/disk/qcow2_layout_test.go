package disk

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const layoutClusterSize = 512

func TestQCOW2RejectsInvalidLayoutWithoutWriting(t *testing.T) {
	t.Parallel()
	put64 := func(data []byte, offset int, value uint64) { binary.BigEndian.PutUint64(data[offset:], value) }
	for _, tt := range []struct {
		name   string
		change func([]byte) []byte
		want   string
	}{
		{name: "data aliases l1", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, 3*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "overlapping"},
		{name: "data aliases refcount table", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "overlapping"},
		{name: "data aliases refcount block", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, 2*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "overlapping"},
		{name: "data aliases own l2", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, 4*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "overlapping"},
		{name: "data aliases later l2", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, 6*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "overlapping"},
		{name: "preallocated zero aliases metadata", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, 3*layoutClusterSize|qcow2OFlagCopied|qcow2OFlagZero)

			return b
		}, want: "overlapping"},
		{name: "data aliases other data", change: func(b []byte) []byte {
			put64(b, 6*layoutClusterSize, 5*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "overlapping"},
		{name: "l1 aliases refcount table", change: func(b []byte) []byte {
			put64(b, 40, layoutClusterSize)

			return b
		}, want: "overlapping"},
		{name: "refcount block aliases l1", change: func(b []byte) []byte {
			put64(b, layoutClusterSize, 3*layoutClusterSize)

			return b
		}, want: "overlapping"},
		{name: "refcount block aliases header", change: func(b []byte) []byte {
			put64(b, layoutClusterSize, 1)

			return b
		}, want: "reserved"},
		{name: "duplicate refcount block", change: func(b []byte) []byte {
			put64(b, layoutClusterSize+8, 2*layoutClusterSize)

			return b
		}, want: "overlapping"},
		{name: "duplicate l2", change: func(b []byte) []byte {
			put64(b, 3*layoutClusterSize+8, 4*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "overlapping"},
		{name: "l1 reserved low bit", change: func(b []byte) []byte {
			b[3*layoutClusterSize+7] |= 1

			return b
		}, want: "reserved l1"},
		{name: "l1 reserved high bit", change: func(b []byte) []byte {
			b[3*layoutClusterSize] |= 1

			return b
		}, want: "reserved l1"},
		{name: "refcount reserved bit", change: func(b []byte) []byte {
			b[layoutClusterSize+7] |= 1

			return b
		}, want: "reserved"},
		{name: "l2 reserved bit", change: func(b []byte) []byte {
			b[4*layoutClusterSize+7] |= 2

			return b
		}, want: "reserved l2"},
		{name: "copied flag without offset", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, qcow2OFlagCopied)

			return b
		}, want: "no host offset"},
		{name: "copied zero flag without offset", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, qcow2OFlagCopied|qcow2OFlagZero)

			return b
		}, want: "no host offset"},
		{name: "version 2 zero flag", change: func(b []byte) []byte {
			binary.BigEndian.PutUint32(b[4:8], qcow2Version2)
			clear(b[72:layoutClusterSize])
			b[4*layoutClusterSize+7] |= 1

			return b
		}, want: "version 2"},
		{name: "l2 outside file", change: func(b []byte) []byte {
			put64(b, 3*layoutClusterSize, 8*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "file bounds"},
		{name: "refcount block outside file", change: func(b []byte) []byte {
			put64(b, layoutClusterSize, 8*layoutClusterSize)

			return b
		}, want: "file bounds"},
		{name: "data outside file", change: func(b []byte) []byte {
			put64(b, 4*layoutClusterSize, 8*layoutClusterSize|qcow2OFlagCopied)

			return b
		}, want: "file bounds"},
		{name: "truncated data", change: func(b []byte) []byte { return b[:len(b)-1] }, want: "file bounds"},
		{name: "l1 outside file", change: func(b []byte) []byte {
			put64(b, 40, 8*layoutClusterSize)

			return b
		}, want: "file bounds"},
		{name: "refcount table outside file", change: func(b []byte) []byte {
			put64(b, 48, 8*layoutClusterSize)

			return b
		}, want: "file bounds"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := tt.change(qcow2LayoutFixture(qcow2Version3))
			assertInvalidLayoutUnchanged(t, data, tt.want)
		})
	}
}

func TestQCOW2RequiresMetadataRefcounts(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name    string
		cluster int
	}{
		{name: "header", cluster: 0},
		{name: "refcount table", cluster: 1},
		{name: "refcount block", cluster: 2},
		{name: "l1 table", cluster: 3},
		{name: "l2 table", cluster: 4},
		{name: "guest data", cluster: 5},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			data := qcow2LayoutFixture(qcow2Version3)
			binary.BigEndian.PutUint16(data[2*layoutClusterSize+tt.cluster*2:], 0)
			assertInvalidLayoutUnchanged(t, data, "refcount")
		})
	}
}

func assertInvalidLayoutUnchanged(t *testing.T, data []byte, want string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "invalid.qcow2")
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, open := range []func(string) (Image, error){Open, OpenReadOnly} {
		image, err := open(path)
		if err == nil {
			_ = image.Close()
			t.Fatal("malformed image accepted")
		}
		if !strings.Contains(err.Error(), want) {
			t.Fatalf("error = %v, want %q", err, want)
		}
	}
	after, err := os.ReadFile(path)
	if err != nil || !bytes.Equal(after, data) {
		t.Fatalf("rejected image was modified: %v", err)
	}
}

// Two distinct L2 tables allow testing aliases with metadata visited later.
// Every allocated cluster has refcount one; corruption tests change mappings
// without updating refcounts, reproducing why trusting refcounts is insufficient.
func qcow2LayoutFixture(version uint32) []byte {
	data := make([]byte, 8*layoutClusterSize)
	put32 := func(offset int, value uint32) { binary.BigEndian.PutUint32(data[offset:], value) }
	put64 := func(offset int, value uint64) { binary.BigEndian.PutUint64(data[offset:], value) }
	copy(data, qcow2Magic)
	put32(4, version)
	put32(20, 9)
	put64(24, 64<<10)
	put32(36, 2)
	put64(40, 3*layoutClusterSize)
	put64(48, layoutClusterSize)
	put32(56, 1)
	if version == qcow2Version3 {
		put32(96, qcow2RefcountOrder16)
		put32(100, qcow2HeaderV3Len)
	}
	put64(layoutClusterSize, 2*layoutClusterSize)
	for cluster := range 8 {
		binary.BigEndian.PutUint16(data[2*layoutClusterSize+cluster*2:], 1)
	}
	put64(3*layoutClusterSize, 4*layoutClusterSize|qcow2OFlagCopied)
	put64(3*layoutClusterSize+8, 6*layoutClusterSize|qcow2OFlagCopied)
	put64(4*layoutClusterSize, 5*layoutClusterSize|qcow2OFlagCopied)
	put64(6*layoutClusterSize, 7*layoutClusterSize|qcow2OFlagCopied)
	for i := range layoutClusterSize {
		data[5*layoutClusterSize+i] = 0x5a
		data[7*layoutClusterSize+i] = 0x6b
	}

	return data
}

func TestQCOW2PreallocatedQEMUInteroperability(t *testing.T) {
	t.Parallel()
	for _, options := range []string{
		"cluster_size=512,preallocation=metadata",
		"cluster_size=65536,preallocation=metadata",
		"cluster_size=2097152,preallocation=metadata",
		"cluster_size=65536,preallocation=full",
		"compat=0.10,cluster_size=65536,preallocation=metadata",
	} {
		t.Run(options, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "preallocated.qcow2")
			runCommand(t, "qemu-img", "create", "-f", "qcow2", "-o", options, path, "4M")
			image, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer image.Close()
			assertReadAt(t, image, make([]byte, 1024), 0)
			want := bytes.Repeat([]byte{0x3c}, 1024)
			mustWriteAt(t, image, want, 65500)
			if err := image.Sync(); err != nil {
				t.Fatal(err)
			}
			if err := image.Close(); err != nil {
				t.Fatal(err)
			}
			runCommand(t, "qemu-img", "check", path)
			rawPath := filepath.Join(t.TempDir(), "disk.raw")
			runCommand(t, "qemu-img", "convert", "-f", "qcow2", "-O", "raw", path, rawPath)
			got, err := os.ReadFile(rawPath)
			if err != nil {
				t.Fatal(err)
			}
			expected := make([]byte, 4<<20)
			copy(expected[65500:], want)
			if !bytes.Equal(got, expected) {
				t.Fatal("QEMU readback differs from guest data")
			}
		})
	}
}
