//go:build linux && amd64

package encoder

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestQuantMaxGuardPages(t *testing.T) {
	words := func(n int) []int16 {
		buffer := idctGuardedBuffer(t, n*2)
		return unsafe.Slice((*int16)(unsafe.Pointer(&buffer[0])), n)
	}
	for _, direct := range []bool{false, true} {
		if direct && !cpu.X86.HasAVX2 {
			continue
		}
		dct, ff, mf, maxima := words(64), words(8), words(8), words(4)
		for i := range dct {
			dct[i] = int16(i*701 - 20000)
		}
		copy(ff, g_kiQuantInterFF[20][:])
		copy(mf, g_kiQuantMF[20][:])
		var want [64]int16
		copy(want[:], dct)
		var wantMax [4]int16
		WelsQuantFour4x4Max_c(want[:], ff, mf, wantMax[:])
		if direct {
			quantFour4x4MaxAVX2((*[64]int16)(dct), (*[8]int16)(ff), (*[8]int16)(mf), (*[4]int16)(maxima))
		} else {
			WelsQuantFour4x4Max_sse2(dct, ff, mf, maxima)
		}
		if *(*[64]int16)(dct) != want || *(*[4]int16)(maxima) != wantMax || *(*[8]int16)(ff) != g_kiQuantInterFF[20] || *(*[8]int16)(mf) != g_kiQuantMF[20] {
			t.Fatalf("direct%t: coefficients, maxima or source tables differ", direct)
		}
	}
}
