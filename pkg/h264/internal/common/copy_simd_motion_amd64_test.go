//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"testing"
)

var integerMotionCopyModes = []struct {
	name string
	fn   PWelsMcFunc
}{
	{"LumaSSE2", McLuma_sse2},
	{"LumaAVX2", McLuma_avx2},
	{"ChromaSSE2", McChroma_sse2},
}

func TestIntegerMotionCopyParity(t *testing.T) {
	for _, mode := range integerMotionCopyModes {
		for _, shape := range [][2]int32{{8, 8}, {8, 16}, {16, 8}, {16, 16}, {8, 4}, {4, 8}, {4, 4}, {8, 12}, {2, 8}, {5, 8}} {
			for _, strides := range [][2]int32{{32, 37}, {8, 16}, {0, 0}, {3, 5}, {-32, 37}, {32, -37}} {
				for _, motion := range [][2]int16{{0, 0}, {8, -16}, {-16, 24}} {
					name := fmt.Sprintf("%s/%dx%d/strides%v/mv%v", mode.name, shape[0], shape[1], strides, motion)
					t.Run(name, func(t *testing.T) {
						src, dst := make([]byte, 2048), bytes.Repeat([]byte{0xa5}, 2048)
						for i := range src {
							src[i] = byte(i*47 + i/13)
						}
						original, want := bytes.Clone(src), bytes.Clone(dst)
						srcOff, dstOff := 800, 811
						McCopy_c(src, srcOff, strides[0], want, dstOff, strides[1], shape[0], shape[1])
						mode.fn(src, srcOff, strides[0], dst, dstOff, strides[1], motion[0], motion[1], shape[0], shape[1])
						if !bytes.Equal(dst, want) || !bytes.Equal(src, original) {
							t.Fatal("integer motion differs from scalar copy or changes source")
						}
					})
				}
			}
		}
	}
}

func TestIntegerMotionCopyAliases(t *testing.T) {
	for _, mode := range integerMotionCopyModes {
		for _, shape := range [][2]int32{{8, 8}, {8, 16}, {16, 8}, {16, 16}, {8, 4}, {4, 8}, {8, 12}} {
			for _, strides := range [][2]int32{{32, 32}, {32, 37}, {0, 0}, {3, 5}, {-32, -32}, {-32, 37}} {
				for _, shift := range []int{-33, -8, -1, 0, 1, 8, 33} {
					name := fmt.Sprintf("%s/%dx%d/strides%v/shift%d", mode.name, shape[0], shape[1], strides, shift)
					t.Run(name, func(t *testing.T) {
						actual := make([]byte, 2048)
						for i := range actual {
							actual[i] = byte(i*43 + i/7)
						}
						want := bytes.Clone(actual)
						McCopy_c(want[11:], 800, strides[0], want[7:], 804+shift, strides[1], shape[0], shape[1])
						mode.fn(actual[11:], 800, strides[0], actual[7:], 804+shift, strides[1], 8, -16, shape[0], shape[1])
						if !bytes.Equal(actual, want) {
							t.Fatal("integer motion changed scalar row ordering")
						}
					})
				}
			}
		}
	}
}

func TestIntegerMotionCopyVisibleBounds(t *testing.T) {
	// Only the sixteen-row kernels take the new checked assembly path. Other
	// shapes retain the existing scalar contract, tested in parity and aliases.
	for _, mode := range integerMotionCopyModes {
		for _, shape := range [][2]int32{{8, 16}, {16, 16}} {
			t.Run(fmt.Sprintf("%s/%dx%d", mode.name, shape[0], shape[1]), func(t *testing.T) {
				const stride = 32
				size := int((shape[1]-1)*stride + shape[0])
				for _, source := range []bool{false, true} {
					src, dst := make([]byte, size+64), bytes.Repeat([]byte{0xa5}, size+64)
					if source {
						src = src[:size-1]
					} else {
						dst = dst[:size-1]
					}
					before := bytes.Clone(dst[:cap(dst)])
					mustBlockCopyPanic(t, func() { mode.fn(src, 0, stride, dst, 0, stride, 0, 0, shape[0], shape[1]) })
					if !bytes.Equal(dst[:cap(dst)], before) {
						t.Fatal("invalid span wrote destination before rejection")
					}
				}
			})
		}
	}
}
