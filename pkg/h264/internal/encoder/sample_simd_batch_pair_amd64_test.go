//go:build amd64

package encoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Evaluate the full two-dimensional matrix rather than sharing the SIMD
// butterfly or its max-absolute-value reduction.
func satdMatrix4x4(src, ref []byte, off, stride int) int32 {
	basis := [4][4]int32{
		{1, 1, 1, 1},
		{1, 1, -1, -1},
		{1, -1, -1, 1},
		{1, -1, 1, -1},
	}
	var sum int32
	for v := range 4 {
		for u := range 4 {
			var coefficient int32
			for y := range 4 {
				for x := range 4 {
					coefficient += basis[v][y] * basis[u][x] *
						(int32(src[off+y*stride+x]) - int32(ref[off+y*stride+x]))
				}
			}
			if coefficient < 0 {
				coefficient = -coefficient
			}
			sum += coefficient
		}
	}
	return (sum + 1) >> 1
}

func TestSATDPairReductionAllBinaryBlocks(t *testing.T) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		t.Skip("AVX2 required")
	}
	const stride = 16
	var src, ref [stride * 4]byte
	for pattern := range 1 << 16 {
		for y := range 4 {
			for x := range 4 {
				value := byte((pattern >> (y*4 + x) & 1) * 255)
				src[y*stride+x], ref[y*stride+x] = value, 255-value
			}
		}
		want := satdMatrix4x4(src[:], ref[:], 0, stride)
		got := sampleSatdBlockAVX2(src[:], 0, stride, ref[:], 0, stride, 16, 4)
		if got != want {
			t.Fatalf("binary pattern %#x: got %d, want %d", pattern, got, want)
		}
	}
}

func TestSATDPairReductionEveryLane(t *testing.T) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		t.Skip("AVX2 required")
	}
	var src, ref [16 * 16]byte
	for pixel := range src {
		for value := 1; value <= 255; value++ {
			src[pixel] = byte(value)
			got := sampleSatdBlockAVX2(src[:], 0, 16, ref[:], 0, 16, 16, 16)
			if want := int32(8 * value); got != want {
				t.Fatalf("pixel %d value %d: got %d, want %d", pixel, value, got, want)
			}
		}
		src[pixel] = 0
	}
}
