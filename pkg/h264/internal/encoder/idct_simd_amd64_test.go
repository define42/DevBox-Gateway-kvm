//go:build amd64

package encoder

import (
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestIDctSIMDDispatchAndBounds(t *testing.T) {
	var funcs SWelsFuncPtrList
	WelsInitReconstructionFuncs(&funcs, common.WELS_CPU_SSE2)
	for _, tc := range []struct {
		name               string
		size, coeffs       int
		selected, expected PIDctFunc
	}{{"4x4", 4, 16, funcs.pfIDctT4, WelsIDctT4Rec_sse2}, {"8x8", 8, 64, funcs.pfIDctFourT4, WelsIDctFourT4Rec_sse2}, {"16x16-DC", 16, 16, funcs.pfIDctI16x16Dc, WelsIDctRecI16x16Dc_sse2}} {
		t.Run(tc.name, func(t *testing.T) {
			if reflect.ValueOf(tc.selected).Pointer() != reflect.ValueOf(tc.expected).Pointer() {
				t.Fatal("SIMD dispatch not selected")
			}
			for _, field := range []string{"output", "prediction", "coefficients", "negative-offset", "overflowing-offset"} {
				t.Run(field, func(t *testing.T) {
					buf := make([]byte, tc.size*tc.size+32)
					rec, pred := buf, append([]byte(nil), buf...)
					coeff := make([]int16, tc.coeffs+32)
					off := 0
					switch field {
					case "output":
						rec = rec[:tc.size*tc.size-1]
					case "prediction":
						pred = pred[:tc.size*tc.size-1]
					case "coefficients":
						coeff = coeff[:tc.coeffs-1]
					case "negative-offset":
						off = -1
					case "overflowing-offset":
						off = int(^uint(0) >> 1)
					}
					defer func() {
						if recover() == nil {
							t.Fatal("invalid span did not panic before assembly")
						}
					}()
					tc.selected(rec, off, int32(tc.size), pred, 0, int32(tc.size), coeff)
				})
			}
		})
	}
}
