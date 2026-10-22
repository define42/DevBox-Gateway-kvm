//go:build linux && amd64

package encoder

import (
	"testing"
	"unsafe"
)

func TestHadamardQuant2x2GuardPages(t *testing.T) {
	words := func(n int) []int16 {
		buffer := idctGuardedBuffer(t, n*2)
		return unsafe.Slice((*int16)(unsafe.Pointer(&buffer[0])), n)
	}
	rs, dct, block := words(49), words(4), words(4)
	for i := range rs {
		rs[i] = int16(i*1701 + 13)
	}
	var wantRS [49]int16
	copy(wantRS[:], rs)
	var wantDCT, wantBlock [4]int16
	want := WelsHadamardQuant2x2_c(wantRS[:], 32767, 32767, wantDCT[:], wantBlock[:])
	got := hadamardQuant2x2(rs, 32767, 32767, dct, block)
	if got != want || *(*[49]int16)(rs) != wantRS || *(*[4]int16)(dct) != wantDCT || *(*[4]int16)(block) != wantBlock {
		t.Fatal("guarded minimum input/output parity differs")
	}
}
