package encoder

import (
	"bytes"
	"math/rand"
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestIDctSIMDParity(t *testing.T) {
	var funcs SWelsFuncPtrList
	WelsInitReconstructionFuncs(&funcs, common.WELS_CPU_SSE2)
	for _, tc := range []struct {
		name             string
		size             int
		coeffs           int
		scalar, selected PIDctFunc
	}{{"4x4", 4, 16, WelsIDctT4Rec_c, funcs.pfIDctT4}, {"8x8", 8, 64, WelsIDctFourT4Rec_c, funcs.pfIDctFourT4}, {"16x16-DC", 16, 16, WelsIDctRecI16x16Dc_c, funcs.pfIDctI16x16Dc}} {
		t.Run(tc.name, func(t *testing.T) {
			rng := rand.New(rand.NewSource(921))
			for run := 0; run < 1200; run++ {
				coeff := make([]int16, tc.coeffs)
				for i := range coeff {
					switch run % 4 {
					case 0:
						coeff[i] = int16(rng.Uint32())
					case 1:
						coeff[i] = []int16{-32768, 32767, -1, 0, 1}[rng.Intn(5)]
					case 2:
						coeff[i] = int16(rng.Intn(4097) - 2048)
					}
				}
				coeffBefore := append([]int16(nil), coeff...)
				stride := tc.size + run%11
				off := 1 + run%17
				pred := make([]byte, off+(tc.size-1)*stride+tc.size+19)
				rng.Read(pred)
				predBefore := append([]byte(nil), pred...)
				want := append([]byte(nil), pred...)
				got := append([]byte(nil), pred...)
				if run%2 == 0 {
					tc.scalar(want, off, int32(stride), want, off, int32(stride), coeff)
					tc.selected(got, off, int32(stride), got, off, int32(stride), coeff)
				} else {
					tc.scalar(want, off, int32(stride), pred, off, int32(stride), coeff)
					tc.selected(got, off, int32(stride), pred, off, int32(stride), coeff)
				}
				if !bytes.Equal(got, want) || !bytes.Equal(pred, predBefore) || !reflect.DeepEqual(coeff, coeffBefore) {
					t.Fatalf("run %d: got=%v want=%v coeff=%v", run, got, want, coeff)
				}
			}
		})
	}
}

func TestIDctSIMDAliasesAndStrides(t *testing.T) {
	for _, tc := range []struct {
		size, coeffs int
		scalar, simd PIDctFunc
	}{
		{4, 16, WelsIDctT4Rec_c, WelsIDctT4Rec_sse2},
		{8, 64, WelsIDctFourT4Rec_c, WelsIDctFourT4Rec_sse2},
		{16, 16, WelsIDctRecI16x16Dc_c, WelsIDctRecI16x16Dc_sse2},
	} {
		for _, stride := range []int32{0, 2, 31, -31} {
			for _, delta := range []int{-1, 0, 1, 32, 900} {
				got := make([]byte, 2048)
				for i := range got {
					got[i] = byte(i*37 + 11)
				}
				want := bytes.Clone(got)
				coeff := make([]int16, tc.coeffs)
				for i := range coeff {
					coeff[i] = int16(i*323 - 2000)
				}
				tc.scalar(want, 600+delta, stride, want, 600, stride, coeff)
				tc.simd(got, 600+delta, stride, got, 600, stride, coeff)
				if !bytes.Equal(got, want) {
					t.Fatalf("size=%d stride=%d delta=%d", tc.size, stride, delta)
				}
			}
		}
	}
}
