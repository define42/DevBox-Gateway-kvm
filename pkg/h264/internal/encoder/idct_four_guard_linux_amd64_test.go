//go:build linux && amd64

package encoder

import (
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
	"golang.org/x/sys/unix"
)

func TestIDctFourPackedGuardPages(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	pred, predStride := sampleSatd8GuardedRows(t, 8, 17)
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, 16*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	for y := range 8 {
		if err := unix.Mprotect(mapping[(2*y+1)*page:(2*y+2)*page], unix.PROT_NONE); err != nil {
			t.Fatal(err)
		}
	}
	rec := mapping[page-8 : 15*page : 15*page]
	coeffBytes := idctGuardedBuffer(t, 128)
	coeff := (*[64]int16)(unsafe.Pointer(&coeffBytes[0]))
	for i := range coeff {
		coeff[i] = int16(i*701 - 20000)
	}
	before := *coeff
	var densePred, want [64]byte
	for y := range 8 {
		copy(densePred[y*8:y*8+8], pred[y*int(predStride):y*int(predStride)+8])
	}
	WelsIDctFourT4Rec_c(want[:], 0, 8, densePred[:], 0, 8, coeff[:])
	idctFourT4RecPackedAVX2(&rec[0], 2*page, &pred[0], int(predStride), coeff)
	for y := range 8 {
		for x := range 8 {
			if got := rec[y*2*page+x]; got != want[y*8+x] {
				t.Fatalf("pixel %d,%d: got %d, want %d", x, y, got, want[y*8+x])
			}
		}
	}
	if *coeff != before {
		t.Fatal("coefficients were modified")
	}
}
