//go:build linux && amd64

package common

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestMcAVX2RefinementGuardPages(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, kernel := range mcAVX2TestKernels {
		for _, width := range []int{4, 5, 8, 9, 16, 17} {
			for _, height := range []int{1, 5, 9, 17} {
				for _, atEnd := range []bool{false, true} {
					t.Run(fmt.Sprintf("%s/%dx%d/end%v", kernel.name, width, height, atEnd), func(t *testing.T) {
						stride := width + 5
						if kernel.name == "vertical" {
							stride = width
						}
						srcOff, srcLen := mcRefinementSourceLayout(kernel.name, width, height, stride)
						src := mcAVX2GuardedBuffer(t, srcLen, atEnd)
						dst := mcAVX2GuardedBuffer(t, width*height, atEnd)
						for i := range src {
							src[i] = byte(i*53 + 37)
						}
						want := make([]byte, len(dst))
						kernel.c(src, srcOff, int32(stride), want, 0, int32(width), int32(width), int32(height))
						kernel.avx2(src, srcOff, int32(stride), dst, 0, int32(width), int32(width), int32(height))
						if !bytes.Equal(dst, want) {
							t.Fatal("guarded refinement shape differs")
						}
					})
				}
			}
		}
	}
}

func TestMcAVX2RefinementCenterRawGuardPages(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, width := range []int{5, 9, 17} {
		for _, height := range []int{1, 2, 5, 9, 17} {
			for _, atEnd := range []bool{false, true} {
				t.Run(fmt.Sprintf("%dx%d/end%v", width, height, atEnd), func(t *testing.T) {
					stride := width + 5
					srcBytes := mcAVX2GuardedBuffer(t, 2*stride*height, atEnd)
					src := unsafe.Slice((*int16)(unsafe.Pointer(unsafe.SliceData(srcBytes))), stride*height)
					dst := mcAVX2GuardedBuffer(t, width*height, atEnd)
					for i := range src {
						src[i] = int16(i*733 + 0x7FFF)
					}
					mcHorRawRefineAVX2(unsafe.SliceData(dst), width, unsafe.SliceData(src), stride, width, height)
					for row := 0; row < height; row++ {
						for col := 0; col < width; col++ {
							value := (HorFilterInput16bit_c(src, row*stride+col) + 512) >> 10
							if got, want := dst[row*width+col], byte(min(max(value, 0), 255)); got != want {
								t.Fatalf("row %d col %d: got %d want %d", row, col, got, want)
							}
						}
					}
				})
			}
		}
	}
}
