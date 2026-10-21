package qcow2import

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"io"
	"os"
	"path/filepath"
	"testing"

	"github.com/klauspost/compress/flate"
	"github.com/klauspost/compress/zstd"
)

const testCluster = 65536

func testImage() []byte {
	p := make([]byte, 6*testCluster)
	copy(p, "QFI\xfb")
	binary.BigEndian.PutUint32(p[4:], 3)
	binary.BigEndian.PutUint32(p[20:], 16)
	binary.BigEndian.PutUint64(p[24:], 2*testCluster+37)
	binary.BigEndian.PutUint32(p[36:], 1)
	binary.BigEndian.PutUint64(p[40:], 3*testCluster)
	binary.BigEndian.PutUint64(p[48:], testCluster)
	binary.BigEndian.PutUint32(p[56:], 1)
	binary.BigEndian.PutUint32(p[96:], 4)
	binary.BigEndian.PutUint32(p[100:], 104)
	binary.BigEndian.PutUint64(p[testCluster:], 2*testCluster)
	for i := 0; i < 6; i++ {
		binary.BigEndian.PutUint16(p[2*testCluster+i*2:], 1)
	}
	binary.BigEndian.PutUint64(p[3*testCluster:], 4*testCluster|copiedFlag)
	binary.BigEndian.PutUint64(p[4*testCluster:], 5*testCluster|copiedFlag)
	for i := 5 * testCluster; i < len(p); i++ {
		p[i] = byte(i % 251)
	}

	return p
}

func openTestImage(tb testing.TB, p []byte) (*os.File, *Reader) {
	tb.Helper()
	path := filepath.Join(tb.TempDir(), "source.qcow2")
	if err := os.WriteFile(path, p, 0o600); err != nil {
		tb.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = file.Close() })
	reader, err := Open(file)
	if err != nil {
		tb.Fatal(err)
	}
	tb.Cleanup(func() { _ = reader.Close() })

	return file, reader
}

func compressedImage(tb testing.TB, kind byte, payload []byte) []byte {
	tb.Helper()
	p := testImage()
	var encoded []byte
	if kind == 0 {
		encoded = deflatePayload(tb, payload)
	} else {
		writer, err := zstd.NewWriter(nil, zstd.WithEncoderConcurrency(1), zstd.WithEncoderCRC(true))
		if err != nil {
			tb.Fatal(err)
		}
		encoded = writer.EncodeAll(payload, nil)
		writer.Close()
		binary.BigEndian.PutUint64(p[72:], 1<<3)
		binary.BigEndian.PutUint32(p[100:], 112)
		p[104] = 1
	}
	// Exercise unaligned compressed offsets and arbitrary final-sector padding.
	offset := 5*testCluster + 37
	end := offset + len(encoded)
	sectors := (end-1)/512 - offset/512
	entry := compressedFlag | uint64(sectors)<<54 | uint64(offset)
	binary.BigEndian.PutUint64(p[4*testCluster:], entry)
	copy(p[offset:], encoded)

	return p
}

func deflatePayload(tb testing.TB, payload []byte) []byte {
	tb.Helper()
	var buffer bytes.Buffer
	writer, err := flate.NewWriter(&buffer, flate.BestCompression)
	if err != nil {
		tb.Fatal(err)
	}
	if _, err = writer.Write(payload); err != nil {
		tb.Fatal(err)
	}
	if err = writer.Close(); err != nil {
		tb.Fatal(err)
	}

	return buffer.Bytes()
}

func TestReaderReadAt(t *testing.T) {
	t.Parallel()
	p := testImage()
	file, r := openTestImage(t, p)
	expected := make([]byte, r.Size())
	copy(expected, p[5*testCluster:])
	got := make([]byte, len(expected))
	if n, err := r.ReadAt(got, 0); err != nil || n != len(got) || !bytes.Equal(got, expected) {
		t.Fatalf("ReadAt: n=%d err=%v equal=%v", n, err, bytes.Equal(got, expected))
	}
	tail := make([]byte, 100)
	if n, err := r.ReadAt(tail, r.Size()-37); n != 37 || !errors.Is(err, io.EOF) {
		t.Fatalf("EOF read=%d,%v", n, err)
	}
	if n, err := r.ReadAt(tail, -1); n != 0 || err == nil {
		t.Fatal("negative offset accepted")
	}
	if _, err := r.ReadAt(tail, r.Size()); !errors.Is(err, io.EOF) {
		t.Fatalf("past EOF: %v", err)
	}
	if err := r.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := file.Stat(); err != nil {
		t.Fatalf("Close closed source: %v", err)
	}
}

func TestReaderCompressed(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name string
		kind byte
	}{{"deflate", 0}, {"zstd", 1}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			payload := bytes.Repeat([]byte("abcd"), testCluster/4)
			_, r := openTestImage(t, compressedImage(t, tt.kind, payload))
			got := make([]byte, testCluster-34)
			if n, err := r.ReadAt(got, 17); err != nil || n != len(got) || !bytes.Equal(got, payload[17:testCluster-17]) {
				t.Fatalf("compressed read=%d,%v equal=%v", n, err, bytes.Equal(got, payload[17:testCluster-17]))
			}
			ext, err := r.Extent(17, r.Size()-17)
			if err != nil || ext.Start != 17 || ext.Length != testCluster-17 || !ext.Compressed {
				t.Fatalf("extent=%+v,%v", ext, err)
			}
		})
	}
}

func TestReaderRejectsInvalidCompression(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name   string
		kind   byte
		length int
	}{
		{"short-deflate", 0, testCluster - 1},
		{"long-deflate", 0, testCluster + 1},
		{"short-zstd", 1, testCluster - 1},
		{"long-zstd", 1, testCluster + 1},
	} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, r := openTestImage(t, compressedImage(t, tt.kind, bytes.Repeat([]byte{42}, tt.length)))
			if _, err := r.ReadAt(make([]byte, 1), 0); err == nil {
				t.Fatal("malformed cluster accepted even by partial read")
			}
		})
	}
	t.Run("checksum", func(t *testing.T) {
		t.Parallel()
		p := compressedImage(t, 1, bytes.Repeat([]byte{42}, testCluster))
		offset := 5*testCluster + 37
		frame, err := singleZstdFrame(p[offset:], testCluster)
		if err != nil {
			t.Fatal(err)
		}
		p[offset+len(frame)-1] ^= 255
		_, r := openTestImage(t, p)
		if _, err = r.ReadAt(make([]byte, 1), 0); err == nil {
			t.Fatal("corrupt checksum accepted")
		}
	})
	t.Run("truncated", func(t *testing.T) {
		t.Parallel()
		p := compressedImage(t, 0, bytes.Repeat([]byte{42}, testCluster))
		clear(p[5*testCluster+37:])
		_, r := openTestImage(t, p)
		if _, err := r.ReadAt(make([]byte, 1), 0); err == nil {
			t.Fatal("truncated DEFLATE accepted")
		}
	})
}

func TestReaderExtendedL2(t *testing.T) {
	t.Parallel()
	p := testImage()
	binary.BigEndian.PutUint64(p[72:], 1<<4)
	// Allocate subclusters 0 and 2, explicitly zero subcluster 1.
	binary.BigEndian.PutUint64(p[4*testCluster+8:], uint64(2)<<32|5)
	_, r := openTestImage(t, p)
	got := make([]byte, testCluster)
	if _, err := r.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	expected := make([]byte, testCluster)
	copy(expected[:2048], p[5*testCluster:5*testCluster+2048])
	copy(expected[4096:6144], p[5*testCluster+4096:5*testCluster+6144])
	if !bytes.Equal(got, expected) {
		t.Fatal("extended subcluster data mismatch")
	}
	ext, err := r.Extent(1000, 10000)
	if err != nil || ext.Length != 1048 || !ext.Allocated || ext.Zero {
		t.Fatalf("extent=%+v,%v", ext, err)
	}
	ext, err = r.Extent(2048, 10000)
	if err != nil || ext.Length != 2048 || !ext.Zero {
		t.Fatalf("zero extent=%+v,%v", ext, err)
	}
}

func TestReaderSharedClusters(t *testing.T) {
	t.Parallel()
	p := testImage()
	binary.BigEndian.PutUint16(p[2*testCluster+10:], 2)
	binary.BigEndian.PutUint64(p[4*testCluster:], 5*testCluster)
	binary.BigEndian.PutUint64(p[4*testCluster+8:], 5*testCluster)
	_, r := openTestImage(t, p)
	got := make([]byte, 2*testCluster)
	if _, err := r.ReadAt(got, 0); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got[:testCluster], got[testCluster:]) {
		t.Fatal("shared cluster differs")
	}
}

func TestReaderRefcountWidths(t *testing.T) {
	t.Parallel()
	for _, tt := range []struct {
		name  string
		order uint32
	}{{"1", 0}, {"2", 1}, {"4", 2}, {"8", 3}, {"16", 4}, {"32", 5}, {"64", 6}} {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := testImage()
			binary.BigEndian.PutUint32(p[96:], tt.order)
			clear(p[2*testCluster : 3*testCluster])
			width := 1 << tt.order
			for i := 0; i < 6; i++ {
				if width < 8 {
					p[2*testCluster+i*width/8] |= 1 << ((i * width) % 8)
				} else {
					p[2*testCluster+(i+1)*width/8-1] = 1
				}
			}
			_, r := openTestImage(t, p)
			if _, err := r.ReadAt(make([]byte, 10), 0); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestReaderRejectsMalformedMetadata(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func([]byte)
	}{
		{"backing", func(p []byte) { binary.BigEndian.PutUint64(p[8:], 1024) }},
		{"dirty", func(p []byte) { binary.BigEndian.PutUint64(p[72:], 1) }},
		{"corrupt", func(p []byte) { binary.BigEndian.PutUint64(p[72:], 2) }},
		{"unknown-feature", func(p []byte) { binary.BigEndian.PutUint64(p[72:], 1<<63) }},
		{"cluster-overflow", func(p []byte) { binary.BigEndian.PutUint32(p[20:], 63) }},
		{"l1-overflow", func(p []byte) { binary.BigEndian.PutUint32(p[36:], 0xffffffff) }},
		{"l1-past-file", func(p []byte) { binary.BigEndian.PutUint64(p[40:], 100*testCluster) }},
		{"extension-overflow", func(p []byte) {
			binary.BigEndian.PutUint32(p[104:], 42)
			binary.BigEndian.PutUint32(p[108:], 0xffffffff)
		}},
		{"refcount-overflow", func(p []byte) { binary.BigEndian.PutUint64(p[testCluster:], 0xfffffffffffffe00) }},
		{"refcount-zero", func(p []byte) { binary.BigEndian.PutUint16(p[2*testCluster+10:], 0) }},
		{"metadata-overlap", func(p []byte) { binary.BigEndian.PutUint64(p[3*testCluster:], 2*testCluster|copiedFlag) }},
		{"data-overlap", func(p []byte) { binary.BigEndian.PutUint64(p[4*testCluster:], 3*testCluster|copiedFlag) }},
		{"data-past-file", func(p []byte) { binary.BigEndian.PutUint64(p[4*testCluster:], 6*testCluster|copiedFlag) }},
		{"compressed-copied", func(p []byte) {
			binary.BigEndian.PutUint64(p[4*testCluster:], 5*testCluster|compressedFlag|copiedFlag)
		}},
		{"inconsistent-compression", func(p []byte) { binary.BigEndian.PutUint64(p[72:], 1<<3) }},
		{"extended-overlap", func(p []byte) {
			binary.BigEndian.PutUint64(p[72:], 1<<4)
			binary.BigEndian.PutUint64(p[4*testCluster+8:], uint64(1)<<32|1)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			p := testImage()
			tt.mutate(p)
			path := filepath.Join(t.TempDir(), "bad.qcow2")
			if err := os.WriteFile(path, p, 0o600); err != nil {
				t.Fatal(err)
			}
			file, err := os.Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			if _, err := Open(file); err == nil {
				t.Fatal("malformed image accepted")
			}
		})
	}
}

func TestOpenContextCanceled(t *testing.T) {
	t.Parallel()
	file, _ := openTestImage(t, testImage())
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := OpenContext(ctx, file); !errors.Is(err, context.Canceled) {
		t.Fatalf("OpenContext=%v", err)
	}
}

func TestReaderBitmapMetadata(t *testing.T) {
	t.Parallel()
	p := append(testImage(), make([]byte, 3*testCluster)...)
	binary.BigEndian.PutUint64(p[88:], 1)
	binary.BigEndian.PutUint32(p[104:], 0x23852875)
	binary.BigEndian.PutUint32(p[108:], 24)
	binary.BigEndian.PutUint32(p[112:], 1)
	binary.BigEndian.PutUint64(p[120:], 32)
	binary.BigEndian.PutUint64(p[128:], 6*testCluster)
	binary.BigEndian.PutUint64(p[6*testCluster:], 7*testCluster)
	binary.BigEndian.PutUint32(p[6*testCluster+8:], 1)
	p[6*testCluster+16] = 1
	p[6*testCluster+17] = 9
	binary.BigEndian.PutUint16(p[6*testCluster+18:], 1)
	p[6*testCluster+24] = 'a'
	binary.BigEndian.PutUint64(p[7*testCluster:], 8*testCluster)
	for i := 6; i < 9; i++ {
		binary.BigEndian.PutUint16(p[2*testCluster+i*2:], 1)
	}
	_, r := openTestImage(t, p)
	if _, err := r.ReadAt(make([]byte, 10), 0); err != nil {
		t.Fatal(err)
	}
}

func TestReaderSharedL2Table(t *testing.T) {
	t.Parallel()
	p := testImage()
	const coverage = testCluster * (testCluster / 8)
	binary.BigEndian.PutUint64(p[24:], coverage+testCluster)
	binary.BigEndian.PutUint32(p[36:], 2)
	binary.BigEndian.PutUint64(p[3*testCluster:], 4*testCluster)
	binary.BigEndian.PutUint64(p[3*testCluster+8:], 4*testCluster)
	binary.BigEndian.PutUint64(p[4*testCluster:], 5*testCluster)
	binary.BigEndian.PutUint16(p[2*testCluster+8:], 2)
	binary.BigEndian.PutUint16(p[2*testCluster+10:], 2)
	_, r := openTestImage(t, p)
	got := make([]byte, 100)
	if _, err := r.ReadAt(got, coverage); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, p[5*testCluster:5*testCluster+100]) {
		t.Fatal("shared L2 read mismatch")
	}
}

func TestReaderRejectsUndercountedAliases(t *testing.T) {
	t.Parallel()
	p := testImage()
	binary.BigEndian.PutUint64(p[4*testCluster:], 5*testCluster)
	binary.BigEndian.PutUint64(p[4*testCluster+8:], 5*testCluster)
	path := filepath.Join(t.TempDir(), "alias.qcow2")
	if err := os.WriteFile(path, p, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := Open(file); err == nil {
		t.Fatal("undercounted shared data accepted")
	}
}

func TestReaderRejectsUndercountedSharedL2(t *testing.T) {
	t.Parallel()
	p := testImage()
	const coverage = testCluster * (testCluster / 8)
	binary.BigEndian.PutUint64(p[24:], coverage+testCluster)
	binary.BigEndian.PutUint32(p[36:], 2)
	binary.BigEndian.PutUint64(p[3*testCluster:], 4*testCluster)
	binary.BigEndian.PutUint64(p[3*testCluster+8:], 4*testCluster)
	binary.BigEndian.PutUint64(p[4*testCluster:], 5*testCluster)
	binary.BigEndian.PutUint16(p[2*testCluster+8:], 2)
	// The shared L2 table is counted correctly, but its data cluster is not.
	path := filepath.Join(t.TempDir(), "alias.qcow2")
	if err := os.WriteFile(path, p, 0o600); err != nil {
		t.Fatal(err)
	}
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if _, err := Open(file); err == nil {
		t.Fatal("undercounted data through shared L2 accepted")
	}
}

func TestReaderConcurrentReads(t *testing.T) {
	t.Parallel()
	payload := bytes.Repeat([]byte{91}, testCluster)
	_, r := openTestImage(t, compressedImage(t, 1, payload))
	for i := 0; i < 8; i++ {
		t.Run(string(rune('a'+i)), func(t *testing.T) {
			t.Parallel()
			p := make([]byte, 1024)
			if _, err := r.ReadAt(p, 123); err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(p, payload[:1024]) {
				t.Fatal("concurrent read mismatch")
			}
			if _, err := r.Extent(1, 1024); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func FuzzReaderHeader(f *testing.F) {
	seed := testImage()
	f.Add(seed[:112])
	f.Add([]byte("QFI\xfb"))
	f.Fuzz(func(t *testing.T, header []byte) {
		if len(header) > testCluster {
			t.Skip()
		}
		p := testImage()
		copy(p, header)
		path := filepath.Join(t.TempDir(), "fuzz.qcow2")
		if err := os.WriteFile(path, p, 0o600); err != nil {
			t.Fatal(err)
		}
		file, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}
		defer file.Close()
		r, err := OpenContext(t.Context(), file)
		if err != nil {
			return
		}
		defer r.Close()
		limit := min(r.Size(), int64(8192))
		ext, err := r.Extent(0, limit)
		if err != nil {
			return
		}
		if ext.Length <= 0 || ext.Length > limit || ext.Start != 0 {
			t.Fatalf("invalid extent: %+v", ext)
		}
		_, _ = r.ReadAt(make([]byte, limit), 0)
	})
}
