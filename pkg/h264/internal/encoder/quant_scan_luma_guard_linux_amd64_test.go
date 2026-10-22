//go:build linux && amd64

package encoder

import (
	"slices"
	"testing"
	"unsafe"
)

func TestQuantScanLumaExactBufferGuards(t *testing.T) {
	words := func(n int) []int16 {
		buf := idctGuardedBuffer(t, n*2)
		return unsafe.Slice((*int16)(unsafe.Pointer(&buf[0])), n)
	}
	dct, levels, ff, mf, maxima := words(256), words(256), words(8), words(8), words(16)
	maskBytes := idctGuardedBuffer(t, 32)
	masks := unsafe.Slice((*uint16)(unsafe.Pointer(&maskBytes[0])), 16)
	for i := range dct {
		dct[i] = int16(i*701 - 20000)
	}
	copy(ff, g_kiQuantInterFF[20][:])
	copy(mf, g_kiQuantMF[20][:])
	want := slices.Clone(dct)
	var wantLevels [256]int16
	var wantMax [16]int16
	var wantMask [16]uint16
	quantScanLumaFourCalls(want, wantLevels[:], ff, mf, wantMax[:], wantMask[:])
	quantScanLuma(dct, levels, ff, mf, maxima, masks)
	if !slices.Equal(dct, want) || !slices.Equal(levels, wantLevels[:]) ||
		!slices.Equal(maxima, wantMax[:]) || !slices.Equal(masks, wantMask[:]) {
		t.Fatal("guarded luma batch differs from four calls")
	}
}
