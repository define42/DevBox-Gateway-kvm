//go:build amd64

package encoder

import (
	"bytes"
	"fmt"
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type quarterBatchFixture struct {
	funcs   SWelsFuncPtrList
	me      SWelsME
	refine  SMeRefinePointer
	params  SQuarRefineParams
	scratch []byte
}

func newQuarterBatchFixture(block uint8) *quarterBatchFixture {
	f := &quarterBatchFixture{}
	WelsInitSampleSadFunc(&f.funcs, common.WELS_CPU_SSE2|common.WELS_CPU_SSSE3|common.WELS_CPU_SSE41|common.WELS_CPU_AVX2)
	common.InitMcFunc(&f.funcs.sMcFuncs, common.WELS_CPU_SSE2)
	f.funcs.sSampleDealingFuncs.pfMeCost = &f.funcs.sSampleDealingFuncs.pfSampleSatd
	f.me = SWelsME{pEncMb: make([]byte, 1024), iEncMbOff: 3, uiBlockSize: block}
	f.scratch = bytes.Repeat([]byte{0xa5}, 2048)
	f.refine = SMeRefinePointer{pQuarPixBest: f.scratch[:], pQuarPixTmp: f.scratch[640:]}
	f.params = SQuarRefineParams{iStrideA: 27, iStrideB: 29, iBestCost: 1 << 30, iBestQuarPix: ME_NO_BEST_QUAR_PIXEL}
	rng := rand.New(rand.NewSource(66420))
	rng.Read(f.me.pEncMb)
	for i := range 4 {
		f.params.pSrcA[i], f.params.pSrcB[i] = make([]byte, 1024), make([]byte, 1024)
		rng.Read(f.params.pSrcA[i])
		rng.Read(f.params.pSrcB[i])
		f.params.iSrcAOff[i], f.params.iSrcBOff[i] = i+1, i+3
		f.params.iLms[i] = int32((3 - i) * 7)
	}
	return f
}

func TestQuarterRefineValidatedMatchesSequential(t *testing.T) {
	requireSampleAVX2(t)
	sizes := [][3]int32{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}}
	for _, size := range sizes {
		for scenario := range 11 {
			t.Run(fmt.Sprintf("%dx%d/scenario%d", size[0], size[1], scenario), func(t *testing.T) {
				run := func(validated bool) (*quarterBatchFixture, int) {
					f := newQuarterBatchFixture(uint8(size[2]))
					calls := 0
					if validated {
						f.funcs.sSampleDealingFuncs.bSampleAvgSatdFour = false
						f.funcs.sSampleDealingFuncs.pfSampleAvgSatdValidated = func(enc *byte, encStride int,
							a *byte, aStride int, b *byte, bStride, width, height int) int {
							calls++
							return satdAvgBlockAVX2(enc, encStride, a, aStride, b, bStride, width, height)
						}
					} else {
						f.funcs.sSampleDealingFuncs.pfSampleAvgSatd = nil
					}
					encStride := int32(31)
					switch scenario {
					case 1:
						f.params.iBestCost = 0
					case 2, 3:
						clear(f.me.pEncMb)
						for i := range 4 {
							clear(f.params.pSrcA[i])
							clear(f.params.pSrcB[i])
							f.params.iLms[i] = 20
							if scenario == 3 {
								f.params.iLms[i] -= int32(i * 5)
							}
						}
					case 4:
						f.params.pSrcA[2], f.params.iSrcAOff[2] = f.refine.pQuarPixTmp, 0
					case 5:
						f.params.pSrcB[3], f.params.iSrcBOff[3] = f.refine.pQuarPixBest, 1
					case 6:
						f.me.pEncMb = f.refine.pQuarPixTmp
					case 7:
						f.refine.pQuarPixTmp = f.scratch[1:]
					case 8:
						f.funcs.sSampleDealingFuncs.pfMeCost = &f.funcs.sSampleDealingFuncs.pfSampleSad
					case 9:
						f.params.iStrideA, f.params.iStrideB = 0, 0
					case 10:
						encStride = 0
					}
					MeRefineQuarPixel(&f.funcs, &f.me, &f.refine, size[0], size[1], &f.params, encStride)
					return f, calls
				}
				got, calls := run(true)
				want, _ := run(false)
				if got.params.iBestCost != want.params.iBestCost || got.params.iBestQuarPix != want.params.iBestQuarPix {
					t.Fatalf("got cost/mode=%d/%d, want %d/%d", got.params.iBestCost, got.params.iBestQuarPix,
						want.params.iBestCost, want.params.iBestQuarPix)
				}
				gotSwap := &got.refine.pQuarPixBest[0] != &got.scratch[0]
				wantSwap := &want.refine.pQuarPixBest[0] != &want.scratch[0]
				if gotSwap != wantSwap {
					t.Fatalf("buffer swap=%v, want %v", gotSwap, wantSwap)
				}
				shouldValidate := size[0] >= 8 && scenario < 4
				if (calls == 4) != shouldValidate || (calls != 0 && calls != 4) {
					t.Fatalf("validated calls=%d, enabled=%v", calls, shouldValidate)
				}
				for y := int32(0); y < size[1]; y++ {
					start, end := y*ME_REFINE_BUF_STRIDE, y*ME_REFINE_BUF_STRIDE+size[0]
					if !bytes.Equal(got.refine.pQuarPixBest[start:end], want.refine.pQuarPixBest[start:end]) {
						t.Fatalf("best row %d differs", y)
					}
					if !shouldValidate && !bytes.Equal(got.refine.pQuarPixTmp[start:end], want.refine.pQuarPixTmp[start:end]) {
						t.Fatalf("fallback temporary row %d differs", y)
					}
				}
			})
		}
	}
}

func TestQuarterRefineValidatedChecksEveryVisibleSpan(t *testing.T) {
	requireSampleAVX2(t)
	for input := range 11 {
		t.Run(fmt.Sprintf("span%d", input), func(t *testing.T) {
			f := newQuarterBatchFixture(BLOCK_16x16)
			calls := 0
			f.funcs.sSampleDealingFuncs.pfSampleAvgSatdValidated = func(_ *byte, _ int,
				_ *byte, _ int, _ *byte, _, _, _ int) int {
				calls++
				return 0
			}
			switch input {
			case 0:
				f.me.pEncMb = f.me.pEncMb[:f.me.iEncMbOff+15*31+15]
			case 1:
				f.refine.pQuarPixBest = f.refine.pQuarPixBest[:15*ME_REFINE_BUF_STRIDE+15]
			case 2:
				f.refine.pQuarPixTmp = f.refine.pQuarPixTmp[:15*ME_REFINE_BUF_STRIDE+15]
			default:
				i := (input - 3) / 2
				if input%2 == 1 {
					f.params.pSrcA[i] = f.params.pSrcA[i][:f.params.iSrcAOff[i]+15*ME_REFINE_BUF_STRIDE+15]
				} else {
					stride := f.params.iStrideA
					if i >= 2 {
						stride = f.params.iStrideB
					}
					f.params.pSrcB[i] = f.params.pSrcB[i][:f.params.iSrcBOff[i]+15*int(stride)+15]
				}
			}
			// This check must use len, even though the backing allocation is
			// large enough. It must reject the whole batch before any raw call.
			if quarterRefineBuffersDisjoint(&f.me, &f.refine, &f.params, 31, 16, 16) {
				t.Fatal("short visible span accepted")
			}
			panicked := false
			func() {
				defer func() { panicked = recover() != nil }()
				MeRefineQuarPixel(&f.funcs, &f.me, &f.refine, 16, 16, &f.params, 31)
			}()
			if !panicked {
				t.Fatal("short visible span did not retain the checked fallback panic")
			}
			if calls != 0 {
				t.Fatalf("invalid batch made %d raw calls", calls)
			}
		})
	}
}
