//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
	"golang.org/x/sys/unix"
)

func intraACGuardedWords(t *testing.T, count int, atEnd bool) ([]int16, []byte) {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, 3*page, unix.PROT_NONE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	middle := mapping[page : 2*page]
	if err := unix.Mprotect(middle, unix.PROT_READ|unix.PROT_WRITE); err != nil {
		t.Fatal(err)
	}
	start := page
	if atEnd {
		start = 2*page - count*2
	}
	return unsafe.Slice((*int16)(unsafe.Pointer(&mapping[start])), count), middle
}

func TestIntraACExactSpanGuards(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 required")
	}
	for _, atEnd := range []bool{false, true} {
		t.Run(fmt.Sprintf("atEnd%v", atEnd), func(t *testing.T) {
			dct, _ := intraACGuardedWords(t, 256, atEnd)
			levels, _ := intraACGuardedWords(t, 256, atEnd)
			ff, ffPage := intraACGuardedWords(t, 8, atEnd)
			mf, mfPage := intraACGuardedWords(t, 8, atEnd)
			maskWords, _ := intraACGuardedWords(t, 16, atEnd)
			masks := (*[16]uint16)(unsafe.Pointer(&maskWords[0]))
			var input, want, wantLevels [256]int16
			for i := range input {
				input[i] = int16(i*1701 - 32768)
			}
			for i := range ff {
				ff[i], mf[i] = int16(i*8191-32768), int16(i*4093-32768)
			}
			want = input
			wantMask := intraACUnsignedReference(want[:], wantLevels[:], ff, mf)
			if err := unix.Mprotect(ffPage, unix.PROT_READ); err != nil {
				t.Fatal(err)
			}
			if err := unix.Mprotect(mfPage, unix.PROT_READ); err != nil {
				t.Fatal(err)
			}
			funcs := SWelsFuncPtrList{pfQuantizationFour4x4: WelsQuantFour4x4_sse2}
			for _, raw := range []bool{false, true} {
				copy(dct, input[:])
				for i := range levels {
					levels[i] = 12345
				}
				if raw {
					quantScanIntraLumaAVX2((*[256]int16)(dct), (*[256]int16)(levels), (*[8]int16)(ff), (*[8]int16)(mf), masks)
				} else if !tryQuantScanIntraLuma(&funcs, dct, levels, ff, mf, masks) {
					t.Fatal("guarded exact spans declined")
				}
				if *(*[256]int16)(dct) != want || *(*[256]int16)(levels) != wantLevels || *masks != wantMask {
					t.Fatalf("raw%v guarded output differs", raw)
				}
			}
		})
	}
}
