//go:build amd64

package encoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// The companion native harness runs this same deterministic input corpus
// against OpenH264's AVX2 SATD and horizontal half-pixel functions.
func TestMotionNativeCorpus(t *testing.T) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		t.Skip("AVX2 required")
	}
	var a [32*16 + 3]byte
	var b [32*16 + 7]byte
	var src [48 * 32]byte
	var dst [32 * 17]byte
	cases := []PSampleSadSatdCostFunc{
		WelsSampleSatd8x8_avx2, WelsSampleSatd16x8_avx2,
		WelsSampleSatd8x16_avx2, WelsSampleSatd16x16_avx2,
	}
	state := uint32(0x2645ad)
	next := func() byte {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return byte(state)
	}
	hash := uint64(14695981039346656037)
	add := func(value byte) { hash = (hash ^ uint64(value)) * 1099511628211 }
	for range 1024 {
		for i := range a {
			a[i] = next()
		}
		for i := range b {
			b[i] = next()
		}
		for _, fn := range cases {
			value := uint32(fn(a[:], 3, 32, b[:], 7, 32))
			for i := range 4 {
				add(byte(value >> (8 * i)))
			}
		}
		for i := range src {
			src[i] = next()
		}
		common.McHorVer20_avx2(src[:], 3*48+3, 48, dst[:], 0, 32, 16, 16)
		for y := range 16 {
			for x := range 16 {
				add(dst[y*32+x])
			}
		}
	}
	// OpenH264 2.6.0, codec/common/x86 SATD and luma interpolation kernels.
	const nativeHash = uint64(0x928d2228b8537ee8)
	if hash != nativeHash {
		t.Fatalf("motion corpus: got %016x, native OpenH264 produced %016x", hash, nativeHash)
	}
	t.Logf("motion-corpus fnv64=%016x", hash)
}
