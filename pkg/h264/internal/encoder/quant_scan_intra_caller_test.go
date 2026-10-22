package encoder

import (
	"bytes"
	"fmt"
	"reflect"
	"testing"
	"unsafe"
)

func sameIntraACState(got, want *intraDCFixture) bool {
	g, w := got.base, want.base
	return g.mb.uiCbp == w.mb.uiCbp && g.nonzero == w.nonzero && g.coefficients == w.coefficients && g.dct == w.dct &&
		got.prediction == want.prediction && bytes.Equal(g.source, w.source) && bytes.Equal(g.reconstructed, w.reconstructed) &&
		reflect.DeepEqual(g.mb.pNonZeroCount, w.mb.pNonZeroCount)
}

func intraACCallPanic(call func()) (message string) {
	defer func() {
		if value := recover(); value != nil {
			message = fmt.Sprintf("%T: %v", value, value)
		}
	}()
	call()
	return
}

func TestIntraACCompleteCallerParity(t *testing.T) {
	for _, strides := range [][2]int32{{16, 16}, {23, 29}, {1920, 1984}} {
		for _, content := range []string{"Zero", "DC", "Sparse", "Dense"} {
			t.Run(fmt.Sprintf("%s/Stride%d_%d", content, strides[0], strides[1]), func(t *testing.T) {
				for qp := uint8(0); qp <= 51; qp++ {
					got, want := newIntraDCFixture(strides[0], strides[1], qp, content), newIntraDCFixture(strides[0], strides[1], qp, content)
					WelsEncRecI16x16Y(&got.base.ctx, &got.base.mb, &got.base.cache)
					intraACOriginalCaller(&want.base.ctx, &want.base.mb, &want.base.cache)
					if !sameIntraACState(got, want) {
						t.Fatalf("QP%d: full coefficient/count/reconstruction state differs", qp)
					}
				}
			})
		}
	}
}

func TestIntraACCustomCallbacksAndAliases(t *testing.T) {
	for _, scenario := range []string{"customQuant", "customAfterDCT", "builtinAfterDCT", "nilAfterDCT", "quantChangesNext", "quantPartialPanic", "scalarQuant", "fullRangeDCT", "snapshotFunctions", "snapshotResidual", "sameOutput", "futureOutput", "aliasedCounts", "shortCounts0", "shortCounts1", "shortCounts7", "shortCounts15"} {
		for _, qp := range []uint8{0, 11, 12, 26, 51} {
			t.Run(fmt.Sprintf("%s/QP%d", scenario, qp), func(t *testing.T) {
				got, want := newIntraDCFixture(23, 29, qp, "Dense"), newIntraDCFixture(23, 29, qp, "Dense")
				var events [2][]string
				for route, f := range []*intraDCFixture{got, want} {
					g := f.base
					quantCalls := 0
					customQuant := func(res, ff, mf []int16) {
						quantCalls++
						events[route] = append(events[route], fmt.Sprintf("Q%d", quantCalls))
						WelsQuantFour4x4_sse2(res, ff, mf)
						res[1] ^= 123
						if scenario == "quantPartialPanic" {
							panic("quantization callback")
						}
						if scenario == "quantChangesNext" && quantCalls == 1 {
							g.functions.pfQuantizationFour4x4 = func(res, ff, mf []int16) {
								events[route] = append(events[route], "replacementQ")
								WelsQuantFour4x4_sse2(res, ff, mf)
								res[7] ^= 77
							}
						}
					}
					switch scenario {
					case "customQuant", "quantChangesNext", "quantPartialPanic", "snapshotFunctions":
						g.functions.pfQuantizationFour4x4 = customQuant
					case "builtinAfterDCT":
						g.functions.pfQuantizationFour4x4 = func([]int16, []int16, []int16) { panic("stale quantizer") }
					case "scalarQuant":
						g.functions.pfQuantizationFour4x4 = WelsQuantFour4x4_c
					case "sameOutput":
						g.cache.pCoeffLevel = g.dct.iLumaBlock[:]
					case "futureOutput":
						g.cache.pCoeffLevel = unsafe.Slice((*int16)(unsafe.Pointer(&g.dct)), 408)[16:272]
					case "aliasedCounts":
						g.mb.pNonZeroCount = unsafe.Slice((*int8)(unsafe.Pointer(&g.coefficients[0])), 24)
					case "shortCounts0":
						g.mb.pNonZeroCount = g.nonzero[:0]
					case "shortCounts1":
						g.mb.pNonZeroCount = g.nonzero[:1]
					case "shortCounts7":
						g.mb.pNonZeroCount = g.nonzero[:7]
					case "shortCounts15":
						g.mb.pNonZeroCount = g.nonzero[:15]
					}
					dctCalls := 0
					dct := g.functions.pfDctFourT4
					g.functions.pfDctFourT4 = func(res []int16, a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) {
						dctCalls++
						events[route] = append(events[route], fmt.Sprintf("D%d", dctCalls))
						if scenario == "fullRangeDCT" {
							for i := range 64 {
								res[i] = int16((i+dctCalls*17)*1701 - 32768)
							}
						} else {
							dct(res, a, aOff, aStride, b, bOff, bStride)
						}
						if dctCalls == 4 {
							switch scenario {
							case "customAfterDCT":
								g.functions.pfQuantizationFour4x4 = customQuant
							case "builtinAfterDCT":
								g.functions.pfQuantizationFour4x4 = WelsQuantFour4x4_sse2
							case "nilAfterDCT":
								g.functions.pfQuantizationFour4x4 = nil
							case "snapshotFunctions":
								g.ctx.pFuncList = &SWelsFuncPtrList{}
							case "snapshotResidual":
								g.cache.pCoeffLevel = nil
							}
						}
					}
					// These custom scan callbacks were already bypassed by the
					// separate coefficientScanMask calls and must remain unused.
					g.functions.pfScan4x4Ac = func([]int16, []int16) { panic("unexpected scan callback") }
				}
				gp := intraACCallPanic(func() { WelsEncRecI16x16Y(&got.base.ctx, &got.base.mb, &got.base.cache) })
				wp := intraACCallPanic(func() { intraACOriginalCaller(&want.base.ctx, &want.base.mb, &want.base.cache) })
				if gp != wp || !reflect.DeepEqual(events[0], events[1]) || !sameIntraACState(got, want) {
					t.Fatalf("panic%q/%q events%v/%v or full state differs", gp, wp, events[0], events[1])
				}
				if scenario == "customAfterDCT" || scenario == "customQuant" || scenario == "snapshotFunctions" {
					if !reflect.DeepEqual(events[0], []string{"D1", "D2", "D3", "D4", "Q1", "Q2", "Q3", "Q4"}) {
						t.Fatalf("callback capture/order differs: %v", events[0])
					}
				}
				if scenario == "quantChangesNext" && !reflect.DeepEqual(events[0], []string{"D1", "D2", "D3", "D4", "Q1", "replacementQ", "replacementQ", "replacementQ"}) {
					t.Fatalf("callback replacement was not observed: %v", events[0])
				}
				wantPanic := scenario == "nilAfterDCT" || scenario == "quantPartialPanic" || scenario == "shortCounts0" || scenario == "shortCounts1" || scenario == "shortCounts7" || scenario == "shortCounts15"
				if (gp != "") != wantPanic {
					t.Fatalf("fixture panic=%q, wantPanic=%v", gp, wantPanic)
				}
			})
		}
	}
}

func TestIntraACShortResidualPartialState(t *testing.T) {
	for _, length := range []int{0, 63, 64, 127, 128, 191, 192, 240, 241, 255, 256, 384} {
		t.Run(fmt.Sprint(length), func(t *testing.T) {
			got, want := newIntraDCFixture(23, 29, 26, "Dense"), newIntraDCFixture(23, 29, 26, "Dense")
			var calls [2]int
			for route, f := range []*intraDCFixture{got, want} {
				f.base.cache.pCoeffLevel = f.base.coefficients[:length]
				f.base.functions.pfDctFourT4 = func(res []int16, a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) {
					calls[route]++
					for i := range min(len(res), 64) {
						res[i] = int16((i+calls[route]*17)*1701 - 32768)
					}
				}
			}
			gp := intraACCallPanic(func() { WelsEncRecI16x16Y(&got.base.ctx, &got.base.mb, &got.base.cache) })
			wp := intraACCallPanic(func() { intraACOriginalCaller(&want.base.ctx, &want.base.mb, &want.base.cache) })
			if gp != wp || calls[0] != calls[1] || !sameIntraACState(got, want) || (gp == "") != (length >= 256) {
				t.Fatalf("len%d panic%q/%q calls%v or partial state differs", length, gp, wp, calls)
			}
		})
	}
}

func TestIntraACCompleteCallerNoAllocs(t *testing.T) {
	for _, content := range []string{"Zero", "DC", "Sparse", "Dense"} {
		f := newIntraDCFixture(1920, 1984, 20, content)
		if n := testing.AllocsPerRun(100, func() { f.base.mb.uiCbp = 0x30; WelsEncRecI16x16Y(&f.base.ctx, &f.base.mb, &f.base.cache) }); n != 0 {
			t.Fatalf("%s: %.2f allocations, want zero", content, n)
		}
	}
}
