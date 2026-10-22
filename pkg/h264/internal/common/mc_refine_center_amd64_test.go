//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"testing"
	"unsafe"

	"golang.org/x/sys/cpu"
)

func TestMcAVX2RefinementCenterRawSigned(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewSource(0x51917))
	for _, width := range []int{5, 9, 17} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			const height = 9
			stride, dstStride := width+5, width+3
			input := make([]int16, 1+stride*height)
			src := input[1:]
			for pattern := 0; pattern < 40; pattern++ {
				for i := range src {
					switch pattern {
					case 0:
						src[i] = -32768
					case 1:
						src[i] = 32767
					case 2:
						src[i] = int16((i&1)*65535 - 32768)
					case 3:
						src[i] = 0
					default:
						src[i] = int16(rng.Intn(65536) - 32768)
					}
				}
				before := slices.Clone(input)
				want := bytes.Repeat([]byte{0xCA}, 1+height*dstStride+8)
				got := bytes.Clone(want)
				for row := 0; row < height; row++ {
					for col := 0; col < width; col++ {
						value := (HorFilterInput16bit_c(src, row*stride+col) + 512) >> 10
						want[1+row*dstStride+col] = byte(min(max(value, 0), 255))
					}
				}
				mcHorRawRefineAVX2(&got[1], dstStride, unsafe.SliceData(src), stride, width, height)
				if !bytes.Equal(got, want) {
					t.Fatalf("pattern %d: signed output or canary differs", pattern)
				}
				if !slices.Equal(input, before) {
					t.Fatalf("pattern %d: signed source changed", pattern)
				}
			}
		})
	}
}
