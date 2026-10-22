//go:build linux && amd64

package encoder

import (
	"bytes"
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

// Top and left rows occupy read-only pages separated by inaccessible pages.
// HU needs a single byte per row; HD/VR need a contiguous 4/5-byte top edge.
// The checked gather performs byte accesses only; raw kernels receive values.
func intra4DirectionalGuardRef(t *testing.T, mode string, atEnd bool) ([]byte, int, int32) {
	t.Helper()
	page := unix.Getpagesize()
	const rows = 5
	mapping, err := unix.Mmap(-1, 0, (2*rows+1)*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	width := 4
	if mode == "VR" {
		width = 5
	} else if mode == "HU" {
		width = 1
	}
	start := page
	if atEnd {
		start += page - width
	}
	for row := 0; row < rows; row++ {
		for x := 0; x < width; x++ {
			mapping[start+row*2*page+x] = byte(17 + row*41 + x*63)
		}
	}
	for p := 0; p < 2*rows+1; p++ {
		prot := unix.PROT_NONE
		if p&1 != 0 {
			prot = unix.PROT_READ
		}
		if err := unix.Mprotect(mapping[p*page:(p+1)*page], prot); err != nil {
			t.Fatal(err)
		}
	}
	end := start + (rows-1)*2*page + width
	return mapping[start:end:end], 2*page + 1, int32(2 * page)
}

func TestIntra4DirectionalGuardPages(t *testing.T) {
	for _, tc := range intra4DirectionalCases {
		for _, atEnd := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/end%t", tc.name, atEnd), func(t *testing.T) {
				ref, off, stride := intra4DirectionalGuardRef(t, tc.name, atEnd)
				want := make([]byte, 16)
				tc.scalar(want, 0, ref, off, stride)
				dst, _ := intraDCGuardedBuffer(t, 16, atEnd)
				tc.fast(dst, 0, ref, off, stride)
				if !bytes.Equal(dst, want) {
					t.Fatal("guarded checked output differs")
				}
				var packed uint64
				positions := tc.reads(int(stride))
				for i, source := range tc.packing {
					packed |= uint64(ref[off+positions[source]]) << (8 * i)
				}
				tc.raw(&dst[0], packed)
				if !bytes.Equal(dst, want) {
					t.Fatal("guarded raw output differs")
				}
			})
		}
	}
}
