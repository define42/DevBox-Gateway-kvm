//go:build linux && amd64

package common

import (
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
	"golang.org/x/sys/unix"
)

func centerPairsGuardedRows(t *testing.T, height, rowBytes int, atEnd, readOnly bool) ([]byte, int) {
	t.Helper()
	page := unix.Getpagesize()
	mapping, err := unix.Mmap(-1, 0, (2*height+1)*page, unix.PROT_READ|unix.PROT_WRITE, unix.MAP_ANON|unix.MAP_PRIVATE)
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
		start += page - rowBytes
	}
	for y := range height {
		for x := range rowBytes {
			mapping[start+2*y*page+x] = byte(y*73 + x*157 + 11)
		}
	}
	for p := range 2*height + 1 {
		prot := unix.PROT_NONE
		if p&1 != 0 {
			prot = unix.PROT_READ
			if !readOnly {
				prot |= unix.PROT_WRITE
			}
		}
		if err := unix.Mprotect(mapping[p*page:(p+1)*page], prot); err != nil {
			t.Fatal(err)
		}
	}
	end := start + 2*(height-1)*page + rowBytes
	return mapping[start:end:end], 2 * page
}

func TestMcCenterPairsExactRowGuards(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, width := range []int{5, 9, 17} {
		for _, height := range []int{1, 2, 5, 9, 17} {
			for _, atEnd := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/end%v", width, height, atEnd), func(t *testing.T) {
					srcBytes, srcStrideBytes := centerPairsGuardedRows(t, height, 2*(width+5), atEnd, true)
					dst, dstStride := centerPairsGuardedRows(t, height, width, atEnd, false)
					src := unsafe.Slice((*int16)(unsafe.Pointer(unsafe.SliceData(srcBytes))), len(srcBytes)/2)
					srcStride := srcStrideBytes / 2
					mcHorRawRefineAVX2(&dst[0], dstStride, &src[0], srcStride, width, height)
					for y := range height {
						for x := range width {
							want := centerPairsReference(src, y*srcStride+x)
							if got := dst[y*dstStride+x]; got != want {
								t.Fatalf("pixel(%d,%d): got=%d want=%d", x, y, got, want)
							}
						}
					}
				})
			}
		}
	}
}
