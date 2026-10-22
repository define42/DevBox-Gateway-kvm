//go:build amd64

package common

import (
	"bytes"
	"fmt"
	"math/rand"
	"reflect"
	"testing"
)

func requireChromaSSSE3(t *testing.T) {
	t.Helper()
	if WelsCPUFeatureDetect(nil)&WELS_CPU_SSSE3 == 0 {
		t.Skip("SSSE3 required")
	}
}

func TestChromaSSSE3Dispatch(t *testing.T) {
	var funcs SMcFunc
	for _, flags := range []uint32{0, WELS_CPU_SSE2, WELS_CPU_SSE2 | WELS_CPU_SSSE3, 0} {
		InitMcFunc(&funcs, flags)
		want := PWelsMcFunc(McChroma_c)
		if flags&WELS_CPU_SSE2 != 0 {
			want = McChroma_sse2
		}
		if flags&WELS_CPU_SSSE3 != 0 && WelsCPUFeatureDetect(nil)&WELS_CPU_SSSE3 != 0 {
			want = McChroma_ssse3
		}
		if reflect.ValueOf(funcs.PMcChromaFunc).Pointer() != reflect.ValueOf(want).Pointer() {
			t.Fatalf("flags=%x selected unexpected chroma implementation", flags)
		}
	}
}

func TestChromaSSSE3MatchesScalar(t *testing.T) {
	requireChromaSSSE3(t)
	rng := rand.New(rand.NewSource(264833))
	for _, size := range [][2]int32{{8, 1}, {8, 2}, {8, 3}, {8, 4}, {8, 5}, {8, 8}, {8, 9}, {8, 16}, {8, 17}, {4, 8}, {2, 4}, {3, 5}, {16, 16}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			for pattern := range 4 {
				const srcOff, dstOff, srcStride, dstStride = 3, 5, 23, 21
				src := make([]byte, srcOff+int(size[1])*srcStride+int(size[0])+1)
				rng.Read(src)
				for i := range src {
					switch pattern {
					case 0:
						src[i] = 0
					case 1:
						src[i] = 255
					case 2:
						src[i] = byte(255 * (i & 1))
					}
				}
				srcBefore := bytes.Clone(src)
				for dx := int16(-8); dx < 8; dx++ {
					for dy := int16(-8); dy < 8; dy++ {
						got := bytes.Repeat([]byte{0xa5}, dstOff+(int(size[1])-1)*dstStride+int(size[0])+7)
						want := bytes.Clone(got)
						McChroma_c(src, srcOff, srcStride, want, dstOff, dstStride, dx, dy, size[0], size[1])
						McChroma_ssse3(src, srcOff, srcStride, got, dstOff, dstStride, dx, dy, size[0], size[1])
						if !bytes.Equal(got, want) || !bytes.Equal(src, srcBefore) {
							t.Fatalf("pattern=%d MV(%d,%d): output or input differs", pattern, dx, dy)
						}
					}
				}
			}
		})
	}
}

func TestChromaSSSE3SignedStridesAndAliases(t *testing.T) {
	requireChromaSSSE3(t)
	for _, strides := range [][2]int32{{0, 0}, {1, 2}, {3, 5}, {19, 13}, {-19, 13}, {19, -13}, {-19, -13}} {
		for _, height := range []int32{7, 8} {
			for _, delta := range []int{-1, 0, 1, 20, 400} {
				got := make([]byte, 1024)
				for i := range got {
					got[i] = byte(i*37 + 11)
				}
				want := bytes.Clone(got)
				McChroma_c(want, 256, strides[0], want, 256+delta, strides[1], -3, 5, 8, height)
				McChroma_ssse3(got, 256, strides[0], got, 256+delta, strides[1], -3, 5, 8, height)
				if !bytes.Equal(got, want) {
					t.Fatalf("strides=%v height=%d delta=%d: aliased output differs", strides, height, delta)
				}
			}
			// Disjoint planes force the new kernel for each unusual stride.
			src, got, want := make([]byte, 1024), make([]byte, 1024), make([]byte, 1024)
			for i := range src {
				src[i] = byte(i*13 + i/19)
			}
			McChroma_c(src, 256, strides[0], want, 256, strides[1], 3, -5, 8, height)
			McChroma_ssse3(src, 256, strides[0], got, 256, strides[1], 3, -5, 8, height)
			if !bytes.Equal(got, want) {
				t.Fatalf("strides=%v height=%d: disjoint output differs", strides, height)
			}
		}
	}
}

func TestChromaSSSE3VisibleBounds(t *testing.T) {
	requireChromaSSSE3(t)
	for _, phase := range [][2]int16{{3, 5}, {0, 5}, {5, 0}} {
		for _, field := range []string{"source", "destination", "negative-source", "negative-destination", "overflowing-source", "overflowing-destination"} {
			t.Run(fmt.Sprintf("%v/%s", phase, field), func(t *testing.T) {
				src, dst := make([]byte, 256), make([]byte, 256)
				srcOff, dstOff := 1, 3
				switch field {
				case "source":
					src = src[:srcOff+8*16+8]
				case "destination":
					dst = dst[:dstOff+7*16+7]
				case "negative-source":
					srcOff = -1
				case "negative-destination":
					dstOff = -1
				case "overflowing-source":
					srcOff = int(^uint(0) >> 1)
				case "overflowing-destination":
					dstOff = int(^uint(0) >> 1)
				}
				requireSIMDBoundsPanic(t, func() {
					McChroma_ssse3(src, srcOff, 16, dst, dstOff, 16, phase[0], phase[1], 8, 8)
				})
			})
		}
	}
}
