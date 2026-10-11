// No C unit test exists for mv_pred.cpp; these tests check the H.264 MV
// prediction rules (clause 8.4.1.3) on the encoder MV cache layout.

package encoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func median3(a, b, c int16) int16 {
	return int16(common.WelsMedian(int32(a), int32(b), int32(c)))
}

func TestPredMv16x16(t *testing.T) {
	r := rand.New(rand.NewSource(50))
	for it := 0; it < 2000; it++ {
		var c SMVComponentUnit
		for i := range c.iRefIndexCache {
			c.iRefIndexCache[i] = int8(r.Intn(4) - 2) // -2 (not avail) .. 1
		}
		for i := range c.sMotionVectorCache {
			c.sMotionVectorCache[i] = SMVUnitXY{int16(r.Intn(64) - 32), int16(r.Intn(64) - 32)}
		}
		iRef := int32(r.Intn(2))
		var got SMVUnitXY
		PredMv(&c, 0, 4, iRef, &got)

		// A = left (6), B = top (1), C = top-right (5) or D = top-left (0)
		refA, refB := int32(c.iRefIndexCache[6]), int32(c.iRefIndexCache[1])
		mvA, mvB := c.sMotionVectorCache[6], c.sMotionVectorCache[1]
		refC, mvC := int32(c.iRefIndexCache[5]), c.sMotionVectorCache[5]
		if refC == common.REF_NOT_AVAIL {
			refC, mvC = int32(c.iRefIndexCache[0]), c.sMotionVectorCache[0]
		}
		var want SMVUnitXY
		if refB == common.REF_NOT_AVAIL && refC == common.REF_NOT_AVAIL && refA != common.REF_NOT_AVAIL {
			want = mvA
		} else {
			n := 0
			if refA == iRef {
				n++
				want = mvA
			}
			if refB == iRef {
				n++
				want = mvB
			}
			if refC == iRef {
				n++
				want = mvC
			}
			if n != 1 {
				want = SMVUnitXY{median3(mvA.iMvX, mvB.iMvX, mvC.iMvX), median3(mvA.iMvY, mvB.iMvY, mvC.iMvY)}
			}
		}
		if got != want {
			t.Fatalf("PredMv: got %v want %v (cache %+v ref %d)", got, want, c, iRef)
		}
	}
}

func TestUpdateMotionInfo(t *testing.T) {
	var mbc SMbCache
	mb := SMB{sMv: make([]SMVUnitXY, 16), pRefIndex: make([]int8, 4)}
	mv := SMVUnitXY{5, -7}
	UpdateP16x16MotionInfo(&mbc, &mb, 1, &mv)
	for i := 0; i < 16; i++ {
		if mb.sMv[i] != mv {
			t.Fatalf("sMv[%d]", i)
		}
		ci := common.G_kuiCache30ScanIdx[i]
		if mbc.sMvComponents.sMotionVectorCache[ci] != mv || mbc.sMvComponents.iRefIndexCache[ci] != 1 {
			t.Fatalf("cache[%d]", ci)
		}
	}
	for i := 0; i < 4; i++ {
		if mb.pRefIndex[i] != 1 {
			t.Fatalf("pRefIndex[%d]", i)
		}
	}

	// 16x8 lower partition
	mv2 := SMVUnitXY{-3, 2}
	UpdateP16x8MotionInfo(&mbc, &mb, 8, 0, &mv2)
	for i := 0; i < 16; i++ {
		want := mv
		if i >= 8 {
			want = mv2
		}
		if mb.sMv[i] != want {
			t.Fatalf("16x8 sMv[%d] = %v want %v", i, mb.sMv[i], want)
		}
	}
	if mb.pRefIndex[0] != 1 || mb.pRefIndex[1] != 1 || mb.pRefIndex[2] != 0 || mb.pRefIndex[3] != 0 {
		t.Fatalf("16x8 pRefIndex %v", mb.pRefIndex)
	}

	// 8x16 right partition: scan4 indices 4..7 / 12..15 are blocks with x>=8 in 8x8 order
	mv3 := SMVUnitXY{9, 9}
	update_P8x16_motion_info(&mbc, &mb, 4, 2, &mv3)
	for _, i := range []int{4, 5, 6, 7, 12, 13, 14, 15} {
		if mb.sMv[common.G_kuiMbCountScan4Idx[i]] != mv3 {
			t.Fatalf("8x16 sMv[scan %d]", i)
		}
	}
	if mb.pRefIndex[1] != 2 || mb.pRefIndex[3] != 2 {
		t.Fatalf("8x16 pRefIndex %v", mb.pRefIndex)
	}
}

func TestPredSkipMv(t *testing.T) {
	var mbc SMbCache
	c := &mbc.sMvComponents
	for i := range c.iRefIndexCache {
		c.iRefIndexCache[i] = 0
	}
	c.sMotionVectorCache[6] = SMVUnitXY{4, 4}
	c.sMotionVectorCache[1] = SMVUnitXY{8, 0}
	c.sMotionVectorCache[5] = SMVUnitXY{6, 2}
	var got SMVUnitXY
	PredSkipMv(&mbc, &got)
	if got != (SMVUnitXY{6, 2}) {
		t.Fatalf("skip mvp %v", got)
	}
	c.sMotionVectorCache[1] = SMVUnitXY{}
	PredSkipMv(&mbc, &got)
	if got != (SMVUnitXY{}) {
		t.Fatalf("skip mvp (zero top) %v", got)
	}
}
