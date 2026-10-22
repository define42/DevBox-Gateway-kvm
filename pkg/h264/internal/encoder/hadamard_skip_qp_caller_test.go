package encoder

import (
	"fmt"
	"reflect"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type chromaSkipOutcome struct {
	accepted     bool
	panicText    string
	coefficients [384]int16
	levels       SDCTCoeff
	events       []string
}

func runChromaSkipCaller(qp int, uv int32, content, scenario string, scalar bool) (out chromaSkipOutcome) {
	f := newChromaSkipCallerFixture(qp, uv, content)
	defer func() {
		if value := recover(); value != nil {
			out.panicText = fmt.Sprintf("%T: %v", value, value)
		}
		out.coefficients, out.levels = f.coefficients, f.levels
	}()
	f.reset()
	switch scenario {
	case "aliasLevels":
		// All fields are int16 arrays. Alias residual storage with the complete
		// SDCTCoeff allocation so the unchanged AC scan can overwrite later
		// residual/level views after the pure DC skip check.
		f.cache.pCoeffLevel = unsafe.Slice((*int16)(unsafe.Pointer(&f.levels)), 384)
		copy(f.cache.pCoeffLevel[f.offset:f.offset+64], f.input[:])
	case "short0":
		f.cache.pCoeffLevel = f.coefficients[:0]
	case "short48":
		f.cache.pCoeffLevel = f.coefficients[:f.offset+48]
	case "short49":
		f.cache.pCoeffLevel = f.coefficients[:f.offset+49]
	case "short63":
		f.cache.pCoeffLevel = f.coefficients[:f.offset+63]
	case "nilDct":
		f.cache.pDct = nil
	case "nilPPS":
		f.layer.sLayerInfo.pPpsP = nil
	case "nilFunctions":
		f.ctx.pFuncList = nil
	case "invalidUV":
		uv = 0
	}
	scan, score := f.funcs.pfScan4x4Ac, f.funcs.pfCalculateSingleCtr4x4
	f.funcs.pfScan4x4Ac = func(level, residual []int16) {
		out.events = append(out.events, "scan")
		scan(level, residual)
		if scenario == "callbackMutation" {
			residual[17] ^= 15
		}
	}
	f.funcs.pfCalculateSingleCtr4x4 = func(level []int16) int32 {
		out.events = append(out.events, "score")
		if scenario == "callbackPanic" {
			panic("score callback")
		}
		return score(level)
	}
	if scalar {
		out.accepted = originalChromaSkipCaller(&f.ctx, &f.mb, &f.cache, uv)
	} else {
		out.accepted = WelsTryPUVskip(&f.ctx, &f.mb, &f.cache, uv)
	}
	return out
}

func TestChromaSkipQPCompleteCallerParity(t *testing.T) {
	for qp := range 52 {
		for _, uv := range []int32{1, 2} {
			for _, content := range []string{"Zero", "DCReject", "DCBoundary", "SparseAC", "ACReject", "Dense", "Extremes"} {
				t.Run(fmt.Sprintf("QP%d/UV%d/%s", qp, uv, content), func(t *testing.T) {
					want, got := runChromaSkipCaller(qp, uv, content, "normal", true), runChromaSkipCaller(qp, uv, content, "normal", false)
					if !reflect.DeepEqual(got, want) || want.panicText != "" {
						t.Fatalf("got=%+v want=%+v", got, want)
					}
					if content == "Zero" && !got.accepted || content == "DCReject" && got.accepted {
						t.Fatal("fixture did not exercise expected DC decision")
					}
				})
			}
		}
	}
	for _, uv := range []int32{1, 2} {
		for _, content := range []string{"Zero", "DCReject", "SparseAC"} {
			for _, scenario := range []string{"aliasLevels", "short0", "short48", "short49", "short63", "nilDct", "nilPPS", "nilFunctions", "invalidUV", "callbackMutation", "callbackPanic"} {
				t.Run(fmt.Sprintf("fallback/UV%d/%s/%s", uv, content, scenario), func(t *testing.T) {
					want, got := runChromaSkipCaller(26, uv, content, scenario, true), runChromaSkipCaller(26, uv, content, scenario, false)
					if !reflect.DeepEqual(got, want) {
						t.Fatalf("got=%+v want=%+v", got, want)
					}
				})
			}
		}
	}
}

// Frozen complete production caller with the original scalar DC decision.
func originalChromaSkipCaller(pEncCtx *sWelsEncCtx, pCurMb *SMB, pMbCache *SMbCache, iUV int32) bool {
	var pRes []int16
	if iUV == 1 {
		pRes = pMbCache.pCoeffLevel[256:]
	} else {
		pRes = pMbCache.pCoeffLevel[256+64:]
	}

	kuiQp := common.G_kuiChromaQpTable[common.CLIP3_QP_0_51(int32(pCurMb.uiLumaQp)+
		int32(pEncCtx.pCurDqLayer.sLayerInfo.pPpsP.uiChromaQpIndexOffset))]

	pMF := g_kiQuantMF[kuiQp][:]
	pFF := g_kiQuantInterFF[kuiQp][:]

	if WelsHadamardQuant2x2Skip_c(pRes, int16(int32(pFF[0])<<1), pMF[0]>>1) != 0 {
		return false
	} else {
		var aMax [4]int16 // uint16_t aMax[4] in C, passed as (int16_t*)
		iSingleCtrMb := int32(0)
		pBlock := pMbCache.pDct.iChromaBlock[((iUV-1)<<2)*16:]
		WelsQuantFour4x4Max_sse2(pRes, pFF, pMF, aMax[:])

		iResOff := 0
		iBlockOff := 0
		for j := 0; j < 4; j++ {
			if uint16(aMax[j]) > 1 {
				return false // iSingleCtrMb += 9, can't be P_SKIP
			} else if uint16(aMax[j]) == 1 {
				pEncCtx.pFuncList.pfScan4x4Ac(pBlock[iBlockOff:], pRes[iResOff:])
				iSingleCtrMb += pEncCtx.pFuncList.pfCalculateSingleCtr4x4(pBlock[iBlockOff:])
			}
			if iSingleCtrMb >= 7 {
				return false //from JVT-O079
			}
			iResOff += 16
			iBlockOff += 16
		}
		return true
	}
}

func observeChromaSkipQPFixture(f *chromaSkipCallerFixture, scalar bool) (out chromaSkipOutcome) {
	defer func() {
		if value := recover(); value != nil {
			out.panicText = fmt.Sprintf("%T: %v", value, value)
		}
		out.coefficients, out.levels = f.coefficients, f.levels
	}()
	f.reset()
	if scalar {
		out.accepted = originalChromaSkipCaller(&f.ctx, &f.mb, &f.cache, f.uv)
	} else {
		out.accepted = WelsTryPUVskip(&f.ctx, &f.mb, &f.cache, f.uv)
	}
	return out
}

func TestChromaSkipQPChangedTableCaller(t *testing.T) {
	for _, qp := range []int{0, 20, 51} {
		for _, uv := range []int32{1, 2} {
			for _, content := range []string{"Zero", "DCBoundary", "DCReject", "SparseAC", "Dense"} {
				for _, change := range []string{"FF", "MF", "zeroMF", "negativeFF", "negativeMF", "equivalentDC", "ACOnly"} {
					t.Run(fmt.Sprintf("QP%d/UV%d/%s/%s", qp, uv, content, change), func(t *testing.T) {
						// Build the same ordinary fixture before changing the tables;
						// in particular, the fixture must not divide by the new zero MF.
						gotFixture := newChromaSkipCallerFixture(qp, uv, content)
						wantFixture := newChromaSkipCallerFixture(qp, uv, content)
						chromaQP := common.G_kuiChromaQpTable[qp]
						oldFF, oldMF := g_kiQuantInterFF[chromaQP], g_kiQuantMF[chromaQP]
						defer func() { g_kiQuantInterFF[chromaQP], g_kiQuantMF[chromaQP] = oldFF, oldMF }()
						switch change {
						case "FF":
							g_kiQuantInterFF[chromaQP][0] ^= 1
						case "MF":
							g_kiQuantMF[chromaQP][0] ^= 2
						case "zeroMF":
							g_kiQuantMF[chromaQP][0] = 0
						case "negativeFF":
							g_kiQuantInterFF[chromaQP][0] = 16384
						case "negativeMF":
							g_kiQuantMF[chromaQP][0] = -32768
						case "equivalentDC":
							g_kiQuantInterFF[chromaQP][0] ^= -32768
							g_kiQuantMF[chromaQP][0] ^= 1
						case "ACOnly":
							g_kiQuantInterFF[chromaQP][1] = -32768
							g_kiQuantMF[chromaQP][1] = -1
						}
						got, want := observeChromaSkipQPFixture(gotFixture, false), observeChromaSkipQPFixture(wantFixture, true)
						if !reflect.DeepEqual(got, want) {
							t.Fatalf("live table result/state differs: got=%+v want=%+v", got, want)
						}
					})
				}
			}
		}
	}
}

func TestChromaSkipQPIgnoresUnusedCallback(t *testing.T) {
	for _, nilCallback := range []bool{false, true} {
		for _, content := range []string{"Zero", "DCReject", "SparseAC"} {
			gotFixture, wantFixture := newChromaSkipCallerFixture(20, 1, content), newChromaSkipCallerFixture(20, 1, content)
			for _, f := range []*chromaSkipCallerFixture{gotFixture, wantFixture} {
				if nilCallback {
					f.funcs.pfQuantizationHadamard2x2Skip = nil
				} else {
					f.funcs.pfQuantizationHadamard2x2Skip = func([]int16, int16, int16) int32 {
						panic("the direct DC skip path must not invoke this callback")
					}
				}
			}
			got, want := observeChromaSkipQPFixture(gotFixture, false), observeChromaSkipQPFixture(wantFixture, true)
			if !reflect.DeepEqual(got, want) || got.panicText != "" {
				t.Fatalf("nil%t %s: ignored callback changed result: got=%+v want=%+v", nilCallback, content, got, want)
			}
		}
	}
}
