package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var intraVaaBenchmarkSink int32

// Only pre-existing APIs are used so the same source benchmarks the scalar
// baseline and selected candidate through the complete checked dispatch call.
func BenchmarkIntraVaaDispatch(b *testing.B) {
	for _, dispatch := range []struct {
		name  string
		flags uint32
	}{
		{"detected", common.WelsCPUFeatureDetect(nil)}, {"scalar", 0},
	} {
		var funcs SWelsFuncPtrList
		InitIntraAnalysisVaaInfo(&funcs, dispatch.flags)
		for _, stride := range []int32{16, 32, 2048} {
			for _, offset := range []int{0, 3} {
				b.Run(fmt.Sprintf("%s/stride%d/off%d", dispatch.name, stride, offset), func(b *testing.B) {
					src := make([]byte, offset+15*int(stride)+16)
					for i := range src {
						src[i] = byte(i*37 + i/13 + 7)
					}
					var result int32
					b.ReportAllocs()
					for b.Loop() {
						result = funcs.pfGetVarianceFromIntraVaa(src, offset, stride)
					}
					intraVaaBenchmarkSink = result
				})
			}
		}
	}
}
