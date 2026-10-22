//go:build linux && amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

func chromaSSSE3GuardedRows(t *testing.T, rows, width, step int, readOnly bool) ([]byte, int32) {
	t.Helper()
	page := unix.Getpagesize()
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
		start := y * step * page
		for x := range width {
			mapping[start+page-width+x] = byte(y*37 + x*19 + 7)
		}
		if readOnly {
			if err := unix.Mprotect(mapping[start:start+page], unix.PROT_READ); err != nil {
				t.Fatal(err)
			}
		}
		if err := unix.Mprotect(mapping[start+page:start+step*page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	end := (rows-1)*step*page + page
	return mapping[page-width : end : end], int32(step * page)
}

func TestChromaSSSE3ExactRowsGuardPages(t *testing.T) {
	requireChromaSSSE3(t)
	for _, height := range []int32{1, 3, 8, 16} {
		for _, signs := range [][2]int32{{1, 1}, {-1, 1}, {1, -1}, {-1, -1}} {
			t.Run(fmt.Sprintf("height%d/signs%v", height, signs), func(t *testing.T) {
				src, srcStride := chromaSSSE3GuardedRows(t, int(height)+1, 9, 2, true)
				dst, dstStride := chromaSSSE3GuardedRows(t, int(height), 8, 3, false)
				srcOff, dstOff := 0, 0
				if signs[0] < 0 {
					srcOff, srcStride = int(height*srcStride), -srcStride
				}
				if signs[1] < 0 {
					dstOff, dstStride = int((height-1)*dstStride), -dstStride
				}
				for dx := int16(0); dx < 8; dx++ {
					for dy := int16(0); dy < 8; dy++ {
						want := make([]byte, 8*height)
						McChroma_c(src, srcOff, srcStride, want, 0, 8, dx, dy, 8, height)
						McChroma_ssse3(src, srcOff, srcStride, dst, dstOff, dstStride, dx, dy, 8, height)
						for y := int32(0); y < height; y++ {
							for x := range 8 {
								if got := dst[dstOff+int(y*dstStride)+x]; got != want[8*y+int32(x)] {
									t.Fatalf("MV(%d,%d) pixel(%d,%d): got=%d want=%d", dx, dy, x, y, got, want[8*y+int32(x)])
								}
							}
						}
					}
				}
			})
		}
	}
}
