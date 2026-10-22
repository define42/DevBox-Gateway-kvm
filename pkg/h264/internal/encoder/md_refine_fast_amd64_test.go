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
	if !fastCtx.pFuncList.sMcFuncs.BRefineAVX2 || !fastCtx.pFuncList.sSampleDealingFuncs.bSatdAVX2 {
		t.Skip("AVX2 refinement functions are not installed for this CPU configuration")
	}
	slowCtx.pFuncList.sMcFuncs.BRefineAVX2 = false

	const mvdSize = 648*2 + 1
	costs := make([]uint16, 52*mvdSize)
	MvdCostInit(costs, mvdSize)
	copies := map[uint8]PCopyFunc{
		BLOCK_16x16: common.WelsCopy16x16_c, BLOCK_16x8: common.WelsCopy16x8_c,
		BLOCK_8x16: common.WelsCopy8x16_c, BLOCK_8x8: common.WelsCopy8x8_c,
	}
	rng := rand.New(rand.NewPCG(5, 9))
	var accepted int
	for test := 0; test < 4000; test++ {
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
				MeRefineFracPixel(ctx, pred, 0, &m, &refine, int32(w), int32(h))
			}
			return m, refine, pred, cache.pBufferInterPredMe
		}
		if meRefineFracPixelFast(slowCtx, nil, 0, &me, &SMeRefinePointer{}, int32(w), int32(h)) {
			t.Fatal("fast path ran with the checked half-pixel functions")
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
