//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

// Guard each row separately on both sides. Moving the row to either end of
// its read-only page detects horizontal overreads and accidental writes.
func sampleSatdWrapperGuardedRows(t *testing.T, width, height, seed int, atEnd bool) ([]byte, int32) {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, height*3*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	start := page
	if atEnd {
		start += page - width
	}
	for y := range height {
		base := y * 3 * page
		for x := range width {
			mapping[base+start+x] = byte(y*31 + x*17 + seed)
		}
		if err := unix.Mprotect(mapping[base:base+page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mprotect(mapping[base+page:base+2*page], unix.PROT_READ); err != nil {
			t.Fatal(err)
		}
		if err := unix.Mprotect(mapping[base+2*page:base+3*page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	end := (height-1)*3*page + start + width
	return mapping[start:end:end], int32(3 * page)
}

func TestSampleSatdWrapperGuardPages(t *testing.T) {
	requireSampleAVX2(t)
	for _, tc := range sampleSatdWrapperCases() {
		for _, atEnd := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/end%v/reverse%v", tc.width, tc.height, atEnd, reverse), func(t *testing.T) {
					a, stride := sampleSatdWrapperGuardedRows(t, tc.width, tc.height, 3, atEnd)
					ref, _ := sampleSatdWrapperGuardedRows(t, tc.width, tc.height, 29, atEnd)
					off := 0
					if reverse {
						off = (tc.height - 1) * int(stride)
						stride = -stride
					}
					got := tc.fn(a, off, stride, ref, off, stride)
					legacy := sampleSatdBlockAVX2(a, off, stride, ref, off, stride, tc.width, tc.height)
					want := sampleSatdWrapperScalar(a, off, stride, ref, off, stride, tc.width, tc.height)
					if got != want || got != legacy {
						t.Fatalf("got=%d legacy=%d scalar=%d", got, legacy, want)
					}
					if got := tc.fn(a, off, stride, a, off, stride); got != 0 {
						t.Fatalf("identical planes: got=%d", got)
					}
				})
			}
		}
	}
}
