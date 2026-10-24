//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/api"
	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// Uses baseline APIs only; copy unchanged for paired complete-search timings.
func newWholeSearchFixture(block uint8, width, height, stride, motion int, scenario string) (*diamondSadFourCallerFixture, *SDqLayer) {
	f := newDiamondSadFourCallerFixture(block, width, height, stride, motion, common.WELS_CPU_SSE2)
	WelsInitMeFunc(&f.funcs, common.WELS_CPU_SSE2, false)
	layer := &SDqLayer{pRefPic: &SPicture{}}
	layer.iEncStride[0], layer.pRefPic.iLineSize[0] = int32(stride), int32(stride)
	params := &SWelsSvcCodingParam{}
	params.IUsageType, params.IComplexityMode = api.CAMERA_VIDEO_REAL_TIME, api.LOW_COMPLEXITY
	ctx := sWelsEncCtx{pFuncList: &f.funcs, pCurDqLayer: layer, pSvcParam: params, eSliceType: common.P_SLICE}
	PreprocessSliceCoding(&ctx)
	switch scenario {
	case "early":
		f.initial.uSadPredISatd = 1
	case "two":
		f.slice.uiMvcNum = 1
		f.slice.sMvc[0] = SMVUnitXY{4, 0}
	case "many":
		f.slice.uiMvcNum = 5
		f.slice.sMvc = [5]SMVUnitXY{{4, 0}, {-4, 0}, {0, 4}, {0, -4}, {8, 0}}
	}
	return f, layer
}

func BenchmarkWholeIntegerSearchCaller(b *testing.B) {
	shapes := []struct {
		name  string
		block uint8
		w, h  int
	}{
		{"16x16", BLOCK_16x16, 16, 16}, {"8x8", BLOCK_8x8, 8, 8}, {"8x4", BLOCK_8x4, 8, 4}, {"4x4", BLOCK_4x4, 4, 4},
	}
	for _, shape := range shapes {
		for _, stride := range []int{32, 2048} {
			for _, scenario := range []string{"early", "stationary", "moving", "two", "many"} {
				b.Run(fmt.Sprintf("%s/stride%d/%s", shape.name, stride, scenario), func(b *testing.B) {
					motion := 0
					if scenario == "moving" || scenario == "two" || scenario == "many" {
						motion = 1
					}
					f, layer := newWholeSearchFixture(shape.block, shape.w, shape.h, stride, motion, scenario)
					me := f.initial
					b.ReportAllocs()
					for b.Loop() {
						me = f.initial
						WelsMotionEstimateSearch(&f.funcs, layer, &me, &f.slice)
					}
					diamondSadFourCallerSink.mv = me.sMv
					diamondSadFourCallerSink.cost = me.uiSadCost
					diamondSadFourCallerSink.refOff = me.iRefMbOff
				})
			}
		}
	}
}
