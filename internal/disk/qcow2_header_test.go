package disk

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestQCOW2RejectsMalformedHeader(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func([]byte)
		want string
	}{
		{name: "header exceeds cluster", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[100:], 1024)
		}, want: "header exceeds"},
		{name: "short v3 header", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[100:], 96)
		}, want: "header_length"},
		{name: "unaligned v3 header", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[100:], 105)
		}, want: "header_length"},
		{name: "compression without feature", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[100:], 112)
			b[104] = 1
		}, want: "compression type"},
		{name: "header padding", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[100:], 112)
			b[111] = 1
		}, want: "header padding"},
		{name: "l1 allocation cap", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[36:], qcow2MaxL1Bytes/8+1)
		}, want: "l1 table exceeds"},
		{name: "refcount allocation cap", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[56:], qcow2MaxRefcountTableBytes/512+1)
		}, want: "refcount table exceeds"},
		{name: "l1 coverage", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[36:], 1)
		}, want: "l1 table is too small"},
		{name: "l1 offset overflow", edit: func(b []byte) {
			binary.BigEndian.PutUint64(b[40:], ^uint64(511))
		}, want: "offset exceeds"},
		{name: "refcount offset overflow", edit: func(b []byte) {
			binary.BigEndian.PutUint64(b[48:], ^uint64(511))
		}, want: "offset exceeds"},
		{name: "unknown incompatible feature", edit: func(b []byte) {
			binary.BigEndian.PutUint64(b[72:], uint64(1)<<63)
		}, want: "unknown incompatible"},
		{name: "autoclear feature", edit: func(b []byte) {
			binary.BigEndian.PutUint64(b[88:], 1)
		}, want: "autoclear"},
		{name: "unsupported refcount width", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[96:], 3)
		}, want: "refcount_order"},
		{name: "extension length", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[104:], 0x12345678)
			binary.BigEndian.PutUint32(b[108:], 4096)
		}, want: "extension exceeds"},
		{name: "extension length overflow", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[104:], 0x12345678)
			binary.BigEndian.PutUint32(b[108:], ^uint32(0))
		}, want: "extension exceeds"},
		{name: "duplicate unknown extension", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], 0x12345678, nil)
			putTestHeaderExtension(b[112:], 0x12345678, nil)
		}, want: "duplicate header extension"},
		{name: "duplicate known extension", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], qcow2ExtensionFeatureNames, nil)
			putTestHeaderExtension(b[112:], qcow2ExtensionFeatureNames, nil)
		}, want: "duplicate header extension"},
		{name: "feature table length", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], qcow2ExtensionFeatureNames, []byte{0})
		}, want: "feature name table length"},
		{name: "feature table type", edit: func(b []byte) {
			entry := make([]byte, qcow2FeatureNameEntrySize)
			entry[0] = 3
			putTestHeaderExtension(b[104:], qcow2ExtensionFeatureNames, entry)
		}, want: "feature name table entry"},
		{name: "feature table bit", edit: func(b []byte) {
			entry := make([]byte, qcow2FeatureNameEntrySize)
			entry[1] = 64
			putTestHeaderExtension(b[104:], qcow2ExtensionFeatureNames, entry)
		}, want: "feature name table entry"},
		{name: "encryption extension without encryption", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], qcow2ExtensionEncryption, make([]byte, 16))
		}, want: "encryption extension"},
		{name: "external data extension without feature", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], qcow2ExtensionExternalData, []byte("data.raw"))
		}, want: "external data extension"},
		{name: "bitmap extension", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], qcow2ExtensionBitmaps, make([]byte, 24))
		}, want: "bitmap extensions"},
		{name: "backing format without backing file", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], qcow2ExtensionBackingFormat, []byte("raw"))
		}, want: "backing format extension"},
		{name: "extension terminator length", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[108:], 1)
		}, want: "extension terminator"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			tc.edit(data)
			//nolint:gosec // The fixture path is created beneath this test's private TempDir.
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			for _, open := range []func(string) (Image, error){Open, OpenReadOnly} {
				image, err := open(path)
				if image != nil {
					_ = image.Close()
				}
				if err == nil || !strings.Contains(err.Error(), tc.want) {
					t.Fatalf("Open: got %v, want error containing %q", err, tc.want)
				}
			}
		})
	}
}

func TestQCOW2CompatibleHeaders(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name string
		edit func([]byte)
	}{
		{name: "v2", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[4:], qcow2Version2)
			clear(b[72:512])
		}},
		{name: "v3"},
		{name: "unknown compatible feature", edit: func(b []byte) {
			binary.BigEndian.PutUint64(b[80:], uint64(1)<<63)
		}},
		{name: "unknown additional header field", edit: func(b []byte) {
			binary.BigEndian.PutUint32(b[100:], 120)
			binary.BigEndian.PutUint64(b[112:], 0x12345678)
		}},
		{name: "unknown extension and nonzero extension padding", edit: func(b []byte) {
			putTestHeaderExtension(b[104:], 0x12345678, []byte("abc"))
			b[119] = 0x5a
		}},
		{name: "feature names", edit: func(b []byte) {
			entry := make([]byte, qcow2FeatureNameEntrySize)
			entry[0], entry[1] = 1, 63
			copy(entry[2:], "compatible feature")
			putTestHeaderExtension(b[104:], qcow2ExtensionFeatureNames, entry)
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			path := createEmptyCacheTestQCOW2(t)
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if tc.edit != nil {
				tc.edit(data)
			}
			//nolint:gosec // The fixture path is created beneath this test's private TempDir.
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			image, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer image.Close()
			want := []byte("compatible metadata survives a guest write")
			mustWriteAt(t, image, want, 17)
			assertReadAt(t, image, want, 17)
			if err := image.Close(); err != nil {
				t.Fatal(err)
			}
			after, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(data[:512], after[:512]) {
				t.Fatal("guest write modified the header or compatible extensions")
			}
		})
	}
}

func TestQCOW2HeaderQEMUInteroperability(t *testing.T) {
	t.Parallel()
	for _, compat := range []string{"0.10", "1.1"} {
		t.Run(compat, func(t *testing.T) {
			t.Parallel()
			path := filepath.Join(t.TempDir(), "disk.qcow2")
			runCommand(t, "qemu-img", "create", "-f", "qcow2", "-o", "compat="+compat+",cluster_size=512", path, "1M")
			image, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer image.Close()
			want := bytes.Repeat([]byte{0x5a}, 2048)
			mustWriteAt(t, image, want, 499)
			if err := image.Close(); err != nil {
				t.Fatal(err)
			}
			runCommand(t, "qemu-img", "check", path)
			reopened, err := OpenReadOnly(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertReadAt(t, reopened, want, 499)
		})
	}
}

func putTestHeaderExtension(area []byte, kind uint32, data []byte) {
	binary.BigEndian.PutUint32(area[:4], kind)
	binary.BigEndian.PutUint32(area[4:8], uint32(len(data)))
	copy(area[8:], data)
}
