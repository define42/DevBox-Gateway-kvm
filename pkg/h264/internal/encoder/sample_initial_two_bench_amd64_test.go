//go:build amd64

package encoder

import (
	"fmt"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// This fixture and both benchmarks use only APIs predating the two-candidate
// experiment, so the file can be copied unchanged to the frozen baseline.
type initialTwoFixture struct {
	funcs  SWelsFuncPtrList
	me     SWelsME
	slice  SSlice
	stride int32
}

var initialTwoShapes = [][3]int{{16, 16, BLOCK_16x16}, {16, 8, BLOCK_16x8}, {8, 16, BLOCK_8x16}, {8, 8, BLOCK_8x8}, {8, 4, BLOCK_8x4}, {4, 4, BLOCK_4x4}, {4, 8, BLOCK_4x8}}

func newInitialTwoFixture(width, height int, block uint8, stride int32, scenario string) *initialTwoFixture {
	f := &initialTwoFixture{stride: stride}
	WelsInitSampleSadFunc(&f.funcs, common.WELS_CPU_SSE2)
	WelsInitMeFunc(&f.funcs, common.WELS_CPU_SSE2, false)
	f.me.uiBlockSize = block
	f.me.pEncMb, f.me.iEncMbOff = make([]byte, (height-1)*int(stride)+width+3), 3
	f.me.pRefMb, f.me.iRefMbOff = make([]byte, (height+12)*int(stride)), 4*int(stride)+8
	f.me.pColoRefMb, f.me.iColoRefMbOff = f.me.pRefMb, f.me.iRefMbOff
	for i := range f.me.pRefMb {
		f.me.pRefMb[i] = byte(i*37 + (i/int(stride))*19 + 3)
	}
	f.me.pMvdCost, f.me.iMvdCostOff = make([]uint16, 1025), 512
	for i := range f.me.pMvdCost {
		d := i - f.me.iMvdCostOff
		if d < 0 {
			d = -d
		}
		f.me.pMvdCost[i] = uint16(d)
	}
	f.me.uiSadCost, f.me.uiSatdCost = 98765, 87654
	f.me.sMv, f.me.sDirectionalMv = SMVUnitXY{3, -2}, SMVUnitXY{-1, 1}
	f.me.iDirectionalSadCost = 76543
	f.slice.uiMvcNum = 1
	f.slice.sMvc[0] = SMVUnitXY{4, 4}
	f.slice.sMvStartMin, f.slice.sMvStartMax = SMVUnitXY{-3, -3}, SMVUnitXY{3, 3}
	target := f.me.iRefMbOff + int(stride) + 1
	switch scenario {
	case "mvpWins":
		target = f.me.iRefMbOff
	case "tie":
		clear(f.me.pRefMb)
		clear(f.me.pMvdCost)
	case "duplicate":
		f.slice.sMvc[0] = SMVUnitXY{1, -1}
	case "fiveMVC":
		f.slice.uiMvcNum = 5
		f.slice.sMvc = [5]SMVUnitXY{{4, 4}, {4, 4}, {-4, 4}, {-4, -4}, {4, -4}}
	case "zeroMVC":
		f.slice.uiMvcNum = 0
	}
	for y := range height {
		copy(f.me.pEncMb[f.me.iEncMbOff+y*int(stride):][:width], f.me.pRefMb[target+y*int(stride):][:width])
	}
	return f
}

func BenchmarkInitialPointTwoCandidates(b *testing.B) {
	for _, shape := range initialTwoShapes {
		for _, stride := range []int32{32, 2048} {
			for _, scenario := range []string{"mvcWins", "duplicate", "fiveMVC"} {
				b.Run(fmt.Sprintf("%dx%d/stride%d/%s", shape[0], shape[1], stride, scenario), func(b *testing.B) {
					f := newInitialTwoFixture(shape[0], shape[1], uint8(shape[2]), stride, scenario)
					base := f.me
					b.ReportAllocs()
					for b.Loop() {
						f.me = base
						WelsMotionEstimateInitialPoint(&f.funcs, &f.me, &f.slice, stride, stride)
						sampleSatdBenchmarkSink = int32(f.me.uiSadCost)
					}
				})
			}
		}
	}
}

// Include per-invocation state, because a fixture stored outside b.Loop hid
// the previous quarter callback escape. Existing directional callbacks may
// already make local ME state escape; compare allocations to the same baseline.
func BenchmarkInitialPointLocalState(b *testing.B) {
	for _, shape := range initialTwoShapes[:4] {
		b.Run(fmt.Sprintf("%dx%d", shape[0], shape[1]), func(b *testing.B) {
			f := newInitialTwoFixture(shape[0], shape[1], uint8(shape[2]), 2048, "mvcWins")
			b.ReportAllocs()
			for b.Loop() {
				me := f.me
				WelsMotionEstimateInitialPoint(&f.funcs, &me, &f.slice, f.stride, f.stride)
				sampleSatdBenchmarkSink = int32(me.uiSadCost)
			}
		})
	}
}
