//go:build linux && amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

func TestPackedSadGuardPages(t *testing.T) {
	for _, test := range packedSadTestCases {
		t.Run(fmt.Sprintf("%dx%d", test.width, test.height), func(t *testing.T) {
			page := unix.Getpagesize()
			src := packedSadGuardedRows(t, test.height, page, 2, 37)
			ref := packedSadGuardedRows(t, test.height, page, 4, 19)
			off := page - test.width
			want := packedSadReference(src, off, 2*page, ref, off, 4*page, test.width, test.height)
			if got := test.fn(src, off, int32(2*page), ref, off, int32(4*page)); got != want {
				t.Fatalf("guarded rows: got %d want %d", got, want)
			}
		})
	}
}

// Each row ends at a guard page and is read-only; distinct row steps ensure
// neither kernel accidentally uses the other input's stride.
func packedSadGuardedRows(t *testing.T, rows, page, step, seed int) []byte {
	t.Helper()
	mapping, err := unix.Mmap(-1, 0, rows*step*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	for y := range rows {
		for x := range page {
			mapping[y*step*page+x] = byte(x*seed + y*53 + y*y)
		}
		if err := unix.Mprotect(mapping[y*step*page:y*step*page+page], unix.PROT_READ); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mprotect(mapping[y*step*page+page:(y+1)*step*page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	end := (rows-1)*step*page + page
	return mapping[:end:end]
}
