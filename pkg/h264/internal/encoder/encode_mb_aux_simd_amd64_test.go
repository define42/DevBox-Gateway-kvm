//go:build amd64

package encoder

import (
	"reflect"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestEncodingSIMDDispatch(t *testing.T) {
	pointer := func(fn any) uintptr { return reflect.ValueOf(fn).Pointer() }
	var funcs SWelsFuncPtrList

	WelsInitEncodingFuncs(&funcs, 0)
	if got, want := pointer(funcs.pfDctT4), pointer(WelsDctT4_c); got != want {
		t.Fatalf("scalar DCT dispatch = %#x, want %#x", got, want)
	}
	if got, want := pointer(funcs.pfQuantizationFour4x4Max), pointer(WelsQuantFour4x4Max_c); got != want {
		t.Fatalf("scalar quant dispatch = %#x, want %#x", got, want)
	}

	WelsInitEncodingFuncs(&funcs, common.WELS_CPU_SSE2)
	checks := []struct {
		name string
		got  any
		want any
	}{
		{"DctT4", funcs.pfDctT4, WelsDctT4_sse2},
		{"DctFourT4", funcs.pfDctFourT4, WelsDctFourT4_sse2},
		{"Quant4x4", funcs.pfQuantization4x4, WelsQuant4x4_sse2},
		{"Quant4x4Dc", funcs.pfQuantizationDc4x4, WelsQuant4x4Dc_sse2},
		{"QuantFour4x4", funcs.pfQuantizationFour4x4, WelsQuantFour4x4_sse2},
		{"QuantFour4x4Max", funcs.pfQuantizationFour4x4Max, WelsQuantFour4x4Max_sse2},
	}
	for _, check := range checks {
		if got, want := pointer(check.got), pointer(check.want); got != want {
			t.Errorf("SSE2 %s dispatch = %#x, want %#x", check.name, got, want)
		}
	}
}
