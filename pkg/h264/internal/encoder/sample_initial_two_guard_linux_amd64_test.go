//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
)

func TestInitialSadTwoGuardPages(t *testing.T) {
	for _, width := range []int{8, 16} {
		for _, height := range []int{4, 8, 12, 16} {
			for _, atEnd := range []bool{false, true} {
				for _, reverse := range []bool{false, true} {
					t.Run(fmt.Sprintf("%dx%d/end%v/reverse%v", width, height, atEnd, reverse), func(t *testing.T) {
						enc, stride := sampleSatdWrapperGuardedRows(t, width, height, 3, atEnd)
						first, _ := sampleSatdWrapperGuardedRows(t, width, height, 17, atEnd)
						second, _ := sampleSatdWrapperGuardedRows(t, width, height, 99, atEnd)
						off := 0
						if reverse {
							off = (height - 1) * int(stride)
							stride = -stride
						}
						got0, got1 := sadTwoSSE2(&enc[off], int(stride), &first[off], &second[off], int(stride), width, height)
						want0 := initialTwoScalar(enc, off, int(stride), first, off, int(stride), width, height)
						want1 := initialTwoScalar(enc, off, int(stride), second, off, int(stride), width, height)
						if got0 != want0 || got1 != want1 {
							t.Fatalf("got=(%d,%d), want=(%d,%d)", got0, got1, want0, want1)
						}
					})
				}
			}
		}
	}
}
