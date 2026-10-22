//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// This file intentionally uses only fields/APIs present before quarter
// batching. Copy it unchanged to the baseline or failed candidate: allocating
// params outside the measured loop would hide a per-block escape regression.
func TestQuarterRefinePerInvocationParamsNoAllocs(t *testing.T) {
	requireSampleAVX2(t)
	for _, size := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		t.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(t *testing.T) {
			f := newQuarterBatchFixture(uint8(size[2]))
			allocs := testing.AllocsPerRun(100, func() {
				params := f.params
				MeRefineQuarPixel(&f.funcs, &f.me, &f.refine, size[0], size[1], &params, 31)
				sampleSatdBenchmarkSink = params.iBestCost
			})
			if allocs != 0 {
				t.Fatalf("local refinement parameters allocate %.2f times/call, want zero", allocs)
			}
		})
	}
}

func BenchmarkQuarterRefineLocalParams(b *testing.B) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		b.Skip("AVX2 required")
	}
	for _, size := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		b.Run(fmt.Sprintf("%dx%d", size[0], size[1]), func(b *testing.B) {
			f := newQuarterBatchFixture(uint8(size[2]))
			b.ReportAllocs()
			for b.Loop() {
				params := f.params
				MeRefineQuarPixel(&f.funcs, &f.me, &f.refine, size[0], size[1], &params, 31)
				sampleSatdBenchmarkSink = params.iBestCost
			}
		})
	}
}

type fractionalCallerFixture struct {
	ctx    sWelsEncCtx
	layer  SDqLayer
	pic    SPicture
	funcs  SWelsFuncPtrList
	me     SWelsME
	refine SMeRefinePointer
	cache  SMbCache
	pred   []byte
	width  int32
	height int32
}

func newFractionalCallerFixture(width, height int32, block uint8, stride int32, motion bool) *fractionalCallerFixture {
	f := &fractionalCallerFixture{width: width, height: height, pred: make([]byte, 256)}
	flags := uint32(common.WELS_CPU_SSE2 | common.WELS_CPU_SSSE3 | common.WELS_CPU_SSE41 | common.WELS_CPU_AVX2)
	WelsInitSampleSadFunc(&f.funcs, flags)
	common.InitMcFunc(&f.funcs.sMcFuncs, flags)
	f.funcs.sSampleDealingFuncs.pfMeCost = &f.funcs.sSampleDealingFuncs.pfSampleSatd
	f.ctx.pFuncList, f.ctx.pCurDqLayer = &f.funcs, &f.layer
	f.layer.iEncStride[0], f.pic.iLineSize[0] = stride, stride
	f.layer.pRefPic, f.layer.bSatdInMdFlag = &f.pic, true
	f.cache.pBufferInterPredMe = make([]byte, 4*640)
	f.me.uiBlockSize = block
	f.me.pEncMb, f.me.iEncMbOff = make([]byte, int(height-1)*int(stride)+int(width)+3), 3
	f.me.pRefMb, f.me.iRefMbOff = make([]byte, int(height+12)*int(stride)), 4*int(stride)+8
	for i := range f.me.pRefMb {
		f.me.pRefMb[i] = byte(i*73 + (i/int(stride))*17 + 19)
	}
	for y := int32(0); y < height; y++ {
		for x := int32(0); x < width; x++ {
			value := f.me.pRefMb[f.me.iRefMbOff+int(y*stride+x)]
			if motion {
				value = byte(x*37 + y*29 + x*y*11 + 7)
			}
			f.me.pEncMb[f.me.iEncMbOff+int(y*stride+x)] = value
		}
	}
	f.me.pMvdCost, f.me.iMvdCostOff = make([]uint16, 1025), 512
	for i := range f.me.pMvdCost {
		distance := i - f.me.iMvdCostOff
		if distance < 0 {
			distance = -distance
		}
		f.me.pMvdCost[i] = uint16(distance)
	}
	f.me.uSadPredISatd = uint32(f.funcs.sSampleDealingFuncs.pfMeCost[block](
		f.me.pEncMb, f.me.iEncMbOff, stride, f.me.pRefMb, f.me.iRefMbOff, stride))
	copies := [MAX_BLOCK_TYPE]PCopyFunc{
		common.WelsCopy16x16_c, common.WelsCopy16x8_c, common.WelsCopy8x16_c,
		common.WelsCopy8x8_c, common.WelsCopy4x4_c, common.WelsCopy8x4_c, common.WelsCopy4x8_c,
	}
	f.refine.pfCopyBlockByMode = copies[block]
	return f
}

func (f *fractionalCallerFixture) run() {
	f.me.sMv = SMVUnitXY{}
	f.me.uiSatdCost = 0
	InitMeRefinePointer(&f.refine, &f.cache, 0)
	f.refine.pHalfPixHV = nil
	// This is the production caller that creates SQuarRefineParams locally,
	// includes the half-pixel filters/scoring, and consumes quarter results.
	MeRefineFracPixel(&f.ctx, f.pred, 0, &f.me, &f.refine, f.width, f.height)
}

func TestMeRefineFracPixelNoPerBlockAllocs(t *testing.T) {
	requireSampleAVX2(t)
	for _, size := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		for _, motion := range []bool{false, true} {
			t.Run(fmt.Sprintf("%dx%d/motion%v", size[0], size[1], motion), func(t *testing.T) {
				f := newFractionalCallerFixture(size[0], size[1], uint8(size[2]), 2048, motion)
				allocs := testing.AllocsPerRun(100, f.run)
				if allocs != 0 {
					t.Fatalf("full fractional refinement allocates %.2f times/block, want zero", allocs)
				}
			})
		}
	}
}

func BenchmarkMeRefineFracPixelCaller(b *testing.B) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		b.Skip("AVX2 required")
	}
	for _, size := range [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}} {
		for _, stride := range []int32{32, 2048} {
			for _, motion := range []bool{false, true} {
				b.Run(fmt.Sprintf("%dx%d/stride%d/motion%v", size[0], size[1], stride, motion), func(b *testing.B) {
					f := newFractionalCallerFixture(size[0], size[1], uint8(size[2]), stride, motion)
					b.ReportAllocs()
					for b.Loop() {
						f.run()
					}
					sampleSatdBenchmarkSink = int32(f.me.uiSatdCost)
				})
			}
		}
	}
}
