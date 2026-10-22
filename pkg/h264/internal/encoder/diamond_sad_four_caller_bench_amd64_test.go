//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// This fixture uses only APIs and fields present in round10. Copy this file
// unchanged to the baseline for complete-caller before/after measurements.
var diamondSadFourCallerShapes = []struct {
	name          string
	block         uint8
	width, height int
}{
	{"16x16", BLOCK_16x16, 16, 16},
	{"16x8", BLOCK_16x8, 16, 8},
	{"8x16", BLOCK_8x16, 8, 16},
	{"8x8", BLOCK_8x8, 8, 8},
	{"8x4", BLOCK_8x4, 8, 4},
	{"4x8", BLOCK_4x8, 4, 8},
	{"4x4", BLOCK_4x4, 4, 4},
}

type diamondSadFourCallerFixture struct {
	funcs      SWelsFuncPtrList
	initial    SWelsME
	slice      SSlice
	stride     int32
	wantMv     SMVUnitXY
	wantCost   uint32
	wantRefOff int
}

func newDiamondSadFourCallerFixture(block uint8, width, height, stride, motion int, cpuFlags uint32) *diamondSadFourCallerFixture {
	f := &diamondSadFourCallerFixture{stride: int32(stride)}
	WelsInitSampleSadFunc(&f.funcs, cpuFlags)
	const rows, originX, originY = 64, 8, 20
	ref := make([]byte, rows*stride)
	for y := 0; y < rows; y++ {
		for x := 0; x < stride; x++ {
			switch motion {
			case 1:
				ref[y*stride+x] = byte(x * 4)
			case 2:
				ref[y*stride+x] = byte(y * 4)
			default:
				ref[y*stride+x] = byte(x*19 + y*37 + x*y*3)
			}
		}
	}
	if motion == 1 {
		f.wantMv.iMvX = 3
	} else if motion == 2 {
		f.wantMv.iMvY = 3
	}
	refOff := originY*stride + originX
	f.wantRefOff = refOff + int(f.wantMv.iMvY)*stride + int(f.wantMv.iMvX)
	src := make([]byte, (height-1)*stride+width)
	for y := 0; y < height; y++ {
		copy(src[y*stride:y*stride+width], ref[f.wantRefOff+y*stride:f.wantRefOff+y*stride+width])
	}
	mvd := make([]uint16, 512)
	for i := range mvd {
		d := i - len(mvd)/2
		if d < 0 {
			d = -d
		}
		mvd[i] = uint16(d)
	}
	f.wantCost = uint32(4 * (f.wantMv.iMvX + f.wantMv.iMvY))
	f.initial = SWelsME{
		pMvdCost: mvd, iMvdCostOff: len(mvd) / 2,
		uiBlockSize: block, pEncMb: src, pRefMb: ref, iRefMbOff: refOff,
	}
	f.initial.uiSadCost = uint32(f.funcs.sSampleDealingFuncs.pfSampleSad[block](src, 0, int32(stride), ref, refOff, int32(stride)))
	f.initial.uiSatdCost = f.initial.uiSadCost
	f.slice.sMvStartMin = SMVUnitXY{-6, -16}
	f.slice.sMvStartMax = SMVUnitXY{7, 17}
	return f
}

var diamondSadFourCallerSink struct {
	mv     SMVUnitXY
	cost   uint32
	refOff int
}

func BenchmarkDiamondSadFourCaller(b *testing.B) {
	for _, shape := range diamondSadFourCallerShapes {
		for _, stride := range []int{32, 2048} {
			for motion, name := range []string{"Stationary", "MoveX3", "MoveY3"} {
				b.Run(fmt.Sprintf("%s/stride%d/%s", shape.name, stride, name), func(b *testing.B) {
					f := newDiamondSadFourCallerFixture(shape.block, shape.width, shape.height, stride, motion, common.WELS_CPU_SSE2)
					me := f.initial
					b.ReportAllocs()
					for b.Loop() {
						// Reset the complete mutable search state. Picture/table
						// construction and descriptor allocation stay outside.
						me.sMv = f.initial.sMv
						me.uiSadCost = f.initial.uiSadCost
						me.uiSatdCost = f.initial.uiSatdCost
						me.iRefMbOff = f.initial.iRefMbOff
						WelsDiamondSearch(&f.funcs, &me, &f.slice, f.stride, f.stride)
					}
					if me.sMv != f.wantMv || me.uiSadCost != f.wantCost || me.uiSatdCost != f.wantCost || me.iRefMbOff != f.wantRefOff {
						b.Fatalf("search got mv=%v cost=%d/%d ref=%d, want %v/%d/%d", me.sMv, me.uiSadCost, me.uiSatdCost, me.iRefMbOff, f.wantMv, f.wantCost, f.wantRefOff)
					}
					diamondSadFourCallerSink.mv = me.sMv
					diamondSadFourCallerSink.cost = me.uiSadCost
					diamondSadFourCallerSink.refOff = me.iRefMbOff
				})
			}
		}
	}
}
