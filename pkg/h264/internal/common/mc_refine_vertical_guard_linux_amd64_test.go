//go:build linux && amd64

package common

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestMcVerReuseAVX2GuardPages(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, atEnd := range []bool{false, true} {
		for _, raw := range []bool{false, true} {
			widths := []int{4, 5, 8, 9, 10, 13, 14, 16, 17, 21, 22, 24}
			for _, width := range widths {
				for _, height := range []int{1, 2, 5, 6, 9, 17, 18, 31} {
					t.Run(fmt.Sprintf("raw%v/%dx%d/end%v", raw, width, height, atEnd), func(t *testing.T) {
						// A tightly packed block reaches both the last legal
						// source row and the last legal column at the guard.
						src := mcAVX2GuardedBuffer(t, (height+5)*width, atEnd)
						for i := range src {
							src[i] = byte(i*57 + 29)
						}
						if raw {
							dstBytes := mcAVX2GuardedBuffer(t, 2*height*width, atEnd)
							dst := unsafe.Slice((*int16)(unsafe.Pointer(unsafe.SliceData(dstBytes))), height*width)
							mcVerRawReuseAVX2(&dst[0], width, &src[2*width], width, width, height)
							for row := 0; row < height; row++ {
								for col := 0; col < width; col++ {
									want := int16(FilterInput8bitWithStride_c(src, (row+2)*width+col, int32(width)))
									if got := dst[row*width+col]; got != want {
										t.Fatalf("row %d col %d: got %d want %d", row, col, got, want)
									}
								}
							}
						} else {
							dst := mcAVX2GuardedBuffer(t, height*width, atEnd)
							want := make([]byte, len(dst))
							McHorVer02_c(src, 2*width, int32(width), want, 0,
								int32(width), int32(width), int32(height))
							mcVerReuseAVX2(&dst[0], width, &src[2*width], width, width, height)
							if !bytes.Equal(dst, want) {
								t.Fatal("guarded clipped outputs differ")
							}
						}
					})
				}
			}
		}
	}
}
