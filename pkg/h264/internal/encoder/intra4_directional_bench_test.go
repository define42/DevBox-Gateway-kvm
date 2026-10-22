package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Baseline-compatible, self-contained checked-predictor benchmark. The actual
// initialized callback is timed; scalar controls expose unrelated layout noise.
func BenchmarkIntra4Directional(b *testing.B) {
	var f SWelsFuncPtrList
	WelsInitIntraPredFuncs(&f, common.WelsCPUFeatureDetect(nil))
	for _, tc := range []struct {
		name   string
		mode   int
		scalar func([]byte, int, []byte, int, int32)
	}{
		{"HD", common.I4_PRED_HD, WelsI4x4LumaPredHD_c},
		{"HU", common.I4_PRED_HU, WelsI4x4LumaPredHU_c},
		{"VR", common.I4_PRED_VR, WelsI4x4LumaPredVR_c},
	} {
		for _, stride := range []int32{32, 1984} {
			for _, dispatched := range []bool{false, true} {
				method := "Scalar"
				call := tc.scalar
				if dispatched {
					method = "Dispatched"
					call = f.pfGetLumaI4x4Pred[tc.mode]
				}
				b.Run(fmt.Sprintf("%s/stride%d/%s", tc.name, stride, method), func(b *testing.B) {
					ref := make([]byte, 5*int(stride)+16)
					for i := range ref {
						ref[i] = byte(i*73 + i*i*11 + 29)
					}
					var dst [24]byte
					b.ReportAllocs()
					for b.Loop() {
						call(dst[:], 3, ref, int(stride)+5, stride)
					}
					intra4DirectionalBenchmarkSink = dst[3] + dst[18]
				})
			}
		}
	}
}

var intra4DirectionalBenchmarkSink byte
