package decoder

import (
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// No C++ unit test exists for decode_slice.cpp; these are sanity checks of
// the self-contained helpers against straightforward reference formulas.

func TestWelsChromaDcIdct(t *testing.T) {
	var blk [64]int16
	blk[0], blk[16], blk[32], blk[48] = 7, -3, 11, 2
	WelsChromaDcIdct(blk[:])
	a, b, c, d := int16(7), int16(-3), int16(11), int16(2)
	want := [4]int16{a + b + c + d, a - b + c - d, a + b - c - d, a - b - c + d}
	got := [4]int16{blk[0], blk[16], blk[32], blk[48]}
	if got != want {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestWelsLumaDcDequantIdct(t *testing.T) {
	H := [4][4]int32{{1, 1, 1, 1}, {1, 1, -1, -1}, {1, -1, -1, 1}, {1, -1, 1, -1}}
	xOff := [4]int{0, 16, 64, 80}
	yOff := [4]int{0, 32, 128, 160}
	var ctx SWelsDecoderContext
	for _, iQp := range []int32{0, 10, 26, 51} {
		var blk [256]int16
		var in [4][4]int32
		for y := 0; y < 4; y++ {
			for x := 0; x < 4; x++ {
				v := int32((y*4+x)*37%53 - 26)
				in[y][x] = v
				blk[yOff[y]+xOff[x]] = int16(v)
			}
		}
		WelsLumaDcDequantIdct(blk[:], iQp, &ctx)
		qmul := int32(common.G_kuiDequantCoeff[iQp][0]) << 4
		for k := 0; k < 4; k++ {
			for i := 0; i < 4; i++ {
				var s int32
				for j := 0; j < 4; j++ {
					var tmp int32
					for x := 0; x < 4; x++ {
						tmp += H[i][x] * in[j][x]
					}
					s += H[k][j] * tmp
				}
				want := int16((s*qmul + 32) >> 6)
				if got := blk[yOff[k]+xOff[i]]; got != want {
					t.Fatalf("qp %d (%d,%d): got %d want %d", iQp, k, i, got, want)
				}
			}
		}
	}
}

func TestWelsBlockZero(t *testing.T) {
	blk := make([]int16, 16*20)
	for i := range blk {
		blk[i] = 5
	}
	WelsBlockZero8x8_c(blk[2:], 20)
	for y := 0; y < 16; y++ {
		for x := 0; x < 20; x++ {
			idx := y*20 + x
			inBlk := idx >= 2 && y < 8 && (idx-2)%20 < 8
			if inBlk && blk[idx] != 0 || !inBlk && blk[idx] != 5 {
				t.Fatalf("unexpected value %d at %d", blk[idx], idx)
			}
		}
	}
}

func TestWelsMap16x16NeighToSample(t *testing.T) {
	n := SWelsNeighAvail{iLeftAvail: 1, iTopAvail: 1, iLeftTopAvail: 1,
		iLeftType: common.MB_TYPE_INTRA4x4, iTopType: common.MB_TYPE_16x16, iLeftTopType: common.MB_TYPE_INTRA16x16}
	var v uint8
	WelsMap16x16NeighToSampleNormal(&n, &v)
	if v != 7 {
		t.Fatalf("normal: got %d", v)
	}
	v = 0
	WelsMap16x16NeighToSampleConstrain1(&n, &v)
	if v != 6 {
		t.Fatalf("constrain1: got %d", v)
	}
	var s [30]int32
	WelsMapNxNNeighToSampleConstrain1(&n, s[:])
	if s[6] != 1 || s[24] != 1 || s[0] != 1 || s[1] != 0 || s[5] != 0 {
		t.Fatalf("NxN constrain1: %v", s)
	}
}

func TestComputeColocatedTemporalScaling(t *testing.T) {
	var ctx SWelsDecoderContext
	var layer SDqLayer
	ctx.pCurDqLayer = &layer
	sh := &layer.sLayerInfo.sSliceInLayer.sSliceHeaderExt.sSliceHeader
	sh.uiRefCount[common.LIST_0] = 2
	sh.iPicOrderCntLsb = 4
	ref0 := &SPicture{iFramePoc: 0}
	ref1 := &SPicture{iFramePoc: 8}
	ctx.sRefPic.pRefList[common.LIST_0][0] = ref0
	ctx.sRefPic.pRefList[common.LIST_0][1] = ref1
	ctx.sRefPic.pRefList[common.LIST_1][0] = ref1
	if !ComputeColocatedTemporalScaling(&ctx) {
		t.Fatal("returned false")
	}
	s := layer.sLayerInfo.sSliceInLayer.iMvScale[common.LIST_0]
	// td = 8, tb = 4, tx = (16384 + 4) / 8 = 2048, (4*2048+32)>>6 = 128
	if s[0] != 128 || s[1] != 256 {
		t.Fatalf("got %d %d", s[0], s[1])
	}
}
