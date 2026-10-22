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
	funcs.bDiamondSearch = true
	rng := rand.New(rand.NewPCG(4, 4))
	const stride, rows = 96, 96
	costs := make([]uint16, 2048)
	for i := range costs {
		costs[i] = uint16(rng.IntN(64))
	}
	var fast, slow int
	for test := 0; test < 20000; test++ {
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
