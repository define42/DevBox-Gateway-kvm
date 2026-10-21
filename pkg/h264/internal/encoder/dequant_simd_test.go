package encoder

import (
	"math/rand"
	"slices"
	"testing"
	"unsafe"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestDequantSIMDMatchesScalar(t *testing.T) {
	rng := rand.New(rand.NewSource(914))
	for _, tc := range []struct {
		name       string
		size       int
		fn, scalar PDeQuantizationFunc
	}{
		{"4x4", 16, WelsDequant4x4_sse2, WelsDequant4x4_c},
		{"Four4x4", 64, WelsDequantFour4x4_sse2, WelsDequantFour4x4_c},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for iteration := 0; iteration < 1024; iteration++ {
				offset := iteration % 8
				got := make([]int16, tc.size+offset+3)
				for i := range got {
					got[i] = int16(rng.Uint32())
				}
				mf := make([]uint16, 8)
				for i := range mf {
					mf[i] = uint16(rng.Uint32())
				}
				if iteration < len(common.G_kuiDequantCoeff) {
					copy(mf, common.G_kuiDequantCoeff[iteration][:])
				}
				want := slices.Clone(got)
				tc.scalar(want[offset:], mf)
				tc.fn(got[offset:], mf)
				if !slices.Equal(got, want) {
					t.Fatalf("iteration=%d coefficients differ", iteration)
				}
			}
			// Preserve sequential scalar behavior even when an unsafe caller
			// makes differently typed coefficient and multiplier slices overlap.
			for offset := 0; offset < tc.size; offset++ {
				got := make([]int16, tc.size+8)
				for i := range got {
					got[i] = int16(i*701 - 30000)
				}
				want := slices.Clone(got)
				gotMF := unsafe.Slice((*uint16)(unsafe.Pointer(&got[offset])), 8)
				wantMF := unsafe.Slice((*uint16)(unsafe.Pointer(&want[offset])), 8)
				tc.scalar(want, wantMF)
				tc.fn(got, gotMF)
				if !slices.Equal(got, want) {
					t.Fatalf("alias offset=%d differs", offset)
				}
			}
		})
	}
}

func BenchmarkDequantFour4x4(b *testing.B) {
	for _, tc := range []struct {
		name string
		fn   PDeQuantizationFunc
	}{{"Scalar", WelsDequantFour4x4_c}, {"SIMD", WelsDequantFour4x4_sse2}} {
		b.Run(tc.name, func(b *testing.B) {
			var coefficients [64]int16
			for i := range coefficients {
				coefficients[i] = int16(i*11 + 1)
			}
			for b.Loop() {
				tc.fn(coefficients[:], common.G_kuiDequantCoeff[20][:])
			}
		})
	}
}
