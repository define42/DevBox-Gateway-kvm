//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func requireSampleAVX2(t *testing.T) {
	t.Helper()
	if common.WelsCPUFeatureDetect(nil)&common.WELS_CPU_AVX2 == 0 {
		t.Skip("AVX2 required")
	}
}

func sampleAvgSatdReference(enc []byte, encOff int, encStride int32,
	a []byte, aOff int, aStride int32, b []byte, bOff int, bStride int32, width, height int32) int32 {
	var avg [256]byte
	common.PixelAvg_c(avg[:], 0, width, a, aOff, aStride, b, bOff, bStride, width, height)
	var cost int32
	for y := int32(0); y < height; y += 4 {
		for x := int32(0); x < width; x += 4 {
			cost += WelsSampleSatd4x4_c(enc, encOff+int(y*encStride+x), encStride,
				avg[:], int(y*width+x), width)
		}
	}
	return cost
}

func TestSampleAvgSatdAVX2Parity(t *testing.T) {
	requireSampleAVX2(t)
	rng := rand.New(rand.NewSource(0x264a))
	for _, size := range [][2]int32{{8, 4}, {8, 8}, {8, 16}, {16, 8}, {16, 16}} {
		w, h := size[0], size[1]
		for _, strides := range [][3]int32{{32, 32, 32}, {23, 27, 31}, {0, 3, 5}, {-23, -27, -31}} {
			for iteration := 0; iteration < 120; iteration++ {
				var inputs [3][]byte
				var offsets [3]int
				for n, stride := range strides {
					first, last := 7, 7+int((h-1)*stride)
					if last < first {
						first, last = last, first
					}
					offsets[n] = 7 - first + 3
					inputs[n] = make([]byte, last-first+3+int(w))
					rng.Read(inputs[n])
					if iteration < 4 {
						for i := range inputs[n] {
							switch iteration {
							case 0:
								inputs[n][i] = byte(n * 127)
							case 1:
								inputs[n][i] = 255
							case 2:
								inputs[n][i] = byte((i + n) % 2 * 255)
							case 3:
								inputs[n][i] = byte((i/4 + n) % 2)
							}
						}
					}
				}
				got := sampleAvgSatdAVX2(inputs[0], offsets[0], strides[0], inputs[1], offsets[1], strides[1], inputs[2], offsets[2], strides[2], w, h)
				want := sampleAvgSatdReference(inputs[0], offsets[0], strides[0], inputs[1], offsets[1], strides[1], inputs[2], offsets[2], strides[2], w, h)
				if got != want {
					t.Fatalf("size=%v strides=%v iteration=%d got=%d want=%d", size, strides, iteration, got, want)
				}
			}
		}
	}
}

func TestSampleSatdBatchUnusualStrides(t *testing.T) {
	requireSampleAVX2(t)
	for _, size := range [][2]int{{8, 8}, {8, 16}, {16, 8}, {16, 16}} {
		for _, stride := range []int{0, 3, -19, 19} {
			w, h := size[0], size[1]
			off := 3
			if stride < 0 {
				off -= (h - 1) * stride
			}
			span := (h - 1) * stride
			if span < 0 {
				span = -span
			}
			a, b := make([]byte, span+w+3), make([]byte, span+w+3)
			for i := range a {
				a[i], b[i] = byte(i*17), byte(i*23+1)
			}
			got := sampleSatdBlockAVX2(a, off, int32(stride), b, off, int32(stride), w, h)
			var want int32
			for y := 0; y < h; y += 4 {
				for x := 0; x < w; x += 4 {
					want += WelsSampleSatd4x4_c(a, off+y*stride+x, int32(stride), b, off+y*stride+x, int32(stride))
				}
			}
			if got != want {
				t.Fatalf("size=%v stride=%d got=%d want=%d", size, stride, got, want)
			}
		}
	}
}

func TestSampleAvgSatdDispatch(t *testing.T) {
	var funcs SWelsFuncPtrList
	for _, flags := range []uint32{0, common.WELS_CPU_SSE2, common.WELS_CPU_AVX2, 0} {
		WelsInitSampleSadFunc(&funcs, flags)
		if got, want := funcs.sSampleDealingFuncs.pfSampleAvgSatd != nil, flags&common.WELS_CPU_AVX2 != 0; got != want {
			t.Fatalf("flags=%x fused=%v want=%v", flags, got, want)
		}
	}
}

func TestQuarterRefineFusedMatchesSequential(t *testing.T) {
	requireSampleAVX2(t)
	sizes := [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}}
	for _, size := range sizes {
		for scenario := 0; scenario < 9; scenario++ {
			t.Run(fmt.Sprintf("%dx%d/scenario%d", size[0], size[1], scenario), func(t *testing.T) {
				type result struct {
					cost, mode int32
					best, tmp  []byte
					swapped    bool
					fusedCalls int
				}
				run := func(fused bool) result {
					var funcs SWelsFuncPtrList
					WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2|common.WELS_CPU_SSSE3|common.WELS_CPU_SSE41|common.WELS_CPU_AVX2)
					common.InitMcFunc(&funcs.sMcFuncs, common.WELS_CPU_SSE2)
					funcs.sSampleDealingFuncs.pfMeCost = &funcs.sSampleDealingFuncs.pfSampleSatd
					if scenario == 8 {
						funcs.sSampleDealingFuncs.pfMeCost = &funcs.sSampleDealingFuncs.pfSampleSad
					}
					calls := 0
					if fused {
						funcs.sSampleDealingFuncs.pfSampleAvgSatd = func(enc []byte, encOff int, encStride int32, a []byte, aOff int, aStride int32, b []byte, bOff int, bStride int32, w, h int32) int32 {
							calls++
							return sampleAvgSatdAVX2(enc, encOff, encStride, a, aOff, aStride, b, bOff, bStride, w, h)
						}
					} else {
						funcs.sSampleDealingFuncs.pfSampleAvgSatd = nil
					}
					rng := rand.New(rand.NewSource(364))
					enc := make([]byte, 1024)
					rng.Read(enc)
					scratch := bytes.Repeat([]byte{0xa5}, 2048)
					refine := SMeRefinePointer{pQuarPixBest: scratch[:], pQuarPixTmp: scratch[640:]}
					me := SWelsME{pEncMb: enc, iEncMbOff: 3, uiBlockSize: uint8(size[2])}
					params := SQuarRefineParams{iStrideA: 27, iStrideB: 29, iBestCost: 1 << 30, iBestQuarPix: ME_NO_BEST_QUAR_PIXEL}
					if scenario == 1 {
						params.iBestCost = 0
					}
					for i := 0; i < 4; i++ {
						params.pSrcA[i] = make([]byte, 1024)
						params.pSrcB[i] = make([]byte, 1024)
						rng.Read(params.pSrcA[i])
						rng.Read(params.pSrcB[i])
						params.iSrcAOff[i], params.iSrcBOff[i] = i+1, i+3
						params.iLms[i] = int32((3 - i) * 7)
						if scenario == 2 || scenario == 3 {
							clear(params.pSrcA[i])
							clear(params.pSrcB[i])
							clear(enc)
							params.iLms[i] = 20
							if scenario == 3 {
								params.iLms[i] = int32(20 - i*5)
							}
						}
					}
					// Exercise exact and shifted input/output aliases, plus
					// overlapping output buffers. These retain old semantics.
					if scenario == 4 {
						params.pSrcA[2] = refine.pQuarPixTmp
						params.iSrcAOff[2] = 0
					}
					if scenario == 5 {
						params.pSrcB[3] = refine.pQuarPixBest
						params.iSrcBOff[3] = 1
					}
					if scenario == 6 {
						me.pEncMb = refine.pQuarPixTmp
					}
					if scenario == 7 {
						refine.pQuarPixTmp = scratch[1:]
					}
					MeRefineQuarPixel(&funcs, &me, &refine, size[0], size[1], &params, 31)
					best, tmp := []byte{}, []byte{}
					for y := int32(0); y < size[1]; y++ {
						start := int(y * ME_REFINE_BUF_STRIDE)
						best = append(best, refine.pQuarPixBest[start:start+int(size[0])]...)
						tmp = append(tmp, refine.pQuarPixTmp[start:start+int(size[0])]...)
					}
					return result{params.iBestCost, params.iBestQuarPix, best, tmp, &refine.pQuarPixBest[0] != &scratch[0], calls}
				}
				got, want := run(true), run(false)
				if got.cost != want.cost || got.mode != want.mode || got.swapped != want.swapped || !bytes.Equal(got.best, want.best) {
					t.Fatalf("fused cost/mode/swap=%d/%d/%v sequential=%d/%d/%v bestEqual=%v", got.cost, got.mode, got.swapped, want.cost, want.mode, want.swapped, bytes.Equal(got.best, want.best))
				}
				shouldFuse := size[0] >= 8 && scenario < 4
				if (got.fusedCalls > 0) != shouldFuse {
					t.Fatalf("fusedCalls=%d shouldFuse=%v", got.fusedCalls, shouldFuse)
				}
				if !shouldFuse && !bytes.Equal(got.tmp, want.tmp) {
					t.Fatal("fallback scratch differs")
				}
			})
		}
	}
}
