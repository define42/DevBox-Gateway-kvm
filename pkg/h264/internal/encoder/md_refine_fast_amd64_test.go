//go:build amd64

package encoder

import (
	"bytes"
	"math/rand/v2"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// meRefineFracPixelFast must reproduce the checked refinement exactly: motion
// vector, costs, scratch buffers and the copied prediction.
func TestMeRefineFracPixelFastParity(t *testing.T) {
	var processors int32
	flags := common.WelsCPUFeatureDetect(&processors)
	if flags&common.WELS_CPU_AVX2 == 0 {
		t.Skip("AVX2 unavailable")
	}
	const encStride, refStride = 48, 96
	newCtx := func() *sWelsEncCtx {
		pFunc := &SWelsFuncPtrList{}
		common.InitMcFunc(&pFunc.sMcFuncs, flags)
		WelsInitSampleSadFunc(pFunc, flags)
		pFunc.sSampleDealingFuncs.pfMeCost = &pFunc.sSampleDealingFuncs.pfSampleSatd
		return &sWelsEncCtx{
			pFuncList: pFunc,
			pCurDqLayer: &SDqLayer{
				iEncStride: [3]int32{encStride, encStride / 2, encStride / 2},
				pRefPic:    &SPicture{iLineSize: [3]int32{refStride, refStride / 2, refStride / 2}},
			},
		}
	}
	fastCtx, slowCtx := newCtx(), newCtx()
	if !fastCtx.pFuncList.sSampleDealingFuncs.bSampleHalfSatdPair {
		t.Skip("AVX2 refinement functions are not installed for this CPU configuration")
	}

	const mvdSize = 648*2 + 1
	costs := make([]uint16, 52*mvdSize)
	MvdCostInit(costs, mvdSize)
	copies := map[uint8]PCopyFunc{
		BLOCK_16x16: common.WelsCopy16x16_c, BLOCK_16x8: common.WelsCopy16x8_c,
		BLOCK_8x16: common.WelsCopy8x16_c, BLOCK_8x8: common.WelsCopy8x8_c,
	}
	rng := rand.New(rand.NewPCG(5, 9))
	var accepted int
	for test := 0; test < 2500; test++ {
		ref := make([]byte, refStride*64)
		for i := range ref {
			ref[i] = byte(rng.Uint32())
		}
		block := uint8(rng.IntN(4))
		w, h := meBlockDims[block][0], meBlockDims[block][1]
		refOff := 20*refStride + 24
		enc := make([]byte, encStride*16)
		// Derive the source from a shifted, filtered or noisy reference so
		// every half- and quarter-pixel winner occurs.
		for y := 0; y < h; y++ {
			for x := 0; x < w; x++ {
				v := int(ref[refOff+y*refStride+x])
				switch test % 4 {
				case 1:
					v = (v + int(ref[refOff+y*refStride+x+1]) + 1) / 2
				case 2:
					v = (v + int(ref[refOff+(y+1)*refStride+x]) + 1) / 2
				case 3:
					v += rng.IntN(5) - 2
				}
				enc[y*encStride+x] = byte(min(max(v, 0), 255))
			}
		}
		qp := rng.IntN(52)
		me := SWelsME{
			pMvdCost: costs, iMvdCostOff: qp*mvdSize + mvdSize/2, uiBlockSize: block,
			pEncMb: enc, pRefMb: ref, iRefMbOff: refOff,
			sMv:           SMVUnitXY{int16(4 * (rng.IntN(9) - 4)), int16(4 * (rng.IntN(9) - 4))},
			sMvp:          SMVUnitXY{int16(rng.IntN(33) - 16), int16(rng.IntN(33) - 16)},
			uSadPredISatd: uint32(rng.IntN(3000)),
		}
		satdInMd := rng.IntN(2) == 0
		fastCtx.pCurDqLayer.bSatdInMdFlag, slowCtx.pCurDqLayer.bSatdInMdFlag = satdInMd, satdInMd
		// Scratch offsets used for each partition's position in the macroblock.
		offsets := map[uint8][]int32{
			BLOCK_16x16: {0},
			BLOCK_16x8:  {0, ME_REFINE_BUF_STRIDE_BLK8},
			BLOCK_8x16:  {0, ME_REFINE_BUF_WIDTH_BLK8},
			BLOCK_8x8:   g_kiPixStrideIdx8x8[:],
		}[block]
		offset := offsets[rng.IntN(len(offsets))]
		run := func(ctx *sWelsEncCtx, direct bool) (SWelsME, SMeRefinePointer, []byte, []byte) {
			var cache SMbCache
			cache.pBufferInterPredMe = bytes.Repeat([]byte{0x5c}, 4*640)
			var refine SMeRefinePointer
			InitMeRefinePointer(&refine, &cache, offset)
			refine.pfCopyBlockByMode = copies[block]
			pred := bytes.Repeat([]byte{0xc5}, 256)
			m := me
			if direct {
				if !meRefineFracPixelFast(ctx, pred, 0, &m, &refine, int32(w), int32(h)) {
					t.Fatalf("test %d: fast refinement declined a valid block", test)
				}
			} else {
				meRefineFracPixelChecked(ctx, pred, 0, &m, &refine, int32(w), int32(h))
			}
			return m, refine, pred, cache.pBufferInterPredMe
		}
		gotMe, gotRefine, gotPred, gotArena := run(fastCtx, true)
		wantMe, wantRefine, wantPred, wantArena := run(slowCtx, false)
		accepted++
		if !reflect.DeepEqual(gotMe, wantMe) || !bytes.Equal(gotPred, wantPred) || !bytes.Equal(gotArena, wantArena) ||
			!reflect.DeepEqual(gotRefine.pQuarPixBest, wantRefine.pQuarPixBest) ||
			!reflect.DeepEqual(gotRefine.pHalfPixHV, wantRefine.pHalfPixHV) {
			t.Fatalf("test %d block %d: mv %v cost %d, want mv %v cost %d", test, block, gotMe.sMv, gotMe.uiSatdCost, wantMe.sMv, wantMe.uiSatdCost)
		}
	}
	if accepted == 0 {
		t.Fatal("no refinement ran")
	}
}

func TestMeRefineFracPixelFastDeclinesWithoutMutation(t *testing.T) {
	requireSampleAVX2(t)
	tests := []struct {
		name   string
		change func(*fractionalCallerFixture)
	}{
		{"negativeCostBase", func(f *fractionalCallerFixture) { f.me.iMvdCostOff = -1; f.me.sMvp = SMVUnitXY{-32, -32} }},
		{"oversizedCostBase", func(f *fractionalCallerFixture) { f.me.iMvdCostOff = len(f.me.pMvdCost); f.me.sMvp = SMVUnitXY{32, 32} }},
		{"shortEncoded", func(f *fractionalCallerFixture) { f.me.pEncMb = f.me.pEncMb[:1] }},
		{"shortReference", func(f *fractionalCallerFixture) { f.me.pRefMb = f.me.pRefMb[:f.me.iRefMbOff] }},
		{"shortCost", func(f *fractionalCallerFixture) { f.me.pMvdCost = f.me.pMvdCost[:1] }},
		{"shortHalf", func(f *fractionalCallerFixture) { f.refine.pHalfPixH = f.refine.pHalfPixH[:1] }},
		{"shortQuarter", func(f *fractionalCallerFixture) { f.refine.pQuarPixTmp = f.refine.pQuarPixTmp[:1] }},
		{"unknownArena", func(f *fractionalCallerFixture) { f.refine.arena = nil }},
		{"aliasedHalves", func(f *fractionalCallerFixture) { f.refine.pHalfPixH = f.refine.pHalfPixV }},
		{"aliasedSource", func(f *fractionalCallerFixture) { f.me.pEncMb = f.refine.arena; f.me.iEncMbOff = 0 }},
		{"overflowRefOffset", func(f *fractionalCallerFixture) { f.me.iRefMbOff = int(^uint(0) >> 1) }},
		{"overflowScratchOffset", func(f *fractionalCallerFixture) { f.refine.arenaOff = int(^uint(0) >> 1) }},
		{"motionWrap", func(f *fractionalCallerFixture) { f.me.sMv.iMvX = 32767 }},
		{"averageDisabled", func(f *fractionalCallerFixture) { f.funcs.sSampleDealingFuncs.pfSampleAvgSatd = nil }},
		{"quarterDisabled", func(f *fractionalCallerFixture) { f.funcs.sSampleDealingFuncs.bSampleAvgSatdFour = false }},
		{"pairDisabled", func(f *fractionalCallerFixture) { f.funcs.sSampleDealingFuncs.bSampleHalfSatdPair = false }},
		{"customFilter", func(f *fractionalCallerFixture) {
			original := f.funcs.sMcFuncs.PfLumaHalfpelHor
			f.funcs.sMcFuncs.PfLumaHalfpelHor = func(a []byte, ao int, as int32, b []byte, bo int, bs, w, h int32) {
				original(a, ao, as, b, bo, bs, w, h)
			}
		}},
		{"customAverage", func(f *fractionalCallerFixture) {
			original := f.funcs.sMcFuncs.PfSampleAveraging
			f.funcs.sMcFuncs.PfSampleAveraging = func(a []byte, ao int, as int32, b []byte, bo int, bs int32, c []byte, co int, cs, w, h int32) {
				original(a, ao, as, b, bo, bs, c, co, cs, w, h)
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f := newFractionalCallerFixture(16, 16, BLOCK_16x16, 32, true)
			InitMeRefinePointer(&f.refine, &f.cache, 0)
			test.change(f)
			want := f.me
			arena := bytes.Clone(f.cache.pBufferInterPredMe)
			pred := bytes.Clone(f.pred)
			if meRefineFracPixelFast(&f.ctx, f.pred, 0, &f.me, &f.refine, 16, 16) {
				t.Fatal("unexpected fast path")
			}
			if !reflect.DeepEqual(want, f.me) || !bytes.Equal(arena, f.cache.pBufferInterPredMe) || !bytes.Equal(pred, f.pred) {
				t.Fatal("declined fast path changed state")
			}
		})
	}
}
