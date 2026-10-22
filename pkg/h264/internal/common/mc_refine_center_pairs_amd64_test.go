//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"golang.org/x/sys/cpu"
)

func centerPairsReference(src []int16, off int) byte {
	var sum int64
	for i, coefficient := range [...]int64{1, -5, 20, 20, -5, 1} {
		sum += int64(src[off+i]) * coefficient
	}
	value := (sum + 512) >> 10
	return byte(min(max(value, 0), 255))
}

func TestMcCenterPairsFullSignedRange(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	rng := rand.New(rand.NewSource(0x264c3))
	for _, width := range []int{5, 9, 17} {
		for _, height := range []int{1, 2, 5, 9, 17} {
			for _, strides := range [][2]int{{0, 0}, {3, 2}, {width + 5, width}, {24, 32}} {
				t.Run(fmt.Sprintf("%dx%d/strides%v", width, height, strides), func(t *testing.T) {
					const srcOff, dstOff = 1, 3
					src := make([]int16, srcOff+(height-1)*strides[0]+width+5)
					for pattern := range 32 {
						for i := range src {
							switch pattern {
							case 0:
								src[i] = -32768
							case 1:
								src[i] = 32767
							case 2:
								src[i] = int16((i&1)*65535 - 32768)
							case 3:
								src[i] = int16(15 + i%3) // around the first rounding boundary
							case 4:
								src[i] = int16(8175 + i%3) // around the last unclipped value
							default:
								src[i] = int16(rng.Intn(65536) - 32768)
							}
						}
						before := slices.Clone(src)
						got := bytes.Repeat([]byte{0xca}, dstOff+(height-1)*strides[1]+width+7)
						want := bytes.Clone(got)
						for y := range height {
							for x := range width {
								want[dstOff+y*strides[1]+x] = centerPairsReference(src, srcOff+y*strides[0]+x)
							}
						}
						mcHorRawRefineAVX2(&got[dstOff], strides[1], &src[srcOff], strides[0], width, height)
						if !bytes.Equal(got, want) {
							t.Fatalf("pattern=%d: pixels or canaries differ", pattern)
						}
						if !slices.Equal(src, before) {
							t.Fatal("source modified")
						}
					}
				})
			}
		}
	}
}

func TestMcCenterPairsTapLanesAndRounding(t *testing.T) {
	if !cpu.X86.HasAVX2 {
		t.Skip("AVX2 unavailable")
	}
	for _, width := range []int{5, 9, 17} {
		t.Run(fmt.Sprintf("width%d", width), func(t *testing.T) {
			src, got := make([]int16, width+5), make([]byte, width)
			check := func() {
				t.Helper()
				mcHorRawRefineAVX2(&got[0], width, &src[0], width+5, width, 1)
				for x := range width {
					if want := centerPairsReference(src, x); got[x] != want {
						t.Fatalf("column=%d: got=%d want=%d input=%v", x, got[x], want, src)
					}
				}
			}
			// Every input lane at several floors/clipping boundaries and full
			// signed extremes; a nonzero background also detects missing taps.
			for _, background := range []int16{0, 16, 4096, 8176} {
				for i := range src {
					src[i] = background
				}
				for tap := range src {
					for _, value := range []int16{-32768, -32767, -513, -512, -1, 0, 1, 511, 512, 513, 32766, 32767} {
						src[tap] = value
						check()
					}
					src[tap] = background
				}
			}
			// Large opposite-signed pairs cancel into rounding-sensitive
			// outputs, so clipping cannot conceal signed multiplication errors.
			for x := range width {
				for _, middle := range []int16{-32768, -32742, -32741, -19713, -19712, -19711} {
					for _, last := range []int16{-32768, -32767, -32240, -32239, -32238} {
						clear(src)
						copy(src[x:], []int16{32767, 32767, middle, 32767, -32768, last})
						check()
					}
				}
			}
		})
	}
}
