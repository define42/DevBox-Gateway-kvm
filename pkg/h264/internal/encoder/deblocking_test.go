// No C unit test exists for the encoder deblocking.cpp; these tests check
// the boundary strength derivation against direct evaluations of the rules.

package encoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestDeblockingBSInsideMB(t *testing.T) {
	r := rand.New(rand.NewSource(60))
	for it := 0; it < 500; it++ {
		nnz := make([]int8, 48)
		for i := 0; i < 16; i++ {
			nnz[i] = int8(r.Intn(2))
		}
		mb := SMB{sMv: make([]SMVUnitXY, 16)}
		for i := range mb.sMv {
			mb.sMv[i] = SMVUnitXY{int16(r.Intn(12) - 6), int16(r.Intn(12) - 6)}
		}
		var bs, bs16 [2][4][4]uint8
		DeblockingBSInsideMBNormal(&mb, &bs, nnz)
		DeblockingBSInsideMBAvsbase(nnz, &bs16, 1)
		for e := 1; e < 4; e++ {
			for k := 0; k < 4; k++ {
				// vertical edge e, row k: blocks (k*4+e) and (k*4+e-1)
				for d := 0; d < 2; d++ {
					var b, bn int
					if d == 0 {
						b, bn = k*4+e, k*4+e-1
					} else {
						b, bn = e*4+k, (e-1)*4+k
					}
					var want uint8
					if nnz[b]|nnz[bn] != 0 {
						want = 2
					} else if common.WELS_ABS(int32(mb.sMv[b].iMvX)-int32(mb.sMv[bn].iMvX)) >= 4 ||
						common.WELS_ABS(int32(mb.sMv[b].iMvY)-int32(mb.sMv[bn].iMvY)) >= 4 {
						want = 1
					}
					if bs[d][e][k] != want {
						t.Fatalf("normal bs[%d][%d][%d] = %d want %d", d, e, k, bs[d][e][k], want)
					}
					want16 := uint8(0)
					if nnz[b]|nnz[bn] != 0 {
						want16 = 2
					}
					if bs16[d][e][k] != want16 {
						t.Fatalf("16x16 bs[%d][%d][%d] = %d want %d", d, e, k, bs16[d][e][k], want16)
					}
				}
			}
		}
	}
}

// A flat picture with all edges filtered must stay flat.
func TestDeblockingFilterFrameFlat(t *testing.T) {
	const mbW, mbH = 3, 2
	const pad = 32
	strideY := int32(mbW*16 + 2*pad)
	strideC := int32(mbW*8 + pad)
	hY := mbH*16 + 2*pad
	hC := mbH*8 + pad
	buf := make([]uint8, int(strideY)*hY+2*int(strideC)*hC)
	for i := range buf {
		buf[i] = 77
	}
	var pic SPicture
	pic.pBuffer = buf
	for i := 0; i < 3; i++ {
		pic.pData[i] = buf
	}
	pic.iDataOff[0] = pad*int(strideY) + pad
	pic.iDataOff[1] = int(strideY)*hY + (pad/2)*int(strideC) + pad/2
	pic.iDataOff[2] = pic.iDataOff[1] + int(strideC)*hC
	pic.iLineSize = [3]int32{strideY, strideC, strideC}

	mbs := make([]SMB, mbW*mbH)
	nnz := make([]int8, 48*mbW*mbH)
	mvs := make([]SMVUnitXY, 16*mbW*mbH)
	refs := make([]int8, 4*mbW*mbH)
	for i := range mbs {
		mbs[i].iMbXY = int32(i)
		mbs[i].iMbX = int16(i % mbW)
		mbs[i].iMbY = int16(i / mbW)
		mbs[i].pMbList = mbs
		mbs[i].pNonZeroCount = nnz[i*48 : i*48+48]
		mbs[i].sMv = mvs[i*16 : i*16+16]
		mbs[i].pRefIndex = refs[i*4 : i*4+4]
		mbs[i].uiLumaQp = 40
		mbs[i].uiChromaQp = 38
		if i%2 == 0 {
			mbs[i].uiMbType = common.MB_TYPE_INTRA16x16
		} else {
			mbs[i].uiMbType = common.MB_TYPE_16x16
			for k := 0; k < 16; k++ {
				nnz[i*48+k] = 3
			}
		}
	}
	var sl SSlice
	var dq SDqLayer
	dq.sMbDataP = mbs
	dq.iMbWidth = mbW
	dq.iMbHeight = mbH
	dq.pDecPic = &pic
	dq.ppSliceInLayer = []*SSlice{&sl}

	var fl SWelsFuncPtrList
	DeblockingInit(&fl.pfDeblocking, 0)
	WelsBlockFuncInit(&fl.pfSetNZCZero, 0)
	DeblockingFilterFrameAvcbase(&dq, &fl)
	for i, v := range buf {
		if v != 77 {
			t.Fatalf("flat picture changed at %d: %d", i, v)
		}
	}
	// pfSetNZCZero normalised the inter MBs' nnz to 1
	if nnz[48] != 1 {
		t.Fatalf("nnz not normalised: %d", nnz[48])
	}
}
