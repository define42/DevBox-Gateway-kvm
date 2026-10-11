// Port of test/encoder/EncUT_GetIntraPredictor.cpp. The C tests re-derive
// each predictor; here the expected values come from H.264 clause 8.3
// reference formulas evaluated on a random neighbourhood with a non-zero
// stride (the *Top variants replace the top-right samples by the last top
// sample, as the encoder does).

package encoder

import (
	"math/rand"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

type ipNeighbours struct {
	buf    []uint8
	off    int // origin (top-left sample of the block)
	stride int32
}

func newIpNeighbours(r *rand.Rand, stride int32, rows int) ipNeighbours {
	buf := make([]uint8, int(stride)*(rows+2))
	for i := range buf {
		buf[i] = uint8(r.Intn(256))
	}
	return ipNeighbours{buf: buf, off: int(stride) + 1, stride: stride}
}

// top(x) = p[x,-1] (x may be -1 for the top-left corner), left(y) = p[-1,y].
func (n ipNeighbours) top(x int) int32  { return int32(n.buf[n.off-int(n.stride)+x]) }
func (n ipNeighbours) left(y int) int32 { return int32(n.buf[n.off+y*int(n.stride)-1]) }

// p is p[x,y] with the H.264 convention (x=-1 left column, y=-1 top row).
func (n ipNeighbours) p(x, y int) int32 {
	if y == -1 {
		return n.top(x)
	}
	return n.left(y)
}

func ref4x4(n ipNeighbours, mode int) [16]uint8 {
	var out [16]uint8
	topClamped := func(x int) int32 {
		if mode == common.I4_PRED_DDL_TOP || mode == common.I4_PRED_VL_TOP {
			if x > 3 {
				x = 3
			}
		}
		return n.top(x)
	}
	for y := 0; y < 4; y++ {
		for x := 0; x < 4; x++ {
			var v int32
			switch mode {
			case common.I4_PRED_V:
				v = n.top(x)
			case common.I4_PRED_H:
				v = n.left(y)
			case common.I4_PRED_DC:
				v = (n.top(0) + n.top(1) + n.top(2) + n.top(3) + n.left(0) + n.left(1) + n.left(2) + n.left(3) + 4) >> 3
			case common.I4_PRED_DC_L:
				v = (n.left(0) + n.left(1) + n.left(2) + n.left(3) + 2) >> 2
			case common.I4_PRED_DC_T:
				v = (n.top(0) + n.top(1) + n.top(2) + n.top(3) + 2) >> 2
			case common.I4_PRED_DC_128:
				v = 128
			case common.I4_PRED_DDL, common.I4_PRED_DDL_TOP:
				if x == 3 && y == 3 {
					v = (topClamped(6) + 3*topClamped(7) + 2) >> 2
				} else {
					v = (topClamped(x+y) + 2*topClamped(x+y+1) + topClamped(x+y+2) + 2) >> 2
				}
			case common.I4_PRED_DDR:
				if x > y {
					v = (n.top(x-y-2) + 2*n.top(x-y-1) + n.top(x-y) + 2) >> 2
				} else if x < y {
					v = (n.p(-1, y-x-2) + 2*n.p(-1, y-x-1) + n.p(-1, y-x) + 2) >> 2
				} else {
					v = (n.top(0) + 2*n.top(-1) + n.left(0) + 2) >> 2
				}
			case common.I4_PRED_VR:
				z := 2*x - y
				switch {
				case z >= 0 && z&1 == 0:
					v = (n.top(x-(y>>1)-1) + n.top(x-(y>>1)) + 1) >> 1
				case z >= 0:
					v = (n.top(x-(y>>1)-2) + 2*n.top(x-(y>>1)-1) + n.top(x-(y>>1)) + 2) >> 2
				case z == -1:
					v = (n.left(0) + 2*n.top(-1) + n.top(0) + 2) >> 2
				default:
					v = (n.left(y-1) + 2*n.left(y-2) + n.p(-1, y-3) + 2) >> 2
				}
			case common.I4_PRED_HD:
				z := 2*y - x
				switch {
				case z >= 0 && z&1 == 0:
					v = (n.p(-1, y-(x>>1)-1) + n.left(y-(x>>1)) + 1) >> 1
				case z >= 0:
					v = (n.p(-1, y-(x>>1)-2) + 2*n.p(-1, y-(x>>1)-1) + n.left(y-(x>>1)) + 2) >> 2
				case z == -1:
					v = (n.left(0) + 2*n.top(-1) + n.top(0) + 2) >> 2
				default:
					v = (n.top(x-1) + 2*n.top(x-2) + n.top(x-3) + 2) >> 2
				}
			case common.I4_PRED_VL, common.I4_PRED_VL_TOP:
				if y&1 == 0 {
					v = (topClamped(x+(y>>1)) + topClamped(x+(y>>1)+1) + 1) >> 1
				} else {
					v = (topClamped(x+(y>>1)) + 2*topClamped(x+(y>>1)+1) + topClamped(x+(y>>1)+2) + 2) >> 2
				}
			case common.I4_PRED_HU:
				z := x + 2*y
				switch {
				case z < 5 && z&1 == 0:
					v = (n.left(y+(x>>1)) + n.left(y+(x>>1)+1) + 1) >> 1
				case z < 5:
					v = (n.left(y+(x>>1)) + 2*n.left(y+(x>>1)+1) + n.left(y+(x>>1)+2) + 2) >> 2
				case z == 5:
					v = (n.left(2) + 3*n.left(3) + 2) >> 2
				default:
					v = n.left(3)
				}
			}
			out[y*4+x] = uint8(v)
		}
	}
	return out
}

func TestGetIntraPredictor_I4x4(t *testing.T) {
	r := rand.New(rand.NewSource(20))
	var sFuncList SWelsFuncPtrList
	WelsInitIntraPredFuncs(&sFuncList, 0)
	for iter := 0; iter < 200; iter++ {
		stride := int32(r.Intn(256) + 16)
		n := newIpNeighbours(r, stride, 8)
		for mode := 0; mode < common.I4_PRED_A; mode++ {
			pPred := make([]uint8, 20)
			sFuncList.pfGetLumaI4x4Pred[mode](pPred, 2, n.buf, n.off, stride)
			want := ref4x4(n, mode)
			for i := 0; i < 16; i++ {
				if pPred[2+i] != want[i] {
					t.Fatalf("I4x4 mode %d: pred[%d] = %d, want %d", mode, i, pPred[2+i], want[i])
				}
			}
		}
	}
}

func refChroma(n ipNeighbours, mode int) [64]uint8 {
	var out [64]uint8
	sumT := func(a int) int32 { return n.top(a) + n.top(a+1) + n.top(a+2) + n.top(a+3) }
	sumL := func(a int) int32 { return n.left(a) + n.left(a+1) + n.left(a+2) + n.left(a+3) }
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			var v int32
			switch mode {
			case common.C_PRED_V:
				v = n.top(x)
			case common.C_PRED_H:
				v = n.left(y)
			case common.C_PRED_DC:
				bx, by := x>>2, y>>2
				switch {
				case bx == 0 && by == 0:
					v = (sumT(0) + sumL(0) + 4) >> 3
				case bx == 1 && by == 0:
					v = (sumT(4) + 2) >> 2
				case bx == 0 && by == 1:
					v = (sumL(4) + 2) >> 2
				default:
					v = (sumT(4) + sumL(4) + 4) >> 3
				}
			case common.C_PRED_DC_L:
				v = (sumL((y>>2)*4) + 2) >> 2
			case common.C_PRED_DC_T:
				v = (sumT((x>>2)*4) + 2) >> 2
			case common.C_PRED_DC_128:
				v = 128
			case common.C_PRED_P:
				var H, V int32
				for i := 0; i < 4; i++ {
					H += int32(i+1) * (n.top(4+i) - n.top(2-i))
					V += int32(i+1) * (n.p(-1, 4+i) - n.p(-1, 2-i))
				}
				a := 16 * (n.left(7) + n.top(7))
				b := (34*H + 32) >> 6
				c := (34*V + 32) >> 6
				v = int32(common.WelsClip1((a + b*int32(x-3) + c*int32(y-3) + 16) >> 5))
			}
			out[y*8+x] = uint8(v)
		}
	}
	return out
}

func TestGetIntraPredictor_Chroma(t *testing.T) {
	r := rand.New(rand.NewSource(21))
	var sFuncList SWelsFuncPtrList
	WelsInitIntraPredFuncs(&sFuncList, 0)
	for iter := 0; iter < 200; iter++ {
		stride := int32(r.Intn(64) + 16)
		n := newIpNeighbours(r, stride, 10)
		for mode := 0; mode < common.C_PRED_A; mode++ {
			pPred := make([]uint8, 70)
			sFuncList.pfGetChromaPred[mode](pPred, 3, n.buf, n.off, stride)
			want := refChroma(n, mode)
			for i := 0; i < 64; i++ {
				if pPred[3+i] != want[i] {
					t.Fatalf("chroma mode %d: pred[%d] = %d, want %d", mode, i, pPred[3+i], want[i])
				}
			}
		}
	}
}

func ref16x16(n ipNeighbours, mode int) [256]uint8 {
	var out [256]uint8
	var sumT, sumL int32
	for i := 0; i < 16; i++ {
		sumT += n.top(i)
		sumL += n.left(i)
	}
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			var v int32
			switch mode {
			case common.I16_PRED_V:
				v = n.top(x)
			case common.I16_PRED_H:
				v = n.left(y)
			case common.I16_PRED_DC:
				v = (sumT + sumL + 16) >> 5
			case common.I16_PRED_DC_L:
				v = (sumL + 8) >> 4
			case common.I16_PRED_DC_T:
				v = (sumT + 8) >> 4
			case common.I16_PRED_DC_128:
				v = 128
			case common.I16_PRED_P:
				var H, V int32
				for i := 0; i < 8; i++ {
					H += int32(i+1) * (n.top(8+i) - n.top(6-i))
					V += int32(i+1) * (n.p(-1, 8+i) - n.p(-1, 6-i))
				}
				a := 16 * (n.left(15) + n.top(15))
				b := (5*H + 32) >> 6
				c := (5*V + 32) >> 6
				v = int32(common.WelsClip1((a + b*int32(x-7) + c*int32(y-7) + 16) >> 5))
			}
			out[y*16+x] = uint8(v)
		}
	}
	return out
}

func TestGetIntraPredictor_I16x16(t *testing.T) {
	r := rand.New(rand.NewSource(22))
	var sFuncList SWelsFuncPtrList
	WelsInitIntraPredFuncs(&sFuncList, 0)
	for iter := 0; iter < 100; iter++ {
		stride := int32(r.Intn(16) + 16)
		n := newIpNeighbours(r, stride, 18)
		for mode := 0; mode < common.I16_PRED_DC_A; mode++ {
			pPred := make([]uint8, 260)
			sFuncList.pfGetLumaI16x16Pred[mode](pPred, 1, n.buf, n.off, stride)
			want := ref16x16(n, mode)
			for i := 0; i < 256; i++ {
				if pPred[1+i] != want[i] {
					t.Fatalf("I16x16 mode %d: pred[%d] = %d, want %d", mode, i, pPred[1+i], want[i])
				}
			}
		}
	}
}
