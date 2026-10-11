//go:build amd64

package encoder

import (
	"math/rand"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func quantInputs(seed int64, n int) ([]int16, []int16, []int16) {
	r := rand.New(rand.NewSource(seed))
	dct := make([]int16, n)
	for i := range dct {
		dct[i] = int16(r.Uint32())
	}
	ff := make([]int16, 8)
	mf := make([]int16, 8)
	for i := range ff {
		ff[i] = int16(r.Intn(1 << 15))
		mf[i] = int16(r.Intn(1 << 15))
	}
	return dct, ff, mf
}

func TestQuantSIMDMatchesScalar(t *testing.T) {
	tests := []struct {
		name   string
		length int
		scalar PQuantizationFunc
		simd   PQuantizationFunc
	}{
		{"4x4", 16, WelsQuant4x4_c, WelsQuant4x4_sse2},
		{"Four4x4", 64, WelsQuantFour4x4_c, WelsQuantFour4x4_sse2},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for seed := int64(0); seed < 2000; seed++ {
				dct, ff, mf := quantInputs(seed, test.length)
				want := slices.Clone(dct)
				got := slices.Clone(dct)
				test.scalar(want, ff, mf)
				test.simd(got, ff, mf)
				if !slices.Equal(got, want) {
					t.Fatalf("seed %d mismatch at coefficient %d: got %v want %v", seed, firstInt16Diff(got, want), got, want)
				}
			}
		})
	}
}

func TestQuant4x4DcSIMDMatchesScalar(t *testing.T) {
	edges := []int16{0, 1, 2, 16383, 16384, 32766, 32767}
	extremes := []int16{-32768, -32767, -1, 0, 1, 32766, 32767}

	for _, ff := range edges {
		for _, mf := range edges {
			dct := make([]int16, 16)
			for i := range dct {
				dct[i] = extremes[i%len(extremes)]
			}
			want := slices.Clone(dct)
			got := slices.Clone(dct)
			WelsQuant4x4Dc_c(want, ff, mf)
			WelsQuant4x4Dc_sse2(got, ff, mf)
			if !slices.Equal(got, want) {
				t.Fatalf("edge ff=%d mf=%d coefficient %d mismatch: got %v want %v", ff, mf, firstInt16Diff(got, want), got, want)
			}
		}
	}

	r := rand.New(rand.NewSource(8181))
	for iteration := 0; iteration < 4000; iteration++ {
		dct := make([]int16, 16)
		for i := range dct {
			dct[i] = int16(r.Uint32())
		}
		ff := int16(r.Intn(1 << 15))
		mf := int16(r.Intn(1 << 15))
		want := slices.Clone(dct)
		got := slices.Clone(dct)
		WelsQuant4x4Dc_c(want, ff, mf)
		WelsQuant4x4Dc_sse2(got, ff, mf)
		if !slices.Equal(got, want) {
			t.Fatalf("iteration %d ff=%d mf=%d coefficient %d mismatch: got %v want %v", iteration, ff, mf, firstInt16Diff(got, want), got, want)
		}
	}
}

func TestQuantFour4x4MaxSIMDMatchesScalar(t *testing.T) {
	extremes := []int16{-32768, -32767, -1, 0, 1, 32766, 32767}
	for seed := int64(0); seed < 4000; seed++ {
		dct, ff, mf := quantInputs(seed, 64)
		if seed == 0 {
			for i := range dct {
				dct[i] = extremes[i%len(extremes)]
			}
			ff = []int16{0, 1, 2, 16383, 16384, 32766, 32767, 7}
			mf = []int16{0, 1, 2, 16383, 16384, 32766, 32767, 11}
		}

		wantDCT := slices.Clone(dct)
		gotDCT := slices.Clone(dct)
		wantMax := []int16{-1, -1, -1, -1, 1234, 1234}
		gotMax := slices.Clone(wantMax)
		WelsQuantFour4x4Max_c(wantDCT, ff, mf, wantMax)
		WelsQuantFour4x4Max_sse2(gotDCT, ff, mf, gotMax)

		if !slices.Equal(gotDCT, wantDCT) {
			t.Fatalf("seed %d coefficient %d mismatch: got %v want %v", seed, firstInt16Diff(gotDCT, wantDCT), gotDCT, wantDCT)
		}
		if !slices.Equal(gotMax, wantMax) {
			t.Fatalf("seed %d maxima mismatch: got %v want %v", seed, gotMax, wantMax)
		}
	}
}

func TestInitQuantizationSIMD(t *testing.T) {
	var funcs SWelsFuncPtrList
	WelsInitEncodingFuncs(&funcs, 0)
	initQuantizationSIMD(&funcs, common.WELS_CPU_SSE2)

	dct, ff, mf := quantInputs(99, 64)
	wantDCT := slices.Clone(dct)
	gotDCT := slices.Clone(dct)
	wantMax := make([]int16, 4)
	gotMax := make([]int16, 4)
	WelsQuantFour4x4Max_c(wantDCT, ff, mf, wantMax)
	funcs.pfQuantizationFour4x4Max(gotDCT, ff, mf, gotMax)
	if !slices.Equal(gotDCT, wantDCT) || !slices.Equal(gotMax, wantMax) {
		t.Fatalf("dispatched SIMD quantizer differs from scalar")
	}

	dcInput, _, _ := quantInputs(100, 16)
	wantDC := slices.Clone(dcInput)
	gotDC := slices.Clone(dcInput)
	WelsQuant4x4Dc_c(wantDC, 32767, 32767)
	funcs.pfQuantizationDc4x4(gotDC, 32767, 32767)
	if !slices.Equal(gotDC, wantDC) {
		t.Fatalf("dispatched SIMD DC quantizer differs from scalar")
	}
}

func firstInt16Diff(a, b []int16) int {
	for i := range a {
		if a[i] != b[i] {
			return i
		}
	}
	return -1
}

func BenchmarkQuantFour4x4Max(b *testing.B) {
	input, ff, mf := quantInputs(1234, 64)
	for _, bench := range []struct {
		name string
		fn   PQuantizationMaxFunc
	}{
		{"Scalar", WelsQuantFour4x4Max_c},
		{"SSE2", WelsQuantFour4x4Max_sse2},
	} {
		b.Run(bench.name, func(b *testing.B) {
			dct := make([]int16, 64)
			max := make([]int16, 4)
			b.ReportAllocs()
			b.SetBytes(int64(len(dct) * 2))
			for i := 0; i < b.N; i++ {
				copy(dct, input)
				bench.fn(dct, ff, mf, max)
			}
		})
	}
}
