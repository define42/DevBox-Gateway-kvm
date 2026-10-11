//go:build amd64

package encoder

import "github.com/define42/DevBox-Gateway-kvm/pkg/h264/internal/common"

// The assembly entry points use slices so the Go runtime keeps their backing
// arrays live for the duration of each call.

//go:noescape
func quant4x4SSE2(dct []int16, ff []int16, mf []int16)

// Scalar FF and MF arguments use int64 internally to keep the assembly ABI
// naturally aligned. Only their low int16 values are consumed.
//
//go:noescape
func quant4x4DcSSE2(dct []int16, ff int64, mf int64)

//go:noescape
func quantFour4x4SSE2(dct []int16, ff []int16, mf []int16)

//go:noescape
func quantFour4x4MaxSSE2(dct []int16, ff []int16, mf []int16, max []int16)

func WelsQuant4x4_sse2(dct []int16, ff []int16, mf []int16) {
	_ = dct[15]
	_ = ff[7]
	_ = mf[7]
	quant4x4SSE2(dct, ff, mf)
}

func WelsQuant4x4Dc_sse2(dct []int16, ff int16, mf int16) {
	_ = dct[15]
	quant4x4DcSSE2(dct, int64(ff), int64(mf))
}

func WelsQuantFour4x4_sse2(dct []int16, ff []int16, mf []int16) {
	_ = dct[63]
	_ = ff[7]
	_ = mf[7]
	quantFour4x4SSE2(dct, ff, mf)
}

func WelsQuantFour4x4Max_sse2(dct []int16, ff []int16, mf []int16, max []int16) {
	_ = dct[63]
	_ = ff[7]
	_ = mf[7]
	_ = max[3]
	quantFour4x4MaxSSE2(dct, ff, mf, max)
}

// initQuantizationSIMD replaces only kernels implemented for the detected
// instruction set. WelsInitEncodingFuncs calls this after installing scalar
// defaults.
func initQuantizationSIMD(funcs *SWelsFuncPtrList, cpuFlags uint32) {
	if cpuFlags&common.WELS_CPU_SSE2 == 0 {
		return
	}
	funcs.pfQuantization4x4 = WelsQuant4x4_sse2
	funcs.pfQuantizationDc4x4 = WelsQuant4x4Dc_sse2
	funcs.pfQuantizationFour4x4 = WelsQuantFour4x4_sse2
	funcs.pfQuantizationFour4x4Max = WelsQuantFour4x4Max_sse2
}
