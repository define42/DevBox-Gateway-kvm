//go:build linux && amd64

package encoder

import (
	"fmt"
	"testing"

	"golang.org/x/sys/cpu"
	"golang.org/x/sys/unix"
)

func TestIntraVaaSIMDExactRowsGuardPages(t *testing.T) {
	if !cpu.X86.HasSSSE3 {
		t.Skip("SSSE3 unavailable")
	}
	page := unix.Getpagesize()
	for _, atEnd := range []bool{false, true} {
		t.Run(fmt.Sprintf("atEnd%v", atEnd), func(t *testing.T) {
			mapping, err := unix.Mmap(-1, 0, 33*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := unix.Munmap(mapping); err != nil {
					t.Error(err)
				}
			})
			start := page
			if atEnd {
				start += page - 16
			}
			for y := range 16 {
				for x := range 16 {
					mapping[start+y*2*page+x] = byte(y*37 + x*19 + 7)
				}
			}
			for p := range 33 {
				prot := unix.PROT_NONE
				if p&1 != 0 {
					prot = unix.PROT_READ
				}
				if err := unix.Mprotect(mapping[p*page:(p+1)*page], prot); err != nil {
					t.Fatal(err)
				}
			}
			end := start + 30*page + 16
			src := mapping[start:end:end]
			for _, stride := range []int32{int32(2 * page), -int32(2 * page), 0} {
				off := 0
				if stride < 0 {
					off = 30 * page
				}
				want := intraVaaReference(src, off, stride)
				if got := AnalysisVaaInfoIntra_ssse3(src, off, stride); got != want {
					t.Fatalf("stride=%d: got=%d want=%d", stride, got, want)
				}
			}
		})
	}
}
