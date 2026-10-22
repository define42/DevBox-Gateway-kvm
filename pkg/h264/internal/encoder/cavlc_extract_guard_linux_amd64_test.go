//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
	"unsafe"
)

func TestCavlcExtractGuardPages(t *testing.T) {
	for _, end := range []int32{3, 14, 15} {
		for _, length := range []int{int(end + 1), 16} {
			t.Run(fmt.Sprintf("end%d/len%d", end, length), func(t *testing.T) {
				input := idctGuardedBuffer(t, length*2)
				coeff := unsafe.Slice((*int16)(unsafe.Pointer(&input[0])), length)
				levelBytes := idctGuardedBuffer(t, 32)
				levels := (*[16]int16)(unsafe.Pointer(&levelBytes[0]))
				runBytes := idctGuardedBuffer(t, 16)
				runs := (*[16]uint8)(runBytes)
				for i := range coeff {
					coeff[i] = int16(i*17 - 91)
				}
				var count, wantCount int32
				var wantLevel [16]int16
				var wantRun [16]uint8
				wantZeros := cavlcParamCal_ref(coeff, wantRun[:], wantLevel[:], &wantCount, end)
				zeros := cavlcExtract(coeff, runs, levels, &count, end)
				if zeros != wantZeros || count != wantCount || *levels != wantLevel || *runs != wantRun {
					t.Fatal("guarded extraction differs from scalar")
				}
			})
		}
	}
}
