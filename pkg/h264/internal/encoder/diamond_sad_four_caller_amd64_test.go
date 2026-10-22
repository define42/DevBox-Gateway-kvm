//go:build amd64

package encoder

import (
	"fmt"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestDiamondSadFourCallerParity(t *testing.T) {
	for _, shape := range diamondSadFourCallerShapes {
		for _, stride := range []int{32, 2048} {
			for motion := 0; motion < 3; motion++ {
				t.Run(fmt.Sprintf("%s/stride%d/motion%d", shape.name, stride, motion), func(t *testing.T) {
					got := newDiamondSadFourCallerFixture(shape.block, shape.width, shape.height, stride, motion, common.WELS_CPU_SSE2)
					want := newDiamondSadFourCallerFixture(shape.block, shape.width, shape.height, stride, motion, 0)
					WelsDiamondSearch(&got.funcs, &got.initial, &got.slice, got.stride, got.stride)
					WelsDiamondSearch(&want.funcs, &want.initial, &want.slice, want.stride, want.stride)
					if !reflect.DeepEqual(got.initial, want.initial) || got.slice.sMbCacheInfo.iDiamondSad != want.slice.sMbCacheInfo.iDiamondSad {
						t.Fatal("complete search state differs from independent scalar SAD route")
					}
					if got.initial.sMv != got.wantMv || got.initial.uiSadCost != got.wantCost || got.initial.iRefMbOff != got.wantRefOff {
						t.Fatalf("fixture did not reach expected result: mv=%v cost=%d ref=%d, want %v/%d/%d", got.initial.sMv, got.initial.uiSadCost, got.initial.iRefMbOff, got.wantMv, got.wantCost, got.wantRefOff)
					}
				})
			}
		}
	}
}

func TestDiamondSadFourCallerCallbackAndTies(t *testing.T) {
	for _, shape := range diamondSadFourCallerShapes {
		for motion := 0; motion < 3; motion++ {
			f := newDiamondSadFourCallerFixture(shape.block, shape.width, shape.height, 32, motion, common.WELS_CPU_SSE2)
			original := f.funcs.sSampleDealingFuncs.pfSample4Sad[shape.block]
			calls := 0
			f.funcs.sSampleDealingFuncs.pfSample4Sad[shape.block] = func(a []byte, ao int, as int32, ref []byte, ro int, rs int32, costs []int32) {
				calls++
				wantRef := f.initial.iRefMbOff
				if motion == 1 {
					wantRef += calls - 1
				} else if motion == 2 {
					wantRef += (calls - 1) * 32
				}
				if ro != wantRef {
					t.Fatalf("%s motion=%d call=%d ref=%d want=%d", shape.name, motion, calls, ro, wantRef)
				}
				original(a, ao, as, ref, ro, rs, costs)
			}
			me := f.initial
			WelsDiamondSearch(&f.funcs, &me, &f.slice, f.stride, f.stride)
			wantCalls := 4
			if motion == 0 {
				wantCalls = 1
			}
			if calls != wantCalls || me.sMv != f.wantMv {
				t.Fatalf("%s motion=%d calls=%d mv=%v, want=%d/%v", shape.name, motion, calls, me.sMv, wantCalls, f.wantMv)
			}
		}

		f := newDiamondSadFourCallerFixture(shape.block, shape.width, shape.height, 32, 0, common.WELS_CPU_SSE2)
		clear(f.initial.pEncMb)
		clear(f.initial.pRefMb)
		clear(f.initial.pMvdCost)
		f.initial.uiSadCost = 1
		me := f.initial
		WelsDiamondSearch(&f.funcs, &me, &f.slice, f.stride, f.stride)
		if me.sMv != (SMVUnitXY{0, -1}) || me.uiSadCost != 0 || me.iRefMbOff != f.initial.iRefMbOff-32 {
			t.Fatalf("%s equal candidate costs did not keep first (up) candidate: mv=%v cost=%d ref=%d", shape.name, me.sMv, me.uiSadCost, me.iRefMbOff)
		}
	}
}

func TestDiamondSadFourCallerZeroAllocs(t *testing.T) {
	for _, shape := range diamondSadFourCallerShapes {
		f := newDiamondSadFourCallerFixture(shape.block, shape.width, shape.height, 2048, 1, common.WELS_CPU_SSE2)
		if allocs := testing.AllocsPerRun(100, func() {
			me := f.initial
			WelsDiamondSearch(&f.funcs, &me, &f.slice, f.stride, f.stride)
			diamondSadFourCallerSink.cost = me.uiSadCost
		}); allocs != 0 {
			t.Fatalf("%s: complete search allocated %g times", shape.name, allocs)
		}
	}
}
