//go:build amd64

package processing

import (
	"math/rand"
	"reflect"
	"slices"
	"testing"

	"github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"
)

func TestVAACalcSadSSE2MatchesC(t *testing.T) {
	tests := []struct {
		name                  string
		width, height, stride int32
		curOff, refOff        int
		pattern               string
	}{
		{name: "single/random/aligned", width: 16, height: 16, stride: 16, pattern: "random"},
		{name: "wide/random/unaligned", width: 32, height: 16, stride: 37, curOff: 1, refOff: 5, pattern: "random"},
		{name: "rows/equal/unaligned", width: 48, height: 32, stride: 65, curOff: 15, refOff: 7, pattern: "equal"},
		{name: "extreme", width: 64, height: 48, stride: 96, curOff: 3, refOff: 19, pattern: "extreme"},
		{name: "checkerboard", width: 304, height: 320, stride: 320, curOff: 11, refOff: 23, pattern: "checkerboard"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			size := max(test.curOff, test.refOff) + int(test.stride*test.height) + 64
			cur := make([]uint8, size)
			ref := make([]uint8, size)
			rng := rand.New(rand.NewSource(int64(test.width)<<32 | int64(test.height)))
			rng.Read(cur)
			rng.Read(ref)

			for y := int32(0); y < test.height; y++ {
				curRow := test.curOff + int(y*test.stride)
				refRow := test.refOff + int(y*test.stride)
				for x := int32(0); x < test.width; x++ {
					switch test.pattern {
					case "equal":
						cur[curRow+int(x)] = ref[refRow+int(x)]
					case "extreme":
						cur[curRow+int(x)] = 0
						ref[refRow+int(x)] = 255
					case "checkerboard":
						cur[curRow+int(x)] = uint8((x+y)&1) * 255
						ref[refRow+int(x)] = uint8((x+y+1)&1) * 255
					}
				}
			}

			mbCount := int(test.width>>4) * int(test.height>>4)
			want := make([][4]int32, mbCount+2)
			got := make([][4]int32, mbCount+2)
			const sentinel = int32(0x5a5a5a5a)
			for i := range want {
				want[i] = [4]int32{sentinel, sentinel, sentinel, sentinel}
				got[i] = want[i]
			}
			curBefore := slices.Clone(cur)
			refBefore := slices.Clone(ref)
			wantFrame, gotFrame := int32(-1), int32(-1)

			VAACalcSad_c(cur, test.curOff, ref, test.refOff, test.width, test.height, test.stride, &wantFrame, want)
			VAACalcSad_sse2(cur, test.curOff, ref, test.refOff, test.width, test.height, test.stride, &gotFrame, got)

			if gotFrame != wantFrame {
				t.Fatalf("frame SAD = %d, want %d", gotFrame, wantFrame)
			}
			if !slices.Equal(got, want) {
				for i := range want {
					if got[i] != want[i] {
						t.Fatalf("macroblock %d SAD = %v, want %v", i, got[i], want[i])
					}
				}
			}
			if !slices.Equal(cur, curBefore) || !slices.Equal(ref, refBefore) {
				t.Fatal("VAA modified an input plane")
			}
		})
	}
}

func TestInitVaaFuncsSSE2(t *testing.T) {
	scalar := NewCVAACalculation(0)
	if got, want := reflect.ValueOf(scalar.m_sVaaFuncs.pfVAACalcSad).Pointer(),
		reflect.ValueOf(VAACalcSad_c).Pointer(); got != want {
		t.Fatalf("scalar SAD dispatch = %#x, want %#x", got, want)
	}

	simd := NewCVAACalculation(int32(common.WELS_CPU_SSE2))
	if got, want := reflect.ValueOf(simd.m_sVaaFuncs.pfVAACalcSad).Pointer(),
		reflect.ValueOf(VAACalcSad_sse2).Pointer(); got != want {
		t.Fatalf("SSE2 SAD dispatch = %#x, want %#x", got, want)
	}

	if got, want := reflect.ValueOf(simd.m_sVaaFuncs.pfVAACalcSadBgd).Pointer(),
		reflect.ValueOf(VAACalcSadBgd_c).Pointer(); got != want {
		t.Fatalf("SSE2 background SAD dispatch = %#x, want scalar %#x", got, want)
	}
	if got, want := reflect.ValueOf(simd.m_sVaaFuncs.pfVAACalcSadSsd).Pointer(),
		reflect.ValueOf(VAACalcSadSsd_c).Pointer(); got != want {
		t.Fatalf("SSE2 SSD dispatch = %#x, want scalar %#x", got, want)
	}
}

func TestVpFrameWorkUsesDetectedVAAFeatures(t *testing.T) {
	var result EResult
	framework := NewCVpFrameWork(1, &result)
	if result != RET_SUCCESS {
		t.Fatalf("NewCVpFrameWork result = %d", result)
	}
	defer framework.destroy()

	strategy, ok := framework.m_pStgChain[int(METHOD_VAA_STATISTICS)-1].(*CVAACalculation)
	if !ok {
		t.Fatalf("VAA strategy type = %T", framework.m_pStgChain[int(METHOD_VAA_STATISTICS)-1])
	}

	cores := int32(1)
	flags := common.WelsCPUFeatureDetect(&cores)
	if strategy.m_iCPUFlag != int32(flags) {
		t.Fatalf("VAA CPU flags = %#x, detected %#x", uint32(strategy.m_iCPUFlag), flags)
	}
	want := VAACalcSadFunc(VAACalcSad_c)
	if flags&common.WELS_CPU_SSE2 != 0 {
		want = VAACalcSad_sse2
	}
	if got := reflect.ValueOf(strategy.m_sVaaFuncs.pfVAACalcSad).Pointer(); got != reflect.ValueOf(want).Pointer() {
		t.Fatalf("VAA SAD dispatch = %#x, want %#x", got, reflect.ValueOf(want).Pointer())
	}
}
