//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
)

func TestHalfSatdPairGuardPages(t *testing.T) {
	requireSampleAVX2(t)
	for _, shape := range sampleSatdWrapperCases() {
		for _, atEnd := range []bool{false, true} {
			for _, reverse := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/end%v/reverse%v", shape.width, shape.height, atEnd, reverse), func(t *testing.T) {
					enc, stride := sampleSatdWrapperGuardedRows(t, shape.width, shape.height, 1, atEnd)
					first, _ := sampleSatdWrapperGuardedRows(t, shape.width, shape.height, 17, atEnd)
					second, _ := sampleSatdWrapperGuardedRows(t, shape.width, shape.height, 83, atEnd)
					off := 0
					if reverse {
						off = (shape.height - 1) * int(stride)
						stride = -stride
					}
					got0, got1 := satdTwoAVX2(&enc[off], int(stride), &first[off], &second[off], int(stride), shape.width, shape.height)
					want0 := sampleSatdWrapperScalar(enc, off, stride, first, off, stride, shape.width, shape.height)
					want1 := sampleSatdWrapperScalar(enc, off, stride, second, off, stride, shape.width, shape.height)
					if got0 != want0 || got1 != want1 {
						t.Fatalf("got=(%d,%d), want=(%d,%d)", got0, got1, want0, want1)
					}
				})
			}
		}
	}
}
