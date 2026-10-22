//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"
)

var packedSadTestCases = []struct {
	width, height int
	fn            func([]byte, int, int32, []byte, int, int32) int32
}{
	{16, 16, WelsSampleSad16x16_sse2},
	{16, 8, WelsSampleSad16x8_sse2},
	{8, 4, WelsSampleSad8x4_sse2},
	{8, 8, WelsSampleSad8x8_sse2},
	{8, 16, WelsSampleSad8x16_sse2},
	{4, 4, WelsSampleSad4x4_sse2},
	{4, 8, WelsSampleSad4x8_sse2},
}

func packedSadReference(src []byte, srcOff, srcStride int, ref []byte, refOff, refStride, width, height int) int32 {
	var sum int32
	for y := range height {
		for x := range width {
			delta := int32(src[srcOff+y*srcStride+x]) - int32(ref[refOff+y*refStride+x])
			if delta < 0 {
				delta = -delta
			}
			sum += delta
		}
	}
	return sum
}

func TestPackedSadRowLanes(t *testing.T) {
	for _, test := range packedSadTestCases {
		t.Run(fmt.Sprintf("%dx%d", test.width, test.height), func(t *testing.T) {
			const srcStride, refStride, srcOff, refOff = 29, 37, 3, 7
			src, ref := make([]byte, 1024), make([]byte, 1024)
			for y := range test.height {
				for x := range test.width {
					src[srcOff+y*srcStride+x] = 255
					if got := test.fn(src, srcOff, srcStride, ref, refOff, refStride); got != 255 {
						t.Fatalf("source impulse (%d,%d): got %d", x, y, got)
					}
					src[srcOff+y*srcStride+x] = 0
					ref[refOff+y*refStride+x] = 255
					if got := test.fn(src, srcOff, srcStride, ref, refOff, refStride); got != 255 {
						t.Fatalf("reference impulse (%d,%d): got %d", x, y, got)
					}
					ref[refOff+y*refStride+x] = 0
				}
			}
			for _, invert := range []bool{false, true} {
				for y := range test.height {
					for x := range test.width {
						a, b := byte(0), byte(255)
						if invert {
							a, b = b, a
						}
						src[srcOff+y*srcStride+x], ref[refOff+y*refStride+x] = a, b
					}
				}
				want := int32(test.width * test.height * 255)
				if got := test.fn(src, srcOff, srcStride, ref, refOff, refStride); got != want {
					t.Fatalf("maximum difference: got %d want %d", got, want)
				}
			}
		})
	}
}

func TestPackedSadRowsRandomStridesAndAliases(t *testing.T) {
	rng := rand.New(rand.NewSource(0x2648))
	for _, test := range packedSadTestCases {
		for _, strides := range [][2]int{{0, 0}, {0, 31}, {29, 0}, {3, 5}, {8, 16}, {31, 37}, {63, 47}} {
			t.Run(fmt.Sprintf("%dx%d/strides%v", test.width, test.height, strides), func(t *testing.T) {
				for iteration := 0; iteration < 40; iteration++ {
					src, ref := make([]byte, 2048), make([]byte, 2048)
					rng.Read(src)
					rng.Read(ref)
					if iteration&1 != 0 {
						ref = src[5:]
					}
					srcBefore, refBefore := bytes.Clone(src), bytes.Clone(ref)
					srcOff, refOff := iteration%31, iteration%17
					want := packedSadReference(src, srcOff, strides[0], ref, refOff, strides[1], test.width, test.height)
					if got := test.fn(src, srcOff, int32(strides[0]), ref, refOff, int32(strides[1])); got != want {
						t.Fatalf("iteration %d: got %d want %d", iteration, got, want)
					}
					if !bytes.Equal(src, srcBefore) || !bytes.Equal(ref, refBefore) {
						t.Fatal("SAD modified an input")
					}
				}
			})
		}
	}
}

func TestPackedSadVisibleBounds(t *testing.T) {
	for _, test := range packedSadTestCases {
		t.Run(fmt.Sprintf("%dx%d", test.width, test.height), func(t *testing.T) {
			const stride = 32
			size := (test.height-1)*stride + test.width
			for _, source := range []bool{false, true} {
				src, ref := make([]byte, size+64), make([]byte, size+64)
				if source {
					src = src[:size-1]
				} else {
					ref = ref[:size-1]
				}
				requireSIMDBoundsPanic(t, func() { test.fn(src, 0, stride, ref, 0, stride) })
			}
			valid := make([]byte, size)
			requireSIMDBoundsPanic(t, func() { test.fn(valid, 0, -stride, valid, 0, stride) })
			requireSIMDBoundsPanic(t, func() { test.fn(valid, 0, stride, valid, 0, -stride) })
			requireSIMDBoundsPanic(t, func() { test.fn(valid, -1, stride, valid, 0, stride) })
			requireSIMDBoundsPanic(t, func() { test.fn(valid, 0, stride, valid, -1, stride) })
		})
	}
}
