//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"testing"
)

type blockCopyTestCase struct {
	width, height int
	scalar, simd  func([]byte, int, int32, []byte, int, int32)
}

var blockCopyTestCases = []blockCopyTestCase{
	{8, 16, WelsCopy8x16_c, WelsCopy8x16_sse2},
	{16, 16, WelsCopy16x16_c, WelsCopy16x16_sse2},
}

func TestBlockCopySIMDParity(t *testing.T) {
	for _, test := range blockCopyTestCases {
		for _, srcStride := range []int{0, 3, test.width, 29, -3, -31} {
			for _, dstStride := range []int{0, 5, test.width, 37, -5, -41} {
				name := fmt.Sprintf("%dx%d/src%d/dst%d", test.width, test.height, srcStride, dstStride)
				t.Run(name, func(t *testing.T) {
					src, dst := make([]byte, 2048), bytes.Repeat([]byte{0xa5}, 2048)
					for i := range src {
						src[i] = byte(i*47 + i/13)
					}
					original, want := bytes.Clone(src), bytes.Clone(dst)
					srcOff, dstOff := 7, 11
					if srcStride < 0 {
						srcOff -= (test.height - 1) * srcStride
					}
					if dstStride < 0 {
						dstOff -= (test.height - 1) * dstStride
					}
					test.scalar(want, dstOff, int32(dstStride), src, srcOff, int32(srcStride))
					test.simd(dst, dstOff, int32(dstStride), src, srcOff, int32(srcStride))
					if !bytes.Equal(dst, want) {
						t.Fatal("copy differs from scalar or writes destination padding")
					}
					if !bytes.Equal(src, original) {
						t.Fatal("copy changed source")
					}
				})
			}
		}
	}
}

func TestBlockCopySIMDAliases(t *testing.T) {
	for _, test := range blockCopyTestCases {
		for _, stride := range []int32{0, 5, 32, -32} {
			for _, shift := range []int{-33, -16, -1, 0, 1, 7, 16, 33} {
				name := fmt.Sprintf("%dx%d/stride%d/shift%d", test.width, test.height, stride, shift)
				t.Run(name, func(t *testing.T) {
					actual := make([]byte, 2048)
					for i := range actual {
						actual[i] = byte(i*43 + i/7)
					}
					want := bytes.Clone(actual)
					// Different slice bases exercise aliases beyond identical headers.
					srcOff, dstOff := 800, 804+shift
					test.scalar(want[7:], dstOff, stride, want[11:], srcOff, stride)
					test.simd(actual[7:], dstOff, stride, actual[11:], srcOff, stride)
					if !bytes.Equal(actual, want) {
						t.Fatal("overlapping copy changed scalar row ordering")
					}
				})
			}
		}
	}
}

func TestBlockCopySIMDVisibleBounds(t *testing.T) {
	for _, test := range blockCopyTestCases {
		t.Run(fmt.Sprintf("%dx%d", test.width, test.height), func(t *testing.T) {
			const stride = 32
			size := (test.height-1)*stride + test.width
			for _, source := range []bool{false, true} {
				src, dst := make([]byte, size+64), bytes.Repeat([]byte{0xa5}, size+64)
				if source {
					src = src[:size-1]
				} else {
					dst = dst[:size-1]
				}
				before := bytes.Clone(dst[:cap(dst)])
				mustBlockCopyPanic(t, func() { test.simd(dst, 0, stride, src, 0, stride) })
				if !bytes.Equal(dst[:cap(dst)], before) {
					t.Fatal("invalid span wrote destination before rejection")
				}
			}
			valid := make([]byte, size)
			mustBlockCopyPanic(t, func() { test.simd(valid, -1, stride, valid, 0, stride) })
			mustBlockCopyPanic(t, func() { test.simd(valid, 0, stride, valid, -1, stride) })
			mustBlockCopyPanic(t, func() { test.simd(valid, 0, -stride, valid, 0, stride) })
			mustBlockCopyPanic(t, func() { test.simd(valid, 0, stride, valid, 0, -stride) })
			mustBlockCopyPanic(t, func() { test.simd(nil, 0, stride, valid, 0, stride) })
			mustBlockCopyPanic(t, func() { test.simd(valid, 0, stride, nil, 0, stride) })
			maxInt := int(^uint(0) >> 1)
			mustBlockCopyPanic(t, func() { test.simd(valid, maxInt, stride, valid, 0, stride) })
			mustBlockCopyPanic(t, func() { test.simd(valid, 0, stride, valid, maxInt, stride) })
		})
	}
}

func mustBlockCopyPanic(t *testing.T, fn func()) {
	t.Helper()
	defer func() {
		if recover() == nil {
			t.Error("invalid visible span did not panic")
		}
	}()
	fn()
}
