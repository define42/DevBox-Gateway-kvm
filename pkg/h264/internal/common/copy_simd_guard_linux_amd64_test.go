//go:build linux && amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

func TestBlockCopySIMDGuardPages(t *testing.T) {
	for _, test := range blockCopyTestCases {
		t.Run(fmt.Sprintf("%dx%d", test.width, test.height), func(t *testing.T) {
			page := unix.Getpagesize()
			srcMap := copyGuardedRows(t, test.height, page)
			dstMap := copyGuardedRows(t, test.height, page)
			for y := range test.height {
				for x := range page {
					srcMap[y*page*2+x] = byte(y*43 + x*29)
					dstMap[y*page*2+x] = 0xa5
				}
				if err := unix.Mprotect(srcMap[y*page*2:y*page*2+page], unix.PROT_READ); err != nil {
					t.Fatal(err)
				}
			}
			// Every row ends immediately at a guard page. SIMD must read and
			// write exactly eight or sixteen bytes, including the final row.
			off := page - test.width
			end := (test.height-1)*page*2 + page
			test.simd(dstMap[:end:end], off, int32(page*2), srcMap[:end:end], off, int32(page*2))
			for y := range test.height {
				for x := range page {
					want := byte(0xa5)
					if x >= off {
						want = srcMap[y*page*2+x]
					}
					if got := dstMap[y*page*2+x]; got != want {
						t.Fatalf("row %d byte %d: got %d want %d", y, x, got, want)
					}
				}
			}
		})
	}
}

func copyGuardedRows(t *testing.T, rows, page int) []byte {
	t.Helper()
	mapping, err := unix.Mmap(-1, 0, rows*page*2, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	for row := range rows {
		if err := unix.Mprotect(mapping[(row*2+1)*page:(row*2+2)*page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	return mapping
}
