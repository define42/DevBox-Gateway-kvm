//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

// Every row ends at an unreadable page, so a wider load cannot hide inside
// the next row's valid storage. Read-only pages also reject accidental stores.
func sampleSatd8GuardedRows(t *testing.T, height, seed int) ([]byte, int32) {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, height*2*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	for y := range height {
		row := y * 2 * page
		for x := range 8 {
			mapping[row+page-8+x] = byte(y*31 + x*17 + seed)
		}
		if err := unix.Mprotect(mapping[row:row+page], unix.PROT_READ); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mprotect(mapping[row+page:row+2*page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	end := (height-1)*2*page + page
	return mapping[page-8 : end : end], int32(2 * page)
}

func TestSampleSatd8RowsGuardPages(t *testing.T) {
	requireSampleAVX2(t)
	for _, height := range []int{4, 8, 12, 16} {
		for _, reverse := range []bool{false, true} {
			t.Run(fmt.Sprintf("height%d/reverse%v", height, reverse), func(t *testing.T) {
				var inputs [3][]byte
				var offsets [3]int
				var strides [3]int32
				for n := range inputs {
					inputs[n], strides[n] = sampleSatd8GuardedRows(t, height, n*43)
					if reverse {
						offsets[n] = (height - 1) * int(strides[n])
						strides[n] = -strides[n]
					}
				}
				for _, average := range []bool{false, true} {
					got := sampleSatd8Checked(inputs, offsets, strides, height, average)
					want := sampleSatd8Matrix(inputs, offsets, strides, height, average)
					if got != want {
						t.Fatalf("average=%v: got %d, want %d", average, got, want)
					}
				}
				// All operands can share the same read-only plane.
				inputs[1], inputs[2] = inputs[0], inputs[0]
				for _, average := range []bool{false, true} {
					if got := sampleSatd8Checked(inputs, offsets, strides, height, average); got != 0 {
						t.Fatalf("identical planes average=%v: got %d, want 0", average, got)
					}
				}
			})
		}
	}
}
