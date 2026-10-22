//go:build amd64

package common

import "testing"

// Isolated source and reference impulses distinguish every contribution in the
// rolling three-row and paired-row schedules, including their first/last rows.
func TestSampleSadFourRowsEveryPixel(t *testing.T) {
	for _, tc := range sadFourSIMDCases {
		t.Run(tc.name, func(t *testing.T) {
			aStride, refStride := tc.width+3, tc.width+5
			a := make([]byte, (tc.height-1)*aStride+tc.width)
			ref := make([]byte, (tc.height+2)*refStride)
			refOff := refStride + 1
			check := func(plane, x, y int) {
				t.Helper()
				want := sadFourReference(a, 0, aStride, ref, refOff, refStride, tc.width, tc.height)
				got := [6]int32{-123, -1, -1, -1, -1, -456}
				tc.batched(a, 0, int32(aStride), ref, refOff, int32(refStride), got[1:5])
				if [4]int32(got[1:5]) != want || got[0] != -123 || got[5] != -456 {
					t.Fatalf("plane=%d pixel=(%d,%d): got=%v want=%v", plane, x, y, got, want)
				}
			}
			for _, amplitude := range []byte{1, 127, 255} {
				for y := range tc.height {
					for x := range tc.width {
						off := y*aStride + x
						a[off] = amplitude
						check(0, x, y)
						a[off] = 0
					}
				}
				for y := -1; y <= tc.height; y++ {
					for x := -1; x <= tc.width; x++ {
						off := refOff + y*refStride + x
						ref[off] = amplitude
						check(1, x, y)
						ref[off] = 0
					}
				}
			}
		})
	}
}
