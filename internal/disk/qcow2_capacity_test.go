package disk

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestQCOW2FullCapacityValidation(t *testing.T) {
	t.Parallel()
	// One 512-byte refcount table covers 64 * 256 host clusters. The fixture
	// already maps two of its 128 guest clusters and both required L2 tables.
	const coverage = 64 * 256 * layoutClusterSize
	for _, tc := range []struct {
		name     string
		fileSize int64
		edit     func([]byte)
		wantErr  bool
	}{
		{name: "exact space including refcount block", fileSize: coverage - 127*layoutClusterSize},
		{name: "no space for refcount block", fileSize: coverage - 126*layoutClusterSize, wantErr: true},
		{name: "partial host cluster rounds up", fileSize: coverage - 127*layoutClusterSize + 1, wantErr: true},
		{name: "refcount block at range boundary", fileSize: 63*256*layoutClusterSize - layoutClusterSize},
		{name: "full table with missing data", fileSize: coverage, wantErr: true},
		{
			name: "missing L2 also needs space", fileSize: coverage - 130*layoutClusterSize,
			edit: func(b []byte) { clear(b[3*layoutClusterSize : 4*layoutClusterSize]) }, wantErr: true,
		},
		{
			name: "missing L2 fits", fileSize: coverage - 131*layoutClusterSize,
			edit: func(b []byte) { clear(b[3*layoutClusterSize : 4*layoutClusterSize]) },
		},
		{
			name: "allocated zero cluster needs no growth", fileSize: coverage - 127*layoutClusterSize,
			edit: func(b []byte) { b[4*layoutClusterSize+7] |= byte(qcow2OFlagZero) },
		},
		{
			name: "unallocated zero cluster needs growth", fileSize: coverage - 127*layoutClusterSize,
			edit: func(b []byte) { binary.BigEndian.PutUint64(b[4*layoutClusterSize:], qcow2OFlagZero) }, wantErr: true,
		},
		{
			name: "mapping past virtual capacity is not credited", fileSize: coverage,
			edit: func(b []byte) { binary.BigEndian.PutUint64(b[24:], layoutClusterSize+1) }, wantErr: true,
		},
		{
			name: "fully mapped disk needs no growth", fileSize: coverage + layoutClusterSize,
			edit: func(b []byte) { binary.BigEndian.PutUint64(b[24:], layoutClusterSize) },
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			data := qcow2LayoutFixture(qcow2Version3)
			if tc.edit != nil {
				tc.edit(data)
			}
			path := filepath.Join(t.TempDir(), "disk.qcow2")
			if err := os.WriteFile(path, data, 0o600); err != nil {
				t.Fatal(err)
			}
			if err := os.Truncate(path, tc.fileSize); err != nil {
				t.Fatal(err)
			}
			before := readWriteTestFile(t, path)
			capacity := int64(binary.BigEndian.Uint64(data[24:32]))
			for _, open := range []func(string) (Image, error){
				OpenReadOnly, Open, func(path string) (Image, error) { return OpenQCOW2WithSize(path, capacity) },
			} {
				img, err := open(path)
				if img != nil {
					_ = img.Close()
				}
				if tc.wantErr && !errors.Is(err, errQCOW2Capacity) {
					t.Fatalf("Open = %v, want full-capacity error", err)
				}
				if !tc.wantErr && err != nil {
					t.Fatal(err)
				}
			}
			if !bytes.Equal(before, readWriteTestFile(t, path)) {
				t.Fatal("capacity validation modified the image")
			}
			if tc.wantErr {
				return
			}
			img, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer img.Close()
			want := bytes.Repeat([]byte{0x5a}, int(capacity))
			mustWriteAt(t, img, want, 0)
			if err := img.Sync(); err != nil {
				t.Fatal(err)
			}
			if err := img.Close(); err != nil {
				t.Fatal(err)
			}
			reopened, err := Open(path)
			if err != nil {
				t.Fatal(err)
			}
			defer reopened.Close()
			assertReadAt(t, reopened, want, 0)
		})
	}
}
