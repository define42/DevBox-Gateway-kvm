//go:build amd64

package encoder

import (
	"math/rand"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestSampleSatdSIMDMatchesScalar(t *testing.T) {
	flags := common.WelsCPUFeatureDetect(nil)
	if flags&common.WELS_CPU_SSE41 == 0 || flags&common.WELS_CPU_SSSE3 == 0 {
		t.Skip("SSE4.1 and SSSE3 are required")
	}

	type satdFn func([]uint8, int, int32, []uint8, int, int32) int32
	cases := []struct {
		name   string
		scalar satdFn
		sse41  satdFn
		avx2   satdFn
	}{
		{"16x16", WelsSampleSatd16x16_c, WelsSampleSatd16x16_sse41, WelsSampleSatd16x16_avx2},
		{"16x8", WelsSampleSatd16x8_c, WelsSampleSatd16x8_sse41, WelsSampleSatd16x8_avx2},
		{"8x16", WelsSampleSatd8x16_c, WelsSampleSatd8x16_sse41, WelsSampleSatd8x16_avx2},
		{"8x8", WelsSampleSatd8x8_c, WelsSampleSatd8x8_sse41, WelsSampleSatd8x8_avx2},
		{"8x4", WelsSampleSatd8x4_c, WelsSampleSatd8x4_sse41, nil},
		{"4x8", WelsSampleSatd4x8_c, WelsSampleSatd4x8_sse41, nil},
		{"4x4", WelsSampleSatd4x4_c, WelsSampleSatd4x4_sse41, nil},
	}

	rng := rand.New(rand.NewSource(0x264))
	for _, stride := range []int{16, 23, 32, 63} {
		for _, off := range []int{0, 1, 3, 7, 15, 31} {
			for iteration := 0; iteration < 20; iteration++ {
				a := make([]uint8, stride*24+128)
				ref := make([]uint8, stride*24+128)
				for i := range a {
					a[i] = uint8(rng.Intn(256))
					ref[i] = uint8(rng.Intn(256))
				}
				if iteration == 0 {
					for i := range a {
						a[i], ref[i] = 255, 0
					}
				} else if iteration == 1 {
					copy(ref, a)
				}

				aOff, refOff := stride+off, 2*stride+off
				for _, tc := range cases {
					want := tc.scalar(a, aOff, int32(stride), ref, refOff, int32(stride))
					if got := tc.sse41(a, aOff, int32(stride), ref, refOff, int32(stride)); got != want {
						t.Fatalf("SSE4.1 %s stride=%d off=%d iteration=%d: got %d want %d", tc.name, stride, off, iteration, got, want)
					}
					if tc.avx2 != nil && flags&common.WELS_CPU_AVX2 != 0 {
						if got := tc.avx2(a, aOff, int32(stride), ref, refOff, int32(stride)); got != want {
							t.Fatalf("AVX2 %s stride=%d off=%d iteration=%d: got %d want %d", tc.name, stride, off, iteration, got, want)
						}
					}
				}
			}
		}
	}
}

func TestSampleSIMDDispatch(t *testing.T) {
	ptr := func(fn any) uintptr { return reflect.ValueOf(fn).Pointer() }
	var funcs SWelsFuncPtrList

	WelsInitSampleSadFunc(&funcs, 0)
	if got, want := ptr(funcs.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8]), ptr(common.WelsSampleSad8x8_c); got != want {
		t.Fatalf("scalar SAD dispatch = %#x, want %#x", got, want)
	}
	if got, want := ptr(funcs.sSampleDealingFuncs.pfSampleSatd[BLOCK_8x8]), ptr(WelsSampleSatd8x8_c); got != want {
		t.Fatalf("scalar SATD dispatch = %#x, want %#x", got, want)
	}

	WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2)
	if got, want := ptr(funcs.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8]), ptr(common.WelsSampleSad8x8_sse2); got != want {
		t.Fatalf("SSE2 SAD dispatch = %#x, want %#x", got, want)
	}
	if got, want := ptr(funcs.sSampleDealingFuncs.pfSample4Sad[BLOCK_8x8]), ptr(common.WelsSampleSadFour8x8_sse2); got != want {
		t.Fatalf("SSE2 four-SAD dispatch = %#x, want %#x", got, want)
	}

	sseFlags := uint32(common.WELS_CPU_SSSE3 | common.WELS_CPU_SSE41)
	WelsInitSampleSadFunc(&funcs, sseFlags)
	if got, want := ptr(funcs.sSampleDealingFuncs.pfSampleSatd[BLOCK_8x8]), ptr(WelsSampleSatd8x8_sse41); got != want {
		t.Fatalf("SSE4.1 SATD dispatch = %#x, want %#x", got, want)
	}

	WelsInitSampleSadFunc(&funcs, sseFlags|common.WELS_CPU_AVX2)
	if got, want := ptr(funcs.sSampleDealingFuncs.pfSampleSatd[BLOCK_8x8]), ptr(WelsSampleSatd8x8_avx2); got != want {
		t.Fatalf("AVX2 SATD dispatch = %#x, want %#x", got, want)
	}
	if got, want := ptr(funcs.sSampleDealingFuncs.pfSampleSatd[BLOCK_4x4]), ptr(WelsSampleSatd4x4_sse41); got != want {
		t.Fatalf("AVX2 4x4 SATD dispatch = %#x, want retained SSE4.1 %#x", got, want)
	}
}

func BenchmarkSampleSatd8x8(b *testing.B) {
	const stride = 32
	a := make([]uint8, stride*16)
	ref := make([]uint8, stride*16)
	for i := range a {
		a[i] = uint8(i*17 + 3)
		ref[i] = uint8(i*29 + 11)
	}
	flags := common.WelsCPUFeatureDetect(nil)
	tests := []struct {
		name string
		need uint32
		fn   func([]uint8, int, int32, []uint8, int, int32) int32
	}{
		{"scalar", 0, WelsSampleSatd8x8_c},
		{"sse41", common.WELS_CPU_SSE41 | common.WELS_CPU_SSSE3, WelsSampleSatd8x8_sse41},
		{"avx2", common.WELS_CPU_AVX2, WelsSampleSatd8x8_avx2},
	}
	for _, tc := range tests {
		b.Run(tc.name, func(b *testing.B) {
			if flags&tc.need != tc.need {
				b.Skip("CPU feature unavailable")
			}
			var result int32
			b.ReportAllocs()
			for i := 0; i < b.N; i++ {
				result = tc.fn(a, 3, stride, ref, 5, stride)
			}
			if result == 0 {
				b.Fatal("unexpected zero SATD")
			}
		})
	}
}
