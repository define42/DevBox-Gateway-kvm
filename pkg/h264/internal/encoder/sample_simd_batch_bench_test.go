//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

var sampleSatdBenchmarkSink int32

func BenchmarkSampleSatdBlocks(b *testing.B) {
	flags := common.WelsCPUFeatureDetect(nil)
	for _, tc := range []struct {
		name             string
		scalar, sse, avx PSampleSadSatdCostFunc
	}{
		{"8x8", WelsSampleSatd8x8_c, WelsSampleSatd8x8_sse41, WelsSampleSatd8x8_avx2},
		{"16x8", WelsSampleSatd16x8_c, WelsSampleSatd16x8_sse41, WelsSampleSatd16x8_avx2},
		{"8x16", WelsSampleSatd8x16_c, WelsSampleSatd8x16_sse41, WelsSampleSatd8x16_avx2},
		{"16x16", WelsSampleSatd16x16_c, WelsSampleSatd16x16_sse41, WelsSampleSatd16x16_avx2},
	} {
		for _, impl := range []struct {
			name string
			flag uint32
			fn   PSampleSadSatdCostFunc
		}{
			{"scalar", 0, tc.scalar}, {"sse41", common.WELS_CPU_SSE41 | common.WELS_CPU_SSSE3, tc.sse}, {"avx2", common.WELS_CPU_AVX2, tc.avx},
		} {
			if flags&impl.flag != impl.flag {
				continue
			}
			b.Run(tc.name+"/"+impl.name, func(b *testing.B) {
				a, ref := make([]byte, 32*16+3), make([]byte, 32*16+7)
				for i := range a {
					a[i] = byte(i * 17)
				}
				for i := range ref {
					ref[i] = byte(i*23 + 3)
				}
				b.ReportAllocs()
				var cost int32
				for b.Loop() {
					cost = impl.fn(a, 3, 32, ref, 7, 32)
				}
				sampleSatdBenchmarkSink = cost
			})
		}
	}
}

func BenchmarkSampleAvgSatd(b *testing.B) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		b.Skip("AVX2 required")
	}
	for _, size := range [][2]int32{{8, 8}, {16, 8}, {8, 16}, {16, 16}} {
		for _, fused := range []bool{false, true} {
			b.Run(fmt.Sprintf("%dx%d/fused=%v", size[0], size[1], fused), func(b *testing.B) {
				enc, a, c, tmp := make([]byte, 1024), make([]byte, 1024), make([]byte, 1024), make([]byte, 1024)
				for i := range enc {
					enc[i], a[i], c[i] = byte(i*17), byte(i*31+1), byte(i*13+2)
				}
				var cost int32
				b.ReportAllocs()
				for b.Loop() {
					if fused {
						cost = sampleAvgSatdAVX2(enc, 3, 31, a, 5, 32, c, 7, 29, size[0], size[1])
					} else {
						common.PixelAvg_sse2(tmp, 0, 32, a, 5, 32, c, 7, 29, size[0], size[1])
						cost = sampleSatdBlockAVX2(enc, 3, 31, tmp, 0, 32, int(size[0]), int(size[1]))
					}
				}
				sampleSatdBenchmarkSink = cost
			})
		}
	}
}

func BenchmarkQuarterRefineFused(b *testing.B) {
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		b.Skip("AVX2 required")
	}
	for _, fused := range []bool{false, true} {
		b.Run(fmt.Sprintf("fused=%v", fused), func(b *testing.B) {
			var funcs SWelsFuncPtrList
			WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2|common.WELS_CPU_SSE41|common.WELS_CPU_SSSE3|common.WELS_CPU_AVX2)
			funcs.sSampleDealingFuncs.pfMeCost = &funcs.sSampleDealingFuncs.pfSampleSatd
			if !fused {
				funcs.sSampleDealingFuncs.pfSampleAvgSatd = nil
			}
			common.InitMcFunc(&funcs.sMcFuncs, common.WELS_CPU_SSE2)
			enc, scratch := make([]byte, 1024), make([]byte, 2560)
			for i := range enc {
				enc[i] = byte(i*17 + 3)
			}
			me := SWelsME{pEncMb: enc, uiBlockSize: BLOCK_16x16}
			params := SQuarRefineParams{iStrideA: 29, iStrideB: 31}
			for i := 0; i < 4; i++ {
				params.pSrcA[i], params.pSrcB[i] = make([]byte, 1024), make([]byte, 1024)
				for j := range params.pSrcA[i] {
					params.pSrcA[i][j], params.pSrcB[i][j] = byte(j*13+i), byte(j*31+i)
				}
			}
			refine := SMeRefinePointer{pQuarPixBest: scratch[1280:], pQuarPixTmp: scratch[1920:]}
			b.ReportAllocs()
			for b.Loop() {
				params.iBestCost, params.iBestQuarPix = 1<<30, ME_NO_BEST_QUAR_PIXEL
				MeRefineQuarPixel(&funcs, &me, &refine, 16, 16, &params, 32)
			}
			sampleSatdBenchmarkSink = params.iBestCost
		})
	}
}
