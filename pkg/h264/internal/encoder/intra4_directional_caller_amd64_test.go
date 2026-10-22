//go:build amd64

package encoder

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func scalarIntra4Directions(f *intra4SadCallerFixture) {
	for _, tc := range intra4DirectionalCases {
		f.functions.pfGetLumaI4x4Pred[tc.mode] = tc.scalar
	}
}

func compareIntra4DirectionalCaller(t *testing.T, got, want *intra4SadCallerFixture, limit int32) {
	t.Helper()
	got.reset(limit)
	want.reset(limit)
	var gr, wr int32
	gp := intra4DirectionalPanic(func() { gr = WelsMdI4x4Fast(&got.ctx, &got.md, &got.mb, &got.cache) })
	wp := intra4DirectionalPanic(func() { wr = WelsMdI4x4Fast(&want.ctx, &want.md, &want.mb, &want.cache) })
	if gp != wp || gr != wr || !reflect.DeepEqual(got.mb, want.mb) || !reflect.DeepEqual(got.md, want.md) ||
		!reflect.DeepEqual(got.cache, want.cache) || !reflect.DeepEqual(got.source, want.source) ||
		!reflect.DeepEqual(got.reconstructed, want.reconstructed) || got.coefficients != want.coefficients ||
		got.dct != want.dct || got.nonzero != want.nonzero || got.modes != want.modes ||
		got.previous != want.previous || got.remaining != want.remaining || got.scratch != want.scratch {
		t.Fatalf("complete caller result/panic/state differs: result%d/%d panic%q/%q", gr, wr, gp, wp)
	}
}

func TestIntra4DirectionalCallerParity(t *testing.T) {
	for _, strides := range [][2]int32{{32, 32}, {1920, 1984}} {
		for _, qp := range []uint8{0, 20, 32, 51} {
			for neighbors := uint8(0); neighbors < 16; neighbors++ {
				for _, content := range []string{"Flat", "Horizontal", "Vertical", "Smooth", "Noise", "Extreme"} {
					for _, predicted := range []int8{-1, 0, 2, 5, 6, 8} {
						t.Run(fmt.Sprintf("%d_%d/QP%d/n%d/%s/p%d", strides[0], strides[1], qp, neighbors, content, predicted), func(t *testing.T) {
							for _, limit := range []int32{0, 1000, math.MaxInt32} {
								got := newIntra4SadCallerFixture(strides[0], strides[1], qp, content, neighbors, predicted)
								want := newIntra4SadCallerFixture(strides[0], strides[1], qp, content, neighbors, predicted)
								scalarIntra4Directions(want)
								compareIntra4DirectionalCaller(t, got, want, limit)
							}
						})
					}
				}
			}
		}
	}
}

// Dispatch changes no caller logic. Trace the actual predictor sequence and
// exercise callback replacement during the first predictor and transform.
func TestIntra4DirectionalCallerCallbacks(t *testing.T) {
	for _, mutation := range []string{"none", "replace-from-DC", "replace-from-DCT", "custom-direction"} {
		got := newIntra4SadCallerFixture(32, 32, 20, "Noise", 15, 2)
		want := newIntra4SadCallerFixture(32, 32, 20, "Noise", 15, 2)
		scalarIntra4Directions(want)
		var traces [2][]string
		for route, f := range []*intra4SadCallerFixture{got, want} {
			for mode, original := range f.functions.pfGetLumaI4x4Pred {
				f.functions.pfGetLumaI4x4Pred[mode] = func(dst []byte, off int, ref []byte, ro int, s int32) {
					traces[route] = append(traces[route], fmt.Sprintf("mode%d dst%d ref%d stride%d", mode, off, ro, s))
					if mutation == "replace-from-DC" && mode == common.I4_PRED_DC {
						f.functions.pfGetLumaI4x4Pred[common.I4_PRED_HD] = WelsI4x4LumaPredHD_c
					}
					if mutation == "custom-direction" && mode == common.I4_PRED_HU {
						WelsI4x4LumaPredHU_c(dst, off, ref, ro, s)
						dst[off] ^= 1
						return
					}
					original(dst, off, ref, ro, s)
				}
			}
			if mutation == "replace-from-DCT" {
				original := f.functions.pfDctT4
				f.functions.pfDctT4 = func(d []int16, a []byte, ao int, as int32, b []byte, bo int, bs int32) {
					f.functions.pfGetLumaI4x4Pred[common.I4_PRED_VR] = WelsI4x4LumaPredVR_c
					original(d, a, ao, as, b, bo, bs)
				}
			}
		}
		compareIntra4DirectionalCaller(t, got, want, math.MaxInt32)
		if !reflect.DeepEqual(traces[0], traces[1]) {
			t.Fatalf("%s callback sequence differs", mutation)
		}
	}
}

func TestIntra4DirectionalCallerAliasesAndFailures(t *testing.T) {
	for _, scenario := range []string{"scratch-ref", "scratch-source", "short-scratch", "short-reference", "short-source", "negative-stride", "zero-stride"} {
		t.Run(scenario, func(t *testing.T) {
			got := newIntra4SadCallerFixture(32, 32, 20, "Noise", 15, 2)
			want := newIntra4SadCallerFixture(32, 32, 20, "Noise", 15, 2)
			scalarIntra4Directions(want)
			for _, f := range []*intra4SadCallerFixture{got, want} {
				switch scenario {
				case "scratch-ref":
					f.cache.pMemPredBlk4 = f.reconstructed[28:60]
				case "scratch-source":
					f.cache.pMemPredBlk4 = f.source[3:35]
				case "short-scratch":
					f.cache.pMemPredBlk4 = f.scratch[:15:15]
				case "short-reference":
					f.cache.SPicData.pCsMb[0] = f.reconstructed[:39:39]
				case "short-source":
					f.cache.SPicData.pEncMb[0] = f.source[:9:9]
				case "negative-stride":
					f.layer.iCsStride[0] = -1
				case "zero-stride":
					f.layer.iCsStride[0] = 0
				}
			}
			compareIntra4DirectionalCaller(t, got, want, math.MaxInt32)
		})
	}
}

func TestIntra4DirectionalCallerVisitsAndAllocs(t *testing.T) {
	var aggregate [3]int
	for _, content := range []string{"Flat", "Smooth", "Noise", "Horizontal", "Vertical", "Extreme"} {
		for _, neighbors := range []uint8{0, 2, 7, 15} {
			f := newIntra4SadCallerFixture(32, 32, 20, content, neighbors, 2)
			var calls [3]int
			for i, tc := range intra4DirectionalCases {
				original := f.functions.pfGetLumaI4x4Pred[tc.mode]
				f.functions.pfGetLumaI4x4Pred[tc.mode] = func(d []byte, o int, r []byte, ro int, s int32) { calls[i]++; original(d, o, r, ro, s) }
			}
			WelsMdI4x4Fast(&f.ctx, &f.md, &f.mb, &f.cache)
			t.Logf("%s neighbor%d HD/HU/VR calls=%v", content, neighbors, calls)
			for i, n := range calls {
				aggregate[i] += n
			}
		}
	}
	for i, n := range aggregate {
		if n == 0 {
			t.Fatalf("mode%d was never exercised", intra4DirectionalCases[i].mode)
		}
	}
	f := newIntra4SadCallerFixture(1920, 1984, 20, "Noise", 15, 2)
	if n := testing.AllocsPerRun(50, func() {
		f.reset(math.MaxInt32)
		intra4SadBenchmarkSink = WelsMdI4x4Fast(&f.ctx, &f.md, &f.mb, &f.cache)
	}); n != 0 {
		t.Fatalf("whole caller allocates %g times", n)
	}
}
