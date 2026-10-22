//go:build linux && amd64

package common

import (
	"fmt"
	"testing"

	"golang.org/x/sys/unix"
)

func TestIntegerMotionCopyGuardPages(t *testing.T) {
	for _, mode := range integerMotionCopyModes {
		for _, shape := range [][2]int32{{8, 8}, {8, 16}, {16, 8}, {16, 16}, {8, 4}, {4, 4}} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode.name, shape[0], shape[1]), func(t *testing.T) {
				width, height, page := int(shape[0]), int(shape[1]), unix.Getpagesize()
				srcMap := copyGuardedRows(t, height, page)
				dstMap := copyGuardedRows(t, height, page)
				for y := range height {
					for x := range page {
						srcMap[y*page*2+x] = byte(y*43 + x*29)
						dstMap[y*page*2+x] = 0xa5
					}
					if err := unix.Mprotect(srcMap[y*page*2:y*page*2+page], unix.PROT_READ); err != nil {
						t.Fatal(err)
					}
				}
				off, end := page-width, (height-1)*page*2+page
				mode.fn(srcMap[:end:end], off, int32(page*2), dstMap[:end:end], off, int32(page*2), 8, -16, shape[0], shape[1])
				for y := range height {
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
}
