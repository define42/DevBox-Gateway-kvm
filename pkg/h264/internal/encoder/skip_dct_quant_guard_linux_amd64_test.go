//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestSkipDctQuantExactGuards(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	words := func(t *testing.T, n int) []int16 {
		buffer := idctGuardedBuffer(t, n*2)
		return unsafe.Slice((*int16)(unsafe.Pointer(&buffer[0])), n)
	}
	for _, stride := range []int{0, 1, 8, 19, 127} {
		t.Run(fmt.Sprintf("raw/stride%d", stride), func(t *testing.T) {
			a, b := idctGuardedBuffer(t, 7*stride+8), idctGuardedBuffer(t, 7*stride+8)
			dct, maxima, ff, mf := words(t, 64), words(t, 4), words(t, 8), words(t, 8)
			for i := range a {
				a[i], b[i] = byte(i*29+17), byte(i*13+71)
			}
			copy(ff, g_kiQuantInterFF[32][:])
			copy(mf, g_kiQuantMF[32][:])
			var want [64]int16
			var wantMax [4]int16
			WelsDctFourT4_c(want[:], a, 0, int32(stride), b, 0, int32(stride))
			quantFour4x4MaxSSE2(want[:], ff, mf, wantMax[:])
			skipDctQuant8x8AVX2((*[64]int16)(dct), &a[0], stride, &b[0], stride, (*[8]int16)(ff), (*[8]int16)(mf), (*[4]int16)(maxima))
			if *(*[64]int16)(dct) != want || *(*[4]int16)(maxima) != wantMax {
				t.Fatal("guarded raw output differs")
			}
		})
	}
	for _, stride := range []int32{0, 1, 16, 19, 127} {
		t.Run(fmt.Sprintf("caller/stride%d", stride), func(t *testing.T) {
			s := newSkipDctQuantPixelsState(stride, 32, 0)
			s.source = idctGuardedBuffer(t, 15*int(stride)+16)
			for i := range s.source {
				s.source[i] = 128
			}
			s.cache.SPicData.pEncMb[0], s.cache.SPicData.iEncMbOff[0] = s.source, 0
			s.cache.pCoeffLevel, s.cache.pSkipMb = words(t, 256), idctGuardedBuffer(t, 256)
			for i := range s.cache.pSkipMb {
				s.cache.pSkipMb[i] = 128
			}
			levelBytes := idctGuardedBuffer(t, int(unsafe.Sizeof(SDCTCoeff{})))
			s.cache.pDct = (*SDCTCoeff)(unsafe.Pointer(&levelBytes[0]))
			for i := range s.cache.pDct.iLumaBlock {
				s.cache.pDct.iLumaBlock[i] = -12345
			}
			if !welsTryPYskipPixels(&s.ctx, &s.mb, &s.cache) {
				t.Fatal("guarded equal pixels must accept")
			}
			for _, v := range s.cache.pCoeffLevel {
				if v != 0 {
					t.Fatal("guarded zero residual differs")
				}
			}
			for _, v := range s.cache.pDct.iLumaBlock {
				if v != -12345 {
					t.Fatal("zero maxima unexpectedly scanned levels")
				}
			}
		})
	}
}
