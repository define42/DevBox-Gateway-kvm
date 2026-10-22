//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"slices"
	"testing"

	"golang.org/x/sys/cpu"
)

func TestMcVerReuseAVX2MatchesScalar(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, raw := range []bool{false, true} {
		widths := []int{4, 5, 8, 9, 10, 13, 14, 16, 17, 21, 22, 24}
		for _, width := range widths {
			// Cross every exit of the five-row register ring, including
			// multiple complete rotations beyond the usual 17-row blocks.
			for _, height := range []int{1, 2, 3, 4, 5, 6, 9, 10, 11, 17, 18, 26, 31} {
				t.Run(fmt.Sprintf("raw%v/%dx%d", raw, width, height), func(t *testing.T) {
					for _, padding := range []int{0, 7} {
						srcStride, dstStride := width+padding, width+padding
						src := make([]byte, (height+4)*srcStride+width)
						for pattern := 0; pattern < 5; pattern++ {
							for i := range src {
								switch pattern {
								case 0:
									src[i] = byte(i*73 + (i>>2)*29 + 19)
								case 1:
									src[i] = 0
								case 2:
									src[i] = 255
								case 3:
									// The first output reaches +10710.
									if (0b101101>>((i/srcStride)%6))&1 != 0 {
										src[i] = 255
									} else {
										src[i] = 0
									}
								case 4:
									// The first output reaches -2550.
									src[i] ^= 255
								}
							}
							before := bytes.Clone(src)
							dstLen := 1 + (height-1)*dstStride + width + 7
							if raw {
								want := make([]int16, dstLen)
								for i := range want {
									want[i] = 0x1357
								}
								got := slices.Clone(want)
								for row := 0; row < height; row++ {
									for col := 0; col < width; col++ {
										want[1+row*dstStride+col] = int16(FilterInput8bitWithStride_c(
											src, (row+2)*srcStride+col, int32(srcStride)))
									}
								}
								mcVerRawReuseAVX2(&got[1], dstStride, &src[2*srcStride], srcStride, width, height)
								if !slices.Equal(got, want) {
									t.Fatalf("pattern %d padding %d: signed outputs or row canaries differ", pattern, padding)
								}
							} else {
								want := bytes.Repeat([]byte{0xA5}, dstLen)
								got := bytes.Clone(want)
								McHorVer02_c(src, 2*srcStride, int32(srcStride), want, 1,
									int32(dstStride), int32(width), int32(height))
								mcVerReuseAVX2(&got[1], dstStride, &src[2*srcStride], srcStride, width, height)
								if !bytes.Equal(got, want) {
									t.Fatalf("pattern %d padding %d: clipped outputs or row canaries differ", pattern, padding)
								}
							}
							if !bytes.Equal(src, before) {
								t.Fatal("source changed")
							}
						}
					}
				})
			}
		}
	}
}

func TestMcVerRawReuseAVX2BinaryTapPatterns(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	// Exhausting all binary six-tap inputs covers both signed extrema and
	// rounding boundaries without relying on random data or the clipped pass.
	for _, width := range []int{4, 5, 8, 9, 10, 13, 14, 16, 17, 21, 22, 24} {
		src, got := make([]byte, 6*width), make([]int16, width)
		for pattern := 0; pattern < 64; pattern++ {
			for row := 0; row < 6; row++ {
				for col := 0; col < width; col++ {
					src[row*width+col] = byte(((pattern >> row) & 1) * 255)
				}
			}
			mcVerRawReuseAVX2(&got[0], width, &src[2*width], width, width, 1)
			want := int16(FilterInput8bitWithStride_c(src, 2*width, int32(width)))
			for col, value := range got {
				if value != want {
					t.Fatalf("width %d pattern %06b col %d: got %d want %d", width, pattern, col, value, want)
				}
			}
		}
	}
}
