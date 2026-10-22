package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// This fixture uses only baseline APIs. The dispatched case initializes the
// production function table in both trees; no benchmark shim is needed.
func BenchmarkUpdateMbMv(b *testing.B) {
	var functions SWelsFuncPtrList
	InitIntraAnalysisVaaInfo(&functions, common.WelsCPUFeatureDetect(nil))
	for _, method := range []struct {
		name string
		fn   PUpdateMbMvFunc
	}{
		{"Scalar", UpdateMbMv_c},
		{"Dispatched", functions.pfUpdateMbMv},
	} {
		for _, blocks := range []int{1, 120 * 68} {
			for _, origin := range []int{0, 1, 2, 3} {
				b.Run(fmt.Sprintf("%s/Blocks%d/Origin%d", method.name, blocks, origin), func(b *testing.B) {
					backing := make([]SMVUnitXY, blocks*16+origin+1)
					dst := backing[origin : origin+blocks*16]
					value := SMVUnitXY{-32768, 17329}
					b.ReportAllocs()
					if blocks == 1 {
						for b.Loop() {
							method.fn(dst, value)
						}
					} else {
						for b.Loop() {
							for block := 0; block < blocks; block++ {
								method.fn(dst[block*16:block*16+16], value)
							}
						}
					}
					b.ReportMetric(float64(blocks), "blocks/op")
					if dst[0] != value || dst[len(dst)-1] != value {
						b.Fatal("fill produced an unexpected motion vector")
					}
				})
			}
		}
	}
}

// Complete active skip decision, using the existing real MC, SAD/SATD and
// transform fixture. Initialization and expected-result checking are untimed.
// Rejected skip controls perform no fill and expose unrelated regressions.
func BenchmarkUpdateMbMvSkipCaller(b *testing.B) {
	flags := common.WelsCPUFeatureDetect(nil)
	for _, scenario := range []string{"Direct", "PredThreshold", "RefThreshold", "RejectLuma", "RejectU", "RejectV", "TransformSuccess"} {
		for _, useSAD := range []bool{true, false} {
			mode := "SAD"
			if !useSAD {
				mode = "SATD"
			}
			b.Run(scenario+"/"+mode, func(b *testing.B) {
				s := newSkipDctQuantCallerState(scenario, flags, useSAD)
				InitIntraAnalysisVaaInfo(&s.functions, flags)
				want := scenario != "RejectLuma" && scenario != "RejectU" && scenario != "RejectV"
				if got := WelsMdPSkipEnc(&s.ctx, &s.md, &s.blocks[1], &s.cache); got != want {
					b.Fatalf("fixture returned %v, want %v", got, want)
				}
				b.ReportAllocs()
				var got bool
				for b.Loop() {
					got = WelsMdPSkipEnc(&s.ctx, &s.md, &s.blocks[1], &s.cache)
				}
				skipDctQuantCallerBenchmarkSink = got
			})
		}
	}
}
