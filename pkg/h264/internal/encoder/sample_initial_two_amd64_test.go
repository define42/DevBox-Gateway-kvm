//go:build amd64

package encoder

import (
	"fmt"
	"math"
	"math/rand"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func initialTwoScalar(a []byte, aOff, aStride int, b []byte, bOff, bStride, width, height int) int32 {
	var sum int32
	for y := range height {
		for x := range width {
			d := int32(a[aOff+y*aStride+x]) - int32(b[bOff+y*bStride+x])
			if d < 0 {
				d = -d
			}
			sum += d
		}
	}
	return sum
}

func TestInitialSadTwoKernelParity(t *testing.T) {
	rng := rand.New(rand.NewSource(211))
	for _, width := range []int{8, 16} {
		for _, height := range []int{4, 8, 12, 16} {
			for _, stride := range []int{0, 3, width, 31, 2048, -31} {
				t.Run(fmt.Sprintf("%dx%d/stride%d", width, height, stride), func(t *testing.T) {
					spanStride := stride
					if spanStride < 0 {
						spanStride = -spanStride
					}
					length := (height-1)*spanStride + width + 7
					a, first, second := make([]byte, length), make([]byte, length), make([]byte, length)
					off := 3
					if stride < 0 {
						off += (height - 1) * spanStride
					}
					check := func() {
						t.Helper()
						want0 := initialTwoScalar(a, off, stride, first, off, stride, width, height)
						want1 := initialTwoScalar(a, off, stride, second, off, stride, width, height)
						got0, got1 := sadTwoSSE2(&a[off], stride, &first[off], &second[off], stride, width, height)
						if got0 != want0 || got1 != want1 {
							t.Fatalf("got=(%d,%d), want=(%d,%d)", got0, got1, want0, want1)
						}
					}
					for scenario := range 16 {
						for i := range a {
							a[i], first[i], second[i] = byte(rng.Intn(256)), byte(rng.Intn(256)), byte(rng.Intn(256))
							if scenario < 8 {
								a[i], first[i], second[i] = byte((scenario&1)*255), byte(((scenario>>1)&1)*255), byte(((scenario>>2)&1)*255)
							}
						}
						check()
					}
					clear(a)
					clear(first)
					clear(second)
					for y := range height {
						for x := range width {
							at := off + y*stride + x
							a[at], first[at], second[at] = 91, 255, 3
							check()
							a[at], first[at], second[at] = 0, 0, 0
						}
					}
					for i := range a {
						a[i] = byte(rng.Intn(256))
					}
					// All inputs may alias, including different offsets into one allocation.
					got0, got1 := sadTwoSSE2(&a[off], stride, &a[off], &a[off+1], stride, width, height)
					want := initialTwoScalar(a, off, stride, a, off+1, stride, width, height)
					if got0 != 0 || got1 != want {
						t.Fatalf("alias got=(%d,%d), want=(0,%d)", got0, got1, want)
					}
				})
			}
		}
	}
}

func TestInitialSadTwoMixedStrides(t *testing.T) {
	for _, width := range []int{8, 16} {
		for _, height := range []int{4, 8, 16} {
			for _, strides := range [][2]int{{0, 31}, {31, 0}, {3, 33}, {17, 33}, {-33, 17}, {17, -33}} {
				makeBlock := func(stride, seed int) ([]byte, int) {
					absStride, off := stride, 3
					if stride < 0 {
						absStride = -stride
						off += (height - 1) * absStride
					}
					buf := make([]byte, (height-1)*absStride+width+3)
					for i := range buf {
						buf[i] = byte(i*37 + seed)
					}
					return buf, off
				}
				a, aOff := makeBlock(strides[0], 3)
				first, refOff := makeBlock(strides[1], 17)
				second, _ := makeBlock(strides[1], 99)
				got0, got1 := sadTwoSSE2(&a[aOff], strides[0], &first[refOff], &second[refOff], strides[1], width, height)
				want0 := initialTwoScalar(a, aOff, strides[0], first, refOff, strides[1], width, height)
				want1 := initialTwoScalar(a, aOff, strides[0], second, refOff, strides[1], width, height)
				if got0 != want0 || got1 != want1 {
					t.Fatalf("%dx%d strides=%v: got=(%d,%d), want=(%d,%d)", width, height, strides, got0, got1, want0, want1)
				}
			}
		}
	}
}

type initialTwoOutcome struct {
	mv, directional SMVUnitXY
	refOff          int
	cost, satd      uint32
	directionalCost int32
	mvcCount        uint8
	early           bool
	panicText       string
	callbackState   []int64
}

func initialTwoRun(f *initialTwoFixture, enabled, direction bool) (out initialTwoOutcome) {
	f.funcs.sSampleDealingFuncs.bSampleSadTwo = enabled
	f.funcs.pfCheckDirectionalMv = func(pSad PSampleSadSatdCostFunc, me *SWelsME, min, max SMVUnitXY, encStride, refStride int32, cost *int32) bool {
		out.callbackState = append(out.callbackState, int64(me.sMv.iMvX), int64(me.sMv.iMvY), int64(me.iRefMbOff), int64(me.uiSadCost), int64(*cost), int64(encStride), int64(refStride), int64(f.slice.uiMvcNum))
		if direction {
			*cost = 2
			return true
		}
		return false
	}
	defer func() {
		if value := recover(); value != nil {
			out.panicText = fmt.Sprint(value)
		}
		out.mv, out.directional = f.me.sMv, f.me.sDirectionalMv
		out.refOff, out.cost, out.satd = f.me.iRefMbOff, f.me.uiSadCost, f.me.uiSatdCost
		out.directionalCost = f.me.iDirectionalSadCost
		out.mvcCount = f.slice.uiMvcNum
	}()
	out.early = WelsMotionEstimateInitialPoint(&f.funcs, &f.me, &f.slice, f.stride, f.stride)
	return out
}

func TestInitialSadTwoWholeSearchParity(t *testing.T) {
	for _, shape := range initialTwoShapes {
		for _, scenario := range []string{"mvcWins", "mvpWins", "tie", "duplicate", "fiveMVC", "zeroMVC", "clipped", "negativeRound", "costWins", "zeroStride", "overlapStride", "inputAlias"} {
			for _, direction := range []bool{false, true} {
				for _, early := range []uint32{0, 90000} {
					t.Run(fmt.Sprintf("%dx%d/%s/direction%v/early%d", shape[0], shape[1], scenario, direction, early), func(t *testing.T) {
						var outcomes [2]initialTwoOutcome
						for route := range outcomes {
							f := newInitialTwoFixture(shape[0], shape[1], uint8(shape[2]), 32, scenario)
							f.me.uSadPredISatd = early
							switch scenario {
							case "clipped":
								f.me.sMvp, f.slice.sMvc[0] = SMVUnitXY{-100, 100}, SMVUnitXY{100, -100}
							case "negativeRound":
								f.me.sMvp, f.slice.sMvc[0] = SMVUnitXY{-3, -6}, SMVUnitXY{-5, -7}
							case "costWins":
								clear(f.me.pRefMb)
								clear(f.me.pEncMb)
								f.me.pMvdCost[512] = 60000
							case "zeroStride":
								f.stride = 0
							case "overlapStride":
								f.stride = 3
							case "inputAlias":
								f.me.pEncMb, f.me.iEncMbOff = f.me.pRefMb, f.me.iRefMbOff+1
							}
							outcomes[route] = initialTwoRun(f, route == 1, direction)
						}
						if outcomes[0].panicText != "" || !reflect.DeepEqual(outcomes[0], outcomes[1]) {
							t.Fatalf("legacy=%+v\nbatch=%+v", outcomes[0], outcomes[1])
						}
						if scenario == "tie" && !direction && outcomes[1].mv != (SMVUnitXY{}) {
							t.Fatalf("tie replaced MVP: %+v", outcomes[1])
						}
					})
				}
			}
		}
	}
}

func TestInitialSadTwoMalformedFallback(t *testing.T) {
	for _, scenario := range []string{"shortEnc", "shortRef", "empty", "encOff", "refOff", "negativeStride", "hugeStride", "overflowOff", "mvdFirst", "mvdSecond", "tooManyMVC"} {
		t.Run(scenario, func(t *testing.T) {
			var outcomes [2]initialTwoOutcome
			for route := range outcomes {
				f := newInitialTwoFixture(16, 16, BLOCK_16x16, 32, "mvcWins")
				switch scenario {
				case "shortEnc":
					f.me.pEncMb = f.me.pEncMb[:len(f.me.pEncMb)-1]
				case "shortRef":
					f.me.pRefMb = f.me.pRefMb[:f.me.iRefMbOff+15*32+16]
				case "empty":
					f.me.pEncMb = nil
				case "encOff":
					f.me.iEncMbOff = -1
				case "refOff":
					f.me.iRefMbOff = -1
				case "negativeStride":
					f.stride = -32
				case "hugeStride":
					f.stride = math.MaxInt32
				case "overflowOff":
					f.me.iRefMbOff = math.MaxInt
					f.me.sMvp = SMVUnitXY{4, 4}
				case "mvdFirst":
					f.me.iMvdCostOff = -1
				case "mvdSecond":
					f.me.pMvdCost = f.me.pMvdCost[:513]
				case "tooManyMVC":
					f.slice.uiMvcNum = 6
				}
				outcomes[route] = initialTwoRun(f, route == 1, false)
			}
			if outcomes[0].panicText == "" || !reflect.DeepEqual(outcomes[0], outcomes[1]) {
				t.Fatalf("legacy=%+v\nbatch=%+v", outcomes[0], outcomes[1])
			}
		})
	}
}

func TestInitialSadTwoCustomCallbackOptOut(t *testing.T) {
	f := newInitialTwoFixture(8, 8, BLOCK_8x8, 32, "mvcWins")
	f.funcs.sSampleDealingFuncs.bSampleSadTwo = false
	var offsets []int
	f.funcs.sSampleDealingFuncs.pfSampleSad[BLOCK_8x8] = func(a []byte, aOff int, aStride int32, ref []byte, refOff int, refStride int32) int32 {
		offsets = append(offsets, refOff)
		return int32(30 - len(offsets)*10)
	}
	originalOff := f.me.iRefMbOff
	f.funcs.pfCheckDirectionalMv = func(pSad PSampleSadSatdCostFunc, me *SWelsME, min, max SMVUnitXY, encStride, refStride int32, cost *int32) bool {
		if len(offsets) != 2 || me.iRefMbOff != originalOff || me.uiSadCost != 98765 {
			t.Fatalf("callback observed changed order/state: offsets=%v, me=%+v", offsets, me)
		}
		return false
	}
	WelsMotionEstimateInitialPoint(&f.funcs, &f.me, &f.slice, f.stride, f.stride)
	if !reflect.DeepEqual(offsets, []int{originalOff, originalOff + 33}) || f.me.sMv != (SMVUnitXY{1, 1}) {
		t.Fatalf("custom SAD order/result: offsets=%v, mv=%v", offsets, f.me.sMv)
	}
}

func TestInitialSadTwoDispatchAndEligibility(t *testing.T) {
	var funcs SWelsFuncPtrList
	WelsInitSampleSadFunc(&funcs, common.WELS_CPU_SSE2)
	if !funcs.sSampleDealingFuncs.bSampleSadTwo {
		t.Fatal("SSE2 capability not installed")
	}
	WelsInitSampleSadFunc(&funcs, 0)
	if funcs.sSampleDealingFuncs.bSampleSadTwo {
		t.Fatal("scalar reinitialization retained capability")
	}
	for _, shape := range initialTwoShapes {
		f := newInitialTwoFixture(shape[0], shape[1], uint8(shape[2]), 32, "mvcWins")
		_, _, _, ok := meInitialSadTwo(&f.me, SMVUnitXY{}, SMVUnitXY{1, 1}, f.me.iRefMbOff, f.stride, f.stride)
		if ok != (shape[0] >= 8) {
			t.Fatalf("shape=%v eligibility=%v", shape, ok)
		}
	}
}

func TestInitialSadTwoNoAdditionalCallerAllocs(t *testing.T) {
	for _, shape := range initialTwoShapes {
		f := newInitialTwoFixture(shape[0], shape[1], uint8(shape[2]), 2048, "mvcWins")
		var allocs [2]float64
		for route := range allocs {
			f.funcs.sSampleDealingFuncs.bSampleSadTwo = route == 1
			allocs[route] = testing.AllocsPerRun(100, func() {
				me := f.me
				WelsMotionEstimateInitialPoint(&f.funcs, &me, &f.slice, f.stride, f.stride)
				sampleSatdBenchmarkSink = int32(me.uiSadCost)
			})
		}
		if allocs[1] != allocs[0] {
			t.Fatalf("shape=%v: legacy=%v allocations, batch=%v", shape, allocs[0], allocs[1])
		}
	}
}

// The duplicate fast path may omit only the MVC iteration, never the initial
// checked SAD/MVD lookup or the directional callback's view of the slice count.
func TestInitialSadTwoDuplicateControls(t *testing.T) {
	for _, shape := range initialTwoShapes {
		for _, scenario := range []string{"rounded", "clipped", "zeroStride", "shortEnc", "shortRef", "negativeStride", "mvdFirst"} {
			t.Run(fmt.Sprintf("%dx%d/%s", shape[0], shape[1], scenario), func(t *testing.T) {
				var outcomes [2]initialTwoOutcome
				for route := range outcomes {
					f := newInitialTwoFixture(shape[0], shape[1], uint8(shape[2]), 32, "duplicate")
					switch scenario {
					case "clipped":
						f.me.sMvp = SMVUnitXY{math.MaxInt16, math.MinInt16}
						f.slice.sMvc[0] = SMVUnitXY{100, -100}
						// Both predictors clip to this one point. Keep the MVD
						// table large enough for the unrounded quarter-pixel MVP.
						f.slice.sMvStartMin, f.slice.sMvStartMax = SMVUnitXY{}, SMVUnitXY{}
						f.me.pMvdCost, f.me.iMvdCostOff = make([]uint16, 65537), 32768
					case "zeroStride":
						f.stride = 0
					case "shortEnc":
						f.me.pEncMb = f.me.pEncMb[:len(f.me.pEncMb)-1]
					case "shortRef":
						f.me.pRefMb = f.me.pRefMb[:f.me.iRefMbOff+(shape[1]-1)*32+shape[0]-1]
					case "negativeStride":
						f.stride = -32
					case "mvdFirst":
						f.me.iMvdCostOff = -1
					}
					outcomes[route] = initialTwoRun(f, route == 1, false)
				}
				if !reflect.DeepEqual(outcomes[0], outcomes[1]) {
					t.Fatalf("legacy=%+v\nbatch=%+v", outcomes[0], outcomes[1])
				}
				if outcomes[1].mvcCount != 1 {
					t.Fatalf("duplicate changed slice MVC count: %+v", outcomes[1])
				}
				valid := scenario == "rounded" || scenario == "clipped" || scenario == "zeroStride"
				if valid != (outcomes[1].panicText == "") {
					t.Fatalf("unexpected duplicate failure: %+v", outcomes[1])
				}
			})
		}
	}
}

func TestInitialSadTwoCustomCallbackMutationOptOut(t *testing.T) {
	for _, shape := range initialTwoShapes {
		t.Run(fmt.Sprintf("%dx%d", shape[0], shape[1]), func(t *testing.T) {
			f := newInitialTwoFixture(shape[0], shape[1], uint8(shape[2]), 32, "duplicate")
			f.funcs.sSampleDealingFuncs.bSampleSadTwo = false
			clear(f.me.pMvdCost)
			originalOff := f.me.iRefMbOff
			originalMin, originalMax := f.slice.sMvStartMin, f.slice.sMvStartMax
			var offsets []int
			f.funcs.sSampleDealingFuncs.pfSampleSad[shape[2]] = func(a []byte, aOff int, aStride int32, ref []byte, refOff int, refStride int32) int32 {
				offsets = append(offsets, refOff)
				if len(offsets) == 1 {
					// The legacy loop snapshots count and bounds but reads MVC
					// elements after the first callback. A previously duplicate
					// MVC therefore becomes a candidate, even with count now 0.
					f.slice.sMvc[0] = SMVUnitXY{-4, 4}
					f.slice.uiMvcNum = 0
					f.slice.sMvStartMin, f.slice.sMvStartMax = SMVUnitXY{}, SMVUnitXY{}
				}
				return int32(40 - len(offsets)*10)
			}
			directionalCalls := 0
			f.funcs.pfCheckDirectionalMv = func(pSad PSampleSadSatdCostFunc, me *SWelsME, min, max SMVUnitXY, encStride, refStride int32, cost *int32) bool {
				directionalCalls++
				if !reflect.DeepEqual(offsets, []int{originalOff, originalOff + 31}) ||
					min != originalMin || max != originalMax || f.slice.uiMvcNum != 0 ||
					me.iRefMbOff != originalOff || me.uiSadCost != 98765 {
					t.Fatalf("callback order/state changed: offsets=%v, count=%d, min=%v, max=%v, me=%+v", offsets, f.slice.uiMvcNum, min, max, me)
				}
				return false
			}
			early := WelsMotionEstimateInitialPoint(&f.funcs, &f.me, &f.slice, f.stride, f.stride)
			if early || directionalCalls != 1 || f.me.sMv != (SMVUnitXY{-1, 1}) || f.me.uiSadCost != 20 {
				t.Fatalf("custom SAD result: early=%v, directionalCalls=%d, mv=%v, cost=%d", early, directionalCalls, f.me.sMv, f.me.uiSadCost)
			}
		})
	}
}
