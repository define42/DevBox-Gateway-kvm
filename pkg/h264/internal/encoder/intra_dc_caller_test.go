package encoder

import (
	"bytes"
	"fmt"
	"testing"
)

func sameIntraDCFixture(got, want *intraDCFixture) bool {
	g, w := got.base, want.base
	return g.mb.uiCbp == w.mb.uiCbp && g.nonzero == w.nonzero && g.coefficients == w.coefficients && g.dct == w.dct &&
		got.prediction == want.prediction && bytes.Equal(g.source, w.source) && bytes.Equal(g.reconstructed, w.reconstructed)
}

func TestIntraDCCallerParity(t *testing.T) {
	for _, strides := range [][2]int32{{16, 16}, {23, 29}, {1920, 1984}} {
		for _, content := range []string{"Zero", "DC", "Sparse", "Dense"} {
			t.Run(fmt.Sprintf("%s/Stride%d_%d", content, strides[0], strides[1]), func(t *testing.T) {
				for qp := uint8(0); qp <= 51; qp++ {
					got := newIntraDCFixture(strides[0], strides[1], qp, content)
					want := newIntraDCFixture(strides[0], strides[1], qp, content)
					WelsEncRecI16x16Y(&got.base.ctx, &got.base.mb, &got.base.cache)
					welsEncRecI16x16ScalarDCReference(&want.base.ctx, &want.base.mb, &want.base.cache)
					if !sameIntraDCFixture(got, want) {
						t.Fatalf("QP%d: complete reconstruction state differs", qp)
					}
					if content == "Zero" && got.base.mb.uiCbp != 0x30 {
						t.Fatalf("QP%d: zero residual unexpectedly set luma CBP", qp)
					}
					if content == "DC" {
						if got.base.mb.uiCbp != 0x30 || got.base.dct.iLumaI16x16Dc == [16]int16{} {
							t.Fatalf("QP%d: DC-only fixture did not exercise inverse DC reconstruction", qp)
						}
					}
					if content == "Dense" && got.base.mb.uiCbp&15 == 0 {
						t.Fatalf("QP%d: dense fixture did not exercise AC reconstruction", qp)
					}
				}
			})
		}
	}
}

func TestIntraDCCallerCustomTransformAndFailures(t *testing.T) {
	for _, length := range []int{0, 63, 64, 127, 128, 191, 192, 240, 241, 255, 256, 384} {
		for _, qp := range []uint8{0, 11, 12, 20, 51} {
			got := newIntraDCFixture(23, 29, qp, "Dense")
			want := newIntraDCFixture(23, 29, qp, "Dense")
			var calls [2]int
			for route, f := range []*intraDCFixture{got, want} {
				f.base.cache.pCoeffLevel = f.base.coefficients[:length]
				f.base.functions.pfDctFourT4 = func(res []int16, a []uint8, aOff int, aStride int32, b []uint8, bOff int, bStride int32) {
					calls[route]++
					for i := 0; i < len(res) && i < 64; i++ {
						res[i] = int16((i+calls[route]*17)*1701 - 32768)
					}
				}
				// These callbacks were bypassed by the old direct scalar calls.
				// Installing optimized direct calls must not newly consult them.
				f.base.functions.pfTransformHadamard4x4Dc = func([]int16, []int16) { panic("unexpected Hadamard callback") }
				f.base.functions.pfDequantizationIHadamard4x4 = func([]int16, uint16) { panic("unexpected inverse callback") }
			}
			gotPanic := intraDCPanic(func() { WelsEncRecI16x16Y(&got.base.ctx, &got.base.mb, &got.base.cache) })
			wantPanic := intraDCPanic(func() { welsEncRecI16x16ScalarDCReference(&want.base.ctx, &want.base.mb, &want.base.cache) })
			if gotPanic != wantPanic || calls[0] != calls[1] || !sameIntraDCFixture(got, want) || (length >= 256 && gotPanic != "") {
				t.Fatalf("length%d QP%d: panic%q/%q calls%v or partial state differs", length, qp, gotPanic, wantPanic, calls)
			}
		}
	}
}

func TestIntraDCCallerNoAllocs(t *testing.T) {
	for _, content := range []string{"Zero", "DC", "Sparse", "Dense"} {
		f := newIntraDCFixture(1920, 1984, 20, content)
		if allocations := testing.AllocsPerRun(100, func() {
			f.base.mb.uiCbp = 0x30
			WelsEncRecI16x16Y(&f.base.ctx, &f.base.mb, &f.base.cache)
		}); allocations != 0 {
			t.Fatalf("%s: %.2f allocations, want zero", content, allocations)
		}
	}
}
