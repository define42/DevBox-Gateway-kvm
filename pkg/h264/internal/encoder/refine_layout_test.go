package encoder

import (
	"math/rand/v2"
	"testing"
)

// The layout proof may only accept configurations that the full pairwise
// check also accepts. Real InitMeRefinePointer layouts must be accepted.
func TestQuarterRefineLayoutDisjointSound(t *testing.T) {
	rng := rand.New(rand.NewPCG(640, 4))
	sizes := [][2]int32{{16, 16}, {16, 8}, {8, 16}, {8, 8}}
	var accepted int
	for test := 0; test < 200000; test++ {
		arena := make([]byte, 4*refineRegion)
		picture := make([]byte, 64*80)
		var cache SMbCache
		cache.pBufferInterPredMe = arena
		var refine SMeRefinePointer
		InitMeRefinePointer(&refine, &cache, int32([]int{0, 8, 128, 136, 256}[rng.IntN(5)]))
		if rng.IntN(2) == 0 {
			refine.pQuarPixBest, refine.pQuarPixTmp = refine.pQuarPixTmp, refine.pQuarPixBest
		}
		size := sizes[rng.IntN(len(sizes))]
		pick := func() ([]byte, int) {
			switch rng.IntN(8) {
			case 0:
				return refine.pHalfPixH, rng.IntN(34)
			case 1:
				return refine.pHalfPixV, rng.IntN(34)
			case 2:
				return arena, rng.IntN(len(arena)) // arbitrary arena position
			case 3:
				return refine.pQuarPixBest, rng.IntN(34)
			default:
				return picture, rng.IntN(len(picture))
			}
		}
		var params SQuarRefineParams
		params.iStrideA = []int32{ME_REFINE_BUF_STRIDE, 64}[rng.IntN(2)]
		params.iStrideB = []int32{ME_REFINE_BUF_STRIDE, 64, 4}[rng.IntN(3)]
		for i := 0; i < 4; i++ {
			params.pSrcA[i], params.iSrcAOff[i] = pick()
			params.pSrcB[i], params.iSrcBOff[i] = pick()
		}
		me := SWelsME{}
		me.pEncMb, me.iEncMbOff = pick()
		encStride := []int32{64, ME_REFINE_BUF_STRIDE}[rng.IntN(2)]
		if quarterRefineLayoutDisjoint(&me, &refine, &params, encStride, size[0], size[1]) {
			accepted++
			if !quarterRefineBuffersDisjointFull(&me, &refine, &params, encStride, size[0], size[1]) {
				t.Fatalf("test %d: layout proof accepted overlapping buffers", test)
			}
		}
	}
	if accepted == 0 {
		t.Fatal("layout proof never accepted a configuration")
	}

	// A production configuration: half-pixel scratch inputs and a reference picture.
	arena := make([]byte, 4*refineRegion)
	picture := make([]byte, 64*80)
	var cache SMbCache
	cache.pBufferInterPredMe = arena
	var refine SMeRefinePointer
	InitMeRefinePointer(&refine, &cache, 0)
	params := SQuarRefineParams{iStrideA: ME_REFINE_BUF_STRIDE, iStrideB: 64}
	for i := 0; i < 4; i++ {
		params.pSrcA[i], params.iSrcAOff[i] = refine.pHalfPixV, i
		params.pSrcB[i], params.iSrcBOff[i] = picture, 64*2+2
	}
	me := SWelsME{pEncMb: picture, iEncMbOff: 64*40 + 8}
	if !quarterRefineLayoutDisjoint(&me, &refine, &params, 64, 16, 16) {
		t.Fatal("production layout rejected")
	}
}
