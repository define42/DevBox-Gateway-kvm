//go:build linux && amd64

package encoder

import (
	"testing"
	"unsafe"
)

func TestQuantScanGuardPages(t *testing.T) {
	for _, ac := range []bool{false, true} {
		words := func(n int) []int16 {
			buf := idctGuardedBuffer(t, n*2)
			return unsafe.Slice((*int16)(unsafe.Pointer(&buf[0])), n)
		}
		dct, levels, ff, mf, maxima := words(64), words(64), words(8), words(8), words(4)
		maskBytes := idctGuardedBuffer(t, 8)
		masks := unsafe.Slice((*uint16)(unsafe.Pointer(&maskBytes[0])), 4)
		for i := range dct {
			dct[i] = int16(i*701 - 20000)
		}
		copy(ff, g_kiQuantInterFF[20][:])
		copy(mf, g_kiQuantMF[20][:])
		quantScanFour4x4(dct, levels, ff, mf, maxima, masks, ac)
	}
}
