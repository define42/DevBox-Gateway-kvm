package encoder

import (
	"fmt"
	"math/rand"
	"reflect"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type skipDctQuantCallerEvent struct {
	kind      string
	args      [10]int
	state     [12]int32
	pixelHash uint64
	coeffHash uint64
}

func traceSkipDctQuantCaller(s *skipDctQuantCallerState, mutation string) *[]skipDctQuantCallerEvent {
	events := new([]skipDctQuantCallerEvent)
	count := make(map[string]int)
	record := func(kind string, args ...int) {
		mb := &s.blocks[1]
		event := skipDctQuantCallerEvent{kind: kind, state: [12]int32{mb.iMbXY, *mb.pSadCost,
			s.md.iCostLuma, s.md.iCostSkipMb, s.md.iSadPredSkip, int32(mb.uiLumaQp),
			int32(mb.pRefIndex[0]), int32(mb.pRefIndex[1]), int32(mb.pRefIndex[2]), int32(mb.pRefIndex[3]),
			int32(mb.sP16x16Mv.iMvX), int32(mb.sP16x16Mv.iMvY)}}
		copy(event.args[:], args)
		event.pixelHash, event.coeffHash = 14695981039346656037, 14695981039346656037
		for _, value := range s.cache.pSkipMb {
			event.pixelHash = (event.pixelHash ^ uint64(value)) * 1099511628211
		}
		for _, value := range s.cache.pCoeffLevel {
			event.coeffHash = (event.coeffHash ^ uint64(uint16(value))) * 1099511628211
		}
		*events = append(*events, event)
		count[kind]++
	}
	wrapMC := func(kind string, fn common.PWelsMcFunc) common.PWelsMcFunc {
		return func(src []uint8, srcOff int, srcStride int32, dst []uint8, dstOff int, dstStride int32,
			mvX, mvY int16, width, height int32) {
			record(kind, srcOff, int(srcStride), dstOff, int(dstStride), int(mvX), int(mvY), int(width), int(height), len(src), len(dst))
			fn(src, srcOff, srcStride, dst, dstOff, dstStride, mvX, mvY, width, height)
		}
	}
	s.functions.sMcFuncs.PMcLumaFunc = wrapMC("mc-luma", s.functions.sMcFuncs.PMcLumaFunc)
	s.functions.sMcFuncs.PMcChromaFunc = wrapMC("mc-chroma", s.functions.sMcFuncs.PMcChromaFunc)
	wrapCost := func(kind string, fn PSampleSadSatdCostFunc) PSampleSadSatdCostFunc {
		return func(a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) int32 {
			record(kind, aOff, int(aStride), bOff, int(bStride), len(a), len(b))
			cost := fn(a, aOff, aStride, b, bOff, bStride)
			if mutation == "sad-threshold" && kind == "sad-luma" {
				s.md.iSadPredSkip = 1 << 20
			}
			return cost
		}
	}
	s.functions.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16] = wrapCost("sad-luma", s.functions.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16])
	s.functions.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8] = wrapCost("sad-chroma", s.functions.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8])
	s.functions.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16] = wrapCost("satd-luma", s.functions.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16])
	dct := s.functions.pfDctFourT4
	s.functions.pfDctFourT4 = func(coeff []int16, src []uint8, srcOff int, srcStride int32, pred []uint8, predOff int, predStride int32) {
		record("dct", len(coeff), srcOff, int(srcStride), predOff, int(predStride), len(src), len(pred))
		dct(coeff, src, srcOff, srcStride, pred, predOff, predStride)
		if mutation == "transform" && count["dct"] == 1 {
			// Luma reloads the context's table, while later chroma transforms
			// use the function table captured at entry. Preserve both rules.
			s.ctx.pFuncList = &s.alternate
			s.blocks[1].uiLumaQp = 31
		}
	}
	scan := s.functions.pfScan4x4Ac
	s.functions.pfScan4x4Ac = func(levels, coeff []int16) {
		record("scan-ac", len(levels), len(coeff))
		scan(levels, coeff)
	}
	score := s.functions.pfCalculateSingleCtr4x4
	s.functions.pfCalculateSingleCtr4x4 = func(levels []int16) int32 {
		record("score", len(levels))
		return score(levels)
	}
	update := s.functions.pfUpdateMbMv
	s.functions.pfUpdateMbMv = func(mvs []SMVUnitXY, mv SMVUnitXY) {
		record("update-mv", len(mvs), int(mv.iMvX), int(mv.iMvY))
		update(mvs, mv)
		if mutation == "finish" || mutation == "transform" {
			// Finalization must re-read mutable costs/index/stride after the
			// callback while keeping the prediction slice captured at entry.
			s.blocks[1].pSadCost = &s.alternateSad
			s.blocks[1].iMbXY = 2
			s.md.bMdUsingSad = !s.md.bMdUsingSad
			s.cache.SPicData.iEncMbOff[0]++
			s.layer.iEncStride[0] = 65
			s.cache.pSkipMb = s.enc[2][skipDctQuantCallerOrigin : skipDctQuantCallerOrigin+384]
		}
	}
	s.alternate = s.functions
	s.alternate.pfDctFourT4 = func(coeff []int16, src []uint8, srcOff int, srcStride int32, pred []uint8, predOff int, predStride int32) {
		record("dct-alternate", len(coeff), srcOff, int(srcStride), predOff, int(predStride), len(src), len(pred))
		dct(coeff, src, srcOff, srcStride, pred, predOff, predStride)
	}
	return events
}

func compareSkipDctQuantCaller(t *testing.T, got, want *skipDctQuantCallerState, mutation string) ([]skipDctQuantCallerEvent, bool) {
	t.Helper()
	gotTrace, wantTrace := traceSkipDctQuantCaller(got, mutation), traceSkipDctQuantCaller(want, mutation)
	gotResult := WelsMdPSkipEnc(&got.ctx, &got.md, &got.blocks[1], &got.cache)
	wantResult := legacySkipDctQuantCallerWelsMdPSkipEnc(&want.ctx, &want.md, &want.blocks[1], &want.cache)
	if gotResult != wantResult || !reflect.DeepEqual(*gotTrace, *wantTrace) {
		t.Fatalf("result/trace differs: accepted=%v/%v\ngot=%v\nwant=%v", gotResult, wantResult, *gotTrace, *wantTrace)
	}
	if got.enc != want.enc || got.ref != want.ref || got.prediction != want.prediction ||
		got.coefficients != want.coefficients || got.levels != want.levels || got.alternateSad != want.alternateSad ||
		!reflect.DeepEqual(got.blocks, want.blocks) || !reflect.DeepEqual(got.cache, want.cache) ||
		!reflect.DeepEqual(got.md, want.md) || !reflect.DeepEqual(got.refPic, want.refPic) ||
		!reflect.DeepEqual(got.decPic, want.decPic) || got.layer.iEncStride != want.layer.iEncStride ||
		(got.ctx.pFuncList == &got.alternate) != (want.ctx.pFuncList == &want.alternate) ||
		(got.blocks[1].pSadCost == &got.alternateSad) != (want.blocks[1].pSadCost == &want.alternateSad) {
		t.Fatal("pixel, coefficient, macroblock, cost, or callback-selected state differs")
	}
	return *gotTrace, gotResult
}

func TestSkipDctQuantCallerPaths(t *testing.T) {
	cases := []struct {
		name       string
		accepted   bool
		transforms int
	}{
		{"Direct", true, 0}, {"PredThreshold", true, 0}, {"RefThreshold", true, 0},
		{"RejectLuma", false, 1}, {"RejectU", false, 5}, {"RejectV", false, 6}, {"TransformSuccess", true, 6},
	}
	for _, flags := range []uint32{0, common.WelsCPUFeatureDetect(nil)} {
		for _, useSAD := range []bool{true, false} {
			for _, tc := range cases {
				t.Run(fmt.Sprintf("%s/flags%x/SAD%t", tc.name, flags, useSAD), func(t *testing.T) {
					got, want := newSkipDctQuantCallerState(tc.name, flags, useSAD), newSkipDctQuantCallerState(tc.name, flags, useSAD)
					trace, accepted := compareSkipDctQuantCaller(t, got, want, "none")
					transforms, updates, satds := 0, 0, 0
					for _, event := range trace {
						switch event.kind {
						case "dct":
							transforms++
						case "update-mv":
							updates++
						case "satd-luma":
							satds++
						}
					}
					wantUpdates, wantSatds := 0, 0
					if tc.accepted {
						wantUpdates = 1
						if !useSAD {
							wantSatds = 1
						}
					}
					if accepted != tc.accepted || transforms != tc.transforms || updates != wantUpdates || satds != wantSatds {
						t.Fatalf("wrong fixture path: accepted=%v DCT=%d updates=%d SATD=%d", accepted, transforms, updates, satds)
					}
				})
			}
		}
	}
}

func TestSkipDctQuantCallerCallbackMutations(t *testing.T) {
	for _, scenario := range []string{"Direct", "TransformSuccess", "RejectLuma", "RejectU", "RejectV"} {
		for _, mutation := range []string{"sad-threshold", "finish", "transform"} {
			for _, useSAD := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/SAD%t", scenario, mutation, useSAD), func(t *testing.T) {
					got, want := newSkipDctQuantCallerState(scenario, common.WelsCPUFeatureDetect(nil), useSAD), newSkipDctQuantCallerState(scenario, common.WelsCPUFeatureDetect(nil), useSAD)
					compareSkipDctQuantCaller(t, got, want, mutation)
				})
			}
		}
	}
}

func TestSkipDctQuantCallerQpMotionAndThresholds(t *testing.T) {
	rng := rand.New(rand.NewSource(732664))
	flags := common.WelsCPUFeatureDetect(nil)
	for qp := 0; qp <= 51; qp++ {
		for _, mv := range []SMVUnitXY{{}, {1, -3}, {8, 12}, {-1024, 0}, {0, 1024}} {
			got, want := newSkipDctQuantCallerState("TransformSuccess", flags, qp%2 == 0), newSkipDctQuantCallerState("TransformSuccess", flags, qp%2 == 0)
			for plane := range 3 {
				for i := range got.enc[plane] {
					pixel := byte(124 + rng.Intn(9))
					got.enc[plane][i], want.enc[plane][i] = pixel, pixel
				}
			}
			for _, s := range []*skipDctQuantCallerState{got, want} {
				s.blocks[1].uiLumaQp = uint8(qp)
				for i := range s.cache.sMvComponents.sMotionVectorCache {
					s.cache.sMvComponents.sMotionVectorCache[i] = mv
				}
			}
			t.Run(fmt.Sprintf("QP%d/MV%d_%d", qp, mv.iMvX, mv.iMvY), func(t *testing.T) {
				compareSkipDctQuantCaller(t, got, want, "none")
			})
		}
	}
	for _, scenario := range []string{"PredThreshold", "RefThreshold"} {
		for _, threshold := range []int32{0, 1, 2} { // SAD is exactly 1; equality must continue transforming.
			got, want := newSkipDctQuantCallerState(scenario, flags, true), newSkipDctQuantCallerState(scenario, flags, true)
			for _, s := range []*skipDctQuantCallerState{got, want} {
				if scenario == "PredThreshold" {
					s.md.iSadPredSkip = threshold
				} else {
					s.refPic.pMbSkipSad[1] = threshold
				}
			}
			trace, _ := compareSkipDctQuantCaller(t, got, want, "none")
			transforms := 0
			for _, event := range trace {
				if event.kind == "dct" {
					transforms++
				}
			}
			if (threshold <= 1 && transforms != 6) || (threshold == 2 && transforms != 0) {
				t.Fatalf("%s threshold=%d made %d transforms", scenario, threshold, transforms)
			}
		}
	}
}

func TestSkipDctQuantCallerAliases(t *testing.T) {
	for _, alias := range []string{"prediction-reference", "prediction-source", "coefficient-scan"} {
		for _, scenario := range []string{"Direct", "TransformSuccess", "RejectU", "RejectV"} {
			for _, useSAD := range []bool{true, false} {
				t.Run(fmt.Sprintf("%s/%s/SAD%t", alias, scenario, useSAD), func(t *testing.T) {
					got, want := newSkipDctQuantCallerState(scenario, common.WelsCPUFeatureDetect(nil), useSAD), newSkipDctQuantCallerState(scenario, common.WelsCPUFeatureDetect(nil), useSAD)
					var gotBacking, wantBacking [1024]int16
					for i := range gotBacking {
						gotBacking[i], wantBacking[i] = int16(i*301+17), int16(i*301+17)
					}
					for i, s := range []*skipDctQuantCallerState{got, want} {
						switch alias {
						case "prediction-reference":
							s.cache.pSkipMb = s.ref[0][skipDctQuantCallerOrigin-3 : skipDctQuantCallerOrigin-3+384]
						case "prediction-source":
							s.cache.pSkipMb = s.enc[0][skipDctQuantCallerOrigin+1 : skipDctQuantCallerOrigin+1+384]
						case "coefficient-scan":
							backing := &gotBacking
							if i == 1 {
								backing = &wantBacking
							}
							s.cache.pDct = (*SDCTCoeff)(unsafe.Pointer(&backing[256]))
							s.cache.pCoeffLevel = backing[240:624]
						}
					}
					compareSkipDctQuantCaller(t, got, want, "none")
					if gotBacking != wantBacking {
						t.Fatal("aliased coefficient backing differs")
					}
				})
			}
		}
	}
}

// Keep the pre-change closure and control flow as an independent reference.
func legacySkipDctQuantCallerWelsMdPSkipEnc(pEncCtx *sWelsEncCtx, pWelsMd *SWelsMD, pCurMb *SMB, pMbCache *SMbCache) bool {
	pCurLayer := pEncCtx.pCurDqLayer
	pFunc := pEncCtx.pFuncList

	pRefLuma, iRefLumaOff := pMbCache.SPicData.pRefMb[0], pMbCache.SPicData.iRefMbOff[0]
	pRefCb, iRefCbOff := pMbCache.SPicData.pRefMb[1], pMbCache.SPicData.iRefMbOff[1]
	pRefCr, iRefCrOff := pMbCache.SPicData.pRefMb[2], pMbCache.SPicData.iRefMbOff[2]
	iLineSizeY := pCurLayer.pRefPic.iLineSize[0]
	iLineSizeUV := pCurLayer.pRefPic.iLineSize[1]

	pDstLuma, iDstLumaOff := pMbCache.pSkipMb, 0
	pDstCb, iDstCbOff := pMbCache.pSkipMb, 256
	pDstCr, iDstCrOff := pMbCache.pSkipMb, 256+64

	sMvp := SMVUnitXY{}
	var n int32

	iEncStride := pCurLayer.iEncStride[0]
	pEncMb, iEncMbOff := pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0]
	pStrideEncBlockOffset := pEncCtx.pStrideTab.pStrideEncBlockOffset[pEncCtx.uiDependencyId]
	var pEncBlockOffset []int32

	var iSadCostLuma int32 = 0
	var iSadCostChroma int32 = 0
	var iSadCostMb int32 = 0

	PredSkipMv(pMbCache, &sMvp)

	// Special case, need to clip the vector //
	sQpelMvp := SMVUnitXY{sMvp.iMvX >> 2, sMvp.iMvY >> 2}
	n = (int32(pCurMb.iMbX) << 4) + int32(sQpelMvp.iMvX)
	if n < -29 {
		return false
	} else if n > ((int32(pCurLayer.iMbWidth) << 4) + 12) {
		return false
	}

	n = (int32(pCurMb.iMbY) << 4) + int32(sQpelMvp.iMvY)
	if n < -29 {
		return false
	} else if n > ((int32(pCurLayer.iMbHeight) << 4) + 12) {
		return false
	}

	//luma
	iRefLumaOff += int(int32(sQpelMvp.iMvY)*iLineSizeY + int32(sQpelMvp.iMvX))
	pFunc.sMcFuncs.PMcLumaFunc(pRefLuma, iRefLumaOff, iLineSizeY, pDstLuma, iDstLumaOff, 16, sMvp.iMvX, sMvp.iMvY, 16, 16)
	iSadCostLuma = pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
		pCurLayer.iEncStride[0], pDstLuma, iDstLumaOff, 16)

	iStrideUV := int((int32(sQpelMvp.iMvY)>>1)*iLineSizeUV + (int32(sQpelMvp.iMvX) >> 1))
	iRefCbOff += iStrideUV
	pFunc.sMcFuncs.PMcChromaFunc(pRefCb, iRefCbOff, iLineSizeUV, pDstCb, iDstCbOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8) //Cb
	iSadCostChroma = pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1],
		pCurLayer.iEncStride[1], pDstCb, iDstCbOff, 8)

	iRefCrOff += iStrideUV
	pFunc.sMcFuncs.PMcChromaFunc(pRefCr, iRefCrOff, iLineSizeUV, pDstCr, iDstCrOff, 8, sMvp.iMvX, sMvp.iMvY, 8, 8) //Cr
	iSadCostChroma += pFunc.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8](pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2],
		pCurLayer.iEncStride[2], pDstCr, iDstCrOff, 8)

	iSadCostMb = iSadCostLuma + iSadCostChroma

	// update motion info to current MB and finish as P_Skip
	decideSkip := func() bool {
		//update motion info to current MB
		setRefIndexZero(pCurMb)
		pFunc.pfUpdateMbMv(pCurMb.sMv, sMvp)

		if pWelsMd.bMdUsingSad {
			*pCurMb.pSadCost = iSadCostLuma
			pWelsMd.iCostLuma = *pCurMb.pSadCost
		} else {
			pWelsMd.iCostLuma = pFunc.sSampleDealingFuncs.pfSampleSatd[BLOCK_16x16](pMbCache.SPicData.pEncMb[0], pMbCache.SPicData.iEncMbOff[0],
				pCurLayer.iEncStride[0], pDstLuma, iDstLumaOff, 16)
		}

		pWelsMd.iCostSkipMb = iSadCostMb

		pCurMb.sP16x16Mv = sMvp
		pCurLayer.pDecPic.sMvList[pCurMb.iMbXY] = sMvp

		return true
	}

	if iSadCostMb == 0 ||
		iSadCostMb < pWelsMd.iSadPredSkip ||
		(pCurLayer.pRefPic.iPictureType == common.P_SLICE &&
			pMbCache.uiRefMbType == common.MB_TYPE_SKIP &&
			iSadCostMb < pCurLayer.pRefPic.pMbSkipSad[pCurMb.iMbXY]) {
		return decideSkip()
	}

	if legacySkipDctQuantPixels(pEncCtx, pCurMb, pMbCache) {
		iEncStride = pEncCtx.pCurDqLayer.iEncStride[1]
		pEncMb, iEncMbOff = pMbCache.SPicData.pEncMb[1], pMbCache.SPicData.iEncMbOff[1]
		pEncBlockOffset = pStrideEncBlockOffset[16:]
		pFunc.pfDctFourT4(pMbCache.pCoeffLevel[256:], pEncMb, iEncMbOff+int(pEncBlockOffset[0]), iEncStride, pMbCache.pSkipMb, 256, 8)
		if WelsTryPUVskip(pEncCtx, pCurMb, pMbCache, 1) {
			pEncMb, iEncMbOff = pMbCache.SPicData.pEncMb[2], pMbCache.SPicData.iEncMbOff[2]
			pEncBlockOffset = pStrideEncBlockOffset[20:]
			pFunc.pfDctFourT4(pMbCache.pCoeffLevel[320:], pEncMb, iEncMbOff+int(pEncBlockOffset[0]), iEncStride, pMbCache.pSkipMb, 320, 8)
			if WelsTryPUVskip(pEncCtx, pCurMb, pMbCache, 2) {
				return decideSkip()
			}
		}
	}
	return false
}

func TestSkipDctQuantCallerBuiltinPaths(t *testing.T) {
	flags := common.WelsCPUFeatureDetect(nil)
	for _, scenario := range []string{"Direct", "PredThreshold", "RefThreshold", "RejectLuma", "RejectU", "RejectV", "TransformSuccess"} {
		for _, useSAD := range []bool{true, false} {
			got, want := newSkipDctQuantCallerState(scenario, flags, useSAD), newSkipDctQuantCallerState(scenario, flags, useSAD)
			gotResult := WelsMdPSkipEnc(&got.ctx, &got.md, &got.blocks[1], &got.cache)
			wantResult := legacySkipDctQuantCallerWelsMdPSkipEnc(&want.ctx, &want.md, &want.blocks[1], &want.cache)
			if gotResult != wantResult || got.enc != want.enc || got.ref != want.ref || got.prediction != want.prediction ||
				got.coefficients != want.coefficients || got.levels != want.levels || !reflect.DeepEqual(got.blocks, want.blocks) ||
				!reflect.DeepEqual(got.md, want.md) || !reflect.DeepEqual(got.decPic, want.decPic) {
				t.Fatalf("%s/SAD%t: complete builtin skip caller result or state differs", scenario, useSAD)
			}
		}
	}
}
