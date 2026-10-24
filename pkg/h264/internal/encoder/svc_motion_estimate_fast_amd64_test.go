//go:build amd64

package encoder

import (
	"math/rand/v2"
	"reflect"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

// The fused integer search must leave exactly the motion-estimation state of
// WelsMotionEstimateInitialPoint followed by WelsDiamondSearch.
func TestWelsMotionEstimateIntegerFastParity(t *testing.T) {
	if !common.SadPtrSupported {
		t.Skip("pointer SAD kernels unavailable")
	}
	var funcs SWelsFuncPtrList
	WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2)
	for i := range funcs.pfSearchMethod {
		funcs.pfSearchMethod[i] = WelsDiamondSearch
	}
	funcs.bWholeIntegerSearch = true
	rng := rand.New(rand.NewPCG(4, 4))
	const stride, rows = 96, 96
	costs := make([]uint16, 2048)
	for i := range costs {
		costs[i] = uint16(rng.IntN(64))
	}
	var fast, slow int
	for test := 0; test < 2500; test++ {
		enc := make([]byte, stride*rows)
		ref := make([]byte, stride*rows)
		for i := range enc {
			enc[i] = byte(rng.Uint32())
			ref[i] = enc[i] + byte(rng.IntN(9)) // correlated so the search moves
		}
		if test%3 == 0 { // shift the reference so the best match is off center
			copy(ref[3*stride+2:], enc)
		}
		funcs.pfCheckDirectionalMv = CheckDirectionalMvFalse
		var slice SSlice
		span := int16(1 + rng.IntN(12))
		slice.sMvStartMin = SMVUnitXY{-span, -span}
		slice.sMvStartMax = SMVUnitXY{span, span}
		slice.uiMvcNum = uint8(rng.IntN(len(slice.sMvc) + 1))
		for i := range slice.sMvc {
			slice.sMvc[i] = SMVUnitXY{int16(rng.IntN(129) - 64), int16(rng.IntN(129) - 64)}
		}
		me := SWelsME{
			uiBlockSize:   uint8(rng.IntN(MAX_BLOCK_TYPE)),
			pMvdCost:      costs,
			iMvdCostOff:   1024,
			pEncMb:        enc,
			iEncMbOff:     40*stride + 40,
			pRefMb:        ref,
			iRefMbOff:     40*stride + 40,
			sMvp:          SMVUnitXY{int16(rng.IntN(65) - 32), int16(rng.IntN(65) - 32)},
			uSadPredISatd: uint32(rng.IntN(4000)),
		}
		if test%7 == 0 { // reference window out of range: must decline
			me.iRefMbOff = 2*stride + 2
		}
		want, got := me, me
		wantSlice, gotSlice := slice, slice
		if !welsMotionEstimateIntegerFast(&funcs, &got, &gotSlice, stride, stride) {
			slow++
			if !reflect.DeepEqual(got, me) || gotSlice.sMbCacheInfo.iDiamondSad != slice.sMbCacheInfo.iDiamondSad {
				t.Fatalf("test %d: declined search changed state", test)
			}
			continue
		}
		fast++
		if !WelsMotionEstimateInitialPoint(&funcs, &want, &wantSlice, stride, stride) {
			WelsDiamondSearch(&funcs, &want, &wantSlice, stride, stride)
			MeEndIntepelSearch(&want)
		}
		if !reflect.DeepEqual(got, want) || unsafe.SliceData(got.pRefMb) != unsafe.SliceData(want.pRefMb) ||
			gotSlice.sMbCacheInfo.iDiamondSad != wantSlice.sMbCacheInfo.iDiamondSad {
			t.Fatalf("test %d block %d: mv %v cost %d/%d off %d, want mv %v cost %d/%d off %d", test, me.uiBlockSize,
				got.sMv, got.uiSadCost, got.uiSatdCost, got.iRefMbOff, want.sMv, want.uiSadCost, want.uiSatdCost, want.iRefMbOff)
		}
	}
	if fast == 0 || slow == 0 {
		t.Fatalf("fast %d, declined %d: both paths must be exercised", fast, slow)
	}
}

func TestWelsMotionEstimateIntegerFastFallback(t *testing.T) {
	tests := []struct {
		name   string
		change func(*diamondSadFourCallerFixture)
	}{
		{"negativeCostBase", func(f *diamondSadFourCallerFixture) {
			f.initial.iMvdCostOff = -1
			f.initial.sMvp = SMVUnitXY{-128, -128}
		}},
		{"oversizedCostBase", func(f *diamondSadFourCallerFixture) {
			f.initial.iMvdCostOff = len(f.initial.pMvdCost)
			f.initial.sMvp = SMVUnitXY{128, 128}
		}},
		{"disabled", func(f *diamondSadFourCallerFixture) { f.funcs.bWholeIntegerSearch = false }},
		{"scalar", func(f *diamondSadFourCallerFixture) { WelsInitSampleSadFunc(&f.funcs, 0) }},
		{"shortSource", func(f *diamondSadFourCallerFixture) { f.initial.pEncMb = f.initial.pEncMb[:1] }},
		{"shortReference", func(f *diamondSadFourCallerFixture) { f.initial.pRefMb = f.initial.pRefMb[:f.initial.iRefMbOff] }},
		{"shortCosts", func(f *diamondSadFourCallerFixture) { f.initial.pMvdCost = f.initial.pMvdCost[:1] }},
		{"negativeOffset", func(f *diamondSadFourCallerFixture) { f.initial.iEncMbOff = -1 }},
		{"overflowOffset", func(f *diamondSadFourCallerFixture) { f.initial.iRefMbOff = int(^uint(0) >> 1) }},
		{"reversedWindow", func(f *diamondSadFourCallerFixture) { f.slice.sMvStartMin = SMVUnitXY{20, 20} }},
		{"invalidCount", func(f *diamondSadFourCallerFixture) { f.slice.uiMvcNum = 6 }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			f, _ := newWholeSearchFixture(BLOCK_16x16, 16, 16, 32, 1, "many")
			test.change(f)
			got := f.initial
			saved := f.slice.sMbCacheInfo.iDiamondSad
			if welsMotionEstimateIntegerFast(&f.funcs, &got, &f.slice, f.stride, f.stride) {
				t.Fatal("unexpected fast-path eligibility")
			}
			if !reflect.DeepEqual(got, f.initial) || f.slice.sMbCacheInfo.iDiamondSad != saved {
				t.Fatal("declined path changed state")
			}
		})
	}
}

func TestWelsMotionEstimateIntegerFastCallbacks(t *testing.T) {
	for _, kind := range []string{"directional", "search", "sad"} {
		t.Run(kind, func(t *testing.T) {
			f, layer := newWholeSearchFixture(BLOCK_16x16, 16, 16, 32, 1, "many")
			calls := 0
			switch kind {
			case "directional":
				f.funcs.bWholeIntegerSearch = false
				f.funcs.pfCheckDirectionalMv = func(_ PSampleSadSatdCostFunc, me *SWelsME, _, _ SMVUnitXY, _, _ int32, cost *int32) bool {
					calls++
					*cost = 123
					return false
				}
			case "search":
				f.funcs.bWholeIntegerSearch = false
				f.funcs.pfSearchMethod[BLOCK_16x16] = func(fs *SWelsFuncPtrList, me *SWelsME, s *SSlice, a, b int32) {
					calls++
					WelsDiamondSearch(fs, me, s, a, b)
				}
			case "sad":
				f.funcs.sSampleDealingFuncs.bSampleSadTwo = false
				original := f.funcs.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16]
				f.funcs.sSampleDealingFuncs.pfSampleSad[BLOCK_16x16] = func(a []byte, ao int, as int32, b []byte, bo int, bs int32) int32 {
					calls++
					return original(a, ao, as, b, bo, bs)
				}
			}
			got := f.initial
			WelsMotionEstimateSearch(&f.funcs, layer, &got, &f.slice)
			if calls == 0 {
				t.Fatal("custom callback was bypassed")
			}
		})
	}
}
