package encoder

import (
	"fmt"
	"math/rand"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestLazySkipMatchesOriginal(t *testing.T) {
	rng := rand.New(rand.NewSource(0x264515))
	for qp := 0; qp <= 51; qp++ {
		for scenario := 0; scenario < 16; scenario++ {
			name := fmt.Sprintf("QP%d/scenario%d", qp, scenario)
			t.Run(name, func(t *testing.T) {
				stride := int32([]int{16, 19, 31}[scenario%3])
				const sourceOffset = 7
				source := make([]byte, sourceOffset+15*int(stride)+16+9)
				prediction := make([]byte, 384)
				rng.Read(source)
				rng.Read(prediction)
				for y := 0; y < 16; y++ {
					for x := 0; x < 16; x++ {
						value := int(prediction[y*16+x])
						switch {
						case scenario == 0: // Exactly equal pixels always accept all groups.
						case scenario <= 4: // First rejection at a selected 8x8 group.
							prediction[y*16+x], value = 255, 255
							if (y/8)*2+x/8 == scenario-1 {
								value = 0
							}
						case scenario <= 8: // Sparse, small residuals around skip thresholds.
							if rng.Intn(12) == 0 {
								value += rng.Intn(2*(scenario-4)+1) - (scenario - 4)
							}
						case scenario <= 12: // Dense, small residuals.
							amplitude := 1 << (scenario - 9)
							value += rng.Intn(amplitude*2+1) - amplitude
						default: // Independent full-range source and prediction.
							value = rng.Intn(256)
						}
						source[sourceOffset+y*int(stride)+x] = byte(max(0, min(255, value)))
					}
				}

				var initialCoefficients [384]int16
				var initialLevels SDCTCoeff
				for i := range initialCoefficients {
					initialCoefficients[i] = int16(i*97 + 11)
				}
				for i := range initialLevels.iLumaBlock {
					initialLevels.iLumaBlock[i] = int16(i*13 - 3000)
				}
				wantCoefficients, preCoefficients, lazyCoefficients := initialCoefficients, initialCoefficients, initialCoefficients
				wantLevels, preLevels, lazyLevels := initialLevels, initialLevels, initialLevels
				wantCache := skipLazyCache(wantCoefficients[:], &wantLevels, source, sourceOffset, prediction)
				preCache := skipLazyCache(preCoefficients[:], &preLevels, source, sourceOffset, prediction)
				lazyCache := skipLazyCache(lazyCoefficients[:], &lazyLevels, source, sourceOffset, prediction)
				mb := SMB{uiLumaQp: uint8(qp)}
				var scalar, fast SWelsFuncPtrList
				WelsInitEncodingFuncs(&scalar, 0)
				WelsInitEncodingFuncs(&fast, common.WELS_CPU_SSE2)
				layer := SDqLayer{iEncStride: [3]int32{stride, 8, 8}}
				wantContext := sWelsEncCtx{pFuncList: &scalar, pCurDqLayer: &layer}
				fastContext := sWelsEncCtx{pFuncList: &fast, pCurDqLayer: &layer}

				WelsDctMb(wantCoefficients[:], source, sourceOffset, stride, prediction, 0, scalar.pfDctFourT4)
				want, groups := originalTryPYskip(&wantContext, &mb, &wantCache)
				WelsDctMb(preCoefficients[:], source, sourceOffset, stride, prediction, 0, fast.pfDctFourT4)
				if got := WelsTryPYskip(&fastContext, &mb, &preCache); got != want || preCoefficients != wantCoefficients || preLevels != wantLevels {
					t.Fatalf("pretransformed result/state differs: accepted=%v want=%v", got, want)
				}

				transforms := 0
				transform := fast.pfDctFourT4
				fast.pfDctFourT4 = func(coeff []int16, src []byte, srcOff int, srcStride int32, pred []byte, predOff int, predStride int32) {
					group := transforms
					transforms++
					if group >= 4 || srcOff != sourceOffset+(group>>1)*8*int(stride)+(group&1)*8 || predOff != (group>>1)*128+(group&1)*8 {
						t.Fatalf("unexpected transform order: group=%d source=%d prediction=%d", group, srcOff, predOff)
					}
					transform(coeff, src, srcOff, srcStride, pred, predOff, predStride)
				}
				if got := welsTryPYskipPixels(&fastContext, &mb, &lazyCache); got != want {
					t.Fatalf("lazy accepted=%v want=%v", got, want)
				}
				if transforms != groups {
					t.Fatalf("transformed %d groups, original first decision requires %d", transforms, groups)
				}
				if scenario == 0 && (!want || transforms != 4) {
					t.Fatal("identical pixels must accept after four transforms")
				}
				if scenario >= 1 && scenario <= 4 && (want || transforms != scenario) {
					t.Fatalf("forced rejection: accepted=%v transforms=%d want=%d", want, transforms, scenario)
				}
				if lazyLevels != wantLevels || !slices.Equal(lazyCoefficients[:groups*64], wantCoefficients[:groups*64]) {
					t.Fatal("visited group coefficients or level state differ")
				}
				if want && lazyCoefficients != wantCoefficients {
					t.Fatal("accepted skip coefficient state differs")
				}
				if !slices.Equal(lazyCoefficients[groups*64:], initialCoefficients[groups*64:]) {
					t.Fatal("lazy skip modified unvisited groups or chroma scratch")
				}
			})
		}
	}
}

func skipLazyCache(coefficients []int16, levels *SDCTCoeff, source []byte, sourceOffset int, prediction []byte) SMbCache {
	cache := SMbCache{pCoeffLevel: coefficients, pDct: levels, pSkipMb: prediction}
	cache.SPicData.pEncMb[0] = source
	cache.SPicData.iEncMbOff[0] = sourceOffset
	return cache
}

// Frozen original WelsTryPYskip, before metadata fusion/lazy transforms. The
// extra return value only records which group first makes the skip decision.
func originalTryPYskip(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache) (bool, int) {
	iSingleCtrMb := int32(0)
	pRes := pMbCache.pCoeffLevel
	kuiQp := pCurMb.uiLumaQp

	pBlock := pMbCache.pDct.iLumaBlock[:]
	var aMax [4]int16
	pMF := g_kiQuantMF[kuiQp][:]
	pFF := g_kiQuantInterFF[kuiQp][:]

	iResOff := 0
	iBlockOff := 0
	for i := 0; i < 4; i++ {
		WelsQuantFour4x4Max_sse2(pRes[iResOff:], pFF, pMF, aMax[:])
		for j := 0; j < 4; j++ {
			if uint16(aMax[j]) > 1 {
				return false, i + 1
			} else if uint16(aMax[j]) == 1 {
				pEncCtx.pFuncList.pfScan4x4(pBlock[iBlockOff:], pRes[iResOff:])
				iSingleCtrMb += pEncCtx.pFuncList.pfCalculateSingleCtr4x4(pBlock[iBlockOff:])
			}
			if iSingleCtrMb >= 6 {
				return false, i + 1
			}
			iResOff += 16
			iBlockOff += 16
		}
	}
	return true, 4
}
