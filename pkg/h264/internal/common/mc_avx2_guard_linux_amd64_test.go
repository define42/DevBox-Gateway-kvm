//go:build linux && amd64

package common

import (
	"bytes"
	"fmt"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
	"golang.org/x/sys/unix"
)

func mcAVX2GuardedBuffer(t *testing.T, size int, atEnd bool) []byte {
	t.Helper()
	page := unix.Getpagesize()
	if size > page {
		t.Fatal("guarded buffer exceeds one page")
	}
	mapping, err := unix.Mmap(-1, 0, page*3, unix.PROT_NONE, unix.MAP_ANON|unix.MAP_PRIVATE)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := unix.Munmap(mapping); err != nil {
			t.Error(err)
		}
	})
	if err := unix.Mprotect(mapping[page:2*page], unix.PROT_READ|unix.PROT_WRITE); err != nil {
		t.Fatal(err)
	}
	off := page
	if atEnd {
		off = 2*page - size
	}
	return mapping[off : off+size : off+size]
}

func TestMcAVX2GuardPages(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, atEnd := range []bool{false, true} {
		for _, width := range []int32{4, 8, 16, 17} {
			for _, kernel := range mcAVX2TestKernels {
				t.Run(fmt.Sprintf("%s/width%d/end%v", kernel.name, width, atEnd), func(t *testing.T) {
					const height = 17
					stride, rows, off := int(width+5), height, 2
					if kernel.name == "vertical" {
						stride, rows, off = int(width), height+5, int(2*width)
					}
					if kernel.name == "center" {
						rows, off = height+5, 2*stride+2
					}
					src := mcAVX2GuardedBuffer(t, stride*rows, atEnd)
					dst := mcAVX2GuardedBuffer(t, int(width)*height, atEnd)
					for i := range src {
						src[i] = byte(i*73 + 23)
					}
					want := make([]byte, len(dst))
					kernel.c(src, off, int32(stride), want, 0, width, width, height)
					kernel.avx2(src, off, int32(stride), dst, 0, width, width, height)
					if !bytes.Equal(dst, want) {
						t.Fatal("guarded block differs")
					}
				})
			}
		}
	}
}

func TestMcAVX2RawPassGuardPages(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, atEnd := range []bool{false, true} {
		for _, width := range []int{1, 4, 8, 13, 16, 17, 21, 22} {
			t.Run(fmt.Sprintf("width%d/end%v", width, atEnd), func(t *testing.T) {
				const height = 3
				src := mcAVX2GuardedBuffer(t, (height+5)*width, atEnd)
				for i := range src {
					src[i] = byte(i*53 + 21)
				}
				rawBytes := mcAVX2GuardedBuffer(t, 2*height*width, atEnd)
				raw := unsafe.Slice((*int16)(unsafe.Pointer(unsafe.SliceData(rawBytes))), height*width)
				mcVerticalRawAVX2(raw, width, src[2*width:], width, width, height)

				stride := width + 5
				inputBytes := mcAVX2GuardedBuffer(t, 2*height*stride, atEnd)
				input := unsafe.Slice((*int16)(unsafe.Pointer(unsafe.SliceData(inputBytes))), height*stride)
				for i := range input {
					input[i] = int16(i*733 + 0x7FFF)
				}
				dst := mcAVX2GuardedBuffer(t, height*width, atEnd)
				mcHorizontalRawToU8AVX2(dst, width, input, stride, width, height)
				for row := 0; row < height; row++ {
					for col := 0; col < width; col++ {
						value := (HorFilterInput16bit_c(input, row*stride+col) + 512) >> 10
						if got, want := dst[row*width+col], byte(min(max(value, 0), 255)); got != want {
							t.Fatalf("row %d col %d: got %d want %d", row, col, got, want)
						}
					}
				}
			})
		}
	}
}

func TestMcAVX2HorizontalVectorTailsGuardPages(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, atEnd := range []bool{false, true} {
		for _, width := range []int{15, 16, 17, 31, 32, 33, 47, 48, 49} {
			t.Run(fmt.Sprintf("width%d/end%v", width, atEnd), func(t *testing.T) {
				const height = 3
				stride := width + 5
				src := mcAVX2GuardedBuffer(t, stride*height, atEnd)
				dst := mcAVX2GuardedBuffer(t, width*height, atEnd)
				for i := range src {
					src[i] = byte(i*73 + 23)
				}
				want := make([]byte, len(dst))
				McHorVer20_c(src, 2, int32(stride), want, 0, int32(width), int32(width), height)
				McHorVer20_avx2(src, 2, int32(stride), dst, 0, int32(width), int32(width), height)
				if !bytes.Equal(dst, want) {
					t.Fatal("horizontal vector blocks or their tail differ")
				}
			})
		}
	}
}
