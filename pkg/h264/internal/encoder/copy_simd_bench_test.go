package encoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Use existing function-table entries so this same benchmark can be built
// against the scalar baseline and SIMD candidate without changing the harness.
func BenchmarkEncodingBlockCopy(b *testing.B) {
	var functions SWelsFuncPtrList
	WelsInitEncodingFuncs(&functions, common.WelsCPUFeatureDetect(nil))
	cases := []struct {
		name string
		fn   PCopyFunc
	}{
		{"8x8", functions.pfCopy8x8Aligned},
		{"8x16", functions.pfCopy8x16Aligned},
		{"16x8", functions.pfCopy16x8NotAligned},
		{"16x16", functions.pfCopy16x16NotAligned},
	}
	for _, test := range cases {
		b.Run(test.name, func(b *testing.B) {
			var src, dst [32*16 + 7]byte
			for i := range src {
				src[i] = byte(i*29 + 3)
			}
			b.ReportAllocs()
			for b.Loop() {
				test.fn(dst[:], 5, 32, src[:], 3, 32)
			}
		})
	}
}
