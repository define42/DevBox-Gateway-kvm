//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/unix"
)

// Put every visible row against a guard, on either side of its readable
// page. No wider read can hide in row padding, and inputs cannot be written.
func sampleFourGuardedRows(t *testing.T, width, height, seed int, before bool) ([]byte, int) {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, (2*height+1)*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	first := 2*page - width
	if before {
		first = page
	}
	for y := range height {
		for x := range width {
			mapping[first+y*2*page+x] = byte(seed*37 + x*17 + y*31)
		}
	}
	for i := range 2*height + 1 {
		protection := unix.PROT_NONE
		if i%2 != 0 {
			protection = unix.PROT_READ
		}
		if err := unix.Mprotect(mapping[i*page:(i+1)*page], protection); err != nil {
			t.Fatal(err)
		}
	}
	last := first + (height-1)*2*page + width
	return mapping[first:last:last], 2 * page
}

func TestSampleAvgSatdFourGuardPages(t *testing.T) {
	requireSampleAVX2(t)
	for _, width := range []int{8, 16} {
		for _, height := range []int{4, 8, 12, 16} {
			for _, reverse := range []bool{false, true} {
				for _, before := range []bool{false, true} {
					t.Run(fmt.Sprintf("%dx%d/reverse%v/before%v", width, height, reverse, before), func(t *testing.T) {
						enc, encStride := sampleFourGuardedRows(t, width, height, 0, before)
						encOff := 0
						if reverse {
							encOff, encStride = (height-1)*encStride, -encStride
						}
						var candidates [4]sampleAvgSatdCandidate
						var want [4]int32
						for i := range candidates {
							a, aStride := sampleFourGuardedRows(t, width, height, 2*i+1, before)
							b, bStride := sampleFourGuardedRows(t, width, height, 2*i+2, before)
							aOff, bOff := 0, 0
							if reverse {
								aOff, aStride = (height-1)*aStride, -aStride
								bOff, bStride = (height-1)*bStride, -bStride
							}
							candidates[i] = sampleAvgSatdCandidate{&a[aOff], &b[bOff], aStride, bStride}
							want[i] = sampleAvgSatdReference(enc, encOff, int32(encStride), a, aOff, int32(aStride),
								b, bOff, int32(bStride), int32(width), int32(height))
						}
						output := idctGuardedBuffer(t, 16)
						costs := (*[4]int32)(unsafe.Pointer(&output[0]))
						satdAvgFourAVX2(&enc[encOff], encStride, &candidates, width, height, costs)
						if *costs != want {
							t.Fatalf("got=%v want=%v", *costs, want)
						}
					})
				}
			}
		}
	}
}
